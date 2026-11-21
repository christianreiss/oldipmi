package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/websocket"
)

// DialWS opens a console WebSocket on port of p's host: wss://, or ws:// if
// secure is false, which only a profile whose web interface is plain HTTP
// allows. cookie, if set, goes with the handshake. A failed connection or
// handshake is a *websocket.DialError (for errors.As, which also reaches its
// cause); when ctx ends first, its error.
func DialWS(ctx context.Context, p Policy, port int, secure bool, cookie string) (*websocket.Conn, error) {
	scheme, origin := "wss", "https"
	if !secure {
		scheme, origin = "ws", "http"
	}
	if !secure && !p.HTTP {
		return nil, ErrPlainWebSocket
	}
	host := net.JoinHostPort(p.Host, fmt.Sprint(port))
	cfg, err := websocket.NewConfig(scheme+"://"+host+"/", origin+"://"+host)
	if err != nil {
		return nil, fmt.Errorf("invalid console address")
	}
	cfg.TlsConfig = TLSConfig(p)
	cfg.Dialer = &net.Dialer{Timeout: 10 * time.Second}
	cfg.Header = http.Header{}
	if cookie != "" {
		cfg.Header.Set("Cookie", cookie)
	}
	// DialContext also ends the WebSocket handshake when ctx ends.
	ws, err := cfg.DialContext(ctx)
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var d *websocket.DialError
	if errors.As(err, &d) {
		return nil, dialError{d}
	}
	return ws, err
}

// dialError is a failed WebSocket connection or handshake, with its text
// unchanged, whose cause errors.Is and errors.As reach: x/net's DialError
// does not unwrap.
type dialError struct{ *websocket.DialError }

func (e dialError) Unwrap() []error { return []error{e.DialError, e.DialError.Err} }
