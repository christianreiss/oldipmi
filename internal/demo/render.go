package demo

import (
	"fmt"
	"oldipmi/internal/hid"
	"oldipmi/internal/protocol"
	"strings"
)

type rgb [3]byte

var (
	bg      = rgb{0x1b, 0x28, 0x38}
	grid    = rgb{0x2a, 0x3c, 0x52}
	header  = rgb{0x2f, 0x4f, 0x6f}
	white   = rgb{0xf0, 0xf0, 0xf0}
	gray    = rgb{0x9a, 0xa8, 0xb8}
	yellow  = rgb{0xff, 0xd8, 0x4a}
	green   = rgb{0x5c, 0xe0, 0x7a}
	red     = rgb{0xff, 0x40, 0x40}
	orange  = rgb{0xff, 0xa0, 0x30}
	cyan    = rgb{0x40, 0xd0, 0xff}
	textBox = rgb{0x10, 0x18, 0x22}
)

type canvas struct {
	w, h int
	pix  []byte
}

func (c *canvas) set(x, y int, col rgb) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	i := (y*c.w + x) * 4
	c.pix[i], c.pix[i+1], c.pix[i+2], c.pix[i+3] = col[0], col[1], col[2], 255
}

func (c *canvas) rect(x, y, w, h int, col rgb) {
	for yy := max(y, 0); yy < min(y+h, c.h); yy++ {
		for xx := max(x, 0); xx < min(x+w, c.w); xx++ {
			c.set(xx, yy, col)
		}
	}
}

// text draws s with the 5x7 font at the given integer scale; returns the x after the text.
func (c *canvas) text(x, y int, s string, scale int, col rgb) int {
	for _, r := range s {
		g, ok := glyphs[r]
		if !ok {
			g = unknownGlyph
		}
		for row := 0; row < 7; row++ {
			for colIdx := 0; colIdx < 5; colIdx++ {
				if g[row][colIdx] == '#' {
					c.rect(x+colIdx*scale, y+row*scale, scale, scale, col)
				}
			}
		}
		x += 6 * scale
	}
	return x
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// wrap splits typed text into display lines of at most cols characters.
func wrap(typed []rune, cols int) []string {
	var lines []string
	cur := []rune{}
	for _, r := range typed {
		switch r {
		case '\n':
			lines = append(lines, string(cur))
			cur = cur[:0]
			continue
		case '\t':
			cur = append(cur, []rune("    ")...)
		default:
			cur = append(cur, r)
		}
		for len(cur) > cols {
			lines = append(lines, string(cur[:cols]))
			cur = append([]rune{}, cur[cols:]...)
		}
	}
	return append(lines, string(cur))
}

// render draws the current state; the caller holds s.mu.
func (s *Session) render() protocol.Frame {
	c := &canvas{w: s.w, h: s.h, pix: make([]byte, s.w*s.h*4)}
	if s.power == "off" {
		c.rect(0, 0, s.w, s.h, rgb{0, 0, 0})
		c.text(20, 20, "Host powered off (demo). Send power 'on' to continue.", 2, gray)
		c.text(20, 50, s.message, 2, gray)
		return protocol.Frame{Width: s.w, Height: s.h, Pixels: c.pix}
	}
	c.rect(0, 0, s.w, s.h, bg)
	// Coordinate grid every 100 px, labelled, so pointer positions can be read off.
	for x := 100; x < s.w; x += 100 {
		c.rect(x, 0, 1, s.h, grid)
		c.text(x+2, s.h-10, fmt.Sprint(x), 1, gray)
	}
	for y := 100; y < s.h; y += 100 {
		c.rect(0, y, s.w, 1, grid)
		c.text(2, y+2, fmt.Sprint(y), 1, gray)
	}
	media := "none"
	if s.media != "" {
		media = s.media
	}
	c.rect(0, 0, s.w, 28, header)
	c.text(10, 7, fmt.Sprintf("OLDIPMI DEMO %dx%d  power:%s  media:%s  caps:%s num:%s scroll:%s",
		s.w, s.h, s.power, media, onOff(s.caps), onOff(s.num), onOff(s.scroll)), 2, white)

	const cell, line, left = 12, 18, 20
	cols := max(10, (s.w-2*left-16)/cell)
	y := 40
	c.text(left, y, "Typed text:", 2, gray)
	y += line + 4
	const rows = 8
	c.rect(left, y-4, cols*cell+16, rows*line+8, textBox)
	lines := wrap(s.typed, cols)
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	for i, l := range lines {
		x := c.text(left+8, y+i*line, l, 2, yellow)
		if i == len(lines)-1 {
			c.rect(x, y+i*line+14, 10, 2, yellow) // cursor
		}
	}
	y += rows*line + 16
	c.text(left, y, "Key events:", 2, gray)
	y += line
	for i, e := range s.events {
		c.text(left+8, y+i*line, e, 2, white)
	}
	y += maxEvents*line + 8
	var held []string
	for u := 4; u <= 231; u++ {
		if s.held[u] {
			held = append(held, hid.Name(u))
		}
	}
	if len(held) == 0 {
		held = []string{"-"}
	}
	c.text(left, y, "Held keys: "+strings.Join(held, " "), 2, white)
	y += line
	btn := []byte("---")
	for bit, ch := range "LRM" {
		if s.buttons&(1<<bit) != 0 {
			btn[bit] = byte(ch)
		}
	}
	c.text(left, y, fmt.Sprintf("Pointer: x=%d y=%d buttons=%s wheel=%+d clicks L=%d R=%d M=%d",
		s.px, s.py, btn, s.wheel, s.clicks[0], s.clicks[1], s.clicks[2]), 2, white)
	y += line
	c.text(left, y, fmt.Sprintf("Received: %d key events, %d pointer events", s.keyEvents, s.ptrCount), 2, gray)
	y += line
	c.text(left, y, s.message, 2, green)

	for _, m := range s.marks {
		col := map[uint8]rgb{1: green, 2: orange, 4: cyan}[m.button]
		c.rect(m.x-4, m.y-4, 9, 9, col)
		c.rect(m.x-2, m.y-2, 5, 5, bg)
	}
	// Pointer crosshair.
	c.rect(s.px-12, s.py, 25, 1, red)
	c.rect(s.px, s.py-12, 1, 25, red)
	c.rect(s.px-1, s.py-1, 3, 3, red)
	return protocol.Frame{Width: s.w, Height: s.h, Pixels: c.pix}
}
