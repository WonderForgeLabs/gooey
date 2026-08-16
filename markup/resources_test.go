package markup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func resDoc(body string) string {
	return `<Gooey xmlns="wonderforge.io/gooey/2026">` + "\n" + body + "\n</Gooey>"
}

// resLoad builds a document against a context with one host-registered
// style ("host", bold) so every collision case has something to collide
// WITH. A test that only ever declares in markup cannot tell "the chain
// was consulted first" from "the chain was the only thing consulted".
func resLoad(t *testing.T, body string) (gooey.Component, *Context, error) {
	t.Helper()
	ctx := &Context{
		Values: map[string]any{"Text": prop.NewSource("hello"), "Err": prop.NewSource("")},
		Styles: map[string]render.Style{
			"host":   {Bold: true},
			"accent": {Fg: render.RGB(0x11, 0x11, 0x11)},
		},
	}
	fsys := fstest.MapFS{"page.gooey": {Data: []byte(resDoc(body))}}
	w, err := Load(fsys, "page.gooey", ctx)
	return w, ctx, err
}

func mustResLoad(t *testing.T, body string) (gooey.Component, *Context) {
	t.Helper()
	w, ctx, err := resLoad(t, body)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return w, ctx
}

func namedStyle(t *testing.T, ctx *Context, name string) render.Style {
	t.Helper()
	x, err := Find[*components.Text](ctx, name)
	if err != nil {
		t.Fatalf("find %q: %v", name, err)
	}
	return x.Style.Get()
}

// TestDocumentStyleResolvesInBothSpellings pins the two <Style> forms
// against each other. They are one table — styleFields — so the useful
// assertion is that the flat attribute form and the <Setter> form
// produce the identical render.Style, not that each produces something.
func TestDocumentStyleResolvesInBothSpellings(t *testing.T) {
	_, ctx := mustResLoad(t, `
  <Gooey.Resources>
    <Style Key="flat" Fg="#ffaa3c" Bg="#12121e" Bold="true" Underline="true"/>
    <Style Key="setters">
      <Setter Property="Fg" Value="#ffaa3c"/>
      <Setter Property="Bg" Value="#12121e"/>
      <Setter Property="Bold" Value="true"/>
      <Setter Property="Underline" Value="true"/>
    </Style>
  </Gooey.Resources>
  <VStack>
    <Text Name="A" Style="flat">a</Text>
    <Text Name="B" Style="setters">b</Text>
  </VStack>`)

	want := render.Style{
		Fg:        render.RGB(0xff, 0xaa, 0x3c),
		Bg:        render.RGB(0x12, 0x12, 0x1e),
		Bold:      true,
		Underline: true,
	}
	if got := namedStyle(t, ctx, "A"); got != want {
		t.Fatalf("attribute form resolved %+v, want %+v", got, want)
	}
	if got := namedStyle(t, ctx, "B"); got != want {
		t.Fatalf("<Setter> form resolved %+v, want %+v", got, want)
	}
}

// TestStyleSetterReadsResource is the lvalue pin at load: a Resource=
// setter must take the VALUE the resource holds, which is the only way
// "#12121e in two languages" stops being two languages.
func TestStyleSetterReadsResource(t *testing.T) {
	_, ctx := mustResLoad(t, `
  <Gooey.Resources>
    <Style Key="panel">
      <Setter Property="Fg" Resource="ink"/>
      <Setter Property="Bold" Resource="loud"/>
    </Style>
    <Resource Key="ink"  Type="color" Value="#12121e"/>
    <Resource Key="loud" Type="bool"  Value="true"/>
  </Gooey.Resources>
  <Text Name="A" Style="panel">a</Text>`)

	want := render.Style{Fg: render.RGB(0x12, 0x12, 0x1e), Bold: true}
	if got := namedStyle(t, ctx, "A"); got != want {
		t.Fatalf("resolved %+v, want %+v", got, want)
	}
}

// The <Style> above references a <Resource> declared BELOW it, which is
// the arm this asserts: instantiate materializes every scalar before it
// resolves any setter, so a file need not be written bottom-up. Without
// the two passes the case above fails at load with "no resource named".
func TestStyleMayReferenceAResourceDeclaredLater(t *testing.T) {
	if _, _, err := resLoad(t, `
  <Gooey.Resources>
    <Style Key="panel"><Setter Property="Fg" Resource="ink"/></Style>
    <Resource Key="ink" Type="color" Value="#12121e"/>
  </Gooey.Resources>
  <Text Style="panel">a</Text>`); err != nil {
		t.Fatalf("forward reference should load: %v", err)
	}
}

// TestPageStyleBeatsContextStyles is the COLLISION RULE, stated as a
// test because the fallback order alone does not state it.
//
// The page wins. Context.Styles is the outermost scope, below every
// markup-declared one, so the nearest declaration wins exactly as it
// does between two markup scopes. See resources.go for the argument.
func TestPageStyleBeatsContextStyles(t *testing.T) {
	_, ctx := mustResLoad(t, `
  <Gooey.Resources>
    <Style Key="accent" Fg="#ffaa3c"/>
  </Gooey.Resources>
  <VStack>
    <Text Name="A" Style="accent">a</Text>
    <Text Name="B" Style="host">b</Text>
  </VStack>`)

	if got, want := namedStyle(t, ctx, "A"), (render.Style{Fg: render.RGB(0xff, 0xaa, 0x3c)}); got != want {
		t.Fatalf("page-declared accent resolved %+v, want %+v — the host's Styles[\"accent\"] won", got, want)
	}
	// ... and the host's map is still the fallback for everything the
	// page does NOT declare. Shadowing one key must not hide the rest.
	if got, want := namedStyle(t, ctx, "B"), (render.Style{Bold: true}); got != want {
		t.Fatalf("host style resolved %+v, want %+v", got, want)
	}
}

