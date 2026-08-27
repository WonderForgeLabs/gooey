package render

import (
	"strings"
	"testing"
)

// The findings a review of #358's first two commits confirmed by running
// code. Each is pinned here by the shape that broke, not by the fix.

// F1/F6. A cell reserves columns by its cluster's width, so it must DRAW
// that cluster — the two numbers have to come from the same thing.
//
// Storing only the first rune made them disagree in both directions at
// once: "⚠️" is U+26A0 U+FE0F, two columns as a cluster and one as a lead
// rune, so the row reserved two and drew one; and every rune past the
// first was silently dropped, so decomposed "é" painted as "e".
// The second is the one worth dwelling on, because the comment that
// shipped with it called it "a narrowing, not a regression — the old loop
// gave each mark its own cell and its own column". That was wrong: the
// old flusher emitted both runes and the TERMINAL composed them, so the
// accent rendered. The change lost it.
func TestACellDrawsTheClusterItReservedColumnsFor(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string
		why  string
	}{
		{"⚠️x", "⚠️x", "VS16 carries the width on the cluster's SECOND rune"},
		{"☹️x", "☹️x", "the same shape with a different base"},
		{"🏳️‍🌈x", "🏳️‍🌈x", "a ZWJ sequence is four runes and one glyph"},
		{"1️⃣x", "1️⃣x", "a keycap's U+FE0F U+20E3 tail is part of the glyph"},
		{"éx", "éx", "a combining mark must survive to the terminal, " +
			"which is what composes it"},
		{"世界", "世界", "the ordinary wide case"},
		{"abc", "abc", "the ASCII control"},
	} {
		b := NewBuffer(8, 1)
		b.SetString(0, 0, c.in, Style{})

		if got := strings.TrimRight(RowText(b, 0), " "); got != c.want {
			t.Errorf("SetString(%q) reads back as %q, want %q — %s",
				c.in, got, c.want, c.why)
		}
		// And it must reach the wire, which is a separate claim: the
		// buffer could hold the cluster and the flusher still emit only
		// the lead rune.
		out := string(NewFlusher().Encode(nil, b, TrueColor))
		if !strings.Contains(out, c.want) {
			t.Errorf("the flush of %q does not contain %q: %q", c.in, c.want, out)
		}
		if x, by, ok := Displaced(b, 0); ok {
			t.Errorf("%q displaces cell %d by %d columns — %s", c.in, x, by, c.why)
		}
	}
}

// F2. TerminalWidth's last-cell arithmetic floored RuneWidth, and
// RuneWidth(Continuation) is 1 because string(rune(-1)) is U+FFFD. So a
// row ending in a wide glyph measured one column over — invisible to any
// fixture ending in ASCII.
func TestTerminalWidthCountsATrailingContinuationAsNoColumns(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want int
	}{
		{"世", 2, 2},
		{"ab世", 4, 4},
		{"世界", 4, 4},
		{"abcd", 4, 4},
	} {
		b := NewBuffer(c.w, 1)
		b.SetString(0, 0, c.in, Style{})
		if got := TerminalWidth(b, 0); got != c.want {
			t.Errorf("TerminalWidth of %q in a %d-cell row = %d, want %d — the "+
				"trailing continuation draws nothing and advances nothing",
				c.in, c.w, got, c.want)
		}
	}
}

// F4. The framework's paint model is overpainting (flush.go:25), so a
// single-cell write landing on half a wide glyph is ORDINARY, not exotic.
// Both halves of the break are silent and neither self-corrects:
//
//   - over the lead, the orphaned Continuation is a column the flusher
//     skips forever, so nothing can ever repaint it;
//   - over the continuation, the surviving lead still draws two columns
//     and displaces the rest of the row.
func TestOverpaintingHalfAWideGlyphRepairsTheOther(t *testing.T) {
	for _, c := range []struct {
		at   int
		want string
		why  string
	}{
		// The repaired cell becomes a SPACE, not nothing: the column
		// still exists and still belongs to that cell. So the row is
		// three cells wide either way, and b does not move — repairing a
		// glyph must not re-flow the text after it.
		{0, "x b", "writing over the LEAD must clear its orphaned continuation"},
		{1, " yb", "writing over the CONTINUATION must clear its orphaned lead"},
	} {
		b := NewBuffer(4, 1)
		b.SetString(0, 0, "世b", Style{})
		b.Set(c.at, 0, []rune("xy")[c.at], Style{})

		if x, by, ok := Displaced(b, 0); ok {
			t.Errorf("Set(%d) leaves cell %d displaced by %d — %s", c.at, x, by, c.why)
		}
		if got := strings.TrimRight(RowText(b, 0), " "); got != strings.TrimRight(c.want, " ") {
			t.Errorf("Set(%d) gives row %q, want %q — %s",
				c.at, got, strings.TrimRight(c.want, " "), c.why)
		}
	}
}

