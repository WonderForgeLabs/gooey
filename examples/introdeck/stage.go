package main

// The stage: how a beat's slide gets on screen.
//
// A slide is markup, and markup lives in markup files. NARRATION.md's
// ```gooey fences hold whole <Gooey> documents and stage.gooey holds the
// default one; this file only chooses between them and hands the bytes
// over. Nothing here builds markup, wraps it in an envelope, or splices
// an attribute into it — every shape that reaches the loader is a shape
// a person wrote and the loader has already checked.
//
// The call is control.Service.PatchMarkup, which is the same call
// patch_markup makes over MCP. The deck deliberately has no private
// route to change itself: if this path were special, the claim in beat
// 4.3 — that the agent surface is the only surface — would be false
// about the very program making it.

// stageSource is the document that belongs in the Stage slot right now.
//
// The prompter never gets a staged slide. Its job is the words, and a
// live system readout is exactly the wrong thing to be reading off while
// speaking.
func (d *Deck) stageSource() string {
	if d.prompter.Get() {
		return d.plain
	}
	if b := d.beat(); b.Staged() {
		return b.Markup
	}
	return d.plain
}

// Restage patches the Stage slot if what belongs there has changed.
//
// A failure is reported to the status line rather than returned: a bad
// fence in the script should leave the previous slide up and say so, not
// take the deck down mid-take.
func (d *Deck) Restage() {
	if d.svc == nil {
		return // before the app exists; the declared tree is already right
	}
	src := d.stageSource()
	if src == d.staged {
		return
	}
	if _, err := d.svc.PatchMarkup("Stage", src); err != nil {
		d.status.Set("stage: " + err.Error())
		return
	}
	d.staged = src
}
