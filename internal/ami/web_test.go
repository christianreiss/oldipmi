package ami

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"oldipmi/internal/protocol"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// The AMI MegaRAC web login as the BMC sees it: what is requested in which
// order, with which form fields and cookies, and the logout. This transcript
// must not change.

const amiJNLP = `<jnlp><application-desc><argument>-kvmtoken</argument><argument>tok-1</argument><argument>-kvmport</argument><argument>7578</argument><argument>-kvmsecure</argument><argument>1</argument><argument>-cdport</argument><argument>5120</argument><argument>-vmsecure</argument><argument>1</argument></application-desc></jnlp>`

// The Java iKVM JNLP of an ATEN BMC (10 arguments, TLS tunnel on).
const atenJNLP10 = `<jnlp><application-desc main-class="tw.com.aten.ikvm.KVMMain"><argument>bmc</argument><argument>tok</argument><argument>tok</argument><argument>null</argument><argument>63630</argument><argument>623</argument><argument>0</argument><argument>0</argument><argument>1</argument><argument>5900</argument></application-desc></jnlp>`

type amiBMC struct {
	mu     sync.Mutex
	log    []string          // "METHOD path cookie=... form=..."
	answer map[string]string // path -> body
}

func (b *amiBMC) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	entry := r.Method + " " + r.URL.RequestURI()
	if c := r.Header.Get("Cookie"); c != "" {
		entry += " cookie=" + c
	}
	if len(body) > 0 {
		form, _ := url.ParseQuery(string(body))
		entry += " form=" + form.Encode()
	}
	b.mu.Lock()
	b.log = append(b.log, entry)
	b.mu.Unlock()
	io.WriteString(rw, b.answer[r.URL.Path])
}

func (b *amiBMC) requests() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.log...)
}

func amiLogin(t *testing.T, p protocol.Profile, answer map[string]string) (Launch, error, []string, []string) {
	t.Helper()
	bmc := &amiBMC{answer: answer}
	srv := httptest.NewTLSServer(bmc)
	defer srv.Close()
	p.Host, p.Family, p.Username, p.LegacyTLS = "127.0.0.1", "ami", "admin", true
	w, err := newWeb(p)
	if err != nil {
		t.Fatal(err)
	}
	w.Base = srv.URL
	var stages []string
	ctx := protocol.WithProgress(context.Background(), func(p protocol.Progress) { stages = append(stages, p.Stage) })
	l, err := w.login(ctx, "pw-1")
	if err == nil {
		w.Close()
	}
	if err != nil && strings.Contains(err.Error(), "pw-1") {
		t.Fatal("the password is in the error")
	}
	return l, err, bmc.requests(), stages
}

func TestAMIWebLoginTranscript(t *testing.T) {
	const create = "POST /rpc/WEBSES/create.asp form=WEBVAR_PASSWORD=pw-1&WEBVAR_USERNAME=admin"
	const cookies = " cookie=SessionCookie=cookie-1; Username=admin"
	ok := map[string]string{"/rpc/WEBSES/create.asp": `{'HAPI_STATUS':0,'SESSION_COOKIE':'cookie-1'}`, "/Java/jviewer.jnlp": amiJNLP, "/custom.jnlp": amiJNLP}

	l, err, got, stages := amiLogin(t, protocol.Profile{}, ok)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{create, "GET /Java/jviewer.jnlp" + cookies, "GET /rpc/WEBSES/logout.asp" + cookies}; !reflect.DeepEqual(got, want) {
		t.Fatalf("requests\n %q\nwant\n %q", got, want)
	}
	if !reflect.DeepEqual(stages, []string{"logging_in", "discovering"}) {
		t.Fatal("stages", stages)
	}
	want := Launch{Token: "tok-1", Cookie: "cookie-1", Port: 7578, CDPort: 5120, Secure: true, MediaSecure: true,
		Args: map[string]string{"kvmtoken": "tok-1", "kvmport": "7578", "kvmsecure": "1", "cdport": "5120", "vmsecure": "1"},
		Pos:  []string{"-kvmtoken", "tok-1", "-kvmport", "7578", "-kvmsecure", "1", "-cdport", "5120", "-vmsecure", "1"}}
	if !reflect.DeepEqual(l, want) {
		t.Fatalf("launch %+v\nwant   %+v", l, want)
	}

	// A profile's own JNLP path replaces the default.
	_, err, got, _ = amiLogin(t, protocol.Profile{JNLPPath: "/custom.jnlp"}, ok)
	if want := []string{create, "GET /custom.jnlp" + cookies, "GET /rpc/WEBSES/logout.asp" + cookies}; err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("custom JNLP path: %v\n %q\nwant\n %q", err, got, want)
	}
}

