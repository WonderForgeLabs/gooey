package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

func TestCommandAndCmdSatisfyAction(t *testing.T) {
	ran := 0
	var a gooey.Action = gooey.Command(func() { ran++ })
	if !a.CanExecute() {
		t.Error("a plain Command must always be executable")
	}
	a.Run()

	on := prop.NewSource(false)
	a = gooey.NewCommand(func() { ran++ }).When(on)
	if a.CanExecute() {
		t.Error("a command whose condition is false reported itself executable")
	}
	a.Run()
	if ran != 1 {
		t.Fatalf("ran %d times; Run must be a no-op while CanExecute is false", ran)
	}
	on.Set(true)
	if !a.CanExecute() {
		t.Error("the condition flipped true and CanExecute did not follow")
	}
	a.Run()
	if ran != 2 {
		t.Fatalf("ran %d times, want 2", ran)
	}

	// Nil in every shape a component can meet it.
	if gooey.CanExecute(nil) {
		t.Error("a nil Action is not executable")
	}
	if gooey.CanExecute(gooey.Command(nil)) {
		t.Error("a nil Command is not executable")
	}
	if gooey.CanExecute((*gooey.Cmd)(nil)) {
		t.Error("a nil *Cmd is not executable")
	}
	gooey.Command(nil).Run() // must not panic
	(*gooey.Cmd)(nil).Run()
	if gooey.CanExecute(gooey.NewCommand(nil)) {
		t.Error("a command with no func is not executable")
	}
}

// The whole point of CanExecute-as-a-computed: the button read the
// condition while painting, so flipping it repaints exactly that button.
func TestDisabledFlipRepaintsOneButton(t *testing.T) {
	dirty := prop.NewSource(false)
	save := &Button{
		Content: Str("save"),
		Click:   gooey.NewCommand(func() {}).When(dirty),
	}
	other := &Button{Content: Str("quit"), Click: gooey.Command(func() {})}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{save, other}}, 20, 4)
	c.Frame()

	dirty.Set(true)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("enabling one command painted %d components, want 1", painted)
	}
	dirty.Set(false)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("disabling one command painted %d components, want 1", painted)
	}
	// The other button never read the condition, so nothing about it is
	// dirty and a settled frame costs nothing.
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("a settled composition painted %d components, want 0", painted)
	}
}

// A condition built as a computed is the CanExecuteChanged story: the
// button depends on it transitively and nothing is raised by hand.
//
// Note what this does NOT claim. Invalidation in this graph is
// structural — a Set dirties every transitive dependent and evaluation
// happens later — so writing to the source under the condition repaints
// the button whether or not the derived bool actually moved. The
// guarantee is about WHICH components repaint, never about proving a
// value settled; a caller that wants the cheaper behavior compares
// before it Sets, exactly as ItemsView's row update does.
func TestCanExecuteThroughAComputed(t *testing.T) {
	text := prop.NewSource("")
	unrelated := prop.NewSource(0)
	canSave := prop.NewComputed(func() bool { return text.Get() != "" })
	save := &Button{Content: Str("save"), Click: gooey.NewCommand(func() {}).When(canSave)}
	other := &Button{Content: Str("quit"), Click: gooey.Command(func() {})}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{save, other}}, 20, 4)
	c.Frame()

	text.Set("x")
	if !canSave.Get() {
		t.Fatal("the computed condition did not follow its source")
	}
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("a computed condition turning true painted %d, want 1 (the button that read it)", painted)
	}
	// A property nothing in the chain reads costs nothing.
	unrelated.Set(1)
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("an unrelated property painted %d components, want 0", painted)
	}
}

