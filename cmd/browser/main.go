// browser: the demo launcher. A directory-reader viewmodel data-binds
// to the repo's cmd/ tree through the fs.FS seam: entries are filtered
// to the Go cmd convention — directories exactly one level deep that
// contain a main.go. Picking one runs it via `go run` in a child
// process that takes over THIS terminal (raw mode and alt screen are
// fully restored first, the input decoder is torn down so the child
// owns stdin, and everything is rebuilt when the child exits).
//
// Recording uses the same handoff: `r` wraps the demo in asciinema, so
// the terminal the demo drives is the one being captured, and agg turns
// the cast into a GIF afterwards.
//
//	j/k ↑/↓  select    enter  run    r  record    q/esc  quit
package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

var (
	accent = render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	dim    = render.Style{Fg: render.RGB(140, 140, 150)}
)

// recDir is where casts and GIFs land, relative to the module root.
const recDir = "recordings"

type demo struct {
	name   string
	doc    string // leading comment block of main.go
	markup int    // number of .gooey files
	cast   bool   // a recording already exists
	gif    bool
}

// moduleRoot walks up from cwd to the directory holding go.mod — `go
// run ./cmd/<name>` must execute there.
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

// scan reads the demo inventory through fs.FS — the directory listing
// is data like any other, so the UI binds to it and refreshes when the
// tree changes. Paths are joined with path.Join, not filepath.Join:
// fs.FS is defined on slash-separated names regardless of host OS.
func scan(fsys fs.FS, self string) []demo {
	entries, err := fs.ReadDir(fsys, "cmd")
	if err != nil {
		return nil
	}
	var out []demo
	for _, e := range entries {
		// The cmd convention: a directory one deep with a main.go.
		if !e.IsDir() || e.Name() == self {
			continue
		}
		dir := path.Join("cmd", e.Name())
		if _, err := fs.Stat(fsys, path.Join(dir, "main.go")); err != nil {
			continue
		}
		d := demo{name: e.Name()}
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
		_, err := fs.Stat(fsys, path.Join(recDir, e.Name()+".cast"))
		d.cast = err == nil
		_, err = fs.Stat(fsys, path.Join(recDir, e.Name()+".gif"))
		d.gif = err == nil
		out = append(out, d)
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

// pending is a launch deferred to the top of the main loop. Commands
// dispatch mid-frame; tearing the screen down there would destroy the
// composition that is still being walked.
type pending struct {
	name   string
	record bool
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
		return fmt.Sprintf("%d demos under cmd/ — pick one; it takes over this terminal and hands it back when it exits",
			len(demos.Get()))
	})
	hint := prop.NewComputed(func() string {
		ds := demos.Get()
		if len(ds) == 0 {
			return "j/k ↑/↓ select   enter run   r record   q quit"
		}
		// The info pane already spells out the go run command, so the
		// hint stays short enough that `q  quit` survives the clip at
		// 80 columns — the one affordance that must never scroll off.
		n := ds[clampIdx(sel.Get(), len(ds))].name
		return fmt.Sprintf("enter run   r record → %s/%s.cast   j/k ↑/↓ select   q quit", recDir, n)
	})

	// asciinema is looked up once: the `r` affordance reports its own
	// absence instead of failing at the handoff, when the screen is
	// already gone and there is nowhere to show the error.
	recorder, recErr := exec.LookPath("asciinema")
	gifTool, gifErr := exec.LookPath("agg")

	running := true
	var pend *pending
	ctx := &markup.Context{
		Values: map[string]any{
			"Title": title, "Hint": hint, "Status": status,
			"Run": gooey.Command(func() {
				if ds := demos.Get(); len(ds) > 0 {
					pend = &pending{name: ds[clampIdx(sel.Get(), len(ds))].name}
				}
			}),
			"Record": gooey.Command(func() {
				if recErr != nil {
					status.Set("asciinema not installed — `apt install asciinema` (or brew) to record")
					return
				}
				if ds := demos.Get(); len(ds) > 0 {
					pend = &pending{name: ds[clampIdx(sel.Get(), len(ds))].name, record: true}
				}
			}),
			"Quit": gooey.Command(func() { running = false }),
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
	tree, err := markup.Load(os.DirFS(mdir), "browser.gooey", ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// --- screen lifecycle: closeUI/openUI bracket the child handoff ---
	var (
		screen     *term.Screen
		readTTY    *os.File // input handle, owned here — see readEvents
		comp       *gooey.Composer
		evs        chan input.Event
		decDone    chan struct{}
		needsFrame = true
		cols, rows int
	)

	openUI := func() error {
		s, err := term.Open()
		if err != nil {
			return err
		}
		screen = s
		nc, nr := screen.Size()
		if err := screen.Raw(); err != nil {
			return err
		}
		screen.EnableMouse()
		// Input comes off a SECOND /dev/tty handle that this program
		// opens and only ever reads from, decoded by readEvents rather
		// than term.DecodeEvents. See readEvents for why: a decoder on
		// the Screen's own tty cannot be stopped, and would outlive the
		// handoff still eating the child's keystrokes.
		readTTY, err = os.OpenFile("/dev/tty", os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		evs = make(chan input.Event, 64)
		decDone = make(chan struct{})
		// Passed in rather than captured: these three are rebound on
		// every openUI, and a goroutine reading them from the enclosing
		// scope would be reading whichever session happened to be
		// current when it was first scheduled.
		go func(f *os.File, out chan<- input.Event, done chan struct{}) {
			defer close(done)
			readEvents(f, out)
		}(readTTY, evs, decDone)
		// The composition survives a handoff: Flush writes the whole
		// buffer, so re-entering a blank alt screen repaints correctly
		// from the retained buffer with nothing dirty. Rebuilding it
		// per launch would instead strand the previous paint nodes as
		// dependents of the same viewmodel properties.
		if comp == nil || nc != cols || nr != rows {
			cols, rows = nc, nr
			comp = gooey.NewComposer(tree, cols, rows)
			// Color depth comes from the environment, not from
			// Screen.Detect: Detect queries the terminal and, when
			// nothing answers, abandons a goroutine with a Read still
			// pending on the tty — a second reader competing with the
			// decoder for every keystroke.
			comp.SetCaps(term.Caps{Cols: cols, Rows: rows, Color: term.DetectColorDepth()})
			comp.OnInvalidate(func() { needsFrame = true })
		}
		needsFrame = true
		return nil
	}

	// closeUI hands the terminal to a child. It closes the input handle
	// first and WAITS for the reader to actually die, so no goroutine of
	// ours is still on the tty when the child starts, then restores the
	// screen (cooked mode, main screen, mouse off).
	//
	// The wait is a real check, not a formality: it is what caught the
	// term.Screen teardown problem readEvents works around. Draining evs
	// while waiting does double duty — the reader flushes what it had
	// buffered on the way out and would block forever on an unread
	// channel, and those pre-handoff keystrokes are exactly the stale
	// input that must never be replayed into the resumed UI.
	closeUI := func() bool {
		readTTY.Close()
		ok := false
		deadline := time.After(2 * time.Second)
		for wait := true; wait; {
			select {
			case <-decDone:
				ok, wait = true, false
			case <-evs:
			case <-deadline:
				wait = false
			}
		}
		screen.Restore()
		screen = nil
		return ok
	}

	if err := openUI(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() {
		if readTTY != nil {
			readTTY.Close()
		}
		if screen != nil {
			screen.Restore()
		}
	}()

	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	var lastMod time.Time

	for running {
		if pend != nil {
			p := *pend
			pend = nil
			clean := closeUI()

			var msg string
			if p.record {
				msg = record(root, gifTool, gifErr == nil, recorder, p.name)
			} else {
				compiling(p.name)
				if err := run(root, "go", "run", "./cmd/"+p.name); err != nil {
					msg = fmt.Sprintf("%s exited: %v", p.name, err)
				} else {
					msg = fmt.Sprintf("%s exited — welcome back", p.name)
				}
			}
			if !clean {
				msg += "  [warning: input decoder outlived the handoff]"
			}
			if err := openUI(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			status.Set(msg)
			rev.Set(rev.Get() + 1) // a recording may have appeared
			continue
		}
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case <-poll.C:
			if st, err := os.Stat(filepath.Join(root, "cmd")); err == nil && st.ModTime() != lastMod {
				lastMod = st.ModTime()
				rev.Set(rev.Get() + 1) // directory changed → rebind
			}
		case ev := <-evs:
			comp.Handle(ev)
		}
	}
}

// readEvents is this program's copy of term.DecodeEvents, reading from
// a tty handle the browser opens itself. The duplication buys one thing
// the framework version cannot currently give a launcher: a reader that
// can be stopped.
//
// term.Screen.Raw and Screen.Size have to hand an integer fd to
// golang.org/x/term, which means calling os.File.Fd — and Fd puts the
// file back into blocking mode and drops it from the runtime poller.
// A Read pending on such a file is an uninterruptible syscall, so
// Screen.Restore's Close does NOT unblock a decoder sitting on the
// Screen's tty. That goroutine survives the handoff still parked on the
// terminal, and every launch adds another one competing with the child
// (and with the next decoder) for keystrokes.
//
// A handle that is only ever read from never has Fd called on it, stays
// registered with the poller, and unblocks its reader on Close — which
// is what makes closeUI's wait terminate. The escape-timeout policy is
// term.EscTimeout so the two decoders cannot drift apart.
func readEvents(tty *os.File, out chan<- input.Event) {
	chunks := make(chan []byte, 8)
	go func() {
		defer close(chunks)
		for {
			buf := make([]byte, 128)
			n, err := tty.Read(buf)
			if n > 0 {
				chunks <- buf[:n]
			}
			if err != nil {
				return
			}
		}
	}()

	var pend []byte
	drain := func(idle bool) {
		for len(pend) > 0 {
			ev, n, ok := input.Decode(pend, idle)
			if n == 0 && !ok {
				return // incomplete: wait for more bytes
			}
			pend = pend[n:]
			if ok {
				out <- ev
			}
		}
	}
	timer := time.NewTimer(term.EscTimeout)
	defer timer.Stop()
	for {
		drain(false)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if len(pend) > 0 {
			timer.Reset(term.EscTimeout)
		}
		select {
		case c, ok := <-chunks:
			if !ok {
				drain(true)
				return
			}
			pend = append(pend, c...)
		case <-timer.C:
			drain(true)
		}
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

// run executes a child that owns the terminal. SIGINT is shielded for
// the duration: the tty driver signals the whole foreground process
// group, so a ctrl+c meant for the demo would otherwise kill the
// browser along with it. The child still receives its own SIGINT — this
// only stops ours from being fatal.
func run(dir, name string, args ...string) error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// record runs the demo inside asciinema with the same terminal handoff,
// so what gets captured is the real session: asciinema owns the
// terminal, the user drives the demo, and quitting the demo ends the
// recording. agg then renders a GIF if it is installed.
func record(root, gifTool string, haveGif bool, recorder, name string) string {
	dir := filepath.Join(root, recDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "cannot create " + recDir + ": " + err.Error()
	}
	cast := filepath.Join(dir, name+".cast")
	fmt.Printf("\n── recording %s → %s/%s.cast — quit the demo to stop the recording ──\n", name, recDir, name)
	compiling(name)
	if err := run(root, recorder, "rec", "--overwrite", "-c", "go run ./cmd/"+name, cast); err != nil {
		return fmt.Sprintf("recording %s failed: %v", name, err)
	}
	msg := fmt.Sprintf("recorded → %s/%s.cast", recDir, name)
	if !haveGif {
		return msg + "  (agg not installed — no GIF)"
	}
	gif := filepath.Join(dir, name+".gif")
	fmt.Printf("\n── rendering %s/%s.gif ──\n\n", recDir, name)
	if err := run(root, gifTool, "--theme", "dracula", "--font-size", "14", cast, gif); err != nil {
		return msg + fmt.Sprintf("  (agg failed: %v)", err)
	}
	return msg + fmt.Sprintf(" + %s/%s.gif", recDir, name)
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
		row := ev.Y - w.Bounds().Y
		if row >= 0 && row < len(w.demos.Get()) {
			w.sel.Set(row)
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
	s := clampIdx(w.sel.Get(), len(ds))
	for i, d := range ds {
		if i >= b.H {
			break
		}
		st := render.Style{}
		if i == s {
			st.Reverse = true
			for x := 0; x < b.W; x++ {
				f.Cells.Set(b.X+x, b.Y+i, ' ', st)
			}
		}
		label := d.name
		if d.markup > 0 {
			label = fmt.Sprintf("%s  ⟨%d .gooey⟩", d.name, d.markup)
		}
		if d.gif {
			label += "  ●"
		} else if d.cast {
			label += "  ○"
		}
		f.Cells.SetString(b.X, b.Y+i, clip(label, b.W), st)
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
	f.Cells.SetString(b.X, b.Y, clip("go run ./cmd/"+d.name, b.W), accent)
	if d.cast {
		art := recDir + "/" + d.name + ".cast"
		if d.gif {
			art += "  +  " + recDir + "/" + d.name + ".gif"
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
