package main

import (
	"fmt"
	"net"
	"os"
)

func main() {
	// println to debug
	l, err := net.Listen("tcp", "0.0.0.0:6379")
	if err != nil {
		fmt.Println("Failed to bind to port 6379")
		os.Exit(1)
	}
	conn, err := l.Accept()
	if err != nil {
		fmt.Println("Error accepting connection: ", err.Error())
		os.Exit(1)
	}
	defer l.Close()
	defer conn.Close()
	conn.Write([]byte("+PONG\r\n"))

}
