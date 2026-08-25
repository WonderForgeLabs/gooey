package markup

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// watcherPage wraps a body in a page whose FS also holds the two paths
// the tests watch, so the fs.FS seam is exercised the way markup.Load
// exercises it rather than through Context.Includes.
func watcherPage(body string) fstest.MapFS {
	return fstest.MapFS{
		"page.gooey":  {Data: []byte("<Gooey>\n  <VStack>\n" + body + "\n  </VStack>\n</Gooey>")},
		"notes.md":    {Data: []byte("hello")},
		"assets/a.md": {Data: []byte("a")},
	}
}

func loadWatcher(t *testing.T, body string, values map[string]any) (*components.FileWatcher, error) {
	t.Helper()
	if values == nil {
		values = map[string]any{}
	}
	ctx := &Context{Values: values}
	w, err := Load(watcherPage(body), "page.gooey", ctx)
	if err != nil {
		return nil, err
	}
	a, ok := w.(gooey.Attacher)
	if !ok {
		t.Fatalf("the page root is %T, which hosts no attachments", w)
	}
	for _, at := range a.Attachments() {
		if fw, ok := at.(*components.FileWatcher); ok {
			return fw, nil
		}
	}
	t.Fatalf("no *components.FileWatcher among %d attachments — the element did not land as one", len(a.Attachments()))
	return nil, nil
}

// Non-visual like Timer: it lands on its parent as an attachment rather
// than as a child, and the Composer is what starts it.
func TestFileWatcherBuildsAsAnAttachmentWithThePageFS(t *testing.T) {
	reload := 0
	fw, err := loadWatcher(t,
		`    <FileWatcher Paths="notes.md" Changed="{{.Reload}}" Interval="900ms"/>`,
		map[string]any{"Reload": gooey.Command(func() { reload++ })})
	if err != nil {
		t.Fatal(err)
	}
	if fw.FS == nil {
		t.Fatal("the watcher got no fs.FS — literal paths would resolve against nothing")
	}
	if got, want := fw.Interval, 900*time.Millisecond; got != want {
		t.Errorf("Interval = %v, want %v", got, want)
	}
	if got := fw.Paths.Get(); len(got) != 1 || got[0] != "notes.md" {
		t.Errorf("Paths = %q, want [notes.md]", got)
	}
	if !gooey.CanExecute(fw.Changed) {
		t.Fatal("Changed did not bind")
	}
	fw.Changed.Run()
	if reload != 1 {
		t.Errorf("Changed ran the wrong delegate (%d calls)", reload)
	}
}

// Absent Interval is the framework's own hot-reload poll, not zero. The
// component treats zero as "use the default", so this pins the SURFACE:
// a page that omits Interval is not declaring a busy loop.
func TestFileWatcherIntervalDefaultsRatherThanRequiring(t *testing.T) {
	fw, err := loadWatcher(t, `    <FileWatcher Paths="notes.md" Changed="{{.Reload}}"/>`,
		map[string]any{"Reload": gooey.Command(func() {})})
	if err != nil {
		t.Fatal(err)
	}
	if fw.Interval != 0 {
		t.Errorf("Interval = %v, want the zero that means DefaultWatchInterval", fw.Interval)
	}
}

// The literal form: a pipe-separated list, which is what a fixed set of
// sources is and does not deserve a property in the viewmodel.
func TestFileWatcherLiteralPathsSplitOnPipe(t *testing.T) {
	fw, err := loadWatcher(t, `    <FileWatcher Paths="notes.md | assets" Changed="{{.Reload}}"/>`,
		map[string]any{"Reload": gooey.Command(func() {})})
	if err != nil {
		t.Fatal(err)
	}
	got := fw.Paths.Get()
	if len(got) != 2 || got[0] != "notes.md" || got[1] != "assets" {
		t.Fatalf("Paths = %q, want [notes.md assets] — split and trimmed", got)
	}
}

