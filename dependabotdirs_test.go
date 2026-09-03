package gooey

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every nested module must be covered by a `directories` entry, or it is
// silently never updated.
//
// `.github/dependabot.yml` documents this as its one maintenance edge: a new
// top-level namespace is not matched by any existing glob, "and nothing will
// say so — it will simply never be updated". That is the exact shape #207 is
// about, and the one CLAUDE.md's whole Verify section exists to remove: a
// config that quietly stops covering a module while everything stays green.
// `ci.yml` earned `TestCIWorkflowDiscoversEveryNestedModule` by losing this
// twice — `paint/` and every `apps/*` module went unbuilt behind a wall of
// green. This is the same test for the same failure in a different file.
//
// Removing the count from the prose (which an earlier commit did, correctly)
// only deleted the ASSERTION. The coverage claim underneath it was still
// unchecked. This checks it. Raised in review of #453.
type ghDependabot struct {
	Updates []struct {
		Ecosystem   string   `yaml:"package-ecosystem"`
		Directories []string `yaml:"directories"`
		Directory   string   `yaml:"directory"`
	} `yaml:"updates"`
}

func TestDependabotDirectoriesCoverEveryNestedModule(t *testing.T) {
	b, err := os.ReadFile(".github/dependabot.yml")
	if err != nil {
		t.Skipf("no .github/dependabot.yml in this tree (%v) — nothing to check", err)
	}
	var cfg ghDependabot
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		t.Fatalf(".github/dependabot.yml does not parse as YAML: %v", err)
	}

	// Expand the config's entries to the module directories they actually
	// reach, the same way a reader would have to in their head.
	var covered []string
	entries := 0
	for _, u := range cfg.Updates {
		if u.Ecosystem != "gomod" {
			continue
		}
		pats := u.Directories
		if u.Directory != "" {
			pats = append(pats, u.Directory)
		}
		for _, pat := range pats {
			entries++
			// Entries are repo-absolute ("/apps/*"); filepath.Glob wants them
			// relative to this package, which IS the repo root.
			rel := strings.TrimPrefix(pat, "/")
			if rel == "" {
				continue // the root module; see below
			}
			hits, err := filepath.Glob(rel)
			if err != nil {
				t.Errorf("dependabot.yml: %q is not a valid glob: %v", pat, err)
				continue
			}
			reached := 0
			for _, h := range hits {
				if _, err := os.Stat(filepath.Join(h, "go.mod")); err == nil {
					covered = append(covered, filepath.ToSlash(h))
					reached++
				}
			}
			// AN ENTRY THAT REACHES NOTHING is the other way this list rots,
			// and the direction a set-difference cannot see: `covered` is
			// built only from directories that have a go.mod, so it is a
			// subset of the discovered modules by construction and comparing
			// the two that way can never fail. (The first draft of this test
			// did exactly that and I only found out by mutating a stale entry
			// in and watching nothing happen.) A namespace that gets renamed
			// leaves a glob matching zero modules, and it reads as coverage.
			if reached == 0 {
				t.Errorf("dependabot.yml: %q matches no directory containing a "+
					"go.mod. Either the path is stale — a renamed or removed "+
					"namespace — or it never matched, and either way it reads "+
					"as coverage that is not there.", pat)
			}
		}
	}
	if entries == 0 {
		t.Fatal("no gomod `directories` entries found — this test would pass " +
			"vacuously, which is the same defect it exists to catch")
	}
	sort.Strings(covered)

	// discoverModules excludes the repository root by construction, which is
	// what this comparison wants: `/` is deliberately absent from the config
	// (dependabot.yml explains why — it is the only directory that enters
	// Dependabot's vendoring path, and `go mod vendor` refuses in workspace
	// mode). So the root being uncovered is the documented decision, and
	// every OTHER module being covered is the invariant.
	want := discoverModules(t)

	if missing := missingFrom(covered, want); len(missing) > 0 {
		t.Errorf("dependabot.yml covers no directory for %v.\n"+
			"\tA module no `directories` entry matches is never updated, and "+
			"nothing reports it — the failure this file names as its one "+
			"maintenance edge. Add a glob for the namespace.", missing)
	}
}
