package protocol

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"oldipmi/internal/transport"
	"regexp"
	"strconv"
	"strings"
)

type Launch struct {
	Family string
	Args   map[string]string
	Pos    []string
	// Token is the console credential. ATEN Java JNLPs carry a user and a
	// password field; PasswordToken is set only when the two differ.
	Token, PasswordToken, Cookie string
	Port, HIDPort, CDPort        int
	Secure, MediaSecure, Legacy  bool
	// HTML5 marks ATEN firmware that only offers the WebSocket (noVNC) console.
	// Token is then the iKVM session token and Cookie the web session cookie header.
	HTML5 bool
	// Redfish marks an HTML5 console discovered through the Supermicro Redfish
	// iKVM resource (X11 and later; the only web path documented for X12/X13).
	Redfish bool
	// ClientType marks HTML5 firmware whose own noVNC client (X12/X13
	// generation) ends the 48-byte token authentication with a client-type
	// byte; the BMC waits for it before answering.
	ClientType bool
}

// Web is a web session with the logins of the AMI and ATEN families.
type Web struct {
	*transport.Web
	Profile Profile
	Cookie  string

	cgi         bool   // an ATEN /cgi/login.cgi session may exist
	redfishPath string // Redfish session resource to DELETE on Close
}

func NewWeb(p Profile) (*Web, error) {
	tw, err := transport.NewWeb(p.Policy())
	if err != nil {
		return nil, err
	}
	w := &Web{Web: tw, Profile: p}
	// Closing logs out of the sessions this client opened: the Redfish
	// session first (Close runs the last added first), then the web login.
	w.OnClose(func(ctx context.Context) {
		if w.Profile.Family == "aten" {
			if w.cgi {
				w.Request(ctx, "GET", "/cgi/logout.cgi", nil, false)
			}
		} else if w.Cookie != "" {
			w.Request(ctx, "GET", "/rpc/WEBSES/logout.asp", nil, false)
		}
	})
	w.OnClose(func(ctx context.Context) {
		if w.redfishPath != "" {
			w.Do(ctx, "DELETE", w.redfishPath, nil, "", false)
		}
	})
	return w, nil
}

var cookieRE = regexp.MustCompile(`['"]?SESSION_COOKIE['"]?\s*:\s*['"]([^'"\r\n]+)['"]`)
var statusRE = regexp.MustCompile(`['"]?HAPI_STATUS['"]?\s*:\s*(-?\d+)`)

func (w *Web) Login(ctx context.Context, password string) (Launch, error) {
	fam := w.Profile.Family
	if fam == "" {
		fam = "ami"
	}
	mode, e := w.Profile.ConsoleModeValue()
	if e != nil {
		return Launch{}, e
	}
	if mode == ConsoleHTML5 && fam != "aten" {
		return Launch{}, fmt.Errorf("the HTML5 console mode is only implemented for the ATEN / Supermicro family; set this profile's console mode to Auto")
	}
	Report(ctx, StageLoggingIn, nil)
	if fam == "aten" {
		return w.atenLogin(ctx, password, mode)
	}
	b, e := w.Request(ctx, "POST", "/rpc/WEBSES/create.asp", url.Values{"WEBVAR_USERNAME": {w.Profile.Username}, "WEBVAR_PASSWORD": {password}}, false)
	if e != nil {
		return Launch{}, e
	}
	if m := statusRE.FindSubmatch(b); m != nil && string(m[1]) != "0" {
		return Launch{}, fmt.Errorf("BMC rejected login (HAPI_STATUS=%s)", m[1])
	}
	m := cookieRE.FindSubmatch(b)
	if m == nil || strings.Contains(strings.ToLower(string(m[1])), "fail") {
		return Launch{}, fmt.Errorf("BMC did not issue a valid login session")
	}
	w.Cookie = string(m[1])
	u, _ := url.Parse(w.Base)
	w.Client.Jar.SetCookies(u, []*http.Cookie{{Name: "SessionCookie", Value: w.Cookie, Path: "/"}, {Name: "Username", Value: w.Profile.Username, Path: "/"}})
	Report(ctx, StageDiscovering, nil)
	path := w.Profile.JNLPPath
	if path == "" {
		path = "/Java/jviewer.jnlp"
	}
	b, e = w.Request(ctx, "GET", path, nil, true)
	if e != nil {
		w.Close()
		return Launch{}, e
	}
	if !bytes.Contains(bytes.ToLower(b), []byte("<jnlp")) {
		w.Close()
		return Launch{}, fmt.Errorf("BMC returned no JNLP (login was not accepted or the session expired; check the account and password)")
	}
	doc, e := transport.ParseJNLP(b)
	var l Launch
	switch {
	case e != nil:
	case atenMain(doc.Main):
		e = fmt.Errorf("this BMC needs the ATEN family; select it for this profile")
	default:
		l, e = jnlpLaunch(doc)
	}
	if e != nil {
		w.Close()
		return l, e
	}
	l.Cookie = w.Cookie
	return l, nil
}

