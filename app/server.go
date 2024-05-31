package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	ARRAY       = '*'
	BULK_STRING = '$'
)

type Server struct {
	port      string
	db        map[string]string
	expiry    map[string]time.Time
	hasExpiry map[string]bool
	isMaster  bool
	master    *Server
}

func pingMaster(conn net.Conn) {
	_, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n"))
	if err != nil {
		fmt.Println("Error pinging master:", err.Error())
	}
	input := make([]byte, 1024)
	_, err = conn.Read(input)
	if err != nil {
		fmt.Println("Error reading input from master:", err.Error())
	}
	if string(input) != "+PONG\r\n" {
		fmt.Println("Error receiving PONG from master: got", string(input))
	}
}

func (s *Server) REPLCONF(conn net.Conn) {
	port := strings.Split(s.port, ":")[1]
	output := fmt.Sprintf("*3\r\n$8\r\nREPLCONF\r\n$14\r\nlistening-port\r\n$4\r\n%s\r\n", port)
	_, err := conn.Write([]byte(output))
	if err != nil {
		fmt.Println("Error sending REPLCONF to master:", err.Error())
	}
	input := make([]byte, 1024)
	_, err = conn.Read(input)
	if err != nil {
		fmt.Println("Error receiving response from master:", err.Error())
	}
	if string(input) != "+OK\r\n" {
		fmt.Println("Error receiving OK response from master: got", string(input))
	}
	_, err = conn.Write([]byte("*3\r\n$8\r\nREPLCONF\r\n$4\r\ncapa\r\n$6\r\npsync2\r\n"))
	if err != nil {
		fmt.Println("Error sending REPLCONF to master:", err.Error())
	}
	input = make([]byte, 1024)
	_, err = conn.Read(input)
	if err != nil {
		fmt.Println("Error receiving response from master:", err.Error())
	}
	if string(input) != "+OK\r\n" {
		fmt.Println("Error receiving OK response from master: got", string(input))
	}
}

func (s *Server) handshake() error {
	if s.isMaster {
		return errors.New("server is a slave to no master")
	}
	conn, err := net.Dial("tcp", s.master.port)
	if err != nil {
		fmt.Println("Error dialing master's port:", err.Error())
		return err
	}
	pingMaster(conn)
	s.REPLCONF(conn)
	return nil
}

func writeFields(conn net.Conn, fields map[string]string) {
	var output string
	for key, val := range fields {
		body := key + ":" + val
		output += fmt.Sprintf("%s\n", body)
	}
	output = output[:len(output)-1]
	output = "$" + strconv.Itoa(len(output)) + "\r\n" + output + "\r\n"
	_, err := conn.Write([]byte(output))
	if err != nil {
		fmt.Println("Error writing to connection: ", err.Error())
	}
}

func INFO_REPL(conn net.Conn, server *Server) {
	info := make(map[string]string)
	if server.isMaster {
		info["role"] = "master"
		info["master_replid"] = "8371b4fb1155b71f4a04d3e1bc3e18c4a990aeeb" // pseudorandom: i chose it randomly
		info["master_repl_offset"] = "0"
	} else {
		info["role"] = "slave"
	}
	writeFields(conn, info)
}

func PING(conn net.Conn) {
	_, err := conn.Write([]byte("+PONG\r\n"))
	if err != nil {
		fmt.Println("Error writing to connection: ", err.Error())
	}
}

func ECHO(conn net.Conn, argLen int, arg string) {
	output := fmt.Sprintf("$%d\r\n%s\r\n", argLen, arg)
	_, err := conn.Write([]byte(output))
	if err != nil {
		fmt.Println("Error writing to connection: ", err.Error())
	}
}

func SET(conn net.Conn, key, value string, server *Server) {
	server.db[key] = value
	server.hasExpiry[key] = false
	_, err := conn.Write([]byte("+OK\r\n"))
	if err != nil {
		fmt.Println("Error writing to connection: ", err.Error())
	}
}

func SETPX(conn net.Conn, key, value string, ms int, requestTime time.Time, server *Server) {
	server.db[key] = value
	server.hasExpiry[key] = true
	expireTime := requestTime.Add(time.Duration(ms * int(time.Millisecond)))
	server.expiry[key] = expireTime
	_, err := conn.Write([]byte("+OK\r\n"))
	if err != nil {
		fmt.Println("Error writing to connection: ", err.Error())
	}
}

// what happens after expiration? delete?
func GET(conn net.Conn, key string, requestTime time.Time, server *Server) {
	if server.hasExpiry[key] && time.Now().Compare(server.expiry[key]) > 0 {
		_, err := conn.Write([]byte("$-1\r\n"))
		if err != nil {
			fmt.Println("Error writing to connection: ", err.Error())
		}
	} else {
		value := server.db[key]
		output := fmt.Sprintf("$%d\r\n%s\r\n", len(value), value)
		_, err := conn.Write([]byte(output))
		if err != nil {
			fmt.Println("Error writing to connection: ", err.Error())
		}
	}
}

