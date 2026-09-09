package markup

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

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

// TestAllowBindingsAloneFiresNothing is the arm the pair above leaves
// out, and it is the one a reader of the reference table would get
// wrong.
//
// That table said `Bindings` means "scoped <KeyBinding>s attached inside
// fire", which is half of it: Bindings decides whether they are
// REGISTERED. Whether one is ever REACHED is a different door.
// Dispatch starts at frozenHostFor(focused, AllowFor(ev)), which hoists
// the start of the bubble to the outermost ancestor that withholds the
// key's class — so with every class withheld the walk begins AT the
// <Frozen> and runs upward, and a binding attached below it is never
// visited. Registered, correct, unreachable.
//
// So `Allow="Bindings"` on its own is not a weak grant, it is an inert
// one: there is no keystroke it can ever admit. The composition it needs
// is Bindings PLUS the class of the key it binds, which is what the two
// arms above assert for chords.
func TestAllowBindingsAloneFiresNothing(t *testing.T) {
	if n := chordFires(t, `Allow="Bindings"`); n != 0 {
		t.Errorf(`Allow="Bindings" fired the chord %d times. If this now `+
			`works, the reference table's plain reading became true and `+
			`the paragraph warning against it should go.`, n)
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

// ---- #424: the fail-closed seal must be REPORTABLE from markup ----

const errAllowPage = `<Gooey>
  <VStack>
    <Frozen Allow="{{.Allow}}" AllowError="{{.Err}}">
      <TextBox Name="inside" Text="{{.In}}"/>
    </Frozen>
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

func errAllowCtx(allow string) *Context {
	return &Context{
		// NewDispatcher, not &Dispatcher{}. The zero value's wake channel
		// is nil, so Post's select falls straight through to default and
		// the wake path never runs — it is the one Dispatcher shape whose
		// Post cannot wake an app loop, which makes it the wrong one to
		// pin a posted publication with. Raised in review of #459.
		Dispatcher: gooey.NewDispatcher(),
		Values: map[string]any{
			"Allow": prop.NewSource(allow),
			"Err":   prop.NewSource(""),
			"In":    prop.NewSource("in"),
			"Out":   prop.NewSource("out"),
		},
	}
}

// TestABoundAllowPublishesItsParseFailure is #424: the failure was
// recorded and unreadable.
//
// components.Frozen already fails CLOSED on an unparseable bound set and
// already records why, in AllowError. Nothing in the repo read it — a Go
// host could, a page could not — so the only symptom of a typo in a
// bound Allow was a subtree that had silently stopped responding. That
// is exactly the class of failure the rest of markup refuses to ship: a
// LITERAL Allow is a load error naming the attribute.
//
// AllowError= is the channel. It is an ordinary bound handle, so the
// page owns the property and can render it, and the framework Sets it.
func TestABoundAllowPublishesItsParseFailure(t *testing.T) {
	ctx := errAllowCtx("Focus")
	c := allowPage(t, errAllowPage, ctx)
	errProp := ctx.Values["Err"].(*prop.Property[string])
	if got := errProp.Get(); got != "" {
		t.Fatalf("a parseable Allow published %q; want no error", got)
	}

	ctx.Values["Allow"].(*prop.Property[string]).Set("Focus Clicks")
	// THE INTERVAL, and it is the whole reason this attribute requires a
	// Dispatcher. Between the Set that breaks the parse and the Drain
	// that publishes it, the work must be QUEUED and the sink must still
	// hold its old value. An inline Set — the mutation
	// `sink.Set(errC.Get())` in place of `d.Post(...)` — leaves the whole
	// package green without this, because every other assertion here
	// Drains before it reads and cannot tell the two apart.
	// Raised in review of #459.
	if n := ctx.Dispatcher.Pending(); n != 1 {
		t.Errorf("the publication is not queued (Pending()=%d, want 1): it ran inline, "+
			"inside the invalidation, which is the confinement violation the Dispatcher exists to avoid", n)
	}
	if got := errProp.Get(); got != "" {
		t.Errorf("the sink already holds %q before Drain: the Set was not posted", got)
	}
	ctx.Dispatcher.Drain()
	c.Frame()
	if boxesIn(c.Focus().Order()) != 1 {
		t.Fatal("the set did not fail closed; nothing below was tested")
	}
	got := errProp.Get()
	if got == "" {
		t.Fatalf("the subtree sealed and the page was told nothing")
	}
	// The MESSAGE, not merely non-empty: the author's mistake is a
	// category name, so the name has to be in it.
	if !strings.Contains(got, "Clicks") {
		t.Errorf("the published failure does not name the bad category:\n\t%s", got)
	}
}

// TestAGoodAllowClearsAPublishedFailure is the other half, and it is the
// half a "the error appears" test passes without: a channel that only
// ever fills up reports the FIRST typo forever, so a page fixing its set
// keeps showing the old message beside a subtree that now works.
func TestAGoodAllowClearsAPublishedFailure(t *testing.T) {
	ctx := errAllowCtx("Focus Clicks")
	c := allowPage(t, errAllowPage, ctx)
	ctx.Dispatcher.Drain()
	errProp := ctx.Values["Err"].(*prop.Property[string])
	if errProp.Get() == "" {
		t.Fatal("the bad set published nothing; nothing below was tested")
	}

	allow := ctx.Values["Allow"].(*prop.Property[string])
	allow.Set("Focus")
	ctx.Dispatcher.Drain()
	c.Frame()
	if got := errProp.Get(); got != "" {
		t.Errorf("a repaired Allow left the old failure published: %q", got)
	}

	// A THIRD transition, and it is the one the first two cannot stand in
	// for. A computed invalidates ONCE and then stays dirty until somebody
	// reads it, so a hook that publishes without re-evaluating fires on the
	// first change and is deaf to every change after it. Both assertions
	// above pass against that — each only ever crosses one edge.
	allow.Set("Focus Nonsense")
	ctx.Dispatcher.Drain()
	c.Frame()
	if got := errProp.Get(); got == "" {
		t.Error("a SECOND bad set published nothing: the observer fired once and " +
			"stopped, so the page is told about the first typo only")
	}
}

// TestAllowErrorRefusesWhatCannotReceiveASet keeps the attribute honest
// about what it is: a WRITE target, which is the opposite direction from
// every other attribute on this element.
//
// Each arm asserts the SHAPE of the refusal, not that one exists. This
// package fails at load for a dozen reasons and an unknown attribute is
// one of them — so every arm below passed before AllowError existed at
// all, on "no such attribute", which is how a test like this comes to
// assert nothing.
func TestAllowErrorRefusesWhatCannotReceiveASet(t *testing.T) {
	for _, tc := range []struct {
		name, page, want string
	}{{
		// A literal has nowhere for the message to go: it would read as
		// configured and report nothing, forever.
		name: "a literal",
		page: strings.Replace(errAllowPage, `AllowError="{{.Err}}"`, `AllowError="oops"`, 1),
		want: "not a binding expression",
	}, {
		// Reporting the parse of a set the element does not have. The
		// author either meant to bind Allow too, or meant nothing.
		name: "without an Allow",
		page: strings.Replace(errAllowPage, `Allow="{{.Allow}}" `, "", 1),
		want: "without a BOUND Allow",
	}, {
		// A LITERAL Allow already parsed, at load, and produced an error
		// naming the attribute if it was wrong. So the channel beside it
		// can never carry anything — it reads as configured and reports
		// nothing forever, which is the failure mode #424 is about,
		// re-created by the fix for it.
		name: "beside a literal Allow",
		page: strings.Replace(errAllowPage, `Allow="{{.Allow}}"`, `Allow="Focus"`, 1),
		want: "without a BOUND Allow",
	}, {
		// The wrong TYPE is the ordinary Bound[string] rejection, and it
		// earns an arm because the message has to name THIS attribute:
		// the page binds several properties here and a refusal that does
		// not say which one sends the author reading all of them.
		name: "a non-string handle",
		page: strings.Replace(errAllowPage, "{{.Err}}", "{{.Count}}", 1),
		want: "AllowError",
	}, {
		// A COMPUTED sink is the fourth spelling of "reads as configured
		// and reports nothing forever", and the worst of them: Bound does
		// not check Settable, so before this guard the priming Set PANICKED
		// inside Build — in the package whose contract is that everything
		// resolvable resolves before the UI is live, and under the
		// os.DirFS watcher a rebuild panic takes the app down rather than
		// showing a load error. Raised in review of #459.
		name: "a computed handle",
		page: strings.Replace(errAllowPage, "{{.Err}}", "{{.Derived}}", 1),
		want: "COMPUTED",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := errAllowCtx("Focus")
			ctx.Values["Count"] = prop.NewSource(0)
			ctx.Values["Derived"] = prop.NewComputed(func() string { return "derived" })
			_, err := Build([]byte(tc.page), ctx)
			if err == nil {
				t.Fatalf("%s built; it cannot receive a Set", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal is not the one this arm is about:\n\t%v", err)
			}
		})
	}
}

// TestAllowErrorWithoutADispatcherIsALoadError pins the requirement the
// publication carries, at the moment the page is read rather than on the
// first bad set — which is the only time a page without a Dispatcher
// would otherwise discover it, by publishing nothing and saying nothing.
func TestAllowErrorWithoutADispatcherIsALoadError(t *testing.T) {
	ctx := errAllowCtx("Focus")
	ctx.Dispatcher = nil
	_, err := Build([]byte(errAllowPage), ctx)
	if err == nil {
		t.Fatal("<Frozen AllowError=…> built with no Dispatcher; the publication has no route")
	}
	if !strings.Contains(err.Error(), "Dispatcher") {
		t.Errorf("the refusal does not name what is missing:\n\t%v", err)
	}
}

// errAllowReportPage RENDERS the published failure. errAllowPage does not
// — it holds the sink and nothing reads it — so a damage assertion there
// would be counting the repaints of a property with no consumer, which is
// zero however badly the publication behaves.
const errAllowReportPage = `<Gooey>
  <VStack>
    <Frozen Allow="{{.Allow}}" AllowError="{{.Err}}">
      <TextBox Name="inside" Text="{{.In}}"/>
    </Frozen>
    <Text Name="report">{{.Err}}</Text>
    <Text Name="bystander">steady</Text>
  </VStack>
</Gooey>`

// TestABenignAllowChangeRepublishesNothing is the guard on the publish.
//
// prop.Set does not compare (CLAUDE.md's own trap, prop/prop.go:117), and
// Allow changes far more often than it breaks. Every benign edit — "Focus"
// to "Hover", both parseable, message unchanged at "" — would otherwise
// invalidate every dependent of the sink and repaint the error label for
// nothing.
//
// The instrument is a COUNTED DEPENDENT rather than a damage count,
// because it answers precisely this question and nothing else: a computed
// re-evaluates only if it was invalidated, so the read count moves if and
// only if the sink was Set. A frame count here would be confounded by the
// Composer's own freeze observer, which re-evaluates on any Allow change
// whether or not the message moved. Raised in review of #459.
func TestABenignAllowChangeRepublishesNothing(t *testing.T) {
	ctx := errAllowCtx("Focus")
	c := allowPage(t, errAllowPage, ctx)
	errProp := ctx.Values["Err"].(*prop.Property[string])
	allow := ctx.Values["Allow"].(*prop.Property[string])

	reads := 0
	watcher := prop.NewComputed(func() string { reads++; return errProp.Get() })
	watcher.Get()
	settled := reads

	// Focus -> Hover. Both parse, so the message is "" on both sides.
	allow.Set("Hover")
	ctx.Dispatcher.Drain()
	c.Frame()
	watcher.Get()
	if reads != settled {
		t.Errorf("a benign Allow change republished an unchanged message: "+
			"the watcher re-evaluated (%d -> %d). prop.Set does not compare, so every "+
			"dependent of the sink repainted for a change nobody can see", settled, reads)
	}

	// NON-VACUITY. The assertion above passes just as well against a
	// publication that has stopped working altogether, so a real change
	// has to still get through.
	allow.Set("Nonsense")
	ctx.Dispatcher.Drain()
	c.Frame()
	watcher.Get()
	if reads == settled {
		t.Fatal("a BREAKING Allow change republished nothing either: the guard is not " +
			"comparing, it is swallowing, and the assertion above proved nothing")
	}
}

// TestPublishingAFailureRepaintsOnlyItsReader is the damage-count pin.
//
// CLAUDE.md is explicit that this is the only instrument for a repaint
// claim — "a bounds assertion or a 'the cell says X' assertion passes just
// as well when the entire tree repainted". The claim the feature makes is
// that publishing a failure repaints what reads it and nothing else.
//
// DIFFERENTIAL, against a control page identical but for the reader, and
// the reason is measured rather than assumed: an absolute count cannot
// answer this. Any Allow change re-evaluates the Composer's own freeze
// observer, which repaints one component on its own, so the first version
// of this test asserted breaking > benign and read 1 against 1 — the
// publication was completely hidden inside the observer's own repaint.
// Subtracting a page that publishes identically and reads nothing leaves
// exactly the reader's repaint. Raised in review of #459.
func TestPublishingAFailureRepaintsOnlyItsReader(t *testing.T) {
	// The control differs in ONE character sequence: the report renders a
	// constant instead of the sink. Everything else — the Frozen, the
	// binding, the publication — is identical, so the difference between
	// the two counts is the reader and nothing else.
	control := strings.Replace(errAllowReportPage, `>{{.Err}}</Text>`, `>steady</Text>`, 1)
	if control == errAllowReportPage {
		t.Fatal("the control page is the same as the reporting one; the subtraction is vacuous")
	}

	run := func(t *testing.T, page string) (benign, breaking int) {
		t.Helper()
		ctx := errAllowCtx("Focus")
		c := allowPage(t, page, ctx)
		ctx.Dispatcher.Drain()
		c.Frame()
		if _, painted := c.Frame(); painted != 0 {
			t.Fatalf("the page has not settled: %d components still repainting", painted)
		}
		allow := ctx.Values["Allow"].(*prop.Property[string])

		// Benign: both sides parse, so the published message does not move.
		allow.Set("Hover")
		ctx.Dispatcher.Drain()
		_, benign = c.Frame()

		// Breaking: the message changes, so a reader of it must repaint.
		allow.Set("Nonsense")
		ctx.Dispatcher.Drain()
		_, breaking = c.Frame()
		return benign, breaking
	}

	readerBenign, readerBreaking := run(t, errAllowReportPage)
	quietBenign, quietBreaking := run(t, control)

	if readerBenign != quietBenign {
		t.Errorf("a benign Allow change repainted %d components with a reader and %d without: "+
			"the reader repainted for a message that did not change, which is the "+
			"unguarded Set (prop.Set does not compare)", readerBenign, quietBenign)
	}
	if readerBreaking-quietBreaking != 1 {
		t.Errorf("a breaking Allow change repainted %d components with a reader and %d without "+
			"(difference %d, want exactly 1): publishing a failure must repaint the one "+
			"component that renders it, and no others",
			readerBreaking, quietBreaking, readerBreaking-quietBreaking)
	}
	t.Logf("repaints: with a reader benign=%d breaking=%d; without benign=%d breaking=%d",
		readerBenign, readerBreaking, quietBenign, quietBreaking)
}

// TestAnUnsealedFrozenStillPublishesItsParseFailure pins "reports the
// PARSE, not the seal", which frozenerror.go and markup-reference.md both
// assert in prose and nothing tested.
//
// It was derived from component.go:242 rather than pinned, and the whole
// publication suite is blind to it: errAllowPage omits Active, so Frozen()
// is true in every other test here and the two readings agree everywhere.
// Binding Active to a FALSE source separates them — nothing is sealed, and
// an unparseable Allow must still say so, because the message is about the
// set not parsing rather than about the subtree being frozen.
//
// WHAT IT DOES NOT GUARD, checked rather than assumed. Review suggested
// this would also go red if someone "optimized" gooey.frozenAllow to ask
// Frozen() before FrozenAllow() (component.go:235-241). It does not, and I
// took that framing on trust before mutating it: rewriting frozenAllow to
// return early when unfrozen leaves this test GREEN. armAllowError's
// computed calls f.FrozenAllow() on the component directly and never
// routes through that function, so its ordering cannot reach this
// assertion. What the test does pin is that the COMPUTED does not gate on
// Frozen() — inserting `if !f.Frozen() { return "" }` at the top of it
// fails this and nothing else. Raised in review of #459.
func TestAnUnsealedFrozenStillPublishesItsParseFailure(t *testing.T) {
	const page = `<Gooey>
  <VStack>
    <Frozen Active="{{.Off}}" Allow="{{.Allow}}" AllowError="{{.Err}}">
      <TextBox Name="inside" Text="{{.In}}"/>
    </Frozen>
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`
	ctx := errAllowCtx("Focus Clicks") // unparseable: "Clicks" is not a category
	ctx.Values["Off"] = prop.NewSource(false)
	c := allowPage(t, page, ctx)
	ctx.Dispatcher.Drain()
	c.Frame()

	// Nothing is sealed — the arm that makes this test about the SEAL
	// rather than merely about the parse.
	if boxesIn(c.Focus().Order()) != 2 {
		t.Fatalf("Active=false still sealed the subtree; the premise of this test does not hold")
	}
	if got := ctx.Values["Err"].(*prop.Property[string]).Get(); got == "" {
		t.Error("an unparseable Allow published nothing while Active was false: " +
			"the message reports the PARSE, not the seal, and a page with a bound " +
			"Active would be told nothing until it happened to freeze")
	}
}

// TestTwoFrozenCannotShareOneFailureChannel is #424's symptom reappearing
// in plain page markup, and it was REAL — measured before the guard:
//
//	after load:                 Err="unknown Allow category \"Nonsense\"; …"
//	after A's benign change:    Err=""      <- B still sealed, message gone
//
// Each <Frozen> arms its own computed, and publish compared against
// sink.Get() — treating the sink as the record of what THIS arm last
// published, which it stops being the moment a second arm writes to it. A
// going "Focus" -> "Hover" (both parseable) yielded "", saw it differ from
// the sink's current value (B's live failure), and wrote over it. B's
// computed was clean, so it never republished: the subtree stays sealed
// and the reader shows nothing, which is the exact failure this attribute
// exists to remove. Two writers on one property is refusable at load, like
// the other spellings. Raised in review of #459.
func TestTwoFrozenCannotShareOneFailureChannel(t *testing.T) {
	const page = `<Gooey>
  <VStack>
    <Frozen Allow="{{.AllowA}}" AllowError="{{.Err}}">
      <TextBox Name="a" Text="{{.In}}"/>
    </Frozen>
    <Frozen Allow="{{.AllowB}}" AllowError="{{.Err}}">
      <TextBox Name="b" Text="{{.In}}"/>
    </Frozen>
  </VStack>
</Gooey>`
	ctx := errAllowCtx("Focus")
	ctx.Values["AllowA"] = prop.NewSource("Focus")
	ctx.Values["AllowB"] = prop.NewSource("Nonsense")
	_, err := Build([]byte(page), ctx)
	if err == nil {
		t.Fatal("two <Frozen> armed one AllowError property; they erase each other")
	}
	if !strings.Contains(err.Error(), "already the failure channel") {
		t.Errorf("the refusal is not the one this test is about:\n\t%v", err)
	}
}

// TestASecondBuildMayReuseASinkTheFirstArmed keeps the guard above from
// breaking the two hosts that rebuild a page against ONE Context: the
// os.DirFS watcher and the designer, where docCtx shares ed.ctx.Values and
// the document is rebuilt on every edit.
//
// Without per-build scoping the second build would refuse what the first
// armed, and the failure would look like the user's markup being wrong.
// This is the arm that makes armedSinks' save/restore load-bearing rather
// than decorative.
func TestASecondBuildMayReuseASinkTheFirstArmed(t *testing.T) {
	ctx := errAllowCtx("Focus")
	if _, err := Build([]byte(errAllowPage), ctx); err != nil {
		t.Fatalf("first build: %v", err)
	}
	if _, err := Build([]byte(errAllowPage), ctx); err != nil {
		t.Fatalf("REBUILD against the same Context was refused — a watcher or the "+
			"designer would report this as the document being wrong:\n\t%v", err)
	}
}

// TestAllowCannotAliasItsOwnErrorChannel: <Frozen Allow="{{.X}}"
// AllowError="{{.X}}"> built, and the priming publish then overwrote the
// author's own allow set with the parse message BEFORE the UI was live —
// measured: X went "Focus" -> "" during Build.
//
// Raised in review of #459.
func TestAllowCannotAliasItsOwnErrorChannel(t *testing.T) {
	const page = `<Gooey>
  <VStack>
    <Frozen Allow="{{.X}}" AllowError="{{.X}}">
      <TextBox Name="a" Text="{{.In}}"/>
    </Frozen>
  </VStack>
</Gooey>`
	ctx := errAllowCtx("Focus")
	ctx.Values["X"] = prop.NewSource("Focus")
	_, err := Build([]byte(page), ctx)
	if err == nil {
		t.Fatal("Allow and AllowError bound to one property built; the publication " +
			"overwrites the set it just read")
	}
	if !strings.Contains(err.Error(), "cannot be both") {
		t.Errorf("the refusal is not the one this test is about:\n\t%v", err)
	}
	// The author's set must survive the refusal — a load error that has
	// already scribbled on the page's state is not a refusal.
	if got := ctx.Values["X"].(*prop.Property[string]).Get(); got != "Focus" {
		t.Errorf("the allow set was modified before the refusal: X=%q, want \"Focus\"", got)
	}
}

// TestAPublishDoesNotClobberAnotherWritersValue pins the half the load
// guard cannot reach.
//
// armedSinks refuses two <Frozen> in MARKUP, but nothing stops a
// code-behind, an MCP set_value, or a Startable from writing the same
// property. publish therefore compares against this arm's OWN last
// published value rather than reading the sink back: an arm whose message
// has not changed must not write at all, whatever the sink now holds.
//
// The differential is exact — comparing against sink.Get() instead makes
// the benign change below Set("") over the other writer's value. Raised in
// review of #459.
func TestAPublishDoesNotClobberAnotherWritersValue(t *testing.T) {
	ctx := errAllowCtx("Focus") // parseable: this arm's message is ""
	c := allowPage(t, errAllowPage, ctx)
	ctx.Dispatcher.Drain()
	c.Frame()
	errProp := ctx.Values["Err"].(*prop.Property[string])
	if got := errProp.Get(); got != "" {
		t.Fatalf("a parseable Allow published %q; the premise does not hold", got)
	}

	// Somebody else owns the line right now.
	errProp.Set("saving…")

	// A BENIGN change: this arm's message is "" before and after, so it
	// has nothing to say and must stay quiet.
	ctx.Values["Allow"].(*prop.Property[string]).Set("Hover")
	ctx.Dispatcher.Drain()
	c.Frame()
	if got := errProp.Get(); got != "saving…" {
		t.Errorf("a benign Allow change overwrote another writer's value: %q, want \"saving…\" — "+
			"publish is reading the sink back instead of tracking what it last published", got)
	}
}

// TestAllowErrorInsideAnItemTemplateReachesItsRowsHandle is round five's
// critical, and it was introduced by round four's own guard.
//
// document.build allocates ctx.armedSinks and its defer restores it to
// nil on the way out. buildItemsView constructs the row Context
// FIELD-BY-FIELD (markup/itemsview.go) and calls build(row, item)
// directly — never document.build — so armedSinks is nil there and
// elements.go's `ctx.armedSinks[sink] = raw` panics with "assignment to
// entry in nil map". That row Context is the only *Context in this
// package built outside document.build.
//
// A PANIC INSIDE Build is the exact defect the Settable() guard was
// added to remove, reintroduced eighty lines from the comment arguing
// against it. And the timing is worse than a load panic: ItemsView.
// Validate builds one throwaway row at load, so a collection non-empty
// at load panics during Build, while a table fed by a timer is empty at
// load and the same markup panics on FIRST SCROLL — inside the composer,
// where a panic skips Screen.Restore and takes the user's terminal modes
// and unsaved work with it.
//
// Row scope is also the right answer rather than the cheap one: sharing
// the page's map with rows would add an entry per row realization and
// drop none, and two rows binding one sink is a real collision the page
// guard should catch. Raised in review of #459.
func TestAllowErrorInsideAnItemTemplateReachesItsRowsHandle(t *testing.T) {
	const page = `<Gooey>
  <ItemsView Items="{{.Rows}}">
    <ItemsView.ItemTemplate>
      <Frozen Allow="{{.Cats}}" AllowError="{{.Err}}">
        <Text>{{.Label}}</Text>
      </Frozen>
    </ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	ctx := errAllowCtx("Focus")
	// NON-EMPTY at load, so ItemsView.Validate realizes a throwaway row
	// during Build and the panic (if any) lands here rather than on a
	// scroll nothing in this test would perform.
	//
	// It must be a real ItemSource: the first version of this test handed
	// Items a []map[string]any, which <ItemsView> refuses BEFORE it ever
	// builds a row — so the test passed without reaching the line under
	// test at all. A load error that arrives too early is the same
	// vacuous pass as no assertion.
	rows := prop.NewSource([]post{{Title: "one"}, {Title: "two"}})
	// THE ROW'S OWN Err HANDLE IS KEPT, so the test can assert the arm
	// actually PUBLISHED rather than only that it did not panic. Each row
	// gets its own — that is the point of row scope — so the mapping func
	// records them as it goes.
	//
	// An UNPARSEABLE Cats, because a publication of "" is indistinguishable
	// from never publishing at all: prop.Set does not compare, but the
	// zero value a fresh source already holds does not say who wrote it.
	var errs []*prop.Property[string]
	ctx.Values["Rows"] = components.Items(rows, func(x post) map[string]any {
		e := prop.NewSource("")
		errs = append(errs, e)
		return map[string]any{
			"Label": x.Title,
			"Cats":  prop.NewSource("NoSuchCategory"),
			"Err":   e,
		}
	})

	// err == nil, not "any error that is not about ItemSource". The row
	// reaches the arm today, so the strong assertion is available — and
	// the weak one left a regression that turns a row-template AllowError
	// into a load error looking exactly like a pass, including one in the
	// guard chain this round touched.
	if _, err := Build([]byte(page), ctx); err != nil {
		t.Fatalf("an AllowError inside an item template is a load error: %v", err)
	}
	if len(errs) == 0 {
		t.Fatal("no row was ever realized, so the row Context was never " +
			"constructed and this test is vacuous")
	}
	ctx.Dispatcher.Drain()

	// AT LEAST ONE, not all: how many rows Validate realizes during Build
	// is ItemsView's business, and pinning it here would make this test
	// fail on a change that has nothing to do with AllowError.
	published := false
	for _, e := range errs {
		if e.Get() != "" {
			published = true
			if !strings.Contains(e.Get(), "NoSuchCategory") {
				t.Errorf("the row published %q, which does not name the "+
					"unparseable category", e.Get())
			}
		}
	}
	if !published {
		t.Error("no row published its parse failure — the arm inside an item " +
			"template does not reach its own Err handle, so AllowError is " +
			"silent exactly where a row cannot report any other way")
	}
}

// TestTwoControlsCannotShareOneFailureChannel is the guarantee
// Context.armedSinks' own doc comment makes and the code did not keep:
// "a nested Load inherits the outermost map (so two controls sharing a
// sink are still caught)".
//
// control() builds a fresh child *Context and propagates Declared,
// Components, Handlers, Includes and Dispatcher — not armedSinks — so
// the child's document.build finds nil and allocates its own. Two panes
// over one status line is the surface <Frozen> exists for, and it is
// what an <Include> is for, so this is the collision the page guard was
// written to catch, one boundary over.
//
// The own-last-value compare does not save it: A's message genuinely
// changes ("err" -> ""), and that is the value that erases B. Raised in
// review of #459.
func TestTwoControlsCannotShareOneFailureChannel(t *testing.T) {
	const pane = `<Gooey>
  <Frozen Allow="{{.Allow}}" AllowError="{{.Err}}">
    <TextBox Text="{{.In}}"/>
  </Frozen>
</Gooey>`
	const page = `<Gooey>
  <VStack>
    <Pane Allow="{{.AllowA}}" Err="{{.Err}}" In="{{.In}}"/>
    <Pane Allow="{{.AllowB}}" Err="{{.Err}}" In="{{.In}}"/>
  </VStack>
</Gooey>`
	fsys := fstest.MapFS{"pane.gooey": &fstest.MapFile{Data: []byte(pane)}}
	ctx := errAllowCtx("Focus")
	ctx.Values["AllowA"] = prop.NewSource("Focus")
	ctx.Values["AllowB"] = prop.NewSource("Nonsense")
	ctx.Includes = fsys
	// Include is a Go-side Builder, not an element name: a markup-only
	// control is registered under whatever tag the page uses for it.
	ctx.Components = map[string]Builder{"Pane": Include(fsys, "pane.gooey")}
	_, err := Build([]byte(page), ctx)
	if err == nil {
		t.Fatal("two controls armed one AllowError property. Each publishes its own " +
			"transitions, so the parseable one going \"err\" -> \"\" erases the other's " +
			"live failure and the other's computed is clean, so it never republishes: " +
			"a sealed subtree with nothing to show for it")
	}
	if !strings.Contains(err.Error(), "already the failure channel") {
		t.Errorf("the refusal is not the one this test is about:\n\t%v", err)
	}
}

// TestAliasIsCaughtByHandleNotByText: the alias guard compares
// bindingPath TEXT where the dup-sink guard forty lines below compares
// resolved handles, so two guards on one line of defence disagree about
// what "the same property" means — and the weaker one is the one
// protecting the author's page state.
//
// bindingPath is bindRe.FindStringSubmatch: the FIRST binding in the
// string, string-compared. Both spellings below build cleanly today and
// destroy the allow set during Build, which is precisely what
// TestAllowCannotAliasItsOwnErrorChannel exists to refuse.
//
// This retires "Pointer identity cannot catch it … The binding PATHS are
// what match." Pointer identity of the BoundText computed cannot — true,
// it is a fresh computed per call. Pointer identity of the resolved
// SOURCE handle can, and it is available: the check moves below the
// Bound[string] call so `sink` exists. Safe, because Bound only reads
// and armAllowError is still the last statement, so the "the author's
// set must survive the refusal" assertion still holds. Raised in review
// of #459.
func TestAliasIsCaughtByHandleNotByText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		page  string
		setup func(*Context)
		// read names the Values entry the allow set lives in, so the
		// test can prove the refusal happened BEFORE anything wrote.
		read string
	}{{
		// bindingPath takes the FIRST binding, so "{{.A}} {{.X}}" reads
		// as "A" and never matches "X".
		name: "alias is not the first binding in a multi-binding Allow",
		page: `<Gooey>
  <Frozen Allow="{{.A}} {{.X}}" AllowError="{{.X}}">
    <TextBox Name="a" Text="{{.In}}"/>
  </Frozen>
</Gooey>`,
		setup: func(c *Context) {
			c.Values["A"] = prop.NewSource("Focus")
			c.Values["X"] = prop.NewSource("Hover")
		},
		read: "X",
	}, {
		// Two NAMES, one handle. The text differs; the property does
		// not — and the dup-sink guard already refuses exactly this
		// shape for its own question, because it keys by pointer.
		name: "two names resolving to one handle",
		page: `<Gooey>
  <Frozen Allow="{{.X}}" AllowError="{{.Y}}">
    <TextBox Name="a" Text="{{.In}}"/>
  </Frozen>
</Gooey>`,
		setup: func(c *Context) {
			shared := prop.NewSource("Focus")
			c.Values["X"] = shared
			c.Values["Y"] = shared
		},
		read: "X",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := errAllowCtx("Focus")
			tc.setup(ctx)
			before := ctx.Values[tc.read].(*prop.Property[string]).Get()
			_, err := Build([]byte(tc.page), ctx)
			if err == nil {
				t.Fatalf("Allow and AllowError resolve to ONE property and the build was "+
					"accepted; the priming publish then overwrites the set it just read "+
					"(%s: %q -> %q)", tc.read, before,
					ctx.Values[tc.read].(*prop.Property[string]).Get())
			}
			if !strings.Contains(err.Error(), "cannot be both") {
				t.Errorf("the refusal is not the one this test is about:\n\t%v", err)
			}
			if got := ctx.Values[tc.read].(*prop.Property[string]).Get(); got != before {
				t.Errorf("the allow set was modified before the refusal: %s=%q, want %q",
					tc.read, got, before)
			}
		})
	}
}

