//go:build netconf

package netconf

/*
#cgo LDFLAGS: -L${SRCDIR}/../../../.local/lib -lnetconf2 -lyang
*/
import "C"

// Enabled reports whether NETCONF support is built in.
func Enabled() bool { return true }
