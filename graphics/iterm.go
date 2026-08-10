package graphics

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
)

// ITerm2 encodes via the OSC 1337 inline-images protocol (iTerm2,
// WezTerm, mintty). Sized in cells via width/height parameters.
type ITerm2 struct{}

func (ITerm2) Name() string { return "iterm2" }

func (ITerm2) Encode(out *[]byte, img image.Image, cols, rows, cellW, cellH int) error {
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return err
	}
	b64 := base64.StdEncoding.EncodeToString(pngBuf.Bytes())
	*out = append(*out, fmt.Sprintf(
		"\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=0:%s\x07",
		cols, rows, b64)...)
	return nil
}