// TestTheAliasGuardSeesAPathInsideAValueCall.
//
// The guard walked bindRe, which matches only a WHOLE-BODY `{{.Path}}`.
// A path carried as an ARGUMENT is invisible to it, so
// `Allow="{{v:Echo .X}}"` aliased the sink with nothing to notice, and
// the priming publish destroyed the author's Allow source during Build —
// the exact harm the guard was added for, reached by writing the same
// alias a different way. Measured in review of #459 against this
// package's own echoProvider; this is that probe, kept.
//
// THE SECOND ASSERTION IS THE DAMAGE, not the refusal. A guard that
// refuses but has already scribbled on X is not a refusal, and the
// original bug's whole signature was X going "Focus" -> "" during Build.
func TestTheAliasGuardSeesAPathInsideAValueCall(t *testing.T) {
	withValues(t, &echoProvider{})

	const page = `<Gooey xmlns:v="` + valueURI + `">
  <VStack>
    <Frozen Allow="{{v:Echo .X}}" AllowError="{{.X}}">
      <TextBox Name="a" Text="{{.In}}"/>
    </Frozen>
  </VStack>
</Gooey>`
	ctx := errAllowCtx("Focus")
	ctx.Values["X"] = prop.NewSource("Focus")

	_, err := Build([]byte(page), ctx)
	if err == nil {
		t.Fatal("an Allow reading the sink through a value call built clean; " +
			"the publication overwrites the set it just read")
	}
	if !strings.Contains(err.Error(), "cannot be both") {
		t.Errorf("the refusal is not the one this test is about:\n\t%v", err)
	}
	if got := ctx.Values["X"].(*prop.Property[string]).Get(); got != "Focus" {
		t.Errorf("the allow set was destroyed before the refusal: X=%q, want \"Focus\"", got)
	}
}

