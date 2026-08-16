// prefs: the three settings the browser wants — last source,
// keep-recording, auto-restart — persisted through gooey's settings
// store and bound straight into markup.
//
// The point of the demo is that there is nothing to see. The checkboxes
// are bound to *prop.Property[bool] handles, the source line to a
// *prop.Property[string], and none of the markup knows those handles
// came off disk. Toggle one and the setting is written; quit and
// relaunch and it is still set.
//
// Two things in here are worth reading rather than watching:
//
//   - countingProvider is a host-supplied Provider. It wraps the
//     file-backed one only to count writes and report them to the UI —
//     and because Save runs on the store's writer goroutine, it may not
//     touch the counter property directly. It posts, exactly as a Timer
//     posts its tick. That is the whole confinement rule, in ten lines,
//     in the layer where a host would actually get it wrong.
//
//   - the `writes` counter on screen is the honest measure of how often
//     a setting costs a disk write. Hammer `r` and `a` and watch it go
//     up once per keystroke; hold a key so several toggles land in one
//     dispatcher batch and watch several toggles cost one write; toggle
//     a setting and toggle it back within one batch and watch it cost
//     none at all.
//
// Auto-restart is present here only as a persisted flag, which is all
// it should ever be: the flag is a user preference, the dev-loop
// supervisor that reads it is a different thing living somewhere else.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/cmd/internal/demomain"
	strhandlers "github.com/WonderForgeLabs/gooey/handlers/str"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/settings"
)

// sources is the demo's stand-in for the browser's source list.
var sources = []string{"", "origin/main", "worktree:wysiwyg", "worktree:settings-store"}

// countingProvider is a host provider: it delegates to the file-backed
// one and reports each completed write to the UI.
//
// Load and Save run OFF the UI goroutine, so writes must never be Set
// from in here. post is the only legal route back to the graph.
type countingProvider struct {
	inner settings.Provider
	post  func(func())
	// n is the authority on the count and lives outside the graph,
	// because the last write of all happens during shutdown when there
	// is no loop left to drain a post. writes is the same number made
	// bindable, and it can only lag.
	n      atomic.Int64
	writes *prop.Property[int]
}

func (c *countingProvider) Load() ([]byte, error) { return c.inner.Load() }

func (c *countingProvider) Save(doc []byte) error {
	err := c.inner.Save(doc)
	if err == nil {
		n := int(c.n.Add(1))
		c.post(func() { c.writes.Set(n) })
	}
	return err
}

func main() {
	path := os.Getenv("GOOEY_SETTINGS")
	if path == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			gooey.Exit(err)
		}
		path = filepath.Join(dir, "gooey", "prefs.json")
	}

	var app *gooey.App
	writes := prop.NewSource(0)
	prov := &countingProvider{
		inner:  settings.File(path),
		post:   func(fn func()) { app.Post(fn) },
		writes: writes,
	}

	store, err := settings.Open(prov)
	if err != nil {
		gooey.Exit(err)
	}

	// The three settings, registered before Start — Start takes the
	// baseline the document is compared against, so every key has to
	// exist by then.
	lastSource, err := settings.Value(store, "browser.lastSource", "")
	if err != nil {
		gooey.Exit(err)
	}
	keepRecording, err := settings.Value(store, "browser.keepRecording", false)
	if err != nil {
		gooey.Exit(err)
	}
	autoRestart, err := settings.Value(store, "browser.autoRestartApp", false)
	if err != nil {
		gooey.Exit(err)
	}

	// problem carries its own separator because the page can only
	// concatenate: markup has no conditional, so "show this only when it
	// is non-empty" has to be baked into the value.
	problem := prop.NewSource("")
	store.OnError(func(err error) { problem.Set("        ! " + err.Error()) })

	// Everything below reads the setting handles the way it would read
	// any other property, and the page reads them directly: the source
	// line is `last source:  {{str:Default .LastSource ...}}` in
	// prefs.gooey, so its wording, its fallback and its spacing all
	// hot-reload with the file.
	//
	// writesText exists only because an interpolation takes string
	// handles: {{.Writes}} on a *prop.Property[int] is a load error, and
	// there is no str:Int to convert one.
	writesText := prop.NewComputed(func() string { return strconv.Itoa(writes.Get()) })

	cycle := func() {
		cur := lastSource.Get()
		next := 0
		for i, s := range sources {
			if s == cur {
				next = (i + 1) % len(sources)
			}
		}
		lastSource.Set(sources[next])
	}

	stop := func() {} // replaced once the store is started

	ctx := &markup.Context{
		Values: map[string]any{
			"LastSource":    lastSource,
			"KeepRecording": keepRecording,
			"AutoRestart":   autoRestart,
			// A plain string binds as well as a handle does: the document
			// path never changes, so it needs no property.
			"Path":          path,
			"Writes":        writesText,
			"Problem":       problem,
			"Cycle":         gooey.Command(cycle),
			"ToggleRecord":  gooey.Command(func() { keepRecording.Set(!keepRecording.Get()) }),
			"ToggleRestart": gooey.Command(func() { autoRestart.Set(!autoRestart.Get()) }),
			// Toggle-and-toggle-back inside ONE command is the cheap
			// demonstration that a Set which changes nothing changes
			// nothing on disk: two Sets, one batch, zero writes.
			"Nudge": gooey.Command(func() {
				keepRecording.Set(!keepRecording.Get())
				keepRecording.Set(!keepRecording.Get())
			}),
			"Reset": gooey.Command(func() {
				for _, k := range store.Keys() {
					store.Delete(k)
				}
			}),
			"Quit": gooey.Command(func() { stop(); app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	// The one line of Go behind `{{str:Default .LastSource `(none yet)`}}`.
	// Registration IS the grant: the page may read the namespaces this
	// host registered and no others, and an undeclared prefix fails the
	// load rather than rendering blank.
	markup.RegisterValues(strhandlers.URI, strhandlers.New())

	app = gooey.NewApp(markup.Page(demomain.MarkupFS("prefs", "prefs.gooey"), "prefs.gooey", ctx))
	// Start after the app exists: post is the store's only route back to
	// the graph, and stop is what guarantees the last change is written.
	stop = store.Start(app.Post)
	err = app.Run(context.Background())
	stop()
	if err != nil {
		gooey.Exit(err)
	}
	// A last word on stdout so a scripted run can assert the round trip
	// without a terminal.
	fmt.Println("settings written to " + path + "; writes=" + strconv.Itoa(int(prov.n.Load())))
}
