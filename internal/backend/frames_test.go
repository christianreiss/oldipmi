package backend

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"oldipmi/internal/protocol"
	"testing"
	"time"
)

// framesConsole sends good, waits for next, then sends bad and another
// frame, and ends only with its context.
type framesConsole struct {
	good, bad protocol.Frame
	next      chan struct{}
}

func (c *framesConsole) Run(ctx context.Context, emit func(protocol.Frame)) error {
	emit(c.good)
	select {
	case <-c.next:
	case <-ctx.Done():
		return ctx.Err()
	}
	emit(c.bad)
	emit(c.good) // nothing after a refused frame goes out
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond) // time to send that frame, were it let through
	return ctx.Err()
}
func (c *framesConsole) Key(protocol.Input) error     { return nil }
func (c *framesConsole) Pointer(protocol.Input) error { return nil }
func (c *framesConsole) Release() error               { return nil }
func (c *framesConsole) Close() error                 { return nil }

// readFrames sends each frame on the video connection to the channel.
func readFrames(r io.Reader) <-chan protocol.Frame {
	frames := make(chan protocol.Frame, 4)
	go func() {
		for {
			h := make([]byte, 12)
			if _, err := io.ReadFull(r, h); err != nil {
				return
			}
			pix := make([]byte, binary.BigEndian.Uint32(h)-8)
			if _, err := io.ReadFull(r, pix); err != nil {
				return
			}
			frames <- protocol.Frame{Width: int(binary.BigEndian.Uint32(h[4:])), Height: int(binary.BigEndian.Uint32(h[8:])), Pixels: pix}
		}
	}()
	return frames
}

// A frame that the desktop would refuse ends the console with what is wrong
// with it; the frames before it reach the desktop, none after it.
func TestFramesAreCheckedBeforeTheyGoOut(t *testing.T) {
	good := protocol.Frame{Width: 2, Height: 2, Pixels: bytes.Repeat([]byte{1, 2, 3, 255}, 4)}
	for _, tc := range []struct {
		name string
		bad  protocol.Frame
		want string
	}{
		{"no width", protocol.Frame{Width: 0, Height: 768}, "the console sent an empty picture (0 × 768)"},
		{"no height", protocol.Frame{Width: 1024, Height: 0}, "the console sent an empty picture (1024 × 0)"},
		{"too wide", protocol.Frame{Width: 4097, Height: 1, Pixels: make([]byte, 4097*4)}, "the console sent a 4097 × 1 picture, over the limit of 4096 × 2160"},
		{"too high", protocol.Frame{Width: 1, Height: 2161, Pixels: make([]byte, 2161*4)}, "the console sent a 1 × 2161 picture, over the limit of 4096 × 2160"},
		{"too few bytes", protocol.Frame{Width: 2, Height: 2, Pixels: make([]byte, 15)}, "the console sent a 2 × 2 picture in 15 bytes instead of 16"},
		{"too many bytes", protocol.Frame{Width: 2, Height: 2, Pixels: make([]byte, 17)}, "the console sent a 2 × 2 picture in 17 bytes instead of 16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			console := &framesConsole{good: good, bad: tc.bad, next: make(chan struct{})}
			control, peer := net.Pipe()
			video, screen := net.Pipe()
			t.Cleanup(func() { peer.Close(); screen.Close() })
			peer.SetDeadline(time.Now().Add(5 * time.Second))
			frames := readFrames(screen)
			done := make(chan error, 1)
			go func() {
				done <- serve(context.Background(), control, video, func(context.Context, protocol.Profile, string) (protocol.Console, error) {
					return console, nil
				})
			}()
			writeCommand(t, peer, Command{Op: OpConnect})
			for e := readEvent(t, peer); e.State != StateConnected; e = readEvent(t, peer) {
			}
			select {
			case f := <-frames:
				if f.Width != 2 || f.Height != 2 || !bytes.Equal(f.Pixels, good.Pixels) {
					t.Fatalf("the good frame arrived as %+v", f)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the good frame did not arrive")
			}
			close(console.next)
			if e := readEvent(t, peer); e.Type != EventError || e.State != StateDisconnected || e.ID != 0 || e.Message != tc.want {
				t.Fatalf("got %+v, want the console's end with %q", e, tc.want)
			}
			if err := <-done; err == nil || err.Error() != tc.want {
				t.Fatalf("serve returned %v, want %q", err, tc.want)
			}
			select {
			case f := <-frames:
				t.Fatalf("a frame went out after the refused one: %dx%d", f.Width, f.Height)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}
