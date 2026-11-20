package aten

import (
	"context"
	"io"
	"log"
	"net"
	"net/http/httptest"
	"oldipmi/internal/protocol"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// The ATEN side of the transport policy (see internal/protocol/policy_test.go):
// the HTML5 console's WebSocket uses TLS unless the profile logs in over plain
// HTTP, it checks the BMC's certificate unless legacy TLS is on, and the web
// session cookie never goes over a connection that fails that policy. The ISO
// media channel is plain TCP, so it needs the plain-console option.

// noConnection fails the test if anything connected to ln.
func noConnection(t *testing.T, ln net.Listener) {
	t.Helper()
	ln.(*net.TCPListener).SetDeadline(time.Now().Add(100 * time.Millisecond))
	if c, err := ln.Accept(); err == nil {
		c.Close()
		t.Fatal("the BMC saw a connection")
	}
}

func TestHTML5WebSocketPolicy(t *testing.T) {
	// The handler runs after the handshake, so it reports what it saw.
	console := func(cookie chan<- string) websocket.Handler {
		return func(ws *websocket.Conn) {
			cookie <- ws.Request().Header.Get("Cookie")
			io.Copy(io.Discard, ws)
		}
	}
	launch := protocol.Launch{Family: "aten", HTML5: true, Token: "tok", Cookie: "SID=sid-1"}
	for _, c := range []struct {
		name    string
		tls     bool // the BMC's console port speaks TLS
		secure  bool // the launch (Secure is set when the web login used HTTPS)
		profile protocol.Profile
		err     string // the exact error, or a part of Go's TLS error
	}{
		{"wss", true, true, protocol.Profile{LegacyTLS: true}, ""},
		{"wss, certificate checked", true, true, protocol.Profile{}, "unknown authority"},
		{"ws with the HTTP option", false, false, protocol.Profile{HTTP: true}, ""},
		{"ws without the HTTP option", false, false, protocol.Profile{AllowPlainConsole: true}, "unencrypted console not permitted"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cookie := make(chan string, 1)
			srv := httptest.NewUnstartedServer(console(cookie))
			srv.Config.ErrorLog = log.New(io.Discard, "", 0)
			if c.tls {
				srv.StartTLS()
			} else {
				srv.Start()
			}
			defer srv.Close()
			p := c.profile
			p.Host = "127.0.0.1"
			l := launch
			l.Secure = c.secure
			l.Port = srv.Listener.Addr().(*net.TCPAddr).Port
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			w, err := dialHTML5(ctx, p, l)
			if w != nil {
				w.Close()
			}
			switch {
			case c.err == "" && err != nil:
				t.Fatal(err)
			case c.err == "unknown authority" && (err == nil || !strings.Contains(err.Error(), c.err)):
				t.Fatalf("error %v, want Go's %q", err, c.err)
			case c.err != "" && c.err != "unknown authority" && (err == nil || err.Error() != c.err):
				t.Fatalf("error %v, want %q", err, c.err)
			}
			if c.err != "" {
				srv.Close()
				select {
				case got := <-cookie:
					t.Fatalf("the session cookie %q reached the BMC", got)
				default:
				}
				return
			}
			select {
			case got := <-cookie:
				if got != "SID=sid-1" {
					t.Fatalf("the console got the cookie %q", got)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the console was not reached")
			}
		})
	}
	// Refused before connecting at all.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	l := launch
	l.Port = ln.Addr().(*net.TCPAddr).Port
	if _, err := dialHTML5(context.Background(), protocol.Profile{Host: "127.0.0.1"}, l); err == nil || err.Error() != "unencrypted console not permitted" {
		t.Fatal(err)
	}
	noConnection(t, ln)
}

func TestISOMediaNeedsThePlainConsoleOption(t *testing.T) {
	dir := t.TempDir()
	iso, short := filepath.Join(dir, "boot.iso"), filepath.Join(dir, "short.iso")
	if os.WriteFile(iso, make([]byte, 4096), 0o600) != nil || os.WriteFile(short, make([]byte, 100), 0o600) != nil {
		t.Fatal("cannot write the test images")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	for _, c := range []struct {
		name, path string
		profile    protocol.Profile
		err        string
	}{
		{"not a whole number of sectors", short, protocol.Profile{AllowPlainConsole: true}, "invalid ISO image"},
		{"plain console not allowed", iso, protocol.Profile{LegacyTLS: true}, "BMC requests an unencrypted console; enable this host's plain-console option explicitly"},
	} {
		p := c.profile
		p.Host = "127.0.0.1"
		if _, err := newMedia(context.Background(), p, port, c.path); err == nil || err.Error() != c.err {
			t.Errorf("%s: %v, want %q", c.name, err, c.err)
		}
	}
	noConnection(t, ln)
	ln.(*net.TCPListener).SetDeadline(time.Time{})
	accepted := make(chan bool, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
		accepted <- err == nil
	}()
	s, err := newMedia(context.Background(), protocol.Profile{Host: "127.0.0.1", AllowPlainConsole: true}, port, iso)
	if err != nil {
		t.Fatal(err)
	}
	s.close()
	if !<-accepted {
		t.Fatal("the media channel did not connect")
	}
}
