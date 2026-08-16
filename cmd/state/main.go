// state: click a button, get the app serialized as JSON into a
// text box. The UI is pure markup — built-in components only, no custom
// components, no UserControl setup funcs — so every delegate lives here in
// the viewmodel, which is the "no code-behind" contract.
//
// Serialization is viewmodel-side and explicit: properties are typed
// handles, so the snapshot Get()s each one into a plain struct for
// encoding/json. (Generating this snapshot from x:Property
// declarations is exactly the wire-schema job gooey gen picks up
// later.)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/cmd/internal/demomain"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

var messages = []string{"hello, gooey", "state is properties", "the graph is the app", "serialize me"}

func main() {
	// --- viewmodel ---
	count := prop.NewSource(0)
	msgIdx := prop.NewSource(0)
	jsonOut := prop.NewSource("press [ serialize → json ]")
	serializedAt := prop.NewSource("")
	// auto is bound to the checkbox: manual (command-driven snapshots)
	// vs auto (the JSON is a computed and re-serializes reactively).
	auto := prop.NewSource(false)
	// manual drives the serialize button's Visibility: in auto mode the
	// button is meaningless, so it collapses. Visibility="{{.Manual}}"
	// binds a bool (true→Visible, false→Collapsed); a computed works
	// because binding is by handle, and toggling the checkbox reflows
	// the button row with no code watching anything.
	manual := prop.NewComputed(func() bool { return !auto.Get() })

	live := prop.NewComputed(func() string {
		return fmt.Sprintf("live state:  count=%d  message=%q%s",
			count.Get(), messages[msgIdx.Get()%len(messages)], serializedAt.Get())
	})

	var app *gooey.App

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
				"frames":           app.Frames(),           // frames flushed since start
				"paintedLastFrame": app.PaintedLastFrame(), // damage: components repainted last frame, not the whole tree
				// Focus/hover are live Component references; %T prints
				// their concrete types ("*components.Button", "<nil>") since
				// components have no serializable identity yet — Name=
				// would be the stable key when that matters.
				"focused":       fmt.Sprintf("%T", app.Composer().Focus().Focused()),
				"hovered":       fmt.Sprintf("%T", app.Composer().Focus().Hovered()),
				"focusOrderLen": len(app.Composer().Focus().Order()), // tab stops discovered in the tree
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
			"Live": live, "Json": display, "Auto": auto, "Manual": manual,
			"Increment": gooey.Command(func() { count.Set(count.Get() + 1) }),
			"Cycle":     gooey.Command(func() { msgIdx.Set(msgIdx.Get() + 1) }),
			"Serialize": gooey.Command(serialize),
			"Quit":      gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	// The framework figures the app observes above — frames, damage,
	// focus — are the App's and the Composer's own counters, so this
	// demo no longer keeps a private copy of the run loop to derive them.
	app = gooey.NewApp(markup.Page(demomain.MarkupFS("state", "state.gooey"), "state.gooey", ctx))
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}
