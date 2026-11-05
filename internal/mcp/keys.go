package mcp

import (
	"fmt"
	"strings"
)

// USB HID keyboard usages (page 0x07), matching desktop/console.cpp.
const (
	usageCtrl  = 224
	usageShift = 225
	usageAlt   = 226
	usageEnter = 40
	usageTab   = 43
)

var namedKeys = map[string]int{
	"enter": 40, "return": 40, "esc": 41, "escape": 41, "backspace": 42, "tab": 43,
	"space": 44, "capslock": 57, "printscreen": 70, "scrolllock": 71, "pause": 72,
	"insert": 73, "home": 74, "pageup": 75, "delete": 76, "del": 76, "end": 77,
	"pagedown": 78, "right": 79, "left": 80, "down": 81, "up": 82, "numlock": 83,
	"menu": 101,
	"ctrl": 224, "control": 224, "lctrl": 224, "shift": 225, "lshift": 225,
	"alt": 226, "lalt": 226, "meta": 227, "super": 227, "win": 227, "lmeta": 227,
	"rctrl": 228, "rshift": 229, "ralt": 230, "altgr": 230, "rmeta": 231,
}

// KeyUsage resolves a key name such as "Enter", "F5", "a" or "7" to a HID usage.
func KeyUsage(name string) (int, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if u, ok := namedKeys[n]; ok {
		return u, nil
	}
	if len(n) >= 2 && n[0] == 'f' {
		var f int
		if _, err := fmt.Sscanf(n[1:], "%d", &f); err == nil && f >= 1 && f <= 12 && fmt.Sprint(f) == n[1:] {
			return 57 + f, nil
		}
	}
	if len(n) == 1 {
		if u, shift, ok := CharUsage(rune(n[0])); ok && !shift {
			return u, nil
		}
	}
	return 0, fmt.Errorf("unknown key %q", name)
}

var shiftedDigits = ")!@#$%^&*("
var punct = map[rune]int{'-': 45, '=': 46, '[': 47, ']': 48, '\\': 49, ';': 51, '\'': 52, '`': 53, ',': 54, '.': 55, '/': 56}
var shiftedPunct = map[rune]int{'_': 45, '+': 46, '{': 47, '}': 48, '|': 49, ':': 51, '"': 52, '~': 53, '<': 54, '>': 55, '?': 56}

// CharUsage maps one US-layout ASCII character to its HID usage and whether Shift is needed.
func CharUsage(r rune) (usage int, shift, ok bool) {
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
		return usageEnter, false, true
	case r == '\t':
		return usageTab, false, true
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
