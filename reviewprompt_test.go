package gooey

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The review prompts are instructions to an agent, and a wrong instruction is
// worse than a missing one: it gets applied. #207 fixed three stale facts in
// CLAUDE.md and the same three were still baked into these two files (#212) —
// including "core gooey depends only on `golang.org/x/term`" stated as the
// rule to flag new dependencies against, while `golang.org/x/image` had been a
// direct requirement all along. A reviewer applying that literally flags a
// legitimate import as a violation.
//
// The fix was to stop restating and start pointing at CLAUDE.md and ci.yml,
// which derive. These tests pin the two claims that can still be checked
// mechanically. They deliberately do NOT check for phrases like "only x/term"
// — a prose blocklist is the same hand-maintained list one layer up, and
// #212's own issue text is the proof: it was filed naming fifteen nested
// modules including `examples/gitui` and `examples/kanbandemo`, and by the
// time it was fixed the count and those two paths were all wrong.

var promptFiles = []string{
	".github/workflows/claude.yml",
	".github/workflows/claude-code-review.yml",
}

// backtickPath finds backticked repo locations, in three spellings these
// files use: `handlers/temporal`, the trailing-slash `prop/`, and the
// slash-free `CLAUDE.md`.
//
// The trailing-slash form is not an afterthought — claude.yml writes every
// directory that way, so a pattern requiring a second segment matched
// NOTHING in that file. The vacuity guard below is what caught that; the
// first version of this test passed on claude.yml having checked zero paths.
//
// The slash-free form is the one that matters most, and the first version
// could not see it at all (found in review). This fix's whole mechanism is
// "stop restating, point at `CLAUDE.md` and `ci.yml`, which derive" — and a
// pattern requiring a `/` verifies neither of the two things being pointed
// at. Rename CLAUDE.md and every prompt would point at nothing while the
// test stayed green: the exact drift #212 is about, one level up.
//
// Requiring a dot in the slash-free case is what keeps prose out: the
// backticked tokens in these files are `CLAUDE.md`, `go.mod`, `ci.yml` and
// `reviewmirror_test.go` — all real files — against `skipped`, `review`,
// `permissions`, `render` and friends, which are words.
var backtickPath = regexp.MustCompile("`([A-Za-z_.][A-Za-z0-9_.-]*(?:/[A-Za-z0-9_.*-]*)*)`")

// authorities are the referents this fix REPLACED restatement with. If a
// prompt stops naming them, the mechanism is gone even though every
// remaining path still resolves — so a non-vacuity guard alone cannot
// notice. Coverage of these two specifically is the thing worth asserting
// (found in review: `checked > 0` proves a path was checked, not that the
// file's location claims are covered).
var authorities = []string{"CLAUDE.md", "ci.yml"}

// forbidsGoBuild recognises a line that mentions `go build ./...` in order
// to PROHIBIT it. `\bNOT\b` and not `strings.Contains(…, "NOT")`, because
// the latter is satisfied by NOTE, NOTHING, ANNOTATION and DENOTES.
var forbidsGoBuild = regexp.MustCompile(`\bNOT\b`)

