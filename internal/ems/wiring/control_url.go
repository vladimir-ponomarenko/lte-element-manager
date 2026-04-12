package wiring

import (
	"net"
	"strings"
)

func controlLocalURL(addr string) string {
	a := strings.TrimSpace(addr)
	if a == "" {
		return ""
	}

	if strings.HasPrefix(a, ":") {
		return "http://127.0.0.1" + a
	}

	host, port, err := net.SplitHostPort(a)
	if err == nil && port != "" {
		_ = host
		return "http://127.0.0.1:" + port
	}

	if strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://") {
		return a
	}
	return "http://127.0.0.1" + "/" + strings.TrimPrefix(a, "/")
}
