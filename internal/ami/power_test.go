package ami

import (
	"context"
	"oldipmi/internal/protocol"
	"testing"
)

// AMI controls power over IPMI with the password of the login, which Close
// forgets (internal/protocol pins the commands). 192.0.2.1 is never contacted.
func TestPowerChecksBeforeConnecting(t *testing.T) {
	c := client(protocol.Profile{Host: "192.0.2.1", Username: "admin"}, Launch{})
	c.password = "pw"
	if err := c.Power(context.Background(), "bogus"); err == nil || err.Error() != "unknown power action" {
		t.Fatal(err)
	}
	c.Close()
	if err := c.Power(context.Background(), "on"); err == nil || err.Error() != "power control requires account credentials" {
		t.Fatal("after Close:", err)
	}
}
