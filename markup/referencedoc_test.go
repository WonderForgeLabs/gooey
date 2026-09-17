package markup

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

	var everyInheriting []string
	for name, rule := range boundaryPartition {
		if rule.inherit && isExportedField(name) {
			everyInheriting = append(everyInheriting, name)
		}
	}
	sort.Strings(everyInheriting)

	backticked := regexp.MustCompile("`([A-Za-z]+)`")
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
			if !strings.Contains(flat, "inherit") {
				continue
			}
			named := map[string]bool{}
			for _, m := range backticked.FindAllStringSubmatch(flat, -1) {
				if r, ok := boundaryPartition[m[1]]; ok && r.inherit && isExportedField(m[1]) {
					named[m[1]] = true
				}
			}
			if len(named) < 2 {
				continue
			}
			checked++
			if strings.Contains(flat, "boundaryPartition") {
				continue // cites the source rather than copying it
			}
			var missing []string
			for _, name := range everyInheriting {
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
				"markup.boundaryPartition, which is what the reference and the "+
				"three pages corrected in #490 do:\n\t%s",
				page, len(named), strings.Join(missing, ", "), flat)
		}
	}
	if checked == 0 {
		t.Fatal("no paragraph in the doc corpus makes an inheritance claim naming " +
			"two or more boundary fields, so this guard ruled on nothing: either " +
			"the walk is not reaching the pages or the trigger no longer matches " +
			"how they are written")
	}
}