// TestAMarkupDeclaredStyleWorksAtEveryStyleSite runs the SAME six sites
// style_test.go pins, with the host's Styles map empty so the only place
// "dim" can come from is the document's own <Gooey.Resources>.
//
// It reuses that file's styleSites table rather than restating it, for
// the reason the table was built by construction in the first place: a
// feature wired into five of six style lookups leaves the sixth as a
// style that silently does nothing, and a hand-copied list here would go
// stale the moment a seventh site is added there.
func TestAMarkupDeclaredStyleWorksAtEveryStyleSite(t *testing.T) {
	load := func(body string) error {
		ctx := styleCtx()
		ctx.Styles = nil // the host grants nothing; markup is the only source
		fsys := fstest.MapFS{"page.gooey": {Data: []byte(
			"<Gooey>\n<Gooey.Resources><Style Key=\"dim\" Dim=\"true\"/></Gooey.Resources>\n" + body + "\n</Gooey>")}}
		_, err := Load(fsys, "page.gooey", ctx)
		return err
	}
	// The same six sites again, but with the host's map intact and a
	// resources block that does NOT define the name. This is the arm that
	// says adding a second source did not SHADOW the first: every existing
	// document is a page with no resources, and the first page anyone adds
	// a <Gooey.Resources> to still names most of its styles in Go.
	loadWithHost := func(body string) error {
		fsys := fstest.MapFS{"page.gooey": {Data: []byte(
			"<Gooey>\n<Gooey.Resources><Style Key=\"unrelated\" Bold=\"true\"/></Gooey.Resources>\n" + body + "\n</Gooey>")}}
		_, err := Load(fsys, "page.gooey", styleCtx())
		return err
	}
	for _, site := range styleSites {
		t.Run(site.name, func(t *testing.T) {
			if err := load(site.good); err != nil {
				t.Fatalf("a page-declared style is not honoured here: %v", err)
			}
			if err := loadWithHost(site.good); err != nil {
				t.Fatalf("a HOST style stopped resolving here once the page declared resources: %v", err)
			}
			// The near-miss twin, at the same site: adding a second place
			// a style may come from must not soften the rule for a name
			// that is in neither.
			err := load(site.bad)
			if err == nil {
				t.Fatal("a name in neither the document nor the host loaded clean")
			}
			if !strings.Contains(err.Error(), "no style named") {
				t.Fatalf("error was %v; want the registered-style error", err)
			}
		})
	}
}

