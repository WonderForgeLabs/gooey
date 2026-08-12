package components

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// GridLen is one row/column definition: Fixed cells, Auto (size to
// content), or Star (weighted share of what remains) — XAML's
// GridLength.
type GridLen struct {
	Fixed int
	Star  float64
	Auto  bool
}

func Auto() GridLen          { return GridLen{Auto: true} }
func Star(w float64) GridLen { return GridLen{Star: w} }
func Fixed(n int) GridLen    { return GridLen{Fixed: n} }

// ParseGridLens parses "Auto,2*,10,*" — the markup form.
func ParseGridLens(s string) ([]GridLen, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var out []GridLen
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		switch {
		case strings.EqualFold(p, "Auto"):
			out = append(out, Auto())
		case p == "*":
			out = append(out, Star(1))
		case strings.HasSuffix(p, "*"):
			w, err := strconv.ParseFloat(strings.TrimSuffix(p, "*"), 64)
			if err != nil {
				return nil, fmt.Errorf("grid: bad star length %q", p)
			}
			out = append(out, Star(w))
		default:
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil, fmt.Errorf("grid: bad length %q", p)
			}
			out = append(out, Fixed(n))
		}
	}
	return out, nil
}

// Grid is the workhorse layout panel: children go into cells addressed
// by the Layout attached properties Row/Col/RowSpan/ColSpan.
// Missing definitions default to a single star track. Background, when
// set, is filled by the framework (gooey.HasBackground).
type Grid struct {
	gooey.Base
	Rows, Cols []GridLen
	Children   []gooey.Component
	Background *prop.Property[render.Color]

	rowSz, colSz []int
}

func (g *Grid) ChildComponents() []gooey.Component { return g.Children }

func (g *Grid) BackgroundProperty() *prop.Property[render.Color] { return g.Background }

func (g *Grid) rows() []GridLen {
	if len(g.Rows) == 0 {
		return []GridLen{Star(1)}
	}
	return g.Rows
}

func (g *Grid) cols() []GridLen {
	if len(g.Cols) == 0 {
		return []GridLen{Star(1)}
	}
	return g.Cols
}

func cellOf(w gooey.Component, nRows, nCols int) (r, c, rs, cs int) {
	l := gooey.LayoutOf(w)
	if l == nil {
		return 0, 0, 1, 1
	}
	r, c = min(l.Row, nRows-1), min(l.Col, nCols-1)
	rs, cs = max(1, l.RowSpan), max(1, l.ColSpan)
	rs, cs = min(rs, nRows-r), min(cs, nCols-c)
	return
}

// Measure measures every child once (caching its desired size — star-
// track children need this too, both for alignment and for nested
// grids to compute their own tracks), then sizes Auto tracks to the
// max desired of their span-1 children. Fixed+auto sum is the desired
// size; any starred grid asks for everything it's offered. Star track
// sizes stay zero here and resolve against the final extent in Arrange.
func (g *Grid) Measure(avail gooey.Size) gooey.Size {
	rows, cols := g.rows(), g.cols()
	desired := make([]gooey.Size, len(g.Children))
	for i, ch := range g.Children {
		desired[i] = gooey.MeasureChild(ch, avail)
	}
	track := func(defs []GridLen, dim func(i int) int, inTrack func(i, t int) bool) []int {
		out := make([]int, len(defs))
		for t, d := range defs {
			switch {
			case d.Auto:
				for i := range g.Children {
					if inTrack(i, t) {
						out[t] = max(out[t], dim(i))
					}
				}
			case d.Star > 0:
				out[t] = 0
			default:
				out[t] = d.Fixed
			}
		}
		return out
	}
	g.rowSz = track(rows,
		func(i int) int { return desired[i].H },
		func(i, t int) bool {
			r, _, rs, _ := cellOf(g.Children[i], len(rows), len(cols))
			return r == t && rs == 1
		})
	g.colSz = track(cols,
		func(i int) int { return desired[i].W },
		func(i, t int) bool {
			_, c, _, cs := cellOf(g.Children[i], len(rows), len(cols))
			return c == t && cs == 1
		})
	dw, dh := 0, 0
	starW, starH := false, false
	for i, c := range cols {
		dw += g.colSz[i]
		starW = starW || c.Star > 0
	}
	for i, r := range rows {
		dh += g.rowSz[i]
		starH = starH || r.Star > 0
	}
	if starW {
		dw = avail.W
	}
	if starH {
		dh = avail.H
	}
	return gooey.Size{W: min(dw, avail.W), H: min(dh, avail.H)}
}

