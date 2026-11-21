package protocol_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"syscall"
	"testing"
	"time"

	"oldipmi/internal/protocol"
	"oldipmi/internal/testkit"
	"oldipmi/internal/transport"
)

// The errors that end a console's setup, made for real through the connections
// the drivers use (Dial, DialWS, the web session), or built as package net
// builds them where a test cannot make them (name lookups, routes). Each must
// get its class. testdata/ipc/errors.json holds them for the desktop's hint
// tests; only their codes are compared here, as the words of Go's errors may
// change from release to release. UPDATE_GOLDEN=1 writes their rows anew.
func TestErrorCodeOfRealErrors(t *testing.T) {
	dial := func(port int, secure bool) func(*testing.T) error {
		return func(t *testing.T) error {
			_, err := transport.Dial(context.Background(), local, port, secure)
			return err
		}
	}
	tlsDial := func(edit func(*x509.Certificate)) func(*testing.T) error {
		return func(t *testing.T) error {
			return dial(tlsServer(t, &tls.Config{Certificates: []tls.Certificate{cert(t, edit)}}), true)(t)
		}
	}
	rawDial := func(answer func(net.Conn)) func(*testing.T) error {
		return func(t *testing.T) error { return dial(server(t, answer), true)(t) }
	}
	webRequest := func(w *transport.Web, ctx context.Context) error {
		_, err := w.Request(ctx, "POST", "/login", nil, false)
		return err
	}
	status := func(code int) func(*testing.T) error {
		return func(t *testing.T) error {
			bmc := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(code) }))
			defer bmc.Close()
			w := web(t, transport.Policy{Host: "127.0.0.1", HTTP: true}, bmc.URL)
			return webRequest(w, context.Background())
		}
	}
	lookup := func(d *net.DNSError) func(*testing.T) error {
		return func(*testing.T) error { return &net.OpError{Op: "dial", Net: "tcp", Err: d} }
	}
	connect := func(errno syscall.Errno) func(*testing.T) error {
		return func(*testing.T) error {
			return &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 443}, Err: os.NewSyscallError("connect", errno)}
		}
	}
	cases := []struct {
		name, code string
		err        func(*testing.T) error
	}{
		{"dns: no such host", protocol.CodeDNS, lookup(&net.DNSError{Err: "no such host", Name: "bmc.example.invalid", Server: "192.0.2.53:53", IsNotFound: true})},
		{"dns: server misbehaving", protocol.CodeDNS, lookup(&net.DNSError{Err: "server misbehaving", Name: "bmc.example.net", Server: "192.0.2.53:53"})},
		{"dns: timeout", protocol.CodeUnreachable, lookup(&net.DNSError{Err: "i/o timeout", Name: "bmc.example.net", Server: "192.0.2.53:53", IsTimeout: true})},
		{"no route to host", protocol.CodeUnreachable, connect(syscall.EHOSTUNREACH)},
		{"network is unreachable", protocol.CodeUnreachable, connect(syscall.ENETUNREACH)},
		{"connection refused", protocol.CodeRefused, func(t *testing.T) error { return dial(closedPort(t), true)(t) }},
		// A BMC that takes the connection and never answers.
		{"console: no answer", protocol.CodeUnreachable, func(t *testing.T) error {
			port := server(t, func(c net.Conn) { io.Copy(io.Discard, c) })
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			_, err := transport.Dial(ctx, local, port, true)
			return err
		}},
		{"web: no answer", protocol.CodeUnreachable, func(t *testing.T) error {
			w := web(t, local, fmt.Sprintf("https://127.0.0.1:%d", server(t, func(c net.Conn) { io.Copy(io.Discard, c) })))
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			return webRequest(w, ctx)
		}},
		{"web: no answer in the client's time", protocol.CodeUnreachable, func(t *testing.T) error {
			w := web(t, local, fmt.Sprintf("https://127.0.0.1:%d", server(t, func(c net.Conn) { io.Copy(io.Discard, c) })))
			w.Client.Timeout = 200 * time.Millisecond
			return webRequest(w, context.Background())
		}},
		{"tls: certificate for another address", protocol.CodeTLSName, tlsDial(func(c *x509.Certificate) { c.IPAddresses = []net.IP{net.IPv4(192, 0, 2, 1)} })},
		{"tls: certificate without addresses", protocol.CodeTLSName, tlsDial(func(c *x509.Certificate) { c.DNSNames = []string{"bmc.example.net"} })},
		{"tls: name only in the common name", protocol.CodeTLSUnverified, tlsDial(func(c *x509.Certificate) { c.Subject.CommonName = "127.0.0.1" })},
		{"tls: unknown authority", protocol.CodeTLSUnverified, tlsDial(forLocal)},
		{"tls: expired", protocol.CodeTLSUnverified, tlsDial(func(c *x509.Certificate) {
			forLocal(c)
			c.NotBefore, c.NotAfter = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)
		})},
		// Old BMCs answer a TLS 1.2 hello in the version they have.
		{"tls: BMC chooses TLS 1.0", protocol.CodeTLSOld, rawDial(serverHello(tls.VersionTLS10))},
		{"tls: BMC chooses TLS 1.1", protocol.CodeTLSOld, rawDial(serverHello(tls.VersionTLS11))},
		{"tls: BMC refuses TLS 1.2", protocol.CodeTLS, func(t *testing.T) error {
			c := &tls.Config{Certificates: []tls.Certificate{cert(t, forLocal)}, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS10}
			return dial(tlsServer(t, c), true)(t)
		}},
		{"tls: not TLS", protocol.CodeTLS, rawDial(func(c net.Conn) { io.WriteString(c, "HTTP/1.0 400 Bad Request\r\n\r\n") })},
		{"tls: client certificate unreadable", protocol.CodeTLS, func(t *testing.T) error {
			p := local
			p.ClientCert, p.ClientKey = "/nonexistent/operator.crt", "/nonexistent/operator.key"
			_, err := transport.Dial(context.Background(), p, closedPort(t), true)
			return err
		}},
		{"plain console", protocol.CodePlainConsole, dial(5900, false)},
		{"plain WebSocket console", protocol.CodePlainConsole, func(t *testing.T) error {
			_, err := transport.DialWS(context.Background(), local, 80, false, "")
			return err
		}},
		{"WebSocket: unknown authority", protocol.CodeTLSUnverified, func(t *testing.T) error {
			port := tlsServer(t, &tls.Config{Certificates: []tls.Certificate{cert(t, forLocal)}})
			_, err := transport.DialWS(context.Background(), local, port, true, "")
			return err
		}},
		{"web: unknown authority", protocol.CodeTLSUnverified, func(t *testing.T) error {
			port := tlsServer(t, &tls.Config{Certificates: []tls.Certificate{cert(t, forLocal)}})
			return webRequest(web(t, local, fmt.Sprintf("https://127.0.0.1:%d", port)), context.Background())
		}},
		{"web: status 401", protocol.CodeAuth, status(401)},
		{"web: status 403", protocol.CodeAuth, status(403)},
		{"web: status 500", "", status(500)},
		{"end of stream", "", func(*testing.T) error { return io.EOF }},
	}
	got := map[string]testkit.ErrorExample{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.err(t)
			if code := protocol.ErrorCode(err); err == nil || code != c.code {
				t.Fatalf("%v: code %q, want %q", err, code, c.code)
			}
			got[c.name] = testkit.ErrorExample{Name: c.name, Message: varying.Replace(err.Error()), Code: c.code}
		})
	}
	path, f := testkit.ReadErrors(t)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if t.Failed() {
			return
		}
		// These rows first, in this order, then those of the drivers.
		var rows []testkit.ErrorExample
		for _, c := range cases {
			rows = append(rows, got[c.name])
		}
		for _, r := range f.Errors {
			if _, ok := got[r.Name]; !ok {
				rows = append(rows, r)
			}
		}
		f.Errors = rows
		testkit.WriteErrors(t, path, f)
		return
	}
	rows := map[string]testkit.ErrorExample{}
	for _, r := range f.Errors {
		rows[r.Name] = r
	}
	for _, c := range cases {
		if r, ok := rows[c.name]; !ok || r.Code != c.code {
			t.Errorf("errors.json row %q: %+v, want code %q (UPDATE_GOLDEN=1 writes it)", c.name, r, c.code)
		}
	}
}

