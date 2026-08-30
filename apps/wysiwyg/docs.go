package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// The platform-documentation tab: the repo's own docs/ tree, inside the
// editor. Issue #288, a sub-issue of the editor epic #240.
//
// # Why this is an fs.FS and not a directory path
//
// It is the seam the whole framework is built on — os.DirFS plus a
// watcher in dev, embed.FS in release, one code path either way — and
// the pane taking an fs.FS is what lets the tests below hand it a
// fstest.MapFS instead of depending on the repo they run inside.
//
// # What embedding would actually cost, which is not what #288 assumed
//
// #288 framed "does the shipped editor embed docs/?" as a binary-size
// decision. Size is the smaller half. `go:embed` CANNOT REACH OUTSIDE THE
// PACKAGE DIRECTORY — no `..` in a pattern, no symlink following — and
// docs/ sits three levels above apps/wysiwyg. So embedding is not a
// one-line switch here at all; it needs a package at the repo root that
// embeds the tree and exports the FS, which every consumer then imports.
//
// That is a real design decision with a real cost, and it is exactly the
// kind of thing the cheap version exists to defer. Nothing below assumes
// either answer: swap what docsFS returns and the pane does not change.

// DocsDirName is the directory the docs tree lives in, at the repo root.
const DocsDirName = "docs"

// DocsSentinel is a page THIS docs tree is known to contain, and finding
// it is what lets docsFS claim it found the right directory rather than
// a directory with the right name.
//
// The review of PR #426 caught the difference. The walk below is bounded
// in DEPTH, and the comment used to claim that bound stopped it wandering
// into somebody else's docs/ — but depth is not identity. An installed
// binary at ~/bin/wysiwyg probes ~/bin/docs and then ~/docs, which plenty
// of people have; under `go run` the executable is in /tmp/go-buildNNN/
// b001/exe, so the fourth parent probe is /tmp/docs. Either would have
// been adopted and its contents listed as the platform documentation.
const DocsSentinel = "architecture.md"

// docsFS is the documentation tree, or nil when it cannot be found.
//
// NIL IS A LEGAL ANSWER and not a failure to handle. The editor is
// routinely run from a release directory with no docs/ beside it, and an
// editor that refused to start because its help was missing would be
// worse than one whose help pane says so. docsPages turns nil into an
// empty list and the pane says what happened.
//
// It walks UP rather than guessing one path, because the editor is run
// from three places that are all correct: the repo root (go run
// ./apps/wysiwyg), the package directory (go test ./apps/wysiwyg), and
// wherever the built binary sits. The walk is bounded in depth — a fixed
// number of parents, not "until /" — and a candidate is accepted only if
// it holds DocsSentinel, which is the half that makes it the RIGHT tree
// rather than merely a nearby one.
func docsFS() fs.FS {
	// Two starting points, and they are the working directory and the
	// executable's directory rather than editorFS's answer. An earlier
	// comment here said it followed "whatever editorFS resolved", which
	// described code that was never written: editorFS (main.go) falls
	// back to the executable directory only when -page is absent,
	// whereas this always tries both. Close, but not the same, and the
	// review of PR #426 was right that a comment describing a different
	// resolution is worse than none.
	starts := []string{"."}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir := start
		for i := 0; i < 4; i++ {
			cand := filepath.Join(dir, DocsDirName)
			if isDocsTree(cand) {
				return os.DirFS(cand)
			}
			dir = filepath.Join(dir, "..")
		}
	}
	return nil
}

// isDocsTree reports whether cand is a directory that looks like THIS
// repo's docs tree, by the presence of DocsSentinel inside it.
func isDocsTree(cand string) bool {
	if st, err := os.Stat(cand); err != nil || !st.IsDir() {
		return false
	}
	st, err := os.Stat(filepath.Join(cand, DocsSentinel))
	return err == nil && !st.IsDir()
}

// docPage is one page in the tree: the path to read it by, and the label
// the list shows.
type docPage struct {
	Path  string // slash-separated, relative to the docs FS root
	Label string
}

// docsPages lists the markdown under fsys, depth-first in path order.
//
// SORTED BY PATH, which sorts by directory first and is what makes the
// flat list readable without a tree widget: learn/ pages arrive together,
// specs/ together, and the top-level pages come first because a shorter
// path sorts before a longer one sharing its prefix.
//
// A nil FS gives an empty list rather than an error. See docsFS.
//
// IT RETURNS WHAT IT COULD NOT READ, and that second value is a fix from
// the review of PR #426 rather than decoration. Continuing the walk past
// an unreadable subtree is right — one bad directory must not cost the
// user every page in the others — but the previous version also
// discarded the FACT that it had skipped something, along with
// WalkDir's own return. A docs/ whose permissions changed then produced
// a short list, or an empty one, with no signal anywhere: the pane told
// the reader the tree did not exist when it existed and could not be
// read. Those are different problems with different fixes.
func docsPages(fsys fs.FS) (pages []docPage, skipped int) {
	if fsys == nil {
		return nil, 0
	}
	// WalkDir'S OWN RETURN IS DISCARDED, and it is discarded rather than
	// counted because it is always nil. A trailing `if err != nil {
	// skipped++ }` stood here until the review of #426, arguing that a
	// root failure reaches the walker but never the callback. io/fs does
	// the opposite: a failed Stat on the root calls fn(root, nil, err),
	// so the callback DOES see it, counts it, and returns nil; a failed
	// ReadDir calls fn a second time with a non-nil d, which returns
	// SkipDir, which walkDir converts to nil. The callback returns only
	// nil or SkipDir and both are swallowed.
	//
	// So the branch could not fire, and its comment was the worse half:
	// a reader who trusted it would believe root failures arrive by a
	// second road and would not think to test the first.
	// TestAnUnreadableRootIsCountedOnce takes that road.
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			skipped++
			// A directory that cannot be read is skipped rather than
			// abandoning the walk: one unreadable subtree must not cost
			// the user every page in the others.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.EqualFold(path.Ext(p), ".md") {
			return nil
		}
		pages = append(pages, docPage{Path: p, Label: p})
		return nil
	})
	sort.Slice(pages, func(i, j int) bool { return pages[i].Path < pages[j].Path })
	return pages, skipped
}

// docBody reads one page, returning the read error AS THE BODY.
//
// Deliberate: the pane has one string to show, and a reader who selects
// a page that has since been deleted is better served by the error in
// the place the text would be than by an empty pane that looks like a
// page with nothing in it. The two are indistinguishable otherwise,
// which is the failure this avoids rather than a nicety.
func docBody(fsys fs.FS, p string) string {
	if fsys == nil {
		return "No docs/ directory was found beside the editor."
	}
	b, err := fs.ReadFile(fsys, p)
	if err != nil {
		return "cannot read " + p + ": " + err.Error()
	}
	return string(b)
}
