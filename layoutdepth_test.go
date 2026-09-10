package gooey

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/render"
)

// depthbox is a container with exactly one child, measured and arranged
// the way every container in the framework does it — through
// MeasureChild/ArrangeChild, never through child.Measure. It is the
// smallest thing that can express both a legal deep tree and a cycle.
type depthbox struct {
	Base
	kid Component
}

func (b *depthbox) ChildComponents() []Component {
	if b.kid == nil {
		return nil
	}
	return []Component{b.kid}
}
func (b *depthbox) Render(*Frame) {}
func (b *depthbox) Measure(avail Size) Size {
	if b.kid == nil {
		return Size{1, 1}
	}
	return MeasureChild(b.kid, avail)
}
func (b *depthbox) Arrange(r Rect) {
	b.Base.Arrange(r)
	if b.kid != nil {
		ArrangeChild(b.kid, r)
	}
}

// chain builds a tree whose deepest MeasureChild call sits at depth
// levels: the root is measured directly (depth 0) and each link below it
// costs one MeasureChild.
func chain(levels int) *depthbox {
	leaf := &depthbox{}
	cur := leaf
	for i := 0; i < levels; i++ {
		cur = &depthbox{kid: cur}
	}
	return cur
}

// selfCycle is the construction that used to kill the process: a
// container that is its own child. Before MaxLayoutDepth existed, both
// tests below died with
//
//	runtime: goroutine stack exceeds 1000000000-byte limit
//	fatal error: stack overflow
//
// naming nothing but an endless MeasureChild/Measure alternation — the
// one failure mode this framework cannot report through its own error
// path, because a fatal error skips Screen.Restore and takes the
// terminal's modes and the user's unsaved work with it.
func selfCycle() *depthbox {
	b := &depthbox{}
	b.kid = b
	return b
}

func TestMeasureCycleReportsInsteadOfCrashing(t *testing.T) {
	TakeLayoutFault()
	got := selfCycle().Measure(Size{80, 24})
	if got != (Size{}) {
		t.Fatalf("a capped subtree measured %v, want the zero size", got)
	}
	f := TakeLayoutFault()
	if f == nil {
		t.Fatal("no LayoutFault recorded; the cap fired silently")
	}
	if f.Phase != "Measure" {
		t.Errorf("fault phase %q, want Measure", f.Phase)
	}
	if f.Depth != MaxLayoutDepth+1 {
		t.Errorf("fault depth %d, want %d (the level it refused)", f.Depth, MaxLayoutDepth+1)
	}
}

func TestArrangeCycleReportsInsteadOfCrashing(t *testing.T) {
	TakeLayoutFault()
	selfCycle().Arrange(Rect{0, 0, 80, 24})
	f := TakeLayoutFault()
	if f == nil {
		t.Fatal("no LayoutFault recorded; Arrange still recurses unbounded")
	}
	if f.Phase != "Arrange" {
		t.Errorf("fault phase %q, want Arrange", f.Phase)
	}
}

// The whole value of the cap is turning an unactionable crash into a
// diagnosis, so the message has to name the component, not just the
// depth.
func TestLayoutFaultNamesTheComponent(t *testing.T) {
	TakeLayoutFault()
	selfCycle().Measure(Size{80, 24})
	f := TakeLayoutFault()
	if f == nil {
		t.Fatal("no fault recorded")
	}
	if f.At == nil {
		t.Fatal("fault names no component")
	}
	if _, ok := f.At.(*depthbox); !ok {
		t.Errorf("fault names %T, want the container that recursed", f.At)
	}
	if msg := f.Error(); !strings.Contains(msg, "*gooey.depthbox") {
		t.Errorf("message does not name the type: %s", msg)
	}
}

// The cap must not reject a document that works today. A tree exactly at
// the bound lays out; one level past it does not. Anything looser than
// this pins nothing — a cap of 3 would pass a test that only checked the
// cycle.
func TestExactlyMaxDepthLaysOutAndOneMoreFaults(t *testing.T) {
	TakeLayoutFault()
	if got := chain(MaxLayoutDepth).Measure(Size{80, 24}); got != (Size{1, 1}) {
		t.Fatalf("a tree exactly %d deep measured %v, want the leaf's {1 1}", MaxLayoutDepth, got)
	}
	if f := TakeLayoutFault(); f != nil {
		t.Fatalf("a legal tree faulted: %v", f)
	}

	if got := chain(MaxLayoutDepth + 1).Measure(Size{80, 24}); got != (Size{}) {
		t.Fatalf("a tree %d deep measured %v, want the zero size", MaxLayoutDepth+1, got)
	}
	if TakeLayoutFault() == nil {
		t.Fatalf("a tree %d deep did not fault", MaxLayoutDepth+1)
	}
}