// ---- ATEN / Supermicro ----
//
// Generations and their web paths, as far as sources document them:
//
//   - X9/X10/X11 ATEN firmware: POST /cgi/login.cgi (name, pwd) sets the SID
//     cookie; GET /cgi/url_redirect.cgi?url_name=ikvm&url_type=jwsk returns the
//     Java iKVM JNLP (ATENtion BmcArmingClient; supermicro-java-console gists).
//     Some builds use url_name=man_ikvm (osso.nl, 2020).
//   - Later X10/X11 web UIs base64-encode name and pwd in the browser and add
//     check=00 (osso.nl 2020; observed in a live BMC's login page). The form is
//     detected from the unauthenticated login page, so only one login attempt
//     is ever sent.
//   - Firmware without Java iKVM serves the HTML5 console at
//     /cgi/url_redirect.cgi?url_name=man_ikvm_html5_bootstrap (verified live).
//   - Redfish: GET /redfish/v1/Managers/1/IKVM (X11 Redfish Reference Guide
//     2.0) or /redfish/v1/Managers/1/Oem/Supermicro/IKVM (Redfish User Guide
//     4.0, X12/X13) returns "URI": "/redfish/<token>.IKVM", the HTML5 console
//     page. The path token is its only authenticator and lives as long as the
//     Redfish session (runZero, CVE-2026-18451). Whether that page and its
//     WebSocket match the X10/X11 HTML5 console is NOT verified.
//   - ATENtion reports that firmware answering login.cgi with HTTP 400 needs
//     the Redfish session path.
//
// OldIPMI never posts /cgi/op.cgi (the CSRF-protected settings endpoint), so it
// never switches the BMC between the Java and HTML5 console.

const (
	atenJNLPPath      = "/cgi/url_redirect.cgi?url_name=ikvm&url_type=jwsk"
	atenAltJNLPPath   = "/cgi/url_redirect.cgi?url_name=man_ikvm&url_type=jwsk"
	atenHTML5Path     = "/cgi/url_redirect.cgi?url_name=man_ikvm_html5_bootstrap"
	atenNoVNCRFB      = "/novnc/include/rfb.js"
	redfishSessions   = "/redfish/v1/SessionService/Sessions"
	redfishIKVMOem    = "/redfish/v1/Managers/1/Oem/Supermicro/IKVM"
	redfishIKVMLegacy = "/redfish/v1/Managers/1/IKVM"
)

var (
	atenBase64FormRE = regexp.MustCompile(`pwd\.value\s*=\s*btoa\s*\(`)
	atenCheckRE      = regexp.MustCompile(`name=["']?check["']?[^>]*value=["']?([0-9A-Za-z]{1,8})`)
)

// atenLoginForm builds the login form the BMC's own login page would submit.
func (w *Web) atenLoginForm(ctx context.Context, password string) url.Values {
	form := url.Values{"name": {w.Profile.Username}, "pwd": {password}}
	page, e := w.Request(ctx, "GET", "/", nil, true)
	if e != nil || !atenBase64FormRE.Match(page) {
		return form // classic X9-era form: plain fields
	}
	check := "00"
	if m := atenCheckRE.FindSubmatch(page); m != nil {
		check = string(m[1])
	}
	enc := base64.StdEncoding.EncodeToString
	return url.Values{"name": {enc([]byte(w.Profile.Username))}, "pwd": {enc([]byte(password))}, "check": {check}}
}

func (w *Web) hasSession() bool {
	u, _ := url.Parse(w.Base)
	return len(w.Client.Jar.Cookies(u)) > 0 // the jar drops empty "deleted" cookies
}

// javaUnavailable records why the Java iKVM could not be obtained.
type javaUnavailable struct{ html bool }

func (e javaUnavailable) Error() string {
	if e.html {
		return "the BMC answered the Java iKVM request with a web page instead of a JNLP (Java iKVM is disabled in its Remote Control settings, or the web session was not accepted)"
	}
	return "this firmware has no Java iKVM (the JNLP URL answers 404)"
}