// The bound form shares the viewmodel's own handle, so a page can add a
// path without a rebuild.
func TestFileWatcherBoundPathsShareTheHandle(t *testing.T) {
	src := prop.NewSource([]string{"notes.md"})
	fw, err := loadWatcher(t, `    <FileWatcher Paths="{{.Sources}}" Changed="{{.Reload}}"/>`,
		map[string]any{"Sources": src, "Reload": gooey.Command(func() {})})
	if err != nil {
		t.Fatal(err)
	}
	src.Set([]string{"notes.md", "assets"})
	if got := fw.Paths.Get(); len(got) != 2 {
		t.Fatalf("Paths = %q — the binding is a copy, not the handle", got)
	}
}

// ZERO PATHS IS INERT. A bound list that resolves empty is a page under
// construction, and must load: it is what makes Paths="{{.MaybeEmpty}}"
// safe to write before the viewmodel has caught up.
func TestFileWatcherBoundEmptyPathsIsLegalAndInert(t *testing.T) {
	fw, err := loadWatcher(t, `    <FileWatcher Paths="{{.Sources}}" Changed="{{.Reload}}"/>`,
		map[string]any{"Sources": prop.NewSource([]string(nil)), "Reload": gooey.Command(func() {})})
	if err != nil {
		t.Fatalf("a watcher over an empty bound list failed to load: %v", err)
	}
	if got := fw.Paths.Get(); len(got) != 0 {
		t.Fatalf("Paths = %q, want empty", got)
	}
}

// Everything resolvable must fail at LOAD time, never as a surprise on
// the poll that finally runs. Each of these is a mistake the loader can
// see, and a watcher that accepted them would be silently deaf — which
// is the one failure mode a watcher may not have.
func TestFileWatcherLoadErrors(t *testing.T) {
	cases := []struct {
		name, body, want string
		values           map[string]any
	}{{
		name: "no Paths at all",
		body: `    <FileWatcher Changed="{{.Reload}}"/>`,
		want: "needs Paths",
	}, {
		name: "Paths written empty",
		body: `    <FileWatcher Paths="" Changed="{{.Reload}}"/>`,
		want: "needs Paths",
	}, {
		// An fs.FS path is slash-separated, unrooted, and has no "." or
		// ".." elements. A rooted path would fs.Stat as ErrInvalid
		// forever, which this component cannot tell from "not there
		// yet" — so it would poll a path that can never exist and
		// report nothing at all.
		name: "a rooted path, which no fs.FS can serve",
		body: `    <FileWatcher Paths="/etc/hosts" Changed="{{.Reload}}"/>`,
		want: "not a path in this file system",
	}, {
		name: "a path that climbs out of the FS",
		body: `    <FileWatcher Paths="../secrets" Changed="{{.Reload}}"/>`,
		want: "not a path in this file system",
	}, {
		name: "an unparseable Interval",
		body: `    <FileWatcher Paths="notes.md" Changed="{{.Reload}}" Interval="soon"/>`,
		want: "Interval",
	}, {
		name: "a zero Interval, which would be a busy loop",
		body: `    <FileWatcher Paths="notes.md" Changed="{{.Reload}}" Interval="0s"/>`,
		want: "must be positive",
	}, {
		name: "an unregistered handler name",
		body: `    <FileWatcher Paths="notes.md" Changed="OnReload"/>`,
		want: "no handler",
	}, {
		name:   "Paths bound to the wrong type",
		body:   `    <FileWatcher Paths="{{.Sources}}" Changed="{{.Reload}}"/>`,
		values: map[string]any{"Sources": prop.NewSource("notes.md")},
		want:   "need *prop.Property[[]string]",
	}, {
		name:   "Path bound to the wrong type",
		body:   `    <FileWatcher Paths="notes.md" Path="{{.Hit}}" Changed="{{.Reload}}"/>`,
		values: map[string]any{"Hit": prop.NewSource(0)},
		want:   "need *prop.Property[string]",
	}, {
		name:   "Enabled bound to the wrong type",
		body:   `    <FileWatcher Paths="notes.md" Enabled="{{.Live}}" Changed="{{.Reload}}"/>`,
		values: map[string]any{"Live": prop.NewSource("yes")},
		want:   "need *prop.Property[bool]",
	}, {
		name: "an attribute nothing reads",
		body: `    <FileWatcher Paths="notes.md" Changed="{{.Reload}}" Recursive="true"/>`,
		want: "Recursive",
	}, {
		// Non-visual elements have no bounds to place, so a layout
		// attribute is an error rather than a silent no-op.
		name: "a layout attribute on a non-visual element",
		body: `    <FileWatcher Paths="notes.md" Changed="{{.Reload}}" Width="4"/>`,
		want: "Width",
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			values := map[string]any{"Reload": gooey.Command(func() {})}
			for k, v := range c.values {
				values[k] = v
			}
			ctx := &Context{Values: values}
			_, err := Load(watcherPage(c.body), "page.gooey", ctx)
			if err == nil {
				t.Fatalf("loaded without error; want one mentioning %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %q, want it to mention %q", err, c.want)
			}
		})
	}
}

