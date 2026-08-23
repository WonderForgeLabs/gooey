package toolbox

import (
	"image/color"
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/prop"
)

// fsys builds a set over the app root — the directory Dir is relative
// to. These tests read the SHIPPED assets rather than a fixture, because
// half of what is being checked is that the files the catalog names are
// actually on disk under the names it uses.
func fsys() *Icons {
	return New(os.DirFS("../.."), prop.NewSource[color.Color](onDark))
}

var (
	onDark  = color.RGBA{0xc8, 0xcd, 0xdc, 0xff}
	onLight = color.RGBA{0x3a, 0x3f, 0x4c, 0xff}
)

func TestForReturnsTheSameHandlePerName(t *testing.T) {
	// The cache is a DAMAGE contract, not a speed one. The palette
	// re-projects on every document revision, and ItemsView answers a
	// re-projected *prop.Property[image.Image] by comparing the handle
	// pointer first. Handing back a fresh computed each time would push
	// the decision onto the picture compare below it, which is true only
	// while the raster cache underneath keeps returning the same image —
	// a guarantee that belongs to another type and could change without
	// this one noticing.
	ic := fsys()
	a, b := ic.For("check"), ic.For("check")
	if a == nil {
		t.Fatal("For returned no handle for a name that exists")
	}
	if a != b {
		t.Fatal("For handed out two handles for one name; ItemsView's pointer compare cannot see through that")
	}
	if c := ic.For("table"); c == a {
		t.Fatal("two different names share one handle")
	}
}

func TestForDeclinesAnEmptyName(t *testing.T) {
	// An element that declares no icon must reach the row as an ABSENCE.
	// A placeholder here would make "declares no icon" and "declares
	// this icon" render alike, which is the dishonesty the catalog's
	// AttrsKnown split exists to prevent, one field over.
	if h := fsys().For(""); h != nil {
		t.Fatalf("For(\"\") returned a handle (%v); an undeclared icon must stay undeclared", h)
	}
}

func TestFlippingTheTintReRastersTheIcon(t *testing.T) {
	// THE POINT OF THE WHOLE DESIGN. The tint is read inside For's
	// computed, so it is a dependency; flipping it produces a different
	// picture with no invalidate call and no cache to sweep.
	//
	// Reading the tint OUTSIDE the closure compiles, loads, renders, and
	// is silently wrong forever — the icon would keep its first colour
	// and nothing in the framework would report a picture that stopped
	// changing. That is the mutation this test exists to catch, so it
	// compares PIXELS rather than identity: two handles could differ
	// while drawing the same thing.
	tint := prop.NewSource[color.Color](onDark)
	ic := New(os.DirFS("../.."), tint)
	if err := ic.Preload([]string{"check"}, onDark, onLight); err != nil {
		t.Fatal(err)
	}
	h := ic.For("check")

	first := h.Get()
	if first == nil {
		t.Fatal("no picture for a preloaded icon")
	}
	tint.Set(onLight)
	second := h.Get()
	if second == nil {
		t.Fatal("no picture after the tint changed")
	}

	// A codicon is mostly transparent, so comparing a fixed pixel would
	// compare two zeroes. Find one the glyph actually covers, then ask
	// whether the two rasters disagree there.
	b := first.Bounds()
	var moved bool
	for y := b.Min.Y; y < b.Max.Y && !moved; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := first.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			if first.At(x, y) != second.At(x, y) {
				moved = true
				break
			}
		}
	}
	if !moved {
		t.Fatal("the two tints rasterized to identical pixels: the tint is not being read inside the computed")
	}
}

func TestPreloadNamesAMissingAsset(t *testing.T) {
	// Painting cannot report an error, so a broken asset has exactly one
	// place it can be stated. An error that did not name the file would
	// leave the reader diffing a directory against a catalog by hand.
	err := fsys().Preload([]string{"no-such-icon"}, onDark)
	if err == nil {
		t.Fatal("a missing asset preloaded without complaint")
	}
	if !strings.Contains(err.Error(), "no-such-icon.svg") {
		t.Fatalf("the error does not name the file: %v", err)
	}
}

func TestPreloadSkipsAnUndeclaredIcon(t *testing.T) {
	// An element with no icon is a fact about the catalog, not a fault
	// in it — so an empty name must not become a request for ".svg".
	// Without the skip this fails with `open .svg: file does not exist`,
	// which would make every app registering a plain Builder fail to
	// start.
	if err := fsys().Preload([]string{"", "check"}, onDark); err != nil {
		t.Fatalf("an undeclared icon must be skipped, not rejected: %v", err)
	}
}

func TestAnUnsubstitutedCurrentColorIsAnError(t *testing.T) {
	// The field note from imagefmt/svg, pinned where this package's
	// callers will read it: a codicon declares fill="currentColor",
	// which names a CSS cascade the rasterizer does not implement.
	// Reaching it unsubstituted is `param mismatch` — an ERROR, not a
	// black glyph — which is the good outcome, and the reason New takes
	// a tint handle rather than treating colour as optional decoration.
	err := fsys().Preload([]string{"check"}) // no tints: "as authored"
	if err == nil {
		t.Fatal("rasterizing currentColor with no tint succeeded; the note in svg.IconSet.At no longer holds")
	}
}
