package main

import (
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"
)

// TestALongPageIsBoundedBeforeItIsMeasured pins the measure limit.
//
// Layout is UNCONDITIONAL — Composer measures and arranges every frame
// whether anything is damaged or not — so a <Text> holding a whole
// markdown file is re-measured for as long as the tab is open, not once
// per selection. That is the half the pane's comment used to miss by
// answering the I/O objection with "on a keystroke rather than per
// frame": true of the read, false of what the string then costs.
//
// The assertion is on what docsBody HANDS OVER, not on what is drawn:
// the pane already shows only what fits, because components are clipped
// to their bounds. This is about the size of the thing measured.
func TestALongPageIsBoundedBeforeItIsMeasured(t *testing.T) {
	long := strings.Repeat("a line of prose\n", docsBodyMaxLines*3)
	ed, _ := docsPage(t, fstest.MapFS{"architecture.md": {Data: []byte(long)}})

	body := ed.docsBody.Get()
	if got := strings.Count(body, "\n"); got > docsBodyMaxLines+4 {
		t.Errorf("the pane hands %d lines to its <Text>; the bound is %d, and "+
			"layout re-measures all of them every frame", got, docsBodyMaxLines)
	}
	// AND IT SAYS SO. A cut with no marker is indistinguishable from a
	// short file, which is the failure docBody's own comment refuses to
	// ship — worse here, because there is nothing to scroll with.
	if !strings.Contains(body, "continues past what the pane measures") {
		t.Error("the body was cut with no marker saying so; a reader at the " +
			"bottom cannot tell a truncated page from a short one")
	}
}

// TestAShortPageIsHandedOverWhole is the other side, and without it the
// test above passes against a clamp that truncates everything.
func TestAShortPageIsHandedOverWhole(t *testing.T) {
	const src = "# Architecture\nline one\nline two\n"
	ed, _ := docsPage(t, fstest.MapFS{"architecture.md": {Data: []byte(src)}})

	if got := ed.docsBody.Get(); got != src {
		t.Errorf("a %d-line page came back as %q, want it untouched",
			strings.Count(src, "\n"), got)
	}
}

// TestClampLinesCutsOnALineBoundary is the unit claim underneath both:
// the cut must land between lines, never mid-line, or the last thing the
// reader sees is half a sentence that looks like the file's own content.
func TestClampLinesCutsOnALineBoundary(t *testing.T) {
	in := "one\ntwo\nthree\nfour\nfive\n"
	got := clampLines(in, 2)
	head, _, ok := strings.Cut(got, "\n\n…")
	if !ok {
		t.Fatalf("clampLines(%q, 2) = %q, which carries no marker", in, got)
	}
	if head != "one\ntwo" {
		t.Errorf("kept %q, want the first two whole lines", head)
	}
}

// TestAPageOfExactlyTheBoundIsNotMarked is the boundary the two tests
// above step over by construction — their arms are docsBodyMaxLines*3
// and 3, so neither can see what happens AT n.
//
// The marker is a claim that content was cut. Claiming it when nothing
// was cut is the inverse of the confusion the marker exists to prevent:
// the reader goes looking for a continuation that does not exist. It is
// latent for the repo's own docs today — the longest page is nowhere
// near 400 lines — so it fires on whichever page reaches exactly the
// bound, which is precisely the day nobody is looking.
//
// BYTE-IDENTICAL, not "contains no marker": the same defect also ate the
// string's trailing newline, and an assertion on the marker alone passes
// against that.
func TestAPageOfExactlyTheBoundIsNotMarked(t *testing.T) {
	for _, n := range []int{1, 2, docsBodyMaxLines} {
		in := strings.Repeat("x\n", n)
		if got := clampLines(in, n); got != in {
			t.Errorf("clampLines(%d lines, n=%d) = %q, want it back unchanged — "+
				"nothing followed the nth newline, so nothing was cut", n, n, got)
		}
	}
}

// TestClampLinesDoesNotUseZeroAsNotFound is the second half of the same
// defect: cut doubled as an index and as a found-flag, so a string whose
// first byte is a newline — a legitimate cut at 0 — was returned whole.
//
// Unreachable at docsBodyMaxLines, and pinned anyway, because the reason
// it is unreachable is the value of one constant.
func TestClampLinesDoesNotUseZeroAsNotFound(t *testing.T) {
	const in = "\nabc\ndef\n"
	got := clampLines(in, 1)
	if got == in {
		t.Fatalf("clampLines(%q, 1) returned it unclamped; a cut at index 0 is "+
			"a real cut, not a not-found sentinel", in)
	}
	if head, _, ok := strings.Cut(got, "\n\n…"); !ok || head != "" {
		t.Errorf("clampLines(%q, 1) = %q, want the empty first line plus the marker", in, got)
	}
}

// TestOneVeryLongLineIsBoundedToo is the case the line cap alone could
// not reach.
//
// docsBodyMaxLines counts NEWLINES; the per-frame cost is StringWidth
// over BYTES. A page written one long line per paragraph — docs/demos.md
// is exactly that shape — passes a 400-line cap untouched at any size,
// and a page with no newlines at all passed it however large it was.
// Found in review of #426.
func TestOneVeryLongLineIsBoundedToo(t *testing.T) {
	// No newlines anywhere, well past the byte bound.
	huge := strings.Repeat("x", docsBodyMaxBytes*2)
	got := clampLines(huge, docsBodyMaxLines)
	if len(got) >= len(huge) {
		t.Errorf("a %d-byte page with no newline came back at %d bytes — the "+
			"line cap counts newlines and found none, so nothing bounded the "+
			"work StringWidth does every frame", len(huge), len(got))
	}
	if !strings.Contains(got, "continues past what the pane measures") {
		t.Error("the clamped page carries no marker; a reader cannot tell it " +
			"from a page that simply ends there")
	}

	// AND A MULTI-BYTE RUNE AT THE BOUNDARY SURVIVES INTACT. Cutting at
	// a byte offset is what makes this bound possible and is also how it
	// could corrupt the last character; ranging over runes is what keeps
	// the cut on a boundary.
	wide := strings.Repeat("世", docsBodyMaxBytes) // 3 bytes each
	cut := clampLines(wide, docsBodyMaxLines)
	if !utf8.ValidString(cut) {
		t.Error("clamping a page of wide runes produced invalid UTF-8 — the " +
			"cut landed inside a character")
	}

	// A page under both bounds is returned untouched, or the assertions
	// above pass against a function that always clamps.
	small := "one\ntwo\nthree\n"
	if got := clampLines(small, docsBodyMaxLines); got != small {
		t.Errorf("a short page was clamped to %q; both bounds are far above it", got)
	}
}
