package protocol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

func newSMWeb(t *testing.T, mode string, routes map[string]http.HandlerFunc) (*Web, *smBMC, *[]string, context.Context) {
	bmc := &smBMC{t: t, routes: routes}
	srv := httptest.NewTLSServer(bmc)
	t.Cleanup(srv.Close)
	w, err := NewWeb(Profile{Host: "127.0.0.1", Family: "aten", Username: smUser, LegacyTLS: true, ConsoleMode: mode})
	if err != nil {
		t.Fatal(err)
	}
	w.Base = srv.URL
	stages := &[]string{}
	ctx := WithProgress(context.Background(), func(p Progress) { *stages = append(*stages, p.Stage) })
	return w, bmc, stages, ctx
}

func noSecret(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), smPass) {
		t.Fatal("password leaked into error")
	}
}

// X9-era firmware: plain login form, Java iKVM JNLP.
func TestSupermicroX9JavaJNLP(t *testing.T) {
	w, bmc, stages, ctx := newSMWeb(t, "", map[string]http.HandlerFunc{
		"GET /":               body(`<form action="/cgi/login.cgi"><input name="name"><input name="pwd"></form>`),
		"POST /cgi/login.cgi": atenLogin(false),
		"GET " + atenJNLPPath: sessionOnly(smJNLP10),
		"GET /cgi/url_redirect.cgi?url_name=topmenu": body(`<script>SmcCsrfInsert("CSRF_TOKEN", "abcdef123");</script>`),
	})
	l, err := w.Login(ctx, smPass)
	if err != nil {
		t.Fatal(err)
	}
	if l.Family != "aten" || l.HTML5 || l.Token != "Tok3nAbc" || l.PasswordToken != "" || !l.Secure || l.Port != 5900 || l.CDPort != 623 {
		t.Fatalf("%+v", l)
	}
	if !reflect.DeepEqual(*stages, []string{"logging_in", "discovering"}) {
		t.Fatal(*stages)
	}
	// The console preference (CSRF-protected op.cgi) is never touched.
	if bmc.hit("POST /cgi/op.cgi") || bmc.hit("GET "+atenHTML5Path) {
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
		"GET /":                  body(loginPage),
		"POST /cgi/login.cgi":    atenLogin(true),
		"GET " + atenAltJNLPPath: sessionOnly(smJNLP22),
	})
	l, err := w.Login(ctx, smPass)
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
		"POST /cgi/login.cgi":    atenLogin(false),
		"GET " + atenJNLPPath:    body(`<html><body>Java plug-in is disabled</body></html>`),
		"GET " + atenAltJNLPPath: body(`<html><body>Java plug-in is disabled</body></html>`),
		"GET " + atenHTML5Path:   sessionOnly(smHTML5Page),
	})
	l, err := w.Login(ctx, smPass)
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
		"POST /cgi/login.cgi":  atenLogin(false),
		"GET " + atenHTML5Path: sessionOnly(smHTML5Page),
	})
	l, err := w.Login(ctx, smPass)
	if err != nil || !l.HTML5 || l.Token != "H5tok-777" {
		t.Fatal(l, err)
	}
}

func TestSupermicroJavaModeDoesNotFallBack(t *testing.T) {
	w, bmc, _, ctx := newSMWeb(t, "java", map[string]http.HandlerFunc{
		"POST /cgi/login.cgi":  atenLogin(false),
		"GET " + atenHTML5Path: sessionOnly(smHTML5Page),
	})
	_, err := w.Login(ctx, smPass)
	if err == nil || !strings.Contains(err.Error(), "no Java iKVM") || !strings.Contains(err.Error(), "console mode to Auto or HTML5") {
		t.Fatal(err)
	}
	if bmc.hit("GET " + atenHTML5Path) {
		t.Fatal("java mode used the HTML5 console")
	}
	if !bmc.hit("GET /cgi/logout.cgi") {
		t.Fatal("failed discovery left the web session open")
	}
}

func TestSupermicroHTML5ModeSkipsJava(t *testing.T) {
	w, bmc, _, ctx := newSMWeb(t, "HTML5", map[string]http.HandlerFunc{
		"POST /cgi/login.cgi":  atenLogin(false),
		"GET " + atenJNLPPath:  sessionOnly(smJNLP10),
		"GET " + atenHTML5Path: sessionOnly(smHTML5Page),
	})
	l, err := w.Login(ctx, smPass)
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
		"POST /cgi/login.cgi":    atenLogin(false),
		"GET " + atenJNLPPath:    body(smTimeoutPage),
		"GET " + atenAltJNLPPath: body(smTimeoutPage),
		"GET " + atenHTML5Path:   body(smTimeoutPage),
	})
	_, err := w.Login(ctx, smPass)
	noSecret(t, err)
	if err == nil || !strings.Contains(err.Error(), "instead of a JNLP") || !strings.Contains(err.Error(), "HTML5 console was also unavailable") {
		t.Fatal(err)
	}
	if (*stages)[len(*stages)-1] != "discovering" {
		t.Fatal(*stages)
	}
}

