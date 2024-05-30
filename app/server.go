package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	ARRAY       = '*'
	BULK_STRING = '$'
)

// refactor this ugly piece of shit function for the love of god
// add error handling for faulty inputs (e.g. index out of bounds)
func handleRequest(conn net.Conn, fields []string) error {
	for i := 0; i < len(fields); {
		field := fields[i]
		switch field[0] /* first byte */ {
		case ARRAY:
			numElem, _ := strconv.Atoi(field[1:])
			if err := handleRequest(conn, fields[i+1:2*numElem+i+1]); err != nil {
				fmt.Println("Error parsing client request: ", err.Error())
				return err
			}
			i += (2*numElem + 1)
		case BULK_STRING:
			switch strings.ToUpper(fields[i+1]) {
			case "PING":
				_, err := conn.Write([]byte("+PONG\r\n"))
				if err != nil {
					fmt.Println("Error writing to connection: ", err.Error())
					return err
				}
				i += 2
			case "ECHO":
				argLen, _ := strconv.Atoi(fields[i+2][1:])
				arg := fields[i+3]
				output := fmt.Sprintf("$%d\r\n%s\r\n", argLen, arg)
				_, err := conn.Write([]byte(output))
				if err != nil {
					fmt.Println("Error writing to connection: ", err.Error())
				}
				i += 4
			default:
				fmt.Println("Error parsing client request: command not found or supported")
				return errors.New("command not found or supported")
			}
		default:
			fmt.Println("Error parsing client request: RESP data type not found or supported")
			return errors.New("RESP data type not found or supported")
		}
	}
	return nil
}

func parseFields(input []byte) []string {
	return strings.Split(string(input), "\r\n")
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
		handleRequest(conn, parseFields(input))
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

func startServer() error {
	l, err := net.Listen("tcp", "0.0.0.0:6379")
	if err != nil {
		fmt.Println("Failed to bind to port 6379")
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

func main() {
	if startServer() != nil {
		os.Exit(1)
	}
}
