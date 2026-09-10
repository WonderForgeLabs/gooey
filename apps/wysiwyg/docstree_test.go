package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey/render"
)

// docsTreeFields are the three source properties that describe one
// thing. Named once, here, because both tests below range over them and
// a fourth field joining the group has to join both at once.
var docsTreeFields = []string{"docsRoot", "docList", "docsSkipped"}

// TestSettingTheDocsTreeCarriesTheListWithIt is the pin issue #442 asks
// for: set only the root and assert the list follows.
//
// The defect it stands against is not hypothetical — it is what the
// three fields ALLOWED, and what the comment shipped with #426 described
// without closing. docsBody resolves docList's paths against
// docsRoot.Get(), so a root written on its own leaves the pane listing
// pages from the old tree and rendering every one of them as
// "cannot read …": a full-looking list where nothing opens.
//
// THE SECOND HALF IS WHAT MAKES IT A MEASUREMENT. Asserting the new
// page renders would pass on a helper that ignored its argument and
// re-read the real docs/ tree, because the fixture's page name is a real
// one. The assertion that discriminates is the SKIPPED COUNT and the
// list length: the fixture is built so both differ from the tree the
// editor started with.
func TestSettingTheDocsTreeCarriesTheListWithIt(t *testing.T) {
	ed, _ := docsPage(t, fakeDocs())
	before := len(ed.docList.Get())
	if before < 2 {
		t.Fatalf("the starting fixture holds %d pages, so a refresh to one "+
			"page would not be a change this test could see", before)
	}

	// One page and one unreadable directory, so all three fields must
	// move. The skipped count needs a WALK ERROR, not merely a non-.md
	// file — docsPages counts what it could not read, and a MapFS full
	// of PNGs reports zero.
	one := oneBadDir{fstest.MapFS{
		"architecture.md": {Data: []byte("# Architecture\nonly page")},
		"private/x.md":    {Data: []byte("# hidden")},
	}}
	ed.setDocsTree(one)

	if got := len(ed.docList.Get()); got != 1 {
		t.Errorf("the list holds %d pages after a refresh to a one-page "+
			"tree, so it is still describing the old root: %v",
			got, ed.docList.Get())
	}
	if got := ed.docsSkipped.Get(); got != 1 {
		t.Errorf("the skipped count is %d, want the new tree's 1", got)
	}
	if got := ed.docsBody.Get(); !strings.Contains(got, "only page") {
		t.Errorf("the pane shows %q — docsBody resolves docList's paths "+
			"against docsRoot, so a list left over from the old tree "+
			"renders as a page that cannot be read", got)
	}
}

// TestTheDocsTreeHasOneWriter derives #442's invariant from the source
// instead of trusting the paragraph that states it.
//
// One writer is what makes "these three always agree" checkable by
// inspection. A convention cannot be: the coupling held for the whole
// life of the fields and nobody had written it down, which is the state
// the review of #426 found.
//
// THE TEST FILES ARE NOT EXEMPT BY OVERSIGHT — they are exempt because
// proving each field's read is OBSERVABLE means writing exactly the
// inconsistent state setDocsTree exists to prevent
// (TestARefreshOfEitherUnobservableFieldReachesThePane does), so a guard
// that covered them would forbid the test that proves the wiring. It
// covers production code, which is where the invariant has to hold.
//
// IT COUNTS docsPages CALLS AS WRITES, and that is not tidiness — a
// mutation went SILENT without it. Restoring the construction this
// change replaced (three prop.NewSource calls filled from a second
// docsPages) reintroduces exactly the state #442 is about and calls Set
// nowhere at all, so a guard watching only Set sees a single writer and
// agrees. What actually makes the three able to disagree is a SECOND
// PLACE THAT DERIVES THEM: every docsPages call site is a place the
// list and the skipped count can be computed against a root that some
// other line chose.
func TestTheDocsTreeHasOneWriter(t *testing.T) {
	writes := docsTreeWrites(t, ".")
	for fn, fields := range writes {
		if fn == "setDocsTree" {
			continue
		}
		t.Errorf("%s writes the docs tree (%s) — setDocsTree is meant to be "+
			"the only writer, because a second place that sets or derives "+
			"the three leaves docsBody resolving two of them against a root "+
			"the third did not choose",
			fn, strings.Join(fields, ", "))
	}
	// THE MUST-FIRE HALF. A scanner that matched nothing — a renamed
	// field, a parser that silently returned no files, a walk rooted at
	// the wrong directory — would satisfy the loop above by finding
	// nothing at all, and a negative assertion passes for any reason.
	want := append(append([]string{}, docsTreeFields...), docsTreeDeriver+"()")
	sort.Strings(want)
	got := append([]string{}, writes["setDocsTree"]...)
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Errorf("the scan found setDocsTree touching %v; it must touch all "+
			"of %v, and finding fewer means the scan is not reading what "+
			"this test thinks it is", got, want)
	}
}

