package markup

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// card is the shape every test here instantiates: a markup-only control
// that declares its surface. %s is where each test drops its
// declarations.
func cardFS(decls, body string) fstest.MapFS {
	if body == "" {
		body = `<Text>{{.Title}}</Text>`
	}
	return fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey xmlns="wonderforge.io/gooey/2026" xmlns:x="` + XNamespace + `">
` + decls + `
  ` + body + `
</Gooey>`)},
	}
}

func loadPage(t *testing.T, fsys fstest.MapFS, page string, ctx *Context) (gooey.Component, error) {
	t.Helper()
	fsys["page.gooey"] = &fstest.MapFile{Data: []byte(page)}
	if ctx.Includes == nil {
		ctx.Includes = fsys
	}
	return Load(fsys, "page.gooey", ctx)
}

func TestDeclaredPropertyDefault(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Title" Type="string" Default="untitled"/>`, "")
	w, err := loadPage(t, fsys, `<Gooey><Card/></Gooey>`, &Context{Values: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if out := renderToString(t, w, 20, 1); !strings.Contains(out, "untitled") {
		t.Fatalf("declared default did not materialize:\n%s", out)
	}
}

// The default is a real source, per instance — declared markup state,
// not a copied literal, and not shared between instances.
func TestDeclaredDefaultIsPerInstanceSource(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Title" Type="string" Default="untitled"/>`, "")
	ctx := &Context{Values: map[string]any{}, Named: map[string]gooey.Component{}}
	page := `<Gooey><VStack><Card Name="a"/><Card Name="b"/></VStack></Gooey>`
	if _, err := loadPage(t, fsys, page, ctx); err != nil {
		t.Fatal(err)
	}
	a, err := Find[*components.Text](ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Find[*components.Text](ctx, "b")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two instances share one component")
	}
}

func TestDeclaredPropertyBoundHandlePassesThrough(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Title" Type="string"/>`, "")
	title := prop.NewSource("live")
	ctx := &Context{Values: map[string]any{"Header": title}}
	w, err := loadPage(t, fsys, `<Gooey><Card Title="{{.Header}}"/></Gooey>`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out := renderToString(t, w, 20, 1); !strings.Contains(out, "live") {
		t.Fatalf("bound value did not reach the control:\n%s", out)
	}
	// The parent's node passed through — it was not copied.
	title.Set("changed")
	if out := renderToString(t, w, 20, 1); !strings.Contains(out, "changed") {
		t.Fatalf("declared property is not the parent's live handle:\n%s", out)
	}
}

// Every declared type coerces its literal into a handle of that exact
// type — the whole type table in one pass, asserted on the handles a
// control's context actually receives.
func TestDeclaredPropertyLiteralIsCoerced(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Label" Type="string"/>
  <x:Property Name="Count" Type="int"/>
  <x:Property Name="Ratio" Type="float"/>
  <x:Property Name="On" Type="bool"/>
  <x:Property Name="Every" Type="duration"/>
  <x:Property Name="Tint" Type="color"/>
  <Text>{{.Label}}</Text>
</Gooey>`)},
	}
	var got map[string]any
	setup := func(e Element, parent *Context) (*Context, error) {
		got = parent.DeclaredProperties()
		return &Context{}, nil
	}
	ctx := &Context{
		Values:     map[string]any{},
		Components: map[string]Builder{"Card": UserControl(fsys, "card.gooey", setup)},
	}
	page := `<Gooey><Card Label="hi" Count="7" Ratio="1.5" On="true" Every="600ms" Tint="#ff8800"/></Gooey>`
	if _, err := loadPage(t, fsys, page, ctx); err != nil {
		t.Fatal(err)
	}
	assertHandle(t, got, "Label", "hi")
	assertHandle(t, got, "Count", 7)
	assertHandle(t, got, "Ratio", 1.5)
	assertHandle(t, got, "On", true)
	assertHandle(t, got, "Every", 600*time.Millisecond)
	assertHandle(t, got, "Tint", render.RGB(0xff, 0x88, 0x00))
}

// Absent attributes materialize the declared default, typed the same way.
func TestDeclaredDefaultsAreTypedSources(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Count" Type="int" Default="42"/>
  <x:Property Name="Every" Type="duration" Default="1s"/>
  <x:Property Name="Tint" Type="color" Default="#08f"/>
  <x:Property Name="Bare" Type="int"/>
  <Text>x</Text>
</Gooey>`)},
	}
	var got map[string]any
	setup := func(e Element, parent *Context) (*Context, error) {
		got = parent.DeclaredProperties()
		return &Context{}, nil
	}
	ctx := &Context{
		Values:     map[string]any{},
		Components: map[string]Builder{"Card": UserControl(fsys, "card.gooey", setup)},
	}
	if _, err := loadPage(t, fsys, `<Gooey><Card/></Gooey>`, ctx); err != nil {
		t.Fatal(err)
	}
	assertHandle(t, got, "Count", 42)
	assertHandle(t, got, "Every", time.Second)
	assertHandle(t, got, "Tint", render.RGB(0x00, 0x88, 0xff))
	// No Default is the type's zero value, not a nil handle.
	assertHandle(t, got, "Bare", 0)
}

