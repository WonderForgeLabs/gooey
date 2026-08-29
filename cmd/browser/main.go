// browser: the launcher for everything runnable in this repo. A
// directory-reader viewmodel data-binds to TWO roots through the fs.FS
// seam — cmd/ (the demos) and docs/learn/examples/ (the finished code
// for each Learn tutorial) — filtered by the same Go convention:
// directories exactly one level deep that contain a main.go. They are
// listed as two labeled groups. Picking one runs it via `go run` in a
// child process that takes over THIS terminal, which is one call to
// gooey.App.Suspend: the screen is restored, the input decoder is torn
// down so nothing of ours is still reading the tty, and everything is
// rebuilt when the child exits.
//
// Recording uses the same handoff: `r` wraps the demo in asciinema, so
// the terminal the demo drives is the one being captured, and agg turns
// the cast into a GIF afterwards.
//
// The preview pane shows what the directory itself says: a README.md
// when the demo has one, rendered as markdown (markdown.go), and the
// main.go doc comment otherwise. `p` plays the demo's recorded GIF in
// that same pane, decoded and coalesced into whole frames and animated
// by a clock the Composer owns (gifplay.go).
//
// None of it needs a restart. A <Timer> in the page polls a fingerprint
// of every directory the UI reads from (watch.go), so a recording
// finished in another terminal, a new demo, an added .gooey file or an
// edited doc comment all reach the list and the visible preview on
// their own.
//
// Two markup documents describe the screen. browser.gooey is the shell:
// the grid, the chrome, every key gesture, the poll, and the picker's
// place in z-order. infopane.gooey is the preview pane, a markup-only
// control instantiated with the handles it needs — the lines it shows,
// their order, and which of them collapse are all declared there. What
// is left in Go is data (the directory scan), algorithms (markdown,
// GIF coalescing, git), and the three leaves markup has no element for.
//
// The tree being browsed does not have to be the tree the browser was
// launched from. `b` opens a source picker (picker.go) listing the
// repository's worktrees and local branches (source.go); picking one
// re-resolves the demo list, previews, watching, exec and recording
// against that checkout — a branch with no worktree gets a throwaway
// detached one under the system temp dir, removed on switch-away and on
// exit. Recordings are the one thing that stays anchored to the LAUNCH
// tree: they are artifacts the user keeps, and an artifact written into
// an ephemeral checkout would be deleted with it.
//
//	j/k ↑/↓  select    enter  run    r  record    p  play GIF    b  sources    q/esc  quit
package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/cmd/internal/demomain"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

var (
	accent = render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	dim    = render.Style{Fg: render.RGB(140, 140, 150)}
)

// recDir is where casts and GIFs land, relative to the module root.
const recDir = "recordings"

// maxREADME bounds what the preview will take from a directory. A README
// is read on every rescan; something enormous parked in a demo directory
// is not worth stalling the poll for.
const maxREADME = 64 << 10

type demo struct {
	name    string // display name, unique within its group
	dir     string // source-relative directory: `go run ./<dir>`
	group   string // which root it came from
	rec     string // recording base name, unique across groups
	ownDir  bool   // run it from its own directory rather than the module root
	modDir  string // nested-module root to run from, "" for the repo module
	doc     string // leading comment block of main.go
	readme  string // README.md, when the directory has one — preferred over doc
	markup  int    // number of .gooey files
	cast    bool   // a recording already exists
	gif     bool
	gifPath string  // root-relative GIF `p` would play, "" when none
	gifDir  string  // the host root gifPath resolves under (launch or source)
	gifKey  fileKey // its identity, so a re-recording invalidates the decode
}

// scanEnv is the pair of roots a scan reads through. Demo content — the
// directories, doc comments, READMEs, .gooey files and checked-in GIFs —
// comes from the SELECTED SOURCE; recordings come from the LAUNCH tree,
// because `r` writes them there no matter which source is active (an
// artifact recorded into an ephemeral worktree would vanish with it).
// For the launch source the two halves are the same tree and this
// collapses to what the browser always did.
type scanEnv struct {
	src     fs.FS  // the selected source's checkout
	srcRoot string // its host root, for running and for playing its GIFs
	rec     fs.FS  // the launch tree, where recordings live
	recRoot string
}

func scanEnvFor(srcRoot, launchRoot string) scanEnv {
	return scanEnv{src: os.DirFS(srcRoot), srcRoot: srcRoot,
		rec: os.DirFS(launchRoot), recRoot: launchRoot}
}