// TestUnknownStyleKeyStillFailsLoad is the promise this feature had to
// keep. Adding a second place a style can come from must not weaken the
// rule that a name found in NEITHER place is a load error — that is the
// silent-drop class the strict-style check landed to close.
func TestUnknownStyleKeyStillFailsLoad(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"page declares resources but not this key", `
  <Gooey.Resources><Style Key="accent" Fg="#ffaa3c"/></Gooey.Resources>
  <Text Style="acccent">a</Text>`},
		{"near miss on a HOST style, with resources present", `
  <Gooey.Resources><Style Key="accent" Fg="#ffaa3c"/></Gooey.Resources>
  <Text Style="hst">a</Text>`},
		{"a subtree scope does not leak to a later sibling", `
  <VStack>
    <Border><Border.Resources><Style Key="inner" Fg="#ffaa3c"/></Border.Resources><Text Style="inner">a</Text></Border>
    <Text Style="inner">b</Text>
  </VStack>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := resLoad(t, tc.body)
			if err == nil {
				t.Fatal("loaded clean; an unresolvable style name must fail the load")
			}
			if !strings.Contains(err.Error(), "no style named") {
				t.Fatalf("error was %v; want the registered-style error", err)
			}
		})
	}
}

// TestSubtreeOverrideShadows: an inner scope redefining a key produces a
// DIFFERENT handle for readers inside it, and the outer readers keep the
// outer one. That is the whole per-subtree-override story — no priority
// numbers, just lexical capture — so the pin is that a Set on the outer
// resource moves the outer reader and leaves the inner one alone.
func TestSubtreeOverrideShadows(t *testing.T) {
	_, ctx := mustResLoad(t, `
  <Gooey.Resources>
    <Resource Key="ink" Type="color" Value="#111111"/>
    <Style Key="body"><Setter Property="Fg" Resource="ink"/></Style>
  </Gooey.Resources>
  <VStack>
    <Text Name="Outer" Style="body">a</Text>
    <Border>
      <Border.Resources>
        <Resource Key="ink" Type="color" Value="#222222"/>
        <Style Key="body"><Setter Property="Fg" Resource="ink"/></Style>
      </Border.Resources>
      <Text Name="Inner" Style="body">b</Text>
    </Border>
  </VStack>`)

	if got, want := namedStyle(t, ctx, "Outer").Fg, render.RGB(0x11, 0x11, 0x11); got != want {
		t.Fatalf("outer Fg %v, want %v", got, want)
	}
	if got, want := namedStyle(t, ctx, "Inner").Fg, render.RGB(0x22, 0x22, 0x22); got != want {
		t.Fatalf("inner Fg %v, want %v — the inner scope did not shadow", got, want)
	}

	h, ok := ctx.Resource("ink").(*prop.Property[render.Color])
	if !ok {
		t.Fatalf("Context.Resource(\"ink\") is %T; want *prop.Property[render.Color]", ctx.Resource("ink"))
	}
	h.Set(render.RGB(0x33, 0x33, 0x33))
	if got, want := namedStyle(t, ctx, "Outer").Fg, render.RGB(0x33, 0x33, 0x33); got != want {
		t.Fatalf("after Set the outer Fg is %v, want %v", got, want)
	}
	if got, want := namedStyle(t, ctx, "Inner").Fg, render.RGB(0x22, 0x22, 0x22); got != want {
		t.Fatalf("after Set the inner Fg is %v, want %v — the shadow shares the outer handle", got, want)
	}
}

// resourcePage is three labels: two styled through the SAME resource,
// one styled through a different one. The third is load-bearing — with
// only readers on the page, "exactly the readers repainted" and
// "everything repainted" are the same number, and the assertion would
// pass over a tree that repaints wholesale.
const resourcePage = `<Gooey xmlns="wonderforge.io/gooey/2026">
  <Gooey.Resources>
    <Resource Key="ink"   Type="color" Value="#111111"/>
    <Resource Key="other" Type="color" Value="#222222"/>
    <Style Key="themed"><Setter Property="Fg" Resource="ink"/></Style>
    <Style Key="apart"><Setter Property="Fg" Resource="other"/></Style>
  </Gooey.Resources>
  <VStack>
    <Text Style="themed">a</Text>
    <Text Style="themed">b</Text>
    <Text Style="apart">c</Text>
  </VStack>
</Gooey>`

// TestResourceSetRepaintsExactlyReaders is the damage pin, and the only
// assertion here that can tell a live resource handle from a value
// baked at load.
//
// The mechanism: a Resource= setter closes over the HANDLE and its Get
// runs inside the style computed, which is read inside each Text's own
// paint node — so prop.node.recordRead sees the computed on evalStack
// and records the edge. Bake the color in at build instead and the count
// falls to 0 while every rendered cell still looks right, which is
// exactly why a cell assertion cannot stand in for this one.
func TestResourceSetRepaintsExactlyReaders(t *testing.T) {
	ctx := &Context{}
	fsys := fstest.MapFS{"page.gooey": {Data: []byte(resourcePage)}}
	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c := gooey.NewComposer(w, 12, 3)
	if _, n := c.Frame(); n == 0 {
		t.Fatalf("first frame painted %d components; want the whole tree", n)
	}
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("settled frame painted %d, want 0", n)
	}

	h, ok := ctx.Resource("ink").(*prop.Property[render.Color])
	if !ok {
		t.Fatalf("Context.Resource(\"ink\") is %T; want a color handle", ctx.Resource("ink"))
	}
	h.Set(render.RGB(0xff, 0xaa, 0x3c))

	f, n := c.Frame()
	if n != 2 {
		t.Fatalf("Set on a resource repainted %d components, want exactly the 2 that read it", n)
	}
	if got, want := f.Cells.At(0, 0).Style.Fg, render.RGB(0xff, 0xaa, 0x3c); got != want {
		t.Fatalf("reader painted Fg %v, want %v", got, want)
	}
	if got, want := f.Cells.At(0, 2).Style.Fg, render.RGB(0x22, 0x22, 0x22); got != want {
		t.Fatalf("non-reader painted Fg %v, want %v", got, want)
	}
}

// TestStaticStyleCostsNoRepaint: a style with no Resource= setter has no
// dependencies, so nothing about the styling system makes a settled
// frame dirty. The zero here is what says "after build there is no
// styling machinery left running".
func TestStaticStyleCostsNoRepaint(t *testing.T) {
	fsys := fstest.MapFS{"page.gooey": {Data: []byte(resDoc(`
  <Gooey.Resources><Style Key="flat" Fg="#ffaa3c" Bold="true"/></Gooey.Resources>
  <VStack><Text Style="flat">a</Text><Text Style="flat">b</Text></VStack>`))}}
	w, err := Load(fsys, "page.gooey", &Context{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c := gooey.NewComposer(w, 12, 2)
	c.Frame()
	for i := 0; i < 3; i++ {
		if _, n := c.Frame(); n != 0 {
			t.Fatalf("settled frame %d painted %d, want 0", i, n)
		}
	}
}

// TestHotReloadRepaintsWithoutARebuild is the point of the whole
// feature: the palette moves into the file a designer edits, and editing
// it changes the running app with no `go build`.
//
// It drives the real Watch — a real os.DirFS, a real ModTime poll, a
// real rewrite — because the claim is about the editing loop, and a test
// that called Load twice would prove only that Load reads its argument.
func TestHotReloadRepaintsWithoutARebuild(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "page.gooey")
	write := func(fg string) {
		t.Helper()
		src := resDoc(`
  <Gooey.Resources><Style Key="accent" Fg="` + fg + `"/></Gooey.Resources>
  <Text Style="accent">a</Text>`)
		if err := os.WriteFile(page, []byte(src), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write("#111111")

	fsys := os.DirFS(dir)
	ctx := &Context{}
	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c := gooey.NewComposer(w, 12, 1)
	if f, _ := c.Frame(); f.Cells.At(0, 0).Style.Fg != render.RGB(0x11, 0x11, 0x11) {
		t.Fatalf("first paint Fg %v", f.Cells.At(0, 0).Style.Fg)
	}

	swapped := make(chan gooey.Component, 1)
	stop := Watch(fsys, "page.gooey", ctx, func(nw gooey.Component) { swapped <- nw })
	defer stop()

	// The edit is REPEATED on a ticker rather than written once after a
	// sleep. Watch takes its ModTime baseline inside its own goroutine,
	// so a single write racing that goroutine's start is recorded as the
	// baseline and never seen as a change — which is a flake on a loaded
	// machine, not a property of the feature. Re-writing bumps ModTime
	// again, so any tick after the baseline is detected.
	edit := time.NewTicker(500 * time.Millisecond)
	defer edit.Stop()
	deadline := time.After(20 * time.Second)
	for {
		select {
		case nw := <-swapped:
			c2 := gooey.NewComposer(nw, 12, 1)
			f, _ := c2.Frame()
			if got, want := f.Cells.At(0, 0).Style.Fg, render.RGB(0xff, 0xaa, 0x3c); got != want {
				t.Fatalf("after the edit the paint is %v, want %v", got, want)
			}
			return
		case <-edit.C:
			write("#ffaa3c")
		case <-deadline:
			t.Fatal("Watch never rebuilt after the palette was edited")
		}
	}
}

// --- control boundaries -------------------------------------------------

// controlKids builds a page against card.gooey and returns the root's
// children. A control instance's Name map is scoped to that INSTANCE —
// Find on the page context cannot see inside one — so every assertion
// about a control's insides walks the built tree instead.
func controlKids(t *testing.T, page, card string) (*Context, []gooey.Component) {
	t.Helper()
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(page)},
		"card.gooey": {Data: []byte(card)},
	}
	ctx := &Context{Includes: fsys}
	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c, ok := w.(gooey.Container)
	if !ok {
		t.Fatalf("root is %T, not a container", w)
	}
	return ctx, c.ChildComponents()
}

// TestResourcesAreAmbientAcrossAControlBoundary: Values isolate,
// resources inherit. A control's markup can bind only what crossed its
// declared surface, but it is styled by the site that instantiated it —
// otherwise a themed page would have an unthemed hole wherever it used a
// control, and the theme would have to be passed as an attribute to
// every control on the page.
func TestResourcesAreAmbientAcrossAControlBoundary(t *testing.T) {
	_, kids := controlKids(t,
		resDoc(`
  <Gooey.Resources>
    <Resource Key="ink" Type="color" Value="#111111"/>
    <Style Key="body"><Setter Property="Fg" Resource="ink"/></Style>
  </Gooey.Resources>
  <VStack><Card/></VStack>`),
		// The control declares NOTHING: no resources, no x:Property. It
		// names a style that exists only on the page.
		resDoc(`<Text Style="body">card</Text>`))

	if got, want := styleOfLeaf(t, kids[0]).Get().Fg, render.RGB(0x11, 0x11, 0x11); got != want {
		t.Fatalf("control resolved the page's style as %v, want %v — theme did not cross the boundary", got, want)
	}
}

// TestControlResourcesShadowAmbientPerInstance covers the three halves
// of a resource block inside a CONTROL file: it shadows the ambient
// chain for that control's subtree, the shadow does NOT leak back out to
// the instantiation site, and Context.Resource still answers for the
// page rather than for whichever control instantiated last.
func TestControlResourcesShadowAmbientPerInstance(t *testing.T) {
	ctx, kids := controlKids(t,
		resDoc(`
  <Gooey.Resources>
    <Resource Key="ink" Type="color" Value="#111111"/>
    <Style Key="body"><Setter Property="Fg" Resource="ink"/></Style>
  </Gooey.Resources>
  <VStack>
    <Card/>
    <Text Name="Page" Style="body">page</Text>
  </VStack>`),
		resDoc(`
  <Gooey.Resources>
    <Resource Key="ink" Type="color" Value="#222222"/>
    <Style Key="mine"><Setter Property="Fg" Resource="ink"/></Style>
  </Gooey.Resources>
  <VStack>
    <Text Style="body">inherited</Text>
    <Text Style="mine">own</Text>
  </VStack>`))

	card, ok := kids[0].(gooey.Container)
	if !ok {
		t.Fatalf("card instance is %T, not a container", kids[0])
	}
	cardKids := card.ChildComponents()

	// The page's style resolves inside the control, and its Fg comes
	// from the PAGE's "ink" — a style captures the scope it was DECLARED
	// in, lexically, like a closure. A control shadowing "ink" retunes
	// the styles the control itself declares; the other reading would
	// let any control silently repaint a style it merely borrowed.
	if got, want := styleOfLeaf(t, cardKids[0]).Get().Fg, render.RGB(0x11, 0x11, 0x11); got != want {
		t.Fatalf("inherited style resolved %v, want %v", got, want)
	}
	// The control's own style reads the control's own "ink".
	if got, want := styleOfLeaf(t, cardKids[1]).Get().Fg, render.RGB(0x22, 0x22, 0x22); got != want {
		t.Fatalf("the control's own style resolved %v, want %v", got, want)
	}
	// The shadow did not leak back out to the instantiation site.
	if got, want := namedStyle(t, ctx, "Page").Fg, render.RGB(0x11, 0x11, 0x11); got != want {
		t.Fatalf("page Fg %v, want %v — the control's <Gooey.Resources> leaked out", got, want)
	}
	// ... and Context.Resource still serves the PAGE's document scope.
	h, ok := ctx.Resource("ink").(*prop.Property[render.Color])
	if !ok {
		t.Fatalf("Context.Resource(\"ink\") is %T", ctx.Resource("ink"))
	}
	if got, want := h.Get(), render.RGB(0x11, 0x11, 0x11); got != want {
		t.Fatalf("document scope holds %v, want %v", got, want)
	}
}

// TestEachInstantiationResolvesTheAmbientScopeAtItsOwnSite: a control's
// resource block is a DEFINITION, instantiated afresh wherever the
// control appears, against the chain visible AT THAT SITE. Two instances
// of one control under different ambient themes must therefore resolve
// differently.
//
// The pin is worth stating because the tempting optimisation — parse the
// block once, keep the scope on it — passes every other test in this
// file: two instances both look right as long as they sit in the same
// theme. This is the arm that separates "instantiated per site" from
// "instantiated once and reused", and reusing would also alias the
// handles, which is the half nothing can observe yet (no API reaches
// another instance's scope to Set it).
func TestEachInstantiationResolvesTheAmbientScopeAtItsOwnSite(t *testing.T) {
	_, kids := controlKids(t,
		resDoc(`
  <Gooey.Resources><Resource Key="ink" Type="color" Value="#111111"/></Gooey.Resources>
  <VStack>
    <Card/>
    <Border>
      <Border.Resources><Resource Key="ink" Type="color" Value="#222222"/></Border.Resources>
      <Card/>
    </Border>
  </VStack>`),
		// The control declares a style over the AMBIENT ink, so what it
		// resolves to is a property of where it was instantiated.
		resDoc(`
  <Gooey.Resources>
    <Style Key="mine"><Setter Property="Fg" Resource="ink"/></Style>
  </Gooey.Resources>
  <Text Style="mine">card</Text>`))

	if got, want := styleOfLeaf(t, kids[0]).Get().Fg, render.RGB(0x11, 0x11, 0x11); got != want {
		t.Fatalf("first instance resolved %v, want %v", got, want)
	}
	inner, ok := kids[1].(gooey.Container)
	if !ok {
		t.Fatalf("second child is %T, not the Border", kids[1])
	}
	if got, want := styleOfLeaf(t, inner.ChildComponents()[0]).Get().Fg, render.RGB(0x22, 0x22, 0x22); got != want {
		t.Fatalf("second instance resolved %v, want %v — the block was instantiated once and reused", got, want)
	}
}

// styleOfLeaf digs the Text out of a control instance. The instance's
// root is whatever its markup declared, so this walks rather than
// asserting a shape.
func styleOfLeaf(t *testing.T, w gooey.Component) *prop.Property[render.Style] {
	t.Helper()
	if x, ok := w.(*components.Text); ok {
		return x.Style
	}
	c, ok := w.(gooey.Container)
	if !ok {
		t.Fatalf("no Text under %T", w)
	}
	for _, k := range c.ChildComponents() {
		if x, ok := k.(*components.Text); ok {
			return x.Style
		}
	}
	t.Fatalf("no Text under %T", w)
	return nil
}

// --- load errors --------------------------------------------------------

// TestResourceLoadErrors is the strictness table. Everything resolvable
// resolves at LOAD: a resource block that cannot be honoured must fail
// the file that declares it, never paint a zero style and look like
// somebody's choice.
func TestResourceLoadErrors(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{
			"unknown element in the block",
			`<Gooey.Resources><Color Key="x"/></Gooey.Resources><Text>a</Text>`,
			"<Resource> and <Style> elements only",
		},
		{
			"another property element on the root",
			`<Gooey.Styles><Style Key="a" Bold="true"/></Gooey.Styles><Text>a</Text>`,
			"the root's only slot is <Gooey.Resources>",
		},
		{
			"resource with no Type",
			`<Gooey.Resources><Resource Key="ink" Value="#111"/></Gooey.Resources><Text>a</Text>`,
			"needs a Type",
		},
		{
			"resource with an unknown Type",
			`<Gooey.Resources><Resource Key="ink" Type="colour" Value="#111"/></Gooey.Resources><Text>a</Text>`,
			`unknown Type "colour"`,
		},
		{
			"resource typed any",
			`<Gooey.Resources><Resource Key="ink" Type="any" Value="x"/></Gooey.Resources><Text>a</Text>`,
			"has no literal syntax",
		},
		{
			"resource with no Value",
			`<Gooey.Resources><Resource Key="ink" Type="color"/></Gooey.Resources><Text>a</Text>`,
			"needs a Value",
		},
		{
			"resource whose Value is not its Type",
			`<Gooey.Resources><Resource Key="ink" Type="color" Value="periwinkle"/></Gooey.Resources><Text>a</Text>`,
			"not a color",
		},
		{
			"resource with an unknown attribute",
			`<Gooey.Resources><Resource Key="ink" Type="color" Value="#111" Fg="#222"/></Gooey.Resources><Text>a</Text>`,
			`<Resource> has no attribute "Fg"`,
		},
		{
			"duplicate key in one block",
			`<Gooey.Resources><Style Key="a" Bold="true"/><Style Key="a" Dim="true"/></Gooey.Resources><Text>a</Text>`,
			"already defined in this <Resources> block",
		},
		{
			"duplicate across the two forms",
			`<Gooey.Resources><Resource Key="a" Type="bool" Value="true"/><Style Key="a" Bold="true"/></Gooey.Resources><Text>a</Text>`,
			"already defined in this <Resources> block",
		},
		{
			"style with no Key",
			`<Gooey.Resources><Style Bold="true"/></Gooey.Resources><Text>a</Text>`,
			"<Style> needs a Key",
		},
		{
			"style with a TargetType and no Key",
			`<Gooey.Resources><Style TargetType="Border" Bold="true"/></Gooey.Resources><Text>a</Text>`,
			"implicit type matching is not implemented yet",
		},
		{
			"style that sets nothing",
			`<Gooey.Resources><Style Key="a"/></Gooey.Resources><Text>a</Text>`,
			"sets nothing",
		},
		{
			"style attribute that is not a style field",
			`<Gooey.Resources><Style Key="a" Colour="#111"/></Gooey.Resources><Text>a</Text>`,
			`has no attribute "Colour"`,
		},
		{
			"style attribute whose literal is wrong",
			`<Gooey.Resources><Style Key="a" Bold="yes please"/></Gooey.Resources><Text>a</Text>`,
			"want true or false",
		},
		{
			"style holding something other than a Setter",
			`<Gooey.Resources><Style Key="a"><Resource Key="b" Type="bool" Value="true"/></Style></Gooey.Resources><Text>a</Text>`,
			"holds <Setter> elements only",
		},
		{
			"setter with an unknown Property",
			`<Gooey.Resources><Style Key="a"><Setter Property="Italic" Value="true"/></Style></Gooey.Resources><Text>a</Text>`,
			`no style field "Italic"`,
		},
		{
			"setter with both Value and Resource",
			`<Gooey.Resources><Resource Key="ink" Type="color" Value="#111"/><Style Key="a"><Setter Property="Fg" Value="#222" Resource="ink"/></Style></Gooey.Resources><Text>a</Text>`,
			"takes Value or Resource, not both",
		},
		{
			"setter with neither",
			`<Gooey.Resources><Style Key="a"><Setter Property="Fg"/></Style></Gooey.Resources><Text>a</Text>`,
			"needs a Value or a Resource",
		},
		{
			"setter naming a resource that is not in scope",
			`<Gooey.Resources><Style Key="a"><Setter Property="Fg" Resource="ink"/></Style></Gooey.Resources><Text>a</Text>`,
			"no resource named",
		},
		{
			"setter whose resource is the wrong type",
			`<Gooey.Resources><Resource Key="pad" Type="int" Value="1"/><Style Key="a"><Setter Property="Fg" Resource="pad"/></Style></Gooey.Resources><Text>a</Text>`,
			"wants a color resource",
		},
		{
			"setter naming a style as its resource",
			`<Gooey.Resources><Style Key="b" Bold="true"/><Style Key="a"><Setter Property="Fg" Resource="b"/></Style></Gooey.Resources><Text>a</Text>`,
			"is a <Style>, not a <Resource>",
		},
		{
			"a state section, which is reserved and not built",
			`<Gooey.Resources><Style Key="a" Bold="true"><Style.Focus><Setter Property="Fg" Value="#111"/></Style.Focus></Style></Gooey.Resources><Text Style="a">a</Text>`,
			"state section is not implemented yet",
		},
		{
			"Style= naming a <Resource>",
			`<Gooey.Resources><Resource Key="ink" Type="color" Value="#111"/></Gooey.Resources><Text Style="ink">a</Text>`,
			"not a <Style>",
		},
		{
			"Style= on an element the style excludes",
			`<Gooey.Resources><Style Key="a" TargetType="Border" Bold="true"/></Gooey.Resources><Text Style="a">a</Text>`,
			`declares TargetType="Border"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := resLoad(t, tc.body)
			if err == nil {
				t.Fatalf("loaded clean; want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error was %v; want it to contain %q", err, tc.want)
			}
		})
	}
}

