package power

import (
	"context"
	"oldipmi/internal/protocol"
	"reflect"
	"testing"
)

// The power actions and what they send over IPMI (AMI and Avocent embed IPMI;
// ATEN sends its own codes in-band): the chassis control commands (IPMI 2.0,
// Chassis Control) and the checks, which return before anything is sent.
func TestChassisControl(t *testing.T) {
	got := map[string]byte{}
	for action, c := range chassisControl {
		got[string(action)] = byte(c)
	}
	if want := map[string]byte{"off": 0, "on": 1, "reset": 3, "soft": 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("chassis control %v, want %v", got, want)
	}
	for _, c := range []struct{ password, action, err string }{
		{"pw", "bogus", "unknown power action"},
		{"", "bogus", "unknown power action"}, // the action is checked first
		{"pw", "", "unknown power action"},
		{"", "on", "power control requires account credentials"},
	} {
		// 192.0.2.1 is never contacted: both checks come before the connection.
		i := NewIPMI(protocol.Profile{Host: "192.0.2.1", Username: "admin"}, c.password)
		if err := i.Power(context.Background(), protocol.PowerAction(c.action)); err == nil || err.Error() != c.err {
			t.Errorf("%q with password %t: %v, want %q", c.action, c.password != "", err, c.err)
		}
	}
}

// Close forgets the password, so a power command after it fails before it
// connects.
func TestCloseForgetsThePassword(t *testing.T) {
	i := NewIPMI(protocol.Profile{Host: "192.0.2.1", Username: "admin"}, "pw")
	i.Close()
	if i.password != "" {
		t.Fatal("Close kept the password")
	}
	if err := i.Power(context.Background(), protocol.PowerOn); err == nil || err.Error() != "power control requires account credentials" {
		t.Fatal(err)
	}
}