// roots are the two places a runnable gooey program lives: the demos,
// and the finished code for each Learn tutorial. They are listed rather
// than discovered because the ORDER is the presentation — demos first,
// examples second — and because a recursive walk of the module would
// also find things that are not meant to be launched.
//
// Recordings are named with the prefix, not the display name: `cmd/pixels`
// and a future `docs/learn/examples/pixels` would otherwise write to the
// same recordings/pixels.cast.
//
// ownDir is where the two roots genuinely differ. The demos locate their
// markup relative to the MODULE ROOT (with a fallback beside the
// executable), so they must run from there. The tutorial examples call
// os.DirFS(".") and are documented as `cd <dir> && go run .` — that
// simplicity is the point of the tutorial, so the launcher matches it
// and runs them from their own directory. Getting this backwards fails
// the same way in both directions: `open page.gooey: no such file`.
var roots = []struct {
	path, group, prefix string
	ownDir              bool
	// modDir is a nested Go module's root, relative to the repo root.
	// Entries under it cannot be `go run` from the repo root — the
	// nested module is excluded from the parent — so the launcher runs
	// them from modDir with a package path relative to it. That is why
	// the temporal demos were invisible here: right convention, wrong
	// module.
	modDir string
}{
	{path: "cmd", group: "demos"},
	// The temporal demos are demos like any other — they just live in
	// the nested module, so they carry a modDir. Listing them under the
	// same group keeps the list smooth; the info pane's command line is
	// what tells the full story (cd handlers/temporal && go run …).
	{path: "handlers/temporal/cmd", group: "demos", prefix: "temporal-", modDir: "handlers/temporal"},
	// mcp/cmd/server moved into mcp/ when that package became a nested
	// module of its own, for the same reason: the MCP SDK's graph is
	// quarantined there, and a binary that imports it has to live inside it.
	{path: "mcp/cmd", group: "demos", prefix: "mcp-", modDir: "mcp"},
	{path: "docs/learn/examples", group: "learn examples", prefix: "learn-", ownDir: true},
	// apps/ holds showcase applications; some (kanban) are their own
	// nested modules because they import gooey/mcp's SDK graph. ownDir
	// covers both cases: `go run .` from the app's directory works
	// whether it is its own module or part of the root one.
	// Every entry here is a Go app today. The scan is one level deep and
	// requires a main.go, so an app's own subdirectories — kanban's
	// Python worker/ companion, say — are never entries in their own
	// right; they belong to the app that owns them, and that is the
	// point of them living inside it.
	{path: "apps", group: "apps", prefix: "app-", ownDir: true},
}

// moduleRoot walks up from cwd to the directory holding go.mod — `go
// run ./<dir>` must execute there.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		dir = parent
	}
}

// scan reads the inventory through fs.FS — the directory listing is data
// like any other, so the UI binds to it and refreshes when the tree
// changes. Paths are joined with path.Join, not filepath.Join: fs.FS is
// defined on slash-separated names regardless of host OS.
//
// Both roots use the same convention (a directory one level deep holding
// a main.go), so one loop covers them; a root that does not exist simply
// contributes nothing.
func scan(env scanEnv, self string) []demo {
	var out []demo
	for _, r := range roots {
		entries, err := fs.ReadDir(env.src, r.path)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || (r.path == "cmd" && e.Name() == self) {
				continue
			}
			dir := path.Join(r.path, e.Name())
			if _, err := fs.Stat(env.src, path.Join(dir, "main.go")); err != nil {
				continue
			}
			d := demo{name: e.Name(), dir: dir, group: r.group,
				rec: r.prefix + e.Name(), ownDir: r.ownDir, modDir: r.modDir}
			if src, err := fs.ReadFile(env.src, path.Join(dir, "main.go")); err == nil {
				d.doc = leadingComment(string(src))
			}
			// A README is the directory speaking for itself, so it wins
			// over the doc comment. It is read here, with everything else,
			// so an edit to it re-reaches the pane through the same rescan
			// that notices a new .gooey file.
			if md, err := fs.ReadFile(env.src, path.Join(dir, "README.md")); err == nil && len(md) <= maxREADME {
				d.readme = string(md)
			}
			if files, err := fs.ReadDir(env.src, dir); err == nil {
				for _, f := range files {
					if strings.HasSuffix(f.Name(), ".gooey") {
						d.markup++
					}
				}
			}
			// Existing artifacts are part of the same directory data: the
			// list shows what has already been recorded — in the LAUNCH
			// tree, which is where `r` writes regardless of source.
			_, err := fs.Stat(env.rec, path.Join(recDir, d.rec+".cast"))
			d.cast = err == nil
			_, err = fs.Stat(env.rec, path.Join(recDir, d.rec+".gif"))
			d.gif = err == nil
			d.gifPath, d.gifDir, d.gifKey, _ = gifFor(env, d.rec, d.name)
			out = append(out, d)
		}
	}
	return out
}

