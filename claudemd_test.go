package gooey

import (
	"fmt"
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
var citation = regexp.MustCompile("`(" + rePath + "):(\\d+)(?:-(\\d+))?`")

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
var citeForms = []struct {
	name string
	re   *regexp.Regexp
	// fields pulls (ident, path, lo, hi) out of one match; hi == lo for a
	// citation that names a single line.
	fields func([]string) (string, string, int, int)
	// sample renders a citation IN THIS FORM, and it is what lets
	// TestTheCitationGuardCatchesWhatItIsFor derive its cases from this
	// slice instead of hand-writing one per form.
	//
	// It earns its place twice over: the hand-written cases had form B
	// and form C labelled the other way round, so a failure named a
	// shape the reader could not locate — and nothing in the file defines
	// A/B/C at all, which is why the subtest name is form.name now.
	// Raised in review of #475.
	sample func(ident, path string, line int) string
}{
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

func identFirst(m []string) (ident, path string, lo, hi int) {
	lo = mustAtoi(m[3])
	hi = lo
	if m[4] != "" {
		hi = mustAtoi(m[4])
	}
	return m[1], m[2], lo, hi
}

func pathFirst(m []string) (ident, path string, lo, hi int) {
	lo = mustAtoi(m[2])
	hi = lo
	if m[3] != "" {
		hi = mustAtoi(m[3])
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
// TestTheCitationGuardCatchesWhatItIsFor below. Without that arm, a
// widened citeWindow or a downgraded error silently turns the whole
// thing into a no-op that still reports PASS.
//
// It returns descriptions rather than calling t.Errorf so both callers
// can decide what a problem means: one requires none, the other requires
// some.
func citationProblems(md string, read func(string) ([]string, error)) (problems, forms []string) {
	lines := map[string][]string{}
	missing := map[string]bool{}
	src := func(path string) []string {
		if s, ok := lines[path]; ok {
			return s
		}
		s, err := read(path)
		if err != nil {
			missing[path] = true
			lines[path] = nil
			return nil
		}
		lines[path] = s
		return s
	}

	// ---- half one: every citation names a real place ----
	for _, m := range citation.FindAllStringSubmatch(md, -1) {
		path, lo := m[1], mustAtoi(m[2])
		hi := lo
		if m[3] != "" {
			hi = mustAtoi(m[3])
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
			if s == nil || lo < 1 || hi > len(s) {
				continue // half one already reported it
			}
			// The LEAF of a dotted name: the doc writes `Composer.Frame`
			// and the file writes `func (c *Composer) Frame(`.
			leaf := ident
			if i := strings.LastIndex(ident, "."); i >= 0 {
				leaf = ident[i+1:]
			}
			from, to := max(0, lo-1-citeWindow), min(len(s), hi+citeWindow)
			if !strings.Contains(strings.Join(s[from:to], "\n"), leaf) {
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
	return problems, forms
}

func TestCLAUDEMDCitationsResolve(t *testing.T) {
	b, err := os.ReadFile(claudeMD)
	if err != nil {
		t.Fatalf("reading %s: %v", claudeMD, err)
	}
	md := string(b)
	if len(citation.FindAllString(md, -1)) == 0 {
		t.Fatalf("%s carries no `file:line` citation at all, so this test checks "+
			"nothing — the pattern has drifted from the file", claudeMD)
	}

	problems, forms := citationProblems(md, readLines)
	for _, p := range problems {
		t.Errorf("%s %s (#466)", claudeMD, p)
	}

	// NON-VACUITY, per FORM rather than as a fraction: a ratio would be
	// the "number in prose" this file argues against, and it would pass
	// while one form's regexp quietly matched nothing.
	for _, f := range citeForms {
		if !slices.Contains(forms, f.name) {
			t.Errorf("no citation in %s matches the form %s, so that pattern is "+
				"checking nothing. If the doc genuinely stopped using the form, "+
				"delete it here — but first check the regexp has not simply rotted "+
				"against a change in the prose around it.", claudeMD, f.name)
		}
	}
}

// TestTheCitationGuardCatchesWhatItIsFor points the guard at documents
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
func TestTheCitationGuardCatchesWhatItIsFor(t *testing.T) {
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
	files := map[string][]string{"fake.go": src}
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
			problems, forms := citationProblems(md, read)
			if len(forms) == 0 {
				t.Fatalf("the guard matched no form at all in %q, so the rejection "+
					"below would be about the MECHANICAL half and this arm would "+
					"pass without the identifier check ever running", md)
			}
			if len(problems) == 0 {
				t.Errorf("the guard accepted %q, where %q is %d lines from the cited "+
					"line and citeWindow is %d. It reports PASS against a correct "+
					"document whether or not it is checking anything, so an arm that "+
					"cannot fail here is a guard that has stopped working.",
					md, "Alpha", driftLine-identLine, citeWindow)
			}
		})

		// AND THE OTHER DIRECTION, per form: "reject everything" must not
		// be a passing strategy for any of them, and a form whose regexp
		// rotted would otherwise look identical to a form that works.
		t.Run("the true line, "+form.name, func(t *testing.T) {
			md := form.sample("Alpha", "fake.go", identLine)
			if problems, _ := citationProblems(md, read); len(problems) > 0 {
				t.Errorf("the guard rejected the CORRECT citation %q: %v", md, problems)
			}
		})
	}

	// THE EDGE, both sides of it. These pin the reach as a mechanism
	// rather than as a number: exactly citeWindow away is inside, one
	// more is not. Without the accept arm, "reject anything not on the
	// cited line" would satisfy every rejection arm above.
	if md := citeForms[0].sample("Alpha", "fake.go", edgeLine); true {
		if problems, _ := citationProblems(md, read); len(problems) > 0 {
			t.Errorf("the guard rejected %q, where `Alpha` is exactly citeWindow "+
				"(%d) lines from the cited line — the window does not reach as far "+
				"as it says: %v", md, citeWindow, problems)
		}
	}

	for _, tc := range []struct{ name, md string }{
		{"a file that does not exist", "`Alpha` (`nosuch.go:3`) does the thing."},
		{"a line past the end of the file", "`Alpha` (`fake.go:900`) does the thing."},
		// LINE 0 panicked rather than reporting, because half two quotes
		// s[lo-1] in its message. Raised in review of #475.
		{"line zero, which is not a line", "`Alpha` (`fake.go:0`) does the thing."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems, _ := citationProblems(tc.md, read)
			if len(problems) == 0 {
				t.Errorf("the guard accepted %q. It reports PASS against a correct "+
					"document whether or not it is checking anything, so an arm "+
					"that cannot fail here is a guard that has stopped working.",
					tc.md)
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
	if problems, _ := citationProblems(dup, read); len(problems) == 0 {
		t.Errorf("the guard accepted %q. The second identifier on one cited line "+
			"was deduplicated away unchecked, so a wrong citation hides behind a "+
			"right one at the same location", dup)
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
	dir := t.TempDir()
	path := filepath.Join(dir, "three.go")
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
	if problems, _ := citationProblems("`Gamma` (`"+path+":4`) x.", readLines); len(problems) == 0 {
		t.Error("a citation one line past the end of the file is accepted")
	}
	if problems, _ := citationProblems("`Gamma` (`"+path+":3`) x.", readLines); len(problems) > 0 {
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

// mustAtoi is total for this caller: the regexps only ever hand it a
// digit run, so a failure is a bug in a pattern rather than in the
// document, and a zero would silently become "line 0".
func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic("citation pattern produced a non-numeric line " + s)
	}
	return n
}
