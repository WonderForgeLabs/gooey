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

// specTestName is a backticked Go test name in a spec.
var specTestName = regexp.MustCompile("`(Test[A-Za-z0-9_]*)`")

// specTestFunc is a test function declaration in the tree.
var specTestFunc = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\s*\(`)

// specHeading is a Markdown ATX heading; the level scopes plannedMarker.
var specHeading = regexp.MustCompile(`^(#+)\s`)

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
const plannedMarker = "<!-- spec-tests: planned -->"

const specDir = "docs/specs"

// specClaim is one backticked test name in a spec, outside a marked
// section.
type specClaim struct {
	file string
	line int
	name string
	text string
}

// specClaims returns every name a spec asserts, skipping sections marked
// planned. It is separate from the test so the parser can be pointed at a
// synthetic document — see TestTheSpecClaimGuardCatchesWhatItIsFor.
func specClaims(file, body string) []specClaim {
	var out []specClaim
	// skipDepth is the heading level of the marked section currently in
	// force, or 0 when none is. A heading at that level or shallower ends
	// it; a deeper one is inside it and stays skipped.
	skipDepth := 0
	pendingDepth := 0
	for i, line := range strings.Split(body, "\n") {
		if h := specHeading.FindStringSubmatch(line); h != nil {
			d := len(h[1])
			if skipDepth > 0 && d <= skipDepth {
				skipDepth = 0
			}
			pendingDepth = d
			continue
		}
		if strings.Contains(line, plannedMarker) {
			// The marker attaches to the heading above it. Without a
			// heading it marks nothing, rather than silently exempting
			// the rest of the file.
			if pendingDepth > 0 {
				skipDepth = pendingDepth
			}
			continue
		}
		if skipDepth > 0 {
			continue
		}
		for _, m := range specTestName.FindAllStringSubmatch(line, -1) {
			out = append(out, specClaim{file, i + 1, m[1], strings.TrimSpace(line)})
		}
	}
	return out
}

// testFuncsUnder is every `func TestX(` beneath root, nested modules
// included. Derived rather than listed, which is the point: neither side
// of this comparison is written down anywhere.
//
// The root is a parameter so the arm below can walk a FIXTURE. Without
// that, the dot-directory pruning is unfalsifiable: dropping it can only
// ADD names, and adding names to a corpus whose claims all resolve
// changes no outcome — the mutation was silent until this took an
// argument.
func testFuncsUnder(t *testing.T, root string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
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
		for _, m := range specTestFunc.FindAllStringSubmatch(string(b), -1) {
			found[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree for test functions: %v", err)
	}
	return found
}

// unresolved returns the claims naming a test the tree does not hold.
// Separate from the test for the same reason testFuncsUnder takes a root:
// with a corpus whose claims all resolve, a mutation that stops comparing
// altogether changes nothing observable.
func unresolved(claims []specClaim, funcs map[string]bool) []specClaim {
	var out []specClaim
	for _, c := range claims {
		if !funcs[c.name] {
			out = append(out, c)
		}
	}
	return out
}

func TestEverySpecTestNameResolves(t *testing.T) {
	funcs := testFuncsUnder(t, ".")
	if len(funcs) == 0 {
		t.Fatal("the walk found no test functions at all, so every name below " +
			"would report as orphaned and the failure would be this test's")
	}

	entries, err := os.ReadDir(specDir)
	if err != nil {
		t.Fatalf("reading %s: %v", specDir, err)
	}
	claims := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(specDir, e.Name())
		b, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("reading %s: %v", p, err)
			continue
		}
		found := specClaims(e.Name(), string(b))
		claims += len(found)
		for _, c := range unresolved(found, funcs) {
			t.Errorf("%s:%d names %s as evidence and no such test exists.\n\t%s\n"+
				"A renamed test leaves the row reading as a check while checking "+
				"nothing. Fix the name, delete the claim, or — if the test is one "+
				"this section PROPOSES rather than one the tree holds — put %q "+
				"under the section's heading. (#468)",
				p, c.line, c.name, c.text, plannedMarker)
		}
	}
	if claims == 0 {
		t.Errorf("no spec names a test outside a marked section, so this test "+
			"checks nothing. Either %s has stopped citing tests or the pattern "+
			"has drifted from how they are written.", specDir)
	}
}

// TestTheSpecClaimGuardCatchesWhatItIsFor is the arm that keeps the guard
// honest, and it is not decoration.
//
// A checker like this fails OPEN in every direction that matters: widen
// the marker's scope and whole files stop being read, break the name
// pattern and nothing is collected, and neither shows against a corpus
// that is already correct — docs/specs passes just as well with the check
// disabled. So the parser is pointed at documents whose contents are
// known.
func TestTheSpecClaimGuardCatchesWhatItIsFor(t *testing.T) {
	const marked = "## Implementation plan\n" + plannedMarker + "\nTests: `TestPlanned`.\n"

	for _, tc := range []struct {
		name string
		body string
		want []string
	}{
		{"a plain claim is collected", "Pinned by `TestAlpha`.", []string{"TestAlpha"}},
		{"two on one line", "`TestAlpha` and `TestBeta`.", []string{"TestAlpha", "TestBeta"}},
		{"a marked section is skipped", marked, nil},
		{
			name: "the marker ends at the next same-level heading",
			body: marked + "## After\nPinned by `TestAfter`.\n",
			want: []string{"TestAfter"},
		},
		{
			name: "a deeper heading stays inside the marked section",
			body: marked + "### Still planned\nTests: `TestDeeper`.\n",
			want: nil,
		},
		{
			name: "an unbackticked historical name is not a claim",
			body: "It was TestOldName until the rename.",
			want: nil,
		},
		{
			name: "a marker with no heading above it marks nothing",
			body: plannedMarker + "\nPinned by `TestLoose`.\n",
			want: []string{"TestLoose"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, c := range specClaims("t.md", tc.body) {
				got = append(got, c.name)
			}
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("collected %v, want %v", got, want)
			}
		})
	}

	// The comparison itself, which a clean corpus cannot exercise: with
	// every claim resolving, "compare and report" and "report nothing"
	// are the same green.
	t.Run("an unresolved claim is reported", func(t *testing.T) {
		claims := specClaims("t.md", "Pinned by `TestHere` and `TestGone`.")
		got := unresolved(claims, map[string]bool{"TestHere": true})
		if len(got) != 1 || got[0].name != "TestGone" {
			t.Errorf("unresolved reported %v, want exactly TestGone", got)
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

		funcs := testFuncsUnder(t, root)
		if !funcs["TestVisible"] {
			t.Error("the walk missed an ordinary test file, so the two absences " +
				"below prove nothing")
		}
		for _, n := range []string{"TestInsideADotDir", "TestInVendor"} {
			if funcs[n] {
				t.Errorf("the walk found %s; a claim resolved against a checkout "+
					"under .claude/worktrees/ is checked against another branch's "+
					"tree, not this one", n)
			}
		}
	})
}