// runIn is the working directory and package argument this entry is
// launched with — the pair that has to agree with how it finds its own
// markup. See roots.ownDir.
func (d demo) runIn(root string) (dir, pkg string) {
	if d.ownDir {
		return filepath.Join(root, filepath.FromSlash(d.dir)), "."
	}
	if d.modDir != "" {
		// Nested module: run from ITS root with the package path
		// relative to it (handlers/temporal + ./cmd/temporaldemo).
		return filepath.Join(root, filepath.FromSlash(d.modDir)),
			"./" + strings.TrimPrefix(d.dir, d.modDir+"/")
	}
	return root, "./" + d.dir
}

// rows is the list as PAINTED: a group header before each run of entries
// from the same root, then the entries. Selection indexes the demo
// slice, never these rows, so a header can never be selected — the two
// coordinate systems meet only here and in the click handler.
type row struct {
	header string
	demo   int // index into the demo slice; -1 for a header
}

func rowsFor(ds []demo) []row {
	var out []row
	group := ""
	for i, d := range ds {
		if d.group != group {
			group = d.group
			out = append(out, row{header: group, demo: -1})
		}
		out = append(out, row{demo: i})
	}
	return out
}

func leadingComment(src string) string {
	var sb strings.Builder
	for _, ln := range strings.Split(src, "\n") {
		if strings.HasPrefix(ln, "//") {
			sb.WriteString(strings.TrimPrefix(strings.TrimPrefix(ln, "//"), " "))
			sb.WriteString("\n")
			continue
		}
		break
	}
	return strings.TrimSpace(sb.String())
}