// The depth counter is shared across passes, so a pass that trips the
// cap must still leave it at zero — otherwise the NEXT frame inherits
// the depth and faults on a tree that is perfectly legal.
func TestDepthUnwindsAfterAFault(t *testing.T) {
	TakeLayoutFault()
	selfCycle().Measure(Size{80, 24})
	TakeLayoutFault()

	if got := chain(4).Measure(Size{80, 24}); got != (Size{1, 1}) {
		t.Fatalf("a 4-deep tree after a fault measured %v, want {1 1}", got)
	}
	if f := TakeLayoutFault(); f != nil {
		t.Fatalf("a shallow tree faulted after an earlier cycle: %v", f)
	}
}

// A runaway names its first refusal, not its millionth: keeping the
// first is what stops a cycle allocating one record per level.
func TestFaultKeepsTheFirstNotTheLast(t *testing.T) {
	TakeLayoutFault()
	first := &depthbox{}
	first.kid = first
	first.Measure(Size{80, 24})
	f := TakeLayoutFault()
	if f == nil {
		t.Fatal("no fault")
	}
	if f.At != Component(first) {
		t.Errorf("fault names %p, want the cycling container %p", f.At, first)
	}
	if TakeLayoutFault() != nil {
		t.Error("taking the fault did not clear it")
	}
}

// The Composer is where an app actually meets this: a cyclic tree must
// produce a frame and a readable fault, not a dead process.
//
// Before this change the line below did not fail, it HUNG — and then
// died in a walk the issue never mentioned. Composer.build allocates a
// paint node and three property nodes per level, so the cycle grew the
// heap instead of the stack, and when that was fixed the very same
// construction died in FocusManager.walk with "fatal error: stack
// overflow" inside m.parent's own mapassign. Capping MeasureChild alone
// would have left both.
func TestComposerSurvivesACycleAndReportsIt(t *testing.T) {
	TakeLayoutFault()
	c := NewComposer(selfCycle(), 20, 4)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("cyclic tree painted %d, want 1 (the root, whose subtree was not walked)", painted)
	}
	f := c.LayoutFault()
	if f == nil {
		t.Fatal("Composer reported no LayoutFault for a cyclic tree")
	}
	// Compose is the FIRST walk to meet the cycle, and the Composer keeps
	// the first. Asserting the phase — not merely that something faulted
	// — is what pins that ordering; "latest wins" reported Focus here.
	if f.Phase != "Compose" {
		t.Errorf("fault phase %q, want Compose (the earliest walk, which is the one kept)", f.Phase)
	}
	if !strings.Contains(f.Error(), "*gooey.depthbox") {
		t.Errorf("fault message does not name the component: %s", f.Error())
	}
}

// The walks that a cycle reaches are not one walk. Each of these died on
// its own before this change, in its own way, and each is reachable by an
// ordinary frame or an ordinary mouse event — so each is pinned
// separately. A single "the composer survives" test would pass with any
// one of them still unbounded.
func TestEveryTreeWalkRefusesACycle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phase string
		run   func(root *depthbox)
	}{
		{"Measure", "Measure", func(r *depthbox) { r.Measure(Size{80, 24}) }},
		{"Arrange", "Arrange", func(r *depthbox) { r.Arrange(Rect{0, 0, 80, 24}) }},
		{"Render", "Render", func(r *depthbox) {
			// Compose measures and arranges before it renders, and both
			// fault first; the taker keeps the earliest, so those are
			// drained and the render walk is left to record its own.
			r.Measure(Size{8, 2})
			r.Arrange(Rect{0, 0, 8, 2})
			TakeLayoutFault()
			renderTree(r, &Frame{Cells: render.NewBuffer(8, 2)}, 0)
		}},
		{"HitTest", "HitTest", func(r *depthbox) {
			r.Base.Arrange(Rect{0, 0, 8, 2})
			m := NewFocusManager(r)
			TakeLayoutFault() // NewFocusManager walks; Focus is not under test
			m.HitTest(1, 1)
		}},
		// Called through focusTargetFor on a click. Reached directly
		// because the route to it needs a focus manager already built
		// over the cyclic tree, and building one records a Focus fault
		// that would mask this one.
		{"Focusable", "Focusable", func(r *depthbox) { firstFocusable(r, 0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			TakeLayoutFault()
			tc.run(selfCycle())
			f := TakeLayoutFault()
			if f == nil {
				t.Fatalf("the %s walk did not refuse a cyclic tree", tc.name)
			}
			if f.Phase != tc.phase {
				t.Errorf("fault phase %q, want %q", f.Phase, tc.phase)
			}
		})
	}
}

// forkbox is selfCycle's other shape, and the difference is the whole of
// the finding below: TWO children rather than one.
//
// depthbox has a single kid, so a cycle through it is a LINE. Every walk
// in this package unwinds it in MaxLayoutDepth steps whether or not it
// stops at the cap, because there is only ever one branch to take. That
// is why TestEveryTreeWalkRefusesACycle's HitTest arm passes against a
// walk with no bound on total work at all: the arm cannot express the
// case that costs anything.
type forkbox struct {
	Base
	kids []Component
}

func (b *forkbox) ChildComponents() []Component { return b.kids }
func (b *forkbox) Render(*Frame)                {}
func (b *forkbox) Measure(avail Size) Size      { return avail }

