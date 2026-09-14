package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
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

// TestAControlsAncestryIsNotAliasedByItsSiblings is the test the
// three-index slice at usercontrol.go's `child.controls = append(...)`
// did not have, and the reason it is worth having is not the load path
// these other tests take.
//
// Every context built during a Load is used and dropped inside that
// Load, so a sibling overwriting the slot a previous sibling wrote
// cannot be observed there — the previous sibling's whole subtree is
// already built, the walk being depth-first and single-goroutine. The
// hazard is a context that OUTLIVES its build: markup/itemsview.go's row
// context captures ctx.controls and builds rows from it at scroll time,
// long after every sibling has appended. Rewrite that array in between
// and the cycle guard reads an ancestry that names controls the row is
// not inside — so a genuine cycle through the row's own control is not
// found, and #216's `fatal error: stack overflow` comes back on a path
// no test walks.
//
// The probe stands in for that retained context. It captures the LIVE
// slice its control was handed and a copy of the contents at that
// moment; if the two disagree once Load has returned, somebody
// rewrote ancestry that was still being pointed at.
//
// THE DEPTH IS LOAD-BEARING AND IS NOT ARITHMETIC IN THE TEST. Append
// only leaves spare capacity once growth has over-allocated, which does
// not happen at the first two levels — a two-deep fixture passes against
// the bug. Rather than write down what Go's growth rule does today, the
// fixture nests until a spare slot exists and the assertion below
// requires the observation to be there: if a future runtime allocates
// differently, this fails as "the fixture no longer reaches the hazard"
// rather than passing quietly.
func TestAControlsAncestryIsNotAliasedByItsSiblings(t *testing.T) {
	type shot struct {
		at    string
		live  []string // the slice the control was handed, still aliasing
		taken []string // its contents at build time
	}
	var shots []shot
	probe := &ElementDef{
		Name:     "Probe",
		Proto:    &components.Text{},
		Known:    true,
		Doc:      "Records the ancestry its enclosing control was built with.",
		Attrs:    []AttrSpec{{Name: "At", Kind: KindString, Binds: BindsLiteral, Origin: OriginRegistered}},
		Children: ChildSpec{Mode: ModeLeaf},
		Build: func(e Element, ctx *Context) (gooey.Component, error) {
			shots = append(shots, shot{
				at:    e.Attrs["At"],
				live:  ctx.controls,
				taken: append([]string{}, ctx.controls...),
			})
			return &components.Text{}, nil
		},
	}

	// Alpha and Beta are siblings deep enough that their parent's
	// ancestry has a spare slot to fight over.
	fsys := fstest.MapFS{}
	for name, src := range map[string]string{
		"app.gooey":   `<Gooey><Outer/></Gooey>`,
		"outer.gooey": `<Gooey><Mid/></Gooey>`,
		"mid.gooey":   `<Gooey><Inner/></Gooey>`,
		"inner.gooey": `<Gooey><VStack><Alpha/><Beta/></VStack></Gooey>`,
		"alpha.gooey": `<Gooey><Probe At="alpha"/></Gooey>`,
		"beta.gooey":  `<Gooey><Probe At="beta"/></Gooey>`,
	} {
		fsys[name] = &fstest.MapFile{Data: []byte(src)}
	}
	if _, err := Load(fsys, "app.gooey", &Context{
		Includes: fsys,
		Elements: map[string]*ElementDef{"Probe": probe},
	}); err != nil {
		t.Fatalf("the fixture does not load: %v", err)
	}

	// Non-vacuity, in both directions. Two probes, each four controls
	// deep, or the fixture is not the one this test describes — and a
	// one-shot run would make the comparison below vacuous, since
	// nothing would have appended after the capture.
	if len(shots) != 2 {
		t.Fatalf("the fixture built %d probes, not 2: %v", len(shots), shots)
	}
	for _, s := range shots {
		if len(s.taken) != 4 {
			t.Fatalf("the %s probe's control is %d deep, not 4, so its parent's "+
				"ancestry has no spare slot for a sibling to write into and this "+
				"test cannot see the bug: %v", s.at, len(s.taken), s.taken)
		}
	}

	// The assertion. Beta appended after Alpha's context was captured;
	// if that append landed in Alpha's backing array, Alpha's ancestry
	// now says "Beta".
	for _, s := range shots {
		for i := range s.taken {
			if s.live[i] != s.taken[i] {
				t.Errorf("the %s control was built with ancestry %v and now reads %v — "+
					"a sibling's append rewrote an array it was still pointing at. A "+
					"context that outlives its build (itemsview.go's row context) would "+
					"run the cycle guard against that, and miss a cycle through %q.",
					s.at, s.taken, s.live, s.taken[len(s.taken)-1])
				break
			}
		}
	}
}

