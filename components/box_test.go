package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/render"
)

// filled returns a buffer painted with '#' everywhere, so any cell the
// box helpers touch is visible as "not '#'". Testing against a blank
// buffer would make a stray SPACE indistinguishable from an untouched
// cell, and spaces out of place are the exact hazard these guards exist
// for — they read as "nothing painted" and overwrite just the same.
func filled(w, h int) *render.Buffer {
	b := render.NewBuffer(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			b.Set(x, y, '#', render.Style{})
		}
	}
	return b
}

// outsideWrites reports every cell outside r that is no longer '#'.
func outsideWrites(t *testing.T, b *render.Buffer, r gooey.Rect) {
	t.Helper()
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			if b.At(x, y).Rune == '#' {
				continue
			}
			if x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
				t.Errorf("wrote %q at (%d,%d) — outside %+v",
					string(b.At(x, y).Rune), x, y, r)
			}
		}
	}
}

func rowString(b *render.Buffer, y, x0, x1 int) string {
	out := []rune{}
	for x := x0; x < x1; x++ {
		out = append(out, b.At(x, y).Rune)
	}
	return string(out)
}

func TestDrawBoxRunesShape(t *testing.T) {
	b := filled(12, 6)
	r := gooey.Rect{X: 1, Y: 1, W: 6, H: 4}
	DrawBoxRunes(b, r, render.Style{})
	want := []string{
		"#╭────╮#",
		"#│####│#",
		"#│####│#",
		"#╰────╯#",
	}
	for i, w := range want {
		if got := rowString(b, r.Y+i, 0, 8); got != w {
			t.Errorf("row %d = %q, want %q", r.Y+i, got, w)
		}
	}
	outsideWrites(t, b, r)
}

// A degenerate rect paints nothing at all. With W or H at zero the
// far-edge arithmetic walks BACKWARDS and the corners land outside the
// rect on both axes — outside the calling paint node's damage rect,
// where the composer's sweep can never clean them.
func TestDrawBoxRunesDegenerateRectPaintsNothing(t *testing.T) {
	for _, r := range []gooey.Rect{
		{X: 3, Y: 2, W: 0, H: 0},
		{X: 3, Y: 2, W: 8, H: 0},
		{X: 3, Y: 2, W: 0, H: 3},
		{X: 3, Y: 2, W: -4, H: -1},
	} {
		b := filled(12, 6)
		DrawBoxRunes(b, r, render.Style{})
		for y := 0; y < b.H; y++ {
			for x := 0; x < b.W; x++ {
				if b.At(x, y).Rune != '#' {
					t.Errorf("%+v painted %q at (%d,%d)", r, string(b.At(x, y).Rune), x, y)
				}
			}
		}
	}
}

// One and two cells wide or tall are NOT degenerate: they paint, and
// every cell they paint is inside the rect.
func TestDrawBoxRunesTinyRectsStayInBounds(t *testing.T) {
	for w := 1; w <= 3; w++ {
		for h := 1; h <= 3; h++ {
			b := filled(12, 8)
			r := gooey.Rect{X: 4, Y: 3, W: w, H: h}
			DrawBoxRunes(b, r, render.Style{})
			outsideWrites(t, b, r)
		}
	}
}

// The title clips to the room the box has, and a box with no room for
// even one rune of title writes NOTHING — not the two pad spaces, which
// at four or five columns wide would land on the far corner and rub it
// out.
func TestDrawBoxTitleClipsAndNeverStrandsPadding(t *testing.T) {
	for _, tc := range []struct {
		w    int
		want string // the top row of the box, corner to corner
	}{
		// Below seven columns the budget (w-6) is zero or less: no
		// title at all, and crucially no pad spaces either.
		{4, "╭──╮"},
		{5, "╭───╮"},
		{6, "╭────╮"},
		// From seven up it clips to w-6 runes, always leaving the far
		// corner and one edge cell before it intact.
		{7, "╭─ t ─╮"},
		{9, "╭─ tit ─╮"},
		{11, "╭─ title ─╮"},
		{20, "╭─ title ──────────╮"},
	} {
		b := filled(24, 4)
		r := gooey.Rect{X: 1, Y: 1, W: tc.w, H: 3}
		DrawBoxRunes(b, r, render.Style{})
		DrawBoxTitle(b, r, "title", render.Style{})
		if got := rowString(b, r.Y, r.X, r.X+r.W); got != tc.want {
			t.Errorf("w=%d top row = %q, want %q", tc.w, got, tc.want)
		}
		outsideWrites(t, b, r)
	}
}

func TestDrawBoxTitleEmptyTitleWritesNothing(t *testing.T) {
	b := filled(12, 4)
	r := gooey.Rect{X: 1, Y: 1, W: 10, H: 3}
	DrawBoxTitle(b, r, "", render.Style{})
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			if b.At(x, y).Rune != '#' {
				t.Fatalf("empty title painted %q at (%d,%d)", string(b.At(x, y).Rune), x, y)
			}
		}
	}
}

// Border delegates to the helpers and must come out looking the same as
// calling them directly — the refactor's whole claim in one assertion.
func TestBorderMatchesTheSharedBoxHelpers(t *testing.T) {
	r := gooey.Rect{X: 2, Y: 1, W: 14, H: 5}
	st := render.Style{Bold: true}

	got := filled(20, 8)
	bd := &Border{Title: Str("hello"), Style: Sty(st), Child: text("x")}
	bd.Arrange(r)
	bd.Render(&gooey.Frame{Cells: got})

	want := filled(20, 8)
	DrawBoxRunes(want, r, st)
	DrawBoxTitle(want, r, "hello", st)

	for y := 0; y < got.H; y++ {
		for x := 0; x < got.W; x++ {
			if got.At(x, y) != want.At(x, y) {
				t.Fatalf("(%d,%d): Border painted %+v, helpers painted %+v",
					x, y, got.At(x, y), want.At(x, y))
			}
		}
	}
}

// A pill is pillRows tall BY DEFINITION, not "as tall as its bounds".
// The cell tier passes an explicit pillRows to DrawBoxRunes for exactly
// this reason: Measure caps the button at three rows, but a container
// that stretches it hands Render a taller rect, and painting the box on
// that rect would put a cell-tier button and its pixel-tier twin
// (rasterized at pillRows by pillFor) at different heights on the same
// page. Nothing else pinned this, so passing `r` instead of pillRows
// used to be a silent one-character regression.
func TestCellTierPillStaysThreeRowsInATallerBounds(t *testing.T) {
	const tall = 6
	r := gooey.Rect{X: 1, Y: 1, W: 10, H: tall}

	cells := filled(14, 9)
	b := &Button{Content: Str("Save"), Chrome: ChromePixel, Click: gooey.Command(func() {})}
	b.Arrange(r)
	b.Render(&gooey.Frame{Cells: cells}) // no Graphics: the universal tier

	if got := cells.At(r.X, r.Y).Rune; got != boxTopLeft {
		t.Fatalf("top-left corner is %q, want %q", string(got), string(boxTopLeft))
	}
	if got := cells.At(r.X, r.Y+pillRows-1).Rune; got != boxBottomLeft {
		t.Fatalf("the pill's bottom edge is at row %d: found %q, want %q",
			r.Y+pillRows-1, string(got), string(boxBottomLeft))
	}
	// Everything below the pill belongs to whoever else is on the page.
	for y := r.Y + pillRows; y < r.Y+tall; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			if got := cells.At(x, y).Rune; got != '#' {
				t.Fatalf("the pill grew into row %d at column %d (%q) — it must stay %d rows",
					y, x, string(got), pillRows)
			}
		}
	}
}
