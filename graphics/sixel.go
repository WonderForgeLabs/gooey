package graphics

import (
	"fmt"
	"image"
	"sort"
)

// Sixel encodes via the DEC sixel protocol (DCS q ... ST).
//
// # The palette is adaptive, and that is the whole quality story
//
// Sixel names colors by REGISTER: the stream declares up to 256 of them
// and every pixel then refers to one. So the picture's fidelity is
// decided entirely by which 256 colors get declared, and there are two
// ways to choose them.
//
// This encoder used to choose them the cheap way — a fixed 6x6x6 cube,
// 216 registers at evenly spaced levels, every image quantized to the
// same grid whatever it contained. That is a 40-step error per channel in
// the worst case, and it is worst exactly where UI chrome lives: a
// two-tone icon whose two tones are both off, a gradient turned into
// visible bands.
//
// It now chooses them from the IMAGE:
//
//   - if the picture has 256 or fewer distinct colors — which is every
//     piece of interface chrome ever drawn — all of them are declared and
//     the encoding is LOSSLESS. Nothing is approximated at all.
//   - otherwise a median cut splits the color space until 256 boxes
//     remain and each box contributes its own weighted average, so the
//     registers land where the picture actually has colors.
//
// # The unit of "distinct" is the sixel color space, not 24-bit RGB
//
// Sixel declares a register as three values 0..100, not 0..255, so two
// 24-bit colors one step apart are the SAME sixel color. Counting
// distinct colors in 24-bit space would therefore overcount — a 200-color
// gradient could look like 400 and be pushed into median cut for no
// reason. Everything below counts and cuts in the 101-level space the
// protocol actually has.
//
// # No dithering, deliberately
//
// Error diffusion needs a nearest-register lookup for colors that are not
// in the image (the error pushes them off-palette), which means either a
// per-pixel search over 256 registers or a 32768-entry lookup table built
// per frame. Both cost more per frame than the encode itself. With an
// adaptive palette the banding dithering exists to hide is mostly gone,
// and the case this encoder is actually asked to draw — chrome — is
// lossless. If a photo-heavy consumer appears, that is when to pay for it.
type Sixel struct{}

func (Sixel) Name() string { return "sixel" }

// maxRegisters is the sixel register count this encoder will use. 256 is
// the number every sixel implementation supports; some support more, and
// none can be relied on to.
const maxRegisters = 256

// sixelLevels is the protocol's per-channel resolution: values 0..100
// inclusive.
const sixelLevels = 101

func (Sixel) Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error {
	pxW, pxH := cols*cellW, rows*cellH
	if pxW <= 0 || pxH <= 0 {
		return fmt.Errorf("graphics: sixel: empty target %dx%d (cell size unknown?)", pxW, pxH)
	}
	rgba := Scale(img, pxW, pxH)

	// One pass to the protocol's own color space, counting as it goes.
	// key is the 0..100 triple packed; counts feeds the median cut's
	// weighting, so a color covering half the image pulls a box toward it.
	keys := make([]int32, pxW*pxH)
	counts := map[int32]int{}
	for y := 0; y < pxH; y++ {
		for x := 0; x < pxW; x++ {
			r, g, b, _ := rgba.At(x, y).RGBA()
			k := packSixel(to100(uint8(r>>8)), to100(uint8(g>>8)), to100(uint8(b>>8)))
			keys[y*pxW+x] = k
			counts[k]++
		}
	}

	palette, index := buildPalette(counts)

	// The index plane: every pixel's register, resolved through the map
	// the palette builder returned. No nearest-color search anywhere —
	// each distinct color was assigned when the palette was built.
	idx := make([]uint8, len(keys))
	for i, k := range keys {
		idx[i] = index[k]
	}

	buf := append(*out, "\x1bP0;0;0q"...)
	buf = append(buf, fmt.Sprintf("\"1;1;%d;%d", pxW, pxH)...)
	for i, c := range palette {
		r, g, b := unpackSixel(c)
		buf = append(buf, fmt.Sprintf("#%d;2;%d;%d;%d", i, r, g, b)...)
	}

	// Bands of 6 vertical pixels, one pass per register per band.
	//
	// Only the registers PRESENT in a band are walked. With an adaptive
	// palette that matters more than it did: a 256-color palette over a
	// mostly flat band would otherwise emit 256 empty passes per band.
	line := make([]byte, pxW)
	for y0 := 0; y0 < pxH; y0 += 6 {
		first := true
		for _, reg := range registersInBand(idx, pxW, pxH, y0, len(palette)) {
			any := false
			for x := 0; x < pxW; x++ {
				var bits byte
				for dy := 0; dy < 6 && y0+dy < pxH; dy++ {
					if idx[(y0+dy)*pxW+x] == reg {
						bits |= 1 << dy
					}
				}
				line[x] = bits
				if bits != 0 {
					any = true
				}
			}
			if !any {
				continue
			}
			if !first {
				buf = append(buf, '$') // carriage return within band
			}
			first = false
			buf = append(buf, fmt.Sprintf("#%d", reg)...)
			buf = appendRLE(buf, line)
		}
		buf = append(buf, '-') // next band
	}
	buf = append(buf, "\x1b\\"...)
	*out = buf
	return nil
}

