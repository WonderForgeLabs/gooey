package graphics

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
)

// Kitty encodes via the Kitty graphics protocol: PNG transmitted as
// base64 in chunked APC sequences, displayed at the cursor and scaled
// to a cell rectangle (c=cols,r=rows).
//
// It is the one protocol with placement identity (IDEncoder). An image
// transmitted with i=ID stays in the terminal's store, so a later frame
// can re-place it (a=p), replace its pixels (a=T with the same id), or
// delete it (a=d) — all without re-sending the PNG. That is what lets an
// incremental frame move a picture for thirty bytes.
//
// The delete forms are a case distinction, not a spelling: d=i deletes
// the PLACEMENTS of an image and keeps the pixels, d=I deletes the image
// data too. Moving an image wants the first; a component that vanished
// wants the second, or the terminal accumulates every picture the session
// ever showed.
type Kitty struct{}

func (Kitty) Name() string { return "kitty" }

func (k Kitty) Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error {
	return k.Transmit(out, 0, img, cols, rows, cellW, cellH)
}

// Transmit sends img under id (omitted when id is zero, which is the
// anonymous one-shot form) and displays it at the cursor.
func (Kitty) Transmit(out *[]byte, id int, img image.Image, cols, rows, _, _ int) error {
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return err
	}
	b64 := base64.StdEncoding.EncodeToString(pngBuf.Bytes())

	buf := *out
	const chunk = 4096
	first := true
	for len(b64) > 0 {
		n := min(chunk, len(b64))
		last := n == len(b64)
		ctrl := ""
		if first {
			// a=T: transmit and display; f=100: PNG; q=2: suppress responses
			ctrl = fmt.Sprintf("a=T,f=100,q=2,c=%d,r=%d,", cols, rows)
			if id > 0 {
				ctrl += fmt.Sprintf("i=%d,", id)
			}
			first = false
		}
		m := 1
		if last {
			m = 0
		}
		buf = append(buf, fmt.Sprintf("\x1b_G%sm=%d;%s\x1b\\", ctrl, m, b64[:n])...)
		b64 = b64[n:]
	}
	*out = buf
	return nil
}

func (Kitty) Place(out *[]byte, id, cols, rows int) {
	*out = append(*out, fmt.Sprintf("\x1b_Ga=p,i=%d,c=%d,r=%d,q=2\x1b\\", id, cols, rows)...)
}

func (Kitty) Delete(out *[]byte, id int, data bool) {
	d := "i"
	if data {
		d = "I"
	}
	*out = append(*out, fmt.Sprintf("\x1b_Ga=d,d=%s,i=%d,q=2\x1b\\", d, id)...)
}
