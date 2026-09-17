package components

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// qualifiedEmbed reports whether e is `pkg.T` or `*pkg.T`, generic forms
// included — an embed from a package this scan does not parse.
//
// It exists so that the two ways receiverName can answer "" stop meaning
// the same thing. gooey.Base is an ast.SelectorExpr and is meant to be
// skipped: this package's scan reads its own directory, so a method
// promoted from another package is residue it states rather than
// follows. Every OTHER unnamed shape is the hole the receiver side
// already treats as an error — a type embedding something this
// extractor cannot name has a method set the scan under-reports, and an
// adornment reached that way goes unchecked with nothing said. Until
// this split, both fell through the same silent `continue`. Raised in
// review of #458.
func qualifiedEmbed(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.SelectorExpr:
			_, ok := x.X.(*ast.Ident)
			return ok
		default:
			return false
		}
	}
}

// receiverName is the TYPE a method hangs off, through every spelling a
// receiver can have: T, *T, and — the one this missed — the generic
// forms T[P] and T[P, Q], which parse as ast.IndexExpr and
// ast.IndexListExpr and reach the Ident only through .X.
//
// It returns "" for a shape it does not know, and the caller treats that
// as an error rather than a skip: see there. For an EMBEDDED FIELD the
// caller asks qualifiedEmbed first, because pkg.T is a shape this
// deliberately does not name.
//
// NOT HYPOTHETICAL. This package already declares generic receivers —
// itemsview.go's Len and At — so before the two Index arms the extractor
// was returning "" on real methods every run and dropping their types
// out of the scan without a word. Measured by removing the arms: the
// error names them. Raised in review of #458.
func receiverName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

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
// adds it, whether it declares those methods or EMBEDS a type that does —
// the promotion pass below is what makes the second half true, and it was
// not true when this sentence was first written. Its limit is the package
// boundary: a type embedding one declared elsewhere is still invisible
// here, which is stated beside the pass rather than left to be
// discovered. Raised in review of #458.
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
	// type -> the types it embeds, for the promotion pass below.
	embeds := map[string][]string{}
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
			// EMBEDDING, collected in the same pass, because a method
			// set is not the same thing as a set of declarations and
			// this scan could only see the second. `type badge struct {
			// DragGhost }` declares neither Anchor nor Place, has both,
			// and was invisible here — and invisible to the floor below
			// too, which catches the scan collapsing to nothing rather
			// than one type going missing. Promoting an embedded type's
			// methods onto the embedder is what makes the derived set a
			// method-set question again. Raised in review of #458.
			//
			// WITHIN THIS PACKAGE. The scan parses this directory, so
			// promotion reaches an embed declared here and stops at one
			// naming another package — gooey.Base is the case, and
			// qualifiedEmbed is where that residue is stated rather
			// than fallen through. Raised in review of #458, round
			// seven.
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, sp := range gd.Specs {
					ts, ok := sp.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok || st.Fields == nil {
						continue
					}
					// fld, not f: f is the FILE path this loop
					// reports with, and a field named f shadows it.
					for _, fld := range st.Fields.List {
						if len(fld.Names) != 0 {
							continue // a named field promotes nothing
						}
						if e := receiverName(fld.Type); e != "" {
							embeds[ts.Name.Name] = append(embeds[ts.Name.Name], e)
							continue
						}
						if qualifiedEmbed(fld.Type) {
							// STATED RESIDUE: an embed from another
							// package, whose methods this scan cannot
							// see because it reads this directory only.
							// gooey.Base is the case, and it carries no
							// Anchor or Place.
							continue
						}
						t.Errorf("%s: %s embeds a type this scan cannot name, so the "+
							"methods it promotes are invisible to the transparency "+
							"check below and an adornment reached that way goes "+
							"unchecked. Teach receiverName the shape, or "+
							"qualifiedEmbed if it names another package",
							f, ts.Name.Name)
					}
				}
				continue
			}
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
				continue
			}
			name := receiverName(fn.Recv.List[0].Type)
			if name == "" {
				// AN ERROR, NOT A continue. A receiver shape this
				// extractor cannot name drops the type out of `methods`
				// entirely, so it never reaches `adornments` and the
				// loop below never asks whether it is transparent — a
				// new adornment goes unchecked and nothing says so. The
				// len(adornments) floor cannot see it either: that
				// catches the scan collapsing to nothing, not a fourth
				// adornment going missing. An exemption that grows by
				// itself is the one shape an exemption must not have,
				// which is the argument docFilesIn makes in this same
				// suite. Raised in review of #458.
				t.Errorf("%s: the receiver of %s is a shape this scan cannot name, "+
					"so its type is invisible to the transparency check below. "+
					"Teach receiverName the shape rather than letting it fall "+
					"through", f, fn.Name.Name)
				continue
			}
			if methods[name] == nil {
				methods[name] = map[string]bool{}
			}
			methods[name][fn.Name.Name] = true
		}
	}

	// PROMOTION TO A FIXED POINT, because embedding chains: a type
	// embedding a type embedding DragGhost has Anchor and Place too, and
	// one pass would stop at the middle. The loop runs until nothing
	// changes, which terminates because Go forbids a cycle of embedded
	// struct types.
	//
	// AN EMBEDDER IS NOT EXEMPTED BY INHERITING HitTestTransparent. It
	// lands in `adornments` carrying the promoted method, so the call
	// table below demands an entry for it — an error naming the type,
	// which is the behaviour that table's paragraph asks for. What
	// embedding must not do is make the type disappear.
	//
	// WHAT THIS STILL CANNOT SEE, stated rather than glossed: a type
	// embedding one declared in ANOTHER package. The scan parses *.go
	// here and nothing else, so promotion stops at the package boundary.
	// That residue is narrower than the gap it closes — every adornment
	// this package ships is declared in it — but it is a residue, and
	// the doc's claim is scoped to match rather than left absolute.
	for changed := true; changed; {
		changed = false
		for outer, inner := range embeds {
			for _, e := range inner {
				for name := range methods[e] {
					if methods[outer][name] {
						continue
					}
					if methods[outer] == nil {
						methods[outer] = map[string]bool{}
					}
					methods[outer][name] = true
					changed = true
				}
			}
		}
	}

	// BY METHOD NAME, which is LOOSER than the Adornment interface it
	// models (components/adorn.go): there is no signature check, so a
	// future type declaring an unrelated Place(int) and Anchor() string
	// would be reported here as an opaque adornment at the top rank.
	// That direction is loud — a false alarm naming a type, not a
	// silence — so it is a cost of the derivation rather than a hole in
	// it, and worth one sentence rather than a types pass. Raised in
	// review of #458.
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
			t.Errorf("%s has Anchor and Place — it is an Adornment — and neither "+
				"declares nor inherits HitTestTransparent. It is therefore OPAQUE at "+
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
