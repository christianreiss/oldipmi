package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// fakeApp stands in for the desktop's agent socket, in a temporary directory.
// It records every request line per connection and answers each with the
// lines answer returns. A "CLOSE" line closes the connection after the lines
// before it; nil closes it without an answer. Each close is sent on closed.
type fakeApp struct {
	path   string
	ln     net.Listener
	answer func(conn int, req map[string]any) []string
	mu     sync.Mutex
	reqs   [][]string
	closed chan int
}

func newFakeApp(t *testing.T, answer func(conn int, req map[string]any) []string) *fakeApp {
	t.Helper()
	a := &fakeApp{path: filepath.Join(t.TempDir(), "agent.sock"), answer: answer, closed: make(chan int, 8)}
	var err error
	if a.ln, err = net.Listen("unix", a.path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.ln.Close() })
	go func() {
		for n := 0; ; n++ {
			c, err := a.ln.Accept()
			if err != nil {
				return
			}
			a.mu.Lock()
			a.reqs = append(a.reqs, nil)
			a.mu.Unlock()
			go a.serve(n, c)
		}
	}()
	return a
}

func (a *fakeApp) serve(n int, c net.Conn) {
	defer c.Close()
	rd := bufio.NewReader(c)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return
		}
		var req map[string]any
		json.Unmarshal(line, &req)
		a.mu.Lock()
		a.reqs[n] = append(a.reqs[n], strings.TrimSpace(string(line)))
		a.mu.Unlock()
		lines := a.answer(n, req)
		if lines == nil {
			lines = []string{"CLOSE"}
		}
		for _, l := range lines {
			if l == "CLOSE" {
				c.Close()
				a.closed <- n
				return
			}
			c.Write([]byte(l + "\n"))
		}
	}
}

// requests are the request lines each connection received.
func (a *fakeApp) requests() [][]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([][]string(nil), a.reqs...)
}

func answer(req map[string]any, result string) string {
	return fmt.Sprintf(`{"id":%v,"ok":true,"result":%s}`, req["id"], result)
}

const hello = `{"args":{"name":"fixture-agent","protocol":"2025-06-18","version":"1.0"},"id":%d,"op":"hello"}`

func TestClientWithoutTheApp(t *testing.T) {
	c := &Client{Path: filepath.Join(t.TempDir(), "agent.sock")}
	if _, err := c.Call("status", map[string]any{}); err != ErrNotRunning {
		t.Fatalf("got %v, want ErrNotRunning", err)
	}
}

// The client introduces itself first, reads past lines that are not its
// reply (the app's answer to a line it could not parse, a reply to an earlier
// request, garbage) and returns the app's refusals as errors.
func TestClientRequestsAndReplies(t *testing.T) {
	a := newFakeApp(t, func(_ int, req map[string]any) []string {
		switch req["op"] {
		case "hello":
			return []string{answer(req, `{"app":"OldIPMI","version":"0.4.0"}`)}
		case "key":
			return []string{fmt.Sprintf(`{"id":%v,"ok":false,"error":"Not connected (state: disconnected). Use connect first."}`, req["id"])}
		}
		return []string{
			`{"id":0,"ok":false,"error":"Invalid JSON request."}`,
			`not json`,
			fmt.Sprintf(`{"id":%v,"ok":true,"result":{"stale":true}}`, req["id"].(float64)-1),
			answer(req, `{"state":"connected"}`),
		}
	})
	c := &Client{Path: a.path}
	c.Hello("fixture-agent", "1.0", "2025-06-18")
	raw, err := c.Call("status", map[string]any{"profile": "Demo"})
	if err != nil || string(raw) != `{"state":"connected"}` {
		t.Fatalf("status: %s, %v", raw, err)
	}
	if _, err := c.Call("key", map[string]any{"usage": 4, "down": true}); err == nil || err.Error() != "Not connected (state: disconnected). Use connect first." {
		t.Fatalf("key: %v", err)
	}
	// A hello while connected goes out at once.
	c.Hello("fixture-agent", "1.0", "2025-06-18")
	want := [][]string{{
		fmt.Sprintf(hello, 1),
		`{"args":{"profile":"Demo"},"id":2,"op":"status"}`,
		`{"args":{"down":true,"usage":4},"id":3,"op":"key"}`,
		fmt.Sprintf(hello, 4),
	}}
	if got := a.requests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the app received\n%q\nwant\n%q", got, want)
	}
}

