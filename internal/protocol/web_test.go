package protocol

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
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

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestLoginFailureDoesNotFetchToken(t *testing.T) {
	w, _ := NewWeb(Profile{Host: "bmc", Family: "ami"})
	count := 0
	w.Client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		count++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{'HAPI_STATUS':-6,'SESSION_COOKIE':'Failure_Login'}`)), Header: make(http.Header)}, nil
	})
	_, e := w.Login(context.Background(), "do-not-log")
	if e == nil || count != 1 || strings.Contains(e.Error(), "do-not-log") {
		t.Fatal(count, e)
	}
}
func FuzzJNLP(f *testing.F) {
	f.Add([]byte("<jnlp/>"))
	f.Fuzz(func(t *testing.T, b []byte) { ParseJNLP(b) })
}
