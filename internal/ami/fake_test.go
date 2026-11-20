package ami

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"oldipmi/internal/protocol"
	"reflect"
	"testing"
	"time"
)

// A fake AMI console after the web login (web_test.go tests the login and its
// transcript): the IVTP handshake of the native client and the legacy MegaRAC
// VSC one, with what the client sends, in order. Phase 3 splits the
// client into a VSC and an IVTP session; these transcripts must not change.

// end is the BMC's end of a console connection, framed like the client's: an
// 8-byte header for IVTP, 7 bytes for the legacy VSC.
type end struct {
	conn   net.Conn
	legacy bool
}

func (e end) send(typ, status uint16, b []byte) error {
	n, off := 8, 2
	if e.legacy {
		n, off = 7, 1
	}
	p := make([]byte, n+len(b))
	if e.legacy {
		p[0] = byte(typ)
	} else {
		binary.LittleEndian.PutUint16(p, typ)
	}
	binary.LittleEndian.PutUint32(p[off:], uint32(len(b)))
	binary.LittleEndian.PutUint16(p[off+4:], status)
	copy(p[n:], b)
	_, err := e.conn.Write(p)
	return err
}

func (e end) read() (uint16, []byte, error) {
	n, off := 8, 2
	if e.legacy {
		n, off = 7, 1
	}
	h := make([]byte, n)
	if _, err := io.ReadFull(e.conn, h); err != nil {
		return 0, nil, err
	}
	typ := binary.LittleEndian.Uint16(h)
	if e.legacy {
		typ = uint16(h[0])
	}
	b := make([]byte, binary.LittleEndian.Uint32(h[off:]))
	_, err := io.ReadFull(e.conn, b)
	return typ, b, err
}

func cstring(b []byte) string { return string(bytes.TrimRight(b, "\x00")) }

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "megarac"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// listen opens a console port on the loopback address, with TLS if cert is given.
func listen(t *testing.T, cert *tls.Certificate) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if cert != nil {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{*cert}})
	}
	t.Cleanup(func() { ln.Close() })
	return ln, port
}

// seen is what one BMC connection received.
type seen struct {
	packets []uint16
	payload map[uint16][]byte
	err     error
}

// accept serves one connection: it sends first, then answers each packet the
// client sends, until the client closes. answer may be nil.
func accept(ln net.Listener, legacy bool, first func(end) error, answer func(end, uint16) error) <-chan seen {
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
		e := end{conn, legacy}
		if s.err = first(e); s.err != nil {
			return
		}
		for {
			typ, b, err := e.read()
			if err != nil {
				return // the client closed
			}
			s.packets = append(s.packets, typ)
			s.payload[typ] = b
			if answer != nil {
				if s.err = answer(e, typ); s.err != nil {
					return
				}
			}
		}
	}()
	return done
}

func client(p protocol.Profile, l Launch) *Client {
	return &Client{p: p, launch: l, pressed: map[uint32]bool{}, width: 1024, height: 768}
}

func stagesOf(t *testing.T, stages *[]string) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return protocol.WithProgress(ctx, func(p protocol.Progress) { *stages = append(*stages, p.Stage) })
}

// The native client: the BMC opens with 23, the client answers 58 and the
// authentication (18: the token, its own address, the user), the BMC accepts
// with 19, and the client starts the session with 6.
func TestIVTPSessionTranscript(t *testing.T) {
	cert := selfSigned(t)
	ln, port := listen(t, &cert)
	done := accept(ln, false, func(e end) error { return e.send(23, 0, nil) }, func(e end, typ uint16) error {
		if typ == 18 {
			return e.send(19, 0, []byte{1})
		}
		return nil
	})
	var stages []string
	c := client(protocol.Profile{Host: "127.0.0.1", Username: "admin", LegacyTLS: true},
		Launch{Token: "ivtp-token", Port: port, Secure: true})
	if err := c.attach(stagesOf(t, &stages)); err != nil {
		t.Fatal(err)
	}
	c.Close()
	s := <-done
	if s.err != nil {
		t.Fatal(s.err)
	}
	if want := []uint16{58, 18, 6}; !reflect.DeepEqual(s.packets, want) {
		t.Fatalf("packets %v, want %v", s.packets, want)
	}
	auth := s.payload[18]
	if len(auth) != 373 || cstring(auth[1:130]) != "ivtp-token" || cstring(auth[130:195]) != "127.0.0.1" || cstring(auth[195:324]) != "admin" {
		t.Fatalf("authentication %q", auth)
	}
	if !reflect.DeepEqual(stages, []string{"connecting"}) {
		t.Fatal("stages", stages)
	}
}

func TestIVTPSessionRefused(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer func(end) error // instead of accepting the authentication
		err    string
	}{
		{"rejected", func(e end) error { return e.send(19, 0, []byte{0}) }, "AMI rejected native session"},
		{"stopped", func(e end) error { return e.send(8, 0, nil) }, "AMI stopped native session"},
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
			cert := selfSigned(t)
			ln, port := listen(t, &cert)
			done := accept(ln, false, func(e end) error { return e.send(23, 0, nil) }, func(e end, typ uint16) error {
				if typ == 18 {
					return c.answer(e)
				}
				return nil
			})
			var stages []string
			cl := client(protocol.Profile{Host: "127.0.0.1", Username: "admin", LegacyTLS: true},
				Launch{Token: "ivtp-token", Port: port, Secure: true})
			err := cl.attach(stagesOf(t, &stages))
			cl.Close()
			if err == nil || err.Error() != c.err {
				t.Fatalf("error %v, want %q", err, c.err)
			}
			<-done
		})
	}
}