// A request that could not be sent goes out once more, on a new connection
// that starts with the hello. One whose reply was lost does not: it may have
// run. The next request reconnects.
func TestClientReconnects(t *testing.T) {
	a := newFakeApp(t, func(n int, req map[string]any) []string {
		switch {
		case req["op"] == "hello":
			return []string{answer(req, `{}`)}
		case req["op"] == "power":
			return nil // the app ends while the request runs
		case n == 0:
			return []string{answer(req, `{"conn":0}`), "CLOSE"}
		}
		return []string{answer(req, fmt.Sprintf(`{"conn":%d}`, n))}
	})
	c := &Client{Path: a.path}
	c.Hello("fixture-agent", "1.0", "2025-06-18")
	status := func(want string) {
		t.Helper()
		if raw, err := c.Call("status", map[string]any{}); err != nil || string(raw) != want {
			t.Fatalf("status: %s, %v; want %s", raw, err, want)
		}
	}
	status(`{"conn":0}`)
	<-a.closed
	status(`{"conn":1}`) // request 3 cannot be sent on the closed connection
	if _, err := c.Call("power", map[string]any{"action": "soft"}); err == nil || !strings.Contains(err.Error(), "may or may not have run") {
		t.Fatalf("power: %v", err)
	}
	<-a.closed
	status(`{"conn":2}`)
	want := [][]string{
		{fmt.Sprintf(hello, 1), `{"args":{},"id":2,"op":"status"}`},
		{fmt.Sprintf(hello, 4), `{"args":{},"id":5,"op":"status"}`, `{"args":{"action":"soft"},"id":6,"op":"power"}`},
		{fmt.Sprintf(hello, 7), `{"args":{},"id":8,"op":"status"}`},
	}
	if got := a.requests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the app received\n%q\nwant\n%q", got, want)
	}
}

// The one retry ends in ErrNotRunning when the app is gone.
func TestClientGivesUpWhenTheAppIsGone(t *testing.T) {
	a := newFakeApp(t, func(_ int, req map[string]any) []string { return []string{answer(req, `{}`), "CLOSE"} })
	c := &Client{Path: a.path}
	if _, err := c.Call("status", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	<-a.closed
	a.ln.Close()
	if _, err := c.Call("status", map[string]any{}); err != ErrNotRunning {
		t.Fatalf("got %v, want ErrNotRunning", err)
	}
}

// Calls from several goroutines go out one at a time on one connection, and
// each gets its own reply.
func TestClientCallsOneAtATime(t *testing.T) {
	a := newFakeApp(t, func(_ int, req map[string]any) []string {
		args, _ := json.Marshal(req["args"])
		return []string{answer(req, string(args))}
	})
	c := &Client{Path: a.path}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, err := c.Call("status", map[string]any{"n": i})
			if want := fmt.Sprintf(`{"n":%d}`, i); err != nil || string(raw) != want {
				t.Errorf("call %d: %s, %v", i, raw, err)
			}
		}()
	}
	wg.Wait()
	if got := a.requests(); len(got) != 1 || len(got[0]) != 8 {
		t.Fatalf("the app received %q", got)
	}
}