func assertHandle[T comparable](t *testing.T, vals map[string]any, name string, want T) {
	t.Helper()
	h, ok := vals[name].(*prop.Property[T])
	if !ok {
		t.Fatalf("%s is %T; want *prop.Property[%T]", name, vals[name], want)
	}
	if got := h.Get(); got != want {
		t.Fatalf("%s = %v; want %v", name, got, want)
	}
}

func TestDeclaredPropertyUncoercibleLiteralErrors(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Count" Type="int"/>`, `<Text>x</Text>`)
	_, err := loadPage(t, fsys, `<Gooey><Card Count="seven"/></Gooey>`, &Context{Values: map[string]any{}})
	assertErrContains(t, err, `dependency property "Count"`, `is not a int`)
}

func TestDeclaredPropertyTypeMismatchErrors(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Title" Type="string"/>`, "")
	ctx := &Context{Values: map[string]any{"N": prop.NewSource(3)}}
	_, err := loadPage(t, fsys, `<Gooey><Card Title="{{.N}}"/></Gooey>`, ctx)
	assertErrContains(t, err, `dependency property "Title"`, `*prop.Property[int]`, `*prop.Property[string]`)
}

func TestDeclaredPropertyRequiredMissingErrors(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Title" Type="string" Required="true"/>`, "")
	_, err := loadPage(t, fsys, `<Gooey><Card/></Gooey>`, &Context{Values: map[string]any{}})
	assertErrContains(t, err, "card.gooey", `dependency property "Title"`, "required attribute missing")
}

// Strict mode: declaring a surface makes a typo a load error rather than
// an attribute that silently does nothing.
func TestStrictModeRejectsUndeclaredAttribute(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Title" Type="string" Default="untitled"/>`, "")
	_, err := loadPage(t, fsys, `<Gooey><Card Titel="oops"/></Gooey>`, &Context{Values: map[string]any{}})
	assertErrContains(t, err, "card.gooey", `no dependency property "Titel"`, "declared: Title")
}

