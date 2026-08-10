package render

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Flush writes the whole buffer to w as ANSI escape sequences, encoding
// color at the given depth — the buffer itself is always 24-bit, so
// downsampling happens here and nowhere else.
//
// POC note: full repaint every frame. The retained tree makes damage-rect
// diffing (compare against previous buffer, emit only changed spans) a
// drop-in replacement here — deliberately out of scope for the POC.
func Flush(w io.Writer, b *Buffer, depth ColorDepth) error {
	var sb strings.Builder
	sb.WriteString("\x1b[H") // home
	var cur Style
	styleSet := false
	for y := 0; y < b.H; y++ {
		if y > 0 {
			sb.WriteString("\r\n")
		}
		for x := 0; x < b.W; x++ {
			c := b.Cells[y*b.W+x]
			if !styleSet || c.Style != cur {
				sb.WriteString(sgr(c.Style, depth))
				cur = c.Style
				styleSet = true
			}
			sb.WriteRune(c.Rune)
		}
	}
	sb.WriteString("\x1b[0m")
	_, err := io.WriteString(w, sb.String())
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
