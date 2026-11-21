package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"oldipmi/internal/hid"
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

// powerActions are the power tool's actions, in its schema's order; the app's
// agent API takes the same words (testdata/ipc/names.json, "powerActions").
var powerActions = []string{"on", "off", "reset", "soft"}

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

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }
func intProp(desc string, lo, hi int) map[string]any {
	return map[string]any{"type": "integer", "description": desc, "minimum": lo, "maximum": hi}
}

func argInt(a map[string]any, k string, def, lo, hi int) (int, error) {
	v, ok := a[k]
	if !ok || v == nil {
		return def, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) || int(f) < lo || int(f) > hi {
		return 0, fmt.Errorf("%s must be an integer in %d..%d", k, lo, hi)
	}
	return int(f), nil
}
func argReqInt(a map[string]any, k string, lo, hi int) (int, error) {
	if v, ok := a[k]; !ok || v == nil {
		return 0, fmt.Errorf("%s is required", k)
	}
	return argInt(a, k, 0, lo, hi)
}
func argBool(a map[string]any, k string) (bool, error) {
	v, ok := a[k]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be true or false", k)
	}
	return b, nil
}

// argEnum reads one of values, in any case, as values spells it; without a
// value it is def, or required when def is "".
func argEnum(a map[string]any, k string, values []string, def string) (string, error) {
	v, ok := a[k]
	if !ok || v == nil {
		if def == "" {
			return "", fmt.Errorf("%s is required", k)
		}
		return def, nil
	}
	if s, ok := v.(string); ok {
		for _, w := range values {
			if strings.EqualFold(s, w) {
				return w, nil
			}
		}
	}
	return "", fmt.Errorf("%s must be %s or %s", k, strings.Join(values[:len(values)-1], ", "), values[len(values)-1])
}
func argStr(a map[string]any, k string) (string, error) {
	v, ok := a[k]
	if !ok || v == nil {
		return "", fmt.Errorf("%s is required", k)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", fmt.Errorf("%s must be a non-empty string", k)
	}
	return s, nil
}
func text(s string) []Content { return []Content{{Type: "text", Text: s}} }

func call(b Backend, op string, args map[string]any) (json.RawMessage, error) {
	if args == nil {
		args = map[string]any{}
	}
	return b.Call(op, args)
}

func getStatus(b Backend) (statusReply, error) {
	raw, err := call(b, opStatus, nil)
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
	t.Run = func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
		raw, err := call(b, op, a)
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

// input sends key and pointer events and, if the tool stops early (error or
// cancellation), lets go of whatever it still holds. Keys held on purpose with
// key_down are not touched.
type input struct {
	ctx     context.Context
	b       Backend
	held    []int
	buttons int
	x, y    int
}

func (in *input) key(u int, down bool) error {
	if _, err := call(in.b, opKey, map[string]any{"usage": u, "down": down}); err != nil {
		return err
	}
	if down {
		in.held = append(in.held, u)
	} else {
		for i, h := range in.held {
			if h == u {
				in.held = append(in.held[:i], in.held[i+1:]...)
				break
			}
		}
	}
	return nil
}
func (in *input) tap(u, holdMS int) error {
	if err := in.key(u, true); err != nil {
		return err
	}
	if err := sleep(in.ctx, time.Duration(holdMS)*time.Millisecond); err != nil {
		return err
	}
	return in.key(u, false)
}
func (in *input) pointer(x, y, buttons, wheel int) error {
	if _, err := call(in.b, opPointer, map[string]any{"x": x, "y": y, "buttons": buttons, "wheel": wheel}); err != nil {
		return err
	}
	in.x, in.y, in.buttons = x, y, buttons
	return nil
}
func (in *input) cleanup() {
	for i := len(in.held) - 1; i >= 0; i-- {
		call(in.b, opKey, map[string]any{"usage": in.held[i], "down": false})
	}
	in.held = nil
	if in.buttons != 0 {
		call(in.b, opPointer, map[string]any{"x": in.x, "y": in.y, "buttons": 0, "wheel": 0})
		in.buttons = 0
	}
}
func (in *input) wait(ms int) error { return sleep(in.ctx, time.Duration(ms)*time.Millisecond) }

func inputTool(t Tool, run func(in *input, a map[string]any) (string, error)) Tool {
	t.Run = func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
		in := &input{ctx: ctx, b: b}
		msg, err := run(in, a)
		in.cleanup()
		if err != nil {
			return nil, err
		}
		return text(msg), nil
	}
	return t
}

var buttonProp = map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "description": "Mouse button, default left"}

