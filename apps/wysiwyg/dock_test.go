package main

// The dock's contract tests. Three of them are DAMAGE COUNTS, and those
// are the only assertions in this file that could catch the failure they
// are about: "the pane is not showing" is equally true of a pane that
// vanished for free and of one that repainted the entire page on the way
// out, and a bounds assertion or a cell assertion cannot tell those
// apart.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
)

// dockFixture composes the SHIPPED page — not a hand-written miniature —
// because what is being tested is the arrangement the user gets. A
// miniature dock would pass while the real one was mis-declared.
func dockFixture(t *testing.T) (*editor, *gooey.Composer) {
	t.Helper()
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	c.Frame()
	settle(t, c)
	return ed, c
}

func pane(t *testing.T, ed *editor, id string) *dockPane {
	t.Helper()
	p := ed.dock.ByID(id)
	if p == nil {
		t.Fatalf("no pane %q in the dock", id)
	}
	return p
}

// rowText reads w cells of row y — the assertion primitive for "is
// anything drawn here".
func rowText(f *gooey.Frame, y, x, w int) string {
	var sb strings.Builder
	for i := 0; i < w; i++ {
		sb.WriteRune(f.Cells.At(x+i, y).Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

// TestHideIsNotCollapse is the central discrimination test, and it is
// written as one test rather than two because the whole claim is that the
// two operations DIFFER. Two separate tests would each pass against an
// implementation where hide and collapse were the same call.
func TestHideIsNotCollapse(t *testing.T) {
	ed, c := dockFixture(t)
	props := pane(t, ed, "properties")
	before := props.Bounds()
	if before.W <= 0 || before.H <= 0 {
		t.Fatalf("the PROPERTIES pane is %+v before anything happened", before)
	}

	// HIDE: the pane keeps its bounds. That is the user's rule ("it keeps
	// its state and its size") and it is gooey.Hidden's definition —
	// occupies space, does not paint.
	ed.dock.ToggleHidden(props)
	settle(t, c)
	if got := props.Bounds(); got != before {
		t.Errorf("hiding moved the pane from %+v to %+v; a HIDDEN pane keeps its size, "+
			"and a dock whose columns reflow when a pane is hidden is a dock that "+
			"rearranges itself under the user", before, got)
	}

	// And back, so the collapse half starts from the same place.
	ed.dock.ToggleHidden(props)
	settle(t, c)
	if got := props.Bounds(); got != before {
		t.Fatalf("showing the pane again left it at %+v, want %+v", got, before)
	}

	// COLLAPSE: the pane shrinks to its header row. This is the operation
	// that reclaims space, and it must NOT be what hide did.
	ed.dock.ToggleCollapsed(props)
	settle(t, c)
	got := props.Bounds()
	if got.H != headerH {
		t.Errorf("collapsing left the pane %d rows tall, want %d — a collapsed pane is "+
			"its header and nothing else", got.H, headerH)
	}
	if got == before {
		t.Error("collapsing changed nothing about the pane's bounds; collapse and hide " +
			"have become the same operation")
	}
}

// TestHidingAPaneBlanksItsContentAndKeepsItsSubtree is the pair of facts
// that a hidden CONTAINER forces, and it is the reason hiding a pane is
// two property writes rather than one.
//
// composer.go gives a hidden container the `covered` treatment: it fills
// its own bounds and the z-ordered pass repaints its subtree ABOVE it. So
// hiding the pane alone would erase the header and leave every child
// painting on top — the pane would "vanish" with its contents still on
// screen. The content is Collapsed as well, which takes it out of layout
// and out of paint while leaving the component itself in the tree.
func TestHidingAPaneBlanksItsContentAndKeepsItsSubtree(t *testing.T) {
	ed, c := dockFixture(t)
	props := pane(t, ed, "properties")
	b := props.Bounds()

	f, _ := c.Frame()
	live := rowText(f, b.Y+2, b.X, b.W)
	if strings.TrimSpace(live) == "" {
		t.Fatalf("the PROPERTIES pane draws nothing at row %d before hiding; this test "+
			"would pass vacuously", b.Y+2)
	}

	// What "keeps its state" means is that the components HOLDING state
	// are the same pointers afterwards — not that the subtree has the
	// same shape.
	//
	// THE SHAPE DOES NOT SURVIVE, and finding that out is what this test
	// is worth. A collapsed component measures zero, and ItemsView is a
	// Dynamic container that realizes only the rows it can actually see —
	// so at zero height it realizes none, and the subtree collapses from
	// about a hundred components to a dozen. That is correct: a realized
	// row is DERIVED from the item source, not state, and it is rebuilt
	// from the same data on the way back.
	//
	// What must survive is what a rebuild could not restore: the caret in
	// a TextBox, which is component-local and has no other home. So the
	// assertion is on the declared components by identity, and the row
	// count is recorded rather than asserted.
	before := statefulKids(props)
	if len(before) == 0 {
		t.Fatal("the PROPERTIES pane holds no state-carrying components; this test would " +
			"pass against an implementation that discarded the entire subtree")
	}
	beforeAll := 0
	walkTree(props, func(gooey.Component) { beforeAll++ })

	ed.dock.ToggleHidden(props)
	settle(t, c)
	f, _ = c.Frame()
	for dy := 0; dy < b.H; dy++ {
		if got := rowText(f, b.Y+dy, b.X, b.W); strings.TrimSpace(got) != "" {
			t.Errorf("row %d of the hidden pane still reads %q; the pane's own chrome was "+
				"erased but its children painted over the erasure", b.Y+dy, got)
			break
		}
	}

	afterAll := 0
	walkTree(props, func(gooey.Component) { afterAll++ })
	t.Logf("hidden pane subtree: %d components -> %d (the difference is ItemsView's "+
		"realized rows, which are derived and come back)", beforeAll, afterAll)

	after := statefulKids(props)
	if len(after) != len(before) {
		t.Fatalf("the hidden pane holds %d state-carrying components, was %d; hiding is "+
			"supposed to keep them — a TextBox's caret is component-local and a rebuild "+
			"cannot restore it", len(after), len(before))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("state-carrying component %d was REPLACED while the pane was hidden; "+
				"a replaced TextBox has a caret at 0 and the user's next character lands "+
				"mid-word", i)
		}
	}

	// And it comes back. A pane that hides cheaply and cannot be revealed
	// is not a pane, it is a deletion.
	ed.dock.ToggleHidden(props)
	settle(t, c)
	f, _ = c.Frame()
	if got := rowText(f, b.Y+2, b.X, b.W); strings.TrimSpace(got) == "" {
		t.Error("revealing the pane left it blank")
	}
}

// statefulKids is every component in the subtree whose state a rebuild
// could not restore. Today that is the TextBox (its caret) and the
// ItemsView (its scroll offset); both are DECLARED components, so both
// must survive by identity.
func statefulKids(root gooey.Component) []gooey.Component {
	var out []gooey.Component
	walkTree(root, func(k gooey.Component) {
		switch k.(type) {
		case *components.TextBox, *components.ItemsView:
			out = append(out, k)
		}
	})
	return out
}

// TestHidingAPaneCostsFarLessThanTheTree is the damage pin, and it is the
// assertion the task asked for by name.
//
// The number is not a guess and it is not free: a HIDDEN CONTAINER is
// marked `covered` by the Composer, which forces its subtree to repaint
// ABOVE it in the same frame. Here the subtree is Collapsed, so there is
// nothing to force — which is exactly why the count stays small, and
// exactly what would change if the pair were split back into one write.
func TestHidingAPaneCostsFarLessThanTheTree(t *testing.T) {
	ed, c := dockFixture(t)
	props := pane(t, ed, "properties")

	total := 0
	walkTree(c.Root(), func(gooey.Component) { total++ })

	ed.dock.ToggleHidden(props)
	_, painted := c.Frame()
	if painted == 0 {
		t.Fatal("hiding a pane repainted nothing at all: the pane did not leave the " +
			"screen, so every count below measures nothing")
	}
	t.Logf("hide repainted %d of %d components", painted, total)
	// A quarter of the tree is the ceiling. Loose enough to survive
	// another pane being added to the shell, tight enough that a hide
	// which re-mounts or re-composes — the failure this is watching for —
	// blows straight through it.
	if painted > total/4 {
		t.Errorf("hiding one pane repainted %d of %d components; damage %v", painted, total, c.Damage())
	}
	if _, again := c.Frame(); again != 0 {
		t.Errorf("the frame after a hide repainted %d with nothing changed: the count "+
			"above is not damage", again)
	}
}

// TestCollapsingCostsMoreThanHiding, MEASURED rather than assumed, and it
// is the second half of the hide/collapse distinction — this time in the
// currency the framework actually spends.
//
// Collapsing reflows the slot: the pane shrinks and its slot-mates grow,
// so every one of them changes bounds and every changed bound is a
// repaint. Hiding reflows nothing. If these two ever cost the same, one
// of them has stopped doing what it says.
func TestCollapsingCostsMoreThanHiding(t *testing.T) {
	ed, c := dockFixture(t)

	// Two panes share the LEFT slot, so collapsing one has a neighbour to
	// hand the space to. Without that the two operations legitimately
	// cost the same and this test would be measuring nothing.
	ex := pane(t, ed, "explorer")
	if got := dockSlot(pane(t, ed, "tools").slot.Get()); got != dockLeft {
		t.Fatalf("the fixture assumes TOOLS shares the LEFT slot with EXPLORER; it is in %s", slotName(got))
	}

	ed.dock.ToggleHidden(ex)
	_, hidden := c.Frame()
	settle(t, c)
	ed.dock.ToggleHidden(ex)
	settle(t, c)

	ed.dock.ToggleCollapsed(ex)
	_, collapsed := c.Frame()

	t.Logf("hide repainted %d, collapse repainted %d", hidden, collapsed)
	if hidden == 0 || collapsed == 0 {
		t.Fatal("one of the two operations repainted nothing; neither count means anything")
	}
	if collapsed <= hidden {
		t.Errorf("collapsing repainted %d and hiding repainted %d: collapse reflows the "+
			"slot and hide does not, so collapse must cost more. Equal counts mean the "+
			"two have become one operation", collapsed, hidden)
	}
}

// TestDockingMovesAPaneToAnotherSlot is drag-to-position, driven from the
// model the keyboard and the mouse both call.
func TestDockingMovesAPaneToAnotherSlot(t *testing.T) {
	ed, c := dockFixture(t)
	props := pane(t, ed, "properties")
	before := props.Bounds()

	ed.dock.SetActive(props)
	ed.dock.MoveActive(dockLeft)
	settle(t, c)

	after := props.Bounds()
	if after.X >= before.X {
		t.Errorf("docking PROPERTIES to the LEFT slot left it at x=%d, having started at "+
			"x=%d; it did not move to the other side", after.X, before.X)
	}
	if dockSlot(props.slot.Get()) != dockLeft {
		t.Errorf("the model says the pane is in %s after a move to Left", slotName(dockSlot(props.slot.Get())))
	}
	// And back, because a dock you cannot undock from is a one-way door.
	ed.dock.MoveActive(dockRight)
	settle(t, c)
	if got := props.Bounds(); got.X != before.X {
		t.Errorf("moving the pane back put it at x=%d, want %d", got.X, before.X)
	}
}

// TestADockMoveSchedulesAFrame exists because every other test in this
// file could not catch the bug it is about, and a mutation run is what
// showed that.
//
// Layout is OUTSIDE the property graph: an arranged rect subscribes to
// nothing, and a slot or order change moves only layout. So the dock host
// reads dock.rev while PAINTING, and that read is the only thing that
// turns a model edit into a scheduled frame.
//
// Deleting that read breaks the running app — a pane would move and
// nothing would ask for the frame that draws it — and every bounds test
// above still passed, because they call Composer.Frame() themselves.
// The harness was performing the very thing under test.
//
// So this asserts the INVALIDATION rather than the result: OnInvalidate
// is what App.Run listens to, and it firing is the whole claim.
func TestADockMoveSchedulesAFrame(t *testing.T) {
	ed, c := dockFixture(t)

	invalidated := 0
	c.OnInvalidate(func() { invalidated++ })

	// A baseline: with nothing changed, nothing is scheduled. Without
	// this the counter below could be measuring a composition that asks
	// for a frame on every tick.
	c.Frame()
	if invalidated != 0 {
		t.Fatalf("the idle composition scheduled %d frames; this test cannot attribute "+
			"anything to the dock", invalidated)
	}

	ed.dock.SetActive(pane(t, ed, "properties"))
	ed.dock.MoveActive(dockLeft)
	if invalidated == 0 {
		t.Error("docking a pane scheduled NO frame. The model changed and the screen did " +
			"not: layout is outside the property graph, so something inside a paint node " +
			"has to read the dock's revision or a move is invisible until the next " +
			"unrelated repaint")
	}

	// A pane that CAN move down. Reorder is a no-op at the end of a slot
	// and correctly schedules nothing there, so the target has to be
	// chosen rather than inherited from the move above — which left
	// PROPERTIES last in the left slot, and made this arm assert that a
	// legitimate no-op was a bug.
	c.Frame()
	settle(t, c)
	first := ed.dock.ByID("explorer")
	ed.dock.SetActive(first)
	mates := 0
	for _, p := range ed.dock.panes {
		if p.slot.Get() == first.slot.Get() {
			mates++
		}
	}
	if mates < 2 {
		t.Fatalf("EXPLORER has %d slot-mates; there is nothing to reorder past", mates-1)
	}
	invalidated = 0
	ed.dock.Reorder(1)
	if invalidated == 0 {
		t.Error("reordering a pane scheduled no frame")
	}

	invalidated = 0
	c.Frame()
	settle(t, c)
	invalidated = 0
	ed.dock.Resize(4)
	if invalidated == 0 {
		t.Error("resizing a pane scheduled no frame")
	}
}

// TestPinDecidesWhatHideUnpinnedSpares is what makes pin a THIRD
// operation rather than a second spelling of hide. Both arms are
// asserted: without the pinned arm this passes against an implementation
// where HideUnpinned hides everything.
func TestPinDecidesWhatHideUnpinnedSpares(t *testing.T) {
	ed, c := dockFixture(t)
	props := pane(t, ed, "properties") // Pinned by default
	tools := pane(t, ed, "tools")      // Declared Pinned="false"

	if !props.pinned.Get() {
		t.Fatal("PROPERTIES is not pinned; the sparing arm of this test would be vacuous")
	}
	if tools.pinned.Get() {
		t.Fatal("TOOLS is pinned; the hiding arm of this test would be vacuous")
	}

	ed.dock.HideUnpinned()
	settle(t, c)
	if !tools.hidden.Get() {
		t.Error("HideUnpinned left the UNPINNED pane showing")
	}
	if props.hidden.Get() {
		t.Error("HideUnpinned hid the PINNED pane; pin means nothing if it does not spare " +
			"the pane from exactly this")
	}

	// Pinning the other one makes it survive the same call — the
	// discrimination that proves pin is read, not just stored.
	ed.dock.ShowAll()
	ed.dock.TogglePinned(tools)
	ed.dock.HideUnpinned()
	settle(t, c)
	if tools.hidden.Get() {
		t.Error("after pinning it, TOOLS was still hidden by HideUnpinned; the pin flag " +
			"is being written and not read")
	}
}

// TestReorderMovesAPaneWithinItsSlot.
func TestReorderMovesAPaneWithinItsSlot(t *testing.T) {
	ed, c := dockFixture(t)
	ex, tools := pane(t, ed, "explorer"), pane(t, ed, "tools")
	if ex.Bounds().Y >= tools.Bounds().Y {
		t.Fatalf("the fixture assumes EXPLORER sits above TOOLS; they are at y=%d and y=%d",
			ex.Bounds().Y, tools.Bounds().Y)
	}
	ed.dock.SetActive(ex)
	ed.dock.Reorder(1)
	settle(t, c)
	if ex.Bounds().Y <= tools.Bounds().Y {
		t.Errorf("after reordering down, EXPLORER is at y=%d and TOOLS at y=%d; they did "+
			"not swap", ex.Bounds().Y, tools.Bounds().Y)
	}
}

// TestDraggingAPaneHeaderDocksIt exercises the MOUSE path — the one a pty
// transcript can never reach, which is precisely why it needs a Go test.
// The keyboard path has both.
func TestDraggingAPaneHeaderDocksIt(t *testing.T) {
	ed, c := dockFixture(t)
	props := pane(t, ed, "properties")
	b := props.Bounds()

	// Press the header, but NOT its first cell — that cell is the
	// collapse chevron and is a different gesture.
	if !c.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.ButtonLeft, X: b.X + 4, Y: b.Y}) {
		t.Fatal("a press on the pane header was not consumed")
	}
	settle(t, c)
	// Release over the LEFT slot.
	c.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, Button: input.ButtonLeft, X: 6, Y: 10})
	settle(t, c)

	if got := dockSlot(props.slot.Get()); got != dockLeft {
		t.Errorf("dragging the PROPERTIES header to the left edge docked it in %s", slotName(got))
	}
}

