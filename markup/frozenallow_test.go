package markup

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// <Frozen Allow="…">: freezing with exceptions.
//
// These tests drive the BUILTIN <Frozen> element and components.Frozen —
// not the frozenHost probe the older freeze tests register, which shadows
// the builtin name. That is deliberate: what is being pinned here is the
// vocabulary a page writes, so the page has to be the thing under test.
//
// Every test is a PAIR. A permission that is granted has to be shown
// changing an outcome that the same page, with the permission withheld,
// does not produce — otherwise a category could be silently inert and
// the test would still be green. The freeze tests learned that lesson the
// hard way (see TestFocusCannotBeSetIntoAFrozenSubtree's comment), and a
// vocabulary of thirteen categories is thirteen more chances to make the
// same mistake.

// allowPage builds and composes WITHOUT withFrozen, so <Frozen> resolves
// to the builtin element rather than to the test probe.
func allowPage(t *testing.T, src string, ctx *Context) *gooey.Composer {
	t.Helper()
	if ctx == nil {
		ctx = &Context{}
	}
	w, err := Build([]byte(src), ctx)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	c := gooey.NewComposer(w, 30, 8)
	c.Frame()
	return c
}

// frozenWrap renders a page template with the <Frozen> wrapper carrying
// the given attributes, so two arms differ by exactly the Allow set.
func frozenWrap(tmpl, attrs string) string {
	open := "<Frozen>"
	if attrs != "" {
		open = "<Frozen " + attrs + ">"
	}
	return fmt.Sprintf(tmpl, open, "</Frozen>")
}

// ---- the base case: a bare <Frozen> is the bool it generalizes ----

func TestABareFrozenElementIsTheBoolItReplaces(t *testing.T) {
	frozen := allowPage(t, frozenWrap(twoBoxes, ""), boxCtx())
	if n := boxesIn(frozen.Focus().Order()); n != 1 {
		t.Errorf("a bare <Frozen> left %d TextBox focus stops, want 1 (only the outside one)", n)
	}
	live := allowPage(t, fmt.Sprintf(twoBoxes, "", ""), boxCtx())
	if n := boxesIn(live.Focus().Order()); n != 2 {
		t.Errorf("the control has %d TextBox focus stops, want 2", n)
	}
}

func TestFrozenAllowAllIsNotFrozenAtAll(t *testing.T) {
	c := allowPage(t, frozenWrap(twoBoxes, `Allow="All"`), boxCtx())
	if n := boxesIn(c.Focus().Order()); n != 2 {
		t.Errorf(`<Frozen Allow="All"> left %d TextBox focus stops, want 2: `+
			`AllowAll is the same value "not frozen" has, so the two must be `+
			`indistinguishable`, n)
	}
}

// ---- Hover and Pointer are separate doors ----

const allowButtonPage = `<Gooey>
  <VStack>
    %s<Button Name="btn" Content="go" Click="{{.Fire}}"/>%s
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

func allowButtonCtx(fired *int) *Context {
	return &Context{Values: map[string]any{
		"Fire": gooey.Command(func() { *fired++ }),
		"Out":  prop.NewSource("out"),
	}}
}

// clickButton drives a full press/release on the button named btn and
// reports whether its Click ran and whether the pointer hovered it.
func clickButton(t *testing.T, attrs string) (fired int, hovered bool) {
	t.Helper()
	ctx := allowButtonCtx(&fired)
	c := allowPage(t, frozenWrap(allowButtonPage, attrs), ctx)
	btn, err := Find[*components.Button](ctx, "btn")
	if err != nil {
		t.Fatal(err)
	}
	b := btn.Bounds()
	c.HandleMouse(input.MouseEvent{Kind: input.MouseMove, X: b.X, Y: b.Y})
	hovered = c.Focus().Hovered() == gooey.Component(btn)
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: b.X, Y: b.Y})
	c.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, X: b.X, Y: b.Y})
	return fired, hovered
}

// TestAllowHoverLightsTheDescendantWhileTheClickStillStopsAtTheHost is
// the test that earns Hover and Pointer being two categories rather than
// one "Mouse".
//
// It is also the design surface's actual requirement, stated as an
// assertion: the element under the pointer highlights, so the user can
// see what they are about to select, and clicking it does nothing, so the
// editor — not the document — decides what a click means.
func TestAllowHoverLightsTheDescendantWhileTheClickStillStopsAtTheHost(t *testing.T) {
	fired, hovered := clickButton(t, `Allow="Hover"`)
	if !hovered {
		t.Error(`Allow="Hover" did not hover the Button inside the picture`)
	}
	if fired != 0 {
		t.Errorf(`Allow="Hover" fired the Button's Click %d times, want 0: `+
			`Pointer was not granted`, fired)
	}

	// Both controls, because each half needs its own.
	fired, hovered = clickButton(t, "")
	if hovered {
		t.Error("a bare <Frozen> hovered the Button, so the Hover arm proved nothing")
	}
	if fired != 0 {
		t.Errorf("a bare <Frozen> fired Click %d times, want 0", fired)
	}
	fired, hovered = clickButton(t, `Allow="Mouse"`)
	if !hovered || fired != 1 {
		t.Errorf(`Allow="Mouse" hovered=%v fired=%d, want true/1: without this the `+
			`click assertion above could be a page that never clicks anything`,
			hovered, fired)
	}
}

