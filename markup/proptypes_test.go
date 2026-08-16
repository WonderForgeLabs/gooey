package markup

import (
	"image"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The bind-only rows of propKinds — style, image, series — are a type a
// control can DECLARE and have type-checked but cannot spell in an
// attribute. Their whole value is the load error, so every positive here
// has a near-miss twin: bind the wrong handle, write a literal, write a
// Default. A suite of positives alone passes against an implementation
// that accepts everything, which is the exact hole these rows close.

// declaredSurface returns the resolved declarations of the control whose
// root component is Named `name`. It is the only way to see the handle
// an instance actually got, as opposed to what it rendered.
func declaredSurface(t *testing.T, ctx *Context, name string) DeclaredSurface {
	t.Helper()
	root, ok := ctx.Named[name]
	if !ok {
		t.Fatalf("no component named %q", name)
	}
	ds, ok := ctx.Declared[root]
	if !ok {
		t.Fatalf("%q has no declared surface", name)
	}
	return ds
}

func declaredHandle(t *testing.T, ctx *Context, name, declared string) any {
	t.Helper()
	for _, p := range declaredSurface(t, ctx, name).Props {
		if p.Name == declared {
			return p.Handle
		}
	}
	t.Fatalf("%q declares no %q", name, declared)
	return nil
}

// --- style ----------------------------------------------------------

// A style crosses the boundary as the parent's own live handle: the
// point of the row is that a declared style stays REACTIVE, so identity
// (not equality) is the assertion.
func TestDeclaredStyleIsTheParentsLiveHandle(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Tint" Type="style" Required="true"/>`,
		`<Text Style="{{.Tint}}">hi</Text>`)
	accent := prop.NewSource(render.Style{Bold: true})
	ctx := &Context{
		Values: map[string]any{"Accent": accent},
		Named:  map[string]gooey.Component{},
	}
	if _, err := loadPage(t, fsys, `<Gooey><Card Name="c" Tint="{{.Accent}}"/></Gooey>`, ctx); err != nil {
		t.Fatal(err)
	}
	if got := declaredHandle(t, ctx, "c", "Tint"); got != any(accent) {
		t.Fatalf("Tint handle is %T (%p); want the parent's own node %p", got, got, accent)
	}
}

// The near-miss twin: a handle of the wrong type is refused, and the
// error names the element the PAGE author wrote plus both types.
func TestDeclaredStyleRejectsAWrongHandle(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Tint" Type="style" Required="true"/>`,
		`<Text Style="{{.Tint}}">hi</Text>`)
	ctx := &Context{Values: map[string]any{"Label": prop.NewSource("accent")}}
	_, err := loadPage(t, fsys, `<Gooey><Card Tint="{{.Label}}"/></Gooey>`, ctx)
	assertErrContains(t, err, "card.gooey", `dependency property "Tint"`,
		`<Card Tint="{{.Label}}">`, "*prop.Property[string]",
		`Type="style"`, "render.Style]")
}

// A literal is refused even when it names a style that IS registered —
// the refusal is about WHERE the name could be checked, not about
// whether this particular page happens to have it.
func TestDeclaredStyleLiteralIsALoadError(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Tint" Type="style"/>`,
		`<Text Style="{{.Tint}}">hi</Text>`)
	ctx := &Context{
		Values: map[string]any{},
		Styles: map[string]render.Style{"accent": {Bold: true}},
	}
	_, err := loadPage(t, fsys, `<Gooey><Card Tint="accent"/></Gooey>`, ctx)
	assertErrContains(t, err, "card.gooey", `dependency property "Tint"`,
		`<Card Tint="accent">`, `Type="style" has no literal syntax`,
		`pass a handle, as Tint="{{.Something}}"`)
}

// --- series ---------------------------------------------------------

