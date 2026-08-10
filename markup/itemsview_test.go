package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func inputEnter() input.KeyEvent      { return input.Named(input.KeyEnter) }
func inputDown() input.KeyEvent       { return input.Named(input.KeyDown) }
func inputRune(r rune) input.KeyEvent { return input.Rune(r) }

type post struct{ Title, Date string }

func postSource(items ...post) *prop.Property[[]post] { return prop.NewSource(items) }

func postItems(p *prop.Property[[]post]) *prop.Property[components.ItemSource] {
	return components.Items(p, func(x post) map[string]any {
		return map[string]any{"Title": x.Title, "Date": x.Date}
	})
}

func listCtx(p *prop.Property[[]post], extra map[string]any) *Context {
	vals := map[string]any{"Posts": postItems(p), "Sel": prop.NewSource(0)}
	for k, v := range extra {
		vals[k] = v
	}
	return &Context{
		Values: vals,
		Styles: map[string]render.Style{"dim": {Fg: render.RGB(120, 120, 120)}},
	}
}

const listPage = `<Gooey>
  <ItemsView Items="{{.Posts}}" Selected="{{.Sel}}">
    <ItemsView.ItemTemplate>
      <HStack Gap="1">
        <Text>{{.Title}}</Text>
        <Text Style="dim">{{.Date}}</Text>
      </HStack>
    </ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`

func TestPropertyElementBuildsATemplate(t *testing.T) {
	p := postSource(post{"first", "jan"}, post{"second", "feb"})
	w, err := Build([]byte(listPage), listCtx(p, nil))
	if err != nil {
		t.Fatal(err)
	}
	v, ok := w.(*components.ItemsView)
	if !ok {
		t.Fatalf("root is %T, want *components.ItemsView", w)
	}
	c := gooey.NewComposer(v, 30, 4)
	f, _ := c.Frame()
	if got := lineOf(f.Cells, 0, 30); got != "first jan" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := lineOf(f.Cells, 1, 30); got != "second feb" {
		t.Fatalf("row 1 = %q", got)
	}
}

