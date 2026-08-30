package main

import (
	"errors"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"
)

// TestTheEditorFindsThisRepoSDocs is the one test that runs docsFS
// against the tree it was written for.
//
// Every other docs test substitutes an fstest.MapFS, which is the point
// of the fs.FS seam — they must not depend on the repo they run inside.
// The cost is that the resolution itself was pinned by nothing: rename
// DocsSentinel, move docs/, or push apps/wysiwyg one directory deeper
// than the four-parent walk, and the tab degrades to "No docs/ directory
// was found beside the editor" with a green suite behind it.
//
// The assertion is cheap because these tests always run inside a
// checkout, and it names both causes so the failure is actionable rather
// than a bare nil. Found in review of #426.
func TestTheEditorFindsThisRepoSDocs(t *testing.T) {
	fsys := docsFS()
	if fsys == nil {
		wd, _ := os.Getwd()
		t.Fatalf("docsFS() found nothing from %s — either the four-parent walk no "+
			"longer reaches the repo root from apps/wysiwyg, or %s/%s was renamed "+
			"or moved out from under the sentinel check",
			wd, DocsDirName, DocsSentinel)
	}
	// AND that it found the right tree, not merely a directory. isDocsTree
	// already tests the sentinel, so this is the same claim read back
	// through the FS the pane will actually list from.
	if _, err := fs.Stat(fsys, DocsSentinel); err != nil {
		t.Fatalf("docsFS() resolved a tree that does not hold %s: %v", DocsSentinel, err)
	}
	pages, _ := docsPages(fsys)
	if len(pages) == 0 {
		t.Fatal("the repo's own docs tree lists no markdown pages — the docs tab " +
			"would open empty")
	}
}

// unreadableRoot is an fs.FS whose root cannot be walked at all: the one
// failure mode fstest.MapFS cannot express.
type unreadableRoot struct{}

func (unreadableRoot) Open(string) (fs.File, error) { return nil, errors.New("permission denied") }

// TestAnUnreadableRootIsCountedOnce takes the road docsPages' deleted
// error branch claimed did not exist.
//
// That branch counted fs.WalkDir's own return on the reasoning that a
// root failure reaches the walker but never the callback. It reaches
// both — io/fs calls fn(root, nil, err) — so the callback counted it and
// the branch would have counted it a second time, reporting two skipped
// entries where there is one. The test that would have caught it is this
// one, and it did not exist. Found in review of #426.
func TestAnUnreadableRootIsCountedOnce(t *testing.T) {
	pages, skipped := docsPages(unreadableRoot{})
	if len(pages) != 0 {
		t.Errorf("an unreadable root yielded %d pages", len(pages))
	}
	if skipped != 1 {
		t.Errorf("an unreadable root reports %d skipped entries, want exactly 1 — "+
			"one failure counted twice is as wrong as one counted never", skipped)
	}
}

// TestAStaleSelectionStillShowsAPage is the blank-pane case.
//
// ItemsView.selection CLAMPS its read and writes back only on a gesture,
// so a docList that refreshes shorter leaves the list highlighting a row
// the pane must agree about. It did not: an out-of-range index took the
// empty-message road, and with a non-empty list that road returned "" —
// a blank pane, which docBody's own comment says is the state worth
// avoiding because it is indistinguishable from a page with nothing in
// it.
//
// The trigger is a shorter list, which is exactly what docList was
// promoted to a source property to make expressible. Found in review of
// #426.
func TestAStaleSelectionStillShowsAPage(t *testing.T) {
	ed, _ := docsPage(t, fakeDocs())

	ed.docsSel.Set(3)
	if got := ed.docsBody.Get(); got == "" {
		t.Fatal("the fixture does not select a real page; the test proves nothing")
	}

	// The refresh: one page left, and a selection pointing past it.
	one := fstest.MapFS{"architecture.md": {Data: []byte("# Architecture\nonly page")}}
	pages, skipped := docsPages(one)
	ed.docsRoot.Set(one)
	ed.docsSkipped.Set(skipped)
	ed.docList.Set(pages)

	got := ed.docsBody.Get()
	if got == "" {
		t.Fatalf("docsSel=%d over a list of %d renders a BLANK pane while the "+
			"list highlights a row — the two read the same index and disagree "+
			"about what it means", ed.docsSel.Get(), len(pages))
	}
	if want := "# Architecture\nonly page"; got != want {
		t.Errorf("the pane shows %q, want the nearest real page %q", got, want)
	}
}

// TestARefreshOfEitherUnobservableFieldReachesThePane is the property
// half: docsRoot and docsSkipped were plain fields read inside docsBody,
// so writing them invalidated nothing.
//
// It was masked because every refresh also wrote docList, whose Set did
// invalidate — a coupling nobody had written down. Each arm here writes
// ONE of them and leaves docList alone, which is the write the old code
// could not see.
func TestARefreshOfEitherUnobservableFieldReachesThePane(t *testing.T) {
	t.Run("docsSkipped", func(t *testing.T) {
		ed, _ := docsPage(t, fstest.MapFS{})
		before := ed.docsBody.Get()
		ed.docsSkipped.Set(2)
		if after := ed.docsBody.Get(); after == before {
			t.Errorf("the pane still says %q after the skipped count changed — "+
				"the read is not observable", after)
		}
	})
	t.Run("docsRoot", func(t *testing.T) {
		ed, _ := docsPage(t, fstest.MapFS{})
		before := ed.docsBody.Get()
		ed.docsRoot.Set(nil)
		if after := ed.docsBody.Get(); after == before {
			t.Errorf("the pane still says %q after the docs tree went away — "+
				"the read is not observable", after)
		}
	})
}