func (w *Web) atenLogin(ctx context.Context, password string, mode string) (l Launch, err error) {
	defer func() {
		if err != nil {
			w.Close()
		}
	}()
	form := w.atenLoginForm(ctx, password)
	w.cgi = true
	_, err = w.Request(ctx, "POST", "/cgi/login.cgi", form, false)
	switch code := transport.HTTPStatus(err); {
	case code == 400 || code == 404 || code == 405:
		w.cgi = false
		return w.redfishLaunch(ctx, password, true)
	case err != nil:
		return Launch{}, fmt.Errorf("the BMC web login (/cgi/login.cgi) failed: %v; check the hostname and this profile's HTTP / legacy TLS options", err)
	}
	if !w.hasSession() {
		hint := ""
		if w.Profile.HTTP {
			hint = "; if the credentials are correct, disable HTTP login for this profile (the BMC may issue its session cookie only over HTTPS)"
		}
		return Launch{}, fmt.Errorf("the BMC did not open a web session: the account or password was rejected, the account lacks web access, or it is locked after failed attempts; check the credentials in the BMC web interface%s", hint)
	}
	Report(ctx, StageDiscovering, nil)
	if mode == ConsoleHTML5 {
		return w.atenHTML5(ctx, password)
	}
	l, err = w.atenJNLP(ctx)
	var ju javaUnavailable
	if err == nil || !errors.As(err, &ju) {
		return l, err
	}
	if mode == ConsoleJava {
		return Launch{}, fmt.Errorf("%v; set this profile's console mode to Auto or HTML5, or enable the Java plug-in in the BMC web interface", ju)
	}
	if w.Profile.JNLPPath != "" {
		return Launch{}, fmt.Errorf("%v at this profile's custom JNLP path; clear the custom JNLP path, or set the console mode to HTML5", ju)
	}
	// Firmware without (or with disabled) Java iKVM: the web session must stay
	// logged in because it authorizes the HTML5 WebSocket.
	l, err = w.atenHTML5(ctx, password)
	if err != nil && ju.html {
		return l, fmt.Errorf("%v; the HTML5 console was also unavailable: %v", ju, err)
	}
	return l, err
}

// atenJNLP fetches and parses the Java iKVM launch file.
func (w *Web) atenJNLP(ctx context.Context) (Launch, error) {
	paths := []string{atenJNLPPath, atenAltJNLPPath}
	if w.Profile.JNLPPath != "" {
		paths = []string{w.Profile.JNLPPath}
	}
	var missing javaUnavailable
	for _, path := range paths {
		b, e := w.Request(ctx, "GET", path, nil, true)
		if transport.HTTPStatus(e) == 404 {
			continue
		}
		if e != nil {
			return Launch{}, fmt.Errorf("the Java iKVM request failed: %v; retry, or set this profile's console mode to HTML5", e)
		}
		if !bytes.Contains(bytes.ToLower(b), []byte("<jnlp")) {
			missing.html = true
			continue
		}
		doc, e := transport.ParseJNLP(b)
		var l Launch
		switch {
		case e == nil && !atenMain(doc.Main):
			return Launch{}, fmt.Errorf("the BMC served a non-ATEN JNLP; select the AMI family for this profile")
		case e == nil:
			l, e = jnlpLaunch(doc)
		}
		if e != nil {
			return l, fmt.Errorf("%v; set this profile's console mode to HTML5 if the BMC offers it, and report the BMC firmware version", e)
		}
		l.Cookie = w.Cookie
		return l, nil
	}
	return Launch{}, missing
}

var html5TokenRE = regexp.MustCompile(`id="entry_value"\s+value="([^"]{1,24})"`)

// atenHTML5 reads the iKVM session token from the HTML5 console page. Firmware
// without that page is tried through Redfish (X11 and later).
func (w *Web) atenHTML5(ctx context.Context, password string) (Launch, error) {
	b, e := w.Request(ctx, "GET", atenHTML5Path, nil, true)
	if transport.HTTPStatus(e) == 404 {
		l, e2 := w.redfishLaunch(ctx, password, false)
		if e2 != nil {
			return l, fmt.Errorf("this firmware's web interface offers no HTML5 console page (404), and Redfish discovery failed: %w", e2)
		}
		return l, nil
	}
	if e != nil {
		return Launch{}, fmt.Errorf("the HTML5 console page request failed: %v; retry, or check the BMC web interface", e)
	}
	l, ok := w.html5FromPage(b)
	if !ok {
		return Launch{}, fmt.Errorf("the HTML5 console page has no session token: the account may lack Remote Console rights, another login may have ended the session, or this firmware's console page is unsupported; check the account in the BMC web interface and retry")
	}
	l.ClientType = w.html5ClientType(ctx)
	return l, nil
}