// TestTheAliasGuardSeesEveryArgumentOfAValueCall. One capture group
// yields one match per EXPRESSION, so a single-pass scan of
// `{{v:Echo .A .X}}` reports only .A — the "first match only" defect this
// guard was already fixed for once, returning one level down.
func TestTheAliasGuardSeesEveryArgumentOfAValueCall(t *testing.T) {
	withValues(t, &echoProvider{})

	const page = `<Gooey xmlns:v="` + valueURI + `">
  <VStack>
    <Frozen Allow="{{v:Echo .Other .X}}" AllowError="{{.X}}">
      <TextBox Name="a" Text="{{.In}}"/>
    </Frozen>
  </VStack>
</Gooey>`
	ctx := errAllowCtx("Focus")
	ctx.Values["X"] = prop.NewSource("Focus")
	ctx.Values["Other"] = prop.NewSource("Alpha")

	_, err := Build([]byte(page), ctx)
	if err == nil {
		t.Fatal("an alias in the SECOND argument built clean")
	}
	if !strings.Contains(err.Error(), "cannot be both") {
		t.Errorf("the refusal is not the one this test is about:\n\t%v", err)
	}
	if got := ctx.Values["X"].(*prop.Property[string]).Get(); got != "Focus" {
		t.Errorf("the allow set was destroyed before the refusal: X=%q", got)
	}
}

