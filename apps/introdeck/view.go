package main

// The bindable surface.
//
// Everything here is a VALUE. Not a sentence, not a formatted line, not
// a label with punctuation baked into it — the beat's id, the beat's
// title, the voice, the second count. deck.gooey composes those into
// what you read, because a Text's content already interpolates literals
// and paths into one computed with the right damage, and doing that job
// in Go would be a worse second copy of it.
//
// The test for whether something belongs here: could the markup have
// written it? "3 / 29" could — it is `{{.Slide}} / {{.Total}}`. The
// wrapped body of a narration paragraph could not, so it lives here.

import (
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func (d *Deck) Context() *markup.Context {
	ctx := &markup.Context{
		Values: d.values(),
		Styles: palette(),
		// The Include seam. With Includes set, <Counter/> resolves to
		// counter.gooey by convention — which is what lets beat 2.5 put
		// a file's text beside that same file's rendering without a
		// second copy of it existing anywhere.
		Includes: d.dir,
	}
	RegisterTerminal(ctx)
	return ctx
}

func (d *Deck) values() map[string]any {
	text := func(f func() string) *prop.Property[string] { return prop.NewComputed(f) }

	return map[string]any{
		// --- where we are -------------------------------------------
		"Part":      text(func() string { return d.beat().Part }),
		"BeatID":    text(func() string { return d.beat().ID }),
		"BeatTitle": text(func() string { return d.beat().Title }),
		"Voice":     text(func() string { return strings.ToUpper(d.beat().Voice) }),
		"SlideName": text(func() string { return d.beat().Slide }),
		"Slide":     text(func() string { return itoa(d.idx.Get() + 1) }),
		"Total":     text(func() string { d.rev.Get(); return itoa(len(d.beats)) }),

		// --- the words ----------------------------------------------
		"Body": text(d.body),

		// --- beat 2.5's two panes, one file --------------------------
		// The include declares no clock and no redraw instruction — it
		// reads a number and has a button that changes it. That read is
		// the whole reason it repaints, which is the slide's point: the
		// file on the left says nothing about updating.
		"CounterSource": d.source,
		"Count":         text(func() string { return itoa(d.count.Get()) }),
		"Bump":          gooey.Command(d.Bump),

		// Beat 3.7's receipt. It is App.PaintedLastFrame — the framework's
		// own damage count for the frame the click caused — and not a
		// number this deck computes, because a slide whose claim is "this
		// number is the truth" cannot be showing a number it invented.
		"Painted": text(func() string { return itoa(d.painted.Get()) }),

		// --- the clock ----------------------------------------------
		// Two handles rather than one "0:22 / 1:20" string: the markup
		// joins them, and the OVER marker is a separate element with its
		// own visibility instead of a suffix spliced into a label.
		"Now":    text(func() string { return clockOf(secs(d.elapsed.Get())) }),
		"Target": text(func() string { return clockOf(d.beat().Dur) }),
		"Hold":   text(func() string { return clockOf(d.beat().Hold) }),
		"Over":   prop.NewComputed(d.over),
		"Progress": prop.NewComputed(func() int {
			b, e := d.beat(), d.elapsed.Get()
			if b.Dur <= 0 {
				return 0
			}
			return clamp(e*100/int(b.Dur.Seconds()), 0, 100)
		}),

		// --- modes and status ---------------------------------------
		"Prompter": d.prompter,
		"Auto":     d.auto,
		"Status":   d.status,

		// --- the live readout inside the top island ------------------
		"Cpu":  d.cpu,
		"Mem":  d.mem,
		"Load": d.load,
		// The process table is a real list: Items adapts the typed slice,
		// the row template lays the columns out with Width and HAlign,
		// and ItemsView realizes only the rows that fit. A change to one
		// process Sets that row's values and repaints that row.
		"Procs": components.Items(d.procs, func(p Proc) map[string]any {
			return map[string]any{
				"PID":  itoa(p.PID),
				"Name": p.Name,
				"RSS":  gib(p.RSS),
			}
		}),
		"Sys": d.sysline,

		// --- what the keys and the Timer do --------------------------
		"Next":           gooey.Command(func() { d.Advance(1) }),
		"Prev":           gooey.Command(func() { d.Advance(-1) }),
		"First":          gooey.Command(func() { d.GoTo(0) }),
		"Last":           gooey.Command(func() { d.GoTo(len(d.beats) - 1) }),
		"Replay":         gooey.Command(d.Replay),
		"Hush":           gooey.Command(d.Hush),
		"Reload":         gooey.Command(func() { d.status.Set(d.Reload()) }),
		"TogglePrompter": gooey.Command(d.TogglePrompter),
		"ToggleAuto":     gooey.Command(d.ToggleAuto),
		"Tick":           gooey.Command(d.Tick),
		"ReloadCounter":  gooey.Command(d.ReloadCounter),
		"CounterLive":    d.counterLive,
		"Quit":           gooey.Command(d.Quit),
	}
}

// body is the one string Go really does own: prose re-wrapped to a
// column. Both reads happen before the branch, so neither drops out of
// the dependency set on the frames the branch does not take.
func (d *Deck) body() string {
	b, w, p := d.beat(), d.wrapAt.Get(), d.prompter.Get()
	if p {
		return wrapText(b.Speak, w)
	}
	if len(b.Lines) > 0 {
		return lines(b.Lines)
	}
	// No lines and no markup means a deliberate blank, and a blank is
	// what gets shown.
	//
	// This used to return "› live — the real app is on screen here" for a
	// live beat, which was a caption describing a thing that was not
	// there: nine beats had no staged slide, so the panel said the app
	// was on screen while being the only thing on screen. A slide that
	// narrates its own absence is worse than an empty one, because an
	// empty one reads as unfinished and this read as working. Those nine
	// beats now stage the real thing; if one ever loses its ```gooey
	// fence again, this returns "" and the gap is visible.
	return ""
}

func (d *Deck) over() bool {
	b, e := d.beat(), d.elapsed.Get()
	return b.Dur > 0 && e > int(b.Dur.Seconds())
}

func palette() map[string]render.Style {
	return map[string]render.Style{
		"panel":    {Fg: render.RGB(120, 90, 220)},
		"headline": {Fg: render.RGB(255, 170, 60), Bold: true},
		"body":     {Fg: render.RGB(226, 226, 232)},
		"dim":      {Fg: render.RGB(130, 130, 142)},
		"cue":      {Fg: render.RGB(110, 200, 160)},
		"warn":     {Fg: render.RGB(240, 110, 90), Bold: true},
		"island":   {Fg: render.RGB(90, 150, 210)},
		"mono":     {Fg: render.RGB(190, 200, 214)},
	}
}
