package aten

import (
	"bytes"
	"context"
	"io"
	"net"
	"oldipmi/internal/transport"
	"testing"
	"time"
)

// ATEN sends power control in-band on the console connection: 0x1a and a code
// per power word (Phase 3 gives the words one type; this pins the codes).
func TestPowerInBand(t *testing.T) {
	for word, code := range map[string]byte{"off": 0, "on": 1, "reset": 2, "soft": 3} {
		a, b := net.Pipe()
		c := Client{wire: &transport.Wire{Conn: a}, controlling: true}
		done := make(chan error, 1)
		go func() { done <- c.Power(context.Background(), word) }()
		p := make([]byte, 2)
		if _, err := io.ReadFull(b, p); err != nil {
			t.Fatal(word, err)
		}
		if err := <-done; err != nil || !bytes.Equal(p, []byte{0x1a, code}) {
			t.Errorf("%s: sent % x (%v), want 1a %02x", word, p, err, code)
		}
		a.Close()
		b.Close()
	}
	// Refused before anything is sent: a write would run into the deadline.
	for _, c := range []struct {
		controlling bool
		action, err string
	}{
		{true, "bogus", "unknown power action"},
		{false, "bogus", "unknown power action"},
		{false, "on", "BMC granted view-only access"},
	} {
		a, b := net.Pipe()
		a.SetDeadline(time.Now().Add(100 * time.Millisecond))
		cl := Client{wire: &transport.Wire{Conn: a}, controlling: c.controlling}
		if err := cl.Power(context.Background(), c.action); err == nil || err.Error() != c.err {
			t.Errorf("%q, controlling %t: %v, want %q", c.action, c.controlling, err, c.err)
		}
		a.Close()
		b.Close()
	}
}
