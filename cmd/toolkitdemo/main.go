// toolkitdemo: the UI toolkit on one page — wave 1 (ProgressBar,
// Spinner, Toggle, Segmented, StatusBar, ButtonBar, and a Button
// wearing the pixel chrome) plus wave 2's overlays: a MenuBar over the
// content and a ToastHost that pops transient notifications. The
// overlay elements are declared LAST in the markup, because document
// order is z-order and an overlay is nothing more than a later sibling
// painting above what it covers. The adornment layer sits at the very
// end of the file: the buttons carry Tooltip="..." shorthands (the
// toast button spells the child form, with a gesture hint), and resting
// the pointer on one shows its tip through that layer.
//
// It is markup-first for the reason every demo here is: a component that
// cannot be spelled in markup is not finished. Every one of these has a
// builder with typed attribute resolution, so the page below is the
// whole UI and this file is only a viewmodel — properties, commands, and
// the two Timers' worth of state they drive.
//
// The one thing this file reads back OUT of the framework is the
// graphics tier, for the caption beside the pixel button. Capabilities
// are a property of the composition, not of the viewmodel, so it is
// read once on the first frame and posted into an ordinary string
// property from there.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

var stages = []string{"Idle", "Fetch", "Build", "Deploy"}

// The stages the viewmodel actually reasons about. Fetch is the one with
// no measurable progress, which is what puts the bar in its
// indeterminate mode.
const (
	stageIdle = iota
	stageFetch
	stageBuild
	stageDeploy
)

