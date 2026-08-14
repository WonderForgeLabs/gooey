package imaging

// The decode path as an untrusted transport sees it.
//
// Both control-plane surfaces decode bytes a client sent, on the app's
// single UI goroutine. A stall there is not the sender's stall — it is
// input, layout and every other session's frames, and Bridge.round's
// timeout bounds the WAITING rather than the decode, so it outlives the
// timeout that looks like it should catch it.
//
// The bomb is the case a byte cap alone does not cover: a PNG header is
// 45 bytes and can declare 40000x40000, which is 6.4 GB of RGBA. That
// is why these tests exist as a pair — one for each cap — rather than
// one test for "an image that is too big".

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// bombPNG builds a syntactically valid PNG header declaring w x h with
// no image data behind it. DecodeConfig answers from the IHDR alone, so
// this is the whole attack in 45 bytes.
func bombPNG(w, h uint32) []byte {
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	var ihdr bytes.Buffer
	binary.Write(&ihdr, binary.BigEndian, w)
	binary.Write(&ihdr, binary.BigEndian, h)
	ihdr.Write([]byte{8, 6, 0, 0, 0}) // 8-bit, RGBA, no interlace
	chunk := func(typ string, data []byte) {
		binary.Write(&buf, binary.BigEndian, uint32(len(data)))
		payload := append([]byte(typ), data...)
		buf.Write(payload)
		binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(payload))
	}
	chunk("IHDR", ihdr.Bytes())
	return buf.Bytes()
}

// realPNG is an ordinary small picture — the control case, so a passing
// bomb test cannot be explained by "the limiter refuses everything".
func realPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestDecodeLimitedRefusesADeclaredPixelBombBeforeAllocating(t *testing.T) {
	// 40000x40000 = 1.6e9 pixels, 6.4 GB as RGBA, from 45 bytes on the
	// wire. A MaxBytes-only limiter admits this without noticing.
	data := bombPNG(40000, 40000)
	if len(data) > 1024 {
		t.Fatalf("the bomb should be tiny; got %d bytes", len(data))
	}

	lim := Limits{MaxBytes: 16 << 20, MaxPixels: 16_000_000}
	_, err := DecodeLimited(bytes.NewReader(data), "bomb.png", lim)
	if err == nil {
		t.Fatal("a 40000x40000 declaration decoded without complaint")
	}
	var lerr *LimitError
	if !errors.As(err, &lerr) {
		t.Fatalf("error is %T (%v), want a *LimitError — a size refusal must be "+
			"distinguishable from a malformed file, or a caller retries the same bomb", err, err)
	}
	if !bytes.Contains([]byte(lerr.Error()), []byte("40000x40000")) {
		t.Errorf("the refusal does not name the offending size: %v", lerr)
	}
}

func TestDecodeLimitedRefusesOversizeBytesWithoutReadingThemAll(t *testing.T) {
	// 4 MiB of payload against a 1 MiB cap.
	big := bytes.Repeat([]byte("\x89PNG\r\n\x1a\n"), 512*1024)
	r := &countingReader{data: big}

	_, err := DecodeLimited(r, "big.png", Limits{MaxBytes: 1 << 20})
	var lerr *LimitError
	if !errors.As(err, &lerr) {
		t.Fatalf("error is %T (%v), want *LimitError", err, err)
	}
	// The cap must bound the READ, not merely the verdict. Reading it
	// all and then refusing still lets a sender make the app allocate
	// whatever it likes.
	if r.n > (1<<20)+512 {
		t.Errorf("read %d bytes against a %d-byte cap — the limiter buffered the whole payload before refusing", r.n, 1<<20)
	}
}

type countingReader struct {
	data []byte
	off  int
	n    int
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.off >= len(c.data) {
		return 0, errEOF
	}
	n := copy(p, c.data[c.off:])
	c.off += n
	c.n += n
	return n, nil
}

var errEOF = errors.New("EOF")

func TestDecodeLimitedPassesAnOrdinaryPicture(t *testing.T) {
	// A phone photograph is 4032x3024 (12.2 MP) and must still work:
	// a cap that refuses real pictures teaches callers to pre-scale and
	// hides what the limit is for. Asserting the dimension arithmetic
	// rather than encoding 12 MP of test data.
	if over := overPixels(4032, 3024, 16_000_000); over != "" {
		t.Errorf("a 4032x3024 phone photo is refused by the shipped pixel cap: %s", over)
	}

	data := realPNG(t, 64, 32)
	img, err := DecodeLimited(bytes.NewReader(data), "ok.png", Limits{MaxBytes: 16 << 20, MaxPixels: 16_000_000})
	if err != nil {
		t.Fatalf("an ordinary 64x32 PNG was refused: %v", err)
	}
	if got := img.Bounds(); got.Dx() != 64 || got.Dy() != 32 {
		t.Errorf("bounds = %v, want 64x32", got)
	}
}

// The trusted paths must not have acquired a ceiling. Load and Decode
// read an app's OWN fs.FS — its embedded assets — where a limit would be
// a behaviour change for every existing app and would protect nobody.
func TestDecodeAndLoadStayUnlimited(t *testing.T) {
	data := bombPNG(40000, 40000)
	// The bomb has no pixel data, so it fails as a truncated file rather
	// than as a limit. What matters is WHICH error: a *LimitError here
	// would mean the zero-value Limits had grown a ceiling.
	_, err := Decode(bytes.NewReader(data), "asset.png")
	var lerr *LimitError
	if errors.As(err, &lerr) {
		t.Fatalf("Decode applied a size limit (%v); the trusted path must stay unlimited", lerr)
	}
}

// Every registered format either supplies Config — so a bomb is refused
// before the allocation — or is covered by the decoded-bounds backstop.
// This pins that the backstop is not silently the only defense for a
// format that could have had a header check.
func TestCoreRasterFormatsSupplyAConfigHook(t *testing.T) {
	regMu.RLock()
	defer regMu.RUnlock()
	want := map[string]bool{"png": true, "jpeg": true, "gif": true, "bmp": true}
	for _, f := range formats {
		if want[f.Name] && f.Config == nil {
			t.Errorf("format %q has no Config: a declared-dimension bomb in that format "+
				"is only caught after it has been allocated", f.Name)
		}
	}
}
