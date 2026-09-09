package gooey

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// "Z-order is document order, so declare the overlay LAST" was the
// framework's rule until #437 lifted overlays into a paint layer of their
// own, #439 ranked that layer, and #438 made both paint paths ask one
// function. It survived the code by a wide margin: #443 was filed naming
// ~15 sites still teaching it, and the sweep that closed it found roughly
// thirty-five — because the issue's table was a sample and everyone,
// including the sweep's first pass, searched for the phrasings already in
// it.
//
// This test is the part that does not go stale. It does not hold a list
// of sites and it does not assert the phrase is absent — the phrase is
// legitimately present in a dozen places that quote it in order to bury
// it, and a test demanding its absence would force those to be deleted,
// taking the history with them.
//
// What it asserts is that THE CLAIM IS NEVER MADE UNQUALIFIED. Every line
// stating the rule must carry, within a few lines, either the correction
// that makes it true ("in two layers", a rank, gooey.Overlay) or a marker
// that the sentence is being quoted as history ("used to say",
// "superseded", "no longer"). That is checkable from content alone, so a
// file added next year is covered without anyone remembering this exists.
//
// Deliberately NOT a repo-wide ban on the word "z-order": ordinary
// document order is still the rule for everything that is not lifted, and
// dozens of comments correctly describe the forward pass, the restore
// sweep and later-sibling painting. Only the overlay-hosting rule is the
// one that changed.
// PARALLEL OVER THE FILES, because this is the most expensive test in
// the root package — 11.9s of the package's 26s, measured — and the work
// is embarrassingly parallel: read a file, match it, produce findings.
// Nothing shared is written.
//
// The findings are collected into a slice INDEXED BY FILE and reported
// after the join, not sent to t.Errorf from the workers. Two reasons,
// and the first is correctness: t.Fatalf outside the test's own
// goroutine does not fail the test, it is a bare runtime.Goexit and the
// message is lost. The second is that a test whose failures arrive in
// scheduler order is a test whose output changes between runs for no
// reason anybody can act on.
//
// Raised in review of #458.
func TestNoFileTeachesTheRetiredOverlayRule(t *testing.T) {
	scanForRetiredRule(t, statesTheRetiredRule, prefilterWords, qualifierRes, scanAdvice)
}

// scanAdvice is NAMED rather than written at the call, because
// TestTheScanItselfReportsAFixtureFile drives the same scan and would
// otherwise be pinning a different string from the one the repo sees.
const scanAdvice = "Overlays are lifted out of document order into a paint " +
	"layer (#437) and ranked within it (#439), so declaring " +
	"one last decides nothing. Since #465 that is true of " +
	"HIT-TESTING TOO — naming the hit walk used to earn a line " +
	"an exemption here and no longer does. Either state the " +
	"current rule, or mark the sentence as history; the " +
	"markers this test accepts are in qualifierRes."

// TestNoFileTeachesTheRetiredInputRule is the same guard on the INPUT
// plane, and its absence is what let four live sites survive #465.
//
// The paint rule retired in #437; the input rule — "hit-testing walks
// document order, so a later sibling takes the press" — retired in #465,
// and this file closed the exemption that used to wave such a line
// through WITHOUT adding a pattern for the claim that had just become
// wrong. Removing a hole is not the same as guarding what fell into it:
// component.go's own Overlay doc, this file's qualifierRes comment,
// docs/learn/07-app-chrome.md and components/toast_test.go all still
// taught it, and none was reachable by retiredRule. They were found with
// a grep the suite does not run, which is the definition of unguarded.
//
// SHARES THE EPITAPHS AND NOT THE CORRECTIONS, which is not where this
// started. "Used to say", "superseded", "no longer" bury whichever rule
// stands beside them, so those are one list. The CORRECTION markers are
// not interchangeable, and two of the paint ones exempt the very
// sentence the input guard exists to catch — see qualifierRes, and
// TestThePaintCorrectionDoesNotExemptTheInputClaim, which measures it.
// Raised in review of #478.
func TestNoFileTeachesTheRetiredInputRule(t *testing.T) {
	scanForRetiredRule(t, statesTheRetiredInputRule, inputPrefilterWords, inputQualifierRes,
		"FocusManager.HitTest asks overlayOf since #465 — the same "+
			"membership-and-rank rule paint derives its order from — so "+
			"the hit walk is lifted, it does know about the marker, and "+
			"a later ordinary sibling does not take the press from an "+
			"overlay. Either state the current rule, or mark the "+
			"sentence as history; the markers this test accepts are in "+
			"inputQualifierRes — note that a #437/#439 citation or the "+
			"bare word \"lifted\" is NOT one of them, because a paint "+
			"correction says nothing about the hit walk and \"is not "+
			"lifted\" would qualify itself.")
}

// scanForRetiredRule is the walk both guards run, differing only in the
// patterns, the prefilter, and the advice in the failure.
//
// It was the body of TestNoFileTeachesTheRetiredOverlayRule until #478
// needed a second plane. Copying it would have copied the join, the span
// arithmetic, the superseded-head exemption and the prefilter contract —
// four subtleties this file spent a review each on — into a place where
// only one copy would receive the next fix.
// PARALLEL OVER THE FILES, because between them these two guards are
// the most expensive thing in the root package — 11.9s of 26s for the
// paint one alone, measured — and the work is embarrassingly parallel:
// read a file, match it, produce findings. Nothing shared is written.
//
// The findings are collected into a slice INDEXED BY FILE and reported
// after the join, not sent to t.Errorf from the workers. Two reasons,
// and the first is correctness: t.Fatalf outside the test's own
// goroutine does not fail the test, it is a bare runtime.Goexit and the
// message is lost. The second is that a test whose failures arrive in
// scheduler order is a test whose output changes between runs for no
// reason anybody can act on.
//
// Raised in review of #458, generalised to two planes in review of #478.
func scanForRetiredRule(t *testing.T, states func(string) bool, prefilter []string, quals []*regexp.Regexp, advice string) {
	t.Helper()
	files := docFiles(t)
	found := make([][]string, len(files))
	errs := make([]error, len(files))

	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			found[i], errs[i] = retiredRuleProblems(f, states, prefilter, quals, advice)
		}()
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, ps := range found {
		for _, p := range ps {
			t.Error(p)
		}
	}
}

