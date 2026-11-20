package transport

import (
	"context"
	"time"
)

// Keepalive calls send every interval until ctx ends. When a send fails, it
// passes the error to fail and stops; what that means for the console is up
// to fail. Run it in a goroutine of its own.
func Keepalive(ctx context.Context, interval time.Duration, send func() error, fail func(error)) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := send(); err != nil {
				fail(err)
				return
			}
		}
	}
}
