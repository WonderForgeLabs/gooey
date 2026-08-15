package main

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// demoFS reads the real .gooey files into a MapFS so a test can mutate
// one of them — the point being that the contract these tests exercise
// is the SHIPPED markup, not a fixture that resembles it.
func demoFS(t *testing.T) fstest.MapFS {
	t.Helper()
	fsys := fstest.MapFS{}
	for _, n := range []string{"dashboard.gooey", "card.gooey", "badge.gooey"} {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		fsys[n] = &fstest.MapFile{Data: b}
	}
	return fsys
}

func demoCtx(fsys fstest.MapFS) *markup.Context {
	return &markup.Context{
		Values: map[string]any{
			"Ticking":   prop.NewSource(true),
			"Reqs":      prop.NewSource("1200"),
			"ReqsTrend": prop.NewSource("▁▂▃"),
			"Lat":       prop.NewSource("38.0"),
			"LatTrend":  prop.NewSource("▁▂▃"),
			"Errs":      prop.NewSource("3"),
			"ErrsTrend": prop.NewSource("▁▂▃"),
			"Gors":      prop.NewSource("86"),
			"GorsTrend": prop.NewSource("▁▂▃"),
			"Advance":   func() {},
			"Quit":      func() {},
		},
		Styles:   map[string]render.Style{},
		Includes: fsys,
	}
}

func TestDashboardLoadsWithDeclaredProperties(t *testing.T) {
	fsys := demoFS(t)
	if _, err := markup.Load(fsys, "dashboard.gooey", demoCtx(fsys)); err != nil {
		t.Fatal(err)
	}
}

// The payoff of declaring the surface: a misspelled attribute is a load
// error naming the control and the property, where before it was an
// entry in an implicit context that nothing ever read.
func TestDashboardTypoIsALoadError(t *testing.T) {
	fsys := demoFS(t)
	fsys["dashboard.gooey"] = &fstest.MapFile{
		Data: []byte(strings.Replace(string(fsys["dashboard.gooey"].Data),
			`Caption="per second"`, `Captoin="per second"`, 1)),
	}
	_, err := markup.Load(fsys, "dashboard.gooey", demoCtx(fsys))
	if err == nil {
		t.Fatal("expected a load error for the undeclared attribute")
	}
	for _, want := range []string{"card.gooey", `dependency property "Captoin"`, "declared: Caption, Title, Trend, Value"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// Title is Required, so dropping it fails the load rather than rendering
// an empty border.
func TestDashboardMissingRequiredIsALoadError(t *testing.T) {
	fsys := demoFS(t)
	fsys["dashboard.gooey"] = &fstest.MapFile{
		Data: []byte(strings.Replace(string(fsys["dashboard.gooey"].Data),
			`Title="requests"`, ``, 1)),
	}
	_, err := markup.Load(fsys, "dashboard.gooey", demoCtx(fsys))
	if err == nil {
		t.Fatal("expected a load error for the missing required attribute")
	}
	if !strings.Contains(err.Error(), `dependency property "Title" — required attribute missing`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A bound attribute is type-checked against the declaration: the
// dashboard's handles are strings, and a card that declared Value as an
// int would reject them by type, not by coincidence.
func TestDashboardBoundAttributeIsTypeChecked(t *testing.T) {
	fsys := demoFS(t)
	fsys["card.gooey"] = &fstest.MapFile{
		Data: []byte(strings.Replace(string(fsys["card.gooey"].Data),
			`Name="Value"   Type="string" Default="—"`, `Name="Value"   Type="int" Default="0"`, 1)),
	}
	_, err := markup.Load(fsys, "dashboard.gooey", demoCtx(fsys))
	if err == nil {
		t.Fatal("expected a load error for the mistyped binding")
	}
	if !strings.Contains(err.Error(), `dependency property "Value"`) ||
		!strings.Contains(err.Error(), "*prop.Property[int]") {
		t.Fatalf("unexpected error: %v", err)
	}
}
