package gooey

import (
	"errors"
	"fmt"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// CLAUDE.md's "Verify" section is the loop every agent runs to decide the
// tree is green. It used to name modules literally, and by the time issue
// #207 was filed it named five and silently skipped seven packs/temporal-*
// — the loop still exited 0, so a reader believed they had verified a tree
// they had never compiled. These tests pin the two ways that recurs: the
// documented discovery drifting behind the real module set, and the doc
// naming a module that no longer exists.

const claudeMD = "CLAUDE.md"

// moduleNamespaces are the directories nested modules live under. A
// backticked path in the doc whose first segment is one of these is a
// module reference, which lets TestCLAUDEMDNamesNoDeletedModule tell
// `handlers/temporal` apart from `prop/prop.go:33`.
//
// Yes, this is an enumerated list in a change whose whole point is that
// enumerated lists go stale — so it is worth saying why it is not derived
// from discoverModules. Deriving it would drop a namespace from the set at
// the exact moment its last module is deleted, which is precisely the case
// this test exists to catch: the doc would still name the dead module and
// nothing would complain. A stale entry here fails safe in the other
// direction — a module added under a NEW namespace simply is not checked
// for staleness by this secondary test, while
// TestCLAUDEMDVerifyLoopReachesEveryNestedModule, the primary anti-drift
// guard, discovers it with no list at all.
func moduleNamespaces(t *testing.T) map[string]bool {
	t.Helper()

	// The literal half. Every entry here is a namespace that must stay
	// checked even when the tree contains no module under it any more —
	// which is exactly when the doc is most likely to be pointing at a
	// module somebody just deleted. A DERIVED-only set would drop the
	// namespace at that moment and go green on the very case this guard
	// exists to catch, so these cannot simply be replaced by the walk.
	//
	// `examples` is such an entry rather than an oversight: no module has
	// lived under an `examples/` directory since the 2026-08-15 rename
	// (`docs/learn/examples/` has no go.mod), and it stays so that a
	// surviving `examples/foo` reference in the doc is still reported.
	ns := map[string]bool{
		"handlers": true,
		"packs":    true,
		"imagefmt": true,
		"examples": true,
	}

	// The derived half, unioned on top. This is what makes a namespace
	// covered the day its first module lands, instead of the day somebody
	// remembers to edit this list. `apps` went uncovered for exactly that
	// reason — the rename created the namespace, the doc started naming
	// modules in it, and the literal list was never touched (#316).
	for _, mod := range discoverModules(t) {
		if dir, _, nested := strings.Cut(mod, "/"); nested {
			ns[dir] = true
		}
	}
	return ns
}

// discoverModules walks the tree for nested modules the way the doc's loop
// must. Dot-directories are skipped: .claude/worktrees/ holds whole
// checkouts of this repo, so a naive walk finds their modules too — it
// passes in a clean CI checkout and fails on every developer machine.
func discoverModules(t *testing.T) []string {
	t.Helper()

	var mods []string
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" && p != "go.mod" {
			mods = append(mods, filepath.ToSlash(filepath.Dir(p)))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking for nested modules: %v", err)
	}
	sort.Strings(mods)
	return mods
}

// findCmd pulls the `find … -name go.mod …` invocation out of a shell
// block. The doc has to contain one: an enumerated list of module names is
// the failure mode this test exists to prevent.
var findCmd = regexp.MustCompile(`find [^\n|)]*-name go\.mod[^\n|)]*`)

func verifySection(t *testing.T) string {
	t.Helper()

	b, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("reading %s: %v", claudeMD, err)
	}
	doc := string(b)

	start := strings.Index(doc, "\n## Verify\n")
	if start < 0 {
		t.Fatalf("%s has no `## Verify` section", claudeMD)
	}
	rest := doc[start+len("\n## Verify\n"):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func TestCLAUDEMDVerifyLoopReachesEveryNestedModule(t *testing.T) {
	want := discoverModules(t)
	if len(want) == 0 {
		t.Fatal("no nested modules found; the walk is wrong, not the tree")
	}

	sec := verifySection(t)
	cmd := findCmd.FindString(sec)
	if cmd == "" {
		t.Fatalf("the %s `## Verify` section must DISCOVER nested modules with a "+
			"`find … -name go.mod` command, not enumerate them. A written list of "+
			"module names goes stale the first time someone adds a module, and the "+
			"loop keeps exiting 0 — see issue #207.", claudeMD)
	}

	// POSIX ONLY, and skipped rather than emulated on Windows.
	//
	// The point of this test is that the command WRITTEN IN THE DOC really
	// reaches every module, so it has to run that command rather than a Go
	// reimplementation of it — a reimplementation would pass while the
	// documented line was broken, which is the failure this test exists to
	// catch. That makes it inescapably POSIX: `find … -name go.mod` is a
	// POSIX utility, and Windows ships a find.exe that searches text inside
	// files and would take `-name` as a filename.
	//
	// So on Windows there is nothing honest to assert, and the choice is
	// between skipping and breaking `go test ./...` at the repo root for
	// every native-Windows contributor — which CLAUDE.md's own Verify
	// section tells everyone to run. A skip with a reason is the correct
	// outcome: the claim is unverifiable on that platform, and saying so is
	// better than a green run that checked nothing or a red one that found
	// no defect.
	if runtime.GOOS == "windows" {
		t.Skip("the documented discovery is a POSIX `find` invocation; Windows' " +
			"find.exe is an unrelated text-search tool, so this claim cannot be " +
			"checked here. Verify the loop under WSL or Git Bash.")
	}
	argv := shellWords(t, cmd)
	out, err := exec.Command(argv[0], argv[1:]...).Output()
	if err != nil {
		t.Fatalf("running the documented discovery %q: %v", cmd, err)
	}

	var got []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "./")
		if line == "" {
			continue
		}
		// The root go.mod comes out of the same walk. The documented loop
		// skips it explicitly, because the block above it already covers
		// the root module.
		if dir := path.Dir(line); dir != "." {
			got = append(got, dir)
		}
	}
	sort.Strings(got)

	if missing := missingFrom(got, want); len(missing) > 0 {
		t.Errorf("the documented verify loop never visits %v.\n"+
			"It discovers %d module(s); the tree has %d. Fix the command in %s "+
			"`## Verify` — a loop that skips a module still exits 0, which is the "+
			"whole failure mode (#207).", missing, len(got), len(want), claudeMD)
	}
	if extra := missingFrom(want, got); len(extra) > 0 {
		t.Errorf("the documented verify loop visits %v, which are not modules of "+
			"this repo. Dot-directories must stay excluded: .claude/worktrees/ "+
			"holds whole checkouts, so a loop without the exclusion re-tests every "+
			"worktree on the machine.", extra)
	}
}

func TestCLAUDEMDNamesNoDeletedModule(t *testing.T) {
	b, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("reading %s: %v", claudeMD, err)
	}

	namespaces := moduleNamespaces(t)
	backticked := regexp.MustCompile("`([a-z][a-z0-9]*(?:/[a-z0-9][a-z0-9._-]*)+)`")
	seen := map[string]bool{}
	for _, m := range backticked.FindAllStringSubmatch(string(b), -1) {
		ref := m[1]
		// A nested module is exactly `namespace/name`. Deeper paths are
		// something else — `apps/gitui/gitui` is the binary that module
		// builds, and the Traps section names it on purpose.
		parts := strings.Split(ref, "/")
		if len(parts) != 2 || !namespaces[parts[0]] || seen[ref] {
			continue
		}
		// Only whole directory paths name a module; `prop/prop.go:33` and
		// friends carry an extension or a line suffix.
		if strings.ContainsAny(ref, ":*") || filepath.Ext(ref) != "" {
			continue
		}
		seen[ref] = true

		if _, err := os.Stat(filepath.Join(ref, "go.mod")); err != nil {
			t.Errorf("%s names module %q, which has no go.mod (%v). A module that "+
				"was renamed or deleted leaves the doc pointing readers at nothing.",
				claudeMD, ref, err)
		}
	}
	if len(seen) == 0 {
		t.Errorf("%s names no nested module at all; this test would pass "+
			"vacuously, so the reference pattern has drifted", claudeMD)
	}
}

// shellWords splits the documented command into argv so it can be run
// directly, never through `sh -c`: the string comes out of a file, and a
// shell would let a stray metacharacter in it mean something. Only a plain
// `find` with quoted literals is accepted, which is all the doc needs.
func shellWords(t *testing.T, cmd string) []string {
	t.Helper()

	var argv []string
	for _, w := range strings.Fields(cmd) {
		if len(w) >= 2 && (w[0] == '\'' || w[0] == '"') && w[len(w)-1] == w[0] {
			w = w[1 : len(w)-1]
		}
		if strings.ContainsAny(w, "`$|;&<>") {
			t.Fatalf("documented discovery contains shell metacharacters in %q; "+
				"keep it a plain find invocation", w)
		}
		switch w {
		case "-exec", "-execdir", "-ok", "-okdir", "-delete", "-fprint":
			t.Fatalf("documented discovery uses %q; it must only list paths", w)
		}
		argv = append(argv, w)
	}
	if len(argv) == 0 || argv[0] != "find" {
		t.Fatalf("documented discovery %q is not a find invocation", cmd)
	}
	return argv
}

// missingFrom returns the elements of want that are absent from got.
func missingFrom(got, want []string) []string {
	have := make(map[string]bool, len(got))
	for _, g := range got {
		have[g] = true
	}
	var missing []string
	for _, w := range want {
		if !have[w] {
			missing = append(missing, w)
		}
	}
	return missing
}

// TestModuleNamespacesCoversEveryLiveNamespace is the guard on the guard.
// TestCLAUDEMDNamesNoDeletedModule can only check a reference whose first
// path segment is a known namespace, so a namespace it has never heard of
// makes it vacuous for every module underneath — and vacuous in the GREEN
// direction, which is the one nobody investigates.
//
// That is not hypothetical. The 2026-08-15 rename moved the demo tree
// under `apps/`, the doc names `apps/gitui` and `apps/wysiwyg`, and the
// namespace list never learned the word — so the guard covered none of
// the modules the doc actually names (#316).
// The oracle is git's INDEX, not discoverModules. That distinction is the
// whole test: `moduleNamespaces` derives half its set from
// `discoverModules`, so auditing it with `discoverModules` would re-derive
// the same answer by the same method and compare it against itself. Such a
// test cannot fail, and a test that cannot fail is the exact defect this
// one is about.
//
// git knows the tracked go.mod files by a completely separate mechanism,
// which is the same two-source cross-check ci.yml's `discover` job runs
// against its own walk — and it makes this falsifiable in both directions
// that matter: drop the union from `moduleNamespaces` and `apps` goes
// missing; break `discoverModules`' pruning and git still sees the module
// the walk lost.
func TestModuleNamespacesCoversEveryLiveNamespace(t *testing.T) {
	ns := moduleNamespaces(t)

	out, err := exec.Command("git", "ls-files", "--", ":(glob)**/go.mod").Output()
	if err != nil {
		t.Fatalf("listing tracked go.mod files: %v", err)
	}

	checked := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The MODULE's directory, then its first segment. Cutting the raw
		// `git ls-files` path at the first "/" is wrong in a way that looks
		// right: `grpc/go.mod` would yield "grpc" as a namespace, when grpc
		// is a top-level module with no namespace at all. The guard's rule
		// is the one to match — a nested module is exactly `namespace/name`,
		// i.e. a two-segment directory.
		modDir := path.Dir(line)
		if modDir == "." {
			continue // the root go.mod
		}
		parts := strings.Split(modDir, "/")
		if len(parts) < 2 {
			continue // a top-level module (grpc, mcp, paint) — no namespace
		}
		checked++
		if !ns[parts[0]] {
			t.Errorf("git tracks %q, so namespace %q has a live module — but the "+
				"deleted-module guard does not know that namespace, so every "+
				"%s/* reference in %s goes unchecked and the guard stays green "+
				"while the doc points readers at nothing.",
				line, parts[0], parts[0], claudeMD)
		}
	}

	// Without this the test passes by checking nothing the day the pathspec
	// stops matching — the vacuous-green failure it exists to prevent.
	if checked == 0 {
		t.Fatal("git listed no nested go.mod at all; the pathspec is wrong, not the tree")
	}
}

