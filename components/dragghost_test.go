package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
)

// A page shaped like an app that hosts free adornments: h rows of filler
// under a layer declared LAST (document order is z-order). Every filler
// is a Text — no HoverState anywhere — so the damage counts below are
// the ghost's alone and not some host's hover repaint, the same reason
// tipPage uses a Text host.
func ghostPage(w, h int) (*DragGhost, *AdornmentLayer, gooey.Component) {
	rows := make([]gooey.Component, 0, h+1)
	for y := 0; y < h; y++ {
		t := &Text{Content: Str(strings.Repeat("#", w))}
		t.LayoutProps().Top = y
		rows = append(rows, t)
	}
	layer := &AdornmentLayer{}
	ghost := &DragGhost{Label: Str("3 files")}
	return ghost, layer, &Canvas{Children: append(rows, layer)}
}

func motion(x, y int) input.MouseEvent {
	return input.MouseEvent{Kind: input.MouseMove, X: x, Y: y}
}

// counter wires the composition's scheduler hook so a test can ask the
// question a painted count cannot: was a frame ever REQUESTED? Driving
// Frame() by hand is the harness doing the thing under test, so "the
// pointer costs nothing" has to be asserted here, before any frame.
func counter(c *gooey.Composer) *int {
	n := 0
	c.OnInvalidate(func() { n++ })
	return &n
}

// quiet is the EFFECT half of a zero-cost claim: it asserts that a
// stretch of pointer motion changed nothing a user or a terminal could
// observe — no cell differs, and no byte reaches the wire.
//
// It deliberately does NOT replace the invalidation and painted counts
// beside it, because for this feature effect and cost are different
// quantities. The flusher is a cell DIFF, so a component that repaints
// and draws exactly what was already there emits zero bytes and leaves
// the screen byte-identical: an observer asserting only effect passes
// happily on a tree that is repainting on every cell the pointer
// crosses, which is the precise failure ?1003h makes ruinous and the
// one issue #177 exists to avoid. Effect proves nothing VISIBLE
// happened; the counts prove no WORK happened. Both, or the claim is
// half made.
func quiet(t *testing.T, c *gooey.Composer, before string, sink *strings.Builder, what string) {
	t.Helper()
	sink.Reset()
	if err := c.Flush(sink); err != nil {
		t.Fatal(err)
	}
	if n := c.FlushBytes(); n != 0 {
		t.Fatalf("%s emitted %d bytes to the terminal, want 0", what, n)
	}
	if got := screen(c, 30, 5); got != before {
		t.Fatalf("%s changed cells.\nbefore:\n%s\nafter:\n%s", what, before, got)
	}
}

// THE ZERO-COST GUARANTEE, asserted where it actually lives. ?1003h
// delivers a report per cell crossed; with nothing following the pointer
// those reports must not even SCHEDULE a frame, let alone paint one.
// The invalidation count is the load-bearing assertion — painted==0
// would also hold if the frame were scheduled and simply found nothing
// dirty, which is a completely different (and much worse) cost.
func TestPointerMotionWithoutAFollowerSchedulesNoFrame(t *testing.T) {
	_, _, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	inval := counter(c)
	c.Frame()
	var sink strings.Builder
	c.Flush(&sink) // settle the terminal so FlushBytes below means something
	before := screen(c, 30, 5)

	*inval = 0
	for x := 0; x < 12; x++ {
		c.HandleMouse(motion(x, 2))
	}
	if *inval != 0 {
		t.Fatalf("12 cells of pointer motion scheduled %d frames, want 0", *inval)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("motion with no follower painted %d components, want 0", painted)
	}
	quiet(t, c, before, &sink, "12 cells of motion with no follower")
}

