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

func buttonMask(a map[string]any) (int, error) {
	b, err := buttonArg.get(a)
	return map[string]int{"left": 1, "right": 2, "middle": 4}[b], err
}

func coords(a map[string]any, xs, ys intSpec) (int, int, error) {
	x, err := xs.get(a)
	if err != nil {
		return 0, 0, err
	}
	y, err := ys.get(a)
	if err != nil {
		return 0, 0, err
	}
	return x, y, nil
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

// profileArg is the optional selector every session-bound tool takes: several
// consoles can be open at once, so a call names the one it means.
var profileArg = strSpec{Key: "profile", Desc: "Which open console to act on: profile name, id or host. " +
	"Optional while exactly one console is open; required when several are (see status → sessions)."}

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

// The arguments of the tools below; connect's and screenshot's are with them.
var (
	allArg = boolSpec{Key: "all", Desc: "Close every open console instead of one"}

	waitTimeoutArg = intSpec{Key: "timeout_ms", Desc: "default 10000", Hi: maxWaitMS, Def: 10000}
	quietArg       = intSpec{Key: "quiet_ms", Desc: "default 500", Hi: maxWaitMS, Def: 500}
	msArg          = intSpec{Key: "ms", Desc: "milliseconds", Hi: maxWaitMS, Req: true}

	textArg      = strSpec{Key: "text", Desc: "Up to 4096 characters", Req: true}
	delayArg     = intSpec{Key: "delay_ms", Desc: "Pause after each key event, default 8; use 30-50 for slow BIOS screens", Hi: 2000, Def: defaultKeyGap}
	keyNameArg   = strSpec{Key: "name", Desc: "Key name or combination of up to 6 keys", Req: true}
	repeatArg    = intSpec{Key: "repeat", Desc: "Times to press, default 1", Lo: 1, Hi: 100, Def: 1}
	keyHoldArg   = intSpec{Key: "hold_ms", Desc: "How long the key is held, default 50", Hi: 10000, Def: 50}
	comboHoldArg = intSpec{Key: "hold_ms", Desc: "How long the full combination is held, default 50", Hi: 10000, Def: 50}
	keysArg      = keysSpec{Key: "keys", Desc: "Key names", Min: 1, Max: 6}

	xArg         = intSpec{Key: "x", Desc: "Framebuffer X", Hi: 4095, Req: true}
	yArg         = intSpec{Key: "y", Desc: "Framebuffer Y", Hi: 4095, Req: true}
	buttonArg    = enumSpec{Key: "button", Desc: "Mouse button, default left", Values: []string{"left", "right", "middle"}, Def: "left"}
	modifiersArg = keysSpec{Key: "modifiers", Desc: "Keys held during the click", Max: 4}
	fromXArg     = intSpec{Key: "x", Desc: "Start X", Hi: 4095, Req: true}
	fromYArg     = intSpec{Key: "y", Desc: "Start Y", Hi: 4095, Req: true}
	toXArg       = intSpec{Key: "to_x", Desc: "Target X", Hi: 4095, Req: true}
	toYArg       = intSpec{Key: "to_y", Desc: "Target Y", Hi: 4095, Req: true}
	amountArg    = intSpec{Key: "amount", Desc: "Wheel steps, -20..20, not 0", Lo: -20, Hi: 20, Req: true, NonZero: true}

	actionArg = enumSpec{Key: "action", Values: powerActions}
	pathArg   = strSpec{Key: "path", Desc: "Absolute path to a readable .iso file on this computer", Req: true}
)

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
			Schema:      params(allArg),
			Run: func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
				all, err := allArg.get(a)
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
			Schema:      params(waitTimeoutArg),
			Run: func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
				t, err := waitTimeoutArg.get(a)
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
			Schema:      params(quietArg, waitTimeoutArg),
			Run: func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
				q, err := quietArg.get(a)
				if err != nil {
					return nil, err
				}
				t, err := waitTimeoutArg.get(a)
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
			Schema:      params(msArg),
			Run: func(ctx context.Context, _ Backend, a map[string]any) ([]Content, error) {
				ms, err := msArg.get(a)
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
			Schema: params(textArg, delayArg)},
			func(in *input, a map[string]any) (string, error) {
				s, err := textArg.get(a)
				if err != nil {
					return "", err
				}
				gap, err := delayArg.get(a)
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
			Schema:      params(keyNameArg, repeatArg, keyHoldArg)},
			func(in *input, a map[string]any) (string, error) {
				n, err := keyNameArg.get(a)
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
				rep, err := repeatArg.get(a)
				if err != nil {
					return "", err
				}
				hold, err := keyHoldArg.get(a)
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
			Schema:      params(keysArg, comboHoldArg)},
			func(in *input, a map[string]any) (string, error) {
				us, err := keysArg.get(a)
				if err != nil {
					return "", err
				}
				hold, err := comboHoldArg.get(a)
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
			Schema:      params(keysArg),
			Run:         keyHold(true)},
		{Name: "key_up", Title: "Release key",
			Description: "Release keys previously held with key_down.",
			Schema:      params(keysArg),
			Run:         keyHold(false), Idempotent: true},
		simpleDone(Tool{Name: "release_input", Title: "Release all input", Idempotent: true,
			Description: "Release every held key and mouse button on the remote side (also the user's). Use when keys seem stuck.",
			Schema:      obj(map[string]any{})}, opRelease, "Released all held keys and mouse buttons."),
		inputTool(Tool{Name: "mouse_move", Title: "Move pointer", Idempotent: true,
			Description: "Move the pointer to a framebuffer pixel (absolute).",
			Schema:      params(xArg, yArg)},
			func(in *input, a map[string]any) (string, error) {
				x, y, err := coords(a, xArg, yArg)
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
			Schema:      params(xArg, yArg, buttonArg, modifiersArg)},
			clicker(1)),
		inputTool(Tool{Name: "mouse_double_click", Title: "Double-click", Destructive: true,
			Description: "Move to a framebuffer pixel and double-click.",
			Schema:      params(xArg, yArg, buttonArg)},
			clicker(2)),
		inputTool(Tool{Name: "mouse_drag", Title: "Drag", Destructive: true,
			Description: "Press a button at (x,y), move in steps to (to_x,to_y) and release there.",
			Schema:      params(fromXArg, fromYArg, toXArg, toYArg, buttonArg)},
			func(in *input, a map[string]any) (string, error) {
				x, y, err := coords(a, fromXArg, fromYArg)
				if err != nil {
					return "", err
				}
				tx, ty, err := coords(a, toXArg, toYArg)
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
			Schema:      params(xArg, yArg, amountArg)},
			func(in *input, a map[string]any) (string, error) {
				x, y, err := coords(a, xArg, yArg)
				if err != nil {
					return "", err
				}
				n, err := amountArg.get(a)
				if err != nil {
					return "", err
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
			Schema:      params(actionArg),
			Run: func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
				act, err := actionArg.get(a)
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
			Schema:      params(pathArg),
			Run: func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
				path, err := pathArg.get(a)
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
		us, err := keysArg.get(a)
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
		x, y, err := coords(a, xArg, yArg)
		if err != nil {
			return "", err
		}
		m, err := buttonMask(a)
		if err != nil {
			return "", err
		}
		mods, err := modifiersArg.get(a)
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

// connect's arguments.
var (
	connectProfileArg = strSpec{Key: "profile", Desc: "Profile name, id or host (see list_profiles)", Req: true}
	connectTimeoutArg = intSpec{Key: "timeout_ms", Desc: "Time allowed for login and console setup, default 45000", Lo: 1000, Hi: maxWaitMS, Def: 45000}
	passwordWaitArg   = intSpec{Key: "password_wait_ms", Desc: "Time to wait for the user to enter a missing password in the OldIPMI window, default 60000; 0 = do not wait",
		Hi: maxWaitMS, Def: 60000}
)

func connectTool() Tool {
	return Tool{Name: "connect", Title: "Open console", Idempotent: true,
		Description: "Open the console of a saved profile (by name, id or host) and wait until it is connected with a first frame, or report the failing stage. " +
			"Uses the password saved in KWallet. If none is saved, OldIPMI asks the USER for it in its window; this tool keeps waiting for that (password_wait_ms) — tell the user to type it there. " +
			"Other consoles that are already open stay open (several can be open at once; address each with the profile argument of the other tools). " +
			"Calling connect again for the profile that is already connecting or connected just waits for it.",
		Schema: params(connectProfileArg, connectTimeoutArg, passwordWaitArg),
		Run: func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
			name, err := connectProfileArg.get(a)
			if err != nil {
				return nil, err
			}
			timeout, err := connectTimeoutArg.get(a)
			if err != nil {
				return nil, err
			}
			pwWait, err := passwordWaitArg.get(a)
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

// screenshot's arguments.
var (
	regionXArg = intSpec{Key: "x", Desc: "Region left", Hi: 4095}
	regionYArg = intSpec{Key: "y", Desc: "Region top", Hi: 4095}
	regionWArg = intSpec{Key: "width", Desc: "Region width", Lo: 1, Hi: 4096}
	regionHArg = intSpec{Key: "height", Desc: "Region height", Lo: 1, Hi: 4096}
	scaleArg   = numSpec{Key: "scale", Desc: "Downscale factor, default 1", Lo: 0.1, Hi: 1}
	maxSizeArg = intSpec{Key: "max_size", Desc: "Longest side of the returned image in pixels, 64..4096, default 1568; 0 = no limit", Hi: 4096, Def: defaultMaxImage}
)

func screenshotTool() Tool {
	return Tool{Name: "screenshot", Title: "Screenshot", ReadOnly: true,
		Description: "PNG image of the remote screen (returned as MCP image content). Optional crop region in framebuffer pixels, and downscaling: scale (0.1..1) and/or max_size " +
			"(longest image side, default 1568; 0 = full resolution). The text block states the exact mapping; all other tools always take FRAMEBUFFER coordinates.",
		Schema: params(regionXArg, regionYArg, regionWArg, regionHArg, scaleArg, maxSizeArg),
		Run: func(_ context.Context, b Backend, a map[string]any) ([]Content, error) {
			args := map[string]any{} // the region and the scale go to the app only when given
			for _, r := range []intSpec{regionXArg, regionYArg, regionWArg, regionHArg} {
				if given(a, r.Key) {
					n, err := r.get(a)
					if err != nil {
						return nil, err
					}
					args[r.Key] = n
				}
			}
			if given(a, scaleArg.Key) {
				f, err := scaleArg.get(a)
				if err != nil {
					return nil, err
				}
				args["scale"] = f
			}
			ms, err := maxSizeArg.get(a)
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
