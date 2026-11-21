// Package demo is a synthetic console backend (family "demo") that needs no BMC.
// It renders a test screen that echoes every keystroke, the pointer position and
// clicks, and accepts power and virtual-media commands without effect, so the
// whole stack (MCP -> app -> oldipmi-core -> session -> frames) can be verified.
//
// The profile's host selects variants: "WxH" anywhere in it sets the
// framebuffer size (e.g. "demo-1920x1080"), "fail" makes the login fail, "slow"
// makes each setup stage take two seconds, "hang" keeps the login waiting until
// it is cancelled, and "unreachable" fails the login as an unreachable BMC does.
package demo

import (
	"context"
	"fmt"
	"oldipmi/internal/hid"
	"oldipmi/internal/protocol"
	"oldipmi/internal/video/raster"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultWidth  = 1024
	DefaultHeight = 768
	maxTyped      = 4000
	maxEvents     = 8
	maxMarks      = 16
)

var sizeRE = regexp.MustCompile(`(\d{3,4})x(\d{3,4})`)

// Size returns the framebuffer size a demo host name selects.
func Size(host string) (int, int) {
	m := sizeRE.FindStringSubmatch(strings.ToLower(host))
	if m == nil {
		return DefaultWidth, DefaultHeight
	}
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	return min(max(w, 320), raster.MaxWidth), min(max(h, 200), raster.MaxHeight)
}

// Connect walks through the usual setup stages and returns a demo session.
func Connect(ctx context.Context, p protocol.Profile) (protocol.Console, error) {
	host := strings.ToLower(p.Host)
	step := 150 * time.Millisecond
	if strings.Contains(host, "slow") {
		step = 2 * time.Second
	}
	wait := func() error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(step):
			return nil
		}
	}
	if strings.Contains(host, "hang") {
		<-ctx.Done() // a login that never answers, independent of any timing
		return nil, ctx.Err()
	}
	if err := wait(); err != nil {
		return nil, err
	}
	if strings.Contains(host, "fail") {
		return nil, fmt.Errorf("demo login rejected (the host name contains \"fail\")")
	}
	if strings.Contains(host, "unreachable") {
		return nil, fmt.Errorf("demo BMC unreachable (the host name contains \"unreachable\"): %w", syscall.EHOSTUNREACH)
	}
	info := &protocol.ConsoleInfo{Protocol: "OldIPMI demo (synthetic, no network)",
		Endpoints:    []protocol.Endpoint{{Role: "Video / input", Address: "in-process", Transport: "none"}},
		Capabilities: []string{"Video", "Keyboard", "Pointer", "Power", "Read-only ISO"}}
	protocol.Report(ctx, protocol.StageDiscovering, info)
	if err := wait(); err != nil {
		return nil, err
	}
	protocol.Report(ctx, protocol.StageConnecting, info)
	if err := wait(); err != nil {
		return nil, err
	}
	return New(Size(p.Host)), nil
}

type mark struct {
	x, y   int
	button uint8
}

// Session is the demo console state.
type Session struct {
	mu                  sync.Mutex
	w, h                int
	typed               []rune
	events              []string
	eventNo             int
	held                hid.Pressed
	tap                 map[int]bool
	caps, num, scroll   bool
	px, py, wheel       int
	buttons             uint8
	clicks              [3]int
	marks               []mark
	keyEvents, ptrCount int
	power, message      string
	media               string
	dirty, closed       chan struct{}
	once                sync.Once
}

// New returns a powered-on demo session with the given framebuffer size.
func New(w, h int) *Session {
	return &Session{w: w, h: h, tap: map[int]bool{}, power: "on", num: true,
		px: w / 2, py: h / 2, message: "Ready. Everything you send is echoed on this screen.",
		dirty: make(chan struct{}, 1), closed: make(chan struct{})}
}

func (s *Session) changed() {
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

// Run emits a frame now and after every state change.
func (s *Session) Run(ctx context.Context, emit func(protocol.Frame)) error {
	for {
		s.mu.Lock()
		f := s.render()
		s.mu.Unlock()
		emit(f)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.closed:
			return nil
		case <-s.dirty:
		}
	}
}

func (s *Session) event(e string) {
	s.eventNo++
	s.events = append(s.events, fmt.Sprintf("#%d %s", s.eventNo, e))
	if len(s.events) > maxEvents {
		s.events = s.events[len(s.events)-maxEvents:]
	}
}

func (s *Session) heldMods(except ...int) []string {
	var names []string
	for u := hid.LeftCtrl; u <= hid.RightSuper; u++ {
		skip := false
		for _, e := range except {
			skip = skip || e == u
		}
		if s.held.Has(uint32(u)) && !skip {
			names = append(names, hid.Name(u))
		}
	}
	return names
}

