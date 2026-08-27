package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The reader pane is the article viewport, and these are the pins issue
// #67 asks for: a long article scrolls with the keyboard and the wheel,
// and scrolling repaints ONLY the pane.
//
// The wheel is driven in-process, through Composer.HandleMouse, because
// mouse reports cannot be injected through a recording pty — the feature
// stays fully keyboard-operable for exactly that reason.

const paneW, paneH = 40, 8

// article is a story long enough that its wrapped body dwarfs the pane —
// several SCREENS of it, not merely more than fits, so that a pagedown
// lands in the middle of the article rather than clamping to the end and
// passing for the wrong reason.
func article() *Story {
	var b strings.Builder
	for i := 0; i < 400; i++ {
		b.WriteString("word ")
	}
	return &Story{
		Title:     "a title",
		Link:      "https://example.test/a",
		Author:    "someone",
		Published: "2026-08-23",
		Body:      b.String(),
	}
}

// pane builds the reader pane inside a stack with a sibling, so that
// "only the reader pane repainted" is a claim with something to exclude.
// The sibling is FIRST because articleBody measures to the whole space it
// is offered; a stack hands the leftovers to whatever comes after.
func pane(t *testing.T, s *Story) (*articleBody, *prop.Property[int], *gooey.Composer) {
	t.Helper()
	body := &articleBody{story: prop.NewSource(s)}
	off := prop.NewSource(0)
	body.scroll.Offset = off
	sibling := &components.Text{Content: prop.NewSource("status")}
	root := &components.VStack{Children: []gooey.Component{sibling, body}}
	c := gooey.NewComposer(root, paneW, paneH+1)
	c.Focus().SetFocus(body)
	c.Frame()
	return body, off, c
}

// row reads one screen row back as a string, trailing blanks trimmed.
// row is the row as a terminal would read it, trailing blanks trimmed.
// The readback itself is render.RowText, which is where the
// continuation markers get skipped: building the string here cell by
// cell rendered them as literal runes, so no fixture in this package
// could hold a wide glyph and be asserted on.
func row(b *render.Buffer, y int) string {
	return strings.TrimRight(render.RowText(b, y), " ")
}

// The headline acceptance: the article does not stop at the pane's
// height. Line 0 of the pane changes as the offset moves, and the text
// that appears is text that was BELOW the fold before.
func TestArticleScrollsWithTheKeyboard(t *testing.T) {
	body, off, c := pane(t, article())
	first := body.lines(paneW)
	if len(first) <= 2*paneH {
		t.Fatalf("the fixture article is %d lines in a %d-row pane; it is too short for a pagedown to land clear of the end", len(first), paneH)
	}

	c.HandleKey(input.Rune('j'))
	if got := off.Get(); got != 1 {
		t.Fatalf("j left the offset at %d, want 1", got)
	}
	f, _ := c.Frame()
	// The pane starts on screen row 1 — the sibling owns row 0.
	if got, want := row(f.Cells, 1), first[1].text; got != want {
		t.Fatalf("after j the pane's first row is %q, want %q (the article's second line)", got, want)
	}

	c.HandleKey(input.Named(input.KeyPageDown))
	if got := off.Get(); got != 1+paneH {
		t.Fatalf("pagedown left the offset at %d, want %d (one screen on from 1)", got, 1+paneH)
	}
}

// THE damage pin. A bounds assertion or a "the cell says X" assertion
// passes just as well when the whole tree repainted, so the only evidence
// that scrolling is cheap is the count: the pane is a leaf, so exactly one
// paint node runs and the sibling and the stack stay clean.
func TestArticleScrollRepaintsOnlyTheReaderPane(t *testing.T) {
	_, _, c := pane(t, article())
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("the composition was not settled before the gesture: %d components repainted", n)
	}
	c.HandleKey(input.Rune('j'))
	if _, n := c.Frame(); n != 1 {
		t.Fatalf("scrolling one line repainted %d components, want exactly 1 (the reader pane)", n)
	}
	c.HandleKey(input.Named(input.KeyPageDown))
	if _, n := c.Frame(); n != 1 {
		t.Fatalf("scrolling one page repainted %d components, want exactly 1 (the reader pane)", n)
	}
}

