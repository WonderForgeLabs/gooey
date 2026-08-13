package activitybar

import (
	"image"
	"os"
	"testing"
)

// The rail is drawn in pixels, and pixels are invisible to every
// screen-level check this repo has: screen_text reads the cell plane and
// agg records the cell plane, so both report blank where a sixel band is.
//
// That makes these tests the ONLY automated evidence the rail is right.
// They assert the image rather than the app, which is why Rail returns an
// image.Image — a component would have made the pixels reachable only
// through a composed frame that cannot see them.

// railFS is the real asset directory. The icons are the thing under test
// as much as the compositing is: a rail that renders four empty squares
// because the SVGs did not load is exactly the failure a fixture would
// hide.
func railFS() *Renderer {
	return &Renderer{fsys: os.DirFS("../../"), icons: DefaultIcons}
}

func at(img image.Image, x, y int) (r, g, b uint8) {
	rr, gg, bb, _ := img.At(x, y).RGBA()
	return uint8(rr >> 8), uint8(gg >> 8), uint8(bb >> 8)
}

func isMarker(img image.Image, y int) bool {
	r, g, b := at(img, 1, y) // inside markerW
	return r == marker.R && g == marker.G && b == marker.B
}

// isBlurredMarker is the unfocused cue. Separate from isMarker rather than
// a tolerance on it: the two states must be TELLABLE APART, and a fuzzy
// "close to blue" predicate would pass for both.
func isBlurredMarker(img image.Image, y int) bool {
	r, g, b := at(img, 1, y)
	return r == markerBlurred.R && g == markerBlurred.G && b == markerBlurred.B
}

// TestTheMarkerDimsWhenTheRailLosesFocus — reported against the running
// editor as "no idea where focus is".
//
// Written as a pair, like the marker-placement test: the focused rail shows
// the bright cue and NOT the dim one, the unfocused rail the reverse. One
// direction alone would pass against a rail that drew the same colour
// always, since isMarker would simply be false in both cases if the colour
// were wrong.
func TestTheMarkerDimsWhenTheRailLosesFocus(t *testing.T) {
	r := railFS()
	const sel = 1
	mid := sel*slotH + slotH/2

	focused := r.Rail(sel, true)
	if !isMarker(focused, mid) {
		t.Error("a focused rail does not show the bright marker")
	}
	if isBlurredMarker(focused, mid) {
		t.Error("a focused rail shows the dim marker")
	}

	blurred := r.Rail(sel, false)
	if !isBlurredMarker(blurred, mid) {
		t.Error("an unfocused rail does not dim the marker; focus is invisible")
	}
	if isMarker(blurred, mid) {
		t.Error("an unfocused rail still shows the bright marker")
	}
	// The cue must still be THERE. Which view is showing is true whether or
	// not the rail holds the keyboard, so losing focus may not erase it.
	if rr, gg, bb := at(blurred, 1, mid); rr == bg.R && gg == bg.G && bb == bg.B {
		t.Error("an unfocused rail erased the marker entirely; the selection is still real")
	}
}

// TestEveryIconAssetLoads is the precondition for everything below. It is
// separate from the drawing tests on purpose: "the rail is blank" and
// "the rail is wrong" are different failures and should not share a
// message.
func TestEveryIconAssetLoads(t *testing.T) {
	r := railFS()
	if err := r.Preload(); err != nil {
		t.Fatalf("icon assets: %v", err)
	}
	for _, ic := range DefaultIcons {
		img, err := r.icon(ic.File, active)
		if err != nil {
			t.Fatalf("%s: %v", ic.File, err)
		}
		if img.Bounds().Dx() != iconPx || img.Bounds().Dy() != iconPx {
			t.Errorf("%s rasterized at %v, want %dx%d — the SVG was decoded at its intrinsic size instead of the target",
				ic.File, img.Bounds().Size(), iconPx, iconPx)
		}
	}
}

// TestAnIconIsActuallyDrawn — a rasterizer that silently produced an
// empty image would satisfy every size assertion above. This requires
// pixels in the tint colour to exist.
func TestAnIconIsActuallyDrawn(t *testing.T) {
	r := railFS()
	for _, ic := range DefaultIcons {
		img, err := r.icon(ic.File, active)
		if err != nil {
			t.Fatal(err)
		}
		painted := 0
		for y := 0; y < iconPx; y++ {
			for x := 0; x < iconPx; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0x8000 {
					painted++
				}
			}
		}
		// A codicon covers a real fraction of its box. One stray pixel is
		// not a drawn icon.
		if painted < 20 {
			t.Errorf("%s: only %d opaque pixels — the SVG rasterized to (almost) nothing", ic.File, painted)
		}
	}
}