// Layout and Name belong to the ELEMENT, not to the control, so strict
// mode must not reject them.
func TestStrictModeAllowsLayoutAndName(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Title" Type="string" Default="untitled"/>`, "")
	page := `<Gooey><Grid Rows="*" Cols="*"><Card Name="c" Grid.Row="0" Margin="1" Width="8"/></Grid></Gooey>`
	if _, err := loadPage(t, fsys, page, &Context{Values: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
}

// No declarations at all is the pre-existing tier, untouched.
func TestUndeclaredControlKeepsPassThrough(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey><Text>{{.Anything}}</Text></Gooey>`)},
	}
	w, err := loadPage(t, fsys, `<Gooey><Card Anything="loose"/></Gooey>`, &Context{Values: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if out := renderToString(t, w, 20, 1); !strings.Contains(out, "loose") {
		t.Fatalf("pass-through tier regressed:\n%s", out)
	}
}

func TestDeclarationParseErrors(t *testing.T) {
	cases := []struct {
		name  string
		decl  string
		wants []string
	}{
		{"no name", `<x:Property Type="string"/>`, []string{"needs a Name"}},
		{"no type", `<x:Property Name="T"/>`, []string{`dependency property "T"`, "needs a Type"}},
		{"unknown type", `<x:Property Name="T" Type="widget"/>`, []string{"unknown Type", "widget"}},
		{"unknown attr", `<x:Property Name="T" Type="string" Defualt="x"/>`, []string{`no attribute "Defualt"`}},
		{"bad default", `<x:Property Name="T" Type="int" Default="x"/>`, []string{`Default="x" is not a int`}},
		{"required and default", `<x:Property Name="T" Type="int" Default="1" Required="true"/>`, []string{"exclusive"}},
		{"any with default", `<x:Property Name="T" Type="any" Default="x"/>`, []string{`no literal syntax`}},
		{"reserved name", `<x:Property Name="Margin" Type="string"/>`, []string{"reserved"}},
		{"duplicate", `<x:Property Name="T" Type="int"/><x:Property Name="T" Type="int"/>`, []string{"declared twice"}},
		{"unknown language element", `<x:Member Name="T"/>`, []string{"unknown language element"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := cardFS("  "+c.decl, `<Text>x</Text>`)
			_, err := loadPage(t, fsys, `<Gooey><Card/></Gooey>`, &Context{Values: map[string]any{}})
			assertErrContains(t, err, c.wants...)
		})
	}
}

// The prefix must be declared: <Property> without xmlns:x would
// otherwise be read as a component and fail with "unknown element".
func TestDeclarationNeedsTheXNamespace(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey><Property Name="T" Type="string"/><Text>x</Text></Gooey>`)},
	}
	_, err := loadPage(t, fsys, `<Gooey><Card/></Gooey>`, &Context{Values: map[string]any{}})
	assertErrContains(t, err, "<x:Property>", XNamespace)
}

func TestDeclarationMustBeRootChild(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <VStack><x:Property Name="T" Type="string"/><Text>x</Text></VStack>
</Gooey>`)},
	}
	_, err := loadPage(t, fsys, `<Gooey><Card/></Gooey>`, &Context{Values: map[string]any{}})
	assertErrContains(t, err, "direct child of the root")
}

// `any` is the escape hatch: an app type crosses the boundary unchecked.
func TestDeclaredAnyAcceptsAppTypes(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Palette" Type="any" Required="true"/>
  <ColorPicker Value="{{.Palette}}"/>
</Gooey>`)},
	}
	ctx := &Context{Values: map[string]any{"C": prop.NewSource(render.RGB(1, 2, 3))}}
	if _, err := loadPage(t, fsys, `<Gooey><Card Palette="{{.C}}"/></Gooey>`, ctx); err != nil {
		t.Fatal(err)
	}
}