// SetString repairs the same seams, at the two edges of the run it wrote.
func TestSetStringRepairsTheGlyphsItLandsOn(t *testing.T) {
	// Landing on the right half of a wide glyph.
	b := NewBuffer(6, 1)
	b.SetString(0, 0, "世界", Style{})
	b.SetString(1, 0, "ab", Style{})
	if x, by, ok := Displaced(b, 0); ok {
		t.Errorf("writing over a continuation leaves cell %d displaced by %d", x, by)
	}
	// And landing so the run ENDS inside one.
	b = NewBuffer(6, 1)
	b.SetString(0, 0, "世界", Style{})
	b.SetString(0, 0, "a", Style{})
	if x, by, ok := Displaced(b, 0); ok {
		t.Errorf("writing over a lead leaves cell %d displaced by %d", x, by)
	}
}

// F5. Set bounds-checks and SetString did not, so the lead of a glyph
// straddling column 0 was swallowed while its Continuation still landed
// IN cell 0 — an orphan marking a column the flusher skips forever.
func TestSetStringClipsOnTheLeft(t *testing.T) {
	b := NewBuffer(4, 1)
	b.SetString(-1, 0, "世b", Style{})
	if got := b.At(0, 0).Rune; got == Continuation {
		t.Error("cell 0 holds an orphan continuation: a glyph straddling the " +
			"left edge wrote its tail without its head")
	}
	if x, by, ok := Displaced(b, 0); ok {
		t.Errorf("cell %d displaced by %d after a left-clipped write", x, by)
	}
	// The clip must not shift what follows: 世 would have covered columns
	// -1 and 0, so b belongs in column 1.
	if got := b.At(1, 0).Rune; got != 'b' {
		t.Errorf("b landed at %q, not column 1 — clipping changed the columns "+
			"of what survived", string(got))
	}
}

// F3. A damage span may not START on a continuation cell.
//
// diff emitted cup(x, y) and then ran from x; run skips continuations
// without emitting anything, so the cursor stayed at x and the NEXT cell
// landed there — every cell of the run one column early, on top of the
// wide glyph whose tail was skipped.
//
// This needs the Damage path specifically, which is why the existing
// TestARunAfterAWideGlyphIsAddressedAtItsRealColumn did not catch it: a
// cell-level diff never starts a span on an unchanged continuation, but
// Flusher.Damage (flush.go:56, the sixel/iTerm2 removal path) marks
// arbitrary rectangles, so the pixel plane produces exactly this span in
// ordinary operation.
func TestADamageSpanStartingOnAContinuationIsWidenedToItsLead(t *testing.T) {
	b := NewBuffer(8, 1)
	b.SetString(0, 0, "世abcd", Style{})
	f := NewFlusher()
	f.Encode(nil, b, TrueColor) // settle

	f.Damage(Rect{X: 1, Y: 0, W: 3, H: 1}) // starts ON 世's second column
	out := string(f.Encode(nil, b, TrueColor))

	// The cursor must address column 0, the LEAD of the glyph whose
	// continuation the span starts on, and the glyph must be repainted —
	// it is the only way the cells after it land in their own columns.
	if !strings.Contains(out, string(cup(0, 0))) {
		t.Errorf("damage span emitted %q; it must address column 0, the lead of "+
			"the glyph whose continuation the span starts on", out)
	}
	if !strings.Contains(out, "世ab") {
		t.Errorf("damage span emitted %q, want it to repaint 世 then ab — "+
			"skipping the continuation without moving the cursor writes a at "+
			"column 1, over the glyph's right half, and shifts the rest left", out)
	}
}
