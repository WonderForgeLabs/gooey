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
// wherever the built binary sits. The walk is bounded — a fixed number
// of parents, not "until /" — so a run from an unrelated deep directory
// cannot wander into somebody else's docs/ tree.
func docsFS() fs.FS {
	// The editor's own root first: whatever editorFS resolved is where
	// the page and the icons came from, so a release layout that ships
	// docs/ beside them is found without a walk.
	starts := []string{"."}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		dir := start
		for i := 0; i < 4; i++ {
			cand := filepath.Join(dir, DocsDirName)
			if st, err := os.Stat(cand); err == nil && st.IsDir() {
				return os.DirFS(cand)
			}
			dir = filepath.Join(dir, "..")
		}
	}
	return nil
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
func docsPages(fsys fs.FS) []docPage {
	if fsys == nil {
		return nil
	}
	var out []docPage
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
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
		out = append(out, docPage{Path: p, Label: p})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
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