// retiredRuleProblems is the per-file half, pure so it can run off the
// test's goroutine: it returns what it found rather than reporting it.
func retiredRuleProblems(f string, states func(string) bool, prefilter []string, quals []*regexp.Regexp, advice string) ([]string, error) {
	var problems []string
	{
		body, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}
		// A PREFILTER, because the loop below runs a dozen
		// case-insensitive alternations over every line of every
		// .go/.md/.gooey file in the tree. Raised in review of #458,
		// with a claim that this drops the cost "to near-nothing".
		//
		// IT DOES NOT, AND THE MEASURED NUMBERS ARE HERE so the next
		// person does not repeat the two attempts. Walk plus read plus
		// this filter is 113ms of it; the rest is the regex loop.
		// Substring filtering took the pair of tests from ~4.5s to
		// ~3.6s — real, but far from nothing, because 512 of 788 files
		// contain "order" in a repo whose docs are largely about
		// ordering.
		//
		// Prefiltering with retiredRule ITSELF — which is the version
		// that cannot drift out of step with the pattern list — was
		// tried and is WORSE, 5.6s: it scans all 7MB with every pattern
		// and then the per-line loop scans the matches again. The cheap
		// inexact filter beats the exact one here.
		//
		// WHAT ACTUALLY MOVED THE NUMBER was running the files in
		// parallel, not filtering harder: 11.9s to 1.4s. The filter is
		// still worth keeping — it is what makes each worker cheap —
		// but the numbers above are the reason to stop tuning it. Both
		// measurements stay because the second is only interesting
		// against the first.
		//
		// So this stays — and the words it gates on are now a named list
		// that a test CHECKS against every sample retiredRule is pinned
		// with, rather than a hazard written down and hoped for. The
		// comment that used to live here said "KEEP IN STEP WITH
		// retiredRule", which enforced nothing: a pattern needing a word
		// that is not here is skipped for most of the tree, and the
		// negative assertion still passes — a guard switched off by
		// adding to it. That happened immediately: the imperative
		// patterns added in this same review needed "bottom" and
		// "end of", and the new check caught it before they shipped.
		low := strings.ToLower(string(body))
		if !containsAny(low, prefilter) {
			return nil, nil
		}
		lines := strings.Split(string(body), "\n")
		if declaresItselfSuperseded(lines) {
			return nil, nil
		}
		// ONE REPORT PER STATEMENT, and the first version had none.
		//
		// A sentence is found TWICE whenever the line above it does not
		// state the rule on its own: once at that line through the join,
		// and once at the sentence's own line. The first report then
		// names whatever the line above happens to be — a blank line, in
		// the case that found this — so a single stale sentence read as
		// two violations, one of them at a location with nothing on it.
		//
		// Found by TestTheScanItselfReportsAFixtureFile, on the first
		// run of the first fixture ever pointed at this scan, which is
		// the argument for that test rather than a remark about this
		// one. Raised in review of #458.
		reported := map[int]bool{}
		for i, line := range lines {
			// The line AND the line joined to its successor. A rule
			// statement wrapped across two comment lines —
			//
			//	// The AdornmentLayer must be declared
			//	// LAST or it paints underneath.
			//
			// — matches no pattern on either line alone, and every
			// pattern in retiredRule is a phrase of five to nine words
			// that a 72-column comment splits routinely. The file
			// recorded a one-line qualifier FLOOR and said nothing about
			// this, so the guard's own account of its limits was missing
			// the larger one. Raised in review of #458.
			//
			// The join is what is TESTED, not the report: the error still
			// names line i, because that is where a reader starts fixing
			// it.
			hit, span := line, i
			if !states(hit) {
				hit = joinWrapped(lines, i)
				if !states(hit) {
					continue
				}
				// The statement OCCUPIES two lines, so the window runs
				// two lines past the second — not two past the first.
				// Without this a wrapped hit silently loses the last
				// line of its own window, and apps/wysiwyg/wysiwyg.gooey
				// proved it immediately: a blank line joined to "THE MENU
				// BAR IS THE LAST CHILD" reported at the blank line, two
				// lines above the "gooey.Overlay, lifted" that qualifies
				// it, while the same sentence matched cleanly one line
				// down. A guard that reports a corrected site because of
				// where its own lookahead started is noise.
				//
				// EXTENDING, NOT MOVING, and the first version moved it.
				// It passed `span` alone and the window was computed
				// either side of that, so a wrapped hit ran [i-1, i+3]
				// — it bought the line below and paid for it with the
				// line ABOVE, which is where a correction most often
				// sits ("…is what this used to say" lands before the
				// quote as readily as after it). Both ends of the
				// statement are passed now. Raised in review of #458.
				span = i + 1
			}
			if qualifiedIn(lines, i, span, quals) {
				continue
			}
			if reported[span] {
				continue
			}
			reported[span] = true
			// ANCHORED ON THE LINE THAT SAYS SOMETHING. When the join is
			// what matched and the first line is blank or bare comment
			// marker, pointing a reader at it is pointing them at
			// nothing.
			at := i
			if strings.TrimSpace(continuationRe.ReplaceAllString(lines[i], "")) == "" {
				at = span
			}
			problems = append(problems, fmt.Sprintf(
				"%s:%d states a retired z-order rule with nothing "+
					"nearby to qualify it:\n\t%s\n%s",
				f, at+1, strings.TrimSpace(lines[at]), advice))
		}
	}
	return problems, nil
}

// prefilterWords is what TestNoFileTeachesTheRetiredOverlayRule requires a
// file to contain before it scans it line by line. It is an inexact, cheap
// filter over ~7MB, and it is the reason the guard runs in ~3s rather than
// ~6s.
//
// It is NOT in docFiles, which only walks and floors — the two sentences
// above said "docFiles" until review of #458 noticed, in a file whose
// whole thesis is that a description outlives its subject.
//
// It is a NAMED LIST because the relationship to retiredRule is a
// contract, not a coincidence: a pattern that needs a word absent here can
// never fire on most of the tree.
// TestTheRetiredRuleGuardCanActuallyFire asserts every sample survives
// this filter, so the two cannot drift apart silently.
var prefilterWords = []string{"order", "last", "bottom", "end of"}

func containsAnyPrefilterWord(low string) bool { return containsAny(low, prefilterWords) }

