package gooey

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
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
// the comment names X, and X is either DIRECTLY BELOW it or later in
// the file and BARE.
//
// THE SECOND HALF IS NOT THE ORIGINAL RULE. This said "X is the very
// next declaration", and "nothing legitimate has that shape, because a
// comment about the thing below it would be that thing's comment",
// until review of #503 round 13 — by which point three of the four arms
// fired at other distances: laterUndocumented at any distance, and the
// two block arms on any later entry of the block. A reader who took the
// header literally would not expect the distance arm to exist, in the
// guard's first paragraph, in the file whose subject is a comment that
// stopped describing what sits beneath it, with nothing able to go red
// over it. What replaced distance is documented-ness: an honest cross
// reference names something that is itself documented, and a stolen
// comment names something left bare, because the theft is what took its
// doc away. The generated-code note below is the OTHER correction to
// "nothing legitimate has that shape".
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
	//
	// AND THE POSITIONS ARE PINNED NOW, not just described. The paragraph
	// above names both mistakes and calls one of them worse; neither had
	// a counterfactual, because the only thing driving this bookkeeping
	// was the clean tree, where both markings agree. Measured: adding
	// `ruled[moduleOwningDir(…)] = true` beside the reached line — the exact
	// "worse" state — left the guard tests green. The marking moved into
	// coverage() so a fixture can drive it, the same extraction
	// moduleFloorFaults got one level down and for the same reason.
	// Raised in review of #503.
	paths, modules := treeWalk(t)
	reached, ruled := map[string]bool{}, map[string]bool{}
	for _, path := range paths {
		fset := gotoken.NewFileSet()
		f, err := goparser.ParseFile(fset, path, nil, goparser.ParseComments)
		ok := err == nil && !ast.IsGenerated(f)
		coverInto(reached, ruled, filepath.Dir(path), modules, ok)
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
		// says so — and this one is DERIVED a few hundred lines down, by
		// the t.Logf at the end of the walk. `go test -v` is the reader's
		// source, and naming the flag is the whole correction: t.Log on a
		// PASSING test prints nothing, so a plain `go test ./...` — which
		// is what CLAUDE.md's Verify section runs — sends the reader this
		// paragraph redirects to a channel that is silent exactly when
		// the guard is green. Raised in review of #503, corrected in the
		// round after.
		if ast.IsGenerated(f) {
			continue
		}
		files++
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
	for _, s := range moduleFloorFaults(reached, ruled, modules) {
		t.Error(s)
	}
	// BEHIND !t.Failed(), the way TestTheRemediationGrepAgreesWithTheRule
	// is and for the sharper reason. The floor errors immediately above
	// say a module contributed NOTHING — the corpus is short by an
	// unknown amount — and this line then prints a population figure as
	// though it were the tree's. That figure is also what the PR body
	// sends readers to as the authoritative count, so the misread is the
	// intended reading path. Raised in review of #503.
	if !t.Failed() {
		t.Logf("examined %d doc comments across %d files", examined, files)
	}
}

// generatedMarkerPattern is the ERE the !ruled arm below hands the
// reader, written here rather than in the message so that the
// instruction and the check that runs it cannot drift.
//
// THE ANCHORS ARE THE INSTRUCTION. The walk decides generated-ness with
// ast.IsGenerated, which wants the marker on a line of its own before
// the package clause; an unanchored `grep -rL "Code generated .* DO NOT
// EDIT"` excludes a file holding that text anywhere at all, including
// quoted inside a string. This file carries such a string, so the
// unanchored version dropped the most hand-written file in the tree from
// a list defined as "files that are NOT generated" — and it was the
// reader's only next step. TestTheRemediationGrepAgreesWithTheRule is
// what keeps the two answers identical over the real tree. Raised in
// review of #503.
//
// THE TRAILING CARRIAGE RETURN IS NOT COSMETIC, and the two answers
// disagree without an allowance for it. Go's (?m)$ matches before \n
// only, while go/scanner strips a trailing \r from a // comment before
// ast.IsGenerated compares it. Measured on
// "// Code generated by protoc-gen-go. DO NOT EDIT.\r\n\r\npackage fake\r\n":
// IsGenerated is true while an unallowanced `…DO NOT EDIT\.$` is false.
//
// So on a clone with core.autocrlf=true, the test below reddens on every
// .pb.go at once and sends the reader looking for a hand-written marker
// that is not there — and the floor's own `grep -rLE` advice is wrong in
// the same run, which is the arm this const exists to keep honest.
//
// IT IS SPELLED FOR TWO ENGINES, and \r? is only valid in one of them.
// This const has two consumers: TestTheRemediationGrepAgreesWithTheRule
// compiles it with Go's regexp, and moduleFloorFaults renders it
// VERBATIM into a `grep -rLE` command for a human to paste. POSIX ERE
// has no C escapes, so GNU grep reads \r as a literal `r`. The previous
// spelling therefore claimed "the printed command is unchanged in
// meaning" and was wrong in both directions at once. Measured with GNU
// grep 3.11 against a CRLF file, an LF file, and one ending "EDIT.r":
//
//	pattern                CRLF      LF       EDIT.r
//	…EDIT\.\r?$            no match  match    MATCH
//	…EDIT\.[[:space:]]*$   match     match    no match
//
// So `grep -rLE` with \r? listed a CRLF-checked-out .pb.go as NOT
// generated — the exact failure the anchors above were added to fix,
// reintroduced by the fix for the CRLF one. A character class is valid
// in both dialects and means the same thing in both.
//
// THE A/B HAS TO NAME ITS grep. The default `grep` on at least one
// machine in this project is ugrep, which DOES honour \r — under it both
// spellings pass and the defect is invisible. Run it against /bin/grep.
// Raised in review of #503.
const generatedMarkerPattern = `^// Code generated .* DO NOT EDIT\.[[:space:]]*$`

// moduleFloorFaults is the floor itself, and it is a function so that a
// fixture can drive it.
//
// It was a switch inline in the test above, which made the floor — the
// thing that distinguishes this guard from the markup/-scoped version
// #483 was filed about — the one part of this file nothing measured.
// Measured: replacing both arms with `case false && …` left the whole
// root suite GREEN, with no output. Its two siblings pin the inputs
// (TestTheGuardsModuleFloorMatchesTheTreesOwnDiscovery pins that treeWalk
// finds the same modules as discoverModules,
// TestTheGuardsDerivedFloorAndItsHintMeanWhatTheySay pins moduleOwningDir's
// attribution) and neither reached the loop that turns them into a
// failure. This is the same extraction the file already applies to
// stolenComments, for the same reason. Raised in review of #503.
//
// THE TWO MESSAGES ARE THE RETURN VALUE, not a bool, because they are
// distinguishable only by wording: "the walk never got here" and "the
// walk got here and the rule declined every file" are different faults
// with different next steps, and the comment on the two maps above names
// marking them on the wrong side of the skips as a live way to report one
// in the other's words. A fixture asserting WHICH message comes back is
// what can see that; a count cannot.
func moduleFloorFaults(reached, ruled map[string]bool, modules []string) []string {
	var faults []string
	// A FILE NO MODULE CLAIMS, checked ahead of the loop because the
	// loop structurally cannot see it: "" is not a module of this tree,
	// so it appears in no entry of `modules` and the coverage recorded
	// against it is iterated by nothing. moduleOwningDir answers "" only
	// when the module set holds no "." — the root module is the fallback
	// owner for every file no nested module contains — which is the
	// shape a walk that yielded the nested go.mod files and not the
	// root's produces. Measured before this arm existed: with that walk,
	// the whole root suite stayed green while the root module, the one
	// holding this guard, left the derived floor without a word. Raised
	// in review of #503.
	if reached[""] {
		faults = append(faults, "the walk parsed a .go file that no module in this "+
			"tree claims, so its coverage is recorded under the empty module name, "+
			"which no entry below iterates: the module set is missing the root "+
			"module, whose directory \".\" is the fallback owner for every file no "+
			"nested module contains")
	}
	for _, mod := range modules {
		switch {
		case !reached[mod]:
			faults = append(faults, fmt.Sprintf("the walk yielded no .go file under %[1]q, "+
				"which is a module of this tree. TWO CAUSES, and this arm cannot tell "+
				"them apart: a prune ate the module's files, in which case a guard that "+
				"stops at a module boundary is reporting green for code it never read; "+
				"or the module genuinely holds no Go. `find %[1]s -name '*.go'` "+
				"separates them — files listed means the prune, nothing listed means the "+
				"second, and the second is a real answer rather than a fault to fix. "+
				"Reachable today: tools/ holds one .go file, doc.go, whose whole purpose "+
				"is prose, so renaming it puts this module in the second case with no "+
				"prune anywhere. This arm asserted the first until review of #503, and "+
				"the reader who trusted it went looking through treeWalk for a prune "+
				"that was not there", mod))
		case !ruled[mod]:
			faults = append(faults, fmt.Sprintf("every .go file under %[1]q was skipped — it "+
				"did not parse, or it is generated — so this guard read the module and "+
				"ruled on none of it. That is not the prune the case above is about, and "+
				"it is not coverage either. Check which: `go vet ./...` in that module "+
				"answers the parse half, and `grep -rLE '%s' --include='*.go' %s` names "+
				"any file that is NOT generated and was therefore skipped for the other "+
				"reason. The anchors in that pattern are the instruction and not "+
				"decoration: the walk asks ast.IsGenerated, which wants the marker on a "+
				"line of its own before the package clause, and an unanchored pattern "+
				"also matches one quoted inside a string. Two caveats the command cannot "+
				"carry: for the root module that path is \".\", which sweeps every "+
				"nested module and vendor/ as well, and the walk prunes both. There is "+
				"deliberately no way to "+
				"mark a module exempt: an all-generated module is a real answer, and it "+
				"is one a reader should have to give rather than a flag that outlives "+
				"the first hand-written file added to it", mod, generatedMarkerPattern, mod))
		}
	}
	return faults
}

