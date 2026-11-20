package protocol

import (
	"context"
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

// Console is an open console: the video it shows and the keyboard and pointer
// it takes. Run delivers frames until ctx ends or the connection fails; a
// frame's Pixels are only valid during the call. Release lets go of every key
// and button the console holds down. Close ends the console and whatever it
// opened; calling it again does nothing.
//
// A console with virtual media or chassis power control also implements Media
// or Power; the core asks for them when a command needs them.
type Console interface {
	Run(context.Context, func(Frame)) error
	Key(Input) error
	Pointer(Input) error
	Release() error
	Close() error
}

// Media is a console's virtual media: a local ISO that the host sees as a
// read-only CD/DVD until Eject.
type Media interface {
	Mount(context.Context, string) error
	Eject() error
	Mounted() bool
}

// Power is a console's chassis power control.
type Power interface {
	Power(context.Context, PowerAction) error
}

// Policy is what p allows its connections.
func (p Profile) Policy() transport.Policy {
	return transport.Policy{Host: p.Host, HTTP: p.HTTP, LegacyTLS: p.LegacyTLS, AllowPlainConsole: p.AllowPlainConsole,
		ClientCert: p.ClientCert, ClientKey: p.ClientKey}
}
func Read(r io.Reader, n int) ([]byte, error) {
	if n < 0 || n > MaxPayload {
		return nil, fmt.Errorf("packet exceeds limit")
	}
	b := make([]byte, n)
	_, e := io.ReadFull(r, b)
	return b, e
}
