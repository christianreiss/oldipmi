package aten

import (
	"context"
	"oldipmi/internal/protocol"
)

// Open connects to an ATEN (Supermicro) console: the family's driver in the
// backend's registry.
func Open(ctx context.Context, p protocol.Profile, password string) (protocol.Session, error) {
	c, err := Connect(ctx, p, password)
	if err != nil {
		return nil, err
	}
	return c, nil
}
