package markup

import (
	"fmt"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The input half of freezing. <Frozen> is registered by withFrozen in
// frozen_test.go.
//
// Every test here is PAIRED WITH A CONTROL: the same page with the
// wrapper removed, asserting the opposite. Without the pair a freeze test
// passes for a page where the thing could never have happened anyway —
// which is the failure this project has now hit five times, and the one
// worth designing tests against rather than remembering.

func frozenPage(t *testing.T, src string, ctx *Context) *gooey.Composer {
	t.Helper()
	if ctx == nil {
		ctx = &Context{}
	}
	w, err := Build([]byte(src), withFrozen(ctx))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	c := gooey.NewComposer(w, 30, 8)
	c.Frame()
	return c
}

// wrap renders a page template with or without the freezing wrapper, so
// the test and its control differ by exactly that.
func wrap(tmpl string, frozen bool) string {
	if frozen {
		return fmt.Sprintf(tmpl, "<Frozen>", "</Frozen>")
	}
	return fmt.Sprintf(tmpl, "", "")
}

// twoBoxes is one TextBox inside the wrapper and one outside it, so every
// assertion has a live counterpart in the same page.
const twoBoxes = `<Gooey>
  <VStack>
    %s<TextBox Name="inside" Text="{{.In}}"/>%s
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

func boxCtx() *Context {
	return &Context{Values: map[string]any{
		"In":  prop.NewSource("in"),
		"Out": prop.NewSource("out"),
	}}
}

func boxesIn(order []gooey.Component) int {
	n := 0
	for _, w := range order {
		if _, ok := w.(*components.TextBox); ok {
			n++
		}
	}
	return n
}

func TestAFrozenSubtreeIsNotTabbable(t *testing.T) {
	frozen := frozenPage(t, wrap(twoBoxes, true), boxCtx())
	if n := boxesIn(frozen.Focus().Order()); n != 1 {
		t.Errorf("a frozen page has %d TextBox focus stops, want 1 (only the outside one)", n)
	}
	live := frozenPage(t, wrap(twoBoxes, false), boxCtx())
	if n := boxesIn(live.Focus().Order()); n != 2 {
		t.Errorf("the control has %d TextBox focus stops, want 2", n)
	}
}

// keyBindingPage puts a scoped KeyBinding inside the wrapper, beside a
// focusable component.
const keyBindingPage = `<Gooey>
  <VStack>
    %s<Border Name="host">
      <Button Name="inner" Content="in" Click="{{.Fire}}"/>
      <KeyBinding Gesture="ctrl+g" Command="{{.Fire}}"/>
    </Border>%s
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

func bindingCtx() *Context {
	return &Context{Values: map[string]any{
		"Fire": gooey.Command(func() {}),
		"Out":  prop.NewSource("out"),
	}}
}

// TestFocusCannotBeSetIntoAFrozenSubtree is what makes the KeyBinding
// claim true, and it is the assertion that survived writing the obvious
// test and watching its CONTROL fail.
//
// The obvious test — press ctrl+g and require the binding not to fire —
// passed frozen AND passed unfrozen, because a scoped binding only fires
// while the focused chain passes through its host, and in that page focus
// sat on the TextBox outside. Chasing the control revealed the real
// structure: with focus frozen out of the subtree, a binding scoped
// inside it is unreachable anyway. Declining to register m.bindings is
// defence in depth, not the load-bearing part, and claiming otherwise
// would mean resting a guarantee on a test that cannot fail.
//
// What IS reachable, observable and consequential: an explicit SetFocus —
// the route the control plane's `focus` act takes, by name — must be
// refused for anything inside a frozen subtree. Otherwise a remote caller
// can put the caret in a design surface and type into a picture.
func TestFocusCannotBeSetIntoAFrozenSubtree(t *testing.T) {
	ctx := bindingCtx()
	c := frozenPage(t, wrap(keyBindingPage, true), ctx)
	inner, err := Find[*components.Button](ctx, "inner")
	if err != nil {
		t.Fatal(err)
	}
	if c.Focus().SetFocus(inner) {
		t.Error("SetFocus must refuse a component inside a frozen subtree")
	}
	if c.Focus().Focused() == gooey.Component(inner) {
		t.Error("focus landed inside a frozen subtree")
	}

	// Control: the identical page without the wrapper accepts the focus,
	// so the refusal above is about freezing and not about the button.
	ctx2 := bindingCtx()
	c2 := frozenPage(t, wrap(keyBindingPage, false), ctx2)
	inner2, err := Find[*components.Button](ctx2, "inner")
	if err != nil {
		t.Fatal(err)
	}
	if !c2.Focus().SetFocus(inner2) {
		t.Fatal("the control refused the focus too: the frozen case proved nothing")
	}
}

// buttonPage is a clickable target inside the wrapper.
const buttonPage = `<Gooey>
  <VStack>
    %s<Button Name="btn" Content="go" Click="{{.Fire}}"/>%s
    <Text>tail</Text>
  </VStack>
</Gooey>`

func clickAt(c *gooey.Composer, w gooey.Component) input.MouseEvent {
	b := w.(gooey.Bounded).Bounds()
	return input.MouseEvent{X: b.X, Y: b.Y}
}

func TestAPressInsideAFrozenSubtreeNeverReachesIt(t *testing.T) {
	run := func(frozen bool) (int, *components.Button, *gooey.Composer) {
		fired := 0
		ctx := &Context{Values: map[string]any{"Fire": gooey.Command(func() { fired++ })}}
		c := frozenPage(t, wrap(buttonPage, frozen), ctx)
		btn, err := Find[*components.Button](ctx, "btn")
		if err != nil {
			t.Fatal(err)
		}
		at := clickAt(c, btn)
		at.Kind, at.Button = input.MousePress, input.ButtonLeft
		c.HandleMouse(at)
		at.Kind = input.MouseRelease
		c.HandleMouse(at)
		return fired, btn, c
	}

	fired, btn, c := run(true)
	if fired != 0 {
		t.Errorf("a Button inside a frozen subtree fired %d times, want 0", fired)
	}
	// And hit-testing still finds it — that is what makes click-to-select
	// possible, and it is the half a naive implementation removes.
	b := btn.Bounds()
	if got := c.Focus().HitTest(b.X, b.Y); got != btn {
		t.Errorf("HitTest must still return the frozen descendant, got %T", got)
	}

	if fired, _, _ = run(false); fired != 1 {
		t.Errorf("the control fired %d times, want 1", fired)
	}
}

// sinkPage puts a recording, CONSUMING leaf inside the wrapper.
const sinkPage = `<Gooey>
  <VStack>
    %s<Sink Name="sink"/>%s
    <Text>tail</Text>
  </VStack>
</Gooey>`

// TestTheWheelInsideAFrozenSubtreeGoesToTheHost — the wheel is a distinct
// MouseKind and was the class this design originally failed to list. It
// routes through the same target(), so what this pins is that the
// retarget really does live in the shared path rather than in the press
// case only.
//
// The first version of this test asserted only that the HOST received a
// wheel event, and it passed with the retarget deleted — because events
// BUBBLE, so the host sees anything its descendants decline. The
// discriminating assertion needs a descendant that records and CONSUMES:
// frozen, the sink must see nothing and the host must see the wheel;
// unfrozen, exactly the reverse.
func TestTheWheelInsideAFrozenSubtreeGoesToTheHost(t *testing.T) {
	run := func(frozen bool) (inner, host int) {
		ctx := &Context{}
		c := frozenPage(t, wrap(sinkPage, frozen), ctx)
		leaf, err := Find[*sink](ctx, "sink")
		if err != nil {
			t.Fatal(err)
		}
		b := leaf.Bounds()
		if b.W == 0 || b.H == 0 {
			t.Fatal("the sink was never arranged: the pointer cannot be over it")
		}
		c.HandleMouse(input.MouseEvent{Kind: input.WheelUp, X: b.X, Y: b.Y})
		if h := findFrozenHost(c.Root()); h != nil {
			host = len(h.got)
		}
		return len(leaf.got), host
	}

	inner, host := run(true)
	if inner != 0 {
		t.Errorf("the wheel reached a frozen descendant %d times, want 0", inner)
	}
	if host != 1 {
		t.Errorf("the frozen host received %d wheel events, want 1 — "+
			"a swallowed wheel is a different bug from a retargeted one", host)
	}

	if inner, _ = run(false); inner != 1 {
		t.Errorf("the control delivered %d wheel events to the sink, want 1: "+
			"the frozen case proved nothing", inner)
	}
}

func findFrozenHost(w gooey.Component) *frozenHost {
	if h, ok := w.(*frozenHost); ok {
		return h
	}
	if c, ok := w.(gooey.Container); ok {
		for _, ch := range c.ChildComponents() {
			if h := findFrozenHost(ch); h != nil {
				return h
			}
		}
	}
	return nil
}

func TestHoverInsideAFrozenSubtreeDoesNotLightTheDescendant(t *testing.T) {
	run := func(frozen bool) bool {
		ctx := &Context{Values: map[string]any{"Fire": gooey.Command(func() {})}}
		c := frozenPage(t, wrap(buttonPage, frozen), ctx)
		btn, err := Find[*components.Button](ctx, "btn")
		if err != nil {
			t.Fatal(err)
		}
		b := btn.Bounds()
		c.HandleMouse(input.MouseEvent{Kind: input.MouseMove, X: b.X, Y: b.Y})
		return c.Focus().Hovered() == gooey.Component(btn)
	}
	if run(true) {
		t.Error("a Button inside a frozen subtree must not take the hover")
	}
	if !run(false) {
		t.Error("the control did not hover the button, so the frozen case proved nothing")
	}
}

// mnemonicPage puts an accelerator-bearing Button inside the wrapper and
// a TextBox OUTSIDE it, first, so the same component holds focus in both
// arms and neither arm's result depends on where focus landed.
const mnemonicPage = `<Gooey>
  <VStack>
    <TextBox Name="outside" Text="{{.Out}}"/>
    %s<Button Name="btn" Content="_Go" Click="{{.Fire}}"/>%s
  </VStack>
</Gooey>`

// TestAMnemonicInsideAFrozenSubtreeDoesNotFire is the row of the freeze
// table that is BOTH reachable and was unpinned, and the distinction from
// the KeyBinding row above is the whole point of writing it.
//
// A scoped KeyBinding only fires while the focused chain passes through
// its host, so freezing focus out of the subtree already made it
// unreachable — the skip at input.go:411 is defence in depth and says so.
// A mnemonic is the opposite: Dispatch offers it to every MnemonicHandler
// in m.mnemonics regardless of focus (input.go:637), because accelerators
// are page-scoped by design. So alt+g on a page containing a design
// surface would reach into the picture and run the Click of a Button the
// user was only ever looking at, from anywhere on the page.
//
// Measured, not assumed: before this test, deleting `&& !frozen` from the
// m.mnemonics registration left every test in the repository green.
func TestAMnemonicInsideAFrozenSubtreeDoesNotFire(t *testing.T) {
	run := func(frozen bool) int {
		fired := 0
		ctx := &Context{Values: map[string]any{
			"Fire": gooey.Command(func() { fired++ }),
			"Out":  prop.NewSource("out"),
		}}
		c := frozenPage(t, wrap(mnemonicPage, frozen), ctx)
		// The accelerator has to be registered at all, or both arms report
		// zero and the pair proves nothing about freezing.
		btn, err := Find[*components.Button](ctx, "btn")
		if err != nil {
			t.Fatal(err)
		}
		if _, isMnemonic := gooey.Component(btn).(gooey.MnemonicHandler); !isMnemonic {
			t.Fatal("the Button is not a MnemonicHandler: nothing here could fire")
		}
		c.Handle(input.KeyOf(input.KeyEvent{
			Key: input.KeyRune, Rune: 'g', Mods: input.ModAlt}))
		return fired
	}

	if fired := run(true); fired != 0 {
		t.Errorf("alt+g reached a Button inside a frozen subtree %d times, want 0: "+
			"mnemonics are page-scoped, so freezing focus does not stop them", fired)
	}
	if fired := run(false); fired != 1 {
		t.Errorf("the control fired %d times, want 1: the frozen case proved nothing", fired)
	}
}

// hoverWatchPage attaches a HoverWatcher to a component inside the
// wrapper. <Watch> is non-visual, so buildChildren files it as an
// attachment exactly as it does a <Tooltip>.
const hoverWatchPage = `<Gooey>
  <VStack>
    %s<Button Name="btn" Content="go" Click="{{.Fire}}">
      <Watch/>
    </Button>%s
    <Text>tail</Text>
  </VStack>
</Gooey>`

// TestAHoverWatcherInsideAFrozenSubtreeNeverEnters pins the user-visible
// guarantee — no tooltip pops out of a picture — and its own reach was
// measured rather than assumed, because the measurement contradicted the
// obvious expectation.
//
// TWO mechanisms hold this door, and each is sufficient ALONE:
//
//  1. the walk declines to register the watcher (input.go:436), so
//     updateWatchers returns on its len(m.watchers) == 0 fast path;
//  2. DispatchMouse retargets the hit to the frozen host (mouse.go:176),
//     so m.within(hw.host, hit) is false for any host inside the subtree.
//
// Verified by deleting each on its own and running this test: green both
// times. Deleting BOTH fails it. So this test cannot pin either mechanism
// — it pins the GUARANTEE, and it is the only thing in the repository
// that would survive a refactor removing one of the two.
//
// That redundancy is the same shape the Frozen commit removed from
// target/setHover, and the reason it is kept here rather than collapsed
// is the KeyBinding precedent: an unreachable-but-correct skip stays, and
// says out loud that it is defence in depth. The note now at input.go:436
// is what was missing — without it the next reader deletes the skip, this
// test stays green, and the freeze table silently loses a row.
func TestAHoverWatcherInsideAFrozenSubtreeNeverEnters(t *testing.T) {
	run := func(frozen bool) int {
		ctx := &Context{Values: map[string]any{"Fire": gooey.Command(func() {})}}
		c := frozenPage(t, wrap(hoverWatchPage, frozen), ctx)
		btn, err := Find[*components.Button](ctx, "btn")
		if err != nil {
			t.Fatal(err)
		}
		w := findWatcher(btn)
		if w == nil {
			t.Fatal("the <Watch> was never attached to the Button")
		}
		b := btn.Bounds()
		if b.W == 0 || b.H == 0 {
			t.Fatal("the Button was never arranged: the pointer cannot be over it")
		}
		c.HandleMouse(input.MouseEvent{Kind: input.MouseMove, X: b.X, Y: b.Y})
		return w.entered
	}

	if n := run(true); n != 0 {
		t.Errorf("a HoverWatcher inside a frozen subtree entered %d times, want 0", n)
	}
	if n := run(false); n != 1 {
		t.Errorf("the control entered %d times, want 1: the frozen case proved nothing", n)
	}
}

func findWatcher(w gooey.Component) *watcher {
	a, ok := w.(gooey.Attacher)
	if !ok {
		return nil
	}
	for _, at := range a.Attachments() {
		if p, ok := at.(*watcher); ok {
			return p
		}
	}
	return nil
}

// ---- what freezing must NOT change ----

// damagePage: a Text inside the wrapper bound to its own property, and a
// second one outside, so a Set touches exactly one component.
// The wrapper holds a VStack of TWO texts on purpose: wrapping a single
// child makes the host's bounds identical to the child's, and the rect
// assertion below would then pass for a host that repainted its whole
// subtree — vacuous in exactly the way this test exists to rule out.
const damagePage = `<Gooey>
  <VStack>
    %s<VStack>
      <Text Name="inner">{{.In}}</Text>
      <Text>pad</Text>
    </VStack>%s
    <Text Name="outer">{{.Out}}</Text>
  </VStack>
</Gooey>`

// TestFreezingDoesNotCoarsenDamage is the invariant pin. Every
// component's Render is its own paint node, and freezing must leave that
// alone — which is the whole argument for keeping the Container walk
// rather than hiding the subtree behind a host that paints it.
//
// The count alone is not enough: a host that painted its whole subtree in
// one node would ALSO report 1. So the damage RECT is asserted too, and
// it has to be the Text's rather than the host's.
func TestFreezingDoesNotCoarsenDamage(t *testing.T) {
	ctx := boxCtx()
	c := frozenPage(t, wrap(damagePage, true), ctx)
	inner, err := Find[*components.Text](ctx, "inner")
	if err != nil {
		t.Fatal(err)
	}
	ctx.Values["In"].(*prop.Property[string]).Set("changed")
	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("one property changed inside a frozen subtree and %d components "+
			"repainted, want 1: freezing must not coarsen damage", painted)
	}
	dmg := c.Damage()
	if len(dmg) != 1 {
		t.Fatalf("damage rects: %v, want exactly one", dmg)
	}
	// Precondition: the two rects must be distinguishable, or the
	// assertion below cannot fail.
	host := findFrozenHost(c.Root())
	if host == nil {
		t.Fatal("no frozen host in the page")
	}
	if host.Bounds() == inner.Bounds() {
		t.Fatalf("the host and the Text occupy the same rect %v: this test "+
			"cannot tell a per-component repaint from a whole-subtree one",
			host.Bounds())
	}
	if want := inner.Bounds(); dmg[0] != want {
		t.Errorf("damage rect is %v, want the Text's own %v — a whole-subtree rect "+
			"means the frozen host repainted for it", dmg[0], want)
	}
}

