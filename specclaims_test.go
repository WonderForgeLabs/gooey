package gooey

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// A decision record names tests in backticks as its evidence, and nothing
// checked that those names resolve to anything. A rename orphans a row
// silently: the table still reads as evidence and is pointing at nothing.
//
// It is the same shape as the enumerated module list CLAUDE.md already
// refuses — "a written list is stale the first time someone adds one, and
// the failure is silent" — and the sweep that produced this guard found
// three live claims that had rotted, one of them into its own opposite:
// docs/specs/2026-08-10-container-backgrounds.md cited a test that
// documented an artifact, which had since been renamed to one asserting
// the artifact is fixed. The prose still described the bug.
//
// #468.

// citedTestName is a backticked Go test name in prose, with an OPTIONAL
// package qualifier.
//
// The qualifier is not decoration and matching it is not optional: six
// live citations in docs/specs are written `markup.TestX` and
// `wysiwyg.TestX`, and the first version of this guard's pattern required
// a backtick immediately before `Test`, so all six were invisible to it —
// a guard against rot that could not see the citations most likely to
// rot, because a cross-package name is the one whose test you are least
// likely to notice renaming. Review of PR #476 caught that.
var citedTestName = regexp.MustCompile("`(?:([a-z][A-Za-z0-9_]*)\\.)?(Test[A-Za-z0-9_]*)`")

