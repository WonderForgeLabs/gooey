package main

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// fakeDocs is a docs tree with the shape the real one has — top-level
// pages, a nested directory, and a non-markdown file that must not
// appear — so the ordering and the filter are both exercised.
func fakeDocs() fstest.MapFS {
	return fstest.MapFS{
		"architecture.md":         {Data: []byte("# Architecture\nthe grounded walkthrough")},
		"learn/01-first-app.md":   {Data: []byte("# First app\nhello")},
		"learn/howto/testing.md":  {Data: []byte("# Testing\nheadless")},
		"specs/2026-08-10-tty.md": {Data: []byte("# TTY\nlifecycle")},
		"logo.png":                {Data: []byte("\x89PNG")},
		"Makefile":                {Data: []byte("all:")},
	}
}

// docsPage builds the editor over the shipped page with a KNOWN docs
// tree substituted, which is the whole point of the fs.FS seam: the test
// does not depend on the repo it runs inside.
func docsPage(t *testing.T, fsys fstest.MapFS) (*editor, *gooey.Composer) {
	t.Helper()
	ed := newEditor(editorFS())
	ed.docsRoot = fsys
	ed.docList = docsPages(fsys)
	ed.docCache = nil
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}
	root, err := markup.Build(src, ed.ctx)
	if err != nil {
		t.Fatalf("the editor's own page does not load: %v", err)
	}
	ed.rebuild()
	c := gooey.NewComposer(root, 160, 48)
	t.Cleanup(c.Close)
	c.Frame()
	return ed, c
}

// TestTheDocsListIsTheMarkdownUnderTheTree is the pane's source of
// truth, and it is asserted against a substituted FS rather than the
// repo's own docs/ — which is the acceptance criterion about the seam,
// stated as a test rather than as a claim.
func TestTheDocsListIsTheMarkdownUnderTheTree(t *testing.T) {
	got := docsPages(fakeDocs())
	want := []string{
		"architecture.md",
		"learn/01-first-app.md",
		"learn/howto/testing.md",
		"specs/2026-08-10-tty.md",
	}
	if len(got) != len(want) {
		t.Fatalf("listed %d pages %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i].Path != want[i] {
			t.Errorf("page %d is %q, want %q — the list is sorted by PATH so a "+
				"directory's pages arrive together", i, got[i].Path, want[i])
		}
	}
	// The filter is the discriminating half: a tree of only .md files
	// would pass the ordering assertion with no filter at all.
	for _, d := range got {
		if !strings.HasSuffix(d.Path, ".md") {
			t.Errorf("%q is in the list and is not markdown", d.Path)
		}
	}
}

// TestAMissingDocsTreeIsAStateAndNotAFailure. The editor is routinely
// run from a release directory with no docs/ beside it, and an editor
// that refused to start because its help was missing would be worse than
// one whose help pane says so.
func TestAMissingDocsTreeIsAStateAndNotAFailure(t *testing.T) {
	if got := docsPages(nil); got != nil {
		t.Errorf("a nil docs FS listed %v, want no pages", got)
	}
	if got := docBody(nil, "architecture.md"); got == "" {
		t.Error("a nil docs FS gave an EMPTY body, which is indistinguishable on " +
			"screen from a page with nothing in it — it must say what happened")
	}
	ed, c := docsPage(t, fstest.MapFS{})
	ed.activitySel.Set(4)
	c.Frame()
	if body := ed.docsBody.Get(); !strings.Contains(body, "No docs/") {
		t.Errorf("with an empty tree the pane says %q, want it to say the tree is "+
			"missing", body)
	}
}

// TestSelectingADocsPageRendersIt is the feature: the rail's fifth slot
// shows the list, and the selected page's text is on screen.
func TestSelectingADocsPageRendersIt(t *testing.T) {
	ed, c := docsPage(t, fakeDocs())
	ed.activitySel.Set(4)
	f, _ := c.Frame()

	// The list. Read through the component the markup named rather than
	// through ed.docList, so this fails if the binding is wrong as well
	// as if the walk is.
	if ed.docsItems.Get().Len() != len(ed.docList) {
		t.Fatalf("the bound list has %d rows, the tree has %d pages",
			ed.docsItems.Get().Len(), len(ed.docList))
	}

	// The page. The first page is selected at startup, so its text is
	// what the pane shows without anyone pressing anything.
	if got := ed.docsBody.Get(); !strings.Contains(got, "grounded walkthrough") {
		t.Fatalf("the pane shows %q, want the first page's text", got)
	}
	if !onScreen(f, "grounded walkthrough") {
		t.Error("the first page's text is bound but never reached the cell plane")
	}

	// And selecting another page changes what is shown.
	ed.docsSel.Set(2)
	f2, _ := c.Frame()
	if got := ed.docsBody.Get(); !strings.Contains(got, "headless") {
		t.Fatalf("after selecting page 2 the pane shows %q", got)
	}
	if !onScreen(f2, "headless") {
		t.Error("the newly selected page's text never reached the cell plane")
	}
	if onScreen(f2, "grounded walkthrough") {
		t.Error("the previous page's text is still on screen under the new one")
	}
}

