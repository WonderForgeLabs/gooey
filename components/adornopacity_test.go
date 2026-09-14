package components

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEveryAdornmentIsHitTestTransparent replaces a grep with a check.
//
// adorn.go states the failure mode exactly: Add is exported, Adornment
// is an interface requiring only Component/Anchor/Place, and since #465
// the hit walk answers by layer then rank — so an adornment that simply
// omits HitTestTransparent is OPAQUE at OverlayRankAdornment, the top
// rank, above popups and above toasts. It takes the press — and the
// hover with it, since DispatchMouse derives both from one walk — over
// the field it is pinned beside, for exactly as long as its anchor is
// invalid, and there is no load error, no vet and no test to say so.
//
// The paragraph then said "which is the grep to run rather than a count
// to trust here". A grep is what this branch's whole thesis is about
// replacing: a written list guarding a written list was measured SILENT
// when a name was dropped from it. Raised in review of #458.
//
// DERIVED FROM SOURCE, the way TestEveryExportedOverlayHostIsNamed is:
// the subject is every type in this package that declares BOTH halves of
// the Adornment interface's own methods — Anchor() and Place() — which
// is a question only the source can answer, not a table here. A fourth
// adornment added next quarter comes under this guard on the commit that
// adds it.
//
// NOT asked of the type system through a list of values, because the
// values are the list: gooey.HitTestTransparent is satisfied by
// AdornmentLayer itself and by components that are not adornments, so
// "does this implement it" cannot find the ones that should and do not.
func TestEveryAdornmentIsHitTestTransparent(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("globbing this package: %v", err)
	}
	// receiver -> set of methods it declares, over the non-test files.
	methods := map[string]map[string]bool{}
	recvRe := regexp.MustCompile(`^\*?(\w+)$`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			var buf strings.Builder
			switch x := fn.Recv.List[0].Type.(type) {
			case *ast.StarExpr:
				if id, ok := x.X.(*ast.Ident); ok {
					buf.WriteString(id.Name)
				}
			case *ast.Ident:
				buf.WriteString(x.Name)
			}
			name := buf.String()
			if !recvRe.MatchString(name) || name == "" {
				continue
			}
			if methods[name] == nil {
				methods[name] = map[string]bool{}
			}
			methods[name][fn.Name.Name] = true
		}
	}

	var adornments []string
	for recv, m := range methods {
		if m["Anchor"] && m["Place"] {
			adornments = append(adornments, recv)
		}
	}
	// NON-VACUITY, and it is the arm that matters: this walk answering
	// "no adornments" would pass the loop below over nothing, which is
	// exactly the silence the grep already had. Three ship today.
	if len(adornments) < 3 {
		t.Fatalf("the source scan found %v — fewer than the three adornments "+
			"this package is known to ship (tipPopup, markerPopup, DragGhost). "+
			"The scan is broken, not the package, and every assertion below it "+
			"would pass over nothing", adornments)
	}
	t.Logf("adornments found in source: %v", adornments)

	// CALLED, not merely declared. The source scan can only see that a
	// method exists, and `HitTestTransparent() bool { return false }`
	// satisfies that while being the exact defect this guards: opaque at
	// the top overlay rank, taking the press over the field it is pinned
	// beside. The interface is satisfied by the signature, so nothing in
	// the type system distinguishes the two either. Raised in review of
	// #458.
	//
	// A TABLE, and a hand-written one, which the paragraph above argues
	// against for the SET — so the derivation stays in charge of the set
	// and the table only supplies a call. An adornment missing from it is
	// an error rather than a silence, so a fourth still forces an edit
	// here; what it cannot do is quietly shrink the subject.
	transparency := map[string]func() bool{
		"tipPopup":    func() bool { return (&tipPopup{}).HitTestTransparent() },
		"markerPopup": func() bool { return (&markerPopup{}).HitTestTransparent() },
		"DragGhost":   func() bool { return (&DragGhost{}).HitTestTransparent() },
	}
	for _, a := range adornments {
		if !methods[a]["HitTestTransparent"] {
			t.Errorf("%s declares Anchor and Place — it is an Adornment — and "+
				"does not declare HitTestTransparent. It is therefore OPAQUE at "+
				"OverlayRankAdornment, the top overlay rank, and takes the press "+
				"and the hover over whatever it is pinned beside for as long as its "+
				"anchor is invalid. See AdornmentLayer.HitTestTransparent in "+
				"adorn.go", a)
			continue
		}
		call, ok := transparency[a]
		if !ok {
			t.Errorf("%s is an adornment and nothing here CALLS its "+
				"HitTestTransparent, so a `return false` in it would pass this "+
				"test. Add an entry to transparency that constructs one", a)
			continue
		}
		if !call() {
			t.Errorf("%s.HitTestTransparent() returns false. It is therefore OPAQUE "+
				"at OverlayRankAdornment, the top overlay rank, and takes the press "+
				"and the hover over whatever it is pinned beside", a)
		}
	}
}