// TestTheChevronCellCollapses — the header's first cell is the collapse
// hit target, and it must not be the drag handle.
func TestTheChevronCellCollapses(t *testing.T) {
	ed, c := dockFixture(t)
	props := pane(t, ed, "properties")
	b := props.Bounds()
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, Button: input.ButtonLeft, X: b.X, Y: b.Y})
	settle(t, c)
	if !props.collapsed.Get() {
		t.Error("pressing the header's chevron cell did not collapse the pane")
	}
}

// TestCollapsingTheBottomStripGivesItsRowsBack is #431, and it is two
// failures wearing one symptom: "collapsing the bottom panel doesn't
// collapse everything, it just hides its contents."
//
// TestHideIsNotCollapse above covers the same operation on PROPERTIES and
// passes throughout, because PROPERTIES is in a VERTICAL slot — its
// height is its share of the stacking axis, which is where place() spent
// the header row. The bottom strip stacks sideways, and there the same
// arithmetic bought:
//
//   - a pane one COLUMN wide (headerH is a row count, applied to X), so
//     the header rendered as a bare chevron with its title clipped off;
//   - a strip that kept every row of its declared Size, because
//     slotExtent never looked at the collapsed flag at all.
//
// So the assertions are the two axes separately. The width one is what a
// test written from the doctrine sentence alone would not think to make.
func TestCollapsingTheBottomStripGivesItsRowsBack(t *testing.T) {
	ed, c := dockFixture(t)
	panel := pane(t, ed, "panel")
	editor := pane(t, ed, "editor")

	before, editorBefore := panel.Bounds(), editor.Bounds()
	if before.H <= headerH {
		t.Fatalf("the PANEL strip is %d rows before anything happened; this test "+
			"needs it open to have anything to reclaim", before.H)
	}

	ed.dock.ToggleCollapsed(panel)
	settle(t, c)
	got, editorAfter := panel.Bounds(), editor.Bounds()

	// THE ROWS COME BACK. This is the reported symptom, and it is a claim
	// about the pane ABOVE: reclaimed space nobody receives is the same
	// blank strip from the user's side.
	if got.H != headerH {
		t.Errorf("the collapsed strip is %d rows tall, want %d", got.H, headerH)
	}
	if grew := editorAfter.H - editorBefore.H; grew != before.H-headerH {
		t.Errorf("collapsing a %d-row strip gave the editor %d more rows, want %d — "+
			"the strip's height is its slot's extent, and a slot that ignores "+
			"collapse keeps every row of its declared Size", before.H, grew, before.H-headerH)
	}

	// AND THE WIDTH IS UNTOUCHED. headerH is a number of ROWS; a strip
	// that stacks sideways spends its stacking axis in COLUMNS, and
	// spending one row's worth there leaves a one-cell pane.
	if got.W != before.W {
		t.Errorf("the collapsed strip is %d columns wide and was %d; collapse is "+
			"a height, and a pane %d cells wide cannot show its own title",
			got.W, before.W, got.W)
	}
	// Which is the user-visible half, so assert it on the cell plane too
	// rather than trusting the rect: the title has to still be readable.
	f, _ := c.Frame()
	if row := rowText(f, got.Y, got.X, got.W); !strings.Contains(row, panel.Title) {
		t.Errorf("the collapsed header row reads %q and does not contain %q; the "+
			"pane has shrunk to its chevron", row, panel.Title)
	}
}

