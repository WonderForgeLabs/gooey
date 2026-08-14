package markup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
)

// A frozen subtree renders but does not act. The consequence with teeth
// is that nothing inside it STARTS, and <Companion> is why: its Start
// spawns a child process (components/companion.go), so a frozen tree that
// still started its Startables would launch a subprocess the moment
// somebody dropped one on a design canvas — a side effect outside the
// process, from an editor gesture, with no consent path and one that
// outlives the editor.

// frozenHost is a one-child container that freezes its subtree — the
// shape a design surface's COD takes. Registered as an element so a test
// can write it in markup.
type frozenHost struct {
	gooey.Base
	child gooey.Component
	// got records every mouse event routed HERE, which is how a test
	// asserts that an event aimed at a descendant arrived at the host
	// instead of being swallowed.
	got []input.MouseEvent
	// when, where set, is the property the host's answer is DERIVED from —
	// the shape a real design surface takes. Nil means the constant `true`
	// every test written before the flip was observable relies on, and it
	// is also the case that must keep costing nothing: a Frozen() that
	// reads no property records no dependency and is never woken.
	when *prop.Property[bool]
	// state, where set, is the same answer held in PLAIN GO STATE. It is
	// the documented limit of the mechanism made testable — nothing
	// subscribes to a field — and it exists so the negative case is pinned
	// rather than asserted in prose.
	state *bool
	// calls counts how often the framework has asked. A spurious re-sync
	// is otherwise invisible — it repaints nothing, moves no focus and
	// keeps every hover edge — so cost is the only thing left to measure.
	calls int
}

func (h *frozenHost) HandleMouse(ev input.MouseEvent) bool {
	h.got = append(h.got, ev)
	return true
}

func (h *frozenHost) Frozen() bool {
	h.calls++
	if h.when != nil {
		return h.when.Get()
	}
	if h.state != nil {
		return *h.state
	}
	return true
}
func (h *frozenHost) ChildComponents() []gooey.Component { return []gooey.Component{h.child} }
func (h *frozenHost) Measure(a gooey.Size) gooey.Size    { return gooey.MeasureChild(h.child, a) }
func (h *frozenHost) Arrange(b gooey.Rect)               { h.Base.Arrange(b); gooey.ArrangeChild(h.child, b) }
func (h *frozenHost) Render(*gooey.Frame)                {}
func (h *frozenHost) AcceptsFocus() bool                 { return true }

// sink is a leaf that records every mouse event it receives AND consumes
// it, so a test can tell "the descendant never got it" apart from "the
// descendant got it and the host saw it bubble past".
type sink struct {
	gooey.Base
	got []input.MouseEvent
}

func (s *sink) HandleMouse(ev input.MouseEvent) bool {
	s.got = append(s.got, ev)
	return true
}
func (s *sink) Measure(gooey.Size) gooey.Size { return gooey.Size{W: 6, H: 1} }
func (s *sink) Render(*gooey.Frame)           {}

// watcher is a HoverWatcher attachment that counts enter edges.
//
// It is a probe rather than a components.Tooltip on purpose. A Tooltip's
// delay is a Startable, so freezing stops it by a SECOND mechanism, and a
// tooltip that failed to pop up would not say which of the two held the
// door — the exact ambiguity that let two of these guards go unpinned.
type watcher struct {
	gooey.Base
	entered int
}

func (w *watcher) NonVisual() bool               { return true }
func (w *watcher) Interrupted()                  {}
func (w *watcher) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (w *watcher) Render(*gooey.Frame)           {}
func (w *watcher) PointerOver(over bool) {
	if over {
		w.entered++
	}
}