// html5ClientType reports whether the BMC's own noVNC client sends the
// client-type byte after the token (seen on X12/X13 firmware; X10/X11 send
// only the 48-byte token). The handshake is otherwise identical, so the BMC's
// client is the only reliable witness. If it cannot be read, the X10/X11 form
// is used, as before.
func (w *Web) html5ClientType(ctx context.Context) bool {
	b, e := w.Request(ctx, "GET", atenNoVNCRFB, nil, true)
	return e == nil && bytes.Contains(b, []byte("clientType"))
}

func (w *Web) html5FromPage(b []byte) (Launch, bool) {
	m := html5TokenRE.FindSubmatch(b)
	if m == nil {
		return Launch{}, false
	}
	u, _ := url.Parse(w.Base)
	var cookie []string
	for _, c := range w.Client.Jar.Cookies(u) {
		cookie = append(cookie, c.Name+"="+c.Value)
	}
	port := 443
	if w.Profile.HTTP {
		port = 80
	}
	return Launch{Family: "aten", HTML5: true, Token: string(m[1]), Cookie: strings.Join(cookie, "; "), Port: port, Secure: !w.Profile.HTTP, Args: map[string]string{}}, true
}

// redfishLaunch opens a Redfish session and follows the Supermicro iKVM
// resource to the HTML5 console page. login is true when /cgi/login.cgi was
// unavailable, so this is the first credential check.
func (w *Web) redfishLaunch(ctx context.Context, password string, login bool) (Launch, error) {
	body, _ := json.Marshal(map[string]string{"UserName": w.Profile.Username, "Password": password})
	res, e := w.Do(ctx, "POST", redfishSessions, body, "application/json", false)
	switch {
	case e != nil:
		return Launch{}, fmt.Errorf("the Redfish login failed: %v; check the hostname and this profile's HTTP / legacy TLS options", e)
	case res.Status == 401 || res.Status == 403:
		return Launch{}, fmt.Errorf("the BMC rejected the account or password (Redfish HTTP %d); check the credentials and that the account may use Redfish", res.Status)
	case res.Status == 404 || res.Status == 405:
		if login {
			return Launch{}, fmt.Errorf("the BMC offers neither the ATEN web login (/cgi/login.cgi) nor Redfish sessions; this is not a supported Supermicro firmware; check the selected BMC family")
		}
		return Launch{}, fmt.Errorf("this firmware has no Redfish session service; update the BMC firmware or use its web interface")
	case res.Status != 200 && res.Status != 201:
		return Launch{}, fmt.Errorf("the Redfish login answered HTTP %d; retry, or check the BMC web interface", res.Status)
	}
	token := res.Header.Get("X-Auth-Token")
	if token == "" || len(token) > 256 || strings.ContainsAny(token, "\r\n") {
		return Launch{}, fmt.Errorf("the Redfish login returned no session token; report the BMC firmware version")
	}
	w.RedfishToken = token
	if loc, ok := w.SameHostPath(res.Header.Get("Location")); ok && strings.HasPrefix(loc, redfishSessions+"/") {
		w.redfishPath = loc
	}
	if login {
		Report(ctx, StageDiscovering, nil)
	}
	var uri, gen string
	for _, path := range []string{redfishIKVMOem, redfishIKVMLegacy} {
		r, e := w.Do(ctx, "GET", path, nil, "", false)
		if e != nil {
			return Launch{}, fmt.Errorf("the Redfish iKVM request failed: %v; retry", e)
		}
		if r.Status == 404 {
			continue
		}
		if r.Status != 200 {
			return Launch{}, fmt.Errorf("the Redfish iKVM resource answered HTTP %d; check that the account has Remote Console rights", r.Status)
		}
		var doc struct{ URI string }
		if json.Unmarshal(r.Body, &doc) != nil || doc.URI == "" {
			return Launch{}, fmt.Errorf("the Redfish iKVM resource has no console URI; report the BMC firmware version")
		}
		uri, gen = doc.URI, "Supermicro X12/X13 (Redfish OEM iKVM)"
		if path == redfishIKVMLegacy {
			gen = "Supermicro X10/X11 (Redfish iKVM)"
		}
		break
	}
	if uri == "" {
		return Launch{}, fmt.Errorf("this firmware's Redfish service has no iKVM resource; open the console from the BMC web interface or update the BMC firmware")
	}
	path, ok := w.SameHostPath(uri)
	if !ok || len(path) > 512 {
		return Launch{}, fmt.Errorf("the Redfish iKVM URI points outside this BMC; refusing it")
	}
	page, e := w.Request(ctx, "GET", path, nil, true)
	if e != nil {
		return Launch{}, fmt.Errorf("the %s console page request failed: %v; retry, the console URI expires with the session", gen, e)
	}
	l, found := w.html5FromPage(page)
	if !found {
		// The page format of the AST2600-generation console is not documented.
		return Launch{}, fmt.Errorf("found the %s HTML5 console, but its page format is not supported yet; use the BMC web interface's Remote Console and report the BMC model and firmware version", gen)
	}
	l.Redfish = true
	l.ClientType = w.html5ClientType(ctx)
	return l, nil
}

