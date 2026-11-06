package protocol

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
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
}
type Web struct {
	Client  *http.Client
	Base    string
	Profile Profile
	Cookie  string

	cgi         bool   // an ATEN /cgi/login.cgi session may exist
	authToken   string // Redfish X-Auth-Token
	redfishPath string // Redfish session resource to DELETE on Close
	closeOnce   sync.Once
}

// StatusError is a BMC HTTP answer other than the expected success status.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("BMC HTTP status %d", e.Code) }

func httpStatus(err error) int {
	var s *StatusError
	if errors.As(err, &s) {
		return s.Code
	}
	return 0
}

// cleanJar ignores empty-valued cookies. ATEN firmware "deletes" SID with an
// unparsable expiry date; Go would keep the empty cookie (at a narrower path) and
// send it ahead of the real session cookie.
type cleanJar struct{ http.CookieJar }

func (j cleanJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	keep := cookies[:0:0]
	for _, c := range cookies {
		if c.Value != "" {
			keep = append(keep, c)
		}
	}
	j.CookieJar.SetCookies(u, keep)
}

func NewWeb(p Profile) (*Web, error) {
	if p.Host == "" || strings.ContainsAny(p.Host, "/\\@?#\x00\r\n ") {
		return nil, fmt.Errorf("enter a BMC hostname or IP address without a URL")
	}
	scheme := "https"
	if p.HTTP {
		scheme = "http"
	}
	u := &url.URL{Scheme: scheme, Host: p.Host}
	if strings.Contains(p.Host, ":") {
		u.Host = "[" + p.Host + "]"
	}
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Timeout: 20 * time.Second, Jar: cleanJar{jar}, Transport: &http.Transport{TLSClientConfig: TLSConfig(p), DisableKeepAlives: true}}
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || r.URL.Hostname() != p.Host || r.URL.Scheme != scheme {
			return fmt.Errorf("BMC redirect changes host or transport; select the correct transport")
		}
		return nil
	}
	return &Web{Client: c, Base: u.String(), Profile: p}, nil
}

type webResponse struct {
	status int
	header http.Header
	body   []byte
}

