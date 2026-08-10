package graphics

import (
	"fmt"
	"image"
)

// Sixel encodes via the DEC sixel protocol (DCS q ... ST).
// Colors are quantized to a 6×6×6 cube (216 registers), well under the
// 256-register limit.
type Sixel struct{}

func (Sixel) Name() string { return "sixel" }

func (Sixel) Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error {
	pxW, pxH := cols*cellW, rows*cellH
	rgba := Scale(img, pxW, pxH)

	// Quantize to 6-level-per-channel palette; build index plane.
	idx := make([]uint8, pxW*pxH)
	used := map[uint8]bool{}
	for y := 0; y < pxH; y++ {
		for x := 0; x < pxW; x++ {
			r, g, b, _ := rgba.At(x, y).RGBA()
			q := quant6(uint8(r>>8))*36 + quant6(uint8(g>>8))*6 + quant6(uint8(b>>8))
			idx[y*pxW+x] = q
			used[q] = true
		}
	}

	buf := append(*out, "\x1bP0;0;0q"...)
	buf = append(buf, fmt.Sprintf("\"1;1;%d;%d", pxW, pxH)...)
	// Palette registers: sixel wants 0–100 per channel.
	for q := range used {
		r, g, b := int(q)/36, (int(q)/6)%6, int(q)%6
		buf = append(buf, fmt.Sprintf("#%d;2;%d;%d;%d", q, r*100/5, g*100/5, b*100/5)...)
	}
	// Bands of 6 vertical pixels.
	for y0 := 0; y0 < pxH; y0 += 6 {
		first := true
		for q := range used {
			// Build the bit rows for this color in this band.
			line := make([]byte, pxW)
			any := false
			for x := 0; x < pxW; x++ {
				var bits byte
				for dy := 0; dy < 6 && y0+dy < pxH; dy++ {
					if idx[(y0+dy)*pxW+x] == q {
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
			buf = append(buf, fmt.Sprintf("#%d", q)...)
			buf = appendRLE(buf, line)
		}
		buf = append(buf, '-') // next band
	}
	buf = append(buf, "\x1b\\"...)
	*out = buf
	return nil
}

func quant6(v uint8) uint8 { return uint8((int(v)*5 + 127) / 255) }

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