// A Mark counts where its class stands among the others: an error is in the
// first class that fits, by its type or its marks.
func TestErrorCodeOrder(t *testing.T) {
	timeout := fmt.Errorf("read: %w", os.ErrDeadlineExceeded)
	for _, c := range []struct {
		name string
		err  error
		code string
	}{
		{"none", nil, ""},
		{"unmarked", errors.New("the BMC sent an unexpected reply"), ""},
		{"marked", protocol.Mark(protocol.CodeAuth, errors.New("login refused")), protocol.CodeAuth},
		{"marked, wrapped", fmt.Errorf("Avocent: %w", protocol.Mark(protocol.CodeTextMode, io.EOF)), protocol.CodeTextMode},
		{"marked, joined", errors.Join(io.EOF, protocol.Mark(protocol.CodeAuth, io.EOF)), protocol.CodeAuth},
		{"timeout before a mark", protocol.Mark(protocol.CodeTLS, timeout), protocol.CodeUnreachable},
		{"mark before a later class", protocol.Mark(protocol.CodeTLS, &transport.StatusError{Code: 401}), protocol.CodeTLS},
		{"two marks", protocol.Mark(protocol.CodePlainConsole, protocol.Mark(protocol.CodeAuth, io.EOF)), protocol.CodeAuth},
	} {
		if got := protocol.ErrorCode(c.err); got != c.code {
			t.Errorf("%s: %q, want %q", c.name, got, c.code)
		}
	}
	if m := protocol.Mark(protocol.CodeAuth, io.EOF); m.Error() != "EOF" || !errors.Is(m, io.EOF) {
		t.Errorf("a mark changed the error: %q", m)
	}
}

