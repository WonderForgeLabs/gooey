package gooey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every actions/checkout in every workflow must set `lfs: true`.
//
// The repo stores *.wav, *.gif and *.png in Git LFS (.gitattributes), and
// actions/checkout defaults to lfs:FALSE. A job that checks out without it
// gets pointer text where a binary should be — and pointer text is a valid
// file, so nothing errors. A step that reads one sees 130 bytes of
// "version https://git-lfs.github.com/spec/v1" and behaves as though that
// were the content. That is the failure this test exists to prevent: it is
// silent, and it looks like a pass.
//
// Written against the raw text rather than a YAML parse on purpose. The root
// go.mod has exactly two direct requirements and adding a YAML library to
// satisfy a test would be a doctrine change (see CLAUDE.md, "Heavy
// dependencies live in nested modules"). The shape being matched is small
// and stable: a `uses: actions/checkout@…` line, then the `with:` block
// belonging to that step.
func TestEveryCheckoutFetchesLFS(t *testing.T) {
	files, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil {
		t.Fatalf("globbing workflows: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no workflows found — this test would pass vacuously, which is " +
			"the same defect it is written to catch")
	}

	seen := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		lines := strings.Split(string(b), "\n")

		for i, line := range lines {
			col := strings.Index(line, "uses:")
			if col < 0 || !strings.Contains(line, "actions/checkout@") {
				continue
			}
			seen++

			// Walk the keys belonging to this step: anything indented deeper
			// than the `uses:` key's column, plus a `with:` at exactly that
			// column. Stop at the next step or a dedent.
			ok := false
			for j := i + 1; j < len(lines); j++ {
				cur := lines[j]
				if strings.TrimSpace(cur) == "" {
					continue
				}
				indent := len(cur) - len(strings.TrimLeft(cur, " "))
				trimmed := strings.TrimSpace(cur)
				if indent < col || (indent == col && strings.HasPrefix(trimmed, "- ")) {
					break
				}
				if trimmed == "lfs: true" {
					ok = true
					break
				}
			}
			if !ok {
				t.Errorf("%s:%d: actions/checkout without `lfs: true`.\n"+
					"    The repo keeps *.wav, *.gif and *.png in LFS, and checkout "+
					"defaults to lfs:false —\n"+
					"    so this job would get pointer TEXT in place of every one of "+
					"them, with no error.", f, i+1)
			}
		}
	}

	if seen == 0 {
		t.Fatal("no actions/checkout steps found across the workflows — either " +
			"the pattern moved or the workflows did, and either way this test " +
			"was passing without checking anything")
	}
	t.Logf("%d checkout steps across %d workflows, all fetching LFS", seen, len(files))
}
