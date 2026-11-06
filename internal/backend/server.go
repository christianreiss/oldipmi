package backend

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"oldipmi/internal/ami"
	"oldipmi/internal/aten"
	"oldipmi/internal/avocent"
	"oldipmi/internal/demo"
	"oldipmi/internal/protocol"
	"sync"
	"sync/atomic"
	"time"
)

const Version = 1
const MaxControl = 1 << 20

type Command struct {
	Version  int              `json:"version"`
	ID       int              `json:"id"`
	Op       string           `json:"op"`
	Profile  protocol.Profile `json:"profile"`
	Password string           `json:"password"`
	JNLP     string           `json:"jnlp"`
	Input    protocol.Input   `json:"input"`
	Path     string           `json:"path"`
	Action   string           `json:"action"`
}
type Event struct {
	Version int                   `json:"version"`
	ID      int                   `json:"id,omitempty"`
	Type    string                `json:"type"`
	State   string                `json:"state,omitempty"`
	Message string                `json:"message,omitempty"`
	Mounted *bool                 `json:"mounted,omitempty"`
	Info    *protocol.ConsoleInfo `json:"info,omitempty"`
}

func ReadCommand(r io.Reader) (Command, error) {
	var c Command
	var h [4]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return c, e
	}
	n := binary.BigEndian.Uint32(h[:])
	if n == 0 || n > MaxControl {
		return c, fmt.Errorf("invalid IPC command length")
	}
	b := make([]byte, n)
	if _, e := io.ReadFull(r, b); e != nil {
		return c, e
	}
	if e := json.Unmarshal(b, &c); e != nil {
		return c, fmt.Errorf("invalid IPC command")
	}
	if c.Version != Version {
		return c, fmt.Errorf("incompatible IPC version")
	}
	return c, nil
}
func Open(ctx context.Context, p protocol.Profile, password string, jnlp []byte) (protocol.Session, error) {
	switch p.Family {
	case "avocent", "cisco":
		return avocent.Connect(ctx, p, password, jnlp)
	case "ami":
		return ami.Connect(ctx, p, password, jnlp)
	case "aten":
		return aten.Connect(ctx, p, password, jnlp)
	case "demo": // synthetic console for BMC-free end-to-end checks; never touches the network
		return demo.Connect(ctx, p)
	default:
		return nil, fmt.Errorf("unsupported BMC family; select AMI, ATEN, Avocent or Cisco")
	}
}
func Serve(parent context.Context, control, video net.Conn) error {
	return serve(parent, control, video, Open)
}
func serve(parent context.Context, control, video net.Conn, open func(context.Context, protocol.Profile, string, []byte) (protocol.Session, error)) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer control.Close()
	defer video.Close()
	stop := context.AfterFunc(ctx, func() { control.SetReadDeadline(time.Now()); video.Close() })
	defer stop()
	var writer sync.Mutex
	emit := func(e Event) {
		writer.Lock()
		defer writer.Unlock()
		e.Version = Version
		b, _ := json.Marshal(e)
		h := make([]byte, 4)
		binary.BigEndian.PutUint32(h, uint32(len(b)))
		control.SetWriteDeadline(time.Now().Add(3 * time.Second))
		buffers := net.Buffers{h, b}
		if _, err := buffers.WriteTo(control); err != nil {
			cancel()
		}
	}
	var opening atomic.Bool
	opening.Store(true)
	cmds := make(chan Command, 32)
	go func() {
		defer close(cmds)
		defer cancel()
		for {
			c, e := ReadCommand(control)
			if e != nil {
				return
			}

			if c.Op == "disconnect" && opening.Load() {
				cancel()
				return
			}
			select {
			case cmds <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	var first Command
	select {
	case <-ctx.Done():
		return ctx.Err()
	case c, ok := <-cmds:
		if !ok {
			return io.EOF
		}
		first = c
	}
	if first.Op != "connect" {
		return fmt.Errorf("first IPC command must connect")
	}
	stage := "logging_in"
	if first.JNLP != "" {
		stage = "discovering"
	}
	emit(Event{Type: "state", State: stage})
	setupCtx := protocol.WithProgress(ctx, func(p protocol.Progress) {
		stage = p.Stage
		emit(Event{Type: "state", State: stage, Info: p.Info})
	})
	session, e := open(setupCtx, first.Profile, first.Password, []byte(first.JNLP))
	opening.Store(false)
	first.Password = ""
	first.JNLP = ""
	if e != nil {
		if ctx.Err() == nil {
			label := map[string]string{"logging_in": "Login", "discovering": "Console discovery", "connecting": "Console connection"}[stage]
			emit(Event{Type: "error", State: "disconnected", Message: label + " failed: " + e.Error()})
		}
		return e
	}
	defer session.Close()
	latest := make(chan protocol.Frame, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-latest:
				n := 8 + len(f.Pixels)
				h := make([]byte, 12)
				binary.BigEndian.PutUint32(h, uint32(n))
				binary.BigEndian.PutUint32(h[4:], uint32(f.Width))
				binary.BigEndian.PutUint32(h[8:], uint32(f.Height))
				video.SetWriteDeadline(time.Now().Add(3 * time.Second))
				buffers := net.Buffers{h, f.Pixels}
				if _, e := buffers.WriteTo(video); e != nil {
					cancel()
					return
				}
			}
		}
	}()
	done := make(chan error, 1)
	go func() {
		done <- session.Run(ctx, func(f protocol.Frame) {
			f.Pixels = append([]byte(nil), f.Pixels...)
			select {
			case latest <- f:
			default:
				select {
				case <-latest:
				default:
				}
				select {
				case latest <- f:
				default:
				}
			}
		})
	}()
	emit(Event{Type: "state", State: "connected"})
	defer func() { cancel() }()
	mediaTick := time.NewTicker(500 * time.Millisecond)
	defer mediaTick.Stop()
	wasMounted := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-mediaTick.C:
			mounted := session.Mounted()
			if mounted != wasMounted {
				wasMounted = mounted
				emit(Event{Type: "media", Mounted: &mounted})
			}
		case e := <-done:
			if e != nil && ctx.Err() == nil {
				emit(Event{Type: "error", State: "disconnected", Message: e.Error()})
			}
			return e
		case c, ok := <-cmds:
			if !ok {
				return io.EOF
			}
			var err error
			switch c.Op {
			case "disconnect":
				session.Release()
				return nil
			case "key":
				err = session.Key(c.Input)
			case "pointer":
				err = session.Pointer(c.Input)
			case "release":
				err = session.Release()
			case "mount":
				err = session.Mount(ctx, c.Path)
			case "eject":
				err = session.Eject()
			case "power":
				opctx, stop := context.WithTimeout(ctx, 15*time.Second)
				err = session.Power(opctx, c.Action)
				stop()
			default:
				err = fmt.Errorf("unknown command")
			}
			if err != nil {
				emit(Event{ID: c.ID, Type: "error", Message: err.Error()})
			} else {
				ev := Event{ID: c.ID, Type: "ok"}
				if c.Op == "mount" || c.Op == "eject" {
					mounted := session.Mounted()
					wasMounted = mounted
					ev.Mounted = &mounted
				}
				emit(ev)
			}
		}
	}
}
