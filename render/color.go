package render

// Color depth is a property of the WIRE, not of the buffer. A Buffer
// always holds 24-bit Colors; quantization happens once, in Flush, on
// the way out. That split is deliberate: a widget that picks
// RGB(255,170,60) writes that value on every terminal, and asking "what
// will this actually look like?" is a question only the flush — or a
// widget that deliberately previews the answer, like ColorPicker — has
// to ask.

// ColorDepth is how many colors the terminal can display.
type ColorDepth uint8

const (
	// TrueColor emits 24-bit SGR (38;2;r;g;b). It is the zero value, so
	// a Buffer flushed without any capability detection behaves exactly
	// as it did before depth existed.
	TrueColor ColorDepth = iota
	// Color256 quantizes to the xterm-256 palette: the 6×6×6 cube at
	// 16-231 plus the 24-step grayscale ramp at 232-255.
	Color256
	// Color16 quantizes to the 8 ANSI colors and their bright variants.
	Color16
)

func (d ColorDepth) String() string {
	switch d {
	case Color256:
		return "256"
	case Color16:
		return "16"
	}
	return "truecolor"
}

// ParseColorDepth is the inverse of String, for demo flags that force a
// tier so the difference can be recorded on a terminal that has more.
func ParseColorDepth(s string) (ColorDepth, bool) {
	switch s {
	case "truecolor", "24bit", "24":
		return TrueColor, true
	case "256":
		return Color256, true
	case "16":
		return Color16, true
	}
	return TrueColor, false
}

// cubeLevels are the six per-channel values of the xterm-256 color
// cube. They are not evenly spaced — the gap from 0 to 95 is the reason
// dark colors quantize worse than light ones.
var cubeLevels = [6]int{0, 95, 135, 175, 215, 255}

// Quantize256 returns the xterm-256 palette index nearest to c. It
// considers both the color cube and the grayscale ramp and returns
// whichever is closer, which matters for near-neutral colors: the ramp
// has 24 steps where the cube has 6, so a mid gray is far better served
// by 232-255 than by the cube's diagonal.
func Quantize256(c Color) int {
	r, g, b := int(c.R), int(c.G), int(c.B)

	ri, gi, bi := nearestLevel(r), nearestLevel(g), nearestLevel(b)
	cubeDist := dist(r, g, b, cubeLevels[ri], cubeLevels[gi], cubeLevels[bi])

	// Grayscale ramp: level i is 8+10i for i in 0..23.
	avg := (r + g + b) / 3
	gi24 := (avg - 8 + 5) / 10
	gi24 = clampInt(gi24, 0, 23)
	gv := 8 + 10*gi24
	grayDist := dist(r, g, b, gv, gv, gv)

	if grayDist < cubeDist {
		return 232 + gi24
	}
	return 16 + 36*ri + 6*gi + bi
}

func nearestLevel(v int) int {
	best, bestD := 0, 1<<30
	for i, l := range cubeLevels {
		if d := abs(v - l); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// ansi16 are xterm's default RGB values for the 16 ANSI colors. The
// mapping is only as honest as these numbers — a terminal with a custom
// palette will show something else — but they are the values every
// nearest-color table in the wild is built from.
var ansi16 = [16]struct {
	r, g, b int
	name    string
}{
	{0, 0, 0, "black"},
	{205, 0, 0, "red"},
	{0, 205, 0, "green"},
	{205, 205, 0, "yellow"},
	{0, 0, 238, "blue"},
	{205, 0, 205, "magenta"},
	{0, 205, 205, "cyan"},
	{229, 229, 229, "white"},
	{127, 127, 127, "bright black"},
	{255, 0, 0, "bright red"},
	{0, 255, 0, "bright green"},
	{255, 255, 0, "bright yellow"},
	{92, 92, 255, "bright blue"},
	{255, 0, 255, "bright magenta"},
	{0, 255, 255, "bright cyan"},
	{255, 255, 255, "bright white"},
}

// Quantize16 returns the index (0-15) of the nearest ANSI color by
// squared RGB distance — unweighted, so it is predictable and testable
// rather than perceptually tuned.
func Quantize16(c Color) int {
	best, bestD := 0, 1<<30
	for i, a := range ansi16 {
		if d := dist(int(c.R), int(c.G), int(c.B), a.r, a.g, a.b); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// ANSI16Name is the human name of an ANSI color index — what a
// 16-color terminal can honestly claim to be showing.
func ANSI16Name(i int) string {
	if i < 0 || i >= len(ansi16) {
		return "?"
	}
	return ansi16[i].name
}

// Approximate returns the color the terminal will ACTUALLY display for c
// at the given depth. It is the round trip quantization implies, and it
// is what lets a widget show a user the truth about their own terminal
// instead of the color they asked for.
func Approximate(c Color, d ColorDepth) Color {
	if !c.Set {
		return c
	}
	switch d {
	case Color256:
		return palette256(Quantize256(c))
	case Color16:
		a := ansi16[Quantize16(c)]
		return RGB(uint8(a.r), uint8(a.g), uint8(a.b))
	}
	return c
}

// palette256 is the inverse of Quantize256: the RGB an xterm-256 index
// stands for.
func palette256(i int) Color {
	switch {
	case i >= 232:
		v := uint8(8 + 10*(i-232))
		return RGB(v, v, v)
	case i >= 16:
		n := i - 16
		return RGB(
			uint8(cubeLevels[n/36]),
			uint8(cubeLevels[(n/6)%6]),
			uint8(cubeLevels[n%6]),
		)
	default:
		a := ansi16[i]
		return RGB(uint8(a.r), uint8(a.g), uint8(a.b))
	}
}

func dist(r1, g1, b1, r2, g2, b2 int) int {
	dr, dg, db := r1-r2, g1-g2, b1-b2
	return dr*dr + dg*dg + db*db
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
