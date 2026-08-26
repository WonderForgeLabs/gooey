package components

import (
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Text paints a string, one buffer row per line, clipped to its
// arranged bounds. Content and Style are property handles, so a Text
// bound to a computed repaints exactly when that computed changes.
type Text struct {
	gooey.Base
	Content *prop.Property[string]
	Style   *prop.Property[render.Style]
}

func (t *Text) Measure(avail gooey.Size) gooey.Size {
	lines := strings.Split(getStr(t.Content), "\n")
	w := 0
	for _, l := range lines {
		// DISPLAY COLUMNS, not runes. A rune count under-reports any line
		// holding a CJK character or emoji, so layout allocated too few
		// columns and the Text overran its neighbour — with no error,
		// because the cells it wrote were exactly the ones it meant to
		// write (#358).
		if n := render.StringWidth(l); n > w {
			w = n
		}
	}
	return gooey.Size{W: min(w, avail.W), H: min(len(lines), avail.H)}
}

func (t *Text) Render(f *gooey.Frame) {
	style := getSty(t.Style)
	for i, line := range strings.Split(getStr(t.Content), "\n") {
		if i >= t.Bounds().H {
			break
		}
		f.Cells.SetString(t.Bounds().X, t.Bounds().Y+i, clipRunes(line, t.Bounds().W), style)
	}
}
