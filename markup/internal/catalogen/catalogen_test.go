package catalogen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This package had no tests, and that is how two holes in it reached
// review. Both are about the SAME confusion — which element a read
// belongs to — and neither is reachable from the real vocabulary, so
// neither could be pinned against it. testdata/src is the fixture that
// makes them reachable.

func findingsFor(t *testing.T, dir string) []Finding {
	t.Helper()
	out, err := Check("testdata/" + dir)
	if err != nil {
		t.Fatalf("checking the %s fixture: %v", dir, err)
	}
	return out
}

// TestAHostsOwnReadIsNotAChildsAttribute is the first hole.
//
// checkPseudo asks "does the host's Build read this attribute?", and it
// used to ask a walk that recorded reads off EVERY identifier —
// including the host's own element. So a pseudo-element could declare
// an attribute only the host reads off itself and pass: <MenuItem>
// declaring Style, which buildMenuBar reads off the bar, was green while
// `<MenuItem Style="…">` loaded clean and was dropped on the floor.
//
// The fixture reproduces it exactly: <Host> reads Style off e and Text
// off each child. A <Child> declaring Style must be reported.
func TestAHostsOwnReadIsNotAChildsAttribute(t *testing.T) {
	// The baseline first — the clean fixture must be clean, or the
	// assertion below passes for the wrong reason. It differs from the
	// hostread one by exactly the offending line.
	for _, f := range findingsFor(t, "src") {
		t.Errorf("the clean fixture is not clean to begin with: %s", f)
	}

	// Text is read off the child and declared by the child: legal.
	// Style is read off the HOST and declared by the child: not.
	got := findingsFor(t, "hostread")
	var named bool
	for _, f := range got {
		if f.Element == "Child" && strings.Contains(f.String(), "Style") {
			named = true
		}
	}
	if !named {
		t.Errorf("a pseudo-element declaring an attribute the HOST reads off ITSELF was accepted: "+
			"markup setting it would load clean and be silently dropped. findings: %v", got)
	}
}

// TestTheDenyListAppliesToTheChildWalk is the second hole.
//
// The child walk followed any package-level callee by name, where scan
// applies a deny-list (build, buildChildren, checkAttrs, …) so it does
// not wander into the general builder machinery. checkPseudoPool then
// subtracted a set computed WITH the guards from one computed without,
// which is not a subtraction: anything reachable only through the loose
// walk was reported as an attribute no element declares — a finding
// naming something nobody wrote.
//
// The fixture's checkAttrs reads "Smuggled". A clean run is the whole
// assertion.
func TestTheDenyListAppliesToTheChildWalk(t *testing.T) {
	for _, f := range findingsFor(t, "src") {
		if strings.Contains(f.String(), "Smuggled") {
			t.Errorf("the child walk followed a generic builder and reported an attribute "+
				"belonging to no element: %s", f)
		}
	}
}

// TestTheHostsOwnAttributeIsNotReportedUndeclared is the other side of
// the split: <Host> reads Style off itself and declares it, so the pool
// check must not demand that some CHILD declare it.
func TestTheHostsOwnAttributeIsNotReportedUndeclared(t *testing.T) {
	for _, f := range findingsFor(t, "src") {
		if strings.Contains(f.String(), "Style") {
			t.Errorf("the host's own attribute was attributed to its children: %s", f)
		}
	}
}

// TestTheHelperIdiomIsAChildRead is the FALSE-POSITIVE direction, which
// is the loud one: the declaration is real and the read is invisible, so
// the check reports an over-declaration that is not there — a red test
// asserting the opposite of what the code does.
//
// scan has always recognised the form (Bound(e, ctx, "Text"),
// optDuration(e, "Tick")); the child walk did not. The fixture's <Child>
// declares Tick and the host reads it only that way.
func TestTheHelperIdiomIsAChildRead(t *testing.T) {
	for _, f := range findingsFor(t, "src") {
		if strings.Contains(f.String(), "Tick") {
			t.Errorf("an attribute read through a helper was not seen as a read: %s", f)
		}
	}
}

