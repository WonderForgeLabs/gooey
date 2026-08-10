package main

// Every demo in this repo has a recorded GIF, and until now the browser
// could only tell you that. `p` plays it in the preview pane.
//
// Two things make this more than "decode and blit".
//
// A GIF is a DIFF FORMAT: after the first frame, each frame is a patch
// over a canvas, with a disposal rule saying what to do with the patched
// region before the next one lands. Painting the raw frames in sequence
// gives a flickering mess of partial rectangles, so decoding COALESCES
// them into whole images once, up front.
//
// And an animation needs a clock, which in this framework is a lifetime
// question rather than a timing one. The ticker belongs to the
// composition — the preview component is a Startable, so the Composer that
// starts it also stops it, and a hot reload or a terminal hand-off
// cannot leave a ticker running against a pane nobody is showing.
//
// The rendering tier is halfblock only. The pixel protocols (kitty,
// sixel, iterm2) need placements to reach the Composer's flush, which
// the retained path does not carry yet; see graphics.Placement.

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/prop"
)

const (
	// gifMaxDim caps a stored frame's long side. Coalescing turns a
	// diff-encoded file into N full images, so a 900×600 recording with
	// 79 frames would be 170 MB held in RAM to feed a pane that is a few
	// hundred halfblock pixels wide. Downscaling once at decode is the
	// difference between a preview and a memory leak.
	gifMaxDim = 400
	// gifMinDelay is the floor every GIF renderer applies to the 1/100 s
	// delay field; a zero delay means "as fast as possible", which in a
	// terminal means "unreadable".
	gifMinDelay = 20 * time.Millisecond
	gifDefDelay = 100 * time.Millisecond
	// gifCacheMax bounds the decoded-clip cache. Clips are evicted oldest
	// first; re-decoding is a second of work, not a correctness problem.
	gifCacheMax = 64 << 20
)

// fileKey identifies a file BY CONTENT-ISH IDENTITY rather than by name,
// so a re-recorded GIF at the same path invalidates its cached clip. The
// watcher notices the change; this is what makes the notice actionable.
type fileKey struct {
	mod  time.Time
	size int64
}

func keyOf(fsys fs.FS, name string) (fileKey, bool) {
	st, err := fs.Stat(fsys, name)
	if err != nil || st.IsDir() {
		return fileKey{}, false
	}
	return fileKey{mod: st.ModTime(), size: st.Size()}, true
}

// gifClip is a decoded animation: whole frames, already scaled, with the
// file's own per-frame delays. Immutable once built, which is what lets
// the ticker goroutine read the delays without touching the property
// graph.
type gifClip struct {
	frames []*image.RGBA
	delays []time.Duration
	w, h   int
	bytes  int
	path   string // fs.FS name, what status messages show
	id     string // host path — the cache key, unambiguous across source roots
	key    fileKey
}

func (c *gifClip) len() int {
	if c == nil {
		return 0
	}
	return len(c.frames)
}

// decodeClip reads and coalesces a GIF. It runs on a worker goroutine —
// it touches no properties, and a cold decode of a 2 MB recording is far
// too long to spend inside a key handler.
func decodeClip(dir, name string, key fileKey) (*gifClip, error) {
	f, err := os.Open(hostPath(dir, name))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		return nil, err
	}
	if len(g.Image) == 0 {
		return nil, fmt.Errorf("%s: no frames", name)
	}

	w, h := g.Config.Width, g.Config.Height
	if w <= 0 || h <= 0 {
		var r image.Rectangle
		for _, fr := range g.Image {
			r = r.Union(fr.Bounds())
		}
		w, h = r.Dx(), r.Dy()
	}
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("%s: empty canvas", name)
	}
	cw, ch := fitDown(w, h, gifMaxDim)

	// The canvas starts opaque black rather than transparent: a terminal
	// has no alpha, and halfblock would render "nothing" as black anyway.
	// Saying so here keeps the background-disposal case consistent with
	// the initial state instead of flickering between them.
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	black := image.NewUniform(color.Black)
	draw.Draw(canvas, canvas.Bounds(), black, image.Point{}, draw.Src)
	var saved *image.RGBA

	clip := &gifClip{w: cw, h: ch, path: name, id: hostPath(dir, name), key: key}
	for i, src := range g.Image {
		disposal := byte(0)
		if i < len(g.Disposal) {
			disposal = g.Disposal[i]
		}
		if disposal == gif.DisposalPrevious {
			saved = cloneRGBA(canvas)
		}
		// draw.Over is what honours the palette's transparent index: a
		// frame's untouched pixels keep whatever the canvas already had.
		draw.Draw(canvas, src.Bounds(), src, src.Bounds().Min, draw.Over)
		clip.frames = append(clip.frames, graphics.Scale(canvas, cw, ch))

		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, src.Bounds(), black, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			if saved != nil {
				copy(canvas.Pix, saved.Pix)
			}
		}

		d := gifDefDelay
		if i < len(g.Delay) && g.Delay[i] > 0 {
			d = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		if d < gifMinDelay {
			d = gifMinDelay
		}
		clip.delays = append(clip.delays, d)
	}
	clip.bytes = len(clip.frames) * cw * ch * 4
	return clip, nil
}

