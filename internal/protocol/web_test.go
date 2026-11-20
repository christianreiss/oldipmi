package protocol

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"oldipmi/internal/transport"
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
	c := transport.TLSConfig(Profile{Host: "bmc"}.Policy())
	if c.InsecureSkipVerify || c.MinVersion < tls.VersionTLS12 {
		t.Fatal("weak defaults")
	}
	c = transport.TLSConfig(Profile{LegacyTLS: true}.Policy())
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

func FuzzJNLP(f *testing.F) {
	f.Add([]byte("<jnlp/>"))
	f.Fuzz(func(t *testing.T, b []byte) { ParseJNLP(b) })
}

func TestConsoleModeValidation(t *testing.T) {
	for in, want := range map[string]string{"": "auto", "Auto": "auto", "java": "java", " html5 ": "html5"} {
		if got, err := (Profile{ConsoleMode: in}).ConsoleModeValue(); err != nil || got != want {
			t.Fatal(in, got, err)
		}
	}
	w, _ := NewWeb(Profile{Host: "bmc", Family: "ami", ConsoleMode: "html5"})
	w.Client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) { t.Fatal("network used"); return nil, nil })
	if _, err := w.Login(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "only implemented for the ATEN") {
		t.Fatal(err)
	}
	b, _ := json.Marshal(Profile{ConsoleMode: "html5"})
	if !strings.Contains(string(b), `"consoleMode":"html5"`) {
		t.Fatal(string(b))
	}
	if b, _ = json.Marshal(Profile{}); strings.Contains(string(b), "consoleMode") {
		t.Fatal("empty console mode should be omitted")
	}
}
