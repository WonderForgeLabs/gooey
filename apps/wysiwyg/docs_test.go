package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/render"
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
// The parameter is fs.FS RATHER THAN fstest.MapFS so a test can pass a
// genuinely absent tree. fstest.MapFS(nil) stored in an fs.FS field is a
// non-nil interface holding a nil map — the typed-nil trap — so with the
// narrower type `docsRoot.Get() == nil` is unreachable from here, and the
// "no docs/ at all" state could not be tested at all.
func docsPage(t *testing.T, fsys fs.FS) (*editor, *gooey.Composer) {
	t.Helper()
	ed := newEditor(editorFS())
	ed.docsRoot.Set(fsys)
	// Set rather than replace the property: the computeds read
	// ed.docList.Get(), so writing through it is what a real refresh
	// would do, and it invalidates them the way a refresh would.
	pages, skipped := docsPages(fsys)
	ed.docList.Set(pages)
	ed.docsSkipped.Set(skipped)
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
	got, _ := docsPages(fakeDocs())
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
	if got, skipped := docsPages(nil); got != nil || skipped != 0 {
		t.Errorf("a nil docs FS listed %v, want no pages", got)
	}
	if got := docBody(nil, "architecture.md"); got == "" {
		t.Error("a nil docs FS gave an EMPTY body, which is indistinguishable on " +
			"screen from a page with nothing in it — it must say what happened")
	}
	ed, c := docsPage(t, nil)
	ed.activitySel.Set(4)
	c.Frame()
	if body := ed.docsBody.Get(); !strings.Contains(body, "No docs/") {
		t.Errorf("with no docs tree at all the pane says %q, want it to say the "+
			"tree is missing", body)
	}
}

// TestAnEmptyDocsTreeIsNotAMissingOne. Found by the review of PR #426,
// and the previous test is where it hid: that test passed an empty
// fstest.MapFS — a tree that unambiguously EXISTS — and asserted the
// pane said the tree was missing. So the assertion agreed with the bug.
//
// Three states were collapsed into one message keyed off an empty list:
// no docs/ anywhere, a docs/ holding no markdown, and a docs/ that could
// not be read. Only the first was described correctly, and the other two
// sent a reader looking for a directory that was in front of them.
func TestAnEmptyDocsTreeIsNotAMissingOne(t *testing.T) {
	ed, c := docsPage(t, fstest.MapFS{})
	ed.activitySel.Set(4)
	c.Frame()
	body := ed.docsBody.Get()
	if strings.Contains(body, "No docs/") {
		t.Errorf("an EMPTY but present docs tree reports %q — that is the message "+
			"for a tree that is not there, and it sends the reader looking for a "+
			"directory that exists", body)
	}
	if !strings.Contains(body, "no markdown") {
		t.Errorf("an empty docs tree says %q, want it to say the tree is there and "+
			"holds no pages", body)
	}
}

// errDirFS fails ReadDir for one directory and behaves normally
// otherwise — the unreadable subtree fstest.MapFS cannot express.
type errDirFS struct {
	fs.FS
	bad string
}

func (e errDirFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == e.bad {
		return nil, fs.ErrPermission
	}
	return fs.ReadDir(e.FS, name)
}

// TestAnUnreadableSubtreeIsCountedRatherThanSwallowed. docsPages
// discarded fs.WalkDir's return AND every per-entry error, so a docs/
// whose permissions changed produced a short list with no signal
// anywhere. Continuing the walk is right; discarding the fact that it
// skipped something is what made an unreadable tree indistinguishable
// from an empty one — which is finding 2 arriving by a second road.
func TestAnUnreadableSubtreeIsCountedRatherThanSwallowed(t *testing.T) {
	fsys := errDirFS{FS: fakeDocs(), bad: "learn/howto"}
	pages, skipped := docsPages(fsys)
	if skipped == 0 {
		t.Fatal("a subtree that returns ErrPermission was walked with no error " +
			"recorded — the pages it held are simply gone, and nothing anywhere " +
			"says so")
	}
	// The point of continuing: the other subtrees survive.
	var found bool
	for _, p := range pages {
		if strings.HasPrefix(p.Path, "specs/") {
			found = true
		}
	}
	if !found {
		t.Errorf("one unreadable subtree cost the walk the others too: %v", pages)
	}

	// And the count reaches the reader rather than stopping at the walk.
	ed, c := docsPage(t, errDirFS{FS: fstest.MapFS{}, bad: "."})
	ed.activitySel.Set(4)
	c.Frame()
	body := ed.docsBody.Get()
	if !strings.Contains(body, "unreadable") {
		t.Errorf("an unreadable docs tree reports %q, want it to say so", body)
	}
	// AND IT MUST NOT ALSO CLAIM EMPTINESS, which is the half that
	// shipped past this test. When the root itself cannot be read,
	// docsPages returns (nil, 1) and the pane said "holds no markdown
	// pages. 1 entry could not be read." — a claim the code cannot
	// support, contradicted by the sentence after it. Asserting only
	// that the skip note appeared could not see that. Tightened in
	// review of #426.
	if strings.Contains(body, "holds no markdown pages") {
		t.Errorf("an unreadable docs tree reports %q — it claims the directory "+
			"is EMPTY and then says it could not be read. Those are different "+
			"states with different fixes, and this function exists to tell "+
			"them apart", body)
	}
}

