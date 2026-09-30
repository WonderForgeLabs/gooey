package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/render"
)

// EVERY WIDTH ON THESE ROWS IS A COLUMN COUNT, and this file is where
// that is checked for the helpers PACKAGE MAIN sizes its own chrome
// with — the ones that take a string and a cell budget and answer with
// at most that many columns. components/preview holds one more of the
// same shape, overlay.go's fit, beside the module's only per-rune write
// loop; the sentence here said "this app" while reasoning about this
// package, so the sweep stopped at a boundary its own framing hid. That
// one is swept too and pinned in that package's own overlay_test.go,
// which carries the reachability this fixture could not. Raised in
// review of #524.
//
// NO COUNT OF THEM HERE, and this paragraph carried one — "four" —
// until review of #524 measured it wrong at the commit that wrote it:
// browser.go's elide is of exactly that shape, is pinned by this file
// below, and was ADDED by the same commit. shortPath is another. A
// number in prose is a sample taken once, which is the rule CLAUDE.md's
// Verify section gives and the one this branch has now retracted at
// this altitude three times. The inventory is a grep, not a sentence:
//
//	grep -n 'func .*\(s\|p\) string, w int) string' apps/wysiwyg/*.go
//
// They did not agree, which is what the sweep was. dock.go's clipTo
// delegates to render.ClipCols and always has since #441; statusaddr.go's
// ellipsize and padTo, and properties.go's pad, each counted runes. A
// reader of any one file could not see the disagreement, and no fixture
// could: every string in this package's suite was ASCII, where a rune
// count and a column count are the same number and a wrong rule passes.
//
// ellipsize's own pin is NOT here: statusaddr_test.go already had a
// TestEllipsizeNeverExceedsItsWidth, and it measured runes, so for the
// life of both the guard and the function it could not see the defect it
// was written to catch. Strengthening that one in place is what says the
// rule changed; a second test beside it would have left the rune
// assertion standing as the contract.
//
// The fixtures below use CJK deliberately. A wide glyph is one rune and
// two columns, so a rune count asks for a slot NARROWER than its own
// text — and the two answers differ by exactly the number of wide glyphs,
// which is what makes each failure message able to name the rule that
// produced it.
const (
	// wideCell is one rune and two columns.
	wideCell = "世"
	// wideWord is two runes and four columns.
	wideWord = "世界"
)

// TestPadToFillsTheSlotInColumns pins the reserved-slot rule the notice
// depends on. padTo is what blanks the tail of a longer message, and the
// slot it is blanking is measured in cells.
func TestPadToFillsTheSlotInColumns(t *testing.T) {
	for _, tc := range []struct {
		in string
		w  int
	}{
		{wideWord, 6},
		{wideWord, 4},
		{"ab" + wideCell, 8},
	} {
		got := padTo(tc.in, tc.w)
		if n := render.StringWidth(got); n != tc.w {
			t.Errorf("padTo(%q, %d) = %q, %d columns: the slot is %d and a shorter message "+
				"written into it must fill it exactly, or the tail of a longer one survives. "+
				"%q is %d runes and %d columns.",
				tc.in, tc.w, got, n, tc.w, tc.in, len([]rune(tc.in)), render.StringWidth(tc.in))
		}
	}
}

// TestThePropertyPadAnswersInColumns is the same rule for properties.go's
// pad, which is the one a DOCUMENT's own text reaches: drawStepper paints
// `◂ value ▸` through it, and the value is whatever the attribute holds.
func TestThePropertyPadAnswersInColumns(t *testing.T) {
	for _, tc := range []struct {
		in string
		w  int
	}{
		{wideWord, 6},
		{"◂ " + wideWord + " ▸", 8},
		{strings.Repeat(wideCell, 5), 4},
		// AN ODD BUDGET AGAINST WIDE GLYPHS, which is the only shape
		// that reaches pad's documented reason for filling AFTER the
		// clip rather than instead of it: ClipCols stops BEFORE a glyph
		// that would straddle the edge, so five two-column glyphs in
		// five columns come back as four and the fill owes one.
		// Measured: a pad that returns ClipCols(s, w) whenever the
		// input is wide enough was SILENT over the three rows above —
		// the two short inputs never reach the clip, and the even
		// budget clips to exactly 4. The consequence is the one the doc
		// gives: these surfaces float over the page, so a row one
		// column short leaves the page's own cell showing through, and
		// it belongs to a clean node that will not repaint. Raised in
		// review of #524.
		{strings.Repeat(wideCell, 5), 5},
	} {
		got := pad(tc.in, tc.w)
		if n := render.StringWidth(got); n != tc.w {
			t.Errorf("pad(%q, %d) = %q, %d columns, want exactly %d. %q is %d runes and "+
				"%d columns.", tc.in, tc.w, got, n, tc.w, tc.in,
				len([]rune(tc.in)), render.StringWidth(tc.in))
		}
	}
}

