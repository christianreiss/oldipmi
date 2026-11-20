package protocol

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// The transport policy of a profile: what each exception (plain HTTP login,
// legacy TLS, unencrypted console, client certificate) allows, the exact error
// when a BMC asks for more, and that nothing secret leaves before the policy is
// met. Phase 3 moves this code to internal/transport; these tests pin today's
// behaviour there, including its differences: the web login never presents the
// client certificate, the console connection does.

// testCert makes a self-signed certificate for 127.0.0.1, also as PEM files
// (for Profile.ClientCert and ClientKey).
func testCert(t *testing.T, cn string) (tls.Certificate, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, cn+".crt"), filepath.Join(dir, cn+".key")
	if os.WriteFile(certPath, certPEM, 0o600) != nil || os.WriteFile(keyPath, keyPEM, 0o600) != nil {
		t.Fatal("cannot write the test certificate")
	}
	return pair, certPath, keyPath
}

// requests records what reached a test BMC.
type requests struct {
	mu   sync.Mutex
	seen []string // "METHOD path"
	cert []bool   // whether the request came with a client certificate
	form []string // the form body of each request
}

func (q *requests) record(r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seen = append(q.seen, r.Method+" "+r.URL.RequestURI())
	q.cert = append(q.cert, r.TLS != nil && len(r.TLS.PeerCertificates) > 0)
	q.form = append(q.form, string(b))
}

func (q *requests) list() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.seen...)
}

// quiet keeps the expected failed handshakes out of the test log.
func quiet(srv *httptest.Server) { srv.Config.ErrorLog = log.New(io.Discard, "", 0) }

// shown replaces a test server's address, which changes every run, in an error.
func shown(err error, srv *httptest.Server) string {
	if err == nil {
		return "<nil>"
	}
	return strings.ReplaceAll(err.Error(), srv.Listener.Addr().String(), "BMC")
}

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
		w, err := NewWeb(c.p)
		if err != nil || w.Base != c.base {
			t.Errorf("%+v: base %q, error %v; want %q", c.p, w.Base, err, c.base)
		}
	}
	for _, host := range []string{"", "https://bmc", "bmc/path", "admin@bmc", "bmc?x", "bmc#x", "bmc name", "bmc\\x"} {
		_, err := NewWeb(Profile{Host: host})
		if err == nil || err.Error() != "enter a BMC hostname or IP address without a URL" {
			t.Errorf("host %q: %v", host, err)
		}
	}
}

// Without legacy TLS the web login checks the BMC's certificate, and fails in
// the login stage before a request (and so the password) reaches the BMC.
func TestWebLoginChecksTheCertificateBeforeCredentials(t *testing.T) {
	for _, c := range []struct{ family, prefix, suffix string }{
		{"ami", `BMC HTTP transport failed: Post "https://BMC/rpc/WEBSES/create.asp": `, ""},
		{"aten", `the BMC web login (/cgi/login.cgi) failed: BMC HTTP transport failed: Post "https://BMC/cgi/login.cgi": `,
			"; check the hostname and this profile's HTTP / legacy TLS options"},
	} {
		t.Run(c.family, func(t *testing.T) {
			var q requests
			srv := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { q.record(r) }))
			defer srv.Close()
			quiet(srv)
			w, _ := NewWeb(Profile{Host: "127.0.0.1", Family: c.family, Username: "admin"})
			w.Base = srv.URL
			var stages []string
			ctx := WithProgress(context.Background(), func(p Progress) { stages = append(stages, p.Stage) })
			_, err := w.Login(ctx, "do-not-log")
			msg := shown(err, srv)
			if !strings.HasPrefix(msg, c.prefix) || !strings.HasSuffix(msg, c.suffix) || !strings.Contains(msg, "unknown authority") {
				t.Fatalf("error %q", msg)
			}
			if got := q.list(); len(got) != 0 {
				t.Fatal("requests reached the BMC:", got)
			}
			if !reflect.DeepEqual(stages, []string{"logging_in"}) {
				t.Fatal("stages", stages)
			}
		})
	}
}