// The same guarantee for a ghost that is IN the layer but not following
// — the parked half of the PointerFollower split. It has a paint node
// and an armed observer; what it does not have is an edge from the
// pointer, because FollowsPointer is false and the observer's
// short-circuit never reads it.
func TestParkedGhostCostsNothingPerMotion(t *testing.T) {
	ghost, layer, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	inval := counter(c)
	layer.Add(ghost) // in the layer, never Shown: parked
	c.Frame()
	c.Frame()
	var sink strings.Builder
	c.Flush(&sink)
	before := screen(c, 30, 5)

	*inval = 0
	for x := 0; x < 12; x++ {
		c.HandleMouse(motion(x, 2))
	}
	if *inval != 0 {
		t.Fatalf("12 cells of motion past a PARKED ghost scheduled %d frames, want 0", *inval)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("motion past a parked ghost painted %d components, want 0", painted)
	}
	quiet(t, c, before, &sink, "12 cells of motion past a parked ghost")
	if b := ghost.Bounds(); b.W != 0 || b.H != 0 {
		t.Fatalf("parked ghost occupies %v, want a zero rect", b)
	}
}

// Flipping FollowsPointer is what STARTS the wake, and it starts it by
// itself: the observer calls FollowsPointer unconditionally, so the
// property that method reads is already a dependency while the ghost is
// parked. Nothing external invalidates anything.
func TestFollowingFlipSchedulesAFrameAndShowsTheGhost(t *testing.T) {
	ghost, layer, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	inval := counter(c)
	layer.Add(ghost)
	c.HandleMouse(motion(4, 2))
	c.Frame()
	c.Frame()

	*inval = 0
	ghost.Show(c.Focus()) // already in the layer: only the flag flips
	if *inval == 0 {
		t.Fatal("starting to follow scheduled no frame")
	}
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("the ghost appearing painted %d components, want 1 (the ghost)", painted)
	}
	if got := row(c.Cells(), 3); !strings.Contains(got, " 3 files ") {
		t.Fatalf("row 3 = %q, want the ghost below-right of the pointer", got)
	}
}

// Placement: the label sits at the pointer cell plus the offset, which
// defaults to {1,1} — down and right, deliberately NOT under the
// emulator's own pointer glyph, which nothing portable can hide.
func TestDragGhostFollowsThePointerByItsOffset(t *testing.T) {
	ghost, _, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	c.Frame()
	c.HandleMouse(motion(4, 2))
	ghost.Show(c.Focus())
	c.Frame()

	if got, want := ghost.Bounds(), (gooey.Rect{X: 5, Y: 3, W: 9, H: 1}); got != want {
		t.Fatalf("ghost at %v, want %v (pointer 4,2 + default offset 1,1)", got, want)
	}
	c.HandleMouse(motion(9, 1))
	c.Frame()
	if got, want := ghost.Bounds(), (gooey.Rect{X: 10, Y: 2, W: 9, H: 1}); got != want {
		t.Fatalf("after motion the ghost is at %v, want %v", got, want)
	}
}

// THE PER-MOTION DAMAGE PIN. One cell of pointer motion during a drag
// repaints FOUR components, and every one of them is named here because
// a bare number is what lets a regression hide:
//
//	{6 3 9 1}  the ghost, at its new rect — the only real paint
//	{0 3 30 1} the filler row it just uncovered, restored
//	{0 0 30 5} the Canvas, and
//	{0 0 30 5} the AdornmentLayer
//
// The last two are full-page rects that paint NO cells: restoreUnder
// forces every paintable node intersecting the vacated rect, with no
// exemption for a chrome-only container or a Decorator, so both
// ancestors are swept. That is pre-existing composer behaviour, not
// something free adornments introduced — it is the same shape as the
// tooltip's pinned dismissal (restored leaf + 2 swept containers) — and
// it costs nothing on the wire, which the byte pin below is here to
// keep true. If this number moves, find out which rect appeared; do not
// raise the ceiling.
func TestDragMotionRepaintsTheGhostAndWhatItUncovered(t *testing.T) {
	ghost, _, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	c.Frame()
	c.HandleMouse(motion(4, 2))
	ghost.Show(c.Focus())
	c.Frame()
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("the drag did not settle: %d components painted", painted)
	}

	c.HandleMouse(motion(5, 2))
	_, painted := c.Frame()
	if painted != 4 {
		t.Fatalf("one cell of drag motion painted %d components, want 4 "+
			"(ghost + uncovered row + Canvas + layer); damage was %v", painted, c.Damage())
	}
	want := []gooey.Rect{{X: 0, Y: 0, W: 30, H: 5}, {X: 0, Y: 3, W: 30, H: 1},
		{X: 0, Y: 0, W: 30, H: 5}, {X: 6, Y: 3, W: 9, H: 1}}
	got := c.Damage()
	if len(got) != len(want) {
		t.Fatalf("damage %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("damage[%d] = %v, want %v (full set %v)", i, got[i], want[i], got)
		}
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("the frame after a motion painted %d components, want 0", painted)
	}
}

