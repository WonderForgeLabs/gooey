package markup

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// consumedAsData answers the question checkAttrs' asData parameter asks:
// is this element about to be consumed by its parent's reader rather
// than built.
//
// The corpus walk below used to pass a literal false, meaning "about to
// be built", and after #486 moved the pseudo refusals onto that flag the
// guard went blind to exactly this PR's defect class: a <Tab> in a real
// document is never about to be built, its reader consumes it. Measured
// on that HEAD, per element:
//
//	Tab      (parent Tabs)     asData=false -> <nil>
//	Tab      (parent Tabs)     asData=true  -> markup: <Tab Name="Zonk">…
//	Menu     (parent MenuBar)  asData=false -> <nil>
//	MenuItem (parent Menu)     asData=false -> <nil>
//
// So a <Tab Name="…">, <Menu Name="…"> or <MenuItem Name="…"> added to
// any .gooey file in the tree walked straight through.
//
// THIS IS A MODEL OF THE LOADER'S CALL SITES, not a call into them, and
// it is not attrcheck.go's readsAsData either — that one names the
// reader for a message rather than answering this. buildTabs and
// buildMenuBar pass the flag as a literal at three hard-coded places
// (toolkit.go, markup.go) and there is no seam to ask. That is what
// TestTheCorpusGuardActuallyFiresOnAReaderConsumedAttribute is for: it
// runs a document of each of those shapes through this exact predicate
// and requires a refusal, so the model going stale is a failure rather
// than a silence. Raised in review of #486.
func consumedAsData(e Element, ctx *Context) bool {
	spec, ok := ctx.spec(e.Name)
	if !ok || !spec.Pseudo {
		return false
	}
	// Two spellings of "who reads this", because the built-ins use both:
	// <Menu> names its reader with ParsedBy (<MenuBar>), while
	// <MenuItem>'s ParsedBy also names <MenuBar> and its DOCUMENT parent
	// is the <Menu> that restricts to it. noBuild joins the same two.
	return e.parent == spec.ParsedBy || e.parent == namingParent(spec.Name, ctx)
}

// TestEveryGooeyFileInTheRepoHasValidAttributes is the completeness
// measurement for unknown-attribute rejection, and it is the deliverable
// rather than a side effect.
//
// The generator's guard can only catch attributes it can SEE and fails
// to classify. It is structurally unable to catch one it never looks at
// — an attribute read through a parameter, or in a loop over a table.
// Enforcement is what closes that gap: once an unknown attribute is a
// load error, every attribute the catalog missed shows up as a failure
// against real markup.
//
// The unit tests are one corpus. The .gooey files shipped in the repo
// are the other, and the more important one: they are the markup nobody
// wrote a test for, loaded at runtime by the demos, where a missed
// attribute would otherwise surface as a demo that no longer starts.
//
// This walks every one of them and checks the attributes on every
// element. It does not BUILD them — that would need each app's binding
// context — so it is a pure vocabulary check, which is exactly the axis
// rejection added.
//
// THE asData FLAG IS DERIVED, NOT PASSED AS false — see consumedAsData
// above, and TestTheCorpusGuardActuallyFiresOnAReaderConsumedAttribute
// for the counterfactual that keeps the derivation honest.
func TestEveryGooeyFileInTheRepoHasValidAttributes(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &Context{}
	var checked, files int

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable corners of a working tree are not our business
		}
		if d.IsDir() {
			// Other agents' worktrees live under .claude and are not
			// this tree's markup.
			if name := d.Name(); name == ".git" || name == ".claude" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".gooey") {
			return nil
		}
		files++
		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rootEl, _, err := parse(src)
		if err != nil {
			// A malformed document is a different test's problem.
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		var walk func(e Element)
		walk = func(e Element) {
			checked++
			if err := checkAttrs(e, ctx, consumedAsData(e, ctx)); err != nil {
				t.Errorf("%s: %v", rel, err)
			}
			for _, c := range e.Children {
				walk(c)
			}
			for _, p := range e.Props {
				for _, c := range p.Children {
					walk(c)
				}
			}
		}
		walk(rootEl)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("no .gooey files found; this check is measuring nothing")
	}
	t.Logf("checked %d elements across %d .gooey files", checked, files)
}

// TestTheCorpusGuardActuallyFiresOnAReaderConsumedAttribute is the
// counterfactual for the walk above, and without it that walk is a
// negative assertion that passes for any reason.
//
// The walk reports a problem only when a .gooey file HAS one, and the
// tree's files are clean — so it stayed green both before and after the
// derivation went in. This runs a document of each reader-consumed shape
// through the same two calls the walk makes, `consumedAsData` then
// `checkAttrs`, and requires a refusal. Flip consumedAsData back to a
// literal false and all three arms go red.
//
// ONE UNIVERSAL PER SHAPE, AND A NEAR-MISS WHERE THERE IS A SURFACE TO
// MISS. Name is the attribute #461 is about and travels the universal
// path; a misspelling of the element's own travels the ordinary
// unknown-attribute gate, and an arm that only tried Name would pass a
// version that had lost the second.
//
// <Tab> gets no near-miss arm, and that is measured rather than assumed:
// defTab is Known:false — its Opaque sentence says <Tabs> parses the
// element's Header and content itself — so checkAttrs returns early on
// !AttrsKnown and <Tab Heder="a"> is accepted by design. Adding the arm
// anyway reported it as a guard failure, which is the opposite of what
// it is. <Menu> and <MenuItem> are Known:true and do get one.
func TestTheCorpusGuardActuallyFiresOnAReaderConsumedAttribute(t *testing.T) {
	ctx := &Context{}
	for _, tc := range []struct {
		elem, doc string
	}{
		{"Tab", `<Gooey><Tabs><Tab Header="a" Name="Zonk"><Text>x</Text></Tab></Tabs></Gooey>`},
		{"Menu", `<Gooey><MenuBar><Menu Title="File" Name="Zonk"><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`},
		{"Menu", `<Gooey><MenuBar><Menu Titel="File"><MenuItem Text="Open"/></Menu></MenuBar></Gooey>`},
		{"MenuItem", `<Gooey><MenuBar><Menu Title="File"><MenuItem Text="Open" Name="Zonk"/></Menu></MenuBar></Gooey>`},
		{"MenuItem", `<Gooey><MenuBar><Menu Title="File"><MenuItem Txet="Open"/></Menu></MenuBar></Gooey>`},
	} {
		rootEl, _, err := parse([]byte(tc.doc))
		if err != nil {
			t.Fatalf("the fixture does not parse, so the arm below proves "+
				"nothing: %v", err)
		}
		var found error
		var seen bool
		var walk func(e Element)
		walk = func(e Element) {
			if e.Name == tc.elem {
				seen = true
				if !consumedAsData(e, ctx) {
					t.Errorf("consumedAsData says <%s> under <%s> is about to be BUILT, "+
						"so the walk checks it on the wrong axis and this arm "+
						"could not fire", e.Name, e.parent)
				}
			}
			if err := checkAttrs(e, ctx, consumedAsData(e, ctx)); err != nil && found == nil {
				found = err
			}
			for _, c := range e.Children {
				walk(c)
			}
			for _, p := range e.Props {
				for _, c := range p.Children {
					walk(c)
				}
			}
		}
		walk(rootEl)
		if !seen {
			t.Fatalf("no <%s> in the fixture — the walk never reached the "+
				"element this arm is about", tc.elem)
		}
		if found == nil {
			t.Errorf("the corpus walk accepts %s — an attribute the reader "+
				"drops on the floor, which is exactly what this guard exists "+
				"to catch", tc.doc)
		}
	}
}