// registersInBand returns the registers this band actually uses, in
// ascending order. Determinism matters: the same image must produce the
// same bytes, or damage-tracked flushing sees a change where there is
// none. (Map iteration order is why the previous version could emit a
// different byte stream for an identical picture.)
func registersInBand(idx []uint8, pxW, pxH, y0, n int) []uint8 {
	seen := make([]bool, n)
	for dy := 0; dy < 6 && y0+dy < pxH; dy++ {
		row := (y0 + dy) * pxW
		for x := 0; x < pxW; x++ {
			seen[idx[row+x]] = true
		}
	}
	out := make([]uint8, 0, n)
	for i, ok := range seen {
		if ok {
			out = append(out, uint8(i))
		}
	}
	return out
}

// to100 maps an 8-bit channel to the protocol's 0..100, rounding rather
// than truncating: truncation darkens every channel by up to a full step,
// which on a flat UI background is a visible shift.
func to100(v uint8) uint8 { return uint8((int(v)*100 + 127) / 255) }

func packSixel(r, g, b uint8) int32 {
	return int32(r)*sixelLevels*sixelLevels + int32(g)*sixelLevels + int32(b)
}

func unpackSixel(k int32) (r, g, b int) {
	return int(k) / (sixelLevels * sixelLevels), (int(k) / sixelLevels) % sixelLevels, int(k) % sixelLevels
}

// buildPalette returns the registers to declare and the map from every
// color in the image to its register.
//
// The exact branch is the one that matters for this framework: interface
// chrome has few colors, so it takes it, and the result is not an
// approximation of the image — it IS the image.
func buildPalette(counts map[int32]int) ([]int32, map[int32]uint8) {
	distinct := make([]int32, 0, len(counts))
	for k := range counts {
		distinct = append(distinct, k)
	}
	// Sorted so the register numbering — and therefore the emitted bytes —
	// is a function of the image alone.
	sort.Slice(distinct, func(i, j int) bool { return distinct[i] < distinct[j] })

	if len(distinct) <= maxRegisters {
		index := make(map[int32]uint8, len(distinct))
		for i, k := range distinct {
			index[k] = uint8(i)
		}
		return distinct, index
	}
	return medianCut(distinct, counts)
}

// colorBox is a region of the color space holding some of the image's
// colors, waiting to be split or averaged.
type colorBox struct {
	colors []int32
	// weight is how many PIXELS the box covers, not how many colors. A
	// box holding one color that fills half the screen must not be split
	// last just because it is one color.
	weight int
}

