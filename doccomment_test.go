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
	reached := map[string]bool{} // modules the walk yielded a .go file in
	ruled := map[string]bool{}   // and then parsed one this rule applies to
	paths, modules := treeWalk(t)
	for _, path := range paths {
		reached[owningModule(filepath.Dir(path), modules)] = true
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
		// hand-written code the claim held across every doc comment in
		// the tree; generated code is where it does not.
		//
		// NO FIGURE HERE, and the omission is the point: this said 4777
		// and the guard's own t.Logf already reported a different number
		// on the same branch, because the bounds moved under it. A count
		// in prose is a sample taken once — CLAUDE.md's Verify section
		// says so — and this one is DERIVED on every run a few hundred
		// lines down, which is the reader's source. Raised in review of
		// #503.
		if ast.IsGenerated(f) {
			continue
		}
		files++
		ruled[owningModule(filepath.Dir(path), modules)] = true
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
		case !reached[mod]:
			t.Errorf("the walk yielded no .go file under %q, which is a module of this "+
				"tree: a guard that stops at a module boundary reports green for code "+
				"it never read", mod)
		case !ruled[mod]:
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
			// VENDOR IS PRUNED HERE AND NOT IN discoverModules, which
			// mirrors CLAUDE.md's find and prunes only dot-directories.
			// TestTheGuardsModuleFloorMatchesTheTreesOwnDiscovery
			// compares the two sets, so a vendored go.mod appearing
			// would fail that test with a message blaming this floor for
			// something that is not its doing. Harmless today and stays
			// so as long as `go work vendor` keeps stripping the
			// vendored modules' own go.mod files — which CLAUDE.md's
			// "One workspace" section is the record for. Written down
			// because the asymmetry is invisible from either file alone.
			// Raised in review of #503.
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

// TestTheGuardsModuleFloorMatchesTheTreesOwnDiscovery is what makes the
// floor's REACH checkable rather than self-certified.
//
// treeWalk is a third implementation of "where are the modules" beside
// CLAUDE.md's verify loop and ci.yml's matrix, which are pinned to each
// other character-for-character by
// TestCIWorkflowAndCLAUDEMDShareOneDiscovery. The sets agree today. The
// residual risk is the one this file exists to remove, one level up: a
// prune added to treeWalk for some future heavy directory takes the
// modules beneath it out of BOTH halves together, so the floor stays
// internally consistent while covering less, and nothing goes red.
// Raised in review of #503.
func TestTheGuardsModuleFloorMatchesTheTreesOwnDiscovery(t *testing.T) {
	_, mods := treeWalk(t)
	got := map[string]bool{}
	for _, m := range mods {
		if s := filepath.ToSlash(m); s != "." {
			got[s] = true
		}
	}
	want := map[string]bool{}
	for _, m := range discoverModules(t) {
		want[m] = true
	}
	if len(want) == 0 {
		t.Fatal("the canonical discovery found no nested module, so this " +
			"comparison would pass against anything")
	}
	for m := range want {
		if !got[m] {
			t.Errorf("the canonical discovery finds %s and this guard's walk does "+
				"not, so the doc-comment floor no longer covers that module and "+
				"nothing else would say so", m)
		}
	}
	for m := range got {
		if !want[m] {
			t.Errorf("this guard's walk finds %s and the canonical discovery does "+
				"not — the floor is asserting coverage of a module the tree's own "+
				"loop does not build", m)
		}
	}
}

// owningModule is the module a parsed file belongs to: the longest
// directory holding a go.mod that contains dir. A module whose own
// directory holds no .go file — only packages below it — is still
// covered, because every one of those packages attributes back to it.
//
// NEAREST ENCLOSING, not "anywhere under", and the difference is the
// whole floor. The first version asked "did the walk yield a file
// somewhere beneath this module's directory", which every nested module
// answers for the root module too: `mcp/server.go` sits under ".", so
// the root module's entry was satisfied by code the root module does not
// contain, and the arm collapsed into a restatement of the files == 0
// check above. The same prefix logic would let a module nested inside
// another module's directory satisfy its PARENT's entry — latent today
// (no go.mod directory in this tree is a strict prefix of another except
// ".") and live the day somebody adds one. Attributing to the longest
// prefix closes both at once. Raised in review of #503.
//
// The root module is the fallback rather than a special case: "." is a
// prefix of everything, and is the shortest, so it wins only where no
// nested module claims the file. filepath.WalkDir(".") yields paths with
// no "./" prefix, which is why the containment test cannot be a plain
// strings.HasPrefix against dir + separator for "." — it would ask for
// "./…", and nothing the walk produces matches that.
//
// "SHORTEST" IS prefixLen's ANSWER, NOT len's. This compared len(m)
// until review of #503, and len(".") is 1 — so the root module TIED with
// any single-character top-level module directory and, being first in
// the sorted list, won. `z/foo.go` would have attributed to the root
// module, satisfying the root's floor entry with code it does not
// contain: the exact defect the paragraph above says the longest-prefix
// rule closes, reintroduced by the one directory name that is spelled
// with a character and matches none.
func owningModule(dir string, moduleDirs []string) string {
	owner, found := "", false
	for _, m := range moduleDirs {
		if m != "." && dir != m && !strings.HasPrefix(dir, m+string(filepath.Separator)) {
			continue
		}
		// `found` rather than comparing against owner's zero value: the
		// root module's prefix length IS zero, so `prefixLen(m) >
		// prefixLen(owner)` alone would never select it and every file
		// outside a nested module would attribute to "".
		if !found || prefixLen(m) > prefixLen(owner) {
			owner, found = m, true
		}
	}
	return owner
}

// prefixLen is how much of a path a module directory actually claims.
// For every module but the root that is its length; for "." it is zero,
// because the root module's directory is spelled with a character and
// matches none of them.
func prefixLen(m string) int {
	if m == "." {
		return 0
	}
	return len(m)
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
		// wantMsg is the message body, asserted where the WORDING is the
		// finding rather than the identification. The name check below
		// cannot see a remediation sentence that sends the reader to the
		// wrong edit, which is how the block arm came to carry the
		// neighbour arms' tail. Raised in review of #503.
		wantMsg string
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
			wantMsg: "the doc comment on fuzzyRun opens by naming fuzzyGap, which is " +
				"a later entry of the very block it opens. That is a block doc that " +
				"no longer opens on its own first entry — either fuzzyRun was " +
				"inserted above fuzzyGap, or fuzzyGap was moved down past it, and " +
				"either way fuzzyRun has inherited a comment written for fuzzyGap.",
		},
		{
			// TWO INSERTIONS INSIDE ONE BLOCK, which the spec arm could
			// not see while it tested exactly one neighbour — the hole
			// the block-doc arm's comment argues must not exist at this
			// level either. Silent before review of #503.
			name: "a spec's doc names a sibling two entries down",
			src: `const (
	// KindAlpha is the alpha kind.
	KindBeta  = "beta"
	KindGamma = "gamma"
	KindAlpha = "alpha"
)
`,
			want: "KindAlpha",
			wantMsg: "the doc comment on KindBeta opens by naming KindAlpha, which " +
				"is a later entry of this block. That is a doc comment that was " +
				"separated from what it documents — either KindBeta was inserted " +
				"between it and KindAlpha, or the blank line between two comment " +
				"groups was lost, and either way KindBeta is now undocumented.",
		},
		{
			// THE SHAPE MOST OF THE REAL FINDINGS HAD, which is not
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
			// AN UNDERSCORE IS PART OF THE NAME. It was in opensBy's
			// decoration cutset for markdown emphasis, which Go doc
			// comments do not have, so `_handler` trimmed to `handler`,
			// matched no declaration, and the theft went unreported —
			// the false NEGATIVE the "cannot invent a match" argument
			// does not cover. Raised in review of #503.
			name: "a leading underscore is part of the identifier",
			src: `// _handler dispatches the request.
func beta() {}

func _handler() {}
`,
			want: "_handler",
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
			case tc.wantMsg != "" && !strings.Contains(got[0], tc.wantMsg):
				t.Errorf("reported\n\t%q\nwant it to carry\n\t%q", got[0], tc.wantMsg)
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
	report := func(pos gotoken.Pos, name, first, locator, remedy string) {
		at := fset.Position(pos)
		out = append(out, fmt.Sprintf("%s: the doc comment on %s opens by naming %s, "+
			"which is %s. %s%s",
			at, name, first, locator, remedy, confirmHint(at.Filename, pkg, first)))
	}
	// TWO REMEDIES, because the two shapes are repaired by different
	// edits and one sentence described only the first. The neighbour arms
	// found a comment SEPARATED from its subject by something new between
	// them. The block-doc arm found the opposite arrangement — the
	// insertion is at the TOP of the block, above the comment's subject,
	// and nothing is undocumented: the block's first entry has inherited
	// a comment written for a sibling. Sending that reader looking for an
	// insertion "between the two" is the class of defect
	// declaresDirectlyBelow's own comment records fixing one arm over.
	// Raised in review of #503.
	// THE INSERTED DECLARATION IS name, NOT first. first is what the
	// comment documents — one of "the two" the sentence has just
	// established — so it cannot have been inserted between itself and
	// its own comment. The declaration that came between them is the one
	// the comment now sits on. The file header states the defect in that
	// direction ("leaves the comment attached to the newcomer") and the
	// sibling below has always had the roles right; this closure was the
	// one place they were swapped, and wantMsg asserted the swap as
	// correct, so nothing could go red over it. Raised in review of
	// #503, which is also where the same shape was fixed on the block
	// arm.
	separated := func(name, first string) string {
		return fmt.Sprintf("That is a doc comment that was separated from what it "+
			"documents — either %s was inserted between it and %s, or the blank line "+
			"between two comment groups was lost, and either way %s is now "+
			"undocumented.", name, first, name)
	}
	inherited := func(name, first string) string {
		return fmt.Sprintf("That is a block doc that no longer opens on its own "+
			"first entry — either %s was inserted above %s, or %s was moved down "+
			"past it, and either way %s has inherited a comment written for %s.",
			name, first, first, name, first)
	}
	for i, d := range f.Decls {
		g, block := d.(*ast.GenDecl)
		block = block && g.Lparen.IsValid()
		if name, doc, ok := documented(d); ok {
			if first := opensBy(doc); first != "" && first != name {
				switch {
				case i+1 < len(f.Decls) && declaresDirectlyBelow(f.Decls[i+1], first):
					report(d.Pos(), name, first, "the declaration DIRECTLY BELOW it",
						separated(name, first))
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
						"a later entry of the very block it opens",
						inherited(name, first))
				}
			}
		}
		if !block {
			continue
		}
		for j, sp := range g.Specs {
			name, doc, ok := documentedSpec(sp)
			// THE LAST SPEC IS SKIPPED, AND THAT IS THE BOUNDARY rather
			// than an omission. Review of #503 read it as a gap — a doc
			// on the last entry whose subject is declared directly below
			// the CLOSING PAREN — so the reasoning goes here to stop the
			// next reader re-deriving it.
			//
			// The signature this guard recognises is INSERTION: a
			// declaration slid between a doc comment and the thing it
			// documented. A comment inside the parentheses cannot have
			// been separated that way from a declaration outside them,
			// because the closing paren sits between the two and nothing
			// gets inserted across it — moving the paren is not an
			// insertion, it is a rewrite of the block. So a last spec's
			// doc naming something after the block is a cross reference,
			// which is prose, and flagging prose is the failure the
			// neighbour arms' "directly below" fence exists to avoid.
			//
			// docsExamined applies the SAME `j+1 < len(g.Specs)` bound,
			// so the population and the rule are in step and the floor
			// is not undercounted — measured against the review's claim
			// that it was. The two must move together if this ever
			// changes.
			if !ok || j+1 >= len(g.Specs) {
				continue
			}
			// THE WHOLE TAIL, for the reason the block-doc arm above
			// gives about distance inside a parenthesised block: a spec
			// doc is inside that block too, so a doc opening with a
			// sibling's name is describing a sibling it does not
			// document however far down that sibling sits. The cross
			// reference objection that makes DIRECTLY BELOW the right
			// fence BETWEEN declarations does not reach here — a doc
			// opening with its own name is already excluded by
			// first != name, so what is left is the theft signature and
			// not prose. Restricted to g.Specs[j+1] this arm was blind
			// to two insertions where the arm eighteen lines up argued
			// it must not be. Raised in review of #503.
			//
			// ONE FALSE-POSITIVE CLASS IS ACCEPTED, and it is recorded
			// here rather than suppressed because the suppression would
			// cost more than the class does. A JOINT doc opens on the
			// other name it documents:
			//
			//	const (
			//		// alpha and beta are the pair.
			//		beta  = 2
			//		alpha = 3
			//	)
			//
			// That is the theft signature exactly, and the defence above
			// does not cover it — the doc opens on a sibling because it
			// is describing a sibling, legitimately. Measured before
			// accepting it: ZERO instances across the tree's doc
			// comments, including the thirty-odd-entry catalog.go blocks
			// this arm was written for. The remedy for whoever meets it
			// is to open the sentence on the name the doc sits above,
			// which is what Go's own convention asks for anyway — and
			// there is deliberately no escape hatch, because a hatch
			// that no site in the tree needs is a hatch the next theft
			// can use. Raised in review of #503 round 2.
			first := opensBy(doc)
			if first == "" || first == name || !declaresIn(g.Specs[j+1:], first) {
				continue
			}
			locator := "a later entry of this block"
			if specDeclares(g.Specs[j+1], first) {
				locator = "the entry of this block DIRECTLY BELOW it"
			}
			report(sp.Pos(), name, first, locator, separated(name, first))
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
// population this guard was widened to reach: several of the thefts this
// branch repaired are in test files, and the guard's own rationale is that
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
	// NO UNDERSCORE. It was here for markdown emphasis, which Go's doc
	// comment syntax does not have, and it is a legal identifier
	// character — so a doc opening with `_handler` or `trailing_` trimmed
	// to a word no declaration introduces, every arm fell through, and
	// the theft went unreported. That is a false NEGATIVE, which the
	// "cannot invent a match" argument above does not cover: that is a
	// claim about false positives. Raised in review of #503.
	const decoration = "`\"'“”‘’()[]*,.:;!?"
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
	// THE FILESET COMES BACK WITH THE FILE. It was created here and
	// discarded, and the arm below then built a SECOND, empty one and
	// handed it to stolenComments alongside a file the first had parsed
	// — so FileSet.PositionFor answered the zero Position for every Pos,
	// every message opened with a bare "-:" instead of a location, and
	// confirmHint was handed an empty filename. Nothing went red because
	// the assertion counts findings; the cost lands on the day that arm
	// FAILS and prints the positionless form, in the file whose thesis
	// is that a guard's message has to be right. Raised in review of
	// #503.
	parse := func(t *testing.T, src string) (*gotoken.FileSet, *ast.File) {
		t.Helper()
		fset := gotoken.NewFileSet()
		f, err := goparser.ParseFile(fset, "fixture.go", src, goparser.ParseComments)
		if err != nil {
			t.Fatalf("parsing the fixture: %v", err)
		}
		return fset, f
	}

	_, gen := parse(t, "// Code generated by protoc-gen-go. DO NOT EDIT.\n\n"+
		"package fake\n\ntype ValueKind int32\n\n"+theft)
	if !ast.IsGenerated(gen) {
		t.Error("the marker protoc writes does not read as generated, so the walk " +
			"would report seven correct comments in types.pb.go as theft")
	}
	fset, hand := parse(t, "package fake\n\ntype ValueKind int32\n\n"+theft)
	if ast.IsGenerated(hand) {
		t.Fatal("a file with no marker reads as generated, which would take " +
			"hand-written code out of the rule")
	}
	// hand AND ITS OWN fset — the same file, not a third parse of the
	// same bytes. Re-parsing was only ever necessary because the FileSet
	// was thrown away.
	if got := stolenComments(fset, hand, "fake"); len(got) != 1 {
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
	// "./" prefix, so a plain prefix test answers false for a tree whose
	// every file is in a subdirectory — which is every tree except this
	// one, where a .go file happens to sit in the repo root.
	mods := []string{".", "apps/gitui", "mcp"}
	if got := owningModule("input", mods); got != "." {
		t.Errorf("a file parsed under input/ is attributed to %q, not the root "+
			"module, so the floor means \"a .go file sits in the repo root\" rather "+
			"than what owningModule's comment says", got)
	}
	// AND IT IS NOT SATISFIED BY SOMEBODY ELSE'S CODE. This is the arm
	// the first version could not have: under "anywhere beneath", every
	// nested module's files covered "." as well, so the root module —
	// the one this guard lives in — had no independent entry at all.
	if got := owningModule("apps/gitui", mods); got == "." {
		t.Error("a file in the apps/gitui module is attributed to the root module, " +
			"so the root module's floor entry is satisfied by code it does not contain")
	}
	if got := owningModule("mcp/cmd/server", mods); got != "mcp" {
		t.Errorf("a file parsed under mcp/cmd/server is attributed to %q rather than "+
			"the mcp module, so a module whose own directory holds no .go file is "+
			"not covered after all", got)
	}
	if got := owningModule("mcpx", mods); got == "mcp" {
		t.Error("a sibling directory whose name merely starts with the module's " +
			"counts as covering it")
	}
	// A ONE-CHARACTER MODULE DIRECTORY, which is where "shortest" stops
	// being a figure of speech. "." is spelled with one character and
	// claims none of the path, so measuring it with len made it TIE with
	// a top-level module named `z` — and the root, sorting first, took
	// the file. This tree has no such module, which is why the tie could
	// sit here unnoticed; the fixture supplies one. Raised in review of
	// #503.
	short := []string{".", "z"}
	if got := owningModule("z", short); got != "z" {
		t.Errorf("a file in the single-character module z/ is attributed to %q; "+
			"the root module's directory is one character long too, so a length "+
			"comparison cannot tell the shortest prefix from the shortest name", got)
	}
	if got := owningModule("z/cmd/tool", short); got != "z" {
		t.Errorf("a file under z/cmd/tool is attributed to %q rather than the z "+
			"module", got)
	}
	// AND THE ROOT STILL WINS WHEN NOTHING ELSE CLAIMS THE FILE — the
	// half a prefixLen that simply returns 0 would break, since owner's
	// zero value measures 0 as well.
	if got := owningModule("input", short); got != "." {
		t.Errorf("a file under input/ is attributed to %q rather than the root "+
			"module, so the fallback stopped being reachable", got)
	}
	// NESTED MODULES, which this tree does not have today. The floor's
	// stated virtue is that a module added tomorrow is covered without
	// anyone editing this test, and a module added tomorrow INSIDE an
	// existing one is the case the prefix version got wrong.
	nested := []string{".", "apps/gitui", "apps/gitui/plugin"}
	if got := owningModule("apps/gitui/plugin/cmd", nested); got != "apps/gitui/plugin" {
		t.Errorf("a file in a module nested inside another is attributed to %q, so "+
			"the parent's floor entry is satisfied by its child's files", got)
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
