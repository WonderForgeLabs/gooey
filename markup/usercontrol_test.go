package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func renderToString(t *testing.T, w gooey.Widget, cols, rows int) string {
	t.Helper()
	f := gooey.Compose(w, term.Caps{Cols: cols, Rows: rows, CellW: 10, CellH: 20}, nil)
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			sb.WriteRune(f.Cells.At(x, y).Rune)
		}
		sb.WriteRune('\n')
	}
	return sb.String()
}

func TestMarkupOnlyUserControl(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey>
  <VStack>
    <Card Title="{{.Header}}" Sub="static subtitle"/>
  </VStack>
</Gooey>`)},
		"card.gooey": {Data: []byte(`<Gooey>
  <Border Title="{{.Title}}">
    <Text>{{.Sub}}</Text>
  </Border>
</Gooey>`)},
	}
	header := prop.NewSource("live title")
	ctx := &Context{
		Values:   map[string]any{"Header": header},
		Styles:   map[string]render.Style{},
		Includes: fsys, // <Card/> resolves to card.gooey by convention
	}
	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	out := renderToString(t, w, 30, 5)
	if !strings.Contains(out, "live title") {
		t.Fatalf("bound attribute did not reach control:\n%s", out)
	}
	if !strings.Contains(out, "static subtitle") {
		t.Fatalf("literal attribute did not reach control:\n%s", out)
	}

	// The binding is a live handle, not a copied value.
	header.Set("changed")
	if out := renderToString(t, w, 30, 5); !strings.Contains(out, "changed") {
		t.Fatalf("attribute binding is not live:\n%s", out)
	}
}

func TestMarkupOnlyControlUnknownBindingErrors(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey": {Data: []byte(`<Gooey><Card Title="{{.Missing}}"/></Gooey>`)},
		"card.gooey": {Data: []byte(`<Gooey><Text>{{.Title}}</Text></Gooey>`)},
	}
	ctx := &Context{Values: map[string]any{}, Includes: fsys}
	if _, err := Load(fsys, "page.gooey", ctx); err == nil {
		t.Fatal("expected error for unresolvable attribute binding")
	}
}
