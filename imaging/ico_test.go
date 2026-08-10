package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func le16(v int) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, uint16(v)); return b }
func le32(v int) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, uint32(v)); return b }

type icoFixtureEntry struct {
	w, h, bits int
	payload    []byte
}

// buildICO assembles an ICONDIR + entries + payloads by hand — the
// point of the test is the parser, so no library writes the fixture.
func buildICO(entries ...icoFixtureEntry) []byte {
	var out []byte
	out = append(out, le16(0)...) // reserved
	out = append(out, le16(1)...) // type: icon
	out = append(out, le16(len(entries))...)
	off := 6 + 16*len(entries)
	for _, e := range entries {
		w, h := e.w, e.h
		if w == 256 {
			w = 0
		}
		if h == 256 {
			h = 0
		}
		out = append(out, byte(w), byte(h), 0, 0)
		out = append(out, le16(1)...) // planes
		out = append(out, le16(e.bits)...)
		out = append(out, le32(len(e.payload))...)
		out = append(out, le32(off)...)
		off += len(e.payload)
	}
	for _, e := range entries {
		out = append(out, e.payload...)
	}
	return out
}

// dibHeader is a 40-byte BITMAPINFOHEADER with the ICO-doubled height.
func dibHeader(w, h, bits int) []byte {
	var out []byte
	out = append(out, le32(40)...)
	out = append(out, le32(w)...)
	out = append(out, le32(2*h)...) // doubled: XOR + AND mask
	out = append(out, le16(1)...)   // planes
	out = append(out, le16(bits)...)
	out = append(out, le32(0)...) // BI_RGB
	out = append(out, make([]byte, 20)...)
	return out
}

// dib24Fixture is the 2×2 fixture as a masked 24-bit DIB with pixel
// (1,1) — white — punched transparent by the AND mask.
func dib24Fixture() []byte {
	out := dibHeader(2, 2, 24)
	// XOR data, bottom-up, BGR, rows padded to 4 bytes (rowSize 8).
	out = append(out, 0xff, 0x00, 0x00, 0xff, 0xff, 0xff, 0, 0) // y=1: blue, white
	out = append(out, 0x00, 0x00, 0xff, 0x00, 0xff, 0x00, 0, 0) // y=0: red, green
	// AND mask, bottom-up, 4 bytes per row: bit set = transparent.
	out = append(out, 0x40, 0, 0, 0) // y=1: x=1 transparent
	out = append(out, 0x00, 0, 0, 0) // y=0: opaque
	return out
}

func TestICODIBEntryWithANDMask(t *testing.T) {
	img, err := Decode(bytes.NewReader(buildICO(icoFixtureEntry{w: 2, h: 2, bits: 24, payload: dib24Fixture()})), "fixture.ico")
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]int]color.NRGBA{
		{0, 0}: {255, 0, 0, 255},
		{1, 0}: {0, 255, 0, 255},
		{0, 1}: {0, 0, 255, 255},
		{1, 1}: {255, 255, 255, 0}, // masked out
	}
	for pos, w := range want {
		got := color.NRGBAModel.Convert(img.At(pos[0], pos[1])).(color.NRGBA)
		if w.A == 0 {
			if got.A != 0 {
				t.Fatalf("pixel %v alpha = %d, want transparent", pos, got.A)
			}
			continue
		}
		if got != w {
			t.Fatalf("pixel %v = %v, want %v", pos, got, w)
		}
	}
}

func TestICOPNGEntry(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, fixture()); err != nil {
		t.Fatal(err)
	}
	img, err := Decode(bytes.NewReader(buildICO(icoFixtureEntry{w: 2, h: 2, bits: 32, payload: buf.Bytes()})), "png-entry.ico")
	if err != nil {
		t.Fatal(err)
	}
	wantPixels(t, img)
}

func TestICOPicksTheLargestEntry(t *testing.T) {
	// A 1×1 DIB decoy first, the 2×2 PNG fixture second: decoding must
	// choose by size, not by directory order.
	decoy := dibHeader(1, 1, 24)
	decoy = append(decoy, 0x00, 0x00, 0x00, 0) // one black pixel, padded
	decoy = append(decoy, 0x00, 0, 0, 0)       // opaque mask
	var buf bytes.Buffer
	if err := png.Encode(&buf, fixture()); err != nil {
		t.Fatal(err)
	}
	ico := buildICO(
		icoFixtureEntry{w: 1, h: 1, bits: 24, payload: decoy},
		icoFixtureEntry{w: 2, h: 2, bits: 32, payload: buf.Bytes()},
	)
	img, err := Decode(bytes.NewReader(ico), "multi.ico")
	if err != nil {
		t.Fatal(err)
	}
	wantPixels(t, img)
}

func TestICO32BitAlphaWinsOverMask(t *testing.T) {
	// 32-bit BGRA with real alpha; the all-transparent AND mask must be
	// ignored because the alpha plane is live.
	out := dibHeader(2, 2, 32)
	out = append(out,
		0xff, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff, 0x80, // y=1: blue a=255, white a=128
		0x00, 0x00, 0xff, 0xff, 0x00, 0xff, 0x00, 0xff) // y=0: red, green, both a=255
	out = append(out, 0xc0, 0, 0, 0, 0xc0, 0, 0, 0) // mask says: everything transparent
	img, err := Decode(bytes.NewReader(buildICO(icoFixtureEntry{w: 2, h: 2, bits: 32, payload: out})), "alpha.ico")
	if err != nil {
		t.Fatal(err)
	}
	nr := img.(*image.NRGBA)
	if got := nr.NRGBAAt(0, 0); got != (color.NRGBA{255, 0, 0, 255}) {
		t.Fatalf("pixel (0,0) = %v, want opaque red", got)
	}
	if got := nr.NRGBAAt(1, 1); got.A != 0x80 {
		t.Fatalf("pixel (1,1) alpha = %d, want the DATA alpha 128, not the mask", got.A)
	}
}

func TestICO8BitPaletteEntry(t *testing.T) {
	out := dibHeader(2, 2, 8)
	// 4-color palette (BGRX): red, green, blue, white.
	out = append(out,
		0x00, 0x00, 0xff, 0x00,
		0x00, 0xff, 0x00, 0x00,
		0xff, 0x00, 0x00, 0x00,
		0xff, 0xff, 0xff, 0x00)
	// biClrUsed says 4.
	binary.LittleEndian.PutUint32(out[32:36], 4)
	// XOR rows bottom-up, rowSize 4: y=1 indices 2,3; y=0 indices 0,1.
	out = append(out, 2, 3, 0, 0)
	out = append(out, 0, 1, 0, 0)
	// Opaque mask.
	out = append(out, 0, 0, 0, 0, 0, 0, 0, 0)
	img, err := Decode(bytes.NewReader(buildICO(icoFixtureEntry{w: 2, h: 2, bits: 8, payload: out})), "pal.ico")
	if err != nil {
		t.Fatal(err)
	}
	wantPixels(t, img)
}