// TestAChipMeasuresItsAddressInColumns. Measure answers chipWidth, and
// Measure is layout: an undersized answer is a chip whose own text does
// not fit the rect it asked for.
func TestAChipMeasuresItsAddressInColumns(t *testing.T) {
	c := &addrChip{label: wideWord, addr: "127.0.0.1:1"}
	// THE NUMBER IS WRITTEN OUT, for the reason
	// TestAStyleListIsSizedInColumns states below: a want computed with
	// render.StringWidth is chipWidth's body character for character,
	// so it is the rule under test restated. It does catch the rune
	// mutation — the fixture is wide enough that the two rules differ —
	// but it is the weaker of two available pins standing where this
	// file's own argument asks for the stronger. Raised in review of
	// #524.
	//
	// 18: 世界 is 4 columns, a space, 127.0.0.1:1 is 11, and the dot and
	// its space are two more. A rune count answers 16.
	const want, byRunes = 18, 16
	if got := c.chipWidth(); got != want {
		t.Errorf("a chip labelled %q measures %d cells, want %d: its text %q is %d runes "+
			"and %d columns, and the dot and its space are two more. %d is what a "+
			"rune count answers",
			wideWord, got, want, c.idleText(),
			len([]rune(c.idleText())), render.StringWidth(c.idleText()), byRunes)
	}
}

// TestShortPathFitsTheColumnsItWasGiven. The workspace list is the one
// place in this app where a name comes off the FILE SYSTEM, so the
// characters in it are not the app's to choose.
func TestShortPathFitsTheColumnsItWasGiven(t *testing.T) {
	p := "apps/" + strings.Repeat(wideWord, 3) + "/" + strings.Repeat(wideWord, 3) + ".gooey"
	// THE FIRST THREE WIDTHS are each at least the path's RUNE count (24,
	// against 36 columns), so a rune count answers "it fits" for them and
	// the loop discriminates rather than agreeing with the rule it is
	// meant to reject. The four below them are the bound's own end, and
	// the paragraph after this one is their argument — not this one's:
	// the widths were three when it was written, and it went on saying
	// "all three" over a list of seven. Corrected in review of #524.
	//
	// DOWN TO ONE COLUMN, and the narrow end is a separate defect rather
	// than more of the same: elide walked to the first cluster starting
	// at or past its cut column, and when the trailing cluster was wider
	// than w-1 there was no such cluster, so the walk ran off the end and
	// kept the last one anyway. shortPath("a/世世", 2) answered "…世" —
	// three columns for a two-column budget. The explorer arranges it at
	// the pane's live width, which a narrowed pane makes small (#528),
	// and the loop goes this far regardless: this is the one function in
	// the package whose entire job is the bound. Found in review of #524.
	for _, w := range []int{30, 26, 24, 4, 3, 2, 1} {
		got := shortPath(p, w)
		if n := render.StringWidth(got); n > w {
			t.Errorf("shortPath(%q, %d) = %q, %d columns wide. The path is %d runes and "+
				"%d columns, so a rune count reports it as fitting a slot it overruns.",
				p, w, got, n, len([]rune(p)), render.StringWidth(p))
		}
	}
	// And the same at the boundary the walk actually falls off, which the
	// path above is too long to reach: a single wide glyph in two columns
	// needs no elision at all, and got one that overran anyway.
	for _, tc := range []struct {
		s string
		w int
	}{{wideCell, 2}, {wideWord, 2}, {"ab" + wideCell, 2}, {wideWord, 3}} {
		got := elide(tc.s, tc.w)
		if n := render.StringWidth(got); n > tc.w {
			t.Errorf("elide(%q, %d) = %q, %d columns wide", tc.s, tc.w, got, n)
		}
	}
}