// atenArgOffset skips a leading codebase URL and jar/native-library names.
// Some ATEN JNLPs (iKVM 1.69.39, osso.nl 2020) pass those as arguments 0-9 and
// the connection arguments from index 10.
func atenArgOffset(a []string) int {
	n := 0
	for n < len(a) {
		s := strings.ToLower(strings.TrimSpace(a[n]))
		if !strings.Contains(s, "://") && !strings.HasSuffix(s, ".jar") {
			break
		}
		n++
	}
	return n
}

// atenMain reports whether a JNLP starts ATEN's iKVM client.
func atenMain(main string) bool { return strings.Contains(main, "aten") }

func ParseJNLP(b []byte) (Launch, error) {
	doc, e := transport.ParseJNLP(b)
	if e != nil {
		return Launch{}, e
	}
	return jnlpLaunch(doc)
}

// jnlpLaunch reads the console's address and credentials from the arguments
// of a JNLP, in the layout of its family.
func jnlpLaunch(doc transport.JNLP) (Launch, error) {
	a := doc.Args
	l := Launch{Family: "ami", Args: map[string]string{}, Pos: a}
	number := func(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
	if atenMain(doc.Main) {
		// KVMMain arguments (ATENtion; drac-kvm iKVM version table):
		// 0 host, 1 user/token, 2 password/token, 3 host name, 4 KVM port
		// (stunnel-local when tunnelled), 5 media port, 6 company, 7 board,
		// 8 TLS tunnel flag, 9 TLS KVM port, [10 remote media port, 11 media service].
		// iKVM 1.69.21 has only 0-7: plain RFB on argument 4.
		a = a[atenArgOffset(a):]
		if len(a) < 8 {
			return l, fmt.Errorf("unsupported ATEN JNLP layout (%d connection arguments)", len(a))
		}
		l.Family = "aten"
		l.Token = strings.TrimSpace(a[1])
		if p := strings.TrimSpace(a[2]); p != l.Token {
			l.PasswordToken = p
		}
		l.Port = number(a[4])
		l.CDPort = number(a[5])
		if len(a) >= 10 {
			l.Secure = strings.TrimSpace(a[8]) != "0"
			if l.Secure {
				l.Port = number(a[9])
				// With the tunnel, argument 5 is a local stunnel port; the
				// longer layout names the BMC's media port separately.
				if len(a) >= 11 && number(a[10]) > 0 {
					l.CDPort = number(a[10])
				}
			}
		}
		if len(l.PasswordToken) > 128 {
			return l, fmt.Errorf("JNLP has invalid connection parameters")
		}
	} else if len(a) > 0 && strings.HasPrefix(a[0], "-") {
		if len(a)%2 != 0 {
			return l, fmt.Errorf("incomplete AMI JNLP argument pairs")
		}
		for i := 0; i < len(a); i += 2 {
			l.Args[strings.TrimPrefix(a[i], "-")] = a[i+1]
		}
		l.Token = l.Args["kvmtoken"]
		l.Port = number(l.Args["kvmport"])
		l.CDPort = number(l.Args["cdport"])
		l.Secure = l.Args["kvmsecure"] == "1"
		l.MediaSecure = l.Args["vmsecure"] == "1"
	} else {
		if len(a) < 10 {
			return l, fmt.Errorf("unsupported AMI JNLP arguments")
		}
		l.Legacy = true
		l.Token = a[2]
		l.Port = number(a[1])
		l.Secure = a[3] == "1"
		l.MediaSecure = a[4] == "1"
		l.CDPort = number(a[5])
		l.HIDPort = number(a[8])
	}
	if l.Token == "" || len(l.Token) > 128 || l.Port < 1 || l.Port > 65535 {
		return l, fmt.Errorf("JNLP has invalid connection parameters")
	}
	return l, nil
}
