package gooey

import (
	"fmt"
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

// TestTheLegThatRunsTheRootModuleGetsFullHistory pins the OTHER thing
// ci.yml's checkout step decides, and it is the half that used to be
// remembered rather than derived.
//
// Several guards in the root suite read git history — the own-module
// pins are checked for existence, for their commit stamp, and for
// ancestry of origin/main — and `actions/checkout` produces
// `refs/remotes/origin/main` and the parent commits they need ONLY at
// `fetch-depth <= 0`. At depth 1 on a pull request it fetches the merge
// commit and none of its parents, and `pinCoverage` reds the suite with
// "NOTHING was checked here".
//
// The step used to spell that requirement as `matrix.mode == 'test'` —
// the tier NAME, not the leg carrying the root module — with the bridge
// between them in a prose comment. `discover` derives a `depth` per leg
// now, and this asserts the two halves of that: the expression is read
// off the matrix rather than recomputed here, and the matrix field is
// computed from whether the leg holds `.`. Raised in review of #497.
func TestTheLegThatRunsTheRootModuleGetsFullHistory(t *testing.T) {
	b, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("reading ci.yml: %v", err)
	}
	var wf ghWorkflow
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("ci.yml does not parse as YAML: %v", err)
	}

	// EVERY CHECKOUT IN THE FILE, not one job by name. Exactly one of
	// them departs from the action's default, and which job holds it is
	// not this test's business — the job that runs the module matrix has
	// been renamed once already.
	set := map[string]string{}
	for job, j := range wf.Jobs {
		for i, st := range j.Steps {
			if !isCheckout(st.Uses) {
				continue
			}
			if v, ok := st.With["fetch-depth"]; ok {
				set[fmt.Sprintf("job %q step %d", job, i+1)] = fmt.Sprint(v)
			}
		}
	}
	if len(set) != 1 {
		t.Fatalf("ci.yml has %d checkout step(s) setting fetch-depth, want "+
			"exactly 1: %v. The other checkouts ask git nothing about history, "+
			"and a second one departing from the default is either a duplicate "+
			"of this rule or a new one nothing here explains", len(set), set)
	}
	for where, got := range set {
		if got != "${{ matrix.depth }}" {
			t.Errorf("ci.yml: %s sets fetch-depth to %q. It must read the "+
				"matrix's derived `depth`: keying full history on the tier NAME "+
				"is a coupling to the `case` in discover that nothing checks, and "+
				"that `case` carries its own proposal to invert itself — after "+
				"which the root module is in the `race` leg at depth 1 and the "+
				"root suite reds pointing at a fetch-depth: 0 on the wrong leg",
				where, got)
		}
	}

	// THE DERIVATION ITSELF is TestCIMatrixPackingPartitionsEveryModule's,
	// which extracts ci.yml's own jq program and RUNS it over a fixture
	// where the root module sits in the `test` tier — so the two halves
	// are pinned by execution rather than by two copies of a string. This
	// test owns only the half it can see: that the step reads the
	// matrix's answer instead of recomputing one.
}