// coverInto records one file against the two coverage maps: reached
// whenever the walk yielded it, ruled only when the rule applies to it.
//
// A FUNCTION FOR THE SAME REASON moduleFloorFaults is one. The two
// markings are one line apart in the caller and one word apart in
// effect, and the positions carry an argument: reached BEFORE the parse
// and generated skips, so a module whose every file is skipped reports
// as read-and-not-ruled; ruled AFTER them, because marking it before
// makes a module of nothing but .pb.go read as covered. That second
// state is the one the caller's comment calls worse, and it was
// reachable in silence — the only thing exercising these lines was the
// clean tree, where a module always has at least one ordinary file and
// so both maps agree whatever the order.
//
// A BOOLEAN RATHER THAN AN *ast.File, which is what makes the fixture
// possible at all. Handing this a file would put the parse and
// ast.IsGenerated inside it and leave a fixture needing real bytes on
// disk; planting them under testdata/ is worse than it looks, because
// testdata is deliberately unpruned here, so a go.mod there would be
// picked up by BOTH treeWalk and discoverModules and redden
// TestTheGuardsModuleFloorMatchesTheTreesOwnDiscovery for an unrelated
// reason. The caller keeps the parse; this keeps the order.
//
// IT TOOK TWO, AND THE SECOND COULD NOT FAIL. The caller computes
// `applies` as `err == nil && !ast.IsGenerated(f)`, so `applies` already
// implies `parsed` and `if parsed && applies` was `if applies` written
// twice. Measured: replacing it with `if applies` left all seven guard
// tests green, and the fixture table agreed from the other side — the
// generated-only row and the does-not-parse row were the SAME call with
// the same expectations, and no row could have been `{false, true}`.
// A guard nothing can fail is a claim about a caller that does not
// exist, so it is gone rather than fixtured. The two fixture rows stay:
// they are one input to THIS function and two causes at the call site,
// and the floor's message names both. Raised in review of #503.
func coverInto(reached, ruled map[string]bool, dir string, modules []string, applies bool) {
	mod := moduleOwningDir(dir, modules)
	reached[mod] = true
	if applies {
		ruled[mod] = true
	}
}

// TestTheCoverageMarkingsHaveTheOrderTheirCommentClaims is the
// counterfactual for coverInto, and the generated-only module is the arm
// that matters.
//
// A module contributing one generated file is READ and RULED ON BY
// NOTHING, so the floor must report it — that is the whole of the
// "worse" mistake the caller's comment names, and before this arm
// existed, marking ruled beside reached left the suite green. The
// parse-failure arm is its sibling: same answer, different cause, and
// the floor's !ruled message names both so a reader can tell which.
//
// THE CLEAN ARM IS NOT DECORATION either: without it the two arms above
// pass for a coverInto that never marks ruled at all, which is the
// mirror mistake and would make every module in the tree report as
// unruled. Raised in review of #503.
func TestTheCoverageMarkingsHaveTheOrderTheirCommentClaims(t *testing.T) {
	const mod = "m"
	mods := []string{".", mod}
	for _, tc := range []struct {
		name             string
		applies          bool
		wantR, wantRuled bool
	}{
		{"an ordinary file", true, true, true},
		// SAME INPUT, TWO CAUSES, and that is the point of keeping both
		// rows after the parsed parameter went: at the call site these
		// are a generated file and an unparseable one, the floor's
		// !ruled message names both so a reader can tell which, and the
		// answer coverInto has to give is the same for either.
		{"a module of nothing but generated code", false, true, false},
		{"a file that does not parse", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reached, ruled := map[string]bool{}, map[string]bool{}
			coverInto(reached, ruled, mod, mods, tc.applies)
			if reached[mod] != tc.wantR {
				t.Errorf("reached[%q] = %v, want %v: the walk yielded the file, so "+
					"the module is reached whatever the rule then does with it — "+
					"marking this after the skips would report a skipped module as "+
					"one a prune ate", mod, reached[mod], tc.wantR)
			}
			if ruled[mod] != tc.wantRuled {
				t.Errorf("ruled[%q] = %v, want %v: ruled means the rule was APPLIED, "+
					"and marking it before the parse and generated skips makes a "+
					"module of nothing but .pb.go read as covered — which is the "+
					"mistake coverInto's comment calls the worse of the two",
					mod, ruled[mod], tc.wantRuled)
			}
		})
	}
}