func main() {
	// -mode forces the pixel protocol the way cmd/demo does. A capability
	// probe is a round trip to the terminal, and under a recording pty
	// the answer is a timeout — so the only way to exercise the pixel
	// chrome in a captured log is to say which protocol to speak.
	mode := flag.String("mode", "", "force graphics mode: kitty|sixel|iterm2|cells")
	hold := flag.Duration("hold", 0, "exit after this duration instead of waiting for q")
	flag.Parse()
	enc, forced, err := encoderFor(*mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// --- viewmodel ---
	pct := prop.NewSource(0)
	running := prop.NewSource(true)
	stageIdx := prop.NewSource(1)
	log := prop.NewSource("start the job, or push the rocker off to see the bar go indeterminate")
	status := prop.NewSource("ready")
	clock := prop.NewSource(time.Now().Format("15:04:05"))
	tier := prop.NewSource("(detecting…)")

	// The indeterminate mode is DERIVED, and derived from the stage the
	// job is in: fetching has no measurable progress, so the bar says
	// "working" rather than lying about a percentage, and it becomes a
	// real meter the moment building starts. Nothing subscribes to
	// anything — the bar read this computed while painting, which is the
	// whole subscription.
	busy := prop.NewComputed(func() bool { return running.Get() && stageIdx.Get() == stageFetch })
	stage := prop.NewComputed(func() string { return stages[clampIdx(stageIdx.Get())] })

	var app *gooey.App
	fetched := 0 // ticks spent fetching; UI-goroutine state, not a property
	advance := func() {
		if !running.Get() {
			return
		}
		switch clampIdx(stageIdx.Get()) {
		case stageFetch:
			if fetched++; fetched > 24 {
				stageIdx.Set(stageBuild)
				log.Set("fetch done — the bar has a number now, so it stops marching")
			}
		case stageBuild:
			v := pct.Get()
			if v < 100 {
				pct.Set(v + 2)
				return
			}
			stageIdx.Set(stageDeploy)
			status.Set("deployed")
			log.Set("build finished — reset to run it again")
		}
	}

	var ctx *markup.Context
	ctx = &markup.Context{
		Values: map[string]any{
			"Pct": pct, "Busy": busy, "Running": running,
			"StageIndex": stageIdx, "Stage": stage,
			"Log": log, "Status": status, "Clock": clock, "Tier": tier,
			"Advance":   gooey.Command(advance),
			"TickClock": gooey.Command(func() { clock.Set(time.Now().Format("15:04:05")) }),
			// Picking a stage by hand rewinds the job to it, which is how
			// the demo gets you back to the indeterminate bar without a
			// restart.
			"StageChanged": gooey.Command(func() {
				i := clampIdx(stageIdx.Get())
				fetched = 0
				if i <= stageFetch {
					pct.Set(0)
				}
				log.Set("stage → " + stages[i])
			}),
			"Start": gooey.Command(func() {
				running.Set(true)
				fetched = 0
				pct.Set(0)
				stageIdx.Set(stageFetch)
				status.Set("running")
				log.Set("job started — fetching, so the bar has nothing to count yet")
			}),
			// Abort is the conditional command in the bar: it asks its
			// condition while PAINTING, so the member goes dim and
			// refuses the moment the job stops, with no event anywhere.
			"Abort": gooey.NewCommand(func() {
				running.Set(false)
				status.Set("aborted")
				log.Set("job aborted — the spinner parks and the bar holds")
			}).When(running),
			"Reset": gooey.Command(func() {
				pct.Set(0)
				fetched = 0
				running.Set(false)
				stageIdx.Set(stageIdle)
				status.Set("ready")
				log.Set("reset")
			}),
			"Deploy": gooey.Command(func() {
				stageIdx.Set(len(stages) - 1)
				log.Set("deploying — the pixel button is an ordinary Button with different chrome")
			}),
			"Quit": gooey.Command(func() { app.Quit() }),
			// Notify pops a toast over the page. The host is looked up
			// per fire rather than captured, so a hot-reload swap — which
			// rebuilds Named — never leaves this holding a dead layer.
			"Notify": gooey.Command(func() {
				if toasts, err := markup.Find[*components.ToastHost](ctx, "Toasts"); err == nil {
					toasts.Show("job " + status.Get() + " · " + stages[clampIdx(stageIdx.Get())])
				}
			}),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	dir := "cmd/toolkitdemo"
	if _, err := os.Stat(filepath.Join(dir, "toolkit.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}
	fsys := os.DirFS(dir)

	// The probe is what turns the pixel chrome on: without capabilities
	// there is no protocol and no cell size, and the button draws its
	// pill in box runes instead. Both are correct; the caption says
	// which one you are looking at.
	var opts []gooey.Option
	if forced {
		// A forced protocol still needs a cell size — the chrome is
		// generated at that resolution — and only a probe can really
		// know it, so assume the common 10×20.
		opts = append(opts,
			gooey.WithGraphics(enc),
			gooey.WithCaps(term.Caps{CellW: 10, CellH: 20, Color: term.DetectColorDepth()}))
	} else {
		opts = append(opts, gooey.WithCapabilityProbe())
	}
	app = gooey.NewApp(markup.Page(fsys, "toolkit.gooey", ctx), opts...)
	if *hold > 0 {
		app.Every(*hold, app.Quit)
	}
	told := false
	app.BeforeFrame(func() {
		if told {
			return
		}
		told = true
		c := app.Composer()
		name := "cells (no graphics protocol)"
		if enc := c.Graphics(); enc != nil {
			name = enc.Name()
		}
		tier.Set(fmt.Sprintf("chrome: %s   cell %dx%dpx", name, c.Caps().CellW, c.Caps().CellH))
	})
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

// encoderFor resolves -mode. "cells" is a real answer, not the absence
// of one: it forces the universal tier, which is what you want when
// checking that the pill still reads without a pixel plane.
func encoderFor(mode string) (enc graphics.Encoder, forced bool, err error) {
	switch mode {
	case "":
		return nil, false, nil // capabilities decide
	case "kitty":
		return graphics.Kitty{}, true, nil
	case "sixel":
		return graphics.Sixel{}, true, nil
	case "iterm2":
		return graphics.ITerm2{}, true, nil
	case "cells":
		return nil, true, nil
	}
	return nil, false, fmt.Errorf("unknown -mode %q: want kitty, sixel, iterm2 or cells", mode)
}

func clampIdx(i int) int {
	if i < 0 {
		return 0
	}
	if i >= len(stages) {
		return len(stages) - 1
	}
	return i
}
