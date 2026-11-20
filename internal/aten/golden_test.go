package aten

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"oldipmi/internal/protocol"
	"oldipmi/internal/testkit"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// Golden frames: what a session shows for a run of framebuffer updates,
// compared with the pictures in testdata/frames (UPDATE_GOLDEN=1 rewrites them,
// see internal/testkit). Both consoles keep state across frames: the Java
// console's tiles and palettes update the picture in place, and the HTML5
// console drops a picture equal to the one before. It compares with the last
// frame's pixels, so the decoder must not reuse that buffer (Phase 3).

func colour555(r, g, b int) uint16 { return uint16(r<<10 | g<<5 | b) }

// rect is a framebuffer-update rectangle at 0,0: size, encoding and payload.
func rect(w, h int, enc uint32, payload []byte) []byte {
	b := make([]byte, 20, 20+len(payload))
	binary.BigEndian.PutUint16(b[4:], uint16(w))
	binary.BigEndian.PutUint16(b[6:], uint16(h))
	binary.BigEndian.PutUint32(b[8:], enc)
	binary.BigEndian.PutUint32(b[16:], uint32(len(payload)))
	return append(b, payload...)
}

func update(rects ...[]byte) []byte {
	b := []byte{0, 0, 0, byte(len(rects))}
	for _, r := range rects {
		b = append(b, r...)
	}
	return b
}

// hermon is a Java console frame: mode (1 is the whole screen), encoding and
// subtype, and for encodings other than 0 the frame's length in bytes 6 to 9.
func hermon(mode, enc, sub byte, data []byte) []byte {
	p := make([]byte, 10, 10+len(data))
	p[0], p[1], p[2] = mode, enc, sub
	if enc != 0 {
		binary.BigEndian.PutUint32(p[6:], uint32(10+len(data)))
	}
	return append(p, data...)
}

// rfbConsole serves the Java console's handshake (as in
// TestNativeSessionHandshakeFrameAndCancellation), then sends the messages.
func rfbConsole(t *testing.T, messages [][]byte) (int, <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	done := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(5 * time.Second))
		read := func(n int) { io.ReadFull(c, make([]byte, n)) }
		c.Write([]byte("RFB 003.008\n"))
		read(12)
		c.Write([]byte{1, 16})
		read(1)
		c.Write(make([]byte, 24))
		read(48)
		c.Write(make([]byte, 4))
		read(1)
		c.Write(make([]byte, 36))
		read(4)
		read(10)
		for _, m := range messages {
			if _, err := c.Write(m); err != nil {
				done <- err
				return
			}
		}
		io.Copy(io.Discard, c)
		done <- nil
	}()
	return ln.Addr().(*net.TCPAddr).Port, done
}

