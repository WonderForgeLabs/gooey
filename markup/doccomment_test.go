package markup

import (
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"strings"
	"testing"
)

// TestNoDocCommentNamesTheDeclarationBelowIt is the guard for a mistake
// with NO OTHER INSTRUMENT: inserting a function between a doc comment
// and the function it documents.
//
// gofmt reformats it happily and `go vet` says nothing, because the
// result is valid Go — the comment simply becomes the new function's doc.
// Review of #470 found gridLens sitting above litInt with litInt's
// thirty-line rationale attached to it, which `go doc -u ./markup litInt`
// showed and nothing in the suite did. Writing this guard immediately
// found a SECOND one in the same branch: refusedTheEmptyValue had been
// inserted above ruleRefusedIt with no blank line, so one comment group
// covered both and ruleRefusedIt went bare.
//
// EVERY DOCUMENTED DECLARATION, not just functions. The first version
// walked *ast.FuncDecl alone, which left out the shape this package is
// most made of: elements.go is 40-odd `var defX = &ElementDef{...}`
// blocks, most of them documented, and inserting a new element between
// one of those comments and its var is the same mistake with the same
// silence. `declares` below already understood a GenDecl on the receiving
// end — it was only the SUBJECT that was narrow, so the guard could see
// a var being stolen from and not a var being stolen from by. Raised in
// review of #470.
//
// THE SIGNATURE, not the convention. "A doc comment opens with the name
// of what it documents" is Go's convention, and asserting it directly
// flags six honest sentences in this package — a test whose name starts
// with the function it exercises, a wrapper whose comment opens by naming
// the unexported form it calls. What theft actually looks like is
// narrower and unambiguous: the comment names X, and X is the very next
// top-level declaration in the file. Nothing legitimate has that shape,
// because a comment about the thing below it would be that thing's
// comment.
func TestNoDocCommentNamesTheDeclarationBelowIt(t *testing.T) {
	fset := gotoken.NewFileSet()
	pkgs, err := goparser.ParseDir(fset, ".", nil, goparser.ParseComments)
	if err != nil {
		t.Fatalf("parse markup/: %v", err)
	}

	var files int
	// EXAMINED, not "found" — the population this rule can judge, which
	// is every documented function with another declaration below it.
	// The counter here before was incremented on the ERROR branch and
	// never read: it could only ever have said "the guard fired n
	// times", which the failures themselves already say, and it was zero
	// in exactly the case a count is for. Raised in review of #470.
	var examined int
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			files++
			for i, d := range f.Decls {
				name, doc, ok := documented(d)
				if !ok || i+1 >= len(f.Decls) {
					continue
				}
				examined++
				first, _, _ := strings.Cut(strings.TrimSpace(doc.Text()), " ")
				first = strings.TrimRight(first, ",.:")
				if first == "" || first == name || !declares(f.Decls[i+1], first) {
					continue
				}
				t.Errorf("%s: the doc comment on %s opens by naming %s, which is the "+
					"declaration DIRECTLY BELOW it. That is a doc comment that was "+
					"separated from what it documents — either %s was inserted between "+
					"the two, or the blank line between two comment groups was lost, and "+
					"either way %s is now undocumented. Confirm with "+
					"`go doc -u ./markup %s`",
					fset.Position(d.Pos()), name, first, name, first, first)
			}
		}
	}
	if files == 0 {
		t.Fatal("no files parsed: this guard would pass vacuously")
	}
	// FILES IS NOT THE FLOOR. A parse that yielded only files with no
	// documented functions in them would satisfy the check above and
	// judge nothing — the count that says this guard did work is the
	// number of doc comments it could have ruled on.
	if examined == 0 {
		t.Fatal("no documented function has a declaration below it, so this guard " +
			"ruled on nothing: the walk is not reaching the package's functions")
	}
	t.Logf("examined %d doc comments across %d files", examined, files)
}

// documented is d's own name and doc comment, for the declarations this
// rule can judge: a package-level func or a var/const/type block.
//
// A method is excluded because its doc opens with the method name and
// the receiver is what disambiguates it; an import block, because it
// declares no name of its own. A parenthesised block answers with its
// FIRST spec's name, which is the one a comment above the block would be
// about.
func documented(d ast.Decl) (name string, doc *ast.CommentGroup, ok bool) {
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Doc == nil || d.Recv != nil {
			return "", nil, false
		}
		return d.Name.Name, d.Doc, true
	case *ast.GenDecl:
		if d.Doc == nil || d.Tok == gotoken.IMPORT || len(d.Specs) == 0 {
			return "", nil, false
		}
		switch s := d.Specs[0].(type) {
		case *ast.ValueSpec:
			if len(s.Names) == 0 {
				return "", nil, false
			}
			return s.Names[0].Name, d.Doc, true
		case *ast.TypeSpec:
			return s.Name.Name, d.Doc, true
		}
	}
	return "", nil, false
}

// declares reports whether d introduces the top-level name want.
func declares(d ast.Decl, want string) bool {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return d.Recv == nil && d.Name.Name == want
	case *ast.GenDecl:
		for _, sp := range d.Specs {
			switch sp := sp.(type) {
			case *ast.TypeSpec:
				if sp.Name.Name == want {
					return true
				}
			case *ast.ValueSpec:
				for _, n := range sp.Names {
					if n.Name == want {
						return true
					}
				}
			}
		}
	}
	return false
}
