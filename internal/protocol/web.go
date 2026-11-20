package protocol

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"oldipmi/internal/transport"
	"regexp"
	"strconv"
	"strings"
)

// Launch is how to reach an AMI console, as the web login found it.
type Launch struct {
	Family string
	Args   map[string]string
	Pos    []string
	// Token is the console credential, Cookie the web session's.
	Token, Cookie               string
	Port, HIDPort, CDPort       int
	Secure, MediaSecure, Legacy bool
}

// Web is a web session with the login of the AMI family.
type Web struct {
	*transport.Web
	Profile Profile
	Cookie  string
}

func NewWeb(p Profile) (*Web, error) {
	tw, err := transport.NewWeb(p.Policy())
	if err != nil {
		return nil, err
	}
	w := &Web{Web: tw, Profile: p}
	// Closing logs out of the web session this client opened.
	w.OnClose(func(ctx context.Context) {
		if w.Cookie != "" {
			w.Request(ctx, "GET", "/rpc/WEBSES/logout.asp", nil, false)
		}
	})
	return w, nil
}

var cookieRE = regexp.MustCompile(`['"]?SESSION_COOKIE['"]?\s*:\s*['"]([^'"\r\n]+)['"]`)
var statusRE = regexp.MustCompile(`['"]?HAPI_STATUS['"]?\s*:\s*(-?\d+)`)

func (w *Web) Login(ctx context.Context, password string) (Launch, error) {
	mode, e := w.Profile.ConsoleModeValue()
	if e != nil {
		return Launch{}, e
	}
	if mode == ConsoleHTML5 {
		return Launch{}, fmt.Errorf("the HTML5 console mode is only implemented for the ATEN / Supermicro family; set this profile's console mode to Auto")
	}
	Report(ctx, StageLoggingIn, nil)
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
	l, e := ParseJNLP(b)
	if e != nil {
		w.Close()
		return l, e
	}
	l.Cookie = w.Cookie
	return l, nil
}

// ParseJNLP reads the console's address and credentials from an AMI JNLP. An
// ATEN JNLP gets the hint to select that family.
func ParseJNLP(b []byte) (Launch, error) {
	doc, e := transport.ParseJNLP(b)
	if e != nil {
		return Launch{}, e
	}
	if strings.Contains(doc.Main, "aten") {
		return Launch{}, fmt.Errorf("this BMC needs the ATEN family; select it for this profile")
	}
	a := doc.Args
	l := Launch{Family: "ami", Args: map[string]string{}, Pos: a}
	number := func(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
	if len(a) > 0 && strings.HasPrefix(a[0], "-") {
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