// The error names ONE control: the innermost one, whose file the author
// opens. Attribution was added for the both-maps refusal (see
// TestAControlCannotShadowAPageDeclaredElement) and, wrapped on every
// unwind frame, it stacked — three controls deep gave three names and
// three "markup: " prefixes, and a cycle named the loop twice, once in
// the prefix and once in the trace it already carried.
//
// Counting is the assertion rather than a substring match, because a
// stacked message CONTAINS the right one: `strings.Contains(err, "mid")`
// passes for "control outer: control mid: …" just as happily.
func TestANestedControlErrorNamesTheInnermostControlOnce(t *testing.T) {
	err := loadCycle(t, map[string]string{
		"app.gooey":   `<Gooey><Outer/></Gooey>`,
		"outer.gooey": `<Gooey><Mid/></Gooey>`,
		"mid.gooey":   `<Gooey><Nope/></Gooey>`,
	})
	if err == nil {
		t.Fatal("an unknown element three controls deep loaded without error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "mid.gooey") {
		t.Errorf("error does not name the control the author must open: %v", err)
	}
	if strings.Contains(msg, "outer.gooey") {
		t.Errorf("error names an enclosing control the author cannot fix by editing: %v", err)
	}
	if n := strings.Count(msg, "markup: "); n != 1 {
		t.Errorf("error carries the package prefix %d times, not once: %v", n, err)
	}
	if n := strings.Count(msg, "control "); n != 1 {
		t.Errorf("error says %q %d times, not once: %v", "control ", n, err)
	}
}

// The cycle refusal already names its control and traces the whole loop,
// so it is the case attribution must leave alone. Its message is the one
// that read worst when it did not: "markup: control card.gooey: markup:
// control card.gooey includes itself: card.gooey → card.gooey" named one
// file four times.
func TestACycleRefusalIsNotAttributedTwice(t *testing.T) {
	err := loadCycle(t, map[string]string{
		"app.gooey":  `<Gooey><Card/></Gooey>`,
		"card.gooey": `<Gooey><Card/></Gooey>`,
	})
	if err == nil {
		t.Fatal("a self-including control loaded without error")
	}
	msg := err.Error()
	if n := strings.Count(msg, "markup: "); n != 1 {
		t.Errorf("error carries the package prefix %d times, not once: %v", n, err)
	}
	// Three: the attribution, and the two ends of the loop it traces.
	// A second attribution makes it four.
	if n := strings.Count(msg, "card.gooey"); n != 3 {
		t.Errorf("error names card.gooey %d times, not 3 (the attribution plus "+
			"both ends of the trace): %v", n, err)
	}
}

// A SETUP IS ARBITRARY GO, and the commonest thing it does with a
// document is load another one — a control whose code-behind builds a
// sub-view, a designer that renders a preview. That inner Load returns
// an error already attributed to ITS control, and wrapping it by hand at
// the setup site would put a second name and a second "markup: " on it:
// the exact stacking attributedErr exists to stop, at the one site where
// the inner error is not this package's own recursion.
//
// Measured against the first version of the fix, which built the
// attributedErr inline at this site and at passAttrs':
//
//	markup: control outer.gooey: markup: control mid.gooey: unknown element <Nope>
func TestASetupsOwnLoadErrorIsNotAttributedTwice(t *testing.T) {
	// The inner document instantiates a CONTROL that fails, so the error
	// the setup gets back is already attributed to mid.gooey. A setup
	// whose own Load fails at the top level is a different case and is
	// correctly named after the control whose setup it is.
	inner := fstest.MapFS{
		"inner.gooey": &fstest.MapFile{Data: []byte(`<Gooey><Mid/></Gooey>`)},
		"mid.gooey":   &fstest.MapFile{Data: []byte(`<Gooey><Nope/></Gooey>`)},
	}
	fsys := fstest.MapFS{
		"app.gooey":   &fstest.MapFile{Data: []byte(`<Gooey><Outer/></Gooey>`)},
		"outer.gooey": &fstest.MapFile{Data: []byte(`<Gooey><Text>x</Text></Gooey>`)},
	}
	ctx := &Context{
		Includes: fsys,
		Components: map[string]Builder{
			"Outer": UserControl(fsys, "outer.gooey", func(e Element, parent *Context) (*Context, error) {
				_, err := Load(inner, "inner.gooey", &Context{Includes: inner})
				return nil, err
			}),
		},
	}
	_, err := Load(fsys, "app.gooey", ctx)
	if err == nil {
		t.Fatal("the setup returned an error and the load succeeded")
	}
	msg := err.Error()
	if !strings.Contains(msg, "mid.gooey") {
		t.Errorf("the error does not name the control the inner load failed in: %v", err)
	}
	if strings.Contains(msg, "outer.gooey") {
		t.Errorf("the error names the enclosing control as well as the one that "+
			"actually failed, which is the stacking attributedErr exists to "+
			"stop: %v", err)
	}
	if n := strings.Count(msg, "markup: "); n != 1 {
		t.Errorf("error carries the package prefix %d times, not once: %v", n, err)
	}
}