// TestTheModuleFloorReportsWhichFaultItFound is the counterfactual the
// floor did not have. Each case asserts the message, not the count: the
// two arms are one word apart in effect and identical in shape, so a
// test that only counted faults would pass with them swapped.
func TestTheModuleFloorReportsWhichFaultItFound(t *testing.T) {
	const (
		unreached = "yielded no .go file"
		unruled   = "was skipped"
		unowned   = "no module in this"
		// BOTH CAUSES, which is the half the unreached arm did not say.
		// Identification and diagnosis are different claims and the
		// arm can pass the first while failing the second, so they are
		// asserted separately rather than as one phrase. Raised in
		// review of #503.
		twoCauses = "the module genuinely holds no Go"
		separator = "-name '*.go'"
		// AND THE SAME TREATMENT FOR THE OTHER ARM, which round 6's fix
		// was not applied to. Its want was the single phrase `unruled`,
		// and TestTheRemediationGrepAgreesWithTheRule reads
		// generatedMarkerPattern DIRECTLY rather than through this
		// message — so the remedy could leave the arm with nothing going
		// red. Measured: replacing the clause naming `go vet ./...` with
		// placeholder text, format args untouched so vet's printf check
		// cannot see it either, left the whole root suite green. That is
		// the parse half of the reader's next step deleted in silence,
		// in the arm whose stated purpose is to separate two causes.
		// Raised in review of #503.
		parseHalf = "go vet ./..."
		// THE FLAG AND THE PATTERN ARE TWO CLAIMS, and pinning the flag
		// alone left the pattern free. generatedMarkerPattern exists so
		// that "the instruction and the check that runs it cannot
		// drift" — TestTheRemediationGrepAgreesWithTheRule reads the
		// CONST, and this arm asserted only `grep -rLE`, the literal
		// flag. Measured: handing the %s the unanchored
		// `Code generated .* DO NOT EDIT` — the exact pattern round 5
		// fixed, which dropped this file, the most hand-written in the
		// tree, from a list defined as files that are not generated —
		// left all seven guard tests green. So the reader of a real
		// !ruled fault would be handed back the command already
		// established to point past the file they are looking for.
		// Asserting the const itself is what closes it. Raised in
		// review of #503.
		markerHalf  = "grep -rLE"
		markerRule  = generatedMarkerPattern
		noExemption = "no way to mark a module exempt"
	)
	for _, tc := range []struct {
		name           string
		reached, ruled map[string]bool
		modules        []string
		// want is EVERY phrase the fault must carry. Empty means no
		// fault at all.
		want []string
	}{
		{
			name:    "a module the walk never reached",
			reached: map[string]bool{".": true},
			ruled:   map[string]bool{".": true},
			modules: []string{".", "mcp"},
			want:    []string{unreached, twoCauses, separator},
		},
		{
			name:    "a module reached but never ruled on",
			reached: map[string]bool{".": true, "mcp": true},
			ruled:   map[string]bool{".": true},
			modules: []string{".", "mcp"},
			want:    []string{unruled, parseHalf, markerHalf, markerRule, noExemption},
		},
		{
			// A SET WITH NO ROOT MODULE IN IT, which is the only way
			// moduleOwningDir answers "". The two arms below iterate
			// `modules`, and "" is in no module set, so this fault has
			// to be found before the loop or not at all.
			name:    "a file no module in the set claims",
			reached: map[string]bool{"": true, "apps/gitui": true, "mcp": true},
			ruled:   map[string]bool{"": true, "apps/gitui": true, "mcp": true},
			modules: []string{"apps/gitui", "mcp"},
			want:    []string{unowned},
		},
		{
			name:    "a module covered both ways",
			reached: map[string]bool{".": true, "mcp": true},
			ruled:   map[string]bool{".": true, "mcp": true},
			modules: []string{".", "mcp"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := moduleFloorFaults(tc.reached, tc.ruled, tc.modules)
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("a fully covered set reported %v, so the floor fires on "+
						"modules it has no complaint about", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("reported %d faults, want exactly 1: %v", len(got), got)
			}
			for _, want := range tc.want {
				if !strings.Contains(got[0], want) {
					t.Errorf("reported %q, want the %q wording — the arms are "+
						"distinguishable only by what they say, so the wrong one sends "+
						"the reader to look for a prune that is not there, or past one "+
						"that is, and an arm that names one of two causes sends them "+
						"looking for the other", got[0], want)
				}
			}
		})
	}
}

// TestTheRemediationGrepAgreesWithTheRule runs the !ruled arm's advice
// instead of reading it.
//
// An error message's remedy is a behavioural claim, and this one is the
// reader's whole next step: the arm says the command names the files
// that are NOT generated, and "generated" is whatever ast.IsGenerated
// says. Nothing compared the two until this test, and the version before
// it disagreed — on doccomment_test.go, the most hand-written file in
// the tree, which carries the marker inside a string literal. The
// comparison is over the real tree rather than a fixture because the
// disagreement was a property of a file that exists, not of a shape
// somebody imagined. Raised in review of #503.
func TestTheRemediationGrepAgreesWithTheRule(t *testing.T) {
	paths, _ := treeWalk(t)
	// (?m) is what makes ^ and $ the line anchors grep -E gives them;
	// without it they anchor the whole file and the pattern matches only
	// a one-line file.
	re := regexp.MustCompile("(?m)" + generatedMarkerPattern)
	// compared, NOT len(paths). paths is every walked .go file and the
	// loop continues past one that does not parse, so len(paths) is the
	// population the walk OFFERED rather than the one this comparison
	// covered. They are equal today only because all of them parse; the
	// walk deliberately includes testdata, so one broken fixture makes
	// the denominator overstate what was checked — in a floor whose
	// whole job is to say how much was. Raised in review of #503.
	generated, compared := 0, 0
	for _, path := range paths {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		f, err := goparser.ParseFile(gotoken.NewFileSet(), path, src, goparser.ParseComments)
		if err != nil {
			continue // as the walk does: a file that does not parse is not this rule's business
		}
		compared++
		if rule, grep := ast.IsGenerated(f), re.Match(src); rule != grep {
			t.Errorf("%s: ast.IsGenerated says %v and the arm's grep says %v, so the "+
				"remediation this floor prints names a different set of files than "+
				"the walk skipped, and the reader is sent past the file they are "+
				"looking for.\n\nTWO RESOLUTIONS, and the pattern is not always the "+
				"one to change. The grep matches the marker at the start of ANY line; "+
				"ast.IsGenerated accepts it only BEFORE the package clause, and no "+
				"anchored pattern can express that — so a hand-written file carrying "+
				"the marker at column 0 after the package clause (a raw string literal "+
				"is the way it happens, and this file's own generated fixture is one "+
				"edit from being written that way) diverges legitimately. If %[1]s is "+
				"such a file, move the marker off column 0 there rather than widening "+
				"the approximation this floor is built on. Raised in review of #503",
				path, rule, grep)
		} else if rule {
			generated++
		}
	}
	// A FLOOR, because two empty sets agree with each other. A tree with
	// no generated file in it makes this test pass against any pattern
	// at all, including one that matches nothing.
	if generated == 0 {
		t.Fatalf("the walk found no generated file among the %d that parsed, so "+
			"this comparison holds between two empty sets and would pass against "+
			"any pattern", compared)
	}
	// GUARDED, because the sentence is a claim about the run it is in:
	// a t.Logf after t.Errorf prints alongside the failure it contradicts.
	if !t.Failed() {
		t.Logf("%d of %d parsed files are generated, and the arm's grep agrees on "+
			"every one (`go test -v` prints this; a passing `go test` does not)",
			generated, compared)
	}
}

// walkedTree is treeWalk's WALK, memoized — the assertions stay in
// treeWalk so that every caller still reddens on them.
//
// Three tests call treeWalk and each one walked the tree again. The
// precedent is one file over and makes the same case in the same words:
// declaredIndex and vendoredIndex in claudemd_test.go are
// sync.OnceValues because "two tests ask for each of these indexes and
// each walk parses the whole tree … The memo changes nothing either
// test asserts: the walk is over files on disk, which no test here
// writes." That is true of this walk verbatim.
//
// THE SPLIT IS WHERE IT IS FOR A REASON. Memoizing treeWalk whole would
// run its sanity assertions once, under whichever test happened to ask
// first — so a narrowed prune would redden one caller instead of three,
// and the mutation matrix written above those assertions (three rows of
// "3") would quietly become rows of "1". Only the WalkDir is memoized;
// the assertions are scans over the returned slices, which cost
// nothing, and they run per caller as before. Raised in review of #503.
var walkedTree = sync.OnceValues(func() (walkResult, error) {
	var w walkResult
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
			w.files = append(w.files, path)
		case d.Name() == "go.mod":
			w.moduleDirs = append(w.moduleDirs, filepath.Dir(path))
		}
		return nil
	})
	return w, err
})

