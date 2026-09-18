package markup

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestTheReferenceNamesEveryDeclaredPropertyType is the guard for a row
// that had gone stale by three entries, and could only ever go stale.
//
// docs/markup-reference.md's `Type` row is a hand-written list of the
// spellings <x:Property Type=""> accepts, and propKinds is the map that
// actually accepts them. Review of #470 found the row naming seven of ten
// — style, image and series were declared, documented in propKinds' own
// comments, usable in a control, and absent from the page an author reads
// to find out what Type takes. Nothing could see it: the list is prose.
//
// It reads the DOC and derives the expectation from the CODE, which is
// the only direction that works. The other way round — a list in a test
// checked against the doc — is two hand-written lists that have to agree,
// which CLAUDE.md refuses for the same reason it refuses an enumerated
// module list.
func TestTheReferenceNamesEveryDeclaredPropertyType(t *testing.T) {
	const path = "../docs/markup-reference.md"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	// THE ROW, not the whole file. Every one of these spellings appears
	// somewhere in a 1600-line reference — "int" and "color" a dozen
	// times each — so a whole-file search would pass on a `Type` row
	// naming none of them. Raised in review of #470.
	row := ""
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "| `Type` |") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("%s has no `Type` row in its declaration-attribute table, so this "+
			"guard read nothing. Either the table moved or its shape changed", path)
	}
	if len(propKinds) == 0 {
		t.Fatal("propKinds is empty, so every spelling below would be vacuously present")
	}
	for _, name := range kindNames() {
		if !strings.Contains(row, "`"+name+"`") {
			t.Errorf("<x:Property Type=%q> is accepted by propKinds and the reference's "+
				"`Type` row does not name it. An author reading that row to find out "+
				"what Type takes is told the type does not exist:\n\t%s", name, row)
		}
	}
	// AND NOTHING IT NO LONGER ACCEPTS. A spelling removed from
	// propKinds leaves the row promising a type that is now a load
	// error, which is the same drift pointing the other way.
	accepted := map[string]bool{}
	for _, name := range kindNames() {
		accepted[name] = true
	}
	for _, word := range strings.Split(row, "`") {
		// Only the lower-case single words are type spellings; the row's
		// prose and its `{{...}}` example are not.
		if word == "" || word != strings.ToLower(word) || strings.ContainsAny(word, " {.|") {
			continue
		}
		if !accepted[word] {
			t.Errorf("the reference's `Type` row offers %q and propKinds does not "+
				"accept it, so a control declaring it is a load error:\n\t%s", word, row)
		}
	}
}

// TestTheReferencePartitionMatchesTheCode is the same argument as the
// test above, applied to the list that arrived while it was being made.
//
// usercontrol.go's doc comment stopped enumerating the boundary in
// review of #490, on the stated grounds that a hand-maintained list of
// fields is what #314 IS. The reference doc gained the full ten-name
// list in prose in the same commit — the same artefact, in the surface
// more readers see, and nothing checking it. A rule that a doc comment
// is held to and a reference page is not, is a rule about doc comments.
//
// DERIVED FROM boundaryPartition, never the other way round. The names
// are read out of the sentence and compared both directions: a field
// that inherits and is missing from the inherit half, and a field named
// in the wrong half, are both errors. Unexported fields are out of scope
// — the paragraph is about the surface a control author registers.
// Raised in review of #490.
func TestTheReferencePartitionMatchesTheCode(t *testing.T) {
	const path = "../docs/markup-reference.md"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	// THE PARAGRAPH, not the file, for the reason the test above gives:
	// every one of these names appears throughout a 1600-line reference.
	//
	// A PARAGRAPH, NOT A LINE. The reference writes this one unwrapped
	// today, and reading only the matching line was the same pin: a
	// re-wrap at 80 columns — which every other prose file here is —
	// would leave the tail of the sentence unread, so a field named in
	// the wrong half after the first newline would go unseen. Markdown
	// ends a paragraph at a blank line, so that is where this stops.
	// Raised in review of #490.
	para := ""
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "Everything a page registers inherits") {
			continue
		}
		for _, l := range lines[i:] {
			if strings.TrimSpace(l) == "" {
				break
			}
			para += l + " "
		}
		break
	}
	if para == "" {
		t.Fatal("the reference no longer carries a paragraph starting " +
			"\"Everything a page registers inherits\". Either it was reworded — " +
			"in which case this guard has to follow it — or the partition is " +
			"undocumented, which is the state #314 was filed from")
	}
	const split = "What does NOT cross"
	i := strings.Index(para, split)
	if i < 0 {
		t.Fatalf("the paragraph names no withheld half, so half the partition "+
			"is unstated:\n\t%s", para)
	}

	backticked := regexp.MustCompile("`([A-Za-z]+)`")
	named := func(s string) map[string]bool {
		out := map[string]bool{}
		for _, m := range backticked.FindAllStringSubmatch(s, -1) {
			if _, ok := boundaryPartition[m[1]]; ok {
				out[m[1]] = true
			}
		}
		return out
	}
	inherits, withheld := named(para[:i]), named(para[i:])
	if len(inherits) == 0 || len(withheld) == 0 {
		t.Fatalf("the paragraph names %d inheriting and %d withheld fields; a "+
			"half with none in it would make every check below vacuous",
			len(inherits), len(withheld))
	}

	for name, rule := range boundaryPartition {
		if !isExportedField(name) {
			continue
		}
		want, got := inherits, withheld
		half, other := "inheriting", "withheld"
		if !rule.inherit {
			want, got = withheld, inherits
			half, other = "withheld", "inheriting"
		}
		switch {
		case got[name]:
			t.Errorf("the reference lists Context.%s as %s; boundaryPartition "+
				"says it is %s — %s", name, other, half, rule.why)
		case !want[name]:
			t.Errorf("the reference's partition paragraph never names "+
				"Context.%s, which is %s. A control author reading it is told "+
				"the boundary is exhaustive", name, half)
		}
	}
}

