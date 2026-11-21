package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// Tool is one MCP tool.
type Tool struct {
	Name        string
	Title       string
	Description string
	Schema      map[string]any
	// Global tools are not tied to one console, so they take no profile
	// selector; connect takes a profile of its own.
	Global bool
	// ReadOnly tools never change the console; Destructive tools may affect the host irreversibly.
	ReadOnly, Destructive, Idempotent bool
	Run                               func(ctx context.Context, b Backend, args map[string]any) ([]Content, error)
}

func (t Tool) describe() map[string]any {
	ann := map[string]any{"title": t.Title, "readOnlyHint": t.ReadOnly, "openWorldHint": !t.ReadOnly}
	if !t.ReadOnly {
		ann["destructiveHint"] = t.Destructive
		ann["idempotentHint"] = t.Idempotent
	}
	return map[string]any{"name": t.Name, "title": t.Title, "description": t.Description, "inputSchema": t.Schema, "annotations": ann}
}

// unknownArgs names arguments the schema does not declare (additionalProperties
// is false everywhere), or returns "".
func (t Tool) unknownArgs(args map[string]any) string {
	props, _ := t.Schema["properties"].(map[string]any)
	var bad []string
	for k := range args {
		if _, ok := props[k]; !ok {
			bad = append(bad, k)
		}
	}
	if len(bad) == 0 {
		return ""
	}
	sort.Strings(bad)
	allowed := make([]string, 0, len(props))
	for k := range props {
		allowed = append(allowed, k)
	}
	sort.Strings(allowed)
	hint := "it takes no arguments"
	if len(allowed) > 0 {
		hint = "allowed: " + strings.Join(allowed, ", ")
	}
	return fmt.Sprintf("Unknown argument %s for %s (%s).", strings.Join(bad, ", "), t.Name, hint)
}

const (
	maxTextLen      = 4096
	maxWaitMS       = 300000
	pollInterval    = 100 * time.Millisecond
	defaultKeyGap   = 8
	defaultMaxImage = 1568
)

// sleep waits d or until ctx is cancelled; tests replace it.
var sleep = func(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func text(s string) []Content { return []Content{{Type: "text", Text: s}} }

func call(ctx context.Context, b Backend, op string, args map[string]any) (json.RawMessage, error) {
	if args == nil {
		args = map[string]any{}
	}
	return b.Call(ctx, op, args)
}

func getStatus(ctx context.Context, b Backend) (statusReply, error) {
	raw, err := call(ctx, b, opStatus, nil)
	if err != nil {
		return statusReply{}, err
	}
	return readReply[statusReply](opStatus, raw)
}

func pretty(raw json.RawMessage) []Content {
	var v any
	if json.Unmarshal(raw, &v) == nil {
		b, _ := json.MarshalIndent(v, "", "  ")
		return text(string(b))
	}
	return text(string(raw))
}

// simple builds a tool that forwards validated arguments to one GUI operation.
func simple(t Tool, op string) Tool { return simpleDone(t, op, "") }

// simpleDone is simple with a sentence for an empty reply, so an agent reads
// what happened instead of "{}".
func simpleDone(t Tool, op, done string) Tool {
	t.Run = func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
		raw, err := call(ctx, b, op, a)
		if err != nil {
			return nil, err
		}
		if done != "" && saidNothing(raw) {
			return text(done), nil
		}
		return pretty(raw), nil
	}
	return t
}

// saidNothing reports whether the app's reply has nothing to show: an empty
// object, or only an empty message, which is how it answers mount and eject.
func saidNothing(raw json.RawMessage) bool {
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil {
		return false
	}
	if v["message"] == "" {
		delete(v, "message")
	}
	return len(v) == 0
}

// poll calls check every pollInterval until it reports done or timeoutMS passes.
func poll(ctx context.Context, timeoutMS int, check func() (bool, error)) (bool, error) {
	start, waited := time.Now(), 0
	for {
		done, err := check()
		if err != nil || done {
			return done, err
		}
		if waited >= timeoutMS || time.Since(start) >= time.Duration(timeoutMS)*time.Millisecond {
			return false, nil
		}
		if err := sleep(ctx, pollInterval); err != nil {
			return false, err
		}
		waited += int(pollInterval / time.Millisecond)
	}
}

// profileArg is the optional selector every session-bound tool takes: several
// consoles can be open at once, so a call names the one it means.
var profileArg = strSpec{Key: "profile", Desc: "Which open console to act on: profile name, id or host. " +
	"Optional while exactly one console is open; required when several are (see status → sessions)."}

// scoped sends every call with args.profile set, unless the call names one itself.
type scoped struct {
	Backend
	profile string
}

func (s scoped) Call(ctx context.Context, op string, args map[string]any) (json.RawMessage, error) {
	if _, ok := args["profile"]; !ok && s.profile != "" {
		cp := make(map[string]any, len(args)+1)
		for k, v := range args {
			cp[k] = v
		}
		cp["profile"] = s.profile
		args = cp
	}
	return s.Backend.Call(ctx, op, args)
}

// sessionBound adds the optional profile argument to a tool and routes all its
// backend calls (status polls, key events, cleanup) to that console.
func sessionBound(t Tool) Tool {
	if t.Global {
		return t
	}
	props := map[string]any{}
	for k, v := range t.Schema["properties"].(map[string]any) {
		props[k] = v
	}
	props["profile"] = profileArg.schema()
	schema := map[string]any{}
	for k, v := range t.Schema {
		schema[k] = v
	}
	schema["properties"] = props
	t.Schema = schema
	run := t.Run
	t.Run = func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
		name, err := profileArg.get(a)
		if err != nil {
			return nil, err
		}
		if name != "" {
			b = scoped{b, name}
		}
		return run(ctx, b, a)
	}
	return t
}

func tools() []Tool {
	all := rawTools()
	for i := range all {
		all[i] = sessionBound(all[i])
	}
	return all
}

// rawTools are the tools in the order agents see them; tools adds the profile
// selector.
func rawTools() []Tool {
	return slices.Concat(sessionTools(), screenTools(), inputTools(), hostTools())
}
