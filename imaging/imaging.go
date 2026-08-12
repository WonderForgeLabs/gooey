// Package imaging is gooey's image-file seam: a table-driven decoder
// registry behind Load and Decode.
//
// The registry answers "what file formats can an image come from"
// without image loading leaking into the render pipeline — components
// take a decoded image.Image and never see a file. Core registers the
// formats whose decoders cost nothing to carry: png, jpeg, and gif from
// the standard library, bmp from golang.org/x/image (the official
// extension repo — allowed in core where third-party SDKs are not), and
// ico from a small parser in this package. Formats with heavy
// dependencies live in nested modules that call Register from their own
// init — SVG rasterization is imagefmt/svg, blank-imported by apps that
// want it — so core's dependency graph stays flat and opting into a
// format is an import line.
//
// A format is picked by content sniffing, never by extension: Match
// sees the first bytes and says yes or no, the same magic-number
// dispatch a terminal's own image support uses. That is why Decode
// takes a name at all — it is for the error, not the lookup.
//
// GIF decodes to its first frame. Animation is a player's job, not a
// decoder's (the browser demo's gifplay pattern), so a *gif.GIF never
// crosses this API.
package imaging

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"strings"
	"sync"

	"golang.org/x/image/bmp"
)

// Format is one entry in the decoder registry.
type Format struct {
	// Name identifies the format in errors ("png", "svg").
	Name string
	// Match reports whether data in this format starts like header.
	// The header holds the file's first bytes (up to 512); a file
	// shorter than that arrives whole.
	Match func(header []byte) bool
	// Decode reads a whole image. It sees the file from byte zero —
	// sniffing does not consume the header.
	Decode func(r io.Reader) (image.Image, error)
	// Config reads only the header and reports the image's dimensions
	// without decoding the pixels. Optional: a format that does not
	// supply it still decodes, it just cannot be size-checked BEFORE
	// the allocation (DecodeLimited falls back to checking the decoded
	// bounds, which catches the value but not the allocation).
	//
	// This is what makes a decompression bomb refusable rather than
	// merely detectable: a few hundred bytes of PNG can declare
	// 40000x40000, and only the header says so.
	Config func(r io.Reader) (image.Config, error)
}

// Limits bounds what DecodeLimited will accept from an untrusted
// source. The zero value means unlimited, so Load and Decode — the
// trusted paths, reading an app's own fs.FS — are unaffected.
type Limits struct {
	// MaxBytes caps the encoded size. Zero means unlimited.
	MaxBytes int
	// MaxPixels caps width*height of the decoded image. Zero means
	// unlimited. This is the cap that matters: bytes bound the wire
	// cost, pixels bound the memory, and the gap between them is the
	// whole trick a bomb plays.
	MaxPixels int
}

// LimitError reports that an image was refused for its size rather than
// its content. It is deliberately distinct from Error: "too big" is a
// policy answer a caller may want to report differently from "did not
// decode".
type LimitError struct {
	Path   string
	Format string
	// What tripped, in the units the limit is expressed in.
	Detail string
}

func (e *LimitError) Error() string {
	if e.Format == "" {
		return fmt.Sprintf("imaging: %s: %s", e.Path, e.Detail)
	}
	return fmt.Sprintf("imaging: %s: %s: %s", e.Path, e.Format, e.Detail)
}

// DecodeLimited is Decode for bytes that arrived from outside the app —
// the control plane's image kind, and anything else a client can send.
//
// The order is the point. The byte cap is checked before any allocation,
// the dimension cap before the pixel buffer exists, and the decoded
// bounds afterwards as a backstop for formats that supply no Config. A
// caller that only capped bytes would still admit a bomb; a caller that
// only checked afterwards would already have paid for it.
//
// Why this lives here rather than at each transport: both the gRPC and
// MCP surfaces decode on the app's single UI goroutine, where a stall is
// every client's stall, not just the sender's. One policy in one place
// is the only way those two stay honest with each other.
func DecodeLimited(r io.Reader, name string, lim Limits) (image.Image, error) {
	var data []byte
	var err error
	if lim.MaxBytes > 0 {
		// Read one byte past the cap: enough to know it was exceeded,
		// never the whole oversized payload.
		data, err = io.ReadAll(io.LimitReader(r, int64(lim.MaxBytes)+1))
		if err == nil && len(data) > lim.MaxBytes {
			return nil, &LimitError{
				Path:   name,
				Detail: fmt.Sprintf("image is larger than the %d-byte limit", lim.MaxBytes),
			}
		}
	} else {
		data, err = io.ReadAll(r)
	}
	if err != nil {
		return nil, &Error{Path: name, Err: err}
	}
	return decodeLimited(data, name, lim)
}

