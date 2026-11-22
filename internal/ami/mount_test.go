package ami

import (
	"context"
	"io"
	"net"
	"oldipmi/internal/ami/iusb"
	"oldipmi/internal/protocol"
	"oldipmi/internal/scsi"
	"oldipmi/internal/testkit"
	"oldipmi/internal/transport"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// isoFile writes an empty ISO of two sectors.
func isoFile(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "install.iso")
	if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// mediaBMC serves one virtual media connection: it reads the client's
// authentication (an IUSB header and 128 bytes), closes authed, waits for ack
// to close and accepts the redirection. Then it hands the connection over.
func mediaBMC(ln net.Listener, ack <-chan struct{}) (conns <-chan net.Conn, authed <-chan struct{}) {
	c, a := make(chan net.Conn, 1), make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		if _, err := io.ReadFull(conn, make([]byte, iusb.HeaderLen+128)); err != nil {
			conn.Close()
			return
		}
		close(a)
		<-ack
		// The ACK, with status 1 (accepted) at payload[30].
		p := iusbPacket(0xf1)
		p[iusb.HeaderLen+30] = 1
		conn.Write(p)
		c <- conn
	}()
	return c, a
}

// iusbPacket is a packet of the BMC with the opcode op at payload[9].
func iusbPacket(op byte) []byte {
	p := make([]byte, iusb.HeaderLen+32)
	h := iusb.Header{Major: iusb.Major, Minor: iusb.Minor, DataPacketLen: 32}
	h.Marshal(p)
	p[iusb.HeaderLen+9] = op
	return p
}

func mediaClient(port int) *Client {
	return client(protocol.Profile{Host: "127.0.0.1", AllowPlainConsole: true}, Launch{Token: "media-token", CDPort: port})
}

// Mount's context bounds the setup only: the ISO stays mounted after it ends,
// until Eject.
func TestMountLastsUntilEject(t *testing.T) {
	ln, port := listen(t, nil)
	ack := make(chan struct{})
	close(ack)
	conns, _ := mediaBMC(ln, ack)
	c := mediaClient(port)
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	if err := c.Mount(ctx, isoFile(t)); err != nil {
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
	time.Sleep(100 * time.Millisecond) // the media's goroutine ends meanwhile
	if err := c.MediaErr(); err != nil {
		t.Fatalf("MediaErr after Eject: %v", err)
	}
}

// When the virtual media ends by itself, the ISO counts as gone and MediaErr
// says why; an Eject clears it. An eject by the server is acknowledged first.
func TestMediaEndsByItself(t *testing.T) {
	eject := iusbPacket(scsi.StartStopUnit)
	eject[iusb.HeaderLen+9+4] = 2 // LoEj set, Start clear
	for _, tc := range []struct {
		name  string
		end   func(net.Conn)
		want  string
		acked bool
	}{
		{"connection lost", func(conn net.Conn) { conn.Close() }, "vmedia: read request: EOF", false},
		{"killed by the BMC", func(conn net.Conn) { conn.Write(iusbPacket(0xf6)) }, "the BMC ended the virtual media", false},
		{"ejected by the server", func(conn net.Conn) { conn.Write(eject) }, "the server ejected the virtual CD", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ln, port := listen(t, nil)
			ack := make(chan struct{})
			close(ack)
			conns, _ := mediaBMC(ln, ack)
			c := mediaClient(port)
			defer c.Close()
			if err := c.Mount(context.Background(), isoFile(t)); err != nil {
				t.Fatal(err)
			}
			if err := c.MediaErr(); err != nil {
				t.Fatalf("MediaErr while mounted: %v", err)
			}
			conn := <-conns
			defer conn.Close()
			tc.end(conn)
			for deadline := time.Now().Add(5 * time.Second); c.Mounted(); time.Sleep(time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("still mounted")
				}
			}
			if err := c.MediaErr(); err == nil || err.Error() != tc.want {
				t.Fatalf("MediaErr %v, want %q", err, tc.want)
			}
			if tc.acked { // the answer to the eject
				b := make([]byte, iusb.HeaderLen)
				if _, err := io.ReadFull(conn, b); err != nil {
					t.Fatalf("no answer to the eject before the end: %v", err)
				}
				if h, err := iusb.ParseHeader(b); err != nil || h.Direction != 128 {
					t.Fatalf("the answer to the eject: %+v, %v", h, err)
				}
			}
			c.Eject()
			if err := c.MediaErr(); err != nil {
				t.Fatalf("MediaErr after Eject: %v", err)
			}
		})
	}
}

// While Mount waits for the BMC, input goes on and Mounted says no. An Eject
// meanwhile waits for the Mount, so it ejects the ISO that Mount mounted.
func TestInputGoesOnWhileMounting(t *testing.T) {
	ln, port := listen(t, nil)
	ack := make(chan struct{})
	accept := sync.OnceFunc(func() { close(ack) })
	conns, authed := mediaBMC(ln, ack)
	c := mediaClient(port)
	console, bmc := net.Pipe()
	go io.Copy(io.Discard, bmc)
	c.wire = &transport.Wire{Conn: console}
	defer c.Close()
	defer accept()
	iso := isoFile(t)
	mounted := make(chan error, 1)
	go func() { mounted <- c.Mount(context.Background(), iso) }()
	<-authed
	keyed := make(chan error, 1)
	go func() { keyed <- c.Key(protocol.Input{Usage: 4, Down: true}) }()
	select {
	case err := <-keyed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a key waited for the mount")
	}
	if c.Mounted() {
		t.Fatal("mounted before the BMC accepted the redirection")
	}
	ejected := make(chan struct{})
	go func() { c.Eject(); close(ejected) }()
	time.Sleep(50 * time.Millisecond)
	select {
	case <-ejected:
		t.Fatal("Eject did not wait for the Mount")
	default:
	}
	accept()
	if err := <-mounted; err != nil {
		t.Fatal(err)
	}
	<-ejected
	conn := <-conns
	defer conn.Close()
	if !testkit.Closed(t, conn, 5*time.Second) || c.Mounted() {
		t.Fatal("the ISO stayed mounted after Eject")
	}
}
