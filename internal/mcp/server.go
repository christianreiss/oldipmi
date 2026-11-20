// Package mcp implements a stdio Model Context Protocol server that forwards
// tool calls to the running OldIPMI desktop application.
//
// It speaks both protocol eras: the legacy initialize handshake (2024-11-05 ..
// 2025-11-25) and the stateless 2026-07-28 revision, where every request names
// its protocol version in params._meta and clients probe with server/discover.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"
)

// ShutdownGrace is how long running tools may continue after stdin closes.
var ShutdownGrace = 5 * time.Second

// Backend performs one operation in the OldIPMI desktop application.
type Backend interface {
	Call(op string, args map[string]any) (json.RawMessage, error)
}

// Greeter is implemented by backends that tell the app which MCP client is attached.
type Greeter interface {
	Hello(name, version, protocol string)
}

const (
	modernVersion  = "2026-07-28"
	maxRequestLine = 8 << 20
	metaVersion    = "io.modelcontextprotocol/protocolVersion"
	metaCaps       = "io.modelcontextprotocol/clientCapabilities"
	metaClientInfo = "io.modelcontextprotocol/clientInfo"
	metaServerInfo = "io.modelcontextprotocol/serverInfo"
)

// legacyVersions are the protocol revisions a client negotiates with
// initialize, newest first. A request that names its revision in _meta speaks
// modernVersion, the only one server/discover lists.
var legacyVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// JSON-RPC and MCP error codes.
const (
	codeParse       = -32700
	codeInvalidReq  = -32600
	codeNoMethod    = -32601
	codeParams      = -32602
	codeInternal    = -32603
	codeUnsupported = -32022
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Content is one MCP tool result block.
type Content struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type Server struct {
	Backend Backend
	Version string
	// Instructions is sent to clients in the initialize and server/discover results.
	Instructions string
	tools        []Tool
	byName       map[string]Tool

	// Tools run one at a time in arrival order (tickets), so input from
	// pipelined calls never interleaves or reorders on the console.
	turnMu         sync.Mutex
	turn           *sync.Cond
	issued, served uint64
	mu             sync.Mutex
	inflight       map[string]*job
	greeted        string
	wg             sync.WaitGroup
	write          func(any)
}

func NewServer(b Backend, version, instructions string) *Server {
	s := &Server{Backend: b, Version: version, Instructions: instructions, inflight: map[string]*job{}}
	s.turn = sync.NewCond(&s.turnMu)
	s.tools = tools()
	s.byName = map[string]Tool{}
	for _, t := range s.tools {
		s.byName[t.Name] = t
	}
	return s
}

// Serve reads newline-delimited JSON-RPC from r until EOF and writes replies to w.
// Requests are read continuously, so ping and notifications/cancelled are handled
// while a long tool call (connect, waits) is running.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	var wmu sync.Mutex
	s.write = func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		wmu.Lock()
		defer wmu.Unlock()
		w.Write(append(b, '\n'))
	}
	defer func() {
		// stdin closed: let running tools finish briefly, then cancel them.
		idle := make(chan struct{})
		go func() { s.wg.Wait(); close(idle) }()
		select {
		case <-idle:
			return
		case <-time.After(ShutdownGrace):
		}
		s.mu.Lock()
		for _, j := range s.inflight {
			j.cancel()
		}
		s.mu.Unlock()
		<-idle
	}()
	br := bufio.NewReaderSize(r, 1<<16)
	for {
		line, tooLong, err := readLine(br)
		if tooLong {
			s.write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: codeInvalidReq, Message: "request exceeds 8 MiB"}})
		} else if len(bytes.TrimSpace(line)) > 0 {
			s.receive(bytes.TrimSpace(line))
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func readLine(br *bufio.Reader) (line []byte, tooLong bool, err error) {
	for {
		chunk, e := br.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(chunk) > maxRequestLine {
				tooLong, line = true, nil
			} else {
				line = append(line, chunk...)
			}
		}
		if e != bufio.ErrBufferFull {
			return line, tooLong, e
		}
	}
}

