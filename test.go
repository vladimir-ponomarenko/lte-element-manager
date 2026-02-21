package main

import (
	"fmt"
	"net"
	"os"
	"strings"
)

func main() {
	socketPath := "/var/run/enb-metrics/enb_metrics.uds"
	if v := os.Getenv("SOCKET_PATH"); v != "" {
		socketPath = v
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		panic(err)
	}
	conn, err := net.ListenPacket("unixgram", socketPath)
	if err != nil {
		panic(err)
	}
	defer os.Remove(socketPath)
	defer conn.Close()

	buf := make([]byte, 65535)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			continue
		}
		msg := strings.ReplaceAll(string(buf[:n]), "\n", " ")
		fmt.Println(msg)
	}
}
