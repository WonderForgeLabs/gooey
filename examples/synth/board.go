package main

// Board is the thing that turns keystrokes into notes.
//
// It is a component with no pixels and one job: be the focus stop, so
// that every key on this page arrives at HandleKey before it reaches
// anything else. That is the framework's own dispatch order — target
// first, then bubble — and it is why the letters that play notes do not
// also have to be spelled out as two dozen KeyBindings.
//
// The keys this component does NOT claim fall through and bubble to the
// page's KeyBindings, which is where the controls live, visibly, in
// markup.

import (
	"math"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
)

type Board struct {
	gooey.Base
	gooey.FocusState

	synth *Synth
}

func (b *Board) Measure(gooey.Size) gooey.Size { return gooey.Size{} }
func (b *Board) Render(*gooey.Frame)           {}

func (b *Board) HandleKey(ev input.KeyEvent) bool {
	if ev.Key != input.KeyRune || ev.Has(input.ModCtrl) || ev.Has(input.ModAlt) {
		return false
	}
	for _, k := range keyboard {
		if k.label == ev.Rune {
			b.synth.Press(k.note)
			return true
		}
	}
	// Not a note. Returning false is what lets q, the arrows and the
	// bracket keys reach the page's KeyBindings — a component that
	// consumed everything would be a component you could not leave.
	return false
}

// pow2 is 2^x. math.Exp2 would do, and this exists only so the note
// formula reads as the twelfth root of two that it is.
func pow2(x float64) float64 { return math.Exp2(x) }
