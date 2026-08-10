// finder is the interactive demo: an fzf-style fuzzy file finder.
// Typing edits a query property; a computed property scores and ranks
// the file index; arrows move a selection property; a preview pane
// derives from the selection. Input → processing → derived views, all
// through the dependency graph — the pipeline is four properties and
// three computeds, and damage tracking means arrow keys repaint only
// the results and preview panes.
//
//	type      filter    ↑/↓ (or ctrl-p/n)  select
//	enter     print selection and exit      esc/ctrl-c  quit
//
// The shell is markup (finder.gooey) and hot-reloads like markuplog.
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
		return fmt.Sprintf("%d files   %d matched in %s   ↑/↓ select   enter open   esc quit",
			len(files), n, lastMatchDur.Round(time.Microsecond))
	})

	// --- markup context ---
	ctx := &markup.Context{
		Values: map[string]any{"Status": status},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		Widgets: map[string]markup.Builder{
			"Input": func(markup.Element, *markup.Context) (gooey.Widget, error) { return &inputBox{query: query}, nil },
			"Results": func(markup.Element, *markup.Context) (gooey.Widget, error) {
				return &resultsPane{rows: matches, sel: sel}, nil
			},
			"Preview": func(markup.Element, *markup.Context) (gooey.Widget, error) { return &previewPane{lines: preview}, nil },
		},
	}

	exe, _ := os.Executable()
	mkPath := filepath.Join(filepath.Dir(exe), "finder.gooey")
	if _, err := os.Stat(mkPath); err != nil {
		mkPath = "cmd/finder/finder.gooey"
	}
	fsys := os.DirFS(filepath.Dir(mkPath))
	name := filepath.Base(mkPath)
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
	var comp *gooey.Composer
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

	events := make(chan input.Event, 32)
	go term.DecodeEvents(screen, events)
	screen.EnableMouse()

	for {
		if needsFrame {
			comp.Frame()
			comp.Flush(screen.File())
			needsFrame = false
		}
		select {
		case w := <-swaps:
			attach(w)
		case e := <-events:
			// The finder has no focus tree; the wheel just moves the
			// selection like the arrows do.
			if e.IsMouse() {
				switch e.Mouse.Kind {
				case input.WheelUp:
					sel.Set(clampSel(sel.Get()-1, len(matches.Get())))
				case input.WheelDown:
					sel.Set(clampSel(sel.Get()+1, len(matches.Get())))
				}
				continue
			}
			ev := e.Key
			switch {
			case ev == input.Named(input.KeyEsc) || ev == ctrl('c'):
				screen.Restore()
				return
			case ev == input.Named(input.KeyEnter):
				ms := matches.Get()
				screen.Restore()
				if len(ms) > 0 {
					fmt.Println(ms[clampSel(sel.Get(), len(ms))].path)
				}
				return
			case ev == input.Named(input.KeyUp) || ev == ctrl('p'):
				sel.Set(clampSel(sel.Get()-1, len(matches.Get())))
			case ev == input.Named(input.KeyDown) || ev == ctrl('n'):
				sel.Set(clampSel(sel.Get()+1, len(matches.Get())))
			case ev == input.Named(input.KeyBackspace):
				if q := query.Get(); q != "" {
					query.Set(q[:len(q)-1])
					sel.Set(0)
				}
			case ev.Key == input.KeyRune && ev.Mods == 0:
				query.Set(query.Get() + string(ev.Rune))
				sel.Set(0)
			}
		}
	}
}

func ctrl(r rune) input.KeyEvent {
	return input.KeyEvent{Key: input.KeyRune, Rune: r, Mods: input.ModCtrl}
}

func clampSel(i, n int) int {
	return max(0, min(i, n-1))
}

// ---- widgets ----

type inputBox struct {
	gooey.Base
	query *prop.Property[string]
}

func (w *inputBox) Measure(avail gooey.Size) gooey.Size { return gooey.Size{W: avail.W, H: 1} }

func (w *inputBox) Render(f *gooey.Frame) {
	b := w.Bounds()
	accent := render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	q := w.query.Get()
	f.Cells.SetString(b.X, b.Y, "> ", accent)
	f.Cells.SetString(b.X+2, b.Y, q, render.Style{Bold: true})
	f.Cells.Set(b.X+2+len(q), b.Y, '█', accent) // end-of-line cursor
}

type resultsPane struct {
	gooey.Base
	rows *prop.Property[[]match]
	sel  *prop.Property[int]
}

func (w *resultsPane) Measure(avail gooey.Size) gooey.Size { return avail }

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
