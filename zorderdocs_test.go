package gooey

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
func TestNoFileTeachesTheRetiredOverlayRule(t *testing.T) {
	for _, f := range docFiles(t) {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
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
		if !containsAnyPrefilterWord(low) {
			continue
		}
		lines := strings.Split(string(body), "\n")
		if declaresItselfSuperseded(lines) {
			continue
		}
		for i, line := range lines {
			if !statesTheRetiredRule(line) {
				continue
			}
			if qualifiedNear(lines, i) {
				continue
			}
			t.Errorf("%s:%d states the retired overlay rule with nothing "+
				"nearby to qualify it:\n\t%s\n"+
				"Overlays are lifted out of document order into a paint "+
				"layer (#437) and ranked within it (#439), so declaring "+
				"one last decides nothing. Either state the current rule, "+
				"or mark the sentence as history — the markers this test "+
				"accepts are in qualifierRes. Hit-testing is the one thing "+
				"position still orders; say so explicitly if that is what "+
				"you mean.", f, i+1, strings.TrimSpace(line))
		}
	}
}

// prefilterWords is what docFiles requires a file to contain before it is
// scanned line by line. It is an inexact, cheap filter over ~7MB, and it
// is the reason the guard runs in ~3s rather than ~6s.
//
// It is a NAMED LIST because the relationship to retiredRule is a
// contract, not a coincidence: a pattern that needs a word absent here can
// never fire on most of the tree.
// TestTheRetiredRuleGuardCanActuallyFire asserts every sample survives
// this filter, so the two cannot drift apart silently.
var prefilterWords = []string{"order", "last", "bottom", "end of"}

func containsAnyPrefilterWord(low string) bool {
	for _, w := range prefilterWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

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
	regexp.MustCompile(`(?i)declare .{0,40}\bLAST\b`),
	regexp.MustCompile(`(?i)declared LAST\b`),
	regexp.MustCompile(`(?i)(as|is) the LAST child`),
	regexp.MustCompile(`(?i)last child of the (root|Grid|page)`),
	regexp.MustCompile(`(?i)last child = top`),
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

func statesTheRetiredRule(line string) bool {
	for _, re := range retiredRule {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// qualifiers are the phrases that make a statement of the old rule
// acceptable. Two families, and the distinction is the point:
//
//   - the CORRECTION — the sentence is stating the current rule, which
//     always involves the second layer, a rank, or the marker;
//   - the EPITAPH — the sentence is quoting the old rule in order to say
//     it is dead, which every good comment about this does.
//
// A third family is admitted grudgingly: hit-testing genuinely still walks
// document order, so a line about the hit walk may say "last" and mean it.
// It has to name the walk to get the exemption.
// Compiled ONCE, like retiredRule above — and a plain slice literal, like
// retiredRule. This was a function returning a fresh slice, recompiling
// nine regexps on every call; the recompile was fixed by hoisting, and the
// closure it was extracted from lingered for a release. Raised in review
// of #458.
var qualifierRes = []*regexp.Regexp{
	// The correction.
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
	regexp.MustCompile(`#4(37|38|39)|#430`),
	// The epitaph.
	regexp.MustCompile(`(?i)used to (say|state|be)|is what this said|is what this used to`),
	regexp.MustCompile(`(?i)superseded|no longer|stopped being|was never|not any more|retired`),
	regexp.MustCompile(`(?i)by convention|convention,? not|incidental|heuristic|arbitrary`),
	regexp.MustCompile(`(?i)do not go looking|does not decide|decides nothing|position is free`),
}

// hitTestExemption is applied to the MATCHING LINE ALONE, never to the
// window, and that is the whole reason it is a separate variable.
//
// Hit-testing genuinely still walks document order, so a line about the
// hit walk may say "last" and mean it — but it has to name the walk to
// earn that, which is what this file's own comment always claimed and
// what qualifiedNear did not do. Every other qualifier reads a ±2 window,
// so a paragraph mentioning hit-testing two lines away exempted a stale
// PAINT claim beside it. That adjacency is not rare: it is this sweep's
// own house style — state the paint rule, then the input divergence, in
// one comment (components/popup.go, mouse.go). Raised in review of #458.
var hitTestExemption = regexp.MustCompile(`(?i)hit-?test|hit order|HitTest`)

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
// THE FLOOR IS ONE LINE, AND THAT IS THE RESIDUAL HOLE. The window
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
	const window = 2
	lo, hi := i-window, i+window
	if lo < 0 {
		lo = 0
	}
	if hi >= len(lines) {
		hi = len(lines) - 1
	}
	// The hit-test exemption is checked against THIS line only. See
	// hitTestExemption: naming the hit walk excuses the line that names
	// it, not its neighbours.
	if hitTestExemption.MatchString(lines[i]) {
		return true
	}
	block := strings.Join(lines[lo:hi+1], "\n")
	for _, re := range qualifierRes {
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
			// This file quotes the rule to test for it; exempting it by
			// name is honest, and it is the only name in here.
			if filepath.Base(p) == "zorderdocs_test.go" {
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
		"// it as the LAST child of its root — document order is z-order, the same",
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
	// docFiles' prefilter skips any file containing neither "order" nor
	// "last", and a comment tells the next author to keep retiredRule in
	// step with it. That comment is true today and enforces nothing: a
	// future pattern for, say, "paints on top because it is declared
	// after" matches neither substring, is skipped for 100% of the tree,
	// and the negative assertion still passes — a guard switched off by
	// adding to it. Every sample the patterns are pinned against must
	// therefore survive the prefilter too. Raised in review of #458.
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

// TestTheHitTestExemptionIsLineScoped pins the one qualifier that reads a
// single line instead of the ±2 window.
//
// Hit-testing really does still walk document order, so a line about the
// hit walk may say "last" and mean it — but it has to NAME the walk to
// earn that. Every other qualifier reads the window, and the exemption
// did too, so a paragraph mentioning hit-testing two lines away excused a
// stale PAINT claim beside it.
//
// That adjacency is not hypothetical: it is this sweep's own house style,
// which is to state the paint rule and then the input divergence in one
// comment (components/popup.go, mouse.go). The exemption was therefore at
// its most permissive exactly where the sweep concentrated its prose.
//
// Reverting the exemption to the window is otherwise SILENT — measured —
// which is what this test is for. Raised in review of #458.
func TestTheHitTestExemptionIsLineScoped(t *testing.T) {
	stale := "// z-order is document order, so declare the MenuBar last."
	if !statesTheRetiredRule(stale) {
		t.Fatalf("the fixture line is not caught by retiredRule, so this test proves nothing:\n\t%s", stale)
	}

	// NAMED ON THE LINE: exempt. This is a real thing to write.
	onTheLine := []string{
		"// unrelated",
		"// hit-testing walks document order, so the last child is hit first.",
		"// unrelated",
	}
	if !qualifiedNear(onTheLine, 1) {
		t.Error("a line that names the hit walk was not exempted; the exemption has " +
			"stopped working and every such comment now has to be reworded")
	}

	// NAMED TWO LINES AWAY, with a stale PAINT claim between: not exempt.
	nearby := []string{
		"// Hit-testing still walks document order.",
		"//",
		stale,
	}
	if qualifiedNear(nearby, 2) {
		t.Error("a stale paint claim was exempted because a NEIGHBOURING line mentions " +
			"hit-testing. The exemption is for the line that names the walk, not for " +
			"its neighbours — and 'paint rule, then input divergence, in one comment' " +
			"is this repo's house style, so the window makes the guard blindest exactly " +
			"where the prose is densest.")
	}
}
