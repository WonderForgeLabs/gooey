package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func cellRow(b *render.Buffer, y int) string {
	var sb strings.Builder
	for x := 0; x < b.W; x++ {
		sb.WriteRune(b.At(x, y).Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

// Visibility="{{...}}" binds a *prop.Property[gooey.Visibility]: the
// handle is live, a Set schedules a frame, and the flip costs exactly
// what the literal sweep costs (one repaint to hide a leaf).
func TestVisibilityBindsVisibilityHandle(t *testing.T) {
	vis := prop.NewSource(gooey.Visible)
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Text>keep</Text>
	    <Text Visibility="{{.Show}}">SECRET</Text>
	  </VStack>
	</Gooey>`
	w := buildOne(t, src, &Context{Values: map[string]any{"Show": vis}})

	c := gooey.NewComposer(w, 12, 3)
	fired := 0
	c.OnInvalidate(func() { fired++ })
	c.Frame()
	if got := cellRow(c.Cells(), 1); got != "SECRET" {
		t.Fatalf("row 1 = %q, want SECRET", got)
	}

	vis.Set(gooey.Hidden)
	if fired == 0 {
		t.Fatal("Set on the bound Visibility did not invalidate the composition")
	}
	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("bound hide painted %d components, want 1", painted)
	}
	if got := cellRow(c.Cells(), 1); got != "" {
		t.Errorf("row 1 after hide = %q, want erased", got)
	}

	vis.Set(gooey.Visible)
	c.Frame()
	if got := cellRow(c.Cells(), 1); got != "SECRET" {
		t.Errorf("row 1 after show = %q, want SECRET", got)
	}
}

// A bool handle binds too: true→Visible, false→Collapsed. Collapsing
// relayouts — the sibling below moves up into the reclaimed row.
func TestVisibilityBindsBoolHandle(t *testing.T) {
	show := prop.NewSource(true)
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Text Visibility="{{.Show}}">detail</Text>
	    <Text>footer</Text>
	  </VStack>
	</Gooey>`
	w := buildOne(t, src, &Context{Values: map[string]any{"Show": show}})

	c := gooey.NewComposer(w, 12, 3)
	c.Frame()
	if cellRow(c.Cells(), 0) != "detail" || cellRow(c.Cells(), 1) != "footer" {
		t.Fatalf("rows = %q,%q", cellRow(c.Cells(), 0), cellRow(c.Cells(), 1))
	}

	show.Set(false)
	c.Frame()
	if got := cellRow(c.Cells(), 0); got != "footer" {
		t.Errorf("row 0 after false = %q, want footer (collapsed reclaims the row)", got)
	}

	show.Set(true)
	c.Frame()
	if cellRow(c.Cells(), 0) != "detail" || cellRow(c.Cells(), 1) != "footer" {
		t.Errorf("rows after true = %q,%q", cellRow(c.Cells(), 0), cellRow(c.Cells(), 1))
	}
}

// A handle of any other type is a load-time error naming what the
// attribute accepts — lvalue semantics fail at build, never at frame.
func TestVisibilityBindingWrongTypeIsLoadError(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Text Visibility="{{.Show}}">x</Text>
	</Gooey>`
	_, err := Build([]byte(src), &Context{Values: map[string]any{"Show": prop.NewSource("nope")}})
	if err == nil {
		t.Fatal("expected a load error for a string-typed Visibility binding")
	}
	for _, want := range []string{"Visibility", "*prop.Property[gooey.Visibility]", "*prop.Property[bool]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// An unresolvable path is a load error too, same as every binding.
func TestVisibilityBindingUnknownPathIsLoadError(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Text Visibility="{{.Missing}}">x</Text>
	</Gooey>`
	if _, err := Build([]byte(src), &Context{Values: map[string]any{}}); err == nil {
		t.Fatal("expected a load error for an unresolvable Visibility binding")
	}
}

// The markup-only control tier: a control declares <x:Property
// Type="bool"/> and binds an inner element's Visibility to it, the page
// passes its own bool handle through the declared surface, and a Set on
// the page's viewmodel flips the element inside the control. No
// code-behind anywhere — this is what the bool mapping buys before a
// "visibility" declared type exists.
func TestBoundVisibilityInsideMarkupOnlyControl(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey>
  <Card Show="{{.ShowDetails}}"/>
</Gooey>`)},
		"card.gooey": {Data: []byte(`<Gooey xmlns:x="wonderforge.io/gooey/x">
  <x:Property Name="Show" Type="bool" Default="true"/>
  <VStack>
    <Text Visibility="{{.Show}}">details</Text>
    <Text>always</Text>
  </VStack>
</Gooey>`)},
	}
	show := prop.NewSource(true)
	ctx := &Context{
		Values:   map[string]any{"ShowDetails": show},
		Includes: fsys,
	}
	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 14, 3)
	c.Frame()
	if cellRow(c.Cells(), 0) != "details" || cellRow(c.Cells(), 1) != "always" {
		t.Fatalf("rows = %q,%q", cellRow(c.Cells(), 0), cellRow(c.Cells(), 1))
	}

	show.Set(false)
	c.Frame()
	if got := cellRow(c.Cells(), 0); got != "always" {
		t.Errorf("row 0 after hiding through the control boundary = %q, want %q", got, "always")
	}
}