// TestABadResourceLiteralNamesTheFileThatDeclaresIt is why the literal
// is coerced at PARSE rather than only when the block is instantiated.
//
// Both places would reject "periwinkle" with the same words, so the
// message alone cannot say which check fired — the discriminating fact
// is the file name. Parsing runs inside loadDocument, whose fileError
// stamps the document the declaration came from; instantiation runs at
// build, where nothing knows the file any more. A palette broken in a
// control would otherwise fail a page load pointing at nothing.
func TestABadResourceLiteralNamesTheFileThatDeclaresIt(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(resDoc(`<VStack><Card/></VStack>`))},
		"card.gooey": {Data: []byte(resDoc(`
  <Gooey.Resources><Resource Key="ink" Type="color" Value="periwinkle"/></Gooey.Resources>
  <Text>card</Text>`))},
	}
	_, err := Load(fsys, "page.gooey", &Context{Includes: fsys})
	if err == nil {
		t.Fatal("a bad resource literal loaded clean")
	}
	if !strings.Contains(err.Error(), "card.gooey") {
		t.Fatalf("error was %v; it must name card.gooey, the file that declares the resource", err)
	}
}

// TestTargetTypeMatchesIsNotAnError is the near-miss twin of the
// TargetType arm above: the check must reject the wrong element and
// accept the right one, or "everything fails" would pass the table.
func TestTargetTypeMatchesIsNotAnError(t *testing.T) {
	if _, _, err := resLoad(t, `
  <Gooey.Resources><Style Key="a" TargetType="Text" Bold="true"/></Gooey.Resources>
  <Text Style="a">a</Text>`); err != nil {
		t.Fatalf("a style used on its declared TargetType should load: %v", err)
	}
}

