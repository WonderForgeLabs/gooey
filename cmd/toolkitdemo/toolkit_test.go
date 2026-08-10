package main

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The contract these tests exercise is the SHIPPED markup, not a
// fixture that resembles it: the demo is the proof that every component
// in the wave can be spelled in markup, so the demo's own file is what
// gets loaded.

func demoFS(t *testing.T) fstest.MapFS {
	t.Helper()
	b, err := os.ReadFile("toolkit.gooey")
	if err != nil {
		t.Fatal(err)
	}
	return fstest.MapFS{"toolkit.gooey": &fstest.MapFile{Data: b}}
}

func demoCtx() *markup.Context {
	return &markup.Context{
		Values: map[string]any{
			"Pct":        prop.NewSource(40),
			"Busy":       prop.NewSource(false),
			"Running":    prop.NewSource(true),
			"StageIndex": prop.NewSource(1),
			"Stage":      prop.NewSource("Fetch"),
			"Log":        prop.NewSource("log"),
			"Status":     prop.NewSource("ready"),
			"Clock":      prop.NewSource("00:00:00"),
			"Tier":       prop.NewSource("chrome: kitty"),
			"Advance":    gooey.Command(func() {}),
			"TickClock":  gooey.Command(func() {}),
			"StageChanged": gooey.Command(func() {
			}),
			"Start":  gooey.Command(func() {}),
			"Abort":  gooey.Command(func() {}),
			"Reset":  gooey.Command(func() {}),
			"Deploy": gooey.Command(func() {}),
			"Quit":   gooey.Command(func() {}),
		},
		Styles: map[string]render.Style{
			"panel":  {},
			"accent": {},
			"dim":    {},
		},
	}
}

func TestDemoPageLoads(t *testing.T) {
	if _, err := markup.Load(demoFS(t), "toolkit.gooey", demoCtx()); err != nil {
		t.Fatal(err)
	}
}

// Every component in the wave has to actually be on the page — a demo
// that quietly lost one would still load.
func TestDemoShowsEveryComponentInTheWave(t *testing.T) {
	root, err := markup.Load(demoFS(t), "toolkit.gooey", demoCtx())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]int{}
	var walk func(gooey.Component)
	walk = func(w gooey.Component) {
		switch c := w.(type) {
		case *components.ProgressBar:
			found["ProgressBar"]++
		case *components.Spinner:
			found["Spinner"]++
		case *components.Toggle:
			found["Toggle"]++
		case *components.Segmented:
			found["Segmented"]++
		case *components.StatusBar:
			found["StatusBar"]++
		case *components.ButtonBar:
			found["ButtonBar"]++
		case *components.Button:
			if c.Chrome == components.ChromePixel {
				found["PixelButton"]++
			}
		}
		if ct, ok := w.(gooey.Container); ok {
			for _, ch := range ct.ChildComponents() {
				walk(ch)
			}
		}
	}
	walk(root)
	for _, want := range []string{
		"ProgressBar", "Spinner", "Toggle", "Segmented", "StatusBar", "ButtonBar", "PixelButton",
	} {
		if found[want] == 0 {
			t.Errorf("the demo page has no %s", want)
		}
	}
}

// The page composes into a real frame at the size the GIF is recorded
// at, and the parts that make the demo legible are on screen.
func TestDemoComposes(t *testing.T) {
	root, err := markup.Load(demoFS(t), "toolkit.gooey", demoCtx())
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 96, 22)
	c.Frame()
	var sb strings.Builder
	cells := c.Cells()
	for y := 0; y < 22; y++ {
		for x := 0; x < 96; x++ {
			sb.WriteRune(cells.At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	screen := sb.String()
	for _, want := range []string{"toolkit — wave 1", "build ", "job running", "Fetch", "start", "Deploy", "ready", "q: quit"} {
		if !strings.Contains(screen, want) {
			t.Errorf("the composed screen does not show %q", want)
		}
	}
}

// The disabled member is the conditional-command demonstration, so it
// has to be genuinely disabled when the condition says no.
func TestDemoAbortIsConditional(t *testing.T) {
	ctx := demoCtx()
	running := prop.NewSource(false)
	ctx.Values["Running"] = running
	ctx.Values["Abort"] = gooey.NewCommand(func() {}).When(running)
	root, err := markup.Load(demoFS(t), "toolkit.gooey", ctx)
	if err != nil {
		t.Fatal(err)
	}
	var abort *components.Button
	var walk func(gooey.Component)
	walk = func(w gooey.Component) {
		if b, ok := w.(*components.Button); ok && b.Content.Get() == "abort" {
			abort = b
		}
		if ct, ok := w.(gooey.Container); ok {
			for _, ch := range ct.ChildComponents() {
				walk(ch)
			}
		}
	}
	walk(root)
	if abort == nil {
		t.Fatal("the demo has no abort button")
	}
	c := gooey.NewComposer(root, 96, 22)
	c.Frame()
	if !c.Cells().At(abort.Bounds().X, abort.Bounds().Y).Style.Dim {
		t.Fatal("abort is not dim while its condition is false")
	}
	running.Set(true)
	c.Frame()
	if c.Cells().At(abort.Bounds().X, abort.Bounds().Y).Style.Dim {
		t.Fatal("abort is still dim after its condition became true")
	}
}