// TestEveryDockActionHasAKey is the pin for the rule the whole feature is
// shaped by: mouse reports cannot be injected through a recording pty, so
// a dock operation reachable only by pointer is one no test can perform
// and no transcript can show.
//
// It reads the SHIPPED page rather than a list written here, so adding a
// dock command without a binding fails this — which a hand-maintained
// list of command names could not do.
func TestEveryDockActionHasAKey(t *testing.T) {
	ed, _ := buildPage(t)
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}
	page := string(src)

	// The gestures the shipped page binds, read out of the page itself.
	bound := map[string]bool{}
	for _, m := range keyBindingRe.FindAllStringSubmatch(page, -1) {
		bound[m[1]] = true
	}
	if len(bound) == 0 {
		t.Fatal("the shipped page declares no KeyBindings at all; every assertion below " +
			"would be vacuous")
	}

	// Every command the dock and the region swap expose. The DOCK MODEL's
	// own surface, so a method that grows a command and no binding fails
	// here rather than shipping as a mouse-only gesture.
	for _, name := range []string{
		"DockLeft", "DockRight", "DockCenter", "DockBottom",
		"NextPane", "PrevPane", "PaneUp", "PaneDown", "Grow", "Shrink",
		"TogglePane", "CollapsePane", "PinPane", "HideUnpinned", "ShowAllPanes",
		"SwapRegion", "UseBuiltin", "UseEditor", "OpenFolder", "Save",
	} {
		if _, ok := ed.ctx.Values[name]; !ok {
			t.Errorf("%s is bound by the page but absent from the context", name)
			continue
		}
		if !bound[name] {
			t.Errorf("%s has no KeyBinding in the shipped page; a dock action reachable "+
				"only by mouse cannot be verified at all, because mouse reports do not "+
				"survive a recording pty", name)
		}
	}
}