func containsAny(low string, words []string) bool {
	for _, w := range words {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

// inputPrefilterWords is the same contract as prefilterWords, for the
// input plane, and TestTheRetiredInputRuleGuardCanActuallyFire holds it
// to the same check: every sample retiredInputRule is pinned against has
// to survive this filter, or the pattern that catches it can never run.
//
// "marker" is in the list because of ONE sentence — component.go's
// "knows nothing about this marker" — which contains neither "hit" nor
// "input" nor "sibling". That is the whole reason the contract is
// checked rather than asserted: the word was missing from the first
// draft of this list and the guard would have skipped the single most
// important file in the finding.
var inputPrefilterWords = []string{"hit", "input", "sibling", "marker", "press", "click", "routing"}

// retiredRule matches a line ASSERTING the hosting rule. Both halves have
// to be there: "z-order" alone is fine (the forward pass, the restore
// sweep and overlapping Canvas children all legitimately talk about it),
// and "last child" alone is fine (a VStack's last child gets the
// remainder; an ItemsView highlight is the row's last child). It is the
// two together, or the bare equation, that only ever meant the thing that
// stopped being true.
var retiredRule = []*regexp.Regexp{
	// The equation itself, either direction. There is no standalone
	// `document order is z-order` entry: the (tree|document) alternation
	// below matches everything it did and one thing more, so keeping both
	// meant a pattern that could never be the reason a line was caught.
	// Removed in review of #458.
	regexp.MustCompile(`(?i)z-?order is document order`),
	regexp.MustCompile(`(?i)(tree|document) order IS z-?order`),
	// The instruction, in the spellings the repo actually used.
	// ONE INFLECTION-TOLERANT PATTERN replaces the pair that needed a
	// literal `declare ` and an adjacent `declared LAST`. Both missed
	// `declared very last` — an ADVERB between the verb and the word —
	// which is how cmd/toolkit/toolkit.gooey kept the rule through a
	// sweep that corrected three other sites in the same file. A
	// predicate assembled from the phrasings you can see is a sample of
	// the ways the thing can be said, and this is the third time that
	// has been the finding. Raised in review of #458.
	regexp.MustCompile(`(?i)declar(e|es|ed|ing)\b.{0,40}\bLAST\b`),
	regexp.MustCompile(`(?i)(as|is) the LAST child`),
	regexp.MustCompile(`(?i)last child of the (root|Grid|page)`),
	regexp.MustCompile(`(?i)last child = top`),
	// WITHOUT the word "child". components/menu_test.go said
	// `bar, // last = on top` twenty-two lines below a header this sweep
	// corrected to say "nothing in this file is asserting z-order".
	// Raised in review of #458.
	regexp.MustCompile(`(?i)last = (on )?top`),
	regexp.MustCompile(`(?i)last-in-document-order`),
	// THE REPO'S OWN PARAPHRASES, added in review of #458 — and the
	// reason they matter is that the sweep missed a site because of
	// them. components/menu_live_test.go said "late in document order:
	// the dropdown paints above the content" ONE LINE ABOVE a line this
	// same change corrected: the flagship instance of the thesis, in a
	// file the sweep had open, invisible because no pattern matched the
	// phrasing. The issue's site table was a sample and so was the first
	// draft of this list.
	regexp.MustCompile(`(?i)(late|last|later) in (the )?document order`),
	regexp.MustCompile(`(?i)document order is paint order`),
	// IMPERATIVE PHRASINGS. No live site matches these today — which is
	// the argument for adding them NOW rather than after the next one
	// lands. Every pattern above was drafted from a phrasing that already
	// existed, which is exactly why the list missed
	// components/menu_live_test.go twice: a predicate assembled from the
	// sites you can see is a sample of the ways the thing can be said.
	// Raised in review of #458.
	regexp.MustCompile(`(?i)must be the last child`),
	regexp.MustCompile(`(?i)belongs at the (end|bottom) of (its|the) (container|markup|page|document)`),
	regexp.MustCompile(`(?i)put it at the (end|bottom) of the (markup|page|document|container)`),
	// "THE ORDER IS THE Z-ORDER", without naming tree or document. Found
	// live in apps/wysiwyg/components/preview/preview.go, where it is
	// TRUE — preview.Overlay is deliberately not a gooey.Overlay — which
	// is precisely why it needed the pattern: the sentence is correct
	// there and would be the retired rule anywhere else, so it has to be
	// caught and then qualified rather than left unmatched. Raised in
	// review of #458.
	regexp.MustCompile(`(?i)the order is the z-?order`),
}

// joinWrapped is line i and line i+1 as one string, with the comment or
// list marker that opens the continuation stripped so the two halves meet
// as prose. `// LAST or it paints underneath.` has to become
// `LAST or it paints underneath.` or the join reads
// `must be declared // LAST` and a pattern with a `.{0,40}` gap survives
// only by luck.
//
// One line of lookahead, not a paragraph: the patterns are single
// phrases, and joining more would let a rule statement on line i be
// completed by an unrelated sentence three lines down. Raised in review
// of #458.
func joinWrapped(lines []string, i int) string {
	if i+1 >= len(lines) {
		return lines[i]
	}
	return lines[i] + " " + continuationRe.ReplaceAllString(lines[i+1], "")
}

// The markers a wrapped line can open with: a Go/`.gooey` comment, a
// markdown heading, list bullet, or blockquote.
//
// The heading form REQUIRES the space. `#+` alone also ate the `#430` in
// "#430 specifically disproved", which is a qualifier — so stripping the
// marker deleted the evidence that the sentence beside it was an epitaph,
// and docs/specs/2026-09-05-menu-item-icons.md was reported as teaching
// the rule it says was disproved. Found by running the guard, not by
// reading the regexp. Raised in review of #458.
var continuationRe = regexp.MustCompile(`^\s*(//+|#{1,6}\s|>+|[-*+]\s)\s*`)

func statesTheRetiredRule(line string) bool { return matchesAny(line, retiredRule) }

func statesTheRetiredInputRule(line string) bool { return matchesAny(line, retiredInputRule) }

func matchesAny(line string, res []*regexp.Regexp) bool {
	for _, re := range res {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// retiredInputRule matches a line asserting that HIT-TESTING answers by
// document order — the claim #465 retired, and the one this file had no
// pattern for until #478.
//
// The asymmetry with retiredRule is deliberate and worth naming. The
// paint rule was an INSTRUCTION ("declare it last"), so its patterns
// hunt imperatives. The input rule was a CAVEAT — every corrected paint
// site added a sentence explaining that the freedom it had just granted
// applied to paint only — so its patterns hunt the shapes a caveat
// takes: what the walk does ("walks document order", "prefers later
// siblings"), what it is not ("not lifted"), what it does not know
// ("knows nothing about this marker"), and what the marker therefore
// buys ("moves paint, not input", "responsible for its own routing").
//
// That is why the four surviving sites were invisible: none of them
// instructs anybody to declare anything. Raised in review of #478.
var retiredInputRule = []*regexp.Regexp{
	// What the walk was said to do.
	regexp.MustCompile(`(?i)hit-?test(ing|s)?\b.{0,60}?\bwalks? (the )?(plain |ordinary )?(document|tree) order`),
	regexp.MustCompile(`(?i)hit-?test(ing|s)?\b.{0,60}?\b(prefers|takes|favou?rs) (the )?(later|last) sibling`),
	regexp.MustCompile(`(?i)(later|last) sibling still (takes|wins|gets) (a|the) (press|click)`),
	regexp.MustCompile(`(?i)hit-?test(ing|s)?\b.{0,40}?\blast sibling first`),
	// What it was said not to be.
	regexp.MustCompile(`(?i)(hit-?test(ing)?|input|clicks?|presses?|the hit walk)\b.{0,40}?\b(is|are|was|were) not lifted`),
	regexp.MustCompile(`(?i)input (was|is) not lifted`),
	// What it was said not to know.
	regexp.MustCompile(`(?i)knows nothing about (this|the) marker`),
	regexp.MustCompile(`(?i)knows nothing about (the )?(overlay )?ranks?`),
	regexp.MustCompile(`(?i)hit-?test(ing)? ignores the overlay layer`),
	// What the marker was therefore said to buy — and not buy.
	//
	// There is no separate `moves paint,? not input` entry: the pattern
	// below matches everything it did and more, so keeping both meant one
	// that could never be the reason a line was caught. Deleting it was
	// SILENT under mutation — which is what a subsumed pattern always is,
	// and why the per-pattern loop in the fire test cannot see the
	// difference between subsumed and working. Found in review of #478,
	// the same finding retiredRule's `document order is z-?order` entry
	// produced in review of #458.
	regexp.MustCompile(`(?i)paint,? not (the )?(input|clicks?|presses?)\b`),
	regexp.MustCompile(`(?i)responsible for its own routing`),
	// IMPERATIVE FORMS, none of which is live today. Added now for the
	// reason retiredRule's imperatives were: a list drafted only from the
	// sentences you can see is a sample of the ways the thing can be
	// said, and this file has been caught by that three times.
	regexp.MustCompile(`(?i)declar(e|es|ed|ing)\b.{0,40}\blast\b.{0,40}\bto (be hit|take the (press|click))`),
	regexp.MustCompile(`(?i)to (be hit|get the (press|click)) first,? declare it last`),
}

// qualifiers are the phrases that make a statement of the old rule
// acceptable. Two families, and the distinction is the point:
//
//   - the CORRECTION — the sentence is stating the current rule, which
//     always involves the second layer, a rank, or the marker;
//   - the EPITAPH — the sentence is quoting the old rule in order to say
//     it is dead, which every good comment about this does.
//
// A THIRD FAMILY USED TO BE ADMITTED and is gone: naming the hit walk
// earned a line an exemption, on the ground that hit-testing genuinely
// answered by document order. #465 made FocusManager.HitTest ask
// overlayOf, so that premise died and the exemption went with it — see
// TestNamingTheHitWalkNoLongerExemptsALine thirty lines below, and
// retiredInputRule, which now catches the claim the exemption used to
// wave through.
//
// This paragraph described the deleted family as live for a whole review
// round after the slice stopped carrying it: the header outliving the
// function, in the file whose thesis is that a description outlives its
// subject. Corrected in review of #478.
// Compiled ONCE, like retiredRule above — and a plain slice literal, like
// retiredRule. This was a function returning a fresh slice, recompiling
// nine regexps on every call; the recompile was fixed by hoisting, and the
// closure it was extracted from lingered for a release. Raised in review
// of #458.
// THE TWO HALVES ARE SPLIT because the input guard can share exactly
// one of them, and finding out which was the interesting part of #478.
//
// The EPITAPH half is plane-agnostic: "used to say", "no longer",
// "superseded" bury whichever rule the sentence beside them states, and
// a corrected site reads the same on either plane.
//
// The CORRECTION half is NOT, and sharing it whole would have made the
// input guard useless in the one place it was most needed. Two of these
// patterns exempt the very sentence they should catch:
//
//   - `\blifted\b` matches "hit-testing, WHICH IS NOT LIFTED" — the
//     retired input claim qualifies itself, on its own line, every time
//     it is written;
//   - `#4(37|38|39)` marks a PAINT correction, and this sweep's house
//     style is to state the paint rule and the input divergence in one
//     comment — so the citation two lines up waved through the caveat
//     beside it. docs/learn/07-app-chrome.md:91 was live, in the tree,
//     and invisible to the first draft of the input guard for both
//     reasons at once.
//
// So the input guard gets inputCorrectionRes: the things that can only
// be said by someone describing the CURRENT walk.
var qualifierRes = append(append([]*regexp.Regexp{}, paintCorrectionRes...), epitaphRes...)

var paintCorrectionRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)two layers|second (paint )?layer|overlay layer`),
	regexp.MustCompile(`(?i)gooey\.Overlay|OverlaysPage|OverlayRank|\bis a gooey\.Overlay\b`),
	regexp.MustCompile(`(?i)\blifted\b|\blifts\b`),
	// RANK, NARROWLY. This was a bare `\brank(s|ed)?\b`, and the bare word
	// appears in ordinary prose about ordering all over the repo — so it
	// was not evidence that a nearby sentence had been corrected, it was
	// a mask. It hid a live stale site in overlayrank_test.go: the word
	// "higher-ranked" one line above "the framework tells it to declare
	// the MenuBar last" was enough to qualify the retired rule as
	// current, inside the file that most explains the new one. The forms
	// below carry the meaning the bare word does not.
	// Raised in review of #458.
	regexp.MustCompile(`(?i)OverlayRank|overlay rank|rank(s|ed)? (it|them|the layer|above|over|beats|higher|lower)|by rank|rank order|equal ranks?`),
	// #465 alongside the paint issues: a sentence correcting either
	// plane cites it, and the input guard shares this list.
	regexp.MustCompile(`#4(37|38|39|65)|#430`),
}

// inputCorrectionRes is the correction half for the INPUT plane, and
// every entry names something only the post-#465 walk does. None of them
// can be written by a sentence stating the retired rule, which is the
// property paintCorrectionRes turned out not to have.
var inputCorrectionRes = []*regexp.Regexp{
	regexp.MustCompile(`#465`),
	regexp.MustCompile(`(?i)overlayOf|asks the same (rule|question|two questions|membership)`),
	regexp.MustCompile(`(?i)hit-?test(ing)?\b.{0,40}?\b(asks|is lifted|is layer-aware|is rank-aware)`),
	regexp.MustCompile(`(?i)(input|the click|the press) (agrees|asks the same|is lifted too)`),
	regexp.MustCompile(`(?i)membership-and-rank`),
}

var epitaphRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)used to (say|state|be)|is what this said|is what this used to`),
	regexp.MustCompile(`(?i)superseded|no longer|stopped being|was never|not any more|retired`),
	regexp.MustCompile(`(?i)by convention|convention,? not|incidental|heuristic|arbitrary`),
	regexp.MustCompile(`(?i)do not go looking|does not decide|decides nothing|position is free`),
}

var inputQualifierRes = append(append([]*regexp.Regexp{}, inputCorrectionRes...), epitaphRes...)

// THE HIT-TEST EXEMPTION IS GONE, and its removal is part of #465
// rather than a tidy-up.
//
// It read `(?i)hit-?test|hit order|HitTest` and was applied to the
// matching line alone. The justification was explicit and, at the time,
// true: "hit-testing genuinely still walks document order, so a line
// about the hit walk may say 'last' and mean it — but it has to name the
// walk to earn that." #465 made FocusManager.HitTest ask overlayOf, the
// same membership-and-rank rule paint derives its order from, so
// position no longer decides anything the hit walk answers on its own.
//
// An exemption outliving its premise is worse than no exemption, because
// it is a hole shaped like the exact sentence that is now wrong: any
// line saying "later siblings win the click" would have named the walk
// and been waved through, in the one file the reader most needs to be
// right. This guard exists because a description outlives its subject;
// it does not get to keep one of its own.

// declaresItselfSuperseded exempts a whole file whose HEAD says the rule
// below it is dead. That is for the dated decision records: a spec is a
// record of what was decided on its date and rewriting its body would
// falsify the history it exists to keep, so the honest repair is a banner
// at the top — which the per-line window cannot see from thirty lines
// down.
//
// Scoped to the head deliberately. A "superseded" note buried in the
// middle of a long document does not reach a reader who lands on a
// section, so it does not buy the exemption either.
func declaresItselfSuperseded(lines []string) bool {
	head := 20
	if len(lines) < head {
		head = len(lines)
	}
	h := strings.Join(lines[:head], "\n")
	return supersededRe.MatchString(h) && supersededOf.MatchString(h)
}

// BOTH HALVES REQUIRED. Keying on the bare word "superseded" exempted a
// file's ENTIRE body on the strength of a banner that might be about
// anything — an allow-list by accident, which is the thing this file's
// own comments argue against. Only the three intended specs match today,
// so this was latent rather than live, but the floor mattered: the
// exemption has to be about THIS rule. All three banners name the
// overlay layer already, so requiring it costs nothing. Raised in review
// of #458.
var (
	supersededRe = regexp.MustCompile(`(?i)superseded`)
	supersededOf = regexp.MustCompile(`(?i)overlay|z-?order`)
)

// qualifiedNear looks in a NARROW window around the hit — two lines each
// way, enough for one wrapped sentence and no more.
//
// THE FLOOR IS ONE LINE, AND THAT IS THE RESIDUAL HOLE — the residual
// one. The larger hole beside it, a rule statement WRAPPED across two
// comment lines matching neither, went unrecorded here until review of
// #458 and is now closed by joinWrapped rather than written down. What
// follows is the part that is still open.
//
// The window
// includes line i itself, so a single line carrying both the retired rule
// and a qualifier passes:
//
//	// A gooey.Overlay must be declared LAST.
//
// — the retired instruction and the marker name, in one sentence, which
// is the two-contradictory-rules-in-one-place shape the narrowing from
// six lines to two was done to kill, merely compressed further.
//
// It cannot be closed by excluding line i: several CORRECTED sites
// legitimately qualify themselves on their own line, docs/markup-reference.md
// among them, where the epitaph and the #430 citation sit in one sentence.
// Excluding i would make the guard reject the very phrasing the sweep
// produced.
//
// So the floor is recorded rather than fixed, next to the six-line
// finding, so the next person narrowing this knows where it is. Raised in
// review of #458.
//
// The window was six lines first, and mutation testing killed that
// version: a stale sentence re-introduced into components/menu.go and
// into cmd/browser/browser.gooey both passed, because each landed within
// six lines of prose correctly explaining the new rule. That is not a
// hypothetical — it is the EXACT shape of the bug this guard is for.
// #443 names components/popup.go as the costliest site precisely because
// it said "LAST, because document order is z-order" eight lines above a
// type that implements OverlaysPage; the file documented two
// contradictory rules and looked well-maintained doing it.
//
// So a file that explains the current rule must not thereby become a
// safe harbor for the old one. Two lines is the width of the epitaph
// every corrected site in this repo actually carries — "…is what this
// used to say" lands on the same line or the next one — and it is too
// narrow to reach a neighbouring paragraph that happens to be right.
func qualifiedNear(lines []string, i int) bool {
	return qualifiedNearSpan(lines, i, i)
}

// qualifiedNearSpan WAS qualifiedNear with the hit-test exemption read
// against the span the match came from rather than line i, and the split
// existed only to serve that exemption: a wrapped statement could name
// the hit walk on line i+1, and refusing there would have rejected the
// sentence the exemption was for.
//
// #465 deleted the exemption, so the `hit string` parameter is gone with
// it and the two functions no longer differ in what they READ. What is
// left of the split is the span, and that outlived the exemption for a
// reason of its own: a statement can occupy two lines, and the ±2 window
// belongs either side of the WHOLE of it. Passing one index and
// computing the window around it SLIDES it down for a wrapped hit
// instead of widening it, which silently drops the line above — where a
// correction sits at least as often as below.
//
// So the names are kept because the CALL SITES still mean different
// things: the scan passes the span it matched on, the guard's own
// fixtures pass a line.
func qualifiedNearSpan(lines []string, first, last int) bool {
	return qualifiedIn(lines, first, last, qualifierRes)
}

// qualifiedIn is the same window against a GIVEN pattern list, which is
// what the input plane needs: the two guards share the epitaphs and not
// the corrections, so a caller has to be able to say which list.
func qualifiedIn(lines []string, first, last int, res []*regexp.Regexp) bool {
	const window = 2
	lo, hi := first-window, last+window
	if lo < 0 {
		lo = 0
	}
	if hi >= len(lines) {
		hi = len(lines) - 1
	}
	// JOINED AS PROSE, not with newlines. The qualifiers are phrases —
	// "no longer", "used to say", "does not decide" — and a 72-column
	// comment splits them exactly as readily as it splits the rule
	// statements the scan above now joins for the same reason. It split
	// one in the tree: components/menu_test.go wrapped "Last is no /
	// longer what puts the dropdown above the content", so the correction
	// the sweep wrote was invisible to the guard checking for it.
	// Raised in review of #458.
	parts := make([]string, 0, hi-lo+1)
	for _, l := range lines[lo : hi+1] {
		parts = append(parts, continuationRe.ReplaceAllString(l, ""))
	}
	block := strings.Join(parts, " ")
	for _, re := range res {
		if re.MatchString(block) {
			return true
		}
	}
	return false
}

// docFiles is every file in the tree that can teach somebody the rule:
// Go source (doc comments and fixture comments — the sites nothing else
// looks for, which is the scope question #443 asked and Elan answered),
// markdown, and .gooey markup, which says it ON SCREEN in the flagship
// demo.
//
// Dot-directories are pruned at EVERY depth, not just the top: this repo
// routinely has agent worktrees under .claude/worktrees/ holding whole
// checkouts of itself, and a top-anchored filter walks into them. vendor/
// is somebody else's prose entirely.
func docFiles(t *testing.T) []string {
	t.Helper()

	var out []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == "." {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor" {
				return fs.SkipDir
			}
			return nil
		}
		switch filepath.Ext(p) {
		case ".go", ".md", ".gooey":
			// THIS FILE quotes the rule in order to test for it, and it
			// is the only exemption in here. The path is the whole
			// path, not the basename: `filepath.Base` exempts a
			// same-named file at ANY depth, so a future
			// `packs/whatever/zorderdocs_test.go` would be skipped
			// silently — an exemption that grows by itself is the one
			// shape an exemption must not have. Raised in review of
			// #458.
			if filepath.ToSlash(p) == "zorderdocs_test.go" {
				return nil
			}
			out = append(out, filepath.ToSlash(p))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	// The floor exists to catch "the walk visits nothing", and 100 was
	// far too slack for that claim: the tree holds ~788 matching files, so
	// 100 also passes with docs/, components/, apps/ and cmd/ lost
	// TOGETHER — the walk could go blind to four of the five places the
	// rule is taught and still call itself intact. 500 is the same
	// guarantee actually enforced, and still leaves room for the tree to
	// shrink by a third. Raised in review of #458.
	const floor = 500
	if len(out) < floor {
		t.Fatalf("found %d documentation files, want at least %d — the walk "+
			"is broken and every assertion below it is vacuous", len(out), floor)
	}
	return out
}

// The guard above is a negative assertion over a tree that currently
// satisfies it, which is the shape that passes for any reason at all —
// including a regex that matches nothing and a walk that visits no files.
// docFiles has its own floor; this pins the other half by handing the
// predicate the exact sentences the sweep removed and requiring each to
// be caught.
func TestTheRetiredRuleGuardCanActuallyFire(t *testing.T) {
	removed := []string{
		"//     from ChildComponents (LAST, because document order is z-order),",
		"// z-order IS document order — so declare the MenuBar as the LAST child",
		"// LAST, because document order is z-order: the menu must paint over",
		"the way an app declares it (last child = top",
		"**Declare the `MenuBar` as the LAST child of its container.**",
		"<AdornmentLayer/>   <!-- last child of the root -->",
		"the MenuBar overlay recipe reused in an app: last-in-document-order",
		// The two the FIRST version of this list did not have, which is
		// how components/menu_live_test.go kept the rule through a sweep
		// that edited the line beneath it. Added with their patterns in
		// review of #458.
		"bar,          // late in document order: the dropdown paints above the content",
		"// it. Document order is paint order, so a component that overflows",
	}
	// Sentences the repo NEVER SHIPPED, kept apart from `removed` because
	// that list carries a factual claim — these are the sentences the
	// sweep deleted — and inventing entries for it would falsify the
	// record. These exist so a pattern guarding a phrasing nobody has
	// written yet still has something to fire on, which the per-pattern
	// check below requires. `declared LAST` is here because review found
	// it matched nothing in `removed`: it was guarding a spelling the
	// repo never used, and there was no way to tell that from the list.
	neverShipped := []string{
		// MOVED FROM `removed` in review of #458, where it was the entry
		// that made the list's own claim false: `git grep "LAST child of
		// its root"` finds it in neither the base branch nor this tree,
		// and it appears on no `-` line of this PR's diff. The list
		// carries a factual claim about provenance and the neverShipped
		// rationale below says inventing entries would falsify it — so an
		// invented entry sitting in `removed` falsified it twice over.
		"// it as the LAST child of its root — document order is z-order, the same",
		// Inflections and the bare equation, so the patterns added for
		// them have something to fire on.
		"// The AdornmentLayer is declared very last, so it paints on top.",
		"// Declaring the ToastHost last is what puts the toasts above the page.",
		"	bar,          // last = on top",
		"// The AdornmentLayer must be declared LAST or it paints underneath.",
		"// The ToastHost must be the last child, or the toasts go behind the page.",
		"// A MenuBar belongs at the end of its container so the dropdown paints on top.",
		"// Put it at the bottom of the markup and it will paint over everything.",
		"// THE ORDER IS THE Z-ORDER and may not be swapped.",
	}
	for _, line := range removed {
		if !statesTheRetiredRule(line) {
			t.Errorf("the guard does not recognize a line the sweep "+
				"actually removed, so it would not have caught it:\n\t%s", line)
		}
	}
	for _, line := range neverShipped {
		if !statesTheRetiredRule(line) {
			t.Errorf("the guard does not recognize a phrasing it exists to catch:\n\t%s", line)
		}
	}
	samples := append(append([]string{}, removed...), neverShipped...)

	// And the other error: a predicate that fires on everything would
	// also pass the loop above while making the real test meaningless.
	// These are correct sentences from the same files.
	kept := []string{
		"// Z-order is document order IN TWO LAYERS. c.paint is the tree in",
		"// cell, children before ancestors and later siblings before earlier ones.",
		"a later sibling paints over an earlier one.",
		"// Adornments is what the layer is currently showing, in z-order.",
		"// It is the row's last child, so its node runs after the template's,",
	}
	// EVERY PATTERN needs a sample, not just every sample a pattern.
	//
	// The loop above pins the list as a WHOLE: it passes while an
	// individual retiredRule entry matches nothing in it, so that entry
	// could be deleted, or could never have worked, and nothing would go
	// red. Walking the other way found both kinds — `declared LAST\b` had
	// no sample containing "declared last", and `document order is
	// z-?order` was fully subsumed by the `(tree|document) order IS
	// z-?order` pattern beside it. A dead pattern in a negative
	// assertion is indistinguishable from a working one until the day it
	// was supposed to fire. Raised in review of #458.
	for _, re := range retiredRule {
		matched := false
		for _, line := range samples {
			if re.MatchString(line) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("retiredRule pattern %v matches none of the sample lines: it is "+
				"either dead or subsumed by another pattern, and deleting it would "+
				"redden nothing. Add the sentence it exists to catch.", re)
		}
	}

	// THE PREFILTER CONTRACT, checked rather than asserted in prose.
	//
	// The scan skips any file containing none of prefilterWords, and the
	// list used to carry a comment telling the next author to keep
	// retiredRule in step with it. Prose enforces nothing: a future
	// pattern for, say, "paints on top because it is declared after"
	// contains no prefilter word, is skipped for 100% of the tree, and
	// the negative assertion still passes — a guard switched off by
	// adding to it. This loop is what replaced that comment. Every sample
	// the patterns are pinned against must survive the prefilter too.
	// Raised in review of #458.
	//
	// The three sentences above described a two-word list living in
	// docFiles, and by the time they were read the list was four words
	// living in the test body. Corrected in review of #458.
	for _, line := range samples {
		if !containsAnyPrefilterWord(strings.ToLower(line)) {
			t.Errorf("the prefilter would skip a file containing this line, so the "+
				"pattern that catches it can never run:\n\t%s\n"+
				"Either add a word to prefilterWords or keep the pattern inside it.", line)
		}
	}

	for _, line := range kept {
		if statesTheRetiredRule(line) && !qualifiedNear([]string{line}, 0) {
			t.Errorf("the guard fires on a correct sentence, which makes "+
				"it noise rather than a check:\n\t%s", line)
		}
	}
}