// A line number in prose is a sample taken once, and it decays faster
// than the counts this file already refuses to write down: any edit ABOVE
// a cited line moves it, so a citation rots without anyone touching the
// thing it describes. CLAUDE.md carries ~31 of them, and by the time #466
// was filed seven pointed at the wrong place and three named a file that
// does not exist — in the file whose stated purpose is "the rules whose
// violation is silent".
//
// It happened inside a single edit while the issue was being written: a
// 13-line comment added to composer.go moved a symbol and invalidated a
// citation written four minutes earlier.
//
// The alternative considered and rejected was dropping line numbers for
// the identifier alone (`hitTest` (`mouse.go`)), which removes the class
// outright. Several citations point INSIDE a function at the specific
// line carrying the argument, which an identifier cannot address, so the
// precision is worth a guard.

// citation matches `path/to/file.go:NNN`, optionally a range.
//
// BUILT FROM rePath, not spelled again. This carried a byte-identical
// copy of that pattern, and in a test whose entire subject is two copies
// of a fact drifting apart that was one copy too many: extend one for a
// new extension and the two halves silently cover different sets — the
// identifier half matching a shape the mechanical half never counted,
// and the non-vacuity guard below watching the wrong pattern. Raised in
// review of #475.
// citationRe, not `citation`: specclaims_test.go declares `type
// citation` in this same package, and a bare noun is the better name
// for "one backticked test name in prose" than for the pattern that
// finds one.
//
// The two names collided on MAIN and in neither PR. #475 added this var
// and #476 added that type; each was green against a main that did not
// yet have the other, and they squash-merged four seconds apart into a
// root package that would not compile. Nothing in CI could have caught
// it — a required check runs against the PR's own merge commit, not
// against the main its neighbour is about to create.
var citationRe = regexp.MustCompile("`(" + rePath + "):(\\d+)(?:-(\\d+))?`")

// citeForms are the spellings CLAUDE.md actually uses to attach an
// identifier to a citation. Only these get the second check; a citation
// in any other shape gets the mechanical half alone.
//
// NO COUNT, deliberately. This said "the three spellings" and the
// paragraph below said "the two forms or nothing" — a count in prose
// gone stale inside the guard written to stop counts in prose going
// stale, and the two disagreed with each other as well as with the
// slice. The honesty arm iterates this slice, so a fourth form is
// covered by construction rather than by somebody remembering to add a
// case. Raised in review of #475.
//
// MATCHED SYNTACTICALLY, not by proximity. An earlier version of this
// guard took the nearest backticked identifier on either side, which
// flagged `input/mouse.go:87` against `FocusManager.Dispatch` from the
// following sentence and `components/timer.go:55` against the word
// `done` — a test that cries wolf gets suppressed, so the rule is one of
// the forms below or nothing.
//
// Each carries its own field extractor rather than a shared one, because
// the identifier and the path swap group positions between them and
// deciding which is which by sniffing for ".go" is the kind of guess this
// guard exists to remove.
//
// A NAMED TYPE, because citeRange restated this field list as an
// anonymous struct and that is a second copy of a fact in the test whose
// thesis is that second copies drift. Adding a field here would have
// been a compile error there rather than a silent drift, so it was not
// the failure class this file is about — but the argument this file
// makes for building `citation` from rePath ("in a test whose entire
// subject is two copies of a fact drifting apart that was one copy too
// many") applies to it just the same. Raised in review of #475.
type citeForm struct {
	name string
	re   *regexp.Regexp
	// fields pulls (ident, path, lo, hi) out of one match; hi == lo for a
	// citation that names a single line.
	fields func([]string) (string, string, int, int)
	// sample renders a citation IN THIS FORM, and it is what lets
	// TestTheCLAUDEMDCitationGuardCatchesWhatItIsFor derive its cases from this
	// slice instead of hand-writing one per form.
	//
	// It earns its place twice over: the hand-written cases had form B
	// and form C labelled the other way round, so a failure named a
	// shape the reader could not locate — and nothing in the file defines
	// A/B/C at all, which is why the subtest name is form.name now.
	// Raised in review of #475.
	sample func(ident, path string, line int) string
}

var citeForms = []citeForm{
	// `Ident` … (`path:NNN`) — prose may sit between, but no backticks,
	// which is what keeps the identifier the one being cited.
	//
	// THE 48 IS A SILENT DEMOTION, and that is why it is written down.
	// Past 48 non-backtick characters between the identifier and its
	// citation, this form stops matching and the citation drops to the
	// MECHANICAL half alone — it still has to name a real line, but
	// nothing checks that the line holds the symbol. There is no error
	// for that, and per-form non-vacuity will not notice, because the
	// other form-A citations still match. 48 is enough for the gaps
	// CLAUDE.md actually writes (every one today is a character or two)
	// and short enough that the identifier is recognisably the subject of
	// the sentence carrying the citation; a paragraph-length gap is a
	// pairing this guard should not be guessing at. Raised in review of
	// #475.
	{
		name: "`Ident` (`path:N`)",
		re: regexp.MustCompile("`(" + reIdent + ")(?:\\([^`]*\\))?`[^`]{0,48}?\\(`(" +
			rePath + "):(\\d+)(?:-(\\d+))?`"),
		fields: identFirst,
		sample: func(ident, path string, line int) string {
			return fmt.Sprintf("`%s` (`%s:%d`) does the thing.", ident, path, line)
		},
	},
	// (`Ident`, `path:NNN`)
	{
		name:   "(`Ident`, `path:N`)",
		re:     regexp.MustCompile("\\(`(" + reIdent + ")`,\\s*`(" + rePath + "):(\\d+)(?:-(\\d+))?`"),
		fields: identFirst,
		sample: func(ident, path string, line int) string {
			return fmt.Sprintf("the sweep (`%s`, `%s:%d`) does the thing.", ident, path, line)
		},
	},
	// (`path:NNN`, in `Ident`)
	{
		name:   "(`path:N`, in `Ident`)",
		re:     regexp.MustCompile("`(" + rePath + "):(\\d+)(?:-(\\d+))?`,\\s*in\\s+`(" + reIdent + ")`"),
		fields: pathFirst,
		sample: func(ident, path string, line int) string {
			return fmt.Sprintf("the sweep (`%s:%d`, in `%s`) does the thing.", path, line, ident)
		},
	},
}

const (
	reIdent = "[A-Za-z_][A-Za-z0-9_]*(?:\\.[A-Za-z_][A-Za-z0-9_]*)*"
	rePath  = "[A-Za-z0-9_./-]+\\.(?:go|yml|yaml|md)"
)

// reIdentIsPath is rePath anchored, for the one question half two asks
// of a captured identifier: is this actually a filename? See the call
// site. It is built from rePath rather than restating the extension
// list, for the reason `citation` is.
var reIdentIsPath = regexp.MustCompile("^(?:" + rePath + ")$")

// identFirst and pathFirst DISCARD the parse failure on purpose. A line
// number half one could not parse comes back as 0, and half two's
// `lo < 1` skip drops the citation there — half one has already reported
// it, with the raw text this side no longer holds. Raised in review of
// #475.
func identFirst(m []string) (ident, path string, lo, hi int) {
	lo, _ = atoiLine(m[3])
	hi = lo
	if m[4] != "" {
		hi, _ = atoiLine(m[4])
	}
	return m[1], m[2], lo, hi
}

func pathFirst(m []string) (ident, path string, lo, hi int) {
	lo, _ = atoiLine(m[2])
	hi = lo
	if m[3] != "" {
		hi, _ = atoiLine(m[3])
	}
	return m[4], m[1], lo, hi
}

// citeWindow is how far from the cited line the identifier may sit. Small
// on purpose: the point of a line number is that it is precise, and a
// window wide enough to always find the symbol is a window that has
// stopped checking anything.
const citeWindow = 3

