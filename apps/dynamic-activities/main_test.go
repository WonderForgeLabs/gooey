package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The page must build against the same viewmodel main installs — a demo
// whose markup and Values map have drifted apart fails at startup, in a
// terminal, where nobody is running a test. Building here catches it in
// CI instead, and covers the two things this page does that no other
// demo does: a Canvas raster bound to color properties, and an activity
// TYPE NAME that is a bound path rather than a literal.
func TestPageBuildsAgainstTheDemoViewmodel(t *testing.T) {
	// The temporal: namespace is a capability grant. Without a provider
	// registered the document is a load error, which is the point: the
	// star's Click cannot resolve unless the host granted it.
	markup.RegisterHandlers(temporalURI, nil)
	if _, err := buildPage(t); err == nil {
		t.Fatal("the page built with no temporal provider registered")
	} else if !strings.Contains(err.Error(), "temporal") {
		t.Fatalf("load error does not name the missing namespace: %v", err)
	}

	markup.RegisterHandlers(temporalURI, fakeTemporal{})
	t.Cleanup(func() { markup.RegisterHandlers(temporalURI, nil) })

	root, err := buildPage(t)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if root == nil {
		t.Fatal("build returned no tree")
	}
}

// ActivityList is the element the Python MCP server patches on every
// create and delete. Its Name is a contract between two processes, so
// pin it here rather than discovering it broken at runtime.
func TestActivityListIsNamed(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(".", "dynamicactivities.gooey"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{`Name="ActivityList"`, `Name="Star"`, `Name="Input"`} {
		if !strings.Contains(string(src), name) {
			t.Errorf("the page no longer declares %s; worker.py addresses it by that name", name)
		}
	}
}

// The TextBox must live OUTSIDE the subtree the Python side patches.
//
// PatchMarkup preserves focus (it re-resolves through the name table)
// but resets a TextBox's caret to 0, so a user typing into an input
// INSIDE a refreshed island loses their cursor position mid-word — see
// mcp/focuspatch_test.go, which pins that behavior. This worker patches
// ActivityList on every create and delete, i.e. while someone may well
// be typing the argument. Keeping Input a sibling rather than a child is
// what makes that harmless, and it is a layout property nothing else
// would catch if a later edit moved the input "next to the buttons".
func TestTheInputIsOutsideThePatchedSubtree(t *testing.T) {
	markup.RegisterHandlers(temporalURI, fakeTemporal{})
	t.Cleanup(func() { markup.RegisterHandlers(temporalURI, nil) })

	ctx, err := buildPageCtx(t)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	list, ok := ctx.Named["ActivityList"]
	if !ok {
		t.Fatal("the page declares no ActivityList element")
	}
	input, ok := ctx.Named["Input"]
	if !ok {
		t.Fatal("the page declares no Input element")
	}
	if inSubtree(list, input) {
		t.Error("the Input TextBox sits inside ActivityList; every create and delete " +
			"patches that subtree, which would reset the caret while the user is typing")
	}
}

func inSubtree(root, want gooey.Component) bool {
	if root == want {
		return true
	}
	c, ok := root.(gooey.Container)
	if !ok {
		return false
	}
	for _, kid := range c.ChildComponents() {
		if inSubtree(kid, want) {
			return true
		}
	}
	return false
}

// The status row is a StatusBar, and this reads the CELL PLANE rather
// than the tree: the interesting claim is not "the sections were built"
// but "they landed against the edges of the screen". A binding that
// resolves and then arranges off-screen is the failure the old
// hand-spaced Text had — at 100 columns it clipped its own tail, and
// nothing said so.
//
// It also pins the thing this adoption was for: no " · " anywhere. The
// separators were Go string concatenation, and StatusBar plus the HStack
// in its Centre slot own the arrangement now, so their reappearance in
// the rendered row means someone put them back.
// 200 columns, because that is the terminal this demo is for: the page's
// own comment sizes its artwork at roughly 247 cells across. The row asks
// for about 175 — the same length the concatenated string asked for, so
// this is not width the StatusBar introduced. What StatusBar changes is
// WHICH end gives way when there is not enough: the edges hold and the
// centre shortens, instead of the tail falling off unseen.
func TestTheStatusRowSectionsLandAgainstTheEdges(t *testing.T) {
	c, _ := statusPage(t, 200, 40)
	row := strings.Trim(statusRow(t, c), "│") // the page's Border, not the bar

	if strings.HasPrefix(row, " ") {
		t.Errorf("the backend section is not against the left edge: %q", row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), "ctrl+c: quit") {
		t.Errorf("the key hints are not against the right edge: %q", row)
	}
	if strings.TrimRight(row, " ") != row {
		t.Errorf("the row has trailing space, so the right section is not at the edge: %q", row)
	}
	if strings.Contains(row, "·") {
		t.Errorf("a · separator is back in the status row; StatusBar and its "+
			"HStack own the spacing now: %q", row)
	}
	// Order matters: the backend, then the three loopback servers, then
	// the keys. Walking left to right is what proves the Centre slot's
	// HStack laid its children out rather than stacking them.
	at := 0
	for _, want := range []string{
		"temporal 127.0.0.1:7233", "grpc 127.0.0.1:1", "mcp http://127.0.0.1:2",
		"activities 127.0.0.1:3", "ctrl+n", "ctrl+l", "ctrl+c",
	} {
		i := strings.Index(row[at:], want)
		if i < 0 {
			t.Fatalf("%q is missing or out of order in the status row: %q", want, row)
		}
		at += i + len(want)
	}
}

// One endpoint landing repaints ONE component. That is the whole reason
// the sections are components rather than one glued string: before this
// change every part of the row shared a single Text, so the gRPC address
// arriving from its listener repainted the mcp address and the key hints
// with it.
//
// The replacement is deliberately the SAME WIDTH. A width change moves
// the HStack's children, and a bounds change vacates cells, which makes
// the Composer clear the old rect and force-repaint everything it
// covered — see apps/wysiwyg's TestTheTwoModeLabelsAreTheSameWidth for
// the same effect measured. That is not a bug and not what this pins.
func TestOneEndpointRepaintsAlone(t *testing.T) {
	c, ctx := statusPage(t, 200, 40)
	if _, settled := c.Frame(); settled != 0 {
		t.Fatalf("the page had not settled: %d components repainted with nothing changed", settled)
	}
	mcpEndpoint := ctx.Values["McpEndpoint"].(*prop.Property[string])
	mcpEndpoint.Set("mcp http://127.0.0.1:9/mcp") // same rune count as the seed
	if _, painted := c.Frame(); painted != 1 {
		t.Errorf("one endpoint changing repainted %d components, want 1", painted)
	}
}

// statusPage builds the page with the four status properties seeded, and
// composes one frame. The seeds are same-width stand-ins for real
// addresses so a later Set can hold the layout still.
func statusPage(t *testing.T, cols, rows int) (*gooey.Composer, *markup.Context) {
	t.Helper()
	markup.RegisterHandlers(temporalURI, fakeTemporal{})
	t.Cleanup(func() { markup.RegisterHandlers(temporalURI, nil) })

	root, ctx, err := buildPageWithCtx(t)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for name, v := range map[string]string{
		"Backend":          "temporal 127.0.0.1:7233 queue gooey-dynamic-activities",
		"GrpcEndpoint":     "grpc 127.0.0.1:1",
		"McpEndpoint":      "mcp http://127.0.0.1:2/mcp",
		"ActivityEndpoint": "activities 127.0.0.1:3",
	} {
		p, ok := ctx.Values[name].(*prop.Property[string])
		if !ok {
			t.Fatalf("the viewmodel has no string property %q; the status row binds it", name)
		}
		p.Set(v)
	}
	c := gooey.NewComposer(root, cols, rows)
	c.Frame()
	return c, ctx
}

// statusRow is the rendered row holding the backend section — found by
// content rather than by index, so a page that grows a row does not turn
// this into a test of the wrong line.
func statusRow(t *testing.T, c *gooey.Composer) string {
	t.Helper()
	cells := c.Cells()
	cols, rows := c.Size()
	for y := range rows {
		var b strings.Builder
		for x := range cols {
			r := cells.At(x, y).Rune
			if r == 0 {
				r = ' '
			}
			b.WriteRune(r)
		}
		if line := b.String(); strings.Contains(line, "temporal 127.0.0.1:7233") {
			return line
		}
	}
	t.Fatal("no rendered row carries the backend section")
	return ""
}

func TestActivityNames(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"\n\n", 0},
		{"Alpha", 1},
		{"Alpha\nBeta\n", 2},
		{"  Alpha  \n\nBeta", 2},
	}
	for _, c := range cases {
		if got := activityNames(c.in); len(got) != c.want {
			t.Errorf("activityNames(%q) = %v, want %d names", c.in, got, c.want)
		}
	}
}

