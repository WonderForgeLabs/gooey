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
type Kitty struct{}

func (Kitty) Name() string { return "kitty" }

func (Kitty) Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error {
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
