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
// None of it needs a restart. A poll fingerprints every directory the UI
// reads from (watch.go), so a recording finished in another terminal, a
// new demo, an added .gooey file or an edited doc comment all reach the
// list and the visible preview on their own.
//
//	j/k ↑/↓  select    enter  run    r  record    p  play GIF    q/esc  quit
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
	dir     string // module-relative directory: `go run ./<dir>`
	group   string // which root it came from
	rec     string // recording base name, unique across groups
	ownDir  bool   // run it from its own directory rather than the module root
	doc     string // leading comment block of main.go
	readme  string // README.md, when the directory has one — preferred over doc
	markup  int    // number of .gooey files
	cast    bool   // a recording already exists
	gif     bool
	gifPath string  // module-relative GIF `p` would play, "" when none
	gifKey  fileKey // its identity, so a re-recording invalidates the decode
}

// roots are the two places a runnable gooey program lives: the demos,
// and the finished code for each Learn tutorial. They are listed rather
// than discovered because the ORDER is the presentation — demos first,
// examples second — and because a recursive walk of the module would
// also find things that are not meant to be launched.
//
// Recordings are named with the prefix, not the display name: `cmd/demo`
// and a future `docs/learn/examples/demo` would otherwise write to the
// same recordings/demo.cast.
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
}{
	{path: "cmd", group: "demos"},
	{path: "docs/learn/examples", group: "learn examples", prefix: "learn-", ownDir: true},
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
func scan(fsys fs.FS, self string) []demo {
	var out []demo
	for _, r := range roots {
		entries, err := fs.ReadDir(fsys, r.path)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || (r.path == "cmd" && e.Name() == self) {
				continue
			}
			dir := path.Join(r.path, e.Name())
			if _, err := fs.Stat(fsys, path.Join(dir, "main.go")); err != nil {
				continue
			}
			d := demo{name: e.Name(), dir: dir, group: r.group,
				rec: r.prefix + e.Name(), ownDir: r.ownDir}
			if src, err := fs.ReadFile(fsys, path.Join(dir, "main.go")); err == nil {
				d.doc = leadingComment(string(src))
			}
			// A README is the directory speaking for itself, so it wins
			// over the doc comment. It is read here, with everything else,
			// so an edit to it re-reaches the pane through the same rescan
			// that notices a new .gooey file.
			if md, err := fs.ReadFile(fsys, path.Join(dir, "README.md")); err == nil && len(md) <= maxREADME {
				d.readme = string(md)
			}
			if files, err := fs.ReadDir(fsys, dir); err == nil {
				for _, f := range files {
					if strings.HasSuffix(f.Name(), ".gooey") {
						d.markup++
					}
				}
			}
			// Existing artifacts are part of the same directory data: the
			// list shows what has already been recorded.
			_, err := fs.Stat(fsys, path.Join(recDir, d.rec+".cast"))
			d.cast = err == nil
			_, err = fs.Stat(fsys, path.Join(recDir, d.rec+".gif"))
			d.gif = err == nil
			d.gifPath, d.gifKey, _ = gifFor(fsys, d.rec, d.name)
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
	fsys := os.DirFS(root)

	// --- viewmodel: the directory IS the data source ---
	rev := prop.NewSource(0) // bumped when anything the UI reads changes → rescan
	demos := prop.NewComputed(func() []demo {
		rev.Get()
		return scan(fsys, "browser")
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
			if d.group == roots[1].group {
				learn++
			}
		}
		return fmt.Sprintf("%d demos + %d learn examples — pick one; it takes over this terminal and hands it back when it exits",
			len(ds)-learn, learn)
	})
	hint := prop.NewComputed(func() string {
		ds := demos.Get()
		if len(ds) == 0 {
			return "j/k ↑/↓ select   enter run   r record   q quit"
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
		return "enter run   r record" + play + "   j/k ↑/↓ select   q quit"
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
	// rebuilds the preview widget, and playback should not stop because
	// browser.gooey was saved. What it may NOT outlive is the composition
	// being live: the widget hands it to the Composer as a Startable, so
	// Composer.Close stops the ticker (see demoInfo.Start).
	play := newPlayer(status)

	ctx := &markup.Context{
		Values: map[string]any{
			"Title": title, "Hint": hint, "Status": status,
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
					play.Toggle(root, d.gifPath, d.gifKey)
				}
			}),
			"Quit": gooey.Command(func() { app.Quit() }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": accent,
			"dim":    dim,
		},
		Widgets: map[string]markup.Builder{
			"DemoList": func(markup.Element, *markup.Context) (gooey.Widget, error) {
				return &demoList{demos: demos, sel: sel}, nil
			},
			"DemoInfo": func(markup.Element, *markup.Context) (gooey.Widget, error) {
				return &demoInfo{demos: demos, sel: sel, play: play}, nil
			},
		},
	}

	mdir := filepath.Join("cmd", "browser")
	if _, err := os.Stat(filepath.Join(mdir, "browser.gooey")); err != nil {
		exe, _ := os.Executable()
		mdir = filepath.Dir(exe)
	}
	app = gooey.NewApp(markup.Page(os.DirFS(mdir), "browser.gooey", ctx))

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
		var msg string
		err := app.Suspend(func() error {
			if record {
				msg = recordDemo(root, gifTool, gifErr == nil, recorder, d)
				return nil
			}
			compiling(d.name)
			dir, pkg := d.runIn(root)
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

	// The directory is a data source like any other, polled onto the UI
	// goroutine: a new recording, a new demo, an added .gooey file or an
	// edited doc comment shows up without a key being pressed. One bump
	// re-derives the list AND the pane currently on screen, because both
	// are bound to the same computed.
	fingerprint := watchKey(root)
	app.Every(watchInterval, func() {
		if k := watchKey(root); k != fingerprint {
			fingerprint = k
			rev.Set(rev.Get() + 1)
		}
	})

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
		if d := ds[clampIdx(sel.Get(), len(ds))]; play.Stale(d.gifPath, d.gifKey) {
			play.Stop()
		}
	})

	if err := app.Run(context.Background()); err != nil {
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
func recordDemo(root, gifTool string, haveGif bool, recorder string, d demo) string {
	dir := filepath.Join(root, recDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "cannot create " + recDir + ": " + err.Error()
	}
	cast := filepath.Join(dir, d.rec+".cast")
	fmt.Printf("\n── recording %s → %s/%s.cast — quit it to stop the recording ──\n", d.name, recDir, d.rec)
	compiling(d.name)
	runDir, pkg := d.runIn(root)
	if err := run(runDir, recorder, "rec", "--overwrite", "-c", "go run "+pkg, cast); err != nil {
		return fmt.Sprintf("recording %s failed: %v", d.name, err)
	}
	msg := fmt.Sprintf("recorded → %s/%s.cast", recDir, d.rec)
	if !haveGif {
		return msg + "  (agg not installed — no GIF)"
	}
	gif := filepath.Join(dir, d.rec+".gif")
	fmt.Printf("\n── rendering %s/%s.gif ──\n\n", recDir, d.rec)
	if err := run(root, gifTool, "--theme", "dracula", "--font-size", "14", cast, gif); err != nil {
		return msg + fmt.Sprintf("  (agg failed: %v)", err)
	}
	return msg + fmt.Sprintf(" + %s/%s.gif", recDir, d.rec)
}

func clampIdx(i, n int) int { return max(0, min(i, n-1)) }

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

// demoInfo is the preview pane: a header of what the selection would DO,
// and a body of what it SAYS — its README rendered as markdown, its doc
// comment when it has no README, or its recorded GIF while `p` is
// playing.
//
// It is a Startable as well as a Widget. The Composer collects Startables
// on the same walk that finds key bindings, so the animation clock's
// lifetime is the composition's: a hot reload or a teardown stops it
// without anything here having to notice.
type demoInfo struct {
	gooey.Base
	demos *prop.Property[[]demo]
	sel   *prop.Property[int]
	play  *player
}

func (w *demoInfo) Measure(avail gooey.Size) gooey.Size { return avail }

func (w *demoInfo) Start(post func(func())) func() { return w.play.Start(post) }

func (w *demoInfo) Render(f *gooey.Frame) {
	b := w.Bounds()
	ds := w.demos.Get()
	if len(ds) == 0 {
		f.Cells.SetString(b.X, b.Y, "no demos found under cmd/", dim)
		return
	}
	d := ds[clampIdx(w.sel.Get(), len(ds))]

	y := b.Y
	line := func(s string, st render.Style) {
		if y < b.Y+b.H {
			f.Cells.SetString(b.X, y, clip(s, b.W), st)
		}
		y++
	}
	cmdline := "go run ./" + d.dir
	if d.ownDir {
		cmdline = "cd " + d.dir + " && go run ."
	}
	line(cmdline, accent)
	// The hint no longer has room for the artifact paths, so they live
	// here, where `r` and `p` each say exactly which file they mean.
	line("r record → "+recDir+"/"+d.rec+".cast", dim)
	if d.cast {
		art := recDir + "/" + d.rec + ".cast"
		if d.gif {
			art += "  +  " + recDir + "/" + d.rec + ".gif"
		}
		line("recorded: "+art, dim)
	}
	if d.gifPath != "" {
		line("p play → "+d.gifPath, dim)
	}
	y++

	// Reading the player inside Render is what makes the animation cheap:
	// the frame index becomes a dependency of THIS paint node and of
	// nothing else, so a tick repaints one widget. The read is
	// unconditional for the same reason it comes first inside Current —
	// a dependency is recorded by the Get that actually happens, and a
	// pane too short to draw into still has to hear about playback.
	img := w.play.Current()

	h := b.Y + b.H - y
	if h <= 0 {
		return
	}
	if img != nil {
		if cols, rows := fitCells(img.Bounds().Dx(), img.Bounds().Dy(), b.W, h); cols > 0 && rows > 0 {
			graphics.DrawHalfblock(f.Cells, img, b.X+(b.W-cols)/2, y+(h-rows)/2, cols, rows)
		}
		return
	}
	if d.readme != "" {
		drawLines(f, b.X, y, b.W, h, renderMarkdown(d.readme, b.W, markdownStyles()))
		return
	}
	// No README: the doc comment, as plain wrapped text. A Go comment is
	// not markdown and pretending otherwise would style its `//` prose
	// with rules its author never opted into.
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

func clip(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w < 0 {
		return ""
	}
	return string(r[:w])
}

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
		case len([]rune(line))+1+len([]rune(word)) <= w:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	return append(out, line)
}
