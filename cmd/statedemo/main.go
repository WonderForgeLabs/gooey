// statedemo: click a button, get the app serialized as JSON into a
// text box. The UI is pure markup — built-in widgets only, no custom
// widgets, no UserControl setup funcs — so every delegate lives here in
// the viewmodel, which is the "no code-behind" contract.
//
// Serialization is viewmodel-side and explicit: properties are typed
// handles, so the snapshot Get()s each one into a plain struct for
// encoding/json. (Generating this snapshot from x:Property
// declarations is exactly the wire-schema job gooey gen picks up
// later.)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

var messages = []string{"hello, gooey", "state is properties", "the graph is the app", "serialize me"}

// checkbox: a focus stop rendering "[x] label", toggled by space/enter
// or a mouse click (the synthesized MouseClick — press+release on this
// widget). Checked state lives in a bound property, so checking it is
// an ordinary Set that the rest of the graph reacts to.
type checkbox struct {
	gooey.Base
	gooey.FocusState
	checked *prop.Property[bool]
	label   string
}

func (c *checkbox) Measure(avail gooey.Size) gooey.Size {
	return gooey.Size{W: min(4+len(c.label), avail.W), H: 1}
}

func (c *checkbox) Render(f *gooey.Frame) {
	b := c.Bounds()
	box := "[ ] "
	if c.checked.Get() {
		box = "[x] "
	}
	st := render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	if c.IsFocused() {
		st.Reverse = true
	}
	f.Cells.SetString(b.X, b.Y, box, st)
	f.Cells.SetString(b.X+4, b.Y, c.label, render.Style{})
}

func (c *checkbox) toggle() { c.checked.Set(!c.checked.Get()) }

func (c *checkbox) HandleKey(ev input.KeyEvent) bool {
	if ev == input.Named(input.KeyEnter) || ev == input.Rune(' ') {
		c.toggle()
		return true
	}
	return false
}

func (c *checkbox) HandleMouse(ev input.MouseEvent) bool {
	if ev.Kind == input.MouseClick {
		c.toggle()
		return true
	}
	return false
}