// TestEveryStatementOfTheHitContractNamesTheAncestorClause is the prose
// half of §1, and it is derived rather than a list of the sites the
// review happened to find.
//
// The contract read "the one that paints last among those whose arranged
// bounds contain the cell" in four files. The walk prunes on bounds at
// EVERY node, so what it returns is the one that paints last among those
// whose bounds — and every ancestor's — contain the cell. A child
// arranged outside its parent's rect paints and can never be hit;
// TestAnOverlayOutsideItsParentPaintsAndIsNotHit measures it.
//
// The behaviour is pinned there. This pins the SENTENCE, because the
// sentence is what a reader acts on and prose fails open: restoring the
// unqualified form breaks nothing and misleads everyone. Any file
// stating the contract in the repo's own words has to carry the
// qualification within the same few lines.
//
// It covers the two sites that use this phrasing (mouse.go and
// docs/architecture.md) and NOT the two that paraphrase it —
// components/popup.go and components/menu.go say it in their own words,
// and a pattern loose enough to catch those would catch every sentence
// about painting order in the repo. Those two are named here so the gap
// is recorded rather than assumed closed. Raised in review of #478.
func TestEveryStatementOfTheHitContractNamesTheAncestorClause(t *testing.T) {
	claim := regexp.MustCompile(`(?i)paints? last among those whose`)
	ancestor := regexp.MustCompile(`(?i)ancestor`)
	found := 0
	for _, f := range docFiles(t) {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		// "among those whose", not "paints last among": the claim wraps
		// at 72 columns in mouse.go ("THE ONE THAT PAINTS / LAST among
		// those whose"), so a prefilter keyed on the whole phrase skips
		// the file the contract is defined in — and the guard reports
		// nothing rather than reporting a miss.
		if !strings.Contains(strings.ToLower(string(body)), "among those whose") {
			continue
		}
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			if !claim.MatchString(joinWrapped(lines, i)) {
				continue
			}
			found++
			lo, hi := i, i+4
			if hi >= len(lines) {
				hi = len(lines) - 1
			}
			if !ancestor.MatchString(strings.Join(lines[lo:hi+1], " ")) {
				t.Errorf("%s:%d states the hit-test contract without the ancestor "+
					"clause:\n\t%s\n"+
					"FocusManager.HitTest prunes on bounds at EVERY node, so a "+
					"component arranged outside its parent's rect paints and is "+
					"never hit — say \"whose bounds, and every ancestor's bounds, "+
					"contain the cell\", or change the walk and this test with it.",
					f, i+1, strings.TrimSpace(line))
			}
		}
	}
	if found == 0 {
		t.Fatal("no file states the hit-test contract in the repo's own words, so " +
			"this guard passed vacuously. Either the phrasing changed — update the " +
			"pattern — or the contract is now written down nowhere.")
	}
}

