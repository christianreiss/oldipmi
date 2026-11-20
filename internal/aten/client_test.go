package aten

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"testing"
	"time"
)

func TestNativeSessionHandshakeFrameAndCancellation(t *testing.T) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	serverDone := make(chan error, 1)
	go func() {
		c, e := ln.Accept()
		if e != nil {
			serverDone <- e
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		read := func(n int) []byte { b := make([]byte, n); _, e = io.ReadFull(c, b); return b }
		c.Write([]byte("RFB 003.008\n"))
		if !bytes.Equal(read(12), []byte("RFB 003.008\n")) {
			serverDone <- fmt.Errorf("banner")
			return
		}
		c.Write([]byte{1, 16})
		if read(1)[0] != 16 {
			serverDone <- fmt.Errorf("security")
			return
		}
		c.Write(make([]byte, 24))
		auth := read(48)
		if string(bytes.TrimRight(auth[:24], "\x00")) != "fixture-token" {
			serverDone <- fmt.Errorf("token")
			return
		}
		c.Write(make([]byte, 4))
		read(1)
		c.Write(make([]byte, 36))
		read(4)
		read(10)
		b := make([]byte, 4+20+522)
		binary.BigEndian.PutUint16(b[2:], 1)
		binary.BigEndian.PutUint16(b[8:], 16)
		binary.BigEndian.PutUint16(b[10:], 16)
		binary.BigEndian.PutUint32(b[20:], 522)
		b[24] = 1
		for i := 34; i < len(b); i += 2 {
			b[i+1] = 0x7c
		}
		c.Write(b)
		io.Copy(io.Discard, c)
		serverDone <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	port := ln.Addr().(*net.TCPAddr).Port
	j := fmt.Sprintf(`<jnlp><application-desc main-class="com.aten.ikvm"><argument>localhost</argument><argument>fixture-token</argument><argument>x</argument><argument>x</argument><argument>%d</argument><argument>623</argument><argument>x</argument><argument>x</argument><argument>0</argument><argument>0</argument></application-desc></jnlp>`, port)
	launch, e := parseJNLP([]byte(j))
	if e != nil {
		t.Fatal(e)
	}
	// Past the web login: the same path Connect takes once the BMC has handed out the launch file.
	c := &Client{p: protocol.Profile{Host: "127.0.0.1", AllowPlainConsole: true}, controlling: true, launch: launch}
	defer c.Close()
	if e := c.attach(ctx); e != nil {
		t.Fatal(e)
	}
	frames := 0
	e = c.Run(ctx, func(f protocol.Frame) {
		frames++
		if f.Width != 16 || f.Height != 16 || f.Pixels[0] != 248 {
			t.Error("incorrect frame")
		}
		cancel()
	})
	if frames != 1 {
		t.Fatalf("frames=%d error=%v", frames, e)
	}
	if e := <-serverDone; e != nil {
		t.Fatal(e)
	}
}
func TestPointerButtonTranslation(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	c := Client{wire: &transport.Wire{Conn: a}, controlling: true}
	done := make(chan error, 1)
	go func() { done <- c.Pointer(protocol.Input{Buttons: 2, X: 12, Y: 34}) }()
	p := make([]byte, 18)
	if _, e := io.ReadFull(b, p); e != nil {
		t.Fatal(e)
	}
	if p[2] != 4 {
		t.Fatal("right click mapped to middle")
	}
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}

// A framebuffer update request covers the whole screen: for the HTML5 console
// the size of its last frame, for the Java one the size of the decoder's frame.
func TestUpdateRequestSize(t *testing.T) {
	for _, c := range []struct {
		name        string
		html5       bool
		incremental bool
		want        []byte
	}{
		{"java", false, false, []byte{3, 0, 0, 0, 0, 0, 0x04, 0x00, 0x03, 0x00}},
		{"java incremental", false, true, []byte{3, 1, 0, 0, 0, 0, 0x04, 0x00, 0x03, 0x00}},
		{"html5", true, false, []byte{3, 0, 0, 0, 0, 0, 0x05, 0x00, 0x02, 0xd0}},
		{"html5 incremental", true, true, []byte{3, 1, 0, 0, 0, 0, 0x05, 0x00, 0x02, 0xd0}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			cl := &Client{wire: &transport.Wire{Conn: a}, reqW: 1280, reqH: 720}
			cl.launch.HTML5 = c.html5
			if err := cl.dec.Resize(1024, 768); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cl.updateRequest(c.incremental) }()
			got := make([]byte, len(c.want))
			if _, err := io.ReadFull(b, got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, c.want) {
				t.Fatalf("request % x, want % x", got, c.want)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestJavaTokenAuthUsesDistinctPasswordField(t *testing.T) {
	c := &Client{launch: Launch{Token: "user1", PasswordToken: "pass2"}}
	a := c.tokenAuth()
	if !bytes.Equal(bytes.TrimRight(a[:24], "\x00"), []byte("user1")) || !bytes.Equal(bytes.TrimRight(a[24:], "\x00"), []byte("pass2")) {
		t.Fatalf("%q", a)
	}
	c.launch.PasswordToken = ""
	if a = c.tokenAuth(); !bytes.Equal(a[:24], a[24:]) {
		t.Fatalf("%q", a)
	}
}

func TestHTML5TokenAuthClientTypeByte(t *testing.T) {
	c := &Client{launch: Launch{HTML5: true, Token: "tok"}}
	if a := c.tokenAuth(); len(a) != 48 || !bytes.Equal(bytes.TrimRight(a, "\x00"), []byte("tok")) {
		t.Fatalf("X10/X11: %q", a)
	}
	c.launch.ClientType = true
	if a := c.tokenAuth(); len(a) != 49 || a[48] != 0 || !bytes.Equal(bytes.TrimRight(a[:24], "\x00"), []byte("tok")) || !bytes.Equal(a[24:48], make([]byte, 24)) {
		t.Fatalf("X12/X13: %q", a)
	}
}

// fbu16 is a framebuffer update with one 16x16 RGB555 keyframe.
func fbu16() []byte {
	b := make([]byte, 4+20+522)
	binary.BigEndian.PutUint16(b[2:], 1)
	binary.BigEndian.PutUint16(b[8:], 16)
	binary.BigEndian.PutUint16(b[10:], 16)
	binary.BigEndian.PutUint32(b[20:], 522)
	b[24] = 1
	return b
}

// The mouse-status message (0x37) has a 2-byte body on old ATEN firmware
// (live: firmware 2.50) and 3 bytes elsewhere; both must stay in sync.
func TestJavaMouseStatusLength(t *testing.T) {
	for _, c := range []struct {
		name         string
		stream       []byte
		want, frames int
	}{
		{"2-byte, update follows", append([]byte{0x37, 0, 1}, fbu16()...), 2, 1},
		{"3-byte, status 1", append([]byte{0x37, 0, 1, 1}, fbu16()...), 3, 1},
		{"3-byte, status 0", append([]byte{0x37, 0, 1, 0}, fbu16()...), 3, 1},
		{"2-byte twice", append(append(append([]byte{0x37, 0, 1}, fbu16()...), 0x37, 0, 1), fbu16()...), 2, 2},
		{"3-byte, status 0, then 0x39", append(append([]byte{0x37, 0, 1, 0, 0x39}, make([]byte, 264)...), fbu16()...), 3, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, cli := net.Pipe()
			defer srv.Close()
			go io.Copy(io.Discard, srv)
			go srv.Write(c.stream)
			cl := &Client{wire: &transport.Wire{Conn: cli}, controlling: true}
			cl.dec.Resize(16, 16)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			frames := 0
			done := make(chan error, 1)
			go func() {
				done <- cl.Run(ctx, func(protocol.Frame) {
					if frames++; frames == c.frames {
						cancel()
					}
				})
			}()
			if e := <-done; ctx.Err() != context.Canceled || frames != c.frames {
				t.Fatalf("frames=%d err=%v", frames, e)
			}
			if cl.mouseLen != c.want {
				t.Fatalf("mouseLen=%d", cl.mouseLen)
			}
		})
	}
}

// Release lets go of every held key, lowest usage first, then of the buttons.
func TestReleaseLetsGoOfEachKey(t *testing.T) {
	srv, cli := net.Pipe()
	defer srv.Close()
	srv.SetDeadline(time.Now().Add(5 * time.Second)) // a missing packet fails, not hangs
	c := &Client{wire: &transport.Wire{Conn: cli}, controlling: true}
	done := make(chan error, 1)
	go func() {
		for _, u := range []uint32{30, 225, 4} {
			if err := c.Key(protocol.Input{Usage: u, Down: true}); err != nil {
				done <- err
				return
			}
		}
		done <- c.Release()
	}()
	var got []string
	p := make([]byte, 18)
	for i := 0; i < 7; i++ {
		if _, err := io.ReadFull(srv, p); err != nil {
			t.Fatal(err)
		}
		if p[0] == 4 {
			got = append(got, fmt.Sprintf("key %d down=%d", binary.BigEndian.Uint32(p[5:]), p[2]))
		} else {
			got = append(got, fmt.Sprintf("packet %d", p[0]))
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	want := []string{"key 30 down=1", "key 225 down=1", "key 4 down=1", "key 4 down=0", "key 30 down=0", "key 225 down=0", "packet 5"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("sent %q\nwant %q", got, want)
	}
}
