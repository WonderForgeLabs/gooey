package mcp

import (
	"os"
	"strings"
	"testing"
)

// TestScreenSizeStatesWhatTreeSnapshotCouldOnlyImply is #204: a client
// computing send_mouse coordinates had to infer the screen from the root
// component's arranged bounds, which equals the terminal only while the
// root happens to fill it.
//
// The numbers are the test app's own terminal (60x14), read from the
// fixture rather than written twice, so a harness that resizes moves both
// sides together.
func TestScreenSizeStatesWhatTreeSnapshotCouldOnlyImply(t *testing.T) {
	app, _, _, c := setup(t)

	got := c.json("screen_size", nil)
	if int(got["cols"].(float64)) != app.cols || int(got["rows"].(float64)) != app.rows {
		t.Errorf("screen_size reports %vx%v, want the terminal's %dx%d",
			got["cols"], got["rows"], app.cols, app.rows)
	}
	// THE CELL METRICS COME WITH IT because the graphics layer needs them
	// and a client that has to ask twice will ask once. They are terminal
	// capabilities, so they are reported whatever the scope.
	if got["cell_width"] == nil || got["cell_height"] == nil {
		t.Errorf("screen_size reports no cell metrics: %v", got)
	}
}

// TestAGuestIsToldItsIslandsSize is the half that makes the tool safe to
// add to a scoped session.
//
// A guest's whole screen IS its island — that is the fiction Screen
// already maintains by cropping, and a size tool that answered with the
// terminal would break it in the one direction that matters: a client
// told the screen is 60x14 when it may only touch a 60x3 border computes
// coordinates for cells it cannot reach, and send_mouse answers those
// with silence.
//
// The assertion that the two DIFFER is not decoration. If the island
// happened to fill the terminal, both arms would read the same and this
// test would pass against a tool that ignored the grant entirely.
func TestAGuestIsToldItsIslandsSize(t *testing.T) {
	guest, host := islandServer(t)

	g := guest.json("screen_size", nil)
	h := host.json("screen_size", nil)

	if g["cols"] == h["cols"] && g["rows"] == h["rows"] {
		t.Fatalf("the guest and the host are told the same size (%vx%v), so this test "+
			"cannot tell a scoped answer from an unscoped one", g["cols"], g["rows"])
	}
	// The island is a Border inside a VStack that also holds a Text, so
	// it is strictly shorter than the screen and no wider.
	if int(g["rows"].(float64)) >= int(h["rows"].(float64)) {
		t.Errorf("the guest is told %v rows and the host %v; the island is one of two "+
			"children of a VStack and cannot be as tall as the screen", g["rows"], h["rows"])
	}
	if int(g["cols"].(float64)) > int(h["cols"].(float64)) {
		t.Errorf("the guest is told %v columns, wider than the host's %v", g["cols"], h["cols"])
	}
}

// TestTheTutorialsToolInventoryIsComplete is the class, not the
// instance. The tutorial lists every tool by name in prose, and prose
// enumerating a set is the thing this repo keeps being wrong about — the
// inventory said "no tool reports terminal size" for as long as that was
// true and would have gone on saying it, because nothing reads the list
// but a human.
//
// DERIVED FROM v1Tools, so a tool added tomorrow fails here rather than
// leaving a tutorial that is quietly missing one. The reverse direction
// is deliberately NOT asserted: the page is free to mention a name that
// is not a tool.
func TestTheTutorialsToolInventoryIsComplete(t *testing.T) {
	const page = "../docs/learn/08-remote-control.md"
	body, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("reading %s: %v", page, err)
	}
	s := &Server{}
	tools := s.v1Tools()
	if len(tools) == 0 {
		t.Fatal("v1Tools is empty, so this guard would pass vacuously")
	}
	for _, tl := range tools {
		if !strings.Contains(string(body), "`"+tl.Name+"`") {
			t.Errorf("%s never names `%s`. The page's inventory is what a reader uses to "+
				"find a tool, and a tool missing from it does not exist as far as they "+
				"are concerned", page, tl.Name)
		}
	}
}
