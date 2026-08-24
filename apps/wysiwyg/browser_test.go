package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
)

// TestFuzzyMatchesASubsequence — the basic contract, both arms.
func TestFuzzyMatchesASubsequence(t *testing.T) {
	for _, tc := range []struct {
		q, s string
		want bool
	}{
		{"", "anything", true},
		{"abc", "a_b_c", true},
		{"abc", "xxaxxbxxcxx", true},
		{"abc", "acb", false},
		{"abc", "ab", false},
		{"WYS", "apps/wysiwyg/main.go", true}, // case-insensitive
	} {
		got, _, _ := fuzzyMatch(tc.q, tc.s)
		if got != tc.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tc.q, tc.s, got, tc.want)
		}
	}
}

// TestFuzzyPrefersContiguousAndSegmentStarts is what makes the ranking
// worth having. Without the ordering assertions a matcher that returned a
// constant score would pass every other test in this file.
func TestFuzzyPrefersContiguousAndSegmentStarts(t *testing.T) {
	contiguous := scoreOf(t, "main", "main.go")
	scattered := scoreOf(t, "main", "m_a_i_n_x.go")
	if contiguous <= scattered {
		t.Errorf("a contiguous match scored %d and a scattered one %d; a fuzzy list that "+
			"does not rank the obvious hit first is a list nobody can use", contiguous, scattered)
	}

	atStart := scoreOf(t, "go", "a/go.mod")
	midWord := scoreOf(t, "go", "a/lego.mod")
	if atStart <= midWord {
		t.Errorf("a segment-start match scored %d and a mid-word one %d; the corpus is "+
			"PATHS and what a user aims at is almost always a segment", atStart, midWord)
	}

	// The boundary bonus belongs to the RUN, not to each character of it,
	// and this is the case that can tell the difference — which took a
	// mutation run to find, because for ordinary queries the gap penalty
	// hides it.
	//
	// A query containing a separator matches a run whose SECOND character
	// follows a segment break: in "a/main.go" the query "/m" matches '/'
	// then 'm', contiguously, with 'm' sitting right after a '/'. Paying
	// the boundary bonus per character scores that run higher than the
	// identical run without a separator in it, so the same match shape
	// would rank differently depending on a character of the QUERY. The
	// two must score alike.
	withSeparator := scoreOf(t, "/m", "a/main.go")
	withoutSeparator := scoreOf(t, "Xm", "aXmain.go")
	if withSeparator != withoutSeparator {
		t.Errorf("the same match shape scored %d with a separator inside the run and %d "+
			"without; the segment bonus is being paid per character rather than once per "+
			"run, so a query containing a slash is scored on a different scale",
			withSeparator, withoutSeparator)
	}

	short := scoreOf(t, "dock", "dock.go")
	long := scoreOf(t, "dock", "a/very/long/path/that/keeps/going/dock.go")
	if short <= long {
		t.Errorf("the short path scored %d and the long one %d; the length penalty is not "+
			"breaking ties", short, long)
	}
}

func scoreOf(t *testing.T, q, s string) int {
	t.Helper()
	ok, score, _ := fuzzyMatch(q, s)
	if !ok {
		t.Fatalf("fuzzyMatch(%q, %q) did not match; the comparison below is meaningless", q, s)
	}
	return score
}

// TestFuzzyIndexesRunesNotBytes is a REGRESSION GUARD against the copy
// this matcher was written from.
//
// cmd/finder's `fuzzy` lowercases the string and then indexes the byte
// slice, while its caller walks the same string with `for i, r := range`
// — which also yields byte offsets, so the two agree for ASCII and
// silently disagree for anything else. The positions are what a
// highlighter uses, so the bug is invisible until somebody has a
// non-ASCII path.
//
// Written as a rune-position assertion rather than a "does it match"
// assertion, because matching is not where the two implementations
// differ.
func TestFuzzyIndexesRunesNotBytes(t *testing.T) {
	// Each of these is multi-byte, so a byte-indexing implementation
	// reports positions past the end of the rune slice.
	const s = "ααα/main.go"
	ok, _, hits := fuzzyMatch("main", s)
	if !ok {
		t.Fatal("no match; the position assertion below would be vacuous")
	}
	runes := []rune(s)
	for _, i := range hits {
		if i < 0 || i >= len(runes) {
			t.Fatalf("matched position %d is outside the %d-rune string; the matcher is "+
				"indexing BYTES", i, len(runes))
		}
	}
	var got []rune
	for _, i := range hits {
		got = append(got, runes[i])
	}
	if string(got) != "main" {
		t.Errorf("the matched positions spell %q, want %q — they do not point at the "+
			"characters that matched", string(got), "main")
	}
}