// The two full-page rects above must stay free on the WIRE. A drag is
// the one gesture that repaints per cell crossed, so if a swept
// container ever starts emitting cells this is where it shows up: the
// flush is a cell diff, and 46 bytes is the ghost's own row plus the one
// cell it uncovered. A full-page emission on this page would be an order
// of magnitude more.
func TestDragMotionStaysCheapOnTheWire(t *testing.T) {
	ghost, _, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	c.Frame()
	c.HandleMouse(motion(4, 2))
	ghost.Show(c.Focus())
	c.Frame()
	var sink strings.Builder
	c.Flush(&sink)

	for i := 0; i < 3; i++ {
		c.HandleMouse(motion(5+i, 2))
		c.Frame()
		sink.Reset()
		if err := c.Flush(&sink); err != nil {
			t.Fatal(err)
		}
		if n := c.FlushBytes(); n > 80 {
			t.Fatalf("motion %d flushed %d bytes, want the ghost's row and the cell it "+
				"uncovered (~46), not a page", i, n)
		}
	}
}

// Hide takes the ghost out of the layer and the screen goes back to
// exactly what it was — the departing-adornment restore, unchanged by
// being free. And the motion AFTER a hide is free again: the paint node
// and its observer left with the ghost.
func TestHideRestoresTheScreenAndTheZeroCost(t *testing.T) {
	ghost, _, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	inval := counter(c)
	c.Frame()
	before := screen(c, 30, 5)

	c.HandleMouse(motion(4, 2))
	ghost.Show(c.Focus())
	c.Frame()
	if got := screen(c, 30, 5); got == before {
		t.Fatal("the ghost never painted: the screen is unchanged with a ghost up")
	}

	ghost.Hide()
	c.Frame()
	if got := screen(c, 30, 5); got != before {
		t.Fatalf("hiding the ghost left a scar.\nbefore:\n%s\nafter:\n%s", before, got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("the frame after a hide painted %d components, want 0", painted)
	}
	*inval = 0
	for x := 0; x < 12; x++ {
		c.HandleMouse(motion(x, 2))
	}
	if *inval != 0 {
		t.Fatalf("motion after Hide scheduled %d frames, want 0", *inval)
	}
}

// A retitled ghost repaints itself and nothing else — the ordinary
// property rule, still true for a component the tree does not position.
func TestGhostLabelChangeRepaintsTheGhostAlone(t *testing.T) {
	ghost, _, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	c.Frame()
	c.HandleMouse(motion(4, 2))
	ghost.Show(c.Focus())
	c.Frame()
	c.Frame()

	ghost.Label.Set("7 files")
	_, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("retitling the ghost painted %d components, want 1; damage %v", painted, c.Damage())
	}
	if got := row(c.Cells(), 3); !strings.Contains(got, " 7 files ") {
		t.Fatalf("row 3 = %q, want the new label", got)
	}
}

