package aten

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"oldipmi/internal/protocol"
	"oldipmi/internal/testkit"
	"oldipmi/internal/transport"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Synthetic Supermicro BMCs. None of these flows except the X10/X11 HTML5
// fallback has been verified against real hardware; the fixtures follow the
// sources cited in web.go.

const (
	smUser = "ADMIN"
	smPass = "s3cret-do-not-log"
)

const smTimeoutPage = `<html><body onload='logout_alert()'>SessionTimeout ()</body></html>`

// Classic 10-argument KVMMain layout with the TLS tunnel enabled (iKVM 1.69.37).
const smJNLP10 = `<?xml version="1.0" encoding="UTF-8"?><jnlp spec="1.0+" codebase="https://bmc/"><resources><jar href="iKVM__V1.69.37.0x0.jar" main="true"/></resources>
<application-desc main-class="tw.com.aten.ikvm.KVMMain"><argument>10.0.0.5</argument><argument>Tok3nAbc</argument><argument>Tok3nAbc</argument><argument>null</argument><argument>63630</argument><argument>623</argument><argument>0</argument><argument>0</argument><argument>1</argument><argument>5900</argument></application-desc></jnlp>`

// 22-argument layout: codebase and jar names first (osso.nl, iKVM 1.69.39).
const smJNLP22 = `<jnlp spec="1.0+" codebase="http://10.0.0.5:80/"><application-desc main-class="tw.com.aten.ikvm.KVMMain">
<argument>http://10.0.0.5:80/</argument><argument>iKVM__V1.69.39.0x0.jar</argument><argument>libwin_x86__V1.0.12.jar</argument><argument>libwin_x86_64__V1.0.12.jar</argument><argument>libwin_x86_64__V1.0.12.jar</argument><argument>liblinux_x86__V1.0.12.jar</argument><argument>liblinux_x86__V1.0.12.jar</argument><argument>liblinux_x86_64__V1.0.12.jar</argument><argument>liblinux_x86_64__V1.0.12.jar</argument><argument>libmac_x86_64__V1.0.12.jar</argument>
<argument>10.0.0.5</argument><argument>rotUser1</argument><argument>rotPass2</argument><argument>null</argument><argument>63630</argument><argument>63631</argument><argument>0</argument><argument>0</argument><argument>1</argument><argument>5900</argument><argument>623</argument><argument>1</argument></application-desc></jnlp>`

const smHTML5Page = `<html><body><input type="hidden" id="entry_value" value="H5tok-777"></body></html>`

type smBMC struct {
	t      *testing.T
	mu     sync.Mutex
	hits   []string
	routes map[string]http.HandlerFunc
}

func (b *smBMC) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.hits = append(b.hits, r.Method+" "+r.URL.RequestURI())
	b.mu.Unlock()
	if strings.HasPrefix(r.URL.Path, "/cgi/") && r.URL.Path != "/cgi/login.cgi" && r.Header.Get("Referer") == "" {
		rw.WriteHeader(500) // some ATEN web servers require a Referer
		return
	}
	if h, ok := b.routes[r.Method+" "+r.URL.RequestURI()]; ok {
		h(rw, r)
		return
	}
	http.NotFound(rw, r)
}
func (b *smBMC) hit(prefix string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, h := range b.hits {
		if strings.HasPrefix(h, prefix) {
			return true
		}
	}
	return false
}

func body(s string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) { io.WriteString(rw, s) }
}

// sessionOnly serves page only to a request carrying the SID cookie.
func sessionOnly(page string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if c, e := r.Cookie("SID"); e != nil || c.Value != "sid-ok" {
			io.WriteString(rw, smTimeoutPage)
			return
		}
		io.WriteString(rw, page)
	}
}