// An HTTPS profile never falls back to plain HTTP: against a BMC that only
// speaks HTTP, the login fails without a request reaching it.
func TestWebLoginNeverFallsBackToHTTP(t *testing.T) {
	var q requests
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { q.record(r) }))
	defer srv.Close()
	quiet(srv)
	w, _ := NewWeb(Profile{Host: "127.0.0.1", Family: "ami", LegacyTLS: true})
	w.Base = "https://" + srv.Listener.Addr().String()
	_, err := w.Login(context.Background(), "do-not-log")
	if want := `BMC HTTP transport failed: Post "https://BMC/rpc/WEBSES/create.asp": http: server gave HTTP response to HTTPS client`; shown(err, srv) != want {
		t.Fatalf("error %q\nwant  %q", shown(err, srv), want)
	}
	if got := q.list(); len(got) != 0 {
		t.Fatal("requests reached the BMC:", got)
	}
}

// A redirect may not leave the BMC or its transport: not to another host and
// not from HTTPS to HTTP, and the target never sees the login.
func TestWebRedirectsStayOnTheBMC(t *testing.T) {
	var target requests
	plain := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { target.record(r) }))
	defer plain.Close()
	for _, c := range []struct{ name, to string }{
		{"to plain HTTP", "http://" + plain.Listener.Addr().String() + "/rpc/WEBSES/create.asp"},
		{"to another host", "https://localhost:%s/rpc/WEBSES/create.asp"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var q requests
			var to string
			srv := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				q.record(r)
				http.Redirect(rw, r, to, http.StatusTemporaryRedirect)
			}))
			defer srv.Close()
			_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
			to = strings.Replace(c.to, "%s", port, 1)
			w, _ := NewWeb(Profile{Host: "127.0.0.1", Family: "ami", LegacyTLS: true})
			w.Base = srv.URL
			_, err := w.Login(context.Background(), "do-not-log")
			// Go's error names the refused target.
			want := `BMC HTTP transport failed: Post "` + to + `": BMC redirect changes host or transport; select the correct transport`
			if err == nil || err.Error() != want {
				t.Fatalf("error %v\nwant  %s", err, want)
			}
			if got := q.list(); len(got) != 1 {
				t.Fatal("requests at the BMC:", got)
			}
			if got := target.list(); len(got) != 0 {
				t.Fatal("the redirect target was contacted:", got)
			}
		})
	}
}

// The web login does not present the profile's client certificate (only the
// console connection does, see TestConsoleDialPolicy), even when the BMC asks.
func TestWebLoginSendsNoClientCertificate(t *testing.T) {
	_, certPath, keyPath := testCert(t, "operator")
	var q requests
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		q.record(r)
		switch r.URL.Path {
		case "/rpc/WEBSES/create.asp":
			io.WriteString(rw, `{'HAPI_STATUS':0,'SESSION_COOKIE':'cookie-1'}`)
		case "/Java/jviewer.jnlp":
			io.WriteString(rw, `<jnlp><application-desc><argument>-kvmtoken</argument><argument>tok-1</argument><argument>-kvmport</argument><argument>7578</argument><argument>-kvmsecure</argument><argument>1</argument></application-desc></jnlp>`)
		}
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequestClientCert}
	srv.StartTLS()
	defer srv.Close()
	w, _ := NewWeb(Profile{Host: "127.0.0.1", Family: "ami", Username: "admin", LegacyTLS: true, ClientCert: certPath, ClientKey: keyPath})
	w.Base = srv.URL
	if _, err := w.Login(context.Background(), "pw"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	want := []string{"POST /rpc/WEBSES/create.asp", "GET /Java/jviewer.jnlp", "GET /rpc/WEBSES/logout.asp"}
	if got := q.list(); !reflect.DeepEqual(got, want) {
		t.Fatalf("requests %q, want %q", got, want)
	}
	for i, sent := range q.cert {
		if sent {
			t.Errorf("%s came with the client certificate", q.seen[i])
		}
	}
}

func TestTLSConfigPerProfile(t *testing.T) {
	c := TLSConfig(Profile{Host: "bmc.example.net"})
	if c.ServerName != "bmc.example.net" || c.InsecureSkipVerify || c.MinVersion != tls.VersionTLS12 || c.CipherSuites != nil {
		t.Errorf("default: %+v", c)
	}
	c = TLSConfig(Profile{Host: "bmc.example.net", LegacyTLS: true})
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
	server, _, _ := testCert(t, "bmc")
	_, certPath, keyPath := testCert(t, "operator")
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
			w, err := Dial(context.Background(), p, port, c.secure)
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
		if _, err := Dial(context.Background(), Profile{Host: "127.0.0.1"}, port, true); err == nil || err.Error() != "invalid console port" {
			t.Errorf("port %d: %v", port, err)
		}
	}
}

