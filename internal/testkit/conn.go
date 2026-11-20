package testkit

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// Closed waits up to d at the BMC's end of conn, whose client end sends
// nothing, and reports whether the client closed its end: true at the end of
// the stream, false when d passes first.
func Closed(t testing.TB, conn net.Conn, d time.Duration) bool {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(d))
	_, err := conn.Read(make([]byte, 1))
	var ne net.Error
	switch {
	case errors.Is(err, io.EOF):
		return true
	case errors.As(err, &ne) && ne.Timeout():
		return false
	}
	t.Fatalf("the client sent data or failed: %v", err)
	return false
}
