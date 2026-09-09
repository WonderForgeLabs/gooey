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
// MATCHED ON THE INTERFACE'S OWN SIGNATURE, not on the method name. A
// method called Encode taking something else is not an Encoder, and
// Encoder is what decides — so the parameter and result types are
// compared against the ones the interface declares, which are read from
// the same parse. That is also what keeps this from drifting silently:
// change Encoder.Encode and nothing matches any more, which the
// non-vacuity floor above turns into a failure rather than an empty set.
func declaredEncoders(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		// The package's own source, not its tests: a scratch encoder in
		// a _test.go file is not something a consumer can be handed.
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse graphics/: %v", err)
	}

	var want []string
	var found []string
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			if sig := encoderMethodSig(fset, f); sig != nil {
				want = sig
			}
		}
	}
	if want == nil {
		t.Fatal("no `type Encoder interface` with an Encode method found in this " +
			"package, so there is no signature to match declarations against")
	}
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 {
					continue
				}
				if fn.Name.Name != "Encode" || !slices.Equal(funcSig(fset, fn.Type), want) {
					continue
				}
				if n := receiverName(fn.Recv.List[0].Type); n != "" &&
					!slices.Contains(found, n) {
					found = append(found, n)
				}
			}
		}
	}
	slices.Sort(found)
	return found
}

// encoderMethodSig is Encoder.Encode's parameter and result types, or
// nil if this file does not declare the interface.
func encoderMethodSig(fset *token.FileSet, f *ast.File) []string {
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
			for _, m := range it.Methods.List {
				ft, ok := m.Type.(*ast.FuncType)
				if !ok || len(m.Names) != 1 || m.Names[0].Name != "Encode" {
					continue
				}
				return funcSig(fset, ft)
			}
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