// The motivating case, straight through: a declared series feeds the
// component that plots it.
func TestDeclaredSeriesReachesSparkline(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Trend" Type="series" Required="true"/>`,
		`<Sparkline Values="{{.Trend}}"/>`)
	points := prop.NewSource([]float64{1, 2, 3})
	ctx := &Context{
		Values: map[string]any{"Points": points},
		Named:  map[string]gooey.Component{},
	}
	if _, err := loadPage(t, fsys, `<Gooey><Card Name="c" Trend="{{.Points}}"/></Gooey>`, ctx); err != nil {
		t.Fatal(err)
	}
	if got := declaredHandle(t, ctx, "c", "Trend"); got != any(points) {
		t.Fatalf("Trend handle is %T; want the parent's own node", got)
	}
}

// The near-miss twin, and the bug the row exists for: <Card
// Trend="{{.Title}}"/> used to load clean under Type="any" and fail one
// level down inside <Sparkline Values>, naming an element the page's
// author never wrote. The error must name <Card>, and must NOT send the
// reader to the Sparkline.
func TestDeclaredSeriesRejectsAStringHandleAtTheCard(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Trend" Type="series" Required="true"/>`,
		`<Sparkline Values="{{.Trend}}"/>`)
	ctx := &Context{Values: map[string]any{"Title": prop.NewSource("hello")}}
	_, err := loadPage(t, fsys, `<Gooey><Card Trend="{{.Title}}"/></Gooey>`, ctx)
	assertErrContains(t, err, `dependency property "Trend"`,
		`<Card Trend="{{.Title}}">`, "*prop.Property[string]",
		`Type="series"`, "*prop.Property[[]float64]")
	for _, wrong := range []string{"Sparkline", "Values"} {
		if strings.Contains(err.Error(), wrong) {
			t.Fatalf("error names %q, sending the reader into a file they did not edit:\n%v", wrong, err)
		}
	}
}

// A comma list is the literal somebody will reach for first; it is a
// load error rather than a second, looser spelling of what <Sparkline
// Values> already refuses.
func TestDeclaredSeriesLiteralIsALoadError(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Trend" Type="series"/>`,
		`<Sparkline Values="{{.Trend}}"/>`)
	_, err := loadPage(t, fsys, `<Gooey><Card Trend="1,2,3"/></Gooey>`, &Context{Values: map[string]any{}})
	assertErrContains(t, err, `dependency property "Trend"`,
		`<Card Trend="1,2,3">`, `Type="series" has no literal syntax`,
		`pass a handle, as Trend="{{.Something}}"`)
}

// --- image ----------------------------------------------------------

func TestDeclaredImageReachesImageElement(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Pic" Type="image" Required="true"/>`,
		`<Image Src="{{.Pic}}" Cols="4" Rows="2"/>`)
	pic := prop.NewSource[image.Image](image.NewRGBA(image.Rect(0, 0, 2, 2)))
	ctx := &Context{
		Values: map[string]any{"Logo": pic},
		Named:  map[string]gooey.Component{},
	}
	if _, err := loadPage(t, fsys, `<Gooey><Card Name="c" Pic="{{.Logo}}"/></Gooey>`, ctx); err != nil {
		t.Fatal(err)
	}
	if got := declaredHandle(t, ctx, "c", "Pic"); got != any(pic) {
		t.Fatalf("Pic handle is %T; want the parent's own node", got)
	}
}

func TestDeclaredImageRejectsAWrongHandle(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Pic" Type="image" Required="true"/>`,
		`<Image Src="{{.Pic}}" Cols="4" Rows="2"/>`)
	ctx := &Context{Values: map[string]any{"Path": prop.NewSource("logo.png")}}
	_, err := loadPage(t, fsys, `<Gooey><Card Pic="{{.Path}}"/></Gooey>`, ctx)
	assertErrContains(t, err, `dependency property "Pic"`,
		`<Card Pic="{{.Path}}">`, "*prop.Property[string]",
		`Type="image"`, "*prop.Property[image.Image]")
}