// Placement clamps into the layer rather than falling off it: a drag
// into the bottom-right corner slides the ghost along the edge. It
// deliberately does not FLIP to the other side of the pointer the way an
// anchored tooltip does — a ghost that jumped sides near an edge would
// read as a rival cursor.
func TestGhostClampsAtTheScreenEdge(t *testing.T) {
	ghost, _, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	c.Frame()
	c.HandleMouse(motion(29, 4))
	ghost.Show(c.Focus())
	c.Frame()

	b := ghost.Bounds()
	if b.X+b.W > 30 || b.Y+b.H > 5 || b.X < 0 || b.Y < 0 {
		t.Fatalf("ghost at %v falls outside the 30x5 layer", b)
	}
	if got, want := b, (gooey.Rect{X: 21, Y: 4, W: 9, H: 1}); got != want {
		t.Fatalf("ghost at %v, want %v (clamped, not flipped)", got, want)
	}
}

// nilAnchored is an ordinary (non-free) adornment whose anchor is nil.
// It exists to discriminate: the layer must still drop THIS one, which
// is what proves the free exemption is keyed on PointerFollower and not
// on "Anchor returned nil".
type nilAnchored struct {
	gooey.Base
	orphans int
}

func (n *nilAnchored) Anchor() gooey.Component          { return nil }
func (n *nilAnchored) Place(_, _ gooey.Rect) gooey.Rect { return gooey.Rect{W: 1, H: 1} }
func (n *nilAnchored) Measure(_ gooey.Size) gooey.Size  { return gooey.Size{W: 1, H: 1} }
func (n *nilAnchored) Render(*gooey.Frame)              {}
func (n *nilAnchored) orphaned()                        { n.orphans++ }

// The exemption, both ways round. A free adornment answers nil to Anchor
// and is kept anyway — nothing can orphan it, because there is no anchor
// to be gone — while an ordinary adornment answering nil is dropped and
// told. Without the discriminating half, a layer that simply stopped
// dropping things would pass.
func TestFreeAdornmentSurvivesANilAnchorAndAnAnchoredOneDoesNot(t *testing.T) {
	ghost, layer, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	c.Frame()
	c.HandleMouse(motion(4, 2))
	ghost.Show(c.Focus())

	orphan := &nilAnchored{}
	layer.Add(orphan)
	c.Frame()
	c.Frame()

	if orphan.orphans != 1 {
		t.Fatalf("the anchored adornment with a nil anchor was orphaned %d times, want 1", orphan.orphans)
	}
	adorns := layer.Adornments()
	if len(adorns) != 1 || adorns[0] != gooey.Component(ghost) {
		t.Fatalf("layer holds %d adornments, want just the free ghost", len(adorns))
	}
	if b := ghost.Bounds(); b.W == 0 {
		t.Fatalf("the free ghost was dropped or zeroed: bounds %v", b)
	}
}

// Show on a page that declares no layer reports false and leaves the
// ghost out of every tree — the same supported "this app shows no
// adornments" answer Tooltip and ValidationMarker give.
func TestShowWithoutALayerReportsFalse(t *testing.T) {
	filler := &Text{Content: Str("no layer here")}
	page := &Canvas{Children: []gooey.Component{filler}}
	c := gooey.NewComposer(page, 30, 5)
	c.Frame()

	ghost := &DragGhost{Label: Str("3 files")}
	if ghost.Show(c.Focus()) {
		t.Fatal("Show reported success on a page with no AdornmentLayer")
	}
	if ghost.Show(nil) {
		t.Fatal("Show reported success with no focus manager")
	}
	if ghost.FollowsPointer() {
		t.Fatal("a ghost that was never placed is following the pointer")
	}
}