// TestTheRetiredInputRuleGuardCanActuallyFire is the non-vacuity arm for
// the input plane, held to the same three checks as its paint twin: every
// sample is recognised, every PATTERN has a sample, every sample survives
// the prefilter, and correct sentences do not fire.
//
// The third check is not ceremony, and the samples had to be chosen for
// it rather than merely passing it. "marker" was in inputPrefilterWords
// with a rationale saying a sample needed it — and dropping the word was
// measured SILENT, because the one sentence naming the marker also says
// "sibling". A justification written before it was measured; the sample
// below is now a line that carries ONLY "marker", so the word is
// load-bearing and the claim is true. Same for "routing", which one
// sample carries alone.
//
// A guard switched off by adding to it is exactly the defect
// prefilterWords' own contract loop was written for.
func TestTheRetiredInputRuleGuardCanActuallyFire(t *testing.T) {
	// Sentences this change actually removed. The list carries a factual
	// claim, so nothing invented goes in it.
	removed := []string{
		"// IT MOVES PAINT, NOT INPUT. FocusManager.HitTest walks document order",
		"// and knows nothing about this marker, so a later sibling still takes a",
		"// So an overlay that does NOT take capture is responsible for its own routing.",
		"// pointer. Hit-testing prefers later siblings (they paint on top),",
		"still load-bearing is **hit-testing**, which is not lifted — so an",
		"**`Overlay` moves paint, not input.** Hit-testing still walks plain document order, last sibling first.",
		"3. **Input was not lifted.** Hit-testing still walks plain document",
		"`FocusManager.HitTest` walks document order and knows nothing about ranks either.",
		"- **`Overlay` still moves paint, not input.** Neither path consults it",
	}
	// Phrasings the repo never shipped, kept apart for the reason the
	// paint twin keeps its own: inventing entries for `removed` would
	// falsify the provenance claim that list makes. These exist so the
	// patterns guarding a sentence nobody has written yet still have
	// something to fire on — which is the argument for adding imperative
	// forms NOW, given that every pattern above was drafted from a
	// phrasing that already existed.
	neverShipped := []string{
		"// Hit-testing ignores the overlay layer, so position still decides.",
		"// To be hit first, declare it last.",
		"// Declare the ToastHost last to take the press.",
		"// The later sibling still wins the click.",
		"// Clicks are not lifted.",
		// CARRIES ONLY "marker" among the prefilter words, deliberately:
		// it is what makes that entry load-bearing rather than decorative.
		"// It knows nothing about the marker.",
		"// HitTest knows nothing about ranks.",
		"// hit-testing takes the last sibling first, whatever paints on top.",
		"// The marker moves paint, not the clicks.",
	}
	for _, line := range removed {
		if !statesTheRetiredInputRule(line) {
			t.Errorf("the guard does not recognize a line this change actually "+
				"removed, so it would not have caught it:\n\t%s", line)
		}
	}
	for _, line := range neverShipped {
		if !statesTheRetiredInputRule(line) {
			t.Errorf("the guard does not recognize a phrasing it exists to catch:\n\t%s", line)
		}
	}
	samples := append(append([]string{}, removed...), neverShipped...)

	for _, re := range retiredInputRule {
		matched := false
		for _, line := range samples {
			if re.MatchString(line) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("retiredInputRule pattern %v matches none of the sample lines: "+
				"it is either dead or subsumed by another pattern, and deleting it "+
				"would redden nothing. Add the sentence it exists to catch.", re)
		}
	}

	for _, line := range samples {
		if !containsAny(strings.ToLower(line), inputPrefilterWords) {
			t.Errorf("the prefilter would skip a file containing this line, so the "+
				"pattern that catches it can never run:\n\t%s\n"+
				"Either add a word to inputPrefilterWords or keep the pattern "+
				"inside it.", line)
		}
	}

	// Correct sentences about the CURRENT walk. A predicate that fired on
	// these would pass every loop above while making the real guard noise.
	kept := []string{
		"// FocusManager.HitTest asks overlayOf, so the hit walk is lifted and ranked.",
		"// a later sibling paints over an earlier one.",
		"// HitTest returns the component that paints last among those whose bounds contain the cell.",
		"// Popup holds pointer capture while open, so presses never reach the walk.",
	}
	for _, line := range kept {
		if statesTheRetiredInputRule(line) && !qualifiedIn([]string{line}, 0, 0, inputQualifierRes) {
			t.Errorf("the guard fires on a correct sentence, which makes it noise "+
				"rather than a check:\n\t%s", line)
		}
	}
}