// What the connection page shows per family and console (no tokens, cookies
// or credentials ever reach it).
func TestConsoleInfoPerLaunch(t *testing.T) {
	p := Profile{Host: "bmc.example.net"}
	ep := func(role, addr, transport string) Endpoint {
		return Endpoint{Role: role, Address: addr, Transport: transport}
	}
	for _, c := range []struct {
		name   string
		launch Launch
		legacy bool
		want   ConsoleInfo
	}{
		{"AMI IVTP", Launch{Family: "ami", Token: "secret", Cookie: "secret", Port: 7578, Secure: true, CDPort: 5120, MediaSecure: true}, false,
			ConsoleInfo{Protocol: "AMI IVTP", Endpoints: []Endpoint{ep("Video / input", "bmc.example.net:7578", "TLS"), ep("Media (advertised)", "bmc.example.net:5120", "TLS")},
				Capabilities: []string{"Video", "Keyboard", "Pointer", "Read-only ISO"}}},
		{"AMI IVTP, plain, no media", Launch{Family: "ami", Token: "secret", Port: 7578}, true,
			ConsoleInfo{Protocol: "AMI IVTP", Endpoints: []Endpoint{ep("Video / input", "bmc.example.net:7578", "Plain TCP")},
				Capabilities: []string{"Video", "Keyboard", "Pointer"}, LegacyTLS: true}},
		{"AMI MegaRAC VSC", Launch{Family: "ami", Legacy: true, Token: "secret", Port: 7578, HIDPort: 7582, CDPort: 5120}, false,
			ConsoleInfo{Protocol: "AMI MegaRAC VSC", Endpoints: []Endpoint{ep("Video", "bmc.example.net:7578", "Plain TCP"), ep("Input", "bmc.example.net:7582", "TLS"),
				ep("Media (advertised)", "bmc.example.net:5120", "Plain TCP")}, Capabilities: []string{"Video", "Keyboard", "Pointer", "Read-only ISO"}}},
		{"ATEN Java", Launch{Family: "aten", Token: "secret", PasswordToken: "secret", Port: 5900, Secure: true, CDPort: 623, MediaSecure: true}, false,
			ConsoleInfo{Protocol: "ATEN RFB", Endpoints: []Endpoint{ep("Video / input", "bmc.example.net:5900", "TLS"), ep("Media (advertised)", "bmc.example.net:623", "Plain TCP")},
				Capabilities: []string{"Video", "Keyboard", "Pointer", "Read-only ISO"}}},
		{"ATEN HTML5", Launch{Family: "aten", HTML5: true, Token: "secret", Cookie: "secret", Port: 443, Secure: true}, false,
			ConsoleInfo{Protocol: "ATEN HTML5 (AST2100 over WebSocket)", Endpoints: []Endpoint{ep("Video / input", "bmc.example.net:443", "TLS (WebSocket)")},
				Capabilities: []string{"Video", "Keyboard", "Pointer", "Power"}}},
		{"ATEN HTML5 over HTTP", Launch{Family: "aten", HTML5: true, Token: "secret", Port: 80}, false,
			ConsoleInfo{Protocol: "ATEN HTML5 (AST2100 over WebSocket)", Endpoints: []Endpoint{ep("Video / input", "bmc.example.net:80", "Plain TCP (WebSocket)")},
				Capabilities: []string{"Video", "Keyboard", "Pointer", "Power"}}},
		{"ATEN Redfish", Launch{Family: "aten", HTML5: true, Redfish: true, Token: "secret", Port: 443, Secure: true}, true,
			ConsoleInfo{Protocol: "ATEN HTML5 (AST2100 over WebSocket), discovered via Redfish", Endpoints: []Endpoint{ep("Video / input", "bmc.example.net:443", "TLS (WebSocket)")},
				Capabilities: []string{"Video", "Keyboard", "Pointer", "Power"}, LegacyTLS: true}},
	} {
		q := p
		q.LegacyTLS = c.legacy
		if got := c.launch.ConsoleInfo(q); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}
