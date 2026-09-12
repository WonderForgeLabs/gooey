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
// WHY ONLY THE SELF-MARKED HOSTS, and the reason is TRUTH rather than
// noise — which is worth separating, because the first draft of this
// comment had it the other way round.
//
// Widened to all four names in liftedSurfaceByName the matcher examines
// 16 sentences at the stack tip and flags none, so the restriction is
// not buying quiet. It is buying correctness: a MenuBar genuinely does
// NOT lift — its dropdown's surface does, which is why Grid.Row still
// places the bar — so "a MenuBar is not lifted" is a true sentence, and
// a guard that flags it is wrong rather than merely noisy. The scope is
// the names where there is no host-versus-surface distinction to lose:
// hosts that carry gooey.Overlay THEMSELVES.
//
// That scope is DERIVED, not listed. selfMarkedHosts asks the type
// system, so a host that adopts the marker directly comes under the
// guard on the commit that adopts it, and one that loses it drops out.
func TestNoDocSaysASelfMarkedHostStaysInDocumentOrder(t *testing.T) {
	hosts := selfMarkedHosts(t)
	if len(hosts) == 0 {
		t.Fatal("no overlay host carries gooey.Overlay on its own type, so this " +
			"guard is checking nothing. Either the markers came off, or " +
			"overlayHostByName has stopped naming the hosts")
	}
	t.Logf("hosts checked: %v", hosts)
	nameRe := regexp.MustCompile(`\b(` + strings.Join(hosts, "|") + `)\b`)
	// THE NEGATION HAS TO ATTACH TO THE HOST, not merely share a sentence
	// with it, and that distinction is the whole guard. See attachedNeg.
	attachedRe := regexp.MustCompile(`(?i)` + "`?" + `\b(?:` +
		strings.Join(hosts, "|") + `)\b` + "`?" +
		`(?:\s+[\w()` + "`" + `']+){0,3}\s+` + negSpellings)
	// THE PRESCRIPTIVE SPELLING, which is the half this guard shipped
	// without and which three live sites then walked through.
	//
	// Everything above keys on liftVerbRe — the sentence must contain the
	// word "lift" — so it only ever sees the DENIAL of the lift ("does not
	// lift", "no such lift"). The retired rule's other form does not argue
	// about lifting at all; it just tells the reader where to put the
	// element. Both sentences below are RETIRED — quoted as history, not
	// stated — since #437 lifted overlays into a layer and #439 ranked it:
	//
	//	README.md          "an `AdornmentLayer` (last child of the root)"
	//	howto-forms.md     "<AdornmentLayer/>   <!-- last child of the root -->"
	//
	// Both are false for a host carrying gooey.Overlay — position is
	// exactly what the marker stops mattering — and both were invisible
	// here, because neither contains "lift". A guard that only catches the
	// spelling that argues with it misses the spelling that simply
	// instructs, and the instruction is the one a reader follows.
	// THE SPELLINGS ARE INSTRUCTIONS, not descriptions, and "declared
	// last" is deliberately absent from them. components/toast.go's doc
	// comment says "a `ToastHost` declared last still landed BENEATH every
	// open dropdown — #439", which is a TRUE account of the bug the ranks
	// fixed. A guard that flags it is wrong rather than noisy, the same
	// distinction the attachment rule below was added for. What is caught
	// is prose telling a reader where to PUT one.
	//
	// The gap is `.{0,80}?` and NOT `[^.!?]{0,80}?`, which is what the
	// first draft used to mean "in the same sentence". That class cannot
	// cross an exclamation mark, and the second site this arm exists to
	// catch is an HTML comment quoting the rule #437 retired —
	// `<!-- last child of the root -->` — whose
	// `<!--` contains one. (That quotation is history, not a claim.) The constraint was redundant as well as wrong:
	// proseUnits has already cut the text into sentences, so every string
	// reaching this regex is one, and 80 characters is the proximity rule.
	positionRe := regexp.MustCompile(`(?i)` + "`?" + `\b(?:` +
		strings.Join(hosts, "|") + `)\b` + "`?" +
		// The alternation below is a list of RETIRED spellings — #437
		// lifted these hosts and #439 ranked them, so each is a rule to
		// catch, never one this file states.
		`.{0,80}?` +
		`(?:last child|declare it last|must be last|` +
		// Retired likewise (#437/#439): specimens, not claims.
		`last element|at the end of the root|bottom of the root)`)

	// flagged is the whole question, in one place, so the tree walk and
	// every fixture below ask it identically. A fixture that reimplements
	// the predicate is a fixture that can agree with a broken one.
	flagged := func(text string) bool {
		for _, s := range proseUnits(text) {
			if nameRe.MatchString(s) && positionRe.MatchString(s) {
				return true
			}
			if liftVerbRe.MatchString(s) && nameRe.MatchString(s) &&
				attachedRe.MatchString(s) {
				return true
			}
		}
		return false
	}

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
			if !nameRe.MatchString(s) {
				continue
			}
			// docs/specs/ is exempt from THIS arm, and only this one.
			//
			// A spec is a dated decision record — CLAUDE.md's convention,
			// and the reason they are named by the date of the decision
			// rather than of the commit. Four of them describe the
			// AdornmentLayer and ToastHost of 2026-08-10 — before #437
			// lifted them — when "the last
			// child of the root" was how those hosts got on top, and that
			// is a true account, superseded since, of what was decided then. Rewriting them
			// to match today would be falsifying the record; flagging
			// them would make this guard cry wolf on every historical
			// design note until someone did.
			//
			// The negation arm above needs no such exemption, because a
			// spec saying a host "does not lift" would still be wrong
			// about its own date — the marker predates none of them.
			if positionRe.MatchString(s) && !strings.HasPrefix(path, "../docs/specs/") {
				examined++
				t.Errorf("%s ties a host that implements gooey.Overlay to a POSITION:\n\t%s\n"+
					"The marker is what lifts it, from wherever it is declared, so a "+
					"placement rule here is an instruction a reader can follow and "+
					"still be wrong.", path, s)
				continue
			}
			if !liftVerbRe.MatchString(s) {
				continue
			}
			examined++
			if !attachedRe.MatchString(s) {
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
	// prose gets deleted. Measured at 9 on the branch that added this and
	// 9 at the stack tip above it; the floor sits under that with room to
	// edit, and far enough above zero that deleting the paragraphs which
	// make the claim fails.
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
		// Both fixtures quote sentences RETIRED by #437 and #439; they are
		// specimens the matcher must catch, not rules this file states.
		{"README's adornment row — the prescriptive spelling",
			"WPF's adorner plane: an `AdornmentLayer` (last child of the root) " +
				"hosts components positioned against a *target's* arranged bounds."},
		// Retired likewise (#437): quoted so the arm cannot pass vacuously.
		{"howto-forms' markup comment — the prescriptive spelling",
			"<AdornmentLayer/>   <!-- last child of the root -->"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !flagged(tc.text) {
				t.Errorf("the matcher does not flag the sentence it was written for:\n\t%s",
					tc.text)
			}
		})
	}

	// AND THE TWO IT MUST NOT FLAG, which is the arm the first version of
	// this guard lacked and paid for. Both are live at the stack tip and
	// both are CORRECT: the negation belongs to a dropdown's position in
	// one and to the MenuBar in the other. They are quoted here so that
	// tightening the sentence rule back into an attachment-free one goes
	// red in this package rather than four PRs later.
	for _, tc := range []struct{ name, text string }{
		{"architecture.md — the negation is a dropdown's position",
			"Z-order is document order **in two layers**: the ordinary tree, " +
				"and then every component implementing `gooey.Overlay` — a popup " +
				"surface, a `ToastHost`, an `AdornmentLayer` — lifted to the end " +
				"with its subtree, because a dropdown is not at a position in the " +
				"document, it is on top of it."},
		{"demos.md — the negation is the MenuBar",
			"the `MenuBar`'s dropdown, the `ToastHost` and the `AdornmentLayer` " +
				"are lifted out of document order into a paint layer of their own " +
				"and ranked within it, so they paint above every tab from wherever " +
				"their hosts sit — the BAR is not lifted and never was, which is " +
				"why `Grid.Row` still keeps it on the top row."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if flagged(tc.text) {
				t.Errorf("the matcher flags correct prose:\n\t%s\n"+
					"The negation in this sentence belongs to another subject. A "+
					"guard that fires on prose like this is noise, and noise is "+
					"how a guard gets deleted.", tc.text)
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
			heading + "It is not what decides that.", false},
		{"a negation in the SAME paragraph is",
			"## Overlays\n\nAn AdornmentLayer is not lifted, so declare it last.", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if hit := flagged(tc.text); hit != tc.want {
				t.Errorf("flagged=%v, want %v, for:\n\t%q\nunits: %q",
					hit, tc.want, tc.text, proseUnits(tc.text))
			}
		})
	}
}

