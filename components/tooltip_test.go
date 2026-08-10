package components

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
)

// A page with an adornment layer: a tooltipped Text host over a filler
// row, hosted the way an app declares it (layer last = top of z-order).
// The host is a Text on purpose — no HoverState — so the damage pins
// below count the tooltip alone, not the host's own hover repaint.
func tipPage(w int) (*Tooltip, gooey.Component, *AdornmentLayer, *Canvas) {
	host := &Text{Content: Str("save")}
	host.LayoutProps().Left, host.LayoutProps().Top = 2, 0
	filler := &Text{Content: Str(strings.Repeat("#", w))}
	filler.LayoutProps().Top = 1
	tip := &Tooltip{Text: Str("writes the file")}
	host.Attach(tip)
	layer := &AdornmentLayer{}
	page := &Canvas{Children: []gooey.Component{host, filler, layer}}
	return tip, host, layer, page
}

func hoverAt(c *gooey.Composer, x, y int) {
	c.HandleMouse(input.MouseEvent{Kind: input.MouseMove, X: x, Y: y})
}

// An un-started composition has no dispatcher to marshal a delayed show
// through, so hover-in shows immediately — which is also what makes the
// damage pins deterministic. Appearing paints exactly ONE component:
// the popup. The layer's own node stays clean.
func TestTooltipAppearPaintsThePopupAlone(t *testing.T) {
	tip, _, _, page := tipPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()

	hoverAt(c, 3, 0)
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("tooltip appear painted %d components, want 1 (the popup)", painted)
	}
	if !tip.IsShown() {
		t.Fatal("the tooltip is not shown")
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, " writes the file ") {
		t.Fatalf("row 1 = %q, want the tooltip below its host", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d components, want 0", painted)
	}
}

// Hover-out dismisses, and the vacated cells restore from what was
// beneath — the composer's restore pass through the Dynamic-departure
// path, at the same pinned cost a toast dismissal has on this shape.
func TestTooltipHoverOutRestoresWhatWasBeneath(t *testing.T) {
	tip, _, _, page := tipPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	before := screen(c, 30, 4)

	hoverAt(c, 3, 0)
	c.Frame()
	hoverAt(c, 25, 3)
	_, painted := c.Frame()
	if tip.IsShown() {
		t.Fatal("the tooltip is still shown after hover-out")
	}
	if painted != 3 {
		t.Fatalf("dismissing painted %d components, want 3 (restored leaf + 2 swept containers)", painted)
	}
	if got := screen(c, 30, 4); got != before {
		t.Fatalf("hover-out left a scar.\nbefore:\n%s\nafter:\n%s", before, got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d components, want 0", painted)
	}
}

// Any key dismisses — and does NOT consume: the key still routes. The
// tip stays down until the pointer leaves the host and comes back.
func TestTooltipKeyDismissesWithoutConsuming(t *testing.T) {
	tip, _, _, page := tipPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	before := screen(c, 30, 4)

	hoverAt(c, 3, 0)
	c.Frame()
	c.HandleKey(input.Rune('x'))
	c.Frame()
	if tip.IsShown() {
		t.Fatal("a keypress did not dismiss the tooltip")
	}
	if got := screen(c, 30, 4); got != before {
		t.Fatal("the key dismissal left a scar")
	}

	// Still hovering: motion within the host must not resurrect the tip.
	hoverAt(c, 4, 0)
	c.Frame()
	if tip.IsShown() {
		t.Fatal("the tooltip came back without the pointer leaving the host")
	}
	// Leave and return: the tip shows again.
	hoverAt(c, 25, 3)
	hoverAt(c, 3, 0)
	c.Frame()
	if !tip.IsShown() {
		t.Fatal("the tooltip did not show again after a fresh hover")
	}
}

func TestTooltipPressDismisses(t *testing.T) {
	tip, _, _, page := tipPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	hoverAt(c, 3, 0)
	c.Frame()
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 3, Y: 0})
	c.Frame()
	if tip.IsShown() {
		t.Fatal("a press did not dismiss the tooltip")
	}
}