func main() {
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// --- sources: which checkout the browser resolves against ---
	// The launch tree is source zero and always exists; everything else
	// comes from git at `b`-time (source.go). srcMgr owns the worker all
	// git work runs on and the ephemeral worktrees that must not outlive
	// this process.
	srcMgr := newSourceMgr(root)
	launchSrc := source{Name: filepath.Base(root), Root: root, Launch: true}
	if b := branchOf(root); b != "" {
		launchSrc.Name, launchSrc.Branch = b, b
	}
	cur := prop.NewSource(launchSrc)

	// --- viewmodel: the directory IS the data source ---
	rev := prop.NewSource(0) // bumped when anything the UI reads changes → rescan
	demos := prop.NewComputed(func() []demo {
		rev.Get()
		return scan(scanEnvFor(cur.Get().Root, root), "browser")
	})
	sel := prop.NewSource(0)
	status := prop.NewSource("ready")

	// title and hint are computed off the same two sources the list
	// paints from, so the chrome is bound rather than restated: the
	// markup names them, it does not spell out what they say.
	title := prop.NewComputed(func() string {
		ds := demos.Get()
		var learn int
		for _, d := range ds {
			if d.group == roots[len(roots)-1].group {
				learn++
			}
		}
		return fmt.Sprintf("%d demos + %d learn examples — pick one; it takes over this terminal and hands it back when it exits",
			len(ds)-learn, learn)
	})
	// The picker exists before the hint because the hint READS it: while
	// the picker is open the hint shows the picker's keys.
	//
	// That read used to be load-bearing for a second reason — it was the
	// only subscription to the open property, because a Collapsed popup
	// never evaluates its own Render and so the FIRST open scheduled no
	// frame. components.Popup carries its own subscription now (see
	// picker.go), so this is just a hint saying what the keys are.
	// What to do with a picked source is wired below, once the app and
	// the launch machinery exist.
	var switchTo func(source)
	picker := newSourcePicker(func(s source) { switchTo(s) })

	hint := prop.NewComputed(func() string {
		if picker.IsOpen() {
			return "j/k ↑/↓ select   enter switch source   esc close"
		}
		ds := demos.Get()
		if len(ds) == 0 {
			return "j/k ↑/↓ select   enter run   r record   b sources   q quit"
		}
		// The hint stays short enough that `q quit` survives the clip at
		// 80 columns — the one affordance that must never scroll off.
		// The artifact PATHS moved into the info pane, which wraps and is
		// wide: `learn-03-binding-and-state.cast` spelled out here pushed
		// the quit key off the end of an 80-column terminal.
		play := ""
		if ds[clampIdx(sel.Get(), len(ds))].gifPath != "" {
			play = "   p play"
		}
		return "enter run   r record" + play + "   b sources   j/k ↑/↓ select   q quit"
	})

	// The preview pane's header is five lines of text ABOUT the
	// selection. The lines themselves are infopane.gooey's; what is here
	// is only what markup cannot derive.
	//
	// The split is deliberate and worth reading twice, because it is the
	// whole shape of "push structure into markup". <Text> interpolates
	// literal and bound segments, so every fixed word — "r record → ",
	// "recordings/", ".cast" — belongs in the document, and the handle
	// beside it carries DATA and nothing else. Only where the string has
	// a BRANCH in it (three ways to spell a `go run` line; an artifact
	// pair that is one path or two) does a whole-line computed earn its
	// place, because the binding dialect has no conditional.
	//
	// Each line also has a companion flag, because "this line is absent"
	// is a Visibility question and markup can ask it
	// (Visibility="{{.ShowPlay}}"), where Go used to answer it with an
	// `if` around a paint. They are flags rather than emptiness tests
	// only because bindVisibility takes a bool or a Visibility and
	// nothing else — there is no string-is-empty converter.
	cmdLine := selLine(demos, sel, cur, func(d demo, _ source) string {
		switch {
		case d.ownDir:
			return "cd " + d.dir + " && go run ."
		case d.modDir != "":
			return "cd " + d.modDir + " && go run ./" + strings.TrimPrefix(d.dir, d.modDir+"/")
		}
		return "go run ./" + d.dir
	})
	recName := selLine(demos, sel, cur, func(d demo, _ source) string { return d.rec })
	gifPath := selLine(demos, sel, cur, func(d demo, _ source) string { return d.gifPath })
	sourceDesc := selLine(demos, sel, cur, func(_ demo, src source) string { return src.describe() })
	// The hint has no room for the artifact paths, so they live in the
	// pane, where `r` and `p` each say exactly which file they mean.
	artifacts := selLine(demos, sel, cur, func(d demo, _ source) string {
		art := recDir + "/" + d.rec + ".cast"
		if d.gif {
			art += "  +  " + recDir + "/" + d.rec + ".gif"
		}
		return art
	})
	hasDemos := selFlag(demos, sel, cur, func(demo, source) bool { return true })
	noDemos := prop.NewComputed(func() bool { return len(demos.Get()) == 0 })
	showSource := selFlag(demos, sel, cur, func(_ demo, src source) bool { return !src.Launch })
	showRecorded := selFlag(demos, sel, cur, func(d demo, _ source) bool { return d.cast })
	showPlay := selFlag(demos, sel, cur, func(d demo, _ source) bool { return d.gifPath != "" })

	// The pane border names the active source, so which tree you are
	// looking at is chrome, not something to remember.
	paneTitle := prop.NewComputed(func() string {
		s := cur.Get()
		t := "gooey demo browser"
		if s.Branch != "" || !s.Launch {
			t += " — ⎇ " + s.Name
		}
		if s.Ephemeral {
			t += " (ephemeral worktree)"
		}
		return t
	})

	// asciinema is looked up once: the `r` affordance reports its own
	// absence instead of failing at the handoff, when the screen is
	// already gone and there is nowhere to show the error.
	recorder, recErr := exec.LookPath("asciinema")
	gifTool, gifErr := exec.LookPath("agg")

	// launch is filled in once the App exists; the commands below close
	// over it. A launch is POSTED rather than run inline: a command
	// dispatches in the middle of routing an event, and the terminal
	// hand-off belongs at the top of the loop, on its own.
	var launch func(d demo, record bool)
	var app *gooey.App

	// The player outlives any single composition — a markup reload
	// rebuilds the preview component, and playback should not stop because
	// browser.gooey was saved. What it may NOT outlive is the composition
	// being live: the component hands it to the Composer as a Startable, so
	// Composer.Close stops the ticker (see demoBody.Start).
	play := newPlayer(status)

	// The markup root is resolved BEFORE the context, because the
	// context names a second document: the preview pane is its own
	// markup-only control (an Include), loaded through the same fs.FS
	// seam and listed on markup.Page below so editing it hot-reloads.
	mfs := demomain.MarkupFS("browser", "browser.gooey")

	// rescan is what the page's <Timer> ticks. The fingerprint is
	// UI-goroutine state: the command runs posted, like every Action.
	fingerprint := watchKey(root, root)

	ctx := &markup.Context{
		Values: map[string]any{
			"Title": title, "Hint": hint, "Status": status, "PaneTitle": paneTitle,
			// The preview pane's header: the data each line interpolates,
			// plus the flag that collapses it.
			"CmdLine": cmdLine, "RecName": recName, "GifPath": gifPath,
			"SourceDesc": sourceDesc, "Artifacts": artifacts,
			"HasDemos": hasDemos, "NoDemos": noDemos, "ShowSource": showSource,
			"ShowRecorded": showRecorded, "ShowPlay": showPlay,
			// The directory is a data source like any other, polled onto
			// the UI goroutine: a new recording, a new demo, an added
			// .gooey file or an edited doc comment shows up without a key
			// being pressed. One bump re-derives the list AND the pane
			// currently on screen, because both are bound to the same
			// computed.
			"Rescan": gooey.Command(func() {
				if k := watchKey(cur.Get().Root, root); k != fingerprint {
					fingerprint = k
					rev.Set(rev.Get() + 1)
				}
			}),
			"Run": gooey.Command(func() {
				if ds := demos.Get(); len(ds) > 0 {
					d := ds[clampIdx(sel.Get(), len(ds))]
					app.Post(func() { launch(d, false) })
				}
			}),
			"Record": gooey.Command(func() {
				if recErr != nil {
					status.Set("asciinema not installed — `apt install asciinema` (or brew) to record")
					return
				}
				if ds := demos.Get(); len(ds) > 0 {
					d := ds[clampIdx(sel.Get(), len(ds))]
					app.Post(func() { launch(d, true) })
				}
			}),
			"Play": gooey.Command(func() {
				if ds := demos.Get(); len(ds) > 0 {
					d := ds[clampIdx(sel.Get(), len(ds))]
					play.Toggle(d.gifDir, d.gifPath, d.gifKey)
				}
			}),
			// `b`: enumerate on the git worker — status --porcelain per
			// worktree is subprocess work with no place on the UI
			// goroutine — and open the picker from a posted closure.
			"Sources": gooey.Command(func() {
				if picker.IsOpen() {
					picker.Dismiss()
					return
				}
				status.Set("reading sources…")
				srcMgr.do(func() {
					list := listSources(root, srcMgr.eph)
					app.Post(func() {
						status.Set(fmt.Sprintf("%d sources — enter switches, esc closes", len(list)))
						picker.Open(list, cur.Get().id())
					})
				})
			}),
			"Quit": gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": accent,
			"dim":    dim,
		},
		Components: map[string]markup.Builder{
			"DemoList": func(markup.Element, *markup.Context) (gooey.Component, error) {
				return &demoList{demos: demos, sel: sel}, nil
			},
			// The preview pane is markup (infopane.gooey); this is only
			// the body it cannot declare — rendered markdown, wrapped doc
			// text, or a GIF frame.
			"DemoBody": func(markup.Element, *markup.Context) (gooey.Component, error) {
				return &demoBody{demos: demos, sel: sel, play: play}, nil
			},
			"InfoPane": markup.Include(mfs, "infopane.gooey"),
			"SourcePicker": func(markup.Element, *markup.Context) (gooey.Component, error) {
				return picker, nil
			},
		},
	}

	app = gooey.NewApp(markup.Page(mfs, "browser.gooey", ctx, "infopane.gooey"))

	// The hand-off, in one call. App.Suspend restores the terminal, JOINS
	// the input decoder so nothing of ours is still reading the tty while
	// the child runs, shields the interrupt the tty driver sends to the
	// whole foreground process group, and takes the terminal back after —
	// picking up a resize that happened while we were away.
	//
	// This used to be sixty lines here: an openUI/closeUI pair, a second
	// read-only /dev/tty handle, and a private copy of the event decoder,
	// all to work around a Screen teardown that could not stop its own
	// reader (docs/specs/2026-08-10-tty-read-lifecycle.md). The framework
	// fixed the lifecycle, so the workaround is gone. The tripwire stays.
	launch = func(d demo, record bool) {
		// Playback stops BEFORE the hand-off, not after. Suspend does not
		// close the composition — it only gives the terminal away — so a
		// ticker left running would spend the child's whole lifetime
		// queueing frame advances onto a dispatcher nobody is draining,
		// and deliver them in one burst on the way back.
		play.Stop()
		// The demo runs in the ACTIVE source's checkout; runIn resolves
		// ownDir and nested modules against that root exactly as it does
		// against the launch tree — a branch's go.mod differences are
		// handled by running from its own root, nothing more.
		srcRoot := cur.Get().Root
		var msg string
		err := app.Suspend(func() error {
			if record {
				msg = recordDemo(root, srcRoot, gifTool, gifErr == nil, recorder, d)
				return nil
			}
			compiling(d.name)
			dir, pkg := d.runIn(srcRoot)
			if err := run(dir, "go", "run", pkg); err != nil {
				msg = fmt.Sprintf("%s exited: %v", d.name, err)
			} else {
				msg = fmt.Sprintf("%s exited — welcome back", d.name)
			}
			return nil
		})
		if err != nil {
			msg += "  [" + err.Error() + "]"
		}
		if app.DecoderLeaked() {
			msg += "  [warning: input decoder outlived the handoff]"
		}
		status.Set(msg)
		rev.Set(rev.Get() + 1) // a recording may have appeared
	}

	// Switching sources. A source that is already a directory — the
	// launch tree, a real worktree — adopts immediately; a bare branch
	// first gets its throwaway detached worktree on the git worker, then
	// adopts from the posted completion. Adoption releases the PREVIOUS
	// source's ephemeral worktree (switch-away is one of the two moments
	// they are removed; exit is the other), resets the selection, and
	// stops playback — a GIF from one branch has no business animating
	// over another branch's preview.
	adopt := func(s source) {
		prev := cur.Get()
		if prev.id() == s.id() {
			return
		}
		if prev.Ephemeral {
			branch := prev.Branch
			srcMgr.do(func() { srcMgr.release(branch) })
		}
		play.Stop()
		cur.Set(s)
		sel.Set(0)
		status.Set("source: " + s.describe())
	}
	swGen := 0 // supersedes in-flight materializations, UI-goroutine state
	switchTo = func(s source) {
		swGen++
		if s.id() == cur.Get().id() {
			status.Set("source: " + s.describe() + " (already active)")
			return
		}
		if s.Root != "" {
			adopt(s)
			return
		}
		gen := swGen
		status.Set("⎇ " + s.Name + ": creating a temporary worktree…")
		srcMgr.do(func() {
			dir, err := srcMgr.materialize(s.Branch)
			app.Post(func() {
				if err != nil {
					status.Set("cannot check out " + s.Name + ": " + err.Error())
					return
				}
				if gen != swGen {
					// A later pick superseded this one while git ran. The
					// worktree stays registered in srcMgr — re-picking the
					// branch reuses it, exit removes it.
					return
				}
				ns := s
				ns.Root, ns.Ephemeral = dir, true
				adopt(ns)
			})
		})
	}

	// Playback belongs to the entry that was selected when it started.
	// Moving the selection, or a rescan that re-resolved (or re-recorded)
	// the GIF, ends it — checked here rather than in a setter because the
	// selection has several writers (keys, clicks, the wheel) and the
	// rescan has none. Stopping from BeforeFrame folds the resulting
	// repaint into the frame that is about to happen.
	app.BeforeFrame(func() {
		if !play.Playing() && play.Source() == "" {
			return
		}
		ds := demos.Get()
		if len(ds) == 0 {
			play.Stop()
			return
		}
		if d := ds[clampIdx(sel.Get(), len(ds))]; play.Stale(d.gifDir, d.gifPath, d.gifKey) {
			play.Stop()
		}
	})

	err = app.Run(context.Background())
	// Ephemeral worktrees are removed BEFORE gooey.Exit, which re-raises
	// a fatal signal and never returns — a deferred cleanup would be
	// skipped exactly when the user hit ctrl+c. Close joins the git
	// worker, so nothing is still adding a worktree while we leave.
	srcMgr.Close()
	if err != nil {
		gooey.Exit(err)
	}
}

