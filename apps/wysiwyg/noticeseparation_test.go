package main

import (
	"fmt"
	"strings"
	"testing"
)

// The correction, asserted: the copy is not a service state.
//
// These live in their own file because they are the pins on the ruling
// rather than on the component — they are what a future edit trips over
// when it reunites the two facts, and they should be findable by name.
// statusaddr_test.go covers the strip; this covers the separation.

// TestTheNoticeCarriesNoServiceDot asserts BOTH halves of the
// separation: not the same glyph, and not the same place.
//
// A coloured dot beside a service name is read as a connection light no
// matter what it was wired to. So the copy feedback may not wear one,
// and it may not sit where one sits — a green cue appearing inside the
// run of cells the eye takes for the endpoint names is the same mistake
// with the dot merely moved.
//
// It drives a real copy through the shipped page rather than setting the
// property, so it fails for a wiring change as well as for a rendering
// one.
func TestTheNoticeCarriesNoServiceDot(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	okCopy(t)
	ed.addrs.CopyCurrent()
	f, _ := c.Frame()

	row := screenRow(f, 47)
	if !strings.Contains(row, "copied") {
		t.Fatalf("the status row is %q and carries no copy confirmation, so this test "+
			"asserts nothing", strings.TrimSpace(row))
	}

	// ONE dot per endpoint on the whole row, while a copy is showing.
	if got, want := strings.Count(row, string(addrDot)), len(ed.addrs.chips); got != want {
		t.Errorf("the row carries %d state dots while a copy is showing, want %d — one "+
			"per endpoint.\nA dot that appears with a copy is a connection light "+
			"reporting the clipboard, which is the arrangement this change exists to "+
			"undo.\nrow: %q", got, want, strings.TrimSpace(row))
	}

	// And none of them is in the notice's cells.
	nb := ed.addrs.notice.Bounds()
	for x := nb.X; x < nb.X+nb.W; x++ {
		if got := f.Cells.At(x, nb.Y).Rune; got == addrDot {
			t.Errorf("cell %d of the notice holds the service dot %q: the clipboard and "+
				"the service state must not share a glyph", x, got)
		}
	}

	// The notice does not overlap either chip, so its colour cannot be
	// read as belonging to a service name.
	for i, ch := range ed.addrs.chips {
		b := ch.Bounds()
		if nb.X < b.X+b.W && b.X < nb.X+nb.W {
			t.Errorf("the notice %+v overlaps chip %d %+v: the copy outcome must not "+
				"share a position with a service name", nb, i, b)
		}
	}
}

// TestNoChipStateWearsTheCautionColour is the forward guard on the
// collision the original author of these assertions flagged before it
// could happen.
//
// Amber is the obvious colour for "serving, nobody attached", and
// copyCaveat already owns amber for "the escape was written but tmux
// may have swallowed it". Those are different meanings, three cells
// apart, on one row — the same one-cue-two-facts collapse this file
// exists to undo, moved up a layer. linkIdle is grey for that reason.
//
// It is deliberately narrow. Red and green ARE shared between the two
// vocabularies and that is correct: on this row a colour means the same
// thing in both places (red broken, green fine, amber check this), and
// the subject is separated by position, glyph and words. A test that
// forbade all palette overlap would be forbidding the consistency that
// makes the row readable, so this forbids exactly one thing — a chip
// wearing the colour that means "go and check something".
func TestNoChipStateWearsTheCautionColour(t *testing.T) {
	caution := copyCaveat.textStyle()
	for name, s := range map[string]linkState{
		"down": linkDown, "idle": linkIdle, "active": linkActive, "serving": linkServing,
	} {
		if s.dotStyle() == caution {
			t.Errorf("the %s chip state wears %+v, which is the clipboard's caution "+
				"colour.\nOne colour means one thing on this row: amber says \"this may "+
				"not have worked, check it\". A serving endpoint with no client attached "+
				"is not a caution — nothing is wrong and there is nothing to check — so "+
				"it is grey. See dotStyle.", name, caution)
		}
	}
	// Discrimination: the caution colour has to be a real, distinct
	// colour, or the check above passes by comparing against nothing.
	if !caution.Fg.Set {
		t.Fatal("the caution colour is unset, so this test cannot fail")
	}
	if caution == copyDone.textStyle() || caution == copyFailed.textStyle() {
		t.Error("the caution colour is not distinct from success or failure, so the " +
			"notice's own three states are not distinguishable either")
	}
}

