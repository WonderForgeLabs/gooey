package components

import (
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// ProgressBar reports how far along a task is — a 0-100 meter when the
// answer is known, a marching band when it is not.
//
// It is Gauge's sibling and deliberately not Gauge: a Gauge answers "how
// much of this resource is in use", so it is labelled and always has a
// number. Progress answers "how much of this work is left", which
// sometimes has no number at all. That second mode is the whole reason
// this component owns a goroutine and Gauge does not.
//
// Indeterminate animation follows the Timer discipline exactly. The
// ticker never touches the graph: it posts a step onto the UI goroutine,
// and the step reads Indeterminate there — at fire time, on the loop —
// so a bar that is currently determinate advances nothing and dirties
// nothing. Lifetime belongs to the Composer, which starts every
// Startable it walks and stops them on Close, so a bar that leaves the
// tree cannot leave a ticker behind.
//
// A ProgressBar with no Indeterminate property at all starts no
// goroutine: nothing about it can ever animate, so there is nothing to
// run.
type ProgressBar struct {
	gooey.Base
	Value         *prop.Property[int]  // 0-100; clamped on read
	Indeterminate *prop.Property[bool] // nil = always determinate
	Label         *prop.Property[string]
	// Style overrides the coloring entirely, the way Gauge's does.
	Style *prop.Property[render.Style]
	// Thresholds colors the bar with the shared good/warn/crit ramp
	// instead of a flat accent.
	//
	// It is opt-in, which is the one place this component deliberately
	// differs from Gauge. A Gauge shows utilization, where a high number
	// is a warning and the ramp IS the meaning. Progress is the
	// opposite: 96% done is the best news the bar has all day, and
	// painting it crit-red says the reverse. So the ramp stays available
	// — for the bars where the value really is a fill approaching a
	// limit, a disk filling or a quota being used up — and stays off by
	// default.
	Thresholds bool
	Width      int           // preferred width in cells; 0 = 34
	Tick       time.Duration // indeterminate step; 0 = 80ms

	phase *prop.Property[int] // source: the marching band's position
}

// IndeterminateTick is the default animation step.
const IndeterminateTick = 80 * time.Millisecond

func (p *ProgressBar) value() int { return meterValue(p.Value) }

// busy reports whether the bar is in its indeterminate mode. Read during
// Render it is a paint dependency, so flipping the mode repaints this one
// component; read at fire time it is just a question, which is what keeps
// the ticker from being a dependency of anything.
func (p *ProgressBar) busy() bool {
	return p.Indeterminate != nil && p.Indeterminate.Get()
}

func (p *ProgressBar) phaseProp() *prop.Property[int] {
	if p.phase == nil {
		p.phase = prop.NewSource(0)
	}
	return p.phase
}

func (p *ProgressBar) interval() time.Duration {
	if p.Tick <= 0 {
		return IndeterminateTick
	}
	return p.Tick
}

// Start runs the animation ticker. A bar that can never be
// indeterminate declines to start at all.
func (p *ProgressBar) Start(post func(func())) func() {
	if p.Indeterminate == nil {
		return func() {}
	}
	// gooey.Every owns the close-and-join contract — see startable.go.
	return gooey.Every(post, p.interval(), p.step)
}

// step runs on the UI goroutine. A determinate bar advances nothing, so
// a paused animation costs exactly one property read per tick and zero
// damage.
func (p *ProgressBar) step() {
	if !p.busy() {
		return
	}
	p.phaseProp().Set(p.phaseProp().Get() + 1)
}

func (p *ProgressBar) Measure(avail gooey.Size) gooey.Size { return meterSize(p.Width, avail) }

func (p *ProgressBar) Render(f *gooey.Frame) {
	b := p.Bounds()
	if b.W <= 0 || b.H <= 0 {
		return
	}
	label := getStr(p.Label)
	x := b.X
	if label != "" {
		f.Cells.SetString(x, b.Y, clipRunes(label, b.W), styleDim)
		x += len([]rune(label))
	}
	if p.busy() {
		p.renderBusy(f, x, b.Y, b.X+b.W-x)
		return
	}
	p.renderValue(f, x, b.Y, b.X+b.W-x)
}

// renderValue is the determinate bar: a fill and a percentage.
func (p *ProgressBar) renderValue(f *gooey.Frame, x, y, w int) {
	v := p.value()
	st := styleGood
	if p.Thresholds {
		st = thresholdStyle(v)
	}
	if p.Style != nil {
		st = p.Style.Get()
	}
	// The whole remainder is track, less the reserved readout; the empty
	// half is dimmed, so a Progress track reads as work not yet done
	// rather than as a second color of progress.
	rx := x + renderFillMeter(f, x, y, w-meterReadout, v, st, styleDim)
	renderMeterReadout(f, rx, y, x+w-rx, v, st)
}

// renderBusy is the indeterminate bar: a band of fill marching across the
// track and wrapping, which says "working" without claiming a number.
func (p *ProgressBar) renderBusy(f *gooey.Frame, x, y, w int) {
	if w <= 0 {
		return
	}
	st := styleGood
	if p.Style != nil {
		st = p.Style.Get()
	}
	band := max(1, w/4)
	pos := p.phaseProp().Get() % w
	lit := make([]bool, w)
	for i := 0; i < band; i++ {
		lit[(pos+i)%w] = true
	}
	for i := 0; i < w; i++ {
		if lit[i] {
			f.Cells.Set(x+i, y, '█', st)
		} else {
			f.Cells.Set(x+i, y, '░', styleDim)
		}
	}
}