func (g *Grid) Arrange(b gooey.Rect) {
	g.Base.Arrange(b)
	// No room means no room for anybody. Auto and fixed tracks come out
	// of the MEASURE cache, which an Arrange into nothing does not
	// refresh — so without this, a Grid arranged at a zero rect hands
	// its children the slots they had last time and their bounds never
	// change. That is not a cosmetic slip: the Composer's bounds sweep
	// is what erases a component's cells, and a child whose bounds did
	// not change is never swept, so the whole subtree stays on screen
	// after its ancestor went away. A Collapsed Tabs page rooted in a
	// Grid is exactly this case (pinned by
	// TestCollapsedGridZeroesItsSubtree).
	if b.W <= 0 || b.H <= 0 {
		for _, ch := range g.Children {
			gooey.ArrangeChild(ch, gooey.Rect{X: b.X, Y: b.Y})
		}
		return
	}
	rows, cols := g.rows(), g.cols()
	rowSz := distributeStars(rows, g.rowSz, b.H)
	colSz := distributeStars(cols, g.colSz, b.W)

	rowOff := offsets(rowSz, b.Y)
	colOff := offsets(colSz, b.X)
	for _, ch := range g.Children {
		r, c, rs, cs := cellOf(ch, len(rows), len(cols))
		slot := gooey.Rect{
			X: colOff[c], Y: rowOff[r],
			W: colOff[c+cs] - colOff[c],
			H: rowOff[r+rs] - rowOff[r],
		}
		gooey.ArrangeChild(ch, slot)
	}
}

// distributeStars gives star tracks their weighted share of the space
// left after fixed and auto tracks.
//
// base is the Measure pass's per-track sizes, and it may be SHORTER
// than defs — even empty — because Arrange can reach a Grid that was
// never measured: ArrangeChild short-circuits a Collapsed child
// straight to Arrange at a zero rect, and MeasureChild short-circuits
// the same child without ever calling Measure. So a Grid that is
// Collapsed on the frame it first appears (a hidden Tabs page, a
// Visibility="Collapsed" panel) arranges on an empty cache, and
// indexing base by track blindly panicked.
//
// Arrange's zero-rect guard above happens to catch every case the
// framework can reach today — unmeasured implies Collapsed implies a
// zero rect — so this padding is the second line, not the first. It
// stays because the function should be total for the cache it is
// handed rather than correct only under one caller's guard order:
// every track is legitimately zero in that state, and the next Measure
// refills the cache. TestCollapsedGridArrangesWithoutMeasure pins the
// pair — remove BOTH and it panics.
func distributeStars(defs []GridLen, base []int, extent int) []int {
	out := append([]int(nil), base...)
	for len(out) < len(defs) {
		out = append(out, 0)
	}
	used, weight := 0, 0.0
	for i, d := range defs {
		if d.Star > 0 {
			weight += d.Star
		} else {
			used += out[i]
		}
	}
	if weight == 0 {
		return clampToExtent(out, extent)
	}
	remaining := max(0, extent-used)
	given := 0
	last := -1
	for i, d := range defs {
		if d.Star > 0 {
			out[i] = int(float64(remaining) * d.Star / weight)
			given += out[i]
			last = i
		}
	}
	if last >= 0 { // hand rounding leftovers to the last star track
		out[last] += remaining - given
	}
	return clampToExtent(out, extent)
}

// clampToExtent makes the track sizes sum to no more than extent.
//
// Starving the star tracks is not enough on its own. `remaining` floors
// at zero, so once the FIXED and Auto tracks alone want more than the
// grid has, the stars correctly collapse and the fixed tracks keep
// their full demand anyway — and offsets() then walks the cumulative
// total straight past the grid's own edge. Every track from that point
// on is handed a rect outside the parent.
//
// That is not a cosmetic overrun. Nothing in the framework clips a
// component to its arranged rect — render.Cells.SetString clips to the
// BUFFER, not the parent — so a rect is a promise to paint there and
// nowhere else. A child given an out-of-bounds rect keeps that promise
// faithfully and paints over its neighbours, or off-screen entirely.
// Text.Render is the clearest case: it clips diligently to its own
// Bounds(), which by then is the wrong rectangle.
//
// Found in examples/wysiwyg, whose shell is Rows="1,1*,12,1" — fourteen
// rows of fixed demand. Above ~15 rows of terminal it is invisible;
// below, the 12-row markup pane runs past the bottom and the status bar
// is arranged entirely outside the screen.
//
// Truncating the straddling track rather than scaling every track is
// deliberate: a fixed track means "this many cells", and a grid that is
// out of room should show the first tracks at their stated size and
// lose the last, exactly as clipping text keeps the first lines. The
// alternative — shrinking everything proportionally — silently violates
// every fixed size on the page to honour a total that cannot be met.
func clampToExtent(sizes []int, extent int) []int {
	if extent < 0 {
		extent = 0
	}
	run := 0
	for i, s := range sizes {
		if s < 0 {
			sizes[i], s = 0, 0
		}
		switch {
		case run >= extent:
			sizes[i] = 0
		case run+s > extent:
			sizes[i] = extent - run
		}
		run += sizes[i]
	}
	return sizes
}

func offsets(sizes []int, start int) []int {
	out := make([]int, len(sizes)+1)
	out[0] = start
	for i, s := range sizes {
		out[i+1] = out[i] + s
	}
	return out
}

func (g *Grid) Render(f *gooey.Frame) {}
