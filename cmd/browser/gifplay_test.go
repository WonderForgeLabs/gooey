package main

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

var (
	red  = color.RGBA{R: 255, A: 255}
	blue = color.RGBA{B: 255, A: 255}
)

// writeGIF builds a two-frame animation whose second frame is a PATCH:
// a 2×2 blue square in the corner of an otherwise untouched 8×8 red
// canvas. Painting the raw frames in sequence would show a lone blue
// square on black; coalescing is what keeps the red.
func writeGIF(t *testing.T, path string, delays []int) {
	t.Helper()
	pal := color.Palette{red, blue}

	full := image.NewPaletted(image.Rect(0, 0, 8, 8), pal)
	for i := range full.Pix {
		full.Pix[i] = 0 // red
	}
	patch := image.NewPaletted(image.Rect(0, 0, 2, 2), pal)
	for i := range patch.Pix {
		patch.Pix[i] = 1 // blue
	}

	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{
		Image:    []*image.Paletted{full, patch},
		Delay:    delays,
		Disposal: []byte{gif.DisposalNone, gif.DisposalNone},
		Config:   image.Config{ColorModel: pal, Width: 8, Height: 8},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeClipCoalescesFrames(t *testing.T) {
	dir := t.TempDir()
	writeGIF(t, filepath.Join(dir, "a.gif"), []int{10, 10})

	clip, err := decodeClip(dir, "a.gif", fileKey{})
	if err != nil {
		t.Fatal(err)
	}
	if clip.len() != 2 {
		t.Fatalf("frames = %d, want 2", clip.len())
	}
	at := func(f int, x, y int) color.RGBA {
		c := clip.frames[f].RGBAAt(x, y)
		return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
	}
	if got := at(1, 0, 0); got != blue {
		t.Fatalf("frame 1 patch = %+v, want blue", got)
	}
	// The whole point: outside the patch, frame 1 still carries frame 0.
	if got := at(1, 7, 7); got != red {
		t.Fatalf("frame 1 outside the patch = %+v, want red — frames were not coalesced", got)
	}
}

func TestDecodeClipDelays(t *testing.T) {
	dir := t.TempDir()
	writeGIF(t, filepath.Join(dir, "a.gif"), []int{7, 0})

	clip, err := decodeClip(dir, "a.gif", fileKey{})
	if err != nil {
		t.Fatal(err)
	}
	if want := 70 * time.Millisecond; clip.delays[0] != want {
		t.Fatalf("delay[0] = %v, want %v", clip.delays[0], want)
	}
	// A zero delay means "as fast as possible", which in a terminal means
	// unreadable — every renderer substitutes a default.
	if clip.delays[1] != gifDefDelay {
		t.Fatalf("delay[1] = %v, want the default %v", clip.delays[1], gifDefDelay)
	}
}

func TestDecodeClipRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.gif"), []byte("not a gif"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeClip(dir, "bad.gif", fileKey{}); err == nil {
		t.Fatal("garbage decoded without error")
	}
	if _, err := decodeClip(dir, "missing.gif", fileKey{}); err == nil {
		t.Fatal("missing file decoded without error")
	}
}

func TestDecodeClipScalesDown(t *testing.T) {
	// A recording is ~900×600; coalescing it at full size would hold
	// hundreds of megabytes to feed a pane a few hundred pixels wide.
	got, _ := fitDown(900, 600, gifMaxDim)
	if got != gifMaxDim {
		t.Fatalf("fitDown long side = %d, want %d", got, gifMaxDim)
	}
	if w, h := fitDown(10, 20, gifMaxDim); w != 10 || h != 20 {
		t.Fatalf("small image was resized to %dx%d", w, h)
	}
}

