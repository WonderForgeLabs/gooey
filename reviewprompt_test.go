package gooey

import (
	"os"
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

// backtickPath finds backticked repo locations, in both spellings these
// files use: `handlers/temporal` and the trailing-slash `prop/`. The
// trailing-slash form is not an afterthought — claude.yml writes every
// directory that way, so a pattern requiring a second segment matched
// NOTHING in that file. The vacuity guard below is what caught that; the
// first version of this test passed on claude.yml having checked zero paths.
//
// A leading segment containing a dot is a module path (golang.org/x/term),
// not a repo path, and `./...`-style patterns are commands, not locations.
var backtickPath = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_.-]*(?:/[A-Za-z0-9_.*-]*)+)`")

func TestReviewPromptsNameOnlyPathsThatExist(t *testing.T) {
	for _, f := range promptFiles {
		body := readFileString(t, f)
		checked := 0
		for _, m := range backtickPath.FindAllStringSubmatch(body, -1) {
			p := strings.TrimSuffix(m[1], "/")
			head, _, _ := strings.Cut(p, "/")
			switch {
			case strings.Contains(head, "."): // golang.org/x/term — a module path
				continue
			case strings.Contains(p, "*"), strings.Contains(p, "..."):
				continue // a glob or a package pattern, not a location
			}
			checked++
			if _, err := os.Stat(p); err != nil {
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
			if strings.Contains(line, "NOT") || strings.Contains(line, "writes an executable") {
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
