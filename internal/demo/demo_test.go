package demo

import (
	"context"
	"image"
	"image/png"
	"oldipmi/internal/hid"
	"oldipmi/internal/protocol"
	"oldipmi/internal/testkit"
	"os"
	"strings"
	"testing"
	"time"
)

func press(t *testing.T, s *Session, usages ...int) {
	t.Helper()
	for _, u := range usages {
		if err := s.Key(protocol.Input{Usage: uint32(u), Down: true}); err != nil {
			t.Fatal(err)
		}
	}
	for i := len(usages) - 1; i >= 0; i-- {
		s.Key(protocol.Input{Usage: uint32(usages[i])})
	}
}

func typeText(t *testing.T, s *Session, text string) {
	t.Helper()
	for _, r := range text {
		u, shift, ok := hid.Char(r)
		if !ok {
			t.Fatalf("%q", r)
		}
		if shift {
			press(t, s, hid.LeftShift, u)
		} else {
			press(t, s, u)
		}
	}
}

func TestEchoesTextCombosAndNamedKeys(t *testing.T) {
	s := New(800, 600)
	all := ""
	for r := rune(32); r < 127; r++ {
		all += string(r)
	}
	typeText(t, s, all+"\nab")
	press(t, s, hid.Backspace)
	if got := s.Typed(); got != all+"\na" {
		t.Fatalf("typed %q", got)
	}
	press(t, s, hid.LeftCtrl, hid.LeftAlt, 76)
	press(t, s, hid.LeftAlt, 70, 27) // Alt+SysRq+x
	press(t, s, 104)                 // F13
	press(t, s, hid.LeftSuper)       // modifier tap
	ev := strings.Join(s.Events(), " ")
	for _, want := range []string{"<Ctrl+Alt+Delete>", "<Alt+PrintScreen+X>", "<F13>", "<Super>", "<Backspace>", "<Enter>"} {
		if !strings.Contains(ev, want) {
			t.Errorf("events %q lack %s", ev, want)
		}
	}
	if s.Typed() != all+"\na" {
		t.Fatal("a chord must not type text")
	}
	if err := s.Key(protocol.Input{Usage: 300, Down: true}); err == nil {
		t.Fatal("invalid usage accepted")
	}
}

func TestCapsLockAndKeypad(t *testing.T) {
	s := New(640, 480)
	press(t, s, 57)
	typeText(t, s, "aB")
	press(t, s, 57, 89, 88) // caps off, KP_1, KP_Enter
	if got := s.Typed(); got != "Ab1\n" {
		t.Fatalf("%q", got)
	}
}

func TestPointerPowerMediaAndFrames(t *testing.T) {
	s := New(640, 480)
	ctx, cancel := context.WithCancel(context.Background())
	frames := make(chan protocol.Frame, 64)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, func(f protocol.Frame) { frames <- f }) }()
	first := <-frames
	if first.Width != 640 || first.Height != 480 || len(first.Pixels) != 640*480*4 {
		t.Fatalf("%dx%d %d", first.Width, first.Height, len(first.Pixels))
	}
	s.Pointer(protocol.Input{X: 100, Y: 200})
	s.Pointer(protocol.Input{X: 100, Y: 200, Buttons: 2})
	s.Pointer(protocol.Input{X: 100, Y: 200})
	s.Pointer(protocol.Input{X: 5000, Y: -3, Wheel: 1})
	var last protocol.Frame
	for timeout := time.After(time.Second); ; {
		select {
		case last = <-frames:
		case <-timeout:
			t.Fatal("no frame after input")
		}
		if string(last.Pixels) != string(first.Pixels) {
			break
		}
	}
	s.mu.Lock()
	if s.px != 639 || s.py != 0 || s.clicks[1] != 1 || s.wheel != 1 {
		t.Fatalf("pointer state %d,%d %v %d", s.px, s.py, s.clicks, s.wheel)
	}
	s.mu.Unlock()
	if s.Power(ctx, "explode") == nil || s.Power(ctx, "off") != nil || s.Power(ctx, "on") != nil {
		t.Fatal("power")
	}
	if s.Mount(ctx, "/tmp/x.iso") != nil || !s.Mounted() || s.Mount(ctx, "/tmp/y.iso") == nil || s.Eject() != nil || s.Mounted() {
		t.Fatal("media")
	}
	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatal(err)
	}
	if path := os.Getenv("OLDIPMI_DEMO_PNG"); path != "" { // visual check of the font and layout
		typeText(t, s, "The quick brown fox jumps over the lazy dog. 0123456789 !@#$%^&*()_+-=[]{};':\",./<>?\\|`~")
		f := s.render()
		out, _ := os.Create(path)
		png.Encode(out, &image.NRGBA{Pix: f.Pixels, Stride: f.Width * 4, Rect: image.Rect(0, 0, f.Width, f.Height)})
		out.Close()
	}
}

func TestConnectStagesAndVariants(t *testing.T) {
	var stages []string
	ctx := protocol.WithProgress(context.Background(), func(p protocol.Progress) { stages = append(stages, p.Stage) })
	sess, err := Connect(ctx, protocol.Profile{Family: "demo", Host: "demo-1920x1080"})
	if err != nil || strings.Join(stages, ",") != "discovering,connecting" {
		t.Fatal(err, stages)
	}
	if d := sess.(*Session); d.w != 1920 || d.h != 1080 {
		t.Fatal(d.w, d.h)
	}
	if _, err := Connect(context.Background(), protocol.Profile{Host: "fail"}); err == nil {
		t.Fatal("fail host must fail")
	}
	c, stop := context.WithCancel(context.Background())
	stop()
	if _, err := Connect(c, protocol.Profile{Host: "slow"}); err == nil {
		t.Fatal("cancel must abort setup")
	}
	// It fails as an unreachable BMC does, with that class (errors.json).
	_, err = Connect(context.Background(), protocol.Profile{Host: "demo-unreachable"})
	testkit.ErrorRow(t, "demo: unreachable", err)
	// "hang" stays at the login, before any stage, until it is cancelled.
	stages = nil
	hung, cancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { _, err := Connect(hung, protocol.Profile{Host: "demo-hang"}); result <- err }()
	select {
	case err := <-result:
		t.Fatal("hang host returned:", err)
	case <-time.After(300 * time.Millisecond): // longer than a normal stage
	}
	cancel()
	if err := <-result; err == nil || len(stages) != 0 {
		t.Fatal("hang host after cancel:", err, stages)
	}
	if w, h := Size("x-99999x1"); w != DefaultWidth || h != DefaultHeight {
		t.Fatal(w, h)
	}
}
