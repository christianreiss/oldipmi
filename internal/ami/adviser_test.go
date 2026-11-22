package ami

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"oldipmi/internal/protocol"
	"oldipmi/internal/testkit"
	"oldipmi/internal/transport"
	"oldipmi/internal/video/ast"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The adviser console of older MegaRAC firmware (ASUS ASMB4, for one): its
// JNLP passes four or more positional arguments (address, port, session
// token, additional session argument, then ones the viewer does not read); the console
// is plain TCP with 7-byte headers.

func adviserJNLP(args ...string) []byte {
	var b strings.Builder
	b.WriteString(`<jnlp codebase="https://bmc.invalid/Java"><resources><jar href="JViewer.jar"/></resources><application-desc>`)
	for _, a := range args {
		fmt.Fprintf(&b, "<argument>%s</argument>", a)
	}
	b.WriteString(`</application-desc></jnlp>`)
	return []byte(b.String())
}

func TestAdviserJNLP(t *testing.T) {
	l, err := parseJNLP(adviserJNLP("10.0.0.5", "7578", "0123456789abcdef", "0", "255", "EN", "web-session-cookie"))
	if err != nil {
		t.Fatal(err)
	}
	if !l.Adviser || l.Legacy || l.Port != 7578 || l.Token != "0123456789abcdef" || l.AdviserSessionArg != "0" || l.Secure || l.CDPort != 0 {
		t.Fatalf("launch %+v", l)
	}
	if l, err = parseJNLP(adviserJNLP("10.0.0.5", "7578", "short", "w")); err != nil || !l.Adviser || l.Token != "short" {
		t.Fatalf("four arguments: %+v %v", l, err)
	}
	for _, c := range []struct {
		name string
		args []string
		err  string
	}{
		{"too few", []string{"10.0.0.5", "7578", "token"}, "unsupported AMI JNLP arguments"},
		{"token too long", []string{"10.0.0.5", "7578", "0123456789abcdefX", "0"}, "JNLP has invalid connection parameters"},
		{"bad port", []string{"10.0.0.5", "x", "token", "0"}, "JNLP has invalid connection parameters"},
	} {
		if _, err := parseJNLP(adviserJNLP(c.args...)); err == nil || err.Error() != c.err {
			t.Errorf("%s: %v, want %q", c.name, err, c.err)
		}
	}
}

