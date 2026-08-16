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
			"Status":       prop.NewSource(""),
			"StarColor":    prop.NewSource(render.RGB(255, 170, 60)),
			"StarGlow":     prop.NewSource(render.RGB(255, 214, 120)),
			"Cycle":        gooey.Command(func() {}),
			"Clear":        gooey.Command(func() {}),
			"Quit":         gooey.Command(func() {}),
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