func lineOf(b *render.Buffer, y, w int) string {
	var sb strings.Builder
	for x := 0; x < w; x++ {
		sb.WriteRune(b.At(x, y).Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

// The template is a FACTORY: one captured subtree, many instances, each
// bound to its own item.
func TestTemplateInstantiatesPerItem(t *testing.T) {
	p := postSource(post{"a", "1"}, post{"b", "2"}, post{"c", "3"})
	w, err := Build([]byte(listPage), listCtx(p, nil))
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 30, 3)
	f, _ := c.Frame()
	for i, want := range []string{"a 1", "b 2", "c 3"} {
		if got := lineOf(f.Cells, i, 30); got != want {
			t.Fatalf("row %d = %q, want %q", i, got, want)
		}
	}
}

// Per-item context isolation, the UserControl rule applied per row: dot
// is the ITEM, so the page's own values are not in scope inside a
// template and referring to one is a load error.
func TestTemplateContextIsIsolatedFromThePage(t *testing.T) {
	p := postSource(post{"a", "1"})
	src := `<Gooey>
  <ItemsView Items="{{.Posts}}">
    <ItemsView.ItemTemplate>
      <Text>{{.PageOnly}}</Text>
    </ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	ctx := listCtx(p, map[string]any{"PageOnly": prop.NewSource("leaked")})
	_, err := Build([]byte(src), ctx)
	if err == nil {
		t.Fatal("a template resolved a PAGE value; item contexts must be isolated")
	}
	if !strings.Contains(err.Error(), "PageOnly") {
		t.Fatalf("error = %v, want it to name the unresolvable path", err)
	}
}

// Bindings inside a template are checked at LOAD, like every other
// binding in the document — as long as there is an item to check against.
func TestTemplateBindingTypoFailsTheLoad(t *testing.T) {
	p := postSource(post{"a", "1"})
	src := `<Gooey>
  <ItemsView Items="{{.Posts}}">
    <ItemsView.ItemTemplate><Text>{{.Titel}}</Text></ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	_, err := Build([]byte(src), listCtx(p, nil))
	if err == nil || !strings.Contains(err.Error(), "Titel") {
		t.Fatalf("err = %v, want a load error naming the bad path", err)
	}
}

func TestTemplateMayPlaceRegisteredComponents(t *testing.T) {
	p := postSource(post{"a", "1"}, post{"b", "2"})
	built := 0
	ctx := listCtx(p, nil)
	ctx.Components = map[string]Builder{
		"Badge": func(e Element, c *Context) (gooey.Component, error) {
			built++
			label, err := bindText(e.Attrs["Text"], c)
			if err != nil {
				return nil, err
			}
			return &components.Text{Content: label}, nil
		},
	}
	src := `<Gooey>
  <ItemsView Items="{{.Posts}}">
    <ItemsView.ItemTemplate><Badge Text="[{{.Title}}]"/></ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	w, err := Build([]byte(src), ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 2)
	f, _ := c.Frame()
	if got := lineOf(f.Cells, 0, 20); got != "[a]" {
		t.Fatalf("row 0 = %q — a registered component did not compose inside the template", got)
	}
	if built < 2 {
		t.Fatalf("the registered builder ran %d times, want one per realized row", built)
	}
}

func TestActivateAndSelectedBind(t *testing.T) {
	p := postSource(post{"a", "1"}, post{"b", "2"})
	opened := 0
	ctx := listCtx(p, map[string]any{"Open": gooey.Command(func() { opened++ })})
	src := `<Gooey>
  <ItemsView Items="{{.Posts}}" Selected="{{.Sel}}" Activate="{{.Open}}">
    <ItemsView.ItemTemplate><Text>{{.Title}}</Text></ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	w, err := Build([]byte(src), ctx)
	if err != nil {
		t.Fatal(err)
	}
	v := w.(*components.ItemsView)
	c := gooey.NewComposer(v, 20, 2)
	c.Frame()
	if !c.HandleKey(inputEnter()) {
		t.Fatal("enter was not routed to the focused ItemsView")
	}
	if opened != 1 {
		t.Fatalf("Activate ran %d times, want 1", opened)
	}
	sel := ctx.Values["Sel"].(*prop.Property[int])
	c.HandleKey(inputDown())
	if sel.Get() != 1 {
		t.Fatalf("Selected = %d after ↓, want 1 — the binding is not shared with the viewmodel", sel.Get())
	}
}

func TestSelectionChangedBindsAndFiresOnAViewMove(t *testing.T) {
	p := postSource(post{"a", "1"}, post{"b", "2"})
	changed := 0
	ctx := listCtx(p, map[string]any{"Reset": gooey.Command(func() { changed++ })})
	src := `<Gooey>
  <ItemsView Items="{{.Posts}}" Selected="{{.Sel}}" SelectionChanged="{{.Reset}}">
    <ItemsView.ItemTemplate><Text>{{.Title}}</Text></ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	w, err := Build([]byte(src), ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 2)
	c.Frame()
	c.HandleKey(inputDown())
	if changed != 1 {
		t.Fatalf("SelectionChanged ran %d times after ↓, want 1", changed)
	}
}

func TestFocusableFalseTakesTheViewOutOfTheTabOrder(t *testing.T) {
	p := postSource(post{"a", "1"})
	src := `<Gooey>
  <ItemsView Items="{{.Posts}}" Selected="{{.Sel}}" Focusable="false">
    <ItemsView.ItemTemplate><Text>{{.Title}}</Text></ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	w, err := Build([]byte(src), listCtx(p, nil))
	if err != nil {
		t.Fatal(err)
	}
	if w.(*components.ItemsView).AcceptsFocus() {
		t.Fatal(`Focusable="false" left the view in the tab order`)
	}

	bad := `<Gooey>
  <ItemsView Items="{{.Posts}}" Focusable="nope">
    <ItemsView.ItemTemplate><Text>{{.Title}}</Text></ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	if _, err := Build([]byte(bad), listCtx(p, nil)); err == nil || !strings.Contains(err.Error(), "Focusable") {
		t.Fatalf("err = %v, want a load error naming Focusable", err)
	}
}

// A template that names the reserved value takes the selection visual
// over; otherwise the view draws the house highlight.
func TestTemplateMentioningSelectedSuppressesTheHouseHighlight(t *testing.T) {
	p := postSource(post{"a", "1"})
	plain, err := Build([]byte(listPage), listCtx(p, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !plain.(*components.ItemsView).Highlight {
		t.Fatal("a template that ignores _selected must get the house highlight")
	}

	src := `<Gooey>
  <ItemsView Items="{{.Posts}}" Selected="{{.Sel}}">
    <ItemsView.ItemTemplate><Checkbox Checked="{{._selected}}" Label="{{.Title}}"/></ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	custom, err := Build([]byte(src), listCtx(p, nil))
	if err != nil {
		t.Fatal(err)
	}
	if custom.(*components.ItemsView).Highlight {
		t.Fatal("a template that binds _selected must suppress the house highlight")
	}
}

func TestPropertyElementErrors(t *testing.T) {
	p := postSource(post{"a", "1"})
	cases := []struct {
		name, src, want string
	}{{
		name: "on an element that does not accept them",
		src:  `<Gooey><VStack><VStack.ItemTemplate><Text>x</Text></VStack.ItemTemplate></VStack></Gooey>`,
		want: "does not accept the property element",
	}, {
		name: "naming an owner that is not the parent",
		src:  `<Gooey><VStack><Grid.ItemTemplate><Text>x</Text></Grid.ItemTemplate></VStack></Gooey>`,
		want: "is a property of <Grid>",
	}, {
		name: "given twice",
		src: `<Gooey><ItemsView Items="{{.Posts}}">
			<ItemsView.ItemTemplate><Text>a</Text></ItemsView.ItemTemplate>
			<ItemsView.ItemTemplate><Text>b</Text></ItemsView.ItemTemplate>
		</ItemsView></Gooey>`,
		want: "given twice",
	}, {
		name: "carrying attributes",
		src:  `<Gooey><ItemsView Items="{{.Posts}}"><ItemsView.ItemTemplate Width="3"><Text>a</Text></ItemsView.ItemTemplate></ItemsView></Gooey>`,
		want: "takes no attributes",
	}, {
		name: "with more than one child",
		src: `<Gooey><ItemsView Items="{{.Posts}}"><ItemsView.ItemTemplate>
			<Text>a</Text><Text>b</Text>
		</ItemsView.ItemTemplate></ItemsView></Gooey>`,
		want: "exactly one child element",
	}, {
		name: "missing altogether",
		src:  `<Gooey><ItemsView Items="{{.Posts}}"/></Gooey>`,
		want: "needs an <ItemsView.ItemTemplate>",
	}, {
		name: "with visual children beside the template",
		src: `<Gooey><ItemsView Items="{{.Posts}}">
			<ItemsView.ItemTemplate><Text>a</Text></ItemsView.ItemTemplate>
			<Text>stray</Text>
		</ItemsView></Gooey>`,
		want: "takes no visual children",
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Build([]byte(tc.src), listCtx(p, nil))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestItemsAttributeMustBeAnItemSource(t *testing.T) {
	src := `<Gooey>
  <ItemsView Items="{{.NotASource}}">
    <ItemsView.ItemTemplate><Text>{{.Title}}</Text></ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	ctx := &Context{Values: map[string]any{"NotASource": prop.NewSource("nope")}}
	_, err := Build([]byte(src), ctx)
	if err == nil || !strings.Contains(err.Error(), "ItemSource") {
		t.Fatalf("err = %v, want both types named", err)
	}
}

func TestKeyBindingStillAttachesToAnItemsView(t *testing.T) {
	p := postSource(post{"a", "1"})
	hit := 0
	ctx := listCtx(p, map[string]any{"Refresh": gooey.Command(func() { hit++ })})
	src := `<Gooey>
  <ItemsView Items="{{.Posts}}" Selected="{{.Sel}}">
    <ItemsView.ItemTemplate><Text>{{.Title}}</Text></ItemsView.ItemTemplate>
    <KeyBinding Gesture="r" Command="{{.Refresh}}"/>
  </ItemsView>
</Gooey>`
	w, err := Build([]byte(src), ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 2)
	c.Frame()
	c.HandleKey(inputRune('r'))
	if hit != 1 {
		t.Fatalf("the attached KeyBinding fired %d times, want 1", hit)
	}
}
