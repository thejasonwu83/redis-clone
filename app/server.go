package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
)

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
		if _, err = conn.Write([]byte("+PONG\r\n")); err != nil {
			fmt.Println("Error writing to connection: ", err.Error())
			return err
		}
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