// TestDecodeClipReducesByAveraging covers the half of the reduction that
// fitDown does not: fitDown only picks the numbers, and every assertion
// above it is arithmetic on those numbers. Nothing here noticed which
// scaler ran, so decodeClip could subsample every frame — dropping thin
// rules and moiréing anything periodic — with the whole file green.
//
// The claim is one a subsampling scaler cannot satisfy at any phase.
// Reading one source pixel per destination pixel can only ever return a
// colour the palette already had, so the source is a 1px checkerboard of
// exactly two: intermediate values in the output are proof that source
// pixels were combined rather than chosen between.
func TestDecodeClipReducesByAveraging(t *testing.T) {
	const w, h = 900, 600
	dark, light := color.RGBA{40, 40, 40, 255}, color.RGBA{200, 200, 200, 255}
	pal := color.Palette{dark, light}
	frame := image.NewPaletted(image.Rect(0, 0, w, h), pal)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			frame.SetColorIndex(x, y, uint8((x+y)%2))
		}
	}
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{
		Image:    []*image.Paletted{frame},
		Delay:    []int{10},
		Disposal: []byte{gif.DisposalNone},
		Config:   image.Config{ColorModel: pal, Width: w, Height: h},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "check.gif"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	clip, err := decodeClip(dir, "check.gif", fileKey{})
	if err != nil {
		t.Fatal(err)
	}
	if clip.w != gifMaxDim {
		t.Fatalf("clip width = %d, want the capped %d", clip.w, gifMaxDim)
	}
	// Sample the interior; the outermost ring blends with the canvas edge.
	blended, total := 0, 0
	for y := 2; y < clip.h-2; y++ {
		for x := 2; x < clip.w-2; x++ {
			total++
			if v := clip.frames[0].RGBAAt(x, y).R; v != dark.R && v != light.R {
				blended++
			}
		}
	}
	if blended*2 < total {
		t.Fatalf("only %d of %d interior pixels hold a value between the source's two colours; "+
			"the frames are being subsampled, so a one-pixel feature survives or vanishes "+
			"depending on where the sampling grid lands", blended, total)
	}
}

func testClip(path string, n int, d time.Duration) *gifClip {
	// id mirrors what decodeClip produces for dir "": hostPath("", path)
	// is just path, so tests address the cache by the same name.
	c := &gifClip{w: 2, h: 2, path: path, id: path}
	for i := 0; i < n; i++ {
		c.frames = append(c.frames, image.NewRGBA(image.Rect(0, 0, 2, 2)))
		c.delays = append(c.delays, d)
	}
	return c
}

func TestCacheDropsStaleEntries(t *testing.T) {
	c := newGifCache()
	key := fileKey{size: 1}
	clip := testClip("a.gif", 1, time.Second)
	clip.key = key
	c.put(clip)

	if c.get("a.gif", key) != clip {
		t.Fatal("cache miss on a fresh entry")
	}
	// A re-recording gives the same path a new identity; the decode of
	// the old bytes must not be handed back.
	if got := c.get("a.gif", fileKey{size: 2}); got != nil {
		t.Fatal("stale clip returned")
	}
	if len(c.clips) != 0 || c.bytes != 0 {
		t.Fatalf("stale clip not evicted: %d entries, %d bytes", len(c.clips), c.bytes)
	}
}

// fakePost stands in for the Dispatcher: it records posted work without
// running it, so a test can decide when (and whether) the UI goroutine
// would have seen it.
type fakePost struct {
	mu    sync.Mutex
	n     int
	queue []func()
}

func (f *fakePost) post(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.queue = append(f.queue, fn)
}

func (f *fakePost) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

func (f *fakePost) drain() {
	f.mu.Lock()
	work := f.queue
	f.queue = nil
	f.mu.Unlock()
	for _, fn := range work {
		fn()
	}
}

func (f *fakePost) waitFor(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if f.count() >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("ticker posted %d times, wanted %d", f.count(), n)
}

// TestStopJoinsTheTicker is the lifecycle proof. The stop returned by
// Start is what Composer.Close calls — on a hot reload, on teardown, and
// (via the browser's explicit Stop) before a terminal hand-off. When it
// returns, the animation goroutine must be GONE, not merely signalled:
// a goroutine still alive across a hand-off is the same class of bug as
// an input decoder that outlives its screen.
func TestStopJoinsTheTicker(t *testing.T) {
	p := newPlayer(prop.NewSource(""))
	f := &fakePost{}
	stop := p.Start(f.post)

	p.begin(testClip("a.gif", 3, 2*time.Millisecond))
	if !p.Playing() {
		t.Fatal("begin did not start playback")
	}
	f.waitFor(t, 3)

	stop() // the Composer.Close path
	if p.Playing() {
		t.Fatal("still playing after the composition was closed")
	}
	settled := f.count()
	time.Sleep(100 * time.Millisecond) // fifty frame delays
	if after := f.count(); after != settled {
		t.Fatalf("ticker survived stop: %d further posts", after-settled)
	}
}

