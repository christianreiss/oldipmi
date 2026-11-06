package hid

import (
	"strings"
	"testing"
)

func TestNamesResolve(t *testing.T) {
	want := map[string]int{
		"Enter": 40, "esc": 41, "Backspace": 42, "Tab": 43, "space": 44,
		"F1": 58, "f12": 69, "F13": 104, "F24": 115,
		"Up": 82, "Down": 81, "Left": 80, "Right": 79, "Home": 74, "End": 77, "PgUp": 75, "PageDown": 78,
		"Insert": 73, "Del": 76, "PrintScreen": 70, "SysRq": 70, "Pause": 72, "Break": 72,
		"CapsLock": 57, "NumLock": 83, "ScrollLock": 71, "Menu": 101,
		"ctrl": 224, "LeftShift": 225, "alt": 226, "Super": 227, "Win": 227, "rctrl": 228, "RShift": 229, "AltGr": 230, "rwin": 231,
		"KP_Enter": 88, "kp-1": 89, "KP_9": 97, "kp0": 98, "KP_Decimal": 99, "kp_divide": 84, "KP_Multiply": 85, "KP_Minus": 86, "KP_Plus": 87, "KP_Equal": 103,
		"a": 4, "Z": 29, "7": 36, "0": 39, "-": 45, "/": 56, "minus": 45, "Backslash": 49, "Grave": 53,
	}
	for n, u := range want {
		if got, err := Usage(n); err != nil || got != u {
			t.Errorf("%s = %d, %v; want %d", n, got, err, u)
		}
	}
	for _, bad := range []string{"F0", "F25", "nonsense", "", "A!", "!", "+"} {
		if _, err := Usage(bad); err == nil {
			t.Errorf("%q must fail", bad)
		}
	}
	if _, err := Usage("!"); err == nil || !strings.Contains(err.Error(), "Shift") {
		t.Errorf("shifted char needs a hint: %v", err)
	}
}

func TestCombos(t *testing.T) {
	for spec, want := range map[string][]int{
		"ctrl+alt+delete": {224, 226, 76}, "Alt+SysRq+b": {226, 70, 5}, "Ctrl+Alt+F1": {224, 226, 58}, "shift+tab": {225, 43},
	} {
		got, err := Combo(spec)
		if err != nil || len(got) != len(want) {
			t.Fatalf("%s: %v %v", spec, got, err)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: %v", spec, got)
			}
		}
	}
	if _, err := Combo("ctrl++"); err == nil {
		t.Fatal("empty part must fail")
	}
}

func TestCharsRoundTrip(t *testing.T) {
	seen := map[[2]int]rune{}
	for r := rune(32); r < 127; r++ {
		u, sh, ok := Char(r)
		if !ok {
			t.Fatalf("%q not typable", r)
		}
		k := [2]int{u, b2i(sh)}
		if o, dup := seen[k]; dup {
			t.Fatalf("%q and %q map to the same key", r, o)
		}
		seen[k] = r
		if back, ok := Rune(u, sh); !ok || back != r {
			t.Fatalf("%q -> %d/%v -> %q", r, u, sh, back)
		}
	}
	if r, _ := Rune(88, false); r != '\n' {
		t.Fatal("KP_Enter")
	}
	if _, _, ok := Char('é'); ok {
		t.Fatal("non-ASCII must fail")
	}
}

func TestKeysListedOnceWithCanonicalNames(t *testing.T) {
	seen := map[int]bool{}
	for _, k := range Keys() {
		if seen[k.Usage] {
			t.Fatalf("usage %d listed twice (%s)", k.Usage, k.Name)
		}
		seen[k.Usage] = true
		if u, err := Usage(k.Name); err != nil || u != k.Usage {
			t.Fatalf("%s does not resolve", k.Name)
		}
		for _, a := range k.Aliases {
			if u, err := Usage(a); err != nil || u != k.Usage {
				t.Fatalf("alias %s does not resolve", a)
			}
		}
	}
	if Name(70) != "PrintScreen" || Name(4) != "A" || Name(104) != "F13" || Name(88) != "KP_Enter" {
		t.Fatal(Name(70), Name(4), Name(104), Name(88))
	}
}
