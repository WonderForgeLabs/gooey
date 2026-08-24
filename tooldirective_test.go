package gooey

import (
	"encoding/json"
	"os/exec"
	"testing"
)

// toolHost is the one module allowed to carry `tool` directives. It has no
// importable packages of its own, which is the entire reason it exists.
const toolHost = "tools"

// A `tool` directive is not free to the people who import you.
//
// `go get -tool` records the tool plus its whole dependency graph as
// `// indirect` requires of the module holding the directive, because that
// is what building the tool needs. Those requires are then part of what MVS
// hands to anyone who depends on that module — a consumer inherits them
// whether or not they ever run the tool, and any module they share with the
// tool's graph gets pulled up to the tool's version.
//
// buf's directive used to live in the root go.mod, for a good reason: `go
// tool` resolves through the main module and therefore through vendor/,
// which is what stopped CI going to the proxy for buf and its ~60
// transitive modules (the `contract` job carries the measurement). The cost
// was invisible from in here and severe from outside: importing gooey
// obliged you to buf, Docker's CLI, quic-go, cel-go and about ninety more,
// and forced upgrades of anything you had in common with them. A consumer
// pinning an older Docker stack could not take gooey at all — the
// resolution succeeded and then failed to compile, several modules away
// from anything they had asked for.
//
// Nothing in this repo could see that, for the same reason nothing could
// see the `v0.0.0` requires that TestNestedModulesRequireAResolvableCore-
// Version now guards: the workspace resolves siblings to the checkout and
// CI inherits it, so the published requirement set is never the one being
// built against. The fix keeps the vendored `go tool` mechanism and moves
// the directive into `tools`, a module nobody imports.
//
// This test is what stops it drifting back. It does not forbid tools — it
// forbids them in modules somebody can import.
func TestToolDirectivesStayOffTheImportableModules(t *testing.T) {
	// The root module is the one every consumer imports, so it is checked
	// alongside the nested ones rather than assumed innocent — the root is
	// exactly where the directive was.
	mods := append([]string{"."}, discoverModules(t)...)

	sawHost := false
	for _, dir := range mods {
		cmd := exec.Command("go", "mod", "edit", "-json")
		cmd.Dir = dir
		body, err := cmd.Output()
		if err != nil {
			t.Fatalf("%s: go mod edit -json: %v", dir, err)
		}

		var mf struct {
			Tool []struct{ Path string }
		}
		if err := json.Unmarshal(body, &mf); err != nil {
			t.Fatalf("%s: parsing go mod edit -json: %v", dir, err)
		}

		if dir == toolHost {
			sawHost = true
			continue
		}
		for _, tool := range mf.Tool {
			t.Errorf("%s declares `tool %s`, which puts that tool's entire "+
				"dependency graph into %s/go.mod as indirect requires and hands "+
				"them to everyone who imports it. Move the directive to %s/ "+
				"(`go -C %s get -tool %s`) and drop it here; the vendored "+
				"`go tool` mechanism keeps working, because go.work makes %s "+
				"share the root vendor/.",
				dir, tool.Path, dir, toolHost, toolHost, tool.Path, toolHost)
		}
	}

	// If `tools` disappears or is renamed, the loop above still passes while
	// silently checking nothing about the exemption — and the next `go get
	// -tool` has nowhere obvious to go but the root module, which is the
	// state this test exists to prevent returning to.
	if !sawHost {
		t.Errorf("no %s module found; it is the only place `tool` directives "+
			"are allowed, so removing it leaves the next tool no home but an "+
			"importable module", toolHost)
	}
}