// onScreen reports whether s appears on any row of the plane.
func onScreen(f *gooey.Frame, s string) bool {
	b := f.Cells
	for y := 0; y < b.H; y++ {
		var row strings.Builder
		for x := 0; x < b.W; x++ {
			row.WriteRune(b.At(x, y).Rune)
		}
		if strings.Contains(row.String(), s) {
			return true
		}
	}
	return false
}

// TestSwitchingDocsPagesLeavesTheRailAlone is the damage assertion, and
// it is three claims because one number cannot carry them.
//
// #288 asks that switching pages repaint the pane and not the rail. The
// count alone cannot say that — twelve components repainting is equally
// consistent with the rail being one of them — so what pins "not the
// rail" is the RECTS: no damaged rect except the root's own comes near
// the ActivityBar's bounds.
//
// THE ROOT'S RECT IS EXPECTED AND IS NOT THIS PANE'S DOING. Any side-bar
// selection change repaints the root node, whose rect is the whole
// window; the toolbox arm below measures that on a pane this change did
// not touch, so the root repaint is visible as a property of the page
// rather than as something the docs tab introduced. Fixing it is a
// separate change to the editor's chrome and would be scope creep here.
//
// The no-op arm is the discriminator. Without it, every count below
// would pass for a composition that had simply never settled, where the
// numbers are leftovers from the first frame rather than damage.
func TestSwitchingDocsPagesLeavesTheRailAlone(t *testing.T) {
	const (
		wantDocs    = 12 // the docs list's two rows, the body, and the chain above them
		wantToolbox = 11 // the same shape with no body Text — the baseline
	)
	for _, tc := range []struct {
		name string
		tab  int
		want int
		set  func(ed *editor)
	}{
		{"docs", 4, wantDocs, func(ed *editor) { ed.docsSel.Set(1) }},
		{"toolbox", 1, wantToolbox, func(ed *editor) { ed.paletteSel.Set(3) }},
	} {
		ed, c := docsPage(t, fakeDocs())
		ed.activitySel.Set(tc.tab)
		settleDocs(t, c)

		// A frame that changes nothing must cost nothing, or the counts
		// below are composition leftovers rather than damage.
		if _, n := c.Frame(); n != 0 {
			t.Fatalf("%s: an idle frame repainted %d components, so nothing below "+
				"measures a page switch", tc.name, n)
		}

		rail := ed.ctx.Named["ActivityBar"]
		if rail == nil {
			t.Fatal("the shipped page does not mount the ActivityBar")
		}
		rb := rail.(interface{ Bounds() gooey.Rect }).Bounds()
		if rb.W == 0 || rb.H == 0 {
			t.Fatalf("%s: the rail was never arranged (%v)", tc.name, rb)
		}

		tc.set(ed)
		_, n := c.Frame()
		if n != tc.want {
			t.Errorf("%s: switching repainted %d components, want %d. If this change "+
				"moved the number, the number IS the change — justify it rather than "+
				"updating it", tc.name, n, tc.want)
		}
		for _, r := range c.Damage() {
			if r.W >= 160 && r.H >= 48 {
				continue // the root node, measured by the toolbox arm too
			}
			if r.X < rb.X+rb.W && rb.X < r.X+r.W && r.Y < rb.Y+rb.H && rb.Y < r.Y+r.H {
				t.Errorf("%s: damage %v reaches the rail at %v — a page switch must not "+
					"redraw the activity bar", tc.name, r, rb)
			}
		}
	}
}

// settleDocs frames until nothing repaints, so a later count is damage
// rather than the composition still coming up.
func settleDocs(t *testing.T, c *gooey.Composer) {
	t.Helper()
	for i := 0; i < 10; i++ {
		if _, painted := c.Frame(); painted == 0 {
			return
		}
	}
	t.Fatal("the composition never settled; no damage count taken from it means anything")
}