const temporalURI = "gooey.dev/handlers/temporal"

// fakeTemporal grants the namespace without a Temporal server: the
// command it returns is never invoked here, only resolved, which is
// exactly what the load-time check needs.
type fakeTemporal struct{}

func (fakeTemporal) NewCommand(c *markup.Call) (gooey.Command, error) {
	if c.Fn != "Activity" || len(c.Args) < 1 || !c.Target.Valid() {
		return nil, fmt.Errorf("temporal provides: Activity <name> [args...] | into .Target")
	}
	return func() {}, nil
}

// buildPage builds the demo page and reports only the tree — the shape
// most tests care about.
func buildPage(t *testing.T) (gooey.Component, error) {
	t.Helper()
	root, _, err := buildPageWithCtx(t)
	return root, err
}

// buildPageCtx is buildPage for the tests that need the CONTEXT rather
// than the tree: Named is the element table PatchMarkup addresses by, so
// a test asking "is this element inside that one" has to look it up
// there rather than walking from a root it cannot name.
func buildPageCtx(t *testing.T) (*markup.Context, error) {
	t.Helper()
	_, ctx, err := buildPageWithCtx(t)
	return ctx, err
}

func buildPageWithCtx(t *testing.T) (gooey.Component, *markup.Context, error) {
	t.Helper()
	ctx := &markup.Context{
		Values: map[string]any{
			"Input":        prop.NewSource(""),
			"Output":       prop.NewSource(""),
			"Selected":     prop.NewSource(""),
			"SelectedLine": prop.NewSource(""),
			"Activities":   prop.NewSource(""),
			"Note":         prop.NewSource(""),

			// The status row's four sections, one property each — the
			// page binds them into StatusBar's Left slot and the HStack
			// in its Center slot.
			"Backend":          prop.NewSource(""),
			"GrpcEndpoint":     prop.NewSource(""),
			"McpEndpoint":      prop.NewSource(""),
			"ActivityEndpoint": prop.NewSource(""),

			"StarColor": prop.NewSource(render.RGB(255, 170, 60)),
			"StarGlow":  prop.NewSource(render.RGB(255, 214, 120)),
			"Cycle":     gooey.Command(func() {}),
			"Clear":     gooey.Command(func() {}),
			"Quit":      gooey.Command(func() {}),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
		Includes:   os.DirFS("."),
		Dispatcher: gooey.NewDispatcher(),
	}
	root, err := markup.Load(os.DirFS("."), "dynamicactivities.gooey", ctx)
	return root, ctx, err
}
