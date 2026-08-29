package markup

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// buildCall matches a test that builds a page through one of markup's
// three page entry points: Page(fsys, name, ctx).Build(), Load(fsys,
// name, ctx), and Build(src, ctx).
//
// Qualified deliberately. An unqualified alternative would match .Build()
// on ANY receiver, and three apps happened to satisfy an earlier version
// of this pattern that way — correctly, as it turned out, but by
// accident, which is the same thing as not being checked. Package markup
// needs no bare form: the root module it lives in is vouched for many
// times over by cmd/* tests that call markup.Load.
//
// The list is the API, so a NEW entry point added to markup and not added
// here silently stops counting. There is no mechanical guard for that;
// markup/page.go, and Load/Build in markup/markup.go, are the surface.
var buildCall = regexp.MustCompile(`\bmarkup\.(Page|Load|Build)\(`)

// TestEveryModuleShippingAPageBuildsOneInATest closes the axis
// TestEveryGooeyFileInTheRepoHasValidAttributes states it cannot cover.
//
// That test checks attribute NAMES against the vocabulary, with an empty
// Context, because it has no app's bindings to build against. So it
// catches <FileWatcher Recursive="true"/> — an undeclared attribute —
// and cannot catch Changed="{{.ReloadCounter}}" naming a value no
// viewmodel supplies. That resolves only at build, against each app's
// own Context.Values, and the failure mode is an app whose page passes
// the corpus test and dies at startup: exactly the class the vocabulary
// work exists to eliminate, surviving one axis over.
//
// The obvious fix is not available. Making the corpus test build the
// pages would need every app's binding context, which is why it was
// written as a vocabulary check in the first place. So the contract is a
// convention — an app owns a test that builds its own pages against its
// own Context — and a convention is worth nothing without something that
// notices when it is not followed. This is that something.
//
// It is deliberately a PRESENCE check, not a quality one. It reads test
// SOURCE for a build call rather than running anything, so a test that
// calls Load and discards the error satisfies it. The gap it closes is
// the one that was actually costing: an app with no build test at all,
// where the number of pages ever built was zero.
//
// DERIVED, never a list. Both halves — which modules ship a page, and
// which own a build test — are computed from the tree, so adding an app
// with a page and no test turns this red without anyone maintaining
// anything. A written list of app names would be stale the first time
// someone added one, and would fail silently: the loop still passes.
func TestEveryModuleShippingAPageBuildsOneInATest(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}

	// Every module directory, so a file can be attributed to the nearest
	// one above it. Attribution by module is the whole subtlety: the root
	// module ships 30-odd pages and has plenty of build tests, and a naive
	// recursive scan from the root would let its tests vouch for every
	// nested app underneath it.
	modules := map[string]bool{}
	pages := map[string][]string{}
	builders := map[string][]string{}

	walk := func(collect func(path string, d fs.DirEntry)) {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable corners of a working tree are not our business
			}
			if d.IsDir() {
				// Pruned at EVERY depth, not just the top. .claude holds
				// other agents' whole checkouts and a gitignored .venv can
				// vendor a third party's Go tree; either one would be
				// attributed to a module in THIS repo.
				if name := d.Name(); name != "." && strings.HasPrefix(name, ".") {
					return filepath.SkipDir
				}
				if d.Name() == "node_modules" || d.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			collect(path, d)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	walk(func(path string, d fs.DirEntry) {
		if d.Name() == "go.mod" {
			modules[filepath.Dir(path)] = true
		}
	})
	if len(modules) == 0 {
		t.Fatal("no go.mod found, so every module below would be attributed to nothing — the walk is broken")
	}

	owner := func(path string) string {
		for d := filepath.Dir(path); ; d = filepath.Dir(d) {
			if modules[d] {
				return d
			}
			if d == root || d == filepath.Dir(d) {
				return root
			}
		}
	}

	walk(func(path string, d fs.DirEntry) {
		switch {
		case strings.HasSuffix(path, ".gooey"):
			m := owner(path)
			pages[m] = append(pages[m], path)
		case strings.HasSuffix(path, "_test.go"):
			src, err := os.ReadFile(path)
			if err != nil || !buildCall.Match(src) {
				return
			}
			m := owner(path)
			builders[m] = append(builders[m], path)
		}
	})

	if len(pages) == 0 {
		t.Fatal("no .gooey files found; this check is measuring nothing")
	}

	var missing []string
	for m := range pages {
		if len(builders[m]) > 0 {
			continue
		}
		rel, _ := filepath.Rel(root, m)
		var names []string
		for _, p := range pages[m] {
			r, _ := filepath.Rel(root, p)
			names = append(names, r)
		}
		sort.Strings(names)
		missing = append(missing, rel+" ships "+strings.Join(names, ", "))
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("these modules ship a .gooey page and no test builds one:\n  %s\n\n"+
			"A page is only known to load once something builds it against the app's own\n"+
			"Context. The corpus test checks attribute NAMES with an empty Context and\n"+
			"cannot see a binding that resolves to nothing, so without a build test the\n"+
			"first thing to discover a broken page is a user starting the app.\n"+
			"Add a test that calls markup.Load with the app's real Context — see\n"+
			"apps/introdeck/deckwatch_test.go for the shape.",
			strings.Join(missing, "\n  "))
	}
	t.Logf("%d modules ship pages; %d have a build test", len(pages), len(pages)-len(missing))
}
