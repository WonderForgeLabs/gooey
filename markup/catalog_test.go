package markup

import (
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup/internal/catalogen"
)

// TestDeclaredVocabularyMatchesTheCode is the drift guard, and it
// guards ONE DIRECTION that nothing else can see.
//
// A declared vocabulary can be wrong two ways, and they are not
// symmetric. UNDER-declaring is loud: unknown attributes are rejected,
// so the first document setting the missing attribute fails to load and
// TestEveryGooeyFileInTheRepoHasValidAttributes names the file.
// OVER-declaring is silent: an attribute the literal permits and no code
// reads is accepted and ignored, which is precisely the silent-drop
// defect this vocabulary exists to prevent, reintroduced through the
// vocabulary itself.
//
// Measured, which is why this test exists rather than a cheaper one:
// deleting an attribute from a literal fails three tests, while ADDING
// a bogus one to <Button> left the entire suite green.
//
// The cheaper alternative — set each declared attribute to an absurd
// value and require an error — cannot reach 51 of 124 rows (41%),
// because text, string, style and identity attributes accept anything.
// Style is the worst case: an unknown style renders unstyled with no
// error, so it is invisible to that trick AND silent at runtime.
func TestDeclaredVocabularyMatchesTheCode(t *testing.T) {
	findings, err := catalogen.Check(".")
	if err != nil {
		t.Fatalf("checking the declared vocabulary: %v", err)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// TestCatalogOpaqueElementsAreExactlyTheseOnes pins the escape hatch.
//
// //gooey:catalog-opaque lets an arm skip enumeration, which is
// necessary — without it the pressure of a novel arm shape lands on
// someone quietly loosening the extractor instead. But an escape nobody
// counts is an escape that spreads. This asserts the EXACT set, so
// adding one is a deliberate edit to this list with a reviewer looking
// at it, not a silent slide. A guard that cannot fail is not a guard.
func TestCatalogOpaqueElementsAreExactlyTheseOnes(t *testing.T) {
	want := map[string]bool{"Tab": true}
	got := map[string]bool{}
	for _, e := range BuiltinElements() {
		if !e.AttrsKnown {
			got[e.Name] = true
			if e.Opaque == "" {
				t.Errorf("<%s> is not AttrsKnown but carries no reason", e.Name)
			}
			if e.Children.Mode != ModeUnknown {
				t.Errorf("<%s> is opaque but claims child mode %q; if the attributes could not be read, neither could the child rule", e.Name, e.Children.Mode)
			}
		}
	}
	for n := range want {
		if !got[n] {
			t.Errorf("<%s> is no longer opaque; remove it from this list", n)
		}
	}
	for n := range got {
		if !want[n] {
			t.Errorf("<%s> became opaque. That is allowed, but it must be deliberate: add it here with a note on why its vocabulary cannot be enumerated.", n)
		}
	}
}

// TestCatalogKnowsTheElementsItMustKnow spot-checks entries whose shape
// the extraction rules each depend on, so a regression in one rule fails
// with a name rather than as a byte diff.
func TestCatalogKnowsTheElementsItMustKnow(t *testing.T) {
	by := map[string]ElementSpec{}
	for _, e := range BuiltinElements() {
		by[e.Name] = e
	}
	cases := []struct {
		el, attr string
		kind     Kind
		binds    Binds
	}{
		{"Button", "Click", KindCommand, BindsEither},      // ctx.Command
		{"Button", "Content", KindText, BindsEither},       // bindText
		{"Button", "Chrome", KindEnum, BindsLiteral},       // ParseX + names slice
		{"Checkbox", "Checked", KindBinding, BindsBinding}, // boundProp[T]
		{"Canvas", "Background", KindColor, BindsEither},   // bindColor
		{"Grid", "Rows", KindGridLens, BindsLiteral},       // ParseGridLens
		{"Timer", "Interval", KindDuration, BindsLiteral},  // ParseDuration
		{"KeyBinding", "Gesture", KindGesture, BindsLiteral},
		{"ButtonBar", "Separator", KindString, BindsLiteral}, // verbatim string field
		{"Companion", "Path", KindString, BindsLiteral},      // //gooey:catalog-attrs
		{"TextBox", "AccentStyle", KindStyle, BindsLiteral},  // reused local name
	}
	for _, c := range cases {
		e, ok := by[c.el]
		if !ok {
			t.Errorf("<%s> missing from the catalog", c.el)
			continue
		}
		var found *AttrSpec
		for i := range e.Attrs {
			if e.Attrs[i].Name == c.attr {
				found = &e.Attrs[i]
			}
		}
		if found == nil {
			t.Errorf("<%s %s> missing from the catalog", c.el, c.attr)
			continue
		}
		if found.Kind != c.kind || found.Binds != c.binds {
			t.Errorf("<%s %s> = %s/%s, want %s/%s", c.el, c.attr, found.Kind, found.Binds, c.kind, c.binds)
		}
	}
}

// TestCatalogChildModesDistinguishNoneFromAttachments is the split that
// makes the catalog usable for placement: <Button> takes no visual
// children but does host a nested <Tooltip>, while <AdornmentLayer>
// takes nothing at all. Collapsing them to a bool would make a palette
// forbid a legal drop or permit an illegal one.
func TestCatalogChildModesDistinguishNoneFromAttachments(t *testing.T) {
	want := map[string]ChildMode{
		"Button":         ModeAttachments,
		"TextBox":        ModeAttachments,
		"AdornmentLayer": ModeNone,
		"ToastHost":      ModeNone,
		"Border":         ModeOne,
		"Grid":           ModeMany,
		"Canvas":         ModeMany,
		"VStack":         ModeMany,
		"Tabs":           ModeRestricted,
	}
	for _, e := range BuiltinElements() {
		if w, ok := want[e.Name]; ok && e.Children.Mode != w {
			t.Errorf("<%s> child mode = %q, want %q", e.Name, e.Children.Mode, w)
		}
	}
}

// TestCatalogRecordsTheBehaviouralAxes checks the go/types half. These
// facts appear nowhere in the switch; they are interface satisfaction,
// resolved at generate time so no reflection is needed at runtime.
func TestCatalogRecordsTheBehaviouralAxes(t *testing.T) {
	nonVisual := map[string]bool{"Timer": true, "Tooltip": true, "Companion": true, "ValidationMarker": true}
	focusable := map[string]bool{"Button": true, "TextBox": true, "Tabs": true, "ItemsView": true, "Checkbox": true}
	for _, e := range BuiltinElements() {
		if nonVisual[e.Name] && !e.NonVisual {
			t.Errorf("<%s> should be NonVisual", e.Name)
		}
		if focusable[e.Name] && !e.Focusable {
			t.Errorf("<%s> should be Focusable", e.Name)
		}
	}
}

// TestCatalogUnionTagsEachSourceHonestly walks the three lifetimes in
// one call: the compiled-in built-ins, a host-registered Go builder
// (a NAME AND NOTHING ELSE), and a markup-only control (fully knowable
// through its <x:Property> declarations).
//
// The registered case is the honesty rule the whole design turns on. An
// empty Attrs list that means "unknown" must never be mistaken for one
// that means "none", which is why a consumer keys on AttrsKnown and not
// on Origin — those answer different questions and only happen to
// correlate today.
func TestCatalogUnionTagsEachSourceHonestly(t *testing.T) {
	ctx := &Context{Components: map[string]Builder{
		"LogPane": func(Element, *Context) (gooey.Component, error) { return nil, nil },
	}}
	ctx.Includes = fstest.MapFS{
		"card.gooey": &fstest.MapFile{Data: []byte(`<Gooey xmlns:x="wonderforge.io/gooey/x">
  <x:Property Name="Title" Type="string" Required="true"/>
  <x:Property Name="Count" Type="int" Default="3"/>
  <Text>{{.Title}}</Text>
</Gooey>`)},
	}
	by := map[string]ElementSpec{}
	for _, e := range ctx.Catalog() {
		by[e.Name] = e
	}

	if b := by["Button"]; b.Origin != OriginBuiltin || !b.AttrsKnown {
		t.Errorf("<Button> = %s/%v, want builtin and known", b.Origin, b.AttrsKnown)
	}

	log, ok := by["LogPane"]
	if !ok {
		t.Fatalf("the registered component is missing; got %v", keys(by))
	}
	if log.Origin != OriginRegistered {
		t.Errorf("<LogPane> origin = %s, want registered", log.Origin)
	}
	if log.AttrsKnown {
		t.Error("<LogPane> claims its attributes are known; a Builder is a func, so they cannot be")
	}
	if len(log.Attrs) != 0 {
		t.Errorf("<LogPane> reports %d attributes it cannot actually know", len(log.Attrs))
	}

	card, ok := by["Card"]
	if !ok {
		t.Fatalf("card.gooey did not become <Card>; got %v", keys(by))
	}
	if card.Origin != OriginInclude || !card.AttrsKnown {
		t.Errorf("<Card> = %s/%v, want include and known", card.Origin, card.AttrsKnown)
	}
	if len(card.Attrs) != 2 {
		t.Fatalf("<Card> has %d attrs, want 2 from its declarations", len(card.Attrs))
	}
	if card.Attrs[0].Name != "Title" || !card.Attrs[0].Required {
		t.Errorf("<Card Title> = %+v, want a required declaration", card.Attrs[0])
	}
	if card.Attrs[1].Kind != KindInt {
		t.Errorf("<Card Count> kind = %s, want int", card.Attrs[1].Kind)
	}
}

// TestValidateIsOpenAndAdoptsContextRules pins the finding that broke
// the tidy story: a BUILTIN element can still have a per-context
// attribute set. <Validate>'s vocabulary is validateBuiltins plus every
// name in Context.Rules, which is exactly what its load error already
// reports through validateRuleNames.
func TestValidateIsOpenAndAdoptsContextRules(t *testing.T) {
	for _, e := range BuiltinElements() {
		if e.Name != "Validate" {
			continue
		}
		if !e.Open {
			t.Error("<Validate> must be Open: Context.Rules extends it at runtime")
		}
		for _, a := range e.Attrs {
			if a.Name == "Required" {
				goto found
			}
		}
		t.Error("<Validate> lost its builtin rules")
	found:
	}

	ctx := &Context{}
	ctx.Rules = map[string]RuleFunc{"Iban": nil}
	for _, e := range ctx.Catalog() {
		if e.Name != "Validate" {
			continue
		}
		for _, a := range e.Attrs {
			if a.Name == "Iban" {
				if a.Origin != OriginRegistered {
					t.Errorf("<Validate Iban> origin = %s, want registered — a host rule is not a builtin one", a.Origin)
				}
				return
			}
		}
		t.Error("<Validate> did not adopt the Context.Rules entry")
	}
}

// TestUniversalAttrsCoverWhatEveryElementTakes pins the attribute set
// that belongs to no arm of the switch and is therefore invisible to a
// per-arm walk. For a visual builder these are the attributes its user
// touches most: the identity patch_markup addresses by, and size.
func TestUniversalAttrsCoverWhatEveryElementTakes(t *testing.T) {
	got := map[string]AttrSpec{}
	for _, a := range UniversalAttrs() {
		got[a.Name] = a
	}
	for _, n := range []string{"Name", "Tooltip", "Width", "Height", "Margin", "HAlign", "VAlign", "Visibility"} {
		if _, ok := got[n]; !ok {
			t.Errorf("universal attribute %s missing", n)
		}
	}
	// An attached property is NOT universal: it depends on the parent.
	for _, n := range []string{"Canvas.Left", "Grid.Row"} {
		if _, ok := got[n]; ok {
			t.Errorf("%q is attached, not universal; it is only valid under its own parent", n)
		}
	}
	if v := got["Visibility"]; v.Binds != BindsEither {
		t.Errorf("Visibility binds %q, want either — it is the one layout attribute that binds", v.Binds)
	}
}

// TestAttachedAttrsAreScopedToTheirParent is the rule a flat
// per-element attribute list cannot express, and the most direct
// instance of the principle this catalog is built on: Canvas.Left is
// meaningful on a child of a <Canvas> and nowhere else.
//
// Offering it elsewhere promises positioning that applyLayout silently
// discards — the same silent drop the catalog exists to delete, so a
// catalog that could not state this rule would reproduce the defect it
// was written to fix.
func TestAttachedAttrsAreScopedToTheirParent(t *testing.T) {
	if got := AttachedParents(); len(got) != 2 || got[0] != "Canvas" || got[1] != "Grid" {
		t.Fatalf("attached parents = %v, want [Canvas Grid]", got)
	}
	has := func(attrs []AttrSpec, name string) bool {
		for _, a := range attrs {
			if a.Name == name {
				return true
			}
		}
		return false
	}
	if !has(AttachedAttrs("Canvas"), "Canvas.Left") {
		t.Error("a <Canvas> must contribute Canvas.Left to its children")
	}
	if !has(AttachedAttrs("Grid"), "Grid.Row") {
		t.Error("a <Grid> must contribute Grid.Row to its children")
	}
	if len(AttachedAttrs("VStack")) != 0 {
		t.Error("a <VStack> contributes no attached properties")
	}

	var button ElementSpec
	for _, e := range BuiltinElements() {
		if e.Name == "Button" {
			button = e
		}
	}
	inCanvas := AttrsFor(button, "Canvas")
	inStack := AttrsFor(button, "VStack")
	if !has(inCanvas, "Canvas.Left") {
		t.Error("a Button inside a Canvas may set Canvas.Left")
	}
	if has(inStack, "Canvas.Left") {
		t.Error("a Button inside a VStack may NOT set Canvas.Left; applyLayout would drop it in silence")
	}
	// The universal set joins in for both, via HasLayout.
	for _, s := range [][]AttrSpec{inCanvas, inStack} {
		if !has(s, "Name") || !has(s, "Width") || !has(s, "Click") {
			t.Error("AttrsFor must union the element's own attributes with the universal set")
		}
	}
}

func keys(m map[string]ElementSpec) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