func (s *Server) receive(raw []byte) {
	if raw[0] != '[' {
		if resp := s.handle(raw, func(r *response) {
			if r != nil {
				s.write(r)
			}
		}); resp != nil {
			s.write(resp)
		}
		return
	}
	// JSON-RPC batch (2025-03-26 clients): answer with one array once all parts are done.
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		s.write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: codeParse, Message: "parse error"}})
		return
	}
	if len(parts) == 0 {
		s.write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: codeInvalidReq, Message: "empty batch"}})
		return
	}
	var mu sync.Mutex
	var out []*response
	var pending sync.WaitGroup
	for _, p := range parts {
		pending.Add(1)
		resp := s.handle(p, func(r *response) {
			if r != nil {
				mu.Lock()
				out = append(out, r)
				mu.Unlock()
			}
			pending.Done()
		})
		if resp != nil {
			mu.Lock() // tool calls of earlier parts may be appending already
			out = append(out, resp)
			mu.Unlock()
		}
		if resp != nil || !s.isAsync(p) {
			pending.Done()
		}
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		pending.Wait()
		mu.Lock()
		defer mu.Unlock()
		if len(out) > 0 {
			s.write(out)
		}
	}()
}

// isAsync reports whether handle answered this message through its callback.
func (s *Server) isAsync(raw json.RawMessage) bool {
	var m message
	return json.Unmarshal(raw, &m) == nil && m.Method == "tools/call" && len(m.ID) > 0 && string(m.ID) != "null"
}

func errResp(id json.RawMessage, code int, msg string, data any) *response {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg, Data: data}}
}

// handle processes one message. It returns the reply, or nil when there is none
// yet (notifications, or tools/call which replies later through async).
func (s *Server) handle(raw []byte, async func(*response)) *response {
	var m message
	if err := json.Unmarshal(raw, &m); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return errResp(nil, codeParse, "parse error", nil)
		}
		return errResp(nil, codeInvalidReq, "invalid request", nil)
	}
	isNotification := len(m.ID) == 0
	if m.Method == "" {
		if len(m.Result) > 0 || len(m.Error) > 0 {
			return nil // a response to a server request; this server sends none
		}
		if isNotification {
			return nil
		}
		return errResp(m.ID, codeInvalidReq, "invalid request: method missing", nil)
	}
	if m.JSONRPC != "2.0" {
		if isNotification {
			return nil
		}
		return errResp(m.ID, codeInvalidReq, `invalid request: jsonrpc must be "2.0"`, nil)
	}
	if isNotification {
		if m.Method == "notifications/cancelled" {
			var p struct {
				RequestID json.RawMessage `json:"requestId"`
			}
			if json.Unmarshal(m.Params, &p) == nil {
				s.mu.Lock()
				if j, ok := s.inflight[string(p.RequestID)]; ok {
					j.cancel()
				}
				s.mu.Unlock()
			}
		}
		return nil // notifications/initialized and unknown notifications need no reply
	}
	if c := m.ID[0]; c != '"' && c != '-' && (c < '0' || c > '9') {
		return errResp(nil, codeInvalidReq, "invalid request: id must be a string or number", nil)
	}
	var p struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if len(m.Params) > 0 && json.Unmarshal(m.Params, &p) != nil {
		return errResp(m.ID, codeParams, "invalid params: must be an object", nil)
	}
	modern := false
	if v, ok := p.Meta[metaVersion]; ok {
		var ver string
		json.Unmarshal(v, &ver)
		if ver != modernVersion {
			return errResp(m.ID, codeUnsupported, "Unsupported protocol version", map[string]any{"supported": []string{modernVersion}, "requested": ver})
		}
		if _, ok := p.Meta[metaCaps]; !ok {
			return errResp(m.ID, codeParams, "invalid params: _meta."+metaCaps+" is required", nil)
		}
		modern = true
		var ci struct{ Name, Version string }
		if json.Unmarshal(p.Meta[metaClientInfo], &ci) == nil {
			s.greet(ci.Name, ci.Version, modernVersion)
		}
	}
	ok := func(result map[string]any) *response {
		if modern {
			result["resultType"] = "complete"
		}
		return &response{JSONRPC: "2.0", ID: m.ID, Result: result}
	}
	switch m.Method {
	case "initialize":
		var ip struct {
			ProtocolVersion string `json:"protocolVersion"`
			ClientInfo      struct{ Name, Version string }
		}
		json.Unmarshal(m.Params, &ip)
		v := legacyVersions[0]
		if slices.Contains(legacyVersions, ip.ProtocolVersion) {
			v = ip.ProtocolVersion
		}
		s.greet(ip.ClientInfo.Name, ip.ClientInfo.Version, v)
		return ok(map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      s.serverInfo(),
			"instructions":    s.Instructions,
		})
	case "server/discover":
		return &response{JSONRPC: "2.0", ID: m.ID, Result: map[string]any{
			"resultType":        "complete",
			"supportedVersions": []string{modernVersion},
			"capabilities":      map[string]any{"tools": map[string]any{}},
			"_meta":             map[string]any{metaServerInfo: s.serverInfo()},
			"instructions":      s.Instructions,
			"ttlMs":             3600000,
			"cacheScope":        "public",
		}}
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		list := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			list = append(list, t.describe())
		}
		r := map[string]any{"tools": list}
		if modern {
			r["ttlMs"], r["cacheScope"] = 3600000, "public"
		}
		return ok(r)
	case "tools/call":
		var cp struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(m.Params, &cp) != nil || cp.Name == "" {
			return errResp(m.ID, codeParams, "invalid params: name is required", nil)
		}
		t, found := s.byName[cp.Name]
		if !found {
			return errResp(m.ID, codeParams, "Unknown tool: "+cp.Name, nil)
		}
		args := map[string]any{}
		if len(cp.Arguments) > 0 && string(cp.Arguments) != "null" && json.Unmarshal(cp.Arguments, &args) != nil {
			return errResp(m.ID, codeParams, "invalid params: arguments must be an object", nil)
		}
		// A misspelt argument would otherwise be ignored and its default used.
		if msg := t.unknownArgs(args); msg != "" {
			return ok(map[string]any{"content": []Content{{Type: "text", Text: msg}}, "isError": true})
		}
		s.start(m.ID, t, args, func(result map[string]any) {
			if result == nil {
				async(nil)
			} else {
				async(ok(result))
			}
		})
		return nil
	}
	return errResp(m.ID, codeNoMethod, "method not found: "+m.Method, nil)
}

