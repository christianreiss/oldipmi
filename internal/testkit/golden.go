// Package testkit holds helpers for the tests of the console families: it runs
// a session until it has shown its frames and compares them with golden
// pictures. Only tests import it.
package testkit

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"oldipmi/internal/protocol"
)

// Collect runs a console session until it has shown n frames and returns
// copies of them: a session may reuse its buffers once the callback returns,
// because the backend copies each frame there too.
func Collect(t testing.TB, n int, run func(context.Context, func(protocol.Frame)) error) []protocol.Frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var frames []protocol.Frame
	err := run(ctx, func(f protocol.Frame) {
		if len(frames) < n {
			frames = append(frames, protocol.Frame{Width: f.Width, Height: f.Height, Pixels: append([]byte(nil), f.Pixels...)})
		}
		if len(frames) == n {
			cancel()
		}
	})
	if len(frames) != n {
		t.Fatalf("the session showed %d of %d frames, then ended: %v", len(frames), n, err)
	}
	return frames
}

// Frames compares frames with the golden pictures testdata/frames/<name>-<n>.png
// of the package under test, n counting from 1. With UPDATE_GOLDEN=1 in the
// environment it writes them instead; look at the new pictures before
// committing them. The desktop draws frames as RGBA, so every pixel must also
// be opaque.
func Frames(t testing.TB, name string, frames []protocol.Frame) {
	t.Helper()
	update := os.Getenv("UPDATE_GOLDEN") == "1"
	path := func(n int) string { return filepath.Join("testdata", "frames", fmt.Sprintf("%s-%d.png", name, n)) }
	for i, f := range frames {
		if len(f.Pixels) != f.Width*f.Height*4 {
			t.Errorf("frame %d: %d bytes for %dx%d", i+1, len(f.Pixels), f.Width, f.Height)
			continue
		}
		for p := 3; p < len(f.Pixels); p += 4 {
			if f.Pixels[p] != 255 {
				t.Errorf("frame %d: pixel %d is not opaque", i+1, p/4)
				break
			}
		}
		got := &image.NRGBA{Pix: f.Pixels, Stride: f.Width * 4, Rect: image.Rect(0, 0, f.Width, f.Height)}
		if update {
			write(t, path(i+1), got)
			continue
		}
		want, err := read(path(i + 1))
		if err != nil {
			t.Errorf("frame %d: %v (UPDATE_GOLDEN=1 writes the golden pictures)", i+1, err)
			continue
		}
		if d := compare(got, want); d != "" {
			t.Errorf("frame %d differs from %s: %s", i+1, path(i+1), d)
		}
	}
	for n := len(frames) + 1; ; n++ {
		if _, err := os.Stat(path(n)); err != nil {
			break
		}
		if !update {
			t.Errorf("%s exists, but the session showed %d frames", path(n), len(frames))
			break
		}
		if err := os.Remove(path(n)); err != nil {
			t.Fatal(err)
		}
	}
}

func write(t testing.TB, path string, m image.Image) {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := image.NewNRGBA(image.Rect(0, 0, m.Bounds().Dx(), m.Bounds().Dy()))
	draw.Draw(out, out.Rect, m, m.Bounds().Min, draw.Src)
	return out, nil
}

// compare names how many pixels differ and the first of them.
func compare(got, want *image.NRGBA) string {
	if got.Rect.Size() != want.Rect.Size() {
		return fmt.Sprintf("size %v, want %v", got.Rect.Size(), want.Rect.Size())
	}
	diff, first := 0, -1
	for i := 0; i < len(got.Pix); i += 4 {
		if !bytes.Equal(got.Pix[i:i+4], want.Pix[i:i+4]) {
			if first < 0 {
				first = i
			}
			diff++
		}
	}
	if diff == 0 {
		return ""
	}
	w := got.Rect.Dx()
	return fmt.Sprintf("%d pixels, the first at %d,%d: RGBA %v, want %v", diff, first/4%w, first/4/w, got.Pix[first:first+4], want.Pix[first:first+4])
}