// TestThePaintCorrectionDoesNotExemptTheInputClaim is the finding that
// made the two qualifier lists different, and it is measured rather than
// argued.
//
// This sweep's house style states the paint rule and the input caveat in
// one comment, so a "#439" or a "lifted" two lines up sits beside every
// input claim in the repo. Sharing qualifierRes whole therefore exempted
// the claim from its own neighbour's correction — and worse, a sentence
// saying "hit-testing is NOT LIFTED" matched the `\blifted\b` qualifier
// on its own line, qualifying itself every time it was written.
//
// docs/learn/07-app-chrome.md:91 was live in the tree and invisible to
// the first draft of the input guard for both reasons at once. Raised in
// review of #478.
func TestThePaintCorrectionDoesNotExemptTheInputClaim(t *testing.T) {
	// The shape from 07-app-chrome.md: a paint correction, then the
	// input caveat beneath it.
	beside := []string{
		"position stopped deciding paint in #437 and stopped deciding order",
		"among overlays in #439. Where it is still load-bearing is",
		"hit-testing, which is not lifted — so an overlay that wants presses",
		"still cares.",
	}
	if !statesTheRetiredInputRule(joinWrapped(beside, 2)) {
		t.Fatal("the fixture is not caught by retiredInputRule, so this test proves nothing")
	}
	if qualifiedIn(beside, 2, 2, inputQualifierRes) {
		t.Error("a retired INPUT claim was waved through by a PAINT correction beside " +
			"it (#437/#439) or by the word 'lifted' in its own negation. Neither says " +
			"anything about the hit walk, and this sweep writes the two rules in one " +
			"comment as a matter of style — so sharing the correction half makes the " +
			"input guard blind exactly where it is needed.")
	}
	// AND THE CORRECTED FORM STILL PASSES, so the guard asks for a
	// rewrite that can be written.
	corrected := []string{
		"position stopped deciding paint in #437 and stopped deciding",
		"hit-testing in #465, where the walk was made to ask the same",
		"membership-and-rank question the paint order is derived from.",
		"It used to say hit-testing is not lifted.",
	}
	if !qualifiedIn(corrected, 3, 3, inputQualifierRes) {
		t.Error("the corrected sentence is still reported, so every comment about " +
			"this walk becomes unwritable")
	}
	// The PAINT guard must keep accepting what it always accepted: the
	// split must not have narrowed it.
	paint := []string{
		"// The MenuBar is declared LAST here, which decides nothing:",
		"// it is a gooey.Overlay and is lifted into the overlay layer (#437).",
	}
	if !qualifiedIn(paint, 0, 0, qualifierRes) {
		t.Error("splitting qualifierRes narrowed the PAINT guard, which was not the " +
			"point: a lift-and-#437 correction still has to exempt a paint-rule line")
	}
}