// TestTheTintIsAppliedBeforeRasterizing — currentColor is a CSS cascade
// the rasterizer has none for, so an unsubstituted document renders in
// oksvg's fallback rather than in the tint. Active and inactive must
// therefore produce visibly different pixels.
func TestTheTintIsAppliedBeforeRasterizing(t *testing.T) {
	r := railFS()
	a, err := r.icon(DefaultIcons[0].File, active)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.icon(DefaultIcons[0].File, inactive)
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < iconPx; y++ {
		for x := 0; x < iconPx; x++ {
			if _, _, _, al := a.At(x, y).RGBA(); al < 0xc000 {
				continue // only compare where the glyph is solid
			}
			r1, g1, b1 := at(a, x, y)
			r2, g2, b2 := at(b, x, y)
			if r1 != r2 || g1 != g2 || b1 != b2 {
				return // found a differing solid pixel: the tint took
			}
		}
	}
	t.Error("active and inactive icons are pixel-identical; fill=\"currentColor\" was not substituted")
}

// TestTheRailIsOneSlotPerIcon — the height is what the host reserves a
// track for, so a rail that disagrees with its icon count is a strip that
// gets clipped or a gap that never fills.
func TestTheRailIsOneSlotPerIcon(t *testing.T) {
	for _, n := range []int{1, 4} {
		r := &Renderer{fsys: os.DirFS("../../"), icons: DefaultIcons[:n]}
		img := r.Rail(0, true)
		if got, want := img.Bounds().Dy(), slotH*n; got != want {
			t.Errorf("%d icons: height %d, want %d", n, got, want)
		}
		if got, want := img.Bounds().Dx(), railW; got != want {
			t.Errorf("%d icons: width %d, want %d", n, got, want)
		}
	}
}

// TestTheMarkerIsBesideTheSelectedIconAndNowhereElse is the one that
// matters, and it is written as a pair: the marker must be present in the
// selected slot AND absent from every other. Asserting only presence
// would pass against a rail that marked every slot.
func TestTheMarkerIsBesideTheSelectedIconAndNowhereElse(t *testing.T) {
	r := railFS()
	for sel := range DefaultIcons {
		img := r.Rail(sel, true)
		for i := range DefaultIcons {
			mid := i*slotH + slotH/2
			got, want := isMarker(img, mid), i == sel
			if got != want {
				t.Errorf("sel=%d: slot %d marker=%v, want %v", sel, i, got, want)
			}
		}
	}
}

// TestSelectionChangesThePicture is the discrimination half for the
// computed in Builder: two different selections must produce two
// different images, or the rail is not drawn from Sel at all.
func TestSelectionChangesThePicture(t *testing.T) {
	r := railFS()
	a, b := r.Rail(0, true), r.Rail(2, true)
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			r1, g1, b1 := at(a, x, y)
			r2, g2, b2 := at(b, x, y)
			if r1 != r2 || g1 != g2 || b1 != b2 {
				return
			}
		}
	}
	t.Error("selecting a different icon produced an identical picture; the rail is not drawn from Sel")
}

// TestAnOutOfRangeSelectionStillDraws — Sel is an ordinary source
// property the host owns, so nothing stops it holding -1 or 99. A rail
// that panicked there would take the terminal down with it.
func TestAnOutOfRangeSelectionStillDraws(t *testing.T) {
	r := railFS()
	for _, sel := range []int{-1, 99} {
		img := r.Rail(sel, true)
		if img.Bounds().Dy() != slotH*len(DefaultIcons) {
			t.Errorf("sel=%d: wrong height", sel)
		}
		for i := range DefaultIcons {
			if isMarker(img, i*slotH+slotH/2) {
				t.Errorf("sel=%d marked slot %d; an out-of-range selection marks nothing", sel, i)
			}
		}
	}
}

// TestAMissingAssetIsALoadError — Preload exists so a missing icon fails
// where the file name can still be reported, not as a blank strip.
func TestAMissingAssetIsALoadError(t *testing.T) {
	r := &Renderer{fsys: os.DirFS("../../"), icons: []Icon{{Name: "nope", File: "nosuch.svg"}}}
	err := r.Preload()
	if err == nil {
		t.Fatal("a missing icon must be a load error")
	}
	if got := err.Error(); !contains(got, "nosuch.svg") {
		t.Errorf("the error must name the file, got: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
