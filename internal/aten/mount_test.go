package aten

import (
	"context"
	"net"
	"oldipmi/internal/protocol"
	"oldipmi/internal/testkit"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Mount's context bounds the media connection's setup only: the ISO stays
// mounted after it ends, until Eject.
func TestMountLastsUntilEject(t *testing.T) {
	iso := filepath.Join(t.TempDir(), "boot.iso")
	if err := os.WriteFile(iso, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	conns := make(chan net.Conn, 1)
	go func() {
		if conn, err := ln.Accept(); err == nil {
			conns <- conn
		}
	}()
	c := &Client{p: protocol.Profile{Host: "127.0.0.1", AllowPlainConsole: true}, launch: Launch{CDPort: ln.Addr().(*net.TCPAddr).Port}}
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	if err := c.Mount(ctx, iso); err != nil {
		t.Fatal(err)
	}
	conn := <-conns
	defer conn.Close()
	cancel()
	if testkit.Closed(t, conn, 200*time.Millisecond) || !c.Mounted() {
		t.Fatal("the ISO was ejected when Mount's context ended")
	}
	c.Eject()
	if !testkit.Closed(t, conn, 5*time.Second) || c.Mounted() {
		t.Fatal("the ISO stayed mounted after Eject")
	}
}
