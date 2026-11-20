package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"oldipmi"
	"strings"
	"sync"
	"time"
)

// Web is a session with a BMC's web interface: one cookie jar, requests that
// stay on the BMC, and what closing the session has to undo.
type Web struct {
	Client *http.Client
	Base   string
	// RedfishToken, once a Redfish session exists, goes with every /redfish/
	// request as X-Auth-Token.
	RedfishToken string

	onClose   []func(context.Context)
	closeOnce sync.Once
}

// StatusError is a BMC HTTP answer other than the expected success status.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("BMC HTTP status %d", e.Code) }

// HTTPStatus is the code of the *StatusError in err's chain, else 0.
func HTTPStatus(err error) int {
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

// NewWeb starts a session with the web interface of p's host, over HTTPS or,
// if p says so, HTTP. Redirects must stay on that host and transport.
func NewWeb(p Policy) (*Web, error) {
	if err := ValidateHost(p.Host); err != nil {
		return nil, err
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
	return &Web{Client: c, Base: u.String()}, nil
}

// Response is a BMC's answer to Do.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Do sends one request and returns any HTTP status; only transport, size and
// truncation problems are errors. tolerant accepts a body that ends early.
func (w *Web) Do(ctx context.Context, method, path string, body []byte, contentType string, tolerant bool) (Response, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return Response{}, fmt.Errorf("invalid BMC endpoint")
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	r, e := http.NewRequestWithContext(ctx, method, w.Base+path, rd)
	if e != nil {
		return Response{}, fmt.Errorf("invalid BMC request")
	}
	// Some ATEN web servers answer 500 without a Referer.
	r.Header.Set("Referer", w.Base+"/")
	r.Header.Set("User-Agent", "OldIPMI/"+oldipmi.Version)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	if w.RedfishToken != "" && strings.HasPrefix(path, "/redfish/") {
		r.Header.Set("X-Auth-Token", w.RedfishToken)
	}
	res, e := w.Client.Do(r)
	if e != nil {
		return Response{}, fmt.Errorf("BMC HTTP transport failed: %w", e)
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if len(b) > 1<<20 {
		return Response{}, fmt.Errorf("BMC response too large")
	}
	if e != nil && !(tolerant && errors.Is(e, io.ErrUnexpectedEOF)) {
		return Response{}, fmt.Errorf("incomplete BMC response")
	}
	return Response{Status: res.StatusCode, Header: res.Header, Body: b}, nil
}

// Request performs a form or plain request that must answer HTTP 200; any
// other status is a *StatusError.
func (w *Web) Request(ctx context.Context, method, path string, form url.Values, tolerant bool) ([]byte, error) {
	var body []byte
	ctype := ""
	if form != nil {
		body, ctype = []byte(form.Encode()), "application/x-www-form-urlencoded"
	}
	res, e := w.Do(ctx, method, path, body, ctype, tolerant)
	if e != nil {
		return nil, e
	}
	if res.Status != 200 {
		return nil, &StatusError{res.Status}
	}
	return res.Body, nil
}

// SameHostPath accepts an absolute path or an absolute URL on this BMC.
func (w *Web) SameHostPath(ref string) (string, bool) {
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

// OnClose adds f to what Close does: a logout, for instance. The last added
// runs first.
func (w *Web) OnClose(f func(ctx context.Context)) { w.onClose = append(w.onClose, f) }

// Close ends the session: it runs the OnClose functions, the last added first
// and all within three seconds, forgets the Redfish token and drops idle
// connections. It is safe to call twice.
func (w *Web) Close() {
	w.closeOnce.Do(func() {
		ctx, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		for i := len(w.onClose) - 1; i >= 0; i-- {
			w.onClose[i](ctx)
		}
		w.RedfishToken = ""
		w.Client.CloseIdleConnections()
	})
}