// Where the app listens; desktop/agentapi.cpp (AgentApi::defaultSocketPath)
// picks the same place.
func TestSocketPath(t *testing.T) {
	t.Setenv("OLDIPMI_AGENT_SOCKET", "/srv/agent.sock")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if p := SocketPath(); p != "/srv/agent.sock" {
		t.Errorf("with OLDIPMI_AGENT_SOCKET: %s", p)
	}
	t.Setenv("OLDIPMI_AGENT_SOCKET", "")
	if p := SocketPath(); p != "/run/user/1000/oldipmi/agent.sock" {
		t.Errorf("with XDG_RUNTIME_DIR: %s", p)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if p, want := SocketPath(), filepath.Join(os.TempDir(), fmt.Sprintf("oldipmi-%d", os.Getuid()), "oldipmi", "agent.sock"); p != want {
		t.Errorf("without XDG_RUNTIME_DIR: %s, want %s", p, want)
	}
}

// An app that drops every connection at the hello.
func TestClientLosesTheAppAtTheHello(t *testing.T) {
	a := newFakeApp(t, func(int, map[string]any) []string { return nil })
	c := &Client{Path: a.path}
	c.Hello("fixture-agent", "1.0", "2025-06-18")
	if _, err := c.Call("status", map[string]any{}); err == nil || err.Error() != "connection to OldIPMI lost" {
		t.Fatalf("got %v", err)
	}
	if got := a.requests(); len(got) != 2 || len(got[0]) != 1 || len(got[1]) != 1 || got[1][0] != fmt.Sprintf(hello, 2) {
		t.Fatalf("the app received %q", got)
	}
}

// A socket served by another user is not used: in a directory that user
// controls, it could be their program waiting for an agent's keys (B-12).
func TestClientRefusesAnotherUsersSocket(t *testing.T) {
	a := newFakeApp(t, func(_ int, req map[string]any) []string { return []string{answer(req, `{}`)} })
	uid := ownUID
	ownUID = func() int { return os.Getuid() + 1 }
	t.Cleanup(func() { ownUID = uid })
	c := &Client{Path: a.path}
	c.Hello("fixture-agent", "1.0", "2025-06-18")
	if _, err := c.Call("status", map[string]any{}); err == nil || !strings.Contains(err.Error(), "served by another user") {
		t.Fatalf("got %v", err)
	}
	for _, conn := range a.requests() {
		if len(conn) != 0 {
			t.Fatalf("the other user's socket received %q", conn)
		}
	}
}

// The client tells the app its version and does not send an operation the
// app does not offer: it names both versions instead (B-01). An app from
// before the version check gets every operation, and one warning.
func TestClientChecksTheAppsOperations(t *testing.T) {
	app := func(hello string) *fakeApp {
		return newFakeApp(t, func(_ int, req map[string]any) []string {
			if req["op"] == "hello" {
				return []string{answer(req, hello)}
			}
			return []string{answer(req, `{}`)}
		})
	}
	var log strings.Builder
	a := app(`{"app":"OldIPMI","version":"0.4.0","api":1,"ops":["hello","status"]}`)
	c := &Client{Path: a.path, Version: "0.5.0", Log: &log}
	c.Hello("fixture-agent", "1.0", "2025-06-18")
	if _, err := c.Call(opStatus, map[string]any{}); err != nil {
		t.Fatalf("status: %v", err)
	}
	_, err := c.Call(opPower, map[string]any{"action": "soft"})
	if want := `OldIPMI 0.4.0 does not offer the operation "power" that oldipmi-mcp 0.5.0 uses: start the oldipmi-mcp of OldIPMI 0.4.0, or update OldIPMI`; err == nil || err.Error() != want {
		t.Fatalf("power: %v, want %q", err, want)
	}
	want := [][]string{{
		`{"args":{"mcpVersion":"0.5.0","name":"fixture-agent","protocol":"2025-06-18","version":"1.0"},"id":1,"op":"hello"}`,
		`{"args":{},"id":2,"op":"status"}`,
	}}
	if got := a.requests(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the app received\n%q\nwant\n%q", got, want)
	}
	// An app that speaks a newer agent API gets nothing.
	a = app(`{"app":"OldIPMI","version":"9.0.0","api":2,"ops":["hello","status"]}`)
	c = &Client{Path: a.path, Version: "0.5.0", Log: &log}
	c.Hello("fixture-agent", "1.0", "2025-06-18")
	if _, err := c.Call(opStatus, map[string]any{}); err == nil || !strings.Contains(err.Error(), "OldIPMI 9.0.0 speaks agent API 2, this oldipmi-mcp 0.5.0 only 1") {
		t.Fatalf("newer API: %v", err)
	}
	if log.Len() != 0 {
		t.Errorf("warned: %s", log.String())
	}
	// Before the version check: no api, no ops.
	a = app(`{"app":"OldIPMI","version":"0.4.0"}`)
	c = &Client{Path: a.path, Version: "0.5.0", Log: &log}
	c.Hello("fixture-agent", "1.0", "2025-06-18")
	for range 2 {
		if _, err := c.Call(opPower, map[string]any{"action": "soft"}); err != nil {
			t.Fatalf("power from an older app: %v", err)
		}
	}
	if w := log.String(); strings.Count(w, "\n") != 1 || !strings.Contains(w, "oldipmi-mcp 0.5.0: OldIPMI 0.4.0 does not list its operations") {
		t.Errorf("warning: %q", w)
	}
}