// citationProblems is the check itself, over a document and a way to
// read the files it cites. It is separate from the test so the guard can
// be pointed at a SYNTHETIC document with a known defect —
// TestTheCLAUDEMDCitationGuardCatchesWhatItIsFor below. Without that arm, a
// widened citeWindow or a downgraded error silently turns the whole
// thing into a no-op that still reports PASS.
//
// It returns descriptions rather than calling t.Errorf so both callers
// can decide what a problem means: one requires none, the other requires
// some.
//
// AND HOW MANY CITATIONS REACHED HALF TWO, which is the answer to a
// question nothing else in this file can ask. A citation drops from the
// identifier check to the mechanical half for reasons that are all
// silent: a backtick inside form A's 48-character gap, a gap longer than
// 48, a respelling that no form matches. What is left — "the file exists
// and is long enough" — is no check at all for a real repo file, and
// per-form non-vacuity cannot see it, because a dozen other form-A
// citations still match and the form is still reported exercised.
//
// Measured: changing CLAUDE.md's `MeasureChild` citation to
// "(see `layout.go`, at `layout.go:1`)" — which cites `package gooey`
// for MeasureChild, a citation as wrong as any this PR corrects — left
// the whole suite green. A policy number can only be pinned by its
// value, which is the argument this branch already accepted for
// citeWindow. Raised in review of #475.
func citationProblems(md string, read func(string) ([]string, error)) (problems, forms []string, checked int) {
	lines := map[string][]string{}
	src := func(path string) []string {
		if s, ok := lines[path]; ok {
			return s
		}
		s, err := read(path)
		if err != nil {
			// The nil IS the record — a second citation to the same
			// unreadable file finds it cached and reports again, which is
			// right: each citation is its own problem and names its own
			// line. A `missing` set beside this was written and never
			// read. Raised in review of #475.
			lines[path] = nil
			return nil
		}
		lines[path] = s
		return s
	}
	// code is src with every block-comment span blanked, memoised
	// separately so the reports above keep quoting the file as written.
	// It is computed per FILE and not per window, for the reason
	// blockFree's own doc gives: a window is a slice, and a span opened
	// above it is invisible from inside.
	blanked := map[string][]string{}
	code := func(path string) []string {
		if s, ok := blanked[path]; ok {
			return s
		}
		s := blockFree(src(path))
		blanked[path] = s
		return s
	}

	// ---- half one: every citation names a real place ----
	for _, m := range citationRe.FindAllStringSubmatch(md, -1) {
		path := m[1]
		// HALF ONE OWNS EVERY MALFORMED LINE NUMBER, because it is the
		// only place holding the RAW text — `fields` has already
		// converted by the time half two sees one. Each of the three
		// arms below was a panic first: a checker whose job is reporting
		// problems should report its own inputs rather than take the
		// root suite down with a stack trace naming no citation.
		lo, loOK := atoiLine(m[2])
		hi, hiOK := lo, true
		if m[3] != "" {
			hi, hiOK = atoiLine(m[3])
		}
		// A DIGIT RUN TOO LONG FOR AN int. The pattern matches \d+, so
		// `file.go:99999999999999999999` is a citation as far as the
		// regexp is concerned; mustAtoi panicked on it with "produced a
		// non-numeric line", whose own message argued it could not
		// happen. Raised in review of #475.
		if !loOK || !hiOK {
			problems = append(problems, fmt.Sprintf(
				"cites %s:%s, and that is not a line number — it does not fit in an "+
					"int, so nothing in the file can be at it", path, m[2]+dashed(m[3])))
			continue
		}
		// LINE 0 IS NOT A LINE, and half two indexes s[lo-1] to quote it
		// — so `file.go:0` panicked with index out of range instead of
		// being reported. Loud rather than silent, but a checker whose
		// job is reporting problems should report this one. Raised in
		// review of #475.
		if lo < 1 {
			problems = append(problems, fmt.Sprintf(
				"cites %s:%d, and there is no line 0 — lines are counted from 1, "+
					"so this addresses nothing", path, lo))
			continue
		}
		// A RANGE THAT RUNS BACKWARDS. Half two slices
		// s[lo-1-citeWindow : hi+citeWindow], so `prop.go:120-100` was a
		// slice bounds panic — the same class as line zero, one more
		// spelling. Raised in review of #475.
		if hi < lo {
			problems = append(problems, fmt.Sprintf(
				"cites %s:%d-%d, which runs backwards; a range names its first line "+
					"then its last", path, lo, hi))
			continue
		}
		s := src(path)
		if s == nil {
			problems = append(problems, fmt.Sprintf(
				"cites %s:%d, and there is no such file. A bare `spinner.go:113` "+
					"for `components/spinner.go:113` reads as a path and resolves "+
					"to nothing.", path, lo))
			continue
		}
		if hi > len(s) {
			problems = append(problems, fmt.Sprintf(
				"cites %s:%d but the file has %d lines; the citation outlived what "+
					"it pointed at", path, hi, len(s)))
		}
	}

	// ---- half two: where the doc names a symbol, the line holds it ----
	seen := map[string]bool{}
	for _, form := range citeForms {
		hits := 0
		for _, m := range form.re.FindAllStringSubmatch(md, -1) {
			ident, path, lo, hi := form.fields(m)
			hits++
			// THE IDENTIFIER IS PART OF THE KEY. Keyed on path:line
			// alone, a second identifier attached to the same cited
			// location was dropped unchecked — "`Alpha` (`fake.go:3`) and
			// also (`Beta`, `fake.go:3`)" reported no problem though Beta
			// is nowhere in the file. Latent rather than live (CLAUDE.md
			// has no such collision today: 21 identifier-checked
			// citations, zero dedup skips), which is exactly the shape
			// this test exists to catch — a guard that passes while
			// checking less than it reports. Safe for non-vacuity because
			// hits++ happens above. Raised in review of #475.
			key := path + ":" + strconv.Itoa(lo) + " " + ident
			if seen[key] {
				continue
			}
			seen[key] = true

			s := src(path)
			if s == nil || lo < 1 || hi < lo || hi > len(s) {
				continue // half one already reported it
			}
			// A FILENAME IS NOT THE CITED SYMBOL. reIdent matches
			// `startable.go` — `startable`, then `.go` is a legal dotted
			// segment — so form A bound a backticked FILENAME sitting
			// within its 48-character gap as the identifier and the leaf
			// became "go". Both outcomes are wrong depending on the
			// neighbourhood: a spurious rejection naming a nonsense
			// symbol, which is the cry-wolf this guard rejected
			// proximity matching to avoid, or a vacuous accept anywhere
			// a `go func(` or a `go` keyword sits inside the window.
			//
			// Latent — no citation pairs with a filename today — but
			// CLAUDE.md writes backticked filenames beside parenthesised
			// citations throughout, so it is one edit away rather than
			// hypothetical.
			//
			// The test is whether the ident is PATH-SHAPED, not whether
			// its last segment is an extension: a Go symbol cannot be
			// named `go`, which is a keyword, but `cfg.yaml` would pass
			// a segment test and is a filename every time it appears in
			// this document. The citation keeps half one; what it loses
			// is an identifier check it was never entitled to. Raised in
			// review of #475.
			if reIdentIsPath.MatchString(ident) {
				continue
			}
			// The LEAF of a dotted name: the doc writes `Composer.Frame`
			// and the file writes `func (c *Composer) Frame(`.
			leaf := ident
			if i := strings.LastIndex(ident, "."); i >= 0 {
				leaf = ident[i+1:]
			}
			// THE WINDOW IS AROUND lo, ON BOTH SIDES. It ran to
			// hi+citeWindow, so a range citation carried its own span
			// INSIDE the window and `composer.go:442-479` would accept
			// the identifier anywhere across 37 lines plus the window at
			// each end — a citation that names a region is exactly the
			// one whose drift is hardest to see by eye, and it was the
			// one this pin stopped reaching. hi is the range's other
			// end and belongs to the validity check above, not here.
			// Raised in review of #475.
			from, to := max(0, lo-1-citeWindow), min(len(s), lo+citeWindow)
			// A WORD BOUNDARY, not a substring. `Settable` contains
			// `Set`, so a window whose only code is
			// `func (p *Property[T]) Settable() bool` satisfied a
			// citation for prop.Set — the same fail-open as the prose
			// case one layer down, satisfied by a DIFFERENT SYMBOL that
			// happens to contain the leaf. It is not hypothetical here:
			// prop/prop.go puts Settable at 114 and Set at 117, three
			// lines apart, so an edit that moves Set out of the window
			// and leaves Settable in it keeps this green while pointing
			// at the wrong symbol.
			//
			// This repo has the bug's twin on the record with the same
			// fix — reviewprompt_test.go: "NOTE" contains "NOT". Raised
			// in review of #475.
			checked++
			if !identRe(leaf).MatchString(codeOnly(code(path)[from:to])) {
				problems = append(problems, fmt.Sprintf(
					"cites %s:%d for %s, but %q is nowhere within %d lines of it — "+
						"line %d holds %q. Any edit above a cited line moves it, so "+
						"the citation rots without anyone touching what it describes.",
					path, lo, ident, leaf, citeWindow, lo, strings.TrimSpace(s[lo-1])))
			}
		}
		if hits > 0 {
			forms = append(forms, form.name)
		}
	}
	return problems, forms, checked
}

// TestABacktickedFilenameIsNotTheCitedIdentifier is finding 3 of review
// #475, and it is asserted through `checked` rather than through the
// absence of a problem.
//
// reIdent matches `startable.go` — `startable`, then `.go` is a legal
// dotted segment — so form A bound a backticked FILENAME sitting inside
// its 48-character gap as the cited identifier and the leaf became "go".
// Which way that goes wrong depends on the neighbourhood: a spurious
// rejection naming a nonsense symbol, or a vacuous accept anywhere a
// `go func(` or a `go` keyword falls inside the window. CLAUDE.md writes
// backticked filenames beside parenthesised citations throughout, so it
// is one edit away rather than hypothetical.
//
// "NO PROBLEM REPORTED" WOULD NOT BE THE ASSERTION. That passes for two
// reasons — the skip working, and the leaf happening to appear near the
// cited line — and the second is exactly the vacuous accept this is
// about. What the test asks instead is the pair: the form MATCHED, and
// NO citation reached the identifier check. Only the skip produces both.
func TestABacktickedFilenameIsNotTheCitedIdentifier(t *testing.T) {
	// "go" is on the cited line, so a leaf of "go" would be ACCEPTED
	// here — this fixture is the vacuous-accept shape, not the
	// spurious-rejection one, and it is the half a problem count cannot
	// see.
	src := []string{
		"package fake",
		"func Alpha() {",
		"\tgo func() {}()",
		"}",
	}
	read := func(path string) ([]string, error) {
		if path != "fake.go" {
			return nil, fs.ErrNotExist
		}
		return src, nil
	}

	md := "`fake.go` (`fake.go:3`) does the thing."
	problems, forms, checked := citationProblems(md, read)
	if len(forms) == 0 {
		t.Fatalf("no form matched %q, so this arm is about nothing — the "+
			"filename never became a candidate identifier and the skip below "+
			"cannot be what produced the result", md)
	}
	if checked != 0 {
		t.Errorf("%d citation(s) in %q reached the identifier check; a backticked "+
			"FILENAME is not the cited symbol, and binding it makes the leaf "+
			"%q — which this fixture puts on the cited line, so the guard "+
			"approves and reports having checked something it never had", checked, md, "go")
	}
	if len(problems) > 0 {
		t.Errorf("the guard rejected %q: %v. Half one is satisfied — fake.go:3 "+
			"exists — and half two has nothing to say about a citation that "+
			"names no symbol", md, problems)
	}
}

// citeFixturePrefix is the name TestTheProductionReaderCountsRealLines
// gives its fixture directory in the module root, and the thing
// .gitignore has to cover.
const citeFixturePrefix = "citelines"

// TestTheFixtureLeftoverIsIgnored is the guard on the pair above.
//
// The fixture is a real Go package in the module root by design — the
// citation grammar cannot match a Windows temp path — and t.Cleanup does
// not run when the binary dies on another test's panic or a -timeout
// kill. .gitignore covers it, and a rename of the prefix would silently
// stop being covered: the leftover only appears after an interrupted
// run, which is exactly when nobody is looking.
func TestTheFixtureLeftoverIsIgnored(t *testing.T) {
	b, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	// The name an actual run leaves behind, not the prefix: os.MkdirTemp
	// appends digits, so a pattern covering the prefix alone would not
	// cover the directory.
	leftover := citeFixturePrefix + "1234567890"
	var covered bool
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pat := strings.TrimSuffix(line, "/")
		if ok, err := path.Match(pat, leftover); err == nil && ok {
			covered = true
			break
		}
	}
	if !covered {
		t.Errorf("no .gitignore pattern matches %q, which is what "+
			"TestTheProductionReaderCountsRealLines leaves in the module root when "+
			"the binary dies before its t.Cleanup runs — a real `package p` that "+
			"`go build ./...` compiles and `git status` reports", leftover)
	}
}

// citeRange renders a RANGE citation in a form that only knows how to
// render a single line.
//
// It rewrites the form's own sample rather than carrying a second
// renderer, so the range spelling cannot drift from the single-line one
// — and the Fatalf is what keeps the rewrite honest: a form whose sample
// stops writing `path:N` in backticks fails loudly here instead of
// silently producing a document the guard does not recognise, which this
// arm would then report as an accept. Raised in review of #475.
func citeRange(t *testing.T, form citeForm, ident, path string, lo, hi int) string {
	t.Helper()
	md := form.sample(ident, path, lo)
	old := fmt.Sprintf(":%d`", lo)
	if !strings.Contains(md, old) {
		t.Fatalf("%s renders %q, which does not spell the line as %q — this helper "+
			"cannot turn it into a range, and the arm below would test a document "+
			"the guard does not match", form.name, md, old)
	}
	return strings.Replace(md, old, fmt.Sprintf(":%d-%d`", lo, hi), 1)
}

// accepts is an ACCEPT arm with its own floor, and the floor is the
// point.
//
// "The guard reported no problem" is satisfied twice over: by a guard
// that examined the citation and approved it, and by a guard that never
// recognised the citation at all. Every accept arm here was the first
// reading and could have been the second — respell a form on both sides,
// in citeForms, and its accept arms go on passing while testing nothing.
// That is what finding 7 of review #475 is about, and deriving the
// citation from form.sample is only half of it: the other half is asking
// whether a form matched. Raised in review of #475.
func accepts(t *testing.T, md string, read func(string) ([]string, error), why string) {
	t.Helper()
	problems, forms, _ := citationProblems(md, read)
	if len(forms) == 0 {
		t.Fatalf("the guard matched no citation form in %q, so it approved by not "+
			"looking. This arm asserts an ACCEPT, which is exactly what a guard "+
			"that recognises nothing reports", md)
	}
	if len(problems) > 0 {
		t.Errorf("the guard rejected %q, where %s: %v", md, why, problems)
	}
}

// citedDocs are the documents whose `file:line` citations are checked.
//
// WHY THE LIST STOPS HERE, since "one more filename" is the whole cost
// of adding to it. README.md and docs/architecture.md carry no
// `file:line` citation at all. The other sixty-odd live under
// docs/specs/, which are DATED decision records describing the tree as
// it was on the day they were written — a citation there going stale is
// the record staying honest about its own moment, and a guard demanding
// they track the tree would be asking a history to be a reference.
//
// markup-reference.md joined after review of #475 pointed out it had
// just had a citation corrected by hand and was free to rot again the
// same way, and LEFT in review of #490, which converted its last one to
// a symbol — `markup.textSource` cannot rot the way `markup.go:1976`
// had by then already rotted, twice. A document with no citations
// contributes nothing here, and the check below is what stops that
// leaving a hole: a doc that gains one and is not listed here is an
// error, so rejoining is not something anyone has to remember.
var citedDocs = []string{claudeMD}

