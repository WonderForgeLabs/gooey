package markup

import (
	"os"
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
