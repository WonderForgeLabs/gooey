package components

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// TestTheHostsThisPageCallsPositionDependentStillAre is an EXPIRING
// guard, and expiring is the whole design.
//
// Review of #455 found docs/learn/concepts/overlays.md saying an overlay
// "paints above the page from wherever it is declared" and naming
// MenuBar, ToastHost and AdornmentLayer in the same breath. It is true
// of the first and false of the other two: only components.Popup's
// surface implements gooey.Overlay today, so a ToastHost moved off the
// end of its Grid falls under whatever is declared after it and the
// toasts silently go behind the page — a failure that is invisible until
// somebody's notification does not appear.
//
// Correcting the prose is not enough, because the prose is only right
// until #439 adopts the marker on both hosts — at which point the
// paragraph saying "not lifted yet" becomes the new wrong sentence and
// nothing would say so. So the claim is ASSERTED FROM THE TYPE SYSTEM:
// this fails the moment either host implements the interface, and its
// message says which paragraphs come out with it.
//
// A test that is SUPPOSED to be deleted, in other words. CLAUDE.md's
// rule for a caveat is that it be derived or expiring rather than
// hand-maintained; this is the expiring kind, and the commit that turns
// it red is the commit that should delete it.
//
// THE HOSTS ARE READ FROM THE PARAGRAPH, not listed here. The first
// version of this test named ToastHost and AdornmentLayer in a Go slice
// — which is a written list guarding a written list, and dropping a name
// from it was measured SILENT. The doc is the thing being protected, so
// the doc is what says which names are at stake; a name it grows that
// this file cannot resolve is a failure, not a skip.
func TestTheHostsThisPageCallsPositionDependentStillAre(t *testing.T) {
	// NON-VACUITY FIRST. If gooey.Overlay stopped being satisfiable —
	// renamed, or the assertion below written against the wrong type —
	// every arm would pass by saying nothing. The Popup surface is the
	// one adopter, so it is the fixture that proves the question is
	// still askable.
	//
	// This floor is UNFALSIFIABLE while it holds, and that is worth
	// stating rather than pretending otherwise: deleting it changes no
	// outcome on a tree where the surface is still an Overlay. It exists
	// for the tree where that stops being true, which is a future state
	// and not a present one. Measured; recorded rather than papered over.
	var lifted any = NewPopup(&Border{}, func(*gooey.Frame, gooey.Rect) {}).Surface()
	if _, ok := lifted.(gooey.Overlay); !ok {
		t.Fatal("components.Popup's surface does not implement gooey.Overlay, so this " +
			"test is asking a question nothing can answer and the assertions below " +
			"pass for any reason")
	}

	names := unliftedHostsNamed(t, "../docs/learn/concepts/overlays.md")
	if len(names) == 0 {
		t.Fatal("the overlays concept page no longer names any host as unlifted. " +
			"Either #439 landed — in which case delete this test and the three " +
			"paragraphs its failure message lists — or the paragraph was reworded and " +
			"this guard is now checking nothing")
	}
	t.Logf("the page calls these hosts position-dependent: %v", names)
	for _, name := range names {
		w, ok := hostByName[name]
		if !ok {
			t.Errorf("the overlays concept page calls %q position-dependent and this "+
				"test cannot resolve that name to a type. Add it to hostByName — a "+
				"name the guard cannot check is a claim nothing is keeping true", name)
			continue
		}
		if _, isOverlay := w(t).(gooey.Overlay); isOverlay {
			t.Errorf("the surface %s hosts now implements gooey.Overlay, so it is "+
				"LIFTED and its position "+
				"in document order no longer decides anything. Three claims to the "+
				"contrary come out with this test, and then delete the test:\n"+
				"  docs/learn/concepts/overlays.md — the \"Which surfaces are lifted\" paragraph\n"+
				"  docs/markup-reference.md — the MenuBar hosting note's last sentence\n"+
				"  docs/learn/07-app-chrome.md — both recap bullets\n"+
				"That is #439.", name)
		}
	}
}