// atenLogin accepts the account in plain (X9) or base64+check=00 form.
func atenLogin(base64Form bool) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		name, pwd := r.PostForm.Get("name"), r.PostForm.Get("pwd")
		if base64Form {
			if r.PostForm.Get("check") != "00" {
				io.WriteString(rw, "<html>login failed</html>")
				return
			}
			n, _ := base64.StdEncoding.DecodeString(name)
			p, _ := base64.StdEncoding.DecodeString(pwd)
			name, pwd = string(n), string(p)
		}
		if name != smUser || pwd != smPass {
			io.WriteString(rw, "<html>login failed</html>")
			return
		}
		h := rw.Header()
		h.Add("Set-Cookie", "SID=; expires=Thursday,01-Jan-1970 00:00:00 GMT; ;Secure; HttpOnly")
		h.Add("Set-Cookie", "SID=sid-ok; path=/ ; ;Secure; HttpOnly")
		io.WriteString(rw, "<html>ok</html>")
	}
}

func newSMWeb(t *testing.T, mode string, routes map[string]http.HandlerFunc) (*web, *smBMC, *[]string, context.Context) {
	bmc := &smBMC{t: t, routes: routes}
	srv := httptest.NewTLSServer(bmc)
	t.Cleanup(srv.Close)
	w, err := newWeb(protocol.Profile{Host: "127.0.0.1", Family: "aten", Username: smUser, LegacyTLS: true, ConsoleMode: mode})
	if err != nil {
		t.Fatal(err)
	}
	w.Base = srv.URL
	stages := &[]string{}
	ctx := protocol.WithProgress(context.Background(), func(p protocol.Progress) { *stages = append(*stages, p.Stage) })
	return w, bmc, stages, ctx
}

func noSecret(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), smPass) {
		t.Fatal("password leaked into error")
	}
}

// A login form the BMC turns down opens no web session: the setup ends with
// what to check (errors.json).
func TestSupermicroLoginRejected(t *testing.T) {
	w, _, stages, ctx := newSMWeb(t, "", map[string]http.HandlerFunc{
		"GET /":               body(`<form action="/cgi/login.cgi"><input name="name"><input name="pwd"></form>`),
		"POST /cgi/login.cgi": atenLogin(false),
	})
	_, err := w.login(ctx, "wrong")
	testkit.ErrorRow(t, "aten: no web session", err)
	if !reflect.DeepEqual(*stages, []string{"logging_in"}) {
		t.Fatal(*stages)
	}
}

// X9-era firmware: plain login form, Java iKVM JNLP.
func TestSupermicroX9JavaJNLP(t *testing.T) {
	w, bmc, stages, ctx := newSMWeb(t, "", map[string]http.HandlerFunc{
		"GET /":               body(`<form action="/cgi/login.cgi"><input name="name"><input name="pwd"></form>`),
		"POST /cgi/login.cgi": atenLogin(false),
		"GET " + jnlpPath:     sessionOnly(smJNLP10),
		"GET /cgi/url_redirect.cgi?url_name=topmenu": body(`<script>SmcCsrfInsert("CSRF_TOKEN", "abcdef123");</script>`),
	})
	l, err := w.login(ctx, smPass)
	if err != nil {
		t.Fatal(err)
	}
	if l.HTML5 || l.Token != "Tok3nAbc" || l.PasswordToken != "" || !l.Secure || l.Port != 5900 || l.CDPort != 623 {
		t.Fatalf("%+v", l)
	}
	if !reflect.DeepEqual(*stages, []string{"logging_in", "discovering"}) {
		t.Fatal(*stages)
	}
	// The console preference (CSRF-protected op.cgi) is never touched.
	if bmc.hit("POST /cgi/op.cgi") || bmc.hit("GET "+html5Path) {
		t.Fatal(bmc.hits)
	}
	w.Close()
	w.Close() // idempotent
	if !bmc.hit("GET /cgi/logout.cgi") {
		t.Fatal("web session not logged out")
	}
}