func buttonMask(a map[string]any) (int, error) {
	b, err := argEnum(a, "button", []string{"left", "right", "middle"}, "left")
	return map[string]int{"left": 1, "right": 2, "middle": 4}[b], err
}

func coords(a map[string]any, kx, ky string) (int, int, error) {
	x, err := argReqInt(a, kx, 0, 4095)
	if err != nil {
		return 0, 0, err
	}
	y, err := argReqInt(a, ky, 0, 4095)
	if err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

// keyList reads a list of min..max key names as HID usages; a missing list
// is an empty one.
func keyList(a map[string]any, k string, min, max int) ([]int, error) {
	bad := fmt.Errorf("%s must list %d..%d key names", k, min, max)
	if min == 0 {
		bad = fmt.Errorf("%s must list up to %d key names", k, max)
	}
	arr, ok := a[k].([]any)
	if a[k] == nil {
		arr, ok = nil, true
	}
	if !ok || len(arr) < min || len(arr) > max {
		return nil, bad
	}
	var us []int
	for _, v := range arr {
		n, ok := v.(string)
		if !ok {
			return nil, bad
		}
		u, err := hid.Usage(n)
		if err != nil {
			return nil, err
		}
		us = append(us, u)
	}
	return us, nil
}

// pressCombo presses usages in order, holds, and releases in reverse order.
func pressCombo(in *input, us []int, holdMS int) error {
	for _, u := range us {
		if err := in.key(u, true); err != nil {
			return err
		}
		if err := in.wait(20); err != nil {
			return err
		}
	}
	if err := in.wait(holdMS); err != nil {
		return err
	}
	for i := len(us) - 1; i >= 0; i-- {
		if err := in.key(us[i], false); err != nil {
			return err
		}
		if err := in.wait(20); err != nil {
			return err
		}
	}
	return nil
}

func comboName(us []int) string {
	var n []string
	for _, u := range us {
		n = append(n, hid.Name(u))
	}
	return strings.Join(n, "+")
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

// profileProp is the optional selector every session-bound tool takes: several
// consoles can be open at once, so a call names the one it means.
var profileProp = prop("string", "Which open console to act on: profile name, id or host. "+
	"Optional while exactly one console is open; required when several are (see status → sessions).")

// scoped sends every call with args.profile set, unless the call names one itself.
type scoped struct {
	Backend
	profile string
}

func (s scoped) Call(op string, args map[string]any) (json.RawMessage, error) {
	if _, ok := args["profile"]; !ok && s.profile != "" {
		cp := make(map[string]any, len(args)+1)
		for k, v := range args {
			cp[k] = v
		}
		cp["profile"] = s.profile
		args = cp
	}
	return s.Backend.Call(op, args)
}

// sessionBound adds the optional profile argument to a tool and routes all its
// backend calls (status polls, key events, cleanup) to that console.
func sessionBound(t Tool) Tool {
	switch t.Name {
	case "list_profiles", "list_keys", "wait", "connect": // not tied to one console (connect takes its own profile)
		return t
	}
	props := map[string]any{}
	for k, v := range t.Schema["properties"].(map[string]any) {
		props[k] = v
	}
	props["profile"] = profileProp
	schema := map[string]any{}
	for k, v := range t.Schema {
		schema[k] = v
	}
	schema["properties"] = props
	t.Schema = schema
	run := t.Run
	t.Run = func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
		if v, ok := a["profile"]; ok && v != nil {
			name, isStr := v.(string)
			if !isStr || name == "" {
				return nil, fmt.Errorf("profile must be a non-empty string")
			}
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

func rawTools() []Tool {
	return []Tool{
		simple(Tool{Name: "list_profiles", Title: "List BMC profiles", ReadOnly: true,
			Description: "List the BMC profiles saved in OldIPMI: id, name, host, family, username and whether a password is saved. Never returns passwords.",
			Schema:      obj(map[string]any{})}, opListProfiles),
		simple(Tool{Name: "status", Title: "Console status", ReadOnly: true,
			Description: "Several consoles can be open at once. Always lists them in sessions (profile, state). For one console (the profile argument, or the only open one): connection state (disconnected, logging_in, discovering, connecting, connected), status message, profile, console info, mounted ISO, framebuffer size and screen-change counters. Also whether a password prompt is waiting for the user. With several open and no profile this is an error that names them.",
			Schema:      obj(map[string]any{})}, opStatus),
		connectTool(),
		{Name: "disconnect", Title: "Disconnect console", Idempotent: true,
			Description: "Close one console session (profile argument, or the only open one), or cancel one that is being set up (also dismisses a pending password prompt). all=true closes every open console. Other consoles stay open.",
			Schema:      obj(map[string]any{"all": prop("boolean", "Close every open console instead of one")}),
			Run: func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
				all, err := argBool(a, "all")
				if err != nil {
					return nil, err
				}
				if v, ok := a["profile"]; ok && v != nil && all {
					return nil, fmt.Errorf("give either profile or all, not both")
				}
				args := map[string]any{}
				if all {
					args["all"] = true
				}
				raw, err := call(b, opDisconnect, args)
				if err != nil {
					return nil, err
				}
				r, err := readReply[disconnectReply](opDisconnect, raw)
				if err != nil {
					return nil, err
				}
				others := ""
				if len(r.StillOpen) > 0 {
					var names []string
					for _, o := range r.StillOpen {
						names = append(names, o.Name)
					}
					others = " Still open: " + strings.Join(names, ", ") + "."
				}
				name := ""
				if r.Profile.Name != "" {
					name = " " + r.Profile.Name
				}
				switch {
				case all && len(r.Closed) > 0:
					var names []string
					for _, o := range r.Closed {
						names = append(names, o.Name)
					}
					return text("Disconnected " + strings.Join(names, ", ") + "."), nil
				case all && r.PromptDismissed:
					return text("Dismissed the password prompt; no console was open."), nil
				case all:
					return text("No console was open; nothing to disconnect."), nil
				case r.Previous == "connected":
					return text("Disconnected" + strings.Replace(name, " ", " from ", 1) + "." + others), nil
				case r.Previous != "" && r.Previous != "disconnected":
					return text("Cancelled the connection" + strings.Replace(name, " ", " to ", 1) + " (it was " + strings.ReplaceAll(r.Previous, "_", " ") + ")." + others), nil
				case r.PromptDismissed:
					return text("Dismissed the password prompt; no console was open." + others), nil
				}
				return text("No console was open; nothing to disconnect." + others), nil
			}},
		screenshotTool(),
		{Name: "wait_for_change", Title: "Wait for screen change", ReadOnly: true,
			Description: "Block until the remote screen content changes, or until the timeout. Use after an action whose effect takes time (boot, menu redraw).",
			Schema:      obj(map[string]any{"timeout_ms": intProp("default 10000", 0, maxWaitMS)}),
			Run: func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
				t, err := argInt(a, "timeout_ms", 10000, 0, maxWaitMS)
				if err != nil {
					return nil, err
				}
				s, err := getStatus(b)
				if err != nil {
					return nil, err
				}
				base := s.ChangeSeq
				changed, err := poll(ctx, t, func() (bool, error) {
					if s, err = getStatus(b); err != nil {
						return false, err
					}
					if s.State != "connected" {
						return false, fmt.Errorf("not connected (state %s: %s)", s.State, s.Message)
					}
					return s.ChangeSeq != base, nil
				})
				if err != nil {
					return nil, err
				}
				if changed {
					return text("Screen changed."), nil
				}
				return text(fmt.Sprintf("No change within %d ms.", t)), nil
			}},
		{Name: "wait_for_stable", Title: "Wait for stable screen", ReadOnly: true,
			Description: "Block until the remote screen has not changed for quiet_ms, or until the timeout. Use after input before taking a screenshot.",
			Schema:      obj(map[string]any{"quiet_ms": intProp("default 500", 0, maxWaitMS), "timeout_ms": intProp("default 10000", 0, maxWaitMS)}),
			Run: func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
				q, err := argInt(a, "quiet_ms", 500, 0, maxWaitMS)
				if err != nil {
					return nil, err
				}
				t, err := argInt(a, "timeout_ms", 10000, 0, maxWaitMS)
				if err != nil {
					return nil, err
				}
				var s statusReply
				stable, err := poll(ctx, t, func() (bool, error) {
					if s, err = getStatus(b); err != nil {
						return false, err
					}
					if s.State != "connected" {
						return false, fmt.Errorf("not connected (state %s: %s)", s.State, s.Message)
					}
					return s.MSSinceChange >= int64(q), nil
				})
				if err != nil {
					return nil, err
				}
				if stable {
					return text(fmt.Sprintf("Stable for %d ms.", s.MSSinceChange)), nil
				}
				return text(fmt.Sprintf("Not stable for %d ms within %d ms (last change %d ms ago).", q, t, s.MSSinceChange)), nil
			}},
		{Name: "wait", Title: "Wait", ReadOnly: true, Idempotent: true,
			Description: "Pause for a fixed time, e.g. while a host boots.",
			Schema:      obj(map[string]any{"ms": intProp("milliseconds", 0, maxWaitMS)}, "ms"),
			Run: func(ctx context.Context, _ Backend, a map[string]any) ([]Content, error) {
				ms, err := argReqInt(a, "ms", 0, maxWaitMS)
				if err != nil {
					return nil, err
				}
				if err := sleep(ctx, time.Duration(ms)*time.Millisecond); err != nil {
					return nil, err
				}
				return text(fmt.Sprintf("Waited %d ms.", ms)), nil
			}},
		{Name: "list_keys", Title: "List key names", ReadOnly: true,
			Description: "All key names accepted by key, key_combo, key_down and key_up, grouped, with aliases.",
			Schema:      obj(map[string]any{}),
			Run: func(context.Context, Backend, map[string]any) ([]Content, error) {
				var out strings.Builder
				group := ""
				for _, k := range hid.Keys() {
					if k.Group != group {
						if group != "" {
							out.WriteString("\n")
						}
						group = k.Group
						out.WriteString(group + ":")
					}
					out.WriteString(" " + k.Name)
					if len(k.Aliases) > 0 {
						out.WriteString(" (" + strings.Join(k.Aliases, ", ") + ")")
					}
				}
				out.WriteString("\n\nNames are case-insensitive; '_', '-' and spaces are ignored. Single unshifted characters such as a, 7, -, / also work. " +
					"Combos: key with \"ctrl+alt+delete\", or key_combo [\"ctrl\",\"alt\",\"delete\"]. SysRq is PrintScreen (Alt+SysRq+b = [\"alt\",\"sysrq\",\"b\"]); Break is Pause.")
				return text(out.String()), nil
			}},
		inputTool(Tool{Name: "type_text", Title: "Type text", Destructive: true,
			Description: "Type text on the remote keyboard as a US-layout keyboard would: every printable ASCII character (Shift added where needed), newline = Enter, tab = Tab. " +
				"The remote OS keymap must be US for symbols to come out right; CapsLock on the remote inverts letter case.",
			Schema: obj(map[string]any{"text": prop("string", "Up to 4096 characters"), "delay_ms": intProp("Pause after each key event, default 8; use 30-50 for slow BIOS screens", 0, 2000)}, "text")},
			func(in *input, a map[string]any) (string, error) {
				s, err := argStr(a, "text")
				if err != nil {
					return "", err
				}
				gap, err := argInt(a, "delay_ms", defaultKeyGap, 0, 2000)
				if err != nil {
					return "", err
				}
				rs := []rune(strings.ReplaceAll(s, "\r\n", "\n"))
				if len(rs) > maxTextLen {
					return "", fmt.Errorf("text exceeds %d characters", maxTextLen)
				}
				for i, r := range rs { // validate everything before sending anything
					if _, _, ok := hid.Char(r); !ok {
						return "", fmt.Errorf("character %q at position %d cannot be typed on a US layout (only printable ASCII, newline and tab)", r, i)
					}
				}
				for _, r := range rs {
					u, shift, _ := hid.Char(r)
					if shift {
						if err := in.key(hid.LeftShift, true); err != nil {
							return "", err
						}
					}
					if err := in.tap(u, gap); err != nil {
						return "", err
					}
					if err := in.wait(gap); err != nil {
						return "", err
					}
					if shift {
						if err := in.key(hid.LeftShift, false); err != nil {
							return "", err
						}
					}
				}
				return fmt.Sprintf("Typed %d characters.", len(rs)), nil
			}),
		inputTool(Tool{Name: "key", Title: "Press key", Destructive: true,
			Description: "Press and release one key or a '+'-joined combination, e.g. Enter, Esc, F2, Down, PageDown, Delete, KP_Enter, ctrl+alt+delete, alt+sysrq+b, ctrl+alt+f1. Optional repeat. Call list_keys for all names.",
			Schema: obj(map[string]any{"name": prop("string", "Key name or combination of up to 6 keys"), "repeat": intProp("Times to press, default 1", 1, 100),
				"hold_ms": intProp("How long the key is held, default 50", 0, 10000)}, "name")},
			func(in *input, a map[string]any) (string, error) {
				n, err := argStr(a, "name")
				if err != nil {
					return "", err
				}
				var us []int
				if len(n) > 1 && strings.Contains(n, "+") {
					us, err = hid.Combo(n)
				} else {
					var u int
					u, err = hid.Usage(n)
					us = []int{u}
				}
				if err != nil {
					return "", err
				}
				if len(us) > 6 {
					return "", fmt.Errorf("at most 6 keys at once")
				}
				rep, err := argInt(a, "repeat", 1, 1, 100)
				if err != nil {
					return "", err
				}
				hold, err := argInt(a, "hold_ms", 50, 0, 10000)
				if err != nil {
					return "", err
				}
				for i := 0; i < rep; i++ {
					if len(us) == 1 {
						err = in.tap(us[0], hold)
					} else {
						err = pressCombo(in, us, hold)
					}
					if err == nil && i < rep-1 {
						err = in.wait(40)
					}
					if err != nil {
						return "", err
					}
				}
				if rep > 1 {
					return fmt.Sprintf("Pressed %s %d times.", comboName(us), rep), nil
				}
				return "Pressed " + comboName(us) + ".", nil
			}),
		inputTool(Tool{Name: "key_combo", Title: "Press key combination", Destructive: true,
			Description: `Press keys together in the given order and release them in reverse, e.g. ["ctrl","alt","delete"], ["alt","sysrq","b"], ["ctrl","alt","f2"], ["shift","f10"].`,
			Schema: obj(map[string]any{"keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 6, "description": "Key names"},
				"hold_ms": intProp("How long the full combination is held, default 50", 0, 10000)}, "keys")},
			func(in *input, a map[string]any) (string, error) {
				us, err := keyList(a, "keys", 1, 6)
				if err != nil {
					return "", err
				}
				hold, err := argInt(a, "hold_ms", 50, 0, 10000)
				if err != nil {
					return "", err
				}
				if err := pressCombo(in, us, hold); err != nil {
					return "", err
				}
				return "Pressed " + comboName(us) + ".", nil
			}),
		{Name: "key_down", Title: "Hold key", Destructive: true,
			Description: "Press and HOLD keys (e.g. Shift while clicking, or a key that must stay down). They stay down until key_up, release_input, disconnect, or this MCP server exits.",
			Schema:      obj(map[string]any{"keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 6, "description": "Key names"}}, "keys"),
			Run:         keyHold(true)},
		{Name: "key_up", Title: "Release key",
			Description: "Release keys previously held with key_down.",
			Schema:      obj(map[string]any{"keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 6, "description": "Key names"}}, "keys"),
			Run:         keyHold(false), Idempotent: true},
		simpleDone(Tool{Name: "release_input", Title: "Release all input", Idempotent: true,
			Description: "Release every held key and mouse button on the remote side (also the user's). Use when keys seem stuck.",
			Schema:      obj(map[string]any{})}, opRelease, "Released all held keys and mouse buttons."),
		inputTool(Tool{Name: "mouse_move", Title: "Move pointer", Idempotent: true,
			Description: "Move the pointer to a framebuffer pixel (absolute).",
			Schema:      obj(map[string]any{"x": intProp("Framebuffer X", 0, 4095), "y": intProp("Framebuffer Y", 0, 4095)}, "x", "y")},
			func(in *input, a map[string]any) (string, error) {
				x, y, err := coords(a, "x", "y")
				if err != nil {
					return "", err
				}
				if err := in.pointer(x, y, 0, 0); err != nil {
					return "", err
				}
				return fmt.Sprintf("Pointer at %d,%d.", x, y), nil
			}),
		inputTool(Tool{Name: "mouse_click", Title: "Click", Destructive: true,
			Description: "Move to a framebuffer pixel and click. Optional modifier keys are held during the click (e.g. [\"shift\"], [\"ctrl\"]).",
			Schema: obj(map[string]any{"x": intProp("Framebuffer X", 0, 4095), "y": intProp("Framebuffer Y", 0, 4095), "button": buttonProp,
				"modifiers": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 4, "description": "Keys held during the click"}}, "x", "y")},
			clicker(1)),
		inputTool(Tool{Name: "mouse_double_click", Title: "Double-click", Destructive: true,
			Description: "Move to a framebuffer pixel and double-click.",
			Schema:      obj(map[string]any{"x": intProp("Framebuffer X", 0, 4095), "y": intProp("Framebuffer Y", 0, 4095), "button": buttonProp}, "x", "y")},
			clicker(2)),
		inputTool(Tool{Name: "mouse_drag", Title: "Drag", Destructive: true,
			Description: "Press a button at (x,y), move in steps to (to_x,to_y) and release there.",
			Schema: obj(map[string]any{"x": intProp("Start X", 0, 4095), "y": intProp("Start Y", 0, 4095), "to_x": intProp("Target X", 0, 4095), "to_y": intProp("Target Y", 0, 4095),
				"button": buttonProp}, "x", "y", "to_x", "to_y")},
			func(in *input, a map[string]any) (string, error) {
				x, y, err := coords(a, "x", "y")
				if err != nil {
					return "", err
				}
				tx, ty, err := coords(a, "to_x", "to_y")
				if err != nil {
					return "", err
				}
				m, err := buttonMask(a)
				if err != nil {
					return "", err
				}
				if err := in.pointer(x, y, 0, 0); err != nil {
					return "", err
				}
				if err := in.wait(30); err != nil {
					return "", err
				}
				if err := in.pointer(x, y, m, 0); err != nil {
					return "", err
				}
				const steps = 10
				for i := 1; i <= steps; i++ {
					if err := in.wait(20); err != nil {
						return "", err
					}
					if err := in.pointer(x+(tx-x)*i/steps, y+(ty-y)*i/steps, m, 0); err != nil {
						return "", err
					}
				}
				if err := in.wait(30); err != nil {
					return "", err
				}
				if err := in.pointer(tx, ty, 0, 0); err != nil {
					return "", err
				}
				return fmt.Sprintf("Dragged from %d,%d to %d,%d.", x, y, tx, ty), nil
			}),
		inputTool(Tool{Name: "mouse_scroll", Title: "Scroll", Destructive: true,
			Description: "Move to a pixel and turn the mouse wheel; positive amount scrolls up, negative down.",
			Schema:      obj(map[string]any{"x": intProp("Framebuffer X", 0, 4095), "y": intProp("Framebuffer Y", 0, 4095), "amount": intProp("Wheel steps, -20..20, not 0", -20, 20)}, "x", "y", "amount")},
			func(in *input, a map[string]any) (string, error) {
				x, y, err := coords(a, "x", "y")
				if err != nil {
					return "", err
				}
				if v, ok := a["amount"]; !ok || v == nil {
					return "", fmt.Errorf("amount is required")
				}
				n, err := argInt(a, "amount", 0, -20, 20)
				if err != nil || n == 0 {
					return "", fmt.Errorf("amount must be a non-zero integer in -20..20")
				}
				dir := 1
				if n < 0 {
					dir, n = -1, -n
				}
				if err := in.pointer(x, y, 0, 0); err != nil {
					return "", err
				}
				for i := 0; i < n; i++ {
					if err := in.wait(30); err != nil {
						return "", err
					}
					if err := in.pointer(x, y, 0, dir); err != nil {
						return "", err
					}
				}
				return fmt.Sprintf("Scrolled %+d at %d,%d.", dir*n, x, y), nil
			}),
		{Name: "power", Title: "Chassis power", Destructive: true,
			Description: "Chassis power control: on, off (hard), reset (hard), soft (ACPI shutdown request). Takes effect IMMEDIATELY with no confirmation and may destroy unsaved work on the host; prefer soft over off. Only with the user's consent.",
			Schema:      obj(map[string]any{"action": map[string]any{"type": "string", "enum": powerActions}}, "action"),
			Run: func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
				act, err := argEnum(a, "action", powerActions, "")
				if err != nil {
					return nil, err
				}
				if _, err := call(b, opPower, map[string]any{"action": act}); err != nil {
					return nil, err
				}
				return text("Power action '" + act + "' accepted by the BMC."), nil
			}},
		{Name: "mount_iso", Title: "Mount ISO",
			Description: "Attach a local ISO file read-only as virtual CD/DVD (AMI and ATEN Java-protocol consoles; not Avocent/Cisco or ATEN HTML5). The host sees it until eject_iso or disconnect.",
			Schema:      obj(map[string]any{"path": prop("string", "Absolute path to a readable .iso file on this computer")}, "path"),
			Run: func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
				path, err := argStr(a, "path")
				if err != nil {
					return nil, err
				}
				raw, err := call(b, opMount, map[string]any{"path": path})
				if err != nil {
					return nil, err
				}
				if saidNothing(raw) {
					return text("ISO attached read-only; the host sees a virtual CD/DVD until eject_iso or disconnect."), nil
				}
				return pretty(raw), nil
			}},
		{Name: "eject_iso", Title: "Eject ISO", Idempotent: true,
			Description: "Detach the mounted virtual media.",
			Schema:      obj(map[string]any{}),
			Run: func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
				// Say so when nothing is mounted instead of reporting a silent no-op as success.
				if s, err := getStatus(b); err == nil && s.State == "connected" && !s.Mounted {
					return text("No virtual media is mounted; nothing to eject."), nil
				}
				raw, err := call(b, opEject, a)
				if err != nil {
					return nil, err
				}
				if saidNothing(raw) {
					return text("Virtual media detached."), nil
				}
				return pretty(raw), nil
			}},
	}
}

func keyHold(down bool) func(context.Context, Backend, map[string]any) ([]Content, error) {
	return func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
		us, err := keyList(a, "keys", 1, 6)
		if err != nil {
			return nil, err
		}
		for _, u := range us {
			if _, err := call(b, opKey, map[string]any{"usage": u, "down": down}); err != nil {
				return nil, err
			}
			if err := sleep(ctx, 20*time.Millisecond); err != nil {
				return nil, err
			}
		}
		if down {
			return text("Holding " + comboName(us) + ". Release with key_up or release_input."), nil
		}
		return text("Released " + comboName(us) + "."), nil
	}
}

