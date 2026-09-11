package markup

import (
	"strings"
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

// TestASeparatorIsSpelledOneWay is hand-written because NO SWEEP CAN
// REACH IT, and that is the interesting half.
//
// bindsweep_test.go derives its arms from AttrSpec declarations, and
// records two categories it cannot see. This is a THIRD: <Menu> and
// <MenuItem> are ModeRestricted children with no AttrSpec at all, so
// Separator is not a declaration that could be widened — it is a
// declaration that does not exist. A sweep over the declared surface
// will never fail here no matter how the reader is spelled.
//
// The reader was `ic.Attrs["Separator"] == "true"` until review of #470.
// A string compare is not a bool grammar: Separator="1" and
// Separator="yes" loaded as ORDINARY ITEMS, so a separator spelled the
// way half of Go spells a bool became a blank menu entry — and then hit
// the "needs Text" check or, with Text present, silently became a
// clickable row. litBool is the same reader the ten declared bools use.
func TestASeparatorIsSpelledOneWay(t *testing.T) {
	ctx := &Context{Values: map[string]any{}}
	// Text="x" IS LOAD-BEARING on this probe, and leaving it off is how
	// the first version of this test passed against the bug. A bare
	// <MenuItem Separator="1"/> does fail to load — with "needs Text",
	// because the string compare quietly made it an ordinary item and
	// ordinary items need text. The assertion has to be that the refusal
	// is ABOUT SEPARATOR, on an element that would otherwise load.
	//
	// " true " is NOT among the bad spellings: litBool trims, exactly as
	// litInt accepts " 3 ", and one whitespace rule across the dialect is
	// the point rather than an exception to it.
	for _, bad := range []string{"1", "yes", "TRUE", "True", ""} {
		src := doc(`<MenuBar><Menu Title="F"><MenuItem Text="x" Separator="` + bad +
			`"/></Menu></MenuBar>`)
		_, err := Build([]byte(src), ctx)
		if err == nil {
			t.Errorf(`<MenuItem Text="x" Separator=%q> loads. Every spelling but `+
				`"true" and "false" has to be a load error, or the item quietly `+
				`stops being a separator`, bad)
			continue
		}
		if !strings.Contains(err.Error(), "Separator") {
			t.Errorf(`<MenuItem Text="x" Separator=%q> is refused, but not for the `+
				`attribute: %v`, bad, err)
		}
	}
	// Both real spellings still mean what they say, which is what keeps
	// the loop above off "refuse everything".
	w := buildOne(t, doc(`<MenuBar><Menu Title="F">`+
		`<MenuItem Separator="true"/>`+
		`<MenuItem Text="x" Separator="false"/>`+
		`</Menu></MenuBar>`), ctx)
	bar := w.(*components.MenuBar)
	if len(bar.Menus[0].Items) != 2 {
		t.Fatalf("items parsed as %+v", bar.Menus[0].Items)
	}
	if !bar.Menus[0].Items[0].Separator {
		t.Error(`Separator="true" did not make a separator`)
	}
	if bar.Menus[0].Items[1].Separator || bar.Menus[0].Items[1].Text != "x" {
		t.Errorf(`Separator="false" did not stay an ordinary item: %+v`,
			bar.Menus[0].Items[1])
	}
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
