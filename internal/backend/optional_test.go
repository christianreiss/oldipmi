package backend

import (
	"context"
	"io"
	"net"
	"oldipmi/internal/protocol"
	"reflect"
	"testing"
	"time"
)

// A console with neither virtual media nor power control (only the Console
// methods): the core refuses a mount and a power command with its own
// sentence, an eject succeeds with nothing mounted, and no media event comes.
func TestConsoleWithoutMediaOrPower(t *testing.T) {
	control, peer := net.Pipe()
	video, screen := net.Pipe()
	defer peer.Close()
	defer screen.Close()
	peer.SetDeadline(time.Now().Add(5 * time.Second))
	go io.Copy(io.Discard, screen)
	done := make(chan error, 1)
	go func() {
		done <- serve(context.Background(), control, video, func(context.Context, protocol.Profile, string) (protocol.Console, error) {
			return bareConsole{}, nil
		})
	}()
	writeCommand(t, peer, Command{Op: OpConnect})
	for e := readEvent(t, peer); e.State != StateConnected; e = readEvent(t, peer) {
		if e.State == StateDisconnected {
			t.Fatal(e.Message)
		}
	}
	for _, c := range []struct {
		cmd  Command
		want Event
	}{
		{Command{ID: 1, Op: OpMount, Path: "/boot.iso"}, Event{ID: 1, Type: EventError, Message: "virtual media is not available for this console type"}},
		{Command{ID: 2, Op: OpEject}, Event{ID: 2, Type: EventOK, Mounted: new(bool)}},
		{Command{ID: 3, Op: OpPower, Action: protocol.PowerOn}, Event{ID: 3, Type: EventError, Message: "power control is not available for this console type"}},
	} {
		writeCommand(t, peer, c.cmd)
		got := readEvent(t, peer)
		got.Version = 0
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.cmd.Op, got, c.want)
		}
	}
	// More than one media tick (500 ms) passes without a media event.
	time.Sleep(700 * time.Millisecond)
	writeCommand(t, peer, Command{ID: 4, Op: OpKey, Input: protocol.Input{Usage: 4, Down: true}})
	if e := readEvent(t, peer); e.ID != 4 || e.Type != EventOK {
		t.Fatalf("after the media ticks: %+v", e)
	}
	writeCommand(t, peer, Command{Op: OpDisconnect})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// bareConsole has only the Console methods.
type bareConsole struct{}

func (bareConsole) Run(ctx context.Context, _ func(protocol.Frame)) error {
	<-ctx.Done()
	return nil
}
func (bareConsole) Key(protocol.Input) error     { return nil }
func (bareConsole) Pointer(protocol.Input) error { return nil }
func (bareConsole) Release() error               { return nil }
func (bareConsole) Close() error                 { return nil }
