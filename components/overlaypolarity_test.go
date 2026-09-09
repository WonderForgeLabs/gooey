package components

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// TestNoDocSaysASelfMarkedHostStaysInDocumentOrder is the guard the
// review of #456 showed was missing, and it is missing in a specific
// shape worth naming: the claim "which hosts lift" was checked on ONE
// page and hand-written on two more.
//
// TestTheHostsThisPageCallsLiftedActuallyAre reads
// docs/learn/concepts/overlays.md and resolves every name it calls
// lifted against the type system. That page was right. Meanwhile #439
// put the marker on ToastHost and AdornmentLayer, and
//
//   - README.md's feature table still said "`ToastHost` has no such lift
//     yet, so for it document order is still the whole rule", and
//   - cmd/toolkit/toolkit.gooey — ON SCREEN, in the overlays tab — still
//     said "ToastHost and AdornmentLayer do not".
//
// Both survived a full PR. A guard anchored to one path cannot see a
// claim made anywhere else, so this one has no path in it: it walks the
// tree and asks the question of every sentence that makes it.
//
// WHY ONLY THE SELF-MARKED HOSTS, and the answer is a measurement rather
// than a preference. Run the same matcher over all four names in
// liftedSurfaceByName and it examines 17 sentences and flags one, and
// that one is CORRECT prose:
//
//	docs/specs/2026-08-30-overlay-layer.md — "**The lift is global, not
//	within the overlay's parent.** 'Above my own siblings' is not enough
//	and never could be: a `MenuBar` three containers deep still has to
//	drop its menu over a dock …"
//
// A MenuBar genuinely does not lift — its dropdown's surface does — so
// "MenuBar is not lifted" is true of the bar and false of the menu, and
// no amount of pattern is going to separate those in English. A guard
// that fires on correct prose is noise, and noise is how a guard gets
// deleted, so the scope is the names where the distinction does not
// exist: hosts that carry gooey.Overlay THEMSELVES.
//
// That scope is DERIVED, not listed. selfMarkedHosts asks the type
// system, so a host that adopts the marker directly comes under the
// guard on the commit that adopts it, and one that loses it drops out.
//
// Measured on this branch: 8 sentences examined, none flagged, and both
// defects above flagged when fed in as fixtures below.
func TestNoDocSaysASelfMarkedHostStaysInDocumentOrder(t *testing.T) {
	hosts := selfMarkedHosts(t)
	if len(hosts) == 0 {
		t.Fatal("no overlay host carries gooey.Overlay on its own type, so this " +
			"guard is checking nothing. Either the markers came off, or " +
			"overlayHostByName has stopped naming the hosts")
	}
	t.Logf("hosts checked: %v", hosts)
	nameRe := regexp.MustCompile(`\b(` + strings.Join(hosts, "|") + `)\b`)

	var examined int
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Dot-directories pruned at EVERY depth, not just the top:
		// .claude/worktrees holds whole other checkouts of this repo,
		// and reading another agent's tree would make this guard report
		// on prose that is not ours. Same reason CLAUDE.md's verify loop
		// prunes with -name '.?*' rather than -not -path './.*'.
		if d.IsDir() {
			if n := d.Name(); n == "vendor" || n == "node_modules" ||
				(strings.HasPrefix(n, ".") && n != "..") {
				return fs.SkipDir
			}
			return nil
		}
		// The corpus is what a READER IS TOLD: prose, shipped markup,
		// and doc comments on the code itself. _test.go is out, and not
		// for tidiness — THIS FILE quotes both defective sentences, once
		// in the comment above and once as the fixture that proves the
		// matcher fires, so a corpus including tests flags the guard
		// itself and there is no wording that escapes it. A test that
		// quotes a wrong sentence in order to catch it is not making the
		// claim. The cost is real and stated: a wrong claim in a test
		// comment goes unguarded.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		switch filepath.Ext(path) {
		case ".md", ".gooey", ".go":
		default:
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		for _, s := range proseUnits(string(body)) {
			if !liftVerbRe.MatchString(s) || !nameRe.MatchString(s) {
				continue
			}
			examined++
			if !unliftedRe.MatchString(s) {
				continue
			}
			t.Errorf("%s says a host that implements gooey.Overlay does not lift:\n\t%s\n"+
				"Hosts carrying the marker are lifted out of document order into "+
				"the overlay layer wherever they are declared, so a reader who "+
				"believes this sentence declares for position and loses. If the "+
				"marker is what changed, this sentence and the type go together.",
				path, s)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}

	// NON-VACUITY, and it is a floor with a number in it because the
	// alternative is a guard that passes by finding nothing.
	//
	// A FLOOR AND NOT THE MEASURED VALUE, deliberately, and this is the
	// one place that choice is right. Elsewhere in this repo a policy
	// number has to be pinned by EQUALITY, because >= lets a demotion
	// hide behind an addition — but the quantity here is how many
	// sentences the docs happen to contain, which is not a policy and
	// moves whenever anyone writes a paragraph. Pinning it exactly makes
	// every prose edit a failing test, and a guard that fires on correct
	// prose gets deleted. Measured at 8 on the branch that added this;
	// the floor sits under that with room to edit, and far enough above
	// zero that deleting the paragraphs which make the claim fails.
	const wantExamined = 5
	if examined < wantExamined {
		t.Errorf("only %d sentences in the tree make a lift claim about %v, want at "+
			"least %d. Either the docs stopped describing the overlay layer, or "+
			"proseUnits/liftVerbRe stopped matching the way they are written — "+
			"and a matcher that finds nothing passes for the wrong reason",
			examined, hosts, wantExamined)
	}
	t.Logf("%d sentences examined", examined)

	// AND THE MATCHER ITSELF, fed the two sentences that were live in
	// this repo when the guard was written. Without this the test above
	// is only evidence that nothing matched.
	for _, tc := range []struct{ name, text string }{
		{"README's feature table",
			"`ToastHost` has no such lift yet, so for it document order is " +
				"still the whole rule."},
		{"the toolkit's on-screen caption",
			`<Text Grid.Row="0" Style="dim">MenuBar dropdowns and Popups lift ` +
				`out of document order; ToastHost and AdornmentLayer do not</Text>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hit bool
			for _, s := range proseUnits(tc.text) {
				if liftVerbRe.MatchString(s) && nameRe.MatchString(s) &&
					unliftedRe.MatchString(s) {
					hit = true
				}
			}
			if !hit {
				t.Errorf("the matcher does not flag the sentence it was written for:\n\t%s",
					tc.text)
			}
		})
	}

	// AND THE BLOCK SPLIT, which the tree cannot exercise.
	//
	// Measured: replacing strings.Split(body, "\n\n") with a single
	// whole-file block left every test on this branch GREEN — the corpus
	// happens to contain no paragraph that ends without a period next to
	// one that opens with a negation. A mechanism whose removal is
	// silent is a state to resolve rather than ship, so the case the tree
	// lacks is supplied here, and the same mutation now goes red on the
	// first arm below.
	//
	// THE PAIR IS THE MECHANISM. The first arm alone is a negative
	// assertion, and a negative assertion passes for any reason at all —
	// including a matcher that has stopped matching. The second moves the
	// negation INTO the paragraph that makes the claim and requires a
	// flag from the same fixture shape, so "no flag" can only mean the
	// paragraphs stayed apart.
	const heading = "## AdornmentLayer is lifted\n\n"
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{"a negation in the NEXT paragraph is not this claim's",
			heading + "Grid.Row is not what decides that.", false},
		{"a negation in the SAME paragraph is",
			"## Overlays\n\nAn AdornmentLayer is not lifted, so declare it last.", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hit bool
			for _, s := range proseUnits(tc.text) {
				if liftVerbRe.MatchString(s) && nameRe.MatchString(s) &&
					unliftedRe.MatchString(s) {
					hit = true
				}
			}
			if hit != tc.want {
				t.Errorf("flagged=%v, want %v, for:\n\t%q\nunits: %q",
					hit, tc.want, tc.text, proseUnits(tc.text))
			}
		})
	}
}

var (
	liftVerbRe = regexp.MustCompile(`(?i)\blift(?:s|ed|ing)?\b`)
	// unliftedRe is the polarity, and it is a LIST OF SPELLINGS rather
	// than anything cleverer — English negation is not a regular
	// language and pretending otherwise would buy the false positives
	// this guard's scope exists to avoid. Its incompleteness is the
	// accepted cost: a new way to spell "does not lift" goes unnoticed
	// until someone adds it. What it does buy is that the two spellings
	// this repo actually shipped cannot come back.
	unliftedRe = regexp.MustCompile(
		`(?i)\b(?:do not|does not|don't|doesn't|no such|not lifted|` +
			`never lifted|is not|are not|neither|nor)\b`)
	tableRowRe = regexp.MustCompile(`^\s*\|`)
)

// proseUnits cuts a file into the units a polarity question can be asked
// of. Two things decide the cuts and both were forced by a measurement.
//
// FLATTEN INSIDE A BLOCK, NEVER ACROSS ONE. These documents wrap at 72
// columns, so a claim routinely spans three lines and matching per-line
// would miss it; flattening the whole FILE, on the other hand, welds
// unrelated paragraphs into one unit and a negation from one lands in a
// lift claim from another.
//
// A MARKDOWN TABLE ROW IS ITS OWN BLOCK. README.md's feature table has
// no blank lines in it, so block-flattening alone made the entire table
// a single unit — which flagged the ranks spec's check table for a
// negation four rows away from the word "lift".
//
// Both of those were live false positives before the split was written
// this way, which is why the shape is spelled out here rather than left
// to read as fussiness.
func proseUnits(body string) []string {
	var out []string
	add := func(s string) {
		flat := strings.Join(strings.Fields(s), " ")
		if flat == "" {
			return
		}
		out = append(out, splitSentences(flat)...)
	}
	for _, block := range strings.Split(body, "\n\n") {
		var rest []string
		for _, line := range strings.Split(block, "\n") {
			if tableRowRe.MatchString(line) {
				add(line)
				continue
			}
			rest = append(rest, line)
		}
		add(strings.Join(rest, " "))
	}
	return out
}

// splitSentences cuts on a period followed by space. Go has no
// lookbehind, so the boundary is rebuilt rather than matched.
func splitSentences(flat string) []string {
	var out []string
	start := 0
	for i := 0; i+1 < len(flat); i++ {
		if flat[i] == '.' && flat[i+1] == ' ' {
			out = append(out, strings.TrimSpace(flat[start:i+1]))
			start = i + 1
		}
	}
	if s := strings.TrimSpace(flat[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

// overlayHostByName is the HOST value for each name the overlays page
// names — deliberately not liftedSurfaceByName, which resolves MenuBar
// and Popup through the surface that carries their marker. The question
// here is the opposite one: does the thing a document CALLS by this name
// carry the marker itself? For MenuBar and Popup the answer is no, and
// that is why they are out of scope rather than why they are absent.
var overlayHostByName = map[string]func() any{
	"Popup": func() any {
		return NewPopup(&Border{}, func(*gooey.Frame, gooey.Rect) {})
	},
	"MenuBar":        func() any { return &MenuBar{} },
	"ToastHost":      func() any { return &ToastHost{} },
	"AdornmentLayer": func() any { return &AdornmentLayer{} },
}

// selfMarkedHosts asks the type system which of those hosts is itself a
// gooey.Overlay, sorted so the regexp this builds is stable.
func selfMarkedHosts(t *testing.T) []string {
	t.Helper()
	var out []string
	for name, make := range overlayHostByName {
		if _, ok := make().(gooey.Overlay); ok {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}
