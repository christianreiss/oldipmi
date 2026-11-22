package protocol

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"oldipmi/internal/transport"
	"os"
	"strings"
	"syscall"
)

// The classes of the errors that end a console's setup or the console. The
// core sends an error's class as its code (testdata/ipc/names.json, codes), so
// that the desktop need not read the English text for its hint. An error of
// no known class has no code.
const (
	CodeDNS           = "dns"            // the BMC's name does not resolve
	CodeUnreachable   = "unreachable"    // no answer in time, or no route to the BMC
	CodeRefused       = "refused"        // nothing listens on the BMC's port
	CodeTLSName       = "tls_name"       // the BMC's certificate names another host
	CodeTLSUnverified = "tls_unverified" // the BMC's certificate cannot be verified
	CodeTLSOld        = "tls_old"        // the BMC chose TLS 1.0 or 1.1
	CodeTLS           = "tls"            // the secure connection failed otherwise
	CodeAuth          = "auth"           // the BMC rejected the account or password
	CodeTextMode      = "text_mode"      // the screen is in a text mode the console cannot draw
	CodePlainConsole  = "plain_console"  // the BMC offers only an unencrypted console
)

// Mark gives err the class code, for what only a driver can tell, such as a
// login the BMC rejected. The text stays err's own.
func Mark(code string, err error) error { return &codedError{code, err} }

type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }

// ErrorCode is the class of err, "" if none fits. The classes are tried in the
// order of the desktop's hints (desktop/qml/stages.js, errorHint), which
// take the first that fits too: an error is in the class whose hint its text
// gets there. A Mark counts where its class stands in that order.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for _, c := range classes {
		code := c.code
		if c.fits != nil && c.fits(err) || anyError(err, func(e error) bool {
			m, ok := e.(*codedError)
			return ok && m.code == code
		}) {
			return code
		}
	}
	return ""
}

var classes = []struct {
	code string
	fits func(error) bool // nil: only a Mark tells
}{
	// A name that does not exist or a resolver that failed; a lookup that
	// timed out is unreachable.
	{CodeDNS, func(err error) bool {
		var d *net.DNSError
		return errors.As(err, &d) && (d.IsNotFound || d.Err == "server misbehaving")
	}},
	{CodeUnreachable, func(err error) bool {
		var d *net.DNSError
		return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) ||
			errors.As(err, &d) && d.IsTimeout ||
			errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH)
	}},
	{CodeRefused, func(err error) bool { return errors.Is(err, syscall.ECONNREFUSED) }},
	// A certificate that names the host only in its legacy Common Name is
	// one that cannot be verified, the class after this one.
	{CodeTLSName, func(err error) bool {
		var h x509.HostnameError
		return errors.As(err, &h) && !strings.Contains(h.Error(), "legacy Common Name field")
	}},
	{CodeTLSUnverified, func(err error) bool {
		var v *tls.CertificateVerificationError
		var h x509.HostnameError
		return errors.As(err, &v) || errors.As(err, &h)
	}},
	// crypto/tls gives this error no type; a test pins its text against a BMC
	// that answers in TLS 1.0 and 1.1.
	{CodeTLSOld, func(err error) bool {
		return anyError(err, func(e error) bool {
			s := e.Error()
			return s == "tls: server selected unsupported protocol version 301" ||
				s == "tls: server selected unsupported protocol version 302"
		})
	}},
	// crypto/tls and crypto/x509 begin each of their errors, alerts included,
	// with their name.
	{CodeTLS, func(err error) bool {
		return errors.Is(err, transport.ErrClientCert) || anyError(err, func(e error) bool {
			s := e.Error()
			return strings.HasPrefix(s, "tls: ") || strings.HasPrefix(s, "x509: ")
		})
	}},
	{CodeAuth, func(err error) bool {
		s := transport.HTTPStatus(err)
		return s == 401 || s == 403
	}},
	{CodeTextMode, nil},
	{CodePlainConsole, func(err error) bool {
		return errors.Is(err, transport.ErrPlainConsole) || errors.Is(err, transport.ErrPlainWebSocket)
	}},
}

// anyError reports whether fits holds for err or an error it wraps.
func anyError(err error, fits func(error) bool) bool {
	if err == nil {
		return false
	}
	if fits(err) {
		return true
	}
	switch u := err.(type) {
	case interface{ Unwrap() error }:
		return anyError(u.Unwrap(), fits)
	case interface{ Unwrap() []error }:
		for _, e := range u.Unwrap() {
			if anyError(e, fits) {
				return true
			}
		}
	}
	return false
}
