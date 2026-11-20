package protocol

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"oldipmi/internal/transport"
	"oldipmi/internal/transport/transporttest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The transport policy of a profile: what each exception (plain HTTP login,
// legacy TLS, unencrypted console, client certificate) allows, the exact error
// when a BMC asks for more, and that nothing secret leaves before the policy is
// met. The code is internal/transport's; these tests reach it through
// Profile.Policy and keep its differences: the web login never presents the
// client certificate, the console connection does. The web logins' side of
// the policy is tested with them, in internal/ami and internal/aten.

func TestWebSchemeFollowsTheHTTPOption(t *testing.T) {
	for _, c := range []struct {
		p    Profile
		base string
	}{
		{Profile{Host: "bmc.example.net"}, "https://bmc.example.net"},
		{Profile{Host: "bmc.example.net", HTTP: true}, "http://bmc.example.net"},
		{Profile{Host: "2001:db8::5"}, "https://[2001:db8::5]"},
		{Profile{Host: "2001:db8::5", HTTP: true}, "http://[2001:db8::5]"},
	} {
		w, err := transport.NewWeb(c.p.Policy())
		if err != nil || w.Base != c.base {
			t.Errorf("%+v: base %q, error %v; want %q", c.p, w.Base, err, c.base)
		}
	}
	for _, host := range []string{"", "https://bmc", "bmc/path", "admin@bmc", "bmc?x", "bmc#x", "bmc name", "bmc\\x"} {
		_, err := transport.NewWeb(Profile{Host: host}.Policy())
		if err == nil || err.Error() != "enter a BMC hostname or IP address without a URL" {
			t.Errorf("host %q: %v", host, err)
		}
	}
}

func TestTLSConfigPerProfile(t *testing.T) {
	c := transport.TLSConfig(Profile{Host: "bmc.example.net"}.Policy())
	if c.ServerName != "bmc.example.net" || c.InsecureSkipVerify || c.MinVersion != tls.VersionTLS12 || c.CipherSuites != nil {
		t.Errorf("default: %+v", c)
	}
	c = transport.TLSConfig(Profile{Host: "bmc.example.net", LegacyTLS: true}.Policy())
	want := []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_RSA_WITH_AES_256_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}
	if c.ServerName != "bmc.example.net" || !c.InsecureSkipVerify || c.MinVersion != tls.VersionTLS10 || !reflect.DeepEqual(c.CipherSuites, want) {
		t.Errorf("legacy: %+v", c)
	}
}

// The console connection (Dial): TLS unless the BMC asks for a plain console
// and the profile allows one, the BMC's certificate checked unless legacy TLS
// is on, and the client certificate presented when the profile has one.
func TestConsoleDialPolicy(t *testing.T) {
	server, _, _ := transporttest.Cert(t, "bmc")
	_, certPath, keyPath := transporttest.Cert(t, "operator")
	for _, c := range []struct {
		name    string
		secure  bool // what the BMC's launch file asks for
		profile Profile
		err     string // the exact error, or a part of Go's TLS error
		peer    string // the client certificate the BMC must see
	}{
		{"plain console refused", false, Profile{}, "BMC requests an unencrypted console; enable this host's plain-console option explicitly", ""},
		{"plain console allowed", false, Profile{AllowPlainConsole: true}, "", ""},
		{"certificate checked", true, Profile{}, "unknown authority", ""},
		{"certificate checked, plain console allowed", true, Profile{AllowPlainConsole: true}, "unknown authority", ""},
		{"legacy TLS", true, Profile{LegacyTLS: true}, "", ""},
		{"client certificate", true, Profile{LegacyTLS: true, ClientCert: certPath, ClientKey: keyPath}, "", "operator"},
		{"client certificate unreadable", true, Profile{LegacyTLS: true, ClientCert: certPath + ".missing", ClientKey: keyPath}, "cannot load client certificate/key", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			type seen struct {
				first []byte // the first bytes on the connection, after TLS if secure
				peer  string
			}
			got := make(chan seen, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					close(got)
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				var s seen
				if c.secure {
					tc := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{server}, ClientAuth: tls.RequestClientCert})
					if tc.Handshake() != nil {
						got <- s
						return
					}
					if certs := tc.ConnectionState().PeerCertificates; len(certs) > 0 {
						s.peer = certs[0].Subject.CommonName
					}
					conn = tc
				}
				s.first = make([]byte, 4)
				if _, err := io.ReadFull(conn, s.first); err != nil {
					s.first = nil
				}
				got <- s
			}()
			p := c.profile
			p.Host = "127.0.0.1"
			port := ln.Addr().(*net.TCPAddr).Port
			w, err := transport.Dial(context.Background(), p.Policy(), port, c.secure)
			if w != nil {
				w.Write([]byte("ping"))
				defer w.Close()
			}
			switch {
			case c.err == "" && err != nil:
				t.Fatal(err)
			case c.err != "" && (err == nil || !(err.Error() == c.err || strings.Contains(err.Error(), c.err) && strings.Contains(c.err, "authority"))):
				t.Fatalf("error %v, want %q", err, c.err)
			}
			if c.err != "" && !strings.Contains(c.err, "authority") {
				// Refused before connecting: the BMC sees no connection at all.
				ln.Close()
				if s, ok := <-got; ok {
					t.Fatalf("the BMC saw a connection: %+v", s)
				}
				return
			}
			s := <-got
			if c.err != "" {
				if s.first != nil {
					t.Fatal("data reached the BMC over an unverified connection")
				}
				return
			}
			if string(s.first) != "ping" || s.peer != c.peer {
				t.Fatalf("the BMC saw %q with client certificate %q, want \"ping\" with %q", s.first, s.peer, c.peer)
			}
		})
	}
	for _, port := range []int{0, -1, 65536} {
		if _, err := transport.Dial(context.Background(), Profile{Host: "127.0.0.1"}.Policy(), port, true); err == nil || err.Error() != "invalid console port" {
			t.Errorf("port %d: %v", port, err)
		}
	}
}