func TestDisabledButtonRefusesEveryActivation(t *testing.T) {
	on := prop.NewSource(false)
	runs := 0
	b := &Button{Content: Str("go"), Click: gooey.NewCommand(func() { runs++ }).When(on)}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{b}}, 20, 3)
	c.Frame()
	x, y := b.Bounds().X, b.Bounds().Y

	if b.HandleKey(input.Named(input.KeyEnter)) {
		t.Error("a disabled button consumed enter; it must bubble on")
	}
	c.HandleMouse(press(x, y))
	c.HandleMouse(release(x, y))
	if runs != 0 {
		t.Fatalf("a disabled command ran %d times", runs)
	}
	if b.IsPressed() {
		t.Error("a disabled button took the pressed visual")
	}

	on.Set(true)
	if !b.HandleKey(input.Named(input.KeyEnter)) {
		t.Error("an enabled button declined enter")
	}
	c.HandleMouse(press(x, y))
	c.HandleMouse(release(x, y))
	if runs != 2 {
		t.Fatalf("enabled command ran %d times, want 2 (enter + click)", runs)
	}
}

// A disabled button paints dim, and painting is where the dependency is
// recorded — so the visual and the subscription are the same read.
func TestDisabledButtonPaintsDim(t *testing.T) {
	on := prop.NewSource(false)
	b := &Button{Content: Str("go"), Click: gooey.NewCommand(func() {}).When(on)}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{b}}, 20, 3)
	f, _ := c.Frame()
	if !f.Cells.At(b.Bounds().X, b.Bounds().Y).Style.Dim {
		t.Fatal("a disabled button did not paint dim")
	}
	on.Set(true)
	f, _ = c.Frame()
	if f.Cells.At(b.Bounds().X, b.Bounds().Y).Style.Dim {
		t.Fatal("an enabled button still paints dim")
	}
	// A button with no command at all is inert, not disabled.
	plain := &Button{Content: Str("x")}
	c2 := gooey.NewComposer(&VStack{Children: []gooey.Component{plain}}, 20, 3)
	f2, _ := c2.Frame()
	if f2.Cells.At(plain.Bounds().X, plain.Bounds().Y).Style.Dim {
		t.Error("a command-less button painted dim; it should look ordinary")
	}
}

// A gesture whose command is disabled is not consumed, so the key keeps
// bubbling and an outer binding can still have it.
func TestKeyBindingHonorsWhen(t *testing.T) {
	on := prop.NewSource(false)
	inner, outer := 0, 0
	pane := &dragPane{}
	scoped := &VStack{Children: []gooey.Component{pane}}
	scoped.Attach(&gooey.KeyBinding{
		Gesture: input.Rune('s'),
		Command: gooey.NewCommand(func() { inner++ }).When(on),
	})
	root := &VStack{Children: []gooey.Component{scoped}}
	root.Attach(&gooey.KeyBinding{Gesture: input.Rune('s'), Command: gooey.Command(func() { outer++ })})
	c := gooey.NewComposer(root, 20, 4)
	c.Frame()

	c.HandleKey(input.Rune('s'))
	if inner != 0 || outer != 1 {
		t.Fatalf("with the condition false: inner=%d outer=%d, want the disabled binding to pass the key on", inner, outer)
	}
	on.Set(true)
	c.HandleKey(input.Rune('s'))
	if inner != 1 || outer != 1 {
		t.Fatalf("with the condition true: inner=%d outer=%d, want the inner binding to win", inner, outer)
	}
}

func TestItemsViewActivateHonorsWhen(t *testing.T) {
	on := prop.NewSource(false)
	opens := 0
	_, _, v, c := newList(t, numbered(10), 20, 5)
	v.Activate = gooey.NewCommand(func() { opens++ }).When(on)
	c.Frame()

	if v.HandleKey(input.Named(input.KeyEnter)) {
		t.Error("a disabled Activate consumed enter")
	}
	if opens != 0 {
		t.Fatalf("a disabled Activate ran %d times", opens)
	}
	on.Set(true)
	if !v.HandleKey(input.Named(input.KeyEnter)) || opens != 1 {
		t.Fatalf("an enabled Activate did not run (opens=%d)", opens)
	}
}
