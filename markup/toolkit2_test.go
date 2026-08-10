package markup

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

func TestMenuBarMarkup(t *testing.T) {
	saved := 0
	can := prop.NewSource(true)
	ctx := &Context{Values: map[string]any{
		"Save": gooey.NewCommand(func() { saved++ }).When(can),
		"Quit": gooey.Command(func() {}),
	}}
	w := buildOne(t, doc(`<MenuBar>
		<Menu Title="File">
			<MenuItem Text="Save" Gesture="CTRL+s" Command="{{.Save}}"/>
			<MenuItem Separator="true"/>
			<MenuItem Text="Quit" Gesture="q" Command="{{.Quit}}"/>
		</Menu>
		<Menu Title="Edit">
			<MenuItem Text="Copy"/>
		</Menu>
	</MenuBar>`), ctx)

	bar, ok := w.(*components.MenuBar)
	if !ok {
		t.Fatalf("root is %T, want *components.MenuBar", w)
	}
	if len(bar.Menus) != 2 || bar.Menus[0].Title != "File" || len(bar.Menus[0].Items) != 3 {
		t.Fatalf("menus parsed as %+v", bar.Menus)
	}
	// Gestures are validated through ParseGesture and stored in the
	// canonical spelling — the hint on screen is the same string a
	// KeyBinding would round-trip.
	if got := bar.Menus[0].Items[0].Gesture; got != "ctrl+s" {
		t.Fatalf("gesture normalized to %q, want %q", got, "ctrl+s")
	}
	if !bar.Menus[0].Items[1].Separator {
		t.Fatal("the separator item did not parse")
	}
	// The bound command is the viewmodel's own conditional action.
	bar.Menus[0].Items[0].Action.Run()
	if saved != 1 {
		t.Fatal("the item's Command did not resolve to the bound action")
	}
	if bar.Menus[1].Items[0].Action != nil {
		t.Fatal("an item with no Command should carry a nil (inert) action")
	}
}

func TestMenuBarMarkupErrors(t *testing.T) {
	ctx := &Context{Values: map[string]any{}}
	buildFails(t, doc(`<MenuBar><Text>x</Text></MenuBar>`), ctx, "must be <Menu>")
	buildFails(t, doc(`<MenuBar><Menu><MenuItem Text="x"/></Menu></MenuBar>`), ctx, "needs a Title")
	buildFails(t, doc(`<MenuBar><Menu Title="F"><Button Content="x"/></Menu></MenuBar>`), ctx, "must be <MenuItem>")
	buildFails(t, doc(`<MenuBar><Menu Title="F"><MenuItem/></Menu></MenuBar>`), ctx, "needs Text")
	buildFails(t, doc(`<MenuBar><Menu Title="F"><MenuItem Text="x" Gesture="wat+z"/></Menu></MenuBar>`), ctx, "unknown modifier")
	buildFails(t, doc(`<MenuBar><Menu Title="F"><MenuItem Text="x" Command="{{.Nope}}"/></Menu></MenuBar>`), ctx, "not found in context")
}

func TestToastHostMarkup(t *testing.T) {
	ctx := &Context{Values: map[string]any{}}
	w := buildOne(t, doc(`<ToastHost Duration="5s"/>`), ctx)
	h, ok := w.(*components.ToastHost)
	if !ok {
		t.Fatalf("root is %T, want *components.ToastHost", w)
	}
	if h.Duration != 5*time.Second {
		t.Fatalf("Duration = %v, want 5s", h.Duration)
	}
}

func TestToastHostMarkupErrors(t *testing.T) {
	ctx := &Context{Values: map[string]any{}}
	buildFails(t, doc(`<ToastHost Duration="soon"/>`), ctx, `Duration="soon"`)
	buildFails(t, doc(`<ToastHost><Text>x</Text></ToastHost>`), ctx, "takes no children")
}
