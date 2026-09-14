package markup

import (
	"os"
	"regexp"
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
