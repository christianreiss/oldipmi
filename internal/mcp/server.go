// Package mcp implements a minimal stdio Model Context Protocol server that
// forwards tool calls to the running OldIPMI desktop application.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Backend performs one operation in the OldIPMI desktop application.
type Backend interface {
	Call(op string, args map[string]any) (json.RawMessage, error)
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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
type toolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

type Server struct {
	Backend Backend
	Version string
	// Instructions is sent to clients in the initialize result.
	Instructions string
	tools        []Tool
	byName       map[string]Tool
}

func NewServer(b Backend, version, instructions string) *Server {
	s := &Server{Backend: b, Version: version, Instructions: instructions}
	s.tools = tools()
	s.byName = map[string]Tool{}
	for _, t := range s.tools {
		s.byName[t.Name] = t
	}
	return s
}

var supported = map[string]bool{"2025-06-18": true, "2025-03-26": true, "2024-11-05": true}

// Serve reads newline-delimited JSON-RPC from r until EOF and writes replies to w.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	br := bufio.NewReaderSize(r, 1<<20)
	var mu sync.Mutex
	write := func(v any) {
		b, _ := json.Marshal(v)
		mu.Lock()
		defer mu.Unlock()
		w.Write(append(b, '\n'))
	}
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			var req request
			if e := json.Unmarshal(line, &req); e != nil {
				write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			} else if len(req.ID) == 0 {
				// Notification (e.g. notifications/initialized): no reply.
			} else {
				write(s.handle(req))
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (s *Server) handle(req request) response {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &p)
		v := "2025-06-18"
		if supported[p.ProtocolVersion] {
			v = p.ProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "oldipmi", "version": s.Version},
			"instructions":    s.Instructions,
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		list := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.Schema})
		}
		resp.Result = map[string]any{"tools": list}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if e := json.Unmarshal(req.Params, &p); e != nil {
			resp.Error = &rpcError{-32602, "invalid params"}
			break
		}
		t, ok := s.byName[p.Name]
		if !ok {
			resp.Error = &rpcError{-32602, "unknown tool " + p.Name}
			break
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		content, e := t.Run(s.Backend, p.Arguments)
		if e != nil {
			resp.Result = toolResult{Content: []Content{{Type: "text", Text: e.Error()}}, IsError: true}
		} else {
			resp.Result = toolResult{Content: content}
		}
	default:
		resp.Error = &rpcError{-32601, fmt.Sprintf("method not found: %s", req.Method)}
	}
	return resp
}