func clicker(n int) func(*input, map[string]any) (string, error) {
	return func(in *input, a map[string]any) (string, error) {
		x, y, err := coords(a, "x", "y")
		if err != nil {
			return "", err
		}
		m, err := buttonMask(a)
		if err != nil {
			return "", err
		}
		mods, err := keyList(a, "modifiers", 0, 4)
		if err != nil {
			return "", err
		}
		if err := in.pointer(x, y, 0, 0); err != nil {
			return "", err
		}
		for _, u := range mods {
			if err := in.key(u, true); err != nil {
				return "", err
			}
		}
		if err := in.wait(30); err != nil {
			return "", err
		}
		for i := 0; i < n; i++ {
			if err := in.pointer(x, y, m, 0); err != nil {
				return "", err
			}
			if err := in.wait(40); err != nil {
				return "", err
			}
			if err := in.pointer(x, y, 0, 0); err != nil {
				return "", err
			}
			if err := in.wait(60); err != nil {
				return "", err
			}
		}
		for i := len(mods) - 1; i >= 0; i-- {
			if err := in.key(mods[i], false); err != nil {
				return "", err
			}
		}
		what := map[int]string{1: "Clicked", 2: "Double-clicked"}[n]
		return fmt.Sprintf("%s at %d,%d.", what, x, y), nil
	}
}

