package main

// What the keys do. Each is one verb, bound in deck.gooey by a
// KeyBinding, so the vocabulary of the deck is readable in the markup
// rather than assembled here.

// Replay restarts the current beat's audio without moving the deck —
// the one you want after a fluffed line.
func (d *Deck) Replay() {
	d.elapsed.Set(0)
	d.status.Set(d.player.Play(d.beat().ID))
}

func (d *Deck) Hush() {
	d.player.Stop()
	d.status.Set("stopped")
}

// TogglePrompter swaps the camera's slide for the presenter's. It has to
// restage, because the two modes are two different documents in the
// Stage slot, not two branches inside one.
func (d *Deck) TogglePrompter() {
	d.prompter.Set(!d.prompter.Get())
	d.Restage()
}

// ToggleAuto resets the clock as well, so turning auto on mid-beat gives
// that beat its whole duration rather than whatever was left of it.
func (d *Deck) ToggleAuto() {
	d.auto.Set(!d.auto.Get())
	d.elapsed.Set(0)
}

func (d *Deck) Quit() {
	d.player.Stop()
	d.app.Quit()
}
