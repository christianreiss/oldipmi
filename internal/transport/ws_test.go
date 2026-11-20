package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// A BMC that accepts the connection but never answers the WebSocket handshake
// does not hold the dial: when the context ends, DialWS returns its error and
// closes the connection, which the BMC sees as the end of the stream.
func TestDialWSEndsWithTheContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			accepted <- c
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := DialWS(ctx, Policy{Host: "127.0.0.1", HTTP: true}, ln.Addr().(*net.TCPAddr).Port, false, "")
		done <- err
	}()
	var bmc net.Conn
	select {
	case bmc = <-accepted:
		defer bmc.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("DialWS did not connect")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DialWS returned %v, want the context's error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DialWS did not end with its context")
	}
	bmc.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadAll(bmc); err != nil {
		t.Fatalf("the connection stayed open: %v", err)
	}
}