// TestAValueCallThatDoesNotAliasStillLoads is what stops the two above
// being satisfied by refusing every value call in an Allow.
func TestAValueCallThatDoesNotAliasStillLoads(t *testing.T) {
	withValues(t, &echoProvider{})

	const page = `<Gooey xmlns:v="` + valueURI + `">
  <VStack>
    <Frozen Allow="{{v:Echo .Other}}" AllowError="{{.X}}">
      <TextBox Name="a" Text="{{.In}}"/>
    </Frozen>
  </VStack>
</Gooey>`
	ctx := errAllowCtx("Focus")
	ctx.Values["X"] = prop.NewSource("")
	ctx.Values["Other"] = prop.NewSource("Focus")

	if _, err := Build([]byte(page), ctx); err != nil {
		t.Fatalf("a value call naming a DIFFERENT property is refused: %v", err)
	}
}

// TestTheAliasGuardReadsPastABacktickLiteral is the miss the regexp
// scanner had and the package's own scanner does not.
//
// scan.go says it in as many words: "a backtick literal may legally
// contain a brace: {{str:Replace .S `}}` `--`}} has to find the LAST
// }} , not the first." allPaths was a `\{\{[^}]*\}\}` scan, so the
// expression ENDED at the backtick's brace pair and .X — the alias —
// was never looked at. The page built clean and the priming publish
// erased the author's Allow source during Build, which is the original
// #459 harm reached by a third spelling.
//
// THE SECOND ASSERTION IS THE DAMAGE, not the refusal, for the reason
// the sibling test above gives. Raised in review of #459.
func TestTheAliasGuardReadsPastABacktickLiteral(t *testing.T) {
	withValues(t, &echoProvider{})

	const page = `<Gooey xmlns:v="` + valueURI + `">
  <VStack>
    <Frozen Allow="{{v:Echo ` + "`}}`" + ` .X}}" AllowError="{{.X}}">
      <TextBox Name="a" Text="{{.In}}"/>
    </Frozen>
  </VStack>
</Gooey>`
	ctx := errAllowCtx("Focus")
	ctx.Values["X"] = prop.NewSource("Focus")

	_, err := Build([]byte(page), ctx)
	if err == nil {
		t.Fatal("an Allow whose alias sits after a backtick literal containing " +
			"`}}` built clean; the scan stopped at the literal's braces and the " +
			"publication overwrites the set it just read")
	}
	if !strings.Contains(err.Error(), "cannot be both") {
		t.Errorf("the refusal is not the one this test is about:\n\t%v", err)
	}
	if got := ctx.Values["X"].(*prop.Property[string]).Get(); got != "Focus" {
		t.Errorf("the allow set was destroyed before the refusal: X=%q, want \"Focus\"", got)
	}
}

