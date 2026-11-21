package mcp

import (
	"context"
	"fmt"
	"time"
)

// screenTools look at the remote screen and wait for it to change or settle.
func screenTools() []Tool {
	return []Tool{
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
		{Name: "wait", Title: "Wait", Global: true, ReadOnly: true, Idempotent: true,
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
	}
}

// The waits' arguments.
var (
	waitTimeoutArg = intSpec{Key: "timeout_ms", Desc: "default 10000", Hi: maxWaitMS, Def: 10000}
	quietArg       = intSpec{Key: "quiet_ms", Desc: "default 500", Hi: maxWaitMS, Def: 500}
	msArg          = intSpec{Key: "ms", Desc: "milliseconds", Hi: maxWaitMS, Req: true}
)

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
