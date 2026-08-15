package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
)

// DESIGN ↔ LIVE, asserted against the SHIPPED page.
//
// This is what the framework's dynamic-Frozen mechanism exists for, and
// until this file there was no consumer of it anywhere — which is why the
// mechanism was a documented constraint ("return a constant, or make the
// flip a structural change") rather than a feature. Everything here is
// therefore end-to-end on purpose: the real wysiwyg.gooey, the real
// editor, the real key event, and no direct call to the property the
// binding is supposed to reach.
//
// The document the editor starts with contains a <Button>, which is the
// discriminator every assertion below leans on: a Button is a focus stop,
// a mnemonic handler and a click target, so "is it in the focus order"
// answers "is the picture operable" without needing a probe component the
// user would never have.

// designPage composes the shipped page at a size the shell actually fits
// in. The Grid declares 4 + 38 + 1* + 46 columns, so at the 80 the other
// tests use the designer's star track resolves to nothing and the
// document is arranged at zero size — which does not change the focus
// order, but does make every mouse assertion untestable.
func designPage(t *testing.T) (*editor, *gooey.Composer) {
	t.Helper()
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 160, 48)
	t.Cleanup(c.Close)
	c.Frame()
	return ed, c
}

// docStops is how many of the composition's focus stops live inside the
// designer — the document, not the editor's own chrome.
func docStops(t *testing.T, c *gooey.Composer) int {
	t.Helper()
	pane := findPreview(c.Root())
	if pane == nil {
		t.Fatal("the shipped page does not mount the designer")
	}
	inside := map[gooey.Component]bool{}
	var walk func(gooey.Component)
	walk = func(w gooey.Component) {
		inside[w] = true
		for _, k := range children(w) {
			walk(k)
		}
	}
	walk(pane)
	n := 0
	for _, w := range c.Focus().Order() {
		if inside[w] && w != pane {
			n++
		}
	}
	return n
}

func pressD(c *gooey.Composer) bool {
	return c.Handle(input.KeyOf(input.KeyEvent{Key: input.KeyRune, Rune: 'd'}))
}

// TestPressingDReRoutesTheDesignerInTheSameFrame is the consumer test.
//
// The keystroke goes in through Composer.Handle, exactly as the terminal
// delivers it, and ONE Frame follows. That single frame is the claim: the
// mechanism is worth having only if the re-route is complete before the
// user's next event, and a test that composed twice would pass for a
// mechanism that needed a second, unrelated structural change to catch up
// — which is precisely the old behaviour.
func TestPressingDReRoutesTheDesignerInTheSameFrame(t *testing.T) {
	ed, c := designPage(t)

	// DESIGN is the default, and the document must be inert in it.
	if !ed.design.Get() {
		t.Fatal("the editor did not start in DESIGN mode")
	}
	if n := docStops(t, c); n != 0 {
		t.Errorf("in DESIGN mode the document contributes %d focus stops, want 0: "+
			"tab walks out of the editor and into the picture", n)
	}

	if !pressD(c) {
		t.Fatal("the root KeyBinding did not consume 'd'")
	}
	c.Frame()
	if ed.design.Get() {
		t.Fatal("'d' did not reach ToggleMode")
	}
	live := docStops(t, c)
	if live == 0 {
		t.Fatal("one frame after going LIVE the document still contributes no focus " +
			"stops — either the re-route did not happen or the document has nothing " +
			"focusable, and the second would make the 0 above meaningless")
	}

	// And back, which is the direction with teeth: making a reachable thing
	// unreachable while the user is looking at it.
	if !pressD(c) {
		t.Fatal("the root KeyBinding did not consume the second 'd'")
	}
	c.Frame()
	if n := docStops(t, c); n != 0 {
		t.Errorf("one frame after returning to DESIGN the document still contributes "+
			"%d focus stops, want 0 (it had %d while live)", n, live)
	}
}