// TestAPathRowFitsTheWidthItWasArranged goes through the SHIPPED
// component rather than the helper: <PathText> is what the explorer's
// item template paints, and it shortens against its ARRANGED width. A
// file system supplies these names, so unlike every other caller in
// this file this one cannot choose its own characters.
func TestAPathRowFitsTheWidthItWasArranged(t *testing.T) {
	const w = 30
	p := "apps/" + strings.Repeat(wideWord, 3) + "/" + strings.Repeat(wideWord, 3) + ".gooey"
	if len([]rune(p)) > w {
		t.Fatalf("the fixture is %d runes against %d cells, so a rune count would "+
			"shorten it too and this measures nothing", len([]rune(p)), w)
	}
	if render.StringWidth(p) <= w {
		t.Fatalf("the fixture is %d columns against %d cells: it fits, so there is "+
			"no overrun to find", render.StringWidth(p), w)
	}
	got := paintPath(t, p, w)
	if !strings.HasSuffix(strings.TrimRight(got, " "), ".gooey") {
		t.Errorf("the row for %q painted %q at %d cells: the TAIL — the part shortPath "+
			"exists to keep — is what went", p, got, w)
	}
}

// paintPath paints <PathText> for p arranged w cells wide and reads the
// row back.
func paintPath(t *testing.T, p string, w int) string {
	t.Helper()
	c := gooey.NewComposer(&pathText{path: components.Str(p)}, w, 1)
	f, _ := c.Frame()
	return render.RowText(f.Cells, 0)
}

// TestTheContextMenuIsSizedForItsWidestItem. menuRect answers the popup's
// rect, so an undersized answer is a dropdown narrower than the row it
// has to show. The strip's own items are app strings today — this drives
// it with a wide one because the helper's rule, not today's data, is
// what the next item added has to obey.
func TestTheContextMenuIsSizedForItsWidestItem(t *testing.T) {
	s := bareStrip(testGrpc)
	s.items = []components.MenuItem{{Text: "copy"}, {Text: strings.Repeat(wideWord, 4)}}
	// WRITTEN OUT rather than computed with the rule under test, for the
	// reason TestAStyleListIsSizedInColumns states below. 20: the widest
	// item is 世界 four times — 8 runes, 16 columns — plus 4 of box and
	// padding. A rune count answers 12. Raised in review of #524.
	const want, byRunes = 20, 12
	if got := s.menuRect().W; got < want {
		t.Errorf("the menu is %d columns wide for an item of 16 columns (%d runes): the "+
			"box, its padding and the item need %d, and %d is what a rune count "+
			"answers", got, len([]rune(s.items[1].Text)), want, byRunes)
	}
}

// TestShortPathStillKeepsTheTail is the other half. Bounding the width
// alone is satisfied by returning "", and the whole argument of shortPath
// is that the DISTINGUISHING part of a path is its last segment.
func TestShortPathStillKeepsTheTail(t *testing.T) {
	p := "apps/" + wideWord + "/leaf.gooey"
	got := shortPath(p, 14)
	if !strings.HasSuffix(got, "leaf.gooey") {
		t.Errorf("shortPath(%q, 14) = %q and dropped the last segment: a list of paths "+
			"that lost its tails renders every candidate the same", p, got)
	}

	// The boundary the width bound alone cannot see: the LAST segment is
	// wider than the whole budget, so there is no arrangement of whole
	// segments that fits and something inside the name has to go. Taking
	// it off the front is the only answer consistent with the rest of
	// this function — clipping the name from the right keeps the part
	// every candidate shares, which is the answer shortPath exists to
	// reject.
	long := "a/" + strings.Repeat(wideWord, 5) + ".gooey"
	got = shortPath(long, 10)
	if n := render.StringWidth(got); n > 10 {
		t.Errorf("shortPath(%q, 10) = %q, %d columns", long, got, n)
	}
	if !strings.HasSuffix(got, ".gooey") {
		t.Errorf("shortPath(%q, 10) = %q: the last segment alone is %d columns, so it had "+
			"to be cut — and cutting it from the RIGHT keeps the leading characters every "+
			"sibling shares and drops the ones that tell them apart",
			long, got, render.StringWidth(strings.Repeat(wideWord, 5)+".gooey"))
	}
}