func TestSupermicroWrongPasswordIsLoginStage(t *testing.T) {
	w, bmc, stages, ctx := newSMWeb(t, "", map[string]http.HandlerFunc{
		"POST /cgi/login.cgi": atenLogin(false),
		"GET " + atenJNLPPath: sessionOnly(smJNLP10),
	})
	_, err := w.Login(ctx, "wrong-password")
	if err == nil || !strings.Contains(err.Error(), "password was rejected") || strings.Contains(err.Error(), "wrong-password") {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*stages, []string{"logging_in"}) || bmc.hit("GET "+atenJNLPPath) {
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
	l, err := w.Login(ctx, smPass)
	if err != nil {
		t.Fatal(err, bmc.hits)
	}
	if !l.HTML5 || !l.Redfish || l.Token != "H5tok-777" || l.Port != 443 {
		t.Fatalf("%+v", l)
	}
	if !reflect.DeepEqual(*stages, []string{"logging_in", "discovering"}) {
		t.Fatal(*stages)
	}
	info := l.ConsoleInfo(w.Profile)
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
	_, err := w.Login(ctx, smPass)
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
	_, err := w.Login(ctx, "wrong")
	if err == nil || !strings.Contains(err.Error(), "rejected the account or password (Redfish HTTP 401)") {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*stages, []string{"logging_in"}) {
		t.Fatal(*stages)
	}
	w, _, _, ctx = newSMWeb(t, "", redfish(t, redfishIKVMOem, "https://evil.invalid/redfish/x.IKVM", smHTML5Page))
	if _, err = w.Login(ctx, smPass); err == nil || !strings.Contains(err.Error(), "outside this BMC") {
		t.Fatal(err)
	}
}

// Firmware whose login.cgi works but that has neither JNLP nor HTML5 page
// is followed into Redfish as well.
func TestSupermicroHTML5PageMissingTriesRedfish(t *testing.T) {
	routes := redfish(t, redfishIKVMOem, "/redfish/Kk1D4UVATDja0Jw.IKVM", smHTML5Page)
	routes["POST /cgi/login.cgi"] = atenLogin(false)
	w, _, _, ctx := newSMWeb(t, "", routes)
	l, err := w.Login(ctx, smPass)
	if err != nil || !l.Redfish {
		t.Fatal(l, err)
	}
}

func TestConsoleModeValidation(t *testing.T) {
	for in, want := range map[string]string{"": "auto", "Auto": "auto", "java": "java", " html5 ": "html5"} {
		if got, err := (Profile{ConsoleMode: in}).ConsoleModeValue(); err != nil || got != want {
			t.Fatal(in, got, err)
		}
	}
	w, _ := NewWeb(Profile{Host: "bmc", Family: "aten", ConsoleMode: "vnc"})
	w.Client.Transport = transport(func(r *http.Request) (*http.Response, error) { t.Fatal("network used"); return nil, nil })
	if _, err := w.Login(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "Auto, Java or HTML5") {
		t.Fatal(err)
	}
	w, _ = NewWeb(Profile{Host: "bmc", Family: "ami", ConsoleMode: "html5"})
	w.Client.Transport = transport(func(r *http.Request) (*http.Response, error) { t.Fatal("network used"); return nil, nil })
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

func atenJNLP(args ...string) []byte {
	var s strings.Builder
	s.WriteString(`<jnlp><application-desc main-class="tw.com.aten.ikvm.KVMMain">`)
	for _, a := range args {
		s.WriteString("<argument>" + a + "</argument>")
	}
	s.WriteString("</application-desc></jnlp>")
	return []byte(s.String())
}

// Argument layouts per iKVM version (drac-kvm version table, ATENtion, osso.nl).
func TestATENJNLPVariants(t *testing.T) {
	cases := []struct {
		name               string
		jnlp               []byte
		port, cd           int
		secure             bool
		token, passwordTok string
	}{
		{"1.69.21 (8 args, plain)", atenJNLP("bmc", "user", "pass", "null", "5900", "623", "2", "0"), 5900, 623, false, "user", "pass"},
		{"1.69.27 (tunnel off)", atenJNLP("bmc", "tok", "tok", "null", "5900", "623", "0", "0", "0", "3520"), 5900, 623, false, "tok", ""},
		{"1.69.37 (tunnel on)", smJNLP10Bytes(), 5900, 623, true, "Tok3nAbc", ""},
		{"1.69.39 (22 args)", []byte(smJNLP22), 5900, 623, true, "rotUser1", "rotPass2"},
		{"trailing certificate", atenJNLP("bmc", "tok", "tok", "h", "63630", "623", "0", "0", "1", "5900", "-----BEGIN CERTIFICATE-----\nMIIx\n-----END CERTIFICATE-----"), 5900, 623, true, "tok", ""},
	}
	for _, c := range cases {
		l, err := ParseJNLP(c.jnlp)
		if err != nil || l.Family != "aten" || l.Port != c.port || l.CDPort != c.cd || l.Secure != c.secure || l.Token != c.token || l.PasswordToken != c.passwordTok {
			t.Errorf("%s: %+v %v", c.name, l, err)
		}
	}
	// ATENtion's truncated fixture (certificate in argument 6) must fail cleanly.
	if _, err := ParseJNLP(atenJNLP("192.0.2.139", "t", "t", "LAB-IPMI", "63630", "623", "-----BEGIN CERTIFICATE-----")); err == nil || !strings.Contains(err.Error(), "unsupported ATEN JNLP layout") {
		t.Fatal(err)
	}
	if _, err := ParseJNLP(atenJNLP("http://x/", "a.jar", "b.jar")); err == nil {
		t.Fatal("jar-only JNLP accepted")
	}
}

func smJNLP10Bytes() []byte { return []byte(smJNLP10) }

func TestSameHostPath(t *testing.T) {
	w, _ := NewWeb(Profile{Host: "bmc.example"})
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
		if got, ok := w.sameHostPath(ref); got != want || ok != (want != "") {
			t.Errorf("%s: %q %v", ref, got, ok)
		}
	}
}