// Later X10/X11 firmware: the login page base64-encodes the fields and adds
// check=00; this firmware also serves the 22-argument JNLP at man_ikvm.
func TestSupermicroBase64LoginAndLongJNLP(t *testing.T) {
	loginPage := `<script>function checkform(){ form1.name.value = btoa (form1.name.value);
        form1.pwd.value  = btoa (form1.pwd.value); document.form1.submit(); }</script>
<form name="form1" action="/cgi/login.cgi" method="post"><input name="name"><input name="pwd" id="pwd"><input name="check" type="hidden" value="00"></form>`
	w, bmc, _, ctx := newSMWeb(t, "auto", map[string]http.HandlerFunc{
		"GET /":               body(loginPage),
		"POST /cgi/login.cgi": atenLogin(true),
		"GET " + altJNLPPath:  sessionOnly(smJNLP22),
	})
	l, err := w.login(ctx, smPass)
	noSecret(t, err)
	if err != nil {
		t.Fatal(err, bmc.hits)
	}
	if l.Token != "rotUser1" || l.PasswordToken != "rotPass2" || !l.Secure || l.Port != 5900 || l.CDPort != 623 {
		t.Fatalf("%+v", l)
	}
}

// Firmware with Java iKVM disabled answers the JNLP URL with a web page.
func TestSupermicroJavaDisabledFallsBackToHTML5(t *testing.T) {
	w, bmc, stages, ctx := newSMWeb(t, "", map[string]http.HandlerFunc{
		"POST /cgi/login.cgi": atenLogin(false),
		"GET " + jnlpPath:     body(`<html><body>Java plug-in is disabled</body></html>`),
		"GET " + altJNLPPath:  body(`<html><body>Java plug-in is disabled</body></html>`),
		"GET " + html5Path:    sessionOnly(smHTML5Page),
	})
	l, err := w.login(ctx, smPass)
	if err != nil {
		t.Fatal(err, bmc.hits)
	}
	if !l.HTML5 || l.Redfish || l.Token != "H5tok-777" || l.Cookie != "SID=sid-ok" || l.Port != 443 || !l.Secure {
		t.Fatalf("%+v", l)
	}
	if !reflect.DeepEqual(*stages, []string{"logging_in", "discovering"}) {
		t.Fatal(*stages)
	}
}

// Firmware without Java iKVM (verified live on an X10/X11-era BMC): 404.
func TestSupermicro404FallsBackToHTML5(t *testing.T) {
	w, _, _, ctx := newSMWeb(t, "", map[string]http.HandlerFunc{
		"POST /cgi/login.cgi": atenLogin(false),
		"GET " + html5Path:    sessionOnly(smHTML5Page),
	})
	l, err := w.login(ctx, smPass)
	if err != nil || !l.HTML5 || l.Token != "H5tok-777" || l.ClientType {
		t.Fatal(l, err)
	}
}

// The BMC's own noVNC client decides the token-auth length: X12/X13 firmware
// (verified live, cgi and Redfish paths) appends a client-type byte, X10/X11
// does not. The handshake is otherwise identical.
const (
	smRFBX11 = `_negotiate_insyde_auth:function(){var definedAuthLen=24;sendUsername.length=definedAuthLen*2;this._sock.send(sendUsername)}`
	smRFBX12 = `_negotiate_insyde_auth:function(){var definedAuthLen=24;var clientType=0;sendUsername.length=definedAuthLen*2+1;sendUsername[definedAuthLen*2]=clientType}`
)