// TestAPathInsideABacktickLiteralIsNotAnAlias is the other direction,
// and without it "read every path" licenses refusing text that only
// LOOKS like one.
//
// `.X` inside backticks is a string argument. The regexp scanner could
// not tell the difference — tokenRe matched anywhere in the expression —
// so this page was refused although its Allow never reads the sink at
// all. A guard that refuses correct markup is the failure the declared
// vocabulary exists to prevent, one level up. Raised in review of #459.
func TestAPathInsideABacktickLiteralIsNotAnAlias(t *testing.T) {
	withValues(t, &echoProvider{})

	const page = `<Gooey xmlns:v="` + valueURI + `">
  <VStack>
    <Frozen Allow="{{v:Echo ` + "`.X`" + `}}" AllowError="{{.X}}">
      <TextBox Name="a" Text="{{.In}}"/>
    </Frozen>
  </VStack>
</Gooey>`
	ctx := errAllowCtx("Focus")
	ctx.Values["X"] = prop.NewSource("Focus")

	if _, err := Build([]byte(page), ctx); err != nil {
		t.Errorf("a page whose Allow mentions .X only inside a backtick LITERAL "+
			"is refused as an alias:\n\t%v\nThe argument is a string; nothing "+
			"there reads the sink.", err)
	}
}