// TestAWideNoticeIsCutWithAnEllipsisNotHardTruncated is the outcome pin,
// and it is at the frame rather than at the helper because the helper's
// wrong answer is not what a user sees. The notice reserves
// copyNoticeWidth cells and the composer clips every Render to its
// bounds, so an overrun does not reach the chip beside it — it is
// swallowed INSIDE the notice, and what the row then shows is a message
// cut without the ellipsis that says it was cut. That is the one
// behaviour ellipsize exists to prevent, and the comment above it says
// so.
func TestAWideNoticeIsCutWithAnEllipsisNotHardTruncated(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	// Thirteen runes, twenty-six columns, against a slot of
	// copyNoticeWidth. A rune count calls this a fit.
	msg := strings.Repeat(wideCell, 13)
	if len([]rune(msg)) > copyNoticeWidth {
		t.Fatalf("the fixture is %d runes against a %d-cell slot, so a rune count would "+
			"truncate it too and the test proves nothing", len([]rune(msg)), copyNoticeWidth)
	}
	if render.StringWidth(msg) <= copyNoticeWidth {
		t.Fatalf("the fixture is %d columns against a %d-cell slot: it fits, so nothing "+
			"is cut", render.StringWidth(msg), copyNoticeWidth)
	}
	ed.addrs.notice.outcome.Set(copyDone)
	ed.addrs.notice.text.Set(msg)

	f, _ := c.Frame()
	y := ed.addrs.notice.Bounds().Y
	row := render.RowText(f.Cells, y)
	// The notice is not the last thing on this row — the chips follow it
	// — so the assertion is the JUNCTION rather than a suffix: the last
	// glyph the notice kept, immediately followed by the ellipsis. Built
	// from the fixture and the contract, never from ellipsize's own
	// answer, which would pass against any cut the function happened to
	// make.
	if cut := wideCell + "…"; !strings.Contains(row, cut) {
		t.Errorf("the status row reads %q and holds no %q. The notice was given %d columns "+
			"of message for a %d-cell slot, so it was cut — and a cut this strip makes must "+
			"end in the ellipsis, which is the whole difference between ellipsize and "+
			"dock.go's clipTo.", row, cut, render.StringWidth(msg), copyNoticeWidth)
	}
	if strings.Contains(row, msg) {
		t.Errorf("the status row reads %q and carries the whole %d-column message in a "+
			"%d-cell slot", row, render.StringWidth(msg), copyNoticeWidth)
	}
}

