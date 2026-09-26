package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// TestTheProblemsPaneSaysWhyTheDocumentDoesNotBuild: the PROBLEMS pane
// bound FitMsg alone, so a document that failed to build left it blank
// and the only account of the failure was the status bar — one row,
// shared with every save and hint, and clipped to what its right-hand
// section left it. Measured at 120x36 on apps/kanban's page: the reason
// ran into "no control plane: …" with no gap and the pane beneath said
// nothing.
//
// The assertion is on a row ABOVE the status bar, because the status
// bar also carries the message at this width and a whole-screen
// Contains passes against the bug.
func TestTheProblemsPaneSaysWhyTheDocumentDoesNotBuild(t *testing.T) {
	root := t.TempDir()
	const bad = `<Gooey>
  <Canvas Name="Root">
    <Text Name="T" Style="nosuchstyle">hi</Text>
  </Canvas>
</Gooey>
`
	const good = `<Gooey>
  <Canvas Name="Root">
    <Text Name="T">hi</Text>
  </Canvas>
</Gooey>
`
	for name, src := range map[string]string{"bad.gooey": bad, "good.gooey": good} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ed, page := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	c := gooey.NewComposer(page, 160, 48)

	ed.openWorkspaceFile("bad.gooey")
	if !strings.HasPrefix(ed.status.Get(), "✗") {
		t.Fatalf("the fixture builds (%q); it must not, or there is nothing to report", ed.status.Get())
	}
	const reason = `no style named "nosuchstyle" is registered`
	if got := ed.problems.Get(); !strings.Contains(got, reason) {
		t.Errorf("Problems reads %q with the document failing to build; want the reason %q", got, reason)
	}
	c.Frame()
	rows := strings.Split(strings.TrimRight(screen(c), "\n"), "\n")
	above := strings.Join(rows[:len(rows)-1], "\n")
	if !strings.Contains(above, reason) {
		t.Errorf("no row above the status bar says %q; the PROBLEMS pane is not showing "+
			"the build error:\n%s", reason, above)
	}

	ed.openWorkspaceFile("good.gooey")
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the healthy fixture does not build: %q", ed.status.Get())
	}
	if got := ed.problems.Get(); got != "" {
		t.Errorf("Problems still reads %q after the document builds; a stale complaint is "+
			"worse than silence", got)
	}
}

// TestAHealthyRebuildDoesNotTouchProblems: sayBuildErr runs on every
// rebuild, and prop.Set does not compare, so an unguarded "" over ""
// would invalidate the PROBLEMS pane on every edit of a document that
// builds. Pinned at the property, as TestTheFitsGuardHoldsWithNoConsumer
// pins its own guard.
func TestAHealthyRebuildDoesNotTouchProblems(t *testing.T) {
	ed, _ := buildPage(t)
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the starting document does not build: %q", ed.status.Get())
	}
	evals := 0
	obs := prop.NewComputed(func() int {
		evals++
		ed.buildErr.Get()
		return evals
	})
	obs.Get()
	settled := evals
	for range 3 {
		ed.rebuild()
		obs.Get()
	}
	if evals != settled {
		t.Errorf("three rebuilds of a healthy document re-evaluated buildErr's observer %d "+
			"times; the write is unguarded", evals-settled)
	}
	// The discrimination: a real change must invalidate, or the loop
	// above is satisfied by a rebuild that never writes at all.
	ed.sayBuildErr("x")
	obs.Get()
	if evals == settled {
		t.Fatal("setting buildErr did not invalidate the observer; this test cannot see a write")
	}
}