// compiling prints the pre-exec notice. `go run` is silent while it
// builds, so a cold demo leaves the terminal blank for seconds right
// after the UI disappears — the exact moment the hand-off looks hung
// rather than busy. Saying so costs one line and is the difference
// between "dead" and "working".
func compiling(name string) {
	fmt.Printf("\n── %s: compiling… (a cold build takes a few seconds; cached after that)\n", name)
	fmt.Printf("── it owns this terminal once it starts — quit it to come back ──\n\n")
}

// run executes a child that owns the terminal. Shielding the interrupt
// is no longer this function's job: App.Suspend does it for the whole
// hand-off, which is where it belongs — the browser is not the only
// program that will ever launch a child onto its terminal.
func run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// record runs the demo inside asciinema with the same terminal handoff,
// so what gets captured is the real session: asciinema owns the
// terminal, the user drives the demo, and quitting the demo ends the
// recording. agg then renders a GIF if it is installed.
//
// The demo runs in srcRoot — whatever source is active — but the
// artifacts land under launchRoot: a recording is something the user
// keeps, and the source might be a throwaway worktree that is minutes
// from deletion.
func recordDemo(launchRoot, srcRoot, gifTool string, haveGif bool, recorder string, d demo) string {
	dir := filepath.Join(launchRoot, recDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "cannot create " + recDir + ": " + err.Error()
	}
	cast := filepath.Join(dir, d.rec+".cast")
	fmt.Printf("\n── recording %s → %s/%s.cast — quit it to stop the recording ──\n", d.name, recDir, d.rec)
	compiling(d.name)
	runDir, pkg := d.runIn(srcRoot)
	if err := run(runDir, recorder, "rec", "--overwrite", "-c", "go run "+pkg, cast); err != nil {
		return fmt.Sprintf("recording %s failed: %v", d.name, err)
	}
	msg := fmt.Sprintf("recorded → %s/%s.cast", recDir, d.rec)
	if !haveGif {
		return msg + "  (agg not installed — no GIF)"
	}
	gif := filepath.Join(dir, d.rec+".gif")
	fmt.Printf("\n── rendering %s/%s.gif ──\n\n", recDir, d.rec)
	if err := run(launchRoot, gifTool, "--theme", "dracula", "--font-size", "14", cast, gif); err != nil {
		return msg + fmt.Sprintf("  (agg failed: %v)", err)
	}
	return msg + fmt.Sprintf(" + %s/%s.gif", recDir, d.rec)
}