// TestAllPathsReadsTheCallsIntoTarget keeps the `| into .Target` clause
// of allPaths alive. Nothing else reaches it: no Allow in the tree pipes
// into anything, so deleting that line is silent against every other
// test here. It is a unit test rather than a page because the state it
// pins is a parse, not a load. Raised in review of #459.
func TestAllPathsReadsTheCallsIntoTarget(t *testing.T) {
	got := allPaths("{{v:Echo `x` | into .Target}}")
	var found bool
	for _, p := range got {
		if p == "Target" {
			found = true
		}
	}
	if !found {
		t.Errorf("allPaths(%q) = %v; a call whose result is WRITTEN INTO the sink "+
			"aliases it as surely as one that reads it",
			"{{v:Echo `x` | into .Target}}", got)
	}
}

// TestAllPathsIgnoresProseOutsideBraces replaces a comment that called
// this state unreachable.
//
// The claim was that only a bound Allow reaches the scan, so there is
// never text outside the braces. A bound Allow may be INTERPOLATED —
// `Allow="{{.Allow}} .X"` builds today, and its runtime parse failing on
// `.X` is the entire point of the attribute — so the state is reachable
// and the mutation dropping the brace requirement was silent for the
// ordinary reason: a missing test. Raised in review of #459; this is
// that test.
func TestAllPathsIgnoresProseOutsideBraces(t *testing.T) {
	for _, p := range allPaths("{{.Allow}} .X") {
		if p == "X" {
			t.Error("allPaths read `.X` from prose OUTSIDE the braces, so an " +
				"interpolated Allow whose text happens to name the sink would " +
				"be refused although nothing in it binds")
		}
	}
}

