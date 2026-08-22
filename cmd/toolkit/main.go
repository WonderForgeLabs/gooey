// toolkit: the whole component kit on one page — every shipped
// component alive at once, organized by a <Tabs> because thirty
// components on one flat page is a wall, not a demo.
//
//	job       ProgressBar, Spinner, Toggle, Segmented, ButtonBar, Button
//	          (cell and pixel chrome), Tooltip, Text, HStack, Grid
//	basics    Border, VStack, Grid, Text, TextBox, Checkbox, Button
//	data      Gauge, Sparkline, ItemsView + its DataTemplate
//	visual    Canvas, ColorPicker, Image
//	forms     Validate behaviors, inline error Texts, ValidationMarker
//	overlays  Popup (through a demo-local owner), ButtonBar, Tooltip
//
// The page chrome — MenuBar, StatusBar, ToastHost, AdornmentLayer — is
// declared OUTSIDE the Tabs, because it belongs to the app rather than
// to any one page (and an overlay declared inside a collapsed tab would
// be collapsed with it). The two Timers and the KeyBindings are at the
// root for the same reason.
//
// It is markup-first for the reason every demo here is: a component
// that cannot be spelled in markup is not finished. The one exception
// is deliberate and is itself part of the story — `components.Popup` is
// a Go-side primitive with no markup element, so the accent-preset
// picker on the "overlays" tab is a demo-local owner registered through
// Context.Components (see preset.go). Everything else in this file is a
// viewmodel: properties, commands, and the state the timers drive.
//
// Two things this file reads back OUT of the framework: the graphics
// tier, for the caption beside the pixel button (capabilities are a
// property of the composition, not of the viewmodel, so it is read once
// on the first frame and posted into an ordinary string property), and
// the validation error properties the <Validate> behaviors publish at
// load time, which the submit gate looks up at evaluation.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/cmd/internal/demomain"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
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

// kitRow is one line of the catalogue the "data" tab lists — the kit
// describing itself. It is also what makes the ItemsView demonstration
// honest: a list needs data with more than one field in it.
type kitRow struct {
	Name  string
	Where string
	Note  string
}

var catalogue = []kitRow{
	{"Text", "basics", "a text block; literal, bound, or a mix"},
	{"Border", "basics", "a titled box around exactly one child"},
	{"Grid", "basics", "Auto / fixed / star tracks, attached Row+Col"},
	{"VStack", "basics", "children top to bottom at their desired heights"},
	{"HStack", "job", "children left to right, with a Gap"},
	{"Canvas", "visual", "absolute placement by Canvas.Left/Top"},
	{"Button", "job", "the focus stop; cell chrome and pixel chrome"},
	{"ButtonBar", "job", "a toolbar and a focus scope that wraps"},
	{"Checkbox", "basics", "[x] label, bound two ways to one property"},
	{"Toggle", "job", "a rocker: ← is off, → is on"},
	{"Segmented", "job", "the rocker past two positions"},
	{"TextBox", "basics", "a single-line editor with selection and a kill buffer"},
	{"Tabs", "page", "a strip over one visible page — this control"},
	{"ItemsView", "data", "items + a template; rows windowed and reused"},
	{"ProgressBar", "job", "a meter when the number is known, a band when it is not"},
	{"Gauge", "data", "a 0-100 meter on the good/warn/crit ramp"},
	{"Sparkline", "data", "a series as stacked block rows, newest right"},
	{"Spinner", "job", "one glyph from a cycling set"},
	{"Timer", "page", "a command on an interval, posted to the loop"},
	{"StatusBar", "page", "three sections, each its own paint node"},
	{"MenuBar", "page", "titles across a row, dropdowns over the content"},
	{"ToastHost", "page", "transient messages, auto-dismissed"},
	{"AdornmentLayer", "page", "the adorner plane: tips and markers"},
	{"Tooltip", "job", "hover help, from an attribute or a child element"},
	{"Popup", "overlays", "the Go-side overlay primitive an owner wires up"},
	{"ColorPicker", "visual", "an RGB editor that adapts to the color depth"},
	{"Image", "visual", "a cell region on the pixel plane, halfblock elsewhere"},
	{"Validate", "forms", "DataAnnotations rules as a markup behavior"},
	{"ValidationMarker", "forms", "the floating error, in the adornment layer"},
	{"KeyBinding", "page", "a declared gesture, scoped by where it hangs"},
}

