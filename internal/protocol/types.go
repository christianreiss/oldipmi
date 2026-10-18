package protocol

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
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
type Wire struct {
	net.Conn
	mu sync.Mutex
}

func (w *Wire) WritePacket(b []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	for len(b) > 0 {
		n, e := w.Write(b)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
func TLSConfig(p Profile) *tls.Config {
	c := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: p.Host}
	if p.LegacyTLS {
		c.MinVersion = tls.VersionTLS10
		c.InsecureSkipVerify = true
		c.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_RSA_WITH_AES_128_CBC_SHA, tls.TLS_RSA_WITH_AES_256_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}
	}
	return c
}
func Dial(ctx context.Context, p Profile, port int, secure bool) (*Wire, error) {
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid console port")
	}
	if !secure && !p.AllowPlainConsole {
		return nil, fmt.Errorf("BMC requests an unencrypted console; enable this host's plain-console option explicitly")
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	addr := net.JoinHostPort(p.Host, strconv.Itoa(port))
	var c net.Conn
	var err error
	if secure {
		tc := TLSConfig(p)
		if p.ClientCert != "" || p.ClientKey != "" {
			cert, e := tls.LoadX509KeyPair(p.ClientCert, p.ClientKey)
			if e != nil {
				return nil, fmt.Errorf("cannot load client certificate/key")
			}
			tc.Certificates = []tls.Certificate{cert}
		}
		c, err = (&tls.Dialer{NetDialer: d, Config: tc}).DialContext(ctx, "tcp", addr)
	} else {
		c, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	return &Wire{Conn: c}, nil
}
func Read(r io.Reader, n int) ([]byte, error) {
	if n < 0 || n > MaxPayload {
		return nil, fmt.Errorf("packet exceeds limit")
	}
	b := make([]byte, n)
	_, e := io.ReadFull(r, b)
	return b, e
}
