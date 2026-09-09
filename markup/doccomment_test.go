package markup

import (
	"fmt"
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
			for _, s := range stolenComments(fset, f) {
				t.Error(s)
			}
			examined += docsExamined(f)
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

// TestTheDocCommentGuardCatchesWhatItIsFor is the arm that makes the
// guard above falsifiable, and its absence is a finding of its own.
//
// The package is clean, which is the point of the guard and the problem
// with checking it: a walk that applies the rule and a walk that returns
// nil are the same green against a corpus with no theft in it. So the
// rule is pointed at documents whose contents are known, and the
// block-level arm is the one that was silently missing — review of #470
// found the walk blind to every entry of a parenthesised const or var
// block, which is the shape catalog.go's thirty-odd Kind and Category
// constants are written in.
func TestTheDocCommentGuardCatchesWhatItIsFor(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string // "" means no fault
	}{
		{
			name: "a function stolen from",
			src: `// alpha does the alpha thing.
func beta() {}

func alpha() {}
`,
			want: "alpha",
		},
		{
			name: "an honest comment that names itself",
			src: `// alpha does the alpha thing.
func alpha() {}

func beta() {}
`,
		},
		{
			name: "a comment naming something further down is not theft",
			src: `// alpha calls gamma, eventually.
func alpha() {}

func beta() {}

func gamma() {}
`,
		},
		{
			// THE BLOCK CASE. Nothing about this is different in kind and
			// the walk could not see it: f.Decls has ONE entry for the
			// whole const block.
			name: "a const stolen from inside a block",
			src: `const (
	// KindAlpha is the alpha kind.
	KindBeta = "beta"

	KindAlpha = "alpha"
)
`,
			want: "KindAlpha",
		},
		{
			name: "an honest block",
			src: `const (
	// KindAlpha is the alpha kind.
	KindAlpha = "alpha"

	// KindBeta is the beta kind.
	KindBeta = "beta"
)
`,
		},
		{
			name: "a var stolen from inside a block",
			src: `var (
	// alpha is the alpha table.
	beta = map[string]int{}

	alpha = map[string]int{}
)
`,
			want: "alpha",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := gotoken.NewFileSet()
			f, err := goparser.ParseFile(fset, "fixture.go", "package fake\n\n"+tc.src,
				goparser.ParseComments)
			if err != nil {
				t.Fatalf("parsing the fixture: %v", err)
			}
			got := stolenComments(fset, f)
			switch {
			case tc.want == "" && len(got) != 0:
				t.Errorf("reported %v on a document with no theft in it", got)
			case tc.want != "" && len(got) == 0:
				t.Errorf("reported nothing; %s is documented by a comment attached to "+
					"the declaration above it, and `go doc` would show it bare", tc.want)
			case tc.want != "" && len(got) == 1 && !strings.Contains(got[0], tc.want):
				t.Errorf("reported %q, which does not name %s", got[0], tc.want)
			}
			// AND THE POPULATION COUNT SEES THE SAME DOCUMENTS. A walk
			// that reported correctly while counting nothing would leave
			// the non-vacuity floor above resting on other files.
			if n := docsExamined(f); n == 0 {
				t.Errorf("docsExamined found nothing to rule on in a fixture the rule " +
					"itself reads, so the floor in the guard above is measuring a " +
					"different population from the check")
			}
		})
	}
}

// stolenComments is the rule itself, reported as messages.
//
// EXTRACTED so a fixture can drive it, which is the discipline this file
// otherwise only argues for: the package is clean, so a walk that
// checks and a walk that returns nil are the same green, and the first
// version of this guard was checked against nothing but a corpus that
// already passed. TestTheDocCommentGuardCatchesWhatItIsFor parses
// documents whose contents are known.
//
// TWO LEVELS, and the second one was missing. A theft inside a
// parenthesised `const (…)` or `var (…)` block is the same mistake with
// the same silence — insert a spec between a comment and the spec it
// documents and the comment simply becomes the new spec's doc. catalog.go
// is 30-odd documented Kind and Category constants in exactly that shape
// and the guard could not see any of them, because it walked f.Decls and
// a block is ONE decl. Raised in review of #470.
func stolenComments(fset *gotoken.FileSet, f *ast.File) []string {
	var out []string
	report := func(pos gotoken.Pos, name, first, where string) {
		out = append(out, fmt.Sprintf("%s: the doc comment on %s opens by naming %s, "+
			"which is the %s DIRECTLY BELOW it. That is a doc comment that was "+
			"separated from what it documents — either %s was inserted between the "+
			"two, or the blank line between two comment groups was lost, and either "+
			"way %s is now undocumented. Confirm with `go doc -u ./markup %s`",
			fset.Position(pos), name, first, where, first, name, first))
	}
	for i, d := range f.Decls {
		if name, doc, ok := documented(d); ok && i+1 < len(f.Decls) {
			if first := opensBy(doc); first != "" && first != name && declares(f.Decls[i+1], first) {
				report(d.Pos(), name, first, "declaration")
			}
		}
		g, ok := d.(*ast.GenDecl)
		if !ok || !g.Lparen.IsValid() {
			continue
		}
		for j, sp := range g.Specs {
			name, doc, ok := documentedSpec(sp)
			if !ok || j+1 >= len(g.Specs) {
				continue
			}
			if first := opensBy(doc); first != "" && first != name && specDeclares(g.Specs[j+1], first) {
				report(sp.Pos(), name, first, "entry of this block")
			}
		}
	}
	return out
}

// docsExamined is the population stolenComments could rule on: every
// documented declaration, and every documented spec inside a block, that
// has another one below it.
func docsExamined(f *ast.File) int {
	n := 0
	for i, d := range f.Decls {
		if _, _, ok := documented(d); ok && i+1 < len(f.Decls) {
			n++
		}
		g, ok := d.(*ast.GenDecl)
		if !ok || !g.Lparen.IsValid() {
			continue
		}
		for j, sp := range g.Specs {
			if _, _, ok := documentedSpec(sp); ok && j+1 < len(g.Specs) {
				n++
			}
		}
	}
	return n
}

// opensBy is the first word of a doc comment, stripped of the
// punctuation a sentence puts after a name.
func opensBy(doc *ast.CommentGroup) string {
	first, _, _ := strings.Cut(strings.TrimSpace(doc.Text()), " ")
	return strings.TrimRight(first, ",.:")
}

// documentedSpec answers for ONE entry of a parenthesised block: its own
// name and its own doc comment. A spec's Doc is the comment directly
// above it inside the block; the block's own Doc belongs to the GenDecl
// and is documented's business.
func documentedSpec(sp ast.Spec) (name string, doc *ast.CommentGroup, ok bool) {
	switch s := sp.(type) {
	case *ast.ValueSpec:
		if s.Doc == nil || len(s.Names) == 0 {
			return "", nil, false
		}
		return s.Names[0].Name, s.Doc, true
	case *ast.TypeSpec:
		if s.Doc == nil {
			return "", nil, false
		}
		return s.Name.Name, s.Doc, true
	}
	return "", nil, false
}

// specDeclares reports whether sp introduces the name want.
func specDeclares(sp ast.Spec, want string) bool {
	switch sp := sp.(type) {
	case *ast.TypeSpec:
		return sp.Name.Name == want
	case *ast.ValueSpec:
		for _, n := range sp.Names {
			if n.Name == want {
				return true
			}
		}
	}
	return false
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
			if specDeclares(sp, want) {
				return true
			}
		}
	}
	return false
}