func TestSupermicroHTML5ClientTypeFromBMCClient(t *testing.T) {
	for _, c := range []struct {
		rfb  string
		want bool
	}{{smRFBX11, false}, {smRFBX12, true}, {"", false}} {
		routes := map[string]http.HandlerFunc{
			"POST /cgi/login.cgi": atenLogin(false),
			"GET " + html5Path:    sessionOnly(smHTML5Page),
		}
		if c.rfb != "" {
			routes["GET "+noVNCRFB] = body(c.rfb)
		}
		w, _, _, ctx := newSMWeb(t, "html5", routes)
		l, err := w.login(ctx, smPass)
		if err != nil || !l.HTML5 || l.ClientType != c.want {
			t.Fatalf("rfb %q: %+v %v", c.rfb, l, err)
		}
		w.Close()
	}
	routes := redfish(t, redfishIKVMOem, "/redfish/Kk1D4UVATDja0Jw.IKVM", smHTML5Page)
	routes["GET "+noVNCRFB] = body(smRFBX12)
	w, _, _, ctx := newSMWeb(t, "", routes)
	if l, err := w.login(ctx, smPass); err != nil || !l.Redfish || !l.ClientType {
		t.Fatalf("redfish: %+v %v", l, err)
	}
	w.Close()
}

func TestSupermicroJavaModeDoesNotFallBack(t *testing.T) {
	w, bmc, _, ctx := newSMWeb(t, "java", map[string]http.HandlerFunc{
		"POST /cgi/login.cgi": atenLogin(false),
		"GET " + html5Path:    sessionOnly(smHTML5Page),
	})
	_, err := w.login(ctx, smPass)
	if err == nil || !strings.Contains(err.Error(), "no Java iKVM") || !strings.Contains(err.Error(), "console mode to Auto or HTML5") {
		t.Fatal(err)
	}
	if bmc.hit("GET " + html5Path) {
		t.Fatal("java mode used the HTML5 console")
	}
	if !bmc.hit("GET /cgi/logout.cgi") {
		t.Fatal("failed discovery left the web session open")
	}
}

func TestSupermicroHTML5ModeSkipsJava(t *testing.T) {
	w, bmc, _, ctx := newSMWeb(t, "HTML5", map[string]http.HandlerFunc{
		"POST /cgi/login.cgi": atenLogin(false),
		"GET " + jnlpPath:     sessionOnly(smJNLP10),
		"GET " + html5Path:    sessionOnly(smHTML5Page),
	})
	l, err := w.login(ctx, smPass)
	if err != nil || !l.HTML5 {
		t.Fatal(l, err)
	}
	if bmc.hit("GET /cgi/url_redirect.cgi?url_name=ikvm") || bmc.hit("GET /cgi/url_redirect.cgi?url_name=man_ikvm&") {
		t.Fatal("html5 mode requested the Java iKVM", bmc.hits)
	}
}

// A valid SID followed by timeout pages on both consoles is a discovery error
// that names both attempts.
func TestSupermicroSessionNotAcceptedByConsoles(t *testing.T) {
	w, _, stages, ctx := newSMWeb(t, "", map[string]http.HandlerFunc{
		"POST /cgi/login.cgi": atenLogin(false),
		"GET " + jnlpPath:     body(smTimeoutPage),
		"GET " + altJNLPPath:  body(smTimeoutPage),
		"GET " + html5Path:    body(smTimeoutPage),
	})
	_, err := w.login(ctx, smPass)
	noSecret(t, err)
	if err == nil || !strings.Contains(err.Error(), "instead of a JNLP") || !strings.Contains(err.Error(), "HTML5 console was also unavailable") {
		t.Fatal(err)
	}
	if (*stages)[len(*stages)-1] != "discovering" {
		t.Fatal(*stages)
	}
}