// A tick already queued when playback ended must not move a frame index
// that now belongs to a different clip, or to nothing at all.
func TestQueuedTickAfterStopIsIgnored(t *testing.T) {
	p := newPlayer(prop.NewSource(""))
	f := &fakePost{}
	defer p.Start(f.post)()

	p.begin(testClip("a.gif", 4, 2*time.Millisecond))
	f.waitFor(t, 2)
	p.Stop()

	f.drain() // deliver everything the ticker queued before it died
	if p.Playing() {
		t.Fatal("a stale tick restarted playback")
	}
	if got := p.frame.Get(); got != 0 {
		t.Fatalf("frame index moved to %d after stop", got)
	}
	if p.Current() != nil {
		t.Fatal("a frame is still being offered for paint")
	}
}

func TestToggleStopsWhatIsPlaying(t *testing.T) {
	p := newPlayer(prop.NewSource(""))
	f := &fakePost{}
	defer p.Start(f.post)()

	clip := testClip("a.gif", 2, time.Second)
	p.cache.put(clip)
	p.Toggle("", "a.gif", fileKey{})
	if !p.Playing() {
		t.Fatal("toggle did not start a cached clip")
	}
	p.Toggle("", "a.gif", fileKey{})
	if p.Playing() {
		t.Fatal("toggle did not stop playback")
	}
}

func TestToggleWithoutAGifSaysSo(t *testing.T) {
	status := prop.NewSource("")
	p := newPlayer(status)
	defer p.Start((&fakePost{}).post)()

	p.Toggle("", "", fileKey{})
	if p.Playing() {
		t.Fatal("playback started with no GIF")
	}
	if status.Get() == "" {
		t.Fatal("no explanation for a demo with no GIF")
	}
}

func TestStaleTracksSelectionAndRerecording(t *testing.T) {
	p := newPlayer(prop.NewSource(""))
	defer p.Start((&fakePost{}).post)()

	key := fileKey{size: 1}
	clip := testClip("a.gif", 2, time.Second)
	clip.key = key
	p.begin(clip)

	if p.Stale("", "a.gif", key) {
		t.Fatal("playing clip reported stale against its own file")
	}
	if !p.Stale("", "b.gif", key) {
		t.Fatal("selection moved and playback was not stale")
	}
	if !p.Stale("", "a.gif", fileKey{size: 2}) {
		t.Fatal("file was re-recorded and playback was not stale")
	}
	// The SAME relative path under a different source root is a
	// different file: switching sources mid-playback goes stale even
	// when the branch has an identically named GIF.
	if !p.Stale("/elsewhere", "a.gif", key) {
		t.Fatal("same name under another root was not stale")
	}
}

// An animation is a property change per frame, so it has to obey the
// same damage discipline as everything else: the pane repaints, the list
// beside it does not. If this count ever climbs, playback has started
// costing a full-tree repaint several times a second.
func TestAnimationRepaintsOnlyThePreview(t *testing.T) {
	demos := prop.NewComputed(func() []demo {
		return []demo{{name: "a", dir: "cmd/a"}, {name: "b", dir: "cmd/b"}}
	})
	sel := prop.NewSource(0)
	play := newPlayer(prop.NewSource(""))

	list := &demoList{demos: demos, sel: sel}
	info := &demoBody{demos: demos, sel: sel, play: play}
	info.LayoutProps().Col = 1
	grid := &components.Grid{
		Cols:     []components.GridLen{components.Star(1), components.Star(1)},
		Children: []gooey.Component{list, info},
	}

	c := gooey.NewComposer(grid, 80, 24)
	c.Start(gooey.NewDispatcher())
	defer c.Close()
	c.Frame() // settle: first frame paints everything

	// A very long delay keeps the ticker from advancing under the test;
	// the ticks here are delivered by hand.
	play.begin(testClip("a.gif", 4, time.Hour))
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("starting playback repainted %d components, want 1", painted)
	}
	play.advance(play.gen)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("an animation tick repainted %d components, want 1", painted)
	}
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("a frame with nothing changed repainted %d components", painted)
	}
	play.Stop()
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("stopping playback repainted %d components, want 1", painted)
	}
	// Every launch calls Stop, almost always with nothing playing. That
	// must be free: prop.Set does not compare values, so an unguarded
	// Set here would repaint the pane on every `enter`.
	play.Stop()
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("stopping an idle player repainted %d components, want 0", painted)
	}
}

