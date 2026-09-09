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
var mdFence = regexp.MustCompile("^ {0,3}(```|~~~)")

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
const plannedMarker = "<!-- spec-tests: planned -->"

// citedRoots is the prose this guard reads. It is deliberately WIDER than
// docs/specs, because the rot is not a property of decision records:
// CLAUDE.md cites nine tests by name as the authority for its own rules
// (TestCIWorkflowAndCLAUDEMDShareOneDiscovery among them), and a rename
// there leaves a rule citing nothing while still reading as enforced.
var citedRoots = []string{
	"docs",
	"CLAUDE.md",
	"README.md",
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
// themselves included.
//
// It exists for one specific misreading: CLAUDE.md's verify loop is a
// ```sh block whose shell comments start at column 0, so a line reading
// `# TestCIWorkflow…` parses as an ATX heading. A phantom heading inside a
// fence ENDS an exemption region early, silently un-skipping the rest of a
// planned section — the guard then fails on names nobody claimed. Review
// of PR #476 caught it before the corpus grew to a file that had one.
func fencedLines(lines []string) []bool {
	in := make([]bool, len(lines))
	open := false
	for i, l := range lines {
		if mdFence.MatchString(l) {
			in[i] = true
			open = !open
			continue
		}
		in[i] = open
	}
	return in
}

// readProse splits one document's citations into the ones it asserts and
// the ones a marked section merely proposes. It is separate from the
// tests so the parser can be pointed at a synthetic document — see
// TestTheCitationGuardCatchesWhatItIsFor.
func readProse(file, body string) (live, planned []citation) {
	lines := strings.Split(body, "\n")
	fenced := fencedLines(lines)

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
	return live, planned
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
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree for test functions: %v", err)
	}
	return found
}

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

// proseFiles is every Markdown file under the cited roots. filepath.WalkDir
// rather than os.ReadDir, so a docs/specs subdirectory — or any of the
// nested trees under docs/learn — is read rather than silently skipped.
func proseFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, root := range citedRoots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if path != root && strings.HasPrefix(d.Name(), ".") {
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
			t.Fatalf("walking %s: %v", root, err)
		}
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
		t.Fatalf("no Markdown found under %v, so this test reads nothing", citedRoots)
	}
	cited := 0
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("reading %s: %v", p, err)
			continue
		}
		live, _ := readProse(p, string(b))
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
		t.Errorf("no document under %v names a test outside a marked section, so "+
			"this test checks nothing. Either the docs have stopped citing tests "+
			"or the pattern has drifted from how they are written.", citedRoots)
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
		_, planned := readProse(p, string(b))
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
	// NON-VACUITY. Every arm above is a for-range over a slice that a
	// broken parser returns empty, and an empty slice is green.
	if marked == 0 {
		t.Errorf("no section under %v is marked %q, so this test read nothing. "+
			"Either every proposal has landed and the markers should be gone, or "+
			"the marker's spelling has drifted.", citedRoots, plannedMarker)
	}
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			live, planned := readProse("t.md", tc.body)
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

	// The comparison itself, which a clean corpus cannot exercise: with
	// every citation resolving, "compare and report" and "report nothing"
	// are the same green.
	t.Run("an unresolved claim is reported", func(t *testing.T) {
		live, _ := readProse("t.md", "Pinned by `TestHere` and `TestGone`.")
		got := unresolved(live, map[string]map[string]bool{"TestHere": {"markup": true}})
		if len(got) != 1 || got[0].name != "TestGone" {
			t.Errorf("unresolved reported %v, want exactly TestGone", got)
		}
	})

	// And the qualifier, which the name check alone cannot see: a test
	// that exists SOMEWHERE resolves under an unqualified citation, so
	// without this the qualifier could be parsed and then thrown away.
	t.Run("a qualifier naming the wrong package is reported", func(t *testing.T) {
		live, _ := readProse("t.md", "See `markup.TestSomewhere` and `wysiwyg.TestSomewhere`.")
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
	t.Run("the corpus reaches past docs/specs", func(t *testing.T) {
		want := map[string]bool{"CLAUDE.md": false, "docs/learn": false, "docs/specs": false}
		for _, f := range proseFiles(t) {
			s := filepath.ToSlash(f)
			for k := range want {
				if s == k || strings.HasPrefix(s, k+"/") {
					want[k] = true
				}
			}
		}
		for _, k := range sortedKeys(want) {
			if !want[k] {
				t.Errorf("no document under %s is read, so a citation there can rot "+
					"with nothing to notice", k)
			}
		}
	})
}
