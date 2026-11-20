package protocol

import (
	"context"
	"reflect"
	"testing"
)

// The power words and what they send over IPMI (AMI and Avocent use this; ATEN
// sends its own codes in-band). Phase 3 gives the words one type for Go, C++
// and QML; these pin today's meaning: the chassis control commands (IPMI 2.0,
// Chassis Control) and the checks, which return before anything is sent.
func TestPowerWordsIPMI(t *testing.T) {
	got := map[string]byte{}
	for word, c := range chassisControl {
		got[word] = byte(c)
	}
	if want := map[string]byte{"off": 0, "on": 1, "reset": 3, "soft": 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chassis control %v, want %v", got, want)
	}
	for _, c := range []struct{ password, action, err string }{
		{"pw", "bogus", "unknown power action"},
		{"", "bogus", "unknown power action"}, // the word is checked first
		{"pw", "", "unknown power action"},
		{"", "on", "power control requires account credentials"},
	} {
		// 192.0.2.1 is never contacted: both checks come before the connection.
		if err := Power(context.Background(), Profile{Host: "192.0.2.1", Username: "admin"}, c.password, c.action); err == nil || err.Error() != c.err {
			t.Errorf("%q with password %t: %v, want %q", c.action, c.password != "", err, c.err)
		}
	}
}