// The wheel is the other half of the acceptance, driven in-process
// through the Composer so it goes via the real hit-test-and-bubble path
// rather than by calling the pane's handler directly.
func TestArticleScrollsWithTheWheel(t *testing.T) {
	body, off, c := pane(t, article())
	lines := body.lines(paneW)

	c.HandleMouse(input.MouseEvent{Kind: input.WheelDown, X: 1, Y: 2})
	if got := off.Get(); got != 1 {
		t.Fatalf("one wheel notch down left the offset at %d, want 1", got)
	}
	f, n := c.Frame()
	if n != 1 {
		t.Fatalf("a wheel notch repainted %d components, want exactly 1 (the reader pane)", n)
	}
	if got, want := row(f.Cells, 1), lines[1].text; got != want {
		t.Fatalf("after a notch down the pane's first row is %q, want %q", got, want)
	}

	c.HandleMouse(input.MouseEvent{Kind: input.WheelUp, X: 1, Y: 2})
	if got := off.Get(); got != 0 {
		t.Fatalf("wheeling back up left the offset at %d, want 0", got)
	}
	f, n = c.Frame()
	if n != 1 {
		t.Fatalf("wheeling back repainted %d components, want exactly 1 (the reader pane)", n)
	}
	if got, want := row(f.Cells, 1), lines[0].text; got != want {
		t.Fatalf("back at the top the pane's first row is %q, want the article's first line %q", got, want)
	}
}

// end/home reach the ends, and the end is the last line against the
// bottom edge rather than the article scrolled off into blank space.
func TestArticleHomeAndEndReachBothEnds(t *testing.T) {
	body, off, c := pane(t, article())
	n := len(body.lines(paneW))

	c.HandleKey(input.Named(input.KeyEnd))
	if got, want := off.Get(), n-paneH; got != want {
		t.Fatalf("end left the offset at %d, want %d (%d lines - %d rows)", got, want, n, paneH)
	}
	f, _ := c.Frame()
	if got, want := row(f.Cells, paneH), body.lines(paneW)[n-1].text; got != want {
		t.Fatalf("at the end the pane's last row is %q, want the article's last line %q", got, want)
	}

	c.HandleKey(input.Named(input.KeyHome))
	if got := off.Get(); got != 0 {
		t.Fatalf("home left the offset at %d, want 0", got)
	}
}

// Holding j at the bottom must cost nothing. prop.Set does not compare,
// so this is the reader-side pin on the compared Set in Scroller.By.
func TestArticleAtTheBottomIsDamageFree(t *testing.T) {
	_, _, c := pane(t, article())
	c.HandleKey(input.Named(input.KeyEnd))
	c.Frame()
	for i := 0; i < 5; i++ {
		c.HandleKey(input.Rune('j'))
	}
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("holding j at the bottom repainted %d components, want 0", n)
	}
}

// With no story open the pane must not swallow keys: q and esc are page
// bindings reached by bubbling, and a pane that consumed j/k/esc while
// empty would strand the user in a reader showing nothing.
func TestEmptyPaneLeavesKeysToThePage(t *testing.T) {
	body := &articleBody{story: prop.NewSource[*Story](nil)}
	body.scroll.Offset = prop.NewSource(0)
	body.Arrange(gooey.Rect{X: 0, Y: 0, W: paneW, H: paneH})
	if body.HandleKey(input.Rune('j')) {
		t.Fatal("the empty pane consumed j; it would never reach the page")
	}
	if body.HandleMouse(input.MouseEvent{Kind: input.WheelDown}) {
		t.Fatal("the empty pane consumed a wheel notch it could not act on")
	}
}

