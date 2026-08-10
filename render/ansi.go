package render

import (
	"fmt"
	"io"
	"strings"
)

// Flush writes the whole buffer to w as ANSI escape sequences.
// POC note: full repaint every frame. The retained tree makes damage-rect
// diffing (compare against previous buffer, emit only changed spans) a
// drop-in replacement here — deliberately out of scope for the POC.
func Flush(w io.Writer, b *Buffer) error {
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
				sb.WriteString(sgr(c.Style))
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

func sgr(s Style) string {
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
		parts = append(parts, fmt.Sprintf("38;2;%d;%d;%d", s.Fg.R, s.Fg.G, s.Fg.B))
	}
	if s.Bg.Set {
		parts = append(parts, fmt.Sprintf("48;2;%d;%d;%d", s.Bg.R, s.Bg.G, s.Bg.B))
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}