func (s *Server) serverInfo() map[string]any {
	return map[string]any{"name": "oldipmi", "title": "OldIPMI console", "version": s.Version}
}

func (s *Server) greet(name, version, protocol string) {
	g, ok := s.Backend.(Greeter)
	if !ok || name == "" {
		return
	}
	s.mu.Lock()
	key := name + "\x00" + version
	fresh := s.greeted != key
	s.greeted = key
	s.mu.Unlock()
	if fresh {
		go g.Hello(name, version, protocol) // best effort; never delays the reply
	}
}

// start runs a tool in the background. Argument and execution failures become
// isError results so the model can correct itself; protocol problems (unknown
// tool, malformed params) were already answered with JSON-RPC errors.
func (s *Server) start(id json.RawMessage, t Tool, args map[string]any, reply func(map[string]any)) {
	ctx, cancel := context.WithCancel(context.Background())
	key, j := string(id), &job{cancel: cancel}
	s.mu.Lock()
	if old, dup := s.inflight[key]; dup {
		old.cancel() // a reused id supersedes the earlier request
	}
	s.inflight[key] = j
	s.mu.Unlock()
	s.turnMu.Lock()
	ticket := s.issued
	s.issued++
	s.turnMu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		s.turnMu.Lock()
		for s.served != ticket {
			s.turn.Wait()
		}
		s.turnMu.Unlock()
		var content []Content
		var err error
		if ctx.Err() == nil {
			content, err = safeRun(ctx, t, s.Backend, args)
		}
		s.turnMu.Lock()
		s.served++
		s.turn.Broadcast()
		s.turnMu.Unlock()
		s.mu.Lock()
		if s.inflight[key] == j {
			delete(s.inflight, key)
		}
		s.mu.Unlock()
		if ctx.Err() != nil {
			reply(nil) // cancelled: the spec says not to answer
			return
		}
		res := map[string]any{"content": content}
		if err != nil {
			msg := err.Error()
			if msg == "" { // a text block must have text
				msg = "The operation failed without an error message."
			}
			res = map[string]any{"content": []Content{{Type: "text", Text: msg}}, "isError": true}
		}
		reply(res)
	}()
}

type job struct{ cancel context.CancelFunc }

func safeRun(ctx context.Context, t Tool, b Backend, args map[string]any) (c []Content, err error) {
	defer func() {
		if r := recover(); r != nil {
			c, err = nil, fmt.Errorf("internal error in %s: %v", t.Name, r)
		}
	}()
	return t.Run(ctx, b, args)
}