func (s *Session) Key(i protocol.Input) error {
	if !hid.ValidKey(i.Usage) {
		return hid.ErrInvalidUsage
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.changed()
	u := int(i.Usage)
	s.keyEvents++
	if !i.Down {
		s.held.Set(i.Usage, false)
		if s.tap[u] {
			delete(s.tap, u)
			s.event("<" + hid.Name(u) + ">")
		}
		return nil
	}
	s.held.Set(i.Usage, true)
	if hid.IsModifier(u) {
		s.tap[u] = true
		return nil
	}
	clear(s.tap)
	switch u {
	case 57:
		s.caps = !s.caps
	case 83:
		s.num = !s.num
	case 71:
		s.scroll = !s.scroll
	}
	shift := s.held.Has(hid.LeftShift) || s.held.Has(hid.RightShift)
	chord := s.heldMods(hid.LeftShift, hid.RightShift)
	if len(chord) > 0 { // Ctrl/Alt/Super chords are commands, not text.
		s.event("<" + s.chord(u) + ">")
		return nil
	}
	r, ok := hid.Rune(u, shift)
	if ok && u >= 4 && u <= 29 && s.caps {
		r, _ = hid.Rune(u, !shift)
	}
	if u >= 89 && u <= 99 && !s.num { // keypad without NumLock acts as navigation
		ok = false
	}
	switch {
	case u == hid.Backspace:
		if len(s.typed) > 0 {
			s.typed = s.typed[:len(s.typed)-1]
		}
		s.event("<Backspace>")
	case ok:
		s.typed = append(s.typed, r)
		if len(s.typed) > maxTyped {
			s.typed = s.typed[len(s.typed)-maxTyped:]
		}
		if r == '\n' || r == '\t' {
			s.event("<" + hid.Name(u) + ">")
		}
	default:
		s.event("<" + s.chord(u) + ">")
	}
	return nil
}

func (s *Session) Pointer(i protocol.Input) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.changed()
	s.ptrCount++
	s.px, s.py = min(max(i.X, 0), s.w-1), min(max(i.Y, 0), s.h-1)
	b := i.Buttons & 7
	for bit := 0; bit < 3; bit++ {
		if b&(1<<bit) != 0 && s.buttons&(1<<bit) == 0 {
			s.clicks[bit]++
			s.marks = append(s.marks, mark{s.px, s.py, 1 << bit})
			if len(s.marks) > maxMarks {
				s.marks = s.marks[1:]
			}
		}
	}
	s.buttons = b
	if i.Wheel != 0 {
		s.wheel += max(-1, min(1, i.Wheel))
	}
	return nil
}

func (s *Session) Release() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held.Clear()
	clear(s.tap)
	s.buttons = 0
	s.changed()
	return nil
}

func (s *Session) Mount(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.media != "" {
		return fmt.Errorf("eject the mounted ISO first")
	}
	s.media = filepath.Base(path)
	s.message = "Mounted " + s.media + " read-only (demo: nothing is read)."
	s.changed()
	return nil
}

func (s *Session) Eject() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.media != "" {
		s.message = "Ejected " + s.media + " (demo)."
	}
	s.media = ""
	s.changed()
	return nil
}

func (s *Session) Mounted() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.media != "" }

// MediaErr is always nil: the demo's ISO goes away only by Eject.
func (s *Session) MediaErr() error { return nil }

func (s *Session) Power(_ context.Context, action protocol.PowerAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch action {
	case protocol.PowerOn, protocol.PowerReset:
		s.power = "on"
	case protocol.PowerOff, protocol.PowerSoft:
		s.power = "off"
	default:
		return protocol.ErrUnknownPowerAction
	}
	s.message = "Power '" + string(action) + "' accepted (demo: no real host is affected)."
	s.changed()
	return nil
}

func (s *Session) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

// Typed returns the echoed text (for tests).
func (s *Session) Typed() string { s.mu.Lock(); defer s.mu.Unlock(); return string(s.typed) }

// Events returns the most recent key events (for tests).
func (s *Session) Events() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

// chord names the held modifiers, then other held keys, then u, e.g. "Alt+PrintScreen+X".
func (s *Session) chord(u int) string {
	names := s.heldMods()
	for _, k := range s.held.Keys() {
		if k < hid.LeftCtrl && int(k) != u {
			names = append(names, hid.Name(int(k)))
		}
	}
	return strings.Join(append(names, hid.Name(u)), "+")
}