// TestEveryCitingDocumentIsChecked closes the list above.
//
// citedDocs is hand-written, and the guard it feeds fails when a listed
// document carries NO citation — the opposite direction, a document that
// cites and is not listed, was unwatched. That is the state
// docs/markup-reference.md would be in tomorrow if someone added a
// `file:line` back to it, which is exactly how it got here: it was
// unwatched, rotted, was corrected by hand, and only then was listed.
//
// docs/specs/ is out for the reason citedDocs gives: a dated record's
// citation describes the tree on its own date. Raised in review of #490.
func TestEveryCitingDocumentIsChecked(t *testing.T) {
	listed := map[string]bool{}
	for _, d := range citedDocs {
		listed[filepath.ToSlash(d)] = true
	}
	var checked int
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(path)
		if d.IsDir() {
			switch {
			case path == ".":
				return nil
			case strings.HasPrefix(d.Name(), "."), d.Name() == "vendor",
				slash == "docs/specs", slash == "presentations":
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".md" || listed[slash] {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		if m := citationRe.FindString(string(b)); m != "" {
			t.Errorf("%s carries the citation %s and is not in citedDocs, so nothing "+
				"checks that the line still holds what it names. Add it to citedDocs "+
				"and raise wantIdentChecked, or write the symbol instead of the line",
				slash, m)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	// The walk reaching nothing would make the check above vacuous, and
	// it is a prune away from that: docs/ alone holds dozens.
	if checked < 10 {
		t.Errorf("the walk read %d unlisted markdown files, which is too few for "+
			"this tree — a prune is eating the documents", checked)
	}
}

func TestCLAUDEMDCitationsResolve(t *testing.T) {
	// ONE FORM COVERAGE SET ACROSS ALL THE DOCUMENTS, not one per
	// document: the forms are properties of the CHECKER, and requiring
	// every document to exercise every form would fail the day a
	// reference stops using a spelling CLAUDE.md still uses.
	var forms []string
	// The TOTAL is what separates "the pattern rotted" from "one
	// document stopped citing": no citation anywhere means the regexp no
	// longer matches the spelling every document uses.
	cited := 0
	identChecked := 0
	for _, doc := range citedDocs {
		b, err := os.ReadFile(doc)
		if err != nil {
			t.Fatalf("reading %s: %v", doc, err)
		}
		md := string(b)
		// NAMES BOTH CAUSES, and the fatal moved to where the first one
		// can actually be told apart. A document with no citation is
		// either the pattern having drifted OR the document's last
		// citation having been legitimately removed, and blaming the
		// pattern for the second sends the reader to the regexps.
		// markup-reference.md carries exactly one, so that is a live
		// possibility rather than a hypothetical. Raised in review of
		// #475.
		n := len(citationRe.FindAllString(md, -1))
		cited += n
		if n == 0 {
			t.Errorf("%s carries no `file:line` citation, so it contributes nothing "+
				"to this test. Either its last citation was removed — in which case "+
				"drop it from citedDocs — or the pattern has drifted from the "+
				"document. The other cited documents say which: if they still match, "+
				"it is the first.", doc)
		}

		problems, got, n := citationProblems(md, readLines)
		for _, p := range problems {
			t.Errorf("%s %s (#466)", doc, p)
		}
		forms = append(forms, got...)
		identChecked += n
	}

	if cited == 0 {
		t.Fatal("no cited document carries a `file:line` citation at all, so this " +
			"test checks nothing. One document going quiet is a document; all of " +
			"them is the pattern")
	}

	// NON-VACUITY, per FORM rather than as a fraction: a ratio would be
	// the "number in prose" this file argues against, and it would pass
	// while one form's regexp quietly matched nothing.
	for _, f := range citeForms {
		if !slices.Contains(forms, f.name) {
			t.Errorf("no citation in %v matches the form %s, so that pattern is "+
				"checking nothing. If the docs genuinely stopped using the form, "+
				"delete it here — but first check the regexp has not simply rotted "+
				"against a change in the prose around it.", citedDocs, f.name)
		}
	}

	// HOW MANY CITATIONS REACHED THE IDENTIFIER CHECK, pinned by VALUE.
	//
	// A citation drops to the mechanical half — "the file exists and is
	// long enough", which for a real repo file is no check at all —
	// whenever no form matches it, and every way that happens is silent:
	// a backtick inside form A's 48-character gap, a gap longer than 48,
	// a respelling. Nothing above notices, because a dozen other form-A
	// citations still match and the form is still reported exercised.
	//
	// Measured on this branch: rewriting CLAUDE.md's MeasureChild
	// citation as "(see `layout.go`, at `layout.go:1`)" — which cites
	// `package gooey` for MeasureChild — left the entire suite green.
	// That is the hole #466 was filed for, reopened by a reword.
	//
	// So the number is shipped, and this file has already accepted the
	// argument for citeWindow: a policy number can only be pinned by its
	// value. Moving it is a decision somebody makes in a failing test
	// rather than a side effect of editing a sentence — UP when a
	// citation gains an identifier, DOWN only with a reason. Raised in
	// review of #475.
	if identChecked != wantIdentChecked {
		t.Errorf("%d citations reached the identifier check across %v, want %d. "+
			"A citation that no form matches keeps only the mechanical half — it "+
			"must name a real line, and nothing checks that the line holds the "+
			"symbol. If you added a citation with an identifier, raise the "+
			"constant; if this went DOWN, a citation was reworded out of every "+
			"form and is now unchecked in the way #466 is about.",
			identChecked, citedDocs, wantIdentChecked)
	}
}

// wantIdentChecked is how many citations across citedDocs reach half
// two, and it is a VALUE rather than a floor for the reason the
// assertion above gives: a >= would let a demotion hide behind an
// addition in the same commit.
const wantIdentChecked = 21

// TestTheCLAUDEMDCitationGuardCatchesWhatItIsFor points the guard at documents
// whose defects are known, and is the arm that keeps the guard honest.
//
// A checker like this fails OPEN in every direction that matters: widen
// citeWindow and every identifier is "near" its line; make a form's
// regexp match nothing and that shape stops being checked; downgrade the
// missing-file report and a bare filename sails through. None of those
// shows up against a document that is already correct — the real
// CLAUDE.md passes just as well with the check disabled.
//
// So each case here is a document the guard MUST reject.
func TestTheCLAUDEMDCitationGuardCatchesWhatItIsFor(t *testing.T) {
	// THE WINDOW IS PINNED BY VALUE, and that is finding 2 of review
	// #475 — including the half my first fix got wrong.
	//
	// The fixture put `Alpha` at line 3 and the drifted citation at line
	// 21 — EIGHTEEN lines apart — so the arm only tripped once the window
	// reached 18. Measured: citeWindow could be raised from 3 to 17, a
	// 35-line neighbourhood in a repo where almost nothing is 35 lines
	// long, with BOTH tests reporting PASS.
	//
	// Deriving the fixture from citeWindow does NOT fix that, which I
	// measured before believing it: the drift moves with the constant, so
	// every widening stays green. A policy number can only be pinned by
	// its VALUE. That is not the "count in prose" this file argues
	// against — the opposite: it is a number asserted against the code
	// that owns it, so raising it is a decision somebody has to make in
	// this test's failure message rather than a constant somebody edits.
	//
	// The two arms below pin the MECHANISM, derived, so the reach cannot
	// change shape underneath the value: an identifier exactly citeWindow
	// away is accepted, one line further is not.
	const shippedWindow = 3
	if citeWindow != shippedWindow {
		t.Fatalf("citeWindow is %d, not %d. Widening it is a policy change, not a "+
			"tuning: at %d the check reads a %d-line neighbourhood, and almost no "+
			"function in this repo is that long — past which \"the line holds the "+
			"symbol\" degrades into \"the symbol is somewhere nearby\" while both "+
			"tests still report PASS. If the widening is deliberate, change this "+
			"number and say why in the constant's comment.",
			citeWindow, shippedWindow, citeWindow, 2*citeWindow+1)
	}

	// The check reads lines [lo-1-citeWindow, hi+citeWindow), so an
	// identifier at identLine is inside the window exactly while the
	// citation sits at or below identLine+citeWindow.
	const identLine = 3
	edgeLine := identLine + citeWindow
	driftLine := edgeLine + 1

	src := make([]string, driftLine+1)
	src[0] = "package fake"
	src[identLine-1] = "func Alpha() {"
	src[identLine] = "}"
	src[driftLine-1] = "func Beta() {"
	src[driftLine] = "}"
	// Everything between is filler that mentions no identifier, so the
	// only thing deciding the two arms below is the distance.
	for i := identLine + 1; i < driftLine-1; i++ {
		src[i] = "// filler"
	}
	// AND A FILE WHOSE ONLY MENTION OF Alpha IS PROSE, four lines above
	// the cited one — the exact shape of CLAUDE.md's `prop.Set` ->
	// `prop/prop.go:101` citation, which pointed at the tail of a
	// neighbouring doc comment and passed. The window has to reach the
	// comment for this to be a test of comment-stripping rather than of
	// distance, so line 5 is cited and the sentence is on line 2.
	commented := []string{
		"package fake",
		"// Alpha reports whether the thing is legal.",
		"func Beta() {",
		"}",
		"func Gamma() {}",
	}
	// AND A FILE LONG ENOUGH FOR A REVERSED RANGE TO REACH THE SLICE.
	// Half two computes s[lo-1-citeWindow : hi+citeWindow], which only
	// inverts when lo exceeds hi by more than 2*citeWindow+1 — so
	// `fake.go:7-3` in an eight-line fixture is backwards as a CITATION
	// and still a valid slice (s[3:6]). It exercises half one's report
	// and says nothing about half two's skip; the two are separate
	// defences against the same malformed input and want separate
	// fixtures. Raised in review of #475.
	long := make([]string, 40)
	for i := range long {
		long[i] = "// filler"
	}
	long[0] = "package fake"
	long[19] = "func Alpha() {"
	// AND A FILE WHOSE CITED LINE HOLDS A SINGLE SLASH, in code. This is
	// the accept arm for codeOnly: it must cut at // and not at /, or a
	// division — or a path in a string literal — takes the identifier
	// after it with the comment text and the guard rejects a correct
	// citation. Cutting too eagerly is silent in every rejection arm
	// above, because they all want a rejection anyway.
	divided := []string{
		"package fake",
		"func Beta(w int) int {",
		"\treturn w / Alpha()",
		"}",
	}
	// AND A FILE WHOSE ONLY MENTION OF THE LEAF IS INSIDE A LONGER
	// IDENTIFIER. This is the arm for the word-boundary match, and the
	// commented.go arm cannot stand in for it: that one tests
	// comment-stripping, and here the text IS code. It mirrors
	// prop/prop.go, where Settable and Set sit three lines apart.
	longer := []string{
		"package fake",
		"func Settable() bool { return true }",
		"func Beta() {",
		"\tx := SetString(1)",
		"}",
	}
	// AND A FILE WHOSE ONLY MENTION IS INSIDE A BLOCK COMMENT, SPANNING
	// THE WINDOW. The span opens above the cited line and closes below
	// it, which is the case a per-window strip cannot see: from inside
	// the slice there is no /* and no */, so every line looks like code.
	blocked := []string{
		"package fake",
		"/*",
		"Alpha is the thing this file used to have.",
		"It has three more lines of prose about it.",
		"*/",
		"func Beta() {}",
	}
	files := map[string][]string{
		"fake.go":      src,
		"commented.go": commented,
		"long.go":      long,
		"divided.go":   divided,
		"longer.go":    longer,
		"blocked.go":   blocked,
	}
	read := func(path string) ([]string, error) {
		s, ok := files[path]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return s, nil
	}

	// PER FORM, ITERATED, so a fourth form is covered the day it is
	// added. The three cases here were hand-written, and two of them
	// carried the WRONG LABEL — form B and form C the other way round,
	// naming a shape nothing in the file defines. The name comes from
	// form.name now, and the document from form.sample.
	for _, form := range citeForms {
		t.Run("a drifted line, "+form.name, func(t *testing.T) {
			md := form.sample("Alpha", "fake.go", driftLine)
			problems, forms, _ := citationProblems(md, read)
			// THIS FORM, not "some form". The sample only ever renders
			// one, so `len(forms) == 0` was a weaker question than the
			// loop is asking: a form whose regexp rotted would fall
			// through to the mechanical half, another form would still
			// be reported for some other citation in a longer document,
			// and the arm would pass while the form it names went
			// unchecked. Raised in review of #475.
			if !slices.Contains(forms, form.name) {
				t.Fatalf("the guard matched %v in %q and not %s itself, so the "+
					"rejection below is about the MECHANICAL half and this arm "+
					"passes without the identifier check ever running on this form",
					forms, md, form.name)
			}
			if len(problems) == 0 {
				t.Errorf("the guard accepted %q, where %q is %d lines from the cited "+
					"line and citeWindow is %d. It reports PASS against a correct "+
					"document whether or not it is checking anything, so an arm that "+
					"cannot fail here is a guard that has stopped working.",
					md, "Alpha", driftLine-identLine, citeWindow)
			} else if got := strings.Join(problems, "\n"); !strings.Contains(got, "is nowhere within") {
				// THE REPORT HAS TO BE THE ONE THIS ARM IS ABOUT, which
				// is the discipline the table and the dup arm already
				// carry: "a rejection arm that only counts problems
				// passes when a DIFFERENT arm of the checker fires."
				// These three are the per-form guarantee that replaced
				// the hand-written cases, so they need it most — and
				// they were fail-open only by accident, because fake.go
				// happens to be driftLine+1 lines long. One line shorter
				// and every drift arm would have passed on an
				// out-of-range report from half ONE. Raised in review of
				// #475.
				t.Errorf("the guard rejected %q, but not for the reason this arm "+
					"is about — it reported %q, and the identifier check reports "+
					"\"is nowhere within\". A rejection arm that counts problems "+
					"passes when a different half of the checker fires", md, got)
			}
		})

		// AND THE OTHER DIRECTION, per form: "reject everything" must not
		// be a passing strategy for any of them, and a form whose regexp
		// rotted would otherwise look identical to a form that works.
		t.Run("the true line, "+form.name, func(t *testing.T) {
			md := form.sample("Alpha", "fake.go", identLine)
			if problems, _, _ := citationProblems(md, read); len(problems) > 0 {
				t.Errorf("the guard rejected the CORRECT citation %q: %v", md, problems)
			}
		})
	}

	// THE EDGE, both sides of it. These pin the reach as a mechanism
	// rather than as a number: exactly citeWindow away is inside, one
	// more is not. Without the accept arm, "reject anything not on the
	// cited line" would satisfy every rejection arm above.
	//
	// PER FORM, for the same reason the two arms above are. Hardcoded to
	// citeForms[0] this pinned one form's reach and left a fourth form's
	// unpinned the day it was added, and reordering the slice moved the
	// arm to a different form without saying so — the drift arms were
	// iterated for exactly that reason and this one was not. It also
	// stood inside `if md := …; true {`, a constant condition doing the
	// work of a plain statement. Raised in review of #475.
	// AND A CITED LINE WITH A SLASH IN IT, accepted. codeOnly strips
	// COMMENTS; cutting at the first / instead would take `w / Alpha()`
	// down to `w ` and reject a citation that is exactly right.
	//
	// DERIVED FROM citeForms[0], not spelled out. It hand-wrote form A's
	// citation, so reordering the slice or changing that form's spelling
	// left this arm testing a shape the guard no longer recognises —
	// which it would report as an ACCEPT, silently, since that is what
	// this arm asserts. Raised in review of #475.
	t.Run("the identifier after a division on the cited line", func(t *testing.T) {
		md := citeForms[0].sample("Alpha", "divided.go", 3)
		accepts(t, md, read, "`Alpha` is on the cited line in CODE — the comment "+
			"stripping has taken real source with it")
	})

	// AND A RANGE THAT DOES CITE ITS DECLARATION, accepted — otherwise
	// "reject every range" satisfies the arm above, and CLAUDE.md cites
	// several (composer.go:442-479 among them).
	//
	// DERIVED FROM citeForms[0], for the same reason the arm above it
	// is: this was the last hand-written copy of form A's spelling, so
	// reordering citeForms or respelling that form left this arm
	// exercising a shape the guard no longer recognises — reported as an
	// ACCEPT, which is what the arm asserts. citeRange renders the
	// range, since sample takes one line. Raised in review of #475.
	t.Run("a range starting at the identifier", func(t *testing.T) {
		md := citeRange(t, citeForms[0], "Alpha", "long.go", 20, 40)
		accepts(t, md, read,
			"`Alpha` is ON the cited start line — a range citation names where a "+
				"thing begins, and refusing them all is not a tighter window, it "+
				"is a broken one")
	})

	for _, form := range citeForms {
		t.Run("the window's edge, "+form.name, func(t *testing.T) {
			md := form.sample("Alpha", "fake.go", edgeLine)
			accepts(t, md, read, fmt.Sprintf("`Alpha` is exactly citeWindow (%d) "+
				"lines from the cited line — the window does not reach as far as "+
				"it says", citeWindow))
		})
	}

	// `wants` is a substring the REPORT has to carry, and it is here
	// because "some problem was reported" is a weaker claim than these
	// arms look like they make. Every malformed line number ends up in
	// one arm or another of half one, so deleting the too-big-for-an-int
	// arm leaves `:99999999999999999999` falling through to the line-zero
	// arm — still reported, still green, and now telling the reader
	// "there is no line 0" about a citation that says nothing of the
	// kind. Naming the expected report is what tells the arms apart.
	// Raised in review of #475.
	for _, tc := range []struct{ name, md, wants string }{
		{"a file that does not exist", "`Alpha` (`nosuch.go:3`) does the thing.",
			"no such file"},
		{"a line past the end of the file", "`Alpha` (`fake.go:900`) does the thing.",
			"the file has"},
		// LINE 0 panicked rather than reporting, because half two quotes
		// s[lo-1] in its message. Raised in review of #475.
		{"line zero, which is not a line", "`Alpha` (`fake.go:0`) does the thing.",
			"there is no line 0"},
		// AND THE OTHER TWO PANICS, same class, found in the same
		// review. A reversed range slices s[lo-1-citeWindow:hi+citeWindow]
		// backwards; a digit run past math.MaxInt reaches an Atoi that
		// used to panic with a message arguing it could not happen.
		// Both took the whole root suite down with a stack trace naming
		// no citation.
		{"a range that runs backwards", "`Alpha` (`fake.go:7-3`) does the thing.",
			"runs backwards"},
		{"a range backwards by more than the window",
			"`Alpha` (`long.go:30-4`) does the thing.", "runs backwards"},
		{"a line number too big for an int",
			"`Alpha` (`fake.go:99999999999999999999`) does the thing.",
			"99999999999999999999"},
		// THE IDENTIFIER IS THERE, BUT ONLY IN A COMMENT — the citation
		// that shipped in this very file. Without codeOnly the window
		// contains "Alpha" and the guard reports no problem, so this arm
		// is the difference between checking a citation and checking a
		// neighbourhood's prose.
		{"the identifier only in prose near the line",
			"`Alpha` (`commented.go:5`) does the thing.",
			"is nowhere within"},
		// A RANGE THAT REACHES THE IDENTIFIER WITHOUT CITING IT. long.go
		// declares Alpha at line 20, and this cites 1-40 — so the whole
		// declaration is inside the range and 19 lines from the line
		// named. The window ran to hi+citeWindow, which put the range's
		// own span inside the reach it is supposed to bound: a citation
		// naming a region could not fail this check whatever it named,
		// and a region is exactly what a reader cannot verify by eye.
		// Raised in review of #475.
		{"a range whose start is nowhere near the identifier",
			"`Alpha` (`long.go:1-40`) does the thing.",
			"is nowhere within"},
		// THE LEAF INSIDE A LONGER IDENTIFIER, in code. `Settable`
		// contains `Set` and `SetString` contains it too, so a substring
		// match satisfied a citation for prop.Set with a window holding
		// neither. Two arms, one either side of the cited line, because
		// a prefix and a suffix are different failures of the same
		// match. Raised in review of #475.
		{"the leaf only as the prefix of a longer identifier",
			"`Set` (`longer.go:3`) does the thing.", "is nowhere within"},
		{"the leaf only inside a longer identifier",
			"`prop.Set` (`longer.go:3`) does the thing.", "is nowhere within"},
		// THE LEAF ONLY INSIDE A BLOCK COMMENT that spans the window.
		// Same fail-open as the // case, one spelling out, and the span
		// deliberately opens above the cited line and closes below it —
		// the shape a per-window strip cannot see at all.
		{"the identifier only inside a block comment",
			"`Alpha` (`blocked.go:3`) does the thing.", "is nowhere within"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems, _, _ := citationProblems(tc.md, read)
			if len(problems) == 0 {
				t.Errorf("the guard accepted %q. It reports PASS against a correct "+
					"document whether or not it is checking anything, so an arm "+
					"that cannot fail here is a guard that has stopped working.",
					tc.md)
				return
			}
			if !strings.Contains(strings.Join(problems, "\n"), tc.wants) {
				t.Errorf("the guard rejected %q, but for the wrong reason: no report "+
					"mentions %q.\n\t%v\nA rejection arm that only counts problems "+
					"passes when a DIFFERENT arm of the checker fires, so the two "+
					"are indistinguishable and either can be deleted unnoticed.",
					tc.md, tc.wants, problems)
			}
		})
	}

	// A SECOND IDENTIFIER AT ONE CITED LINE. seen was keyed on
	// path:line, and set before the check, so this reported nothing
	// though Beta is nowhere near line 3. Latent in CLAUDE.md today,
	// which is the shape this whole test exists to catch: a guard that
	// passes while checking less than it reports. Raised in review of
	// #475.
	dup := "`Alpha` (`fake.go:3`) and also (`Beta`, `fake.go:3`) which is wrong."
	// THE SAME wants DISCIPLINE AS THE TABLE ABOVE, and it was missing
	// here alone. `len(problems) == 0` passes when a DIFFERENT arm of
	// the checker fires — and this document cites the same file and line
	// twice, so several could. The report has to name Beta, which is the
	// identifier the dedup was swallowing. Raised in review of #475.
	problems, _, _ := citationProblems(dup, read)
	if len(problems) == 0 {
		t.Errorf("the guard accepted %q. The second identifier on one cited line "+
			"was deduplicated away unchecked, so a wrong citation hides behind a "+
			"right one at the same location", dup)
	} else if joined := strings.Join(problems, "\n"); !strings.Contains(joined, "Beta") {
		t.Errorf("the guard rejected %q, but no report names Beta:\n\t%v\nThe "+
			"rejection is about something else, so this arm would pass with the "+
			"dedup fix reverted", dup, problems)
	}
}

// TestTheProductionReaderCountsRealLines is the coverage readLines never
// had, and the reason finding 1 of review #475 was invisible.
//
// citationProblems is handed a reader so the honesty arm can drive it
// with a slice literal — which means the arm exercises everything EXCEPT
// the function the real test uses. strings.Split on a newline-terminated
// file yields a trailing "" that is not a line, so len(s) was
// real-lines + 1 and `hi > len(s)` accepted a citation one past the end
// of every file in the repo. For the citations that get the mechanical
// half alone, that was the entire check.
//
// It reads a REAL file, because the defect is in the reading.
func TestTheProductionReaderCountsRealLines(t *testing.T) {
	// A RELATIVE DIRECTORY, and t.TempDir() is what it replaces.
	//
	// The second half of this test drives the citation through
	// citationProblems, which only sees a path rePath matches:
	// `[A-Za-z0-9_./-]+\.go`. An absolute Windows temp path is
	// `C:\Users\…\three.go` — a drive colon and backslashes, neither
	// in that class — so on Windows no citation is found, both
	// assertions read "the guard reported no problem", and the arm fails
	// while saying something that is not true of the reader. The paths
	// this guard is FOR are repo-relative anyway, so the fixture should
	// be one.
	//
	// THE COST IS A REAL GO PACKAGE IN THE MODULE ROOT, and the prefix
	// is load-bearing rather than decorative: t.Cleanup does not run
	// when the binary dies on another test's panic or a -timeout kill,
	// so an interrupted run leaves `citelinesNNNN/three.go` holding
	// `package p` for `./...` to compile and `git status` to report.
	// `.gitignore` carries a line with that reasoning, so the leftover
	// is harmless rather than merely unlikely.
	//
	// TestTheFixtureLeftoverIsIgnored below is what keeps the prefix and
	// the pattern in step — two things that must agree is the shape this
	// branch has already been caught leaving unguarded once, so the
	// prefix is a constant and the guard reads .gitignore. Both raised
	// in review of #475.
	dir, err := os.MkdirTemp(".", citeFixturePrefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := dir + "/three.go"
	if !citationRe.MatchString("`" + path + ":3`") {
		t.Fatalf("the fixture path %q is outside the citation grammar, so the "+
			"checks below would see no citation and pass on that", path)
	}
	// NEWLINE-TERMINATED, like every file gofmt writes — which is
	// precisely the case the trailing element appears in.
	if err := os.WriteFile(path, []byte("package p\n\nfunc Gamma() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("readLines reports %d lines for a 3-line file. A newline TERMINATES "+
			"the last line rather than starting another, and an extra one makes "+
			"every out-of-range check off by one in the permissive direction: %q",
			len(got), got)
	}

	// AND THROUGH THE CHECK, since the count only matters there.
	if problems, _, _ := citationProblems("`Gamma` (`"+path+":4`) x.", readLines); len(problems) == 0 {
		t.Error("a citation one line past the end of the file is accepted")
	}
	if problems, _, _ := citationProblems("`Gamma` (`"+path+":3`) x.", readLines); len(problems) > 0 {
		t.Errorf("the last real line of the file is rejected: %v", problems)
	}
}

// readLines is citationProblems' production reader.
//
// THE TRAILING ELEMENT IS DROPPED, and leaving it in was a fail-open.
// strings.Split of a newline-TERMINATED file yields a final "" that is
// not a line, so len(s) was real-lines + 1 and the `hi > len(s)` check
// accepted a citation one line past the end of every file in the repo —
// then misreported the length in the error text for the citation two
// past. For the ten citations that get the mechanical half ALONE that
// off-by-one was the entire check.
//
// It was invisible because the honesty arm hands citationProblems a
// slice literal and never runs this function: the production reader had
// no coverage at all. TestTheProductionReaderCountsRealLines is that
// coverage, and it reads a real file. Raised in review of #475.
func readLines(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := strings.Split(string(b), "\n")
	if n := len(s); n > 0 && s[n-1] == "" {
		s = s[:n-1]
	}
	return s, nil
}

// atoiLine parses a citation's line number and says whether it could.
//
// IT IS NOT TOTAL, which is the whole difference from the mustAtoi it
// replaced — and this comment was still mustAtoi's for a review round
// after the function changed. That comment argued a failure was
// impossible because "the regexps only ever hand it a digit run", which
// is true of the DIGITS and false of the VALUE: a digit run longer than
// an int overflows, `fake.go:99999999999999999999` is a real thing to
// type, and mustAtoi's answer to it was 0 — a citation reported against
// "line 0" rather than as unparseable. The bool is what the caller
// needs to report it. Raised again in review of #475: a stale comment
// on a rewritten function claims the opposite of what the code does,
// and go vet cannot see it.
func atoiLine(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// dashed renders a range's second half for a message, or nothing.
func dashed(hi string) string {
	if hi == "" {
		return ""
	}
	return "-" + hi
}

// codeOnly drops // comment text from a window before the identifier
// check, because a citation satisfied by PROSE is not checked at all.
//
// CLAUDE.md pointed `prop.Set` at `prop/prop.go:101` — the tail of
// Settable's doc comment, sixteen lines above Set itself — and the guard
// passed it, because "Settable reports whether Set is legal" sits inside
// the window and holds the leaf. So the file whose stated purpose is
// corrected citations shipped one that sends a reader to the wrong
// symbol, and the guard written to prevent exactly that accepted it. A
// citation names where a symbol is DEFINED or CALLED; a mention of it in
// someone else's sentence is neither.
//
// It cuts at the first // on the line, which also truncates a line whose
// string literal holds one. That can only make the guard STRICTER, and
// every identifier-checked citation in CLAUDE.md was re-run against it —
// none loses its leaf this way. A citation that properly points at a
// comment is a decision to record here rather than a silent pass.
//
// BLOCK COMMENTS TOO, and they are the same hole one spelling further
// out: an identifier that appears only inside a /* … */ span satisfied
// the citation for as long as only // was stripped. Nothing in the tree
// passes for that reason today — no file CLAUDE.md or
// docs/markup-reference.md cites contains a /* at all — so this closes a
// class rather than a live case, which is the right time to close one.
//
// THE SPANS ARE FOUND OVER THE WHOLE FILE, in blockFree below, not here.
// A window is a SLICE: a span opened above it and closed below it is
// invisible from inside, so stripping per-window would leave exactly the
// long comments most likely to hold a stray identifier. Both raised in
// review of #475.
func codeOnly(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if j := strings.Index(l, "//"); j >= 0 {
			l = l[:j]
		}
		out[i] = l
	}
	return strings.Join(out, "\n")
}

// blockFree is lines with every /* … */ span blanked out, LINE COUNT
// PRESERVED so a caller's line numbers still index it.
//
// Blanking rather than deleting is the whole point: the citation guard
// indexes this by the cited line number, and a shorter slice would make
// every citation below the first block comment point somewhere else.
//
// It is deliberately not a Go parser. A /* inside a string literal or
// after a // would fool it, and the effect of being fooled is that MORE
// text is blanked — the guard gets stricter and says "is nowhere
// within", which is a visible failure rather than a silent pass. The
// asymmetry is the same one codeOnly's own comment makes.
func blockFree(lines []string) []string {
	out := make([]string, len(lines))
	in := false
	for i, l := range lines {
		var b strings.Builder
		for j := 0; j < len(l); {
			if in {
				if k := strings.Index(l[j:], "*/"); k >= 0 {
					j += k + 2
					in = false
					continue
				}
				break
			}
			if k := strings.Index(l[j:], "/*"); k >= 0 {
				b.WriteString(l[j : j+k])
				j += k + 2
				in = true
				continue
			}
			b.WriteString(l[j:])
			break
		}
		out[i] = b.String()
	}
	return out
}

// identRe matches leaf as a whole identifier rather than as a substring.
//
// QuoteMeta IS BELT-AND-BRACES, and this comment used to say otherwise.
// It argued that `Cells.Clip` has a dot in it and QuoteMeta keeps that a
// literal — but the only caller passes `leaf`, which is the segment
// AFTER the last dot, and reIdent restricts every segment to
// [A-Za-z_][A-Za-z0-9_]*. So `Cells.Clip` arrives as `Clip` and QuoteMeta
// is a no-op on every input this function can receive. It stays as
// defence against a reIdent that later admits more, which is a different
// claim from the one that was written here, and a stale comment on a
// rewritten function is the failure this whole file is about — go vet
// cannot see one. Raised in review of #475.
//
// Compiled per call because the set of leaves is the set of citations,
// small and read once.
func identRe(leaf string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(leaf) + `\b`)
}

// symbolDocs are the pages whose SYMBOL citations are checked, and it is
// DERIVED rather than written: every markdown file in the tree outside
// the pruned directories, so a page that starts citing code by name is
// covered the moment it does.
//
// It was a two-element list — CLAUDE.md and the markup reference — and a
// hand-written list is the exact hole TestEveryCitingDocumentIsChecked
// exists to close for the `file:line` form one function up. The symbol
// form had no such reciprocal, so docs/architecture.md carried dozens of
// package-qualified names that nothing resolved, and adding a document
// to the tree added nothing to the coverage. Deriving is what CLAUDE.md's
// Verify section prescribes for the same reason: a written list is stale
// the first time someone adds one, silently. Raised in review of #490.
func symbolDocs(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(path)
		if d.IsDir() {
			switch {
			case path == ".":
				return nil
			case strings.HasPrefix(d.Name(), "."), d.Name() == "vendor",
				slash == "docs/specs", slash == "presentations":
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".md" {
			out = append(out, slash)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking for documents: %v", err)
	}
	// THE SAME PRUNE AS TestEveryCitingDocumentIsChecked, and the same
	// floor: specs are dated records that describe the tree on their own
	// date, and a prune eating the walk would make every citation check
	// below vacuous at once.
	if len(out) < 40 {
		t.Fatalf("the walk found %d markdown files, which is too few for this "+
			"tree — a prune is eating the documents", len(out))
	}
	return out
}

// fileSuffixes are the tails that make a `pkg.name` span a FILENAME
// rather than a package-qualified symbol. `main.go`, `wizard.go` and
// `control.py` all match symbolCiteRe — `main`, `control` and half the
// tree's directories are real package names — and every one of them is
// somebody writing a path, not a citation. Skipping them is what lets
// the walk above cover the whole tree instead of a list of two.
var fileSuffixes = map[string]bool{
	"go": true, "py": true, "md": true, "sh": true, "yml": true,
	"yaml": true, "json": true, "toml": true, "txt": true, "gooey": true,
	"mod": true, "sum": true, "png": true, "gif": true, "svg": true,
}

// symbolCiteRe is a backticked, package-qualified Go name: one lowercase
// package segment, a dot, one identifier.
//
// THE LOWERCASE HEAD IS THE WHOLE FILTER, and it is what keeps this off
// the markup reference's own vocabulary. `Grid.Row`, `Canvas.Left` and
// `ItemsView.ItemTemplate` are attached-property spellings, not Go
// symbols, and their heads are element names — capitalised, every one.
// A Go package in this repo is lowercase, every one. Matching on that
// costs the `Context.BindingValue` form, which is a TYPE and a method:
// resolving those needs the receiver's package, which the prose does not
// give, and guessing it is how a guard starts reporting the wrong
// symbol.
var symbolCiteRe = regexp.MustCompile("`([a-z][a-z0-9]*)\\.([A-Za-z_][A-Za-z0-9_]*)`")

// symbolForeignMarkerRe opts ONE CITATION out of the symbol check, for a
// `pkg.Name` that is not Go at all.
//
// The collision this answers is not the vendored one — vendoredByPackage
// handles a dependency written in Go, by asking whether the dependency
// declares the name. There is nothing to ask when the language is
// different. apps/dynamic-activities/README.md warns that a bare
// directory named `grpc` shadows the Python package and tells the reader
// to probe for a real attribute on it; `grpc` is also one of OUR package
// names, so the guard read a Python attribute as a stale citation of
// ours and the only remedy it offered — "spell it with enough of its
// import path" — is advice a Python symbol cannot follow.
//
// EXPLICIT, not inferred, for the reason specclaims_test.go's
// plannedMarker gives at greater length. The inference I measured first
// was "an underscore is not a Go name"; the tree holds 73 declarations
// that disprove it, every one generated protobuf under grpc/gen, so a
// citation of `controlv1.ActResult_SendKeys` would have been silently
// exempted by the rule meant to exempt Python.
//
// AND IT EXPIRES: a marked citation whose name our package of that name
// DOES declare is a stale marker hiding a live citation, and the loop
// below fails on it rather than skipping. Raised in review of #490.
//
// IT NAMES THE SYMBOL, and it did not. The marker was LINE-scoped, so it
// exempted every `pkg.Name` on the line it appeared on — and the expiry
// arm below fires only when our package DOES declare the name, which is
// the case that would have passed anyway. The case it could not see is
// the one this guard exists for: a second, genuinely-Go citation on the
// same line that has ROTTED, skipped in silence. The live marker was
// safe only by accident, because the other citation on its line carries
// a slash and the pattern does not match one. Measured both ways —
// adding a rotted markup.NoSuchSymbolHere to that line is caught now (no
// backticks on that name here: a citation form written as an EXAMPLE is
// not a live reference, which this guard would otherwise read as one).
// Raised in review of #490, the round after.
//
// Spelled `<!-- symbols: not-go pkg.Name -->`, exempting exactly that
// citation; anything else on the line is checked.
var symbolForeignMarkerRe = regexp.MustCompile(
	`<!-- symbols: not-go ([a-z][a-z0-9]*\.[A-Za-z_][A-Za-z0-9_]*) -->`)

// exemptedSymbols is every `pkg.Name` a line opts out, as "pkg.Name".
func exemptedSymbols(line string) map[string]bool {
	out := map[string]bool{}
	for _, m := range symbolForeignMarkerRe.FindAllStringSubmatch(line, -1) {
		out[m[1]] = true
	}
	return out
}

// TestEveryCitedSymbolResolves is the half TestCLAUDEMDCitationsResolve
// cannot see, and #490 is what made it necessary.
//
// That PR replaced line citations with symbols on the stated ground that
// a symbol "cannot rot the way markup.go:1976 had" — and then removed
// docs/markup-reference.md from citedDocs, because the pattern there
// matches `path:line` only. Nothing checked the new form: rename
// boundaryPartition and the reference page points at nothing while every
// test still compiles and passes, since they use the identifier rather
// than reading the doc's spelling of it. The net was FEWER checked
// citations than main had, from a change whose argument was that the new
// form is safer. Raised in review of #490.
//
// It resolves against declarations rather than against a grep: a name
// that appears only inside a comment or a string is exactly the rot this
// is for.
func TestEveryCitedSymbolResolves(t *testing.T) {
	declared := declaredByPackage(t)
	foreign, haveVendor := vendoredByPackage(t)
	if len(declared["markup"]) == 0 || len(declared["render"]) == 0 {
		t.Fatalf("the declaration index found %d markup symbols and %d render "+
			"symbols, so the walk is looking somewhere else and every citation "+
			"below would resolve vacuously",
			len(declared["markup"]), len(declared["render"]))
	}

	checked, fromGo := 0, 0
	// rescued counts citations the vendor index cleared, and
	// unadjudicable counts the ones a missing vendor/ leaves undecided —
	// the two halves of the same question, so that whichever
	// configuration this run is in, the answer is reported rather than
	// assumed. See the floor below for what each is worth.
	rescued, unadjudicable := 0, 0
	docs := symbolDocs(t)
	for _, doc := range append(docs, goCommentSources(t)...) {
		var text string
		if strings.HasSuffix(doc, ".go") {
			text = goComments(t, doc)
		} else {
			b, err := os.ReadFile(doc)
			if err != nil {
				t.Fatalf("reading %s: %v", doc, err)
			}
			text = string(b)
		}
		// PER LINE, not per document, and the line is what the marker
		// attaches to. A citation cannot straddle a newline — both
		// backticks are on one line or the pattern does not match — so
		// this reads exactly what the whole-text scan read, and the
		// failures below can now say WHERE.
		for ln, line := range strings.Split(text, "\n") {
			exempted := exemptedSymbols(line)
			for _, m := range symbolCiteRe.FindAllStringSubmatch(line, -1) {
				pkg, name := m[1], m[2]
				if fileSuffixes[name] {
					continue // a path, not a citation — see fileSuffixes
				}
				if strings.HasPrefix(name, "Test") {
					// TEST NAMES BELONG TO TestEveryCitedTestNameResolves,
					// which knows where a test may live and that the root
					// package answers to "gooey" — and which reads Go
					// comments as well as Markdown, so this hands them to
					// a guard that actually looks at this corpus.
					// Delegating to one that read Markdown only left a
					// name cited in a Go comment adjudicated by neither,
					// which is what a delegation costs when nobody checks
					// the delegate's corpus. Adjudicating them HERE
					// instead reported this file's and
					// specclaims_test.go's own EXAMPLES of the citation
					// form — markup.TestX, gooey.TestFoo, spelled without
					// backticks now for the reason specclaims_test.go
					// gives: a name that is not a live reference is not
					// written as one. Raised in review of #490, twice.
					continue
				}
				syms, ours := declared[pkg]
				if !ours {
					continue // a stdlib or third-party name; not ours to check
				}
				if exempted[pkg+"."+name] {
					// THE MARKER EXPIRES HERE. It says "this pkg.Name is
					// not Go"; if our package of that name declares it,
					// that has stopped being true and the marker is
					// hiding a live citation from the check.
					if syms[name] {
						t.Errorf("%s:%d exempts `%s.%s` as not-Go, and package "+
							"%s now declares it. Remove the marker so the citation "+
							"is checked like any other. (#490)",
							doc, ln+1, pkg, name, pkg)
					}
					continue
				}
				if !syms[name] && foreign[pkg][name] {
					// NOT OURS AFTER ALL. Our package of this name does not
					// declare it and a vendored package of the same name
					// does, so the page is citing the dependency. See
					// vendoredByPackage for why this is asked in that order
					// and not the other.
					rescued++
					continue
				}
				if !syms[name] && !haveVendor {
					// UNADJUDICABLE, NOT CLEARED. With no vendor/ there
					// is no way to tell a dependency's symbol from a
					// rename of ours, and this is the exact set where
					// the two are confusable: our package of that name
					// exists and does not declare the symbol. Everything
					// else on this page is still checked, which is the
					// whole point of not skipping the test — see
					// vendoredByPackage.
					unadjudicable++
					continue
				}
				checked++
				if strings.HasSuffix(doc, ".go") {
					fromGo++
				}
				if !syms[name] {
					t.Errorf("%s:%d cites `%s.%s` and package %s declares no %s. A "+
						"symbol citation does not rot the way a line number does, "+
						"but it does go stale on a RENAME — which is the thing a "+
						"line citation cannot do and this form can. If this is a "+
						"DEPENDENCY's %s and not ours, it is not vendored under "+
						"that name either, so spell it with enough of its import "+
						"path to say so — and if it is not Go at all, put "+
						"<!-- symbols: not-go %s.%s --> on the line. (#490)",
						doc, ln+1, pkg, name, pkg, name, pkg, pkg, name)
				}
			}
		}
	}
	// A FLOOR, not an exact count: this reads whole documents rather than
	// one section, and pinning the number would make every added sentence
	// a test edit. Zero is the failure that matters — the pattern having
	// drifted from how the documents spell a citation. The floor rose
	// with the derived set, which reads the whole tree.
	if checked < 100 {
		t.Errorf("only %d package-qualified symbol citations were resolved across "+
			"%d documents, which is fewer than this tree carried when the walk was "+
			"derived — either they stopped citing code by name, or the pattern no "+
			"longer matches the spelling they use", checked, len(docs))
	}
	// AND THE VENDOR ARM IS NOT ALLOWED TO GO QUIET. It is the only
	// mechanism standing between a page citing a dependency's symbol and
	// an error telling the author to rename their own — and with vendor/
	// present nothing else exercises it on real documents
	// (TestAVendoredCollisionIsNotOurStaleCitation drives the decision
	// on a fixture). If it rescues nothing, the standalone arm above is
	// exempting nothing either, and both should go rather than sit here
	// looking like coverage. Raised in review of #490.
	if haveVendor && rescued == 0 {
		t.Error("the vendor index cleared no citation, so nothing in the tree " +
			"cites a dependency's symbol under a package name we also use. " +
			"Either those citations were reworded — in which case the vendor " +
			"index, vendoredIndex, and the unadjudicable arm beside it are all " +
			"dead code — or the vendor walk is looking somewhere else")
	}
	if !haveVendor && unadjudicable != 0 {
		// Reported, not failed: this configuration is legitimate, and
		// the number is what a reader needs to know the guard ran
		// narrower than usual rather than not at all.
		t.Logf("vendor/ is absent, so %d citation(s) naming one of our packages "+
			"and a symbol it does not declare were left undecided; the other %d "+
			"were checked", unadjudicable, checked)
	}
	// AND THE GO-COMMENT HALF SEPARATELY, because the floor above is
	// dominated by markdown and would not notice it going to zero. That
	// half is the whole of #490's finding — the conversions this guard
	// exists for landed in Go comments, where nothing read them — so a
	// walk that stopped returning .go files, or a parse that started
	// failing silently, has to be its own failure rather than a dent in
	// a three-digit total.
	//
	// IT WAS WRITTEN TWO ARMS UP, merged into the vendor arm's comment
	// with no blank line between them, so the whole argument read as the
	// rationale for the vendor floor and the reader it misled was the one
	// auditing whether that floor is worth keeping. That is the defect
	// this branch fixed in markup/referencedoc_test.go and then committed
	// one file over, in the same change. A comment belongs above the arm
	// it is about; the checked-count floor it was sitting beside is a
	// neighbour, not its subject. Raised in review of #490.
	if fromGo == 0 {
		t.Error("no symbol citation was resolved from a Go comment, so the half " +
			"of this guard that #490 added covers nothing. Either goComments is " +
			"returning empty (a parse failing quietly), goCommentSources is " +
			"pruning the tree away, or the convention changed and Go comments no " +
			"longer backtick a pkg.Name")
	}
}

// declaredByPackage maps a package NAME to every identifier declared at
// the top level of one of its files, tests included: a doc may cite a
// guard's table, and rowPartition lives in a _test.go file.
//
// METHOD NAMES ARE IN IT, and that is deliberate rather than an
// oversight in the sentence above: the FuncDecl arm records d.Name.Name
// without looking at the receiver, so `func (p *Property[T]) Set` puts
// Set into package prop. CLAUDE.md cites `prop.Set`, which is a method,
// and it resolves only because of that. The cost is real and worth
// stating: a package-qualified citation can resolve against a method on
// an unrelated type in the same package, so renaming a top-level
// markup.textSource would still pass if any type in markup declared a
// textSource method. Narrowing it would need the receiver's package,
// which is the same thing symbolCiteRe refuses to guess for the
// Context.BindingValue form. Raised in review of #490.
//
// BY NAME AND NOT BY DIRECTORY, because prose writes `markup.Load` and
// never says which directory that is. Two directories with the same
// package name union their symbols, which is the only imprecision here
// and a safe one: it can accept a citation that resolves in the wrong
// copy, never reject one that resolves in the right one.
// ONCE PER BINARY, not once per caller. Two tests ask for each of these
// indexes and each walk parses the whole tree (vendor/ included), which
// put a substantial slice of pure duplicate work in the root suite. The
// memo changes nothing either test asserts: the walk is over files on
// disk, which no test here writes. Raised in review of #490.
//
// The memo stays even though buildDeclaredByPackage now delegates to a
// memoized index of its own: this one is the SEAM, and a caller should
// not have to know that the two indexes share a walk. Collapsing it
// would make the sharing load-bearing for correctness rather than for
// cost.
var declaredIndex = sync.OnceValues(buildDeclaredByPackage)

func declaredByPackage(t *testing.T) map[string]map[string]bool {
	t.Helper()
	out, err := declaredIndex()
	if err != nil {
		t.Fatalf("walking the tree for declarations: %v", err)
	}
	return out
}

// ONE WALK, ONE PARSE, TWO INDEXES. This had its own WalkDir over the
// identical file set with the identical prune, and parsed every .go file
// in the tree a second time — so the memo that removed the duplication
// between CALLERS left the duplication between INDEXES. A file parsed
// with ParseComments carries its Decls too, so the comment corpus can
// produce this for nothing. Measured over the root suite, -count=1,
// three runs each: see the PR comment for the round that did it; no
// seconds are written here, for the reason CLAUDE.md's Verify section
// gives about numbers in prose. Raised in review of #490.
func buildDeclaredByPackage() (map[string]map[string]bool, error) {
	c, err := goCommentIndex()
	if err != nil {
		return nil, err
	}
	return c.declared, nil
}

// goCommentSources are the .go files whose COMMENTS carry symbol
// citations, and their absence was a real hole: this guard read markdown
// only, so #490's own six file:line → symbol conversions — the change
// that motivated the guard — landed in Go comments where nothing could
// see them.
//
// BACKTICKED ONLY, which is the convention that makes this tractable.
// Measured on this tree: matching a bare pkg.Name inside comments finds
// 1448 candidates and calls 336 of them unresolved — proto package
// paths like gooey.control, field selectors like panel.slot, plurals —
// so a guard on that form is noise, not coverage. The backticked form
// finds 18 and resolves all of them. So a citation in a Go comment is
// spelled `pkg.Name` in backticks, the same as in markdown, and one
// written bare is simply outside this guard rather than wrong.
//
// Which is also why the examples in this paragraph are NOT backticked:
// a guard reads its own explanation, and an example of a spelling that
// does not resolve is indistinguishable from a citation that stopped
// resolving. Write illustrative spellings bare.
//
// WHAT IS STILL NOT COVERED, stated because a guard that implies more
// than it checks is the defect this whole file is about: a SAME-PACKAGE
// identifier in a comment — `bindText`, `elementDefs`, `probeElement` —
// has no package qualifier, so symbolCiteRe cannot match it and nothing
// here resolves it. Three of #490's six conversions are of that shape.
func goCommentSources(t *testing.T) []string {
	t.Helper()
	c, err := goCommentIndex()
	if err != nil {
		t.Fatalf("walking for Go sources: %v", err)
	}
	return c.paths
}

// goCommentCorpus is every Go file in the tree with its comments, walked
// and parsed ONCE.
//
// Memoized for the reason declaredIndex gives, at a larger magnitude and
// in the commit that gave it. Two guards read this corpus —
// TestEveryCitedSymbolResolves through goComments and
// TestEveryCitedTestNameResolves through goCitations — and each used to
// do its own WalkDir and its own goparser.ParseFile(…, ParseComments)
// over the same files, for 2.48s and 2.24s of a 25.7s root suite with
// roughly half of it duplicate. The memo changes nothing either test
// asserts: the walk is over files on disk, which no test here writes.
// Raised in review of #490.
//
// TWO PRODUCTS FROM ONE PARSE, because the two readers want different
// shapes of the same thing and neither may be approximated by the other.
// A prose scan wants the comment TEXT with its markers stripped, which
// is what CommentGroup.Text() gives; a citation wants the raw line and
// the file line it is on, so a failure can name a place. Deriving either
// from the other would move a line number or change what a pattern sees.
type goCommentCorpus struct {
	// paths is every .go file, in walk order, so both guards read the
	// same corpus in the same order.
	paths []string
	files map[string]goCommentFile
	// declared is package name -> the top-level names it declares, the
	// third product of the same parse. It answers a question about the
	// TREE rather than about one file, which is why it is not on
	// goCommentFile. See buildDeclaredByPackage.
	declared map[string]map[string]bool
}

type goCommentFile struct {
	// text is every comment group's Text(), concatenated — the form a
	// prose pattern runs over.
	text string
	// lines is one entry per raw comment LINE, with the line of the file
	// it sits on.
	lines []goCommentLine
}

type goCommentLine struct {
	line int
	text string
}

var goCommentIndex = sync.OnceValues(buildGoCommentIndex)

func buildGoCommentIndex() (*goCommentCorpus, error) {
	c := &goCommentCorpus{
		files:    map[string]goCommentFile{},
		declared: map[string]map[string]bool{},
	}
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); p != "." && (strings.HasPrefix(n, ".") || n == "vendor" || n == "testdata") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			c.paths = append(c.paths, filepath.ToSlash(p))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, p := range c.paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		fset := gotoken.NewFileSet()
		f, err := goparser.ParseFile(fset, p, src, goparser.ParseComments)
		if err != nil {
			// The compiler owns this one — an unparseable file has no
			// comments as far as either guard is concerned, which is
			// what both of them did on their own.
			c.files[p] = goCommentFile{}
			continue
		}
		// THE DECLARATION INDEX, from the parse that already happened.
		// An unparseable file contributes nothing here either, which is
		// what the old separate walk did on its own.
		pkg := f.Name.Name
		if c.declared[pkg] == nil {
			c.declared[pkg] = map[string]bool{}
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				c.declared[pkg][d.Name.Name] = true
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.TypeSpec:
						c.declared[pkg][sp.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							c.declared[pkg][n.Name] = true
						}
					}
				}
			}
		}

		var b strings.Builder
		var lines []goCommentLine
		for _, cg := range f.Comments {
			b.WriteString(cg.Text())
			b.WriteByte('\n')
			for _, com := range cg.List {
				at := fset.Position(com.Slash).Line
				for i, l := range strings.Split(com.Text, "\n") {
					lines = append(lines, goCommentLine{line: at + i, text: l})
				}
			}
		}
		c.files[p] = goCommentFile{text: b.String(), lines: lines}
	}
	return c, nil
}

