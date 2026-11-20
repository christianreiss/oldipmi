package demo

import (
	"context"
	"oldipmi/internal/protocol"
)

// Open connects to the synthetic console, which needs no password: the demo
// family's driver in the backend's registry.
func Open(ctx context.Context, p protocol.Profile, _ string) (protocol.Console, error) {
	return Connect(ctx, p)
}

// What the console offers besides itself. The core finds these by asking, so
// a method whose signature drifted would only fail at run time without them.
var (
	_ protocol.Media = (*Session)(nil)
	_ protocol.Power = (*Session)(nil)
)