// workspaceFixture writes a small tree, including the two things scan is
// supposed to prune and one that is nested deep enough to catch a
// top-anchored filter.
func workspaceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{
		"main.gooey",
		"a/nested.gooey",
		"a/b/deep.gooey",
		".git/config",
		"a/.hidden/secret.gooey",
		"vendor/dep/thing.go",
		"a/b/node_modules/pkg/index.js",
	} {
		full := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("<Gooey><Canvas Name=\"R\"/></Gooey>\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestScanPrunesDotDirsAtEveryDepth. The nested `.hidden` is the one that
// matters: a filter anchored at the top passes in a tidy fixture and
// walks into somebody's worktree on a real machine. CLAUDE.md documents
// exactly this trap for the repo's own module discovery.
func TestScanPrunesDotDirsAtEveryDepth(t *testing.T) {
	ws := openWorkspace(workspaceFixture(t))
	if ws.err != "" {
		t.Fatalf("opening the fixture failed: %s", ws.err)
	}
	if len(ws.files) == 0 {
		t.Fatal("the scan found nothing; every assertion below would be vacuous")
	}
	for _, f := range ws.files {
		for _, bad := range []string{".git/", ".hidden/", "vendor/", "node_modules/"} {
			if strings.Contains(f, bad) {
				t.Errorf("the scan walked into %s (found %q)", bad, f)
			}
		}
	}
	// The discrimination half: it must still find the real files,
	// including the deep one — a prune that removed everything would
	// satisfy the loop above.
	want := map[string]bool{"main.gooey": false, "a/nested.gooey": false, "a/b/deep.gooey": false}
	for _, f := range ws.files {
		if _, ok := want[f]; ok {
			want[f] = true
		}
	}
	for f, found := range want {
		if !found {
			t.Errorf("the scan did not find %q; the prune is eating real files", f)
		}
	}
}

// TestTheQueryFiltersAndReranks — the difference from <TypeAhead>, which
// moves a selection and never hides a row.
func TestTheQueryFiltersAndReranks(t *testing.T) {
	ws := openWorkspace(workspaceFixture(t))
	all := ws.ranked("")
	if len(all) < 3 {
		t.Fatalf("the fixture yields %d files; too few to tell filtering from doing nothing", len(all))
	}
	deep := ws.ranked("deep")
	if len(deep) >= len(all) {
		t.Errorf("querying %q returned %d of %d files; the list is not FILTERED", "deep", len(deep), len(all))
	}
	if len(deep) == 0 || !strings.Contains(deep[0], "deep") {
		t.Errorf("querying %q ranked %v first; the obvious hit must lead", "deep", deep)
	}
	// A query that matches nothing returns nothing rather than everything.
	if got := ws.ranked("zzzzzz"); len(got) != 0 {
		t.Errorf("a query matching nothing returned %d rows", len(got))
	}
}

// TestOpeningAFileLoadsTheDocumentModel — through fs.FS, and into the
// node tree rather than into a built component tree.
func TestOpeningAFileLoadsTheDocumentModel(t *testing.T) {
	root := workspaceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "main.gooey"),
		[]byte("<Gooey>\n  <Canvas Name=\"Root\">\n    <Text Name=\"Hello\">hi</Text>\n  </Canvas>\n</Gooey>\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setWorkspace(root)
	ed.openWorkspaceFile("main.gooey")

	if got := ed.openPath.Get(); got != "main.gooey" {
		t.Fatalf("openPath is %q after opening main.gooey", got)
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Fatalf("opening the file left the status at %q", ed.status.Get())
	}
	doc := ed.doc()
	if doc.Elem != "Canvas" || doc.Attrs["Name"] != "Root" {
		t.Fatalf("the document root is <%s Name=%q>; the <Gooey> envelope was not unwrapped",
			doc.Elem, doc.Attrs["Name"])
	}
	if len(doc.Kids) != 1 || doc.Kids[0].Attrs["Name"] != "Hello" {
		t.Errorf("the loaded document does not hold the file's own element")
	}
}

// TestSaveSerialisesTheDocumentModelAndNotTheLiveTree is the hazard, not
// a style point.
//
// The tree on screen is what markup.Build made of the document PLUS
// anything patch_markup put there — this editor serves a control plane
// and is itself patchable. A save that walked the live tree would write
// those patches into the user's file as if they had authored them.
//
// The test forces the two apart: it puts an element into the DOCUMENT and
// checks the file has it, and puts a component into the LIVE TREE that
// the document does not know about and checks the file does NOT.
func TestSaveSerialisesTheDocumentModelAndNotTheLiveTree(t *testing.T) {
	root := workspaceFixture(t)
	ed, _ := buildPage(t)
	ed.setWorkspace(root)
	ed.openWorkspaceFile("main.gooey")

	// In the document: must be saved.
	ed.doc().Kids = append(ed.doc().Kids, &node{
		Elem: "Text", Body: "authored", Attrs: map[string]string{"Name": "Authored"},
	})
	ed.rebuild()

	// In the live tree only: must NOT be saved. This is what a patch
	// looks like from the save path's point of view — a component that
	// exists on screen with no node behind it.
	if ed.docRoot == nil {
		t.Fatal("the document did not build, so there is no live tree to diverge from")
	}
	planted := false
	walkTree(ed.docRoot, func(c gooey.Component) {
		if cv, ok := c.(*components.Canvas); ok && !planted {
			cv.Children = append(cv.Children, &components.Text{Content: components.Str("PatchedIn")})
			planted = true
		}
	})
	if !planted {
		t.Fatal("could not plant a live-tree-only component; this test cannot discriminate")
	}

	if err := ed.saveOpenFile(); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "main.gooey"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "Authored") {
		t.Errorf("the saved file does not contain the element added to the DOCUMENT:\n%s", got)
	}
	// The editor's own workspace Canvas is NOT the user's document, and it
	// is the nearest thing on screen to a component the user never wrote.
	// A save that started one level too high would write it, and the user
	// would open their file to find scaffolding they cannot delete.
	if strings.Contains(got, `Name="Surface"`) {
		t.Errorf("the saved file contains the editor's own surface Canvas; save started "+
			"from the workspace root rather than from the user's document:\n%s", got)
	}
	if strings.Contains(got, "PatchedIn") {
		t.Errorf("the saved file contains a component that exists only in the LIVE TREE; "+
			"save is walking the tree instead of the document, so a patch_markup would be "+
			"written into the user's file as if they had authored it:\n%s", got)
	}
}

// TestSaveWithoutAWritableWorkspaceWritesNothing — the fs.FS seam is
// READ-only, so a workspace with no real directory behind it cannot be
// saved to, and that has to be a refusal rather than a panic.
func TestSaveWithoutAWritableWorkspaceWritesNothing(t *testing.T) {
	ed, _ := buildPage(t)
	if err := ed.saveOpenFile(); err != nil {
		t.Errorf("saving with no workspace returned %v; it should simply do nothing", err)
	}
	if ed.canSave() {
		t.Error("canSave is true with no workspace open")
	}
}

// TestOpeningABadFileLeavesTheDocumentAlone — a file that does not parse
// must not take the editor's current document with it.
func TestOpeningABadFileLeavesTheDocumentAlone(t *testing.T) {
	root := workspaceFixture(t)
	if err := os.WriteFile(filepath.Join(root, "broken.gooey"), []byte("<Gooey><unclosed>"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed, _ := buildPage(t)
	ed.setWorkspace(root)
	before := ed.doc()
	ed.openWorkspaceFile("broken.gooey")
	if ed.doc() != before {
		t.Error("a file that does not parse replaced the open document")
	}
	if !strings.HasPrefix(ed.status.Get(), "✗") {
		t.Errorf("opening a broken file left the status at %q; it must say so", ed.status.Get())
	}
}

// TestTheBrowserListIsBoundAndInvalidates — the list source is a computed
// over the scan revision and the query, and BOTH reads have to happen on
// every evaluation or the pane goes deaf to one of them.
func TestTheBrowserListIsBoundAndInvalidates(t *testing.T) {
	ed, _ := buildPage(t)
	if n := ed.wsFiles.Get().Len(); n != 0 {
		t.Fatalf("with no workspace the browser lists %d files", n)
	}
	ed.setWorkspace(workspaceFixture(t))
	full := ed.wsFiles.Get().Len()
	if full == 0 {
		t.Fatal("opening a workspace did not repopulate the list; the computed is not " +
			"reading the scan revision")
	}
	ed.wsQuery.Set("deep")
	filtered := ed.wsFiles.Get().Len()
	if filtered == 0 || filtered >= full {
		t.Errorf("the query moved the list from %d to %d rows; the computed is not reading "+
			"the query", full, filtered)
	}
	ed.wsQuery.Set("")
	if got := ed.wsFiles.Get().Len(); got != full {
		t.Errorf("clearing the query left %d rows, want %d", got, full)
	}
}
