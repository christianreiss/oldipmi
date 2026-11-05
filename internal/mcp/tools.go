package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Tool is one MCP tool.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Run         func(b Backend, args map[string]any) ([]Content, error)
}

const (
	maxTextLen    = 4096
	maxWaitMS     = 120000
	pollInterval  = 100 * time.Millisecond
	defaultKeyGap = 8
)

var sleep = time.Sleep

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

func argInt(a map[string]any, k string, def int) (int, error) {
	v, ok := a[k]
	if !ok || v == nil {
		return def, nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int(f)) {
		return 0, fmt.Errorf("%s must be an integer", k)
	}
	return int(f), nil
}
func argReqInt(a map[string]any, k string) (int, error) {
	if _, ok := a[k]; !ok {
		return 0, fmt.Errorf("%s is required", k)
	}
	return argInt(a, k, 0)
}
func argStr(a map[string]any, k string) (string, error) {
	v, ok := a[k]
	if !ok {
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

type status struct {
	State         string         `json:"state"`
	Message       string         `json:"message"`
	Mounted       bool           `json:"mounted"`
	Width         int            `json:"width"`
	Height        int            `json:"height"`
	FrameSeq      int64          `json:"frameSeq"`
	ChangeSeq     int64          `json:"changeSeq"`
	MSSinceChange int64          `json:"msSinceChange"`
	ConsoleInfo   map[string]any `json:"consoleInfo"`
}

func getStatus(b Backend) (status, error) {
	var s status
	raw, err := call(b, "status", nil)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(raw, &s)
}
func pretty(raw json.RawMessage) []Content {
	var out strings.Builder
	var v any
	if json.Unmarshal(raw, &v) == nil {
		b, _ := json.MarshalIndent(v, "", "  ")
		out.Write(b)
	} else {
		out.Write(raw)
	}
	return text(out.String())
}

// simple builds a tool that forwards validated arguments to one GUI operation.
func simple(name, desc string, schema map[string]any, op string) Tool {
	return Tool{name, desc, schema, func(b Backend, a map[string]any) ([]Content, error) {
		raw, err := call(b, op, a)
		if err != nil {
			return nil, err
		}
		return pretty(raw), nil
	}}
}

func tapKey(b Backend, usage, gap int) error {
	for _, down := range []bool{true, false} {
		if _, err := call(b, "key", map[string]any{"usage": usage, "down": down}); err != nil {
			return err
		}
		sleep(time.Duration(gap) * time.Millisecond)
	}
	return nil
}
func keyEvent(b Backend, usage int, down bool) error {
	_, err := call(b, "key", map[string]any{"usage": usage, "down": down})
	return err
}
func pointer(b Backend, x, y, buttons, wheel int) error {
	_, err := call(b, "pointer", map[string]any{"x": x, "y": y, "buttons": buttons, "wheel": wheel})
	return err
}
func release(b Backend) { call(b, "release", nil) }

func buttonMask(a map[string]any) (int, error) {
	v, ok := a["button"]
	if !ok {
		return 1, nil
	}
	s, _ := v.(string)
	switch strings.ToLower(s) {
	case "left", "":
		return 1, nil
	case "right":
		return 2, nil
	case "middle":
		return 4, nil
	}
	return 0, fmt.Errorf("button must be left, right or middle")
}

func waitTimeout(a map[string]any, def int) (int, error) {
	t, err := argInt(a, "timeout_ms", def)
	if err != nil {
		return 0, err
	}
	if t < 0 || t > maxWaitMS {
		return 0, fmt.Errorf("timeout_ms must be 0..%d", maxWaitMS)
	}
	return t, nil
}

var xy = map[string]any{"x": prop("integer", "X pixel in the screenshot"), "y": prop("integer", "Y pixel in the screenshot")}

func withButton(extra map[string]any) map[string]any {
	m := map[string]any{"button": map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "description": "default left"}}
	for k, v := range xy {
		m[k] = v
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func tools() []Tool {
	return []Tool{
		simple("list_profiles", "List saved BMC profiles (id, name, host, family, username). Never returns passwords.", obj(map[string]any{}), "list_profiles"),
		simple("status", "Connection state, message, console info, mounted ISO, framebuffer size and screen-change counters.", obj(map[string]any{}), "status"),
		{"connect", "Open the console of a saved profile (by name or id) and wait until it is connected. Uses the password saved in KWallet.",
			obj(map[string]any{"profile": prop("string", "Profile name or id"), "timeout_ms": prop("integer", "default 45000")}, "profile"),
			func(b Backend, a map[string]any) ([]Content, error) {
				if _, err := argStr(a, "profile"); err != nil {
					return nil, err
				}
				t, err := waitTimeout(a, 45000)
				if err != nil {
					return nil, err
				}
				if _, err := call(b, "connect", map[string]any{"profile": a["profile"]}); err != nil {
					return nil, err
				}
				deadline := time.Now().Add(time.Duration(t) * time.Millisecond)
				for {
					s, err := getStatus(b)
					if err != nil {
						return nil, err
					}
					switch s.State {
					case "connected":
						// Wait briefly for the first frame so a screenshot works right away.
						for i := 0; i < 30 && s.Width == 0; i++ {
							sleep(pollInterval)
							if s, err = getStatus(b); err != nil {
								return nil, err
							}
						}
						return text(fmt.Sprintf("Connected (%s). Framebuffer %dx%d.", s.Message, s.Width, s.Height)), nil
					case "disconnected":
						return nil, fmt.Errorf("%s", s.Message)
					}
					if time.Now().After(deadline) {
						return nil, fmt.Errorf("timed out in state %q; use disconnect to cancel", s.State)
					}
					sleep(pollInterval)
				}
			}},
		simple("disconnect", "Close the console session (or cancel one being set up).", obj(map[string]any{}), "disconnect"),
		{"screenshot", "PNG screenshot of the remote console. Coordinates in all other tools refer to this image's pixels (unscaled). Optional crop region and downscale.",
			obj(map[string]any{"x": prop("integer", "region left"), "y": prop("integer", "region top"), "width": prop("integer", "region width"), "height": prop("integer", "region height"),
				"scale": prop("number", "0.1..1.0 downscale factor, default 1")}),
			func(b Backend, a map[string]any) ([]Content, error) {
				raw, err := call(b, "screenshot", a)
				if err != nil {
					return nil, err
				}
				var r struct {
					PNG                    string `json:"png"`
					Width, Height          int
					FullWidth, FullHeight  int
					X, Y, RegionW, RegionH int
				}
				if err := json.Unmarshal(raw, &r); err != nil {
					return nil, err
				}
				return []Content{{Type: "image", Data: r.PNG, MimeType: "image/png"},
					{Type: "text", Text: fmt.Sprintf("Framebuffer %dx%d; image %dx%d from region (%d,%d %dx%d).", r.FullWidth, r.FullHeight, r.Width, r.Height, r.X, r.Y, r.RegionW, r.RegionH)}}, nil
			}},
		{"wait_for_change", "Block until the screen content changes, or time out.",
			obj(map[string]any{"timeout_ms": prop("integer", "default 10000, max 120000")}),
			func(b Backend, a map[string]any) ([]Content, error) {
				t, err := waitTimeout(a, 10000)
				if err != nil {
					return nil, err
				}
				s, err := getStatus(b)
				if err != nil {
					return nil, err
				}
				base, deadline := s.ChangeSeq, time.Now().Add(time.Duration(t)*time.Millisecond)
				for {
					if s, err = getStatus(b); err != nil {
						return nil, err
					}
					if s.State != "connected" {
						return nil, fmt.Errorf("not connected (%s)", s.State)
					}
					if s.ChangeSeq != base {
						return text("Screen changed."), nil
					}
					if time.Now().After(deadline) {
						return text("No change before timeout."), nil
					}
					sleep(pollInterval)
				}
			}},
		{"wait_for_stable", "Block until the screen has not changed for quiet_ms, or time out.",
			obj(map[string]any{"quiet_ms": prop("integer", "default 500"), "timeout_ms": prop("integer", "default 10000, max 120000")}),
			func(b Backend, a map[string]any) ([]Content, error) {
				q, err := argInt(a, "quiet_ms", 500)
				if err != nil || q < 0 || q > maxWaitMS {
					return nil, fmt.Errorf("quiet_ms must be 0..%d", maxWaitMS)
				}
				t, err := waitTimeout(a, 10000)
				if err != nil {
					return nil, err
				}
				deadline := time.Now().Add(time.Duration(t) * time.Millisecond)
				for {
					s, err := getStatus(b)
					if err != nil {
						return nil, err
					}
					if s.State != "connected" {
						return nil, fmt.Errorf("not connected (%s)", s.State)
					}
					if s.MSSinceChange >= int64(q) {
						return text(fmt.Sprintf("Stable for %d ms.", s.MSSinceChange)), nil
					}
					if time.Now().After(deadline) {
						return text("Screen still changing at timeout."), nil
					}
					sleep(pollInterval)
				}
			}},
		{"type_text", "Type text on the remote keyboard (US layout, ASCII; newline = Enter, tab = Tab).",
			obj(map[string]any{"text": prop("string", "max 4096 characters"), "delay_ms": prop("integer", "gap between key events, default 8")}, "text"),
			func(b Backend, a map[string]any) ([]Content, error) {
				s, err := argStr(a, "text")
				if err != nil {
					return nil, err
				}
				gap, err := argInt(a, "delay_ms", defaultKeyGap)
				if err != nil || gap < 0 || gap > 2000 {
					return nil, fmt.Errorf("delay_ms must be 0..2000")
				}
				rs := []rune(s)
				if len(rs) > maxTextLen {
					return nil, fmt.Errorf("text exceeds %d characters", maxTextLen)
				}
				for _, r := range rs { // validate everything before sending anything
					if _, _, ok := CharUsage(r); !ok {
						return nil, fmt.Errorf("character %q cannot be typed on a US layout", r)
					}
				}
				defer release(b)
				for _, r := range rs {
					u, shift, _ := CharUsage(r)
					if shift {
						if err := keyEvent(b, usageShift, true); err != nil {
							return nil, err
						}
					}
					if err := tapKey(b, u, gap); err != nil {
						return nil, err
					}
					if shift {
						if err := keyEvent(b, usageShift, false); err != nil {
							return nil, err
						}
					}
				}
				return text(fmt.Sprintf("Typed %d characters.", len(rs))), nil
			}},
		{"key", "Press and release one key, e.g. Enter, Esc, F2, Down, Delete, a.",
			obj(map[string]any{"name": prop("string", "Key name"), "hold_ms": prop("integer", "hold time, default 50")}, "name"),
			func(b Backend, a map[string]any) ([]Content, error) {
				n, err := argStr(a, "name")
				if err != nil {
					return nil, err
				}
				u, err := KeyUsage(n)
				if err != nil {
					return nil, err
				}
				hold, err := argInt(a, "hold_ms", 50)
				if err != nil || hold < 0 || hold > 10000 {
					return nil, fmt.Errorf("hold_ms must be 0..10000")
				}
				defer release(b)
				if err := keyEvent(b, u, true); err != nil {
					return nil, err
				}
				sleep(time.Duration(hold) * time.Millisecond)
				if err := keyEvent(b, u, false); err != nil {
					return nil, err
				}
				return text("Pressed " + n + "."), nil
			}},
		{"key_combo", `Press keys together in order and release in reverse, e.g. ["ctrl","alt","delete"].`,
			obj(map[string]any{"keys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": 6}}, "keys"),
			func(b Backend, a map[string]any) ([]Content, error) {
				arr, _ := a["keys"].([]any)
				if len(arr) == 0 || len(arr) > 6 {
					return nil, fmt.Errorf("keys must list 1..6 key names")
				}
				var us []int
				for _, v := range arr {
					n, _ := v.(string)
					u, err := KeyUsage(n)
					if err != nil {
						return nil, err
					}
					us = append(us, u)
				}
				defer release(b)
				for _, u := range us {
					if err := keyEvent(b, u, true); err != nil {
						return nil, err
					}
					sleep(20 * time.Millisecond)
				}
				sleep(50 * time.Millisecond)
				for i := len(us) - 1; i >= 0; i-- {
					if err := keyEvent(b, us[i], false); err != nil {
						return nil, err
					}
					sleep(20 * time.Millisecond)
				}
				return text("Pressed combination."), nil
			}},
		simple("release_input", "Release all held keys and mouse buttons.", obj(map[string]any{}), "release"),
		{"mouse_move", "Move the pointer to a framebuffer pixel.", obj(xy, "x", "y"),
			func(b Backend, a map[string]any) ([]Content, error) {
				x, y, err := coords(a)
				if err != nil {
					return nil, err
				}
				if err := pointer(b, x, y, 0, 0); err != nil {
					return nil, err
				}
				return text(fmt.Sprintf("Pointer at %d,%d.", x, y)), nil
			}},
		{"mouse_click", "Click at a framebuffer pixel.", obj(withButton(nil), "x", "y"), clicker(1)},
		{"mouse_double_click", "Double-click at a framebuffer pixel.", obj(withButton(nil), "x", "y"), clicker(2)},
		{"mouse_drag", "Drag from one pixel to another with a button held.",
			obj(withButton(map[string]any{"to_x": prop("integer", "target X"), "to_y": prop("integer", "target Y")}), "x", "y", "to_x", "to_y"),
			func(b Backend, a map[string]any) ([]Content, error) {
				x, y, err := coords(a)
				if err != nil {
					return nil, err
				}
				tx, err := argReqInt(a, "to_x")
				if err != nil {
					return nil, err
				}
				ty, err := argReqInt(a, "to_y")
				if err != nil {
					return nil, err
				}
				m, err := buttonMask(a)
				if err != nil {
					return nil, err
				}
				defer release(b)
				if err := pointer(b, x, y, 0, 0); err != nil {
					return nil, err
				}
				if err := pointer(b, x, y, m, 0); err != nil {
					return nil, err
				}
				const steps = 10
				for i := 1; i <= steps; i++ {
					if err := pointer(b, x+(tx-x)*i/steps, y+(ty-y)*i/steps, m, 0); err != nil {
						return nil, err
					}
					sleep(20 * time.Millisecond)
				}
				if err := pointer(b, tx, ty, 0, 0); err != nil {
					return nil, err
				}
				return text("Dragged."), nil
			}},
		{"mouse_scroll", "Scroll the wheel at a pixel; positive amount scrolls up.",
			obj(map[string]any{"x": xy["x"], "y": xy["y"], "amount": prop("integer", "wheel steps, -20..20")}, "x", "y", "amount"),
			func(b Backend, a map[string]any) ([]Content, error) {
				x, y, err := coords(a)
				if err != nil {
					return nil, err
				}
				n, err := argReqInt(a, "amount")
				if err != nil || n == 0 || n < -20 || n > 20 {
					return nil, fmt.Errorf("amount must be a non-zero integer in -20..20")
				}
				dir := 1
				if n < 0 {
					dir, n = -1, -n
				}
				for i := 0; i < n; i++ {
					if err := pointer(b, x, y, 0, dir); err != nil {
						return nil, err
					}
					sleep(30 * time.Millisecond)
				}
				return text("Scrolled."), nil
			}},
		{"power", "Chassis power control: on, off (hard), reset (hard), soft (graceful shutdown). Takes effect immediately and may interrupt the host.",
			obj(map[string]any{"action": map[string]any{"type": "string", "enum": []string{"on", "off", "reset", "soft"}}}, "action"),
			func(b Backend, a map[string]any) ([]Content, error) {
				act, err := argStr(a, "action")
				if err != nil {
					return nil, err
				}
				switch act {
				case "on", "off", "reset", "soft":
				default:
					return nil, fmt.Errorf("action must be on, off, reset or soft")
				}
				if _, err := call(b, "power", map[string]any{"action": act}); err != nil {
					return nil, err
				}
				return text("Power action '" + act + "' accepted by the BMC."), nil
			}},
		simple("mount_iso", "Mount a local ISO file read-only as virtual media (AMI/ATEN profiles).",
			obj(map[string]any{"path": prop("string", "absolute path to an .iso file")}, "path"), "mount"),
		simple("eject_iso", "Eject the mounted virtual media.", obj(map[string]any{}), "eject"),
	}
}

func coords(a map[string]any) (int, int, error) {
	x, err := argReqInt(a, "x")
	if err != nil {
		return 0, 0, err
	}
	y, err := argReqInt(a, "y")
	if err != nil {
		return 0, 0, err
	}
	if x < 0 || y < 0 {
		return 0, 0, fmt.Errorf("x and y must be >= 0")
	}
	return x, y, nil
}

func clicker(n int) func(Backend, map[string]any) ([]Content, error) {
	return func(b Backend, a map[string]any) ([]Content, error) {
		x, y, err := coords(a)
		if err != nil {
			return nil, err
		}
		m, err := buttonMask(a)
		if err != nil {
			return nil, err
		}
		defer release(b)
		if err := pointer(b, x, y, 0, 0); err != nil {
			return nil, err
		}
		sleep(30 * time.Millisecond)
		for i := 0; i < n; i++ {
			if err := pointer(b, x, y, m, 0); err != nil {
				return nil, err
			}
			sleep(40 * time.Millisecond)
			if err := pointer(b, x, y, 0, 0); err != nil {
				return nil, err
			}
			sleep(60 * time.Millisecond)
		}
		return text(fmt.Sprintf("Clicked at %d,%d.", x, y)), nil
	}
}