var liftVerbRe = regexp.MustCompile(`(?i)\blift(?:s|ed|ing)?\b`)

// negSpellings is a LIST rather than anything cleverer — English negation
// is not a regular language and pretending otherwise buys false
// positives. Its incompleteness is the accepted cost: a new way to spell
// "does not lift" goes unnoticed until someone writes it down here. What
// it buys is that the two spellings this repo actually shipped cannot
// come back.
const negSpellings = `(?:do not|does not|don't|doesn't|no such|` +
	`not lifted|never lifted|is not|are not|was not|were not)`

// ATTACHMENT, and this is the correction that matters most in this file.
//
// The first version of this guard asked only whether a negation appeared
// in the same SENTENCE as a host name. It measured clean — and it was
// measured on the wrong base. #456's own tree does not yet contain the
// prose the sweep above it adds, and against the stack tip that rule
// flags two sentences that are entirely CORRECT:
//
//	docs/architecture.md — "… a `ToastHost`, an `AdornmentLayer` —
//	lifted to the end with its subtree, because a dropdown IS NOT at a
//	position in the document, it is on top of it."
//
//	docs/demos.md — "… the `ToastHost` and the `AdornmentLayer` are
//	lifted … — the BAR IS NOT lifted and never was …"
//
// In both the negation belongs to a different subject: a dropdown's
// position in the first, the MenuBar in the second. An absence proof
// inherits its base, and the base here is what main will look like.
//
// So the negation must FOLLOW the host name within three words of no
// consequence, with nothing that ends a clause in between — the
// character class admits no comma, semicolon, colon or dash. That is
// what separates
//
//	"`ToastHost` has no such lift yet"                (attached, wrong)
//	"ToastHost and AdornmentLayer do not"             (attached, wrong)
//
// from the two above, where the nearest host name is nine and thirty
// words back behind a dash. Measured against the stack tip — which is
// the base that matters, and the one the first version skipped: 9
// sentences examined, none flagged, and both defective spellings flagged
// as fixtures below.
//
// It is an approximation of "whose subject is this", and the shape of
// what it gives up is stated rather than left to be discovered: a
// negation that PRECEDES its host ("nothing lifts a ToastHost") or sits
// further than three words after it goes unseen.

// proseUnits cuts a file into the units a polarity question can be asked
// of. Two things decide the cuts and both were forced by a measurement.
//
// FLATTEN INSIDE A BLOCK, NEVER ACROSS ONE. These documents wrap at 72
// columns, so a claim routinely spans three lines and matching per-line
// would miss it; flattening the whole FILE, on the other hand, welds
// unrelated paragraphs into one unit and a negation from one lands in a
// lift claim from another.
//
// A MARKDOWN TABLE ROW USED TO BE ITS OWN BLOCK HERE and is not any
// more, which is worth recording because the rule was load-bearing until
// it wasn't. README.md's feature table has no blank lines in it, so
// block-flattening made the whole table one unit, and under the old
// sentence-level polarity rule that flagged the ranks spec's check table
// for a negation four rows from the word "lift". The attachment rule
// subsumes it: a negation thirty words and four table cells away from a
// host name is no longer that host's. Measured — removing the row split
// leaves the tree at 0 flagged and every fixture green, so it is gone
// rather than kept as a mechanism nothing can falsify.
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
		add(block)
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