// isExportedField is the reference paragraph's scope: it describes what a
// page REGISTERS, which is the exported surface. The unexported half of
// the partition is a fact about the package and belongs in the Go doc.
func isExportedField(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

// TestTheCompanionSectionStatesTheInheritanceCondition guards the
// sentence the "at every depth" correction reached last.
//
// Context.Dir's own doc comment and markup/companion.go both had the
// overstatement fixed in review of #490, and the reference's <Companion>
// section — the surface more readers land on when they want <Companion>
// behaviour — kept it for another round. The partition paragraph 480
// lines below has always been right, and the guard above reads only
// that one, so nothing could see it.
//
// A REQUIRED WORD, not a forbidden one. "The page must not say 'at any
// depth'" fails open the moment the claim returns in other words, which
// is the shape of guard this branch is about removing. Requiring the
// condition fails CLOSED: a rewrite that drops it goes red, however it
// is phrased.
//
// The expectation comes from boundaryPartition, so the day Dir stops
// inheriting this reports that the paragraph is wrong in the OTHER
// direction rather than passing on a sentence about a rule that no
// longer exists. Raised in review of #490.
func TestTheCompanionSectionStatesTheInheritanceCondition(t *testing.T) {
	const path = "../docs/markup-reference.md"
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	const anchor = "`Context.Dir` is among the fields a UserControl or Include inherits"
	para := ""
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if !strings.Contains(line, anchor) {
			continue
		}
		for _, l := range lines[i:] {
			if strings.TrimSpace(l) == "" {
				break
			}
			para += l + " "
		}
		break
	}
	if para == "" {
		t.Fatalf("%s no longer carries a paragraph saying %q. Either it was "+
			"reworded — in which case this guard has to follow it — or the "+
			"<Companion> section says nothing about where Dir comes from, "+
			"which is the state #314 was filed from", path, anchor)
	}
	if !boundaryPartition["Dir"].inherit {
		t.Fatalf("boundaryPartition says Dir no longer crosses the control "+
			"boundary, and the reference still says it is inherited:\n\t%s", para)
	}
	// ANY OF THREE WORDS, because the accurate one is not "nil". Dir is
	// a string and the loader tests `child.Dir == ""`
	// (markup/usercontrol.go), so an author correcting the prose to
	// "empty" — which is right — turned this red for being right. The
	// guard's DIRECTION is the part worth keeping: it REQUIRES the
	// condition rather than forbidding an overstatement, so a paragraph
	// that drops it fails closed. Only the token was wrong. Raised in
	// review of #490.
	if !strings.Contains(para, "empty") && !strings.Contains(para, "nil") &&
		!strings.Contains(para, "unset") {
		t.Errorf("the <Companion> section states Dir's inheritance without its "+
			"condition — a setup returning a Context with its own Dir keeps "+
			"that one (markup/usercontrol.go), so an unconditional sentence "+
			"here tells a control author the opposite of what the loader "+
			"does. Say empty (accurate), or unset:\n\t%s", para)
	}
}

