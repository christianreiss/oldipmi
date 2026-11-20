package aten

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"oldipmi/internal/protocol"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// fakeKVM speaks the ATEN HTML5 (noVNC/AST2100) protocol over a WebSocket.
type fakeKVM struct {
	mu       sync.Mutex
	cookie   string
	auth     []byte
	received [][]byte
	updates  [][]byte // framebuffer updates to send; nil sends one that ends the frame at once
}

func (k *fakeKVM) handler(ws *websocket.Conn) {
	k.mu.Lock()
	k.cookie = ws.Request().Header.Get("Cookie")
	k.mu.Unlock()
	send := func(b []byte) { websocket.Message.Send(ws, b) }
	rd := &wsConn{Conn: ws}
	readN := func(n int) []byte { b := make([]byte, n); io.ReadFull(rd, b); return b }
	send([]byte("RFB 055.008\n"))
	readN(12)
	send([]byte{1, 16})
	readN(1)
	send(make([]byte, 24))
	auth := readN(48)
	k.mu.Lock()
	k.auth = auth
	k.mu.Unlock()
	send([]byte{0, 0, 0, 0})
	readN(1) // ClientInit
	init := make([]byte, 24)
	binary.BigEndian.PutUint16(init, 480)
	binary.BigEndian.PutUint16(init[2:], 640)
	binary.BigEndian.PutUint32(init[20:], 1)
	send(append(append(init, 'T'), make([]byte, 12)...))
	readN(10) // framebuffer update request
	// Unknown message in its own WebSocket message must be skipped.
	send([]byte{0x7f, 1, 2, 3})
	for _, u := range k.updates {
		send(u)
	}
	if k.updates == nil {
		// One AST2100 rectangle: 32x16, 4:2:2, stream that ends the frame at once.
		payload := append([]byte{4, 7, 0x01, 0xa6}, 0, 0, 0, 0x90, 0, 0, 0, 0)
		hdr := make([]byte, 20)
		binary.BigEndian.PutUint16(hdr[4:], 32)
		binary.BigEndian.PutUint16(hdr[6:], 16)
		binary.BigEndian.PutUint32(hdr[8:], encAST2100)
		binary.BigEndian.PutUint32(hdr[16:], uint32(len(payload)))
		send(append(append([]byte{0, 0, 0, 1}, hdr...), payload...))
	}
	for {
		m := readN(1)
		if m[0] == 4 || m[0] == 5 {
			msg := append(m, readN(17)...)
			k.mu.Lock()
			k.received = append(k.received, msg)
			k.mu.Unlock()
			continue
		}
		if m[0] == 3 { // next update request
			readN(9)
			continue
		}
		if m[0] == 0x37 { // mouse sync: 0x37, 7, 0x0780
			readN(3)
			continue
		}
		return
	}
}

func TestHTML5ConsoleSession(t *testing.T) {
	k := &fakeKVM{}
	srv := httptest.NewTLSServer(websocket.Server{Handler: k.handler, Handshake: func(*websocket.Config, *http.Request) error { return nil }})
	defer srv.Close()
	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	port, _ := strconv.Atoi(portStr)
	p := protocol.Profile{Host: host, Family: "aten", LegacyTLS: true}
	l := Launch{HTML5: true, Token: "tok-123", Cookie: "SID=abc", Port: port, Secure: true}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := &Client{p: p, launch: l, controlling: true}
	var err error
	if c.wire, err = dialHTML5(ctx, p, l); err != nil {
		t.Fatal(err)
	}
	defer c.wire.Close()
	if err = c.negotiate(ctx); err != nil {
		t.Fatal(err)
	}
	got := make(chan protocol.Frame, 1)
	go c.Run(ctx, func(f protocol.Frame) { got <- f })
	select {
	case f := <-got:
		if f.Width != 32 || f.Height != 16 || len(f.Pixels) != 32*16*4 || f.Pixels[3] != 255 {
			t.Fatalf("frame %dx%d len %d", f.Width, f.Height, len(f.Pixels))
		}
	case <-ctx.Done():
		t.Fatal("no frame")
	}
	if err = c.Key(protocol.Input{Usage: 81, Down: true}); err != nil {
		t.Fatal(err)
	}
	if err = c.Pointer(protocol.Input{X: 300, Y: 200, Buttons: 1}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		k.mu.Lock()
		n := len(k.received)
		k.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.cookie != "SID=abc" {
		t.Errorf("cookie %q", k.cookie)
	}
	if !bytes.Equal(k.auth[:7], []byte("tok-123")) || !bytes.Equal(k.auth[7:], make([]byte, 41)) {
		t.Errorf("auth %x", k.auth)
	}
	if len(k.received) < 2 || k.received[0][0] != 4 || k.received[0][2] != 1 || binary.BigEndian.Uint32(k.received[0][5:]) != 81 {
		t.Fatalf("key message %x", k.received)
	}
	if k.received[1][0] != 5 || k.received[1][2] != 1 || binary.BigEndian.Uint16(k.received[1][3:]) != 300 || binary.BigEndian.Uint16(k.received[1][5:]) != 200 {
		t.Errorf("pointer message %x", k.received[1])
	}
	if err := c.Mount(ctx, "/x.iso"); err == nil {
		t.Error("media must be refused in HTML5 mode")
	}
}