// ---- Focus, and the key classes that ride on it ----

// typeInto focuses the TextBox named inside and dispatches ev, returning
// what the bound text ended up as.
func typeInto(t *testing.T, attrs string, ev input.KeyEvent) string {
	t.Helper()
	ctx := boxCtx()
	c := allowPage(t, frozenWrap(twoBoxes, attrs), ctx)
	box, err := Find[*components.TextBox](ctx, "inside")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Focus().SetFocus(box) {
		return "<unfocusable>"
	}
	c.HandleKey(ev)
	return ctx.Values["In"].(*prop.Property[string]).Get()
}

// TestAllowFocusAloneReachesTheSubtreeButDeliversNoLetters is the pair
// that shows Focus and the key classes are genuinely separate — and it is
// the one that would go green on an implementation where granting focus
// quietly granted everything.
func TestAllowFocusAloneReachesTheSubtreeButDeliversNoLetters(t *testing.T) {
	// A freshly focused TextBox has its caret at 0, so a delivered rune
	// lands in FRONT of the existing text.
	if got := typeInto(t, `Allow="Focus"`, input.Rune('x')); got != "in" {
		t.Errorf(`Allow="Focus" let a letter through: text is %q, want %q`, got, "in")
	}
	if got := typeInto(t, `Allow="Alpha"`, input.Rune('x')); got != "xin" {
		t.Errorf(`Allow="Alpha" did not deliver the letter: text is %q, want %q — `+
			`the Focus-only arm above proves nothing without this`, got, "xin")
	}
	// And the reason Alpha works at all: it CARRIES Focus. Spelled without
	// that closure the set would be unreachable, which is the trap the
	// closed constants exist to remove.
	if got := typeInto(t, "", input.Rune('x')); got != "<unfocusable>" {
		t.Errorf("a bare <Frozen> was focusable; got %q", got)
	}
}

func TestAKeyClassAdmitsItsOwnClassAndNoOther(t *testing.T) {
	// Numeric admits a digit and refuses a letter, in ONE page, so the
	// two results cannot be explained by anything but the class.
	if got := typeInto(t, `Allow="Numeric"`, input.Rune('7')); got != "7in" {
		t.Errorf(`Allow="Numeric" dropped a digit: text is %q, want %q`, got, "7in")
	}
	if got := typeInto(t, `Allow="Numeric"`, input.Rune('x')); got != "in" {
		t.Errorf(`Allow="Numeric" admitted a letter: text is %q, want %q`, got, "in")
	}
}

// ---- Chords are their own class, and that is the point of having one ----