// medianCut splits the color space until maxRegisters boxes remain, each
// box contributing one register.
//
// The split rule is the classic one: take the box with the widest spread
// on any channel, sort its colors on that channel, and cut at the
// weighted median so both halves carry a similar share of the picture.
func medianCut(distinct []int32, counts map[int32]int) ([]int32, map[int32]uint8) {
	total := 0
	for _, k := range distinct {
		total += counts[k]
	}
	boxes := []colorBox{{colors: distinct, weight: total}}

	for len(boxes) < maxRegisters {
		bi, ch, spread := widestBox(boxes)
		if bi < 0 || spread == 0 {
			break // every box is a single color; nothing left to split
		}
		a, b := splitBox(boxes[bi], ch, counts)
		if len(a.colors) == 0 || len(b.colors) == 0 {
			break
		}
		boxes[bi] = a
		boxes = append(boxes, b)
	}

	palette := make([]int32, len(boxes))
	index := make(map[int32]uint8, len(distinct))
	for i, box := range boxes {
		palette[i] = averageOf(box, counts)
		for _, k := range box.colors {
			index[k] = uint8(i)
		}
	}
	return palette, index
}

// widestBox finds the box and channel with the largest spread — the next
// split, and the one that removes the most error.
func widestBox(boxes []colorBox) (idx, channel, spread int) {
	idx, spread = -1, 0
	for i, box := range boxes {
		if len(box.colors) < 2 {
			continue
		}
		for c := 0; c < 3; c++ {
			lo, hi := 255, 0
			for _, k := range box.colors {
				v := channelOf(k, c)
				if v < lo {
					lo = v
				}
				if v > hi {
					hi = v
				}
			}
			if hi-lo > spread {
				idx, channel, spread = i, c, hi-lo
			}
		}
	}
	return idx, channel, spread
}

func splitBox(box colorBox, channel int, counts map[int32]int) (colorBox, colorBox) {
	cs := make([]int32, len(box.colors))
	copy(cs, box.colors)
	sort.Slice(cs, func(i, j int) bool {
		ci, cj := channelOf(cs[i], channel), channelOf(cs[j], channel)
		if ci != cj {
			return ci < cj
		}
		return cs[i] < cs[j] // ties broken by value, so the cut is deterministic
	})
	// Cut where half the PIXELS lie, not half the colors.
	half, run, cut := box.weight/2, 0, 1
	for i, k := range cs {
		run += counts[k]
		if run >= half && i+1 < len(cs) {
			cut = i + 1
			break
		}
	}
	lo := colorBox{colors: cs[:cut]}
	hi := colorBox{colors: cs[cut:]}
	for _, k := range lo.colors {
		lo.weight += counts[k]
	}
	for _, k := range hi.colors {
		hi.weight += counts[k]
	}
	return lo, hi
}

// averageOf is the box's register: the pixel-weighted mean of its colors,
// so the declared color sits where the picture's mass is rather than in
// the geometric middle of the box.
func averageOf(box colorBox, counts map[int32]int) int32 {
	var sr, sg, sb, n int
	for _, k := range box.colors {
		w := counts[k]
		r, g, b := unpackSixel(k)
		sr, sg, sb, n = sr+r*w, sg+g*w, sb+b*w, n+w
	}
	if n == 0 {
		return box.colors[0]
	}
	return packSixel(uint8((sr+n/2)/n), uint8((sg+n/2)/n), uint8((sb+n/2)/n))
}

func channelOf(k int32, c int) int {
	r, g, b := unpackSixel(k)
	switch c {
	case 0:
		return r
	case 1:
		return g
	}
	return b
}

// appendRLE emits sixel data chars (0x3F + bits) with !n run compression.
func appendRLE(buf []byte, line []byte) []byte {
	i := 0
	for i < len(line) {
		j := i
		for j < len(line) && line[j] == line[i] {
			j++
		}
		n, ch := j-i, byte(0x3F+line[i])
		if n > 3 {
			buf = append(buf, fmt.Sprintf("!%d%c", n, ch)...)
		} else {
			for k := 0; k < n; k++ {
				buf = append(buf, ch)
			}
		}
		i = j
	}
	return buf
}
