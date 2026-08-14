package markup

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// A DYNAMIC Frozen: the half that used to be a documented constraint.
//
// Every test here is the same shape as the ones in frozen_input_test.go —
// paired arms, so an assertion that could not have failed anyway is
// visible — but the pairing is across TIME rather than across two pages.
// One composition, one property, and the two arms are before and after a
// Set. That is stricter than two pages: a page built frozen and a page
// built live differ in every way at once, whereas these two frames differ
// in exactly one Set, so nothing but the flip can explain the change.
//
// The frame boundary is the claim. Every assertion runs after ONE Frame()
// following the Set, because "the same frame" is the guarantee: the key
// that turns design mode on must leave nothing in the subtree reachable
// by the very next event, not by the one after some other structural
// change happens to come along.

// designPage puts a TextBox inside a host whose frozen-ness is bound, and
// a second one outside it. `When` starts false, so the composition is
// built LIVE and freezes later — which is the direction that used to be
// impossible to observe.
const designPage = `<Gooey>
  <VStack>
    <Frozen When="{{.Design}}">
      <TextBox Name="inside" Text="{{.In}}"/>
    </Frozen>
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

func designCtx(design bool) *Context {
	return &Context{Values: map[string]any{
		"Design": prop.NewSource(design),
		"In":     prop.NewSource("in"),
		"Out":    prop.NewSource("out"),
	}}
}

func designProp(ctx *Context) *prop.Property[bool] {
	return ctx.Values["Design"].(*prop.Property[bool])
}

// TestFlippingFrozenReTabsInTheSameFrame is the headline. Before this
// mechanism the focus order was rebuilt only by walk, which runs at
// construction or when a Dynamic marks the composition dirty — so a host
// that changed its answer kept the OLD routing and the picture stayed
// tabbable.
//
// Both directions are asserted from one composition, and the freeze
// direction is the one with teeth: unfreezing something that was never
// reachable is a smaller claim than making a reachable thing unreachable
// while the user is looking at it.
func TestFlippingFrozenReTabsInTheSameFrame(t *testing.T) {
	ctx := designCtx(false)
	c := frozenPage(t, designPage, ctx)

	// Precondition, not decoration: if the subtree were unreachable from
	// the start, the assertion after the flip would pass for a composition
	// where nothing happened.
	if n := boxesIn(c.Focus().Order()); n != 2 {
		t.Fatalf("built live, the page has %d TextBox focus stops, want 2: "+
			"the flip below could not remove one", n)
	}

	designProp(ctx).Set(true)
	c.Frame()
	if n := boxesIn(c.Focus().Order()); n != 1 {
		t.Errorf("one frame after freezing, %d TextBox focus stops remain, want 1 — "+
			"the picture is still tabbable", n)
	}

	designProp(ctx).Set(false)
	c.Frame()
	if n := boxesIn(c.Focus().Order()); n != 2 {
		t.Errorf("one frame after unfreezing, %d TextBox focus stops, want 2: "+
			"the re-route is one-way", n)
	}
}

// TestFlippingFrozenEvictsFocusFromTheSubtree is the case above with
// focus actually SITTING in the subtree when it freezes. The order test
// alone does not cover it: a stale m.cur pointing into a subtree that
// left m.order is how a caret ends up inside a picture, and Resync's
// existing "focused component vanished" path is what has to catch it.
func TestFlippingFrozenEvictsFocusFromTheSubtree(t *testing.T) {
	ctx := designCtx(false)
	c := frozenPage(t, designPage, ctx)
	inside, err := Find[*components.TextBox](ctx, "inside")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Focus().SetFocus(inside) {
		t.Fatal("focus would not go to the inner box while live: nothing to evict")
	}

	designProp(ctx).Set(true)
	c.Frame()
	if got := c.Focus().Focused(); got == gooey.Component(inside) {
		t.Error("focus stayed inside the subtree that froze under it")
	}
	if c.Focus().Focused() == nil {
		t.Error("focus went nowhere: a composition always has somewhere for keys to land")
	}
	if inside.IsFocused() {
		t.Error("the evicted TextBox still paints itself focused")
	}
}

// ---- Startables ----

// ticker is a Startable leaf that records its own lifetime. It is a probe
// rather than a <Companion> because what this test needs is a deterministic
// edge, not a subprocess: frozen_test.go already proves the subprocess case
// for the static answer, and repeating it here would trade a race-free
// counter for a sleep.
type ticker struct {
	gooey.Base
	starts, stops int
}

func (t *ticker) Start(post func(func())) func() {
	t.starts++
	return func() { t.stops++ }
}
func (t *ticker) Measure(gooey.Size) gooey.Size { return gooey.Size{W: 1, H: 1} }
func (t *ticker) Render(*gooey.Frame)           {}

const startablePage = `<Gooey>
  <VStack>
    <Frozen When="{{.Design}}">
      <Tick Name="tick"/>
    </Frozen>
    <Text>tail</Text>
  </VStack>