func main() {
	// --- viewmodel ---
	count := prop.NewSource(0)
	msgIdx := prop.NewSource(0)
	jsonOut := prop.NewSource("press [ serialize → json ]")
	serializedAt := prop.NewSource("")
	// auto is bound to the checkbox: manual (command-driven snapshots)
	// vs auto (the JSON is a computed and re-serializes reactively).
	auto := prop.NewSource(false)

	live := prop.NewComputed(func() string {
		return fmt.Sprintf("live state:  count=%d  message=%q%s",
			count.Get(), messages[msgIdx.Get()%len(messages)], serializedAt.Get())
	})

	running := true
	frames, lastPainted := 0, 0
	var comp *gooey.Composer

	// snapshot builds the ONE canonical serialization of the app —
	// both modes call it, so identical state always yields identical
	// JSON; the checkbox changes only WHEN it runs, never WHAT it says.
	//
	// Snapshot is by explicit Get(), one field per property: there is
	// no reflective "walk all state" in gooey — state IS the set of
	// property handles, and the graph has no global registry to
	// enumerate (a deliberate consequence of the no-reflection rule;
	// x:Property declarations will let gooey gen emit this function).
	//
	// Whether those Gets are subscriptions is decided entirely by the
	// CALL SITE: invoked from the serialize command (outside any
	// evaluation) they record nothing; invoked from the autoJson
	// computed they become its dependencies. Same function, opposite
	// semantics.
	snapshot := func() map[string]any {
		return map[string]any{
			// The app's own state: the two sources the buttons mutate.
			// These are the only *property* reads — in auto mode, the
			// only things whose changes re-serialize.
			"viewmodel": map[string]any{
				"count":   count.Get(),
				"message": messages[msgIdx.Get()%len(messages)],
			},
			// The framework observing itself via the Composer. These
			// read plain vars and focus state, NOT properties — so even
			// in auto mode they never trigger re-serialization; they
			// refresh as-of whenever a tracked dependency fires.
			"framework": map[string]any{
				"frames":           frames,      // frames flushed since start
				"paintedLastFrame": lastPainted, // damage: widgets repainted last frame, not the whole tree
				// Focus/hover are live Widget references; %T prints
				// their concrete types ("*gooey.Button", "<nil>") since
				// widgets have no serializable identity yet — Name=
				// would be the stable key when that matters.
				"focused":       fmt.Sprintf("%T", comp.Focus().Focused()),
				"hovered":       fmt.Sprintf("%T", comp.Focus().Hovered()),
				"focusOrderLen": len(comp.Focus().Order()), // tab stops discovered in the tree
			},
		}
	}

	marshal := func() string {
		b, err := json.MarshalIndent(snapshot(), "", "  ")
		if err != nil {
			return "marshal error: " + err.Error()
		}
		return string(b)
	}

	// serialize is the Command behind [ serialize → json ] and the `s`
	// KeyBinding. Commands dispatch on the UI goroutine, so property
	// access needs no synchronization. Setting the output property is
	// the entire UI update: the Text bound to {{.Json}} repaints next
	// frame — no "refresh the textbox" call exists anywhere.
	serialize := func() {
		jsonOut.Set(marshal())
		serializedAt.Set("   (serialized " + time.Now().Format("15:04:05") + ")")
	}

	// The reactive path: the SAME marshal, but called inside a
	// computed evaluation — so the viewmodel Gets inside snapshot()
	// are now subscriptions, and any count/msgIdx mutation dirties
	// this node and re-serializes on the next frame, no command
	// involved.
	autoJson := prop.NewComputed(marshal)

	// display feeds the text box and is the conditional-dependency
	// demo from prop's oldest test, now on screen: while auto is
	// checked, jsonOut is not even a dependency (manual serializes are
	// invisible); unchecked, autoJson drops out of the graph and stops
	// evaluating. The checkbox toggle rewires the graph.
	display := prop.NewComputed(func() string {
		if auto.Get() {
			return autoJson.Get()
		}
		return jsonOut.Get()
	})

	ctx := &markup.Context{
		Values: map[string]any{
			"Live": live, "Json": display, "Auto": auto,
			"Increment": gooey.Command(func() { count.Set(count.Get() + 1) }),
			"Cycle":     gooey.Command(func() { msgIdx.Set(msgIdx.Get() + 1) }),
			"Serialize": gooey.Command(serialize),
			"Quit":      gooey.Command(func() { running = false }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		Widgets: map[string]markup.Builder{
			// Demo-local widget pending promotion to a built-in (the
			// input agent owns widgets.go right now). Checked binds a
			// bool property two-way: render reads it, toggle Sets it.
			"Checkbox": func(e markup.Element, c *markup.Context) (gooey.Widget, error) {
				v, err := c.BindingValue(e.Attrs["Checked"])
				if err != nil {
					return nil, err
				}
				checked, ok := v.(*prop.Property[bool])
				if !ok {
					return nil, fmt.Errorf("Checkbox Checked: got %T, want *prop.Property[bool]", v)
				}
				return &checkbox{checked: checked, label: e.Attrs["Label"]}, nil
			},
		},
	}

	dir := "cmd/statedemo"
	if _, err := os.Stat(filepath.Join(dir, "statedemo.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}
	fsys := os.DirFS(dir)
	tree, err := markup.Load(fsys, "statedemo.gooey", ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	screen, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "no tty:", err)
		os.Exit(1)
	}
	cols, rows := screen.Size()

	needsFrame := true
	attach := func(w gooey.Widget) {
		comp = gooey.NewComposer(w, cols, rows)
		comp.OnInvalidate(func() { needsFrame = true })
		needsFrame = true
	}
	attach(tree)
	swaps := make(chan gooey.Widget, 1)
	stopWatch := markup.Watch(fsys, "statedemo.gooey", ctx, func(w gooey.Widget) { swaps <- w })
	defer stopWatch()

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer screen.Restore()
	screen.EnableMouse()

	events := make(chan input.Event, 16)
	go term.DecodeEvents(screen, events)

	for running {
		if needsFrame {
			frames++
			_, lastPainted = comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case w := <-swaps:
			attach(w)
		case ev := <-events:
			// Arrows walk the focus stops: the framework moves focus
			// only when nothing in the tree consumed the arrow, so
			// arrow-driven widgets keep their own handling.
			comp.Handle(ev)
		}
	}
}