// TestEmptyStyleAttributeStaysValid: absent and empty both mean "no
// style". Only a name that was typed and does not exist is an error, and
// that predates this feature.
func TestEmptyStyleAttributeStaysValid(t *testing.T) {
	_, ctx := mustResLoad(t, `
  <Gooey.Resources><Style Key="a" Bold="true"/></Gooey.Resources>
  <Text Name="A" Style="">a</Text>`)
	if got := namedStyle(t, ctx, "A"); got != (render.Style{}) {
		t.Fatalf("Style=\"\" resolved %+v, want the zero style", got)
	}
}

// TestDocumentWithoutResourcesIsUnchanged is the compatibility pin: the
// zero resourceEnv must behave exactly as the package did before there
// was one, including the host map and its unknown-key error.
func TestDocumentWithoutResourcesIsUnchanged(t *testing.T) {
	_, ctx := mustResLoad(t, `<Text Name="A" Style="host">a</Text>`)
	if got, want := namedStyle(t, ctx, "A"), (render.Style{Bold: true}); got != want {
		t.Fatalf("host style resolved %+v, want %+v", got, want)
	}
	if ctx.Resource("anything") != nil {
		t.Fatal("a document with no resources served one")
	}
}

// --- the parts that live outside resources.go ---------------------------
//
// Everything below pins a line in another file: property.go's bindOnly
// flag as parseResourceDecl consumes it, the five value-typed style sites
// in elements.go, the scope itemsview.go's row factory captures, and the
// pop in build(). They are the easiest pins to leave unwritten, because
// the suite above looks complete without them.