// walkResult is what the memo holds. A struct because sync.OnceValues
// carries two values and one of them has to be the error.
type walkResult struct{ files, moduleDirs []string }

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
	w, err := walkedTree()
	if err != nil {
		t.Fatalf("walking the tree: %v", err)
	}
	files, moduleDirs = w.files, w.moduleDirs
	if len(moduleDirs) == 0 {
		t.Fatal("found no go.mod at all, not even the root module's: the module floor " +
			"would pass vacuously")
	}
	// AND THE ROOT MODULE BY NAME, because the check above fires on a
	// shape no prune of this tree produces. The reachable mistake is a
	// walk that yields the nested go.mod files and not the root's —
	// `path != "go.mod"`, the one edit a reader unifying this with
	// discoverModules would make — and nothing else notices it:
	// discoverModules excludes the root deliberately, and
	// TestTheGuardsModuleFloorMatchesTheTreesOwnDiscovery filters "."
	// out of its own side to match, so the root module's membership in
	// the floor was asserted by no test at all. Measured with that edit
	// and without this fatal: the whole root suite green, and the module
	// holding this guard out of the derived floor in silence. Raised in
	// review of #503.
	rooted := false
	for _, m := range moduleDirs {
		if m == "." {
			rooted = true
			break
		}
	}
	if !rooted {
		t.Fatalf("the walk found %d go.mod files and none of them is the root "+
			"module's: moduleOwningDir's fallback is the root module, so every file no "+
			"nested module claims would attribute to no module at all, and the floor "+
			"iterates the module set — the root module would leave it in silence",
			len(moduleDirs))
	}
	// AND THE PRUNE POLICY, POSITIVELY, because the floor above cannot
	// see it. The floor is per-module BOOLEAN reachability — a module
	// faults only when it loses its LAST file — so any narrowing of this
	// walk that leaves one file per module standing is invisible to it,
	// and a narrowing of the FILE filter is a strictly easier edit than
	// a prune of a module directory. Measured on this branch, each with
	// the whole root suite green and no output at all:
	//
	//	&& !strings.HasSuffix(path, "_test.go") on the .go case
	//	    613 files / 4667 doc comments  →  285 / 2422
	//	|| name == "testdata" on the prune above
	//	    613 / 4667  →  609 / 4656
	//
	// The first is the one that matters: it removes the population this
	// guard's own rationale is built on — nobody runs `go doc` on a test
	// file, so a stolen doc there is found by reading only — and where
	// most of its repairs have landed. The second contradicts the prune
	// comment above in the same function, which states that testdata is
	// deliberately not pruned.
	//
	// STATEMENTS OF THAT COMMENT'S OWN POLICY, not a count. A count is
	// the thing this file refuses twice elsewhere, and it would fail on
	// the next file anyone adds; these fail only when the policy changes,
	// which is when someone should be reading this. The residual is the
	// same one the module floor has and is named on
	// TestTheGuardsModuleFloorMatchesTheTreesOwnDiscovery: a narrowing
	// applied to the policy and to these assertions together.
	//
	// THREE OF THE FOUR DISCRIMINATE AND THE FOURTH IS BELT-AND-BRACES,
	// and saying which is which is the point of writing the matrix down.
	// treeWalk is shared by three tests, so a fatal here reddens all
	// three:
	//
	//	mutation to this function              failing tests
	//	  none                                   0
	//	  drop _test.go from the .go case        3
	//	  prune testdata                         3
	//	  stop pruning vendor                    3
	//	  stop pruning dot-directories           0
	//
	// The last row is the honest one: the dot-directory assertion below
	// cannot fire in a fresh clone or in CI, because both offenders it
	// is written for are UNTRACKED — .claude/worktrees/ holds whole
	// checkouts of this repo, apps/kanban/worker/.venv appears once
	// somebody runs that example — and this checkout has neither with Go
	// in it. It is kept as the statement of a policy whose violation is
	// silent on the machines that have those directories, not as a
	// discriminating assertion; treating it as one would be the
	// over-crediting this file's subject is about. Raised in review of
	// #503.
	var tests, fixtures, vendored, hidden string
	for _, f := range files {
		s := filepath.ToSlash(f)
		if strings.HasSuffix(s, "_test.go") && tests == "" {
			tests = s
		}
		// EVERY ONE OF THESE HANDLES THE ROOT POSITION, because a path
		// test written only for the nested case is the shape two of
		// these had. `strings.Contains(s, "/testdata/")` alone cannot
		// see a root-level testdata/, and every one of the tree's
		// testdata Go files is nested today — so the day one moves,
		// the fatal below fires saying testdata "had nothing behind
		// it", which is the opposite of what happened. The vendor test
		// beside it got the root case right, which is what makes the
		// asymmetry visible in one loop. Raised in review of #503.
		if fixtures == "" && (strings.HasPrefix(s, "testdata/") ||
			strings.Contains(s, "/testdata/")) {
			fixtures = s
		}
		if vendored == "" && (strings.HasPrefix(s, "vendor/") ||
			strings.Contains(s, "/vendor/")) {
			vendored = s
		}
		// THE DIRECTORY PART ONLY, and this is the same root-position
		// mistake in its other form. The prune skips DIRECTORIES
		// (d.IsDir() above); scanning every segment including the
		// basename made a dot-prefixed FILE fire it. `.#name.go` is
		// Emacs' lock-file spelling and this repo routinely has five to
		// fifteen agents live, any of which can leave one at the root —
		// measured, one such file reddened all three treeWalk callers
		// with a message blaming two directories that are not involved.
		// It failed CLOSED, which is the safe direction; the defect was
		// the message, in the file whose whole subject is a guard whose
		// message is wrong. filepath.Dir(".#scratch.go") is ".", one
		// character, so the len > 1 test stops there. Raised in review
		// of #503.
		if hidden == "" {
			for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(s)), "/") {
				if len(seg) > 1 && seg[0] == '.' {
					hidden = s
					break
				}
			}
		}
	}
	if tests == "" {
		t.Fatalf("the walk found %d .go files and not one of them is a _test.go: "+
			"the file filter has been narrowed to non-test sources, which is the "+
			"population this guard exists for — a doc comment in a test file is "+
			"reached by reading and by nothing else — and the module floor cannot "+
			"see it, because every module still contributes its non-test files",
			len(files))
	}
	if fixtures == "" {
		// TWO CAUSES, AND THIS ARM CANNOT TELL THEM APART — the same
		// correction the !reached arm took. Either the prune above was
		// narrowed, or the tree's last testdata Go file legitimately
		// went away, and the second is the likelier of the two: the
		// population is small and lives in a couple of packages, so
		// deleting one fixture package can empty it. A count of them
		// is not written here for the reason this file refuses counts
		// twice elsewhere — it would be a sample, wrong on the next
		// fixture anyone adds or removes.
		t.Fatalf("the walk found %d .go files and not one of them is under a "+
			"testdata/ directory. Either the prune above was narrowed — it "+
			"states that testdata is deliberately NOT pruned, because a fixture "+
			"is still a file someone reads, and pruning it leaves every module "+
			"contributing so the floor stays silent — or the tree no longer has "+
			"a testdata Go file to find. Run "+
			"`find . -path '*/testdata/*' -name '*.go' -not -path './vendor/*'` "+
			"to separate them: output means the prune changed, no output means "+
			"the corpus did", len(files))
	}
	if vendored != "" {
		t.Fatalf("the walk yielded %q: vendor/ is other people's code and the prune "+
			"above says so, and a theft in it is not this tree's to fix", vendored)
	}
	if hidden != "" {
		t.Fatalf("the walk yielded %q, which has a dot-directory in its path: the "+
			"prune above is depth-wide for two untracked offenders that exist on "+
			"working machines and in no fresh clone (.claude/worktrees/ holds whole "+
			"checkouts of this repo, apps/kanban/worker/.venv vendors Go of its "+
			"own), so this fails on a developer's tree and passes in CI either way",
			hidden)
	}
	return files, moduleDirs
}

