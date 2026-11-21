package backend

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"oldipmi/internal/protocol"
	"sync"
	"sync/atomic"
	"time"
)

const Version = 1
const MaxControl = 1 << 20

// The refusals for a console without virtual media or power control.
var (
	errNoMedia = errors.New("virtual media is not available for this console type")
	errNoPower = errors.New("power control is not available for this console type")
)

// How long the desktop may leave an event or a video frame unread before the
// session ends. A frame is several MB and is only taken while the GUI's event
// loop runs, so a busy machine, or the GUI blocked for a moment, must not end the
// console; the deadline still ends a session whose GUI stopped reading altogether.
const writeTimeout = 30 * time.Second

// The time limits of the slow commands. Both stay below the 60 seconds that
// oldipmi-mcp waits for an answer (internal/mcp/client.go), so that an agent
// hears about a stuck BMC too. Tests shorten them.
var (
	mountTimeout = 30 * time.Second
	powerTimeout = 15 * time.Second
)

type Command struct {
	Version  int                  `json:"version"`
	ID       int                  `json:"id"`
	Op       string               `json:"op"`
	Profile  protocol.Profile     `json:"profile"`
	Password string               `json:"password"`
	Input    protocol.Input       `json:"input"`
	Path     string               `json:"path"`
	Action   protocol.PowerAction `json:"action"`
}
type Event struct {
	Version int                   `json:"version"`
	ID      int                   `json:"id,omitempty"`
	Type    string                `json:"type"`
	State   string                `json:"state,omitempty"`
	Message string                `json:"message,omitempty"`
	Mounted *bool                 `json:"mounted,omitempty"`
	Info    *protocol.ConsoleInfo `json:"info,omitempty"`
	// Not sent yet; defined here once for what follows:
	Stage string `json:"stage,omitempty"` // the setup stage an error ended
	Code  string `json:"code,omitempty"`  // the class of an error, for hints
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
func Serve(parent context.Context, control, video net.Conn) error {
	return serve(parent, control, video, Open)
}
func serve(parent context.Context, control, video net.Conn, open func(context.Context, protocol.Profile, string) (protocol.Console, error)) error {
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
		control.SetWriteDeadline(time.Now().Add(writeTimeout))
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

			if c.Op == OpDisconnect && opening.Load() {
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
	if first.Op != OpConnect {
		return fmt.Errorf("first IPC command must connect")
	}
	stage := protocol.StageLoggingIn
	emit(Event{Type: EventState, State: stage})
	setupCtx := protocol.WithProgress(ctx, func(p protocol.Progress) {
		stage = p.Stage
		emit(Event{Type: EventState, State: stage, Info: p.Info})
	})
	session, e := open(setupCtx, first.Profile, first.Password)
	opening.Store(false)
	first.Password = ""
	if e != nil {
		if ctx.Err() == nil {
			emit(Event{Type: EventError, State: StateDisconnected, Message: stageLabel[stage] + " failed: " + e.Error()})
		}
		return e
	}
	defer session.Close()
	// Virtual media and power control are optional: without them, a mount or
	// power command is refused, and an eject finds nothing mounted.
	media, _ := session.(protocol.Media)
	power, _ := session.(protocol.Power)
	isMounted := func() bool { return media != nil && media.Mounted() }
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
				video.SetWriteDeadline(time.Now().Add(writeTimeout))
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
	emit(Event{Type: EventState, State: StateConnected})
	// Mount, eject and power can take seconds. They run off this loop, one at
	// a time and in the order they came, so that input goes on meanwhile; the
	// desktop matches their replies by id. Only this loop sends replies, and
	// the session closes only after the command in flight has returned.
	slow := make(chan slowResult, 1)
	var queue []Command
	var inFlight *Command
	var lane sync.WaitGroup
	defer lane.Wait()
	startSlow := func() {
		if inFlight != nil || len(queue) == 0 {
			return
		}
		c := queue[0]
		queue = queue[1:]
		inFlight = &c
		lane.Add(1)
		go func() {
			defer lane.Done()
			slow <- slowResult{c, runSlow(ctx, media, power, c)}
		}()
	}
	wasMounted := false
	reply := func(c Command, err error) {
		if err != nil {
			emit(Event{ID: c.ID, Type: EventError, Message: err.Error()})
			return
		}
		ev := Event{ID: c.ID, Type: EventOK}
		if c.Op == OpMount || c.Op == OpEject {
			mounted := isMounted()
			wasMounted = mounted
			ev.Mounted = &mounted
		}
		emit(ev)
	}
	disconnecting := false
	defer func() { cancel() }()
	mediaTick := time.NewTicker(500 * time.Millisecond)
	defer mediaTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-mediaTick.C:
			if inFlight != nil && inFlight.Op != OpPower {
				continue // the reply of the mount or eject reports the media
			}
			mounted := isMounted()
			if mounted != wasMounted {
				wasMounted = mounted
				emit(Event{Type: EventMedia, Mounted: &mounted})
			}
		case e := <-done:
			if inFlight != nil {
				// The reply of the command in flight comes first, as it did
				// when the command ran in this loop.
				r := <-slow
				reply(r.cmd, r.err)
			}
			if e != nil && ctx.Err() == nil {
				emit(Event{Type: EventError, State: StateDisconnected, Message: e.Error()})
			}
			return e
		case r := <-slow:
			inFlight = nil
			reply(r.cmd, r.err)
			startSlow()
			if disconnecting && inFlight == nil {
				session.Release()
				return nil
			}
		case c, ok := <-cmds:
			if !ok {
				return io.EOF
			}
			if disconnecting {
				continue // nothing after a disconnect is carried out
			}
			var err error
			switch c.Op {
			case OpMount, OpEject, OpPower:
				queue = append(queue, c)
				startSlow()
				continue
			case OpDisconnect:
				if inFlight != nil {
					disconnecting = true // once the slow commands before it are done
					continue
				}
				session.Release()
				return nil
			case OpKey:
				err = session.Key(c.Input)
			case OpPointer:
				err = session.Pointer(c.Input)
			case OpRelease:
				err = session.Release()
			default:
				err = fmt.Errorf("unknown command")
			}
			reply(c, err)
		}
	}
}

type slowResult struct {
	cmd Command
	err error
}

// runSlow carries out a mount, eject or power command, a mount or power
// command within its time limit.
func runSlow(ctx context.Context, media protocol.Media, power protocol.Power, c Command) error {
	switch c.Op {
	case OpMount:
		if media == nil {
			return errNoMedia
		}
		return withTimeout(ctx, mountTimeout, "the ISO was not mounted", func(ctx context.Context) error {
			return media.Mount(ctx, c.Path)
		})
	case OpEject:
		if media == nil {
			return nil
		}
		return media.Eject()
	default: // OpPower
		if power == nil {
			return errNoPower
		}
		return withTimeout(ctx, powerTimeout, "the power command did not finish", func(ctx context.Context) error {
			return power.Power(ctx, c.Action)
		})
	}
}

// withTimeout runs op within d. When op fails because d ran out, the error
// says so in front of op's own.
func withTimeout(ctx context.Context, d time.Duration, what string, op func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	err := op(ctx)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s within %v: %w", what, d, err)
	}
	return err
}