func clampIdx(i, n int) int { return max(0, min(i, n-1)) }

// selLine and selFlag build the preview pane's viewmodel: one computed
// per line of the header, over the SELECTED entry.
//
// The three Gets happen before the empty-list return, and that order is
// the whole contract. A dependency is recorded by the Get that actually
// runs, so a line that returned early past sel.Get() would go deaf to
// the selection moving — the pane would keep showing the previous
// entry's command until something unrelated forced a repaint. Same rule
// the old Render followed by reading `cur` above its own early return.
func selLine(demos *prop.Property[[]demo], sel *prop.Property[int], cur *prop.Property[source],
	f func(d demo, src source) string) *prop.Property[string] {
	return prop.NewComputed(func() string {
		ds, src, i := demos.Get(), cur.Get(), sel.Get()
		if len(ds) == 0 {
			return ""
		}
		return f(ds[clampIdx(i, len(ds))], src)
	})
}

func selFlag(demos *prop.Property[[]demo], sel *prop.Property[int], cur *prop.Property[source],
	f func(d demo, src source) bool) *prop.Property[bool] {
	return prop.NewComputed(func() bool {
		ds, src, i := demos.Get(), cur.Get(), sel.Get()
		if len(ds) == 0 {
			return false
		}
		return f(ds[clampIdx(i, len(ds))], src)
	})
}