// TestTheNoticeSaysWhatItIsAbout is the other half of "not mistakable".
// Position and glyph separate it visually; the words are what make it
// unambiguous when read. Every message the notice can carry names the
// clipboard rather than the endpoint's health.
func TestTheNoticeSaysWhatItIsAbout(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(t *testing.T, ed *editor)
	}{
		{"success", func(t *testing.T, ed *editor) { okCopy(t) }},
		{"failure", func(t *testing.T, ed *editor) { swapClipboard(t, fmt.Errorf("no terminal")) }},
		{"caveat", func(t *testing.T, ed *editor) {
			okCopy(t)
			ed.addrs.caveatFn = func() string { return "inside tmux" }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ed, _ := addrPage(t, testGrpc, testMCP)
			tc.set(t, ed)
			ed.addrs.CopyCurrent()

			got := noticeText(ed.addrs)
			// copied / copy failed / copy unverified.
			if !strings.Contains(got, "cop") {
				t.Errorf("the notice reads %q and never says the word copy: a coloured "+
					"phrase in the status bar that does not name the clipboard is a "+
					"phrase about whatever is nearest it", got)
			}
			if n := len([]rune(got)); n > copyNoticeWidth {
				t.Errorf("the %s message is %d runes and will be clipped into %d: %q\n"+
					"The reserved width has to hold the messages this app actually "+
					"produces, or the ellipsis lands exactly where the information is.",
					tc.name, n, copyNoticeWidth, got)
			}
		})
	}
}

// TestTheRealFailureReasonSurvivesTheReservedWidth is the specific case
// that moved copyNoticeWidth from 20 to 24, kept so it cannot move back
// by accident.
//
// It substitutes nothing: ed.copyToSystem is the shipped seam and `go
// test` has no terminal, so this is the exact string a user sees when a
// copy fails for the most ordinary reason there is. Clipping it costs
// the whole message — "copy failed" is already said by the colour, and
// the reason is the only new information in the row.
func TestTheRealFailureReasonSurvivesTheReservedWidth(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	ed.addrs.CopyCurrent() // no clipboard seam substituted: it genuinely fails

	msg := noticeText(ed.addrs)
	if !strings.HasPrefix(msg, "copy failed:") {
		t.Fatalf("the notice reads %q, want the real failure: this test is measuring "+
			"the wrong string", msg)
	}
	f, _ := c.Frame()
	nb := ed.addrs.notice.Bounds()
	var painted strings.Builder
	for x := nb.X; x < nb.X+nb.W; x++ {
		painted.WriteRune(f.Cells.At(x, nb.Y).Rune)
	}
	if got := strings.TrimRight(painted.String(), " "); got != msg {
		t.Errorf("the notice painted %q for the message %q — the reserved %d cells do "+
			"not hold the failure this app actually produces, so the ellipsis lands "+
			"where the reason starts", got, msg, copyNoticeWidth)
	}
}

// TestTheMenuNamesTheEndpointState is the route by which the numbers
// that must not be colours reach the user. The popup is sized to its
// content, so a sentence there costs no status-bar width.
func TestTheMenuNamesTheEndpointState(t *testing.T) {
	ed, c := addrPage(t, testGrpc, testMCP)
	srv := &stubServer{serving: true, sessions: 2}
	ed.link("grpc").detail = func() string { return sessionDetail(srv) }

	openMenuKey(t, ed, c)

	var found string
	for _, it := range ed.addrs.items {
		if strings.HasPrefix(it.Text, "grpc:") {
			found = it.Text
		}
	}
	if found == "" {
		t.Fatalf("the menu carries no row naming the endpoint's state: %v", ed.addrs.items)
	}
	if !strings.Contains(found, "2 clients attached") {
		t.Errorf("the menu row reads %q and does not carry the live client count", found)
	}
}