// EVERY cell of a drag schedules its own frame, not just the first. A
// dirty computed stays dirty until it is read, and prop.invalidate()
// returns early on an already-dirty node — so an observer the frame sweep
// forgets to re-Get fires ONCE and then goes deaf for the rest of the
// gesture. Nothing else here catches that: the tests around this one call
// Frame() by hand after each motion, which is the harness performing the
// very scheduling under test, so the ghost still appears to follow.
func TestEveryMotionOfADragSchedulesItsOwnFrame(t *testing.T) {
	ghost, _, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	inval := counter(c)
	c.Frame()
	c.HandleMouse(motion(4, 2))
	ghost.Show(c.Focus())
	c.Frame()
	c.Frame()

	for i := 0; i < 4; i++ {
		*inval = 0
		c.HandleMouse(motion(5+i, 2))
		if *inval == 0 {
			t.Fatalf("motion %d of the drag scheduled no frame (the observer went deaf)", i)
		}
		c.Frame()
	}
}

// A re-reported CELL costs nothing. prop.Set does not compare values, so
// notePointer guards on the cell itself: an emulator is free to send the
// same cell twice (a press and its release, sub-cell motion), and without
// the guard each repeat would be a frame for the whole length of a drag.
func TestRepeatedPointerCellSchedulesNothing(t *testing.T) {
	ghost, _, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	inval := counter(c)
	c.Frame()
	c.HandleMouse(motion(4, 2))
	ghost.Show(c.Focus())
	c.Frame()
	c.Frame()

	*inval = 0
	for i := 0; i < 5; i++ {
		c.HandleMouse(motion(4, 2)) // the cell it is already on
	}
	if *inval != 0 {
		t.Fatalf("5 repeats of the SAME cell scheduled %d frames during a drag, want 0", *inval)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("re-reporting the same cell painted %d components, want 0", painted)
	}
}

// A follower present at CONSTRUCTION still follows. NewComposer walks the
// node list before it builds the FocusManager, so build cannot arm the
// observer for these — Frame's late arm is the only thing that ever will,
// and without it a ghost placed before the first frame is deaf forever.
func TestFollowerPresentBeforeTheFirstFrameStillFollows(t *testing.T) {
	ghost, layer, page := ghostPage(30, 5)
	layer.Add(ghost) // in the tree before the composition exists
	c := gooey.NewComposer(page, 30, 5)
	inval := counter(c)
	c.Frame()
	c.HandleMouse(motion(4, 2))
	ghost.Show(c.Focus())
	c.Frame()
	c.Frame()

	*inval = 0
	c.HandleMouse(motion(5, 2))
	if *inval == 0 {
		t.Fatal("motion scheduled no frame for a follower that existed before the first frame")
	}
	c.Frame()
	if got, want := ghost.Bounds(), (gooey.Rect{X: 6, Y: 3, W: 9, H: 1}); got != want {
		t.Fatalf("ghost at %v, want %v", got, want)
	}
}

// The pointer cell comes off EVERY kind of pointer event, not just
// motion: a ghost raised inside a press handler must find the pointer
// already where the press landed. MouseTarget is the counter-case — it
// is a query, and a query moves nothing.
func TestPointerCellTracksEveryEventKindButNotAQuery(t *testing.T) {
	_, _, page := ghostPage(30, 5)
	c := gooey.NewComposer(page, 30, 5)
	c.Frame()
	m := c.Focus()

	if _, seen := m.Pointer(); seen {
		t.Fatal("the pointer is reported as seen before any pointer event")
	}
	c.HandleMouse(press(7, 3))
	if got, seen := m.Pointer(); !seen || got != (gooey.Rect{X: 7, Y: 3, W: 1, H: 1}) {
		t.Fatalf("after a press the pointer is %v (seen=%v), want {7 3 1 1}", got, seen)
	}
	c.HandleMouse(motion(8, 3))
	if got, _ := m.Pointer(); got.X != 8 {
		t.Fatalf("after motion the pointer is %v, want x=8", got)
	}
	c.HandleMouse(release(9, 3))
	if got, _ := m.Pointer(); got.X != 9 {
		t.Fatalf("after a release the pointer is %v, want x=9", got)
	}
	m.MouseTarget(motion(20, 1))
	if got, _ := m.Pointer(); got.X != 9 || got.Y != 3 {
		t.Fatalf("MouseTarget moved the pointer to %v; a query must move nothing", got)
	}
}
