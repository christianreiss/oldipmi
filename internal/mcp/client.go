package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"time"
)

// SocketPath is where the OldIPMI desktop application listens.
func SocketPath() string {
	if p := os.Getenv("OLDIPMI_AGENT_SOCKET"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("oldipmi-%d", os.Getuid()))
	}
	return filepath.Join(dir, "oldipmi", "agent.sock")
}

// Client is a Backend that talks to the desktop application over its Unix socket.
// It reconnects on demand and re-introduces the MCP client after every reconnect.
type Client struct {
	Path    string
	Version string    // of this oldipmi-mcp, which the hello tells the app
	Log     io.Writer // for warnings; os.Stderr when nil
	mu      sync.Mutex
	conn    net.Conn
	lines   chan line     // the lines the app sends on conn, read by read
	done    chan struct{} // closed when conn is dropped
	id      int
	hello   map[string]any
	app     *helloReply // the app's answer to the hello on this connection
	warned  bool
	limit   func(op string) time.Duration // replyWithin, unless a test shortens it
}

// noReply is the error of a request whose reply did not come in time.
type noReply struct {
	op    string
	limit time.Duration
}

func (e noReply) Error() string {
	if e.op == opHello {
		return fmt.Sprintf("OldIPMI did not answer the hello within %v", e.limit)
	}
	return fmt.Sprintf("OldIPMI did not answer %s within %v; the operation may or may not have run", e.op, e.limit)
}

// line is one line the app sent, or the error that ended the connection.
type line struct {
	b   []byte
	err error
}

// replyWithin is how long the client waits for the app's reply to op. The app
// answers most operations at once. Power and mount wait for the BMC, which the
// core allows 15 and 30 seconds (internal/backend/server.go); eject takes as
// long as the BMC takes; connect may wait while the wallet asks the user to
// unlock it.
func replyWithin(op string) time.Duration {
	switch op {
	case opConnect, opCreateProfile, opUpdateProfile, opDeleteProfile:
		return 60 * time.Second
	case opMount, opEject:
		return 35 * time.Second
	case opPower:
		return 20 * time.Second
	}
	return 10 * time.Second
}

