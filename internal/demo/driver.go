package demo

import (
	"context"
	"oldipmi/internal/protocol"
)

// Open connects to the synthetic console, which needs no password: the demo
// family's driver in the backend's registry.
func Open(ctx context.Context, p protocol.Profile, _ string) (protocol.Session, error) {
	return Connect(ctx, p)
}
