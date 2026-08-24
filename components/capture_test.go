package components

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
)

// dragPane records every pointer event it is offered, motion included,
// and never consumes motion so the tests can see what routing did rather
// than what a handler decided.
type dragPane struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	got   []input.MouseEvent
	moves []input.MouseEvent
}

func (p *dragPane) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: avail.W, H: min(1, avail.H)}
}
func (p *dragPane) Render(f *gooey.Frame) { p.IsFocused(); p.IsHovered() }

func (p *dragPane) HandleMouse(ev input.MouseEvent) bool {
	p.got = append(p.got, ev)
	return true
}

func (p *dragPane) HandleMouseMove(ev input.MouseEvent) bool {
	p.moves = append(p.moves, ev)
	return true
}

func (p *dragPane) kinds() []input.MouseKind {
	ks := make([]input.MouseKind, 0, len(p.got))
	for _, e := range p.got {
		ks = append(ks, e.Kind)
	}
	return ks
}

func dragMove(x, y int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MouseMove, Button: input.ButtonLeft, X: x, Y: y}
}

// twoPanes lays two one-row panes out one above the other.
func twoPanes(t *testing.T) (*dragPane, *dragPane, *gooey.Composer) {
	t.Helper()
	a, b := &dragPane{}, &dragPane{}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{a, b}}, 20, 4)
	c.Frame()
	return a, b, c
}

// The core of capture: once a press lands, motion outside the component
// still reaches it and the release comes back to it, so a drag that
// leaves the bounds keeps tracking.
func TestPressCapturesTheDrag(t *testing.T) {
	a, b, c := twoPanes(t)
	ay := a.Bounds().Y

	c.HandleMouse(press(2, ay))
	c.HandleMouse(dragMove(5, b.Bounds().Y)) // over the OTHER pane
	c.HandleMouse(release(5, b.Bounds().Y))

	if len(a.moves) != 1 {
		t.Fatalf("the captor saw %d motion events during the drag, want 1", len(a.moves))
	}
	if got := a.moves[0].X; got != 5 {
		t.Errorf("motion delivered to the captor at x=%d, want the real pointer x=5", got)
	}
	if len(b.got) != 0 || len(b.moves) != 0 {
		t.Errorf("the pane under the pointer saw %v/%v; a captured pointer must not reach it", b.got, b.moves)
	}
	want := []input.MouseKind{input.MousePress, input.MouseRelease}
	if got := a.kinds(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("captor saw %v, want press then release (and NO click: released elsewhere)", got)
	}
}

// Click synthesis keys off the captor: released outside it there is no
// click, released back inside there is — which is a button pressed,
// dragged off, and dragged back.
func TestClickSynthesisFollowsTheCaptor(t *testing.T) {
	a, b, c := twoPanes(t)
	ay, by := a.Bounds().Y, b.Bounds().Y

	c.HandleMouse(press(2, ay))
	c.HandleMouse(dragMove(2, by))
	c.HandleMouse(dragMove(2, ay)) // back inside
	c.HandleMouse(release(2, ay))

	if got := a.kinds(); len(got) != 3 || got[2] != input.MouseClick {
		t.Fatalf("slide off and back gave %v, want press, release, click", got)
	}
	if len(b.got) != 0 {
		t.Errorf("the other pane saw %v during a captured drag", b.got)
	}
}

// Hover is frozen for the length of the gesture and catches up on
// release. Without the freeze, dragging across the tree would repaint
// every component the pointer crossed.
func TestHoverIsSuppressedWhileCaptured(t *testing.T) {
	a, b, c := twoPanes(t)
	ay, by := a.Bounds().Y, b.Bounds().Y

	c.HandleMouse(move(2, ay))
	if !a.IsHovered() {
		t.Fatal("hover did not arrive before the press")
	}
	c.HandleMouse(press(2, ay))
	c.HandleMouse(dragMove(2, by))
	if b.IsHovered() {
		t.Error("the pane under a captured pointer took hover")
	}
	if !a.IsHovered() {
		t.Error("the captor lost hover mid-drag; transitions are suppressed, not inverted")
	}
	c.HandleMouse(release(2, by))
	if a.IsHovered() || !b.IsHovered() {
		t.Error("hover did not catch up with the pointer when the capture ended")
	}
}

