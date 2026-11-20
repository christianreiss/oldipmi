package transport

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Keepalive sends every interval until its context ends. A failed send goes
// to fail, once, and nothing is sent after it.
func TestKeepalive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sent, done := make(chan struct{}, 8), make(chan struct{})
	go func() {
		defer close(done)
		Keepalive(ctx, time.Millisecond, func() error {
			select {
			case sent <- struct{}{}:
			default:
			}
			return nil
		}, func(e error) { t.Errorf("fail(%v) without a failed send", e) })
	}()
	<-sent
	<-sent
	cancel()
	<-done

	broken := errors.New("broken pipe")
	sends, fails := 0, []error{}
	Keepalive(context.Background(), time.Millisecond, func() error {
		if sends++; sends == 3 {
			return broken
		}
		return nil
	}, func(e error) { fails = append(fails, e) })
	if sends != 3 || len(fails) != 1 || fails[0] != broken {
		t.Fatalf("%d sends, fail called with %v; want 3 sends and fail(%v) once", sends, fails, broken)
	}
}
