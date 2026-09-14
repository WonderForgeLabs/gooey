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
	// TWO MAPS, because one cannot tell a module the walk never reached
	// from a module whose every file the rule declines to judge. The
	// floor below is about the FIRST — a prune or a module boundary
	// silently ending the walk — and marking one map after the skips
	// would have reported the second in its words, sending the reader to
	// look for a prune that is not there. The reverse mistake is worse:
	// marking before them makes a module of nothing but .pb.go read as
	// covered. Raised in review of #503.
	reached := map[string]bool{} // directories the walk yielded a .go file in
	ruled := map[string]bool{}   // and then parsed one this rule applies to
	paths, modules := treeWalk(t)
	for _, path := range paths {
		reached[filepath.Dir(path)] = true
		fset := gotoken.NewFileSet()
		f, err := goparser.ParseFile(fset, path, nil, goparser.ParseComments)
		if err != nil {
			// A file that does not parse is not this guard's business —
			// the compiler is already the instrument for that, and a
			// testdata fixture is allowed to be deliberately broken.
			continue
		}
		// GENERATED CODE IS NOT THIS RULE'S SUBJECT, and the reason is
		// the rule's own premise. The defect is a declaration inserted
		// between a comment and what it documents BY HAND; nobody edits
		// a .pb.go, and a theft in one would be protoc's and would come
		// back on the next generate. Excluding them is what keeps the
		// report actionable.
		//
		// Measured, and it is the only reason this line exists: taking
		// methods into the rule (review of #503) made protoc's
		// "// Enum value maps for ValueKind." land directly above the
		// generated `func (x ValueKind) Enum()`, so seven perfectly
		// correct generated comments in types.pb.go read as theft. That
		// also falsifies half a sentence above — "nothing legitimate has
		// that shape" — for a doc that opens with an ordinary English
		// word which is also the next declaration's name. In
		// hand-written code the claim held across 4777 doc comments;
		// generated code is where it does not.
		if ast.IsGenerated(f) {
			continue
		}
		files++
		ruled[filepath.Dir(path)] = true
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
	for _, mod := range modules {
		switch {
		case !anyUnder(reached, mod):
			t.Errorf("the walk yielded no .go file under %q, which is a module of this "+
				"tree: a guard that stops at a module boundary reports green for code "+
				"it never read", mod)
		case !anyUnder(ruled, mod):
			t.Errorf("every .go file under %q was skipped — it did not parse, or it is "+
				"generated — so this guard read the module and ruled on none of it. "+
				"That is not the prune the case above is about, and it is not "+
				"coverage either", mod)
		}
	}
	t.Logf("examined %d doc comments across %d files", examined, files)
}