// A bind-only Type has no literal syntax, so it cannot be a <Resource> —
// and the rejection has to be EXPLICIT rather than left to the coercion
// below it. kindOf's source SHORT-CIRCUITS on an empty literal and hands
// back the zero-valued handle without ever consulting its parse closure,
// so <Resource Type="style" Value=""/> would otherwise load clean and
// produce a live *prop.Property[render.Style] whose value nobody wrote.
//
// The Value="" arms are the whole test. A junk literal fails through the
// backstop in bindKindOf whether or not the explicit check exists, so a
// test written only that way passes over the removed guard and reports
// the hole as covered.
func TestABindOnlyTypeCannotBeAResource(t *testing.T) {
	for _, typ := range []string{"style", "image", "series"} {
		for _, val := range []string{"", "something"} {
			t.Run(typ+"/"+val, func(t *testing.T) {
				_, _, err := resLoad(t, `
  <Gooey.Resources><Resource Key="k" Type="`+typ+`" Value="`+val+`"/></Gooey.Resources>
  <Text>a</Text>`)
				if err == nil {
					t.Fatalf("<Resource Type=%q Value=%q> loaded clean and made a handle nobody gave a value", typ, val)
				}
				if !strings.Contains(err.Error(), "no literal syntax") {
					t.Fatalf("error was %v; want it to say why the type cannot be a resource", err)
				}
			})
		}
	}
}

