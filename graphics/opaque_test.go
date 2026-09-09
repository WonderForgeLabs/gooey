package graphics

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"slices"
	"strings"
	"testing"
)

// OpaqueEncoder's doc called a mis-classified fourth protocol "the
// failure this whole interface exists to make impossible". It was not
// impossible: the mapping is opt-in, and review of #474 measured it —
// adding OpaqueOnly() to Kitty was caught by a downstream pixel test,
// and adding it to ITerm2 left the ENTIRE root suite green.
//
// ITerm2 is the one the interface's own doc names by hand as taking the
// composited branch, and marking it alpha-less changes the picture every
// iTerm2/WezTerm/mintty user gets. A claim about which encoders answer a
// capability belongs where the capability is declared, not only in one
// consumer's pixel test — a second consumer would have to rediscover it.
var _ OpaqueEncoder = Sixel{}

// TestOnlySixelIsAlphaLess is the table half. The compile-time assertion
// above says Sixel implements it; nothing but this says the others do
// NOT, and "does not implement" is the half that cannot be asserted at
// compile time.
//
// THE SET IS PARSED OUT OF THE PACKAGE, and the first version of this
// test only claimed to be. It compared the table against a second
// hand-written literal twenty lines below it — two lists that had to
// agree, which catches somebody editing one of them and does nothing
// about a fourth encoder added to the package without touching either.
// Review of #474 measured exactly that: a scratch type with Name() and
// Encode() compiled into the package and this test stayed green, which
// is the failure the commit claimed to have made impossible, moved one
// level down into the guard.
//
// go/ast reads the declarations that actually define an encoder, so
// nothing outside this package's source can be mistaken for one and
// nothing inside it can hide. The precedent is
// TestAllowForClassifiesEveryDeclaredKey (allow_test.go), for the same
// reason: a list of names written in a test is the thing being verified.
func TestOnlySixelIsAlphaLess(t *testing.T) {
	// Every encoder this package defines, with the answer it must give.
	// Adding one means adding a row, and that IS the review moment: the
	// question "can this protocol carry alpha?" has to be answered
	// deliberately, because the default — not implementing the interface
	// — is the composited branch, and a wrong default is silent.
	table := map[string]struct {
		enc    Encoder
		opaque bool
	}{
		"Sixel":  {Sixel{}, true},
		"Kitty":  {Kitty{}, false},
		"ITerm2": {ITerm2{}, false},
	}

	declared := declaredEncoders(t)
	for _, name := range declared {
		tc, ok := table[name]
		if !ok {
			t.Errorf("%s implements Encoder and has no row here, so nothing says "+
				"whether its wire carries alpha. The default — not implementing "+
				"OpaqueEncoder — is the composited branch: a decision nobody made, "+
				"about a protocol nobody asked", name)
			continue
		}
		_, got := tc.enc.(OpaqueEncoder)
		if got == tc.opaque {
			continue
		}
		if tc.opaque {
			t.Errorf("%s does not declare OpaqueEncoder. Its wire carries no alpha, "+
				"so a caller drawing a faint line will hand it a translucent stroke "+
				"and the line will be discarded rather than dimmed — which is #254, "+
				"exactly", name)
			continue
		}
		t.Errorf("%s declares OpaqueEncoder. It transmits through png.Encode, so the "+
			"TERMINAL composites against its own background — an answer no "+
			"arithmetic in a component can improve on. Marking it alpha-less "+
			"replaces that with a guessed ground for every user of that terminal",
			name)
	}

	// AND THE OTHER DIRECTION: a row for a type the package no longer
	// defines is a row nobody will notice going stale, and it would keep
	// the count looking right while covering nothing.
	for name := range table {
		if !slices.Contains(declared, name) {
			t.Errorf("this table has a row for %s and the package declares no "+
				"encoder by that name", name)
		}
	}

	// NON-VACUITY. A walk that stopped finding declarations leaves both
	// loops above checking nothing and reporting no problem. The floor
	// is well under the real count so it does not become a second number
	// to maintain — the point is that the walk found encoders at all.
	if len(declared) < 2 {
		t.Fatalf("only %d encoder(s) parsed out of this package (%v); the "+
			"declarations moved and this test is checking nothing",
			len(declared), declared)
	}
}

// declaredEncoders is every type in this package that implements
// Encoder, read out of the source rather than listed.
//
// MATCHED ON THE INTERFACE'S OWN METHOD SET, not on a method name. A
// method called Encode taking something else is not an Encoder, and
// Encoder is what decides — so every method the interface declares is
// read from the same parse, and a type has to carry all of them at the
// declared signatures. Matching Encode alone was the first version, and
// it would have demanded a table row from an unrelated helper that
// happened to take the same arguments. That is also what keeps this from
// drifting silently: change Encoder and nothing matches any more, which
// the non-vacuity floor above turns into a failure rather than an empty
// set.
//
// EMBEDDING COUNTS, and missing it was the hole review round 7 found.
// `type WezTerm struct{ ITerm2 }` is an ordinary way to add a protocol
// variant, and it declares no method of its own — so a walk over
// *ast.FuncDecl saw nothing, demanded no row, and let the new encoder
// take the composited branch by default. The fixed point below promotes
// a struct that embeds a satisfier, repeatedly, so a chain of them is
// covered too.
//
// go/types would answer all of this exactly, and is not used: it needs
// golang.org/x/tools in the root go.mod, which is the dependency
// doctrine CLAUDE.md states, for a test. The AST walk is the shape that
// fits.
func declaredEncoders(t *testing.T) []string {
	t.Helper()
	return encodersIn(t, ".")
}

