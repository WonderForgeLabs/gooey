// finder is the interactive demo: an fzf-style fuzzy file finder.
// Typing edits a query property; a computed property scores and ranks
// the file index; arrows move a selection property; a preview pane
// derives from the selection. Input → processing → derived views, all
// through the dependency graph — the pipeline is four properties and
// three computeds, and damage tracking means arrow keys repaint only
// the results and preview panes.
//
//	type      filter    ↑/↓ (or ctrl-p/n)  select   click  select a row
//	enter     print selection and exit      esc/ctrl-c  quit
//
// The shell is markup (finder.gooey) and hot-reloads like markuplog.
// Input is routed rather than switched on: the query line and the
// results pane are focus stops that consume their own keys, page-level
// gestures are <KeyBinding> declarations bound to viewmodel commands,
// and the wheel and clicks arrive by hit-testing — so main's event loop
// is one call to Composer.Handle.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

type match struct {
	path  string
	score int
	idxs  []int // matched rune positions, for highlighting
}

func main() {
	dir := "."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	files := index(dir)

	// --- viewmodel ---
	query := prop.NewSource("")
	sel := prop.NewSource(0)

	var lastMatchDur time.Duration
	matches := prop.NewComputed(func() []match {
		q := query.Get()
		start := time.Now()
		var ms []match
		if q == "" {
			for i, f := range files {
				if i >= 500 {
					break
				}
				ms = append(ms, match{path: f})
			}
		} else {
			for _, f := range files {
				if ok, score, idxs := fuzzy(q, f); ok {
					ms = append(ms, match{f, score, idxs})
				}
			}
			sort.SliceStable(ms, func(i, j int) bool { return ms[i].score > ms[j].score })
			if len(ms) > 500 {
				ms = ms[:500]
			}
		}
		lastMatchDur = time.Since(start)
		return ms
	})

	preview := prop.NewComputed(func() []string {
		ms := matches.Get()
		i := clampSel(sel.Get(), len(ms))
		if len(ms) == 0 {
			return []string{"no matches"}
		}
		return fileHead(ms[i].path, 200)
	})

	status := prop.NewComputed(func() string {
		n := len(matches.Get())
		return fmt.Sprintf("%d files   %d matched in %s   ↑/↓ or click select   enter open   esc quit",
			len(files), n, lastMatchDur.Round(time.Microsecond))
	})

	// --- widgets, built up front so the commands below can address
	// them. Hot reload re-runs the builders and hands back these same
	// instances: the tree is disposable, the state is not.
	var comp *gooey.Composer
	res := &resultsPane{rows: matches, sel: sel}
	// A click selects a row and hands typing straight back to the query
	// line — the query is always live, fzf-style. Framework
	// focus-follows-click has already moved focus to the results pane by
	// the time this runs, so this is a deliberate override, not a
	// workaround.
	// --- commands: the page's gestures resolve to these at load time.
	running, chosen := true, ""
	selectBy := func(d int) gooey.Command {
		return gooey.Command(func() { sel.Set(clampSel(sel.Get()+d, len(matches.Get()))) })
	}
	ctx := &markup.Context{
		Values: map[string]any{
			"Status": status,
			"Query":  query,
			// An edit invalidates the ranking, so the selection returns
			// to the top — the TextBox says WHAT changed, the demo says
			// what that means.
			"ResetSelection": gooey.Command(func() { sel.Set(0) }),
			"SelectPrev":     selectBy(-1),
			"SelectNext":     selectBy(+1),
			"Accept": gooey.Command(func() {
				if ms := matches.Get(); len(ms) > 0 {
					chosen = ms[clampSel(sel.Get(), len(ms))].path
				}
				running = false
			}),
			"Quit": gooey.Command(func() { running = false }),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
			"input":  {Bold: true},
		},
		Widgets: map[string]markup.Builder{
			"Results": func(markup.Element, *markup.Context) (gooey.Widget, error) { return res, nil },
			"Preview": func(markup.Element, *markup.Context) (gooey.Widget, error) {
				return &previewPane{lines: preview}, nil
			},
		},
	}

	// The query line is <TextBox> in the markup; look it up by name to
	// hand focus back to it after a click in the results.
	res.focusQuery = func() {
		if in, err := markup.Find[*gooey.TextBox](ctx, "query"); err == nil {
			comp.Focus().SetFocus(in)
		}
	}

	mkDir := "cmd/finder"
	if _, err := os.Stat(filepath.Join(mkDir, "finder.gooey")); err != nil {
		exe, _ := os.Executable()
		mkDir = filepath.Dir(exe)
	}
	fsys := os.DirFS(mkDir)
	name := "finder.gooey"
	tree, err := markup.Load(fsys, name, ctx)
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
	stopWatch := markup.Watch(fsys, name, ctx, func(w gooey.Widget) { swaps <- w })
	defer stopWatch()

	if err := screen.Raw(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	evs := make(chan input.Event, 32)
	go term.DecodeEvents(screen, evs)
	screen.EnableMouse()

	for running {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case w := <-swaps:
			attach(w)
		case ev := <-evs:
			comp.Handle(ev)
		}
	}
	screen.Restore()
	if chosen != "" {
		fmt.Println(chosen)
	}
}

func clampSel(i, n int) int {
	return max(0, min(i, n-1))
}

// ---- widgets ----