</Gooey>`

// TestFlippingFrozenStopsAndStartsItsSubtreeInTheSameFrame covers the
// consumer that is NOT the input tree, and it is the one with a safety
// argument: Composer.collect decides what runs, and <Companion>.Start
// spawns a child process. A design surface handed a document that freezes
// after the fact would otherwise leave that process running.
func TestFlippingFrozenStopsAndStartsItsSubtreeInTheSameFrame(t *testing.T) {
	ctx := designCtx(false)
	if ctx.Components == nil {
		ctx.Components = map[string]Builder{}
	}
	var tk *ticker
	ctx.Components["Tick"] = func(Element, *Context) (gooey.Component, error) {
		tk = &ticker{}
		return tk, nil
	}
	w, err := Build([]byte(startablePage), withFrozen(ctx))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	c := gooey.NewComposer(w, 30, 8)
	d := gooey.NewDispatcher()
	c.Start(d)
	t.Cleanup(c.Close)
	c.Frame()

	if tk == nil {
		t.Fatal("<Tick> was never built")
	}
	if tk.starts != 1 || tk.stops != 0 {
		t.Fatalf("built live, the ticker is starts=%d stops=%d, want 1/0: "+
			"nothing was running for the freeze to stop", tk.starts, tk.stops)
	}

	designProp(ctx).Set(true)
	c.Frame()
	if tk.stops != 1 {
		t.Errorf("one frame after freezing, the ticker stopped %d times, want 1 — "+
			"a Startable inside a picture is still running", tk.stops)
	}

	designProp(ctx).Set(false)
	c.Frame()
	if tk.starts != 2 {
		t.Errorf("one frame after unfreezing, the ticker started %d times, want 2: "+
			"the stop is one-way", tk.starts)
	}
}

// ---- the pointer, which Resync's liveness checks cannot reach ----

const capturePage = `<Gooey>
  <VStack>
    <Frozen When="{{.Design}}">
      <Sink Name="sink"/>
    </Frozen>
    <Text>tail</Text>
  </VStack>
</Gooey>`

// TestFreezingDropsACaptureTakenBeforeTheFlip is the hole DispatchMouse's
// retarget cannot close, and the reason evictFrozen exists.
//
// A press captures implicitly, from the RAW hit, before any routing. So a
// drag begun while the surface was live has a captor pointing at a
// descendant, and target() returns the captor ahead of the retargeted hit
// — which means every subsequent motion, and the release, goes on
// steering the thing inside the picture. Resync's own liveness check does
// not catch it: it tests m.parent, and m.parent records frozen
// descendants deliberately.
func TestFreezingDropsACaptureTakenBeforeTheFlip(t *testing.T) {
	ctx := designCtx(false)
	c := frozenPage(t, capturePage, ctx)
	leaf, err := Find[*sink](ctx, "sink")
	if err != nil {
		t.Fatal(err)
	}
	b := leaf.Bounds()
	if b.W == 0 || b.H == 0 {
		t.Fatal("the sink was never arranged: the pointer cannot be over it")
	}
	press := input.MouseEvent{Kind: input.MousePress, Button: input.ButtonLeft, X: b.X, Y: b.Y}
	c.HandleMouse(press)
	if c.Focus().Captured() != gooey.Component(leaf) {
		t.Fatalf("the press did not capture the sink (captor is %T): nothing to drop",
			c.Focus().Captured())
	}
	before := len(leaf.got)

	designProp(ctx).Set(true)
	c.Frame()
	if got := c.Focus().Captured(); got != nil {
		t.Errorf("the capture survived the freeze, held by %T", got)
	}

	// And the drag really is over: the release routes to the frozen host,
	// not back into the subtree. Without this the assertion above could be
	// satisfied by clearing a field nothing reads.
	rel := press
	rel.Kind = input.MouseRelease
	c.HandleMouse(rel)
	if n := len(leaf.got) - before; n != 0 {
		t.Errorf("the sink received %d more events after the freeze, want 0", n)
	}
	host := findFrozenHost(c.Root())
	if host == nil {
		t.Fatal("no frozen host in the page")
	}
	if len(host.got) == 0 {
		t.Error("the release reached nobody: it should have routed to the frozen host")
	}
}

// TestFreezingMovesHoverOffTheDescendant is the same hole for hover, and
// it retargets rather than clearing: the pointer really is over the frozen
// host, so the state to jump to is the state the next motion event would
// have produced anyway.
func TestFreezingMovesHoverOffTheDescendant(t *testing.T) {
	ctx := designCtx(false)
	ctx.Values["Fire"] = gooey.Command(func() {})
	c := frozenPage(t, `<Gooey>
  <VStack>
    <Frozen When="{{.Design}}">
      <Button Name="btn" Content="go" Click="{{.Fire}}"/>
    </Frozen>
    <Text>tail</Text>
  </VStack>
