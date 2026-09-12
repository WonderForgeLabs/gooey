package gooey

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoDocCommentNamesTheDeclarationBelowIt is the guard for a mistake
// with NO OTHER INSTRUMENT: inserting a declaration between a doc comment
// and the thing it documents.
//
// gofmt reformats it happily and `go vet` says nothing, because the
// result is valid Go — the comment simply becomes the newcomer's doc. The
// original goes bare, the newcomer gains a comment describing something
// else, and `go doc` is the only tool that shows it. Nobody runs `go doc`
// on a test file.
//
// TREE-WIDE, AND THAT IS THE POINT OF THIS VERSION (#483). The rule was
// written in #470 scoped to markup/, which is where it was first needed
// and not where the mistake lives: the overlay/z-order story alone
// produced three, in markup/markup.go, in zorderdocs_test.go and in
// overlayhit_test.go — two of them outside the one package that was
// guarded. A guard that only watches the package it was born in reports
// green for the whole tree, which is worse than no guard, because the
// green is read as an answer about the tree.
//
// EVERY .go FILE, parsed rather than built, which is what lets ONE test
// in the root module rule on the nested ones too. Nothing here needs a
// package to compile or a module to resolve, so the module boundary that
// stops `./...` is not a boundary for this.
//
// THE SIGNATURE, not the convention. "A doc comment opens with the name
// of what it documents" is Go's convention, and asserting it directly
// flags honest sentences — a test whose name starts with the function it
// exercises, a wrapper whose comment opens by naming the unexported form
// it calls. What theft actually looks like is narrower and unambiguous:
// the comment names X, and X is the very next declaration. Nothing
// legitimate has that shape, because a comment about the thing below it
// would be that thing's comment.
func TestNoDocCommentNamesTheDeclarationBelowIt(t *testing.T) {
	var files, examined int
	seen := map[string]bool{} // directories that contributed a parsed file
	for _, path := range goFilesInTree(t) {
		fset := gotoken.NewFileSet()
		f, err := goparser.ParseFile(fset, path, nil, goparser.ParseComments)
		if err != nil {
			// A file that does not parse is not this guard's business —
			// the compiler is already the instrument for that, and a
			// testdata fixture is allowed to be deliberately broken.
			continue
		}
		files++
		seen[filepath.Dir(path)] = true
		for _, s := range stolenComments(fset, f, filepath.Dir(path)) {
			t.Error(s)
		}
		examined += docsExamined(f)
	}
	if files == 0 {
		t.Fatal("no files parsed: this guard would pass vacuously")
	}
	// FILES IS NOT THE FLOOR. A walk that yielded only files with no
	// documented declarations would satisfy the check above and judge
	// nothing — the count that says this guard did work is the number of
	// doc comments it could have ruled on.
	if examined == 0 {
		t.Fatal("no documented declaration has another below it, so this guard ruled " +
			"on nothing: the walk is not reaching the tree's source")
	}
	// AND NEITHER IS A COUNT. The failure this widening exists to prevent
	// is a walk that silently stops at a module boundary — exactly what
	// the markup/-scoped version did — and no total, however large, can
	// see that: the root module alone would satisfy any number worth
	// writing down. So the floor is DERIVED from the tree: every module
	// in it must have contributed at least one parsed file. A module
	// added tomorrow is covered without anyone editing this test, which
	// is the same discipline CLAUDE.md's verify loop uses against the
	// same mistake.
	for _, mod := range moduleDirsInTree(t) {
		if !anyUnder(seen, mod) {
			t.Errorf("the walk parsed no file under %q, which is a module of this tree: "+
				"a guard that stops at a module boundary reports green for code it "+
				"never read", mod)
		}
	}
	t.Logf("examined %d doc comments across %d files", examined, files)
}

// goFilesInTree is every .go file this guard rules on.
//
// PRUNED AT EVERY DEPTH, and a top-anchored filter is not enough — the
// same trap CLAUDE.md documents for its verify loop, for the same two
// offenders, both of them untracked so neither shows up in a fresh
// clone: .claude/worktrees/ holds whole checkouts of this repo (a theft
// on somebody else's branch is not this branch's failure) and
// apps/kanban/worker/.venv vendors Go of its own. vendor/ is skipped
// because it is other people's code, and testdata is NOT: a fixture is
// still a file someone reads.
func goFilesInTree(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == "." {
				return nil
			}
			if name := d.Name(); strings.HasPrefix(name, ".") || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	return out
}

// moduleDirsInTree is every directory holding a go.mod, which is the
// population the coverage floor above is derived from. Same pruning, same
// reasons.
func moduleDirsInTree(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == "." {
				return nil
			}
			if name := d.Name(); strings.HasPrefix(name, ".") || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			out = append(out, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree for modules: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("found no go.mod at all, not even the root module's: the module floor " +
			"below would pass vacuously")
	}
	return out
}

// anyUnder reports whether any parsed file's directory is dir or beneath
// it. A module whose own directory holds no .go file — only packages
// below it — is still covered.
func anyUnder(seen map[string]bool, dir string) bool {
	for d := range seen {
		if d == dir || strings.HasPrefix(d, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// TestTheDocCommentGuardCatchesWhatItIsFor is the arm that makes the
// guard above falsifiable, and its absence is a finding of its own.
//
// The tree is clean, which is the point of the guard and the problem
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
			got := stolenComments(fset, f, "fake")
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
func stolenComments(fset *gotoken.FileSet, f *ast.File, pkg string) []string {
	var out []string
	report := func(pos gotoken.Pos, name, first, where string) {
		out = append(out, fmt.Sprintf("%s: the doc comment on %s opens by naming %s, "+
			"which is the %s DIRECTLY BELOW it. That is a doc comment that was "+
			"separated from what it documents — either %s was inserted between the "+
			"two, or the blank line between two comment groups was lost, and either "+
			"way %s is now undocumented. Confirm with `go doc -u ./%s %s`",
			fset.Position(pos), name, first, where, first, name, pkg, first))
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