// answersWhatCrosses is the trigger both boundary guards share: a
// paragraph that makes a claim about what inherits across, or crosses, a
// control boundary.
//
// TWO VOCABULARIES, ONE TRIGGER, because the question gets asked both
// ways and a guard that hears one can be switched off by word choice.
// The forbid guard heard only "inherit" until review of #490 measured
// the hole: docs/architecture.md:1264 asks "which half of it CROSSES a
// control boundary", contains no form of "inherit", and was written by
// the same branch to replace a stale enumeration.
//
// \b, NOT strings.Contains, and that is not style. "across" contains
// "cross": with a substring test, docs/markup-reference.md:889 —
// "including across the control boundary", backticking `Components`,
// `Handlers` and `Rules` as an ANALOGY — is reported as an enumeration
// leaving out seven fields. Measured, one false positive across the
// whole corpus, which is one more than a guard like this survives.
// TestTheBoundaryGuardsPickTheirTable is the pin for partitionFor, and
// it is a pin on the direction of the error rather than on the mapping.
//
// The failing shape was not "a guard is silent"; it was "a guard fires
// and its remedy is wrong". A row-seam paragraph judged against
// boundaryPartition is reported for leaving out `Declared`, which a row
// does NOT inherit (#512) — so the author who follows the message makes
// the page false. That is the assertion below: the two tables disagree
// on the probe paragraph, one way round.
//
// THE PROBE IS SYNTHETIC ON PURPOSE. Both row-seam paragraphs in the
// corpus escape the old guards by accident — one because a contrastive
// clause happens to put the string `boundaryPartition` in it, the other
// because it does not answer by NAMING — so a fixture taken from the
// corpus would pass against the bug. This is the paragraph a row-seam
// page would get if it were written the way this branch asks pages to
// be written.
func TestTheBoundaryGuardsPickTheirTable(t *testing.T) {
	const probe = "A row inherits `Styles`, `Components`, `Elements` and " +
		"`Handlers` from the page; the reasons live in `markup.rowPartition`."

	if !enumeratesThePartition(probe) || !answersByNaming(probe) ||
		!answersWhatCrosses.MatchString(probe) {
		t.Fatal("the probe does not clear the triggers, so it measures nothing " +
			"about which table the guards then reach for")
	}
	if _, table := partitionFor(probe); table != "rowPartition" {
		t.Errorf("a paragraph about the row seam is judged against %s", table)
	}

	// THE DIRECTION. Against the row table the probe is exhaustive;
	// against the control table it is reported as incomplete, and the
	// field it is told to add is one a row must not claim.
	named := namedPartitionFields(probe, true)
	miss := func(part map[string]struct {
		inherit bool
		why     string
	}) []string {
		var out []string
		for _, name := range inheritingFields(part) {
			if !named[name] {
				out = append(out, name)
			}
		}
		return out
	}
	has := func(names []string, want string) bool {
		for _, n := range names {
			if n == want {
				return true
			}
		}
		return false
	}
	// NOT EXHAUSTIVENESS — the probe is short and leaves fields out under
	// either table, which is the ordinary finding this guard exists to
	// report. What separates a noisy finding from a WRONG one is a single
	// field: `Declared`. The control seam says it crosses and the row
	// seam says it does not (#512), so under the wrong table the author
	// is told to add the one name that would make the page false.
	if wrong := miss(boundaryPartition); !has(wrong, "Declared") {
		t.Errorf("judged against the control seam the probe is told to add %v, "+
			"and Declared is not among them — that asymmetry is what makes the "+
			"wrong table a wrong REMEDY rather than a noisy one, so without it "+
			"this test does not measure the finding", wrong)
	}
	if right := miss(rowPartition); has(right, "Declared") {
		t.Errorf("judged against the row seam the probe is still told to add "+
			"Declared (%v), so the two tables agree here and nothing above "+
			"discriminates", right)
	}
	// AND THE UNCITED FORM, which is the population these guards actually
	// hunt: a deleted citation is what they are looking for, so a
	// dispatch that can only read a citation answers "control seam" for
	// every paragraph it is asked about. Measured — with the vocabulary
	// arm removed and only the citation arms left, the probe above still
	// resolves to rowPartition (it cites one) and nothing goes red.
	const uncited = "Inside an `<ItemsView.ItemTemplate>` a row inherits " +
		"`Styles`, `Components`, `Elements` and `Handlers` from the page."
	if !enumeratesThePartition(uncited) || !answersByNaming(uncited) {
		t.Fatal("the uncited probe does not clear the triggers, so it measures " +
			"nothing about the vocabulary arm")
	}
	if _, table := partitionFor(uncited); table != "rowPartition" {
		t.Errorf("a row-seam paragraph with NO citation is judged against %s, "+
			"which is every paragraph these guards exist to catch", table)
	}

	// AND THE CITATION WINS OVER THE VOCABULARY. The fixture is a
	// control-seam paragraph that mentions the row seam CONTRASTIVELY —
	// which is how docs/markup-reference.md actually writes the pair —
	// so the vocabulary alone would send it to the wrong table and the
	// citation is what saves it.
	const boundary = "What crosses a control boundary is " +
		"`markup.boundaryPartition`: `Styles`, `Components`, `Elements` and " +
		"`Handlers` inherit. The per-row context an `<ItemsView.ItemTemplate>` " +
		"is realized in is a different partition with the opposite default."
	if !rowSeamVocabulary.MatchString(boundary) {
		t.Fatal("the contrastive fixture no longer matches the row vocabulary, " +
			"so it cannot show the citation overruling it")
	}
	if _, table := partitionFor(boundary); table != "boundaryPartition" {
		t.Errorf("a control-seam paragraph that mentions the row seam "+
			"contrastively is judged against %s", table)
	}
}

