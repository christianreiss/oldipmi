package ami

import (
	"oldipmi/internal/protocol"
	"reflect"
	"testing"
)

func TestJNLPDoesNotExecuteResources(t *testing.T) {
	b := []byte(`<jnlp codebase="https://untrusted.invalid"><resources><jar href="evil.jar"/></resources><application-desc><argument>bmc</argument><argument>5901</argument><argument>token</argument><argument>0</argument><argument>0</argument><argument>5120</argument><argument>5123</argument><argument>511</argument><argument>5900</argument><argument>1</argument></application-desc></jnlp>`)
	l, e := parseJNLP(b)
	if e != nil || !l.Legacy || l.Port != 5901 || l.Token != "token" {
		t.Fatal(l, e)
	}
}

func FuzzJNLP(f *testing.F) {
	f.Add([]byte("<jnlp/>"))
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
		{"AMI IVTP", Launch{Token: "secret", Cookie: "secret", Port: 7578, Secure: true, CDPort: 5120, MediaSecure: true}, false,
			protocol.ConsoleInfo{Protocol: "AMI IVTP", Endpoints: []protocol.Endpoint{ep("Video / input", "bmc.example.net:7578", "TLS"), ep("Media (advertised)", "bmc.example.net:5120", "TLS")},
				Capabilities: []string{"Video", "Keyboard", "Pointer", "Read-only ISO"}, Features: []string{protocol.FeatureMedia}}},
		{"AMI IVTP, plain, no media", Launch{Token: "secret", Port: 7578}, true,
			protocol.ConsoleInfo{Protocol: "AMI IVTP", Endpoints: []protocol.Endpoint{ep("Video / input", "bmc.example.net:7578", "Plain TCP")},
				Capabilities: []string{"Video", "Keyboard", "Pointer"}, LegacyTLS: true}},
		{"AMI MegaRAC VSC", Launch{Legacy: true, Token: "secret", Port: 7578, HIDPort: 7582, CDPort: 5120}, false,
			protocol.ConsoleInfo{Protocol: "AMI MegaRAC VSC", Endpoints: []protocol.Endpoint{ep("Video", "bmc.example.net:7578", "Plain TCP"), ep("Input", "bmc.example.net:7582", "TLS"),
				ep("Media (advertised)", "bmc.example.net:5120", "Plain TCP")}, Capabilities: []string{"Video", "Keyboard", "Pointer", "Read-only ISO"}, Features: []string{protocol.FeatureMedia}}},
	} {
		q := p
		q.LegacyTLS = c.legacy
		if got := c.launch.ConsoleInfo(q); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}