// The delay is the Timer discipline: the goroutine posts the show, the
// UI loop runs it, and stopping the composition closes AND joins so no
// show can arrive after Close. A hover that ended before the timer
// fired posts a STALE show, which must do nothing.
func TestTooltipDelayPostsAndJoins(t *testing.T) {
	tip, _, _, page := tipPage(30)
	tip.Delay = time.Millisecond
	d := gooey.NewDispatcher()
	c := gooey.NewComposer(page, 30, 4)
	c.Start(d)
	c.Frame()

	hoverAt(c, 3, 0)
	if tip.IsShown() {
		t.Fatal("the tooltip showed before its delay")
	}
	waitFor(t, "the posted show", func() bool { return d.Pending() > 0 })
	if tip.IsShown() {
		t.Fatal("the delay goroutine showed the tooltip itself — it must post instead")
	}
	d.Drain()
	if !tip.IsShown() {
		t.Fatal("draining the dispatcher did not show the tooltip")
	}

	// A hover that ends before the timer fires: the posted show is stale.
	hoverAt(c, 25, 3)
	c.Frame()
	hoverAt(c, 3, 0)
	hoverAt(c, 25, 3) // gone again before the 1ms timer fires
	waitFor(t, "the stale posted show", func() bool { return d.Pending() > 0 })
	d.Drain()
	if tip.IsShown() {
		t.Fatal("a stale show fired after the hover already ended")
	}

	// Close joins: no post ever arrives afterwards.
	c.Close()
	hoverAt(c, 3, 0)
	time.Sleep(3 * time.Millisecond)
	if d.Pending() != 0 {
		t.Fatal("a hover after Close still posted a show")
	}
}

