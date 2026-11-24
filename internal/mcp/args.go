package mcp

import (
	"fmt"
	"oldipmi/internal/hid"
	"strings"
)

// A tool's arguments are specs. Each spec gives the JSON schema of one
// argument and reads its value within the same limits, so that what agents
// are told and what the tool checks cannot drift apart. An argument that is
// null counts as not given; one of the wrong type, or beyond its limits, is
// refused with a message that names it. Rules that JSON schema does not say,
// such as max_size's gap below 64, stay with the tool and in its description.

// arg is what a tool's input schema needs of a spec.
type arg interface {
	key() string
	schema() map[string]any
	required() bool
}

// params is the input schema of a tool that takes args; the required ones
// are listed in the order given.
func params(args ...arg) map[string]any {
	props := map[string]any{}
	var req []string
	for _, a := range args {
		props[a.key()] = a.schema()
		if a.required() {
			req = append(req, a.key())
		}
	}
	return obj(props, req...)
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// given reports whether the arguments hold k with a value other than null.
func given(a map[string]any, k string) bool {
	v, ok := a[k]
	return ok && v != nil
}

// intSpec is an integer in Lo..Hi, and not 0 when NonZero. Without a value
// it is Def, or required when Req.
type intSpec struct {
	Key, Desc   string
	Lo, Hi, Def int
	Req         bool
	NonZero     bool
}

func (s intSpec) key() string    { return s.Key }
func (s intSpec) required() bool { return s.Req }
func (s intSpec) schema() map[string]any {
	return map[string]any{"type": "integer", "description": s.Desc, "minimum": s.Lo, "maximum": s.Hi}
}
func (s intSpec) get(a map[string]any) (int, error) {
	if !given(a, s.Key) {
		if s.Req {
			return 0, fmt.Errorf("%s is required", s.Key)
		}
		return s.Def, nil
	}
	f, ok := a[s.Key].(float64)
	if !ok || f != float64(int(f)) || int(f) < s.Lo || int(f) > s.Hi || s.NonZero && f == 0 {
		what := "an integer"
		if s.NonZero {
			what = "a non-zero integer"
		}
		return 0, fmt.Errorf("%s must be %s in %d..%d", s.Key, what, s.Lo, s.Hi)
	}
	return int(f), nil
}

// numSpec is an optional number in Lo..Hi; get reads it once it is given.
type numSpec struct {
	Key, Desc string
	Lo, Hi    float64
}

func (s numSpec) key() string    { return s.Key }
func (s numSpec) required() bool { return false }
func (s numSpec) schema() map[string]any {
	return map[string]any{"type": "number", "description": s.Desc, "minimum": s.Lo, "maximum": s.Hi}
}
func (s numSpec) get(a map[string]any) (float64, error) {
	f, ok := a[s.Key].(float64)
	if !ok || f < s.Lo || f > s.Hi {
		return 0, fmt.Errorf("%s must be a number in %g..%g", s.Key, s.Lo, s.Hi)
	}
	return f, nil
}

// strSpec is a non-empty string; without a value it is "", or required when
// Req.
type strSpec struct {
	Key, Desc string
	Req       bool
}

func (s strSpec) key() string    { return s.Key }
func (s strSpec) required() bool { return s.Req }
func (s strSpec) schema() map[string]any {
	return map[string]any{"type": "string", "description": s.Desc}
}
func (s strSpec) get(a map[string]any) (string, error) {
	if !given(a, s.Key) {
		if s.Req {
			return "", fmt.Errorf("%s is required", s.Key)
		}
		return "", nil
	}
	v, ok := a[s.Key].(string)
	if !ok || v == "" {
		return "", fmt.Errorf("%s must be a non-empty string", s.Key)
	}
	return v, nil
}

// boolSpec is true or false; without a value it is false.
type boolSpec struct{ Key, Desc string }

func (s boolSpec) key() string    { return s.Key }
func (s boolSpec) required() bool { return false }
func (s boolSpec) schema() map[string]any {
	return map[string]any{"type": "boolean", "description": s.Desc}
}
func (s boolSpec) get(a map[string]any) (bool, error) {
	if !given(a, s.Key) {
		return false, nil
	}
	v, ok := a[s.Key].(bool)
	if !ok {
		return false, fmt.Errorf("%s must be true or false", s.Key)
	}
	return v, nil
}

// enumSpec is one of Values, in any case, read as Values spells it. Without
// a value it is Def. A spec with no default is required unless Optional.
type enumSpec struct {
	Key, Desc string
	Values    []string
	Def       string
	Optional  bool
}

func (s enumSpec) key() string    { return s.Key }
func (s enumSpec) required() bool { return s.Def == "" && !s.Optional }
func (s enumSpec) schema() map[string]any {
	m := map[string]any{"type": "string", "enum": s.Values}
	if s.Desc != "" {
		m["description"] = s.Desc
	}
	return m
}
func (s enumSpec) get(a map[string]any) (string, error) {
	if !given(a, s.Key) {
		if s.required() {
			return "", fmt.Errorf("%s is required", s.Key)
		}
		return s.Def, nil
	}
	if v, ok := a[s.Key].(string); ok {
		for _, w := range s.Values {
			if strings.EqualFold(v, w) {
				return w, nil
			}
		}
	}
	last := len(s.Values) - 1
	return "", fmt.Errorf("%s must be %s or %s", s.Key, strings.Join(s.Values[:last], ", "), s.Values[last])
}

// keysSpec is a list of Min..Max key names, read as HID usages. It is
// required when Min is above 0; without a value it is empty.
type keysSpec struct {
	Key, Desc string
	Min, Max  int
}

func (s keysSpec) key() string    { return s.Key }
func (s keysSpec) required() bool { return s.Min > 0 }
func (s keysSpec) schema() map[string]any {
	m := map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": s.Max, "description": s.Desc}
	if s.Min > 0 {
		m["minItems"] = s.Min
	}
	return m
}
func (s keysSpec) get(a map[string]any) ([]int, error) {
	bad := fmt.Errorf("%s must list %d..%d key names", s.Key, s.Min, s.Max)
	if s.Min == 0 {
		bad = fmt.Errorf("%s must list up to %d key names", s.Key, s.Max)
	}
	var names []any
	if given(a, s.Key) {
		var ok bool
		if names, ok = a[s.Key].([]any); !ok {
			return nil, bad
		}
	}
	if len(names) < s.Min || len(names) > s.Max {
		return nil, bad
	}
	var us []int
	for _, v := range names {
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