var local = transport.Policy{Host: "127.0.0.1"}

// The parts of an error's words that change from run to run.
var varying = replacer{
	regexp.MustCompile(`127\.0\.0\.1:\d+`):    "127.0.0.1:PORT",
	regexp.MustCompile(`current time \S+ is`): "current time NOW is",
}

type replacer map[*regexp.Regexp]string

func (r replacer) Replace(s string) string {
	for re, with := range r {
		s = re.ReplaceAllString(s, with)
	}
	return s
}

func web(t *testing.T, p transport.Policy, base string) *transport.Web {
	t.Helper()
	w, err := transport.NewWeb(p)
	if err != nil {
		t.Fatal(err)
	}
	w.Base = base
	return w
}

// closedPort is a local port where nothing listens.
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// server answers each connection on a local port with answer.
func server(t *testing.T, answer func(net.Conn)) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(5 * time.Second))
				answer(c)
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// tlsServer is a BMC that speaks TLS as c configures it.
func tlsServer(t *testing.T, c *tls.Config) int {
	return server(t, func(conn net.Conn) {
		tc := tls.Server(conn, c)
		if tc.Handshake() == nil {
			io.Copy(io.Discard, tc)
		}
	})
}

// serverHello reads the client's hello and answers it with a server hello
// in version, as a BMC that knows nothing newer does.
func serverHello(version uint16) func(net.Conn) {
	return func(c net.Conn) {
		h := make([]byte, 5)
		if _, err := io.ReadFull(c, h); err != nil {
			return
		}
		io.CopyN(io.Discard, c, int64(h[3])<<8|int64(h[4]))
		body := append([]byte{byte(version >> 8), byte(version)}, make([]byte, 32)...) // random
		body = append(body, 0, 0x00, 0x2f, 0)                                          // no session, TLS_RSA_WITH_AES_128_CBC_SHA, no compression
		msg := append([]byte{2, 0, 0, byte(len(body))}, body...)
		c.Write(append([]byte{22, 3, 1, 0, byte(len(msg))}, msg...))
		io.Copy(io.Discard, c)
	}
}

// cert makes a self-signed server certificate, as edit changes it.
func cert(t *testing.T, edit func(*x509.Certificate)) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "bmc"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	edit(tmpl)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// forLocal names 127.0.0.1, so that only the authority is unknown.
func forLocal(c *x509.Certificate) { c.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1)} }