// The positive twin the rejection table needs. Twenty-five rejection rows
// all pass over an implementation that rejects every <Resource>, and no
// other test here declares one of each kind.
//
// It doubles as the drift guard on resourceKindNames, which DERIVES its
// list from propKinds minus the bind-only rows and `any`. Add a propKinds
// row and this fails until someone decides whether it can be a resource —
// which is the decision that would otherwise be made silently, by
// whichever way the new row's bindOnly flag happened to fall.
func TestEveryResourceKindDeclares(t *testing.T) {
	lits := map[string]string{
		"string": "hello", "int": "3", "bool": "true",
		"float": "1.5", "duration": "250ms", "color": "#ffaa3c",
	}
	for _, typ := range resourceKindNames() {
		lit, ok := lits[typ]
		if !ok {
			t.Fatalf("resourceKindNames offers %q and this test has no literal for it: a propKinds "+
				"row landed and nothing checks whether it can actually be declared", typ)
		}
		t.Run(typ, func(t *testing.T) {
			if _, _, err := resLoad(t, `
  <Gooey.Resources><Resource Key="k" Type="`+typ+`" Value="`+lit+`"/></Gooey.Resources>
  <Text>a</Text>`); err != nil {
				t.Fatalf("a valid %s resource failed to load: %v", typ, err)
			}
		})
	}
}

// The value-typed sites take a SNAPSHOT, and that is deliberate: they were
// non-reactive before markup styles existed and stay so. styleValue's Get
// runs outside any evaluation, so it records nothing and a later Set on
// the resource behind the style cannot reach a ToastHost — while the
// handle-typed site two lines away in the same document follows along.
//
// Stated as a test because it is the Get-call-site rule visible from the
// outside, and because a reader who has not internalized that rule will
// otherwise discover this asymmetry by filing a bug against ToastHost.
func TestAValueTypedStyleSiteIsASnapshotNotASubscription(t *testing.T) {
	w, ctx := mustResLoad(t, `
  <Gooey.Resources>
    <Resource Key="ink" Type="color" Value="#111111"/>
    <Style Key="themed"><Setter Property="Fg" Resource="ink"/></Style>
  </Gooey.Resources>
  <VStack>
    <ToastHost Name="Host" Style="themed"/>
    <Text Name="Live" Style="themed">a</Text>
  </VStack>`)

	host, err := Find[*components.ToastHost](ctx, "Host")
	if err != nil {
		t.Fatal(err)
	}
	first := render.RGB(0x11, 0x11, 0x11)
	if host.Style.Fg != first {
		t.Fatalf("the snapshot taken at build is %v, want %v", host.Style.Fg, first)
	}

	c := gooey.NewComposer(w, 20, 3)
	c.Frame()
	next := render.RGB(0xff, 0xaa, 0x3c)
	ctx.Resource("ink").(*prop.Property[render.Color]).Set(next)
	c.Frame()

	if host.Style.Fg != first {
		t.Errorf("the value-typed site moved to %v; it holds a snapshot and has no handle to follow", host.Style.Fg)
	}
	if got := namedStyle(t, ctx, "Live").Fg; got != next {
		t.Errorf("the handle-typed site in the same document is %v, want %v", got, next)
	}
}