const allowBindingPage = `<Gooey>
  <VStack>
    %s<Border Name="host">
      <TextBox Name="inside" Text="{{.In}}"/>
      <KeyBinding Gesture="ctrl+g" Command="{{.Fire}}"/>
    </Border>%s
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

func chordFires(t *testing.T, attrs string) int {
	t.Helper()
	fired := 0
	ctx := &Context{Values: map[string]any{
		"Fire": gooey.Command(func() { fired++ }),
		"In":   prop.NewSource("in"),
		"Out":  prop.NewSource("out"),
	}}
	c := allowPage(t, frozenWrap(allowBindingPage, attrs), ctx)
	box, err := Find[*components.TextBox](ctx, "inside")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Focus().SetFocus(box) {
		t.Fatalf("could not focus the TextBox inside <Frozen %s>: the arm cannot run", attrs)
	}
	c.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 'g', Mods: input.ModCtrl})
	return fired
}

// TestAllowTextDoesNotAdmitAChord is the reason AllowChords exists at
// all: "let the user type" must not also mean "let the user press
// ctrl+s", or a read-only preview saves the document.
//
// The page grants Bindings in BOTH arms, so the only difference between
// them is whether the chord class is in the set — which is what makes the
// negative arm about the class rather than about the binding's
// registration.
func TestAllowTextDoesNotAdmitAChord(t *testing.T) {
	if n := chordFires(t, `Allow="Text Bindings"`); n != 0 {
		t.Errorf(`Allow="Text Bindings" fired a ctrl+g binding %d times, want 0`, n)
	}
	if n := chordFires(t, `Allow="Text Bindings Chords"`); n != 1 {
		t.Errorf(`Allow="Text Bindings Chords" fired the binding %d times, want 1: `+
			`without this the arm above could be a binding that never fires`, n)
	}
}

// TestAllowBindingsIsWhatRegistersAScopedBinding closes the other half:
// with the chord class granted and Bindings withheld, the binding is
// never registered, so the same keystroke does nothing.
func TestAllowBindingsIsWhatRegistersAScopedBinding(t *testing.T) {
	if n := chordFires(t, `Allow="Chords"`); n != 0 {
		t.Errorf(`Allow="Chords" fired a binding that was never granted: %d`, n)
	}
	if n := chordFires(t, `Allow="Chords Bindings"`); n != 1 {
		t.Errorf(`Allow="Chords Bindings" fired the binding %d times, want 1`, n)
	}
}

// ---- Mnemonics: page-scoped, so deliberately not implying Focus ----

func mnemonicFires(t *testing.T, attrs string) (fired int, stops int) {
	t.Helper()
	ctx := &Context{Values: map[string]any{
		"Fire": gooey.Command(func() { fired++ }),
		"Out":  prop.NewSource("out"),
	}}
	c := allowPage(t, frozenWrap(mnemonicPage, attrs), ctx)
	c.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 'g', Mods: input.ModAlt})
	for _, w := range c.Focus().Order() {
		if _, ok := w.(*components.Button); ok {
			stops++
		}
	}
	return fired, stops
}

// TestAllowMnemonicsFiresWithoutGrantingFocus is the test that justifies
// AllowMnemonics being a primitive of its own instead of riding on Focus
// the way the key classes do.
//
// A mnemonic is offered to every MnemonicHandler in the tree regardless
// of what holds focus, so it is reachable inside a subtree that has no
// focus stops at all — and the assertion on `stops` is what proves the
// grant did not quietly widen into focus.
func TestAllowMnemonicsFiresWithoutGrantingFocus(t *testing.T) {
	fired, stops := mnemonicFires(t, `Allow="Mnemonics"`)
	if fired != 1 {
		t.Errorf(`Allow="Mnemonics" fired alt+g %d times, want 1`, fired)
	}
	if stops != 0 {
		t.Errorf(`Allow="Mnemonics" made %d Buttons focus stops, want 0: `+
			`Mnemonics must not imply Focus`, stops)
	}
	if fired, _ := mnemonicFires(t, ""); fired != 0 {
		t.Errorf("a bare <Frozen> fired alt+g %d times, want 0: the arm above "+
			"proves nothing without this", fired)
	}
}

// ---- Start: the category with a safety argument ----

// startProbe is a Startable leaf. It counts Start calls rather than doing
// anything, because what is being measured is whether the framework
// started it — not what it would have done.
type startProbe struct {
	gooey.Base
	started *int
}

func (p *startProbe) Measure(gooey.Size) gooey.Size { return gooey.Size{W: 1, H: 1} }
func (p *startProbe) Render(*gooey.Frame)           {}
func (p *startProbe) Start(func(func())) func() {
	*p.started++
	return func() {}
}

const startPage = `<Gooey>
  <VStack>
    %s<Probe/>%s
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

