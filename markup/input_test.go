package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
)

func TestButtonAndKeyBindingFromMarkup(t *testing.T) {
	fsys := fstest.MapFS{"page.gooey": {Data: []byte(`<Gooey>
  <VStack>
    <Button Name="save" Content="Save" Click="{{.Save}}"/>
    <Button Content="Quit" Click="OnQuit"/>
    <KeyBinding Gesture="ctrl+s" Command="{{.Save}}"/>
  </VStack>
</Gooey>`)}}

	saves, quits := 0, 0
	ctx := &Context{
		Values:   map[string]any{"Save": gooey.Command(func() { saves++ })},
		Handlers: map[string]gooey.Command{"OnQuit": func() { quits++ }},
	}
	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out := renderToString(t, w, 20, 3); !strings.Contains(out, "[ Save ]") {
		t.Fatalf("button did not render:\n%s", out)
	}

	// The KeyBinding is an attachment, not a laid-out child.
	if kids := w.(*components.VStack).ChildComponents(); len(kids) != 2 {
		t.Fatalf("VStack has %d visual children, want 2", len(kids))
	}

	c := gooey.NewComposer(w, 20, 3)
	if !c.HandleKey(input.Named(input.KeyEnter)) { // focus starts on Save
		t.Fatal("enter did not reach the focused button")
	}
	c.Focus().FocusNext()
	c.HandleKey(input.Named(input.KeyEnter))
	if !c.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 's', Mods: input.ModCtrl}) {
		t.Fatal("ctrl+s binding did not fire")
	}
	if saves != 2 || quits != 1 {
		t.Fatalf("saves=%d quits=%d, want 2 and 1", saves, quits)
	}
}

func TestKeyBindingInsideBorder(t *testing.T) {
	fsys := fstest.MapFS{"page.gooey": {Data: []byte(`<Gooey>
  <Border Title="pane">
    <Text>body</Text>
    <KeyBinding Gesture="enter" Command="{{.Open}}"/>
  </Border>
</Gooey>`)}}
	opened := 0
	ctx := &Context{Values: map[string]any{"Open": gooey.Command(func() { opened++ })}}
	w, err := Load(fsys, "page.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if out := renderToString(t, w, 20, 4); !strings.Contains(out, "body") {
		t.Fatalf("border child did not render:\n%s", out)
	}
	if !gooey.NewComposer(w, 20, 4).HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("binding attached to the Border did not fire")
	}
	if opened != 1 {
		t.Fatalf("command ran %d times, want 1", opened)
	}
}

func TestEventBindingErrors(t *testing.T) {
	cases := map[string]string{
		"bad gesture":      `<Gooey><KeyBinding Gesture="ctrl+nope" Command="{{.Save}}"/></Gooey>`,
		"unknown handler":  `<Gooey><Button Content="x" Click="Missing"/></Gooey>`,
		"unbound command":  `<Gooey><KeyBinding Gesture="j" Command="{{.Missing}}"/></Gooey>`,
		"not a func value": `<Gooey><Button Content="x" Click="{{.NotAFunc}}"/></Gooey>`,
	}
	for name, src := range cases {
		fsys := fstest.MapFS{"page.gooey": {Data: []byte(src)}}
		ctx := &Context{Values: map[string]any{
			"Save": gooey.Command(func() {}), "NotAFunc": "hello",
		}}
		if _, err := Load(fsys, "page.gooey", ctx); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
