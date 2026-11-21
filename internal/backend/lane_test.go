package backend

import (
	"context"
	"io"
	"net"
	"oldipmi/internal/protocol"
	"sync"
	"testing"
	"time"
)

// blockingConsole's Mount waits for release, or for its context, and Power
// waits for its context. Closing end ends Run. It notes whether Close came
// while Mount ran.
type blockingConsole struct {
	release, end chan struct{}
	// settle is how long Mount still takes once it has its answer.
	settle time.Duration

	mu                                     sync.Mutex
	mounting, mounted, closed, closedEarly bool
	releases                               int
}

func newBlockingConsole() *blockingConsole {
	return &blockingConsole{release: make(chan struct{}), end: make(chan struct{})}
}

func (c *blockingConsole) Run(ctx context.Context, _ func(protocol.Frame)) error {
	select {
	case <-c.end:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *blockingConsole) Key(protocol.Input) error     { return nil }
func (c *blockingConsole) Pointer(protocol.Input) error { return nil }
func (c *blockingConsole) Release() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releases++
	return nil
}
func (c *blockingConsole) Mount(ctx context.Context, _ string) error {
	c.set(&c.mounting, true)
	defer c.set(&c.mounting, false)
	select {
	case <-c.release:
		c.set(&c.mounted, true)
		time.Sleep(c.settle)
		return nil
	case <-ctx.Done():
		time.Sleep(c.settle)
		return ctx.Err()
	}
}
func (c *blockingConsole) Eject() error  { c.set(&c.mounted, false); return nil }
func (c *blockingConsole) Mounted() bool { return c.get(&c.mounted) }
func (c *blockingConsole) Power(ctx context.Context, _ protocol.PowerAction) error {
	<-ctx.Done()
	return ctx.Err()
}
func (c *blockingConsole) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed, c.closedEarly = true, c.closedEarly || c.mounting
	return nil
}
func (c *blockingConsole) set(f *bool, v bool) { c.mu.Lock(); *f = v; c.mu.Unlock() }
func (c *blockingConsole) get(f *bool) bool    { c.mu.Lock(); defer c.mu.Unlock(); return *f }

// closedAfterMount checks that the console was closed, only after Mount had
// returned, and released the given number of times.
func (c *blockingConsole) closedAfterMount(t *testing.T, releases int) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed || c.closedEarly || c.releases != releases {
		t.Fatalf("closed %v, while mounting %v, %d releases; want closed after the mount and %d releases", c.closed, c.closedEarly, c.releases, releases)
	}
}

// startServe connects a desktop to serve with console and returns the
// desktop's end and serve's result.
func startServe(t *testing.T, console protocol.Console) (net.Conn, <-chan error) {
	t.Helper()
	control, peer := net.Pipe()
	video, screen := net.Pipe()
	t.Cleanup(func() { peer.Close(); screen.Close() })
	peer.SetDeadline(time.Now().Add(10 * time.Second))
	go io.Copy(io.Discard, screen)
	done := make(chan error, 1)
	go func() {
		done <- serve(context.Background(), control, video, func(context.Context, protocol.Profile, string) (protocol.Console, error) {
			return console, nil
		})
	}()
	writeCommand(t, peer, Command{Op: OpConnect})
	for e := readEvent(t, peer); e.State != StateConnected; e = readEvent(t, peer) {
		if e.State == StateDisconnected {
			t.Fatal(e.Message)
		}
	}
	return peer, done
}

func stillRunning(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("serve ended during the mount: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

// While a mount waits for the BMC, a key is answered. An eject and a
// disconnect wait for the mount: its reply comes first, before any media
// event, then the eject's. The console closes after Mount returned.
func TestInputGoesOnDuringAMount(t *testing.T) {
	console := newBlockingConsole()
	console.settle = 700 * time.Millisecond // more than a media tick
	peer, done := startServe(t, console)
	writeCommand(t, peer, Command{ID: 2, Op: OpMount, Path: "/boot.iso"})
	writeCommand(t, peer, Command{ID: 3, Op: OpKey, Input: protocol.Input{Usage: 4, Down: true}})
	if e := readEvent(t, peer); e.ID != 3 || e.Type != EventOK {
		t.Fatalf("the key's reply: %+v", e)
	}
	writeCommand(t, peer, Command{ID: 4, Op: OpEject})
	writeCommand(t, peer, Command{Op: OpDisconnect})
	stillRunning(t, done)
	close(console.release)
	if e := readEvent(t, peer); e.ID != 2 || e.Type != EventOK || e.Mounted == nil || !*e.Mounted {
		t.Fatalf("the mount's reply: %+v", e)
	}
	if e := readEvent(t, peer); e.ID != 4 || e.Type != EventOK || e.Mounted == nil || *e.Mounted {
		t.Fatalf("the eject's reply: %+v", e)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	console.closedAfterMount(t, 1)
}

// When the console ends during a mount, the mount's reply still comes before
// the end, as when it ran in the command loop, and the console closes after
// Mount returned.
func TestConsoleEndsDuringAMount(t *testing.T) {
	console := newBlockingConsole()
	peer, done := startServe(t, console)
	writeCommand(t, peer, Command{ID: 2, Op: OpMount, Path: "/boot.iso"})
	for deadline := time.Now().Add(5 * time.Second); !console.get(&console.mounting); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("Mount was not called")
		}
	}
	close(console.end)
	stillRunning(t, done)
	close(console.release)
	if e := readEvent(t, peer); e.ID != 2 || e.Type != EventOK {
		t.Fatalf("the mount's reply: %+v", e)
	}
	if e := readEvent(t, peer); e.ID != 0 || e.Type != EventError || e.State != StateDisconnected || e.Message != "EOF" {
		t.Fatalf("the end: %+v", e)
	}
	if err := <-done; err != io.EOF {
		t.Fatal(err)
	}
	console.closedAfterMount(t, 0)
}

// A mount or power command that runs out of time is answered with an error
// that says so.
func TestSlowCommandsHaveATimeLimit(t *testing.T) {
	defer func(m, p time.Duration) { mountTimeout, powerTimeout = m, p }(mountTimeout, powerTimeout)
	mountTimeout, powerTimeout = 50*time.Millisecond, 60*time.Millisecond
	console := newBlockingConsole()
	peer, done := startServe(t, console)
	for _, c := range []struct {
		cmd  Command
		want string
	}{
		{Command{ID: 2, Op: OpMount, Path: "/boot.iso"}, "the ISO was not mounted within 50ms: context deadline exceeded"},
		{Command{ID: 3, Op: OpPower, Action: protocol.PowerOn}, "the power command did not finish within 60ms: context deadline exceeded"},
	} {
		writeCommand(t, peer, c.cmd)
		if e := readEvent(t, peer); e.ID != c.cmd.ID || e.Type != EventError || e.Message != c.want {
			t.Fatalf("%s: %+v, want the error %q", c.cmd.Op, e, c.want)
		}
	}
	writeCommand(t, peer, Command{Op: OpDisconnect})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// When the desktop goes away during a mount, the mount is cancelled, and the
// console closes after Mount returned, although Mount takes a moment to give up.
func TestDesktopGoesAwayDuringAMount(t *testing.T) {
	console := newBlockingConsole()
	console.settle = 50 * time.Millisecond
	peer, done := startServe(t, console)
	writeCommand(t, peer, Command{ID: 2, Op: OpMount, Path: "/boot.iso"})
	for deadline := time.Now().Add(5 * time.Second); !console.get(&console.mounting); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("Mount was not called")
		}
	}
	peer.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not end")
	}
	console.closedAfterMount(t, 0)
}