// rowSeamVocabulary is how a paragraph says it is about the ROW seam
// rather than the control boundary, WITHOUT naming a table.
//
// DELIBERATELY NARROW, and a bare `\brow\b` is the reason. The control
// seam's own paragraph in docs/markup-reference.md says "carries a row
// per field with the reason" — a table row, not a template row — so the
// obvious pattern classifies the boundary paragraph as a row-seam one
// and then reports it for citing the wrong table. These are the forms a
// paragraph uses when the SEAM is its subject.
var rowSeamVocabulary = regexp.MustCompile(`(?i)ItemTemplate|\bper[- ]row\b|\brow context\b|\ba row (?:inherits|is not a boundary)\b`)

// partitionFor picks the table a paragraph is answering ABOUT.
//
// THE TABLE WAS HARDCODED, and there are two of them. Both guards below
// took `boundaryPartition` as the only answer, and
// docs/markup-reference.md now carries two paragraphs whose table is
// `rowPartition` — which disagrees with it on six fields. Measured with
// a throwaway probe in review of #490, on a row-seam paragraph written
// the way this branch asks paragraphs to be written:
//
//	"A row inherits `Styles`, `Components`, `Elements` and `Handlers`
//	 from the page; the reasons live in `markup.rowPartition`."
//
// TestNoPageEnumeratesTheBoundaryPartition reported it as leaving out
// `Declared` — which a row does NOT inherit (#512) — so an author
// following the failure message makes the page WRONG. The sibling guard
// then demanded a citation of the other seam's table.
//
// THE CITATION WINS WHERE THERE IS ONE, because a paragraph that names
// its table has already answered this question and a vocabulary guess
// cannot overrule it. That is not a formality: docs/markup-reference.md
// writes the two seams contrastively, so a control-seam paragraph
// naming <ItemsView.ItemTemplate> to say the row is DIFFERENT matches
// the row vocabulary outright.
//
// The vocabulary is only for the uncited paragraph — which is the whole
// population these guards exist to catch, since a deleted citation is
// what they are looking for.
//
// A PARAGRAPH NAMING BOTH TABLES goes to the row, and that is a
// tie-break rather than a derivation. The contrast is drawn from the
// row's side in this corpus — the row's paragraph has to say "not the
// control seam's", while the control's has no reason to name the row
// table — and markup-reference.md:936 is the one paragraph that does it.
// A control-seam paragraph contrastively naming `markup.rowPartition`
// would be judged against the row's table; none exists, and the arm
// below is where that would be changed.
//
// The residue, stated rather than bounded: a row-seam paragraph that
// cites no table AND uses none of the vocabulary above is judged against
// the control seam's. It is reported for "leaving out" fields a row does
// not inherit, which is the defect above in a narrower window. Both
// row-seam paragraphs in the corpus today are covered twice over — each
// cites `markup.rowPartition` and each names `<ItemsView.ItemTemplate>`
// or "per-row" — so this is a claim about paragraphs nobody has written
// yet.
func partitionFor(flat string) (map[string]struct {
	inherit bool
	why     string
}, string) {
	switch {
	case strings.Contains(flat, "rowPartition"):
		return rowPartition, "rowPartition"
	case strings.Contains(flat, "boundaryPartition"):
		return boundaryPartition, "boundaryPartition"
	case rowSeamVocabulary.MatchString(flat):
		return rowPartition, "rowPartition"
	}
	return boundaryPartition, "boundaryPartition"
}

