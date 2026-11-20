package transport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestSameHostPath(t *testing.T) {
	w, _ := NewWeb(Policy{Host: "bmc.example"})
	for ref, want := range map[string]string{
		"/redfish/v1/SessionService/Sessions/7":                         "/redfish/v1/SessionService/Sessions/7",
		"https://bmc.example/redfish/v1/SessionService/Sessions/7":      "/redfish/v1/SessionService/Sessions/7",
		"https://BMC.example:443/redfish/v1/SessionService/Sessions/7":  "/redfish/v1/SessionService/Sessions/7",
		"https://bmc.example:8443/redfish/v1/SessionService/Sessions/7": "",
		"http://bmc.example/redfish/x.IKVM":                             "",
		"https://evil.invalid/redfish/x.IKVM":                           "",
		"//evil.invalid/x":                                              "",
		"/cgi/url_redirect.cgi?url_name=man_ikvm_html5_bootstrap&x=1":   "/cgi/url_redirect.cgi?url_name=man_ikvm_html5_bootstrap&x=1",
	} {
		if got, ok := w.SameHostPath(ref); got != want || ok != (want != "") {
			t.Errorf("%s: %q %v", ref, got, ok)
		}
	}
}

// ATEN firmware sends a malformed-date "delete" cookie for SID before the real one.
func TestEmptyCookieDoesNotShadowSession(t *testing.T) {
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
	w, err := NewWeb(Policy{Host: "127.0.0.1", LegacyTLS: true})
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
