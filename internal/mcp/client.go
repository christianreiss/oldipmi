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
	rd      *bufio.Reader
	id      int
	hello   map[string]any
	app     *helloReply // the app's answer to the hello on this connection
	warned  bool
}

type reply struct {
	ID     int             `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

// ErrNotRunning is returned when nothing answers on the agent socket.
var ErrNotRunning = errors.New("No OldIPMI window is serving agent control (nothing answers on the agent socket). Ask the user to start OldIPMI, or, if it is open, to switch on Settings → AI agents (MCP) → \"Allow AI agents to control OldIPMI\"")

// Call sends one request and waits for its reply. ctx does not limit the
// wait yet: every round trip has 60 seconds.
func (c *Client) Call(ctx context.Context, op string, args map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; ; attempt++ {
		raw, retry, err := c.once(op, args)
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
		c.greet()
	} else {
		c.dial() // dial sends the hello
	}
}

func (c *Client) drop() {
	if c.conn != nil {
		c.conn.Close()
	}
	c.conn, c.rd, c.app = nil, nil, nil
}

func (c *Client) dial() error {
	conn, err := net.DialTimeout("unix", c.Path, 2*time.Second)
	if err != nil {
		return ErrNotRunning
	}
	if err := servedByUs(conn); err != nil {
		conn.Close()
		return fmt.Errorf("refusing the agent socket %s: %w", c.Path, err)
	}
	c.conn, c.rd = conn, bufio.NewReaderSize(conn, 1<<20)
	if c.hello != nil {
		if err := c.greet(); err != nil && c.conn == nil {
			return errors.New("connection to OldIPMI lost")
		}
	}
	return nil
}

// greet says hello and keeps what the app answers about itself: its version
// and, since the version check, its agent API and the ops it offers.
func (c *Client) greet() error {
	raw, _, err := c.roundTrip(opHello, c.hello)
	if err != nil {
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
func (c *Client) once(op string, args map[string]any) (raw json.RawMessage, retry bool, err error) {
	if c.conn == nil {
		if err := c.dial(); err != nil {
			return nil, false, err
		}
	}
	if err := c.offers(op); err != nil {
		return nil, false, err
	}
	return c.roundTrip(op, args)
}

func (c *Client) roundTrip(op string, args map[string]any) (raw json.RawMessage, retry bool, err error) {
	c.id++
	b, _ := json.Marshal(map[string]any{"id": c.id, "op": op, "args": args})
	c.conn.SetDeadline(time.Now().Add(60 * time.Second))
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		c.drop()
		return nil, true, errors.New("connection to OldIPMI lost")
	}
	for {
		line, err := c.rd.ReadBytes('\n')
		if err != nil {
			c.drop()
			return nil, false, errors.New("connection to OldIPMI lost while waiting for the reply; the operation may or may not have run")
		}
		var r reply
		if json.Unmarshal(line, &r) != nil || r.ID != c.id {
			continue
		}
		if !r.OK {
			return nil, false, errors.New(r.Error)
		}
		return r.Result, false, nil
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