// A declared property still crosses one more boundary as a handle: the
// card's own declared Title reaches a nested control's declared Text.
func TestDeclaredPropertiesNest(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Caption" Type="string" Default="no caption"/>
  <Badge Text="{{.Caption}}"/>
</Gooey>`)},
		"badge.gooey": {Data: []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Text" Type="string" Required="true"/>
  <Text>◈ {{.Text}}</Text>
</Gooey>`)},
	}
	w, err := loadPage(t, fsys, `<Gooey><Card Caption="per second"/></Gooey>`, &Context{Values: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if out := renderToString(t, w, 20, 1); !strings.Contains(out, "per second") {
		t.Fatalf("declared literal did not reach the nested control:\n%s", out)
	}
}

// Code-behind tier: setup EXTENDS the declared surface, reading declared
// handles to build private computeds.
func TestCodeBehindExtendsDeclarations(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Title" Type="string" Required="true"/>
  <Text>{{.Shout}}</Text>
</Gooey>`)},
	}
	setup := func(e Element, parent *Context) (*Context, error) {
		title, ok := parent.DeclaredProperties()["Title"].(*prop.Property[string])
		if !ok {
			t.Fatalf("setup did not receive the declared handle: %#v", parent.DeclaredProperties())
		}
		return &Context{Values: map[string]any{
			"Shout": prop.NewComputed(func() string { return strings.ToUpper(title.Get()) }),
		}}, nil
	}
	ctx := &Context{
		Values:     map[string]any{},
		Components: map[string]Builder{"Card": UserControl(fsys, "card.gooey", setup)},
	}
	w, err := loadPage(t, fsys, `<Gooey><Card Title="quiet"/></Gooey>`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out := renderToString(t, w, 20, 1); !strings.Contains(out, "QUIET") {
		t.Fatalf("code-behind computed over a declared handle did not render:\n%s", out)
	}
}

func TestCodeBehindCollidingWithDeclarationErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Title" Type="string" Default="untitled"/>
  <Text>{{.Title}}</Text>
</Gooey>`)},
	}
	setup := func(e Element, parent *Context) (*Context, error) {
		return &Context{Values: map[string]any{"Title": prop.NewSource("mine")}}, nil
	}
	ctx := &Context{
		Values:     map[string]any{},
		Components: map[string]Builder{"Card": UserControl(fsys, "card.gooey", setup)},
	}
	_, err := loadPage(t, fsys, `<Gooey><Card/></Gooey>`, ctx)
	assertErrContains(t, err, `dependency property "Title"`, "declarations own the control's public surface")
}

// A code-behind control that declares nothing behaves exactly as before,
// including seeing a nil DeclaredProperties.
func TestCodeBehindWithoutDeclarationsUnchanged(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey><Text>{{.Mine}}</Text></Gooey>`)},
	}
	setup := func(e Element, parent *Context) (*Context, error) {
		if d := parent.DeclaredProperties(); len(d) != 0 {
			t.Fatalf("undeclared control saw declarations: %#v", d)
		}
		return &Context{Values: map[string]any{"Mine": "own context"}}, nil
	}
	ctx := &Context{
		Values:     map[string]any{},
		Components: map[string]Builder{"Card": UserControl(fsys, "card.gooey", setup)},
	}
	w, err := loadPage(t, fsys, `<Gooey><Card Ignored="x"/></Gooey>`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out := renderToString(t, w, 20, 1); !strings.Contains(out, "own context") {
		t.Fatalf("code-behind tier regressed:\n%s", out)
	}
}

// Declared defaults materialize fresh sources on every instantiation, so
// a hot reload of the page resets them. This is the known wrinkle
// recorded in the spec, pinned here so the behavior is a decision rather
// than a surprise; the fix is Name-keyed state adoption, not a change
// here.
func TestDeclaredDefaultResetsOnRebuild(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Title" Type="string" Default="untitled"/>`, "")
	ctx := &Context{Values: map[string]any{}, Named: map[string]gooey.Component{}}
	page := &fstest.MapFile{Data: []byte(`<Gooey><Card Name="c"/></Gooey>`)}
	fsys["page.gooey"] = page
	ctx.Includes = fsys

	first, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out := renderToString(t, first, 20, 1); !strings.Contains(out, "untitled") {
		t.Fatalf("default missing:\n%s", out)
	}

	ctx.Named = map[string]gooey.Component{}
	second, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("rebuild returned the same tree")
	}
	if out := renderToString(t, second, 20, 1); !strings.Contains(out, "untitled") {
		t.Fatalf("rebuilt default missing:\n%s", out)
	}
}

func assertErrContains(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error containing %q", wants)
	}
	for _, w := range wants {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("error %q does not contain %q", err, w)
		}
	}
}

