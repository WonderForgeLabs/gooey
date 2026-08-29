package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

func menuCtx() *Context {
	return &Context{Values: map[string]any{
		"Wrapped": prop.NewSource(true),
		"Label":   "$EDITOR (nvim)",
		"Count":   7,
		"Live":    prop.NewSource("changes"),
		"Do":      gooey.Command(func() {}),
	}}
}

func buildMenu(t *testing.T, src string) *components.MenuBar {
	t.Helper()
	w, err := Build([]byte(src), menuCtx())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	bar, ok := w.(*components.MenuBar)
	if !ok {
		t.Fatalf("built %T, want *components.MenuBar", w)
	}
	return bar
}

// TestCheckedBindsTheSameHandleTheAppHolds — lvalue semantics, which is
// the whole point: the item and whatever writes the state are looking at
// one property, not two copies.
func TestCheckedBindsTheSameHandleTheAppHolds(t *testing.T) {
	ctx := menuCtx()
	w, err := Build([]byte(`<Gooey><MenuBar><Menu Title="V">
		<MenuItem Text="Wrap" Checked="{{.Wrapped}}" Command="{{.Do}}"/>
	</Menu></MenuBar></Gooey>`), ctx)
	if err != nil {
		t.Fatal(err)
	}
	bar := w.(*components.MenuBar)
	got := bar.Menus[0].Items[0].Checked
	if got == nil {
		t.Fatal("Checked did not bind")
	}
	if got != ctx.Values["Wrapped"] {
		t.Error("the item bound a different handle than the context holds; a check that is " +
			"a COPY of the state cannot stay in step with it")
	}
	ctx.Values["Wrapped"].(*prop.Property[bool]).Set(false)
	if got.Get() {
		t.Error("writing the context's handle did not move the item's check")
	}
}

// TestALiteralCheckedIsALoadError. There is no such thing as a check item
// whose box can never change, and accepting Checked="true" would produce
// exactly that — silently, and looking correct on the first frame.
func TestALiteralCheckedIsALoadError(t *testing.T) {
	_, err := Build([]byte(`<Gooey><MenuBar><Menu Title="V">
		<MenuItem Text="Wrap" Checked="true" Command="{{.Do}}"/>
	</Menu></MenuBar></Gooey>`), menuCtx())
	if err == nil {
		t.Fatal(`<MenuItem Checked="true"> loaded`)
	}
	if !strings.Contains(err.Error(), "bool handle") {
		t.Errorf("the error does not say what Checked needs: %v", err)
	}
}

// TestCheckedRefusesAWrongType — everything resolvable fails at load.
func TestCheckedRefusesAWrongType(t *testing.T) {
	_, err := Build([]byte(`<Gooey><MenuBar><Menu Title="V">
		<MenuItem Text="Wrap" Checked="{{.Live}}" Command="{{.Do}}"/>
	</Menu></MenuBar></Gooey>`), menuCtx())
	if err == nil {
		t.Fatal("Checked bound a string handle")
	}
}

// TestMenuItemTextResolvesAPlainString is what lets an application label
// an item with something the markup cannot know — a resolved $EDITOR,
// most of all.
func TestMenuItemTextResolvesAPlainString(t *testing.T) {
	bar := buildMenu(t, `<Gooey><MenuBar><Menu Title="V">
		<MenuItem Text="{{.Label}}" Command="{{.Do}}"/>
	</Menu></MenuBar></Gooey>`)
	if got := bar.Menus[0].Items[0].Text; got != "$EDITOR (nvim)" {
		t.Errorf("the item's text is %q; a bound Text must resolve, not be printed", got)
	}
}

// TestMenuItemTextRefusesAPropertyHandle. MenuItem.Text is a plain string
// field, so a handle would be sampled ONCE and then silently stop
// tracking — the bound label that never updates. Refusing says so at
// load, where it can be fixed.
func TestMenuItemTextRefusesAPropertyHandle(t *testing.T) {
	_, err := Build([]byte(`<Gooey><MenuBar><Menu Title="V">
		<MenuItem Text="{{.Live}}" Command="{{.Do}}"/>
	</Menu></MenuBar></Gooey>`), menuCtx())
	if err == nil {
		t.Fatal("a property handle was accepted as MenuItem Text; it would be sampled once " +
			"and never update again")
	}
	if !strings.Contains(err.Error(), "static string") {
		t.Errorf("the error does not explain the limit: %v", err)
	}
}

// TestMenuItemTextRefusesANonString — an int is resolvable and wrong.
func TestMenuItemTextRefusesANonString(t *testing.T) {
	if _, err := Build([]byte(`<Gooey><MenuBar><Menu Title="V">
		<MenuItem Text="{{.Count}}" Command="{{.Do}}"/>
	</Menu></MenuBar></Gooey>`), menuCtx()); err == nil {
		t.Fatal("an int was accepted as MenuItem Text")
	}
}

// TestAnUnknownTextBindingIsALoadError — the path has to be checked, not
// left to render as its own template source.
func TestAnUnknownTextBindingIsALoadError(t *testing.T) {
	if _, err := Build([]byte(`<Gooey><MenuBar><Menu Title="V">
		<MenuItem Text="{{.Nonesuch}}" Command="{{.Do}}"/>
	</Menu></MenuBar></Gooey>`), menuCtx()); err == nil {
		t.Fatal("an unknown Text binding loaded")
	}
}
