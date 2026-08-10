package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

func TestTabsMarkupBuilds(t *testing.T) {
	sel := prop.NewSource(0)
	fired := 0
	ctx := &Context{Values: map[string]any{
		"Tab":   sel,
		"OnTab": func() { fired++ },
	}}
	w := buildOne(t, doc(`<Tabs Name="Panel" Selected="{{.Tab}}" Changed="{{.OnTab}}">
	  <Tab Header="mcp"><Text>help text</Text></Tab>
	  <Tab Header="log"><Text>the log</Text></Tab>
	</Tabs>`), ctx)

	tb, ok := w.(*components.Tabs)
	if !ok {
		t.Fatalf("root is %T, want *components.Tabs", w)
	}
	if tb.Selected != sel {
		t.Fatal("Selected is not the viewmodel's own handle (lvalue semantics)")
	}
	if len(tb.Items) != 2 || tb.Items[0].Header.Get() != "mcp" || tb.Items[1].Header.Get() != "log" {
		t.Fatalf("items = %d, want the two declared tabs", len(tb.Items))
	}
	if ctx.Named["Panel"] != w {
		t.Fatal("Name did not register the Tabs")
	}
	tb.Select(1)
	if fired != 1 {
		t.Fatalf("Changed fired %d times, want 1", fired)
	}
}

// The bound selection is the whole switching mechanism: a Set on the
// viewmodel's handle swaps which page is on screen, erasing the old one.
func TestTabsMarkupSwitchSwapsThePages(t *testing.T) {
	sel := prop.NewSource(0)
	ctx := &Context{Values: map[string]any{"Tab": sel}}
	w := buildOne(t, doc(`<Tabs Selected="{{.Tab}}">
	  <Tab Header="one"><Text>PAGE-ONE</Text></Tab>
	  <Tab Header="two"><Text>PAGE-TWO</Text></Tab>
	</Tabs>`), ctx)

	c := gooey.NewComposer(w, 24, 4)
	fired := 0
	c.OnInvalidate(func() { fired++ })
	c.Frame()
	if got := cellRow(c.Cells(), 1); !strings.Contains(got, "PAGE-ONE") {
		t.Fatalf("row 1 = %q, want PAGE-ONE", got)
	}

	sel.Set(1)
	if fired == 0 {
		t.Fatal("Set on the bound selection did not invalidate the composition")
	}
	if _, painted := c.Frame(); painted != 3 {
		t.Fatalf("the bound switch painted %d components, want 3 (strip + outgoing + incoming)", painted)
	}
	if got := cellRow(c.Cells(), 1); !strings.Contains(got, "PAGE-TWO") || strings.Contains(got, "ONE") {
		t.Fatalf("row 1 after switch = %q, want PAGE-TWO with PAGE-ONE erased", got)
	}
}

// Selected is optional: an absent attribute leaves the control on its
// own internal selection, starting at the first tab.
func TestTabsMarkupSelectedIsOptional(t *testing.T) {
	w := buildOne(t, doc(`<Tabs>
	  <Tab Header="a"><Text>first</Text></Tab>
	  <Tab Header="b"><Text>second</Text></Tab>
	</Tabs>`), &Context{})
	c := gooey.NewComposer(w, 20, 4)
	c.Frame()
	if got := cellRow(c.Cells(), 1); !strings.Contains(got, "first") {
		t.Fatalf("row 1 = %q, want the first page by default", got)
	}
}

// A bound Header stays live: a Set repaints the strip — and only the
// strip.
func TestTabsMarkupHeaderBinds(t *testing.T) {
	hdr := prop.NewSource("live")
	ctx := &Context{Values: map[string]any{"H": hdr}}
	w := buildOne(t, doc(`<Tabs><Tab Header="{{.H}}"><Text>x</Text></Tab></Tabs>`), ctx)

	c := gooey.NewComposer(w, 20, 3)
	c.Frame()
	if got := cellRow(c.Cells(), 0); !strings.Contains(got, "live") {
		t.Fatalf("strip = %q, want the bound header", got)
	}
	hdr.Set("renamed")
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("a header change painted %d components, want 1 (the strip)", painted)
	}
	if got := cellRow(c.Cells(), 0); !strings.Contains(got, "renamed") {
		t.Fatalf("strip = %q, want the new header", got)
	}
}

// Non-visual children (a KeyBinding) attach to the Tabs like any
// container; visual non-<Tab> children are a load error.
func TestTabsMarkupTakesAttachments(t *testing.T) {
	ctx := &Context{Values: map[string]any{"Next": func() {}}}
	w := buildOne(t, doc(`<Tabs>
	  <KeyBinding Gesture="ctrl+t" Command="{{.Next}}"/>
	  <Tab Header="a"><Text>x</Text></Tab>
	</Tabs>`), ctx)
	if got := len(w.(*components.Tabs).Attachments()); got != 1 {
		t.Fatalf("attachments = %d, want the KeyBinding", got)
	}
}

func TestTabsMarkupLoadErrors(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{"no tabs", `<Tabs></Tabs>`, "at least one <Tab>"},
		{"visual non-Tab child", `<Tabs><Text>x</Text></Tabs>`, "must be <Tab>"},
		{"missing header", `<Tabs><Tab><Text>x</Text></Tab></Tabs>`, "needs a Header"},
		{"no content", `<Tabs><Tab Header="a"></Tab></Tabs>`, "exactly one content child"},
		{"two children", `<Tabs><Tab Header="a"><Text>x</Text><Text>y</Text></Tab></Tabs>`, "exactly one content child"},
		{"page binds visibility", `<Tabs><Tab Header="a"><Text Visibility="{{.V}}">x</Text></Tab></Tabs>`, "the Tabs owns it"},
		{"Tab outside Tabs", `<Tab Header="a"><Text>x</Text></Tab>`, "only valid directly inside <Tabs>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buildFails(t, doc(tc.src), &Context{Values: map[string]any{"V": prop.NewSource(true)}}, tc.want)
		})
	}
}
