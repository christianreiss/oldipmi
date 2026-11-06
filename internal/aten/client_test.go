package aten

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"oldipmi/internal/protocol"
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
	launch, e := protocol.ParseJNLP([]byte(j))
	if e != nil {
		t.Fatal(e)
	}
	// Past the web login: the same path Connect takes once the BMC has handed out the launch file.
	c := &Client{p: protocol.Profile{Host: "127.0.0.1", AllowPlainConsole: true}, pressed: map[uint32]bool{}, controlling: true, launch: launch}
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
	c := Client{wire: &protocol.Wire{Conn: a}, controlling: true}
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

func TestJavaTokenAuthUsesDistinctPasswordField(t *testing.T) {
	c := &Client{launch: protocol.Launch{Family: "aten", Token: "user1", PasswordToken: "pass2"}}
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
	c := &Client{launch: protocol.Launch{Family: "aten", HTML5: true, Token: "tok"}}
	if a := c.tokenAuth(); len(a) != 48 || !bytes.Equal(bytes.TrimRight(a, "\x00"), []byte("tok")) {
		t.Fatalf("X10/X11: %q", a)
	}
	c.launch.ClientType = true
	if a := c.tokenAuth(); len(a) != 49 || a[48] != 0 || !bytes.Equal(bytes.TrimRight(a[:24], "\x00"), []byte("tok")) || !bytes.Equal(a[24:48], make([]byte, 24)) {
		t.Fatalf("X12/X13: %q", a)
	}
}