// encodersIn is declaredEncoders' body with the directory as a
// parameter, and the parameter is the whole reason this is a separate
// function.
//
// Two of the rules above are unobservable through THIS package, because
// stating them needs a type that gets them wrong — a type declaring
// Encode and not Name, or one declaring neither and embedding nothing —
// and such a type has no business in graphics/. Mutating the walk to
// drop either rule was measured SILENT in review round 7 for exactly
// that reason: nothing in the package could tell the two walks apart.
//
// internal/encoderfixture is that type's home. A package of its own
// keeps those types out of the walk over ".", which is the only
// constraint there is; the walk parses it the same way it parses this
// package, against an Encoder interface the fixture declares itself.
//
// A PACKAGE, NOT A testdata DIRECTORY, and the difference is what checks
// the fixture. Under testdata the go tool compiles the file as part of
// nothing, so its claims about embedding were asserted by this walk
// alone — the thing it exists to verify. See the fixture's own package
// doc. Raised in review of #474.
func encodersIn(t *testing.T, dir string) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi fs.FileInfo) bool {
		// The package's own source, not its tests: a scratch encoder in
		// a _test.go file is not something a consumer can be handed.
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}

	var want map[string][]string
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			if ms := encoderMethodSigs(fset, f); ms != nil {
				want = ms
			}
		}
	}
	if len(want) == 0 {
		t.Fatalf("no `type Encoder interface` with methods found in %s, so there "+
			"is no method set to match declarations against", dir)
	}

	// What each receiver declares itself, what each type embeds, and
	// which names are INTERFACES.
	//
	// The third is not bookkeeping. A type satisfies Encoder by
	// embedding the interface itself — `struct{ Encoder }`, the
	// delegating decorator — and by embedding an interface that embeds
	// it. Neither declares a method and neither embeds anything that
	// does, so a walk seeded only from declarations reports nothing for
	// them however many passes it runs; the fixed point below has
	// nothing to propagate FROM. Seeding the interface's own name is
	// what gives it a source. Raised in review of #474, measured silent.
	//
	// The interfaces are then dropped from the answer: this walk feeds a
	// table of concrete encoders, and demanding a row for `Encoder`
	// itself would be a different kind of wrong.
	has := map[string]map[string]bool{}
	embeds := map[string][]string{}
	ifaces := map[string]bool{}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil || len(d.Recv.List) == 0 {
						continue
					}
					sig, ok := want[d.Name.Name]
					if !ok || !slices.Equal(funcSig(fset, d.Type), sig) {
						continue
					}
					n := receiverName(d.Recv.List[0].Type)
					if n == "" {
						continue
					}
					if has[n] == nil {
						has[n] = map[string]bool{}
					}
					has[n][d.Name.Name] = true
				case *ast.GenDecl:
					if d.Tok != token.TYPE {
						continue
					}
					for _, spec := range d.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}
						var fields *ast.FieldList
						switch t := ts.Type.(type) {
						case *ast.StructType:
							fields = t.Fields
						case *ast.InterfaceType:
							// An interface's "fields" are its method
							// list, and an entry with no name is an
							// EMBEDDED interface — the same relation a
							// struct's anonymous field is.
							ifaces[ts.Name.Name] = true
							fields = t.Methods
						}
						if fields == nil {
							continue
						}
						for _, fld := range fields.List {
							if len(fld.Names) != 0 {
								continue // named field or declared method
							}
							if n := receiverName(fld.Type); n != "" {
								embeds[ts.Name.Name] = append(embeds[ts.Name.Name], n)
							}
						}
					}
				}
			}
		}
	}

	sat := map[string]bool{}
	for n, m := range has {
		if len(m) == len(want) {
			sat[n] = true
		}
	}
	// THE INTERFACE SATISFIES ITSELF, and saying so is the whole fix for
	// the embedded-interface case: everything that embeds it, directly
	// or through another interface, now has a satisfied name to reach.
	sat["Encoder"] = true
	// FIXED POINT, because an embedder may itself be embedded. Bounded
	// by the number of types, since each pass either adds one or stops.
	for grew := true; grew; {
		grew = false
		for outer, inner := range embeds {
			if sat[outer] {
				continue
			}
			for _, in := range inner {
				if sat[in] {
					sat[outer], grew = true, true
					break
				}
			}
		}
	}

	found := make([]string, 0, len(sat))
	for n := range sat {
		if ifaces[n] {
			continue
		}
		found = append(found, n)
	}
	slices.Sort(found)
	return found
}

