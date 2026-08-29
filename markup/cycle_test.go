package markup

import (
	"strings"
	"testing"
	"testing/fstest"
)

// A markup control that is its own ancestor never stops instantiating.
// Before this was a load error the tests below did not fail, they killed
// the test binary:
//
//	runtime: goroutine stack exceeds 1000000000-byte limit
//	fatal error: stack overflow
//	  encoding/xml.(*Decoder).getc
//	  markup.parse → markup.parseDocument → markup.loadDocument
//
// and note WHERE: in loadDocument, at LOAD time, before layout ran at
// all. MaxLayoutDepth cannot reach this one — no tree ever exists.
//
// It is also the reachable one. The wysiwyg editor lets a user create
// card.gooey and drop <Card/> into it, so this is two ordinary editing
// actions, not a hand-written pathological file.
func loadCycle(t *testing.T, files map[string]string) error {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, src := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(src)}
	}
	_, err := Load(fsys, "app.gooey", &Context{Includes: fsys})
	return err
}

func TestAControlIncludingItselfIsALoadError(t *testing.T) {
	err := loadCycle(t, map[string]string{
		"app.gooey":  `<Gooey><Card/></Gooey>`,
		"card.gooey": `<Gooey><Card/></Gooey>`,
	})
	if err == nil {
		t.Fatal("a self-including control loaded without error")
	}
	if !strings.Contains(err.Error(), "includes itself") {
		t.Errorf("error does not say what is wrong: %v", err)
	}
	// The FILE, not the element name: card.gooey is the thing the user has
	// to open to fix it, and it is unambiguous where <Card/> is not.
	if !strings.Contains(err.Error(), "card.gooey") {
		t.Errorf("error does not name the file: %v", err)
	}
}

// The direct case is the easy one. A user reaches the indirect case just
// as easily — card includes panel, panel includes card — and a guard
// that only compared against the immediate parent would miss it. That is
// the same mistake apps/wysiwyg/components/preview/mirror.go documents
// having considered and rejected for its own recursion.
func TestAnIndirectIncludeCycleIsALoadError(t *testing.T) {
	err := loadCycle(t, map[string]string{
		"app.gooey":   `<Gooey><Card/></Gooey>`,
		"card.gooey":  `<Gooey><Panel/></Gooey>`,
		"panel.gooey": `<Gooey><Card/></Gooey>`,
	})
	if err == nil {
		t.Fatal("an indirect include cycle loaded without error")
	}
	if !strings.Contains(err.Error(), "card.gooey → panel.gooey → card.gooey") {
		t.Errorf("error does not trace the loop: %v", err)
	}
}

// The guard is on ANCESTRY, and this is the test that says so. Two
// <Card/> elements side by side are not a cycle, and a guard built on
// "have I seen this control anywhere" — the obvious cheap version —
// rejects this perfectly ordinary page.
func TestSiblingUsesOfOneControlAreNotACycle(t *testing.T) {
	if err := loadCycle(t, map[string]string{
		"app.gooey":  `<Gooey><VStack><Card/><Card/><Card/></VStack></Gooey>`,
		"card.gooey": `<Gooey><Text>hi</Text></Gooey>`,
	}); err != nil {
		t.Fatalf("three sibling <Card/> elements failed to load: %v", err)
	}
}

// Nor is a control used at two different depths of the same page: the
// second <Card/> is inside <Panel/>, so it is a DESCENDANT of the first
// one's site but not of the first one's own subtree.
func TestOneControlAtTwoDepthsIsNotACycle(t *testing.T) {
	if err := loadCycle(t, map[string]string{
		"app.gooey":   `<Gooey><VStack><Card/><Panel/></VStack></Gooey>`,
		"panel.gooey": `<Gooey><Card/></Gooey>`,
		"card.gooey":  `<Gooey><Text>hi</Text></Gooey>`,
	}); err != nil {
		t.Fatalf("a control used at two depths failed to load: %v", err)
	}
}

// Legal nesting of DIFFERENT controls must still work to full depth —
// the guard must key on which control, not on how many.
func TestDeepLegalControlNestingStillLoads(t *testing.T) {
	if err := loadCycle(t, map[string]string{
		"app.gooey":   `<Gooey><Card/></Gooey>`,
		"card.gooey":  `<Gooey><Panel/></Gooey>`,
		"panel.gooey": `<Gooey><Badge/></Gooey>`,
		"badge.gooey": `<Gooey><Text>hi</Text></Gooey>`,
	}); err != nil {
		t.Fatalf("three nested distinct controls failed to load: %v", err)
	}
}

// A REGISTERED ELEMENT'S KEY AND Name MUST AGREE.
//
// granting() shadows builtins by the Context.Elements map KEY but tests
// the builtin list with `shadowed[d.Name]`, and its caller matches
// `d.Name == parentName`. Context.Catalog has the same split. So a def
// registered under one name and calling itself another is read three
// different ways in one package.
//
// The concrete damage: Elements["Table"] = &ElementDef{Name: "Grid"}
// leaves the builtin <Grid> UNSHADOWED, so both it and the host def sit
// in granting()'s output, both granting Grid.Row, and `attached` keeps
// whichever the map yielded last. That is a wrong answer that changes
// between runs.
//
// There is no reading under which the two should differ — buildComponent
// looks up by the key, so a def whose Name is something else can never
// match the element it is registered under and its grant silently never
// applies. Hence a load error rather than picking a winner.
func TestARegisteredElementKeyMustMatchItsName(t *testing.T) {
	page := `<Gooey><Grid Rows="1*" Cols="1*"><Text>hi</Text></Grid></Gooey>`
	fsys := fstest.MapFS{"p.gooey": &fstest.MapFile{Data: []byte(page)}}

	// The mismatch is refused, and the message names the offending KEY
	// so the host can find it.
	bad := &Context{Elements: map[string]*ElementDef{
		"Table": {Name: "Grid", Grants: Grant{Attached: []AttrSpec{{Name: "Grid.Row"}}}},
	}}
	_, err := Load(fsys, "p.gooey", bad)
	if err == nil {
		t.Fatal("a Context registering Elements[\"Table\"] with Name \"Grid\" loaded " +
			"without complaint — the builtin <Grid> is left unshadowed and both defs " +
			"grant Grid.Row, so which one wins is map order")
	}
	for _, want := range []string{"Table", "Grid"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error is %q — it does not name %q, so the host cannot tell "+
				"which registration is wrong", err, want)
		}
	}

	// THE GUARD MUST ALSO SAY YES. A check that only rejects is
	// indistinguishable from one that rejects everything, and this one
	// runs on every Load.
	good := &Context{Elements: map[string]*ElementDef{
		"Table": {Name: "Table", Grants: Grant{Attached: []AttrSpec{{Name: "Table.R"}}}},
	}}
	if _, err := Load(fsys, "p.gooey", good); err != nil {
		t.Errorf("a Context whose key and Name agree failed to load: %v", err)
	}

	// And the overwhelmingly common case — no registered elements at
	// all — is untouched.
	if _, err := Load(fsys, "p.gooey", &Context{}); err != nil {
		t.Errorf("an empty Context failed to load: %v", err)
	}

	// A nil def is skipped rather than dereferenced: granting() already
	// tolerates one, so the guard must not be the thing that panics.
	nilDef := &Context{Elements: map[string]*ElementDef{"Table": nil}}
	if _, err := Load(fsys, "p.gooey", nilDef); err != nil {
		t.Errorf("a Context holding a nil ElementDef failed to load: %v", err)
	}
}