// adornPage anchors a marker to a component inside the wrapper.
const adornPage = `<Gooey>
  <VStack>
    %s<TextBox Name="inside" Text="{{.In}}"/>%s
    <Text>tail</Text>
    <AdornmentLayer Name="layer"/>
  </VStack>
</Gooey>`

// TestAnAdornmentAnchoredInsideAFrozenSubtreeSurvives — the selection
// border a design surface draws is an Adornment anchored to the selected
// component, and AdornmentLayer.Arrange DROPS an adornment whose anchor is
// not visibly reachable from the root through Container.
//
// This is the pin that fails for the whole "hide the subtree behind a
// non-Container host" design, on the first frame, silently. Freezing keeps
// the walk, so the anchor stays reachable.
func TestAnAdornmentAnchoredInsideAFrozenSubtreeSurvives(t *testing.T) {
	ctx := boxCtx()
	c := frozenPage(t, wrap(adornPage, true), ctx)
	layer, err := Find[*components.AdornmentLayer](ctx, "layer")
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := Find[*components.TextBox](ctx, "inside")
	if err != nil {
		t.Fatal(err)
	}
	layer.Add(&probeAdornment{anchor: anchor})
	c.Frame()
	if n := len(layer.Adornments()); n != 1 {
		t.Fatalf("the adornment was dropped: an anchor inside a frozen subtree "+
			"must stay reachable, got %d up", n)
	}
}

