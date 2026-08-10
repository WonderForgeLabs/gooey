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