func TestAMIWebLoginFailures(t *testing.T) {
	const create = "POST /rpc/WEBSES/create.asp form=WEBVAR_PASSWORD=pw-1&WEBVAR_USERNAME=admin"
	const cookies = " cookie=SessionCookie=cookie-1; Username=admin"
	session := `{'HAPI_STATUS':0,'SESSION_COOKIE':'cookie-1'}`
	for _, c := range []struct {
		name     string
		answer   map[string]string
		err      string
		requests []string
		stages   []string
	}{
		{"rejected", map[string]string{"/rpc/WEBSES/create.asp": `{'HAPI_STATUS':-6,'SESSION_COOKIE':'Failure_Login'}`},
			"BMC rejected login (HAPI_STATUS=-6)", []string{create}, []string{"logging_in"}},
		{"no session", map[string]string{"/rpc/WEBSES/create.asp": `{'HAPI_STATUS':0}`},
			"BMC did not issue a valid login session", []string{create}, []string{"logging_in"}},
		{"failed session", map[string]string{"/rpc/WEBSES/create.asp": `{'HAPI_STATUS':0,'SESSION_COOKIE':'Failure_Session'}`},
			"BMC did not issue a valid login session", []string{create}, []string{"logging_in"}},
		// The session is ended again when the console cannot be found.
		{"no JNLP", map[string]string{"/rpc/WEBSES/create.asp": session, "/Java/jviewer.jnlp": "<html>session expired</html>"},
			"BMC returned no JNLP (login was not accepted or the session expired; check the account and password)",
			[]string{create, "GET /Java/jviewer.jnlp" + cookies, "GET /rpc/WEBSES/logout.asp" + cookies}, []string{"logging_in", "discovering"}},
		{"broken JNLP", map[string]string{"/rpc/WEBSES/create.asp": session, "/Java/jviewer.jnlp": "<jnlp><application-desc><argument>-kvmtoken</argument></application-desc></jnlp>"},
			"incomplete AMI JNLP argument pairs",
			[]string{create, "GET /Java/jviewer.jnlp" + cookies, "GET /rpc/WEBSES/logout.asp" + cookies}, []string{"logging_in", "discovering"}},
		// An ATEN launch file, complete or not, names the family to select.
		{"ATEN JNLP", map[string]string{"/rpc/WEBSES/create.asp": session, "/Java/jviewer.jnlp": atenJNLP10},
			"this BMC needs the ATEN family; select it for this profile",
			[]string{create, "GET /Java/jviewer.jnlp" + cookies, "GET /rpc/WEBSES/logout.asp" + cookies}, []string{"logging_in", "discovering"}},
		{"broken ATEN JNLP", map[string]string{"/rpc/WEBSES/create.asp": session, "/Java/jviewer.jnlp": `<jnlp><application-desc main-class="tw.com.aten.ikvm.KVMMain"><argument>bmc</argument></application-desc></jnlp>`},
			"this BMC needs the ATEN family; select it for this profile",
			[]string{create, "GET /Java/jviewer.jnlp" + cookies, "GET /rpc/WEBSES/logout.asp" + cookies}, []string{"logging_in", "discovering"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err, got, stages := amiLogin(t, protocol.Profile{}, c.answer)
			if err == nil || err.Error() != c.err {
				t.Fatalf("error %v, want %q", err, c.err)
			}
			if !reflect.DeepEqual(got, c.requests) {
				t.Fatalf("requests\n %q\nwant\n %q", got, c.requests)
			}
			if !reflect.DeepEqual(stages, c.stages) {
				t.Fatal("stages", stages)
			}
		})
	}
}

func TestFreshLoginDiscoveryAndSafeInfo(t *testing.T) {
	p := protocol.Profile{Host: "bmc.example", Family: "ami", LegacyTLS: true}
	var tokens []string
	for attempt := 1; attempt <= 2; attempt++ {
		w, _ := newWeb(p)
		var stages []string
		ctx := protocol.WithProgress(context.Background(), func(p protocol.Progress) { stages = append(stages, p.Stage) })
		w.Client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
			body := ""
			switch r.URL.Path {
			case "/rpc/WEBSES/create.asp":
				body = fmt.Sprintf(`{'HAPI_STATUS':0,'SESSION_COOKIE':'cookie-%d'}`, attempt)
			case "/Java/jviewer.jnlp":
				cookie, e := r.Cookie("SessionCookie")
				if e != nil || cookie.Value != fmt.Sprintf("cookie-%d", attempt) {
					t.Error("stale session cookie")
				}
				body = fmt.Sprintf(`<jnlp><application-desc><argument>bmc</argument><argument>5901</argument><argument>secret-token-%d</argument><argument>0</argument><argument>0</argument><argument>5120</argument><argument>5123</argument><argument>511</argument><argument>5900</argument><argument>1</argument></application-desc></jnlp>`, attempt)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})
		launch, err := w.login(ctx, "private-password")
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, launch.Token)
		if !reflect.DeepEqual(stages, []string{"logging_in", "discovering"}) {
			t.Fatal(stages)
		}
		info := launch.ConsoleInfo(p)
		b, _ := json.Marshal(info)
		for _, secret := range []string{launch.Token, launch.Cookie, "private-password"} {
			if strings.Contains(string(b), secret) {
				t.Fatal("secret leaked into console info")
			}
		}
		if len(info.Endpoints) != 3 || info.Endpoints[0].Transport != "Plain TCP" || info.Endpoints[1].Transport != "TLS" {
			t.Fatal(info)
		}
		w.Close()
	}
	if tokens[0] == tokens[1] {
		t.Fatal("reused session token")
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLoginFailureDoesNotFetchToken(t *testing.T) {
	w, _ := newWeb(protocol.Profile{Host: "bmc", Family: "ami"})
	count := 0
	w.Client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		count++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{'HAPI_STATUS':-6,'SESSION_COOKIE':'Failure_Login'}`)), Header: make(http.Header)}, nil
	})
	_, e := w.login(context.Background(), "do-not-log")
	if e == nil || count != 1 || strings.Contains(e.Error(), "do-not-log") {
		t.Fatal(count, e)
	}
}

// The HTML5 console mode is ATEN's; it fails before anything is sent.
func TestConsoleModeValidation(t *testing.T) {
	w, _ := newWeb(protocol.Profile{Host: "bmc", Family: "ami", ConsoleMode: "html5"})
	w.Client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) { t.Fatal("network used"); return nil, nil })
	if _, err := w.login(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "only implemented for the ATEN") {
		t.Fatal(err)
	}
}