func TestAdviserConsoleInfo(t *testing.T) {
	got := Launch{Adviser: true, Token: "secret", AdviserSessionArg: "secret", Port: 7578}.ConsoleInfo(protocol.Profile{Host: "bmc.example.net"})
	want := protocol.ConsoleInfo{Protocol: "AMI MegaRAC adviser",
		Endpoints:    []protocol.Endpoint{{Role: "Video / input", Address: "bmc.example.net:7578", Transport: "Plain TCP"}},
		Capabilities: []string{"Video", "Keyboard", "Pointer"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func adviserProfile() protocol.Profile {
	return protocol.Profile{Host: "127.0.0.1", Username: "admin", AllowPlainConsole: true}
}

func adviserLaunch(port int) Launch {
	return Launch{Adviser: true, Token: "adviser-token", AdviserSessionArg: "0", Port: port}
}

// accepts answers the session token packets with 35 and the given byte.
func accepts(ok byte) func(end, uint16) error {
	return func(e end, typ uint16) error {
		if typ == 52 {
			return e.send(35, 0, []byte{ok})
		}
		return nil
	}
}

func silent(end) error { return nil }

// The client speaks first: the session token (34, zero-padded to 16 bytes)
// and the additional JNLP session argument (52). The console accepts with
// 35; the client sends nothing more until there is input, and 25 on close.
func TestAdviserSessionTranscript(t *testing.T) {
	ln, port := listen(t, nil)
	done := accept(ln, true, silent, accepts(1))
	var stages []string
	launch := adviserLaunch(port)
	launch.AdviserSessionArg = "fixture-session-arg"
	c := client(adviserProfile(), launch)
	if err := c.attach(stagesOf(t, &stages)); err != nil {
		t.Fatal(err)
	}
	c.Close()
	s := <-done
	if s.err != nil {
		t.Fatal(s.err)
	}
	if want := []uint16{34, 52, 25}; !reflect.DeepEqual(s.packets, want) {
		t.Fatalf("packets %v, want %v", s.packets, want)
	}
	if tok := s.payload[34]; len(tok) != 16 || cstring(tok) != "adviser-token" {
		t.Fatalf("session token % x", tok)
	}
	if string(s.payload[52]) != "fixture-session-arg" || len(s.payload[25]) != 0 {
		t.Fatalf("session argument %q, stop % x", s.payload[52], s.payload[25])
	}
	if !reflect.DeepEqual(stages, []string{"connecting"}) {
		t.Fatal("stages", stages)
	}
}

func TestAdviserRequiresPlainConsoleOptIn(t *testing.T) {
	_, port := listen(t, nil)
	p := adviserProfile()
	p.AllowPlainConsole = false
	c := client(p, adviserLaunch(port))
	defer c.Close()
	if err := c.attach(context.Background()); !errors.Is(err, transport.ErrPlainConsole) || c.wire != nil {
		t.Fatalf("attach with plain console disabled: %v", err)
	}
}

func TestAdviserSessionRefused(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer func(end) error
		err    string
	}{
		{"rejected", func(e end) error { return e.send(35, 0, []byte{0}) }, "AMI rejected the console session token"},
		{"empty answer", func(e end) error { return e.send(35, 0, nil) }, "AMI rejected the console session token"},
		{"maximum sessions", func(e end) error { return e.send(49, 0, nil) }, "BMC has no free console session; close other viewers and try again"},
		{"stopped", func(e end) error { return e.send(25, 0, nil) }, "AMI stopped native session"},
		{"no answer", func(e end) error {
			for range 20 {
				if err := e.send(99, 0, nil); err != nil {
					return err
				}
			}
			return nil
		}, "AMI authentication response missing"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ln, port := listen(t, nil)
			done := accept(ln, true, silent, func(e end, typ uint16) error {
				if typ == 52 {
					return c.answer(e)
				}
				return nil
			})
			var stages []string
			cl := client(adviserProfile(), adviserLaunch(port))
			err := cl.attach(stagesOf(t, &stages))
			cl.Close()
			if err == nil || err.Error() != c.err {
				t.Fatalf("error %v, want %q", err, c.err)
			}
			if s := <-done; !reflect.DeepEqual(s.packets, []uint16{34, 52}) {
				t.Fatalf("packets %v after a refused session", s.packets)
			}
		})
	}
}

// adviserVideoHeader is the 39-byte header the adviser console puts before
// the ASPEED video header of each frame: 19 reserved bytes, the signature
// "ASP-2000", its own length and the frame's.
func adviserVideoHeader(frame []byte) []byte {
	h := make([]byte, 39)
	copy(h[19:], "ASP-2000")
	binary.LittleEndian.PutUint16(h[27:], 39)
	binary.LittleEndian.PutUint32(h[29:], uint32(len(frame)))
	return append(h, frame...)
}

// The same AST2100 streams as the IVTP console, each behind the adviser's
// video header, show the same pictures. Packets the client does not use
// (a cursor, a bandwidth answer of size×status bytes, keyboard LEDs) come in
// between and are skipped.
func TestAdviserGoldenFrames(t *testing.T) {
	var frames [][]byte
	for i := 1; ; i++ {
		b, err := os.ReadFile(fmt.Sprintf("../video/ast/testdata/streams/ivtp-%d.bin", i))
		if err != nil {
			break
		}
		frames = append(frames, b)
	}
	if len(frames) != 5 {
		t.Fatalf("%d streams, want 5", len(frames))
	}
	ln, port := listen(t, nil)
	done := accept(ln, true, silent, func(e end, typ uint16) error {
		if typ != 52 {
			return nil
		}
		if err := e.send(35, 0, []byte{1}); err != nil {
			return err
		}
		for i, f := range frames {
			switch i {
			case 1:
				if err := e.send(48, 0, make([]byte, 57+64)); err != nil {
					return err
				}
			case 2: // size 4, status 3: three blocks of 4 bytes
				p := []byte{32, 4, 0, 0, 0, 3, 0}
				if _, err := e.conn.Write(append(p, make([]byte, 12)...)); err != nil {
					return err
				}
			case 3:
				if err := e.send(121, 0, []byte{2}); err != nil {
					return err
				}
			}
			if err := fragments(e, 5, adviserVideoHeader(f), 100); err != nil {
				return err
			}
		}
		return nil
	})
	c := client(adviserProfile(), adviserLaunch(port))
	defer c.Close()
	var stages []string
	if err := c.attach(stagesOf(t, &stages)); err != nil {
		t.Fatal(err)
	}
	got := testkit.Collect(t, len(frames), c.Run)
	c.Close()
	if s := <-done; s.err != nil {
		t.Fatal(s.err)
	}
	testkit.Frames(t, "ivtp", got)
}

func TestAdviserFrameWithoutSignature(t *testing.T) {
	ln, port := listen(t, nil)
	accept(ln, true, silent, func(e end, typ uint16) error {
		if typ == 52 {
			if err := e.send(35, 0, []byte{1}); err != nil {
				return err
			}
			return fragments(e, 5, make([]byte, 200), 100)
		}
		return nil
	})
	c := client(adviserProfile(), adviserLaunch(port))
	defer c.Close()
	var stages []string
	if err := c.attach(stagesOf(t, &stages)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Run(ctx, func(protocol.Frame) {}); err == nil || err.Error() != "AMI adviser frame without its ASP-2000 header" {
		t.Fatalf("Run ended with %v", err)
	}
}

// What ends a running session: the console's stop (25), and keyboard/mouse
// encryption (29, 30), which this client does not implement.
func TestAdviserRunEnds(t *testing.T) {
	for _, c := range []struct {
		name string
		typ  uint16
		err  string
	}{
		{"stopped", 25, "BMC ended session (7)"},
		{"encryption", 30, "this BMC encrypts keyboard and mouse input, which OldIPMI does not support"},
		{"encryption by another session", 29, "this BMC encrypts keyboard and mouse input, which OldIPMI does not support"},
	} {
		t.Run(c.name, func(t *testing.T) {
			ln, port := listen(t, nil)
			accept(ln, true, silent, func(e end, typ uint16) error {
				if typ == 52 {
					if err := e.send(35, 0, []byte{1}); err != nil {
						return err
					}
					if c.typ == 25 {
						return e.send(c.typ, 7, nil)
					}
					if err := e.send(33, 0, []byte{2}); err != nil {
						return err
					}
					// The encryption notice is terminal even if its body has
					// not arrived. Advertise a byte and only send the header.
					_, err := e.conn.Write([]byte{byte(c.typ), 1, 0, 0, 0, 7, 0})
					return err
				}
				return nil
			})
			cl := client(adviserProfile(), adviserLaunch(port))
			defer cl.Close()
			var stages []string
			if err := cl.attach(stagesOf(t, &stages)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := cl.Run(ctx, func(protocol.Frame) {}); err == nil || err.Error() != c.err {
				t.Fatalf("Run ended with %v", err)
			}
			if c.typ != 25 {
				if err := cl.Key(protocol.Input{Usage: 4, Down: true}); err == nil {
					t.Error("sent a key after the encryption notice")
				}
				if err := cl.Pointer(protocol.Input{X: 30, Y: 30}); err == nil {
					t.Error("sent a pointer report after the encryption notice")
				}
			}
		})
	}
}

// iusbReport checks an adviser HID packet (type 6): the 32-byte IUSB header
// for the device and interface, the report's length byte and the report.
func iusbReport(t *testing.T, b []byte, device, proto, iface byte) []byte {
	t.Helper()
	if len(b) < 33 || !bytes.HasPrefix(b, []byte("IUSB    ")) || b[17] != device || b[18] != proto || b[21] != iface {
		t.Fatalf("HID packet % x", b)
	}
	var sum byte
	for _, x := range b[:32] {
		sum += x
	}
	n := int(b[32])
	if sum != 0 || binary.LittleEndian.Uint32(b[12:]) != uint32(n+1) || len(b) != 33+n {
		t.Fatalf("HID packet % x: checksum %d, length", b, sum)
	}
	return b[33:]
}

// adviserSession runs a session whose console sends first (after accepting
// the token) and records every packet the client sends.
func adviserSession(t *testing.T, first ...[2]any) (*Client, <-chan seen, *[][]byte) {
	t.Helper()
	ln, port := listen(t, nil)
	var hid [][]byte
	sent := make(chan struct{})
	done := make(chan seen, 1)
	go func() {
		s := seen{payload: map[uint16][]byte{}}
		defer func() { done <- s }()
		conn, err := ln.Accept()
		if err != nil {
			s.err = err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		e := end{conn, true}
		for {
			typ, b, err := e.read()
			if err != nil {
				return
			}
			s.packets = append(s.packets, typ)
			if typ == 6 {
				hid = append(hid, b)
			}
			if typ == 52 {
				if s.err = e.send(35, 0, []byte{1}); s.err != nil {
					return
				}
				for _, p := range first {
					if s.err = e.send(p[0].(uint16), 0, p[1].([]byte)); s.err != nil {
						return
					}
				}
				close(sent)
			}
		}
	}()
	c := client(adviserProfile(), adviserLaunch(port))
	t.Cleanup(func() { c.Close() })
	var stages []string
	if err := c.attach(stagesOf(t, &stages)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go c.Run(ctx, func(protocol.Frame) {})
	<-sent
	return c, done, &hid
}

// waitMode waits until the client has read the console's mouse mode.
func waitMode(t *testing.T, c *Client, mode byte) {
	t.Helper()
	for range 500 {
		c.mu.Lock()
		m := c.mouseMode
		c.mu.Unlock()
		if m == mode {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("mouse mode never became %d", mode)
}

// Keyboard: an 8-byte boot report. Pointer in absolute mode (33 with 2): the
// buttons and the position scaled to 0..32767, without a wheel. Release lets
// go of every key and button, the pointer staying where it was.
func TestAdviserInputAbsolute(t *testing.T) {
	c, done, hid := adviserSession(t, [2]any{uint16(33), []byte{2}})
	waitMode(t, c, 2)
	c.mu.Lock()
	c.width, c.height = 1025, 769
	c.mu.Unlock()
	if err := c.Key(protocol.Input{Usage: 4, Down: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Pointer(protocol.Input{X: 512, Y: 768, Buttons: 1, Wheel: 1}); err != nil {
		t.Fatal(err)
	}
	if err := c.Release(); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if s := <-done; s.err != nil {
		t.Fatal(s.err)
	}
	if len(*hid) != 4 {
		t.Fatalf("%d HID packets, want 4", len(*hid))
	}
	if r := iusbReport(t, (*hid)[0], 48, 16, 0); !bytes.Equal(r, []byte{0, 0, 4, 0, 0, 0, 0, 0}) {
		t.Fatalf("key report % x", r)
	}
	if r := iusbReport(t, (*hid)[1], 49, 32, 1); !bytes.Equal(r, []byte{1, 0xff, 0x3f, 0xff, 0x7f}) {
		t.Fatalf("pointer report % x", r)
	}
	if r := iusbReport(t, (*hid)[2], 48, 16, 0); !bytes.Equal(r, make([]byte, 8)) {
		t.Fatalf("released keys % x", r)
	}
	if r := iusbReport(t, (*hid)[3], 49, 32, 1); !bytes.Equal(r, []byte{0, 0xff, 0x3f, 0xff, 0x7f}) {
		t.Fatalf("released pointer % x", r)
	}
}

// In relative mode (33 with 1) the pointer moves by steps of at most 127
// from where the client last put it, starting at the top left corner.
func TestAdviserInputRelative(t *testing.T) {
	c, done, hid := adviserSession(t, [2]any{uint16(33), []byte{1}})
	waitMode(t, c, 1)
	if err := c.Pointer(protocol.Input{X: 200, Y: 10}); err != nil {
		t.Fatal(err)
	}
	if err := c.Pointer(protocol.Input{X: 190, Y: 10, Buttons: 2}); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if s := <-done; s.err != nil {
		t.Fatal(s.err)
	}
	var got [][]byte
	for _, p := range *hid {
		got = append(got, iusbReport(t, p, 49, 32, 1))
	}
	want := [][]byte{{0, 127, 10}, {0, 73, 0}, {2, 0xf6, 0}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("relative reports % x, want % x", got, want)
	}
}

// Until the console says which mouse mode it uses, the pointer sends nothing,
// as the vendor's viewer does.
func TestAdviserPointerWaitsForMouseMode(t *testing.T) {
	c, done, hid := adviserSession(t)
	if err := c.Pointer(protocol.Input{X: 5, Y: 5, Buttons: 1}); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if s := <-done; s.err != nil {
		t.Fatal(s.err)
	}
	if len(*hid) != 0 {
		t.Fatalf("pointer sent % x before the mouse mode", *hid)
	}
}

// The adviser JNLP advertises no virtual media port.
func TestAdviserMountWithoutMediaPort(t *testing.T) {
	c := client(adviserProfile(), adviserLaunch(1))
	defer c.Close()
	iso := t.TempDir() + "/a.iso"
	if err := os.WriteFile(iso, make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}
	err := c.Mount(context.Background(), iso)
	if err == nil || err.Error() != "this console offers no virtual media" {
		t.Fatalf("Mount: %v", err)
	}
}

func FuzzAdviserDecode(f *testing.F) {
	f.Add(adviserVideoHeader(make([]byte, 100)))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			return
		}
		decodeAdviser(ast.New(), b)
	})
}
