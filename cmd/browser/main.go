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
//	j/k ↑/↓  select    enter  run    r  record    q/esc  quit
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
	"time"

	"github.com/WonderForgeLabs/gooey"
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

type demo struct {
	name   string // display name, unique within its group
	dir    string // module-relative directory: `go run ./<dir>`
	group  string // which root it came from
	rec    string // recording base name, unique across groups
	ownDir bool   // run it from its own directory rather than the module root
	doc    string // leading comment block of main.go
	markup int    // number of .gooey files
	cast   bool   // a recording already exists
	gif    bool
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
	rev := prop.NewSource(0) // bumped when cmd/ or recordings/ changes → rescan
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
		// The info pane already spells out the go run command, so the
		// hint stays short enough that `q  quit` survives the clip at
		// 80 columns — the one affordance that must never scroll off.
		d := ds[clampIdx(sel.Get(), len(ds))]
		return fmt.Sprintf("enter run   r record → %s/%s.cast   j/k ↑/↓ select   q quit", recDir, d.rec)
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
				return &demoInfo{demos: demos, sel: sel}, nil
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
	// goroutine: a new recording or a new demo shows up without a key
	// being pressed.
	var lastMod time.Time
	app.Every(2*time.Second, func() {
		if st, err := os.Stat(filepath.Join(root, "cmd")); err == nil && st.ModTime() != lastMod {
			lastMod = st.ModTime()
			rev.Set(rev.Get() + 1)
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
		if d.gif {
			label += "  ●"
		} else if d.cast {
			label += "  ○"
		}
		f.Cells.SetString(b.X, b.Y+y, clip(label, b.W), st)
	}
}

// demoInfo shows the selected demo's doc comment — data-bound preview.
type demoInfo struct {
	gooey.Base
	demos *prop.Property[[]demo]
	sel   *prop.Property[int]
}

func (w *demoInfo) Measure(avail gooey.Size) gooey.Size { return avail }

func (w *demoInfo) Render(f *gooey.Frame) {
	b := w.Bounds()
	ds := w.demos.Get()
	if len(ds) == 0 {
		f.Cells.SetString(b.X, b.Y, "no demos found under cmd/", dim)
		return
	}
	d := ds[clampIdx(w.sel.Get(), len(ds))]
	cmdline := "go run ./" + d.dir
	if d.ownDir {
		cmdline = "cd " + d.dir + " && go run ."
	}
	f.Cells.SetString(b.X, b.Y, clip(cmdline, b.W), accent)
	if d.cast {
		art := recDir + "/" + d.rec + ".cast"
		if d.gif {
			art += "  +  " + recDir + "/" + d.rec + ".gif"
		}
		f.Cells.SetString(b.X, b.Y+1, clip("recorded: "+art, b.W), dim)
	}
	y := b.Y + 3
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