// A path literal on a DECLARED image is refused, even though <Image
// Src="logo.png"> is legal one line down — the difference is that the
// Image element IS the page, and the declaration is not.
func TestDeclaredImageLiteralIsALoadError(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Pic" Type="image"/>`, `<Image Src="{{.Pic}}" Cols="4" Rows="2"/>`)
	_, err := loadPage(t, fsys, `<Gooey><Card Pic="logo.png"/></Gooey>`, &Context{Values: map[string]any{}})
	assertErrContains(t, err, `dependency property "Pic"`,
		`<Card Pic="logo.png">`, `Type="image" has no literal syntax`,
		`pass a handle, as Pic="{{.Something}}"`)
}

// --- the rule itself, across all three rows --------------------------

// A Default on a bind-only declaration is a defect in the CONTROL, so it
// fails when the control loads — on every page, not on whichever page
// happened to omit the attribute. The empty case is the near-miss: the
// check is on the attribute being WRITTEN, not on it being non-empty.
func TestBindOnlyDefaultIsALoadError(t *testing.T) {
	for _, typ := range []string{"style", "image", "series"} {
		for _, def := range []string{"something", ""} {
			t.Run(typ+"/"+def, func(t *testing.T) {
				decl := `  <x:Property Name="P" Type="` + typ + `" Default="` + def + `"/>`
				fsys := cardFS(decl, `<Text>x</Text>`)
				_, err := loadPage(t, fsys, `<Gooey><Card/></Gooey>`, &Context{Values: map[string]any{}})
				assertErrContains(t, err, `dependency property "P"`,
					`Type="`+typ+`" has no literal syntax, so it takes no Default`,
					"it crosses a control boundary as a handle, never as text",
					"bind it or mark it Required",
					"absent means the zero "+typ)
			})
		}
	}
}

// The three-way rule is unchanged by bind-only: an absent OPTIONAL
// attribute still materializes the type's zero handle, not a nil that
// panics one level down.
func TestBindOnlyAbsentOptionalIsTheZeroHandle(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Tint" Type="style"/>
  <x:Property Name="Pic" Type="image"/>
  <x:Property Name="Trend" Type="series"/>`, `<Text>x</Text>`)
	ctx := &Context{Values: map[string]any{}, Named: map[string]gooey.Component{}}
	if _, err := loadPage(t, fsys, `<Gooey><Card Name="c"/></Gooey>`, ctx); err != nil {
		t.Fatal(err)
	}
	tint, ok := declaredHandle(t, ctx, "c", "Tint").(*prop.Property[render.Style])
	if !ok || tint.Get() != (render.Style{}) {
		t.Errorf("Tint = %#v; want a source holding the zero Style", declaredHandle(t, ctx, "c", "Tint"))
	}
	pic, ok := declaredHandle(t, ctx, "c", "Pic").(*prop.Property[image.Image])
	if !ok || pic.Get() != nil {
		t.Errorf("Pic = %#v; want a source holding a nil image", declaredHandle(t, ctx, "c", "Pic"))
	}
	trend, ok := declaredHandle(t, ctx, "c", "Trend").(*prop.Property[[]float64])
	if !ok || trend.Get() != nil {
		t.Errorf("Trend = %#v; want a source holding a nil series", declaredHandle(t, ctx, "c", "Trend"))
	}
}

// Required is orthogonal to bind-only: an absent required attribute is
// the ordinary missing-attribute error, not the literal-syntax one.
func TestBindOnlyRequiredMissingErrors(t *testing.T) {
	for _, typ := range []string{"style", "image", "series"} {
		t.Run(typ, func(t *testing.T) {
			fsys := cardFS(`  <x:Property Name="P" Type="`+typ+`" Required="true"/>`, `<Text>x</Text>`)
			_, err := loadPage(t, fsys, `<Gooey><Card/></Gooey>`, &Context{Values: map[string]any{}})
			assertErrContains(t, err, "card.gooey", `dependency property "P"`,
				"required attribute missing on <Card>")
			if strings.Contains(err.Error(), "literal syntax") {
				t.Fatalf("an absent attribute reported as a literal problem:\n%v", err)
			}
		})
	}
}

// The new rows are part of the vocabulary an author is shown when they
// misspell a type — a row nobody can discover is a row nobody uses.
func TestUnknownTypeErrorListsTheBindOnlyRows(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="P" Type="widget"/>`, `<Text>x</Text>`)
	_, err := loadPage(t, fsys, `<Gooey><Card/></Gooey>`, &Context{Values: map[string]any{}})
	assertErrContains(t, err, "unknown Type", "image", "series", "style")
}