// withFrozen registers <Frozen> in ctx, building a frozenHost around its
// single child.
func withFrozen(ctx *Context) *Context {
	if ctx.Components == nil {
		ctx.Components = map[string]Builder{}
	}
	ctx.Components["Frozen"] = func(e Element, c *Context) (gooey.Component, error) {
		kids, attach, err := buildChildren(e, c)
		if err != nil {
			return nil, err
		}
		h := &frozenHost{}
		// When="{{.X}}" makes the host's answer DERIVED, through the same
		// binding resolution every other control uses — not a test-only
		// back door, so what the tests exercise is what an app writes.
		// Absent, the host is the constant-true one the older tests use.
		if raw := e.Attrs["When"]; raw != "" {
			v, err := c.BindingValue(raw)
			if err != nil {
				return nil, fmt.Errorf("markup: <Frozen When=%q>: %w", raw, err)
			}
			p, ok := v.(*prop.Property[bool])
			if !ok {
				return nil, fmt.Errorf("markup: <Frozen When=%q> is %T; need *prop.Property[bool]", raw, v)
			}
			h.when = p
		}
		// State="X" is the same fact read out of a plain Go bool — the
		// mechanism's documented blind spot, wired up so a test can watch it
		// stay blind.
		if name := e.Attrs["State"]; name != "" {
			b, ok := c.Values[name].(*bool)
			if !ok {
				return nil, fmt.Errorf("markup: <Frozen State=%q> is %T; need *bool", name, c.Values[name])
			}
			h.state = b
		}
		if len(kids) > 0 {
			h.child = kids[0]
		}
		for _, a := range attach {
			h.Attach(a)
		}
		return h, nil
	}
	ctx.Components["Sink"] = func(e Element, c *Context) (gooey.Component, error) {
		return &sink{}, nil
	}
	ctx.Components["Watch"] = func(e Element, c *Context) (gooey.Component, error) {
		return &watcher{}, nil
	}
	return ctx
}

// TestFreezingAComponentDoesNotSpawnItsProcess asserts on the PROCESS,
// not on len(c.startable).
//
// A count is satisfied by any refactor that moves the append somewhere
// else, and the thing being prevented is a subprocess — so the assertion
// has to be that the subprocess did not happen. The companion writes a
// file as the first thing it does; the test requires that file never to
// appear.
func TestFreezingAComponentDoesNotSpawnItsProcess(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	mark := filepath.Join(dir, "ran.pid")
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Frozen>
	      <VStack>
	        <Companion Name="worker" Path="sh" KillDelay="200ms">
	          <Companion.Args>
	            <Arg>-c</Arg>
	            <Arg>echo $$ > ` + mark + `; sleep 300</Arg>
	          </Companion.Args>
	        </Companion>
	        <Text>inside</Text>
	      </VStack>
	    </Frozen>
	  </VStack>
	</Gooey>`

	w, err := Build([]byte(src), withFrozen(&Context{}))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	comp := gooey.NewComposer(w, 20, 4)
	d := gooey.NewDispatcher()
	comp.Start(d)
	t.Cleanup(comp.Close)
	comp.Frame()

	// Generous relative to the unfrozen control below, which observes the
	// file in milliseconds.
	deadline := time.Now().Add(750 * time.Millisecond)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(mark); err == nil {
			t.Fatalf("a frozen <Companion> spawned its process (pid %s): "+
				"dropping one on a design canvas must not start it", b)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestTheSameCompanionUnfrozenDoesSpawn is the control. Without it the
// test above passes for a page whose companion could never have started
// — a wrong Path, a build that dropped the element, a harness that never
// composed a frame.
func TestTheSameCompanionUnfrozenDoesSpawn(t *testing.T) {
	needSh(t)
	dir := t.TempDir()
	mark := filepath.Join(dir, "ran.pid")
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Companion Name="worker" Path="sh" KillDelay="200ms">
	      <Companion.Args>
	        <Arg>-c</Arg>
	        <Arg>echo $$ > ` + mark + `; sleep 300</Arg>
	      </Companion.Args>
	    </Companion>
	    <Text>outside</Text>
	  </VStack>
	</Gooey>`
	comp, _, _ := companionPage(t, src, &Context{})
	comp.Frame()
	waitForFile(t, mark) // fails the test if it never appears
}