func decodeLimited(data []byte, name string, lim Limits) (image.Image, error) {
	f, ok := pick(data)
	if !ok {
		return nil, &Error{
			Path: name,
			Err:  fmt.Errorf("unrecognized image format (registered: %s)", strings.Join(Names(), ", ")),
		}
	}
	if lim.MaxPixels > 0 && f.Config != nil {
		cfg, err := f.Config(bytes.NewReader(data))
		if err != nil {
			return nil, &Error{Path: name, Format: f.Name, Err: err}
		}
		if over := overPixels(cfg.Width, cfg.Height, lim.MaxPixels); over != "" {
			return nil, &LimitError{Path: name, Format: f.Name, Detail: over}
		}
	}
	img, err := f.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, &Error{Path: name, Format: f.Name, Err: err}
	}
	// The backstop, for a format with no Config. It has already cost the
	// allocation, so it is not the defense — it is what keeps an
	// unbounded image from being STORED and re-encoded on every frame.
	if lim.MaxPixels > 0 && f.Config == nil {
		b := img.Bounds()
		if over := overPixels(b.Dx(), b.Dy(), lim.MaxPixels); over != "" {
			return nil, &LimitError{Path: name, Format: f.Name, Detail: over}
		}
	}
	return img, nil
}

// overPixels reports why w*h exceeds max, or "" when it does not. The
// multiplication is done in int64 because w and h are attacker-chosen
// and their product is exactly the thing that overflows int on 32-bit.
func overPixels(w, h, max int) string {
	if w <= 0 || h <= 0 {
		return fmt.Sprintf("image reports a %dx%d size, which is not a picture", w, h)
	}
	if int64(w)*int64(h) > int64(max) {
		return fmt.Sprintf("image is %dx%d (%d pixels), over the %d-pixel limit",
			w, h, int64(w)*int64(h), max)
	}
	return ""
}

// pick returns the first registered format whose Match accepts data.
func pick(data []byte) (Format, bool) {
	header := data
	if len(header) > sniffLen {
		header = header[:sniffLen]
	}
	regMu.RLock()
	table := formats
	regMu.RUnlock()
	for _, f := range table {
		if f.Match(header) {
			return f, true
		}
	}
	return Format{}, false
}

// sniffLen is how much of the file Match gets to look at. 512 matches
// net/http.DetectContentType, and every magic number worth sniffing
// lives in the first few bytes anyway; SVG is the outlier that needs
// room for an XML prolog before its root element.
const sniffLen = 512

var (
	regMu   sync.RWMutex
	formats []Format
)

// Register adds a format to the registry. Core formats are registered
// by this package's init; a nested format module (imagefmt/svg)
// registers from its own init, so a blank import is the whole opt-in.
// Formats are tried in registration order.
func Register(f Format) {
	regMu.Lock()
	defer regMu.Unlock()
	formats = append(formats, f)
}

// Names lists the registered formats in registration order — the
// vocabulary an "unrecognized format" error can offer.
func Names() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	names := make([]string, len(formats))
	for i, f := range formats {
		names[i] = f.Name
	}
	return names
}

// Error is a failed image load: which file, which format the sniff
// picked (empty when nothing matched), and what went wrong. Markup
// wraps it into its load error, so <Image Src="logo.png"> failing names
// the path and the format at page-load time.
type Error struct {
	Path   string // file path or reader name, as given by the caller
	Format string // sniffed format name, "" when unrecognized
	Err    error
}

func (e *Error) Error() string {
	if e.Format == "" {
		return fmt.Sprintf("imaging: %s: %v", e.Path, e.Err)
	}
	return fmt.Sprintf("imaging: %s: %s: %v", e.Path, e.Format, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Load reads and decodes an image from any fs.FS — the same seam markup
// pages load through, so an app's images ship exactly the way its
// markup does: os.DirFS in dev, embed.FS in release.
func Load(fsys fs.FS, path string) (image.Image, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, &Error{Path: path, Err: err}
	}
	return decode(data, path)
}

// Decode sniffs r's format against the registry and decodes it. The
// name only names the source in errors — decoding never looks at it.
func Decode(r io.Reader, name string) (image.Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, &Error{Path: name, Err: err}
	}
	return decode(data, name)
}

func decode(data []byte, name string) (image.Image, error) {
	return decodeLimited(data, name, Limits{})
}

func hasPrefix(header []byte, magic string) bool {
	return bytes.HasPrefix(header, []byte(magic))
}

func init() {
	Register(Format{
		Name:   "png",
		Match:  func(h []byte) bool { return hasPrefix(h, "\x89PNG\r\n\x1a\n") },
		Decode: png.Decode,
		Config: png.DecodeConfig,
	})
	Register(Format{
		Name:   "jpeg",
		Match:  func(h []byte) bool { return hasPrefix(h, "\xff\xd8\xff") },
		Decode: jpeg.Decode,
		Config: jpeg.DecodeConfig,
	})
	Register(Format{
		Name: "gif",
		Match: func(h []byte) bool {
			return hasPrefix(h, "GIF87a") || hasPrefix(h, "GIF89a")
		},
		// gif.Decode is already "the first frame" — animation is the
		// player's job.
		Decode: gif.Decode,
		Config: gif.DecodeConfig,
	})
	Register(Format{
		Name:   "bmp",
		Match:  func(h []byte) bool { return hasPrefix(h, "BM") },
		Decode: bmp.Decode,
		Config: bmp.DecodeConfig,
	})
	// ICO supplies no Config: the ICONDIR's per-entry width and height
	// are single bytes (max 256, with 0 meaning 256), so the directory
	// cannot describe a bomb — but an entry's payload may itself be a
	// PNG whose real header disagrees with the directory. The decoded
	// bounds backstop covers that, and it is the reason the backstop
	// exists at all.
	Register(Format{
		Name:   "ico",
		Match:  matchICO,
		Decode: decodeICO,
	})
}
