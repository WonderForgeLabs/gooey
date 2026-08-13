package gooey

import (
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
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
var moduleNamespaces = map[string]bool{
	"handlers": true,
	"packs":    true,
	"imagefmt": true,
	"examples": true,
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

	backticked := regexp.MustCompile("`([a-z][a-z0-9]*(?:/[a-z0-9][a-z0-9._-]*)+)`")
	seen := map[string]bool{}
	for _, m := range backticked.FindAllStringSubmatch(string(b), -1) {
		ref := m[1]
		// A nested module is exactly `namespace/name`. Deeper paths are
		// something else — `examples/gitui/gitui` is the binary that module
		// builds, and the Traps section names it on purpose.
		parts := strings.Split(ref, "/")
		if len(parts) != 2 || !moduleNamespaces[parts[0]] || seen[ref] {
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