func probeStarts(t *testing.T, attrs string) int {
	t.Helper()
	started := 0
	ctx := &Context{
		Values:     map[string]any{"Out": prop.NewSource("out")},
		Components: map[string]Builder{},
	}
	ctx.Components["Probe"] = func(Element, *Context) (gooey.Component, error) {
		return &startProbe{started: &started}, nil
	}
	// Composer.Start is what hands the composition a Dispatcher, and
	// without one walkNodes returns before it starts anything — so a
	// probe on a plain NewComposer would read 0 for every arm and the
	// whole test would be unfalsifiable.
	c := allowPage(t, frozenWrap(startPage, attrs), ctx)
	c.Start(gooey.NewDispatcher())
	return started
}

// TestAllowStartIsNeverImplied is the category whose argument is safety
// rather than ergonomics: Companion.Start spawns a child process, so a
// grant that turned starting on as a side effect of wanting hover would
// launch a subprocess from an editing gesture.
//
// The permissive arm grants everything EXCEPT Start, which is the
// discriminating shape — a test granting nothing would pass against an
// implementation where Start rode on any other category.
func TestAllowStartIsNeverImplied(t *testing.T) {
	if n := probeStarts(t, `Allow="Focus Alpha Numeric Punct Space Nav Edit Escape Chords Bindings Mnemonics Pointer Hover"`); n != 0 {
		t.Errorf("every category except Start was granted and the Startable started %d times, want 0", n)
	}
	if n := probeStarts(t, `Allow="Start"`); n != 1 {
		t.Errorf(`Allow="Start" started the Startable %d times, want 1`, n)
	}
	if n := probeStarts(t, ""); n != 0 {
		t.Errorf("a bare <Frozen> started the Startable %d times, want 0", n)
	}
}

// ---- Nesting ----

const nestedPage = `<Gooey>
  <VStack>
    %s<Frozen Allow="Mouse">
      <Button Name="btn" Content="go" Click="{{.Fire}}"/>
    </Frozen>%s
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

const nestedFocusPage = `<Gooey>
  <VStack>
    %s<Frozen Allow="Focus Start">
      <VStack>
        <TextBox Name="inside" Text="{{.In}}"/>
        <Probe/>
      </VStack>
    </Frozen>%s
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

