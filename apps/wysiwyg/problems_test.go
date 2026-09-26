package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
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

// TestAFileThatDoesNotBuildDoesNotShowThePreviousOne: the canvas kept
// the last good preview across a failed build, which is right while
// editing and wrong across an open. Measured in the real binary: opening
// a form whose <TextBox> had no Text left the starting document's "T1"
// and "[ click ]" on the canvas under simple.gooey's name — elements the
// open file does not contain, which a press could not select.
//
// Three arms, because blanking is only half the rule: once the new file
// has built, a later failure is an EDIT, and the last good preview of
// THIS document stays up exactly as before.
func TestAFileThatDoesNotBuildDoesNotShowThePreviousOne(t *testing.T) {
	root := t.TempDir()
	const bad = `<Gooey>
  <Canvas Name="Root">
    <Text Name="L" Style="nosuchstyle">hello</Text>
  </Canvas>
</Gooey>
`
	if err := os.WriteFile(filepath.Join(root, "bad.gooey"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, page := buildPage(t)
	ed.setDispatcher(gooey.NewDispatcher())
	ed.setWorkspace(root)
	c := gooey.NewComposer(page, 160, 48)
	c.Frame()
	canvas := func() string {
		b := ed.pv.Bounds()
		var s strings.Builder
		for y := b.Y; y < b.Y+b.H; y++ {
			s.WriteString(render.SpanText(c.Cells(), b.X, y, b.W))
			s.WriteByte('\n')
		}
		return s.String()
	}
	if !strings.Contains(canvas(), "[ click ]") {
		t.Fatalf("the starting document's button is not on the canvas, so this test "+
			"cannot see it go:\n%s", canvas())
	}

	ed.openWorkspaceFile("bad.gooey")
	if !strings.HasPrefix(ed.status.Get(), "✗") {
		t.Fatalf("the fixture builds (%q); it must not", ed.status.Get())
	}
	c.Frame()
	if got := canvas(); strings.Contains(got, "T1") || strings.Contains(got, "[ click ]") {
		t.Errorf("after opening a file that does not build, the canvas still shows the "+
			"previous document:\n%s", got)
	}

	// Repaired in place: the preview comes back, and it is this file's.
	l := ed.doc().Kids[0]
	delete(l.Attrs, "Style")
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("the repaired document does not build: %q", ed.status.Get())
	}
	c.Frame()
	if got := canvas(); !strings.Contains(got, "hello") {
		t.Errorf("the repaired document is not on the canvas:\n%s", got)
	}

	// And broken again by an edit: the last good preview of THIS
	// document stays, which is the behaviour the blanking must not take.
	l.Attrs["Style"] = "nosuchstyle"
	ed.rebuild()
	if !strings.HasPrefix(ed.status.Get(), "✗") {
		t.Fatalf("the re-broken document builds: %q", ed.status.Get())
	}
	c.Frame()
	if got := canvas(); !strings.Contains(got, "hello") {
		t.Errorf("a failed build mid-edit blanked the canvas; the last good preview of "+
			"the open document should stay:\n%s", got)
	}
}