// TestTheGuardsModuleFloorMatchesTheTreesOwnDiscovery is what makes the
// floor's REACH checkable rather than self-certified.
//
// treeWalk is a third implementation of "where are the modules" beside
// CLAUDE.md's verify loop and ci.yml's matrix, which are pinned to each
// other character-for-character by
// TestCIWorkflowAndCLAUDEMDShareOneDiscovery. The sets agree today.
//
// THE RESIDUAL IS NARROWER THAN THIS COMMENT SAID, and the difference is
// whether this test is worth keeping. It said a prune added to treeWalk
// takes the modules beneath it out of both halves and nothing goes red.
// That is false, and this test is what falsifies it: discoverModules
// (claudemd_test.go) is an INDEPENDENT walk that prunes dot-directories
// only, so a prune here alone makes the two sets disagree. Measured, by
// adding `|| name == "packs"` to the prune above:
//
//	the canonical discovery finds packs/temporal-batch and this
//	guard's walk does not …
//
// eight times, one per pack — while TestNoDocCommentNamesTheDeclaration-
// BelowIt stayed GREEN, which is the half the old sentence had right:
// the pruned modules drop out of `modules` too, so the floor itself has
// nothing to complain about. That is exactly why this test exists.
//
// NO LINE NUMBER IN THAT TRANSCRIPT, for the reason this file gives
// twice about counts. A pasted `file:line` is true when it is measured
// and wrong the moment anything above it grows — and what grew here was
// the comment doing the quoting, so it came to cite itself. That is this
// file's own subject one level up, and nothing checks line numbers
// inside Go comments the way TestEveryCitedTestNameResolves checks them
// in CLAUDE.md. The message text identifies the assertion and cannot go
// stale. Raised in review of #503.
//
// The real residual is one word away: a prune added to BOTH walks
// shrinks the two sets together, and then nothing goes red. Writing the
// weaker claim was the defect this whole file is about — a comment
// saying something the code does not — pointed at the code beneath it,
// and it is how a test gets deleted as redundant. Raised in review of
// #503, corrected in the round after.
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