// An AMI launch file, complete or not, names the family to select, and the
// web session is closed again.
func TestSupermicroAMIJNLPNamesTheFamily(t *testing.T) {
	for _, jnlp := range []string{
		`<jnlp><application-desc><argument>-kvmtoken</argument><argument>tok-1</argument><argument>-kvmport</argument><argument>7578</argument></application-desc></jnlp>`,
		`<jnlp><application-desc><argument>-kvmtoken</argument></application-desc></jnlp>`,
	} {
		w, bmc, stages, ctx := newSMWeb(t, "", map[string]http.HandlerFunc{
			"POST /cgi/login.cgi": atenLogin(false),
			"GET " + jnlpPath:     sessionOnly(jnlp),
		})
		_, err := w.login(ctx, smPass)
		if err == nil || err.Error() != "the BMC served a non-ATEN JNLP; select the AMI family for this profile" {
			t.Fatalf("%s: %v", jnlp, err)
		}
		if !reflect.DeepEqual(*stages, []string{"logging_in", "discovering"}) || !bmc.hit("GET /cgi/logout.cgi") {
			t.Fatal(*stages, bmc.hits)
		}
	}
}

func TestSupermicroWrongPasswordIsLoginStage(t *testing.T) {
	w, bmc, stages, ctx := newSMWeb(t, "", map[string]http.HandlerFunc{
		"POST /cgi/login.cgi": atenLogin(false),
		"GET " + jnlpPath:     sessionOnly(smJNLP10),
	})
	_, err := w.login(ctx, "wrong-password")
	if err == nil || !strings.Contains(err.Error(), "password was rejected") || strings.Contains(err.Error(), "wrong-password") {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*stages, []string{"logging_in"}) || bmc.hit("GET "+jnlpPath) {
		t.Fatal(*stages, bmc.hits)
	}
	n := 0
	for _, h := range bmc.hits {
		if h == "POST /cgi/login.cgi" {
			n++
		}
	}
	if n != 1 {
		t.Fatal("login attempts:", n)
	}
}

// redfish builds a Redfish session service; ikvmPath is where the iKVM
// resource lives (X12/X13 OEM path or the older X11 path).
func redfish(t *testing.T, ikvmPath, uri, page string) map[string]http.HandlerFunc {
	const tok = "xauth-123"
	authed := func(h http.HandlerFunc) http.HandlerFunc {
		return func(rw http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Auth-Token") != tok {
				rw.WriteHeader(401)
				return
			}
			h(rw, r)
		}
	}
	return map[string]http.HandlerFunc{
		"GET /":               body(`<html>new web UI</html>`),
		"POST /cgi/login.cgi": func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(400) },
		"POST " + redfishSessions: func(rw http.ResponseWriter, r *http.Request) {
			var c struct{ UserName, Password string }
			if r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&c) != nil || c.UserName != smUser || c.Password != smPass {
				rw.WriteHeader(401)
				return
			}
			rw.Header().Set("X-Auth-Token", tok)
			rw.Header().Set("Location", redfishSessions+"/7")
			rw.WriteHeader(201)
		},
		"GET " + ikvmPath: authed(func(rw http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(rw, `{"@odata.type":"#IKVM.v1_0_2.IKVM","Id":"IKVM","Current interface":"HTML 5","URI":%q}`, uri)
		}),
		"GET /redfish/Kk1D4UVATDja0Jw.IKVM": body(page),
		"DELETE " + redfishSessions + "/7":  authed(func(rw http.ResponseWriter, r *http.Request) {}),
	}
}

func TestSupermicroX13RedfishHTML5(t *testing.T) {
	w, bmc, stages, ctx := newSMWeb(t, "", redfish(t, redfishIKVMOem, "/redfish/Kk1D4UVATDja0Jw.IKVM", smHTML5Page))
	l, err := w.login(ctx, smPass)
	if err != nil {
		t.Fatal(err, bmc.hits)
	}
	if !l.HTML5 || !l.Redfish || l.Token != "H5tok-777" || l.Port != 443 {
		t.Fatalf("%+v", l)
	}
	if !reflect.DeepEqual(*stages, []string{"logging_in", "discovering"}) {
		t.Fatal(*stages)
	}
	info := l.ConsoleInfo(w.p)
	b, _ := json.Marshal(info)
	if strings.Contains(string(b), "Kk1D4UVATDja0Jw") || strings.Contains(string(b), l.Token) || !strings.Contains(info.Protocol, "Redfish") {
		t.Fatal(string(b))
	}
	w.Close()
	if !bmc.hit("DELETE "+redfishSessions+"/7") || bmc.hit("GET /cgi/logout.cgi") {
		t.Fatal(bmc.hits)
	}
}