// `any` is NOT bind-only and must not become so: it is the escape hatch
// for app types, and a literal through it still makes a string source.
// This is the guard on the other side of the axis — if somebody "tidies
// up" by marking every non-scalar row bind-only, this goes red.
func TestAnyIsNotBindOnly(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Blob" Type="any"/>`, `<Text>x</Text>`)
	ctx := &Context{Values: map[string]any{}, Named: map[string]gooey.Component{}}
	if _, err := loadPage(t, fsys, `<Gooey><Card Name="c" Blob="hello"/></Gooey>`, ctx); err != nil {
		t.Fatal(err)
	}
	h, ok := declaredHandle(t, ctx, "c", "Blob").(*prop.Property[any])
	if !ok {
		t.Fatalf("Blob is %T; want *prop.Property[any]", declaredHandle(t, ctx, "c", "Blob"))
	}
	if h.Get() != "hello" {
		t.Fatalf("Blob = %#v; want the literal as a string", h.Get())
	}
}

// A bound handle of an app type the framework has never heard of still
// crosses through `any` unchecked — the row a bind-only type is NOT
// supposed to replace.
func TestAnyStillPassesAppTypesUnchecked(t *testing.T) {
	type feed struct{ URL string }
	fsys := cardFS(`  <x:Property Name="Feeds" Type="any" Required="true"/>`, `<Text>x</Text>`)
	feeds := prop.NewSource([]*feed{{URL: "a"}})
	ctx := &Context{
		Values: map[string]any{"F": feeds},
		Named:  map[string]gooey.Component{},
	}
	if _, err := loadPage(t, fsys, `<Gooey><Card Name="c" Feeds="{{.F}}"/></Gooey>`, ctx); err != nil {
		t.Fatal(err)
	}
	if got := declaredHandle(t, ctx, "c", "Feeds"); got != any(feeds) {
		t.Fatalf("Feeds handle is %T; want the parent's own node", got)
	}
}

// The scalar rows keep both axes: a color is spellable AND bindable, so
// the bindOnly flag must not have leaked onto the whole table.
func TestSpellableRowsStillTakeLiteralsAndDefaults(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Tint" Type="color" Default="#08f"/>
  <x:Property Name="Count" Type="int"/>`, `<Text>x</Text>`)
	ctx := &Context{Values: map[string]any{}, Named: map[string]gooey.Component{}}
	if _, err := loadPage(t, fsys, `<Gooey><Card Name="c" Count="7"/></Gooey>`, ctx); err != nil {
		t.Fatal(err)
	}
	tint, ok := declaredHandle(t, ctx, "c", "Tint").(*prop.Property[render.Color])
	if !ok || tint.Get() != render.RGB(0x00, 0x88, 0xff) {
		t.Errorf("color Default did not materialize: %#v", declaredHandle(t, ctx, "c", "Tint"))
	}
	count, ok := declaredHandle(t, ctx, "c", "Count").(*prop.Property[int])
	if !ok || count.Get() != 7 {
		t.Errorf("int literal did not coerce: %#v", declaredHandle(t, ctx, "c", "Count"))
	}
}

// Declarations() is the wire schema half: it is a pure function of
// bytes, and it reports the bind-only rows like any other. A control
// whose Default is illegal fails HERE too, which is what makes the
// schema and the loader one contract rather than two.
func TestDeclarationsReportsBindOnlyRows(t *testing.T) {
	src := []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Tint" Type="style"/>
  <x:Property Name="Trend" Type="series" Required="true"/>
  <Text>x</Text>
</Gooey>`)
	ds, err := Declarations(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[0].Type != "style" || ds[1].Type != "series" || !ds[1].Required {
		t.Fatalf("declarations = %#v", ds)
	}

	bad := []byte(`<Gooey xmlns:x="` + XNamespace + `">
  <x:Property Name="Tint" Type="style" Default="accent"/>
  <Text>x</Text>
</Gooey>`)
	if _, err := Declarations(bad); err == nil {
		t.Fatal("Declarations accepted a Default the loader refuses")
	} else {
		assertErrContains(t, err, "has no literal syntax, so it takes no Default")
	}
}

