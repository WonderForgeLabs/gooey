package gooey

import (
	"os/exec"
	"strings"
	"testing"
)

// `review-with-tracking` is the check name reserved so a branch ruleset can
// require it later. Its assertion used to be four lines of inline YAML, and
// inline YAML cannot be run — which is how it came to report success in every
// situation that means no review exists (#198): a label event skipped every
// internal job of the reusable workflow, the mirror saw `skipped`, exited 0,
// and a red check went green on the same commit.
//
// The assertion now lives in a script so it can be exercised, and these tests
// are what stop that from being cosmetic. The first pins that the workflow
// actually calls it; the second runs the script's own suite, so a script
// edited into a no-op reds `go test ./...` rather than quietly greening a
// check nobody re-reads.

const (
	reviewWorkflow = ".github/workflows/claude-code-review.yml"
	assertScript   = ".github/scripts/assert-review-rendered.sh"
	assertSuite    = ".github/scripts/assert-review-rendered_test.sh"
)

func TestReviewMirrorDelegatesToTheAssertionScript(t *testing.T) {
	body := readFileString(t, reviewWorkflow)

	if !strings.Contains(body, assertScript) {
		t.Fatalf("%s must run %s.\n\nThe mirror job's assertion was inlined YAML once, and "+
			"an assertion that cannot be executed outside a PR is one nobody can show is "+
			"wrong — which is the whole of #198.", reviewWorkflow, assertScript)
	}

	// The pre-#198 shape, kept as a named negative: mirroring the call's
	// control flow answers "did the last run avoid failing", and a required
	// check has to answer "was this COMMIT reviewed". Those come apart on
	// any label event.
	if strings.Contains(body, `"$RESULT" != "skipped"`) {
		t.Fatalf("%s still decides from needs.review.result alone. `skipped` means "+
			"\"no new review was needed\", which is only true when an existing review "+
			"covers this head — checking the result without checking the head is the "+
			"bypass #198 was filed for.", reviewWorkflow)
	}
}

func TestReviewMirrorAssertionSuitePasses(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		// The suite stubs the GitHub API with real JSON so the jq
		// expressions are exercised rather than bypassed. Skipping is
		// honest here; pretending to have run it is not.
		t.Skip("jq not installed — the assertion suite stubs the API with real JSON")
	}

	out, err := exec.Command("sh", assertSuite).CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed:\n%s", assertSuite, out)
	}

	// The suite prints its own total on the last line, the same convention
	// the rest of this repo's shell suites follow. A count is not written
	// here on purpose — a number in a Go string is a sample taken once, and
	// this repo has paid for that twice already.
	trimmed := strings.TrimSpace(string(out))
	last := trimmed[strings.LastIndex(trimmed, "\n")+1:]
	if !strings.HasPrefix(last, "REVIEW-MIRROR: ") || strings.Contains(last, "FAILED") {
		t.Fatalf("%s did not report a clean total; last line was %q\n\n%s", assertSuite, last, out)
	}
}
