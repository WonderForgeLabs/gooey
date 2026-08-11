package gooey

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// dynBox is the smallest possible Dynamic container: it stacks whatever
// children it is given, one per row, and raises the structure hook when
// that set changes.
type dynBox struct {
	Base
	kids []Component
	hook func()
}

func (d *dynBox) SetStructureHook(fn func())   { d.hook = fn }
func (d *dynBox) ChildComponents() []Component { return d.kids }
func (d *dynBox) Measure(avail Size) Size      { return avail }
func (d *dynBox) Render(*Frame)                {}
func (d *dynBox) Arrange(b Rect) {
	d.Base.Arrange(b)
	for i, k := range d.kids {
		ArrangeChild(k, Rect{X: b.X, Y: b.Y + i, W: b.W, H: 1})
	}
}

func (d *dynBox) set(kids ...Component) {
	d.kids = kids
	if d.hook != nil {
		d.hook()
	}
}

func lbl(s string) *label { return &label{text: prop.NewSource(s)} }

func line(c *Composer, y int) string {
	b := c.Cells()
	var sb strings.Builder
	for x := 0; x < b.W; x++ {
		sb.WriteRune(b.At(x, y).Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

func TestStructuralChangePaintsNewChildrenInTheSameFrame(t *testing.T) {
	box := &dynBox{}
	c := NewComposer(box, 20, 4)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("empty box painted %d, want 1", painted)
	}

	box.set(lbl("one"), lbl("two"))
	_, painted := c.Frame()
	if painted != 2 {
		t.Fatalf("two new children painted %d nodes, want 2", painted)
	}
	if got := line(c, 0); got != "one" {
		t.Fatalf("row 0 = %q — a child realized this frame did not paint in it", got)
	}
	if got := line(c, 1); got != "two" {
		t.Fatalf("row 1 = %q", got)
	}
}

// Reuse is the whole reason the sync is a diff: a component that is
// still there keeps its node, and a clean node does not repaint just
// because a sibling arrived.
func TestStructuralChangeKeepsCleanNodesClean(t *testing.T) {
	kept := lbl("kept")
	box := &dynBox{}
	box.set(kept)
	c := NewComposer(box, 20, 4)
	c.Frame()

	box.set(kept, lbl("added"))
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("adding one sibling painted %d nodes, want 1 (only the new one)", painted)
	}
	if got := line(c, 0); got != "kept" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := line(c, 1); got != "added" {
		t.Fatalf("row 1 = %q", got)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}
}

func TestRemovedChildrenLeaveNoCellsBehind(t *testing.T) {
	a, b := lbl("alpha"), lbl("bravo")
	box := &dynBox{}
	box.set(a, b)
	c := NewComposer(box, 20, 4)
	c.Frame()

	box.set(a)
	c.Frame()
	if got := line(c, 0); got != "alpha" {
		t.Fatalf("row 0 = %q", got)
	}
	if got := line(c, 1); got != "" {
		t.Fatalf("row 1 = %q — a removed component's cells were not cleared", got)
	}
}

// The input tree must follow the paint tree, or a component realized
// after the composition was built can never be clicked or reached.
func TestFocusResyncPicksUpNewStopsAndKeepsFocus(t *testing.T) {
	first := &eater{label: *lbl("first")}
	box := &dynBox{}
	box.set(first)
	c := NewComposer(box, 20, 4)
	c.Frame()
	if c.Focus().Focused() != Component(first) {
		t.Fatal("the only focus stop did not receive focus")
	}

	second := &eater{label: *lbl("second")}
	box.set(first, second)
	c.Frame()
	if got := len(c.Focus().Order()); got != 2 {
		t.Fatalf("focus order has %d stops, want 2", got)
	}
	if c.Focus().Focused() != Component(first) {
		t.Fatal("focus moved when an unrelated component appeared")
	}
	c.Focus().FocusNext()
	if c.Focus().Focused() != Component(second) {
		t.Fatal("a newly realized focus stop is not reachable")
	}
	if !c.HandleKey(input.Rune('x')) || second.got != 1 {
		t.Fatalf("keys do not route to a realized component (got %d)", second.got)
	}
}

func TestFocusFallsBackWhenTheFocusedComponentVanishes(t *testing.T) {
	a := &eater{label: *lbl("a")}
	b := &eater{label: *lbl("b")}
	box := &dynBox{}
	box.set(a, b)
	c := NewComposer(box, 20, 4)
	c.Frame()
	c.Focus().SetFocus(b)

	box.set(a)
	c.Frame()
	if c.Focus().Focused() != Component(a) {
		t.Fatalf("focused = %v; a composition must always have somewhere for keys to land", c.Focus().Focused())
	}
}

// Anything with a lifetime inside a realized subtree gets the same
// treatment as one that existed at frame zero: started when it appears,
// stopped when it goes.
func TestStartablesInRealizedSubtreesStartAndStop(t *testing.T) {
	box := &dynBox{}
	c := NewComposer(box, 20, 4)
	d := NewDispatcher()
	c.Start(d)

	tk := &ticker{label: *lbl("tick")}
	box.set(tk)
	c.Frame()
	if tk.started != 1 {
		t.Fatalf("a Startable realized after Start ran %d times, want 1", tk.started)
	}

	box.set()
	c.Frame()
	if tk.stopped != 1 {
		t.Fatalf("a Startable removed from the tree was stopped %d times, want 1", tk.stopped)
	}

	c.Close()
	if tk.stopped != 1 {
		t.Fatalf("Close re-stopped an already-removed Startable (%d)", tk.stopped)
	}
}

// A structural re-sync STOPS WHAT LEFT BEFORE IT STARTS WHAT ARRIVED.
//
// The order is not cosmetic. A Startable that owns something outside the
// process — components.Companion owns a child process, a port and a log
// file it truncates on open — cannot be replaced by another declaring the
// same service if the two overlap. Starting first meant the incoming
// child raced the outgoing one for the whole of the outgoing element's
// stop, which for a Companion is bounded only by StopTimeout (10s).
//
// This is the discriminator for that: flip the two loops in walkNodes and
// the sequence below becomes start-new-then-stop-old.
func TestDepartedStartablesStopBeforeArrivalsStart(t *testing.T) {
	var log []string
	box := &dynBox{}
	c := NewComposer(box, 20, 4)
	c.Start(NewDispatcher())

	stayer := &lifecycle{label: *lbl("stay"), log: &log, name: "stayer"}
	outgoing := &lifecycle{label: *lbl("out"), log: &log, name: "outgoing"}
	box.set(stayer, outgoing)
	c.Frame()

	incoming := &lifecycle{label: *lbl("in"), log: &log, name: "incoming"}
	box.set(stayer, incoming)
	c.Frame()

	want := []string{"start stayer", "start outgoing", "stop outgoing", "start incoming"}
	if strings.Join(log, ",") != strings.Join(want, ",") {
		t.Errorf("lifecycle order was %v, want %v", log, want)
	}
}

// A survivor of a re-sync is neither stopped nor restarted, which is what
// makes the reorder safe: membership is pointer identity and the live set
// is complete before the first stop runs, so there is no component that
// both departs and re-appears in one pass.
func TestSurvivingStartablesAreNotCycledByARemoval(t *testing.T) {
	var log []string
	box := &dynBox{}
	c := NewComposer(box, 20, 4)
	c.Start(NewDispatcher())

	stayer := &lifecycle{label: *lbl("stay"), log: &log, name: "stayer"}
	going := &lifecycle{label: *lbl("go"), log: &log, name: "going"}
	box.set(stayer, going)
	c.Frame()
	log = nil

	box.set(stayer)
	c.Frame()
	if strings.Join(log, ",") != "stop going" {
		t.Errorf("removing a sibling produced %v, want only the departed one stopping", log)
	}
}

// lifecycle records start/stop against a shared log, so a test can assert
// the ORDER of a re-sync rather than only its counts.
type lifecycle struct {
	label
	log  *[]string
	name string
}

func (l *lifecycle) Start(func(func())) func() {
	*l.log = append(*l.log, "start "+l.name)
	return func() { *l.log = append(*l.log, "stop "+l.name) }
}

type ticker struct {
	label
	started, stopped int
}

func (t *ticker) Start(func(func())) func() {
	t.started++
	return func() { t.stopped++ }
}