// TestNestingIntersectsRatherThanOverriding is what stops a frozen host
// from being an escape hatch out of the one containing it. The INNER
// element asks for the same permissions in both arms; only the outer
// one's answer changes.
//
// It takes THREE probes because the intersection is enforced in three
// different places, and a test covering one leaves the others free to
// widen. The pointer goes through FocusManager.frozenHostFor, which tests
// each ancestor for the category separately; the focus order goes through
// FocusManager.walk's kidsAllow; the Startable goes through
// Composer.ancestorAllow. Turning any one of those three from Intersect
// into Union has to fail here.
func TestNestingIntersectsRatherThanOverriding(t *testing.T) {
	clicks := func(outer string) int {
		fired := 0
		ctx := allowButtonCtx(&fired)
		c := allowPage(t, frozenWrap(nestedPage, outer), ctx)
		btn, err := Find[*components.Button](ctx, "btn")
		if err != nil {
			t.Fatal(err)
		}
		b := btn.Bounds()
		c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: b.X, Y: b.Y})
		c.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, X: b.X, Y: b.Y})
		return fired
	}
	if n := clicks(""); n != 0 {
		t.Errorf("an inner <Frozen Allow=\"Mouse\"> inside a bare <Frozen> fired "+
			"Click %d times, want 0: nesting must intersect, not override", n)
	}
	if n := clicks(`Allow="Mouse"`); n != 1 {
		t.Errorf("with both hosts allowing Mouse the Click fired %d times, want 1: "+
			"the arm above proves nothing without this", n)
	}

	stopsAndStarts := func(outer string) (stops, started int) {
		ctx := &Context{
			Values: map[string]any{
				"In": prop.NewSource("in"), "Out": prop.NewSource("out"),
			},
			Components: map[string]Builder{},
		}
		ctx.Components["Probe"] = func(Element, *Context) (gooey.Component, error) {
			return &startProbe{started: &started}, nil
		}
		c := allowPage(t, frozenWrap(nestedFocusPage, outer), ctx)
		c.Start(gooey.NewDispatcher())
		return boxesIn(c.Focus().Order()), started
	}
	if stops, started := stopsAndStarts(""); stops != 1 || started != 0 {
		t.Errorf("an inner <Frozen Allow=\"Focus Start\"> inside a bare <Frozen> left "+
			"%d TextBox focus stops and started %d Startables, want 1 and 0", stops, started)
	}
	if stops, started := stopsAndStarts(`Allow="Focus Start"`); stops != 2 || started != 1 {
		t.Errorf("with both hosts allowing Focus and Start there are %d TextBox focus "+
			"stops and %d starts, want 2 and 1: the arm above proves nothing without this",
			stops, started)
	}
}

// ---- The observer: a SET change re-routes in the frame it happens ----