// inheritingFields is every exported field a partition says crosses.
func inheritingFields(part map[string]struct {
	inherit bool
	why     string
}) []string {
	var out []string
	for name, rule := range part {
		if rule.inherit && isExportedField(name) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

var answersWhatCrosses = regexp.MustCompile(`(?i)\b(?:inherit|cross)`)

// backtickedWord is hoisted for the reason partitionWords is, and this
// is the site that comment called "the one still paying it" while the
// pattern below it went on compiling per call. Its consumer runs on a
// STRICTLY LARGER population: namedPartitionFields reaches
// backtickedPartition for every trigger-matching paragraph of the
// ../docs walk in the forbid guard, and for every answersWhatCrosses
// paragraph in the require guard, which calls it without the
// enumeratesThePartition pre-filter. Raised in review of #490.
//
// ABOVE backtickedPartition's DOC BLOCK, not between it and the
// function. Dropped in there it took that block's four paragraphs onto
// itself and left backtickedPartition undocumented — which this
// package's own guard caught on the first run, and which is the defect
// the branch holding this commit exists to police.
var backtickedWord = regexp.MustCompile("`([A-Za-z]+)`")

// backtickedPartition is every partition field a paragraph names in
// backticks, on the side asked for.
//
// Shared rather than spelled twice for the reason this file keeps
// running into: a rule written in two places is a rule only half the
// callers receive the next fix for. The inheriting-only wrapper that
// stood here was deleted in round ten, once the forbid guard stopped
// re-deriving its own copy and the wrapper's only caller went with it.
//
// THE SIDE IS A PARAMETER, and the parameter is the whole of finding
// #490 round 9. The forbid guard adjudicates a PROPER SUBSET of the
// inheriting side, so it counts only that side. The require guard asks
// a different question — "does this paragraph answer what crosses by
// naming fields" — and `Values` and `Named` in one sentence is exactly
// that answer, given from the isolating side. Measured:
// docs/architecture.md's second answering
// paragraph names only those two, so under the inheriting-only bar it
// was never adjudicated and deleting its citation left both boundary
// guards green — the per-page-vs-per-paragraph defect the round before
// believed it had closed. Raised in review of #490.
func backtickedPartition(flat string, inheritingOnly bool) map[string]bool {
	named := map[string]bool{}
	for _, m := range backtickedWord.FindAllStringSubmatch(flat, -1) {
		if r, ok := boundaryPartition[m[1]]; ok && (r.inherit || !inheritingOnly) &&
			isExportedField(m[1]) {
			named[m[1]] = true
		}
	}
	return named
}

// namedPartitionFields is every distinct partition field a paragraph
// names, by backtick or in a comma run, on the side asked for.
//
// THE SET RATHER THAN THE COUNT, because the forbid guard needs both:
// the count is its bar and the set is what its message subtracts from
// everyInheriting to say which fields were left out. It re-derived the
// set inline until round ten, which put the bar in two places and left
// enumeratesThePartition — the function whose doc says it IS the bar —
// called by nobody. Raised in review of #490.
func namedPartitionFields(flat string, inheritingOnly bool) map[string]bool {
	named := backtickedPartition(flat, inheritingOnly)
	for _, name := range partitionRunSide(flat, inheritingOnly) {
		named[name] = true
	}
	return named
}

// namesPartitionFields is how many of them there are.
func namesPartitionFields(flat string, inheritingOnly bool) int {
	return len(namedPartitionFields(flat, inheritingOnly))
}

// answersByNaming is the REQUIRE direction's bar: a paragraph that
// answers the crossing question and names two or more partition fields
// on EITHER side is giving the answer, and has to point at the source.
func answersByNaming(flat string) bool {
	return answersWhatCrosses.MatchString(flat) && namesPartitionFields(flat, false) >= 2
}

// enumeratesThePartition reports that a paragraph answers the crossing
// question BY LISTING the inheriting side — the conjunction
// TestNoPageEnumeratesTheBoundaryPartition adjudicates on, and its only
// spelling.
//
// THE REQUIRE DIRECTION'S BAR IS DELIBERATELY WIDER, which this comment
// used to deny: it said the two guards read the same conjunction "so the
// two cannot disagree about which paragraphs are in scope". They do
// disagree, on purpose. answersByNaming counts BOTH sides, because a
// paragraph that answers "what crosses" by naming the fields that do NOT
// is giving the same answer and owes the same citation; the forbid
// direction only has something to subtract when the list is of the
// inheriting side. The sentence outlived the round that widened one of
// them, and while it stood it sent a reader auditing scope-agreement to
// a function neither guard called. Raised in review of #490.
func enumeratesThePartition(flat string) bool {
	return answersWhatCrosses.MatchString(flat) && namesPartitionFields(flat, true) >= 2
}

// TestNoPageEnumeratesTheBoundaryPartition is the guard widened past the
// one page that had it.
//
// TestTheReferencePartitionMatchesTheCode reads docs/markup-reference.md
// and only its partition paragraph, so three other pages went on carrying
// the four-of-ten list this branch exists to retire —
// docs/architecture.md, docs/getting-started.md and
// docs/learn/05-usercontrols.md, two of them TUTORIALS, which is where a
// control author learns the rule rather than where they check it. A rule
// the reference is held to and every other page is not, is a rule about
// one page. Raised in review of #490.
//
// WHAT IT LOOKS FOR IS AN ENUMERATION, not a mention. A page is free to
// say `Styles` and `Components` in a sentence about registering things;
// what it may not do is answer "which fields inherit" with a list, because
// that answer is ten rows long and goes stale silently — #314 is the
// report of it having done so. So the trigger is the conjunction: a
// paragraph that makes an inheritance CLAIM and backticks two or more
// partition fields. Two escapes, and only two: name every inheriting
// field (the reference paragraph, which is checked field-by-field above),
// or cite boundaryPartition instead of enumerating (what the three
// corrected pages now do).
//
// A PARAGRAPH, NOT A LINE, for the reason the test above gives: these
// pages wrap at 80 columns, so a line-oriented read sees half a sentence.
func TestNoPageEnumeratesTheBoundaryPartition(t *testing.T) {
	const root = "../docs"
	var pages []string
	if err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			pages = append(pages, p)
		}
		return nil
	}); err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	if len(pages) == 0 {
		t.Fatalf("no markdown under %s, so this guard read nothing", root)
	}

	checked := 0
	for _, page := range pages {
		b, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("reading %s: %v", page, err)
		}
		for _, para := range strings.Split(string(b), "\n\n") {
			flat := strings.Join(strings.Fields(para), " ")
			// THE CLAIM IS THE TRIGGER. Without it every table row
			// listing two field names is a finding, and the guard becomes
			// noise a reader learns to widen rather than read.
			// CASE-FOLDED. strings.Contains is exact, so a paragraph
			// under a heading like "**Inherited fields.**", or opening
			// "Inherits from the parent", was skipped with its stale
			// list intact — and docs/learn/05-usercontrols.md already
			// headings this section, so a title-case rewrite of one
			// heading would have switched the guard off for it. Raised
			// in review of #490.
			//
			// AND THE OTHER VOCABULARY. The crossing question gets asked
			// two ways and this guard heard one: "which half of it
			// CROSSES a control boundary" contains no form of "inherit",
			// so docs/architecture.md:1264 — written by this very branch
			// to replace a stale enumeration — could have its citation
			// deleted with both guards green. Measured in review of
			// #490.
			//
			// The trigger is a word boundary rather than a substring,
			// and what that buys is on answersWhatCrosses — stated
			// once, for backtickedPartition's reason.
			if !enumeratesThePartition(flat) {
				continue
			}
			// Backticked or bare, on the inheriting side: see
			// namedPartitionFields, which owns both spellings and the
			// argument for each.
			named := namedPartitionFields(flat, true)
			checked++
			// WHICH TABLE, chosen per paragraph: see partitionFor. The
			// two disagree on six fields, so judging a row-seam
			// paragraph against the control seam's list reports it for
			// leaving out what a row correctly does not inherit.
			part, table := partitionFor(flat)
			if strings.Contains(flat, table) {
				continue // cites the source rather than copying it
			}
			var missing []string
			for _, name := range inheritingFields(part) {
				if !named[name] {
					missing = append(missing, name)
				}
			}
			if len(missing) == 0 {
				continue // the exhaustive form, checked field-by-field above
			}
			t.Errorf("%s answers what inherits with a list of %d field(s) and "+
				"leaves out %s. A page that enumerates a PROPER SUBSET tells a "+
				"control author those fields do not cross, which is #314 "+
				"restated as prose. Either name them all — and expect to be "+
				"wrong again the next time Context grows one — or point at "+
				"markup.%s, which is what the reference and the "+
				"three pages corrected in #490 do:\n\t%s",
				page, len(named), strings.Join(missing, ", "), table, flat)
		}
	}
	if checked == 0 {
		t.Fatal("no paragraph in the doc corpus makes an inheritance claim naming " +
			"two or more boundary fields, so this guard ruled on nothing: either " +
			"the walk is not reaching the pages or the trigger no longer matches " +
			"how they are written")
	}
}