// An ItemsView row is built long after the document finished loading,
// against a Context its factory captured — which is why itemsview.go
// captures the xmlns table there deliberately. The resource chain has to
// ride along, or a template naming a page-declared <Style> resolves
// against an empty chain at ROW-REALIZATION time, after the scope popped.
//
// The collection is EMPTY at load on purpose, and that is the whole
// design of the test. ItemsView.Validate builds one throwaway row against
// the first item, so a populated page catches this at load — which would
// hide exactly the regression this exists to catch. A table fed by a
// timer is empty at load, and the error surfaces painted into the view on
// first realization instead.
func TestAnItemTemplateSeesTheResourcesItWasWrittenIn(t *testing.T) {
	rows := prop.NewSource([]post(nil))
	ctx := &Context{Values: map[string]any{"Rows": postItems(rows)}}
	fsys := fstest.MapFS{"page.gooey": {Data: []byte(resDoc(`
  <Gooey.Resources><Style Key="cell" Fg="#ffaa3c"/></Gooey.Resources>
  <ItemsView Items="{{.Rows}}">
    <ItemsView.ItemTemplate><Text Style="cell">{{.Title}}</Text></ItemsView.ItemTemplate>
  </ItemsView>`))}}
	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	c := gooey.NewComposer(w, 24, 3)
	c.Frame()
	rows.Set([]post{{Title: "first", Date: "jan"}})
	c.Frame()

	got := renderToString(t, w, 24, 3)
	if strings.Contains(got, "template:") {
		t.Fatalf("realizing a row failed against an empty resource chain:\n%s", got)
	}
	if !strings.Contains(got, "first") {
		t.Fatalf("the row never realized:\n%s", got)
	}
}

// The pop, pinned by VALUE and DAMAGE rather than by a load error.
//
// build() defers the pop of an element's <X.Resources>, and the only
// other assertion in this package that notices when it does not is a
// load-error row: a later sibling naming an inner-only key. That row goes
// quiet the moment the inner scope defines a key the outer one ALSO
// defines — which is the normal shape of a theme override, and the shape
// here. So a leaked scope would make "After" green with nothing failing.
func TestAnElementScopeIsPoppedForTheFollowingSibling(t *testing.T) {
	w, ctx := mustResLoad(t, `
  <Gooey.Resources>
    <Resource Key="ink" Type="color" Value="#111111"/>
    <Style Key="body"><Setter Property="Fg" Resource="ink"/></Style>
  </Gooey.Resources>
  <VStack>
    <Text Name="Before" Style="body">before</Text>
    <Border>
      <Border.Resources>
        <Resource Key="ink" Type="color" Value="#00ff00"/>
        <Style Key="body"><Setter Property="Fg" Resource="ink"/></Style>
      </Border.Resources>
      <Text Name="Inner" Style="body">inner</Text>
    </Border>
    <Text Name="After" Style="body">after</Text>
  </VStack>`)

	outer, inner := render.RGB(0x11, 0x11, 0x11), render.RGB(0x00, 0xff, 0x00)
	if got := namedStyle(t, ctx, "Before").Fg; got != outer {
		t.Fatalf("before=%v, want %v", got, outer)
	}
	if got := namedStyle(t, ctx, "Inner").Fg; got != inner {
		t.Fatalf("inner=%v, want %v — the inner scope did not shadow", got, inner)
	}
	if got := namedStyle(t, ctx, "After").Fg; got != outer {
		t.Fatalf("the sibling AFTER the scope resolved %v, want %v — the scope was never popped", got, outer)
	}

	c := gooey.NewComposer(w, 20, 6)
	c.Frame()
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("the tree never settled (%d repainted)", n)
	}
	ctx.Resource("ink").(*prop.Property[render.Color]).Set(render.RGB(0xff, 0xaa, 0x3c))
	if _, n := c.Frame(); n != 2 {
		t.Errorf("a Set on the OUTER ink repainted %d, want the 2 readers outside the inner scope", n)
	}
}

// Resources is an ELEMENT-level slot like Name and the layout attributes,
// so checkProps exempts it globally — which means it is legal on elements
// whose builders declare no property elements at all, not only on the
// Border the tests above happen to use.
//
// The global exemption is only safe because a MISSPELLED slot is still a
// load error, and that second half is asserted here beside the first: an
// exemption keyed on the name "Resources" is one typo away from being an
// exemption for everything.
func TestAnyElementMayOpenAResourceScope(t *testing.T) {
	for _, el := range []string{"VStack", "HStack", "Grid", "Border"} {
		t.Run(el, func(t *testing.T) {
			_, ctx := mustResLoad(t, `
  <`+el+`>
    <`+el+`.Resources><Style Key="s" Bold="true"/></`+el+`.Resources>
    <Text Name="A" Style="s">x</Text>
  </`+el+`>`)
			if !namedStyle(t, ctx, "A").Bold {
				t.Errorf("<%s.Resources> parsed and its style never reached the child", el)
			}
		})
	}
	if _, _, err := resLoad(t, `<VStack><VStack.Resurces/><Text>x</Text></VStack>`); err == nil {
		t.Fatal("a misspelled element slot loaded clean; the global exemption is only safe because this fails")
	}
}
