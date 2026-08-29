package gooey

import "github.com/WonderForgeLabs/gooey/input"

// Paste routing.
//
// A bracketed paste is one event carrying the whole payload (see
// input/paste.go for why the terminal has to bracket it and why it must
// not be re-split into keys). It needs its own dispatch for the same
// reason it needs its own kind: a paste has no gesture, so there is
// nothing for a KeyBinding to match and nothing for a mnemonic to claim.
// What it has is a TARGET — whatever is focused — and an ancestor chain
// to bubble through when the target does not want it.
//
// There is deliberately no PreviewPasteHandler. Every tunnelling phase
// in this framework exists because something real needed to swallow an
// event for the layer underneath (an overlay, a modal), and nothing
// needs to swallow a paste yet. Adding the phase now would be inventing
// a seam with no consumer, and the shape is already written down twice
// (PreviewKeyHandler, PreviewMouseHandler) for whoever finds the case.

// PasteHandler is the optional interface for components that consume a
// bracketed paste. Returning true stops propagation.
//
// A component that implements it takes on a policy it cannot avoid
// having: the payload is arbitrary bytes, including newlines, tabs and
// control characters, and inserting it verbatim is a choice rather than
// the absence of one.
type PasteHandler interface{ HandlePaste(input.PasteEvent) bool }

// DispatchPaste routes a paste to the focused component and then up its
// ancestors, first true wins.
//
// It returns false when nothing in the chain implements PasteHandler,
// and that false is load-bearing: an app that enables bracketed paste
// and handles it nowhere will DROP pastes that, with the mode off, would
// have arrived as keystrokes and been inserted one at a time. That is a
// real regression and the reason TextBox implements PasteHandler — see
// App.acquire, which turns the mode on by default.
func (m *FocusManager) DispatchPaste(ev input.PasteEvent) bool {
	start := m.Focused()
	if start == nil {
		start = m.root
	}
	for n := start; n != nil; n = m.parent[n] {
		if h, ok := n.(PasteHandler); ok && h.HandlePaste(ev) {
			return true
		}
	}
	return false
}

// HandlePaste routes a paste through the tree. See
// FocusManager.DispatchPaste.
func (c *Composer) HandlePaste(ev input.PasteEvent) bool { return c.focus.DispatchPaste(ev) }
