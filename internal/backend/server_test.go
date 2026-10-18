package backend

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"oldipmi/internal/protocol"
	"testing"
	"time"
)

func TestIPCVersionAndBounds(t *testing.T) {
	for _, s := range []string{`{"version":2}`, `{broken`} {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(len(s)))
		if _, e := ReadCommand(bytes.NewReader(append(b, []byte(s)...))); e == nil {
			t.Fatal("accepted invalid command")
		}
	}
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, MaxControl+1)
	if _, e := ReadCommand(bytes.NewReader(b)); e == nil {
		t.Fatal("accepted oversized command")
	}
}
func FuzzIPC(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) { ReadCommand(bytes.NewReader(b)) })
}

// Use real framed IPC so cancellation is exercised while the connection actor
// is blocked inside setup, rather than merely cancelling a fixture's context.
func TestCancelDuringSetup(t *testing.T) {
	control, peer := net.Pipe()
	video, screen := net.Pipe()
	defer peer.Close()
	defer screen.Close()
	peer.SetDeadline(time.Now().Add(2 * time.Second))
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serve(context.Background(), control, video, func(ctx context.Context, p protocol.Profile, password string, jnlp []byte) (protocol.Session, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	}()
	writeCommand(t, peer, Command{Op: "connect"})
	if e := readEvent(t, peer); e.State != "logging_in" {
		t.Fatal(e)
	}
	<-started
	writeCommand(t, peer, Command{Op: "disconnect"})
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not interrupt setup")
	}
}
func TestDiscoveryProgressAndStageError(t *testing.T) {
	for _, stage := range []string{"logging_in", "discovering", "connecting"} {
		t.Run(stage, func(t *testing.T) {
			control, peer := net.Pipe()
			video, screen := net.Pipe()
			defer peer.Close()
			defer screen.Close()
			peer.SetDeadline(time.Now().Add(2 * time.Second))
			done := make(chan error, 1)
			go func() {
				done <- serve(context.Background(), control, video, func(ctx context.Context, p protocol.Profile, password string, jnlp []byte) (protocol.Session, error) {
					protocol.Report(ctx, stage, &protocol.ConsoleInfo{Protocol: "fixture", Capabilities: []string{"Video"}})
					return nil, errors.New("fixture failure")
				})
			}()
			writeCommand(t, peer, Command{Op: "connect", Password: "never-display"})
			readEvent(t, peer)
			progress := readEvent(t, peer)
			if progress.State != stage || progress.Info == nil || progress.Info.Protocol != "fixture" {
				t.Fatal(progress)
			}
			failure := readEvent(t, peer)
			prefix := map[string]string{"logging_in": "Login", "discovering": "Console discovery", "connecting": "Console connection"}[stage]
			if failure.State != "disconnected" || failure.Message != prefix+" failed: fixture failure" {
				t.Fatal(failure)
			}
			<-done
		})
	}
}
func writeCommand(t *testing.T, w io.Writer, c Command) {
	t.Helper()
	c.Version = Version
	b, _ := json.Marshal(c)
	h := make([]byte, 4)
	binary.BigEndian.PutUint32(h, uint32(len(b)))
	if _, err := w.Write(append(h, b...)); err != nil {
		t.Fatal(err)
	}
}
func readEvent(t *testing.T, r io.Reader) Event {
	t.Helper()
	h := make([]byte, 4)
	if _, err := io.ReadFull(r, h); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, binary.BigEndian.Uint32(h))
	if _, err := io.ReadFull(r, b); err != nil {
		t.Fatal(err)
	}
	var e Event
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e
}