// goComments is every comment in one file, concatenated — so the caller
// runs the same pattern over Go comments that it runs over markdown, and
// a `pkg.Name` in a string literal or an identifier is not mistaken for
// prose about one.
//
// It reads the shared index rather than parsing; see goCommentCorpus.
func goComments(t *testing.T, path string) string {
	t.Helper()
	c, err := goCommentIndex()
	if err != nil {
		t.Fatalf("indexing Go comments: %v", err)
	}
	return c.files[path].text
}

// TestAVendoredCollisionIsNotOurStaleCitation drives the three-way
// decision TestEveryCitedSymbolResolves makes per citation, because that
// test reads the tree's real documents — and no document exercises the
// collision today, so it is green whichever way the branch goes.
//
// The three cases are the whole contract, and the middle one is the
// finding: ours declares it (a citation of ours, checked), ours does not
// but a vendored package of the same name does (a citation of theirs,
// left alone), neither declares it (the rename this guard exists for).
// Raised in review of #490.
func TestAVendoredCollisionIsNotOurStaleCitation(t *testing.T) {
	declared := declaredByPackage(t)
	foreign, haveVendor := vendoredByPackage(t)
	if !haveVendor {
		// THIS test's subject really is the vendor index, so with no
		// vendor/ there is nothing for it to decide. Its sibling keeps
		// running — see vendoredByPackage.
		t.Skip("vendor/ is absent (this module consumed standalone), so there is " +
			"nothing vendored to collide with")
	}

	// NON-VACUITY FIRST. Every arm below is "and the other index says
	// X"; an empty foreign index makes the middle arm unreachable and
	// the whole test a restatement of the one above it.
	if len(foreign) == 0 {
		t.Fatal("no vendored package shares a short name with one of ours, so " +
			"the collision this test is about cannot be reached. Either the " +
			"vendor walk is looking somewhere else, or the tree genuinely " +
			"changed — check `grpc`, `term` and `workflow` before deleting this")
	}

	for _, tc := range []struct {
		name         string
		pkg, sym     string
		ours, theirs bool
		wantSkipped  bool
		wantContext  string
	}{
		{"ours declares it", "term", "DecoderTimeout", true, false, false,
			"a citation of our own symbol must still be checked"},
		{"only the dependency declares it", "term", "IsTerminal", false, true, true,
			"x/term declares IsTerminal and our term does not, so the page is " +
				"citing the dependency and rewording the page is the wrong remedy"},
		{"neither declares it", "term", "NoSuchSymbolAnywhere", false, false, false,
			"a name nothing declares is the rename this guard exists for"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := declared[tc.pkg][tc.sym]; got != tc.ours {
				t.Fatalf("our %s declares %s = %v, want %v — the fixture no longer "+
					"describes the tree, so the arm below measures nothing",
					tc.pkg, tc.sym, got, tc.ours)
			}
			if got := foreign[tc.pkg][tc.sym]; got != tc.theirs {
				t.Fatalf("the vendored %s declares %s = %v, want %v", tc.pkg, tc.sym,
					got, tc.theirs)
			}
			// The decision exactly as the loop makes it.
			skipped := !declared[tc.pkg][tc.sym] && foreign[tc.pkg][tc.sym]
			if skipped != tc.wantSkipped {
				t.Errorf("`%s.%s` skipped=%v, want %v: %s",
					tc.pkg, tc.sym, skipped, tc.wantSkipped, tc.wantContext)
			}
		})
	}
}

