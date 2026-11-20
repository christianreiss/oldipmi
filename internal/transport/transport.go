// Package transport is how OldIPMI reaches a BMC: the TLS settings, console
// connections and their rule for plain text, the session with the BMC's web
// interface and the JNLP files it hands out. Every console family goes
// through it, so a host's exceptions mean the same for all of them.
package transport

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Policy is what a connection profile allows its connections
// (protocol.Profile.Policy).
type Policy struct {
	Host string
	// HTTP: the web interface is plain HTTP.
	HTTP bool
	// LegacyTLS accepts TLS 1.0 and old ciphers and skips the certificate check.
	LegacyTLS bool
	// AllowPlainConsole lets a console connection go without TLS.
	AllowPlainConsole bool
	// ClientCert and ClientKey name the client certificate of console
	// connections.
	ClientCert, ClientKey string
}

// TLSConfig is the TLS configuration for p's host.
func TLSConfig(p Policy) *tls.Config {
	c := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: p.Host}
	if p.LegacyTLS {
		c.MinVersion = tls.VersionTLS10
		c.InsecureSkipVerify = true
		c.CipherSuites = []uint16{tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA, tls.TLS_RSA_WITH_AES_128_CBC_SHA, tls.TLS_RSA_WITH_AES_256_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}
	}
	return c
}

// ValidateHost refuses what is not a bare host name or address, such as a URL.
func ValidateHost(host string) error {
	if host == "" || strings.ContainsAny(host, "/\\@?#\x00\r\n ") {
		return fmt.Errorf("enter a BMC hostname or IP address without a URL")
	}
	return nil
}

// Wire is a console connection. WritePacket writes a packet whole, one writer
// at a time.
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

// Dial opens a console connection to port on p's host: TLS, with p's client
// certificate if it names one, or plain TCP if secure is false and p allows
// plain consoles.
func Dial(ctx context.Context, p Policy, port int, secure bool) (*Wire, error) {
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