// The fs.FS seam, stated as a load error rather than as a silent no-op.
// A tree built from raw bytes has no filesystem, and a watcher that
// accepted one would poll nothing forever while looking correct.
func TestFileWatcherWithoutAnFSIsALoadError(t *testing.T) {
	ctx := &Context{Values: map[string]any{"Reload": gooey.Command(func() {})}}
	_, err := Build([]byte(`<Gooey><VStack><FileWatcher Paths="notes.md" Changed="{{.Reload}}"/></VStack></Gooey>`), ctx)
	if err == nil {
		t.Fatal("a watcher built from bytes loaded with no file system to watch")
	}
	if !strings.Contains(err.Error(), "no file system") {
		t.Fatalf("error = %q, want it to name the missing file system", err)
	}
}

// Context.Includes is the other half of that seam: a tree built from
// bytes but given an FS is legal, which is what keeps a UserControl's
// host able to declare one.
func TestFileWatcherAcceptsTheIncludesFS(t *testing.T) {
	ctx := &Context{
		Values:   map[string]any{"Reload": gooey.Command(func() {})},
		Includes: watcherPage(""),
	}
	if _, err := Build([]byte(`<Gooey><VStack><FileWatcher Paths="notes.md" Changed="{{.Reload}}"/></VStack></Gooey>`), ctx); err != nil {
		t.Fatalf("a watcher over Context.Includes failed to load: %v", err)
	}
}

// Path and Enabled are live handles, not copies — Enabled is what lets
// the graph pause the watcher with nothing torn down.
func TestFileWatcherPathAndEnabledAreLiveHandles(t *testing.T) {
	hit, live := prop.NewSource(""), prop.NewSource(true)
	fw, err := loadWatcher(t,
		`    <FileWatcher Paths="notes.md" Path="{{.Hit}}" Enabled="{{.Live}}" Changed="{{.Reload}}"/>`,
		map[string]any{"Hit": hit, "Live": live, "Reload": gooey.Command(func() {})})
	if err != nil {
		t.Fatal(err)
	}
	if fw.Path == nil || fw.Enabled == nil {
		t.Fatal("Path/Enabled bindings were dropped")
	}
	live.Set(false)
	if fw.Enabled.Get() {
		t.Error("Enabled is not a live handle onto the bound property")
	}
	fw.Path.Set("notes.md")
	if hit.Get() != "notes.md" {
		t.Error("Path is not the viewmodel's own handle")
	}
}

// A conditional resolves at the same seam every one-way bool attribute
// uses, with no per-element opt-in — so pausing a watcher on "not
// paused" needs no property of its own.
func TestFileWatcherEnabledAcceptsAConditional(t *testing.T) {
	paused := prop.NewSource(true)
	fw, err := loadWatcher(t,
		`    <FileWatcher Paths="notes.md" Enabled="{{not .Paused}}" Changed="{{.Reload}}"/>`,
		map[string]any{"Paused": paused, "Reload": gooey.Command(func() {})})
	if err != nil {
		t.Fatal(err)
	}
	if fw.Enabled.Get() {
		t.Fatal("Enabled = true while Paused is true")
	}
	paused.Set(false)
	if !fw.Enabled.Get() {
		t.Error("the conditional did not re-evaluate")
	}
}