// partitionWords is one `\bname\b` pattern per partition field, built
// once.
//
// COMPILED PER CALL until #490's review, which is the same memoisation
// this file already applies to declaredIndex and goCommentIndex. It was
// called "the one site still paying it" here, and that was wrong when it
// was written: backtickedPartition, two declarations above, compiled its
// own pattern per call on a larger population, and went on doing so for
// a round because this sentence said there was nothing left. Both are
// hoisted now. The cost this one carried: ~12 compiles per call, once per
// trigger-matching paragraph of the whole ../docs walk and TWICE for
// every paragraph that clears the bar, since enumeratesThePartition and
// the namedPartitionFields beside it each re-derive the run. Measured:
// TestNoPageEnumeratesTheBoundaryPartition 0.46s -> 0.21s and
// TestEveryPageThatAnswersWhatCrossesCitesThePartition 0.10s -> 0.05s,
// against a 4.5s markup suite. Raised in review of #490.
var partitionWords = sync.OnceValue(func() map[string]*regexp.Regexp {
	out := map[string]*regexp.Regexp{}
	for name := range boundaryPartition {
		out[name] = regexp.MustCompile(`\b` + strings.ToLower(name) + `\b`)
	}
	return out
})

// THE UNFORMATTED SPELLING IS HALF OF IT, and leaving it out made the
// guard a check on markup rather than on prose: "styles, registered
// components, handlers, includes" is the four-of-ten answer #314 is
// about, written out, and it sat in docs/markup-reference.md's
// <ItemsView.ItemTemplate> paragraph with only `xmlns` in backticks —
// invisible for two rounds.
//
// Matching bare names ONE AT A TIME was measured first and rejected:
// case-insensitively, "components" and "styles" are English, and the
// corpus answered with two paragraphs that MENTION fields rather than
// answer the crossing question (the `Elements`-cost paragraph in the
// reference, and the Declared registry note in
// docs/specs/2026-08-10-mcp-server.md). Noise a reader learns to widen
// is the failure this guard's own doc warns about, so the signature is
// the RUN instead — see partitionRunSide. A sentence that mentions a
// field does not produce one; a stale answer to "what crosses" always
// does.
//
// THIS PARAGRAPH SAT ABOVE A `checked++` until #490's review, and then
// above a regexp cache. The rewrite that made these functions moved the
// logic and left its twenty-line justification at the old call site,
// where the statement underneath it was a counter; the move that fixed
// that landed one declaration short, with no blank line before
// partitionWords' own comment, so Go read the two as one group and gave
// all of this to the cache while partitionRunSide had none. A comment
// moved onto the wrong declaration is the same defect as a comment left
// behind one — godoc attaches a group to whatever follows it, and
// neither move is visible to anything but a reader. Raised in review of
// #490, twice.
//
// partitionRunSide is every partition field on the side asked for that
// is named inside one comma-or-and list of three or more of them,
// case-insensitively — the shape of a prose answer to "what crosses a
// control boundary", and nothing else in the corpus. The side is a
// parameter for backtickedPartition's reason.
//
// Three rather than two, and a bounded gap between them: two names a
// clause apart is an ordinary sentence ("Styles and Named are handled
// differently"), and the separators a list uses are short. The gap is
// measured in the flattened paragraph, so a list wrapped over three
// source lines is still one run. Raised in review of #490.
func partitionRunSide(flat string, inheritingOnly bool) []string {
	const maxGap = 30 // ", registered " and friends; not a clause

	type hit struct {
		name     string
		at, past int
	}
	lower := strings.ToLower(flat)
	var hits []hit
	for name, rule := range boundaryPartition {
		if (inheritingOnly && !rule.inherit) || !isExportedField(name) {
			continue
		}
		for _, m := range partitionWords()[name].FindAllStringIndex(lower, -1) {
			hits = append(hits, hit{name, m[0], m[1]})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].at < hits[j].at })

	var out []string
	for i := 0; i < len(hits); i++ {
		run := []hit{hits[i]}
		for j := i + 1; j < len(hits); j++ {
			// lower, NOT flat, and the offsets are why. Every `at`/`past`
			// above came from FindAllStringIndex over `lower`, and
			// strings.ToLower is not byte-length preserving: U+212A
			// KELVIN SIGN folds to a one-byte `k` from three, U+0130
			// expands. One such rune earlier in a paragraph shifts every
			// subsequent offset, and this slice then reads the wrong
			// bytes out of `flat` — silently, because the consequence is
			// a run that stops being recognised (or starts), which
			// changes what the two boundary guards adjudicate rather
			// than producing an error. The gap is lowercased again below
			// for the " and " test anyway, so on the ASCII corpus this
			// is byte-for-byte the same and on the rest it is the
			// correct one. Raised in review of #490.
			gap := lower[hits[j-1].past:hits[j].at]
			if len(gap) > maxGap || !strings.ContainsAny(gap, ",&") &&
				!strings.Contains(gap, " and ") {
				break
			}
			run = append(run, hits[j])
		}
		if len(run) >= 3 {
			for _, h := range run {
				out = append(out, h.name)
			}
			i += len(run) - 1
		}
	}
	return out
}

