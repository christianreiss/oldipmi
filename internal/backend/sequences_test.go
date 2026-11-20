package backend

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net"
	"oldipmi/internal/protocol"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"
)

// testdata/ipc/sequences.json holds whole dialogues of the IPC. Here the core
// plays each one against a scripted console: it must send exactly the
// fixture's events, in order, and then end. desktop/session_test.cpp plays the
// same events to a Session.

type opener = func(context.Context, protocol.Profile, string) (protocol.Session, error)

// The console of each dialogue; info is what its connecting event shows.
var dialogueConsoles = map[string]func(info *protocol.ConsoleInfo) opener{
	"consoleInUse": func(info *protocol.ConsoleInfo) opener {
		return func(ctx context.Context, _ protocol.Profile, _ string) (protocol.Session, error) {
			protocol.Report(ctx, protocol.StageDiscovering, nil)
			protocol.Report(ctx, protocol.StageConnecting, info)
			return &scriptedConsole{mounted: []bool{false}, end: make(chan struct{})}, nil
		}
	},
	"loginFails": func(*protocol.ConsoleInfo) opener {
		return func(context.Context, protocol.Profile, string) (protocol.Session, error) {
			return nil, errors.New("the BMC rejected the login")
		}
	},
	"discoveryFails": func(*protocol.ConsoleInfo) opener {
		return func(ctx context.Context, _ protocol.Profile, _ string) (protocol.Session, error) {
			protocol.Report(ctx, protocol.StageDiscovering, nil)
			return nil, errors.New("no console found")
		}
	},
	"connectionFails": func(info *protocol.ConsoleInfo) opener {
		return func(ctx context.Context, _ protocol.Profile, _ string) (protocol.Session, error) {
			protocol.Report(ctx, protocol.StageDiscovering, nil)
			protocol.Report(ctx, protocol.StageConnecting, info)
			return nil, io.EOF
		}
	},
}

// scriptedConsole is the console of consoleInUse: keys work, the guest ejects
// a mounted ISO at once, and power control fails, after which the console ends.
type scriptedConsole struct {
	mu      sync.Mutex
	mounted []bool // what Mounted answers next; the last answer stays
	end     chan struct{}
}

func (s *scriptedConsole) Run(ctx context.Context, _ func(protocol.Frame)) error {
	select {
	case <-s.end:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *scriptedConsole) Key(protocol.Input) error     { return nil }
func (s *scriptedConsole) Pointer(protocol.Input) error { return nil }
func (s *scriptedConsole) Release() error               { return nil }
func (s *scriptedConsole) Mount(context.Context, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mounted = []bool{true, false} // the reply sees it mounted, the next look does not
	return nil
}
func (s *scriptedConsole) Eject() error { return nil }
func (s *scriptedConsole) Mounted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.mounted[0]
	if len(s.mounted) > 1 {
		s.mounted = s.mounted[1:]
	}
	return m
}
func (s *scriptedConsole) Power(context.Context, protocol.PowerAction) error {
	close(s.end)
	return errors.New("power control requires account credentials")
}
func (s *scriptedConsole) Close() error { return nil }

func TestDialogues(t *testing.T) {
	var file map[string]json.RawMessage
	fixture(t, "sequences.json", &file)
	delete(file, "about")
	if got, want := slices.Sorted(maps.Keys(file)), slices.Sorted(maps.Keys(dialogueConsoles)); !slices.Equal(got, want) {
		t.Fatalf("sequences.json has %v, this test scripts %v", got, want)
	}
	for name, console := range dialogueConsoles {
		t.Run(name, func(t *testing.T) {
			var steps []struct{ Desktop, Core json.RawMessage }
			if err := json.Unmarshal(file[name], &steps); err != nil {
				t.Fatal(err)
			}
			var info *protocol.ConsoleInfo
			for _, s := range steps {
				var e Event
				if s.Core != nil && json.Unmarshal(s.Core, &e) == nil && e.Info != nil {
					info = e.Info
				}
			}
			control, peer := net.Pipe()
			video, screen := net.Pipe()
			defer peer.Close()
			defer screen.Close()
			go io.Copy(io.Discard, screen)
			peer.SetDeadline(time.Now().Add(5 * time.Second))
			go serve(context.Background(), control, video, console(info))
			for i, s := range steps {
				if s.Desktop != nil {
					var b bytes.Buffer
					if err := json.Compact(&b, s.Desktop); err != nil {
						t.Fatal(err)
					}
					if _, err := peer.Write(append(binary.BigEndian.AppendUint32(nil, uint32(b.Len())), b.Bytes()...)); err != nil {
						t.Fatalf("step %d: %v", i, err)
					}
					continue
				}
				h := make([]byte, 4)
				if _, err := io.ReadFull(peer, h); err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				got := make([]byte, binary.BigEndian.Uint32(h))
				if _, err := io.ReadFull(peer, got); err != nil {
					t.Fatalf("step %d: %v", i, err)
				}
				var g, w any
				json.Unmarshal(got, &g)
				json.Unmarshal(s.Core, &w)
				if !reflect.DeepEqual(g, w) {
					t.Fatalf("step %d: the core sent\n %s\nwant\n %s", i, got, s.Core)
				}
			}
			// Then nothing: the core ends and closes its side.
			if n, err := io.ReadFull(peer, make([]byte, 4)); err != io.EOF {
				t.Fatalf("after the last step: %d more bytes, %v", n, err)
			}
		})
	}
}
