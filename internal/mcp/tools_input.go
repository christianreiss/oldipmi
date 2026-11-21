package mcp

import (
	"context"
	"fmt"
	"oldipmi/internal/hid"
	"strings"
	"time"
)

// inputTools type, press keys and use the mouse.
func inputTools() []Tool {
	return []Tool{
		{Name: "list_keys", Title: "List key names", Global: true, ReadOnly: true,
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
	}
}

// The keyboard tools' arguments.
var (
	textArg      = strSpec{Key: "text", Desc: "Up to 4096 characters", Req: true}
	delayArg     = intSpec{Key: "delay_ms", Desc: "Pause after each key event, default 8; use 30-50 for slow BIOS screens", Hi: 2000, Def: defaultKeyGap}
	keyNameArg   = strSpec{Key: "name", Desc: "Key name or combination of up to 6 keys", Req: true}
	repeatArg    = intSpec{Key: "repeat", Desc: "Times to press, default 1", Lo: 1, Hi: 100, Def: 1}
	keyHoldArg   = intSpec{Key: "hold_ms", Desc: "How long the key is held, default 50", Hi: 10000, Def: 50}
	comboHoldArg = intSpec{Key: "hold_ms", Desc: "How long the full combination is held, default 50", Hi: 10000, Def: 50}
	keysArg      = keysSpec{Key: "keys", Desc: "Key names", Min: 1, Max: 6}
)

// The mouse tools' arguments.
var (
	xArg         = intSpec{Key: "x", Desc: "Framebuffer X", Hi: 4095, Req: true}
	yArg         = intSpec{Key: "y", Desc: "Framebuffer Y", Hi: 4095, Req: true}
	buttonArg    = enumSpec{Key: "button", Desc: "Mouse button, default left", Values: []string{"left", "right", "middle"}, Def: "left"}
	modifiersArg = keysSpec{Key: "modifiers", Desc: "Keys held during the click", Max: 4}
	fromXArg     = intSpec{Key: "x", Desc: "Start X", Hi: 4095, Req: true}
	fromYArg     = intSpec{Key: "y", Desc: "Start Y", Hi: 4095, Req: true}
	toXArg       = intSpec{Key: "to_x", Desc: "Target X", Hi: 4095, Req: true}
	toYArg       = intSpec{Key: "to_y", Desc: "Target Y", Hi: 4095, Req: true}
	amountArg    = intSpec{Key: "amount", Desc: "Wheel steps, -20..20, not 0", Lo: -20, Hi: 20, Req: true, NonZero: true}
)

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
	if _, err := call(in.ctx, in.b, opKey, map[string]any{"usage": u, "down": down}); err != nil {
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
	if _, err := call(in.ctx, in.b, opPointer, map[string]any{"x": x, "y": y, "buttons": buttons, "wheel": wheel}); err != nil {
		return err
	}
	in.x, in.y, in.buttons = x, y, buttons
	return nil
}

func (in *input) cleanup() {
	ctx := context.WithoutCancel(in.ctx) // let go also when the tool was cancelled
	for i := len(in.held) - 1; i >= 0; i-- {
		call(ctx, in.b, opKey, map[string]any{"usage": in.held[i], "down": false})
	}
	in.held = nil
	if in.buttons != 0 {
		call(ctx, in.b, opPointer, map[string]any{"x": in.x, "y": in.y, "buttons": 0, "wheel": 0})
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

func keyHold(down bool) func(context.Context, Backend, map[string]any) ([]Content, error) {
	return func(ctx context.Context, b Backend, a map[string]any) ([]Content, error) {
		us, err := keysArg.get(a)
		if err != nil {
			return nil, err
		}
		for _, u := range us {
			if _, err := call(ctx, b, opKey, map[string]any{"usage": u, "down": down}); err != nil {
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
