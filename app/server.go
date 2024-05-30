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
	port      int
	db        map[string]string
	expiry    map[string]time.Time
	hasExpiry map[string]bool
	isMaster  bool
	// master    *Server
}

func writeFields(conn net.Conn, fields map[string]string) {
	var output string
	for key, val := range fields {
		body := key + ":" + val
		output += fmt.Sprintf("$%d\r\n%s\r\n", len(body), body)
	}
	_, err := conn.Write([]byte(output))
	if err != nil {
		fmt.Println("Error writing to connection: ", err.Error())
	}
}

func INFO_REPL(conn net.Conn, server *Server) {
	info := make(map[string]string)
	if server.isMaster {
		info["role"] = "master"
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
	l, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(s.port))
	if err != nil {
		fmt.Println("Failed to bind to port", strconv.Itoa(s.port))
		return err
	}
	defer func() error {
		err = l.Close()
		if err != nil {
			fmt.Println("Error closing listener: ", err.Error())
		}
		return err
	}()
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

func newServer(port int, isMaster bool) *Server {
	return &Server{port,
		make(map[string]string),
		make(map[string]time.Time),
		make(map[string]bool),
		isMaster}
}

func main() {
	args := parseArgs()
	port, _ := strconv.Atoi(args["--port"])
	if args["--port"] == "" {
		port = 6379
	}
	var server *Server
	if master := args["--replicaof"]; master != "" { // server is slave
		// masterHost := strings.Split(master, " ")
		// masterPort := strings.Split(master, " ")[1]
		server = newServer(port, false)

	} else { // server is master
		server = newServer(port, true)
	}
	if server.startServer() != nil {
		os.Exit(1)
	}
}