// TestAPageAndARowCannotArmTheSameSink is the collision row scope left
// open, and it erases at LOAD rather than on some later change.
//
// The page map and the row map are disjoint by construction — which is
// right, because a shared map would accumulate an entry per row
// realization and refuse the list's own second row. What was missing is
// that the row map could not SEE the page's arms, so a <Frozen> on the
// page and a <Frozen> in an item template could arm one handle with
// neither guard looking, and the row's priming publish wrote "" over the
// page's live failure inside Build. That is round four's two-writer
// failure arriving through round five's scope change.
//
// Not exotic: components/itemsview.go passes a *prop.Property[string]
// found in a row map straight through, so a projection handing rows the
// page's own handle is an ordinary thing to write — and
// docs/markup-reference.md tells the author the sink must be page-owned,
// which is exactly this shape.
//
// SECOND ASSERTION IS THE DAMAGE. A guard that refuses after the page's
// message is already gone is not a refusal. Raised in review of #459.
func TestAPageAndARowCannotArmTheSameSink(t *testing.T) {
	const page = `<Gooey>
  <VStack>
    <Frozen Allow="{{.Allow}}" AllowError="{{.Err}}">
      <TextBox Name="a" Text="{{.In}}"/>
    </Frozen>
    <ItemsView Items="{{.Rows}}">
      <ItemsView.ItemTemplate>
        <Frozen Allow="{{.Cats}}" AllowError="{{.Err}}">
          <Text>{{.Label}}</Text>
        </Frozen>
      </ItemsView.ItemTemplate>
    </ItemsView>
  </VStack>
</Gooey>`
	// UNPARSEABLE on the page, so the page arm has a message to lose.
	ctx := errAllowCtx("Nonsense")
	shared := ctx.Values["Err"].(*prop.Property[string])
	rows := prop.NewSource([]post{{Title: "one"}, {Title: "two"}})
	// THE PROJECTION HANDS BACK THE PAGE'S OWN HANDLE, which is the whole
	// fixture — the row's Values entry for Err is not a per-row source.
	ctx.Values["Rows"] = components.Items(rows, func(x post) map[string]any {
		return map[string]any{
			"Label": x.Title,
			"Cats":  prop.NewSource("NoSuchCategory"),
			"Err":   shared,
		}
	})

	_, err := Build([]byte(page), ctx)
	if err == nil {
		t.Fatal("a page <Frozen> and a row <Frozen> armed the same handle and the " +
			"page built clean; the row's priming publish erases the page's message")
	}
	if !strings.Contains(err.Error(), "already the failure channel") {
		t.Errorf("the refusal is not the duplicate-sink one:\n\t%v", err)
	}
	if got := shared.Get(); got == "" {
		t.Error("the page's failure message was erased before the refusal " +
			"arrived, so the guard refused a page it had already damaged")
	}
}