func cloneRGBA(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	return dst
}

// fitDown scales w×h so its long side is at most max, keeping aspect.
func fitDown(w, h, max int) (int, int) {
	if w <= max && h <= max {
		return w, h
	}
	if w >= h {
		return max, maxInt(1, h*max/w)
	}
	return maxInt(1, w*max/h), max
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// hostPath turns a slash-separated fs.FS name into a path os.Open will
// take. scan speaks fs.FS; the decoder needs a real file.
func hostPath(dir, name string) string {
	return filepath.Join(dir, filepath.FromSlash(name))
}

// gifCache holds decoded clips keyed by the clip's host path (id) — two
// sources can both have a demo.gif, and the same fs.FS name must not
// hand one branch's recording to another. It is UI-goroutine state:
// every read and write happens in a posted func or a command.
type gifCache struct {
	clips map[string]*gifClip
	order []string
	bytes int
}

func newGifCache() *gifCache { return &gifCache{clips: map[string]*gifClip{}} }

// get returns the cached clip for id only if it still matches the file
// on disk. A stale entry is dropped rather than returned — that is the
// whole point of keying on mtime and size.
func (c *gifCache) get(id string, key fileKey) *gifClip {
	clip, ok := c.clips[id]
	if !ok {
		return nil
	}
	if clip.key != key {
		c.drop(id)
		return nil
	}
	return clip
}

func (c *gifCache) put(clip *gifClip) {
	c.drop(clip.id)
	c.clips[clip.id] = clip
	c.order = append(c.order, clip.id)
	c.bytes += clip.bytes
	for c.bytes > gifCacheMax && len(c.order) > 1 {
		c.drop(c.order[0])
	}
}

func (c *gifCache) drop(id string) {
	clip, ok := c.clips[id]
	if !ok {
		return
	}
	delete(c.clips, id)
	c.bytes -= clip.bytes
	for i, n := range c.order {
		if n == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// player is the preview pane's animation clock.
//
// Its lifetime is the composition's: Start is called by the Composer
// when the tree goes live and the stop it returns is called by
// Composer.Close — on a hot reload, on teardown, and (because the
// browser stops playback explicitly before handing the terminal away) on
// a suspend. Nothing here outlives the pane it draws into.
//
// The ticker goroutine reads clip.delays, which are immutable, and posts
// the advance. It never Gets or Sets: the frame index is a property, and
// properties belong to the UI goroutine.
type player struct {
	post func(func())

	cache   *gifCache
	frame   *prop.Property[int]
	playing *prop.Property[bool]
	status  *prop.Property[string]

	clip      *gifClip
	src       string // gif path this playback belongs to (fs.FS name, for display)
	loading   string // gif path a worker is currently decoding (display)
	loadingID string // its host path — identity, like the cache key
	gen       int    // invalidates posts from a superseded playback

	done   chan struct{}
	exited chan struct{}
}

func newPlayer(status *prop.Property[string]) *player {
	return &player{
		cache:   newGifCache(),
		frame:   prop.NewSource(0),
		playing: prop.NewSource(false),
		status:  status,
	}
}

// Start makes the player live. Implementing Startable on the preview
// component is what hands the Composer the lifetime; see composer.go, which
// collects Startables during the same walk that finds key bindings.
func (p *player) Start(post func(func())) func() {
	p.post = post
	return func() {
		p.Stop()
		p.post = nil
	}
}

// Toggle is the `p` key: stop if anything is running or decoding, start
// otherwise. dir is the host root the GIF resolves under (the source
// root or the launch root — see gifFor), name the root-relative path.
func (p *player) Toggle(dir, name string, key fileKey) {
	if p.playing.Get() || p.loadingID != "" {
		msg := "playback stopped"
		if p.loadingID != "" {
			msg = "decode of " + p.loading + " cancelled"
		}
		p.Stop()
		p.status.Set(msg)
		return
	}
	if name == "" {
		p.status.Set("no GIF for this entry — press r to record one")
		return
	}
	id := hostPath(dir, name)
	if clip := p.cache.get(id, key); clip != nil {
		p.begin(clip)
		return
	}
	if p.post == nil {
		return // not composed; nothing may be scheduled
	}
	p.loading, p.loadingID = name, id
	p.gen++
	p.status.Set("decoding " + name + "…")
	gen, post := p.gen, p.post
	go func() {
		clip, err := decodeClip(dir, name, key)
		post(func() {
			if gen != p.gen || p.loadingID != id {
				return // superseded while we were decoding
			}
			p.loading, p.loadingID = "", ""
			if err != nil {
				p.status.Set("cannot play " + name + ": " + err.Error())
				return
			}
			p.cache.put(clip)
			p.begin(clip)
		})
	}()
}

// begin starts the clock for clip. Runs on the UI goroutine.
func (p *player) begin(clip *gifClip) {
	if clip.len() == 0 {
		p.status.Set("cannot play " + clip.path + ": no frames")
		return
	}
	p.stopTicker()
	p.gen++
	p.clip, p.src = clip, clip.path
	p.frame.Set(0)
	p.playing.Set(true)
	p.status.Set(fmt.Sprintf("playing %s — %d frames, %d×%d — p stops", clip.path, clip.len(), clip.w, clip.h))
	if p.post == nil {
		return
	}
	done, exited := make(chan struct{}), make(chan struct{})
	p.done, p.exited = done, exited
	gen, post := p.gen, p.post
	go func() {
		defer close(exited)
		// The goroutine keeps its OWN index. Reading the frame property
		// from here would be an off-thread read; both counters start at
		// zero and step together, so the delay always belongs to the
		// frame on screen.
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			case <-time.After(clip.delays[i%len(clip.delays)]):
			}
			post(func() { p.advance(gen) })
		}
	}()
}

// advance runs on the UI goroutine, posted. The generation check drops
// ticks that were already in flight when playback was replaced.
func (p *player) advance(gen int) {
	if gen != p.gen || p.clip == nil || !p.playing.Get() {
		return
	}
	p.frame.Set((p.frame.Get() + 1) % p.clip.len())
}

// Stop ends playback and, crucially, WAITS for the ticker goroutine to
// return. A stop that only signals leaves a window in which a goroutine
// is still alive across a terminal hand-off — exactly the class of bug
// the decoder-leak tripwire exists to catch, and the reason this is
// synchronous.
func (p *player) Stop() {
	p.loading, p.loadingID = "", ""
	p.gen++
	p.stopTicker()
	p.clip, p.src = nil, ""
	// Guarded because prop.Set invalidates unconditionally — it does not
	// compare. Stop is called before EVERY hand-off, almost always with
	// nothing playing, and an unguarded Set would repaint the pane each
	// time a demo is launched.
	if p.playing.Get() {
		p.playing.Set(false)
	}
}

func (p *player) stopTicker() {
	if p.done == nil {
		return
	}
	close(p.done)
	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
		// Cannot happen: the goroutine selects on done and Post never
		// blocks. Bounded anyway — a hung UI goroutine is worse than a
		// leaked ticker.
	}
	p.done, p.exited = nil, nil
}

