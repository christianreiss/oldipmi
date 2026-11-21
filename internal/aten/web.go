package aten

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"regexp"
	"strings"
)

// The web login of ATEN / Supermicro BMCs, and where it finds the console.
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
	jnlpPath          = "/cgi/url_redirect.cgi?url_name=ikvm&url_type=jwsk"
	altJNLPPath       = "/cgi/url_redirect.cgi?url_name=man_ikvm&url_type=jwsk"
	html5Path         = "/cgi/url_redirect.cgi?url_name=man_ikvm_html5_bootstrap"
	noVNCRFB          = "/novnc/include/rfb.js"
	redfishSessions   = "/redfish/v1/SessionService/Sessions"
	redfishIKVMOem    = "/redfish/v1/Managers/1/Oem/Supermicro/IKVM"
	redfishIKVMLegacy = "/redfish/v1/Managers/1/IKVM"
)

var (
	base64FormRE = regexp.MustCompile(`pwd\.value\s*=\s*btoa\s*\(`)
	checkRE      = regexp.MustCompile(`name=["']?check["']?[^>]*value=["']?([0-9A-Za-z]{1,8})`)
	html5TokenRE = regexp.MustCompile(`id="entry_value"\s+value="([^"]{1,24})"`)
)

// web is a session with the BMC's web interface.
type web struct {
	*transport.Web
	p           protocol.Profile
	cgi         bool   // a /cgi/login.cgi session may exist
	redfishPath string // the Redfish session to DELETE on Close
}

func newWeb(p protocol.Profile) (*web, error) {
	tw, err := transport.NewWeb(p.Policy())
	if err != nil {
		return nil, err
	}
	w := &web{Web: tw, p: p}
	// Closing logs out of the sessions this client opened: the Redfish
	// session first (Close runs the last added first), then the web login.
	w.OnClose(func(ctx context.Context) {
		if w.cgi {
			w.Request(ctx, "GET", "/cgi/logout.cgi", nil, false)
		}
	})
	w.OnClose(func(ctx context.Context) {
		if w.redfishPath != "" {
			w.Do(ctx, "DELETE", w.redfishPath, nil, "", false)
		}
	})
	return w, nil
}

// login logs in and finds the console the profile's console mode asks for.
// It reports the logging_in and discovering stages; when it fails, it has
// logged out again.
func (w *web) login(ctx context.Context, password string) (l Launch, err error) {
	mode, err := w.p.ConsoleModeValue()
	if err != nil {
		return Launch{}, err
	}
	protocol.Report(ctx, protocol.StageLoggingIn, nil)
	defer func() {
		if err != nil {
			w.Close()
		}
	}()
	form := w.loginForm(ctx, password)
	w.cgi = true
	_, err = w.Request(ctx, "POST", "/cgi/login.cgi", form, false)
	switch code := transport.HTTPStatus(err); {
	case code == 400 || code == 404 || code == 405:
		w.cgi = false
		return w.redfishLaunch(ctx, password, true)
	case err != nil:
		return Launch{}, fmt.Errorf("the BMC web login (/cgi/login.cgi) failed: %w; check the hostname and this profile's HTTP / legacy TLS options", err)
	}
	if !w.hasSession() {
		hint := ""
		if w.p.HTTP {
			hint = "; if the credentials are correct, disable HTTP login for this profile (the BMC may issue its session cookie only over HTTPS)"
		}
		return Launch{}, protocol.Mark(protocol.CodeAuth, fmt.Errorf("the BMC did not open a web session: the account or password was rejected, the account lacks web access, or it is locked after failed attempts; check the credentials in the BMC web interface%s", hint))
	}
	protocol.Report(ctx, protocol.StageDiscovering, nil)
	if mode == protocol.ConsoleHTML5 {
		return w.html5(ctx, password)
	}
	l, err = w.jnlp(ctx)
	var ju javaUnavailable
	if err == nil || !errors.As(err, &ju) {
		return l, err
	}
	if mode == protocol.ConsoleJava {
		return Launch{}, fmt.Errorf("%w; set this profile's console mode to Auto or HTML5, or enable the Java plug-in in the BMC web interface", ju)
	}
	if w.p.JNLPPath != "" {
		return Launch{}, fmt.Errorf("%w at this profile's custom JNLP path; clear the custom JNLP path, or set the console mode to HTML5", ju)
	}
	// Firmware without (or with disabled) Java iKVM: the web session must stay
	// logged in because it authorizes the HTML5 WebSocket.
	l, err = w.html5(ctx, password)
	if err != nil && ju.html {
		return l, fmt.Errorf("%w; the HTML5 console was also unavailable: %w", ju, err)
	}
	return l, err
}

