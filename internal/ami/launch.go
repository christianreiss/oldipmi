package ami

import (
	"fmt"
	"oldipmi/internal/protocol"
	"oldipmi/internal/transport"
	"strconv"
	"strings"
)

// Launch is how to reach the console, as the web login found it. Legacy marks
// the MegaRAC VSC console, whose JNLP passes its arguments by position (Pos);
// the IVTP console's JNLP passes -name value pairs, which Args holds.
type Launch struct {
	Args map[string]string
	Pos  []string
	// Token is the console credential, Cookie the web session's.
	Token, Cookie               string
	Port, HIDPort, CDPort       int
	Secure, MediaSecure, Legacy bool
}

// ConsoleInfo is what the connection page shows of the console: never a
// token, cookie or credential.
func (l Launch) ConsoleInfo(p protocol.Profile) protocol.ConsoleInfo {
	name := "AMI IVTP"
	if l.Legacy {
		name = "AMI MegaRAC VSC"
	}
	info := protocol.ConsoleInfo{Protocol: name, LegacyTLS: p.LegacyTLS,
		Endpoints:    []protocol.Endpoint{protocol.ConsoleEndpoint("Video / input", p.Host, l.Port, l.Secure)},
		Capabilities: []string{"Video", "Keyboard", "Pointer"}}
	if l.Legacy {
		info.Endpoints[0].Role = "Video"
		info.Endpoints = append(info.Endpoints, protocol.ConsoleEndpoint("Input", p.Host, l.HIDPort, true))
	}
	if l.CDPort > 0 && l.CDPort <= 65535 {
		info.Endpoints = append(info.Endpoints, protocol.ConsoleEndpoint("Media (advertised)", p.Host, l.CDPort, l.MediaSecure))
		info.Capabilities = append(info.Capabilities, "Read-only ISO")
	}
	return info
}

// parseJNLP reads the console's address and credentials from the JNLP. An
// ATEN JNLP gets the hint to select that family.
func parseJNLP(b []byte) (Launch, error) {
	doc, e := transport.ParseJNLP(b)
	if e != nil {
		return Launch{}, e
	}
	if strings.Contains(doc.Main, "aten") {
		return Launch{}, fmt.Errorf("this BMC needs the ATEN family; select it for this profile")
	}
	a := doc.Args
	l := Launch{Args: map[string]string{}, Pos: a}
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
