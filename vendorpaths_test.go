package gooey

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// The vendor workflows must not be blind to the ROOT module.
//
// vendor-freshness.yml guards the one-root-vendor invariant and
// vendor-autofix.yml repairs it, both keyed on a `paths:` filter. The
// filter is where that guard can silently stop covering something, and the
// most consequential blind spot is the smallest: `'**/go.mod'` is anything
// followed by a LITERAL `/go.mod`, so under GitHub's documented globbing —
// where `**` is a character-class rule rather than minimatch's special-cased
// `**/` — the repository-root `go.mod`, whose path is just `go.mod` with no
// slash, is not matched. It is why GitHub's own example for "any .js file
// anywhere" is `'**.js'` and not `'**/*.js'`.
//
// The root is the module whose stale vendor/ breaks every OTHER module's
// build, so a filter that covers 25 modules and misses that one reproduces
// the exact failure the workflows exist to remove — arriving through the
// trigger instead of through a missing step. Raised in review of #452.
//
// WHAT THIS TEST DELIBERATELY DOES NOT DO is evaluate the glob. Writing a
// matcher here would mean encoding my reading of GitHub's semantics into
// the test that checks the workflow written from that same reading — and if
// the reading is wrong, both are wrong together and the test agrees with the
// bug. (The repo has been bitten by exactly that shape: a fixture that could
// not express the defect it was written for.) I could not confirm the
// semantics from the docs either way.
//
// So it asserts the property that is true under BOTH readings: the bare
// entries are present alongside the `**` ones. If `**/go.mod` does cover the
// root, the bare entry is harmless duplication; if it does not, the bare
// entry is the whole coverage. Being right about GitHub's globbing stops
// being load-bearing, which is the point.
type ghPathsWorkflow struct {
	On map[string]struct {
		Paths []string `yaml:"paths"`
	} `yaml:"on"`
}

func TestVendorWorkflowsCoverTheRootModule(t *testing.T) {
	// The two files and the event each keys on. Named rather than globbed:
	// these are the only two workflows whose correctness depends on a paths
	// filter, and a new one should have to opt in here consciously.
	for _, tc := range []struct {
		file  string
		event string
	}{
		{".github/workflows/vendor-freshness.yml", "pull_request"},
		{".github/workflows/vendor-autofix.yml", "pull_request_target"},
	} {
		b, err := os.ReadFile(tc.file)
		if err != nil {
			t.Fatalf("reading %s: %v", tc.file, err)
		}
		var wf ghPathsWorkflow
		if err := yaml.Unmarshal(b, &wf); err != nil {
			t.Fatalf("%s: does not parse as YAML: %v", tc.file, err)
		}
		on, ok := wf.On[tc.event]
		if !ok {
			t.Errorf("%s: no `%s:` trigger — this test is checking a filter that "+
				"is no longer there, which means it now passes vacuously",
				tc.file, tc.event)
			continue
		}
		if len(on.Paths) == 0 {
			t.Errorf("%s: `%s:` has no paths filter. An unfiltered trigger is not a "+
				"failure of this invariant, but it is a deliberate change and this "+
				"test should be updated with it rather than around it",
				tc.file, tc.event)
			continue
		}

		have := map[string]bool{}
		for _, p := range on.Paths {
			have[p] = true
		}
		// go.work / go.work.sum have no directory prefix either, and are the
		// files that change when a module JOINS the workspace — the case where
		// vendor/ goes stale without any single go.mod being touched.
		for _, want := range []string{
			"go.mod", "go.sum", // the ROOT module, explicitly
			"**/go.mod", "**/go.sum", // every nested module, any depth
			"go.work", "go.work.sum",
		} {
			if !have[want] {
				t.Errorf("%s: `%s:` paths filter is missing %q.\n"+
					"\tThe bare entries and the ** entries are BOTH required, on "+
					"purpose: `**/go.mod` is anything followed by a literal "+
					"`/go.mod`, so it does not match the root module's own "+
					"go.mod. Keeping both means the guard covers the root "+
					"whichever way GitHub's globbing actually reads.\n"+
					"\thave: %v", tc.file, tc.event, want, on.Paths)
			}
		}
	}
}