// TestPartitionRunSideIndexesOneStringOnly pins the offset half of
// partitionRunSide, which nothing in the corpus can reach.
//
// The hits come from FindAllStringIndex over `lower` and the gap used to
// be sliced out of `flat`. strings.ToLower is not byte-length
// preserving — U+212A KELVIN SIGN folds to a one-byte "k" from three,
// U+0130 expands — so one such rune earlier in a paragraph shifts every
// subsequent offset and the gap test reads the wrong bytes. The
// consequence is silent: a run stops being recognised, which changes
// what the two boundary guards adjudicate rather than producing an
// error.
//
// NO DOCUMENT HAS ONE, which is exactly why this is a unit test and not
// a corpus observation. Every page in ../docs is ASCII in the region
// these runs live, so slicing either string was byte-for-byte identical
// and the whole suite stayed green over the bug — reverting the slice to
// `flat` reddens this test and nothing else. Raised in review of #490.
func TestPartitionRunSideIndexesOneStringOnly(t *testing.T) {
	const run = "styles, components and handlers cross"
	want := partitionRunSide(run, true)
	if len(want) < 3 {
		t.Fatalf("the ASCII control found %v, want a run of at least three: "+
			"this test's premise is that the same sentence is recognised "+
			"with and without a folding rune, and the control side of that "+
			"comparison is not being recognised at all", want)
	}
	// U+212A, three bytes, folding to one. Anywhere before the run is
	// enough — the shift applies to every offset after it.
	got := partitionRunSide("\u212A "+run, true)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("a KELVIN SIGN before the run changes the answer from %v to "+
			"%v. The hit offsets are measured in the lowercased string and "+
			"the gap is being sliced out of a different one, so a rune whose "+
			"folding changes its byte length moves every later slice", want, got)
	}
}

