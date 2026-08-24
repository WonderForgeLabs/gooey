package components

import (
	"time"

	"github.com/WonderForgeLabs/gooey/prop"
)

// Scroller is the scroll MODEL — the half of "a viewport over content
// taller than itself" that is the same everywhere, factored out of the
// half that never is.
//
// The distinction is the whole point of this type, so it is worth being
// precise about. A viewport has two independent parts:
//
//   - the VIEWPORT MODEL: what a visible unit is, how many fit, and how
//     the content at a given offset reaches the screen. This differs
//     fundamentally between hosts. ItemsView realizes uniform-height rows
//     from an ItemSource; the reader's article pane lays out wrapped prose
//     whose line count depends on the pane's own width. Neither can be
//     expressed in the other's terms — see
//     docs/specs/2026-08-23-scrolling.md — so this type does not try.
//   - the SCROLL MODEL: where the offset lives, how it is clamped against
//     the content, and what a gesture does to it. That part IS the same,
//     and it is what lives here.
//
// Two properties of the scroll model are easy to get wrong by hand, which
// is the practical reason not to write it twice:
//
//   - the Set must COMPARE first. prop.Set does not compare values
//     (prop/prop.go), so an uncompared Set at either end of the content
//     would invalidate every dependent and repaint on every key repeat
//     while nothing moved. By is the compared Set.
//   - the wheel is VELOCITY-SENSITIVE, in tiers entered by run length.
//     That is a dozen lines of timing state a second copy would have got
//     subtly different, giving one pane a different feel from the next.
//
// What a Scroller deliberately does NOT decide is the ANCHOR: whether
// offset 0 means the head of the content or the tail of it, and therefore
// which way a given key moves. A log pane anchors to the tail so offset 0
// follows appends; a document anchors to the head so offset 0 is the first
// line. Both clamp identically against Max, so the anchor stays with the
// host, where the difference is visible in the host's own key switch
// rather than hidden behind a flag here.
type Scroller struct {
	// Offset is the scroll position in units, shared with the viewmodel so
	// the app can read it and reset it. Nil is legal: a host that only
	// wants WheelStep — ItemsView in SELECTION mode, where the wheel moves
	// the selection rather than an offset — leaves it unset.
	Offset *prop.Property[int]
	// Now is the clock wheel velocity is measured against. Nil means
	// time.Now; tests inject a fake so event rate is simulated, not slept.
	Now func() time.Time

	// Wheel-velocity state: when the last notch arrived, which way it went,
	// and how many have arrived back-to-back. Plain fields, not properties —
	// a notch's bookkeeping must not be damage.
	wheelAt  time.Time
	wheelDir int
	wheelRun int
}

// Wheel velocity. One notch is one unit — the precision that lets a slow
// wheel touch every line — until notches arrive fast enough to read as a
// continuous gesture, at which point the step scales with the CONTENT, so a
// flick crosses a long article or a big collection in a bounded number of
// notches instead of the fixed lines-per-notch that made every flick
// overshoot to the end. The tiers are entered by run length, not by one fast
// interval, so the first notches of any gesture are always precise.
const (
	// wheelFastGap is the longest interval that still sustains a run;
	// terminals deliver a flick's notches a few ms apart, a deliberate slow
	// roll hundreds of ms apart.
	wheelFastGap = 120 * time.Millisecond
	// wheelFastRun and wheelFlickRun are the run lengths entering the fast
	// (~5% of the content) and flick (~15%) tiers.
	wheelFastRun  = 3
	wheelFlickRun = 9
)

func (s *Scroller) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Max is the largest meaningful offset: content that does not overflow the
// viewport cannot scroll at all, and content that does stops with its last
// unit against the far edge rather than scrolling into blank space.
func (s *Scroller) Max(extent, viewport int) int { return max(0, extent-viewport) }

// At is the clamped offset. Like every Get in gooey, what this call MEANS is
// decided by the call site: inside a Render it subscribes the painting
// component to the offset, and in Arrange or a key handler it is a plain
// read. Hosts rely on both, deliberately.
func (s *Scroller) At(extent, viewport int) int {
	if s.Offset == nil {
		return 0
	}
	return clamp(s.Offset.Get(), 0, s.Max(extent, viewport))
}

// By moves the offset d units and reports the gesture consumed. The Set is
// compared, so holding a key against either end of the content costs
// nothing — no invalidation, no repaint.
//
// It reports consumed even when the offset did not move, because a key at
// the end of a document is still a key this pane answered; letting it bubble
// would hand j/k to an ancestor the moment the user hit the bottom.
func (s *Scroller) By(d, extent, viewport int) bool {
	if s.Offset == nil {
		return false
	}
	if off := clamp(s.At(extent, viewport)+d, 0, s.Max(extent, viewport)); off != s.Offset.Get() {
		s.Offset.Set(off)
	}
	return true
}

// WheelStep is how many units this notch moves, given the recent event rate.
// A pause longer than wheelFastGap or a change of direction resets the run to
// the precise tier. The percentage tiers are floored (2 and 5 units) so they
// outrun the base tier even on short content.
//
// dir is the gesture's direction, +1 or -1. Only its SIGN is used, and only
// to notice a reversal — the caller applies it to the returned step, which is
// always positive.
func (s *Scroller) WheelStep(extent, dir int) int {
	t := s.now()
	if dir == s.wheelDir && !s.wheelAt.IsZero() && t.Sub(s.wheelAt) <= wheelFastGap {
		s.wheelRun++
	} else {
		s.wheelRun = 0
	}
	s.wheelAt, s.wheelDir = t, dir
	switch {
	case s.wheelRun >= wheelFlickRun:
		return max(5, extent*15/100)
	case s.wheelRun >= wheelFastRun:
		return max(2, extent*5/100)
	}
	return 1
}
