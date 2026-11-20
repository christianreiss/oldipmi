package ami

import (
	"context"
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport/transporttest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// The AMI side of the transport policy (see internal/protocol/policy_test.go):
// the web login checks the BMC's certificate before it sends the password,
// never falls back to plain HTTP, follows no redirect off the BMC or its
// transport, and never presents the profile's client certificate.

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

// Without legacy TLS the web login checks the BMC's certificate, and fails in
// the login stage before a request (and so the password) reaches the BMC.
func TestWebLoginChecksTheCertificateBeforeCredentials(t *testing.T) {
	for _, c := range []struct{ family, prefix, suffix string }{
		{"ami", `BMC HTTP transport failed: Post "https://BMC/rpc/WEBSES/create.asp": `, ""},
	} {
		t.Run(c.family, func(t *testing.T) {
			var q requests
			srv := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) { q.record(r) }))
			defer srv.Close()
			quiet(srv)
			w, _ := newWeb(protocol.Profile{Host: "127.0.0.1", Family: c.family, Username: "admin"})
			w.Base = srv.URL
			var stages []string
			ctx := protocol.WithProgress(context.Background(), func(p protocol.Progress) { stages = append(stages, p.Stage) })
			_, err := w.login(ctx, "do-not-log")
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
	w, _ := newWeb(protocol.Profile{Host: "127.0.0.1", Family: "ami", LegacyTLS: true})
	w.Base = "https://" + srv.Listener.Addr().String()
	_, err := w.login(context.Background(), "do-not-log")
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
			w, _ := newWeb(protocol.Profile{Host: "127.0.0.1", Family: "ami", LegacyTLS: true})
			w.Base = srv.URL
			_, err := w.login(context.Background(), "do-not-log")
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
	_, certPath, keyPath := transporttest.Cert(t, "operator")
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
	w, _ := newWeb(protocol.Profile{Host: "127.0.0.1", Family: "ami", Username: "admin", LegacyTLS: true, ClientCert: certPath, ClientKey: keyPath})
	w.Base = srv.URL
	if _, err := w.login(context.Background(), "pw"); err != nil {
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