type reply struct {
	ID     int             `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// ErrNotRunning is returned when nothing answers on the agent socket.
var ErrNotRunning = errors.New("No OldIPMI window is serving agent control (nothing answers on the agent socket). Ask the user to start OldIPMI, or, if it is open, to switch on Settings → AI agents (MCP) → \"Allow AI agents to control OldIPMI\"")

// Call sends one request and waits for its reply, as long as op can take
// (replyWithin) and ctx lasts. A call whose ctx has ended is not sent.
func (c *Client) Call(ctx context.Context, op string, args map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; ; attempt++ {
		raw, retry, err := c.once(ctx, op, args)
		if err == nil || !retry || attempt > 0 {
			return raw, err
		}
		c.drop()
	}
}

// Hello records the MCP client's identity and tells the app (now, or on the next connect).
func (c *Client) Hello(name, version, protocol string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hello = map[string]any{"name": name, "version": version, "protocol": protocol}
	if c.Version != "" {
		c.hello["mcpVersion"] = c.Version
	}
	if c.conn != nil {
		c.greet(context.Background())
	} else {
		c.dial(context.Background()) // dial sends the hello
	}
}

func (c *Client) drop() {
	if c.conn != nil {
		close(c.done)
		c.conn.Close()
	}
	c.conn, c.lines, c.done, c.app = nil, nil, nil, nil
}

func (c *Client) dial(ctx context.Context) error {
	conn, err := net.DialTimeout("unix", c.Path, 2*time.Second)
	if err != nil {
		return ErrNotRunning
	}
	if err := servedByUs(conn); err != nil {
		conn.Close()
		return fmt.Errorf("refusing the agent socket %s: %w", c.Path, err)
	}
	c.conn, c.lines, c.done = conn, make(chan line), make(chan struct{})
	go read(bufio.NewReaderSize(conn, 1<<20), c.lines, c.done)
	if c.hello != nil {
		if err := c.greet(ctx); err != nil && c.conn == nil {
			var late noReply
			if errors.As(err, &late) || ctx.Err() != nil {
				return err
			}
			return errors.New("connection to OldIPMI lost")
		}
	}
	return nil
}

// greet says hello and keeps what the app answers about itself: its version
// and, since the version check, its agent API and the ops it offers. A hello
// without a reply in time ends the connection, so that the next call says
// hello again instead of using a connection the app never described.
func (c *Client) greet(ctx context.Context) error {
	raw, _, err := c.roundTrip(ctx, opHello, c.hello)
	if err != nil {
		var late noReply
		if c.conn != nil && (errors.As(err, &late) || ctx.Err() != nil) {
			c.drop()
		}
		return err
	}
	app, err := readReply[helloReply](opHello, raw)
	if err != nil {
		return err
	}
	c.app = &app
	if app.API == 0 && !c.warned {
		c.warned = true
		log := c.Log
		if log == nil {
			log = os.Stderr
		}
		fmt.Fprintf(log, "oldipmi-mcp %s: OldIPMI %s does not list its operations, as before the version check; one it lacks fails as unknown\n", c.Version, app.Version)
	}
	return nil
}

// offers refuses, naming both versions instead of sending it, an op the app
// said it does not offer, and every op of an app whose agent API is newer
// than this oldipmi-mcp's.
func (c *Client) offers(op string) error {
	switch a := c.app; {
	case a == nil || a.API == 0:
		return nil
	case a.API > agentAPI:
		return fmt.Errorf("OldIPMI %s speaks agent API %d, this oldipmi-mcp %s only %d: start the oldipmi-mcp of OldIPMI %s", a.Version, a.API, c.Version, agentAPI, a.Version)
	case !slices.Contains(a.Ops, op):
		return fmt.Errorf("OldIPMI %s does not offer the operation %q that oldipmi-mcp %s uses: start the oldipmi-mcp of OldIPMI %s, or update OldIPMI", a.Version, op, c.Version, a.Version)
	}
	return nil
}

// once returns retry=true when the failure happened before the request could have been acted on.
func (c *Client) once(ctx context.Context, op string, args map[string]any) (raw json.RawMessage, retry bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if c.conn == nil {
		if err := c.dial(ctx); err != nil {
			return nil, false, err
		}
	}
	if err := c.offers(op); err != nil {
		return nil, false, err
	}
	return c.roundTrip(ctx, op, args)
}

// roundTrip sends one request and waits for its reply. When the time for op
// runs out, or ctx ends, it stops waiting but keeps the connection: a reply
// that comes later is skipped by its id.
func (c *Client) roundTrip(ctx context.Context, op string, args map[string]any) (raw json.RawMessage, retry bool, err error) {
	c.id++
	b, _ := json.Marshal(map[string]any{"id": c.id, "op": op, "args": args})
	limit := replyWithin(op)
	if c.limit != nil {
		limit = c.limit(op)
	}
	c.conn.SetWriteDeadline(time.Now().Add(limit))
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		c.drop()
		return nil, true, errors.New("connection to OldIPMI lost")
	}
	timeout := time.NewTimer(limit)
	defer timeout.Stop()
	for {
		select {
		case l := <-c.lines:
			if l.err != nil {
				c.drop()
				return nil, false, errors.New("connection to OldIPMI lost while waiting for the reply; the operation may or may not have run")
			}
			var r reply
			if json.Unmarshal(l.b, &r) != nil || r.ID != c.id {
				continue // a reply that came too late, or no reply at all
			}
			if !r.OK {
				return nil, false, errors.New(r.Error)
			}
			return r.Result, false, nil
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-timeout.C:
			return nil, false, noReply{op, limit}
		}
	}
}

// read hands each line the app sends to lines, until the connection ends or
// done is closed.
func read(rd *bufio.Reader, lines chan<- line, done <-chan struct{}) {
	for {
		b, err := rd.ReadBytes('\n')
		select {
		case lines <- line{b, err}:
		case <-done:
			return
		}
		if err != nil {
			return
		}
	}
}

// ownUID is this process's user; tests pretend to be someone else.
var ownUID = os.Getuid

// servedByUs checks that the process on the other end of conn runs as this
// user. A socket in a directory another user controls could be served by
// them and see every key an agent types (B-12).
func servedByUs(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return nil
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if credErr != nil {
		return credErr
	}
	if int(cred.Uid) != ownUID() {
		return fmt.Errorf("it is served by another user (uid %d), not by this user's OldIPMI", cred.Uid)
	}
	return nil
}