// These two go together, and neither is worth much alone: the first pins
// that the pane does not subscribe to the offset while it has no article,
// the second that skipping the subscription costs nothing in correctness.
// Pinning only the first would reward a component that had genuinely gone
// deaf.
func TestScrollingAnEmptyReaderIsDamageFree(t *testing.T) {
	_, off, c := pane(t, nil)
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("the composition was not settled before the gesture: %d components repainted", n)
	}
	off.Set(5)
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("scrolling a reader with no article repainted %d components, want 0", n)
	}
}

func TestOffsetSetWhileEmptyStillApplies(t *testing.T) {
	body, off, c := pane(t, nil)
	off.Set(5) // moved while there was nothing to move
	c.Frame()

	body.story.Set(article()) // the story arrives: the pane must honour the offset
	f, _ := c.Frame()
	want := body.lines(paneW)[5].text
	if got := row(f.Cells, 1); got != want {
		t.Fatalf("the pane's first row is %q, want %q — the offset set while empty was lost", got, want)
	}
}

// A narrower pane re-wraps, so the same article is more lines and the end
// is further down. This is the property that makes the article viewport
// un-expressible as an ItemsView: the line count is a function of the
// pane's own width, which no projection can see.
func TestArticleRewrapsWithPaneWidth(t *testing.T) {
	body := &articleBody{story: prop.NewSource(article())}
	body.scroll.Offset = prop.NewSource(0)
	wide, narrow := len(body.lines(60)), len(body.lines(20))
	if narrow <= wide {
		t.Fatalf("wrapping at width 20 gave %d lines and width 60 gave %d; the narrower pane must need more", narrow, wide)
	}
}

// wrap does not measure in BYTES. Byte length made every non-ASCII
// paragraph wrap early — invisible while the pane truncated, plainly
// wrong once the whole article can be scrolled past.
//
// This case uses runes that are multi-byte and ONE column, so it pins
// the byte half alone; the column half is
// TestWrapCountsColumnsNotRunes below. Two tests rather than one because
// a single case cannot distinguish the two failure directions: bytes
// wrap too early, runes too late.
func TestWrapDoesNotCountBytes(t *testing.T) {
	// Six 2-byte runes per word, two words: 13 columns with the space,
	// so both fit a 13-wide column and neither fits by byte count.
	const word = "ααααα" + "α"
	got := wrap(word+" "+word, 13)
	if len(got) != 1 {
		t.Fatalf("wrap split %q into %d lines at width 13, want 1 — it is counting bytes", word+" "+word, len(got))
	}
}

// And it does not measure in RUNES either, which is the opposite error
// and the one that survived the byte fix.
//
// A feed reader renders arbitrary prose off the internet, so this is the
// place in the repo most likely to meet a CJK character or an emoji for
// real. A rune count lets such a line overrun its column by up to its
// own length, and nothing downstream clips it (#357), so it lands on
// whatever is painted beside it.
func TestWrapCountsColumnsNotRunes(t *testing.T) {
	// Four wide glyphs per word: 4 runes and 8 COLUMNS each. Two words
	// plus a space are 17 columns but only 9 runes — so a rune-counting
	// wrap keeps them on one line at width 12 and a column-counting one
	// splits them.
	const word = "世界世界"
	got := wrap(word+" "+word, 12)
	if len(got) != 2 {
		t.Fatalf("wrap put %q on %d line(s) at width 12, want 2 — %q is 8 columns, "+
			"so two of them plus a space need 17 and cannot share a 12-column "+
			"row. Counting runes gives 9 and wrongly fits them",
			word+" "+word, len(got), word)
	}
	// And each produced line must actually fit.
	for i, l := range got {
		if n := render.StringWidth(l); n > 12 {
			t.Errorf("line %d is %d columns wide in a 12-column pane: %q", i, n, l)
		}
	}
}

// The width budget is respected for ordinary prose too — the control, so
// a regression to rune counting cannot hide behind the wide cases and so
// the ASCII path stays covered.
func TestWrapKeepsAsciiWithinItsWidth(t *testing.T) {
	for _, l := range wrap("the quick brown fox jumps over the lazy dog", 12) {
		if n := render.StringWidth(l); n > 12 {
			t.Errorf("line %q is %d columns, over the 12-column budget", l, n)
		}
	}
}