// A moved anchor drags its adornment along: layout re-reads the
// anchor's bounds every frame, and the bounds sweep restores what the
// popup vacated — all in the same frame.
func TestTooltipFollowsAMovingAnchor(t *testing.T) {
	tip, host, _, page := tipPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	hoverAt(c, 3, 0)
	c.Frame()
	if got := row(c.Cells(), 1); !strings.HasPrefix(got[2:], " writes the file ") {
		t.Fatalf("row 1 = %q, want the tooltip at x=2", got)
	}

	host.(*Text).LayoutProps().Left = 10
	c.Frame()
	if !tip.IsShown() {
		t.Fatal("the tooltip vanished when its anchor moved")
	}
	got := row(c.Cells(), 1)
	if !strings.HasPrefix(got[10:], " writes the file ") {
		t.Fatalf("row 1 = %q, want the tooltip moved to x=10", got)
	}
	if got[:10] != "##########" {
		t.Fatalf("row 1 = %q — the vacated cells did not restore", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d components, want 0", painted)
	}
}

// An anchor that goes non-visible takes its adornment down, through the
// layer's own sweep — the tooltip is told (orphaned), so it can show
// again later.
func TestTooltipAnchorVanishingRemovesTheAdornment(t *testing.T) {
	tip, host, layer, page := tipPage(30)
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	hoverAt(c, 3, 0)
	c.Frame()

	host.(*Text).LayoutProps().Visibility = gooey.Hidden
	c.Frame()
	if tip.IsShown() {
		t.Fatal("the tooltip is still shown after its anchor went non-visible")
	}
	if len(layer.Adornments()) != 0 {
		t.Fatal("the layer still hosts the orphaned popup")
	}
	if got := row(c.Cells(), 1); got != strings.Repeat("#", 30) {
		t.Fatalf("row 1 = %q — the popup's cells did not restore", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d components, want 0", painted)
	}
}

// Hovering across two tooltipped components never shows two at once.
func TestTooltipNeverShowsTwoAtOnce(t *testing.T) {
	a := &Text{Content: Str("aaaa")}
	a.LayoutProps().Left = 2
	tipA := &Tooltip{Text: Str("tip A")}
	a.Attach(tipA)
	b := &Text{Content: Str("bbbb")}
	b.LayoutProps().Left = 12
	tipB := &Tooltip{Text: Str("tip B")}
	b.Attach(tipB)
	layer := &AdornmentLayer{}
	page := &Canvas{Children: []gooey.Component{a, b, layer}}
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()

	hoverAt(c, 3, 0)
	c.Frame()
	if !tipA.IsShown() || tipB.IsShown() {
		t.Fatal("hovering A must show exactly A's tip")
	}
	hoverAt(c, 13, 0)
	if n := len(layer.Adornments()); n != 1 {
		t.Fatalf("crossing hosts left %d adornments up, want 1", n)
	}
	c.Frame()
	if tipA.IsShown() || !tipB.IsShown() {
		t.Fatal("hovering B must show exactly B's tip")
	}
	if got := row(c.Cells(), 1); !strings.Contains(got, " tip B") || strings.Contains(got, " tip A") {
		t.Fatalf("row 1 = %q, want only B's tip", got)
	}
}

// When tooltipped hosts nest, the innermost wins — a tooltip on a
// button inside a tooltipped panel shows the button's tip, not both.
func TestTooltipInnermostHostWins(t *testing.T) {
	inner := &Text{Content: Str("inner")}
	innerTip := &Tooltip{Text: Str("inner tip")}
	inner.Attach(innerTip)
	outer := &HStack{Children: []gooey.Component{inner}}
	outerTip := &Tooltip{Text: Str("outer tip")}
	outer.Attach(outerTip)
	layer := &AdornmentLayer{}
	page := &Canvas{Children: []gooey.Component{outer, layer}}
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()

	hoverAt(c, 1, 0)
	c.Frame()
	if !innerTip.IsShown() {
		t.Fatal("the inner host's tip is not shown")
	}
	if outerTip.IsShown() {
		t.Fatal("the outer host's tip is shown too — innermost must win")
	}
}

// A host with a declared KeyBinding gets the gesture rendered as a dim
// hint, in the canonical spelling — display only, like a MenuItem's.
func TestTooltipRendersTheHostGestureHint(t *testing.T) {
	host := &Text{Content: Str("save")}
	host.LayoutProps().Left = 2
	g, err := input.ParseGesture("ctrl+s")
	if err != nil {
		t.Fatal(err)
	}
	host.Attach(&gooey.KeyBinding{Gesture: g})
	tip := &Tooltip{Text: Str("writes the file")}
	host.Attach(tip)
	layer := &AdornmentLayer{}
	page := &Canvas{Children: []gooey.Component{host, layer}}
	c := gooey.NewComposer(page, 40, 4)
	c.Frame()

	hoverAt(c, 3, 0)
	c.Frame()
	got := row(c.Cells(), 1)
	if !strings.Contains(got, " writes the file ") || !strings.Contains(got, "ctrl+s") {
		t.Fatalf("row 1 = %q, want the text and the gesture hint", got)
	}
	hx := strings.Index(got, "ctrl+s")
	if !c.Cells().At(hx, 1).Style.Dim {
		t.Fatal("the gesture hint is not dim")
	}
}

// Flip-to-fit: a host on the bottom row shows its tip ABOVE itself.
func TestTooltipFlipsAboveAtTheScreenEdge(t *testing.T) {
	host := &Text{Content: Str("save")}
	host.LayoutProps().Left, host.LayoutProps().Top = 2, 3
	tip := &Tooltip{Text: Str("writes the file")}
	host.Attach(tip)
	layer := &AdornmentLayer{}
	page := &Canvas{Children: []gooey.Component{host, layer}}
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()

	hoverAt(c, 3, 3)
	c.Frame()
	if got := row(c.Cells(), 2); !strings.Contains(got, " writes the file") {
		t.Fatalf("row 2 = %q, want the tooltip flipped above its host", got)
	}
}

// A page with no AdornmentLayer shows nothing — degraded, not broken.
func TestTooltipWithoutALayerShowsNothing(t *testing.T) {
	host := &Text{Content: Str("save")}
	tip := &Tooltip{Text: Str("writes the file")}
	host.Attach(tip)
	page := &Canvas{Children: []gooey.Component{host}}
	c := gooey.NewComposer(page, 30, 4)
	c.Frame()
	hoverAt(c, 1, 0)
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("a layerless page painted %d components on hover, want 0", painted)
	}
	if tip.IsShown() {
		t.Fatal("the tooltip claims to be shown with no layer to show in")
	}
}

func screen(c *gooey.Composer, w, h int) string {
	var sb strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sb.WriteRune(c.Cells().At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