// Composer.Close is the hot-reload and teardown path. It must reach the
// animation clock, which it can only do because the preview component is a
// Startable — the walk that finds key bindings finds this too.
func TestComposerCloseStopsPlayback(t *testing.T) {
	play := newPlayer(prop.NewSource(""))
	info := &demoBody{
		demos: prop.NewComputed(func() []demo { return nil }),
		sel:   prop.NewSource(0),
		play:  play,
	}
	c := gooey.NewComposer(info, 40, 10)
	c.Start(gooey.NewDispatcher())
	play.begin(testClip("a.gif", 3, 2*time.Millisecond))
	if !play.Playing() {
		t.Fatal("playback did not start")
	}
	c.Close()
	if play.Playing() {
		t.Fatal("closing the composition left the animation running")
	}
}

func TestFitCellsLetterboxes(t *testing.T) {
	// A halfblock cell is two pixels tall, so a 2:1 image exactly fills a
	// square cell grid.
	if w, h := fitCells(40, 20, 40, 40); w != 40 || h != 10 {
		t.Fatalf("wide image = %dx%d cells, want 40x10", w, h)
	}
	// Taller than the pane: height binds and width shrinks to match.
	if w, h := fitCells(20, 80, 40, 10); h != 10 || w != 5 {
		t.Fatalf("tall image = %dx%d cells, want 5x10", w, h)
	}
	if w, h := fitCells(0, 0, 10, 10); w != 0 || h != 0 {
		t.Fatalf("degenerate image = %dx%d, want 0x0", w, h)
	}
}

func TestGifForPrefersRecordings(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, recDir), 0o755); err != nil {
		t.Fatal(err)
	}
	// A GIF at the module root is the legacy layout; it still resolves.
	writeGIF(t, filepath.Join(dir, "reader.gif"), []int{10, 10})
	env := scanEnvFor(dir, dir)

	got, gotDir, _, ok := gifFor(env, "reader", "reader")
	if !ok || got != "reader.gif" || gotDir != dir {
		t.Fatalf("legacy root fallback = %q under %q (%v), want reader.gif under %q", got, gotDir, ok, dir)
	}
	// The checked-in home wins over the legacy root location.
	if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(gifHome)), 0o755); err != nil {
		t.Fatal(err)
	}
	writeGIF(t, filepath.Join(dir, filepath.FromSlash(gifHome), "reader.gif"), []int{10, 10})
	got, gotDir, _, ok = gifFor(env, "reader", "reader")
	if !ok || got != gifHome+"/reader.gif" || gotDir != dir {
		t.Fatalf("checked-in home not preferred: %q under %q (%v)", got, gotDir, ok)
	}
	// A fresh recording supersedes the checked-in GIF.
	writeGIF(t, filepath.Join(dir, recDir, "reader.gif"), []int{10, 10})
	got, gotDir, _, ok = gifFor(env, "reader", "reader")
	if !ok || got != recDir+"/reader.gif" || gotDir != dir {
		t.Fatalf("recording not preferred: %q under %q (%v)", got, gotDir, ok)
	}
	if _, _, _, ok := gifFor(env, "nope", "nope"); ok {
		t.Fatal("reported a GIF that does not exist")
	}
}

// With split roots — a source being browsed and the launch tree holding
// recordings — the fallback GIF comes from the SOURCE and a recording
// still wins from the LAUNCH tree. Each result names the root it
// resolves under, which is what keeps two same-named GIFs apart.
func TestGifForResolvesAcrossRoots(t *testing.T) {
	srcRoot, launchRoot := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(launchRoot, recDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcRoot, filepath.FromSlash(gifHome)), 0o755); err != nil {
		t.Fatal(err)
	}
	writeGIF(t, filepath.Join(srcRoot, filepath.FromSlash(gifHome), "reader.gif"), []int{10, 10})
	env := scanEnvFor(srcRoot, launchRoot)

	got, gotDir, _, ok := gifFor(env, "reader", "reader")
	if !ok || got != gifHome+"/reader.gif" || gotDir != srcRoot {
		t.Fatalf("source fallback = %q under %q (%v), want %s/reader.gif under %q", got, gotDir, ok, gifHome, srcRoot)
	}
	writeGIF(t, filepath.Join(launchRoot, recDir, "reader.gif"), []int{10, 10})
	got, gotDir, _, ok = gifFor(env, "reader", "reader")
	if !ok || got != recDir+"/reader.gif" || gotDir != launchRoot {
		t.Fatalf("launch recording not preferred: %q under %q (%v)", got, gotDir, ok)
	}
}
