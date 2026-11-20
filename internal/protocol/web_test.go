package protocol

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestJNLPDoesNotExecuteResources(t *testing.T) {
	b := []byte(`<jnlp codebase="https://untrusted.invalid"><resources><jar href="evil.jar"/></resources><application-desc><argument>bmc</argument><argument>5901</argument><argument>token</argument><argument>0</argument><argument>0</argument><argument>5120</argument><argument>5123</argument><argument>511</argument><argument>5900</argument><argument>1</argument></application-desc></jnlp>`)
	l, e := ParseJNLP(b)
	if e != nil || !l.Legacy || l.Port != 5901 || l.Token != "token" {
		t.Fatal(l, e)
	}
}
func TestTLSPolicy(t *testing.T) {
	c := TLSConfig(Profile{Host: "bmc"})
	if c.InsecureSkipVerify || c.MinVersion < tls.VersionTLS12 {
		t.Fatal("weak defaults")
	}
	c = TLSConfig(Profile{LegacyTLS: true})
	if !c.InsecureSkipVerify {
		t.Fatal("explicit compatibility setting ignored")
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestLoginFailureDoesNotFetchToken(t *testing.T) {
	w, _ := NewWeb(Profile{Host: "bmc", Family: "ami"})
	count := 0
	w.Client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		count++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{'HAPI_STATUS':-6,'SESSION_COOKIE':'Failure_Login'}`)), Header: make(http.Header)}, nil
	})
	_, e := w.Login(context.Background(), "do-not-log")
	if e == nil || count != 1 || strings.Contains(e.Error(), "do-not-log") {
		t.Fatal(count, e)
	}
}

// A rejected ATEN login sets no session cookie and answers every page with a
// "session timeout" page; it must fail in the login stage.
func TestNonJNLPResponseExplainsLogin(t *testing.T) {
	w, _ := NewWeb(Profile{Host: "bmc", Family: "aten"})
	var stages []string
	ctx := WithProgress(context.Background(), func(p Progress) { stages = append(stages, p.Stage) })
	w.Client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<html><body onload='logout_alert()'>SessionTimeout ()</body></html>`)), Header: make(http.Header)}, nil
	})
	_, e := w.Login(ctx, "do-not-log")
	if e == nil || !strings.Contains(e.Error(), "password was rejected") || strings.Contains(e.Error(), "do-not-log") {
		t.Fatal(e)
	}
	if len(stages) != 1 || stages[0] != "logging_in" {
		t.Fatal("rejected login reported as a later stage:", stages)
	}
}
func FuzzJNLP(f *testing.F) {
	f.Add([]byte("<jnlp/>"))
	f.Fuzz(func(t *testing.T, b []byte) { ParseJNLP(b) })
}

// ATEN firmware sends a malformed-date "delete" cookie for SID before the real one.
func TestATENEmptyCookieDoesNotShadowSession(t *testing.T) {
	var seen string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi/login.cgi" {
			h := rw.Header()
			h.Add("Set-Cookie", "SID=; expires=Thursday,01-Jan-1970 00:00:00 GMT; ;Secure; HttpOnly")
			h.Add("Set-Cookie", "SID=real-session; path=/ ; ;Secure; HttpOnly")
			io.WriteString(rw, "ok")
			return
		}
		seen = r.Header.Get("Cookie")
		io.WriteString(rw, "<jnlp><application-desc main-class='aten'/></jnlp>")
	}))
	defer srv.Close()
	w, err := NewWeb(Profile{Host: "127.0.0.1", Family: "aten", LegacyTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	w.Base = srv.URL
	if _, err := w.Request(context.Background(), "POST", "/cgi/login.cgi", url.Values{"name": {"u"}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Request(context.Background(), "GET", "/cgi/url_redirect.cgi", nil, false); err != nil {
		t.Fatal(err)
	}
	if seen != "SID=real-session" {
		t.Fatalf("cookie header %q", seen)
	}
}