// docsTreeDeriver is the function that turns a tree into the other two
// values. A call to it is as much a write as a Set is: see
// TestTheDocsTreeHasOneWriter.
const docsTreeDeriver = "docsPages"

// docsTreeWrites maps enclosing function name to what it does to the
// docs tree — a `<anything>.<field>.Set(` for each of docsTreeFields,
// and `docsPages()` for a call to the deriver — across the non-test .go
// files in dir.
//
// SYNTAX, not types, which is enough here: the names are unique in this
// package. It is an AST walk rather than a grep because a grep would
// also match them inside comments and strings — this file's own prose
// among them.
func docsTreeWrites(t *testing.T, dir string) map[string][]string {
	t.Helper()
	want := map[string]bool{}
	for _, f := range docsTreeFields {
		want[f] = true
	}
	out := map[string][]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	scanned := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		scanned++
		var fn string
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.FuncDecl:
				fn = v.Name.Name
			case *ast.CallExpr:
				// A bare call to the deriver.
				if id, ok := v.Fun.(*ast.Ident); ok && id.Name == docsTreeDeriver {
					out[fn] = append(out[fn], docsTreeDeriver+"()")
					return true
				}
				// x.<field>.Set(...) — the Set selector, whose own
				// receiver is a selector naming the field.
				sel, ok := v.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Set" {
					return true
				}
				inner, ok := sel.X.(*ast.SelectorExpr)
				if !ok || !want[inner.Sel.Name] {
					return true
				}
				out[fn] = append(out[fn], inner.Sel.Name)
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatalf("no non-test .go file was parsed under %q, so this scan "+
			"would report no writer anywhere and pass", dir)
	}
	return out
}

// oneBadDir is a MapFS with one subdirectory that cannot be read, which
// is the only shape that produces a page AND a skipped entry at once.
// ReadDir is the method that has to fail, not Open: MapFS implements
// fs.ReadDirFS, so fs.WalkDir takes that road and an Open override is
// never consulted.
type oneBadDir struct{ fstest.MapFS }

func (f oneBadDir) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "private" {
		return nil, errors.New("permission denied")
	}
	return f.MapFS.ReadDir(name)
}

// docsLabelCols MEASURES how many columns a docs-list row gives its
// label, by putting a page on screen whose label is a run of one
// character and counting how much of it survives.
//
// Measured rather than computed, and that is the point. The arithmetic
// looks easy — the DockPane declares Size="40", the item template
// spends one column on the selection bar and one on a Margin — and it is
// the arithmetic that goes stale: the dock's own chrome, a scrollbar, a
// changed template each move the answer, and none of them would move a
// number written here. Every one of them moves what lands on the cell
// plane.
func docsLabelCols(t *testing.T) int {
	t.Helper()
	const marker = "M"
	long := strings.Repeat(marker, 200)
	ed, c := docsPage(t, fstest.MapFS{long + ".md": {Data: []byte("# x")}})
	ed.activitySel.Set(4)
	f, _ := c.Frame()

	best := 0
	for y := 0; y < f.Cells.H; y++ {
		row := render.RowText(f.Cells, y)
		run := 0
		for _, r := range row {
			if string(r) == marker {
				run++
				if run > best {
					best = run
				}
				continue
			}
			run = 0
		}
	}
	if best == 0 {
		t.Fatal("no row on the composed frame carries the fixture's label at " +
			"all, so this probe measured nothing")
	}
	if best >= 200 {
		t.Fatalf("the label was not clipped at %d columns, so the frame is "+
			"not the docs pane this test means to measure", best)
	}
	return best
}

