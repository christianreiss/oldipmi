package protocol

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"oldipmi/internal/transport"
	"strings"
)

const MaxPayload = 16 << 20

type Profile struct {
	Name              string `json:"name"`
	Host              string `json:"host"`
	Family            string `json:"family"`
	Username          string `json:"username"`
	HTTP              bool   `json:"http"`
	LegacyTLS         bool   `json:"legacyTLS"`
	AllowPlainConsole bool   `json:"allowPlainConsole"`
	JNLPPath          string `json:"jnlpPath,omitempty"`
	ClientCert        string `json:"clientCert,omitempty"`
	ClientKey         string `json:"clientKey,omitempty"`
	// ConsoleMode selects the ATEN / Supermicro console: "" or "auto" tries
	// the Java iKVM and falls back to HTML5, "java" and "html5" force one.
	ConsoleMode string `json:"consoleMode,omitempty"`
}

const (
	ConsoleAuto  = "auto"
	ConsoleJava  = "java"
	ConsoleHTML5 = "html5"
)

// ConsoleModeValue returns the normalized console mode.
func (p Profile) ConsoleModeValue() (string, error) {
	switch m := strings.ToLower(strings.TrimSpace(p.ConsoleMode)); m {
	case "", ConsoleAuto:
		return ConsoleAuto, nil
	case ConsoleJava, ConsoleHTML5:
		return m, nil
	}
	return "", fmt.Errorf("unknown console mode %q; set this profile's console mode to Auto, Java or HTML5", p.ConsoleMode)
}

type Frame struct {
	Width, Height int
	Pixels        []byte
}
type Input struct {
	Usage   uint32 `json:"usage"`
	Down    bool   `json:"down"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Buttons uint8  `json:"buttons"`
	Wheel   int    `json:"wheel"`
}
type Session interface {
	Run(context.Context, func(Frame)) error
	Key(Input) error
	Pointer(Input) error
	Release() error
	Mount(context.Context, string) error
	Eject() error
	Mounted() bool
	Power(context.Context, string) error
	Close() error
}

// Policy is what p allows its connections.
func (p Profile) Policy() transport.Policy {
	return transport.Policy{Host: p.Host, HTTP: p.HTTP, LegacyTLS: p.LegacyTLS, AllowPlainConsole: p.AllowPlainConsole,
		ClientCert: p.ClientCert, ClientKey: p.ClientKey}
}

// Wire, TLSConfig and Dial are package transport's, until the drivers use it
// themselves.
type Wire = transport.Wire

func TLSConfig(p Profile) *tls.Config { return transport.TLSConfig(p.Policy()) }
func Dial(ctx context.Context, p Profile, port int, secure bool) (*Wire, error) {
	return transport.Dial(ctx, p.Policy(), port, secure)
}
func Read(r io.Reader, n int) ([]byte, error) {
	if n < 0 || n > MaxPayload {
		return nil, fmt.Errorf("packet exceeds limit")
	}
	b := make([]byte, n)
	_, e := io.ReadFull(r, b)
	return b, e
}
