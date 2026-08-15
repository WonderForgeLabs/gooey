package svg

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io/fs"
	"strings"
	"sync"
)

// IconSet loads SVG icons from an fs.FS, tinted and rasterized at one
// pixel size, and caches the result per (path, tint).
//
// It exists because every consumer of vector icons needs the same three
// things and gets all three wrong by default:
//
//   - RASTERIZE AT TARGET SIZE. The registry path (imaging.Decode) renders
//     at the document's intrinsic size, which is right when the pixel
//     pipeline will rescale to a cell rectangle anyway and wrong for an
//     icon: a 16×16 codicon decoded at 16 and scaled to 32 is exactly the
//     blur a vector source exists to avoid. RasterizeAt draws the paths at
//     the destination size instead.
//   - THE TINT IS PART OF THE SOURCE. Codicons and most icon sets declare
//     fill="currentColor", a CSS cascade the rasterizer has no cascade
//     for — left alone it renders as black, or not at all. Substituting
//     the colour into the document BEFORE rasterizing both resolves that
//     and gives state colours for free: the icon is drawn in its colour
//     rather than drawn grey and recoloured after, which is what keeps the
//     anti-aliased edge honest.
//   - CACHE, BECAUSE THIS RUNS IN A PAINT. A rail redraws on every
//     selection change and a toolbox on every scroll; rasterizing per
//     frame is not a cost a TUI can absorb. The cache is keyed by tint as
//     well as path — an icon drawn in two states is two rasters, and a
//     cache keyed by path alone silently serves the first state forever.
//
// Extracted from apps/wysiwyg's activity rail, which had all three and had
// them privately. This is the same code with the editor's directory, size
// and error prefix taken out of it.
type IconSet struct {
	fsys fs.FS
	px   int

	mu    sync.Mutex
	cache map[string]image.Image
}

// Icons returns a set that reads from fsys and rasterizes at px×px.
//
// Paths passed to At are resolved in fsys as given, so an error names the
// path the caller used rather than a rewritten one. Use fs.Sub if a
// prefix is in the way.
func Icons(fsys fs.FS, px int) *IconSet {
	return &IconSet{fsys: fsys, px: px, cache: map[string]image.Image{}}
}

// At returns path rasterized at the set's size, with every occurrence of
// `currentColor` replaced by tint.
//
// A nil tint substitutes nothing and renders the document as authored,
// which is what a multi-colour icon wants. On a monochrome one it FAILS:
// `currentColor` names a CSS cascade the rasterizer does not implement, so
// reaching it unsubstituted is `param mismatch`, not a black icon. That is
// the good outcome — a tint you forgot to pass is a load error rather than
// an invisible glyph — and it is why Preload exists.
//
// Only the RGB channels are substituted. An SVG expresses transparency
// through fill-opacity rather than through the colour keyword, so an
// alpha in tint would be silently dropped; it is ignored deliberately
// rather than half-applied.
func (s *IconSet) At(path string, tint color.Color) (image.Image, error) {
	key := path + "#" + tintKey(tint)

	s.mu.Lock()
	defer s.mu.Unlock()
	if img, ok := s.cache[key]; ok {
		return img, nil
	}

	src, err := fs.ReadFile(s.fsys, path)
	if err != nil {
		return nil, fmt.Errorf("svg: icon %s: %w", path, err)
	}
	doc := string(src)
	if tint != nil {
		doc = strings.ReplaceAll(doc, "currentColor", hexOf(tint))
	}
	img, err := RasterizeAt(bytes.NewReader([]byte(doc)), s.px, s.px)
	if err != nil {
		return nil, fmt.Errorf("svg: icon %s: %w", path, err)
	}
	if s.cache == nil {
		s.cache = map[string]image.Image{}
	}
	s.cache[key] = img
	return img, nil
}

// Preload rasterizes every path in every tint, so a missing or malformed
// asset is a load error rather than a blank space discovered mid-paint.
//
// Painting cannot report an error — a Render returns nothing and a panic
// there takes the terminal's modes with it — so the only place an icon
// problem can be stated is before the first frame. Call this at setup and
// fail on it.
func (s *IconSet) Preload(paths []string, tints ...color.Color) error {
	if len(tints) == 0 {
		tints = []color.Color{nil}
	}
	for _, p := range paths {
		for _, t := range tints {
			if _, err := s.At(p, t); err != nil {
				return err
			}
		}
	}
	return nil
}

// tintKey distinguishes cache entries. Nil is its own key rather than a
// colour, so "as authored" never collides with a tint that happens to be
// black.
func tintKey(c color.Color) string {
	if c == nil {
		return "-"
	}
	return hexOf(c)
}

// hexOf renders a colour as #rrggbb.
//
// Through color.NRGBAModel, and the N is the whole point. Every other
// route to eight-bit channels in image/color is alpha-premultiplied:
// c.RGBA() returns premultiplied words by contract, and color.RGBAModel
// converts *to* premultiplied storage, so both turn a half-alpha red into
// #7f0000 — a different colour, silently, and only for translucent tints.
// NRGBAModel is the one model that divides the alpha back out, giving the
// colour as authored, which is what belongs in a fill= attribute.
func hexOf(c color.Color) string {
	n := color.NRGBAModel.Convert(c).(color.NRGBA)
	return fmt.Sprintf("#%02x%02x%02x", n.R, n.G, n.B)
}