// TestAStyleListIsSizedInColumns is the dropdown's own width, and it
// closes the one row of #523's mutation matrix that was recorded as
// SILENT rather than pinned.
//
// The option list is NOT always Go source. KindEnum and KindBool come
// from an element spec's declared values, but a KindStyle row's options
// are the keys of the running app's Context.Styles — a live map, which
// is the whole point of valueSet reading it rather than a table. So a
// style whose name holds a wide glyph is reachable, and it is what the
// surface has to be wide enough for.
//
// THE NUMBERS ARE WRITTEN OUT, both of them, because a want computed
// with render.StringWidth would be the rule under test restated and
// would pass against a rune count too.
func TestAStyleListIsSizedInColumns(t *testing.T) {
	const wide = "世界世界世界" // six runes, TWELVE columns
	ed, c, p := propsPane(t)
	ed.docCtx.Styles[wide] = render.Style{Dim: true}
	ed.sel = ed.doc().Kids[1] // the Button, which has a Style row
	ed.rebuild()
	c.Frame()
	rowAt(t, ed, c, "Style")
	ed.beginEdit()
	c.Frame()

	if got := p.Mode(); got != editChoice {
		t.Fatalf("the Style row opened editor %v, want editChoice — this test "+
			"measures the dropdown and there is no dropdown", got)
	}
	// The fixture has to be able to tell the two rules apart: the widest
	// option in COLUMNS must not also be the widest in RUNES.
	byRunes := 0
	for _, o := range p.options() {
		byRunes = max(byRunes, len([]rune(optionLabel(o))))
	}
	if byRunes != 7 { // "(unset)", wider than the six runes of `wide`
		t.Fatalf("the widest option is %d runes, want 7: the fixture no longer "+
			"discriminates a column count from a rune count", byRunes)
	}

	if got := p.surfaceSize().W; got != 16 {
		t.Errorf("the style dropdown reserved %d columns, want 16 — twelve for "+
			"%q plus the four of chrome. A rune count answers 11 and clips the "+
			"widest name it is there to show.", got, wide)
	}
}

// TestAShortenedPathSaysWhatItDropped is about FORMAT, not bounds — both
// shapes below fit the budget, and that is why nothing caught this.
//
// shortPath budgeted ONE column for the ellipsis while elide renders
// "…/", two. Neither overruns, but the mismatch made the format
// alternate with the width: "aa/bbbb/cc" in nine columns gave
// "…/bbbb/cc" and in eight gave "…bbbb/cc", and the second tells the
// reader a character was cut out of "bbbb" when what actually went was
// the whole leading "aa/". Reserving two costs a column of packing at
// the boundary ("…/cc" where "…bbbb/cc" would have fitted) and buys a
// row that never says the wrong thing about what it dropped.
//
// THE GUARANTEE HAS A FLOOR, and the loop below used to stop one width
// above it. It holds while "…/" plus the last segment fits in w — four
// columns for this fixture — and at 3 there is no answer that both fits
// and keeps the separator: shortPath("aa/bbbb/cc", 3) = "…cc". That is
// not a bounds bug, it is where the format runs out, and a test that
// walks 9, 8, 7 and stops leaves the reader to guess whether the
// guarantee is unconditional. It is not. Raised in review of #524.
func TestAShortenedPathSaysWhatItDropped(t *testing.T) {
	const p = "aa/bbbb/cc" // ten columns; the last segment fits every width below
	// Down to 4: "…/cc" is four columns, so 4 is the last width at which
	// the separator can be kept at all. 8 through 4 are the SAME call —
	// elide("cc", w) taking its first arm — so they are the range the
	// format is claimed over rather than five separate arms; 9 is the
	// only width here that keeps "bbbb", and 3, below, is the only step
	// that changes the answer. Measured: with elide's first arm widened
	// to w-1, the loop stays green at every width in it and only the
	// boundary below reddens.
	for _, w := range []int{9, 8, 7, 6, 5, 4} {
		got := shortPath(p, w)
		if n := render.StringWidth(got); n > w {
			t.Fatalf("shortPath(%q, %d) = %q, %d columns — a bounds failure, which "+
				"is a different defect from the one this test is for", p, w, got, n)
		}
		if !strings.HasPrefix(got, "…/") {
			t.Errorf("shortPath(%q, %d) = %q. The whole path does not fit and the "+
				"last segment does, so what was dropped is leading SEGMENTS — which "+
				"the separator in \"…/\" is what says. Without it the row reads as a "+
				"cut inside the segment that survived.", p, w, got)
		}
	}
	// THE OTHER SIDE OF THE BOUNDARY, so the loop above is a claim about
	// a range rather than about wherever it happened to stop. One column
	// below "…/cc" the separator cannot be paid for, and asserting that
	// is what makes 4 the floor instead of the smallest number anyone
	// bothered to try.
	if got := shortPath(p, 3); got != "…cc" {
		t.Errorf("shortPath(%q, 3) = %q, want %q: three columns cannot hold "+
			"\"…/\" and a two-column segment both, so the format above is bounded "+
			"and this is where it ends", p, got, "…cc")
	}
}