// TestEveryPageThatAnswersWhatCrossesCitesThePartition is the REQUIRE
// direction, and the guard above is only the forbid one.
//
// Nothing held the four corrected paragraphs to their citation: deleting
// "markup.boundaryPartition" from any of them leaves a paragraph that
// answers the crossing question with nothing at all, and
// TestNoPageEnumeratesTheBoundaryPartition passes it — it forbids a
// SUBSET, and the empty set is not one it can see. That is the direction
// argument this file already makes for
// TestTheCompanionSectionStatesTheInheritanceCondition, applied to the
// pages the same round corrected. Raised in review of #490.
//
// The page list is written down because it is a list of PAGES, not of
// fields: a page that stops existing fails this closed at the read, and
// a new page answering the question is caught by the guard above rather
// than by silence here.
//
// EVERY ENUMERATING PARAGRAPH, NOT THE FIRST ONE ON THE PAGE. This set
// `cited` and broke at the first match, and docs/architecture.md has two
// answering paragraphs (:1264 and :1388) — so deleting the citation from
// the second was invisible here, and invisible to the forbid guard too
// because that one stands down on a paragraph naming no fields. A
// per-page flag for a per-paragraph defect. Raised in review of #490.
//
// THE PER-PARAGRAPH BAR IS answersByNaming, NOT THE TRIGGER, and the
// difference was measured rather than chosen. Requiring a citation from
// every paragraph matching the trigger flags 47 of the 53 such
// paragraphs across these four pages — every sentence that happens to
// use the word "inherits" — which is noise, not a guard.
//
// AND IT IS NOT enumeratesThePartition EITHER, which is what the round
// before used and is the FORBID guard's bar. That one counts only the
// INHERITING side, because a proper-subset finding only means anything
// there. This direction asks a different question: does the paragraph
// answer what crosses by naming fields. `Values` and `Named` in one
// sentence is precisely that answer, given from the isolating side —
// and docs/architecture.md's second answering paragraph is exactly that
// shape, so under the forbid guard's bar it was never adjudicated and
// deleting its citation left both boundary guards green. That is the
// per-page-vs-per-paragraph defect the paragraph above claims to have
// closed, still open one bar over. Measured across the four pages with
// both sides counted: the same paragraphs are in scope plus that one,
// and no new false positive. Raised in review of #490, twice.
//
// BOTH CLAUSES SURVIVE. The per-page floor still runs, because a page
// that loses its only citation may also have lost the enumeration with
// it, and then no paragraph is in scope for the per-paragraph clause.
func TestEveryPageThatAnswersWhatCrossesCitesThePartition(t *testing.T) {
	adjudicated := 0
	for _, page := range []string{
		"../docs/architecture.md",
		"../docs/getting-started.md",
		"../docs/learn/05-usercontrols.md",
		"../docs/markup-reference.md",
	} {
		b, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("reading %s: %v — this page carried the four-of-ten list "+
				"#314 reported, so its disappearance is a finding rather than "+
				"a reason to skip", page, err)
		}
		cited := false
		for _, para := range strings.Split(string(b), "\n\n") {
			flat := strings.Join(strings.Fields(para), " ")
			if !answersWhatCrosses.MatchString(flat) {
				continue
			}
			// THE SEAM PICKS THE TABLE, not the guard: partitionFor.
			_, table := partitionFor(flat)
			if strings.Contains(flat, table) {
				cited = true
				if answersByNaming(flat) {
					adjudicated++
				}
				continue
			}
			if answersByNaming(flat) {
				t.Errorf("%s answers what crosses a context seam by naming "+
					"fields and does not cite markup."+table+":\n\t%s\n"+
					"Every such paragraph has to point at the partition, not just "+
					"the first one on the page — this guard used to stop at the "+
					"first citation, so a second answering paragraph could lose "+
					"its own with both boundary guards green", page, flat)
			}
		}
		if !cited {
			t.Errorf("%s makes no paragraph that answers what crosses a control "+
				"boundary AND cites markup.boundaryPartition. The forbid-direction "+
				"guard cannot see this: it reports a page that names a PROPER "+
				"SUBSET, and a paragraph with the citation deleted names none, "+
				"which passes. Each of these four pages answered the question "+
				"wrongly before #490 and has to keep answering it from the "+
				"source", page)
		}
	}
	// THE MUST-FIRE FLOOR, which every sibling guard in this file carries
	// and this one did not — TestNoPageEnumeratesTheBoundaryPartition has
	// `checked == 0`, TestEveryCitedSymbolResolves has `fromGo == 0` and
	// `rescued == 0`, TestEveryPrereqRowIsReached and
	// TestEveryCitedTestNameResolves have theirs. Measured without it:
	// neutering the bar to a predicate that can never be true — so the
	// per-paragraph clause adjudicates nothing at all — left the whole
	// markup suite green, which is how the defect above could sit under a
	// paragraph claiming to have closed it.
	//
	// IT COUNTS THE PARAGRAPHS THAT PASS, not the findings. On a correct
	// corpus the finding count is legitimately zero, so a floor over
	// findings would be a demand that the docs be wrong. What can be
	// asserted is that the PREDICATE still recognises this corpus: the
	// four pages between them hold paragraphs that answer the crossing
	// question by naming fields and do cite the partition, and if that
	// count reaches zero the bar has stopped matching anything and the
	// clause is adjudicating an empty set. Raised in review of #490.
	if adjudicated == 0 {
		t.Error("no paragraph on any of these four pages answers what crosses by " +
			"naming two or more partition fields AND cites markup.boundaryPartition, " +
			"so the per-paragraph clause above ruled on nothing: answersByNaming has " +
			"stopped recognising this corpus and the clause passes over any number " +
			"of uncited answers")
	}
}
