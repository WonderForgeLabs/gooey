package main

import (
	"strings"
	"testing"
	"testing/fstest"
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
