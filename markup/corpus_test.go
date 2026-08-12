package markup

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryGooeyFileInTheRepoHasValidAttributes is the completeness
// measurement for unknown-attribute rejection, and it is the deliverable
// rather than a side effect.
//
// The generator's guard can only catch attributes it can SEE and fails
// to classify. It is structurally unable to catch one it never looks at
// — an attribute read through a parameter, or in a loop over a table.
// Enforcement is what closes that gap: once an unknown attribute is a
// load error, every attribute the catalog missed shows up as a failure
// against real markup.
//
// The unit tests are one corpus. The .gooey files shipped in the repo
// are the other, and the more important one: they are the markup nobody
// wrote a test for, loaded at runtime by the demos, where a missed
// attribute would otherwise surface as a demo that no longer starts.
//
// This walks every one of them and checks the attributes on every
// element. It does not BUILD them — that would need each app's binding
// context — so it is a pure vocabulary check, which is exactly the axis
// rejection added.
func TestEveryGooeyFileInTheRepoHasValidAttributes(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &Context{}
	var checked, files int

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable corners of a working tree are not our business
		}
		if d.IsDir() {
			// Other agents' worktrees live under .claude and are not
			// this tree's markup.
			if name := d.Name(); name == ".git" || name == ".claude" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".gooey") {
			return nil
		}
		files++
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rootEl, _, err := parse(src)
		if err != nil {
			// A malformed document is a different test's problem.
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		var walk func(e Element)
		walk = func(e Element) {
			checked++
			if err := checkAttrs(e, ctx); err != nil {
				t.Errorf("%s: %v", rel, err)
			}
			for _, c := range e.Children {
				walk(c)
			}
			for _, p := range e.Props {
				for _, c := range p.Children {
					walk(c)
				}
			}
		}
		walk(rootEl)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("no .gooey files found; this check is measuring nothing")
	}
	t.Logf("checked %d elements across %d .gooey files", checked, files)
}
