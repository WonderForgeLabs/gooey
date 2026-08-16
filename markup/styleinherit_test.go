package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey/render"
)

// A UserControl's markup names styles from the PAGE's table, and it
// reaches them by inheritance: usercontrol.go:114 copies the parent's map
// into the child only when the child's is NIL. The unregistered-name check
// runs after that (the child is built at :137), so inheritance is intact —
// but it is intact by ORDERING, not by construction, and nothing said so.
//
// Reported as a hazard by a reader of that fix, correctly: three of
// cmd/reader's UserControl setups return a context with no Styles field
// while their markup says Style="panel". Had the check run one step
// earlier, all three would have become load errors.
func TestAUserControlInheritsThePageStyleTable(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey":  {Data: []byte(`<Gooey><Panel/></Gooey>`)},
		"panel.gooey": {Data: []byte(`<Gooey><Text Style="dim">x</Text></Gooey>`)},
	}
	ctx := &Context{
		Values:   map[string]any{},
		Styles:   map[string]render.Style{"dim": {Dim: true}},
		Includes: fsys,
	}
	if _, err := Load(fsys, "page.gooey", ctx); err != nil {
		t.Fatalf("a control naming a style the PAGE registered failed to load: %v", err)
	}
}

// The trap, stated as a test because it has already cost real time: an
// EMPTY BUT NON-NIL Styles map does not inherit. cmd/cards' test fixture
// carried `Styles: map[string]render.Style{}` while its markup named five
// styles, and adding the check turned all four of that demo's tests red.
// The fixture was wrong before the check existed — it just had no way to
// say so.
func TestAnEmptyStyleTableDoesNotInheritAndIsNotSilent(t *testing.T) {
	fsys := fstest.MapFS{
		"page.gooey":  {Data: []byte(`<Gooey><Panel/></Gooey>`)},
		"panel.gooey": {Data: []byte(`<Gooey><Text Style="dim">x</Text></Gooey>`)},
	}
	ctx := &Context{
		Values:   map[string]any{},
		Styles:   map[string]render.Style{"dim": {Dim: true}},
		Includes: fsys,
		Components: map[string]Builder{
			// The control's own setup supplies an empty map, so nothing
			// is inherited and "dim" is genuinely absent inside it.
			"Panel": UserControl(fsys, "panel.gooey", func(e Element, parent *Context) (*Context, error) {
				return &Context{Values: map[string]any{}, Styles: map[string]render.Style{}}, nil
			}),
		},
	}
	_, err := Load(fsys, "page.gooey", ctx)
	if err == nil {
		t.Fatal("a control with an empty style table silently ignored a name its markup asked for")
	}
	if !strings.Contains(err.Error(), "dim") {
		t.Fatalf("error %q does not name the style the control asked for", err)
	}
}