func TestReviewPromptsNameOnlyPathsThatExist(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range promptFiles {
		body := readFileString(t, f)
		checked := 0
		for _, m := range backtickPath.FindAllStringSubmatch(body, -1) {
			p := strings.TrimSuffix(m[1], "/")
			head, _, hasSlash := strings.Cut(p, "/")
			switch {
			case strings.Contains(p, "*"), strings.Contains(p, "..."):
				continue // a glob or a package pattern, not a location
			case hasSlash && strings.Contains(head, ".") && !strings.HasPrefix(p, "."):
				continue // golang.org/x/term — a module path, not a repo path
			case !hasSlash && !strings.Contains(p, "."):
				continue // `skipped`, `review`, `permissions` — prose, not a file
			}
			checked++
			seen[p] = true
			if !existsInRepo(p) {
				t.Errorf("%s names `%s`, which does not exist.\n\n"+
					"A prompt that names a path an agent cannot find sends it looking, and a "+
					"path that USED to exist sends it somewhere confidently wrong — "+
					"`examples/gitui` and `examples/kanbandemo` were both named in prompts "+
					"across the apps/ rename (#238, #268). Point at a directory that derives "+
					"(CLAUDE.md, ci.yml) rather than restating a location.", f, p)
			}
		}
		// Without this the test passes vacuously the day the regex stops
		// matching anything — the failure mode where a check quietly stops
		// checking is the whole subject of the neighbouring file.
		if checked == 0 {
			t.Errorf("%s: no repo paths were checked at all — the extractor matched nothing, "+
				"so this test proved nothing about the file.", f)
		}
	}

	// Non-vacuity is not coverage. These two are what the prompts point AT
	// instead of restating, so losing them silently undoes the fix while
	// every surviving path still resolves.
	for _, a := range authorities {
		if !seen[a] {
			t.Errorf("no prompt names `%s`.\n\n"+
				"The prompts stopped enumerating modules and dependencies on the basis that "+
				"CLAUDE.md and ci.yml DERIVE them (#212). A prompt that names neither has "+
				"quietly gone back to being on its own.", a)
		}
	}
}

// existsInRepo resolves a name the way a reader would: as a path from the
// repo root, or — for a bare filename like `ci.yml`, which lives at
// .github/workflows/ci.yml — as a basename found anywhere in the tree.
//
// Dot-directories are pruned at EVERY depth, not just the top: this repo
// routinely holds whole checkouts under .claude/worktrees/ and a vendored
// .venv with go.mod files of Temporal's own, and a top-anchored filter walks
// into them (CLAUDE.md's Verify section is the record of what that costs).
//
// `.github` is the deliberate exception, and leaving it out failed the test
// on its first run: `ci.yml` lives at .github/workflows/ci.yml, so the prune
// that protects the walk also hid the single most important thing the walk
// exists to find. `.github` is repo content a prompt may legitimately name;
// .git, .claude and .venv are not.
func existsInRepo(name string) bool {
	if _, err := os.Stat(name); err == nil {
		return true
	}
	if strings.Contains(name, "/") {
		return false // a path is a path; only bare names get searched
	}
	found := false
	_ = filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case found:
			return fs.SkipAll
		case d.IsDir() && p != "." && strings.HasPrefix(d.Name(), ".") && d.Name() != ".github":
			return fs.SkipDir
		case !d.IsDir() && d.Name() == name:
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func TestReviewPromptsDoNotTeachGoBuildDotDotDot(t *testing.T) {
	for _, f := range promptFiles {
		body := readFileString(t, f)
		for line := range strings.SplitSeq(body, "\n") {
			if !strings.Contains(line, "go build ./...") {
				continue
			}
			// The prompts now mention it in order to forbid it, which must
			// stay allowed or the fix cannot describe itself.
			//
			// `strings.Contains(line, "NOT")` was the first spelling and it
			// was a demonstrated bypass, not a theoretical one: **"NOTE"
			// contains "NOT"**, so `# NOTE: run go build ./... before
			// pushing` sailed through while the identical line without the
			// word failed. NOTHING, ANNOTATION and DENOTES open it too. A
			// word boundary is the fix — and the irony is the point, since
			// this file's whole subject is a check that quietly stops
			// checking. Found in review of #276.
			if forbidsGoBuild.MatchString(line) || strings.Contains(line, "writes an executable") {
				continue
			}
			t.Errorf("%s tells an agent to run `go build ./...`:\n  %s\n\n"+
				"That writes an executable into the repo root for every main package. "+
				".gitignore does not cover all of them and several have been committed "+
				"that way, so the prompt was instructing agents to do the thing the "+
				"repo's own trap list exists to stop. Use `go vet ./...`.",
				f, strings.TrimSpace(line))
		}
	}
}
