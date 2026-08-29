package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/activitybar"
)

// namedBounds is boundsOf (drag.go) with the lookup and the failure the
// production helper has no use for: boundsOf answers a zero rect for a
// component that does not report one, which is right for a hit walk and
// wrong here — a page that never mounted the rail and a rail arranged to
// nothing are different facts, and this test is only about the second.
func namedBounds(t *testing.T, ed *editor, name string) gooey.Rect {
	t.Helper()
	c, ok := ed.ctx.Named[name]
	if !ok {
		t.Fatalf("the page mounted no element named %q", name)
	}
	if _, ok := c.(interface{ Bounds() gooey.Rect }); !ok {
		t.Fatalf("%s is %T, which does not report its bounds", name, c)
	}
	return boundsOf(c)
}

// The rail is a COLUMN. Reported from the running editor as "needs to go
// height to top of status bar": it stopped eight rows down, so the strip
// of icons read as a floating stripe rather than an edge of the window,
// and the column beneath it showed the page ground.
//
// Asserted against the DOCK rather than a literal. Both sit in the same
// grid row, so the dock's height IS the row's height — and a hardcoded
// number would go stale the first time a chrome row is added or removed,
// which is the failure this file would otherwise become.
func TestTheActivityRailRunsTheFullHeightOfItsRow(t *testing.T) {
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	t.Cleanup(c.Close)
	c.Frame()

	rail, dock := namedBounds(t, ed, "ActivityBar"), namedBounds(t, ed, "Dock")
	if dock.H <= 0 {
		t.Fatalf("the dock arranged to %dx%d; this test cannot measure the rail "+
			"against a row that is not there", dock.W, dock.H)
	}
	if rail.Y != dock.Y || rail.H != dock.H {
		t.Errorf("the rail occupies y=%d..%d and its row y=%d..%d — it stops short "+
			"of the status bar, and the column below the icons is left on the page "+
			"ground instead of the rail's own",
			rail.Y, rail.Y+rail.H, dock.Y, dock.Y+dock.H)
	}
}

// The half a naive fix breaks, and the reason this was not a one-line
// markup change. components.Image places at Cols: r.W, Rows: r.H — it
// SCALES the picture to whatever rect it is given. So simply dropping
// Height="8" hands it the whole column and smears four icons down 40-odd
// rows: full height, and wrong.
//
// The picture therefore keeps its own size inside a container that is
// full height, and this pins the two facts apart. Without it, a rail that
// stretched would pass the test above.
func TestTheFullHeightRailDoesNotStretchItsIcons(t *testing.T) {
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	t.Cleanup(c.Close)
	c.Frame()

	rail := namedBounds(t, ed, "ActivityBar")
	pic := pictureIn(t, ed.ctx.Named["ActivityBar"])

	// Derived from the rail's own geometry rather than written down, so
	// adding a fifth icon does not turn this red for the wrong reason.
	_, want := activitybar.RailCells(len(activitybar.DefaultIcons))
	if pic.H != want {
		t.Errorf("the picture is %d rows, want %d — one slot per icon. A picture "+
			"as tall as its column (%d) means Image scaled it to the rect and the "+
			"icons are stretched", pic.H, want, rail.H)
	}
	if pic.Y != rail.Y {
		t.Errorf("the picture starts at y=%d but the rail at y=%d; the icons must "+
			"sit at the TOP of the column, not be centred in it", pic.Y, rail.Y)
	}
	if pic.H >= rail.H {
		t.Errorf("the picture (%d rows) fills its column (%d) — then this test and "+
			"the full-height one cannot both be meaningful, and the stretch this "+
			"exists to catch would be invisible", pic.H, rail.H)
	}
}

// pictureIn finds the one component under the rail that reports a
// non-zero rect smaller than its parent — the picture. Walking rather
// than asserting a concrete type: the wrapper is an implementation
// detail of activitybar.Builder and this test is about geometry.
func pictureIn(t *testing.T, root gooey.Component) gooey.Rect {
	t.Helper()
	var found gooey.Rect
	var walk func(gooey.Component, int)
	walk = func(c gooey.Component, depth int) {
		if depth > 8 {
			return
		}
		if depth > 0 {
			if r := boundsOf(c); r.H > 0 && (found.H == 0 || r.H < found.H) {
				found = r
			}
		}
		if ct, ok := c.(gooey.Container); ok {
			for _, k := range ct.ChildComponents() {
				walk(k, depth+1)
			}
		}
	}
	walk(root, 0)
	if found.H == 0 {
		t.Fatal("nothing under the rail reports a rect; the picture is not mounted")
	}
	return found
}

// Geometry is not paint. The two tests above pin the rects — the column
// is full height, the picture is not — and both would pass just as well
// if the column below the icons were left blank, which is the thing that
// was actually reported. This reads the cells.
//
// The rail's ground is opaque and the page's is not the same colour, so
// the discriminating question is whether a cell far below the icons
// carries the rail's background rather than the page's. Sampled at the
// BOTTOM of the column rather than just under the icons: an off-by-one
// in the container's height would leave the last rows bare and a sample
// taken at row 9 could not tell.
func TestTheColumnBelowTheIconsIsPaintedInTheRailsOwnGround(t *testing.T) {
	ed, root := buildPage(t)
	c := gooey.NewComposer(root, 150, 44)
	t.Cleanup(c.Close)
	f, _ := c.Frame()

	rail := namedBounds(t, ed, "ActivityBar")
	_, picRows := activitybar.RailCells(len(activitybar.DefaultIcons))
	if rail.H <= picRows+1 {
		t.Fatalf("the rail is %d rows and the picture %d; there is no column below "+
			"the icons to sample, so this test cannot see its subject",
			rail.H, picRows)
	}

	// The last row of the column, and one column in from its left edge.
	x, y := rail.X, rail.Y+rail.H-1
	got := f.Cells.At(x, y).Style.Bg
	want := activitybar.Ground()
	if got != want {
		t.Errorf("the cell at the bottom of the rail (%d,%d) has background %+v, "+
			"want the rail's own %+v — the column reaches the status bar but is "+
			"not painted, so the icons still read as a floating stripe",
			x, y, got, want)
	}

	// And the page beside it is NOT that colour, or the assertion above
	// would pass against a page that happened to share a ground and this
	// test would be pinning nothing.
	if beside := f.Cells.At(rail.X+rail.W+1, y).Style.Bg; beside == want {
		t.Errorf("the page beside the rail has the same background %+v, so "+
			"matching it proves nothing about the column", beside)
	}
}
