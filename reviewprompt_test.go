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

// expressionBody finds the inside of every `${{ … }}` in a file, with
// quoted string literals stripped out.
//
// It is how a backticked GitHub Actions context reference is told apart
// from a repo path, and the point is that it DERIVES the answer from the
// file rather than carrying a list. `needs.pr-review.outputs` matches the
// path extractor exactly the way `go.mod` does — a dotted, slash-free,
// backticked token — so the extractor cannot separate them, and the
// prompts are workflow files, so such references belong there.
//
// A list of GitHub's context names would work today and is the thing
// #212 is about: hand-maintained, correct when written, silently wrong
// later. What a file does with a token is checkable instead. If the file
// evaluates it as an expression, it is an expression.
//
// Quoted literals are stripped because that is the hole the naive
// version leaves: `hashFiles('examples/gitui/go.sum')` would excuse a
// stale path merely for appearing inside an expression, which is the
// exact drift this test exists to catch (both `examples/gitui` and
// `examples/kanbandemo` really did go stale that way across #238/#268).
// Only an UNQUOTED operand counts.
var (
	// (?s) so `.` spans newlines. Both prompt files fold expressions
	// across lines inside `if:` and `run:` block scalars, and without the
	// flag those bodies are invisible to this extractor — which fails
	// CLOSED, reproducing the exact red this test exists to remove, one
	// YAML style over.
	expressionBody = regexp.MustCompile(`(?s)\$\{\{(.*?)\}\}`)
	quotedLiteral  = regexp.MustCompile(`'[^']*'|"[^"]*"`)
)

