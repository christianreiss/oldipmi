package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
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
type Client struct {
	Path string
	mu   sync.Mutex
	conn net.Conn
	rd   *bufio.Reader
	id   int
}

type reply struct {
	ID     int             `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
}

func (c *Client) Call(op string, args map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; ; attempt++ {
		raw, err, retry := c.once(op, args)
		if err == nil || !retry || attempt > 0 {
			return raw, err
		}
		c.drop()
	}
}

func (c *Client) drop() {
	if c.conn != nil {
		c.conn.Close()
	}
	c.conn, c.rd = nil, nil
}

// once returns retry=true when the failure happened before the request could have been acted on.
func (c *Client) once(op string, args map[string]any) (json.RawMessage, error, bool) {
	if c.conn == nil {
		conn, err := net.DialTimeout("unix", c.Path, 2*time.Second)
		if err != nil {
			return nil, errors.New("OldIPMI is not running (no agent socket); start the OldIPMI application first"), false
		}
		c.conn, c.rd = conn, bufio.NewReaderSize(conn, 1<<20)
	}
	c.id++
	b, _ := json.Marshal(map[string]any{"id": c.id, "op": op, "args": args})
	c.conn.SetDeadline(time.Now().Add(60 * time.Second))
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		return nil, errors.New("connection to OldIPMI lost"), true
	}
	for {
		line, err := c.rd.ReadBytes('\n')
		if err != nil {
			c.drop()
			return nil, errors.New("connection to OldIPMI lost while waiting for the reply; the operation may or may not have run"), false
		}
		var r reply
		if json.Unmarshal(line, &r) != nil || r.ID != c.id {
			continue
		}
		if !r.OK {
			return nil, errors.New(r.Error), false
		}
		return r.Result, nil, false
	}
}
