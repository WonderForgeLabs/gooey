package gooey

import (
	"fmt"

	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// ColorPicker edits an RGB color through three channel bars, and is the
// framework's worked example of a widget whose EXPERIENCE adapts to the
// terminal it landed on.
//
// The adaptation is not a fallback ladder where lesser terminals get a
// degraded version of one design. Each tier answers the question the
// user actually has on that terminal:
//
//   - TrueColor: the bars are smooth gradients — every cell painted with
//     the color that position would produce — because a 24-bit terminal
//     can show the answer directly. The swatch is the color, exactly.
//   - Color256: the same gradients, but the flush quantizes them to the
//     xterm cube, so they band into visible steps. Since the terminal
//     CANNOT show the requested color, the picker stops pretending it
//     can and says what it will really get: the palette index, beside
//     the requested hex.
//   - Color16: a gradient would be a lie across 16 buckets, so the bar
//     becomes a plain fill meter and the readout names the ANSI color
//     the value collapses to. On this terminal the useful question is
//     "which of the sixteen is this?", so that is what it answers.
//
// It reads the tier from Frame.Caps at Render (capabilities are a plain
// field on the frame, not a property — they cannot change mid-session),
// so the same widget instance is correct on any terminal without the
// app configuring anything.
type ColorPicker struct {
	Base
	FocusState
	HoverState
	Value *prop.Property[render.Color]

	channel *prop.Property[int] // 0=R, 1=G, 2=B — a source, so moving between bars is damage
}

// Channel constants for the three bars, top to bottom.
const (
	channelR = 0
	channelG = 1
	channelB = 2
)

// Geometry. The widget is three bar rows, a blank, and the readout row.
const (
	pickerLabelW   = 2 // "R "
	pickerReadoutW = 4 // " 255"
	pickerRows     = 5
	pickerPrefW    = 30
)

func (p *ColorPicker) chanProp() *prop.Property[int] {
	if p.channel == nil {
		p.channel = prop.NewSource(0)
	}
	return p.channel
}

// Channel is the bar currently being edited.
func (p *ColorPicker) Channel() int { return p.chanProp().Get() }

// Color is the current value, defaulting to mid gray when unbound so an
// unbound picker is still legible rather than black-on-black.
func (p *ColorPicker) Color() render.Color {
	if p.Value == nil {
		return render.RGB(128, 128, 128)
	}
	c := p.Value.Get()
	if !c.Set {
		return render.RGB(0, 0, 0)
	}
	return c
}

// Hex renders the current color in the form users copy and paste.
func (p *ColorPicker) Hex() string {
	c := p.Color()
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

func (p *ColorPicker) component(c render.Color, ch int) uint8 {
	switch ch {
	case channelG:
		return c.G
	case channelB:
		return c.B
	}
	return c.R
}

// withComponent returns c with one channel replaced — the pure function
// the gradient painter sweeps and the edit commands Set.
func (p *ColorPicker) withComponent(c render.Color, ch, v int) render.Color {
	v = clamp(v, 0, 255)
	switch ch {
	case channelG:
		c.G = uint8(v)
	case channelB:
		c.B = uint8(v)
	default:
		c.R = uint8(v)
	}
	c.Set = true
	return c
}

// SetChannelValue writes one channel of the bound color.
func (p *ColorPicker) SetChannelValue(ch, v int) {
	if p.Value == nil {
		return
	}
	p.Value.Set(p.withComponent(p.Color(), ch, v))
}

// Adjust moves the current channel by delta.
func (p *ColorPicker) Adjust(delta int) {
	ch := p.Channel()
	p.SetChannelValue(ch, int(p.component(p.Color(), ch))+delta)
}

func (p *ColorPicker) selectChannel(ch int) { p.chanProp().Set(clamp(ch, 0, 2)) }

func (p *ColorPicker) Measure(avail Size) Size {
	return Size{min(pickerPrefW, avail.W), min(pickerRows, avail.H)}
}

// barWidth is the cell span of the gradient/meter part of a row.
func (p *ColorPicker) barWidth() int {
	return max(0, p.bounds.W-pickerLabelW-pickerReadoutW)
}

func (p *ColorPicker) Render(f *Frame) {
	b := p.bounds
	depth := f.Depth()
	cur := p.Color()
	sel := p.Channel()
	barW := p.barWidth()

	for ch := 0; ch < 3; ch++ {
		y := b.Y + ch
		if y >= b.Y+b.H {
			break
		}
		v := int(p.component(cur, ch))

		labelSt := styleDim
		if ch == sel {
			labelSt = render.Style{Bold: true}
			if p.IsFocused() {
				labelSt.Reverse = true
			}
		}
		f.Cells.SetString(b.X, y, clipRunes("RGB"[ch:ch+1]+" ", b.W), labelSt)

		p.renderBar(f, b.X+pickerLabelW, y, barW, cur, ch, v, depth)

		if x := b.X + pickerLabelW + barW; x < b.X+b.W {
			f.Cells.SetString(x, y, clipRunes(fmt.Sprintf("%4d", v), b.X+b.W-x), styleDim)
		}
	}

	p.renderReadout(f, b.X, b.Y+4, b.W, cur, depth)
}

// renderBar paints one channel row. The tier decides what a bar MEANS:
// a swept gradient where the terminal can show one, a fill meter where
// it cannot.
func (p *ColorPicker) renderBar(f *Frame, x, y, w int, cur render.Color, ch, v int, depth render.ColorDepth) {
	if w <= 0 {
		return
	}
	pos := v * (w - 1) / 255 // cell holding the current value
	for i := 0; i < w; i++ {
		var st render.Style
		r := '█'
		switch depth {
		case render.Color16:
			// No usable gradient in 16 colors: a fill meter, colored by
			// the ANSI color the CURRENT value maps to, so the bar and
			// the name below always agree.
			a := render.Approximate(cur, render.Color16)
			if i <= pos {
				st = render.Style{Fg: a}
			} else {
				st = styleDim
				r = '░'
			}
		default:
			// Sweep this channel across the bar, holding the others: cell
			// i is painted with the color choosing position i would give.
			swept := p.withComponent(cur, ch, i*255/max(1, w-1))
			st = render.Style{Fg: swept}
		}
		f.Cells.Set(x+i, y, r, st)
	}
	// The cursor: a contrasting cell marking where the value sits. It
	// reads on every tier because it is a rune change, not a color one.
	if p.IsFocused() && ch == p.Channel() {
		f.Cells.Set(x+pos, y, '▮', render.Style{Fg: render.RGB(255, 255, 255), Bold: true})
	} else {
		f.Cells.Set(x+pos, y, '│', render.Style{Fg: render.RGB(255, 255, 255)})
	}
}

// renderReadout is the swatch and the truth about what the terminal will
// actually display.
func (p *ColorPicker) renderReadout(f *Frame, x, y, w int, cur render.Color, depth render.ColorDepth) {
	if w <= 0 {
		return
	}
	// A truecolor terminal gets a wider swatch: it is the one tier where
	// the swatch is exactly the answer, so it earns the space.
	swatchW := 4
	if depth == render.TrueColor {
		swatchW = 8
	}
	swatchW = min(swatchW, w)
	shown := render.Approximate(cur, depth)
	for i := 0; i < swatchW; i++ {
		f.Cells.Set(x+i, y, '█', render.Style{Fg: shown})
	}

	var label string
	switch depth {
	case render.Color256:
		label = fmt.Sprintf(" %s → xterm %d", p.Hex(), render.Quantize256(cur))
	case render.Color16:
		label = fmt.Sprintf(" %s ≈ %s", p.Hex(), render.ANSI16Name(render.Quantize16(cur)))
	default:
		label = " " + p.Hex()
	}
	if tx := x + swatchW; tx < x+w {
		f.Cells.SetString(tx, y, clipRunes(label, x+w-tx), render.Style{Bold: true})
	}
}

// HandleKey: up/down pick a channel, left/right adjust it. Shift makes
// the step coarse (16), home/end saturate. Consuming up/down means the
// picker keeps vertical arrows while focused — the listbox convention —
// and tab still leaves.
func (p *ColorPicker) HandleKey(ev input.KeyEvent) bool {
	step := 1
	if ev.Has(input.ModShift) {
		step = 16
	}
	switch {
	case ev.Key == input.KeyUp:
		p.selectChannel(p.Channel() - 1)
	case ev.Key == input.KeyDown:
		p.selectChannel(p.Channel() + 1)
	case ev.Key == input.KeyLeft:
		p.Adjust(-step)
	case ev.Key == input.KeyRight:
		p.Adjust(step)
	case ev == input.Rune('k'):
		p.selectChannel(p.Channel() - 1)
	case ev == input.Rune('j'):
		p.selectChannel(p.Channel() + 1)
	case ev == input.Rune('h'):
		p.Adjust(-1)
	case ev == input.Rune('l'):
		p.Adjust(1)
	case ev == input.Rune('H'):
		p.Adjust(-16)
	case ev == input.Rune('L'):
		p.Adjust(16)
	case ev.Key == input.KeyHome:
		p.SetChannelValue(p.Channel(), 0)
	case ev.Key == input.KeyEnd:
		p.SetChannelValue(p.Channel(), 255)
	default:
		return false
	}
	return true
}

// HandleMouse: clicking a bar sets that channel from the click position,
// and the wheel over a bar nudges it. Both select the row they land on,
// so the pointer and the keyboard agree about which channel is current.
func (p *ColorPicker) HandleMouse(ev input.MouseEvent) bool {
	ch := ev.Y - p.bounds.Y
	if ch < 0 || ch > 2 {
		return false
	}
	switch ev.Kind {
	case input.MousePress, input.MouseClick:
		w := p.barWidth()
		i := ev.X - (p.bounds.X + pickerLabelW)
		if w <= 0 || i < 0 || i >= w {
			return false
		}
		p.selectChannel(ch)
		p.SetChannelValue(ch, i*255/max(1, w-1))
		return true
	case input.WheelUp, input.WheelDown:
		p.selectChannel(ch)
		d := 1
		if ev.Kind == input.WheelDown {
			d = -1
		}
		if ev.Mods&input.ModShift != 0 {
			d *= 16
		}
		p.SetChannelValue(ch, int(p.component(p.Color(), ch))+d)
		return true
	}
	return false
}