// TestTheHostsThisPageCallsLiftedActuallyAre is the other direction of
// the same claim, and it is here because the guard was one regex short
// of covering its own subject.
//
// unliftedRe reads only the run of names before "do **not** implement
// gooey.Overlay". A host wrongly named on the LIFTED side was unchecked
// — and that is not hypothetical: the page listed "the `Tooltip` popup"
// among the lifted surfaces while the very next sentence said the
// opposite, and a reader who believed it would move the AdornmentLayer
// and lose their tips behind the page. A Tooltip's tip is tipPopup, an
// ordinary leaf the layer hosts; nothing in components implements
// gooey.Overlay except popupSurface. Raised in review of #455, which is
// the round-five finding one name over.
//
// RESOLVED THROUGH THE SURFACE, not through the host: neither MenuBar
// nor Popup implements the marker itself — the surface each of them
// hosts does — so asking the host would assert the opposite of the page
// about both and pass for the wrong reason, which is exactly the bug
// unliftedRe's own comment records.
func TestTheHostsThisPageCallsLiftedActuallyAre(t *testing.T) {
	names := liftedHostsNamed(t, "../docs/learn/concepts/overlays.md")
	if len(names) == 0 {
		t.Fatal("the overlays concept page no longer names any host as lifted, so " +
			"this guard is checking nothing")
	}
	t.Logf("the page calls these hosts lifted: %v", names)
	for _, name := range names {
		surf, ok := liftedSurfaceByName[name]
		if !ok {
			t.Errorf("the overlays concept page calls %q lifted and this test cannot "+
				"resolve that name to the surface it hosts. Add it to "+
				"liftedSurfaceByName — a name the guard cannot check is a claim "+
				"nothing is keeping true", name)
			continue
		}
		if _, isOverlay := surf(t).(gooey.Overlay); !isOverlay {
			t.Errorf("the page says %s is lifted, but the surface it hosts does not "+
				"implement gooey.Overlay — so it paints in the ordinary layer and its "+
				"position in document order decides where it lands. Either the page is "+
				"wrong, or the marker came off a type that still needs it", name)
		}
	}

	// NEITHER LIST MAY CLAIM A NAME THE OTHER DOES. The page contradicted
	// itself across two adjacent sentences for two rounds, and each arm
	// alone is happy to let it: the unlifted arm never reads the lifted
	// clause and this one never reads the unlifted clause.
	unlifted := map[string]bool{}
	for _, n := range unliftedHostsNamed(t, "../docs/learn/concepts/overlays.md") {
		unlifted[n] = true
	}
	for _, n := range names {
		if unlifted[n] {
			t.Errorf("the overlays concept page names %s as BOTH lifted and "+
				"position-dependent. One of the two sentences is wrong and a reader "+
				"has no way to tell which", n)
		}
	}
}

// liftedRe is unliftedRe's mirror: the run of names immediately before
// "**are** lifted".
var liftedRe = regexp.MustCompile(
	"((?:`[A-Za-z][A-Za-z0-9]*`(?:,)?(?: and)?\\s+)+)\\*\\*are\\*\\*\\s+lifted")

func liftedHostsNamed(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the overlays concept page: %v", err)
	}
	flat := strings.Join(strings.Fields(string(body)), " ")
	m := liftedRe.FindStringSubmatch(flat)
	if m == nil {
		t.Fatalf("%s no longer contains the sentence this guard is anchored to "+
			"(\"… **are** lifted\"). Re-anchor it, or delete it with the paragraph", path)
	}
	return backticked(m[1])
}

// liftedSurfaceByName resolves a name to the SURFACE it hosts, since
// that is the thing the marker is on. Built per call rather than stored,
// because a Popup allocates one.
var liftedSurfaceByName = map[string]func(*testing.T) any{
	"Popup": func(*testing.T) any {
		return NewPopup(&Border{}, func(*gooey.Frame, gooey.Rect) {}).Surface()
	},
	"MenuBar": func(*testing.T) any {
		return (&MenuBar{}).popup().Surface()
	},
}