// TestEveryRowStillArmsTheSameTemplateSink is the discrimination half.
// Without it, "check the page's map too" could be satisfied by sharing
// one map with the rows — which refuses the list's own second row, the
// exact regression row scope was introduced to avoid. Raised in review
// of #459.
func TestEveryRowStillArmsTheSameTemplateSink(t *testing.T) {
	const page = `<Gooey>
  <ItemsView Name="list" Items="{{.Rows}}">
    <ItemsView.ItemTemplate>
      <Frozen Allow="{{.Cats}}" AllowError="{{.Err}}">
        <Text>{{.Label}}</Text>
      </Frozen>
    </ItemsView.ItemTemplate>
  </ItemsView>
</Gooey>`
	ctx := errAllowCtx("Focus")
	rows := prop.NewSource([]post{{Title: "one"}, {Title: "two"}, {Title: "three"}})
	// The handles are allocated ONCE and looked up, not minted inside the
	// projection. That is the realistic shape — a row's error property
	// lives in the model beside the row — and it is what makes this test
	// discriminate: registration is keyed by the HANDLE, so a projection
	// that mints a fresh property on every call gives every realization a
	// different key and passes whatever the map's scope is. Realizing one
	// row TWICE is the case that separates them, and it is not exotic:
	// ItemsView.Validate builds a throwaway row 0 at load and composition
	// then builds row 0 again, so it happens on the first frame of every
	// non-empty list. Raised in review of #459.
	errs := map[string]*prop.Property[string]{}
	for _, name := range []string{"one", "two", "three"} {
		errs[name] = prop.NewSource("")
	}
	ctx.Values["Rows"] = components.Items(rows, func(x post) map[string]any {
		return map[string]any{
			"Label": x.Title,
			"Cats":  prop.NewSource("NoSuchCategory"),
			"Err":   errs[x.Title],
		}
	})

	// COMPOSED, not merely built: ItemsView.Validate realizes ONE
	// throwaway row at load, so a build alone cannot show that the second
	// row was allowed to arm — which is the whole claim. Arrange is what
	// realizes the rest.
	w, err := Build([]byte(page), ctx)
	if err != nil {
		t.Fatalf("rows arming their own per-row sink are refused at load: %v", err)
	}
	c := gooey.NewComposer(w, 30, 8)
	t.Cleanup(c.Close)
	c.Frame()

	list, ok := ctx.Named["list"].(*components.ItemsView)
	if !ok {
		t.Fatalf("the ItemsView is not reachable by name: %T", ctx.Named["list"])
	}
	if err := list.Err(); err != nil {
		t.Errorf("realizing the rows is refused, so the template can arm at most "+
			"once across the whole list:\n\t%v", err)
	}
	// NON-VACUITY. list.Err() is nil for a list that realized nothing at
	// all, so the assertion above passes against a broken fixture.
	armed := 0
	for _, e := range errs {
		if e.Get() != "" {
			armed++
		}
	}
	if armed < 2 {
		t.Fatalf("only %d row sink(s) carry a message, so nothing here shows "+
			"that the SECOND row was allowed to arm", armed)
	}
}

// TestARowRealizedAfterLoadStillSeesThePagesArms is why the page's map is
// captured OUTSIDE the factory rather than read through ctx when a row is
// built.
//
// document.build's defer restores ctx.armedSinks to what it found, and
// for the outermost document that is nil — so by the time a row is
// realized, ctx.armedSinks is nil and armedOuter would be an empty
// lookup. The sibling test above cannot see this: ItemsView.Validate
// realizes one throwaway row at LOAD, while build is still on the stack
// and ctx.armedSinks is still the page's map, so both spellings pass
// there. A collection that is empty at load and filled by a timer is the
// discriminating shape, and it is the ordinary one — the same asymmetry
// the ns/res captures above are written up for.
//
// The factory's error surfaces on ItemsView.Err(); the second assertion
// is again the damage, because a refusal that arrives after the page's
// message is gone is not a refusal.
func TestARowRealizedAfterLoadStillSeesThePagesArms(t *testing.T) {
	const page = `<Gooey>
  <VStack>
    <Frozen Allow="{{.Allow}}" AllowError="{{.Err}}">
      <TextBox Name="a" Text="{{.In}}"/>
    </Frozen>
    <ItemsView Name="list" Items="{{.Rows}}">
      <ItemsView.ItemTemplate>
        <Frozen Allow="{{.Cats}}" AllowError="{{.Err}}">
          <Text>{{.Label}}</Text>
        </Frozen>
      </ItemsView.ItemTemplate>
    </ItemsView>
  </VStack>
</Gooey>`
	ctx := errAllowCtx("Nonsense")
	shared := ctx.Values["Err"].(*prop.Property[string])
	// EMPTY at load, so Validate realizes no row and the row factory runs
	// for the first time below, after build returned.
	rows := prop.NewSource([]post{})
	ctx.Values["Rows"] = components.Items(rows, func(x post) map[string]any {
		return map[string]any{
			"Label": x.Title,
			"Cats":  prop.NewSource("NoSuchCategory"),
			"Err":   shared,
		}
	})

	c := allowPage(t, page, ctx)
	t.Cleanup(c.Close)
	if shared.Get() == "" {
		t.Fatal("the page's own arm published nothing, so this fixture cannot " +
			"show a row erasing it")
	}

	rows.Set([]post{{Title: "one"}})
	c.Frame()

	list, ok := ctx.Named["list"].(*components.ItemsView)
	if !ok {
		t.Fatalf("the ItemsView is not reachable by name: %T", ctx.Named["list"])
	}
	err := list.Err()
	if err == nil {
		t.Fatal("a row realized after load armed a handle the page had already " +
			"armed, and the template reported no error")
	}
	if !strings.Contains(err.Error(), "already the failure channel") {
		t.Errorf("the template error is not the duplicate-sink one:\n\t%v", err)
	}
	if got := shared.Get(); got == "" {
		t.Error("the page's failure message was erased by the row's priming " +
			"publish before the refusal arrived")
	}
}