// Explicit capture outlives the release, which is what a component needs
// when the gesture is not bounded by one press.
func TestExplicitCaptureOutlivesTheRelease(t *testing.T) {
	a, b, c := twoPanes(t)
	fm := c.Focus()
	if !fm.CaptureMouse(a) {
		t.Fatal("CaptureMouse refused a component in the tree")
	}
	if fm.Captured() != gooey.Component(a) {
		t.Fatalf("Captured() = %v, want the pane", fm.Captured())
	}
	c.HandleMouse(press(2, b.Bounds().Y))
	c.HandleMouse(release(2, b.Bounds().Y))
	if fm.Captured() != gooey.Component(a) {
		t.Error("a held capture was given back by a release")
	}
	if len(b.got) != 0 {
		t.Errorf("events reached the pane under the pointer while another held the capture: %v", b.got)
	}
	fm.ReleaseCapture()
	c.HandleMouse(press(2, b.Bounds().Y))
	if len(b.got) == 0 {
		t.Error("ReleaseCapture did not give the pointer back to hit-testing")
	}
	if fm.CaptureMouse(&dragPane{}) {
		t.Error("CaptureMouse accepted a component that is not in the tree")
	}
}

// A press without its release must not strand the pointer: the next
// press starts a fresh gesture, which is the recovery path for a report
// the terminal dropped.
func TestImplicitCaptureIsScopedToOneGesture(t *testing.T) {
	a, b, c := twoPanes(t)
	c.HandleMouse(press(2, a.Bounds().Y))
	c.HandleMouse(press(2, b.Bounds().Y)) // no release in between
	if len(b.got) == 0 {
		t.Fatal("a second press stayed with the first press's captor")
	}
	if c.Focus().Focused() != gooey.Component(b) {
		t.Error("focus-follows-click was skipped for the second press")
	}
}

// Click counting is interval-based and keyed to the component.
func TestDoubleClickCount(t *testing.T) {
	a, b, c := twoPanes(t)
	fm := c.Focus()
	now := time.Unix(0, 0)
	fm.Now = func() time.Time { return now }

	click := func(w *dragPane) {
		y := w.Bounds().Y
		c.HandleMouse(press(2, y))
		c.HandleMouse(release(2, y))
	}
	counts := func(w *dragPane) []int {
		var out []int
		for _, e := range w.got {
			if e.Kind == input.MouseClick {
				out = append(out, e.Count)
			}
		}
		return out
	}

	click(a)
	now = now.Add(100 * time.Millisecond)
	click(a)
	if got := counts(a); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("two quick clicks gave counts %v, want [1 2]", got)
	}
	// A third inside the interval is a TRIPLE click. This assertion used
	// to require the sequence to restart at 1 here, which was the
	// deliberate ceiling while there was no triple-click consumer; the
	// ceiling still exists, it is just at gooey.MaxClickCount now.
	now = now.Add(100 * time.Millisecond)
	click(a)
	if got := counts(a); len(got) != 3 || got[2] != 3 {
		t.Fatalf("a third click gave counts %v, want [1 2 3]", got)
	}
	// The FOURTH is where the sequence restarts, for the reason the
	// third used to: reporting a number nothing understands is worse
	// than starting fresh.
	now = now.Add(100 * time.Millisecond)
	click(a)
	if got := counts(a); len(got) != 4 || got[3] != 1 {
		t.Fatalf("a fourth rapid click gave counts %v, want it to restart at 1", got)
	}
	// Too slow, and on a different component: both restart the count.
	now = now.Add(time.Second)
	click(a)
	if got := counts(a); got[4] != 1 {
		t.Errorf("a click past the interval counted %d, want 1", got[4])
	}
	now = now.Add(10 * time.Millisecond)
	click(b)
	if got := counts(b); len(got) != 1 || got[0] != 1 {
		t.Errorf("a click on another component counted %v, want [1]", got)
	}
}