// vendoredByPackage indexes the EXPORTED declarations of vendored
// packages whose short name collides with one of ours, which is the only
// way a citation of `pkg.Name` can be read two ways.
//
// THE COLLISION IS REAL AND ALREADY HERE. Three of this repo's package
// names are also the last segment of a vendored import path — grpc
// (google.golang.org/grpc), term (golang.org/x/term) and workflow
// (go.temporal.io/sdk/workflow) — so a page writing Temporal's
// `workflow.Now` or x/term's `term.IsTerminal` was asking about a
// package we also happen to have, and TestEveryCitedSymbolResolves
// reported it as a stale citation of OURS. The remedy it offered was to
// reword the documentation, which is the wrong end. Raised in review of
// #490.
//
// THE TWO ABOVE ARE BACKTICKED ON PURPOSE, and this comment is a live
// fixture because of it: the guard reads Go comments, so `workflow.Now`
// and `term.IsTerminal` are citations it resolves on every run, and they
// resolve only through the skip this function feeds. Remove the skip and
// this paragraph reddens the guard it describes. Do not un-backtick them
// to quieten that — the failure is the mechanism working.
//
// NARROW ON PURPOSE, and the narrowness is what keeps the coverage. This
// is consulted ONLY when our own package does not declare the name, and
// then only to ask whether the foreign package does. A term.Foo that
// was renamed out of our term and exists nowhere else still errors,
// which is the rot this guard is for; a `term.IsTerminal` that x/term
// really declares is a citation of x/term and is left alone. Nothing is
// skipped on the strength of the package name by itself.
//
// EXPORTED TOP-LEVEL DECLARATIONS, METHODS EXCLUDED — which is what the
// walk records: funcs, types and vars alike, so a cited vendored TYPE
// (workflow.Context) or VAR (grpc.ErrClientConnClosing) is in here and
// is exempted too. This heading said "TOP-LEVEL FUNCTIONS ONLY" until
// round ten, understating a SKIP list — which is the same direction as
// the error the next paragraph records, written into the heading by the
// edit that fixed it. Only the methods narrowing is real, and the
// paragraph below states it. Raised in review of #490.
//
// EXPORTED is the other narrowing, and it is in the direction that
// keeps coverage rather than spends it. The sentence at the top of this
// comment said EXPORTED while the code indexed every top-level name,
// UNEXPORTED ONES INCLUDED, which is the wrong way round for a SKIP
// list: every name in here is a citation this guard stops checking, so
// an unexported vendored identifier — which no page can legitimately be
// citing, since a consumer cannot name one — was silently widening the
// exemption.
// ast.IsExported is the check the sentence already claimed. No page
// cites an unexported vendored name today, so removing the check again
// turns nothing red — which is the honest status of a tightening, and
// the reason it is recorded here rather than claimed as a fix with a
// test behind it.
//
// Methods are excluded (the d.Recv == nil arm), and that is the opposite
// choice from declaredByPackage, which records them deliberately because
// CLAUDE.md cites `prop.Set`. The asymmetry is the same principle read
// twice: there, including methods lets a real citation RESOLVE, and the
// cost is a citation resolving against the wrong type; here, including
// them would let a citation be SKIPPED, and the cost is coverage. When
// in doubt an index of ours widens and an index of theirs narrows. The
// price is a page citing a vendored METHOD as `pkg.Name`, which would
// still error and would have to be spelled with more of its import path
// — and no page does today. Raised in review of #490.
// Memoized for the reason declaredIndex gives. The parameter is not part
// of the key because there is only one possible argument — declaredIndex
// is itself memoized, so every caller passes the same map.
var vendoredIndex = sync.OnceValues(func() (map[string]map[string]bool, error) {
	declared, err := declaredIndex()
	if err != nil {
		return nil, err
	}
	return buildVendoredByPackage(declared)
})