// The query line used to be a demo-local widget here. It is
// gooey.TextBox now — the framework version does its own editing,
// carries a real caret, and scrolls horizontally, none of which this
// demo had to grow itself.

// resultsPane is the second focus stop. It owns ↑/↓ while focused, and
// takes clicks and the wheel by hit-test whether focused or not.
type resultsPane struct {
	gooey.Base
	gooey.FocusState
	rows       *prop.Property[[]match]
	sel        *prop.Property[int]
	focusQuery func()
}

func (w *resultsPane) Measure(avail gooey.Size) gooey.Size { return avail }

func (w *resultsPane) move(d int) {
	w.sel.Set(clampSel(w.sel.Get()+d, len(w.rows.Get())))
}

func (w *resultsPane) HandleKey(ev input.KeyEvent) bool {
	switch ev {
	case input.Named(input.KeyUp):
		w.move(-1)
	case input.Named(input.KeyDown):
		w.move(+1)
	case input.Named(input.KeyPageUp):
		w.move(-w.Bounds().H)
	case input.Named(input.KeyPageDown):
		w.move(+w.Bounds().H)
	default:
		return false
	}
	return true
}

func (w *resultsPane) HandleMouse(ev input.MouseEvent) bool {
	switch ev.Kind {
	case input.MousePress:
		row := w.top() + ev.Y - w.Bounds().Y
		if row < 0 || row >= len(w.rows.Get()) {
			return false
		}
		w.sel.Set(row)
		if w.focusQuery != nil {
			w.focusQuery()
		}
		return true
	case input.WheelUp:
		w.move(-wheelStep)
	case input.WheelDown:
		w.move(+wheelStep)
	default:
		return false
	}
	return true
}

// wheelStep is the conventional three lines per notch; one line per
// notch reads as broken in a long list.
const wheelStep = 3

// top is the first visible row — the same scroll arithmetic Render
// uses, so a click maps back to the row the user actually sees.
func (w *resultsPane) top() int {
	return max(0, clampSel(w.sel.Get(), len(w.rows.Get()))-w.Bounds().H+1)
}

func (w *resultsPane) Render(f *gooey.Frame) {
	b := w.Bounds()
	ms := w.rows.Get()
	selected := clampSel(w.sel.Get(), len(ms))
	// Keep the selection in view.
	top := max(0, selected-b.H+1)
	hit := render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	for row := 0; row < b.H && top+row < len(ms); row++ {
		m := ms[top+row]
		base := render.Style{}
		if top+row == selected {
			base.Reverse = true
			for x := 0; x < b.W; x++ {
				f.Cells.Set(b.X+x, b.Y+row, ' ', base)
			}
		}
		idx := 0
		for i, r := range m.path {
			if i >= b.W-1 {
				break
			}
			st := base
			if idx < len(m.idxs) && m.idxs[idx] == i {
				st = hit
				st.Reverse = base.Reverse
				idx++
			}
			f.Cells.Set(b.X+i, b.Y+row, r, st)
		}
	}
}

type previewPane struct {
	gooey.Base
	lines *prop.Property[[]string]
}

func (w *previewPane) Measure(avail gooey.Size) gooey.Size { return avail }

func (w *previewPane) Render(f *gooey.Frame) {
	b := w.Bounds()
	dim := render.Style{Fg: render.RGB(140, 140, 150)}
	for i, ln := range w.lines.Get() {
		if i >= b.H {
			break
		}
		num := fmt.Sprintf("%3d ", i+1)
		f.Cells.SetString(b.X, b.Y+i, num, dim)
		f.Cells.SetString(b.X+len(num), b.Y+i, clip(ln, b.W-len(num)), render.Style{})
	}
}

func clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(strings.ReplaceAll(s, "\t", "    "))
	if len(r) > w {
		r = r[:w]
	}
	return string(r)
}

// ---- processing ----

// fuzzy is a case-insensitive subsequence matcher with fzf-ish scoring:
// consecutive hits and segment starts score higher; shorter paths win
// ties.
func fuzzy(q, s string) (bool, int, []int) {
	lq, ls := strings.ToLower(q), strings.ToLower(s)
	idxs := make([]int, 0, len(q))
	score, streak := 0, 0
	j := 0
	for i := 0; i < len(ls) && j < len(lq); i++ {
		if ls[i] != lq[j] {
			streak = 0
			continue
		}
		streak++
		score += 1 + streak // consecutive bonus
		if i == 0 || ls[i-1] == '/' || ls[i-1] == '_' || ls[i-1] == '-' || ls[i-1] == '.' {
			score += 4 // segment-start bonus
		}
		idxs = append(idxs, i)
		j++
	}
	if j < len(lq) {
		return false, 0, nil
	}
	return true, score - len(s)/8, idxs
}

func index(root string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, rel)
		if len(out) >= 50000 {
			return fs.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out
}

var previewCache = map[string][]string{}

func fileHead(path string, n int) []string {
	if ls, ok := previewCache[path]; ok {
		return ls
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{"(unreadable)"}
	}
	for _, b := range data[:min(len(data), 1024)] {
		if b == 0 {
			return []string{"(binary file)"}
		}
	}
	ls := strings.Split(string(data), "\n")
	if len(ls) > n {
		ls = ls[:n]
	}
	previewCache[path] = ls
	return ls
}
