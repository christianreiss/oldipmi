package ami

import (
	"context"
	"oldipmi/internal/protocol"
)

// Open connects to an AMI MegaRAC console: the family's driver in the
// backend's registry.
func Open(ctx context.Context, p protocol.Profile, password string) (protocol.Console, error) {
	c, err := Connect(ctx, p, password)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// What the console offers besides itself. The core finds these by asking, so
// a method whose signature drifted would only fail at run time without them.
var (
	_ protocol.Media = (*Client)(nil)
	_ protocol.Power = (*Client)(nil)
)