// TestEveryDocsPathStaysDistinguishableInThePane is the pin issue #442
// asks for on its third finding.
//
// The docs list uses the full slash-separated path as its row text, in a
// pane whose width is a CONSTANT — so a path longer than the row is cut,
// invisibly, the same failure #426 fixed for the tab strip one control
// up. It is cosmetic only while no two paths collide once cut; the
// moment two do, the list shows the same row twice and neither says
// which page it opens.
//
// This is checked and passing today, which is exactly why it needs a
// pin: `specs/2026-08-10-*` is a seventeen-character shared prefix, and
// the tree it ranges over is the repo's real docs/, so a new page lands
// here on the commit that adds it rather than in front of somebody.
//
// IT RANGES OVER THE REAL TREE, NOT A FIXTURE, and that is deliberate —
// a fixture would assert the property of a corpus nobody ships. The
// skip is the price: docsFS returns nil when the editor is run from
// somewhere without docs/ beside it, which is a legal state, so this
// test has to say it did nothing rather than pass quietly.
func TestEveryDocsPathStaysDistinguishableInThePane(t *testing.T) {
	root := docsFS()
	if root == nil {
		t.Skip("no docs/ tree beside this package, so there is no corpus to " +
			"check — see docsFS")
	}
	pages, _ := docsPages(root)
	if len(pages) < 2 {
		t.Skipf("the docs tree holds %d pages; two paths cannot collide",
			len(pages))
	}
	cols := docsLabelCols(t)

	for _, c := range docsLabelCollisions(pages, cols) {
		t.Errorf("%q and %q both show as %q in a %d-column row — the list "+
			"offers the same text twice and neither row says which page it "+
			"opens. Split the label into directory and filename columns, or "+
			"shorten one of the two paths", c[0], c[1], c[2], cols)
	}
}

// docsLabelCollisions is the check itself, separated so it can be aimed
// at a width the corpus is KNOWN to collide at. ClipCols, not a rune
// slice: a label is a COLUMN count, and a path holding a wide glyph
// would be cut a column short by rune arithmetic and read as distinct
// when the pane shows it otherwise.
func docsLabelCollisions(pages []docPage, cols int) [][3]string {
	var out [][3]string
	seen := map[string]string{}
	for _, p := range pages {
		cut := render.ClipCols(p.Label, cols)
		if first, dup := seen[cut]; dup {
			out = append(out, [3]string{first, p.Label, cut})
			continue
		}
		seen[cut] = p.Label
	}
	return out
}

// TestTheDistinguishabilityCheckCanActuallyFire is the must-fire half.
//
// The test above reports nothing today and is meant to. That makes it a
// NEGATIVE ASSERTION, which passes for any reason — a corpus that came
// back empty, a ClipCols that returned its input, a comparison against
// the wrong field — and every one of those looks exactly like the corpus
// being fine. Aiming the same function at a width the repo's own paths
// must collide at is what separates the two.
//
// The width is derived from the corpus rather than chosen: the docs tree
// has a specs/ directory holding many pages, so clipping to the length
// of that prefix collapses all of them onto one string.
func TestTheDistinguishabilityCheckCanActuallyFire(t *testing.T) {
	root := docsFS()
	if root == nil {
		t.Skip("no docs/ tree beside this package — see docsFS")
	}
	pages, _ := docsPages(root)
	const dir = "specs/"
	n := 0
	for _, p := range pages {
		if strings.HasPrefix(p.Label, dir) {
			n++
		}
	}
	if n < 2 {
		t.Skipf("the docs tree holds %d pages under %q, so no width makes "+
			"two of them collide", n, dir)
	}
	if got := docsLabelCollisions(pages, len(dir)); len(got) == 0 {
		t.Errorf("clipping %d paths to %d columns produced no collision, and "+
			"%d of them start with %q — so they all clip to the same string and "+
			"the check reported none. It cannot see a real collision either",
			len(pages), len(dir), n, dir)
	}
}
