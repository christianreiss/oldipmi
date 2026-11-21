package ami

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"regexp"
	"strings"
)

// web is a session with the BMC's web interface, whose MegaRAC login hands
// out the console's JNLP.
type web struct {
	*transport.Web
	p      protocol.Profile
	cookie string // the web session's, once logged in
}

func newWeb(p protocol.Profile) (*web, error) {
	tw, err := transport.NewWeb(p.Policy())
	if err != nil {
		return nil, err
	}
	w := &web{Web: tw, p: p}
	// Closing logs out of the web session this client opened.
	w.OnClose(func(ctx context.Context) {
		if w.cookie != "" {
			w.Request(ctx, "GET", "/rpc/WEBSES/logout.asp", nil, false)
		}
	})
	return w, nil
}

var cookieRE = regexp.MustCompile(`['"]?SESSION_COOKIE['"]?\s*:\s*['"]([^'"\r\n]+)['"]`)
var statusRE = regexp.MustCompile(`['"]?HAPI_STATUS['"]?\s*:\s*(-?\d+)`)

// login logs in and fetches the console's JNLP. It reports the logging_in and
// discovering stages; when the JNLP is missing or unusable, it logs out again.
func (w *web) login(ctx context.Context, password string) (Launch, error) {
	mode, e := w.p.ConsoleModeValue()
	if e != nil {
		return Launch{}, e
	}
	if mode == protocol.ConsoleHTML5 {
		return Launch{}, fmt.Errorf("the HTML5 console mode is only implemented for the ATEN / Supermicro family; set this profile's console mode to Auto")
	}
	protocol.Report(ctx, protocol.StageLoggingIn, nil)
	b, e := w.Request(ctx, "POST", "/rpc/WEBSES/create.asp", url.Values{"WEBVAR_USERNAME": {w.p.Username}, "WEBVAR_PASSWORD": {password}}, false)
	if e != nil {
		return Launch{}, e
	}
	if m := statusRE.FindSubmatch(b); m != nil && string(m[1]) != "0" {
		return Launch{}, protocol.Mark(protocol.CodeAuth, fmt.Errorf("BMC rejected login (HAPI_STATUS=%s)", m[1]))
	}
	m := cookieRE.FindSubmatch(b)
	if m == nil || strings.Contains(strings.ToLower(string(m[1])), "fail") {
		return Launch{}, fmt.Errorf("BMC did not issue a valid login session")
	}
	w.cookie = string(m[1])
	u, _ := url.Parse(w.Base)
	w.Client.Jar.SetCookies(u, []*http.Cookie{{Name: "SessionCookie", Value: w.cookie, Path: "/"}, {Name: "Username", Value: w.p.Username, Path: "/"}})
	protocol.Report(ctx, protocol.StageDiscovering, nil)
	path := w.p.JNLPPath
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
		return Launch{}, protocol.Mark(protocol.CodeAuth, fmt.Errorf("BMC returned no JNLP (login was not accepted or the session expired; check the account and password)"))
	}
	l, e := parseJNLP(b)
	if e != nil {
		w.Close()
		return l, e
	}
	l.Cookie = w.cookie
	return l, nil
}