const boundAllowPage = `<Gooey>
  <VStack>
    <Frozen Allow="{{.Allow}}">
      <TextBox Name="inside" Text="{{.In}}"/>
    </Frozen>
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

func boundAllowCtx(allow string) *Context {
	return &Context{Values: map[string]any{
		"Allow": prop.NewSource(allow),
		"In":    prop.NewSource("in"),
		"Out":   prop.NewSource("out"),
	}}
}

// TestChangingTheAllowSetReRoutesInTheSameFrame is the headline for the
// widened observer, and it is the assertion that the bool version could
// not even express: nothing here flips frozen/not — the host is frozen in
// both frames — and yet the routing has to change.
//
// It is the whole subscription argument made falsifiable. The Composer's
// observer calls FrozenAllow(), which Gets the Allow handle; that Get is
// inside an evaluation, so it is a subscription; the Set schedules a
// frame; the sweep sees a different Allow and raises structDirty; Resync
// runs in the SAME frame, before anything paints.
func TestChangingTheAllowSetReRoutesInTheSameFrame(t *testing.T) {
	ctx := boundAllowCtx("None")
	c := allowPage(t, boundAllowPage, ctx)
	if n := boxesIn(c.Focus().Order()); n != 1 {
		t.Fatalf(`Allow="None" left %d TextBox focus stops, want 1`, n)
	}

	ctx.Values["Allow"].(*prop.Property[string]).Set("Focus")
	c.Frame()
	if n := boxesIn(c.Focus().Order()); n != 2 {
		t.Errorf("one frame after granting Focus there are %d TextBox focus stops, "+
			"want 2: the re-sync did not happen in the frame the set changed", n)
	}

	// And back, because a permission that cannot be taken away is not a
	// permission.
	ctx.Values["Allow"].(*prop.Property[string]).Set("None")
	c.Frame()
	if n := boxesIn(c.Focus().Order()); n != 1 {
		t.Errorf("one frame after revoking Focus there are %d TextBox focus stops, want 1", n)
	}
}

// TestChangingTheAllowSetRepaintsNothingOfItsOwn is the damage pin.
//
// Widening Frozen from a bool to a set must not have made freezing cost
// pixels: the set changes what the tree MEANS, and nothing in this page
// reads .Allow while painting, so the honest cost is zero repaints. An
// implementation that forced the subtree to repaint on a re-sync — the
// easy way to "make sure" — moves this number, and moving it IS the
// change.
func TestChangingTheAllowSetRepaintsNothingOfItsOwn(t *testing.T) {
	ctx := boundAllowCtx("None")
	c := allowPage(t, boundAllowPage, ctx)
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("the composition had not settled: %d components repainted with "+
			"nothing changed", painted)
	}

	ctx.Values["Allow"].(*prop.Property[string]).Set("Hover Pointer")
	if _, painted := c.Frame(); painted != 0 {
		t.Errorf("changing the allow set repainted %d components, want 0: it changes "+
			"routing, not pixels — damage %v", painted, c.Damage())
	}

	// Discrimination: the harness CAN report a repaint, so the 0 above is
	// a measurement rather than a stuck counter.
	ctx.Values["In"].(*prop.Property[string]).Set("changed")
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("a TextBox inside the frozen subtree changed and %d components "+
			"repainted, want 1: the 0 above proved nothing", painted)
	}
}

// ---- Load-time and runtime error surfaces ----

func TestAnUnknownAllowCategoryInALiteralIsALoadError(t *testing.T) {
	_, err := Build([]byte(frozenWrap(twoBoxes, `Allow="Clicks"`)), boxCtx())
	if err == nil {
		t.Fatal(`<Frozen Allow="Clicks"> built; want a load error`)
	}
	if !strings.Contains(err.Error(), "Clicks") || !strings.Contains(err.Error(), "Pointer") {
		t.Fatalf("the error must name the bad category AND the vocabulary, got: %v", err)
	}
	// The control: the same page with a real category loads.
	if _, err := Build([]byte(frozenWrap(twoBoxes, `Allow="Pointer"`)), boxCtx()); err != nil {
		t.Fatalf("a valid category must load: %v", err)
	}
}

// TestAnUnknownAllowCategoryInABindingFailsClosed pins the half that
// CANNOT be a load error, and pins the direction it fails in.
//
// A bound value does not exist at load time, so the loader has nothing to
// check. What it must never do is fail OPEN — a set nobody can parse must
// not be read as permission — and AllowError is what keeps the failure
// from being silent.
func TestAnUnknownAllowCategoryInABindingFailsClosed(t *testing.T) {
	ctx := boundAllowCtx("Focus")
	c := allowPage(t, boundAllowPage, ctx)
	if n := boxesIn(c.Focus().Order()); n != 2 {
		t.Fatalf(`a bound Allow="Focus" left %d TextBox focus stops, want 2`, n)
	}

	ctx.Values["Allow"].(*prop.Property[string]).Set("Focus Clicks")
	c.Frame()
	if n := boxesIn(c.Focus().Order()); n != 1 {
		t.Errorf("an unparseable bound Allow left %d TextBox focus stops, want 1: "+
			"it must fail CLOSED, not keep the last good set and not fail open", n)
	}
	f := findComponentFrozen(c.Root())
	if f == nil {
		t.Fatal("no components.Frozen in the page")
	}
	if f.AllowError() == nil {
		t.Error("failing closed left AllowError nil, so the failure is silent")
	}
}

func TestALiteralActiveIsALoadError(t *testing.T) {
	_, err := Build([]byte(frozenWrap(twoBoxes, `Active="true"`)), boxCtx())
	if err == nil {
		t.Fatal(`<Frozen Active="true"> built; want a load error — Active is bind-only`)
	}
	// Assert the SHAPE, not that an error exists: nearly everything in this
	// package fails at load, so err != nil says nothing about which
	// mechanism caught it. This one has to be the bind-only rejection.
	if !strings.Contains(err.Error(), "not a binding expression") {
		t.Fatalf("the refusal must be the bind-only one, got: %v", err)
	}
	ctx := boxCtx()
	ctx.Values["Design"] = prop.NewSource(true)
	if _, err := Build([]byte(frozenWrap(twoBoxes, `Active="{{.Design}}"`)), ctx); err != nil {
		t.Fatalf("a bound Active must load: %v", err)
	}
}

func findComponentFrozen(w gooey.Component) *components.Frozen {
	if f, ok := w.(*components.Frozen); ok {
		return f
	}
	if c, ok := w.(gooey.Container); ok {
		for _, ch := range c.ChildComponents() {
			if f := findComponentFrozen(ch); f != nil {
				return f
			}
		}
	}
	return nil
}
