package ami

import (
	"bytes"
	"errors"
	"net"
	"oldipmi/internal/hid"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"testing"
)

// AMI sends the keyboard as a USB boot report in an IUSB packet after every
// key event (hid.Pressed builds it); Release sends an empty report, then a
// pointer report without buttons.
func TestKeyboardReports(t *testing.T) {
	srv, cli := net.Pipe()
	defer srv.Close()
	c := client(protocol.Profile{}, Launch{})
	c.wire = &transport.Wire{Conn: cli}
	bmc := end{conn: srv}
	async := func(f func() error) <-chan error {
		done := make(chan error, 1)
		go func() { done <- f() }()
		return done
	}
	report := func() []byte {
		t.Helper()
		typ, b, err := bmc.read()
		if err != nil || typ != opHIDPkt || len(b) < 33 || string(b[:8]) != "IUSB    " || int(b[32]) != len(b)-33 {
			t.Fatalf("packet %d % x: %v", typ, b, err)
		}
		return b[33:]
	}
	for _, e := range []struct {
		usage uint32
		down  bool
		want  []byte
	}{
		{225, true, []byte{2, 0, 0, 0, 0, 0, 0, 0}}, // Shift
		{9, true, []byte{2, 0, 9, 0, 0, 0, 0, 0}},
		{4, true, []byte{2, 0, 4, 9, 0, 0, 0, 0}}, // lowest usage first
		{225, false, []byte{0, 0, 4, 9, 0, 0, 0, 0}},
	} {
		done := async(func() error { return c.Key(protocol.Input{Usage: e.usage, Down: e.down}) })
		if got := report(); !bytes.Equal(got, e.want) {
			t.Errorf("key %d down=%t: report % x, want % x", e.usage, e.down, got, e.want)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	// Refused without a packet: the next one is the release.
	if err := c.Key(protocol.Input{Usage: 232, Down: true}); !errors.Is(err, hid.ErrInvalidUsage) {
		t.Fatal(err)
	}
	done := async(c.Release)
	if got := report(); !bytes.Equal(got, make([]byte, 8)) {
		t.Errorf("release: keyboard report % x", got)
	}
	if got := report(); !bytes.Equal(got, make([]byte, 6)) {
		t.Errorf("release: pointer report % x", got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