// probeAdornment is the smallest thing the layer will host.
type probeAdornment struct {
	gooey.Base
	anchor gooey.Component
}

func (a *probeAdornment) Anchor() gooey.Component { return a.anchor }
func (a *probeAdornment) Place(anchor, layer gooey.Rect) gooey.Rect {
	return gooey.Rect{X: anchor.X, Y: anchor.Y, W: 1, H: 1}
}
func (a *probeAdornment) Measure(gooey.Size) gooey.Size { return gooey.Size{W: 1, H: 1} }
func (a *probeAdornment) Render(*gooey.Frame)           {}

// frozenValidationPage is validationPage with the field inside the
// wrapper. The AdornmentLayer stays OUTSIDE it, which is where a page
// actually puts one — last child of the root.
const frozenValidationPage = `<Gooey>
  <VStack>
    %s<TextBox Name="field" Text="{{.Name}}" Error="{{.NameErr}}" InvalidStyle="bad">
      <ValidationMarker/>
    </TextBox>%s
    <Text>filler</Text>
    <AdornmentLayer/>
  </VStack>
</Gooey>`

// TestValidatorsStayLiveInsideAFrozenSubtree is the pin that protects the
// PICTURE, and it is the one a later "just freeze everything"
// simplification would break.
//
// A validator is a computed that evaluates during paint, and its
// evaluation is what raises the marker. Freezing it would freeze what the
// surface is for — showing what the component looks like, invalid state
// included. The cost is named rather than hidden: a validator with a side
// effect gets that side effect anyway, and nothing in freezing can
// prevent it. That is the first place "a validator is a pure function of
// its input" stops being a style preference.
func TestValidatorsStayLiveInsideAFrozenSubtree(t *testing.T) {
	up := func(frozen bool) int {
		ctx, _, _ := validationCtx()
		c := frozenPage(t, wrap(frozenValidationPage, frozen), ctx)
		c.Frame()
		layer := findLayer(c.Root())
		if layer == nil {
			t.Fatal("no AdornmentLayer in the page")
		}
		return len(layer.Adornments())
	}
	if n := up(true); n == 0 {
		t.Error("a validation marker inside a frozen subtree did not appear: " +
			"freezing must stop behaviour, not the picture")
	}
	if n := up(false); n == 0 {
		t.Fatal("the control raised no marker either, so the frozen case proved nothing")
	}
}

func findLayer(w gooey.Component) *components.AdornmentLayer {
	if l, ok := w.(*components.AdornmentLayer); ok {
		return l
	}
	if c, ok := w.(gooey.Container); ok {
		for _, ch := range c.ChildComponents() {
			if l := findLayer(ch); l != nil {
				return l
			}
		}
	}
	return nil
}