// TestAPageEditedUnderTheEditorIsRead. The pane's own comment justified
// reading inside an evaluation on the grounds that a page deleted under
// the editor renders its own read error where the text would be. A cache
// keyed by path and never invalidated made that true only for a page
// that had never been opened — and the common path is to open one first.
//
// Selecting away and back is what re-evaluates the computed, which is
// exactly the gesture a reader makes after editing a page in the pane
// next door.
func TestAPageEditedUnderTheEditorIsRead(t *testing.T) {
	fsys := fakeDocs()
	ed, c := docsPage(t, fsys)
	ed.activitySel.Set(4)
	c.Frame()

	ed.docsSel.Set(0)
	first := ed.docsBody.Get()
	if !strings.Contains(first, "grounded walkthrough") {
		t.Fatalf("fixture not as expected: %q", first)
	}

	fsys["architecture.md"] = &fstest.MapFile{Data: []byte("# Architecture\nrewritten")}
	ed.docsSel.Set(1)
	ed.docsSel.Set(0)
	if got := ed.docsBody.Get(); !strings.Contains(got, "rewritten") {
		t.Errorf("after the page changed on disk the pane still shows %q — a cache "+
			"that is never invalidated serves the stale body forever, and the read "+
			"error the comment promises for a DELETED page never appears either", got)
	}

	delete(fsys, "architecture.md")
	ed.docsSel.Set(1)
	ed.docsSel.Set(0)
	if got := ed.docsBody.Get(); !strings.Contains(got, "cannot read") {
		t.Errorf("after the page was deleted the pane shows %q, want the read error "+
			"in the place the text would be — that claim is the whole argument for "+
			"reading inside the evaluation", got)
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
	if ed.docsItems.Get().Len() != len(ed.docList.Get()) {
		t.Fatalf("the bound list has %d rows, the tree has %d pages",
			ed.docsItems.Get().Len(), len(ed.docList.Get()))
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
//
// Through render.RowText, which exists for exactly this and which the
// review of PR #426 caught this helper reimplementing. Six packages once
// had their own cell-by-cell `row(b, y)`, every one of them rendered
// render.Continuation as a literal rune, and the consequence was that no
// fixture in the repo could hold a wide glyph and be asserted on.
// RowText was written to end that; adding a seventh copy here would have
// reopened it in the one package whose fixtures are user-facing prose.
func onScreen(f *gooey.Frame, s string) bool {
	b := f.Cells
	for y := 0; y < b.H; y++ {
		if strings.Contains(render.RowText(b, y), s) {
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

// TestADirectoryNamedDocsIsNotEnough. The walk is bounded in depth, and
// the comment used to offer that bound as protection against adopting
// somebody else's tree. Depth is not identity: an installed binary at
// ~/bin/wysiwyg probes ~/bin/docs and then ~/docs, and under `go run`
// the executable lives in /tmp/go-buildNNN/b001/exe, whose fourth parent
// probe is /tmp/docs. Both are directories real machines have.
//
// The decoy below is the case the depth bound cannot see, because it is
// at depth zero — the nearest candidate, not a distant one.
func TestADirectoryNamedDocsIsNotEnough(t *testing.T) {
	root := t.TempDir()
	decoy := filepath.Join(root, DocsDirName)
	if err := os.MkdirAll(decoy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoy, "shopping-list.md"), []byte("eggs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isDocsTree(decoy) {
		t.Error("a directory named docs/ holding somebody else's markdown was " +
			"accepted as the platform documentation — the editor would list their " +
			"files as its own help")
	}

	if err := os.WriteFile(filepath.Join(decoy, DocsSentinel), []byte("# Architecture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isDocsTree(decoy) {
		t.Error("a docs/ holding the sentinel was REJECTED — a guard that only " +
			"ever says no is a guard nothing has tested, and this is the case " +
			"where it must say yes")
	}
}

// TestTheDocsListIsARealDependency. docsItems was a computed closing
// over a plain slice field, so it read nothing observable: it cached on
// first evaluation and no write could ever invalidate it. That was
// invisible while the list was written exactly once, and the only reason
// the test helper's override worked was that nothing had evaluated the
// computed yet — an accident of ordering, not a property.
//
// So the evaluation has to happen FIRST here. Getting before the Set is
// what makes this test able to fail.
func TestTheDocsListIsARealDependency(t *testing.T) {
	ed, c := docsPage(t, fakeDocs())
	ed.activitySel.Set(4)
	c.Frame()

	before := ed.docsItems.Get().Len()
	if before == 0 {
		t.Fatal("fixture produced no pages")
	}

	ed.docList.Set([]docPage{{Path: "only.md", Label: "only.md"}})
	if got := ed.docsItems.Get().Len(); got != 1 {
		t.Errorf("after the list was replaced the item source still has %d rows "+
			"(was %d) — the computed reads a plain field rather than a property, "+
			"so nothing can invalidate it and any future refresh is silently "+
			"invisible", got, before)
	}
}

// TestADocsPageMayHoldAWideGlyph is what makes the RowText change above
// load-bearing rather than tidy. The per-rune readback it replaced wrote
// render.Continuation — the second cell of every wide glyph — through
// WriteRune, which turns rune -1 into U+FFFD, so a row holding "世界"
// read back as "世\ufffd界\ufffd" and no assertion about it could match.
//
// That mattered nowhere until docs pages became test fixtures, because
// the fixtures are PROSE: the repo's own documentation is exactly the
// place a CJK example, a box-drawing table or an emoji shows up. A
// helper that cannot express what the writer produced would have made
// every one of them unassertable, silently.
func TestADocsPageMayHoldAWideGlyph(t *testing.T) {
	fsys := fakeDocs()
	fsys["wide.md"] = &fstest.MapFile{Data: []byte("# Wide\n世界 is two cells each")}
	ed, c := docsPage(t, fsys)
	ed.activitySel.Set(4)
	c.Frame()

	var i int
	for n, d := range ed.docList.Get() {
		if d.Path == "wide.md" {
			i = n
		}
	}
	ed.docsSel.Set(i)
	f, _ := c.Frame()
	if !onScreen(f, "世界") {
		t.Errorf("a docs page holding a wide glyph is not findable on the plane — "+
			"a readback that renders Continuation as a literal rune reads it as "+
			"%q and no fixture in this package could contain one", "世\ufffd界\ufffd")
	}
}

// TestNoControlCharacterFromADocsPageReachesTheCellPlane is the pin for
// the one thing this feature does that nothing in the tree did before:
// it puts ARBITRARY FILE BYTES into cells.
//
// A TAB is not hypothetical here. Every Go block in docs/learn/*.md is
// tab-indented, so the shipped tree is full of them. render.Buffer
// stores a TAB as an ordinary rune, StringWidth counts it as one
// column, and flush.go emits Cell.Text() verbatim — so the terminal got
// a real TAB, advanced its cursor to the next 8-column stop, and drew
// the rest of the line OUTSIDE the pane. The clip bounds the cell
// plane, not the hardware cursor.
//
// Worse than a smear: the buffer believes those far columns were never
// written, so the diff-based flush treats them as clean and nothing
// repaints them until something else dirties the row.
//
// Asserted on the PLANE rather than on expandControls' return value,
// because the claim is about what reaches a cell — a unit test on the
// helper would pass with the helper wired to nothing. Found in review
// of #426.
func TestNoControlCharacterFromADocsPageReachesTheCellPlane(t *testing.T) {
	fsys := fakeDocs()
	// A tab-indented Go block, exactly as docs/learn/01-first-app.md
	// writes one, plus a control that is not a tab and a CRLF line.
	fsys["tabs.md"] = &fstest.MapFile{
		Data: []byte("# Tabs\n\tcontext\n\tfmt\nbell\x07here\r\n"),
	}
	ed, c := docsPage(t, fsys)
	ed.activitySel.Set(4)
	c.Frame()

	var i int
	for n, d := range ed.docList.Get() {
		if d.Path == "tabs.md" {
			i = n
		}
	}
	ed.docsSel.Set(i)
	f, _ := c.Frame()

	b := f.Cells
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			r := b.At(x, y).Rune
			// Continuation is the second column of a wide glyph, not a
			// control — it is rune -1 and never reaches the terminal.
			if r == render.Continuation {
				continue
			}
			if (r < 0x20 && r != 0) || r == 0x7f {
				t.Fatalf("cell (%d,%d) holds control character %q. A docs page put "+
					"it on the plane, and flush.go emits Cell.Text() verbatim — so "+
					"the terminal receives it, moves its cursor outside the pane's "+
					"clip, and the damage model believes those columns are clean",
					x, y, r)
			}
		}
	}

	// AND THE TEXT SURVIVED THE REWRITE, or the loop above passes against
	// an expandControls that simply dropped everything it could not name.
	if !onScreen(f, "context") {
		t.Error("the tab-indented word is not on the plane; expanding a TAB " +
			"must keep the text it was indenting")
	}
}
