package gooey

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every actions/checkout in every workflow must set `lfs: true`.
//
// The repo stores *.wav, *.gif and *.png in Git LFS (.gitattributes), and
// actions/checkout defaults to lfs:FALSE. A job that checks out without it
// does not fail — it gets pointer TEXT where a binary should be, and a
// pointer file is a perfectly valid file. A step that reads one sees 130
// bytes beginning "version https://git-lfs.github.com/spec/v1" and carries
// on as though that were the content. Silent, and it looks like a pass.
//
// "Always, everywhere" is a rule that decays at the next workflow somebody
// adds — which is exactly how ci.yml came to miss paint/ and every apps/*
// module, twice, with every job green. So it is checked rather than
// asserted in prose.
//
// This parses the YAML rather than matching text. The first version scanned
// lines, and the shapes it had to cope with — no `with:` at all, `with:` on
// the next line, `uses:` sitting under a `- name:` — were only the ones this
// repo happens to use today. A quoted key, a flow mapping, or a comment
// containing the words `uses: actions/checkout@` would each have fooled it
// in a different direction. A parser has no opinion about layout.
type ghWorkflow struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string         `yaml:"name"`
			Uses string         `yaml:"uses"`
			With map[string]any `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func TestEveryCheckoutFetchesLFS(t *testing.T) {
	files, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil {
		t.Fatalf("globbing workflows: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no workflows found — this test would pass vacuously, which is " +
			"the same defect it exists to catch")
	}

	seen := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		var wf ghWorkflow
		if err := yaml.Unmarshal(b, &wf); err != nil {
			t.Errorf("%s: does not parse as YAML: %v", f, err)
			continue
		}

		for job, j := range wf.Jobs {
			for i, s := range j.Steps {
				if !isCheckout(s.Uses) {
					continue
				}
				seen++
				if s.With["lfs"] != true {
					where := s.Name
					if where == "" {
						where = s.Uses
					}
					t.Errorf("%s: job %q step %d (%s) checks out without `lfs: true`.\n"+
						"    The repo keeps *.wav, *.gif and *.png in LFS, and checkout "+
						"defaults to lfs:false —\n"+
						"    so this job gets pointer TEXT in place of every one of them, "+
						"with no error raised.",
						f, job, i+1, where)
				}
			}
		}
	}

	if seen == 0 {
		t.Fatal("no actions/checkout steps found across the workflows — either " +
			"the action moved or the workflows did, and either way this test was " +
			"passing without checking anything")
	}
	t.Logf("%d checkout steps across %d workflows, all fetching LFS", seen, len(files))
}

// isCheckout matches actions/checkout at any version, and only as the whole
// action — `someone/actions-checkout@v1` is a different action.
func isCheckout(uses string) bool {
	const want = "actions/checkout"
	if len(uses) < len(want) {
		return false
	}
	if uses[:len(want)] != want {
		return false
	}
	return len(uses) == len(want) || uses[len(want)] == '@'
}