// The declared-surface registry: every control instance built with
// declarations lands in Context.Declared, keyed by its root component,
// with its resolved handles — including instances nested inside other
// controls, because the registry is page-wide where Named is per
// instance. This is the seam an inspection surface (the MCP tree
// snapshot) reads a control's property schema from.
func TestDeclaredRegistryRecordsInstances(t *testing.T) {
	fsys := fstest.MapFS{
		"card.gooey": {Data: []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Title" Type="string" Default="untitled"/>
  <x:Property Name="Count" Type="int" Default="3"/>
  <VStack><Text>{{.Title}}</Text><Badge/></VStack>
</Gooey>`)},
		"badge.gooey": {Data: []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Tag" Type="string" Default="new"/>
  <Text>{{.Tag}}</Text>
</Gooey>`)},
	}
	ctx := &Context{Values: map[string]any{}, Named: map[string]gooey.Component{}}
	page := `<Gooey><Card Name="c" Title="hello"/></Gooey>`
	if _, err := loadPage(t, fsys, page, ctx); err != nil {
		t.Fatal(err)
	}
	if len(ctx.Declared) != 2 {
		t.Fatalf("Declared has %d entries, want the Card and its nested Badge", len(ctx.Declared))
	}
	card := ctx.Named["c"]
	ds, ok := ctx.Declared[card]
	if !ok {
		t.Fatal("the Card instance is not keyed by its root component")
	}
	if ds.Control != "card.gooey" {
		t.Errorf("Control = %q, want card.gooey", ds.Control)
	}
	byName := map[string]DeclaredProp{}
	for _, p := range ds.Props {
		byName[p.Name] = p
	}
	title, ok := byName["Title"].Handle.(*prop.Property[string])
	if !ok {
		t.Fatalf("Title handle is %T", byName["Title"].Handle)
	}
	if title.Get() != "hello" {
		t.Errorf("Title = %q, want the instantiation-site literal", title.Get())
	}
	if byName["Count"].Type != "int" {
		t.Errorf("Count declared Type = %q", byName["Count"].Type)
	}
	count, ok := byName["Count"].Handle.(*prop.Property[int])
	if !ok || count.Get() != 3 {
		t.Errorf("Count handle = %T, want an int source carrying the default", byName["Count"].Handle)
	}
	// The nested Badge is visible from the PAGE context.
	found := false
	for _, s := range ctx.Declared {
		if s.Control == "badge.gooey" {
			found = true
		}
	}
	if !found {
		t.Error("the nested Badge instance did not reach the page-wide registry")
	}
}

