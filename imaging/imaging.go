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
	header := data
	if len(header) > sniffLen {
		header = header[:sniffLen]
	}
	regMu.RLock()
	table := formats
	regMu.RUnlock()
	for _, f := range table {
		if !f.Match(header) {
			continue
		}
		img, err := f.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, &Error{Path: name, Format: f.Name, Err: err}
		}
		return img, nil
	}
	return nil, &Error{
		Path: name,
		Err:  fmt.Errorf("unrecognized image format (registered: %s)", strings.Join(Names(), ", ")),
	}
}

func hasPrefix(header []byte, magic string) bool {
	return bytes.HasPrefix(header, []byte(magic))
}

func init() {
	Register(Format{
		Name:   "png",
		Match:  func(h []byte) bool { return hasPrefix(h, "\x89PNG\r\n\x1a\n") },
		Decode: png.Decode,
	})
	Register(Format{
		Name:   "jpeg",
		Match:  func(h []byte) bool { return hasPrefix(h, "\xff\xd8\xff") },
		Decode: jpeg.Decode,
	})
	Register(Format{
		Name: "gif",
		Match: func(h []byte) bool {
			return hasPrefix(h, "GIF87a") || hasPrefix(h, "GIF89a")
		},
		// gif.Decode is already "the first frame" — animation is the
		// player's job.
		Decode: gif.Decode,
	})
	Register(Format{
		Name:   "bmp",
		Match:  func(h []byte) bool { return hasPrefix(h, "BM") },
		Decode: bmp.Decode,
	})
	Register(Format{
		Name:   "ico",
		Match:  matchICO,
		Decode: decodeICO,
	})
}