// An unknown console page format fails with the generation named, and the
// Redfish session is still deleted.
func TestSupermicroRedfishUnsupportedPage(t *testing.T) {
	w, bmc, _, ctx := newSMWeb(t, "", redfish(t, redfishIKVMLegacy, "/redfish/Kk1D4UVATDja0Jw.IKVM", `<html><app-root></app-root></html>`))
	_, err := w.login(ctx, smPass)
	if err == nil || !strings.Contains(err.Error(), "X10/X11 (Redfish iKVM)") || !strings.Contains(err.Error(), "not supported yet") {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "Kk1D4UVATDja0Jw") {
		t.Fatal("console URI token leaked")
	}
	if !bmc.hit("DELETE " + redfishSessions + "/7") {
		t.Fatal("Redfish session left open")
	}
}

func TestSupermicroRedfishRejectedAndOffHost(t *testing.T) {
	w, _, stages, ctx := newSMWeb(t, "", redfish(t, redfishIKVMOem, "/redfish/Kk1D4UVATDja0Jw.IKVM", smHTML5Page))
	_, err := w.login(ctx, "wrong")
	if err == nil || !strings.Contains(err.Error(), "rejected the account or password (Redfish HTTP 401)") {
		t.Fatal(err)
	}
	testkit.ErrorRow(t, "aten: Redfish login rejected", err)
	if !reflect.DeepEqual(*stages, []string{"logging_in"}) {
		t.Fatal(*stages)
	}
	w, _, _, ctx = newSMWeb(t, "", redfish(t, redfishIKVMOem, "https://evil.invalid/redfish/x.IKVM", smHTML5Page))
	if _, err = w.login(ctx, smPass); err == nil || !strings.Contains(err.Error(), "outside this BMC") {
		t.Fatal(err)
	}
}

// Firmware whose login.cgi works but that has neither JNLP nor HTML5 page
// is followed into Redfish as well.
func TestSupermicroHTML5PageMissingTriesRedfish(t *testing.T) {
	routes := redfish(t, redfishIKVMOem, "/redfish/Kk1D4UVATDja0Jw.IKVM", smHTML5Page)
	routes["POST /cgi/login.cgi"] = atenLogin(false)
	w, _, _, ctx := newSMWeb(t, "", routes)
	l, err := w.login(ctx, smPass)
	if err != nil || !l.Redfish {
		t.Fatal(l, err)
	}
}