// TestTheXPropertyRefusalNamesTheRoot is the pin nothing carried.
//
// `grep -rn "dependency property declaration"` answered only the Errorf
// itself, so neither the message nor a change to it was under test —
// unlike its two siblings, which values_test.go and handlers_test.go
// both reach. Review of #501 reworded all three to "an element of this
// document" and this one regressed, silently.
//
// AN ELEMENT PREFIX IS NOT A VALUE-EXPRESSION PREFIX, which is why the
// three do not share a wording. handlers.go and values.go resolve a
// prefix inside an attribute VALUE through ctx.ns — flat and
// document-wide, so any element may carry the declaration, which is what
// TestAPrefixDeclaredBelowTheRootIsDocumentWide measures. `x:` prefixes
// an ELEMENT, resolved by encoding/xml with real subtree scoping before
// this package sees it, and <x:Property> must be a direct child of the
// root — so <Gooey> is the only element whose declaration is in scope.
// The second arm here is the advice, followed: it must not come back
// with the same refusal.
func TestTheXPropertyRefusalNamesTheRoot(t *testing.T) {
	const unprefixed = `<Gooey>
  <Property Name="Count" Type="int" Default="1"/>
  <Text>x</Text>
</Gooey>`
	_, err := Build([]byte(unprefixed), &Context{})
	if err == nil {
		t.Fatal("an unprefixed <Property> loaded, so the refusal this test is " +
			"about never fired")
	}
	const want = `add xmlns:x="` + XNamespace + `" to the <Gooey> root element`
	if !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal reads\n\t%v\nwant it to carry\n\t%s\n"+
			"Naming any other element is advice that does not work: see the "+
			"arm below", err, want)
	}

	// THE ADVICE, FOLLOWED LITERALLY, on the element the reworded
	// message would have sent the author to.
	const below = `<Gooey>
  <x:Property Name="Count" Type="int" Default="1"/>
  <Text xmlns:x="` + XNamespace + `">x</Text>
</Gooey>`
	if _, err := Build([]byte(below), &Context{}); err == nil ||
		!strings.Contains(err.Error(), "dependency property declaration") {
		t.Errorf("declaring xmlns:x below the root gave %v; want the SAME "+
			"refusal, because an element prefix is subtree-scoped and the "+
			"<x:Property> above it is still unresolved. If this ever loads, the "+
			"asymmetry documented here is gone and the message may widen", err)
	}

	// AND THE TWO PLACEMENTS THAT DO RESOLVE — so the arm above is about
	// SCOPE and not about the declaration being rejected outright.
	//
	// THE SECOND OF THESE IS THE CORRECTION. This test had the root arm
	// only, and the comment beside it read "the root is the only element
	// whose declaration is in scope" — measured against the sibling case
	// alone, which cannot tell "only the root" from "in scope at the
	// element". XML scoping includes an element's OWN attributes, so
	// <x:Property xmlns:x="…"/> resolves as well. The refusal's advice
	// still names the root, because that is where every example puts it
	// and where one declaration serves every declaration below — but the
	// RULE is scope, and docs/markup-reference.md says so now. Raised in
	// review of #501.
	for _, tc := range []struct{ name, doc string }{
		{"on the root", `<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Count" Type="int" Default="1"/>
  <Text>x</Text>
</Gooey>`},
		{"on the x:Property itself", `<Gooey>
  <x:Property xmlns:x="` + XNamespace + `" Name="Count" Type="int" Default="1"/>
  <Text>x</Text>
</Gooey>`},
	} {
		// ANY error, not only the refusal. This asked whether the error
		// was the <Property> one and let every other failure through —
		// so a fixture typo, or any future change that breaks this
		// document for an unrelated reason, would leave the arm green
		// while it had stopped measuring that the declaration RESOLVES.
		// That matters here specifically: this arm is the correction to
		// a claim that was itself measured against too narrow a case,
		// and a correction that cannot fail is not one. Both fixtures
		// build with err == nil today, so nothing is lost by asking for
		// it. Raised in review of #501.
		_, err := Build([]byte(tc.doc), &Context{})
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), "dependency property declaration") {
			t.Errorf("xmlns:x %s is still refused as an unprefixed <Property>: %v\n"+
				"Both placements put the declaration in scope at the <x:Property>, "+
				"which is what element-prefix resolution asks", tc.name, err)
			continue
		}
		t.Errorf("xmlns:x %s did not build: %v\nThis arm exists to show the "+
			"declaration RESOLVES, so any failure retires it — including one "+
			"that has nothing to do with namespaces", tc.name, err)
	}
}