</Gooey>`, ctx)
	btn, err := Find[*components.Button](ctx, "btn")
	if err != nil {
		t.Fatal(err)
	}
	b := btn.Bounds()
	c.HandleMouse(input.MouseEvent{Kind: input.MouseMove, X: b.X, Y: b.Y})
	if c.Focus().Hovered() != gooey.Component(btn) {
		t.Fatalf("the live page did not hover the button (hover is %T): nothing to move",
			c.Focus().Hovered())
	}

	designProp(ctx).Set(true)
	c.Frame()
	if c.Focus().Hovered() == gooey.Component(btn) {
		t.Error("the button kept the hover after the subtree around it froze")
	}
	if btn.IsHovered() {
		t.Error("the button still paints itself hovered")
	}
}

// ---- what a flip must NOT cost ----

// modePage reads the SAME property in a paintable component and in the
// host's answer, which is what a real design surface does: the status bar
// says DESIGN or LIVE, and the surface freezes, off one source.
const modePage = `<Gooey>
  <VStack>
    <Frozen When="{{.Design}}">
      <VStack>
        <Text Name="inner">{{.In}}</Text>
        <Text>pad</Text>
      </VStack>
    </Frozen>
    <Text Name="outer">{{.Out}}</Text>
  </VStack>
</Gooey>`

// TestAFlipRepaintsNothingOfItsOwn is the damage pin, and it is the
// assertion that would catch the lazy implementation of all this — forcing
// the subtree to repaint on a re-sync, or marking the host dirty to "make
// sure".
//
// Freezing changes what the tree MEANS, not what it looks like. Nothing in
// this page reads Design while painting, so the honest cost of the flip is
// ZERO repaints: the focus eviction has nowhere to go (focus is on neither
// Text) and the re-sync reuses every node.
func TestAFlipRepaintsNothingOfItsOwn(t *testing.T) {
	ctx := designCtx(false)
	c := frozenPage(t, modePage, ctx)
	// Settle: the first Frame in frozenPage painted everything.
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("the composition had not settled: %d components repainted with "+
			"nothing changed", painted)
	}

	designProp(ctx).Set(true)
	_, painted := c.Frame()
	if painted != 0 {
		t.Errorf("freezing repainted %d components, want 0: freezing changes routing, "+
			"not pixels — damage %v", painted, c.Damage())
	}

	// Discrimination: the harness CAN report a repaint, so the 0 above is a
	// measurement and not a stuck counter.
	ctx.Values["In"].(*prop.Property[string]).Set("changed")
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("a Text inside the frozen subtree changed and %d components "+
			"repainted, want 1: the 0 above proved nothing", painted)
	}
}

// TestANoOpSetDoesNotReSyncTheComposition guards the idempotence
// prop.Set does not: setting Design to the value it already holds
// invalidates the observer exactly as a real flip does, and a mechanism
// that re-synced on invalidation rather than on a CHANGED answer would
// walk the tree, stop and restart Startables and rebuild the focus order
// for nothing.
//
// The probe is indirect and says so: a spurious re-sync is cheap enough to
// be invisible in focus, damage or hover, so what is measured is COST —
// how many times Frozen() is called in the frame. The observer re-arming
// costs exactly one call; a re-sync costs more, because walk asks once per
// container and frozenAncestor asks again per node. The flip arm is what
// makes the relation meaningful rather than an unfalsifiable small number.
func TestANoOpSetDoesNotReSyncTheComposition(t *testing.T) {
	ctx := designCtx(false)
	c := frozenPage(t, modePage, ctx)
	host := findFrozenHost(c.Root())
	if host == nil {
		t.Fatal("no frozen host in the page")
	}

	host.calls = 0
	designProp(ctx).Set(false) // the value it already holds
	c.Frame()
	noop := host.calls

	host.calls = 0
	designProp(ctx).Set(true) // a real flip
	c.Frame()
	flip := host.calls

	if noop != 1 {
		t.Errorf("a no-op Set cost %d Frozen() calls, want exactly 1 (the observer "+
			"re-arming): anything more means the composition re-synced", noop)
	}
	if flip <= noop {
		t.Fatalf("a real flip cost %d Frozen() calls and a no-op cost %d: the two are "+
			"indistinguishable, so the assertion above proves nothing", flip, noop)
	}
}

// ---- the documented limit ----

const plainStatePage = `<Gooey>
  <VStack>
    <Frozen State="Flag">
      <TextBox Name="inside" Text="{{.In}}"/>
    </Frozen>
    <TextBox Name="outside" Text="{{.Out}}"/>
  </VStack>