func connectTool() Tool {
	return Tool{Name: "connect", Title: "Open console", Idempotent: true,
		Description: "Open the console of a saved profile (by name, id or host) and wait until it is connected with a first frame, or report the failing stage. " +
			"Uses the password saved in KWallet. If none is saved, OldIPMI asks the USER for it in its window; this tool keeps waiting for that (password_wait_ms) — tell the user to type it there. " +
			"Other consoles that are already open stay open (several can be open at once; address each with the profile argument of the other tools). " +
			"Calling connect again for the profile that is already connecting or connected just waits for it.",
		Schema: obj(map[string]any{"profile": prop("string", "Profile name, id or host (see list_profiles)"),
			"timeout_ms":       intProp("Time allowed for login and console setup, default 45000", 1000, maxWaitMS),
			"password_wait_ms": intProp("Time to wait for the user to enter a missing password in the OldIPMI window, default 60000; 0 = do not wait", 0, maxWaitMS)}, "profile"),
		Run: func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
			name, err := argStr(a, "profile")
			if err != nil {
				return nil, err
			}
			timeout, err := argInt(a, "timeout_ms", 45000, 1000, maxWaitMS)
			if err != nil {
				return nil, err
			}
			pwWait, err := argInt(a, "password_wait_ms", 60000, 0, maxWaitMS)
			if err != nil {
				return nil, err
			}
			raw, err := call(b, opConnect, map[string]any{"profile": name})
			if err != nil {
				return nil, err
			}
			started, err := readReply[connectReply](opConnect, raw)
			if err != nil {
				return nil, err
			}
			// From here on only this profile's console is of interest, whatever else is open.
			if started.Profile.ID != "" {
				b = scoped{b, started.Profile.ID}
			} else {
				b = scoped{b, name}
			}
			label := started.Profile.Name
			if label == "" {
				label = name
			}
			passwordPrompt := func() error {
				return fmt.Errorf("Password required: OldIPMI is asking for the password of %q in its window and the user has not entered it. "+
					"Ask the user to type it there (ticking 'remember' saves it in KWallet); calling connect again reopens the prompt and waits.", label)
			}
			// Phase 1: wait for the user while a password prompt is open.
			var s statusReply
			if _, err = poll(ctx, pwWait, func() (bool, error) {
				if s, err = getStatus(b); err != nil {
					return false, err
				}
				return !(s.State == "disconnected" && s.PasswordRequired), nil
			}); err != nil {
				return nil, err
			}
			if s.State == "disconnected" && s.PasswordRequired {
				return nil, passwordPrompt()
			}
			// Phase 2: wait for login, discovery and the first frame.
			var noFrameSince time.Time
			done, err := poll(ctx, timeout, func() (bool, error) {
				if s, err = getStatus(b); err != nil {
					return false, err
				}
				switch s.State {
				case "connected":
					if s.Width > 0 {
						return true, nil
					}
					if noFrameSince.IsZero() {
						noFrameSince = time.Now()
					}
					return time.Since(noFrameSince) > 5*time.Second, nil
				case "disconnected":
					if s.PasswordRequired {
						return false, passwordPrompt()
					}
					return false, fmt.Errorf("connecting to %s failed: %s", label, s.Message)
				}
				return false, nil
			})
			if err != nil {
				return nil, err
			}
			if !done {
				return nil, fmt.Errorf("timed out after %d ms in state %q (%s). The attempt continues in OldIPMI: call status or connect again to keep waiting, or disconnect to cancel", timeout, s.State, s.Message)
			}
			proto, _ := s.ConsoleInfo["protocol"].(string)
			if s.Width == 0 {
				return text(fmt.Sprintf("Connected to %s (%s), but no video frame has arrived yet; use wait_for_change, then screenshot.", label, proto)), nil
			}
			return text(fmt.Sprintf("Connected to %s (%s). Framebuffer %dx%d. Take a screenshot next.", label, proto, s.Width, s.Height)), nil
		}}
}

