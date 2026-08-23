package gooey

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// The version a nested module REQUIRES of core is the one line in this
// repo that nothing in this repo reads.
//
// `go.work` resolves every sibling to the checkout beside it, so the
// require is inert here — CLAUDE.md's "One workspace" section says so
// outright: "`go test` in a nested module is no longer testing that
// module against what it *requires*". CI inherits the same workspace,
// because its legs `cd` into each module inside this checkout. And the
// `replace … => ../..` that nine of these modules carry cannot close the
// gap, because a replace in a DEPENDENCY's go.mod is ignored by whoever
// depends on it; it applies only here, where the workspace has already
// made it redundant.
//
// So the require is read for the first time on a stranger's machine, by
// `go get`. Every nested module said `v0.0.0` — not a tag, not a
// pseudo-version, nothing a proxy can serve:
//
//	go: github.com/WonderForgeLabs/gooey/imagefmt/svg@latest requires
//	    github.com/WonderForgeLabs/gooey@v0.0.0: reading
//	    github.com/WonderForgeLabs/gooey/go.mod at revision v0.0.0:
//	    unknown revision v0.0.0
//
// That made every consumable module in the tree — imagefmt/svg, paint,
// mcp, grpc, handlers/* — impossible to `go get`, and every apps/* one
// impossible to `go install`, while all 25 modules stayed green in here.
// Nobody inside the workspace could reach the failure, which is why it
// survived: the tree is not the environment the line is for.
//
// This test reads the require rather than resolving around it, so it is
// the only mechanism in the repo that can see the defect at all. It
// deliberately does NOT check that a version resolves — that needs the
// network, and this suite is meant to pass against the vendor directory
// with the network off. It checks the sentinel that can never resolve
// under any conditions, which is the part that does not need a proxy to
// know.
func TestNestedModulesRequireAResolvableCoreVersion(t *testing.T) {
	const core = "github.com/WonderForgeLabs/gooey"

	mods := discoverModules(t)
	if len(mods) == 0 {
		t.Fatal("no nested modules found; the walk is wrong, not the tree")
	}

	// A module that requires core at all must name a version that could be
	// served. `packs/*` require nothing of core and are simply skipped —
	// counted below so a walk that stops finding requires is visible rather
	// than passing as "nothing to check".
	checked := 0
	for _, dir := range mods {
		// `go mod edit -json` is textual — it reports what the file says
		// rather than what the workspace would resolve, which is the whole
		// point here. It is also how ci.yml reads the `go` directive, so
		// this adds no dependency the repo does not already rely on.
		cmd := exec.Command("go", "mod", "edit", "-json")
		cmd.Dir = dir
		body, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: go mod edit -json: %v", dir, err)
		}

		var mf struct {
			Require []struct {
				Path    string
				Version string
			}
		}
		if err := json.Unmarshal(body, &mf); err != nil {
			t.Fatalf("%s: parsing go mod edit -json: %v", dir, err)
		}

		for _, r := range mf.Require {
			if r.Path != core {
				continue
			}
			checked++
			if r.Version == "v0.0.0" {
				t.Errorf("%s requires %s v0.0.0, which is neither a tag nor a "+
					"pseudo-version: `go get` of this module fails with "+
					"\"unknown revision v0.0.0\" for everybody outside this "+
					"workspace. Point it at a published commit — "+
					"`go mod edit -C %s -require=%s@$(git rev-parse --short HEAD)` "+
					"resolves to a pseudo-version — and keep any `replace` line, "+
					"which is what makes local development use the checkout.",
					dir, core, dir, core)
				continue
			}
			// Not a resolution check (no network here), just the shape: a
			// version the proxy could be asked for at all.
			if !strings.HasPrefix(r.Version, "v") {
				t.Errorf("%s requires %s %q, which is not a version", dir, core, r.Version)
			}
		}
	}

	if checked == 0 {
		t.Fatalf("none of the %d nested modules were found to require %s; "+
			"either the requires moved or this test stopped reading them, and "+
			"an empty check is not a passing one", len(mods), core)
	}
}
