package components

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// THIS FILE IS WHAT SURVIVED THE EXPIRY, and the split is the point.
//
// It held two guards over docs/learn/concepts/overlays.md. The first,
// TestTheHostsThisPageCallsPositionDependentStillAre, was EXPIRING by
// design: it read the hosts the page called position-dependent and
// failed the moment either adopted gooey.Overlay. #439 is that moment,
// so it is gone, along with the three paragraphs its failure message
// named — which is exactly what it was written to ask for.
//
// The second does not expire, and deleting it with its neighbour would
// have been the easy mistake. "The names this page calls lifted really
// are lifted" is a claim about the CURRENT tree whatever the current
// tree is: it was wrong before #439 (the page named the Tooltip popup)
// and it can be wrong again the next time a host is added or a marker
// comes off. Raised in review of #455, kept through #439.

// TestTheHostsThisPageCallsLiftedActuallyAre reads the lifted names out
// of the page and checks each one against the type system.
//
// The page listed "the `Tooltip` popup" among the lifted surfaces for
// two rounds, one sentence after saying the opposite — and the guard
// that existed could not see it, because it read only the names on the
// UNLIFTED side. A reader who believed the lifted claim moves the
// AdornmentLayer and loses their tips behind the page.
//
// RESOLVED THROUGH THE SURFACE where a host has one: MenuBar and Popup
// do not implement the marker themselves — popupSurface does — so asking
// the host would assert the opposite of the page about both and pass for
// the wrong reason.
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
				"resolve that name to the value whose type answers. Add it to "+
				"liftedSurfaceByName — a name the guard cannot check is a claim "+
				"nothing is keeping true", name)
			continue
		}
		if _, isOverlay := surf().(gooey.Overlay); !isOverlay {
			t.Errorf("the page says %s is lifted, but the value that would carry the "+
				"marker does not implement gooey.Overlay — so it paints in the "+
				"ordinary layer and its position in document order decides where it "+
				"lands. Either the page is wrong, or the marker came off a type that "+
				"still needs it", name)
		}
	}

	// A TOOLTIP IS THE NEGATIVE, and it is asserted rather than left to
	// the list's silence: the page can only be wrong about a name it
	// mentions, and the failure mode here was mentioning one that should
	// not be there. If a Tooltip ever becomes lifted on its own account
	// this fails and the paragraph about tipPopup comes out with it.
	var tip any = &Tooltip{}
	if _, isOverlay := tip.(gooey.Overlay); isOverlay {
		t.Error("Tooltip now implements gooey.Overlay. The page says its tip is " +
			"lifted by the AdornmentLayer that hosts it rather than on its own " +
			"account; correct that paragraph and this arm together")
	}
}

// liftedRe captures the run of backticked names immediately before
// "**are** lifted".
//
// Anchored on the sentence rather than a line number, because a line
// number in a test is the failure mode line-referencing docs already
// has: it goes stale in silence and the guard starts reading a paragraph
// about something else.
var liftedRe = regexp.MustCompile(
	"((?:`[A-Za-z][A-Za-z0-9]*`(?:,)?(?: and)?\\s+)+)\\*\\*are\\*\\*\\s+lifted")

func liftedHostsNamed(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the overlays concept page: %v", err)
	}
	// FLATTENED, because the claim wraps across lines in a 72-column
	// document and re-wrapping it must not be a failure.
	flat := strings.Join(strings.Fields(string(body)), " ")
	m := liftedRe.FindStringSubmatch(flat)
	if m == nil {
		t.Fatalf("%s no longer contains the sentence this guard is anchored to "+
			"(\"… **are** lifted\"). Re-anchor it, or delete it with the paragraph", path)
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

// liftedSurfaceByName resolves a name to the value whose type carries
// the marker — the hosted surface for the two Popup-backed hosts, the
// host itself for the two that adopted it in #439. It is a LOOKUP, not
// the list: what is at stake comes from the page, and a name missing
// here fails the test rather than being skipped.
var liftedSurfaceByName = map[string]func() any{
	"Popup": func() any {
		return NewPopup(&Border{}, func(*gooey.Frame, gooey.Rect) {}).Surface()
	},
	"MenuBar": func() any {
		return (&MenuBar{}).popup().Surface()
	},
	"ToastHost":      func() any { return &ToastHost{} },
	"AdornmentLayer": func() any { return &AdornmentLayer{} },
}