func main() {
	// -mode forces the pixel protocol the way cmd/pixels does. A capability
	// probe is a round trip to the terminal, and under a recording pty
	// the answer is a timeout — so the only way to exercise the pixel
	// chrome in a captured log is to say which protocol to speak.
	mode := flag.String("mode", "", "force graphics mode: kitty|sixel|iterm2|cells")
	hold := flag.Duration("hold", 0, "exit after this duration instead of waiting for q")
	flag.Parse()
	enc, forced, err := demomain.EncoderFor("-mode", *mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// --- viewmodel: the job (tab "job") ---
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

	// --- viewmodel: the page ---
	tab := prop.NewSource(0)
	hints := prop.NewSource(true) // the Checkbox on "basics" gates the job captions
	greeting := prop.NewSource("")

	// --- viewmodel: the meters on "data" ---
	load := prop.NewSource(12)
	history := prop.NewSource(make([]float64, 0, 64))

	// --- viewmodel: the catalogue list on "data" ---
	rows := prop.NewSource(catalogue)
	kit := components.Items(rows, func(r kitRow) map[string]any {
		return map[string]any{"Name": r.Name, "Where": r.Where, "Note": r.Note}
	})
	kitSel := prop.NewSource(0)
	kitName := prop.NewSource(catalogue[0].Name)
	kitNote := prop.NewSource(catalogue[0].Note)

	// --- viewmodel: the visual tab ---
	// One source colour, read by the ColorPicker, by a style, and by the
	// computed that GENERATES the image beside it. Moving a channel
	// re-runs the generator, because Image's Src is an ordinary handle.
	accent := prop.NewSource(presets[0].color)
	accentStyle := prop.NewComputed(func() render.Style {
		return render.Style{Fg: accent.Get(), Bold: true}
	})
	gradient := prop.NewComputed(func() image.Image { return gradientImage(accent.Get()) })
	presetIdx := prop.NewSource(0)

	// --- viewmodel: the form ---
	formName := prop.NewSource("")
	formEmail := prop.NewSource("")
	formTag := prop.NewSource("")
	formStatus := prop.NewSource("submit enables itself when both fields are valid")

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

	// sample feeds the Gauge and the Sparkline off the same series: the
	// load walks toward whatever the current stage costs. Slower than
	// the job tick on purpose — a sparkline of 120ms samples is a blur.
	samples := 0
	sample := func() {
		target := 8
		switch {
		case !running.Get():
			target = 4
		case clampIdx(stageIdx.Get()) == stageFetch:
			target = 68
		case clampIdx(stageIdx.Get()) == stageBuild:
			target = 55 + pct.Get()/4
		case clampIdx(stageIdx.Get()) == stageDeploy:
			target = 22
		}
		samples++
		// A deterministic wobble, so the shape is interesting without a
		// random source the demo would have to seed.
		v := target + int(9*math.Sin(float64(samples)/3)) + samples%5
		load.Set(clamp100(v))
		h := append(history.Get(), float64(load.Get()))
		if len(h) > 60 {
			h = h[len(h)-60:]
		}
		history.Set(h)
	}

	var ctx *markup.Context
	var sticky *components.Toast // the one toast the demo takes down by hand
	toastHost := func() *components.ToastHost {
		// Looked up per fire rather than captured, so a hot-reload swap —
		// which rebuilds Named — never leaves a command holding a dead
		// layer.
		h, err := markup.Find[*components.ToastHost](ctx, "Toasts")
		if err != nil {
			return nil
		}
		return h
	}
	selectKit := func() {
		i := kitSel.Get()
		if i < 0 || i >= len(catalogue) {
			return
		}
		kitName.Set(catalogue[i].Name)
		kitNote.Set(catalogue[i].Note)
	}

	ctx = &markup.Context{
		Values: map[string]any{
			"Pct": pct, "Busy": busy, "Running": running,
			"StageIndex": stageIdx, "Stage": stage,
			"Log": log, "Status": status, "Clock": clock, "Tier": tier,
			"Tab": tab, "Hints": hints, "Greeting": greeting,
			"Load": load, "History": history,
			"Kit": kit, "KitSel": kitSel, "KitName": kitName, "KitNote": kitNote,
			"Accent": accent, "AccentStyle": accentStyle, "Gradient": gradient,
			"Preset":   presetIdx,
			"FormName": formName, "FormEmail": formEmail, "FormTag": formTag,
			"FormStatus": formStatus,

			"Advance":   gooey.Command(advance),
			"Sample":    gooey.Command(sample),
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

			"TabChanged": gooey.Command(func() {
				status.Set("tab: " + tabNames[clampTab(tab.Get())])
			}),
			"ClearGreeting": gooey.Command(func() { greeting.Set("") }),

			"KitSelected": gooey.Command(selectKit),
			"KitActivate": gooey.Command(func() {
				selectKit()
				if h := toastHost(); h != nil {
					h.Show(kitName.Get() + " — " + kitNote.Get())
				}
			}),

			// The preset picker's Changed: it Set the index, this pushes
			// the colour into the property the ColorPicker edits.
			"PresetChanged": gooey.Command(func() {
				i := presetIdx.Get()
				if i < 0 || i >= len(presets) {
					return
				}
				accent.Set(presets[i].color)
				log.Set("accent preset → " + presets[i].name)
			}),
			"OpenPresets": gooey.Command(func() {
				if p, err := markup.Find[*colorPreset](ctx, "Presets"); err == nil {
					p.Open()
				}
			}),

			// Notify pops a toast over the page.
			"Notify": gooey.Command(func() {
				if h := toastHost(); h != nil {
					h.Show("job " + status.Get() + " · " + stages[clampIdx(stageIdx.Get())])
				}
			}),
			"Sticky": gooey.Command(func() {
				if h := toastHost(); h != nil {
					sticky = h.ShowFor("sticky: a negative duration never expires", -1)
				}
			}),
			"ClearToasts": gooey.Command(func() {
				if h := toastHost(); h != nil && sticky != nil {
					h.Dismiss(sticky)
					sticky = nil
				}
			}),
		},
		Styles: map[string]render.Style{
			"panel":  {Fg: render.RGB(120, 90, 220)},
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
			"err":    {Fg: render.RGB(235, 90, 85)},
		},
		Components: map[string]markup.Builder{
			// Popup has no markup element by design, so its owner is the
			// demo's one custom component. See preset.go.
			"ColorPreset": presetBuilder,
		},
	}

	// The submit gate reads the error properties the <Validate>
	// behaviors PUBLISH at load, looked up inside the computed so the
	// first evaluation (which happens after the page has loaded) finds
	// them. A rebuild republishes fresh Validate computeds over the same
	// FormName/FormEmail sources; this gate stays subscribed to the
	// first load's, which still track those sources — so it keeps
	// working, but an EDITED rule in the markup will not reach it until
	// the process restarts.
	canSubmit := prop.NewComputed(func() bool {
		for _, k := range []string{"FormNameErr", "FormEmailErr"} {
			p, ok := ctx.Values[k].(*prop.Property[string])
			if !ok || p.Get() != "" {
				return false
			}
		}
		return true
	})
	ctx.Values["Submit"] = gooey.NewCommand(func() {
		formStatus.Set("saved: " + formName.Get() + " <" + formEmail.Get() + ">")
	}).When(canSubmit)

	fsys := demomain.MarkupFS("toolkit", "toolkit.gooey")

	// The probe is what turns the pixel chrome on: without capabilities
	// there is no protocol and no cell size, and the button draws its
	// pill in box runes instead. Both are correct; the caption says
	// which one you are looking at.
	//
	// Pinning is all it takes: probe, or pin the protocol. The cell size
	// the chrome is generated at used to be pinned alongside it here;
	// App.caps supplies it now, so this demo no longer carries its own
	// copy of that rule (#322). What is left is identical in every demo
	// with a -mode flag, so it is demomain.GraphicsOptions rather than an
	// option list spelled out here.
	app = gooey.NewApp(markup.Page(fsys, "toolkit.gooey", ctx), demomain.GraphicsOptions(enc, forced)...)
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
		// No encoder means no pixel plane, and a cell size is a property
		// OF that plane: the caption used to print 10×20 there because the
		// demo passed those capabilities in itself, which was a number for
		// a resolution nothing was being generated at.
		enc := c.Graphics()
		if enc == nil {
			tier.Set("chrome: cells (no graphics protocol)")
			return
		}
		tier.Set(fmt.Sprintf("chrome: %s   cell %dx%dpx", enc.Name(), c.Caps().CellW, c.Caps().CellH))
	})
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

var tabNames = []string{"job", "basics", "data", "visual", "forms", "overlays"}

// gradientImage is the "visual" tab's Image source: a diagonal ramp into
// the picked colour, checkered so the halfblock tier has something to
// show. Generated in code on purpose — a demo that needs a binary asset
// checked in to prove Image works has proved the wrong thing.
func gradientImage(c render.Color) image.Image {
	const w, h = 192, 160
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := (float64(x)/w + float64(y)/h) / 2
			if (x/16+y/16)%2 == 0 {
				t *= 0.72 // the checker: same ramp, dimmer squares
			}
			img.Set(x, y, color.RGBA{
				R: uint8(float64(c.R) * t),
				G: uint8(float64(c.G) * t),
				B: uint8(float64(c.B) * t),
				A: 255,
			})
		}
	}
	return img
}

// The bound index properties are edited by components (a Segmented, a
// Tabs strip) and by commands, so every read of one is clamped rather
// than trusted: markup can bind an int, but it cannot declare a range.
func clampIdx(i int) int { return min(max(i, 0), len(stages)-1) }
func clampTab(i int) int { return min(max(i, 0), len(tabNames)-1) }
func clamp100(v int) int { return min(max(v, 0), 100) }
