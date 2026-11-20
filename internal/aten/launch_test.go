package aten

import (
	"oldipmi/internal/protocol"
	"reflect"
	"strings"
	"testing"
)

func atenJNLP(args ...string) []byte {
	var s strings.Builder
	s.WriteString(`<jnlp><application-desc main-class="tw.com.aten.ikvm.KVMMain">`)
	for _, a := range args {
		s.WriteString("<argument>" + a + "</argument>")
	}
	s.WriteString("</application-desc></jnlp>")
	return []byte(s.String())
}

// Argument layouts per iKVM version (drac-kvm version table, ATENtion, osso.nl).
func TestATENJNLPVariants(t *testing.T) {
	cases := []struct {
		name               string
		jnlp               []byte
		port, cd           int
		secure             bool
		token, passwordTok string
	}{
		{"1.69.21 (8 args, plain)", atenJNLP("bmc", "user", "pass", "null", "5900", "623", "2", "0"), 5900, 623, false, "user", "pass"},
		{"1.69.27 (tunnel off)", atenJNLP("bmc", "tok", "tok", "null", "5900", "623", "0", "0", "0", "3520"), 5900, 623, false, "tok", ""},
		{"1.69.37 (tunnel on)", smJNLP10Bytes(), 5900, 623, true, "Tok3nAbc", ""},
		{"1.69.39 (22 args)", []byte(smJNLP22), 5900, 623, true, "rotUser1", "rotPass2"},
		{"trailing certificate", atenJNLP("bmc", "tok", "tok", "h", "63630", "623", "0", "0", "1", "5900", "-----BEGIN CERTIFICATE-----\nMIIx\n-----END CERTIFICATE-----"), 5900, 623, true, "tok", ""},
	}
	for _, c := range cases {
		l, err := parseJNLP(c.jnlp)
		if err != nil || l.Port != c.port || l.CDPort != c.cd || l.Secure != c.secure || l.Token != c.token || l.PasswordToken != c.passwordTok {
			t.Errorf("%s: %+v %v", c.name, l, err)
		}
	}
	// ATENtion's truncated fixture (certificate in argument 6) must fail cleanly.
	if _, err := parseJNLP(atenJNLP("192.0.2.139", "t", "t", "LAB-IPMI", "63630", "623", "-----BEGIN CERTIFICATE-----")); err == nil || !strings.Contains(err.Error(), "unsupported ATEN JNLP layout") {
		t.Fatal(err)
	}
	if _, err := parseJNLP(atenJNLP("http://x/", "a.jar", "b.jar")); err == nil {
		t.Fatal("jar-only JNLP accepted")
	}
}

func smJNLP10Bytes() []byte { return []byte(smJNLP10) }

func FuzzJNLP(f *testing.F) {
	f.Add([]byte(smJNLP10))
	f.Add([]byte(smJNLP22))
	f.Fuzz(func(t *testing.T, b []byte) { parseJNLP(b) })
}

// What the connection page shows per console (no tokens, cookies or
// credentials ever reach it).
func TestConsoleInfoPerLaunch(t *testing.T) {
	p := protocol.Profile{Host: "bmc.example.net"}
	ep := func(role, addr, via string) protocol.Endpoint {
		return protocol.Endpoint{Role: role, Address: addr, Transport: via}
	}
	for _, c := range []struct {
		name   string
		launch Launch
		legacy bool
		want   protocol.ConsoleInfo
	}{
		{"Java", Launch{Token: "secret", PasswordToken: "secret", Port: 5900, Secure: true, CDPort: 623}, false,
			protocol.ConsoleInfo{Protocol: "ATEN RFB", Endpoints: []protocol.Endpoint{ep("Video / input", "bmc.example.net:5900", "TLS"), ep("Media (advertised)", "bmc.example.net:623", "Plain TCP")},
				Capabilities: []string{"Video", "Keyboard", "Pointer", "Read-only ISO"}}},
		{"HTML5", Launch{HTML5: true, Token: "secret", Cookie: "secret", Port: 443, Secure: true}, false,
			protocol.ConsoleInfo{Protocol: "ATEN HTML5 (AST2100 over WebSocket)", Endpoints: []protocol.Endpoint{ep("Video / input", "bmc.example.net:443", "TLS (WebSocket)")},
				Capabilities: []string{"Video", "Keyboard", "Pointer", "Power"}}},
		{"HTML5 over HTTP", Launch{HTML5: true, Token: "secret", Port: 80}, false,
			protocol.ConsoleInfo{Protocol: "ATEN HTML5 (AST2100 over WebSocket)", Endpoints: []protocol.Endpoint{ep("Video / input", "bmc.example.net:80", "Plain TCP (WebSocket)")},
				Capabilities: []string{"Video", "Keyboard", "Pointer", "Power"}}},
		{"Redfish", Launch{HTML5: true, Redfish: true, Token: "secret", Port: 443, Secure: true}, true,
			protocol.ConsoleInfo{Protocol: "ATEN HTML5 (AST2100 over WebSocket), discovered via Redfish", Endpoints: []protocol.Endpoint{ep("Video / input", "bmc.example.net:443", "TLS (WebSocket)")},
				Capabilities: []string{"Video", "Keyboard", "Pointer", "Power"}, LegacyTLS: true}},
	} {
		q := p
		q.LegacyTLS = c.legacy
		if got := c.launch.ConsoleInfo(q); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}
