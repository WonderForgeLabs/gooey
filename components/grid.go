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
func distributeStars(defs []GridLen, base []int, extent int) []int {
	out := append([]int(nil), base...)
	used, weight := 0, 0.0
	for i, d := range defs {
		if d.Star > 0 {
			weight += d.Star
		} else {
			used += out[i]
		}
	}
	if weight == 0 {
		return out
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
	return out
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