// testFuncDecl is a test function declaration in the tree.
var testFuncDecl = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\s*\(`)

// mdHeading is a Markdown ATX heading; the level scopes plannedMarker.
var mdHeading = regexp.MustCompile(`^(#+)\s`)

// mdFence opens or closes a fenced code block. Up to three leading spaces
// is what CommonMark allows before a fence.
var mdFence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

// plannedMarker opts a section OUT of the check.
//
// A spec legitimately names tests that do not exist: an "Implementation
// plan" proposes them for issues not yet built, and 29 of the names in
// docs/specs today are that. Checking those would make the guard start
// RED, and the only ways to land a red guard are an allowlist or a skip
// list — the hand-maintained known-bad list CLAUDE.md forbids, "because a
// stale dismissal spends the attention that would have caught the bug".
//
// So the exemption is EXPLICIT and per section rather than inferred. The
// alternative considered was reading the heading — skip anything called
// "Implementation plan" — and it fails in both directions on today's
// files: docs/specs/2026-08-11-design-surface.md states a live, present-
// tense claim under "Order of work" (a plan heading), and two correct
// sentences that mention a test BY ITS DEAD NAME sit under ordinary
// headings. Guessing from the heading would have missed the first and
// failed on the other two.
//
// A historical mention — "formerly TestX", "it was TestX until" — is
// spelled WITHOUT backticks instead, which is both honest (the name is
// not a live reference) and mechanical (nothing has to classify it).
//
// THE EXEMPTION EXPIRES ON ITS OWN, which is the half that keeps it from
// becoming the very list it replaces: TestNoMarkedSectionNamesALandedTest
// fails the moment a proposed name exists in the tree, because from that
// moment the section is describing something that landed and the name is
// an ordinary citation that can rot like any other. Review of PR #476
// found four such rows already — this marker had gone stale inside the
// commit that introduced it.
// IT BINDS TO THE NEAREST HEADING ABOVE IT, AT THAT HEADING'S DEPTH,
// and the exemption ends at the next heading of that depth or shallower.
// So a marker under `## Implementation plan` → `### Stage 1` exempts
// Stage 1 and nothing else, and the names in Stage 2 fail — loudly,
// which is the right direction, but the failure does not say that the
// marker's depth is why. Put it under the heading whose whole subtree is
// proposed. Raised in review of #476.
const plannedMarker = "<!-- spec-tests: planned -->"

// proseSkip are the directories the Markdown walk does not enter, and
// the list is SHORT ON PURPOSE.
//
// This started as citedRoots — "docs", "CLAUDE.md", "README.md" — which
// is the enumeration shape CLAUDE.md refuses for modules, and it failed
// exactly the way that rule predicts: prose outside those three was
// already citing tests. handlers/prop/README.md names four,
// handlers/str, handlers/sets and presentations/the-rectangle one each,
// and the tracked .claude/plugin/skills/ pages name the CI-discovery
// trio this repo's whole verify story rests on. Appending a citation to
// a nonexistent test in handlers/prop/README.md left the guard GREEN.
// Raised in review of #476.
//
// So the corpus is the TREE, and the prune is by path rather than by dot
// prefix. testFuncsUnder skips every dot-directory because .claude/
// worktrees hold whole checkouts and their tests are another branch's;
// that argument is about worktrees, and applying it to dot-directories
// wholesale is what hid .claude/plugin/skills. Name the two.
var proseSkip = map[string]bool{
	"vendor":            true,
	".claude/worktrees": true,
	".git":              true,
}

// citation is one backticked test name in prose.
type citation struct {
	file string
	line int
	pkg  string // "" when the citation is unqualified
	name string
	text string
}

func (c citation) cited() string {
	if c.pkg == "" {
		return c.name
	}
	return c.pkg + "." + c.name
}

// fencedLines marks the lines inside a fenced code block, the fence lines
// themselves included, and reports whether a fence was left open at EOF.
//
// It exists for one specific misreading: CLAUDE.md's verify loop is a
// ```sh block whose shell comments start at column 0, so a line reading
// `# TestCIWorkflow…` parses as an ATX heading. A phantom heading inside a
// fence ENDS an exemption region early, silently un-skipping the rest of a
// planned section — the guard then fails on names nobody claimed. Review
// of PR #476 caught it before the corpus grew to a file that had one.
//
// COMMONMARK'S CLOSING RULE, not a toggle, and the difference is a
// fail-open. This flipped on ANY fence line: it did not require the
// closer to match the opener's character, did not require it to be at
// least as long, and never checked the block closed. So one nested-fence
// example — ```` wrapping ``` blocks, which docs/learn/** is full of —
// or one ``` closed with ~~~ inverts the rest of the file, and every
// real citation after it is read as code and goes unchecked while the
// suite stays green. That is the exact fail-open this guard exists to
// remove. Reproduced by appending a lone fence and a bogus citation to
// docs/specs/2026-08-25-clipping.md: green.
//
// Every file in the corpus has even parity today, so nothing was broken
// — which is why the second return matters. An imbalance is not silently
// tolerated; the caller reports the file as unparseable, loudly, rather
// than reading half of it. Raised in review of #476.
func fencedLines(lines []string) (in []bool, unclosed bool) {
	in = make([]bool, len(lines))
	var char string
	var width int
	for i, l := range lines {
		m := mdFence.FindStringSubmatch(l)
		if m == nil {
			in[i] = char != ""
			continue
		}
		c, w := m[1][:1], len(m[1])
		switch {
		case char == "":
			char, width = c, w
			in[i] = true
		case c == char && w >= width:
			char, width = "", 0
			in[i] = true
		default:
			// A fence of the OTHER character, or a shorter run of the
			// same one, is content inside the open block.
			in[i] = true
		}
	}
	return in, char != ""
}

// readProse splits one document's citations into the ones it asserts and
// the ones a marked section merely proposes. It is separate from the
// tests so the parser can be pointed at a synthetic document — see
// TestTheCitationGuardCatchesWhatItIsFor.
// The third return is the unclosed-fence report. It is a value rather
// than a t.Errorf so this stays callable from a fixture, and it is not
// dropped by either corpus caller: a file whose fences do not balance is
// half-read, and half-read is exactly the silence this guard exists to
// remove.
func readProse(file, body string) (live, planned []citation, unclosed bool) {
	lines := strings.Split(body, "\n")
	fenced, unclosed := fencedLines(lines)

	// Headings, in order, with their level.
	type heading struct{ line, depth int }
	var heads []heading
	for i, l := range lines {
		if fenced[i] {
			continue
		}
		if h := mdHeading.FindStringSubmatch(l); h != nil {
			heads = append(heads, heading{i, len(h[1])})
		}
	}
	// end is the first line NOT in the section heads[k] opens: the next
	// heading at its level or shallower. A deeper one is inside it.
	end := func(k int) int {
		for j := k + 1; j < len(heads); j++ {
			if heads[j].depth <= heads[k].depth {
				return heads[j].line
			}
		}
		return len(lines)
	}

	// THE MARKER ATTACHES TO ITS SECTION, NOT TO THE REST OF THE FILE,
	// and it covers the WHOLE section rather than only the part below
	// itself. Attaching downward only would be a trap the reader cannot
	// see: a sentence above the marker but under the same heading reads
	// as exempt to a human and was checked by the machine. Review of
	// PR #476 asked which it was; this is the answer, and the arms below
	// pin it.
	skip := make([]bool, len(lines))
	for i, l := range lines {
		if fenced[i] || !strings.Contains(l, plannedMarker) {
			continue
		}
		k := -1
		for j := range heads {
			if heads[j].line >= i {
				break
			}
			k = j
		}
		if k < 0 {
			// No heading above it. It marks nothing, rather than
			// silently exempting the rest of the file.
			continue
		}
		for n := heads[k].line; n < end(k); n++ {
			skip[n] = true
		}
	}

	for i, l := range lines {
		if fenced[i] {
			continue
		}
		for _, m := range citedTestName.FindAllStringSubmatch(l, -1) {
			c := citation{file, i + 1, m[1], m[2], strings.TrimSpace(l)}
			if skip[i] {
				planned = append(planned, c)
			} else {
				live = append(live, c)
			}
		}
	}
	return live, planned, unclosed
}

// testFuncsUnder is every `func TestX(` beneath root, nested modules
// included, mapped to the DIRECTORY NAMES holding it. Derived rather than
// listed, which is the point: neither side of this comparison is written
// down anywhere.
//
// The directories are what a package qualifier is checked against.
// Matching the qualifier to the directory rather than to the Go package
// clause is deliberate: `wysiwyg.TestTheModeFlipRepaintsOnlyTheIndicator`
// lives in apps/wysiwyg, whose package clause is `main`, and every
// citation in the tree spells the qualifier the way the import path ends.
//
// The root is a parameter so the arm below can walk a FIXTURE. Without
// that, the dot-directory pruning is unfalsifiable: dropping it can only
// ADD names, and adding names to a corpus whose claims all resolve
// changes no outcome — the mutation was silent until this took an
// argument.
func testFuncsUnder(t *testing.T, root string) map[string]map[string]bool {
	t.Helper()
	found := map[string]map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Dot-directories at EVERY depth, for the reason CLAUDE.md's
			// verify loop gives: .claude/worktrees/ holds whole checkouts
			// of this repo, so walking into one counts another branch's
			// tests as this one's — and a name deleted here would look
			// present because a worktree still has it.
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "vendor") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dir := filepath.Base(filepath.Dir(path))
		for _, m := range testFuncDecl.FindAllStringSubmatch(string(b), -1) {
			if found[m[1]] == nil {
				found[m[1]] = map[string]bool{}
			}
			found[m[1]][dir] = true
			// AND UNDER THE MODULE'S OWN NAME, for a root-package test.
			// filepath.Base(filepath.Dir(path)) is "." there, so
			// `gooey.TestFoo` — the natural spelling, and the one the
			// race-tier citation above now uses — could never resolve:
			// the report read "the test exists, but in `.`, not gooey",
			// which is a false failure with a confusing message. Raised
			// in review of #476.
			if dir == "." {
				found[m[1]][rootModuleName] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree for test functions: %v", err)
	}
	return found
}

// rootModuleName is what a citation qualified with the ROOT module's
// package spells. It is the last element of the module path, the same
// way every other qualifier in the tree is the last element of an import
// path.
const rootModuleName = "gooey"

// fault is a citation that does not resolve, and why.
type fault struct {
	citation
	why string
}

// unresolved returns the citations naming a test the tree does not hold —
// or holds somewhere the qualifier says it is not. Separate from the test
// for the same reason testFuncsUnder takes a root: with a corpus whose
// citations all resolve, a mutation that stops comparing altogether
// changes nothing observable.
func unresolved(cites []citation, funcs map[string]map[string]bool) []fault {
	var out []fault
	for _, c := range cites {
		dirs, ok := funcs[c.name]
		switch {
		case !ok:
			out = append(out, fault{c, "no such test exists"})
		case c.pkg != "" && !dirs[c.pkg]:
			out = append(out, fault{c, "the test exists, but in " +
				strings.Join(sortedKeys(dirs), "/") + ", not " + c.pkg})
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// proseFiles is every Markdown file in the tree. filepath.WalkDir
// rather than os.ReadDir, so a docs/specs subdirectory — or any of the
// nested trees under docs/learn — is read rather than silently skipped.
func proseFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if proseSkip[filepath.ToSlash(path)] {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree for Markdown: %v", err)
	}
	sort.Strings(out)
	return out
}

func TestEveryCitedTestNameResolves(t *testing.T) {
	funcs := testFuncsUnder(t, ".")
	if len(funcs) == 0 {
		t.Fatal("the walk found no test functions at all, so every name below " +
			"would report as orphaned and the failure would be this test's")
	}

	files := proseFiles(t)
	if len(files) == 0 {
		t.Fatalf("the tree walk found no Markdown at all, so this test reads nothing")
	}
	cited := 0
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("reading %s: %v", p, err)
			continue
		}
		live, _, unclosed := readProse(p, string(b))
		if unclosed {
			t.Errorf("%s leaves a fenced code block open at EOF, so everything "+
				"after the stray fence was read as code and no citation in it was "+
				"checked. Balance the fence — a nested example needs a longer "+
				"outer run (````), and a ``` block cannot be closed with ~~~. "+
				"(#468)", p)
		}
		cited += len(live)
		for _, f := range unresolved(live, funcs) {
			t.Errorf("%s:%d cites %s as evidence and %s.\n\t%s\n"+
				"A renamed test leaves the row reading as a check while checking "+
				"nothing. Fix the name, delete the claim, or — if the test is one "+
				"this section PROPOSES rather than one the tree holds — put %q "+
				"under the section's heading. (#468)",
				p, f.line, f.cited(), f.why, f.text, plannedMarker)
		}
	}
	if cited == 0 {
		t.Errorf("no document in the tree names a test outside a marked section, " +
			"so this test checks nothing. Either the docs have stopped citing " +
			"tests or the pattern has drifted from how they are written.")
	}
}

// TestNoMarkedSectionNamesALandedTest is what stops the exemption from
// becoming the hand-maintained known-bad list it was written to avoid.
//
// plannedMarker means "these names are PROPOSALS". The moment one of them
// exists, that stops being true and the row is an ordinary citation — one
// that can rot, inside a region where nothing would notice. The marker has
// no expiry of its own, so this is it.
//
// It is not hypothetical: review of PR #476 found the marker already stale
// in the commit that introduced it. docs/specs/2026-08-10-styles-and-
// resources.md proposed six damage-count arms, two of which had landed in
// markup/resources_test.go, and the whole of Part 4 — live contract prose,
// not a proposal list — was exempt because the marker sat directly under
// the Part's own heading.
func TestNoMarkedSectionNamesALandedTest(t *testing.T) {
	funcs := testFuncsUnder(t, ".")
	if len(funcs) == 0 {
		t.Fatal("the walk found no test functions, so every proposal below would " +
			"read as still-planned and this test would pass on anything")
	}
	marked := 0
	for _, p := range proseFiles(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("reading %s: %v", p, err)
			continue
		}
		_, planned, _ := readProse(p, string(b))
		marked += len(planned)
		for _, c := range planned {
			if funcs[c.name] == nil {
				continue
			}
			t.Errorf("%s:%d proposes %s under %q, and that test now exists in %s.\n\t%s\n"+
				"The exemption has outlived what it exempted: the row is an ordinary "+
				"citation now, and inside a marked section nothing would notice it "+
				"rotting. Cite it live — outside the marked section, qualified by its "+
				"package — or drop the row. (#468)",
				p, c.line, c.name, plannedMarker,
				strings.Join(sortedKeys(funcs[c.name]), "/"), c.text)
		}
	}
	// NOT AN ERROR WHEN IT IS ZERO, and this used to be one.
	//
	// The arm above is a for-range that a broken parser returns empty
	// for, so it needs a non-vacuity check — but "no section carries the
	// marker" is ALSO the correct end state, the one the arm above is
	// driving the corpus toward, and turning it red made the successful
	// outcome a failing test whose only remedy is editing the test.
	//
	// The parser is pinned where a pin belongs: the `a marked section is
	// skipped` arm of TestTheCitationGuardCatchesWhatItIsFor feeds it a
	// document that HAS a marker and requires the names under it to come
	// back planned rather than live. A spelling drift fails there, on a
	// fixture, whatever the corpus happens to hold. Raised in review of
	// #476.
	t.Logf("sections carrying %q: %d", plannedMarker, marked)
}

// TestTheCitationGuardCatchesWhatItIsFor is the arm that keeps the guard
// honest, and it is not decoration.
//
// A checker like this fails OPEN in every direction that matters: widen
// the marker's scope and whole files stop being read, break the name
// pattern and nothing is collected, and neither shows against a corpus
// that is already correct — the docs pass just as well with the check
// disabled. So the parser is pointed at documents whose contents are
// known.
func TestTheCitationGuardCatchesWhatItIsFor(t *testing.T) {
	const marked = "## Implementation plan\n" + plannedMarker + "\nTests: `TestPlanned`.\n"

	for _, tc := range []struct {
		name          string
		body          string
		live, planned []string
	}{
		{
			name: "a plain claim is collected",
			body: "Pinned by `TestAlpha`.",
			live: []string{"TestAlpha"},
		},
		{
			name: "two on one line",
			body: "`TestAlpha` and `TestBeta`.",
			live: []string{"TestAlpha", "TestBeta"},
		},
		{
			name:    "a marked section is skipped",
			body:    marked,
			planned: []string{"TestPlanned"},
		},
		{
			name:    "the marker ends at the next same-level heading",
			body:    marked + "## After\nPinned by `TestAfter`.\n",
			live:    []string{"TestAfter"},
			planned: []string{"TestPlanned"},
		},
		{
			name:    "a deeper heading stays inside the marked section",
			body:    marked + "### Still planned\nTests: `TestDeeper`.\n",
			planned: []string{"TestPlanned", "TestDeeper"},
		},
		{
			name: "an unbackticked historical name is not a claim",
			body: "It was TestOldName until the rename.",
		},
		{
			name: "a marker with no heading above it marks nothing",
			body: plannedMarker + "\nPinned by `TestLoose`.\n",
			live: []string{"TestLoose"},
		},
		{
			// THE HALF THE OLD ARM MISSED. It covered a marker with no
			// heading at all and called that "attachment"; the question a
			// reader actually has is what happens to a citation ABOVE the
			// marker but under the same heading. It is exempt: the marker
			// scopes its section, not the remainder of the file after
			// itself.
			name:    "a claim above the marker in the same section is exempt too",
			body:    "## Plan\nProposed: `TestEarly`.\n" + plannedMarker + "\nAlso `TestLate`.\n",
			planned: []string{"TestEarly", "TestLate"},
		},
		{
			name: "a package-qualified citation is collected with its qualifier",
			body: "See `markup.TestQualified`.",
			live: []string{"markup.TestQualified"},
		},
		{
			// A COLUMN-0 SHELL COMMENT IS NOT A HEADING. CLAUDE.md's
			// verify loop is exactly this shape, and reading the `#` as a
			// heading ends the marked section early — the guard then fails
			// on names the document never claimed.
			name:    "a fenced block does not end a marked section",
			body:    marked + "```sh\n# TestFake pins that\n```\nStill `TestAfterFence`.\n",
			planned: []string{"TestPlanned", "TestAfterFence"},
		},
		{
			name: "a name inside a fence is not a citation",
			body: "```go\nrun(`TestInAFence`)\n```\n",
		},
		{
			name:    "a tilde fence counts too",
			body:    marked + "~~~\n# TestFake\n~~~\nStill `TestAfterTilde`.\n",
			planned: []string{"TestPlanned", "TestAfterTilde"},
		},
		{
			// A NESTED FENCE, which is ordinary Markdown and was a
			// fail-open. A ```` run wrapping a ``` block used to toggle
			// four times, so the tail of the file read as code and every
			// citation after it went unchecked while the suite stayed
			// green. CommonMark closes on the same character at the same
			// length or longer, which is what makes the inner ``` content.
			name: "a nested fence does not invert the rest of the file",
			body: "````md\n```go\nrun(`TestInside`)\n```\n````\nPinned by `TestOutside`.\n",
			live: []string{"TestOutside"},
		},
		{
			// AND A MISMATCHED CLOSER. A ``` block cannot be closed with
			// ~~~; treating the tilde run as a closer is what let one
			// typo blind a file.
			name: "a tilde line does not close a backtick fence",
			body: "```\n~~~\nrun(`TestInside`)\n```\nPinned by `TestOutside`.\n",
			live: []string{"TestOutside"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live, planned, _ := readProse("t.md", tc.body)
			check := func(what string, got []citation, want []string) {
				t.Helper()
				var names []string
				for _, c := range got {
					names = append(names, c.cited())
				}
				sort.Strings(names)
				w := append([]string(nil), want...)
				sort.Strings(w)
				if strings.Join(names, ",") != strings.Join(w, ",") {
					t.Errorf("%s: collected %v, want %v", what, names, w)
				}
			}
			check("live", live, tc.live)
			check("planned", planned, tc.planned)
		})
	}

	// AN UNBALANCED FENCE IS REPORTED, not tolerated. This is the half
	// that keeps the CommonMark rule above from trading one silence for
	// another: with strict closing, a stray opener no longer inverts the
	// file — it swallows the tail instead, which is the same lost
	// citations. The corpus caller turns this into a failure naming the
	// file. Raised in review of #476.
	t.Run("an unclosed fence is reported", func(t *testing.T) {
		live, _, unclosed := readProse("t.md", "```\nPinned by `TestInside`.\n")
		if !unclosed {
			t.Errorf("a file whose fence never closes was read as balanced, so " +
				"everything after the stray fence is silently uncheckable")
		}
		if len(live) != 0 {
			t.Errorf("collected %v from inside an unclosed fence; the point of "+
				"reporting the imbalance is that this content cannot be trusted "+
				"either way", live)
		}
	})

	// AND A BALANCED FILE DOES NOT REPORT ONE, which is the arm that
	// stops "always true" from being a passing strategy for the check
	// above.
	t.Run("a balanced file reports no imbalance", func(t *testing.T) {
		if _, _, unclosed := readProse("t.md", "```go\nx\n```\nPinned by `TestX`.\n"); unclosed {
			t.Error("a closed fence was reported as unclosed, which would fail " +
				"every document in the corpus")
		}
	})

	// THE ROOT MODULE ANSWERS TO ITS OWN NAME. testFuncsUnder keys a
	// root-package test under filepath.Base(filepath.Dir(path)), which is
	// ".", so `gooey.TestFoo` — the spelling CLAUDE.md's race-tier
	// citation now uses — reported "the test exists, but in `.`, not
	// gooey". A false failure with a confusing message. Raised in review
	// of #476.
	t.Run("a root-module test resolves under the module name", func(t *testing.T) {
		// THIS test, whose declaration is in the root package — so the
		// arm cannot go vacuous by naming something that stopped
		// existing.
		const self = "TestTheCitationGuardCatchesWhatItIsFor"
		live, _, _ := readProse("t.md", "See `"+rootModuleName+"."+self+"`.")
		funcs := testFuncsUnder(t, ".")
		if got := unresolved(live, funcs); len(got) != 0 {
			t.Errorf("a root-package test cited as %s.%s did not resolve: %v",
				rootModuleName, self, got)
		}
		// AND THE DIRECTORY SPELLING STILL WORKS, so the fix added a key
		// rather than moving one — an unqualified citation and a "."
		// qualifier both have to keep resolving.
		if !funcs[self]["."] {
			t.Errorf("%s is no longer recorded under %q; the module name is an "+
				"ADDITIONAL key, not a replacement", self, ".")
		}
	})

	// The comparison itself, which a clean corpus cannot exercise: with
	// every citation resolving, "compare and report" and "report nothing"
	// are the same green.
	t.Run("an unresolved claim is reported", func(t *testing.T) {
		live, _, _ := readProse("t.md", "Pinned by `TestHere` and `TestGone`.")
		got := unresolved(live, map[string]map[string]bool{"TestHere": {"markup": true}})
		if len(got) != 1 || got[0].name != "TestGone" {
			t.Errorf("unresolved reported %v, want exactly TestGone", got)
		}
	})

	// And the qualifier, which the name check alone cannot see: a test
	// that exists SOMEWHERE resolves under an unqualified citation, so
	// without this the qualifier could be parsed and then thrown away.
	t.Run("a qualifier naming the wrong package is reported", func(t *testing.T) {
		live, _, _ := readProse("t.md", "See `markup.TestSomewhere` and `wysiwyg.TestSomewhere`.")
		funcs := map[string]map[string]bool{"TestSomewhere": {"markup": true}}
		got := unresolved(live, funcs)
		if len(got) != 1 || got[0].pkg != "wysiwyg" {
			t.Fatalf("unresolved reported %v, want exactly the wysiwyg citation", got)
		}
		if !strings.Contains(got[0].why, "markup") {
			t.Errorf("the report says %q; it should name where the test actually "+
				"is, or the reader has to go looking", got[0].why)
		}
	})

	// And the walk's dot-directory pruning, for the reason testFuncsUnder
	// documents: .claude/worktrees/ holds whole checkouts of this repo, so
	// a walk that descends into one resolves a name this branch deleted
	// against another branch that still has it — the claim reads as
	// checked and is checked against somebody else's tree.
	t.Run("the walk prunes dot-directories and vendor", func(t *testing.T) {
		root := t.TempDir()
		write := func(dir, name, fn string) {
			t.Helper()
			if dir != "" {
				if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			body := "package p\n\nfunc " + fn + "(t *testing.T) {}\n"
			if err := os.WriteFile(filepath.Join(root, dir, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		write("", "a_test.go", "TestVisible")
		write(".worktrees/other", "b_test.go", "TestInsideADotDir")
		write("vendor/x", "c_test.go", "TestInVendor")
		write("markup", "d_test.go", "TestInMarkup")

		funcs := testFuncsUnder(t, root)
		if funcs["TestVisible"] == nil {
			t.Error("the walk missed an ordinary test file, so the two absences " +
				"below prove nothing")
		}
		if !funcs["TestInMarkup"]["markup"] {
			t.Errorf("the walk recorded TestInMarkup in %v, not markup; a qualified "+
				"citation would be reported against the wrong directory",
				sortedKeys(funcs["TestInMarkup"]))
		}
		for _, n := range []string{"TestInsideADotDir", "TestInVendor"} {
			if funcs[n] != nil {
				t.Errorf("the walk found %s; a claim resolved against a checkout "+
					"under .claude/worktrees/ is checked against another branch's "+
					"tree, not this one", n)
			}
		}
	})

	// The corpus walk, which os.ReadDir got wrong in a way no current file
	// exposes: docs/specs is flat today, so a reader that never descends
	// passes — and would keep passing the day somebody adds a
	// subdirectory, with the new records simply unchecked.
	t.Run("the corpus walk descends into subdirectories", func(t *testing.T) {
		files := proseFiles(t)
		nested := 0
		for _, f := range files {
			if strings.Count(filepath.ToSlash(f), "/") > 1 {
				nested++
			}
		}
		if nested == 0 {
			t.Errorf("every one of the %d documents read is at the top of its root, "+
				"so a walk that never descends would look identical to this one",
				len(files))
		}
	})

	// And the corpus's REACH, which is the half a subdirectory check cannot
	// see: docs/specs alone is nested too, so narrowing the roots back to
	// decision records looks identical to the walk above. The rot is not a
	// property of decision records — CLAUDE.md cites tests as the authority
	// for its own rules, and docs/learn cites them as the reason a tutorial
	// says what it says.
	// THE CORPUS IS THE TREE, and this arm is the floor on that. It used
	// to check three roots because the walk WAS three roots — and prose
	// outside them was already citing tests: handlers/prop/README.md names
	// four, and the tracked .claude/plugin/skills/ pages name the
	// CI-discovery trio this repo's whole verify story rests on. Appending
	// a citation to a nonexistent test in handlers/prop/README.md left the
	// guard green.
	//
	// DERIVED PLUS A HANDFUL OF WITNESSES, and the two halves answer
	// different questions. The derived half — every Markdown file in the
	// tree carrying a citation must be in the corpus — is what covers a
	// document nobody has thought of. The witnesses are the shapes a
	// plausible re-narrowing would drop: a nested module's README, a
	// tracked page under a DOT-directory (the reason a blanket dot-prune
	// is wrong here), and the repo root. Raised in review of #476.
	t.Run("the corpus is the whole tree", func(t *testing.T) {
		read := map[string]bool{}
		for _, f := range proseFiles(t) {
			read[filepath.ToSlash(f)] = true
		}

		// The witnesses, by the property that makes each one a witness
		// rather than by being a list somebody has to maintain: for each
		// prefix, SOME document under it must be read.
		for _, k := range []string{
			"CLAUDE.md", "docs/learn", "docs/specs",
			"handlers", ".claude/plugin",
		} {
			found := false
			for f := range read {
				if f == k || strings.HasPrefix(f, k+"/") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("no document under %s is read, so a citation there can rot "+
					"with nothing to notice. This corpus is the TREE; a walk that "+
					"stops short of %s has been narrowed", k, k)
			}
		}

		// AND THE DERIVED HALF: any Markdown in the tree that cites a
		// test must be in the corpus. The witnesses above cannot cover a
		// file nobody has thought of, and this is the half that does.
		var missed []string
		err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// The two prunes proseSkip makes, plus .git, and NOT
				// dot-directories in general — that is the point.
				if proseSkip[filepath.ToSlash(path)] {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".md") {
				return nil
			}
			p := filepath.ToSlash(path)
			if read[p] {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if live, planned, _ := readProse(p, string(b)); len(live)+len(planned) > 0 {
				missed = append(missed, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking the tree: %v", err)
		}
		for _, p := range missed {
			t.Errorf("%s names a test and is not in the corpus, so that citation "+
				"can rot with nothing to notice", p)
		}
	})
}