// TestADeclarationMakesTheHandleAnAbsentAttributeWouldGet pins
// NewValue against the arm of resolve it is the public spelling of: a
// caller with no instantiation site gets what an omitted optional
// attribute gets, and gets it from the same table row.
//
// Derived from Declarations rather than from a literal Declaration,
// because a Declaration built here would carry no type table row and
// the last case below is the one that proves that matters.
func TestADeclarationMakesTheHandleAnAbsentAttributeWouldGet(t *testing.T) {
	src := `<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Title" Type="string" Default="hi"/>
  <x:Property Name="Count" Type="int"/>
  <x:Property Name="Who" Type="string" Required="true"/>
  <x:Property Name="Tint" Type="style"/>
  <Text Text="{{.Title}}"/>
</Gooey>
`
	decls, err := Declarations([]byte(src))
	if err != nil {
		t.Fatalf("Declarations: %v", err)
	}
	by := map[string]Declaration{}
	for _, d := range decls {
		by[d.Name] = d
	}

	// Default is the value, and a type with no Default — Required or
	// not — is the type's zero, never an error.
	title, err := by["Title"].NewValue()
	if err != nil {
		t.Fatalf("Title: %v", err)
	}
	p, ok := title.(*prop.Property[string])
	if !ok {
		t.Fatalf("Title is %T, want *prop.Property[string]", title)
	}
	if got := p.Get(); got != "hi" {
		t.Errorf("Title = %q, want %q — Default is what an absent attribute resolves to", got, "hi")
	}
	count, err := by["Count"].NewValue()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if got := count.(*prop.Property[int]).Get(); got != 0 {
		t.Errorf("Count = %d, want 0", got)
	}
	// Required has no Default and cannot have one (parseDeclaration
	// refuses the pair), so the zero handle is the only thing left. It
	// is deliberately not an error: Required is a contract with an
	// instantiation site, and a caller holding the definition alone has
	// not broken it.
	who, err := by["Who"].NewValue()
	if err != nil {
		t.Fatalf("Who is Required, and NewValue must not treat that as a breach: %v", err)
	}
	if got := who.(*prop.Property[string]).Get(); got != "" {
		t.Errorf("Who = %q, want the zero string", got)
	}
	// Bind-only follows the same rule it already follows for an absent
	// attribute: the zero handle, not a refusal.
	tint, err := by["Tint"].NewValue()
	if err != nil {
		t.Fatalf("Tint: %v", err)
	}
	if _, ok := tint.(*prop.Property[render.Style]); !ok {
		t.Fatalf("Tint is %T, want *prop.Property[render.Style]", tint)
	}

	// PER CALL, not per declaration. Two hosts previewing the same
	// control file must not write through each other's handle, which is
	// the rule resolve states for two <Card/> elements.
	a, _ := by["Title"].NewValue()
	b, _ := by["Title"].NewValue()
	if a == b {
		t.Error("two NewValue calls returned the same handle; a declaration's value is per-instance")
	}

	// A Declaration the caller built themselves names a Type and
	// carries no row for it. Refused by name rather than panicking on a
	// nil closure inside this package.
	if _, err := (Declaration{Name: "Made", Type: "string"}).NewValue(); err == nil {
		t.Error("a Declaration that never came from a parse must not resolve")
	} else if !strings.Contains(err.Error(), "Made") || !strings.Contains(err.Error(), "type table row") {
		t.Errorf("error is %q, want it to name the property and say why", err)
	}
}

// TestSeedingDeclaredDefaultsBuildsTheDefiningDocument is the reason
// NewValue is exported, end to end: the document that DECLARES a
// property and then binds it has no instantiation site, so nothing
// fills Values and the body's own binding is a load error. Seeded, it
// builds.
//
// Build, not Include — an Include has a site, and it is the absence of
// one that this pins.
func TestSeedingDeclaredDefaultsBuildsTheDefiningDocument(t *testing.T) {
	src := `<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Title" Type="string" Default="hi"/>
  <Text>{{.Title}}</Text>
</Gooey>
`
	// The premise: unseeded, this is the failure the editor reported.
	if _, err := Build([]byte(src), &Context{}); err == nil {
		t.Fatal("a defining document built without its declarations seeded; " +
			"if top-level Build now instantiates declarations, this test and NewValue's reason for existing both need revisiting")
		// The MESSAGE, not just "an error": a fixture that fails to
		// build for an unrelated reason — a misspelled attribute, say —
		// would satisfy a bare err != nil and prove nothing about
		// declarations at all.
	} else if !strings.Contains(err.Error(), `"Title" not found in context`) {
		t.Fatalf("unseeded build failed with %v, want the unresolved binding", err)
	}

	decls, err := Declarations([]byte(src))
	if err != nil {
		t.Fatalf("Declarations: %v", err)
	}
	ctx := &Context{Values: map[string]any{}}
	for _, d := range decls {
		v, err := d.NewValue()
		if err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		ctx.Values[d.Name] = v
	}
	root, err := Build([]byte(src), ctx)
	if err != nil {
		t.Fatalf("seeded build: %v", err)
	}
	txt, ok := root.(*components.Text)
	if !ok {
		t.Fatalf("root is %T, want *components.Text", root)
	}
	if got := txt.Content.Get(); got != "hi" {
		t.Errorf("Text = %q, want the declared Default %q", got, "hi")
	}
}