// ---- tunneling ----

// scrim is an ancestor that vetoes input on the way down.
type scrim struct {
	gooey.Base
	child gooey.Component
	on    bool
	keys  int
	mice  int
}

func (s *scrim) ChildComponents() []gooey.Component { return []gooey.Component{s.child} }
func (s *scrim) Measure(avail gooey.Size) gooey.Size {
	return gooey.MeasureChild(s.child, avail)
}
func (s *scrim) Arrange(b gooey.Rect) {
	s.Base.Arrange(b)
	gooey.ArrangeChild(s.child, b)
}
func (s *scrim) Render(*gooey.Frame) {}

func (s *scrim) PreviewKey(input.KeyEvent) bool {
	s.keys++
	return s.on
}

func (s *scrim) PreviewMouse(input.MouseEvent) bool {
	s.mice++
	return s.on
}

// A Preview that consumes stops the descent: the target never sees the
// event, and neither do the bindings that would have matched it.
func TestKeyTunnelVetoesBeforeTheTarget(t *testing.T) {
	pane := &dragPane{}
	sc := &scrim{child: pane}
	fired := 0
	root := &VStack{Children: []gooey.Component{sc}}
	root.Attach(&gooey.KeyBinding{Gesture: input.Rune('j'), Command: gooey.Command(func() { fired++ })})
	c := gooey.NewComposer(root, 20, 4)
	c.Frame()

	if c.HandleKey(input.Rune('j')); fired != 1 {
		t.Fatalf("with the scrim off the binding fired %d times, want 1", fired)
	}
	if sc.keys != 1 {
		t.Fatalf("the Preview handler was offered the key %d times, want 1", sc.keys)
	}
	sc.on = true
	if !c.HandleKey(input.Rune('j')) {
		t.Fatal("a consuming Preview did not report the event handled")
	}
	if fired != 1 {
		t.Errorf("the binding fired through a vetoing scrim (fired=%d)", fired)
	}
}

func TestMouseTunnelVetoesBeforeTheTarget(t *testing.T) {
	pane := &dragPane{}
	sc := &scrim{child: pane}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{sc}}, 20, 4)
	c.Frame()
	y := pane.Bounds().Y

	c.HandleMouse(press(2, y))
	c.HandleMouse(release(2, y))
	if len(pane.got) == 0 {
		t.Fatal("the target saw nothing with the scrim off")
	}
	if sc.mice == 0 {
		t.Fatal("PreviewMouse was never offered anything")
	}
	sc.on = true
	pane.got, pane.moves = nil, nil
	c.HandleMouse(press(2, y))
	c.HandleMouse(dragMove(3, y))
	c.HandleMouse(release(3, y))
	if len(pane.got) != 0 || len(pane.moves) != 0 {
		t.Errorf("events reached the target past a vetoing PreviewMouse: %v/%v", pane.got, pane.moves)
	}
}

// The tunnel runs root-first, which is what makes an outer veto beat an
// inner one.
func TestTunnelRunsRootFirst(t *testing.T) {
	pane := &dragPane{}
	innerScrim := &scrim{child: pane}
	outer := &scrim{child: &VStack{Children: []gooey.Component{innerScrim}}}
	c := gooey.NewComposer(&VStack{Children: []gooey.Component{outer}}, 20, 4)
	c.Frame()

	outer.on = true
	c.HandleKey(input.Rune('j'))
	if outer.keys != 1 {
		t.Fatalf("the outer Preview saw %d keys, want 1", outer.keys)
	}
	if innerScrim.keys != 0 {
		t.Errorf("the descent continued past a consuming ancestor (inner saw %d)", innerScrim.keys)
	}
}
