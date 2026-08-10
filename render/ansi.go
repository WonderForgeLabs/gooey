package render

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Synchronized output (DEC mode 2026): everything between BeginSync and
// EndSync is presented by the terminal as ONE atomic update instead of
// being drawn as it arrives.
//
// A frame here is a full-buffer repaint of thousands of cells, so a
// terminal that refreshes mid-write shows a half-old, half-new screen —
// the tearing seen during hot reload, where the top of the tree was the
// new tree and the bottom was still the old one. Bracketing the write
// makes that unobservable.
//
// It is emitted UNCONDITIONALLY and needs no capability check: an
// unrecognized DECSET/DECRST is defined to be ignored, so on a terminal
// without mode 2026 these are eight bytes that do nothing.
const (
	BeginSync = "\x1b[?2026h"
	EndSync   = "\x1b[?2026l"
)

// Flush writes the WHOLE buffer to w as ANSI escape sequences, encoding
// color at the given depth — the buffer itself is always 24-bit, so
// downsampling happens here and nowhere else. The write is bracketed in
// synchronized output, so the frame lands atomically.
//
// This is the one-shot path: every cell, every time, no memory of what
// the terminal was showing. It is what a screenshot wants and what
// gooey.Compose does. An interactive host wants Flusher instead, which
// remembers the previous buffer and sends only the difference.
func Flush(w io.Writer, b *Buffer, depth ColorDepth) error {
	return FlushCells(w, b, depth, true)
}

// FlushCells is Flush with the synchronization bracket under the
// caller's control. A host that emits more than the cell plane in one
// frame — graphics placements after the cells, in Frame.Flush — brackets
// the whole sequence itself rather than nesting a second one here.
func FlushCells(w io.Writer, b *Buffer, depth ColorDepth, sync bool) error {
	var out []byte
	if sync {
		out = append(out, BeginSync...)
	}
	out = append(out, "\x1b[H"...) // home
	var e emitter
	for y := 0; y < b.H; y++ {
		if y > 0 {
			out = append(out, "\r\n"...)
		}
		out = e.run(out, b, 0, b.W, y, depth)
	}
	out = append(out, "\x1b[0m"...)
	if sync {
		out = append(out, EndSync...)
	}
	_, err := w.Write(out)
	return err
}

func sgr(s Style, depth ColorDepth) string {
	parts := []string{"0"}
	if s.Bold {
		parts = append(parts, "1")
	}
	if s.Underline {
		parts = append(parts, "4")
	}
	if s.Reverse {
		parts = append(parts, "7")
	}
	if s.Fg.Set {
		parts = append(parts, colorSGR(s.Fg, depth, true))
	}
	if s.Bg.Set {
		parts = append(parts, colorSGR(s.Bg, depth, false))
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

// colorSGR encodes one color as SGR parameters for the given depth.
// The three forms are genuinely different escape grammars, not a
// truncation of one another: 38;2;r;g;b, 38;5;idx, and the bare
// 30-37/90-97 (40-47/100-107 for background) attribute numbers.
func colorSGR(c Color, depth ColorDepth, fg bool) string {
	lead := 48
	if fg {
		lead = 38
	}
	switch depth {
	case Color256:
		return fmt.Sprintf("%d;5;%d", lead, Quantize256(c))
	case Color16:
		i := Quantize16(c)
		base := 40
		if fg {
			base = 30
		}
		if i >= 8 { // bright half: the aixterm 90-97 / 100-107 range
			base += 60
			i -= 8
		}
		return strconv.Itoa(base + i)
	default:
		return fmt.Sprintf("%d;2;%d;%d;%d", lead, c.R, c.G, c.B)
	}
}