// moduleOwningDir is the module a parsed file belongs to: the longest
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
// NAMED moduleOwningDir, NOT owningModule, because overlayonepass_test.go
// declares an owningModule of its own in this same package — a
// path-walking one that stats for go.mod rather than choosing the
// longest prefix from a list. Both landed from separate branches and
// the merge is where they met. This one is renamed because the other
// reached main first; the two answer different questions and neither
// should be folded into the other. Raised by the merge for review of
// #503.
func moduleOwningDir(dir string, moduleDirs []string) string {
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
		// wantExamined is how many doc comments docsExamined counts in
		// this fixture, asserted EXACTLY rather than against zero.
		//
		// The two walks share a bound — `j+1 < len(g.Specs)` — and the
		// comment on stolenComments' copy says they must move together.
		// Nothing made them: docsExamined was only ever compared with
		// zero, so widening its spec bound to `j >= 0` moved the
		// tree-wide population from 4665 to 4691 with the whole suite
		// green. An exact count per arm is what makes a bound changed on
		// one side alone redden. Raised in review of #503.
		wantExamined int
	}{
		{
			// THE TOP-LEVEL LOCATOR IS ASSERTED HERE AND NOWHERE ELSE.
			// wantMsg was set on the block arm and the spec arm only, so
			// the third report site's locator — "the declaration DIRECTLY
			// BELOW it" — was carried by no fixture. That is the exact
			// phrase declaresDirectlyBelow's own comment records as
			// having been FALSE: a doc naming the fifth const of the
			// block below it was announced as directly below, five
			// entries from where the name is. The behaviour is pinned
			// (reverting declaresDirectlyBelow to scan every spec reddens
			// "a later entry of the block below it is a cross
			// reference"); the WORDING was not, and two rounds of this
			// review have turned on a remediation sentence being wrong
			// while the behaviour was right. This is the arm a reader
			// reaches first, which is why it is the one that carries it.
			// Raised in review of #503.
			name:         "a function stolen from",
			wantExamined: 1,
			src: `// alpha does the alpha thing.
func beta() {}

func alpha() {}
`,
			want: "alpha",
			wantMsg: "the doc comment on beta opens by naming alpha, which is the " +
				"declaration DIRECTLY BELOW it. That is a doc comment that was " +
				"separated from what it documents — either beta was inserted " +
				"between it and alpha, or the blank line between two comment " +
				"groups was lost, and either way alpha is now undocumented.",
		},
		{
			// ADJACENT AND DOCUMENTED, which no fixture covered in
			// either direction. The finding is real — beta's comment
			// opens by naming something else, so beta is
			// mis-documented — but NOTHING WAS LOST, and the sentence
			// said "alpha is now undocumented" about a declaration
			// carrying its own doc comment. The counterfactual arm that
			// existed pinned the discriminator only at distance >= 2,
			// which is exactly the asymmetry. Raised in review of #503.
			name: "adjacent and documented is a wrong comment, not a lost one",
			// ONE, NOT TWO: docsExamined counts a documented
			// declaration only when something FOLLOWS it, and the
			// second one here is last.
			wantExamined: 1,
			src: `// alpha is the entry point; this one does the work.
func beta() {}

// alpha does the alpha thing.
func alpha() {}
`,
			want: "alpha",
			wantMsg: "the doc comment on beta opens by naming alpha, which is the " +
				"declaration DIRECTLY BELOW it. That is a doc comment that was " +
				"separated from what it documents — either beta was inserted " +
				"between it and alpha, or the blank line between two comment " +
				"groups was lost, and either way beta reads as a comment about alpha.",
		},
		{
			// A BLOCK WHOSE DOC IS ON ITS FIRST SPEC, which documented()
			// answers "undocumented" for because it reads the BLOCK's
			// Doc. The later-undocumented arm then reported a
			// declaration whose doc comment is on the line directly
			// above it. Fourteen blocks in this tree have this shape and
			// none of them fires today, which is why this fixture is the
			// only thing that can hold it. Raised in review of #503.
			name: "a var block documented on its first spec is documented",
			// ONE, NOT TWO: docsExamined counts a documented
			// declaration only when something FOLLOWS it, and the
			// second one here is last.
			wantExamined: 1,
			src: `// alpha is the alpha thing.
func beta() {}

func other() {}

var (
	// alpha is the alpha table.
	alpha = map[string]int{}
)
`,
		},
		{
			name:         "an honest comment that names itself",
			wantExamined: 1,
			src: `// alpha does the alpha thing.
func alpha() {}

func beta() {}
`,
		},
		{
			name:         "a comment naming something further down is not theft",
			wantExamined: 1,
			src: `// alpha calls gamma, eventually.
func alpha() {}

func beta() {}

func gamma() {}
`,
		},
		{
			// THE MERGED-GROUP SHAPE AT DISTANCE, which the adjacency
			// fence cannot see and which a lost blank line routinely
			// produces. gamma is TWO declarations down, so the i+1 arm
			// is silent; it is bare, which is what makes this theft and
			// not the cross reference directly above.
			name:         "a comment stolen from a declaration two below",
			wantExamined: 1,
			src: `// gamma does the gamma thing.
func beta() {}

type row struct{}

func gamma() {}
`,
			want: "gamma",
			wantMsg: "the doc comment on beta opens by naming gamma, which is a " +
				"LATER declaration in this file that has no doc comment of its " +
				"own. That is a doc comment that was separated from what it " +
				"documents — either beta was inserted between it and gamma, or " +
				"the blank line between two comment groups was lost, and either " +
				"way gamma is now undocumented.",
		},
		{
			// THE COUNTERFACTUAL FOR THE ARM ABOVE, and the whole reason
			// the discriminator is documented-ness rather than distance.
			// Identical shape, one difference: gamma has a doc of its
			// own. Nothing was taken from it, so the sentence above it
			// is a cross reference. Without this arm the new case would
			// be a distance rule wearing a different name, and the dozen
			// honest cross references this file measured would all be
			// faults.
			name: "naming a documented declaration further down is a cross reference",
			// ONE, NOT TWO: gamma's own doc is not in the population
			// because docsExamined counts a documented declaration only
			// when something follows it, and gamma is last. The arm
			// still discriminates — gamma being documented is read off
			// the declaration, not off the population count.
			wantExamined: 1,
			src: `// gamma is worth reading before this one.
func beta() {}

type row struct{}

// gamma does the gamma thing.
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
			name:         "a backticked first word is still a first word",
			wantExamined: 1,
			src:          "// `alpha` does the alpha thing.\nfunc beta() {}\n\nfunc alpha() {}\n",
			want:         "alpha",
		},
		{
			// A FIRST LINE THAT IS JUST THE NAME, which is the shape that
			// made opensBy split on whitespace rather than cut on " ":
			// the cut answered "alpha.\nThe" and every arm fell through.
			// It was live in markup/menuicon_test.go, and repairing that
			// file — the right fix for it — removed the tree's last
			// instance, so the corpus can no longer cover this and only a
			// fixture holds it. Measured: with the Fields call replaced
			// by a cut on a space, all seventeen other arms and the
			// whole-tree guard stay GREEN. Raised in review of #503.
			name:         "a first line with no space in it is still a first word",
			wantExamined: 1,
			src: `// alpha.
//
// The paragraph about it.
func beta() {}

func alpha() {}
`,
			want: "alpha",
		},
		{
			name:         "a possessive first word is still a first word",
			wantExamined: 1,
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
			name:         "a plural first word is not a possessive",
			wantExamined: 1,
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
			name:         "the first entry of the block directly below is theft",
			wantExamined: 1,
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
			name:         "a later entry of the block below it is a cross reference",
			wantExamined: 1,
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
			name:         "a const stolen from inside a block",
			wantExamined: 1,
			src: `const (
	// KindAlpha is the alpha kind.
	KindBeta = "beta"

	KindAlpha = "alpha"
)
`,
			want: "KindAlpha",
			// THE FOURTH LOCATOR, and the one no fixture carried. Three
			// of stolenComments' four report wordings are pinned by a
			// wantMsg; this arm asserted only that KindAlpha appears,
			// and its sibling two entries down pins the OTHER branch of
			// the same `if`. Measured: changing the condition to
			// `if false && specDeclares(…)` — so every spec-level
			// finding reports the non-adjacent wording — left all seven
			// guard tests green. That is the failure
			// declaresDirectlyBelow's own comment records for the
			// top-level arm, reproduced one scope down: a report that
			// says DIRECTLY BELOW when it is not, or declines to when
			// it is. Raised in review of #503.
			wantMsg: "the doc comment on KindBeta opens by naming KindAlpha, which " +
				"is the entry of this block DIRECTLY BELOW it.",
		},
		{
			name:         "an honest block",
			wantExamined: 1,
			src: `const (
	// KindAlpha is the alpha kind.
	KindAlpha = "alpha"

	// KindBeta is the beta kind.
	KindBeta = "beta"
)
`,
		},
		{
			name:         "a var stolen from inside a block",
			wantExamined: 1,
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
			name:         "a block's doc names a later entry of itself",
			wantExamined: 1,
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
			name:         "a spec's doc names a sibling two entries down",
			wantExamined: 1,
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
				"groups was lost, and either way KindAlpha is now undocumented.",
		},
		{
			// THE SHAPE MOST OF THE REAL FINDINGS HAD, which is not
			// an inserted declaration at all: two adjacent comment groups
			// whose separating blank line was lost, so the upper group's
			// subject is now declared below the merged comment. The AST
			// signature is identical to a theft by insertion — which is
			// the point of having it here, since a reader looking for
			// their own case will be looking for this one.
			name:         "a lost blank line merged two comment groups",
			wantExamined: 1,
			src: `// alpha is the alpha thing, and this paragraph is about it.
// beta is a different thing entirely, and the blank line that used to
// separate these two groups is gone.
func beta() {}

func alpha() {}
`,
			want: "alpha",
		},
		{
			// THE SAME LOSS IN THE OTHER ORDER, and this arm exists to
			// make the miss VISIBLE rather than to assert the guard is
			// complete. opensBy reads the first word of the group, so
			// when the honest doc is the upper one the merged group
			// opens with the name it always had, every arm agrees, and
			// alpha below is bare with nothing said about it. The
			// argument against widening is on opensBy; what belongs
			// here is a want:"" that goes red the day somebody widens
			// it, so the accepted class is a decision rather than an
			// assumption. Raised in review of #503.
			name:         "the same lost blank line in the honest order is not caught",
			wantExamined: 1,
			src: `// beta reports the thing.
// alpha is a different thing entirely, and the blank line that used to
// separate these two groups is gone.
func beta() {}

func alpha() {}
`,
		},
		{
			// AN UNDERSCORE IS PART OF THE NAME. It was in opensBy's
			// decoration cutset for markdown emphasis, which Go doc
			// comments do not have, so `_handler` trimmed to `handler`,
			// matched no declaration, and the theft went unreported —
			// the false NEGATIVE the "cannot invent a match" argument
			// does not cover. Raised in review of #503.
			name:         "a leading underscore is part of the identifier",
			wantExamined: 1,
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
			name:         "a method stolen from",
			wantExamined: 1,
			src: `type pane struct{}

// Measure reserves the ring.
func (p *pane) inset() int { return 1 }

func (p *pane) Measure() int { return 2 }
`,
			want: "Measure",
		},
		{
			// A TYPE, which was the one declaration kind no fixture
			// reached. The *ast.TypeSpec arms of documented,
			// documentedSpec and specDeclares were exercised only by the
			// tree-wide corpus, which is clean — so neutering any of the
			// three left the whole root suite green, and neutering
			// documented's dropped the examined population by 518 doc
			// comments, 11% of the tree, with nothing but a t.Logf to
			// say so. Raised in review of #503.
			name:         "a type stolen from",
			wantExamined: 1,
			src: `// Alpha is the alpha thing.
type Beta struct{}

type Alpha struct{}
`,
			want: "Alpha",
		},
		{
			name:         "a type stolen from inside a block",
			wantExamined: 1,
			src: `type (
	// Alpha is the alpha thing.
	Beta struct{}

	Alpha struct{}
)
`,
			want: "Alpha",
		},
		{
			// THE LAST DECLARATION IS THE OTHER BOUNDARY, and the only
			// arm whose population is ZERO. A doc comment on the last
			// declaration in a file cannot have been separated from its
			// subject by an insertion — there is nothing below it to
			// insert — so the rule does not judge it and the population
			// must not count it. That bound was unpinned: dropping
			// `i+1 < len(f.Decls)` from docsExamined left every other arm
			// green. Raised in review of #503.
			name:         "a doc on the last declaration is outside the population",
			wantExamined: 0,
			src: `func beta() {}

// alpha does the alpha thing.
func alpha() {}
`,
		},
		{
			// AN HONEST TYPE BLOCK, AND THE ONE ARM WHOSE POPULATION IS
			// NOT ONE. Three documented specs, of which the rule judges
			// the first two — the last is the boundary the block's
			// closing paren draws. It is what makes wantExamined a
			// measurement rather than a constant: widen docsExamined's
			// spec bound and this arm counts three.
			name:         "an honest type block",
			wantExamined: 2,
			src: `type (
	// Alpha is the alpha thing.
	Alpha struct{}

	// Beta is the beta thing.
	Beta struct{}

	// Gamma is the gamma thing.
	Gamma struct{}
)
`,
		},
		{
			// THE RECEIVER IS NOT PART OF THE QUESTION. A method's doc
			// naming its own method is honest whatever it hangs off,
			// and the exclusion this replaced was argued from telling
			// two Measures apart — which this rule never has to do,
			// because it only ever compares against the declaration
			// directly below.
			name:         "an honest method that names itself",
			wantExamined: 1,
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
			// AND THE POPULATION COUNT SEES THE SAME DOCUMENTS, to the
			// exact number. A walk that reported correctly while counting
			// nothing would leave the non-vacuity floor above resting on
			// other files — but "not nothing" was the whole assertion,
			// and it cannot see a bound that moved on one side only.
			// Every arm's count is derivable by hand from the two rules,
			// which is why it is written down rather than recorded from a
			// run. Raised in review of #503.
			if n := docsExamined(f); n != tc.wantExamined {
				t.Errorf("docsExamined counts %d doc comments in this fixture, want "+
					"%d. The population and the rule share the `j+1 < len(g.Specs)` "+
					"bound, and a bound moved on one side alone changes what the "+
					"floor above MEANS without changing whether it passes", n,
					tc.wantExamined)
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
	//
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
	//
	// AND THE BARE DECLARATION IS first, which the fix for the above got
	// wrong in the other direction: it corrected who was inserted and
	// left the closing clause naming the same declaration for who went
	// bare, so the sentence read "name was inserted … and either way name
	// is now undocumented". name is the one holding a comment — the wrong
	// one, but a comment. first is the one with none attached, in both
	// shapes: after an insertion its own doc is now the newcomer's, and
	// after a lost blank line the two groups have merged onto name and
	// nothing is left above first. A reader sent to document name would
	// write a second doc comment for a declaration that already has one
	// and leave first bare, which is the edit the guard exists to
	// prevent. Raised in the round after, on #503 again.
	// separated takes `bare` — whether `first` was MEASURED to have no
	// doc comment of its own — rather than asserting it.
	//
	// The closing clause said "%s is now undocumented" at all three call
	// sites, and only one of them looked. Round 12's whole argument is
	// that documented-ness rather than distance is what separates theft
	// from prose, and that check lived in laterUndocumented alone: the
	// adjacency arm and the spec-level arm both printed the claim
	// without asking. Reproduced — a doc on `parse` opening with
	// "Parse", with a documented `Parse` directly below, was told
	// "either way Parse is now undocumented", and the go doc -u hint
	// sent the reader to a declaration that already renders correctly.
	//
	// THE FINDING IS STILL REAL WHERE first IS DOCUMENTED, which is why
	// the discriminator is not simply applied at all four sites: the
	// comment on `name` opens by naming something else, so `name` is
	// mis-documented whatever `first`'s state is. What changes is the
	// remedy, because nothing was lost — the reader is looking for a
	// wrong comment, not a missing one. Raised in review of #503.
	separated := func(name, first string, bare bool) string {
		lost := fmt.Sprintf("%s is now undocumented", first)
		if !bare {
			lost = fmt.Sprintf("%s reads as a comment about %s", name, first)
		}
		return fmt.Sprintf("That is a doc comment that was separated from what it "+
			"documents — either %s was inserted between it and %s, or the blank line "+
			"between two comment groups was lost, and either way %s.",
			name, first, lost)
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
						separated(name, first, !documentedItself(f.Decls[i+1])))
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
				// A LATER, UNDOCUMENTED DECLARATION, which is the family
				// the adjacency fence above lets through and the one a
				// merged comment group routinely produces.
				//
				// "Directly below" is the right fence for DISTANCE alone
				// — a comment naming something three functions down is a
				// cross reference, and flagging that is flagging prose.
				// But a lost blank line merges two groups onto whatever
				// declaration follows them, and what follows is often not
				// the subject: this file's own treeWalk sat TWO
				// declarations down behind walkResult, so the i+1 arm
				// never saw it and the guard was green over the defect it
				// exists to find, in its own source.
				//
				// THE DISCRIMINATOR IS NOT DISTANCE, IT IS WHETHER THE
				// NAMED DECLARATION HAS A DOC OF ITS OWN. An honest cross
				// reference names something that is itself documented —
				// every one of the dozen this file measured before
				// rejecting a distance-based widening does. A stolen
				// comment names something left bare, because the theft is
				// exactly what took its doc away. Measured over this tree
				// the refinement reports seven candidates, six of them
				// real, against the dozen false positives the distance
				// version produced.
				case laterUndocumented(f.Decls[i+1:], first):
					// `true` is not an assertion here: this arm's
					// predicate IS the measurement, which is why it
					// carries the locator that states it.
					report(d.Pos(), name, first,
						"a LATER declaration in this file that has no doc "+
							"comment of its own",
						separated(name, first, true))
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
			// MEASURED HERE TOO. The named entry is a spec of this same
			// block, so the question is whether THAT spec carries a doc
			// of its own — findSpec answers it directly rather than
			// letting the sentence assume.
			report(sp.Pos(), name, first, locator,
				separated(name, first, !specDocumented(g.Specs[j+1:], first)))
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
//
// # The order it cannot see, and why it is not widened
//
// It reads the FIRST word of the whole group, so the lost-blank-line
// shape — seven of the eight findings this guard was written for — is
// caught only when the stolen doc was the UPPER group. Reverse the two
// and the merged group opens with the honest doc's own name, every arm
// agrees, and the declaration below is bare with nothing said about it.
// Measured on stolenComments directly: the stolen-first fixture reports
// the theft, the honest-first one reports nothing, and both are one
// keystroke from the same file.
//
// THE OBVIOUS WIDENING IS THE WRONG FIX, and that was measured before
// it was rejected. Scanning every later line and paragraph opening
// across this tree yields a dozen candidates — render/width.go's
// EachCluster doc opening a paragraph with ClipCols, startable.go's
// Delays with Start, term/clipboard.go, components/tabs.go,
// paint/paint.go and more — and every one read is an honest cross
// reference. A rule that fires on those is a rule nobody can leave on.
// First-word-only is therefore load-bearing rather than incidental, and
// the accepted miss is written down here and given a want:"" fixture
// arm, the way documented() records the struct-field gap. Raised in
// review of #503.
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
// rather than a wider switch.
//
// TRACKED AS #527 rather than recorded here alone, because a
// hand-written claim about what is not covered outlives the state it
// describes — CLAUDE.md's "A red suite is yours" requires a boundary of
// this shape to cite an issue the reader can check is still open, so it
// dies with the fix. The issue names the live instance: `workspace` in
// apps/wysiwyg/browser.go has three documented fields and two of them
// have another field directly below. Raised in review of #503.
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

// laterUndocumented reports whether want is declared somewhere below
// this comment and is BARE — no doc comment of its own.
//
// It is the second half of the theft signature, and the half the
// adjacency fence cannot express. The scan stops at the declaration that
// declares want rather than continuing: the first one is the subject,
// and whether IT is documented is the whole question. A documented
// subject means the comment above is a cross reference to a thing that
// already has its own doc, which is prose and not theft.
//
// Shadowing is not a concern here because Go forbids two package-level
// declarations of one name in a file, so there is at most one to find.
func laterUndocumented(rest []ast.Decl, want string) bool {
	for _, d := range rest {
		if !declaresDirectlyBelow(d, want) {
			continue
		}
		return !documentedItself(d)
	}
	return false
}

// documentedItself reports whether d carries a doc comment of its own,
// wherever Go puts it for that shape.
//
// THE BLOCK'S DOC IS NOT THE ONLY PLACE. `documented` answers for a
// *ast.GenDecl from the BLOCK's Doc, so a parenthesised block with no
// doc of its own whose first spec carries one answered "undocumented" —
// and laterUndocumented reported it, printing "a LATER declaration in
// this file that has no doc comment of its own" about a declaration
// whose doc comment is on the line directly above it.
//
// Measured over this tree with the guard's own treeWalk: FOURTEEN such
// blocks across twelve files, including markup/catalog.go — the file
// the block-level work was written for — plus render/color.go,
// control/control.go, companion.go, validate/validate.go,
// handlers/exec/exec.go, paint/shapes/shapes.go,
// components/buttonchrome.go, apps/wysiwyg/editors.go,
// apps/wysiwyg/servelink.go, cmd/browser/gifplay.go and
// cmd/typeahead/typeahead_test.go. Each is a name that produces a false
// finding with a false sentence the moment any doc comment above it
// opens with it. Nothing fires today, so the tree is green and the
// measurement is the only way to see it — which is the argument for
// making it rather than reasoning about it.
//
// declaresDirectlyBelow already descends to Specs[0] for exactly this
// reason; this is the same descent on the other question. Raised in
// review of #503.
func documentedItself(d ast.Decl) bool {
	if _, _, ok := documented(d); ok {
		return true
	}
	if g, ok := d.(*ast.GenDecl); ok && g.Tok != gotoken.IMPORT && len(g.Specs) > 0 {
		_, _, ok := documentedSpec(g.Specs[0])
		return ok
	}
	return false
}

// specDocumented reports whether the spec declaring want, among specs,
// carries a doc comment of its own — the spec-level twin of
// documentedItself.
func specDocumented(specs []ast.Spec, want string) bool {
	for _, sp := range specs {
		if !specDeclares(sp, want) {
			continue
		}
		_, _, ok := documentedSpec(sp)
		return ok
	}
	return false
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
	got := stolenComments(fset, hand, "fake")
	if len(got) != 1 {
		t.Fatalf("the same source reports %d findings when it is NOT generated, "+
			"want 1 — the skip is what suppresses it, so without this the arm "+
			"above would pass over a rule that never fired: %v", len(got), got)
	}
	// AND WHERE IT SAYS IT IS. The comment above records why the
	// discarded FileSet was silent — "nothing went red because the
	// assertion counts findings" — and a count goes on being silent after
	// the fix, so the sentence described a defect nothing could see
	// returning. One line closes it: a second, empty FileSet answers the
	// zero Position for every Pos, so the message opens with a bare "-:".
	// Raised in review of #503.
	if !strings.HasPrefix(got[0], "fixture.go:") {
		t.Errorf("the finding opens with %q rather than a position, so the FileSet "+
			"handed to stolenComments is not the one that parsed the file — and "+
			"confirmHint is being built from an empty filename", got[0])
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
	if got := moduleOwningDir("input", mods); got != "." {
		t.Errorf("a file parsed under input/ is attributed to %q, not the root "+
			"module, so the floor means \"a .go file sits in the repo root\" rather "+
			"than what moduleOwningDir's comment says", got)
	}
	// AND IT IS NOT SATISFIED BY SOMEBODY ELSE'S CODE. This is the arm
	// the first version could not have: under "anywhere beneath", every
	// nested module's files covered "." as well, so the root module —
	// the one this guard lives in — had no independent entry at all.
	if got := moduleOwningDir("apps/gitui", mods); got == "." {
		t.Error("a file in the apps/gitui module is attributed to the root module, " +
			"so the root module's floor entry is satisfied by code it does not contain")
	}
	if got := moduleOwningDir("mcp/cmd/server", mods); got != "mcp" {
		t.Errorf("a file parsed under mcp/cmd/server is attributed to %q rather than "+
			"the mcp module, so a module whose own directory holds no .go file is "+
			"not covered after all", got)
	}
	if got := moduleOwningDir("mcpx", mods); got == "mcp" {
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
	if got := moduleOwningDir("z", short); got != "z" {
		t.Errorf("a file in the single-character module z/ is attributed to %q; "+
			"the root module's directory is one character long too, so a length "+
			"comparison cannot tell the shortest prefix from the shortest name", got)
	}
	if got := moduleOwningDir("z/cmd/tool", short); got != "z" {
		t.Errorf("a file under z/cmd/tool is attributed to %q rather than the z "+
			"module", got)
	}
	// AND THE ROOT STILL WINS WHEN NOTHING ELSE CLAIMS THE FILE — the
	// half a prefixLen that simply returns 0 would break, since owner's
	// zero value measures 0 as well.
	if got := moduleOwningDir("input", short); got != "." {
		t.Errorf("a file under input/ is attributed to %q rather than the root "+
			"module, so the fallback stopped being reachable", got)
	}
	// NESTED MODULES, which this tree does not have today. The floor's
	// stated virtue is that a module added tomorrow is covered without
	// anyone editing this test, and a module added tomorrow INSIDE an
	// existing one is the case the prefix version got wrong.
	nested := []string{".", "apps/gitui", "apps/gitui/plugin"}
	if got := moduleOwningDir("apps/gitui/plugin/cmd", nested); got != "apps/gitui/plugin" {
		t.Errorf("a file in a module nested inside another is attributed to %q, so "+
			"the parent's floor entry is satisfied by its child's files", got)
	}

	// A MODULE SET WITH NO ROOT IN IT. moduleOwningDir has no fallback
	// then and answers "", and "" is not a module of this tree — so the
	// floor's loop, which iterates the module set, never looks at it.
	// Both halves are asserted because either alone is satisfied by a
	// guard that sees nothing: the first pins WHERE the coverage went,
	// the second pins that somebody says so. Raised in review of #503.
	rootless := []string{"apps/gitui", "mcp"}
	if got := moduleOwningDir("input", rootless); got != "" {
		t.Errorf("a root-module file in a set holding no root module is attributed "+
			"to %q; the fallback IS the root module and there is not one here", got)
	}
	covered := map[string]bool{"": true, "apps/gitui": true, "mcp": true}
	if got := moduleFloorFaults(covered, covered, rootless); len(got) != 1 ||
		!strings.Contains(got[0], "no module in this") {
		t.Errorf("a set whose coverage went to the empty module name reports %v, so "+
			"the root module can leave the derived floor without a word", got)
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

// TestTheGeneratedMarkerPatternHasNoCEscapes is the arm
// TestTheRemediationGrepAgreesWithTheRule structurally cannot be.
//
// generatedMarkerPattern has two consumers in two dialects: that test
// compiles it with Go's regexp, and moduleFloorFaults renders it
// VERBATIM into a `grep -rLE` command for a human to paste. The
// agreement test therefore checks the pattern against the engine that is
// NOT the one the printed remedy runs on, so a spelling valid only in Go
// passes it while the advice it prints is broken.
//
// That is not hypothetical: the const carried `\r?` for exactly this
// reason. POSIX ERE has no C escapes — the set GNU grep documents is
// \w \W \s \S \b \B \< \> and backreferences — so it read as a literal
// `r`. Measured with GNU grep 3.11 against a CRLF file, an LF file and
// one ending "DO NOT EDIT.r":
//
//	pattern                CRLF      LF       EDIT.r
//	…EDIT\.\r?$            no match  match    MATCH
//	…EDIT\.[[:space:]]*$   match     match    no match
//
// So `grep -rLE` listed a CRLF-checked-out .pb.go as NOT generated,
// which is the failure the anchors were added to fix, reintroduced by
// the fix for the CRLF one — with the whole suite green.
//
// A SHELL-OUT WOULD NOT BE THE TEST. The default `grep` on at least one
// machine here is ugrep, which honours \r and makes the defect
// invisible; a test that runs whatever `grep` is on PATH would pass or
// fail by the machine. The checkable property is the one that is true of
// the STRING: every backslash in it introduces something POSIX ERE
// defines. Raised in review of #503.
func TestTheGeneratedMarkerPatternHasNoCEscapes(t *testing.T) {
	// The escapes POSIX ERE gives meaning to, plus the ones GNU grep
	// documents as extensions. A backslash before anything else is
	// either a literal of that character (which is fine, and is what
	// `\.` is) or a C escape that means something different in each
	// engine (which is the fault). Letters are the discriminator: `\.`
	// and `\$` are literals in both, `\r` and `\n` are not.
	for i := 0; i < len(generatedMarkerPattern)-1; i++ {
		if generatedMarkerPattern[i] != '\\' {
			continue
		}
		c := generatedMarkerPattern[i+1]
		i++ // consume the escaped byte, so `\\` is not read as an escape
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') {
			continue // an escaped punctuation mark is a literal in both
		}
		switch c {
		case 'w', 'W', 's', 'S', 'b', 'B':
			continue // GNU grep documents these
		}
		t.Errorf("generatedMarkerPattern contains \\%c, which Go's regexp reads "+
			"as an escape and POSIX ERE reads as a literal %q. The const is "+
			"rendered verbatim into the `grep -rLE` command moduleFloorFaults "+
			"prints, so the two consumers would disagree and only the printed "+
			"one would be wrong — silently, because the agreement test compiles "+
			"it with Go. Spell it with a character class: %s",
			c, string(c), generatedMarkerPattern)
	}
}
