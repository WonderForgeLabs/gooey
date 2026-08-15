package svg

import (
	"image/color"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"
)

// icon is a 16×16 document that fills itself with currentColor — the
// shape a codicon has, and the shape that renders black if nothing
// substitutes the keyword.
const icon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16">
  <rect x="0" y="0" width="16" height="16" fill="currentColor"/>
</svg>`

// fixed is the same square in a literal colour, for the case where an
// icon is NOT monochrome and must not be repainted.
const fixed = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16">
  <rect x="0" y="0" width="16" height="16" fill="#00ff00"/>
</svg>`

// countingFS reports how many times each file was actually read, which is
// how the cache tests assert a cache rather than assert a value that
// would be identical either way.
type countingFS struct {
	inner fs.FS
	mu    sync.Mutex
	reads map[string]int
}

func newCountingFS(m fstest.MapFS) *countingFS {
	return &countingFS{inner: m, reads: map[string]int{}}
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.mu.Lock()
	c.reads[name]++
	c.mu.Unlock()
	return c.inner.Open(name)
}

func (c *countingFS) count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads[name]
}

func testFS() *countingFS {
	return newCountingFS(fstest.MapFS{
		"code.svg":  {Data: []byte(icon)},
		"fixed.svg": {Data: []byte(fixed)},
	})
}

