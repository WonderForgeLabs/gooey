package main

import (
	"os"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The claim in scene.gooey's comment — that the border, the title and
// the help line are painted once and never again while the raster
// redraws thirty times a second — is a repaint claim, and the only thing
// that pins a repaint claim is a damage count.
//
// A cell assertion would not do it. "The plasma changed" is true whether
// one component repainted or eleven did, so it passes just as well on
// the day someone gives the Border a bound title and quietly puts the
// whole page back in the damage set every frame.
func TestOnlyTheSceneRepaintsPerFrame(t *testing.T) {
	show := NewShow(30, 0)
	ctx := show.Context()

	root, err := markup.Page(os.DirFS("."), "scene.gooey", ctx).Build()
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 60, 20)
	c.Frame() // first frame paints everything; that is not the claim

	show.frame.Set(show.frame.Get() + 1)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("advancing the frame counter painted %d components, want exactly 1 (the Scene)", painted)
	}

	// And again, because a one-off can be an accident of the first
	// composite rather than a property of the graph.
	show.frame.Set(show.frame.Get() + 1)
	if _, painted := c.Frame(); painted != 1 {
		t.Fatalf("second advance painted %d, want 1", painted)
	}
}

// Changing the effect moves the label as well as the raster, so this one
// must be MORE than 1 — and pinning it is what stops the label quietly
// going deaf. A test that only ever asserts "exactly 1" would pass
// forever with a label that never updated.
func TestChangingEffectRepaintsTheLabelToo(t *testing.T) {
	show := NewShow(30, 0)
	ctx := show.Context()

	root, err := markup.Page(os.DirFS("."), "scene.gooey", ctx).Build()
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 60, 20)
	c.Frame()

	show.Next()
	f, painted := c.Frame()
	if painted < 2 {
		t.Fatalf("changing effect painted %d components, want at least 2 (Scene + the name)", painted)
	}
	if !containsRow(f, "starfield") {
		t.Errorf("the effect name did not reach the cells after Next()")
	}
}

func containsRow(f *gooey.Frame, want string) bool {
	for y := 0; y < f.Cells.H; y++ {
		row := make([]rune, 0, f.Cells.W)
		for x := 0; x < f.Cells.W; x++ {
			row = append(row, f.Cells.At(x, y).Rune)
		}
		if contains(string(row), want) {
			return true
		}
	}
	return false
}

func contains(hay, needle string) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// The raster must stay inside its Grid row. Nothing in the app draws
// outside its own Bounds today, but the halfblock blit writes through
// Buffer.Set, which bounds-checks against the WHOLE buffer rather than
// against the component's rect — so a Measure/Arrange disagreement would
// paint over the border and the help line rather than being clipped, and
// the symptom would be colour hanging below the panel.
//
// Backgrounds are the tell: the raster is the only thing on this page
// that sets one.
func TestRasterStaysInsideItsRow(t *testing.T) {
	show := NewShow(30, 0)
	ctx := show.Context()
	root, err := markup.Page(os.DirFS("."), "scene.gooey", ctx).Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{40, 10}, {80, 24}, {120, 34}, {200, 50}, {24, 6}} {
		c := gooey.NewComposer(root, size[0], size[1])
		f, _ := c.Frame()

		rows := map[int]bool{}
		for y := 0; y < f.Cells.H; y++ {
			for x := 0; x < f.Cells.W; x++ {
				if f.Cells.At(x, y).Style.Bg.Set {
					rows[y] = true
					break
				}
			}
		}
		// Row 0 is the border, row 1 the effect name, and the last two
		// are the help line and the bottom border. None may be coloured.
		for _, y := range []int{0, 1, f.Cells.H - 2, f.Cells.H - 1} {
			if rows[y] {
				t.Errorf("%dx%d: row %d carries a raster background — the framebuffer is painting outside its track",
					size[0], size[1], y)
			}
		}
		if len(rows) == 0 {
			t.Errorf("%dx%d: no row carries a background at all — the raster did not draw, so this test proved nothing",
				size[0], size[1])
		}
	}
}
