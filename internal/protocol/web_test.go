package protocol

import (
	"crypto/tls"
	"encoding/json"
	"oldipmi/internal/transport"
	"strings"
	"testing"
)

func TestTLSPolicy(t *testing.T) {
	c := transport.TLSConfig(Profile{Host: "bmc"}.Policy())
	if c.InsecureSkipVerify || c.MinVersion < tls.VersionTLS12 {
		t.Fatal("weak defaults")
	}
	c = transport.TLSConfig(Profile{LegacyTLS: true}.Policy())
	if !c.InsecureSkipVerify {
		t.Fatal("explicit compatibility setting ignored")
	}
}

func TestConsoleModeValidation(t *testing.T) {
	for in, want := range map[string]string{"": "auto", "Auto": "auto", "java": "java", " html5 ": "html5"} {
		if got, err := (Profile{ConsoleMode: in}).ConsoleModeValue(); err != nil || got != want {
			t.Fatal(in, got, err)
		}
	}
	b, _ := json.Marshal(Profile{ConsoleMode: "html5"})
	if !strings.Contains(string(b), `"consoleMode":"html5"`) {
		t.Fatal(string(b))
	}
	if b, _ = json.Marshal(Profile{}); strings.Contains(string(b), "consoleMode") {
		t.Fatal("empty console mode should be omitted")
	}
}