func TestJavaGoldenFrames(t *testing.T) {
	const w, h = 32, 32
	le := binary.LittleEndian
	var key []byte // RGB555, little-endian
	for y := range h {
		for x := range w {
			key = le.AppendUint16(key, colour555(x, y, (x+y)/2))
		}
	}
	tiles := make([]byte, 10) // mode 0, encoding 0, the number of tiles in bytes 2 to 5
	binary.BigEndian.PutUint32(tiles[2:], 2)
	for _, at := range [][2]byte{{0, 1}, {1, 0}} { // row and column of a 16x16 tile
		tiles = append(tiles, 0, 0, 0, 0, at[0], at[1])
		for i := range 256 {
			tiles = le.AppendUint16(tiles, colour555(31-i%16*2, i/16*2, int(at[1])*31))
		}
	}
	palette := make([]byte, 1024) // 256 entries of blue, green, red and a spare byte
	for i := range 256 {
		palette[i*4], palette[i*4+1], palette[i*4+2] = byte(i), byte(255-i), byte(i*7)
	}
	indexed := make([]byte, w*h)
	for i := range indexed {
		indexed[i] = byte(i%w*8 + i/w)
	}
	palette16 := make([]byte, 1024)
	for i := range 16 {
		palette16[i*4], palette16[i*4+1], palette16[i*4+2] = byte(i*16), 128, byte(255-i*16)
	}
	nibbles := make([]byte, w*h/2) // the first pixel in the low nibble
	for i := range w * h {
		nibbles[i/2] |= byte((i%w/2+i/w)%16) << (i % 2 * 4)
	}
	updates := [][]byte{
		update(rect(w, h, 0, hermon(1, 0, 0, key))),                           // the whole screen in RGB555
		update(rect(w, h, 0, tiles)),                                          // two tiles replace part of it
		update(rect(w, h, 0, hermon(1, 1, 0, append(palette, indexed...)))),   // a palette, a byte per pixel
		update(rect(w, h, 0, hermon(1, 5, 2, append(palette16, nibbles...)))), // a new palette, 4 bits per pixel
	}
	port, done := rfbConsole(t, updates)
	j := fmt.Sprintf(`<jnlp><application-desc main-class="com.aten.ikvm"><argument>localhost</argument><argument>fixture-token</argument><argument>x</argument><argument>x</argument><argument>%d</argument><argument>623</argument><argument>x</argument><argument>x</argument><argument>0</argument><argument>0</argument></application-desc></jnlp>`, port)
	launch, err := protocol.ParseJNLP([]byte(j))
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{p: protocol.Profile{Host: "127.0.0.1", AllowPlainConsole: true}, pressed: map[uint32]bool{}, controlling: true, launch: launch}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.attach(ctx); err != nil {
		t.Fatal(err)
	}
	got := testkit.Collect(t, len(updates), c.Run)
	c.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	testkit.Frames(t, "java", got)
}

// ast2100 turns a whole AST2100 frame (header and stream, as in
// internal/ami/codec/testdata/streams) into an HTML5 rectangle: table selector,
// a byte the client does not read, the YUV mode and the stream.
func ast2100(frame []byte) []byte {
	le := binary.LittleEndian
	mode := uint16(444)
	if frame[55] == 1 {
		mode = 422
	}
	payload := append([]byte{frame[44], 7}, byte(mode>>8), byte(mode))
	payload = append(payload, frame[86:86+le.Uint32(frame[69:])]...)
	return rect(int(le.Uint16(frame[13:])), int(le.Uint16(frame[15:])), encAST2100, payload)
}

func TestHTML5GoldenFrames(t *testing.T) {
	var rects [][]byte
	for i := 1; ; i++ {
		b, err := os.ReadFile(fmt.Sprintf("../ami/codec/testdata/streams/html5-%d.bin", i))
		if err != nil {
			break
		}
		rects = append(rects, ast2100(b))
	}
	if len(rects) != 3 {
		t.Fatalf("%d streams in ../ami/codec/testdata/streams, want 3", len(rects))
	}
	// After the second frame one that changes nothing: its stream ends at once.
	// The session must not show it.
	same := append(append([]byte(nil), rects[1][:24]...), 0, 0, 0, 0x90, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(same[16:], 12)
	k := &fakeKVM{}
	for _, r := range [][]byte{rects[0], rects[1], same, rects[2]} {
		k.updates = append(k.updates, update(r))
	}
	srv := httptest.NewTLSServer(websocket.Server{Handler: k.handler, Handshake: func(*websocket.Config, *http.Request) error { return nil }})
	defer srv.Close()
	host, portStr, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	port, _ := strconv.Atoi(portStr)
	p := protocol.Profile{Host: host, Family: "aten", LegacyTLS: true}
	l := protocol.Launch{Family: "aten", HTML5: true, Token: "tok-123", Cookie: "SID=abc", Port: port, Secure: true}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := &Client{p: p, launch: l, pressed: map[uint32]bool{}, controlling: true}
	var err error
	if c.wire, err = dialHTML5(ctx, p, l); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.negotiate(ctx); err != nil {
		t.Fatal(err)
	}
	testkit.Frames(t, "html5", testkit.Collect(t, 3, c.Run))
}
