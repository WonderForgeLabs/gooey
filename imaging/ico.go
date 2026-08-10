package imaging

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
)

// ICO support, in-repo because the format is an index, not a codec: an
// ICONDIR header, a table of entries, and per-entry payloads that are
// either whole PNG files (Vista+) or bare DIBs — a BITMAPINFOHEADER
// with no BMP file header, pixel rows bottom-up, plus a 1-bit AND mask
// carrying transparency for the depths that have no alpha channel.
// Decoding picks the single best entry (largest, then deepest); an icon
// is one image at many sizes, and the pixel pipeline rescales to cells
// anyway.

// matchICO sniffs the ICONDIR: reserved 0, type 1 (icon) or 2
// (cursor), and a nonzero entry count. The magic alone is weak
// (\x00\x00\x01\x00), which is why the count byte is part of the test.
func matchICO(h []byte) bool {
	if len(h) < 6 {
		return false
	}
	reserved := binary.LittleEndian.Uint16(h[0:2])
	kind := binary.LittleEndian.Uint16(h[2:4])
	count := binary.LittleEndian.Uint16(h[4:6])
	return reserved == 0 && (kind == 1 || kind == 2) && count > 0
}

type icoEntry struct {
	w, h     int // 0 in the file means 256
	bits     int
	off, len int
}

func decodeICO(r io.Reader) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(data) < 6 || !matchICO(data) {
		return nil, fmt.Errorf("not an ICONDIR")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	var best icoEntry
	for i := 0; i < count; i++ {
		off := 6 + 16*i
		if off+16 > len(data) {
			return nil, fmt.Errorf("directory truncated at entry %d of %d", i, count)
		}
		e := icoEntry{
			w:    int(data[off]),
			h:    int(data[off+1]),
			bits: int(binary.LittleEndian.Uint16(data[off+6 : off+8])),
			len:  int(binary.LittleEndian.Uint32(data[off+8 : off+12])),
			off:  int(binary.LittleEndian.Uint32(data[off+12 : off+16])),
		}
		if e.w == 0 {
			e.w = 256
		}
		if e.h == 0 {
			e.h = 256
		}
		if e.w*e.h > best.w*best.h || (e.w*e.h == best.w*best.h && e.bits > best.bits) {
			best = e
		}
	}
	if best.len <= 0 || best.off < 0 || best.off+best.len > len(data) {
		return nil, fmt.Errorf("entry payload out of range (off %d, len %d, file %d)", best.off, best.len, len(data))
	}
	payload := data[best.off : best.off+best.len]
	if hasPrefix(payload, "\x89PNG\r\n\x1a\n") {
		return png.Decode(bytes.NewReader(payload))
	}
	return decodeDIB(payload, best.h)
}

// decodeDIB reads an ICO entry's bare DIB: BITMAPINFOHEADER (whose
// biHeight is DOUBLED when an AND mask follows the color data),
// optional palette, bottom-up XOR pixel rows, optional bottom-up 1-bit
// AND mask. entryH is the directory's idea of the height, used only to
// tell a doubled biHeight from an undoubled one.
func decodeDIB(d []byte, entryH int) (image.Image, error) {
	if len(d) < 40 {
		return nil, fmt.Errorf("DIB header truncated (%d bytes)", len(d))
	}
	headerSize := int(binary.LittleEndian.Uint32(d[0:4]))
	if headerSize < 40 || headerSize > len(d) {
		return nil, fmt.Errorf("bad DIB header size %d", headerSize)
	}
	width := int(int32(binary.LittleEndian.Uint32(d[4:8])))
	rawH := int(int32(binary.LittleEndian.Uint32(d[8:12])))
	bits := int(binary.LittleEndian.Uint16(d[14:16]))
	compression := binary.LittleEndian.Uint32(d[16:20])
	clrUsed := int(binary.LittleEndian.Uint32(d[32:36]))

	if compression != 0 {
		return nil, fmt.Errorf("compressed DIB (biCompression %d) not supported", compression)
	}
	height, masked := rawH, false
	if entryH > 0 && rawH == 2*entryH {
		height, masked = entryH, true
	}
	if width <= 0 || height <= 0 || width > 1<<15 || height > 1<<15 {
		return nil, fmt.Errorf("bad DIB dimensions %d×%d", width, height)
	}

	off := headerSize
	var palette []color.NRGBA
	switch bits {
	case 1, 4, 8:
		n := clrUsed
		if n == 0 {
			n = 1 << bits
		}
		if off+4*n > len(d) {
			return nil, fmt.Errorf("palette truncated")
		}
		palette = make([]color.NRGBA, n)
		for i := range palette {
			p := d[off+4*i:]
			palette[i] = color.NRGBA{R: p[2], G: p[1], B: p[0], A: 0xff}
		}
		off += 4 * n
	case 24, 32:
		// no palette
	default:
		return nil, fmt.Errorf("%d-bit DIB not supported (want 1, 4, 8, 24 or 32)", bits)
	}

	rowSize := ((bits*width + 31) / 32) * 4
	if off+rowSize*height > len(d) {
		return nil, fmt.Errorf("pixel data truncated")
	}
	maskRow := ((width + 31) / 32) * 4
	maskOff := off + rowSize*height
	if masked && maskOff+maskRow*height > len(d) {
		masked = false // tolerate a truncated mask: opaque beats an error
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	anyAlpha := false
	for y := 0; y < height; y++ {
		row := d[off+(height-1-y)*rowSize:]
		for x := 0; x < width; x++ {
			var c color.NRGBA
			switch bits {
			case 1:
				idx := row[x/8] >> (7 - x%8) & 1
				c = paletteAt(palette, int(idx))
			case 4:
				b := row[x/2]
				if x%2 == 0 {
					b >>= 4
				}
				c = paletteAt(palette, int(b&0x0f))
			case 8:
				c = paletteAt(palette, int(row[x]))
			case 24:
				p := row[x*3:]
				c = color.NRGBA{R: p[2], G: p[1], B: p[0], A: 0xff}
			case 32:
				p := row[x*4:]
				c = color.NRGBA{R: p[2], G: p[1], B: p[0], A: p[3]}
				if p[3] != 0 {
					anyAlpha = true
				}
			}
			img.SetNRGBA(x, y, c)
		}
	}

	// The AND mask is the transparency channel for depths that have
	// none. A 32-bit DIB normally carries real alpha instead — but some
	// writers leave every alpha byte zero and mean the mask, so an
	// all-zero alpha plane defers to it (the classic decoder heuristic).
	if masked && (bits != 32 || !anyAlpha) {
		for y := 0; y < height; y++ {
			mrow := d[maskOff+(height-1-y)*maskRow:]
			for x := 0; x < width; x++ {
				transparent := mrow[x/8]>>(7-x%8)&1 == 1
				c := img.NRGBAAt(x, y)
				if transparent {
					c.A = 0
				} else {
					c.A = 0xff
				}
				img.SetNRGBA(x, y, c)
			}
		}
	} else if bits == 32 && !anyAlpha {
		// No mask and a dead alpha plane: the image is opaque, not
		// invisible.
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				c := img.NRGBAAt(x, y)
				c.A = 0xff
				img.SetNRGBA(x, y, c)
			}
		}
	}
	return img, nil
}

func paletteAt(p []color.NRGBA, i int) color.NRGBA {
	if i < len(p) {
		return p[i]
	}
	return color.NRGBA{}
}