func screenshotTool() Tool {
	return Tool{Name: "screenshot", Title: "Screenshot", ReadOnly: true,
		Description: "PNG image of the remote screen (returned as MCP image content). Optional crop region in framebuffer pixels, and downscaling: scale (0.1..1) and/or max_size " +
			"(longest image side, default 1568; 0 = full resolution). The text block states the exact mapping; all other tools always take FRAMEBUFFER coordinates.",
		Schema: obj(map[string]any{"x": intProp("Region left", 0, 4095), "y": intProp("Region top", 0, 4095), "width": intProp("Region width", 1, 4096), "height": intProp("Region height", 1, 4096),
			"scale":    map[string]any{"type": "number", "minimum": 0.1, "maximum": 1, "description": "Downscale factor, default 1"},
			"max_size": intProp("Longest side of the returned image in pixels, 64..4096, default 1568; 0 = no limit", 0, 4096)}),
		Run: func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
			args := map[string]any{}
			for _, r := range []struct {
				k      string
				lo, hi int
			}{{"x", 0, 4095}, {"y", 0, 4095}, {"width", 1, 4096}, {"height", 1, 4096}} {
				if v, ok := a[r.k]; ok && v != nil {
					n, err := argInt(a, r.k, 0, r.lo, r.hi)
					if err != nil {
						return nil, err
					}
					args[r.k] = n
				}
			}
			if v, ok := a["scale"]; ok && v != nil {
				f, ok := v.(float64)
				if !ok || f < 0.1 || f > 1 {
					return nil, fmt.Errorf("scale must be a number in 0.1..1")
				}
				args["scale"] = f
			}
			ms, err := argInt(a, "max_size", defaultMaxImage, 0, 4096)
			if err != nil {
				return nil, err
			}
			if ms > 0 && ms < 64 {
				return nil, fmt.Errorf("max_size must be 0 or 64..4096")
			}
			args["maxSize"] = ms
			raw, err := call(b, opScreenshot, args)
			if err != nil {
				return nil, err
			}
			r, err := readReply[screenshotReply](opScreenshot, raw)
			if err != nil {
				return nil, err
			}
			if r.PNG == "" {
				return nil, fmt.Errorf("the app returned no image")
			}
			mapping := "Image pixels equal framebuffer pixels."
			if r.X != 0 || r.Y != 0 || r.RegionW != r.FullWidth || r.RegionH != r.FullHeight || r.Width != r.RegionW {
				sx := float64(r.Width) / float64(max(1, r.RegionW))
				mapping = fmt.Sprintf("Image shows framebuffer region x=%d y=%d %dx%d at scale %.4g: framebuffer_x = %d + image_x / %.4g, framebuffer_y = %d + image_y / %.4g.",
					r.X, r.Y, r.RegionW, r.RegionH, sx, r.X, sx, r.Y, float64(r.Height)/float64(max(1, r.RegionH)))
			}
			return []Content{{Type: "image", Data: r.PNG, MimeType: "image/png"},
				{Type: "text", Text: fmt.Sprintf("Framebuffer %dx%d; image %dx%d. %s", r.FullWidth, r.FullHeight, r.Width, r.Height, mapping)}}, nil
		}}
}
