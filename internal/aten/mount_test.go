package aten

import (
	"context"
	"io"
	"net"
	"oldipmi/internal/protocol"
	"oldipmi/internal/scsi"
	"oldipmi/internal/testkit"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mount mounts an empty ISO on a client whose media channel goes to a
// listener of the test, and returns the BMC's end of that channel.
func mount(t *testing.T, ctx context.Context) (*Client, net.Conn) {
	t.Helper()
	iso := filepath.Join(t.TempDir(), "boot.iso")
	if err := os.WriteFile(iso, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	conns := make(chan net.Conn, 1)
	go func() {
		if conn, err := ln.Accept(); err == nil {
			conns <- conn
		}
	}()
	c := &Client{p: protocol.Profile{Host: "127.0.0.1", AllowPlainConsole: true}, launch: Launch{CDPort: ln.Addr().(*net.TCPAddr).Port}}
	t.Cleanup(func() { c.Close() })
	if err := c.Mount(ctx, iso); err != nil {
		t.Fatal(err)
	}
	conn := <-conns
	t.Cleanup(func() { conn.Close() })
	return c, conn
}

// Mount's context bounds the media connection's setup only: the ISO stays
// mounted after it ends, until Eject.
func TestMountLastsUntilEject(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c, conn := mount(t, ctx)
	cancel()
	if testkit.Closed(t, conn, 200*time.Millisecond) || !c.Mounted() {
		t.Fatal("the ISO was ejected when Mount's context ended")
	}
	c.Eject()
	if !testkit.Closed(t, conn, 5*time.Second) || c.Mounted() {
		t.Fatal("the ISO stayed mounted after Eject")
	}
	time.Sleep(100 * time.Millisecond) // the media's goroutine ends meanwhile
	if err := c.MediaErr(); err != nil {
		t.Fatalf("MediaErr after Eject: %v", err)
	}
}

// When the virtual media ends by itself, the ISO counts as gone and MediaErr
// says why; an Eject clears it. An eject by the server is acknowledged first.
func TestMediaEndsByItself(t *testing.T) {
	eject := make([]byte, 31) // a USB command wrapper: START STOP UNIT, LoEj set, Start clear
	copy(eject, "USBC")
	eject[14], eject[15], eject[19] = 16, scsi.StartStopUnit, 2
	for _, tc := range []struct {
		name  string
		end   func(net.Conn)
		want  string
		acked bool
	}{
		{"connection lost", func(conn net.Conn) { conn.Close() }, "ATEN virtual media: EOF", false},
		{"ended by the BMC", func(conn net.Conn) { conn.Write(mediaHeader(0, 4, 0)) }, "the BMC ended the virtual media", false},
		{"ejected by the server", func(conn net.Conn) { conn.Write(append(mediaHeader(0, 0, 31), eject...)) },
			"the server ejected the virtual CD", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, conn := mount(t, context.Background())
			if err := c.MediaErr(); err != nil {
				t.Fatalf("MediaErr while mounted: %v", err)
			}
			tc.end(conn)
			for deadline := time.Now().Add(5 * time.Second); c.Mounted(); time.Sleep(time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("still mounted")
				}
			}
			if err := c.MediaErr(); err == nil || err.Error() != tc.want {
				t.Fatalf("MediaErr %v, want %q", err, tc.want)
			}
			if tc.acked { // the status of the eject: passed
				csw := make([]byte, 8+13)
				if _, err := io.ReadFull(conn, csw); err != nil || string(csw[8:12]) != "USBS" || csw[20] != 0 {
					t.Fatalf("no answer to the eject before the end: % x, %v", csw, err)
				}
			}
			c.Eject()
			if err := c.MediaErr(); err != nil {
				t.Fatalf("MediaErr after Eject: %v", err)
			}
		})
	}
}