// demoList is the directory-bound list pane — a focus stop.
type demoList struct {
	gooey.Base
	gooey.FocusState
	demos *prop.Property[[]demo]
	sel   *prop.Property[int]
}

func (w *demoList) Measure(avail gooey.Size) gooey.Size { return avail }

func (w *demoList) HandleKey(ev input.KeyEvent) bool {
	d := 0
	switch ev {
	case input.Rune('j'), input.Named(input.KeyDown):
		d = +1
	case input.Rune('k'), input.Named(input.KeyUp):
		d = -1
	default:
		return false
	}
	w.sel.Set(clampIdx(w.sel.Get()+d, len(w.demos.Get())))
	return true
}

func (w *demoList) HandleMouse(ev input.MouseEvent) bool {
	switch ev.Kind {
	case input.MouseClick:
		// Painted rows include group headers, so a click maps through the
		// same row list the paint uses. Clicking a header selects nothing.
		rows := rowsFor(w.demos.Get())
		if y := ev.Y - w.Bounds().Y; y >= 0 && y < len(rows) && rows[y].demo >= 0 {
			w.sel.Set(rows[y].demo)
		}
		return true
	case input.WheelUp:
		w.sel.Set(clampIdx(w.sel.Get()-1, len(w.demos.Get())))
		return true
	case input.WheelDown:
		w.sel.Set(clampIdx(w.sel.Get()+1, len(w.demos.Get())))
		return true
	}
	return false
}