// forkCycle is a container that is its own child TWICE.
func forkCycle() *forkbox {
	b := &forkbox{}
	b.kids = []Component{b, b}
	return b
}

// TestHitTestOnABranchingCycleTerminates is finding 1 of the review of
// #478, and it is a bound on TOTAL WORK rather than on depth.
//
// Ranked hit-testing gave up the early return on a hit — an earlier
// sibling can out-rank a later one, so every subtree whose bounds contain
// the point has to be visited. That early return was also the only thing
// bounding total work, and nothing noticed: MaxLayoutDepth bounds DEPTH.
// On a branching cycle each level now visits both children instead of
// unwinding on the first hit, so the walk costs 2^MaxLayoutDepth visits.
//
// Measured: the base branch returned in 11ms and the ranked walk had not
// returned after 10 SECONDS. The `depth > MaxLayoutDepth` line was still
// there, still recording a fault, and still looked like the bound — which
// is the worst version of this, because the fault says "handled" and then
// the process hangs.
//
// REACHABILITY IS NARROW AND NOT ZERO. Composer.build makes a cyclic tree
// a load error, so a live composition cannot get here. FocusManager.HitTest
// is public and NewFocusManager(root) builds nothing, which is the path
// this test takes — and is exactly why noteLayoutFaultAt("HitTest", …) is
// in the function at all.
//
// THE TIMEOUT IS THE ASSERTION, and a goroutine is the only way to write
// it: on the bug the call does not return, so nothing after it runs. The
// budget is generous because it is not measuring speed — a correct walk
// finishes in microseconds and a broken one never finishes, so any
// threshold between those separates them.
func TestHitTestOnABranchingCycleTerminates(t *testing.T) {
	root := forkCycle()
	root.Base.Arrange(Rect{0, 0, 8, 2})
	m := NewFocusManager(root)
	TakeLayoutFault() // NewFocusManager walks; Focus is not under test

	done := make(chan Component, 1)
	go func() { done <- m.HitTest(1, 1) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("HitTest did not return on a container that is its own child " +
			"twice. MaxLayoutDepth bounds depth, and the ranked walk visits " +
			"every branch, so the cost is 2^MaxLayoutDepth visits rather than " +
			"MaxLayoutDepth — the cap fires, records a fault, and the walk " +
			"keeps going in the sibling")
	}

	f := TakeLayoutFault()
	if f == nil {
		t.Fatal("no LayoutFault recorded, so the walk terminated for some other " +
			"reason than refusing the cycle")
	}
	if f.Phase != "HitTest" {
		t.Errorf("fault phase %q, want HitTest", f.Phase)
	}
}

// TestABranchingTreeUnderTheCapIsFullyVisited is the arm that stops the
// fix above from being "stop early".
//
// Aborting the whole walk once the cap fires is only correct because a
// legal tree never fires it. A budget on total visits — the other obvious
// spelling — would truncate a WIDE legal tree instead, and every ordering
// assertion in this package would still pass, because truncation drops
// candidates rather than mis-comparing them. 2^12 is 4096 nodes, well past
// any visit budget somebody might think MaxLayoutDepth justifies, and
// every leaf still has to be reachable.
func TestABranchingTreeUnderTheCapIsFullyVisited(t *testing.T) {
	const levels = 12
	// A binary tree of legal depth, every node at the same bounds so the
	// walk cannot prune on the point.
	at := Rect{0, 0, 8, 2}
	var build func(d int) Component
	build = func(d int) Component {
		b := &forkbox{}
		b.Base.Arrange(at)
		if d == levels {
			return b
		}
		b.kids = []Component{build(d + 1), build(d + 1)}
		return b
	}
	root := build(0)
	m := NewFocusManager(root)
	TakeLayoutFault()

	if got := m.HitTest(1, 1); got == nil {
		t.Fatal("a legal binary tree of depth 12 hit nothing at all")
	}
	if f := TakeLayoutFault(); f != nil {
		t.Errorf("a legal tree recorded %v. The abort is for a tree that "+
			"exceeds MaxLayoutDepth, and a WIDE tree is not a deep one — if this "+
			"fires, the bound became a budget on total visits and a wide legal "+
			"tree is being truncated", f)
	}
}

// The cap is on the walk, so a tree that never gets near it must not pay
// for it — and must not be reported. This is the regression that would
// bite every app: a fault on a normal tree.
func TestOrdinaryTreeNeverFaults(t *testing.T) {
	TakeLayoutFault()
	c := NewComposer(chain(8), 20, 4)
	c.Frame()
	c.Frame()
	if f := c.LayoutFault(); f != nil {
		t.Fatalf("an 8-deep tree — deeper than anything in this repo — faulted: %v", f)
	}
}

// BenchmarkMeasureChildDepth7 pins the cost of the cap on the path every
// frame takes. Seven is not an arbitrary depth: it is the deepest
// MeasureChild recursion instrumentation found anywhere in this
// repository's test corpus, so this is the real shape, not a microcase.
func BenchmarkMeasureChildDepth7(b *testing.B) {
	root := chain(7)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		root.Measure(Size{80, 24})
	}
}
