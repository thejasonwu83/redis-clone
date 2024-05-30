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

var db = make(map[string]string)
var expiry = make(map[string]time.Time)
var hasExpiry = make(map[string]bool)

func INFO_REPL(conn net.Conn) {
	_, err := conn.Write([]byte("$11\r\nrole:master\r\n"))
	if err != nil {
		fmt.Println("Error writing to connection: ", err.Error())
	}
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

func SET(conn net.Conn, key, value string) {
	db[key] = value
	hasExpiry[key] = false
	_, err := conn.Write([]byte("+OK\r\n"))
	if err != nil {
		fmt.Println("Error writing to connection: ", err.Error())
	}
}

func SETPX(conn net.Conn, key, value string, ms int, requestTime time.Time) {
	db[key] = value
	hasExpiry[key] = true
	expireTime := requestTime.Add(time.Duration(ms * int(time.Millisecond)))
	expiry[key] = expireTime
	_, err := conn.Write([]byte("+OK\r\n"))
	if err != nil {
		fmt.Println("Error writing to connection: ", err.Error())
	}
}

// what happens after expiration? delete?
func GET(conn net.Conn, key string, requestTime time.Time) {
	if hasExpiry[key] && time.Now().Compare(expiry[key]) > 0 {
		_, err := conn.Write([]byte("$-1\r\n"))
		if err != nil {
			fmt.Println("Error writing to connection: ", err.Error())
		}
	} else {
		value := db[key]
		output := fmt.Sprintf("$%d\r\n%s\r\n", len(value), value)
		_, err := conn.Write([]byte(output))
		if err != nil {
			fmt.Println("Error writing to connection: ", err.Error())
		}
	}
}

// refactor this ugly piece of shit function for the love of god
// add error handling for faulty inputs (e.g. index out of bounds)
func handleRequest(conn net.Conn, fields []string, requestTime time.Time) error {
	fmt.Println("Input:", fields) // debug
	for i := 0; i < len(fields); {
		field := fields[i]
		switch rune(field[0]) /* first byte */ {
		case ARRAY:
			numElem, _ := strconv.Atoi(field[1:])
			if err := handleRequest(conn, fields[i+1:2*numElem+i+1], requestTime); err != nil {
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
					SETPX(conn, key, value, time, requestTime)
					i += 10
				} else {
					SET(conn, key, value)
					i += 6
				}
			case "GET":
				key := fields[i+3]
				GET(conn, key, requestTime)
				i += 4
			case "INFO":
				infoType := fields[i+3]
				if strings.ToLower(infoType) == "replication" {
					INFO_REPL(conn)
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

func handleConnection(conn net.Conn) error {
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
		handleRequest(conn, parseFields(input), requestTime)
		input = make([]byte, 1024)
		_, err = conn.Read(input)
	}
	return nil
}

func connect(l net.Listener) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting connection: ", err.Error())
			return err
		}
		go handleConnection(conn)
	}
}

func startServer(port int) error {

	l, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		fmt.Println("Failed to bind to port", strconv.Itoa(port))
		return err
	}
	defer func() error {
		err = l.Close()
		if err != nil {
			fmt.Println("Error closing listener: ", err.Error())
		}
		return err
	}()
	return connect(l)
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
		default:
			fmt.Println("Error: unknown command line parameter")
		}
	}
	return args
}

func main() {
	args := parseArgs()
	port, _ := strconv.Atoi(args["--port"])
	if args["--port"] == "" {
		port = 6379
	}
	if startServer(port) != nil {
		os.Exit(1)
	}
}