func (w *demoList) Render(f *gooey.Frame) {
	b := w.Bounds()
	ds := w.demos.Get()
	sel := clampIdx(w.sel.Get(), len(ds))
	for y, r := range rowsFor(ds) {
		if y >= b.H {
			break
		}
		if r.demo < 0 {
			f.Cells.SetString(b.X, b.Y+y, clip(r.header, b.W), dim)
			continue
		}
		d := ds[r.demo]
		st := render.Style{}
		if r.demo == sel {
			st.Reverse = true
			for x := 0; x < b.W; x++ {
				f.Cells.Set(b.X+x, b.Y+y, ' ', st)
			}
		}
		// Indented under its header, so the grouping reads as grouping
		// rather than as two lists that happen to be adjacent.
		label := "  " + d.name
		if d.markup > 0 {
			label = fmt.Sprintf("  %s  ⟨%d .gooey⟩", d.name, d.markup)
		}
		// ● recorded with a GIF, ○ recorded as a cast only. ▶ is the
		// separate question `p` asks: is there a GIF to play at all —
		// which for most demos is the one checked in at the repo root
		// rather than anything in recordings/.
		switch {
		case d.gif:
			label += "  ●"
		case d.cast:
			label += "  ○"
		}
		if d.gifPath != "" && !d.gif {
			label += "  ▶"
		}
		f.Cells.SetString(b.X, b.Y+y, clip(label, b.W), st)
	}
}

// demoBody is what the preview pane cannot declare: the selection's
// README rendered as markdown, its main.go doc comment when it has no
// README, or a frame of its recorded GIF while `p` is playing.
//
// Everything ABOVE it in the pane — the command line, the artifact
// paths, the source name, and which of them are showing at all — is
// markup now (infopane.gooey), bound to computeds. What is left here is
// exactly the three things markup has no element for: a markdown
// renderer, a text wrapper, and an aspect-fitted image.
//
// It is a Startable as well as a Component. The Composer collects Startables
// on the same walk that finds key bindings, so the animation clock's
// lifetime is the composition's: a hot reload or a teardown stops it
// without anything here having to notice.
type demoBody struct {
	gooey.Base
	demos *prop.Property[[]demo]
	sel   *prop.Property[int]
	play  *player
}

func (w *demoBody) Measure(avail gooey.Size) gooey.Size { return avail }

func (w *demoBody) Start(post func(func())) func() { return w.play.Start(post) }

func (w *demoBody) Render(f *gooey.Frame) {
	// Every read this node depends on happens FIRST, above any early
	// return. A dependency is recorded by the Get that actually runs, and
	// a body too short to draw into still has to hear about playback —
	// which is the same reason Current reads `playing` before `clip`.
	ds := w.demos.Get()
	i := w.sel.Get()
	img := w.play.Current()

	b := w.Bounds()
	if len(ds) == 0 || b.W <= 0 || b.H <= 0 {
		return
	}
	d := ds[clampIdx(i, len(ds))]

	if img != nil {
		if cols, rows := fitCells(img.Bounds().Dx(), img.Bounds().Dy(), b.W, b.H); cols > 0 && rows > 0 {
			graphics.DrawHalfblock(f.Cells, img, b.X+(b.W-cols)/2, b.Y+(b.H-rows)/2, cols, rows)
		}
		return
	}
	if d.readme != "" {
		drawLines(f, b.X, b.Y, b.W, b.H, renderMarkdown(d.readme, b.W, markdownStyles()))
		return
	}
	// No README: the doc comment, as plain wrapped text. A Go comment is
	// not markdown and pretending otherwise would style its `//` prose
	// with rules its author never opted into.
	y := b.Y
	for _, para := range strings.Split(d.doc, "\n") {
		for _, ln := range wrapLine(para, b.W) {
			if y >= b.Y+b.H {
				return
			}
			f.Cells.SetString(b.X, y, ln, render.Style{})
			y++
		}
	}
}

// clip truncates to w display COLUMNS. Every caller passes a column
// budget — b.W, b.W-3, b.X+b.W-1-x — so counting runes here let a line
// of wide glyphs claim up to twice its slot, and nothing downstream
// clips it (#357).
func clip(s string, w int) string { return render.ClipCols(s, w) }

func wrapLine(s string, w int) []string {
	if w < 4 {
		return nil
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	line := ""
	for _, word := range words {
		switch {
		case line == "":
			line = word
		// COLUMNS. This is the Go-doc-comment path, so it meets
		// whatever an author wrote in their prose.
		case render.StringWidth(line)+1+render.StringWidth(word) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	return append(out, line)
}
