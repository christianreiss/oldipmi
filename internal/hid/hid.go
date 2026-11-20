// Package hid names USB HID keyboard usages (usage page 0x07) and maps US-layout
// ASCII characters to them. It is shared by the MCP server (key names typed by an
// agent), the console drivers (the keys they hold down, pressed.go) and the demo
// console (echoing what it received).
package hid

import (
	"fmt"
	"sort"
	"strings"
)

// Modifier usages.
const (
	LeftCtrl   = 224
	LeftShift  = 225
	LeftAlt    = 226
	LeftSuper  = 227
	RightCtrl  = 228
	RightShift = 229
	RightAlt   = 230
	RightSuper = 231
	Enter      = 40
	Tab        = 43
	Backspace  = 42
)

// Key is one named key.
type Key struct {
	Name    string   `json:"name"`
	Usage   int      `json:"usage"`
	Group   string   `json:"group"`
	Aliases []string `json:"aliases,omitempty"`
}

// table lists every key a name can resolve to, in display order. The first name is canonical.
var table = []struct {
	group string
	usage int
	names []string
}{
	{"editing", 40, []string{"Enter", "Return", "Ret"}},
	{"editing", 41, []string{"Esc", "Escape"}},
	{"editing", 42, []string{"Backspace", "BS", "Bksp"}},
	{"editing", 43, []string{"Tab"}},
	{"editing", 44, []string{"Space", "Spacebar"}},
	{"editing", 73, []string{"Insert", "Ins"}},
	{"editing", 76, []string{"Delete", "Del"}},
	{"navigation", 74, []string{"Home"}},
	{"navigation", 77, []string{"End"}},
	{"navigation", 75, []string{"PageUp", "PgUp", "Prior"}},
	{"navigation", 78, []string{"PageDown", "PgDn", "Next"}},
	{"navigation", 82, []string{"Up", "ArrowUp", "UpArrow"}},
	{"navigation", 81, []string{"Down", "ArrowDown", "DownArrow"}},
	{"navigation", 80, []string{"Left", "ArrowLeft", "LeftArrow"}},
	{"navigation", 79, []string{"Right", "ArrowRight", "RightArrow"}},
	{"system", 70, []string{"PrintScreen", "Print", "PrtSc", "PrtScr", "SysRq", "SysReq"}},
	{"system", 72, []string{"Pause", "Break"}},
	{"system", 101, []string{"Menu", "Application", "Apps", "ContextMenu", "Compose"}},
	{"locks", 57, []string{"CapsLock", "Caps"}},
	{"locks", 83, []string{"NumLock"}},
	{"locks", 71, []string{"ScrollLock", "Scroll"}},
	{"modifiers", LeftCtrl, []string{"Ctrl", "Control", "LCtrl", "LeftCtrl", "LeftControl", "LControl"}},
	{"modifiers", LeftShift, []string{"Shift", "LShift", "LeftShift"}},
	{"modifiers", LeftAlt, []string{"Alt", "LAlt", "LeftAlt", "Option"}},
	{"modifiers", LeftSuper, []string{"Super", "Meta", "Win", "Windows", "GUI", "Cmd", "Command", "LSuper", "LMeta", "LWin", "LGUI", "LeftSuper", "LeftMeta", "LeftWin"}},
	{"modifiers", RightCtrl, []string{"RCtrl", "RightCtrl", "RightControl", "RControl"}},
	{"modifiers", RightShift, []string{"RShift", "RightShift"}},
	{"modifiers", RightAlt, []string{"RAlt", "RightAlt", "AltGr"}},
	{"modifiers", RightSuper, []string{"RSuper", "RMeta", "RWin", "RGUI", "RightSuper", "RightMeta", "RightWin", "RCmd"}},
	{"keypad", 84, []string{"KP_Divide", "KP_Slash", "Numpad_Divide"}},
	{"keypad", 85, []string{"KP_Multiply", "KP_Asterisk", "KP_Star", "Numpad_Multiply"}},
	{"keypad", 86, []string{"KP_Subtract", "KP_Minus", "Numpad_Subtract"}},
	{"keypad", 87, []string{"KP_Add", "KP_Plus", "Numpad_Add"}},
	{"keypad", 88, []string{"KP_Enter", "Numpad_Enter"}},
	{"keypad", 99, []string{"KP_Decimal", "KP_Dot", "KP_Period", "KP_Delete", "Numpad_Decimal"}},
	{"keypad", 103, []string{"KP_Equal", "Numpad_Equal"}},
	{"punctuation", 45, []string{"Minus", "Dash", "Hyphen"}},
	{"punctuation", 46, []string{"Equal", "Equals"}},
	{"punctuation", 47, []string{"LeftBracket", "LBracket", "BracketLeft"}},
	{"punctuation", 48, []string{"RightBracket", "RBracket", "BracketRight"}},
	{"punctuation", 49, []string{"Backslash"}},
	{"punctuation", 51, []string{"Semicolon"}},
	{"punctuation", 52, []string{"Quote", "Apostrophe"}},
	{"punctuation", 53, []string{"Grave", "Backtick", "Backquote"}},
	{"punctuation", 54, []string{"Comma"}},
	{"punctuation", 55, []string{"Period", "Dot"}},
	{"punctuation", 56, []string{"Slash"}},
	{"punctuation", 100, []string{"IntlBackslash", "NonUSBackslash", "102nd"}},
}

var (
	byName  = map[string]int{}
	byUsage = map[int]string{}
	keys    []Key
)

func norm(s string) string {
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(s)))
}

func add(group string, usage int, names ...string) {
	for _, n := range names {
		byName[norm(n)] = usage
	}
	if _, ok := byUsage[usage]; !ok {
		byUsage[usage] = names[0]
	}
	keys = append(keys, Key{Name: names[0], Usage: usage, Group: group, Aliases: names[1:]})
}

