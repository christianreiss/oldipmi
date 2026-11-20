package avocent

import (
	"context"
	"oldipmi/internal/protocol"
)

// Open connects to an Avocent-based console, a Dell iDRAC (family avocent) or a
// Cisco IMC (family cisco): the driver of both families in the backend's
// registry.
func Open(ctx context.Context, p protocol.Profile, password string) (protocol.Console, error) {
	c, err := Connect(ctx, p, password)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// What the console offers besides itself: power control, but no virtual media.
// The core finds it by asking, so a method whose signature drifted would only
// fail at run time without this.
var _ protocol.Power = (*Client)(nil)