// TestTheDesignerSwallowsAClickInDesignModeOnly is the pointer half, and
// it is the gesture the mode is actually named after: in DESIGN a click on
// the document's Button must not run its Click, and in LIVE it must.
//
// The Button's command is the editor's own "Noop", so the assertion cannot
// be made on a side effect the app already has. Focus is the observable
// instead: a press moves focus to the nearest focusable at-or-above the
// hit, and freezing retargets that hit to the pane. So in DESIGN the click
// leaves focus outside the document; in LIVE it lands on the Button.
func TestTheDesignerSwallowsAClickInDesignModeOnly(t *testing.T) {
	_, c := designPage(t)
	btn := findButton(c.Root())
	if btn == nil {
		t.Fatal("the starting document has no Button to click")
	}
	b := btn.Bounds()
	if b.W == 0 || b.H == 0 {
		t.Fatalf("the Button was never arranged (%v): the pointer cannot be over it", b)
	}
	click := func() {
		press := input.MouseEvent{Kind: input.MousePress, Button: input.ButtonLeft, X: b.X, Y: b.Y}
		c.HandleMouse(press)
		press.Kind = input.MouseRelease
		c.HandleMouse(press)
	}

	click()
	if c.Focus().Focused() == gooey.Component(btn) {
		t.Error("a click in DESIGN mode focused the document's Button: the picture is operable")
	}
	// Hit-testing still finds it, which is what keeps click-to-select
	// possible later. Freezing constrains dispatch, not this query.
	if got := c.Focus().HitTest(b.X, b.Y); got != gooey.Component(btn) {
		t.Errorf("HitTest returned %T, want the Button: a design surface has to be able "+
			"to find what the pointer is over", got)
	}

	pressD(c)
	c.Frame()
	// Bounds can move when the mode label changes width; re-read them.
	b = btn.Bounds()
	click()
	if c.Focus().Focused() != gooey.Component(btn) {
		t.Errorf("a click in LIVE mode focused %T, want the Button: the DESIGN arm above "+
			"proved nothing", c.Focus().Focused())
	}
}

// TestTheModeFlipRepaintsOnlyTheIndicator is the damage pin, and it is the
// number this whole design is trying to keep small.
//
// Freezing changes what the tree MEANS, not what it looks like, so the
// only thing that may repaint for a flip is what reads the mode while
// painting — the status bar's centre section, which is its own paint node
// precisely so this is one component and not the whole bar. Anything more
// means something forced a repaint to "make sure" the re-route took.
func TestTheModeFlipRepaintsOnlyTheIndicator(t *testing.T) {
	_, c := designPage(t)
	// Settle: the composition's first frame painted everything.
	for i := 0; i < 3; i++ {
		if _, painted := c.Frame(); painted == 0 {
			break
		} else if i == 2 {
			t.Fatalf("the composition never settled: %d components still repainting", painted)
		}
	}

	pressD(c)
	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("the mode flip repainted %d components, want 1 (the status bar's centre "+
			"section): damage %v", painted, c.Damage())
	}
	// Discrimination: the counter can report other numbers, so the 1 above
	// is a measurement rather than a constant.
	if _, settled := c.Frame(); settled != 0 {
		t.Fatalf("the next frame repainted %d components with nothing changed: the count "+
			"above is not measuring damage", settled)
	}
}

// TestTheStatusBarSaysWhichModeItIsIn — a mode with no indicator is a mode
// the user discovers by being surprised. This reads the CELL PLANE, so it
// fails if the binding is right and the section is arranged off screen,
// which is the failure the properties pane already had once.
func TestTheStatusBarSaysWhichModeItIsIn(t *testing.T) {
	_, c := designPage(t)
	if got := screen(c); !strings.Contains(got, "DESIGN") {
		t.Errorf("the status bar does not say DESIGN anywhere on screen")
	}
	pressD(c)
	c.Frame()
	got := screen(c)
	if !strings.Contains(got, "LIVE —") {
		t.Error("after 'd' the status bar does not say LIVE")
	}
	if strings.Contains(got, "DESIGN —") {
		t.Error("the old DESIGN label is still on screen beside the new one: " +
			"the section repainted without clearing")
	}
}

// TestTheTwoModeLabelsAreTheSameWidth guards the damage pin above from
// the cheapest possible regression: rewording one label.
//
// A wider label moves the section's bounds, and a bounds change vacates
// cells — so the Composer clears the old rect and force-repaints the
// status bar and the root Grid beneath it. Measured before the labels were
// evened up: a one-component flip cost three. Nothing about that is a bug,
// which is exactly why nothing else would catch it.
func TestTheTwoModeLabelsAreTheSameWidth(t *testing.T) {
	d, l := utf8.RuneCountInString(ModeDesign), utf8.RuneCountInString(ModeLive)
	if d != l {
		t.Errorf("ModeDesign is %d runes and ModeLive is %d: the mode flip now moves "+
			"the status bar's centre section, which force-repaints everything the old "+
			"rect covered", d, l)
	}
}

// screen is the retained cell plane as text — what a user would see.
func screen(c *gooey.Composer) string {
	var b strings.Builder
	cells := c.Cells()
	cols, rows := c.Size()
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			r := cells.At(x, y).Rune
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func findButton(w gooey.Component) *components.Button {
	if b, ok := w.(*components.Button); ok {
		return b
	}
	for _, k := range children(w) {
		if got := findButton(k); got != nil {
			return got
		}
	}
	return nil
}
