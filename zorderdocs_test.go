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
				"accepts are in qualifiers(). Hit-testing is the one thing "+
				"position still orders; say so explicitly if that is what "+
				"you mean.", f, i+1, strings.TrimSpace(line))
		}
	}
}

// retiredRule matches a line ASSERTING the hosting rule. Both halves have
// to be there: "z-order" alone is fine (the forward pass, the restore
// sweep and overlapping Canvas children all legitimately talk about it),
// and "last child" alone is fine (a VStack's last child gets the
// remainder; an ItemsView highlight is the row's last child). It is the
// two together, or the bare equation, that only ever meant the thing that
// stopped being true.
var retiredRule = []*regexp.Regexp{
	// The equation itself, either direction.
	regexp.MustCompile(`(?i)document order is z-?order`),
	regexp.MustCompile(`(?i)z-?order is document order`),
	regexp.MustCompile(`(?i)(tree|document) order IS z-?order`),
	// The instruction, in the spellings the repo actually used.
	regexp.MustCompile(`(?i)declare .{0,40}\bLAST\b`),
	regexp.MustCompile(`(?i)declared LAST\b`),
	regexp.MustCompile(`(?i)(as|is) the LAST child`),
	regexp.MustCompile(`(?i)last child of the (root|Grid|page)`),
	regexp.MustCompile(`(?i)last child = top`),
	regexp.MustCompile(`(?i)last-in-document-order`),
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
func qualifiers() []*regexp.Regexp {
	return []*regexp.Regexp{
		// The correction.
		regexp.MustCompile(`(?i)two layers|second (paint )?layer|overlay layer`),
		regexp.MustCompile(`(?i)gooey\.Overlay|OverlaysPage|OverlayRank|\bis a gooey\.Overlay\b`),
		regexp.MustCompile(`(?i)\blifted\b|\blifts\b|\brank(s|ed)?\b`),
		regexp.MustCompile(`#4(37|38|39)|#430`),
		// The epitaph.
		regexp.MustCompile(`(?i)used to (say|state|be)|is what this said|is what this used to`),
		regexp.MustCompile(`(?i)superseded|no longer|stopped being|was never|not any more|retired`),
		regexp.MustCompile(`(?i)by convention|convention,? not|incidental|heuristic|arbitrary`),
		regexp.MustCompile(`(?i)do not go looking|does not decide|decides nothing|position is free`),
		// The hit-test exemption.
		regexp.MustCompile(`(?i)hit-?test|hit order|HitTest`),
	}
}

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
	return supersededRe.MatchString(strings.Join(lines[:head], "\n"))
}

var supersededRe = regexp.MustCompile(`(?i)superseded`)

// qualifiedNear looks in a NARROW window around the hit — two lines each
// way, enough for one wrapped sentence and no more.
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
	block := strings.Join(lines[lo:hi+1], "\n")
	for _, re := range qualifiers() {
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
	if len(out) < 100 {
		t.Fatalf("found %d documentation files, want far more — the walk "+
			"is broken and every assertion below it is vacuous", len(out))
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
	}
	for _, line := range removed {
		if !statesTheRetiredRule(line) {
			t.Errorf("the guard does not recognize a line the sweep "+
				"actually removed, so it would not have caught it:\n\t%s", line)
		}
	}

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
	for _, line := range kept {
		if statesTheRetiredRule(line) && !qualifiedNear([]string{line}, 0) {
			t.Errorf("the guard fires on a correct sentence, which makes "+
				"it noise rather than a check:\n\t%s", line)
		}
	}
}