// hostByName resolves a name the concept page uses to the value whose
// type answers the question. It is a LOOKUP, not the list: what is at
// stake comes from the page, and a name missing here fails the test
// rather than being skipped.
//
// THROUGH THE SURFACE, exactly as liftedSurfaceByName does, and it did
// not until review of #455 — for the one entry where host and surface
// differ and it matters.
//
// A Tooltip never paints: Tooltip.Render is empty and NonVisual()
// returns true (tooltip.go). The thing on screen is tipPopup. So
// `&Tooltip{}` could never implement gooey.Overlay whatever happened to
// tipPopup, and the arm asserting "these are still position-dependent"
// would have stayed green while the page's claim about Tooltip became
// wrong — the one host a reader is most likely to move an
// AdornmentLayer for.
//
// The sibling test's own comment states this rule in full ("RESOLVED
// THROUGH THE SURFACE, not through the host … asking the host would
// assert the opposite of the page about both and pass for the wrong
// reason"), and liftedSurfaceByName follows it. This map is the copy
// that did not — a rule written down once and applied once.
//
// MenuBar and Popup resolve through their surfaces here too. They are
// unreachable in practice, because the cross-check errors if a name
// appears on both lists, but leaving two entries resolving one way and
// three the other is how the next reader learns the wrong rule.
//
// Funcs rather than values, for liftedSurfaceByName's reason: a Popup
// allocates its surface.
var hostByName = map[string]func(*testing.T) any{
	"ToastHost":      func(*testing.T) any { return &ToastHost{} },
	"AdornmentLayer": func(*testing.T) any { return &AdornmentLayer{} },
	"MenuBar":        func(*testing.T) any { return (&MenuBar{}).popup().Surface() },
	"Tooltip":        func(*testing.T) any { return &tipPopup{tip: &Tooltip{}} },
	"Popup": func(*testing.T) any {
		return NewPopup(&Border{}, func(*gooey.Frame, gooey.Rect) {}).Surface()
	},
}

// unliftedRe matches the run of backticked names immediately before the
// page's "do **not** implement `gooey.Overlay`" clause.
//
// The window is that run, not a fixed number of characters before
// it. A 200-byte lookback was the first spelling and it was WRONG in the
// direction that passes: the preceding sentence names `components.Popup`,
// `MenuBar` and `Tooltip` as the surfaces that ARE lifted, so the guard
// swept them in and asserted the opposite of the page about three types
// — and passed, because none of those types implements the marker
// itself either (popupSurface does). A guard that reads more than its
// claim is a guard that is green for the wrong reason.
//
// Anchored on the sentence rather than a line number, because a line
// number in a test is the failure mode line-referencing docs already
// has: it goes stale in silence and the guard starts reading a paragraph
// about something else.
var unliftedRe = regexp.MustCompile(
	"((?:`[A-Za-z][A-Za-z0-9]*`(?:,)?(?: and)?\\s+)+)do \\*\\*not\\*\\*\\s+implement `gooey\\.Overlay`")

// unliftedHostsNamed reads the CLAUSE that makes the claim and returns
// the backticked names inside it.
func unliftedHostsNamed(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the overlays concept page: %v", err)
	}
	// FLATTENED, because the claim wraps across lines in a 72-column
	// document and re-wrapping it must not be a failure.
	flat := strings.Join(strings.Fields(string(body)), " ")
	m := unliftedRe.FindStringSubmatch(flat)
	if m == nil {
		t.Fatalf("%s no longer contains the sentence this guard is anchored to "+
			"(\"… do **not** implement `gooey.Overlay`\"). If #439 landed, delete this "+
			"test and the three paragraphs the failure message lists; if the page was "+
			"reworded, re-anchor it", path)
	}
	return backticked(m[1])
}

var backtickRe = regexp.MustCompile("`([A-Za-z][A-Za-z0-9]*)`")

func backticked(s string) []string {
	var out []string
	for _, m := range backtickRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}
