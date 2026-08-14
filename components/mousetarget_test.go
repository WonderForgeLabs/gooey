package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
)

// FocusManager.MouseTarget answers "where would this event go?" for a
// caller that has to decide something before delivering it — a
// control-plane session checking that a guest's pointer stays inside its
// island.
//
// The only thing that makes such an answer worth anything is that it
// matches where the event ACTUALLY goes, so these tests never assert
// MouseTarget against a hand-written expectation. They dispatch the
// event, ask the tree who received it, and compare. A MouseTarget that
// drifted from DispatchMouse — a new retarget added to one and not the
// other — fails here rather than in whatever depends on it.

// recorder is a leaf that remembers it was the one delivered to.
type recorder struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	hits int
}

func (r *recorder) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: avail.W, H: min(1, avail.H)}
}
func (r *recorder) Render(f *gooey.Frame) { r.IsFocused(); r.IsHovered() }
func (r *recorder) HandleMouse(ev input.MouseEvent) bool {
	r.hits++
	return true
}

// MouseMove does NOT route through HandleMouse — DispatchMouse walks
// MouseMoveHandler for it. A probe that only implemented HandleMouse
// reported "delivered to nil" for every motion event, which is exactly
// the kind of silent hole this file exists to catch, so both handlers
// are implemented and the ground truth covers both paths.
func (r *recorder) HandleMouseMove(ev input.MouseEvent) bool {
	r.hits++
	return true
}

// freezer is a container whose subtree renders but does not act — the
// shape preview.Pane has in the wysiwyg design surface.
type freezer struct {
	gooey.Base
	gooey.FocusState
	gooey.HoverState
	Child  gooey.Component
	frozen bool
	hits   int
}

func (f *freezer) Frozen() bool                             { return f.frozen }
func (f *freezer) ChildComponents() []gooey.Component       { return []gooey.Component{f.Child} }
func (f *freezer) Measure(avail gooey.Size) gooey.Size      { return gooey.MeasureChild(f.Child, avail) }
func (f *freezer) Arrange(r gooey.Rect)                     { f.Base.Arrange(r); gooey.ArrangeChild(f.Child, r) }
func (f *freezer) Render(fr *gooey.Frame)                   { f.IsFocused(); f.IsHovered() }
func (f *freezer) HandleMouse(ev input.MouseEvent) bool     { f.hits++; return true }
func (f *freezer) HandleMouseMove(ev input.MouseEvent) bool { f.hits++; return true }
func (f *freezer) AcceptsFocus() bool                       { return true }

// delivered dispatches ev and reports which of the candidates took it,
// by watching their hit counters. It is the ground truth every
// assertion below compares against.
func delivered(c *gooey.Composer, ev input.MouseEvent, cands map[gooey.Component]*int) gooey.Component {
	before := map[gooey.Component]int{}
	for w, n := range cands {
		before[w] = *n
	}
	c.HandleMouse(ev)
	for w, n := range cands {
		if *n > before[w] {
			return w
		}
	}
	return nil
}

func TestMouseTargetMatchesWhereTheEventActuallyGoes(t *testing.T) {
	leaf := &recorder{}
	host := &freezer{Child: leaf}
	peer := &recorder{}
	root := &VStack{Children: []gooey.Component{host, peer}}
	c := gooey.NewComposer(root, 20, 4)
	c.Frame()

	cands := map[gooey.Component]*int{leaf: &leaf.hits, host: &host.hits, peer: &peer.hits}
	inHost := host.Bounds().Y
	inPeer := peer.Bounds().Y

	cases := []struct {
		name   string
		freeze bool
		ev     input.MouseEvent
	}{
		{"press, thawed", false, press(1, inHost)},
		{"press, FROZEN", true, press(1, inHost)},
		{"move, thawed", false, move(1, inHost)},
		{"move, FROZEN", true, move(1, inHost)},
		{"press on the peer", false, press(1, inPeer)},
	}
	for _, tc := range cases {
		host.frozen = tc.freeze
		c.Focus().ReleaseCapture()
		c.Focus().Resync()

		want := c.Focus().MouseTarget(tc.ev)
		got := delivered(c, tc.ev, cands)
		if want != got {
			t.Errorf("%s: MouseTarget said %T, the event was delivered to %T", tc.name, want, got)
		}
	}

	// The frozen case is the one that must not be vacuous: it has to
	// answer the HOST while the thawed case answers the leaf, or the
	// comparison above proves nothing about retargeting.
	host.frozen = false
	c.Focus().ReleaseCapture()
	c.Focus().Resync()
	if got := c.Focus().MouseTarget(move(1, inHost)); got != gooey.Component(leaf) {
		t.Errorf("thawed target = %T, want the leaf", got)
	}
	host.frozen = true
	c.Focus().Resync()
	if got := c.Focus().MouseTarget(move(1, inHost)); got != gooey.Component(host) {
		t.Errorf("frozen target = %T, want the frozen host", got)
	}
}

// Capture overrides the hit entirely — that is what makes a drag work
// outside the captor's bounds, and it is the case a check written
// against HitTest alone gets backwards.
func TestMouseTargetFollowsAHeldCaptureOffItsBounds(t *testing.T) {
	leaf := &recorder{}
	host := &freezer{Child: leaf}
	peer := &recorder{}
	root := &VStack{Children: []gooey.Component{host, peer}}
	c := gooey.NewComposer(root, 20, 4)
	c.Frame()
	cands := map[gooey.Component]*int{leaf: &leaf.hits, host: &host.hits, peer: &peer.hits}

	if !c.Focus().CaptureMouse(leaf) {
		t.Fatal("CaptureMouse refused a live component")
	}
	// Point at the PEER while the leaf holds the pointer.
	ev := move(1, peer.Bounds().Y)
	want := c.Focus().MouseTarget(ev)
	if want != gooey.Component(leaf) {
		t.Fatalf("captured target = %T, want the captor", want)
	}
	if got := delivered(c, ev, cands); got != want {
		t.Errorf("MouseTarget said %T, delivered to %T", want, got)
	}

	// A fresh PRESS discards an IMPLICIT capture but not a HELD one, and
	// MouseTarget has to model that: with the held capture still on, a
	// press over the peer is the captor's.
	pev := press(1, peer.Bounds().Y)
	if got := c.Focus().MouseTarget(pev); got != gooey.Component(leaf) {
		t.Errorf("press under a HELD capture = %T, want the captor", got)
	}
	c.Focus().ReleaseCapture()

	// Now make the capture IMPLICIT — a press sets one — and check the
	// asymmetry from the other side.
	c.HandleMouse(press(1, leaf.Bounds().Y))
	if c.Focus().Captured() != gooey.Component(leaf) {
		t.Fatalf("a press did not set an implicit captor: %T", c.Focus().Captured())
	}
	pev = press(1, peer.Bounds().Y)
	want = c.Focus().MouseTarget(pev)
	if want != gooey.Component(peer) {
		t.Fatalf("press under an IMPLICIT capture = %T, want the new hit", want)
	}
	if got := delivered(c, pev, cands); got != want {
		t.Errorf("MouseTarget said %T, delivered to %T", want, got)
	}
}