// keyBindingRe pulls the bound command name out of every root
// <KeyBinding>. Matching the page text rather than the built tree is
// deliberate: what a reader has to keep true is the MARKUP, and a
// binding that resolved but was declared somewhere it never fires would
// pass a tree walk.
var keyBindingRe = regexp.MustCompile(`<KeyBinding [^>]*Command="\{\{\.([A-Za-z0-9_]+)\}\}"`)

// TestAnUnknownSlotIsALoadError — everything resolvable fails at LOAD,
// never as a surprise on the fourth gesture.
func TestAnUnknownSlotIsALoadError(t *testing.T) {
	ed, _ := buildPage(t)
	bad := `<Gooey><DockHost><DockPane Id="x" Slot="Lft"><Text>hi</Text></DockPane></DockHost></Gooey>`
	_, err := markup.Build([]byte(bad), ed.ctx)
	if err == nil {
		t.Fatal(`<DockPane Slot="Lft"> loaded; a misspelled slot must not silently mean Left`)
	}
	if !strings.Contains(err.Error(), "Lft") {
		t.Errorf("the load error does not quote the offending value: %v", err)
	}
}

// TestADockPaneNeedsAnId — an unnamed pane is one no menu item and no
// command can address.
func TestADockPaneNeedsAnId(t *testing.T) {
	ed, _ := buildPage(t)
	bad := `<Gooey><DockHost><DockPane Slot="Left"><Text>hi</Text></DockPane></DockHost></Gooey>`
	if _, err := markup.Build([]byte(bad), ed.ctx); err == nil {
		t.Fatal("<DockPane> without an Id loaded")
	}
}

