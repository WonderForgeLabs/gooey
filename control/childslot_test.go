package control

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// childSlot must PROPAGATE SetChild's refusal, not swallow it.
//
// gooey.ChildSetter documents the bool as a contract — "False means the
// index was out of range … and callers must treat it as a refusal rather
// than ignoring it" — and childSlot is the caller. It returned
// `func(gooey.Component)`, so the answer had nowhere to go: PatchMarkup
// would report success while the tree was unchanged, which is the silent
// class the whole patch path exists to remove.
//
// WHY A PURPOSE-BUILT CONTAINER. No container in this repo can currently
// reach the refusal — every one of them refuses only on an out-of-range
// index, and childSlot screens that before returning a setter at all. So
// the path has no in-tree producer, and the only way to fire it is to
// write a container that says no for a reason of its own. That is not
// contrivance: ChildSetter is an OPEN interface, an app registering its
// own container may refuse for any reason it likes, and the interface's
// text promises such a container that its refusal is honoured.

// leaf is the smallest thing that satisfies gooey.Component: Measure,
// Arrange and Render, nothing else. gooey.Base alone is not a Component
// — it carries the layout fields, not the three methods.
type leaf struct{ gooey.Base }

func (l *leaf) Measure(avail gooey.Size) gooey.Size { return gooey.Size{} }
func (l *leaf) Arrange(b gooey.Rect)                { l.Base.Arrange(b) }
func (l *leaf) Render(f *gooey.Frame)               {}

// pickyBox is a one-child container that refuses on demand.
type pickyBox struct {
	leaf
	kid    gooey.Component
	refuse bool
	asked  int
}

func (b *pickyBox) ChildComponents() []gooey.Component { return []gooey.Component{b.kid} }

func (b *pickyBox) SetChild(i int, w gooey.Component) bool {
	b.asked++
	if b.refuse || i != 0 {
		return false
	}
	b.kid = w
	return true
}

func TestChildSlotReportsAContainersRefusal(t *testing.T) {
	fresh := &leaf{}

	// The must-say-YES arm FIRST, so a setter that reported false for
	// everything could not pass this test.
	ok := &pickyBox{kid: &leaf{}}
	put := childSlot(ok, 0)
	if put == nil {
		t.Fatal("no setter for a container that implements ChildSetter at a valid index")
	}
	if !put(fresh) {
		t.Error("a container that accepted the write reported a refusal")
	}
	if ok.kid != gooey.Component(fresh) {
		t.Error("the accepted write did not land: the container's child is unchanged")
	}

	// The arm that must refuse.
	no := &pickyBox{kid: &leaf{}, refuse: true}
	put = childSlot(no, 0)
	if put == nil {
		t.Fatal("no setter for the refusing container; the refusal below would be untested")
	}
	if put(fresh) {
		t.Error("the container refused the write and childSlot reported success — " +
			"PatchMarkup would tell the caller a patch landed that is not on screen")
	}
	if no.asked == 0 {
		t.Error("SetChild was never called, so this test proves nothing about its return")
	}
}

// TestChildSlotRefusesBeforeAskingWhereItCan is the other half, and it is
// why the refusal above needs a purpose-built container: the two cheap
// cases are screened here, without consulting the container at all.
func TestChildSlotRefusesBeforeAskingWhereItCan(t *testing.T) {
	// Not a ChildSetter at all.
	if childSlot(&leaf{}, 0) != nil {
		t.Error("a component that does not implement ChildSetter got a setter")
	}
	// Out of range, both ends — screened without asking.
	b := &pickyBox{kid: &leaf{}}
	for _, i := range []int{-1, 1, 99} {
		if childSlot(b, i) != nil {
			t.Errorf("index %d is outside the container's one child but got a setter", i)
		}
	}
	if b.asked != 0 {
		t.Errorf("the container was consulted %d times for indices childSlot can "+
			"reject on its own", b.asked)
	}
}