// TestAWrappedRuleStatementIsStillCaught pins the two-line join, and
// nothing else in the file can: every sample in
// TestTheRetiredRuleGuardCanActuallyFire is a single line, so deleting
// joinWrapped leaves that test green and the tree green, and the hole
// comes back silently.
//
// The hole is not exotic. Every retiredRule pattern is a phrase of five
// to nine words, and this repo wraps comments at 72 columns, so the
// statement the guard is looking for is split about as often as not.
// Raised in review of #458.
func TestAWrappedRuleStatementIsStillCaught(t *testing.T) {
	wrapped := []string{
		"// The AdornmentLayer must be declared",
		"// LAST or it paints underneath.",
	}
	if statesTheRetiredRule(wrapped[0]) || statesTheRetiredRule(wrapped[1]) {
		t.Fatal("the fixture is not actually wrapped — one of its lines states " +
			"the rule on its own, so this test would pass without the join")
	}
	if !statesTheRetiredRule(joinWrapped(wrapped, 0)) {
		t.Errorf("a rule statement wrapped across two comment lines is invisible "+
			"to the guard:\n\t%s\n\t%s", wrapped[0], wrapped[1])
	}

	// The continuation marker has to go, or the join reads
	// "declared // LAST" and only survives on the slack in a `.{0,40}`.
	if got := joinWrapped(wrapped, 0); strings.Contains(got, "declared //") {
		t.Errorf("joinWrapped left the continuation marker in: %q", got)
	}

	// And the last line of a file has no successor.
	if got := joinWrapped(wrapped, 1); got != wrapped[1] {
		t.Errorf("joinWrapped past the end = %q, want the line itself", got)
	}
}

// TestAWrappedQualifierStillExempts is the other half, and without it the
// join above is a guard that reports corrected prose.
//
// A qualifier is a phrase too — "no longer", "used to say" — and wraps
// the same way. components/menu_test.go had "Last is no / longer what
// puts the dropdown above the content" the moment the join started
// catching the sentence above it, so this is a fixture taken from the
// tree, not an invention. Raised in review of #458.
func TestAWrappedQualifierStillExempts(t *testing.T) {
	lines := []string{
		"// focusable button on row 2, and the MenuBar declared last. Last is no",
		"// longer what puts the dropdown above the content — the surface is a",
	}
	if !statesTheRetiredRule(lines[0]) {
		t.Fatal("the fixture no longer states the rule, so the exemption it " +
			"checks is unreachable")
	}
	if !qualifiedNear(lines, 0) {
		t.Error("a correction wrapped across two lines does not qualify the " +
			"statement it corrects, so the guard fires on the sweep's own prose")
	}
}