// refactor this ugly piece of shit function for the love of god
// add error handling for faulty inputs (e.g. index out of bounds)
func handleRequest(conn net.Conn, fields []string, requestTime time.Time, server *Server) error {
	fmt.Println("Input:", fields) // debug
	for i := 0; i < len(fields); {
		field := fields[i]
		switch rune(field[0]) /* first byte */ {
		case ARRAY:
			numElem, _ := strconv.Atoi(field[1:])
			if err := handleRequest(conn, fields[i+1:2*numElem+i+1], requestTime, server); err != nil {
				fmt.Println("Error parsing client request:", err.Error())
				return err
			}
			i = 2*numElem + i + 1
		case BULK_STRING:
			switch strings.ToUpper(fields[i+1]) {
			case "PING":
				PING(conn)
				i += 2
			case "ECHO":
				argLen, _ := strconv.Atoi(fields[i+2][1:])
				arg := fields[i+3]
				ECHO(conn, argLen, arg)
				i += 4
			case "SET":
				key := fields[i+3]
				value := fields[i+5]
				if i+7 < len(fields) && strings.ToUpper(fields[i+7]) == "PX" {
					time, _ := strconv.Atoi(fields[i+9])
					SETPX(conn, key, value, time, requestTime, server)
					i += 10
				} else {
					SET(conn, key, value, server)
					i += 6
				}
			case "GET":
				key := fields[i+3]
				GET(conn, key, requestTime, server)
				i += 4
			case "INFO":
				infoType := fields[i+3]
				if strings.ToLower(infoType) == "replication" {
					INFO_REPL(conn, server)
				}
				i += 4
			default:
				fmt.Println("Error parsing client request:", strings.ToUpper(fields[i+1]), "not found or supported")
				return errors.New("command not found or supported")
			}
		default:
			fmt.Println("Error parsing client request:", string(field[0]), "not found or supported")
			return errors.New("RESP data type not found or supported")
		}
	}
	return nil
}

func parseFields(input []byte) []string {
	fields := strings.Split(string(input), "\r\n")
	return fields[:len(fields)-1]
}

func handleConnection(conn net.Conn, server *Server) error {
	defer func() error {
		err := conn.Close()
		if err != nil {
			fmt.Println("Error closing connection: ", err.Error())
		}
		return err
	}()
	input := make([]byte, 1024)
	_, err := conn.Read(input)
	for !errors.Is(err, io.EOF) {
		if err != nil {
			fmt.Println("Error reading from connection: ", err.Error())
			return err
		}
		requestTime := time.Now()
		handleRequest(conn, parseFields(input), requestTime, server)
		input = make([]byte, 1024)
		_, err = conn.Read(input)
	}
	return nil
}

func connect(l net.Listener, server *Server) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			return err
		}
		go handleConnection(conn, server)
	}
}

func (s *Server) startServer() error {
	l, err := net.Listen("tcp", s.port)
	if err != nil {
		fmt.Println("Failed to bind to port", s.port)
		return err
	}
	defer func() error {
		err = l.Close()
		if err != nil {
			fmt.Println("Error closing listener: ", err.Error())
		}
		return err
	}()
	if !s.isMaster {
		if err = s.handshake(); err != nil {
			fmt.Println("Error establishing handshake with master:", err.Error())
		}
	}
	return connect(l, s)
}

func parseArgs() map[string]string {
	args := make(map[string]string)
	if len(os.Args[1:]) == 0 {
		return args
	}
	for i := 1; i < len(os.Args); {
		arg := os.Args[i]
		switch arg {
		case "--port":
			if i+1 < len(os.Args) {
				args[arg] = os.Args[i+1]
			}
			i += 2
		case "--replicaof":
			if i+1 < len(os.Args) {
				args[arg] = os.Args[i+1]
			}
			i += 2
		default:
			fmt.Println("Error: unknown command line parameter")
		}
	}
	return args
}

func newServer(port string, isMaster bool, master *Server) *Server {
	return &Server{port,
		make(map[string]string),
		make(map[string]time.Time),
		make(map[string]bool),
		isMaster,
		master}
}

func main() {
	args := parseArgs()
	port := "0.0.0.0:" + args["--port"]
	if args["--port"] == "" {
		port = "0.0.0.0:6379"
	}
	var server *Server
	if masterPort := args["--replicaof"]; masterPort != "" { // server is slave
		masterHost := strings.Split(masterPort, " ")[0]
		if masterHost == "localhost" {
			masterPort = "0.0.0.0:" + strings.Split(masterPort, " ")[1]
		}
		master := newServer(masterPort, true, nil)
		server = newServer(port, false, master)
	} else { // server is master
		server = newServer(port, true, nil)
	}
	if server.startServer() != nil {
		os.Exit(1)
	}
}