// A same-slot drop takes dockModel.Move's guarded path, and that branch
// has to do two opposite things at once — which is why it gets its own
// test rather than another arm on the one above.
//
// It must NOT reorder. Releasing a drag where it started is the gesture
// a user makes to cancel one, and without the guard the reorder below it
// would find the highest order among the pane's slot-mates and put it
// after them: the pane would visibly jump to the bottom of its own slot
// for doing nothing.
//
// It must STILL schedule a frame. dockHost.Render reads dock.rev and
// paints the drag indicator from h.drag, and the release path clears
// h.drag with a plain field write — outside the property graph, so it
// schedules nothing on its own. Move's touch() is the only thing that
// asks for the frame that ERASES "move EXPLORER → left". Make this
// branch a true no-op and that text stays on screen until something
// unrelated repaints.
//
// OnInvalidate is the instrument, not a damage count: the failure being
// guarded is a MISSING SCHEDULE, and a count of repainted components
// cannot see one — it reports zero and looks exactly like a correct
// no-op. The test above says the same thing about the read in Render.
//
// EXPLORER is the subject because it is FIRST in the left slot, sharing
// it with TOOLS. A pane already last in its slot would be unmoved by the
// reorder anyway, so the no-reorder half would pass against a Move with
// the guard deleted — an unfireable assertion, which is the trap the
// reorder arm above documents.
func TestASameSlotDropDoesNotReorderButStillSchedulesAFrame(t *testing.T) {
	ed, c := dockFixture(t)

	explorer := pane(t, ed, "explorer")
	tools := pane(t, ed, "tools")
	if dockSlot(explorer.slot.Get()) != dockSlot(tools.slot.Get()) {
		t.Fatal("EXPLORER and TOOLS no longer share a slot; this test cannot tell a " +
			"guarded no-op from a reorder that had nothing to move")
	}
	if explorer.order.Get() >= tools.order.Get() {
		t.Fatalf("EXPLORER (order %d) is not ahead of TOOLS (order %d), so a missing "+
			"guard would not move it and this test would pass vacuously",
			explorer.order.Get(), tools.order.Get())
	}
	before := explorer.order.Get()

	invalidated := 0
	c.OnInvalidate(func() { invalidated++ })
	c.Frame()
	if invalidated != 0 {
		t.Fatalf("the idle composition scheduled %d frames; this test cannot attribute "+
			"anything to the drop", invalidated)
	}

	ed.dock.Move(explorer, dockSlot(explorer.slot.Get()))

	if got := explorer.order.Get(); got != before {
		t.Errorf("dropping EXPLORER into the slot it is already in moved it from order %d "+
			"to %d. Releasing a drag where it started must not reorder — that is what the "+
			"early return in dockModel.Move is for", before, got)
	}
	if invalidated == 0 {
		t.Error("a same-slot drop scheduled NO frame. dockHost.Render paints the drag " +
			"indicator from h.drag, which the release path clears with a plain field " +
			"write — outside the property graph, so it schedules nothing. Move's touch() " +
			"is the only thing that asks for the frame that erases the indicator, and " +
			"without it the drag text stays on screen after the drop")
	}
}