// identByte reports whether c can sit inside a single expression operand.
// The set is deliberately wider than an identifier: `.` and `-` are in it
// because `needs.pr-review.outputs` is ONE operand, and the point of the
// boundary test is to refuse a match that starts or ends inside one.
func identByte(c byte) bool {
	return c == '.' || c == '-' || c == '_' ||
		c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// operandRef reports whether token appears in body as a whole operand or
// as a leading segment of one.
//
// The leading-segment case is load-bearing rather than sloppy: the
// backticked prose says `needs.pr-review.outputs` while the expression
// evaluates `needs.pr-review.outputs.outcome`, so exact equality would
// miss the very token this test was written for. What it must NOT accept
// is a match at an arbitrary offset — against that same body, plain
// substring matching also excuses `review.outputs`, `outputs.outcome`
// and `s.pr-review.o`, none of which is an operand anyone wrote.
func operandRef(body, token string) bool {
	for i := 0; i+len(token) <= len(body); {
		j := strings.Index(body[i:], token)
		if j < 0 {
			return false
		}
		st := i + j
		en := st + len(token)
		startsClean := st == 0 || !identByte(body[st-1])
		// A trailing `.` is the leading-segment case; anything else that
		// could continue an operand is a match inside one.
		endsClean := en == len(body) || body[en] == '.' || !identByte(body[en])
		if startsClean && endsClean {
			return true
		}
		i = st + 1
	}
	return false
}

func isExpressionRef(token, body string) bool {
	// A SLASH SETTLES IT STRUCTURALLY, before any body is examined.
	// GitHub's expression grammar has no arithmetic operators, so `/`
	// cannot appear unquoted inside `${{ … }}` — a slash-bearing token
	// can only reach an expression body through a string literal.
	//
	// That makes this a derived, complete discriminator for exactly the
	// class this test exists to catch: `examples/gitui` and
	// `examples/kanbandemo` are the paths that really went stale across
	// #238/#268, and they are slash-bearing. Leaving them to the
	// quote-stripper meant any hole in it reopened the hole in the test;
	// here they are unexcusable by construction.
	if strings.Contains(token, "/") {
		return false
	}
	for _, m := range expressionBody.FindAllStringSubmatch(body, -1) {
		if operandRef(quotedLiteral.ReplaceAllString(m[1], " "), token) {
			return true
		}
	}
	return false
}

// forbidsGoBuild recognises a line that mentions `go build ./...` in order
// to PROHIBIT it. `\bNOT\b` and not `strings.Contains(…, "NOT")`, because
// the latter is satisfied by NOTE, NOTHING, ANNOTATION and DENOTES.
var forbidsGoBuild = regexp.MustCompile(`\bNOT\b`)

func TestReviewPromptsNameOnlyPathsThatExist(t *testing.T) {
	seen := map[string]bool{}
	slashChecked := 0
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
			case isExpressionRef(p, body):
				continue // the file evaluates it as ${{ … }} — an expression, not a path
			}
			checked++
			if hasSlash {
				slashChecked++
			}
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

	// A SLASH-BEARING PATH MUST STILL BE CHECKED SOMEWHERE. The vacuity
	// guard above catches an extractor that stopped matching anything;
	// this catches an EXCLUSION that grew until it excused a whole class.
	// The class is the one that really broke — `examples/gitui` and
	// `examples/kanbandemo` across #238/#268 — and `authorities` cannot
	// stand in for it, because both authorities are slash-free: an
	// exclusion that swallowed every path with a `/` in it would leave
	// those two alone and stay green.
	//
	// ACROSS the corpus rather than per file, and that is measured rather
	// than assumed. claude.yml contains exactly two slash-bearing tokens,
	// `./...` and `github.com/WonderForgeLabs/gooey`, and BOTH are
	// correctly excluded — one a package pattern, one a module path. A
	// per-file floor would therefore assert something untrue of that file
	// and fail for a reason that has nothing to do with the exclusion.
	if slashChecked == 0 {
		t.Error("no path containing a `/` survived to be checked in any prompt file — " +
			"an exclusion has grown to cover the whole class of path that went stale " +
			"in #238/#268, and nothing else in this test would notice.")
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

// The exclusion above is a guard that can only ever say "skip", so the
// case that matters is the one where it must NOT: a stale path is
// excused the moment the rule is too loose, and nothing downstream would
// notice, because being excused looks exactly like being correct.
func TestExpressionRefsAreToldApartFromPaths(t *testing.T) {
	const body = "\n" +
		"      # re-running the `merge-gate` check is free, since `needs.pr-review.outputs` survives\n" +
		"        env:\n" +
		"          REVIEW_OUTCOME: ${{ needs.pr-review.outputs.outcome }}\n" +
		"          CACHE_KEY: ${{ hashFiles('examples/gitui/go.sum') }}\n" +
		"          DOC_KEY: ${{ hashFiles('CLAUDE.md') }}\n" +
		"          MODE: ${{ matrix.mode }}\n" +
		// Slash-bearing and UNQUOTED, which GitHub's grammar cannot
		// actually produce — that is exactly why it belongs here. It is
		// the one input the quote-stripper cannot reject, so it is what
		// makes the structural slash rule observable on its own.
		"          BAD: ${{ docs/architecture.md }}\n"
	// A FOLDED expression, which is how both prompt files really write
	// their `if:` conditions. Without (?s) the extractor cannot see this
	// body at all and the exclusion fails closed.
	const folded = "        if: >-\n          ${{ github.event_name == 'push' &&\n" +
		"              needs.pr-review.outputs.outcome == 'success' }}\n"
	if !isExpressionRef("needs.pr-review.outputs", folded) {
		t.Error("a multi-line ${{ … }} is invisible to expressionBody: a folded condition " +
			"is ordinary YAML here, and missing it turns this test red for the exact " +
			"reason it was written to stop")
	}

	for _, c := range []struct {
		token string
		want  bool
		why   string
	}{
		{"needs.pr-review.outputs", true,
			"the file evaluates it unquoted in ${{ … }}, so it is an expression"},
		{"matrix.mode", true,
			"same, and with a single dot — the rule must not depend on segment count"},
		{"examples/gitui/go.sum", false,
			"it appears ONLY inside a quoted literal. Excusing it is how a stale path " +
				"survives a rename (#238, #268) with the test still green"},
		{"CLAUDE.md", false,
			"slash-free and QUOTED — the one shape only the quote-stripper can reject, " +
				"since the structural slash rule has nothing to bite on"},
		{"docs/architecture.md", false,
			"slash-bearing and UNQUOTED — the mirror case, which only the structural " +
				"slash rule can reject, since there is no quote to strip"},
		{"steps.x.outputs.docs/architecture.md", false,
			"a slash cannot appear unquoted in an expression, so a slash-bearing token " +
				"is never an operand however it looks"},
		{"review.outputs", false,
			"a mid-operand substring of needs.pr-review.outputs — nobody wrote this token"},
		{"outputs.outcome", false,
			"a suffix of the same operand, and equally not one"},
		{"s.pr-review.o", false,
			"an arbitrary offset inside the operand"},
	} {
		if got := isExpressionRef(c.token, body); got != c.want {
			t.Errorf("isExpressionRef(%q) = %v, want %v — %s", c.token, got, c.want, c.why)
		}
	}
}