// treeWalk is every .go file this guard rules on, and every directory
// holding a go.mod — ONE walk, because they must share a prune policy.
//
// It was two near-identical walks until review of #503, in the file whose
// neighbour (claudemd_test.go) argues that a second copy of a fact
// drifts. The drift here is not symmetric and one direction is a silent
// weakening of the floor itself: add a prune to the file walk alone and a
// module beneath it becomes permanently unreachable, failing with a
// message that blames the wrong thing; add it to the module walk alone
// and a real module drops OUT of the floor — a guard that stopped at a
// module boundary, reporting green, which is the precise failure #483
// exists to prevent.
//
// PRUNED AT EVERY DEPTH, and a top-anchored filter is not enough — the
// same trap CLAUDE.md documents for its verify loop, for the same two
// offenders, both of them untracked so neither shows up in a fresh
// clone: .claude/worktrees/ holds whole checkouts of this repo (a theft
// on somebody else's branch is not this branch's failure) and
// apps/kanban/worker/.venv vendors Go of its own. vendor/ is skipped
// because it is other people's code, and testdata is NOT: a fixture is
// still a file someone reads.
func treeWalk(t *testing.T) (files, moduleDirs []string) {
	t.Helper()
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
		switch {
		case strings.HasSuffix(path, ".go"):
			files = append(files, path)
		case d.Name() == "go.mod":
			moduleDirs = append(moduleDirs, filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	if len(moduleDirs) == 0 {
		t.Fatal("found no go.mod at all, not even the root module's: the module floor " +
			"would pass vacuously")
	}
	return files, moduleDirs
}

// anyUnder reports whether any parsed file's directory is dir or beneath
// it. A module whose own directory holds no .go file — only packages
// below it — is still covered.
//
// THE ROOT MODULE IS ITS OWN CASE, and without this arm the sentence
// above was false for exactly one module — the one the guard lives in.
// filepath.WalkDir(".") yields paths with no "./" prefix, so
// filepath.Dir("input/paste.go") is "input", and for dir == "." the
// prefix test asked for "./…", which nothing the walk produces can
// match. Measured: anyUnder({"input": true}, ".") answered false. It
// fails CLOSED, so it was a latent spurious failure rather than a silent
// pass — but a derived floor quietly meaning something narrower than its
// own comment, inside the test whose thesis is that derived floors beat
// written-down ones, is the finding. Raised in review of #503.
func anyUnder(seen map[string]bool, dir string) bool {
	if dir == "." {
		return len(seen) > 0
	}
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
			// DECORATION AROUND THE NAME. Backticks and the possessive
			// are what prose does to an identifier, and each of them
			// answered a string no declaration can match, so the theft
			// went unreported exactly the way a spaceless first line
			// did. `any` and ItemsView's are the two the tree actually
			// holds. Raised in review of #503.
			name: "a backticked first word is still a first word",
			src:  "// `alpha` does the alpha thing.\nfunc beta() {}\n\nfunc alpha() {}\n",
			want: "alpha",
		},
		{
			name: "a possessive first word is still a first word",
			src: `// alpha's rows are measured before anything is placed.
func beta() {}

func alpha() {}
`,
			want: "alpha",
		},
		{
			// THE POSSESSIVE IS A SUFFIX, NOT A CUTSET, and this is the
			// arm that says so: trimming "s" as a character would turn
			// specs into spec and report a theft of a name the file does
			// declare, one line down.
			name: "a plural first word is not a possessive",
			src: `// specs are read in order.
func specs() {}

func spec() {}
`,
		},
		{
			// ADJACENCY IS THE SIGNATURE, and a block below is adjacent
			// only at its FIRST entry. Both arms are here because the
			// rule used to scan the whole block and announce whatever it
			// found as "the declaration DIRECTLY BELOW it" — five
			// entries from where the name was. Raised in review of #503.
			name: "the first entry of the block directly below is theft",
			src: `// alpha is the alpha table.
func beta() {}

var (
	alpha = map[string]int{}
	gamma = map[string]int{}
)
`,
			want: "alpha",
		},
		{
			name: "a later entry of the block below it is a cross reference",
			src: `// alpha is the alpha table.
func beta() {}

var (
	gamma = map[string]int{}
	delta = map[string]int{}
	alpha = map[string]int{}
)
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
		{
			// THE BLOCK'S OWN DOC, naming an entry further down the same
			// block. Nothing reported this until review of #503: the
			// block answers documented() with its FIRST spec's name, and
			// the spec-level arm cannot see it because the first spec's
			// Doc is nil. It is the shape an insertion at the TOP of a
			// documented block leaves behind.
			name: "a block's doc names a later entry of itself",
			src: `// fuzzyGap is per character skipped.
const (
	fuzzyRun      = 3
	fuzzyBoundary = 8
	fuzzyGap      = 3
)
`,
			want: "fuzzyGap",
		},
		{
			// THE SHAPE FIVE OF THE SIX REAL FINDINGS HAD, which is not
			// an inserted declaration at all: two adjacent comment groups
			// whose separating blank line was lost, so the upper group's
			// subject is now declared below the merged comment. The AST
			// signature is identical to a theft by insertion — which is
			// the point of having it here, since a reader looking for
			// their own case will be looking for this one.
			name: "a lost blank line merged two comment groups",
			src: `// alpha is the alpha thing, and this paragraph is about it.
// beta is a different thing entirely, and the blank line that used to
// separate these two groups is gone.
func beta() {}

func alpha() {}
`,
			want: "alpha",
		},
		{
			// A METHOD, which the rule could not see until review of
			// #503 and which cost it a real theft: panel.go had
			// Measure's doc comment merged into inset's, three lines
			// above the Measure it was written for, and the guard was
			// green over it for as long as documented() checked
			// d.Recv == nil.
			name: "a method stolen from",
			src: `type pane struct{}

// Measure reserves the ring.
func (p *pane) inset() int { return 1 }

func (p *pane) Measure() int { return 2 }
`,
			want: "Measure",
		},
		{
			// THE RECEIVER IS NOT PART OF THE QUESTION. A method's doc
			// naming its own method is honest whatever it hangs off,
			// and the exclusion this replaced was argued from telling
			// two Measures apart — which this rule never has to do,
			// because it only ever compares against the declaration
			// directly below.
			name: "an honest method that names itself",
			src: `type pane struct{}

// Measure reserves the ring.
func (p *pane) Measure() int { return 2 }

func (p *pane) inset() int { return 1 }
`,
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
			// LENGTH FIRST, THEN THE NAME, UNCONDITIONALLY. The name
			// check used to be guarded by len(got) == 1, so a fixture
			// yielding TWO findings matched no arm at all and passed
			// green with neither finding's identity ever examined — a
			// switch that can go green on an unread result, in the test
			// that exists because "a walk that applies the rule and a
			// walk that returns nil are the same green". Raised in
			// review of #503.
			switch {
			case tc.want == "":
				if len(got) != 0 {
					t.Errorf("reported %v on a document with no theft in it", got)
				}
			case len(got) != 1:
				t.Errorf("reported %d findings, want exactly 1 naming %s: %v",
					len(got), tc.want, got)
			case !strings.Contains(got[0], tc.want):
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
	report := func(pos gotoken.Pos, name, first, locator string) {
		at := fset.Position(pos)
		out = append(out, fmt.Sprintf("%s: the doc comment on %s opens by naming %s, "+
			"which is %s. That is a doc comment that was separated from what it "+
			"documents — either %s was inserted between the two, or the blank line "+
			"between two comment groups was lost, and either way %s is now "+
			"undocumented.%s",
			at, name, first, locator, first, name, confirmHint(at.Filename, pkg, first)))
	}
	for i, d := range f.Decls {
		g, block := d.(*ast.GenDecl)
		block = block && g.Lparen.IsValid()
		if name, doc, ok := documented(d); ok {
			if first := opensBy(doc); first != "" && first != name {
				switch {
				case i+1 < len(f.Decls) && declaresDirectlyBelow(f.Decls[i+1], first):
					report(d.Pos(), name, first, "the declaration DIRECTLY BELOW it")
				// A BLOCK'S DOC NAMING A LATER ENTRY OF ITS OWN BLOCK,
				// which nothing reported until review of #503. documented
				// answers for a block with its FIRST spec's name, and the
				// spec-level arm below cannot cover this either — the
				// first spec's own Doc is nil, because the comment
				// belongs to the GenDecl. So inserting a const at the TOP
				// of a documented block left the block's comment
				// describing the newcomer and the guard silent, one
				// keystroke from the apps/wysiwyg/browser.go theft this
				// branch repaired. Measured on a fixture before the fix:
				// nothing reported, and docsExamined counting it as ruled
				// on — which inflated the non-vacuity floor with a case
				// the rule could not judge.
				// THE WHOLE BLOCK, where the two neighbour arms look at
				// exactly one declaration — an asymmetry, and deliberate.
				// "Directly below" is what makes the signature
				// unambiguous between separate declarations: a comment
				// naming something three functions down is a cross
				// reference, and flagging it would be flagging prose. A
				// parenthesised block has no such reading. Its doc
				// belongs to the block, so a doc that opens by naming one
				// of the block's OWN later entries is describing a
				// sibling it does not document, whatever the distance —
				// and the distance is exactly what an insertion at the
				// top changes. Restricting this arm to g.Specs[1] would
				// re-open the hole for two insertions instead of one.
				case block && declaresIn(g.Specs[1:], first):
					report(d.Pos(), name, first,
						"a later entry of the very block it opens")
				}
			}
		}
		if !block {
			continue
		}
		for j, sp := range g.Specs {
			name, doc, ok := documentedSpec(sp)
			if !ok || j+1 >= len(g.Specs) {
				continue
			}
			if first := opensBy(doc); first != "" && first != name && specDeclares(g.Specs[j+1], first) {
				report(sp.Pos(), name, first, "the entry of this block DIRECTLY BELOW it")
			}
		}
	}
	return out
}

// declaresIn reports whether any of these specs introduces want.
func declaresIn(specs []ast.Spec, want string) bool {
	for _, sp := range specs {
		if specDeclares(sp, want) {
			return true
		}
	}
	return false
}

// confirmHint is the remediation instruction, WITHHELD for a test file
// rather than printed wrong.
//
// `go doc` does not read _test.go files at all — `go doc -u . citeForms`
// answers "no symbol in package" — and test files are much of the
// population this guard was widened to reach: two of the six thefts this
// branch repaired are in one, and the guard's own rationale is that
// nobody runs `go doc` on a test file. A command that reports nothing
// reads as a false alarm to whoever the guard just fired on, which is
// worse than no command beside a position that already locates the line.
// Raised in review of #503, which also caught the root package rendering
// as "./.".
func confirmHint(file, pkg, name string) string {
	if strings.HasSuffix(file, "_test.go") {
		return " The position above is the locator — `go doc` does not read _test.go " +
			"files, so it would answer \"no symbol\" here."
	}
	target := "./" + pkg
	if pkg == "." {
		target = "."
	}
	return fmt.Sprintf(" Confirm with `go doc -u %s %s`", target, name)
}

// docsExamined is the population stolenComments could rule on: every
// documented declaration, and every documented spec inside a block, that
// has another one below it.
func docsExamined(f *ast.File) int {
	n := 0
	for i, d := range f.Decls {
		g, block := d.(*ast.GenDecl)
		block = block && g.Lparen.IsValid()
		// A documented block with more than one entry is rulable even as
		// the LAST declaration in the file, because its own doc can name
		// a later entry of itself. Counting it only when something
		// followed it is what let the fixture in review of #503 be
		// counted as ruled on while the rule passed over it.
		if _, _, ok := documented(d); ok && (i+1 < len(f.Decls) || (block && len(g.Specs) > 1)) {
			n++
		}
		if !block {
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
//
// WORD, NOT "UP TO THE FIRST SPACE". Cutting on " " reads a first line
// that holds no space as running into the second: the whole point of
// this guard is the shape
//
//	// TestSomethingLongAndSpaceless.
//	//
//	// The paragraph about it.
//
// where the name is a line of its own, and opensBy answered
// "TestSomethingLongAndSpaceless.\nThe" for it — a string no
// declaration can match, so every arm below fell through and the theft
// went unreported. It was live in markup/menuicon_test.go when this was
// measured. strings.Fields splits on any whitespace, which is the rule
// the sentence above always meant. Raised in review of #503.
//
// THE DECORATION IS PART OF THE SAME MISS, and trailing punctuation was
// only the half that got written down. `any` in backticks and
// ItemsView's in the possessive are both a doc comment opening by
// naming something, and both answered a string no declaration can
// match, so both fell through every arm exactly the way the spaceless
// first line did. Raised in review of #503 as well. What is stripped is
// only ever DECORATION around a Go identifier — backticks, the quotes
// and brackets prose puts round a name, sentence punctuation, and the
// possessive — so widening it cannot invent a match: the result either
// spells an identifier some declaration below actually introduces, or
// no arm fires.
func opensBy(doc *ast.CommentGroup) string {
	fields := strings.Fields(doc.Text())
	if len(fields) == 0 {
		return ""
	}
	const decoration = "`\"'“”‘’()[]*_,.:;!?"
	w := strings.Trim(fields[0], decoration)
	// The possessive is a SUFFIX, not a cutset: trimming "s" as a
	// character would turn Specs into Spec and report a theft of a name
	// nothing declares — or worse, of one something does.
	for _, poss := range []string{"'s", "’s", "'S", "’S"} {
		if t := strings.TrimSuffix(w, poss); t != w {
			w = t
			break
		}
	}
	return strings.Trim(w, decoration)
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
// rule can judge: any func — method or not — or a var/const/type block.
//
// METHODS USED TO BE EXCLUDED, on the grounds that a method's doc opens
// with the method name and the receiver is what disambiguates it. That
// is an argument about telling two Measures APART, and this rule never
// needs to: it asks whether a doc comment names the declaration
// DIRECTLY BELOW it, which is a question about adjacency in one file.
// Excluding them cost a real theft, sitting in the tree while the guard
// was green — apps/wysiwyg/components/panel/panel.go had Measure's doc
// comment merged into inset's, so Measure was undocumented and
// inset's comment opened by describing a method three lines down.
// Raised in review of #503.
//
// An import block is still excluded, because it declares no name of its
// own. A parenthesised block answers with its FIRST spec's name, which
// is the one a comment above the block would be about.
//
// WHAT IS STILL OUT OF REACH, stated because a boundary nobody writes
// down reads as coverage: this walks f.Decls, so it sees declarations
// and not the names INSIDE them. A struct field's doc comment stolen by
// the field below it, and the same in an interface's method list, are
// the same defect one level down and nothing here looks at either. The
// walk would have to descend into StructType.Fields and
// InterfaceType.Methods to reach them, which is a different traversal
// rather than a wider switch — recorded as a gap, not closed. Raised in
// review of #503.
func documented(d ast.Decl) (name string, doc *ast.CommentGroup, ok bool) {
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Doc == nil {
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

// declaresDirectlyBelow reports whether the name want is what the
// declaration d puts DIRECTLY below the comment above it — d's own name
// for a func, and for a parenthesised block its FIRST entry, which is
// the only one the comment is adjacent to.
//
// It used to scan every spec of the block, which made the report's own
// words false: a doc naming the fifth const of the var block below it
// was announced as "the declaration DIRECTLY BELOW it", five entries
// from where the name actually is. Worse than the wording, it is the
// reading this file rules out one arm down — a comment naming something
// three declarations away is a CROSS REFERENCE, and flagging it is
// flagging prose. Distance is only irrelevant for a block's own doc,
// where the comment belongs to the block however long it runs; from
// outside, adjacency is the whole signature. Raised in review of #503.
//
// A method's own name counts, for the reason documented gives above.
func declaresDirectlyBelow(d ast.Decl, want string) bool {
	switch d := d.(type) {
	case *ast.FuncDecl:
		return d.Name.Name == want
	case *ast.GenDecl:
		return len(d.Specs) > 0 && specDeclares(d.Specs[0], want)
	}
	return false
}

// TestTheGeneratedFileSkipIsTheMarkerAndNotTheDirectory is the
// counterfactual for the one exclusion the walk above makes, and it
// exists because an exclusion is a hole in a guard: a skip that matched
// too much would take hand-written files out of the rule and read as
// green.
//
// It asks the same question the walk asks — ast.IsGenerated, which is
// the "// Code generated … DO NOT EDIT." line before the package clause
// — rather than a path test. That is the difference that matters:
// grpc/gen/ is where this repo's generated code happens to live today,
// and a directory rule would have gone on excluding it after somebody
// hand-wrote a file there.
//
// The fixture carries the theft shape protoc actually produces, so the
// skip is measured against the case it was added for rather than an
// invented one.
func TestTheGeneratedFileSkipIsTheMarkerAndNotTheDirectory(t *testing.T) {
	const theft = `// Enum value maps for ValueKind.
var (
	ValueKind_name = map[int32]string{}
)

func (x ValueKind) Enum() *ValueKind { return &x }
`
	parse := func(t *testing.T, src string) *ast.File {
		t.Helper()
		fset := gotoken.NewFileSet()
		f, err := goparser.ParseFile(fset, "fixture.go", src, goparser.ParseComments)
		if err != nil {
			t.Fatalf("parsing the fixture: %v", err)
		}
		return f
	}

	gen := parse(t, "// Code generated by protoc-gen-go. DO NOT EDIT.\n\n"+
		"package fake\n\ntype ValueKind int32\n\n"+theft)
	if !ast.IsGenerated(gen) {
		t.Error("the marker protoc writes does not read as generated, so the walk " +
			"would report seven correct comments in types.pb.go as theft")
	}
	hand := parse(t, "package fake\n\ntype ValueKind int32\n\n"+theft)
	if ast.IsGenerated(hand) {
		t.Fatal("a file with no marker reads as generated, which would take " +
			"hand-written code out of the rule")
	}
	fset := gotoken.NewFileSet()
	if got := stolenComments(fset, parse(t, "package fake\n\ntype ValueKind int32\n\n"+theft), "fake"); len(got) != 1 {
		t.Errorf("the same source reports %d findings when it is NOT generated, "+
			"want 1 — the skip is what suppresses it, so without this the arm "+
			"above would pass over a rule that never fired: %v", len(got), got)
	}
}

// TestTheGuardsDerivedFloorAndItsHintMeanWhatTheySay pins the two halves
// of this file that are prose everywhere else: the coverage floor's reach
// and the remediation instruction.
//
// Both were wrong in the same direction — a comment claiming more than
// the code does — and neither could go red, because the floor fails
// closed only on a tree shaped differently from this one and the hint is
// a string nobody asserts. Raised in review of #503.
func TestTheGuardsDerivedFloorAndItsHintMeanWhatTheySay(t *testing.T) {
	// THE ROOT MODULE. Its directory is ".", and the walk never emits a
	// "./" prefix, so the prefix arm alone answered false for a tree
	// whose every file is in a subdirectory — which is every tree except
	// this one, where a .go file happens to sit in the repo root.
	if !anyUnder(map[string]bool{"input": true}, ".") {
		t.Error("a file parsed under input/ does not count as covering the root " +
			"module, so the floor means \"a .go file sits in the repo root\" rather " +
			"than what anyUnder's comment says")
	}
	if anyUnder(map[string]bool{}, ".") {
		t.Error("an empty walk covers the root module, which would make the floor " +
			"pass over a walk that parsed nothing")
	}
	if !anyUnder(map[string]bool{"mcp/cmd/server": true}, "mcp") {
		t.Error("a file parsed under mcp/cmd/server does not count as covering the " +
			"mcp module, so a module whose own directory holds no .go file is not " +
			"covered after all")
	}
	if anyUnder(map[string]bool{"mcpx": true}, "mcp") {
		t.Error("a sibling directory whose name merely starts with the module's " +
			"counts as covering it")
	}

	// THE HINT. `go doc` cannot answer for a _test.go file, and much of
	// what this guard reaches is test files.
	if got := confirmHint("markup/doc_test.go", "markup", "alpha"); strings.Contains(got, "go doc -u") {
		t.Errorf("the failure message hands `go doc` to somebody whose finding is in "+
			"a test file, where it answers \"no symbol\": %s", got)
	}
	if got := confirmHint("markup/doc.go", "markup", "alpha"); !strings.Contains(got, "go doc -u ./markup alpha") {
		t.Errorf("the hint for an ordinary file is not the command that shows the "+
			"theft: %s", got)
	}
	if got := confirmHint("app.go", ".", "alpha"); !strings.Contains(got, "go doc -u . alpha") {
		t.Errorf("the root package renders as something other than \".\": %s", got)
	}
}