// do sends one request and returns any HTTP status; only transport, size and
// truncation problems are errors.
func (w *Web) do(ctx context.Context, method, path string, body []byte, contentType string, tolerant bool) (webResponse, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return webResponse{}, fmt.Errorf("invalid BMC endpoint")
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	r, e := http.NewRequestWithContext(ctx, method, w.Base+path, rd)
	if e != nil {
		return webResponse{}, fmt.Errorf("invalid BMC request")
	}
	// Some ATEN web servers answer 500 without a Referer.
	r.Header.Set("Referer", w.Base+"/")
	r.Header.Set("User-Agent", "OldIPMI/0.2")
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if w.authToken != "" && strings.HasPrefix(path, "/redfish/") {
		r.Header.Set("X-Auth-Token", w.authToken)
	}
	res, e := w.Client.Do(r)
	if e != nil {
		return webResponse{}, fmt.Errorf("BMC HTTP transport failed: %w", e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if len(b) > 1<<20 {
		return webResponse{}, fmt.Errorf("BMC response too large")
	}
	if e != nil && !(tolerant && errors.Is(e, io.ErrUnexpectedEOF)) {
		return webResponse{}, fmt.Errorf("incomplete BMC response")
	}
	return webResponse{status: res.StatusCode, header: res.Header, body: b}, nil
}

// Request performs a form or plain request that must answer HTTP 200; any
// other status is a *StatusError.
func (w *Web) Request(ctx context.Context, method, path string, form url.Values, tolerant bool) ([]byte, error) {
	var body []byte
	ctype := ""
	if form != nil {
		body, ctype = []byte(form.Encode()), "application/x-www-form-urlencoded"
	}
	res, e := w.do(ctx, method, path, body, ctype, tolerant)
	if e != nil {
		return nil, e
	}
	if res.status != 200 {
		return nil, &StatusError{res.status}
	}
	return res.body, nil
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
	Report(ctx, "logging_in", nil)
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
	Report(ctx, "discovering", nil)
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
	l, e := ParseJNLP(b)
	if e != nil {
		w.Close()
		return l, e
	}
	l.Cookie = w.Cookie
	return l, nil
}

// Close ends the web sessions this client opened. It is safe to call twice.
func (w *Web) Close() {
	w.closeOnce.Do(func() {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		if w.redfishPath != "" {
			w.do(ctx, "DELETE", w.redfishPath, nil, "", false)
		}
		if w.Profile.Family == "aten" {
			if w.cgi {
				w.Request(ctx, "GET", "/cgi/logout.cgi", nil, false)
			}
		} else if w.Cookie != "" {
			w.Request(ctx, "GET", "/rpc/WEBSES/logout.asp", nil, false)
		}
		w.authToken = ""
		w.Client.CloseIdleConnections()
	})
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
	switch code := httpStatus(err); {
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
	Report(ctx, "discovering", nil)
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
		if httpStatus(e) == 404 {
			continue
		}
		if e != nil {
			return Launch{}, fmt.Errorf("the Java iKVM request failed: %v; retry, or set this profile's console mode to HTML5", e)
		}
		if !bytes.Contains(bytes.ToLower(b), []byte("<jnlp")) {
			missing.html = true
			continue
		}
		l, e := ParseJNLP(b)
		if e != nil {
			return l, fmt.Errorf("%v; set this profile's console mode to HTML5 if the BMC offers it, and report the BMC firmware version", e)
		}
		if l.Family != "aten" {
			return l, fmt.Errorf("the BMC served a non-ATEN JNLP; select the AMI family for this profile")
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
	if httpStatus(e) == 404 {
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
	return l, nil
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
	res, e := w.do(ctx, "POST", redfishSessions, body, "application/json", false)
	switch {
	case e != nil:
		return Launch{}, fmt.Errorf("the Redfish login failed: %v; check the hostname and this profile's HTTP / legacy TLS options", e)
	case res.status == 401 || res.status == 403:
		return Launch{}, fmt.Errorf("the BMC rejected the account or password (Redfish HTTP %d); check the credentials and that the account may use Redfish", res.status)
	case res.status == 404 || res.status == 405:
		if login {
			return Launch{}, fmt.Errorf("the BMC offers neither the ATEN web login (/cgi/login.cgi) nor Redfish sessions; this is not a supported Supermicro firmware; check the selected BMC family")
		}
		return Launch{}, fmt.Errorf("this firmware has no Redfish session service; update the BMC firmware or use its web interface")
	case res.status != 200 && res.status != 201:
		return Launch{}, fmt.Errorf("the Redfish login answered HTTP %d; retry, or check the BMC web interface", res.status)
	}
	token := res.header.Get("X-Auth-Token")
	if token == "" || len(token) > 256 || strings.ContainsAny(token, "\r\n") {
		return Launch{}, fmt.Errorf("the Redfish login returned no session token; report the BMC firmware version")
	}
	w.authToken = token
	if loc, ok := w.sameHostPath(res.header.Get("Location")); ok && strings.HasPrefix(loc, redfishSessions+"/") {
		w.redfishPath = loc
	}
	if login {
		Report(ctx, "discovering", nil)
	}
	var uri, gen string
	for _, path := range []string{redfishIKVMOem, redfishIKVMLegacy} {
		r, e := w.do(ctx, "GET", path, nil, "", false)
		if e != nil {
			return Launch{}, fmt.Errorf("the Redfish iKVM request failed: %v; retry", e)
		}
		if r.status == 404 {
			continue
		}
		if r.status != 200 {
			return Launch{}, fmt.Errorf("the Redfish iKVM resource answered HTTP %d; check that the account has Remote Console rights", r.status)
		}
		var doc struct{ URI string }
		if json.Unmarshal(r.body, &doc) != nil || doc.URI == "" {
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
	path, ok := w.sameHostPath(uri)
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
	return l, nil
}

// sameHostPath accepts an absolute path or an absolute URL on this BMC.
func (w *Web) sameHostPath(ref string) (string, bool) {
	if ref == "" || strings.ContainsAny(ref, "\r\n\x00\\") {
		return "", false
	}
	u, e := url.Parse(ref)
	if e != nil {
		return "", false
	}
	if u.IsAbs() || u.Host != "" {
		base, _ := url.Parse(w.Base)
		port := func(v *url.URL) string {
			if p := v.Port(); p != "" {
				return p
			}
			if v.Scheme == "http" {
				return "80"
			}
			return "443"
		}
		if u.Scheme != base.Scheme || !strings.EqualFold(u.Hostname(), base.Hostname()) || port(u) != port(base) {
			return "", false
		}
	}
	p := u.EscapedPath()
	if u.RawQuery != "" {
		p += "?" + u.RawQuery
	}
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return "", false
	}
	return p, true
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

func ParseJNLP(b []byte) (Launch, error) {
	var doc struct {
		XMLName xml.Name `xml:"jnlp"`
		App     struct {
			Main string   `xml:"main-class,attr"`
			Args []string `xml:"argument"`
		} `xml:"application-desc"`
	}
	if len(b) > 1<<20 {
		return Launch{}, fmt.Errorf("JNLP exceeds size limit")
	}
	if e := xml.Unmarshal(b, &doc); e != nil {
		return Launch{}, fmt.Errorf("invalid or incomplete JNLP XML")
	}
	a := doc.App.Args
	l := Launch{Family: "ami", Args: map[string]string{}, Pos: a}
	number := func(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
	if strings.Contains(doc.App.Main, "aten") {
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