// The legacy VSC: video (plain here, as the launch file says) and input (always
// TLS) each start with a prelogin answer 14/0, then the client sends on the video
// connection the token (12), the web session cookie (32), 11 and 7. The input
// connection carries nothing until there is input.
func TestVSCSessionTranscript(t *testing.T) {
	for _, c := range []struct {
		name, cookie string
		pos          []string // JNLP arguments; the 11th, if any, is the cookie
	}{
		{"cookie from the web session", "cookie-1", nil},
		{"cookie from the launch file", "pos-cookie", []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "pos-cookie"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cert := selfSigned(t)
			video, videoPort := listen(t, nil)
			input, inputPort := listen(t, &cert)
			prelogin := func(e end) error { return e.send(14, 0, nil) }
			videoDone := accept(video, true, prelogin, nil)
			inputDone := accept(input, true, prelogin, nil)
			var stages []string
			cl := client(protocol.Profile{Host: "127.0.0.1", Username: "admin", LegacyTLS: true, AllowPlainConsole: true},
				Launch{Legacy: true, Token: "vsc-token", Cookie: "cookie-1", Pos: c.pos, Port: videoPort, HIDPort: inputPort})
			if err := cl.attach(stagesOf(t, &stages)); err != nil {
				t.Fatal(err)
			}
			cl.Close()
			v, in := <-videoDone, <-inputDone
			if v.err != nil || in.err != nil {
				t.Fatal(v.err, in.err)
			}
			if want := []uint16{12, 32, 11, 7}; !reflect.DeepEqual(v.packets, want) {
				t.Fatalf("video packets %v, want %v", v.packets, want)
			}
			if len(in.packets) != 0 {
				t.Fatalf("input packets %v before any input", in.packets)
			}
			if auth := v.payload[12]; len(auth) != 99 || cstring(auth[1:]) != "vsc-token" {
				t.Fatalf("authentication %q", auth)
			}
			if string(v.payload[32]) != c.cookie || !bytes.Equal(v.payload[11], []byte{8}) || len(v.payload[7]) != 0 {
				t.Fatalf("cookie %q, 11 %v, 7 %v", v.payload[32], v.payload[11], v.payload[7])
			}
			if !reflect.DeepEqual(stages, []string{"connecting"}) {
				t.Fatal("stages", stages)
			}
		})
	}
}

func TestVSCPreloginRefused(t *testing.T) {
	cert := selfSigned(t)
	for _, c := range []struct {
		name         string
		video, input [2]uint16 // the prelogin answers: type, status
		err          string
	}{
		{"video", [2]uint16{14, 1}, [2]uint16{14, 0}, "AMI video prelogin refused (14/1)"},
		{"input", [2]uint16{14, 0}, [2]uint16{13, 0}, "AMI HID prelogin refused (13/0)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			video, videoPort := listen(t, nil)
			input, inputPort := listen(t, &cert)
			videoDone := accept(video, true, func(e end) error { return e.send(c.video[0], c.video[1], nil) }, nil)
			if c.video[1] == 0 {
				accept(input, true, func(e end) error { return e.send(c.input[0], c.input[1], nil) }, nil)
			}
			var stages []string
			cl := client(protocol.Profile{Host: "127.0.0.1", LegacyTLS: true, AllowPlainConsole: true},
				Launch{Legacy: true, Token: "vsc-token", Port: videoPort, HIDPort: inputPort})
			err := cl.attach(stagesOf(t, &stages))
			cl.Close()
			if err == nil || err.Error() != c.err {
				t.Fatalf("error %v, want %q", err, c.err)
			}
			if v := <-videoDone; len(v.packets) != 0 {
				t.Fatalf("the client sent %v after a refused prelogin", v.packets)
			}
		})
	}
}

// What the launch file asks for that this adapter refuses, before connecting.
func TestAttachRefusesBeforeConnecting(t *testing.T) {
	ln, port := listen(t, nil)
	for _, c := range []struct {
		name   string
		launch Launch
		err    string
	}{
		{"single-port tunneling", Launch{Token: "t", Port: port, Args: map[string]string{"singleportenabled": "1"}},
			"AMI single-port tunneling is not supported by this firmware adapter"},
		{"plain console not allowed", Launch{Token: "t", Port: port},
			"BMC requests an unencrypted console; enable this host's plain-console option explicitly"},
	} {
		var stages []string
		cl := client(protocol.Profile{Host: "127.0.0.1", LegacyTLS: true}, c.launch)
		err := cl.attach(stagesOf(t, &stages))
		cl.Close()
		if err == nil || err.Error() != c.err {
			t.Errorf("%s: %v, want %q", c.name, err, c.err)
		}
	}
	ln.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
	if conn, err := ln.Accept(); err == nil {
		conn.Close()
		t.Fatal("the BMC saw a connection")
	}
}