// Playing reports whether a clip is animating, as a property so the
// pane's paint depends on it.
func (p *player) Playing() bool { return p.playing.Get() }

// Current is the frame to paint, read inside Render so the frame
// property becomes a dependency of the preview pane and nothing else:
// one component repaints per animation tick.
//
// The property is read FIRST, and the order is load-bearing. A
// dependency is recorded by the Get that happens, so short-circuiting
// past playing while clip was still nil left the pane with no
// subscription to the thing that starts playback — pressing `p` set a
// property nobody was listening to, and the first GIF frame appeared
// only when some unrelated change forced a repaint.
func (p *player) Current() *image.RGBA {
	if !p.playing.Get() || p.clip == nil {
		return nil
	}
	return p.clip.frames[p.frame.Get()%p.clip.len()]
}

// Source is the GIF path currently loaded or loading, "" when idle.
func (p *player) Source() string {
	if p.loading != "" {
		return p.loading
	}
	return p.src
}

// Stale reports whether what is playing no longer matches what the pane
// is showing — a different entry, the same path under a different source
// root, or the same file re-recorded in place.
func (p *player) Stale(dir, name string, key fileKey) bool {
	id := hostPath(dir, name)
	if p.loadingID != "" {
		return p.loadingID != id
	}
	if p.clip == nil {
		return false
	}
	return p.clip.id != id || p.clip.key != key
}

// fitCells sizes an image into a cols×rows rect, letterboxed. Halfblock
// packs two pixels into one cell, so the target grid is cols × rows*2
// and a halfblock pixel is very nearly square — which is why the aspect
// maths here is plain aspect maths with a factor of two in it.
func fitCells(imgW, imgH, cols, rows int) (int, int) {
	if imgW <= 0 || imgH <= 0 || cols <= 0 || rows <= 0 {
		return 0, 0
	}
	w := cols
	h := (w * imgH / imgW) / 2
	if h > rows {
		h = rows
		w = h * 2 * imgW / imgH
	}
	return maxInt(1, minInt(w, cols)), maxInt(1, minInt(h, rows))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// gifFor picks the GIF to play for a demo: a recording in the launch
// tree first, then the one checked in at the SOURCE's root. Preferring
// recordings/ means a fresh `r` supersedes the committed GIF the moment
// it lands; taking the fallback from the source means browsing a branch
// previews that branch's own GIF, not the launch tree's. The returned
// dir is the host root the path resolves under — what Toggle and Stale
// need to tell two sources' same-named GIFs apart.
func gifFor(env scanEnv, rec, name string) (string, string, fileKey, bool) {
	recorded := path.Join(recDir, rec+".gif")
	if key, ok := keyOf(env.rec, recorded); ok {
		return recorded, env.recRoot, key, true
	}
	if key, ok := keyOf(env.src, name+".gif"); ok {
		return name + ".gif", env.srcRoot, key, true
	}
	return "", "", fileKey{}, false
}