func init() {
	for i := 0; i < 26; i++ {
		add("letters", 4+i, string(rune('a'+i)))
	}
	for i, d := range "1234567890" {
		add("digits", 30+i, string(d))
	}
	for f := 1; f <= 24; f++ {
		u := 57 + f // F1..F12 = 58..69
		if f > 12 {
			u = 103 + f - 12 // F13..F24 = 104..115
		}
		add("function", u, fmt.Sprintf("F%d", f))
	}
	for _, e := range table {
		add(e.group, e.usage, e.names...)
	}
	for d := 1; d <= 9; d++ {
		add("keypad", 88+d, fmt.Sprintf("KP_%d", d), fmt.Sprintf("Numpad%d", d))
	}
	add("keypad", 98, "KP_0", "Numpad0")
	// Single punctuation characters resolve to their unshifted key, too.
	for r, u := range punct {
		byName[string(r)] = u
	}
}

// Usage resolves one key name (case-insensitive; "_", "-" and spaces are ignored
// except as a single character) to its HID usage.
func Usage(name string) (int, error) {
	t := strings.TrimSpace(name)
	if len([]rune(t)) == 1 {
		if u, shift, ok := Char([]rune(t)[0]); ok {
			if shift && !(t[0] >= 'A' && t[0] <= 'Z') {
				return 0, fmt.Errorf("%q needs Shift: use type_text, or a combo such as shift+%s", t, byUsage[u])
			}
			return u, nil
		}
	}
	if u, ok := byName[norm(t)]; ok {
		return u, nil
	}
	return 0, fmt.Errorf("unknown key %q (list_keys shows all key names)", name)
}

// Combo parses "ctrl+alt+delete" into usages. A lone "+" is not a key name.
func Combo(spec string) ([]int, error) {
	parts := strings.Split(spec, "+")
	var us []int
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("invalid key combination %q", spec)
		}
		u, err := Usage(p)
		if err != nil {
			return nil, err
		}
		us = append(us, u)
	}
	return us, nil
}

// Name is the canonical name of a usage, or "usage-N" when unnamed.
func Name(usage int) string {
	if n, ok := byUsage[usage]; ok {
		if len(n) == 1 && n[0] >= 'a' && n[0] <= 'z' {
			return strings.ToUpper(n)
		}
		return n
	}
	return fmt.Sprintf("usage-%d", usage)
}

// IsModifier reports whether usage is one of the eight modifier keys.
func IsModifier(usage int) bool { return usage >= LeftCtrl && usage <= RightSuper }

// Keys returns every named key, sorted by group order then usage.
func Keys() []Key {
	order := map[string]int{"letters": 0, "digits": 1, "function": 2, "editing": 3, "navigation": 4, "system": 5, "locks": 6, "modifiers": 7, "keypad": 8, "punctuation": 9}
	out := append([]Key(nil), keys...)
	sort.SliceStable(out, func(i, j int) bool {
		if order[out[i].Group] != order[out[j].Group] {
			return order[out[i].Group] < order[out[j].Group]
		}
		return out[i].Usage < out[j].Usage
	})
	return out
}

const shiftedDigits = ")!@#$%^&*("

var punct = map[rune]int{'-': 45, '=': 46, '[': 47, ']': 48, '\\': 49, ';': 51, '\'': 52, '`': 53, ',': 54, '.': 55, '/': 56}
var shiftedPunct = map[rune]int{'_': 45, '+': 46, '{': 47, '}': 48, '|': 49, ':': 51, '"': 52, '~': 53, '<': 54, '>': 55, '?': 56}

// Char maps one US-layout ASCII character to its HID usage and whether Shift is needed.
// Newline (LF or CR) is Enter and tab is Tab.
func Char(r rune) (usage int, shift, ok bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return 4 + int(r-'a'), false, true
	case r >= 'A' && r <= 'Z':
		return 4 + int(r-'A'), true, true
	case r >= '1' && r <= '9':
		return 30 + int(r-'1'), false, true
	case r == '0':
		return 39, false, true
	case r == ' ':
		return 44, false, true
	case r == '\n' || r == '\r':
		return Enter, false, true
	case r == '\t':
		return Tab, false, true
	}
	if i := strings.IndexRune(shiftedDigits, r); i >= 0 {
		if i == 0 {
			return 39, true, true
		}
		return 29 + i, true, true
	}
	if u, ok := punct[r]; ok {
		return u, false, true
	}
	if u, ok := shiftedPunct[r]; ok {
		return u, true, true
	}
	return 0, false, false
}

// Rune is the US-layout character a usage produces (with or without Shift), if any.
// Enter yields '\n' and Tab '\t'; keypad digits and operators yield their characters.
func Rune(usage int, shift bool) (rune, bool) {
	r, ok := runeOf[[2]int{usage, b2i(shift)}]
	if !ok && (usage >= 84 && usage <= 99 || usage == 103) {
		r, ok = keypad[usage]
	}
	return r, ok
}

var keypad = map[int]rune{84: '/', 85: '*', 86: '-', 87: '+', 88: '\n', 89: '1', 90: '2', 91: '3', 92: '4', 93: '5', 94: '6', 95: '7', 96: '8', 97: '9', 98: '0', 99: '.', 103: '='}
var runeOf = map[[2]int]rune{}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func init() {
	for r := rune(32); r < 127; r++ {
		u, sh, _ := Char(r)
		runeOf[[2]int{u, b2i(sh)}] = r
	}
	runeOf[[2]int{Enter, 0}], runeOf[[2]int{Enter, 1}] = '\n', '\n'
	runeOf[[2]int{Tab, 0}] = '\t'
}