// An unknown console mode fails before anything is sent.
func TestConsoleModeValidation(t *testing.T) {
	w, _ := newWeb(protocol.Profile{Host: "bmc", Family: "aten", ConsoleMode: "vnc"})
	w.Client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) { t.Fatal("network used"); return nil, nil })
	if _, err := w.login(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "Auto, Java or HTML5") {
		t.Fatal(err)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A rejected ATEN login sets no session cookie and answers every page with a
// "session timeout" page; it must fail in the login stage.
func TestNonJNLPResponseExplainsLogin(t *testing.T) {
	w, _ := newWeb(protocol.Profile{Host: "bmc", Family: "aten"})
	var stages []string
	ctx := protocol.WithProgress(context.Background(), func(p protocol.Progress) { stages = append(stages, p.Stage) })
	w.Client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<html><body onload='logout_alert()'>SessionTimeout ()</body></html>`)), Header: make(http.Header)}, nil
	})
	_, e := w.login(ctx, "do-not-log")
	if e == nil || !strings.Contains(e.Error(), "password was rejected") || strings.Contains(e.Error(), "do-not-log") {
		t.Fatal(e)
	}
	if len(stages) != 1 || stages[0] != "logging_in" {
		t.Fatal("rejected login reported as a later stage:", stages)
	}
}

// A failed request keeps its cause behind the explanation, for the error codes
// of step 3.9: the BMC's HTTP status (transport.HTTPStatus), the HTTP client's
// error and the missing Java console stay readable, and the text starts as
// before.
func TestWebErrorsKeepTheirCause(t *testing.T) {
	fail := func(rw http.ResponseWriter, r *http.Request) { rw.WriteHeader(503) }
	abort := func(rw http.ResponseWriter, r *http.Request) { panic(http.ErrAbortHandler) }
	status := func(err error) bool { return transport.HTTPStatus(err) == 503 }
	client := func(err error) bool { var u *url.Error; return errors.As(err, &u) }
	noJava := func(err error) bool { var ju javaUnavailable; return errors.As(err, &ju) }
	aten := func(more map[string]http.HandlerFunc) map[string]http.HandlerFunc {
		routes := map[string]http.HandlerFunc{"POST /cgi/login.cgi": atenLogin(false)}
		for k, v := range more {
			routes[k] = v
		}
		return routes
	}
	redfishWith := func(route string, h http.HandlerFunc) map[string]http.HandlerFunc {
		routes := redfish(t, redfishIKVMOem, "/redfish/Kk1D4UVATDja0Jw.IKVM", smHTML5Page)
		routes[route] = h
		return routes
	}
	webPage := body("<html>no Java here</html>")
	for _, c := range []struct {
		name, mode, jnlpPath, text string
		routes                     map[string]http.HandlerFunc
		causes                     []func(error) bool
	}{
		{"login", "", "", "the BMC web login (/cgi/login.cgi) failed: BMC HTTP status 503;",
			map[string]http.HandlerFunc{"POST /cgi/login.cgi": fail}, []func(error) bool{status}},
		{"java request", "", "", "the Java iKVM request failed: BMC HTTP status 503;",
			aten(map[string]http.HandlerFunc{"GET " + jnlpPath: fail}), []func(error) bool{status}},
		{"java mode", "java", "", "this firmware has no Java iKVM (the JNLP URL answers 404);",
			aten(nil), []func(error) bool{noJava}},
		{"custom JNLP path", "", "/custom.jnlp", "this firmware has no Java iKVM (the JNLP URL answers 404) at this profile's custom JNLP path;",
			aten(nil), []func(error) bool{noJava}},
		{"html5 after java", "", "", "the BMC answered the Java iKVM request with a web page",
			aten(map[string]http.HandlerFunc{"GET " + jnlpPath: webPage, "GET " + altJNLPPath: webPage, "GET " + html5Path: fail}),
			[]func(error) bool{noJava, status}},
		{"redfish login", "", "", "the Redfish login failed: BMC HTTP transport failed:",
			redfishWith("POST "+redfishSessions, abort), []func(error) bool{client}},
		{"redfish ikvm", "", "", "the Redfish iKVM request failed: BMC HTTP transport failed:",
			redfishWith("GET "+redfishIKVMOem, abort), []func(error) bool{client}},
		{"redfish page", "", "", "the Supermicro X12/X13 (Redfish OEM iKVM) console page request failed: BMC HTTP status 503;",
			redfishWith("GET /redfish/Kk1D4UVATDja0Jw.IKVM", fail), []func(error) bool{status}},
	} {
		w, _, _, ctx := newSMWeb(t, c.mode, c.routes)
		w.p.JNLPPath = c.jnlpPath
		_, err := w.login(ctx, smPass)
		noSecret(t, err)
		if err == nil || !strings.HasPrefix(err.Error(), c.text) {
			t.Fatalf("%s: %v", c.name, err)
		}
		for i, cause := range c.causes {
			if !cause(err) {
				t.Errorf("%s: cause %d lost: %v", c.name, i, err)
			}
		}
	}
}
