package aten

import (
	"errors"
	"fmt"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"strconv"
	"strings"
)

// Launch is how to reach the console, as the web login found it.
type Launch struct {
	// Token is the console credential. Java JNLPs carry a user and a password
	// field; PasswordToken is set only when the two differ.
	Token, PasswordToken string
	// Cookie is the web session's cookie header, which the HTML5 console's
	// WebSocket needs.
	Cookie       string
	Port, CDPort int
	Secure       bool
	// HTML5 marks firmware that only offers the WebSocket (noVNC) console.
	// Token is then the iKVM session token.
	HTML5 bool
	// Redfish marks an HTML5 console discovered through the Supermicro Redfish
	// iKVM resource (X11 and later; the only web path documented for X12/X13).
	Redfish bool
	// ClientType marks HTML5 firmware whose own noVNC client (X12/X13
	// generation) ends the 48-byte token authentication with a client-type
	// byte; the BMC waits for it before answering.
	ClientType bool
}

// ConsoleInfo is what the connection page shows of the console: never a
// token, cookie or credential.
func (l Launch) ConsoleInfo(p protocol.Profile) protocol.ConsoleInfo {
	if l.HTML5 {
		info := protocol.ConsoleInfo{Protocol: "ATEN HTML5 (AST2100 over WebSocket)", LegacyTLS: p.LegacyTLS,
			Endpoints:    []protocol.Endpoint{protocol.ConsoleEndpoint("Video / input", p.Host, l.Port, l.Secure)},
			Capabilities: []string{"Video", "Keyboard", "Pointer", "Power"}}
		info.Endpoints[0].Transport += " (WebSocket)"
		if l.Redfish {
			info.Protocol += ", discovered via Redfish"
		}
		return info
	}
	info := protocol.ConsoleInfo{Protocol: "ATEN RFB", LegacyTLS: p.LegacyTLS,
		Endpoints:    []protocol.Endpoint{protocol.ConsoleEndpoint("Video / input", p.Host, l.Port, l.Secure)},
		Capabilities: []string{"Video", "Keyboard", "Pointer"}}
	if l.CDPort > 0 && l.CDPort <= 65535 {
		// The native ATEN media adapter uses plain TCP.
		info.Endpoints = append(info.Endpoints, protocol.ConsoleEndpoint("Media (advertised)", p.Host, l.CDPort, false))
		info.Capabilities = append(info.Capabilities, "Read-only ISO")
		info.Features = append(info.Features, protocol.FeatureMedia)
	}
	return info
}

// errNotATEN is the answer to a JNLP that starts another family's client.
var errNotATEN = errors.New("the BMC served a non-ATEN JNLP; select the AMI family for this profile")

// parseJNLP reads the console's address and credentials from the Java iKVM's
// JNLP. KVMMain's arguments (ATENtion; drac-kvm iKVM version table) are
// 0 host, 1 user/token, 2 password/token, 3 host name, 4 KVM port
// (stunnel-local when tunnelled), 5 media port, 6 company, 7 board,
// 8 TLS tunnel flag, 9 TLS KVM port, [10 remote media port, 11 media service].
// iKVM 1.69.21 has only 0-7: plain RFB on argument 4.
func parseJNLP(b []byte) (Launch, error) {
	doc, err := transport.ParseJNLP(b)
	if err != nil {
		return Launch{}, err
	}
	if !strings.Contains(doc.Main, "aten") {
		return Launch{}, errNotATEN
	}
	a := doc.Args[argOffset(doc.Args):]
	if len(a) < 8 {
		return Launch{}, fmt.Errorf("unsupported ATEN JNLP layout (%d connection arguments)", len(a))
	}
	number := func(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
	l := Launch{Token: strings.TrimSpace(a[1]), Port: number(a[4]), CDPort: number(a[5])}
	if p := strings.TrimSpace(a[2]); p != l.Token {
		l.PasswordToken = p
	}
	if len(a) >= 10 {
		l.Secure = strings.TrimSpace(a[8]) != "0"
		if l.Secure {
			l.Port = number(a[9])
			// With the tunnel, argument 5 is a local stunnel port; the
			// longer layout names the BMC's media port separately.
			if len(a) >= 11 && number(a[10]) > 0 {
				l.CDPort = number(a[10])
			}
		}
	}
	if len(l.PasswordToken) > 128 || l.Token == "" || len(l.Token) > 128 || l.Port < 1 || l.Port > 65535 {
		return l, fmt.Errorf("JNLP has invalid connection parameters")
	}
	return l, nil
}

// argOffset skips a leading codebase URL and jar/native-library names.
// Some ATEN JNLPs (iKVM 1.69.39, osso.nl 2020) pass those as arguments 0-9 and
// the connection arguments from index 10.
func argOffset(a []string) int {
	n := 0
	for n < len(a) {
		s := strings.ToLower(strings.TrimSpace(a[n]))
		if !strings.Contains(s, "://") && !strings.HasSuffix(s, ".jar") {
			break
		}
		n++
	}
	return n
}