// TestAPseudoElementGetsNoUniversalPass is the hole the universal skip
// left, and it is the same silent-drop defect one set of names over.
//
// The skip is right for an ordinary element — applyLayout consumes
// Margin, Width and the rest outside its Build. A pseudo-element has no
// applyLayout: a nil Proto makes TakesLayout false, so vocabulary()
// never adds the universal set to it. But checkAttrs allows anything in
// spec.Attrs, so DECLARING one makes it settable, unread and dropped.
// Verified in the real vocabulary before the fix: adding Margin to
// defMenuItem left the suite green and
// `<MenuItem Text="Open" Margin="3"/>` built with err == nil.
func TestAPseudoElementGetsNoUniversalPass(t *testing.T) {
	// "Width" is in the universal set and the fixture's host reads it
	// nowhere.
	got := checkPseudo(
		defInfo{name: "Child", declared: map[string]bool{"Width": true}, parsedBy: "Host"},
		buildsFor(t), funcsFor(t))
	if len(got) == 0 {
		t.Error("a pseudo-element declared a universal-named attribute nothing reads and it " +
			"was waved through: markup setting it would load clean and be silently dropped")
	}
}

// TestAPseudoElementsOwnBuildIsScanned closes the branch that skipped
// it. Check's ParsedBy arm continues before the ordinary two-direction
// scan, so a ParsedBy def whose Build reads an attribute had that read
// unattributed AND the declaration serving it reported over-declared —
// both directions wrong at once. Legal shape; today's two only return
// "only valid inside…" errors.
func TestAPseudoElementsOwnBuildIsScanned(t *testing.T) {
	build := childBuildFor(t)
	if build == nil {
		t.Fatal("the fixture's <Child> has no Build: this test would pass vacuously")
	}
	got := checkPseudo(
		defInfo{name: "Child", declared: map[string]bool{"OwnRead": true}, parsedBy: "Host", build: build},
		buildsFor(t), funcsFor(t))
	for _, f := range got {
		if strings.Contains(f.String(), "OwnRead") {
			t.Errorf("an attribute the element's OWN Build reads was reported over-declared: %s", f)
		}
	}
}

// fixtureMaps parses testdata/src the way Check does, so a test can call
// checkPseudo directly with the same inputs the driver would hand it.
// Replicating the collection loop rather than exporting one keeps Check
// a single entry point; the loop is four lines and its shape is pinned
// by every other test in this file going through Check.
func fixtureMaps(t *testing.T) (map[string]*ast.FuncLit, map[string]*ast.FuncDecl, []defInfo) {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir("testdata/src")
	if err != nil {
		t.Fatal(err)
	}
	buildOf := map[string]*ast.FuncLit{}
	funcs := map[string]*ast.FuncDecl{}
	var defs []defInfo
	for _, e := range entries {
		f, err := parser.ParseFile(fset, filepath.Join("testdata/src", e.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[fd.Name.Name] = fd
			}
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, sp := range gd.Specs {
				vs, ok := sp.(*ast.ValueSpec)
				if !ok || len(vs.Values) != 1 {
					continue
				}
				name, declared, build, parsedBy, ok := elementDef(vs.Values[0])
				if !ok {
					continue
				}
				defs = append(defs, defInfo{name, declared, build, parsedBy})
				if build != nil {
					buildOf[name] = build
				}
			}
		}
	}
	if len(buildOf) == 0 || len(funcs) == 0 {
		t.Fatal("the fixture parsed to nothing: every assertion using it would be vacuous")
	}
	return buildOf, funcs, defs
}

func buildsFor(t *testing.T) map[string]*ast.FuncLit {
	t.Helper()
	b, _, _ := fixtureMaps(t)
	return b
}

func funcsFor(t *testing.T) map[string]*ast.FuncDecl {
	t.Helper()
	_, f, _ := fixtureMaps(t)
	return f
}

func childBuildFor(t *testing.T) *ast.FuncLit {
	t.Helper()
	_, _, defs := fixtureMaps(t)
	for _, d := range defs {
		if d.name == "Child" {
			return d.build
		}
	}
	return nil
}
