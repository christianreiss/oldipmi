package protocol

import (
	"context"
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
	"time"
)

type Launch struct {
	Family                      string
	Args                        map[string]string
	Pos                         []string
	Token, Cookie               string
	Port, HIDPort, CDPort       int
	Secure, MediaSecure, Legacy bool
}
type Web struct {
	Client  *http.Client
	Base    string
	Profile Profile
	Cookie  string
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
	c := &http.Client{Timeout: 20 * time.Second, Jar: jar, Transport: &http.Transport{TLSClientConfig: TLSConfig(p), DisableKeepAlives: true}}
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || r.URL.Hostname() != p.Host || r.URL.Scheme != scheme {
			return fmt.Errorf("BMC redirect changes host or transport; select the correct transport")
		}
		return nil
	}
	return &Web{Client: c, Base: u.String(), Profile: p}, nil
}
func (w *Web) Request(ctx context.Context, method, path string, form url.Values, tolerant bool) ([]byte, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return nil, fmt.Errorf("invalid BMC endpoint")
	}
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r, e := http.NewRequestWithContext(ctx, method, w.Base+path, body)
	if e != nil {
		return nil, fmt.Errorf("invalid BMC request")
	}
	r.Header.Set("Referer", w.Base+"/")
	r.Header.Set("User-Agent", "OldIPMI/0.1")
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	res, e := w.Client.Do(r)
	if e != nil {
		return nil, fmt.Errorf("BMC HTTP transport failed: %w", e)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("BMC HTTP status %d", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if len(b) > 1<<20 {
		return nil, fmt.Errorf("BMC response too large")
	}
	if e != nil && !(tolerant && errors.Is(e, io.ErrUnexpectedEOF)) {
		return nil, fmt.Errorf("incomplete BMC response")
	}
	return b, nil
}

var cookieRE = regexp.MustCompile(`['"]?SESSION_COOKIE['"]?\s*:\s*['"]([^'"\r\n]+)['"]`)
var statusRE = regexp.MustCompile(`['"]?HAPI_STATUS['"]?\s*:\s*(-?\d+)`)

func (w *Web) Login(ctx context.Context, password string) (Launch, error) {
	fam := w.Profile.Family
	if fam == "" {
		fam = "ami"
	}
	endpoint := "/rpc/WEBSES/create.asp"
	form := url.Values{"WEBVAR_USERNAME": {w.Profile.Username}, "WEBVAR_PASSWORD": {password}}
	if fam == "aten" {
		endpoint = "/cgi/login.cgi"
		form = url.Values{"name": {w.Profile.Username}, "pwd": {password}}
	}
	b, e := w.Request(ctx, "POST", endpoint, form, false)
	if e != nil {
		return Launch{}, e
	}
	if fam == "ami" {
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
	}
	path := w.Profile.JNLPPath
	if path == "" {
		if fam == "aten" {
			path = "/cgi/url_redirect.cgi?url_name=ikvm&url_type=jwsk"
		} else {
			path = "/Java/jviewer.jnlp"
		}
	}
	b, e = w.Request(ctx, "GET", path, nil, true)
	if e != nil {
		w.Close()
		return Launch{}, e
	}
	l, e := ParseJNLP(b)
	if e != nil {
		w.Close()
		return l, e
	}
	l.Cookie = w.Cookie
	return l, nil
}
func (w *Web) Close() {
	ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
	defer c()
	path := "/rpc/WEBSES/logout.asp"
	if w.Profile.Family == "aten" {
		path = "/cgi/logout.cgi"
	}
	if w.Cookie != "" || w.Profile.Family == "aten" {
		w.Request(ctx, "GET", path, nil, false)
	}
	w.Client.CloseIdleConnections()
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
	number := func(s string) int { n, _ := strconv.Atoi(s); return n }
	if strings.Contains(doc.App.Main, "aten") {
		if len(a) < 10 {
			return l, fmt.Errorf("unsupported ATEN JNLP arguments")
		}
		l.Family = "aten"
		l.Token = a[1]
		l.Secure = a[8] != "0"
		l.Port = number(a[4])
		if l.Secure {
			l.Port = number(a[9])
		}
		l.CDPort = number(a[5])
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