// The bool is whether there IS an index, and it is what the callers
// branch on — this function no longer decides for them.
//
// A SKIP HERE ENDED THE CALLING TEST, which is wider than anything the
// condition justifies. t.Skip from a helper stops the caller, so a
// standalone checkout with no vendor/ lost TestEveryCitedSymbolResolves
// whole — every one of the hundred-plus citations its own floor insists
// on, silenced by a two-citation problem in this file's doc comment. The
// skip replaced a silent empty map, which was worse in the other
// direction (the empty map turned those two citations into errors about
// the wrong thing); the answer to both is to report the condition and
// let each caller narrow. Raised in review of #490, twice.
func vendoredByPackage(t *testing.T) (map[string]map[string]bool, bool) {
	t.Helper()
	if _, err := os.Stat("vendor"); errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	out, err := vendoredIndex()
	if err != nil {
		t.Fatalf("walking vendor for declarations: %v", err)
	}
	return out, true
}

func buildVendoredByPackage(collidesWith map[string]map[string]bool) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	err := filepath.WalkDir("vendor", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		// The DIRECTORY name, not the package clause: the collision is
		// with how prose spells the import, and prose spells it with the
		// path's last segment.
		if _, ours := collidesWith[filepath.Base(filepath.Dir(p))]; !ours {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fset := gotoken.NewFileSet()
		f, err := goparser.ParseFile(fset, p, src, 0)
		if err != nil {
			return nil
		}
		pkg := filepath.Base(filepath.Dir(p))
		if out[pkg] == nil {
			out[pkg] = map[string]bool{}
		}
		keep := func(name string) {
			if ast.IsExported(name) {
				out[pkg][name] = true
			}
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					keep(d.Name.Name)
				}
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.TypeSpec:
						keep(sp.Name.Name)
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							keep(n.Name)
						}
					}
				}
			}
		}
		return nil
	})
	return out, err
}