func rgbaAt(t *testing.T, s *IconSet, path string, tint color.Color, x, y int) color.RGBA {
	t.Helper()
	img, err := s.At(path, tint)
	if err != nil {
		t.Fatalf("At(%q): %v", path, err)
	}
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

// The whole reason this type exists rather than imaging.Decode: a 16×16
// document asked for at 32 must be DRAWN at 32, not decoded at 16.
func TestIconRasterizesAtTheSetSizeNotTheIntrinsicOne(t *testing.T) {
	s := Icons(testFS(), 32)
	img, err := s.At("code.svg", color.RGBA{R: 0xff, A: 0xff})
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 32 {
		t.Fatalf("rasterized to %v, want 32×32 — the set's size, not the document's 16×16", b)
	}
}

func TestTintReplacesCurrentColor(t *testing.T) {
	s := Icons(testFS(), 16)
	got := rgbaAt(t, s, "code.svg", color.RGBA{R: 0x20, G: 0x80, B: 0xf0, A: 0xff}, 8, 8)
	if got.R != 0x20 || got.G != 0x80 || got.B != 0xf0 {
		t.Fatalf("centre pixel is %v, want the tint — currentColor reached the rasterizer unsubstituted", got)
	}
}

// The near-miss twin: same file, same set, a different tint. If the cache
// were keyed by path alone this returns the first tint and the assertion
// is the only thing that would ever notice.
func TestASecondTintIsASecondRaster(t *testing.T) {
	s := Icons(testFS(), 16)
	first := rgbaAt(t, s, "code.svg", color.RGBA{R: 0xff, A: 0xff}, 8, 8)
	second := rgbaAt(t, s, "code.svg", color.RGBA{B: 0xff, A: 0xff}, 8, 8)
	if first == second {
		t.Fatalf("both tints rendered %v — the cache is keyed by path only, so the second state is the first one", first)
	}
	if second.B != 0xff || second.R != 0 {
		t.Fatalf("second tint rendered %v, want blue", second)
	}
}

// A nil tint must leave the document alone. Asserting the colour is not
// enough on its own — a green square is green whether or not
// substitution ran — so this pairs it with the currentColor case above:
// together they say substitution happens exactly when asked for.
func TestANilTintRendersTheDocumentAsAuthored(t *testing.T) {
	s := Icons(testFS(), 16)
	got := rgbaAt(t, s, "fixed.svg", nil, 8, 8)
	if got.G != 0xff || got.R != 0 || got.B != 0 {
		t.Fatalf("centre pixel is %v, want the document's own green", got)
	}
}

// The other half of that, and the reason a forgotten tint is survivable:
// `currentColor` is a CSS cascade the rasterizer does not implement, so an
// unsubstituted one is an ERROR rather than a black or invisible glyph.
// This is load-bearing for Preload — if it ever became a silent black
// icon, Preload would pass and the rail would render wrong.
func TestAnUnsubstitutedCurrentColorIsAnError(t *testing.T) {
	s := Icons(testFS(), 16)
	if _, err := s.At("code.svg", nil); err == nil {
		t.Fatal("a monochrome icon rendered with no tint — currentColor now resolves to something, so a forgotten tint is silent")
	}
}

// Cache proven by counting READS, not by comparing pixels: two calls
// return equal images whether or not anything was cached, so a value
// assertion here would pass against no cache at all.
func TestTheSameIconAndTintIsReadOnce(t *testing.T) {
	fsys := testFS()
	s := Icons(fsys, 16)
	tint := color.RGBA{R: 0xff, A: 0xff}
	for range 5 {
		if _, err := s.At("code.svg", tint); err != nil {
			t.Fatal(err)
		}
	}
	if n := fsys.count("code.svg"); n != 1 {
		t.Fatalf("read the asset %d times for 5 identical requests, want 1", n)
	}
}

func TestDistinctTintsEachReadOnce(t *testing.T) {
	fsys := testFS()
	s := Icons(fsys, 16)
	s.At("code.svg", color.RGBA{R: 0xff, A: 0xff}) //nolint:errcheck // asserted below
	s.At("code.svg", color.RGBA{B: 0xff, A: 0xff}) //nolint:errcheck // asserted below
	s.At("code.svg", color.RGBA{R: 0xff, A: 0xff}) //nolint:errcheck // asserted below
	if n := fsys.count("code.svg"); n != 2 {
		t.Fatalf("read the asset %d times for 2 distinct tints (one repeated), want 2", n)
	}
}

// Nil is not a colour, and specifically not black. If they shared a cache
// key, `At(p, nil)` would store an as-authored render under black's key
// and every later black request would silently get it.
func TestNilAndBlackAreDifferentCacheEntries(t *testing.T) {
	fsys := testFS()
	s := Icons(fsys, 16)
	if _, err := s.At("fixed.svg", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.At("fixed.svg", color.RGBA{A: 0xff}); err != nil {
		t.Fatal(err)
	}
	if n := fsys.count("fixed.svg"); n != 2 {
		t.Fatalf("read the asset %d times for nil and for black, want 2 — nil is keyed as a colour", n)
	}
}

// Preload with no tints at all still has to do something. Its default is
// the nil tint, which is the only tint that can be inferred; an empty
// default makes Preload a no-op that reports success, which is the exact
// failure it exists to prevent.
func TestPreloadWithNoTintsStillLoads(t *testing.T) {
	fsys := testFS()
	s := Icons(fsys, 16)
	if err := s.Preload([]string{"fixed.svg"}); err != nil {
		t.Fatal(err)
	}
	if n := fsys.count("fixed.svg"); n != 1 {
		t.Fatalf("Preload with no tints read the asset %d times, want 1 — it did nothing and said nothing", n)
	}
	if err := s.Preload([]string{"gone.svg"}); err == nil {
		t.Fatal("Preload with no tints passed over a missing asset")
	}
}

func TestAMissingIconNamesThePath(t *testing.T) {
	s := Icons(testFS(), 16)
	_, err := s.At("nope.svg", nil)
	if err == nil {
		t.Fatal("no error for a missing asset")
	}
	if !contains(err.Error(), "nope.svg") {
		t.Fatalf("error %q does not name the path, so it cannot be acted on", err)
	}
}

// Preload's whole job is to move a broken asset from mid-paint to setup,
// where it can be reported.
func TestPreloadReportsABrokenAsset(t *testing.T) {
	s := Icons(testFS(), 16)
	err := s.Preload([]string{"code.svg", "gone.svg"}, color.RGBA{A: 0xff})
	if err == nil {
		t.Fatal("Preload passed over a missing asset — the failure would have arrived in a Render, which cannot report it")
	}
}

func TestPreloadWarmsEveryTintItIsGiven(t *testing.T) {
	fsys := testFS()
	s := Icons(fsys, 16)
	tints := []color.Color{color.RGBA{R: 0xff, A: 0xff}, color.RGBA{B: 0xff, A: 0xff}}
	if err := s.Preload([]string{"code.svg"}, tints...); err != nil {
		t.Fatal(err)
	}
	before := fsys.count("code.svg")
	for _, tn := range tints {
		if _, err := s.At("code.svg", tn); err != nil {
			t.Fatal(err)
		}
	}
	if after := fsys.count("code.svg"); after != before {
		t.Fatalf("reads went %d→%d after Preload, so Preload warmed nothing", before, after)
	}
}

// A translucent tint must convert, not darken. Both obvious routes to
// eight-bit channels are premultiplied — c.RGBA() by contract, and
// color.RGBAModel because color.RGBA IS premultiplied storage — so either
// renders a half-alpha red as #7f0000. The colour is stated with NRGBA
// deliberately: an equivalent color.RGBA literal is premultiplied by
// definition, so it cannot distinguish the two implementations and the
// assertion would pass against the bug.
func TestTintConversionIsNotPremultiplied(t *testing.T) {
	if got := hexOf(color.NRGBA{R: 0xff, A: 0x80}); got != "#ff0000" {
		t.Fatalf("hexOf(half-alpha red) = %s, want #ff0000 — a premultiplied model darkened the tint", got)
	}
	if got := hexOf(color.NRGBA{R: 0x20, G: 0x80, B: 0xf0, A: 0xff}); got != "#2080f0" {
		t.Fatalf("hexOf(opaque NRGBA) = %s, want #2080f0", got)
	}
	if got := hexOf(color.RGBA{R: 0x20, G: 0x80, B: 0xf0, A: 0xff}); got != "#2080f0" {
		t.Fatalf("hexOf(opaque RGBA) = %s, want #2080f0 — an opaque colour must survive the round trip unchanged", got)
	}
}

// A set is built once at setup and read from paints; a Startable warming
// one while the UI goroutine draws from it is the ordinary case, so the
// cache map has to be guarded. imagefmt/svg is not in ci.yml's -race arm,
// so this pin only fires under a local `go test -race ./...` — which is
// what CLAUDE.md's nested-module loop runs, and the reason to keep the
// assertion cheap enough to leave in the default run.
func TestConcurrentAtIsSafe(t *testing.T) {
	s := Icons(testFS(), 16)
	tints := []color.Color{
		color.RGBA{R: 0xff, A: 0xff},
		color.RGBA{G: 0xff, A: 0xff},
		color.RGBA{B: 0xff, A: 0xff},
	}
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Go(func() {
			if _, err := s.At("code.svg", tints[i%len(tints)]); err != nil {
				t.Errorf("At: %v", err)
			}
		})
	}
	wg.Wait()
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