// loginForm builds the login form the BMC's own login page would submit.
func (w *web) loginForm(ctx context.Context, password string) url.Values {
	form := url.Values{"name": {w.p.Username}, "pwd": {password}}
	page, e := w.Request(ctx, "GET", "/", nil, true)
	if e != nil || !base64FormRE.Match(page) {
		return form // classic X9-era form: plain fields
	}
	check := "00"
	if m := checkRE.FindSubmatch(page); m != nil {
		check = string(m[1])
	}
	enc := base64.StdEncoding.EncodeToString
	return url.Values{"name": {enc([]byte(w.p.Username))}, "pwd": {enc([]byte(password))}, "check": {check}}
}

func (w *web) hasSession() bool {
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

// jnlp fetches and reads the Java iKVM's JNLP.
func (w *web) jnlp(ctx context.Context) (Launch, error) {
	paths := []string{jnlpPath, altJNLPPath}
	if w.p.JNLPPath != "" {
		paths = []string{w.p.JNLPPath}
	}
	var missing javaUnavailable
	for _, path := range paths {
		b, e := w.Request(ctx, "GET", path, nil, true)
		if transport.HTTPStatus(e) == 404 {
			continue
		}
		if e != nil {
			return Launch{}, fmt.Errorf("the Java iKVM request failed: %w; retry, or set this profile's console mode to HTML5", e)
		}
		if !bytes.Contains(bytes.ToLower(b), []byte("<jnlp")) {
			missing.html = true
			continue
		}
		l, e := parseJNLP(b)
		if errors.Is(e, errNotATEN) {
			return Launch{}, e
		}
		if e != nil {
			return l, fmt.Errorf("%w; set this profile's console mode to HTML5 if the BMC offers it, and report the BMC firmware version", e)
		}
		return l, nil
	}
	return Launch{}, missing
}

// html5 reads the iKVM session token from the HTML5 console page. Firmware
// without that page is tried through Redfish (X11 and later).
func (w *web) html5(ctx context.Context, password string) (Launch, error) {
	b, e := w.Request(ctx, "GET", html5Path, nil, true)
	if transport.HTTPStatus(e) == 404 {
		l, e2 := w.redfishLaunch(ctx, password, false)
		if e2 != nil {
			return l, fmt.Errorf("this firmware's web interface offers no HTML5 console page (404), and Redfish discovery failed: %w", e2)
		}
		return l, nil
	}
	if e != nil {
		return Launch{}, fmt.Errorf("the HTML5 console page request failed: %w; retry, or check the BMC web interface", e)
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
func (w *web) html5ClientType(ctx context.Context) bool {
	b, e := w.Request(ctx, "GET", noVNCRFB, nil, true)
	return e == nil && bytes.Contains(b, []byte("clientType"))
}

func (w *web) html5FromPage(b []byte) (Launch, bool) {
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
	if w.p.HTTP {
		port = 80
	}
	return Launch{HTML5: true, Token: string(m[1]), Cookie: strings.Join(cookie, "; "), Port: port, Secure: !w.p.HTTP}, true
}

// redfishLaunch opens a Redfish session and follows the Supermicro iKVM
// resource to the HTML5 console page. login is true when /cgi/login.cgi was
// unavailable, so this is the first credential check.
func (w *web) redfishLaunch(ctx context.Context, password string, login bool) (Launch, error) {
	body, _ := json.Marshal(map[string]string{"UserName": w.p.Username, "Password": password})
	res, e := w.Do(ctx, "POST", redfishSessions, body, "application/json", false)
	switch {
	case e != nil:
		return Launch{}, fmt.Errorf("the Redfish login failed: %w; check the hostname and this profile's HTTP / legacy TLS options", e)
	case res.Status == 401 || res.Status == 403:
		return Launch{}, protocol.Mark(protocol.CodeAuth, fmt.Errorf("the BMC rejected the account or password (Redfish HTTP %d); check the credentials and that the account may use Redfish", res.Status))
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
		protocol.Report(ctx, protocol.StageDiscovering, nil)
	}
	var uri, gen string
	for _, path := range []string{redfishIKVMOem, redfishIKVMLegacy} {
		r, e := w.Do(ctx, "GET", path, nil, "", false)
		if e != nil {
			return Launch{}, fmt.Errorf("the Redfish iKVM request failed: %w; retry", e)
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
		return Launch{}, fmt.Errorf("the %s console page request failed: %w; retry, the console URI expires with the session", gen, e)
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
