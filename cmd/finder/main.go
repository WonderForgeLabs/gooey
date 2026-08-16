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
// Input is routed rather than switched on: the query line is the only
// focus stop (fzf-style — typing is always live), the results are an
// <ItemsView Focusable="false"> whose clicks and wheel arrive by
// hit-testing, page-level gestures are <KeyBinding> declarations bound
// to viewmodel commands — so main's event loop is one call to
// Composer.Handle.
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
	"github.com/WonderForgeLabs/gooey/cmd/internal/demomain"
	"github.com/WonderForgeLabs/gooey/components"
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

	// The results feed the <ItemsView> as an item source: one row is the
	// path and its matched positions, both live handles the template's
	// <MatchLine> binds. The projection reads nothing beyond the item, so
	// the plain Items adapter is enough.
	resultRows := components.Items(matches, func(m match) map[string]any {
		return map[string]any{"Path": m.path, "Hits": m.idxs}
	})

	// --- commands: the page's gestures resolve to these at load time.
	var comp *gooey.Composer
	var ctx *markup.Context
	running, chosen := true, ""
	selectBy := func(d func() int) gooey.Command {
		return gooey.Command(func() { sel.Set(clampSel(sel.Get()+d(), len(matches.Get()))) })
	}
	one := func(n int) func() int { return func() int { return n } }
	// A page is the results view's height. The view is looked up lazily —
	// commands resolve at load time, before the tree exists.
	page := func() int {
		if v, err := markup.Find[*components.ItemsView](ctx, "results"); err == nil {
			return max(1, v.Bounds().H)
		}
		return 10
	}
	ctx = &markup.Context{
		Values: map[string]any{
			"Status":    status,
			"Query":     query,
			"Rows":      resultRows,
			"Selection": sel,
			// An edit invalidates the ranking, so the selection returns
			// to the top — the TextBox says WHAT changed, the demo says
			// what that means.
			"ResetSelection": gooey.Command(func() { sel.Set(0) }),
			"SelectPrev":     selectBy(one(-1)),
			"SelectNext":     selectBy(one(+1)),
			"SelectPageUp":   selectBy(func() int { return -page() }),
			"SelectPageDown": selectBy(page),
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
		Components: map[string]markup.Builder{
			"MatchLine": buildMatchLine,
			"Preview": func(markup.Element, *markup.Context) (gooey.Component, error) {
				return &previewPane{lines: preview}, nil
			},
		},
	}

	name := "finder.gooey"
	fsys := demomain.MarkupFS("finder", name)
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
	attach := func(w gooey.Component) {
		comp = gooey.NewComposer(w, cols, rows)
		comp.OnInvalidate(func() { needsFrame = true })
		needsFrame = true
	}
	attach(tree)
	swaps := make(chan gooey.Component, 1)
	stopWatch := markup.Watch(fsys, name, ctx, func(w gooey.Component) { swaps <- w })
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

// ---- components ----

// The query line used to be a demo-local component here. It is
// components.TextBox now — the framework version does its own editing,
// carries a real caret, and scrolls horizontally, none of which this
// demo had to grow itself. The results pane followed: selection,
// windowing, clicks and the wheel are components.ItemsView. What is left
// is the one cell no builtin draws — a path with its matched runes lit —
// and the template places it.

// buildMatchLine is the markup builder for <MatchLine Path="{{.Path}}"
// Hits="{{.Hits}}"/>: both attributes resolve to the ROW's live handles,
// so a re-projection reaches a reused row without rebuilding it.
func buildMatchLine(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
	pv, err := ctx.BindingValue(e.Attrs["Path"])
	if err != nil {
		return nil, fmt.Errorf("markup: <MatchLine Path=%q>: %w", e.Attrs["Path"], err)
	}
	path, ok := pv.(*prop.Property[string])
	if !ok {
		return nil, fmt.Errorf("markup: <MatchLine Path=%q> is %T; need *prop.Property[string]", e.Attrs["Path"], pv)
	}
	hv, err := ctx.BindingValue(e.Attrs["Hits"])
	if err != nil {
		return nil, fmt.Errorf("markup: <MatchLine Hits=%q>: %w", e.Attrs["Hits"], err)
	}
	hits, ok := hv.(*prop.Property[[]int])
	if !ok {
		return nil, fmt.Errorf("markup: <MatchLine Hits=%q> is %T; need *prop.Property[[]int]", e.Attrs["Hits"], hv)
	}
	return &matchLine{path: path, hits: hits}, nil
}

// matchLine paints one result row: the path, with the fuzzy match's rune
// positions in the accent style. Selection is not its business — the
// view's house highlight re-styles whatever this painted.
type matchLine struct {
	gooey.Base
	path *prop.Property[string]
	hits *prop.Property[[]int]
}

func (w *matchLine) Measure(avail gooey.Size) gooey.Size { return gooey.Size{W: avail.W, H: 1} }

func (w *matchLine) Render(f *gooey.Frame) {
	b := w.Bounds()
	hit := render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	idxs := w.hits.Get()
	k := 0
	for i, r := range w.path.Get() {
		if i >= b.W-1 {
			break
		}
		st := render.Style{}
		if k < len(idxs) && idxs[k] == i {
			st = hit
			k++
		}
		f.Cells.Set(b.X+i, b.Y, r, st)
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
