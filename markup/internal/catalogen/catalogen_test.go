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

// TestAHelperHandedAChildReadsTheChildsAttributes is the third hole, and
// unlike the first two it WAS reachable from the real vocabulary — #400
// tripped it on its first day, when <MenuItem Icon> moved its two reads
// into menuItemIcon(ic, ctx, &it) and the whole vocabulary check went
// red against code that was correct.
//
// Two independent bugs, and each alone is enough to produce the false
// over-declaration:
//
//   - passesElement gated the descent on an argument literally named
//     "e", so a helper handed a child was never entered at all.
//   - the descent then set `self` to the callee's own element parameter,
//     which for such a helper IS the child — so its reads were filed as
//     the HOST's own and never reached the child set.
//
// The fixture's childExtras names its parameter c, matching the caller's
// variable, so the second bug survives fixing only the first.
func TestAHelperHandedAChildReadsTheChildsAttributes(t *testing.T) {
	for _, f := range findingsFor(t, "src") {
		if strings.Contains(f.String(), "Deep") {
			t.Errorf("an attribute read through a helper handed the CHILD was not seen as a read: %s", f)
		}
	}
}

// TestWideningTheGateStaysOutOfTheUndifferentiatedWalk is the other half
// of that fix, and it is here because the obvious repair is wrong.
//
// scan — the walk for ordinary elements — collects ONE read set with no
// receiver split, so widening its gate the same way files a child's
// attribute as the host's own read. Applied to both walks at once it
// produced three fresh under-declarations in the shipped vocabulary
// (<MenuBar> reads "Checked", <Tabs> reads "Header", <Companion> reads
// "Value") — every one of them a child's attribute attributed to its
// host. The fixture says the same thing in miniature: Tick and Deep are
// read off children, so <Host> must not be reported as reading either.
func TestWideningTheGateStaysOutOfTheUndifferentiatedWalk(t *testing.T) {
	for _, f := range findingsFor(t, "src") {
		if f.Element == "Host" {
			t.Errorf("a child's attribute was attributed to the host: %s", f)
		}
	}
}

// TestAHelperTakingCtxFirstStillReadsTheHostsElement is finding 3 of the
// review of #400, and it is the same defect class this file already
// guards arriving through the opposite door.
//
// elementArg answers "the first bare identifier argument", which is the
// element for menuItemIcon(ic, ctx, &it) and NOT the element for the
// idiom markup/attr.go documents to element authors:
//
//	value, err := markup.Attr[*prop.Property[int]](ctx, e, "Value")
//
// There it answers "ctx", which is not the host's `e`, so the walk
// decides the helper was handed a child and files the HOST's own reads
// against a child — reporting the host as over-declaring an attribute it
// plainly reads. Latent in the shipped vocabulary only because no
// ElementDef.Build calls Attr yet.
//
// The fixture's ctxFirst reads "Ctxed" off the host, and <Host> declares
// it. Resolving the element positionally from the callee's signature is
// what keeps that attributed correctly.
func TestAHelperTakingCtxFirstStillReadsTheHostsElement(t *testing.T) {
	for _, f := range findingsFor(t, "src") {
		if strings.Contains(f.String(), "Ctxed") {
			t.Errorf("a host attribute read through a ctx-first helper was misattributed: %s", f)
		}
	}
}

// TestOneHelperUsedInBothRolesIsScannedInBoth is finding 4.
//
// The descent's `seen` set keyed on the FUNCTION NAME, and it is checked
// before the walk and set before it, so a builder calling one helper
// twice — once with its own element, once with a child — recorded only
// whichever call came first. The second call's reads never landed, which
// is a read going invisible: a false over-declaration, the class this
// whole file exists to catch.
//
// The fixture calls bothRoles on the host AND on each child, and that
// helper reads "Roled" as a HARDCODED index rather than a string
// argument — which is what makes this reachable at all. An attribute
// named as a call argument is picked up by the literal branch whether or
// not the descent runs, so a first attempt at this fixture could not see
// the bug it was written for.
func TestOneHelperUsedInBothRolesIsScannedInBoth(t *testing.T) {
	for _, f := range findingsFor(t, "src") {
		if strings.Contains(f.String(), "Roled") {
			t.Errorf("one helper used in two roles lost a role's reads: %s", f)
		}
	}
}

// TestAUniversalReadOffAPseudoChildIsReported is finding 5 of the review
// of #454, and it is an asymmetry rather than a missing check.
//
// checkPseudo dropped its `universal[a]` skip when it was established
// that a pseudo-element has no applyLayout and therefore no universal
// set. checkPseudoPool — the under-declared direction of the same check,
// a hundred lines up — kept the skip, so the two halves asserted
// opposite things about the same eight names.
//
// The consequence is a dead read waved through: a host reading
// ic.Attrs["Margin"] off a pseudo child, which vocabulary() will never
// let markup set, is exactly the kind of thing this package exists to
// surface. The hostread fixture reads Margin off its child and nobody
// declares it.
func TestAUniversalReadOffAPseudoChildIsReported(t *testing.T) {
	var named bool
	got := findingsFor(t, "hostread")
	for _, f := range got {
		if strings.Contains(f.String(), "Margin") {
			named = true
		}
	}
	if !named {
		t.Errorf("a universal attribute read off a pseudo child and declared by nobody was waved through: "+
			"markup can never set it, so the read is dead. findings: %v", got)
	}
}