</Gooey>`

// TestFrozenOverPlainGoStateIsStillSampled pins the mechanism's BOUNDARY,
// and it is a test of a limitation rather than of a feature — deliberately,
// because the limitation is the kind that presents as flakiness. The
// observer subscribes to what Frozen() reads; a bare Go bool is read by
// nobody, so nothing wakes and the old sampled behaviour is what you get.
//
// Writing this down as a passing test rather than as a sentence is the
// point: the sentence would survive someone "fixing" it with a per-frame
// poll over every node, and this would not.
//
// The second half is the escape hatch the doc names, and it is what keeps
// this from reading as "plain state cannot work": InvalidateStructure
// re-syncs on demand, and then the very same page re-routes.
func TestFrozenOverPlainGoStateIsStillSampled(t *testing.T) {
	flag := false
	ctx := &Context{Values: map[string]any{
		"Flag": &flag,
		"In":   prop.NewSource("in"),
		"Out":  prop.NewSource("out"),
	}}
	c := frozenPage(t, plainStatePage, ctx)
	if n := boxesIn(c.Focus().Order()); n != 2 {
		t.Fatalf("built live, the page has %d TextBox focus stops, want 2", n)
	}

	flag = true
	c.Frame()
	if n := boxesIn(c.Focus().Order()); n != 2 {
		t.Errorf("a Frozen() over plain Go state re-routed after %d stops remained — "+
			"if this now works, the mechanism grew a poll and the doc comment on "+
			"gooey.Frozen is wrong", n)
	}

	c.InvalidateStructure()
	c.Frame()
	if n := boxesIn(c.Focus().Order()); n != 1 {
		t.Errorf("after InvalidateStructure the page has %d TextBox focus stops, want 1: "+
			"the escape hatch the doc names does not work", n)
	}
}

// TestFreezingClearsTheFocusMemory closes the last of the things
// m.parent's deliberate liveness lie keeps alive across a freeze.
//
// PreviouslyFocused is what an overlay restores to on dismiss, and it
// tests liveness against m.parent — which records frozen descendants on
// purpose (see walk). So without evictFrozen's clear it goes on naming a
// component that is now inside a picture, SetFocus refuses it because it
// is no longer in m.order, and the caller gets a bare false.
//
// That is the failure worth pinning: not a crash, not a misroute, but an
// overlay written as `if !SetFocus(PreviouslyFocused())` with no fallback
// leaving focus wherever it happened to be. Returning nil says "there is
// nothing to restore to", which is true and which a caller can branch on.
//
// The unfrozen arm is what stops this passing vacuously: the same tab and
// the same flip against a host that never freezes must still remember.
func TestFreezingClearsTheFocusMemory(t *testing.T) {
	run := func(freeze bool) (prev gooey.Component, inside gooey.Component) {
		ctx := designCtx(false)
		c := frozenPage(t, designPage, ctx)
		in, err := Find[*components.TextBox](ctx, "inside")
		if err != nil {
			t.Fatal(err)
		}
		// Focus the inside box, then move away: that MOVE is what records
		// m.prev, and it has to happen while the subtree is still live.
		if !c.Focus().SetFocus(in) {
			t.Fatal("could not focus the inside box while the host was live")
		}
		c.Focus().FocusNext()
		if got := c.Focus().PreviouslyFocused(); got != gooey.Component(in) {
			t.Fatalf("PreviouslyFocused = %T before any freeze, want the inside box", got)
		}

		if freeze {
			designProp(ctx).Set(true)
		}
		c.Frame()
		return c.Focus().PreviouslyFocused(), in
	}

	if prev, _ := run(true); prev != nil {
		t.Errorf("PreviouslyFocused = %T after the freeze, want nil: it names a "+
			"component inside a picture, and SetFocus will refuse it with a bare false", prev)
	}
	if prev, in := run(false); prev != gooey.Component(in) {
		t.Errorf("the control arm forgot too (%T): the freeze case proves nothing", prev)
	}
}