// TestNoDocCallsTheRankPassAStableSort is the guard the round-12 review
// left implicit, and the finding it comes from is worth stating in full
// because it is a claim about a MECHANISM rather than a behaviour.
//
// `appendByRank` walks the lifted nodes once and appends each into its
// rank's bucket. There is no comparator and no call into `sort`. Equal
// ranks therefore keep document order STRUCTURALLY — by never being
// reordered — and "the sort is stable" describes a different program:
// one whose correctness rests on the standard library's guarantee, which
// no mutation of this repo could falsify. docs/architecture.md draws
// that distinction at length; docs/learn/concepts/overlays.md contradicted
// it in the same change.
//
// THE MECHANISM HALF IS ASSERTED FIRST, so the prose rule cannot outlive
// its subject: if appendByRank ever really does sort, this test says so
// and the docs it polices become correct rather than wrong.
func TestNoDocCallsTheRankPassAStableSort(t *testing.T) {
	src, err := os.ReadFile("composer.go")
	if err != nil {
		t.Fatalf("reading composer.go: %v", err)
	}
	fn := appendByRankBody(t, string(src))
	for _, bad := range []string{"sort.", "slices.Sort", "slices.SortStable"} {
		if strings.Contains(fn, bad) {
			t.Fatalf("appendByRank now calls %s, so it really is a sort and the "+
				"prose rule below is guarding a claim that stopped being wrong. "+
				"Delete this test with the paragraphs it polices", bad)
		}
	}

	claim := regexp.MustCompile(`(?i)stable sort|sort is \*{0,2}stable`)
	// SCOPED TO THE RANK PASS BY A POSITIVE REQUIREMENT, not by an
	// exemption — the window must NAME the thing this rule is about.
	// Without it the guard reported apps/wysiwyg/browser.go, which says
	// "the sort is STABLE so an empty query leaves the list in its
	// scanned order" about an entirely different and entirely correct
	// sort. A guard that fires on somebody else's correct prose is noise
	// twice over: wrong, and about a file its author will not recognise.
	subject := regexp.MustCompile(`(?i)appendByRank|overlay|OverlayRank|\branks?\b`)
	// The epitaph: a sentence may say "stable sort" in order to say it is
	// NOT one. Narrow on purpose — "not a", "rather than a", "was a
	// claim" — because a loose exemption here would admit the very
	// sentence this exists to catch.
	buried := regexp.MustCompile(`(?i)not a stable sort|rather than a stable sort|` +
		`no sort|never a stable sort|"stable" was a claim`)

	var problems []string
	for _, f := range docFiles(t) {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if !strings.Contains(strings.ToLower(string(body)), "stable") {
			continue
		}
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			if !claim.MatchString(line) {
				continue
			}
			lo, hi := i-2, i+2
			if lo < 0 {
				lo = 0
			}
			if hi >= len(lines) {
				hi = len(lines) - 1
			}
			window := strings.Join(lines[lo:hi+1], " ")
			if !subject.MatchString(window) {
				continue
			}
			if buried.MatchString(window) {
				continue
			}
			problems = append(problems, fmt.Sprintf(
				"%s:%d calls the overlay rank pass a stable sort:\n\t%s\n"+
					"appendByRank is a bucket pass — it appends into a bucket "+
					"per rank and never reorders — so equal ranks keep document "+
					"order structurally rather than on a comparator's promise. "+
					"docs/architecture.md draws the distinction; say it the same "+
					"way or bury the sentence as history.", f, i+1, strings.TrimSpace(line)))
		}
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// appendByRankBody is the source of that one function, so the assertion
// above is about IT rather than about composer.go having no sort call
// anywhere — the file legitimately sorts elsewhere.
func appendByRankBody(t *testing.T, src string) string {
	t.Helper()
	const sig = "func appendByRank["
	i := strings.Index(src, sig)
	if i < 0 {
		t.Fatalf("composer.go no longer declares appendByRank, so this guard is " +
			"reading nothing. Re-anchor it or delete it with the rule")
	}
	rest := src[i:]
	// To the next top-level declaration, which is the next line starting
	// in column zero with "func " or "type ".
	if j := strings.Index(rest[1:], "\nfunc "); j >= 0 {
		rest = rest[:j+1]
	}
	return rest
}

// TestTheScanItselfReportsAFixtureFile drives retiredRuleProblems
// against a file on disk, which is the only arm in here that exercises
// the SCAN rather than the predicate.
//
// Everything else pins statesTheRetiredRule and qualifiedNear directly.
// That leaves the part between them — the prefilter, the
// superseded-banner skip, the wrap join, the window, and now the
// parallel fan-out — checked against a corpus that is expected to be
// clean, which is a guard nobody has seen fire. Emptying the whole scan
// was SILENT before this test existed.
//
// Two files, because one proves only that something happened: the first
// must be reported and the second, which says the same thing with the
// correction beside it, must not.
func TestTheScanItselfReportsAFixtureFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	bad := write("bad.md", "Some prose.\n\nDeclare the ToastHost LAST so it "+
		"paints on top.\n\nMore prose.\n")
	got, err := retiredRuleProblems(bad, statesTheRetiredRule, prefilterWords,
		qualifierRes, scanAdvice)
	if err != nil {
		t.Fatalf("scanning the fixture: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the scan reported %d problems for a file stating the rule "+
			"unqualified, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "bad.md:3") {
		t.Errorf("the report does not name the file and line the statement is "+
			"on:\n\t%s", got[0])
	}

	good := write("good.md", "Some prose.\n\nDeclare the ToastHost LAST so it "+
		"paints on top — that is what this used to say.\n\nMore prose.\n")
	got, err = retiredRuleProblems(good, statesTheRetiredRule, prefilterWords,
		qualifierRes, scanAdvice)
	if err != nil {
		t.Fatalf("scanning the fixture: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("the scan reported a statement that carries its own epitaph on "+
			"the same line: %v", got)
	}

	// THE WRAP AND ITS WINDOW, through the scan rather than through
	// qualifiedNearSpan directly — which is the difference between
	// pinning the function and pinning the CALL. Moving the window in
	// the scan (passing the second line for both ends) was measured
	// SILENT while TestAWrappedHitKeepsTheLineAboveItsWindow passed,
	// because that test calls the function itself.
	//
	// The statement wraps across two lines and the correction sits two
	// lines above the FIRST of them — the one position that separates
	// an extended window from a moved one.
	wrapped := write("wrapped.md", "The surface is lifted out of document order.\n"+
		"\n"+
		"The ToastHost must be declared\n"+
		"LAST or the toasts go behind the page.\n"+
		"\n"+
		"More prose.\n")
	got, err = retiredRuleProblems(wrapped, statesTheRetiredRule, prefilterWords,
		qualifierRes, scanAdvice)
	if err != nil {
		t.Fatalf("scanning the fixture: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("the scan reported a wrapped statement whose correction is two "+
			"lines above it: %v\n"+
			"The window is being taken either side of the statement's SECOND "+
			"line rather than either side of the whole of it, so it slid down "+
			"by one and dropped the line above", got)
	}

	// AND THE PREFILTER MUST NOT BE WHAT SAVED IT. A file carrying none
	// of prefilterWords is skipped whole, so a "clean" answer from one
	// says nothing about the patterns.
	if !containsAnyPrefilterWord("declare the toasthost last so it paints on top.") {
		t.Error("the fixture carries no prefilter word, so both arms above are " +
			"measuring the filter rather than the rule")
	}
}

// TestAWrappedHitKeepsTheLineAboveItsWindow is finding 1 of the round-12
// review, and it is the difference between EXTENDING a window and MOVING
// one.
//
// A statement that wraps occupies lines i and i+1. The scan used to hand
// qualifiedNearSpan only i+1, which then took ±2 around THAT — [i-1,
// i+3]. It bought the line below the wrap and paid for it with the line
// above, and the line above is where a correction sits at least as often
// as below it: "…is what this used to say" reads naturally before the
// quote.
//
// THE FIXTURE PUTS THE QUALIFIER AT i-2 and nowhere else, which is the
// only position that separates the two behaviours. At i-1 both windows
// reach it; at i+3 only the moved one does; at i-2 only the extended one
// does. An arm that put it anywhere else would agree with the bug.
func TestAWrappedHitKeepsTheLineAboveItsWindow(t *testing.T) {
	lines := []string{
		"// The surface is a gooey.Overlay, lifted out of document order.", // i-2
		"//",                                     // i-1
		"// The AdornmentLayer must be declared", // i
		"// LAST or it paints underneath.",       // i+1
		"//",
		"//",
	}
	const i = 2
	if statesTheRetiredRule(lines[i]) {
		t.Fatal("the fixture's first line states the rule on its own, so the " +
			"wrap is not what the scan is matching on and this arm proves nothing")
	}
	if !statesTheRetiredRule(joinWrapped(lines, i)) {
		t.Fatal("the fixture does not state the rule even joined, so there is " +
			"no hit to place a window around")
	}
	// The qualifier is at i-2 and NOWHERE else — asserted, because a
	// fixture that accidentally qualified twice would pass under either
	// rule.
	for n, l := range lines {
		if n == i-2 {
			continue
		}
		if qualifiedNear([]string{l}, 0) {
			t.Fatalf("fixture line %d also qualifies (%q), so this arm cannot "+
				"tell the extended window from the moved one", n, l)
		}
	}

	if !qualifiedNearSpan(lines, i, i+1) {
		t.Error("a statement wrapped across lines i and i+1 does not see the " +
			"qualifier at i-2. The window is being taken either side of the " +
			"SECOND line rather than either side of the whole statement, so it " +
			"slid down by one and dropped the line above — where a correction " +
			"sits as readily as below")
	}
}

// TestNamingTheHitWalkNoLongerExemptsALine is the DELETION of the one
// qualifier that read a single line instead of the ±2 window.
//
// It existed because hit-testing really did still walk document order: a
// line about the hit walk could say "last" and mean it, provided it
// NAMED the walk. #465 made the hit walk ask overlayOf the same question
// the paint does, and an exemption keyed on naming a rule that has just
// become wrong is a hole shaped exactly like the wrong claim — so it had
// to go WITH #465 rather than after it.
//
// THE HEADER OUTLIVED THE FUNCTION for two rebases, which is the exact
// failure this file exists to catch, in this file. It still opened
// "Hit-testing really does still walk document order" — the premise #465
// killed — above a function asserting the opposite, and it named
// TestTheHitTestExemptionIsLineScoped, which no longer exists. Nothing
// went red: gofmt and vet are blind to a doc comment's subject, and the
// retired-rule guard in this file scans for the DECLARE-IT-LAST rule,
// not for claims about the hit walk. Found by rebasing, twice, not by
// the suite. `go doc -u` is the instrument.
//
// What the exemption WAS: any line stating the retired rule was waved
// through if it NAMED the hit walk, applied to that line alone rather
// than to the ±2 window — because a paragraph mentioning hit-testing two
// lines away would otherwise excuse a stale PAINT claim beside it.
//
// Deleting the exemption is otherwise SILENT — measured — which is what
// this test is for. Raised in review of #458, carried out in #478.
func TestNamingTheHitWalkNoLongerExemptsALine(t *testing.T) {
	stale := "// z-order is document order, so declare the MenuBar last."
	if !statesTheRetiredRule(stale) {
		t.Fatalf("the fixture line is not caught by retiredRule, so this test proves nothing:\n\t%s", stale)
	}

	// THE SENTENCE THE EXEMPTION EXISTED FOR. It was true in August and
	// is false now, which is the whole reason the exemption had to go
	// with #465 rather than after it: an exemption keyed on naming the
	// hit walk is a hole shaped exactly like the claim that has just
	// become wrong.
	onTheLine := []string{
		"// unrelated",
		"// hit-testing walks document order, so the last child is hit first.",
		"// unrelated",
	}
	if qualifiedNear(onTheLine, 1) {
		t.Error("a line stating the retired rule was waved through because it names the " +
			"hit walk. That exemption was deleted in #465 — FocusManager.HitTest asks " +
			"overlayOf now, so position decides nothing it answers on its own, and the " +
			"sentence in this fixture is exactly the one a reader must not be left with.")
	}

	// AND THE CORRECTED FORM STILL PASSES, so the guard is asking for a
	// rewrite that can actually be written. Without this arm the test
	// above is satisfied by a guard that rejects every line in the repo.
	corrected := []string{
		"// unrelated",
		"// hit-testing used to walk document order, so the last child was hit first.",
		"// the overlay layer decides first now.",
	}
	if !qualifiedNear(corrected, 1) {
		t.Error("the corrected sentence is still reported. Deleting the hit-test " +
			"exemption must leave the epitaph and correction markers working, or every " +
			"comment about this walk becomes unwritable.")
	}
}
