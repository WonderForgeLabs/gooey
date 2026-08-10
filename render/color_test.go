package render

import (
	"bytes"
	"strings"
	"testing"
)

func TestQuantize256Cube(t *testing.T) {
	cases := []struct {
		name string
		c    Color
		want int
	}{
		// Cube corners are exact: index = 16 + 36r + 6g + b.
		{"black", RGB(0, 0, 0), 16},
		{"pure red", RGB(255, 0, 0), 196},     // 16 + 36*5
		{"pure green", RGB(0, 255, 0), 46},    // 16 + 6*5
		{"pure blue", RGB(0, 0, 255), 21},     // 16 + 5
		{"white", RGB(255, 255, 255), 231},    // 16 + 36*5 + 6*5 + 5
		{"cube level", RGB(95, 175, 215), 74}, // 16 + 36*1 + 6*3 + 4
		// The accent orange the demos use: 255→5, 170→3 (175 is nearer
		// than 135), 60→1 (95 is nearer than 0).
		{"accent orange", RGB(255, 170, 60), 215},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Quantize256(tc.c); got != tc.want {
				t.Errorf("Quantize256(%v) = %d, want %d", tc.c, got, tc.want)
			}
		})
	}
}

// A near-neutral color must reach the grayscale ramp rather than the
// cube: the ramp has 24 steps against the cube's 6, so this is the
// difference between a visible banding artifact and a clean gray.
func TestQuantize256PrefersGrayscaleRamp(t *testing.T) {
	cases := []struct {
		c    Color
		want int
	}{
		{RGB(8, 8, 8), 232},       // first ramp step, exact
		{RGB(128, 128, 128), 244}, // 8+10*12 = 128, exact
		{RGB(238, 238, 238), 255}, // last ramp step, exact
	}
	for _, tc := range cases {
		if got := Quantize256(tc.c); got != tc.want {
			t.Errorf("Quantize256(%v) = %d, want %d (grayscale ramp)", tc.c, got, tc.want)
		}
	}
	// 128 gray via the cube would be level 135 in all three channels —
	// index 16+36*2+6*2+2 = 102. Confirm we did NOT pick that.
	if got := Quantize256(RGB(128, 128, 128)); got == 102 {
		t.Error("mid gray quantized to the cube diagonal, not the ramp")
	}
}

func TestQuantize16(t *testing.T) {
	cases := []struct {
		name string
		c    Color
		want int
		show string
	}{
		{"black", RGB(0, 0, 0), 0, "black"},
		{"pure red", RGB(255, 0, 0), 9, "bright red"},
		{"dim red", RGB(205, 0, 0), 1, "red"},
		{"white", RGB(255, 255, 255), 15, "bright white"},
		{"mid gray", RGB(127, 127, 127), 8, "bright black"},
		// Orange has no ANSI slot. Dim yellow (205,205,0) beats bright
		// yellow (255,255,0) because the green channel is the dominant
		// term: |170-205| = 35 against |170-255| = 85. That is the
		// honest answer a 16-color terminal gives, and it is why the
		// picker names the color rather than just showing a swatch.
		{"accent orange", RGB(255, 170, 60), 3, "yellow"},
		{"panel violet", RGB(120, 90, 220), 12, "bright blue"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Quantize16(tc.c)
			if got != tc.want {
				t.Errorf("Quantize16(%v) = %d (%s), want %d (%s)",
					tc.c, got, ANSI16Name(got), tc.want, tc.show)
			}
			if name := ANSI16Name(got); name != tc.show {
				t.Errorf("ANSI16Name(%d) = %q, want %q", got, name, tc.show)
			}
		})
	}
}

// Approximate is what a component shows the user as "what your terminal
// will really display", so it must be the exact inverse of the
// quantization the flush performs.
func TestApproximateRoundTripsQuantization(t *testing.T) {
	c := RGB(255, 170, 60)
	if got := Approximate(c, TrueColor); got != c {
		t.Errorf("truecolor changed the color: %v", got)
	}
	if got, want := Approximate(c, Color256), palette256(215); got != want {
		t.Errorf("Approximate(256) = %v, want %v", got, want)
	}
	if got, want := Approximate(c, Color16), RGB(205, 205, 0); got != want {
		t.Errorf("Approximate(16) = %v, want %v (yellow)", got, want)
	}
	// An unset color is "terminal default" and must survive untouched at
	// every depth — quantizing it would turn "default" into black.
	for _, d := range []ColorDepth{TrueColor, Color256, Color16} {
		if got := Approximate(Color{}, d); got.Set {
			t.Errorf("depth %s turned an unset color into %v", d, got)
		}
	}
}

// The three depths are different escape grammars, not truncations of one
// another. This is the wire contract.
func TestFlushEmitsPerDepthEscapes(t *testing.T) {
	// Foreground is the accent orange, background pure blue.
	//   256: orange → cube 215, blue → cube 21.
	//   16:  orange → yellow (3) → fg 33; blue → blue (4) → bg 44.
	cases := []struct {
		depth ColorDepth
		want  string
	}{
		{TrueColor, "\x1b[0;38;2;255;170;60;48;2;0;0;255m"},
		{Color256, "\x1b[0;38;5;215;48;5;21m"},
		{Color16, "\x1b[0;33;44m"},
	}
	for _, tc := range cases {
		t.Run(tc.depth.String(), func(t *testing.T) {
			b := NewBuffer(1, 1)
			b.Set(0, 0, 'x', Style{Fg: RGB(255, 170, 60), Bg: RGB(0, 0, 255)})
			var out bytes.Buffer
			if err := Flush(&out, b, tc.depth); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			if !strings.Contains(got, tc.want) {
				t.Errorf("depth %s: want SGR %q in %q", tc.depth, tc.want, got)
			}
		})
	}
}

// The buffer is always 24-bit; depth is a property of the wire. Flushing
// the same buffer twice at two depths must leave the cells untouched.
func TestDepthDoesNotMutateTheBuffer(t *testing.T) {
	b := NewBuffer(1, 1)
	want := Style{Fg: RGB(255, 170, 60)}
	b.Set(0, 0, 'x', want)
	for _, d := range []ColorDepth{Color16, Color256, TrueColor} {
		Flush(&bytes.Buffer{}, b, d)
	}
	if got := b.At(0, 0).Style; got != want {
		t.Errorf("buffer style became %v after flushing, want %v", got, want)
	}
}

func TestParseColorDepth(t *testing.T) {
	for in, want := range map[string]ColorDepth{
		"truecolor": TrueColor, "24bit": TrueColor, "256": Color256, "16": Color16,
	} {
		got, ok := ParseColorDepth(in)
		if !ok || got != want {
			t.Errorf("ParseColorDepth(%q) = %v, %v; want %v, true", in, got, ok, want)
		}
	}
	if _, ok := ParseColorDepth("lots"); ok {
		t.Error("ParseColorDepth accepted nonsense")
	}
}