// encoderMethodSigs is every method Encoder declares, by name, with its
// parameter and result types — or nil if this file does not declare the
// interface.
func encoderMethodSigs(fset *token.FileSet, f *ast.File) map[string][]string {
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Encoder" {
				continue
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok {
				continue
			}
			out := map[string][]string{}
			for _, m := range it.Methods.List {
				ft, ok := m.Type.(*ast.FuncType)
				if !ok || len(m.Names) != 1 {
					continue
				}
				out[m.Names[0].Name] = funcSig(fset, ft)
			}
			if len(out) == 0 {
				return nil
			}
			return out
		}
	}
	return nil
}

// funcSig renders a signature as its parameter types followed by "->"
// and its result types, one entry per value — so `out *[]byte, img
// image.Image` and `out *[]byte` are different signatures, and a
// grouped `cols, rows int` counts twice.
func funcSig(fset *token.FileSet, ft *ast.FuncType) []string {
	var out []string
	add := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, p := range fl.List {
			n := max(len(p.Names), 1)
			for range n {
				var b bytes.Buffer
				if err := printer.Fprint(&b, fset, p.Type); err != nil {
					out = append(out, "?")
					continue
				}
				out = append(out, b.String())
			}
		}
	}
	add(ft.Params)
	out = append(out, "->")
	add(ft.Results)
	return out
}

// receiverName is the type name a method is declared on, with any
// pointer and type parameters stripped.
//
// A QUALIFIED NAME RETURNS "", and that is a known boundary rather than
// an oversight. `type Wez struct{ pkg.Base }`, where pkg.Base satisfies
// Encoder, is invisible to this walk: no row is demanded of it and it
// takes the composited branch by default. It is the same shape as the
// `struct{ Encoder }` hole the fixture's Wrapped closed, and it is
// latent only because graphics/ embeds nothing qualified today —
// resolving one would mean resolving imports, which is a type checker
// rather than an AST walk. Noted in review of #474.
func receiverName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return ""
}

// TestTheEncoderWalkNeedsTheWHOLEInterface drives the walk against a
// fixture package, which is the only way to state the two rules this
// package cannot get wrong.
//
// internal/encoderfixture declares an Encoder of the same two-method shape and
// four types around it:
//
//	Full     declares both methods            → an encoder
//	Derived  embeds Full, declares nothing    → an encoder
//	Chained  embeds Derived                   → an encoder
//	Wrapped  embeds the INTERFACE             → an encoder
//	Boxed    embeds an interface embedding it → an encoder
//	Named    the interface doing that         → an encoder, not a TYPE
//	Partial  declares Encode and not Name     → NOT an encoder
//	Shaped   declares Encode with other types → NOT an encoder
//
// Partial is the arm that was silent in round 6. A walk matching the
// method NAME counts it, and would then demand a row in the
// opaque/composited table for a type that cannot be handed to anything
// expecting an Encoder. Shaped is the same mistake one level down, on
// the signature rather than the name.
//
// Wrapped and Boxed are the arms that were silent in round 7, and they
// fail in the OTHER direction: a satisfier the walk cannot see is a
// missing table row, which is a real encoder shipping with no statement
// of whether it is opaque. `struct{ Encoder }` is the delegating
// decorator, not a corner case.
//
// Named is in the want list's shadow rather than in it: it satisfies
// Encoder and is deliberately NOT reported, because the table this
// feeds is of concrete encoders and an interface cannot have a row.
// TestTheEncoderWalkDropsTheInterfacesThemselves is that half.
func TestTheEncoderWalkNeedsTheWHOLEInterface(t *testing.T) {
	got := encodersIn(t, "internal/encoderfixture")
	want := []string{"Boxed", "Chained", "Derived", "Full", "Wrapped"}
	if !slices.Equal(got, want) {
		t.Errorf("the walk reports %v over the fixture package; want %v.\n"+
			"Partial declares Encode and no Name, and Shaped declares an Encode "+
			"of another signature — neither is an Encoder, and counting one "+
			"demands a table row for a type nothing can be handed as one. "+
			"Wrapped and Boxed declare nothing at all and are encoders anyway, "+
			"by embedding the interface and an interface that embeds it — miss "+
			"one and a real encoder ships with no opacity row",
			got, want)
	}
}

// TestTheEncoderWalkDropsTheInterfacesThemselves is the other edge of
// the same seed.
//
// Marking Encoder satisfied is what lets an embedder reach a satisfied
// name, and it also makes Encoder and Named satisfied names in their own
// right. Returning them would demand an opaque/composited row for a type
// nothing can construct — the exact failure the Partial arm guards
// against, arrived at from the opposite side. Asserted separately
// because the want list above cannot say why a name is absent.
func TestTheEncoderWalkDropsTheInterfacesThemselves(t *testing.T) {
	for _, n := range encodersIn(t, "internal/encoderfixture") {
		if n == "Encoder" || n == "Named" {
			t.Errorf("the walk reports %q, which is an INTERFACE. Seeding the "+
				"interface as satisfied is what makes an embedder reachable; "+
				"returning it demands a table row for a type nobody can hand "+
				"over as a value", n)
		}
	}
}
