package main

import (
	"fmt"
	"net"
	"os"
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
		fmt.Printf("Получен пакет метрик: %s\n", string(buf[:n]))
	}
}