// Strict mode is unaffected: declaring a bind-only surface still makes
// an undeclared attribute a load error, and the message names the
// declared property rather than the literal-syntax rule.
func TestBindOnlySurfaceIsStillStrict(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Tint" Type="style"/>`, `<Text>x</Text>`)
	_, err := loadPage(t, fsys, `<Gooey><Card Tnit="{{.X}}"/></Gooey>`, &Context{Values: map[string]any{}})
	assertErrContains(t, err, `no dependency property "Tnit"`, "declared: Tint")
}

// A bind-only declaration reached through a handler expression is the
// escape-hatch error, not a type error: behavior needs Type="any".
func TestBindOnlyRejectsHandlerExpressions(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Tint" Type="style"/>`, `<Text>x</Text>`)
	ctx := &Context{Values: map[string]any{}}
	_, err := loadPage(t, fsys, `<Gooey><Card Tint="{{sys:Run}}"/></Gooey>`, ctx)
	assertErrContains(t, err, `dependency property "Tint"`,
		"handler expression", `Type="any"`)
}

// The one lie a bind-only row could still tell: bindKindOf's parse
// closure is a backstop nothing should reach. If a future caller
// forgets the bindOnly check, this is the failure it gets — loud, not a
// silent zero value.
func TestBindOnlySourceBackstopFailsLoudly(t *testing.T) {
	for _, typ := range []string{"style", "image", "series"} {
		k := propKinds[typ]
		if !k.bindOnly {
			t.Fatalf("%q is not bind-only", typ)
		}
		if _, err := k.source("anything"); err == nil {
			t.Errorf("%q coerced a literal through the backstop", typ)
		}
		// The zero handle is still reachable — that is the three-way rule.
		if _, err := k.source(""); err != nil {
			t.Errorf("%q could not make its zero handle: %v", typ, err)
		}
	}
}

// Two instances of the same control do not share the zero handle a
// bind-only default materializes: it is per-instance state like every
// other declared source.
func TestBindOnlyZeroHandleIsPerInstance(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Tint" Type="style"/>`, `<Text Style="{{.Tint}}">x</Text>`)
	ctx := &Context{Values: map[string]any{}, Named: map[string]gooey.Component{}}
	page := `<Gooey><VStack><Card Name="a"/><Card Name="b"/></VStack></Gooey>`
	if _, err := loadPage(t, fsys, page, ctx); err != nil {
		t.Fatal(err)
	}
	a := declaredHandle(t, ctx, "a", "Tint")
	b := declaredHandle(t, ctx, "b", "Tint")
	if a == b {
		t.Fatal("two instances share one style handle")
	}
}

// The corpus check's small sibling: nothing in the shipped vocabulary
// regressed into bind-only, and every row still names a handle type in
// its error message. A row with an empty `want` produces an error that
// tells the author nothing.
func TestEveryRowNamesItsHandleType(t *testing.T) {
	spellable := map[string]bool{"string": true, "int": true, "bool": true, "float": true, "duration": true, "color": true}
	for name, k := range propKinds {
		if k.want == "" {
			t.Errorf("row %q has no handle type for its error message", name)
		}
		if spellable[name] && k.bindOnly {
			t.Errorf("row %q is spellable but marked bind-only", name)
		}
		if name == "any" && k.bindOnly {
			t.Errorf("`any` is the escape hatch and must stay spellable")
		}
	}
	for _, name := range []string{"style", "image", "series"} {
		if _, ok := propKinds[name]; !ok {
			t.Errorf("row %q is missing", name)
		}
	}
}
