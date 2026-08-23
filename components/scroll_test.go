package components

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// clock is a hand-wound test clock. Wheel velocity is a function of the
// interval between notches, so the tiers are simulated rather than slept —
// the same trick ItemsView's own wheel tests use through its Now field.
type clock struct{ t time.Time }

func (c *clock) now() time.Time       { return c.t }
func (c *clock) tick(d time.Duration) { c.t = c.t.Add(d) }

func newScroller(off int) (*Scroller, *prop.Property[int]) {
	p := prop.NewSource(off)
	return &Scroller{Offset: p}, p
}

// A viewport at least as tall as its content cannot scroll at all: Max is
// zero, so every gesture clamps back to the top. This is the case that
// keeps a short article from sliding off its own pane.
func TestScrollerCannotScrollContentThatFits(t *testing.T) {
	s, p := newScroller(0)
	if got := s.Max(5, 20); got != 0 {
		t.Fatalf("Max(extent 5, viewport 20) = %d, want 0", got)
	}
	s.By(+10, 5, 20)
	if got := p.Get(); got != 0 {
		t.Fatalf("scrolling content that fits moved the offset to %d, want 0", got)
	}
}

// The far end stops with the last unit against the edge rather than
// scrolling into blank space, and the near end stops at zero.
func TestScrollerClampsAtBothEnds(t *testing.T) {
	s, p := newScroller(0)
	s.By(+1000, 100, 10)
	if got, want := p.Get(), 90; got != want {
		t.Fatalf("scrolling past the end left offset %d, want %d (extent 100 - viewport 10)", got, want)
	}
	s.By(-1000, 100, 10)
	if got := p.Get(); got != 0 {
		t.Fatalf("scrolling past the start left offset %d, want 0", got)
	}
}

// The load-bearing one. prop.Set does not compare values, so a Scroller
// that Set unconditionally would invalidate every dependent on every key
// repeat while the offset stood still — a repaint per keystroke at the
// bottom of a document. The pin is a damage count, not the offset value:
// the value is right either way.
func TestScrollerAtTheEndIsDamageFree(t *testing.T) {
	s, p := newScroller(0)
	painted := &probe{offset: p}
	c := gooey.NewComposer(painted, 20, 4)
	c.Frame()

	s.By(+1000, 100, 10) // travel to the end: this one really moves
	if _, n := c.Frame(); n != 1 {
		t.Fatalf("scrolling to the end painted %d components, want 1", n)
	}
	for i := 0; i < 5; i++ {
		s.By(+1, 100, 10) // already there: must not Set
	}
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("%d components repainted while the offset stood still at the end, want 0", n)
	}
}

// A gesture is consumed even when it did not move anything, so j/k do not
// suddenly bubble to an ancestor the moment the user reaches the bottom.
func TestScrollerConsumesAGestureItCannotActOn(t *testing.T) {
	s, _ := newScroller(0)
	if !s.By(-1, 100, 10) {
		t.Fatal("a gesture at the top reported unconsumed; it would bubble to an ancestor")
	}
}

// A Scroller with no Offset is ItemsView in SELECTION mode: it still
// measures wheel velocity, but owns no offset to move.
func TestScrollerWithoutAnOffsetStillMeasuresVelocity(t *testing.T) {
	s := &Scroller{}
	if s.By(+1, 100, 10) {
		t.Fatal("By reported consumed with no Offset to move")
	}
	if got := s.At(100, 10); got != 0 {
		t.Fatalf("At with no Offset = %d, want 0", got)
	}
	if got := s.WheelStep(100, +1); got != 1 {
		t.Fatalf("the first notch stepped %d, want 1", got)
	}
}

// One notch is one unit until notches arrive fast enough to read as a
// continuous gesture. The tiers are entered by RUN LENGTH, so the opening
// notches of any gesture are precise however fast the first two arrive —
// which is what lets a slow wheel touch every line.
func TestScrollerWheelAcceleratesByRunLength(t *testing.T) {
	ck := &clock{t: time.Unix(0, 0)}
	s := &Scroller{Now: ck.now}
	const extent = 1000

	var steps []int
	for i := 0; i < wheelFlickRun+1; i++ {
		steps = append(steps, s.WheelStep(extent, +1))
		ck.tick(wheelFastGap / 2) // fast enough to sustain the run
	}
	if steps[0] != 1 || steps[1] != 1 || steps[2] != 1 {
		t.Fatalf("the first three notches stepped %v, want three single units", steps[:3])
	}
	if want := max(2, extent*5/100); steps[wheelFastRun] != want {
		t.Fatalf("notch %d stepped %d, want %d (the fast tier)", wheelFastRun, steps[wheelFastRun], want)
	}
	if want := max(5, extent*15/100); steps[wheelFlickRun] != want {
		t.Fatalf("notch %d stepped %d, want %d (the flick tier)", wheelFlickRun, steps[wheelFlickRun], want)
	}
}

// A pause longer than wheelFastGap ends the gesture, and the next notch is
// precise again. Without this a slow, deliberate roll would inherit the
// velocity of a flick made seconds earlier.
func TestScrollerWheelRunDecaysAfterAPause(t *testing.T) {
	ck := &clock{t: time.Unix(0, 0)}
	s := &Scroller{Now: ck.now}
	for i := 0; i < wheelFlickRun+1; i++ {
		s.WheelStep(1000, +1)
		ck.tick(wheelFastGap / 2)
	}
	ck.tick(wheelFastGap * 3) // the user let go
	if got := s.WheelStep(1000, +1); got != 1 {
		t.Fatalf("the first notch after a pause stepped %d, want 1", got)
	}
}

// Reversing direction ends the run too: a flick down followed by a flick
// up must not start at flick speed going the other way.
func TestScrollerWheelRunResetsOnReversal(t *testing.T) {
	ck := &clock{t: time.Unix(0, 0)}
	s := &Scroller{Now: ck.now}
	for i := 0; i < wheelFlickRun+1; i++ {
		s.WheelStep(1000, +1)
		ck.tick(wheelFastGap / 2)
	}
	if got := s.WheelStep(1000, -1); got != 1 {
		t.Fatalf("the first notch after a reversal stepped %d, want 1", got)
	}
}

// probe is a leaf whose paint reads one offset and nothing else, so the
// Composer's painted count is exactly "did that offset change".
type probe struct {
	gooey.Base
	offset *prop.Property[int]
}

func (p *probe) Measure(avail gooey.Size) gooey.Size { return avail }
func (p *probe) Render(*gooey.Frame)                 { p.offset.Get() }
