package main

import (
	"os"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The adoption, proved by BUILDING the page rather than by reading it.
//
// The repo-wide corpus test checks every .gooey file's attributes, which
// is a vocabulary check and not a build: it cannot see whether
// {{.ReloadCounter}} and {{.CounterLive}} resolve, because that needs
// this app's binding context. So the page is loaded here, against the
// real viewmodel, and the watcher is found on the tree.
//
// This is also the only place the deck's counter reload is now tested at
// all. It used to be a size+mtime stamp compared inside deck.go against
// a field, on the deck's own one-second Timer — one of the five
// hand-rolled watchers issue #272 counted, and the one this change
// deletes.
func loadDeck(t *testing.T) (*Deck, gooey.Component) {
	t.Helper()
	dir := os.DirFS(".")
	beats, err := ParseNarration(dir, "NARRATION.md")
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDeck(dir, beats, NewPlayer("audio"), 74, 0)
	if err != nil {
		t.Fatal(err)
	}
	root, err := markup.Load(dir, "deck.gooey", d.Context())
	if err != nil {
		t.Fatalf("deck.gooey no longer builds: %v", err)
	}
	return d, root
}

func deckWatcher(t *testing.T, root gooey.Component) *components.FileWatcher {
	t.Helper()
	a, ok := root.(gooey.Attacher)
	if !ok {
		t.Fatalf("the page root is %T and hosts no attachments", root)
	}
	for _, at := range a.Attachments() {
		if fw, ok := at.(*components.FileWatcher); ok {
			return fw
		}
	}
	t.Fatal("deck.gooey declares no <FileWatcher> — the counter pane went back to being polled by hand")
	return nil
}

func TestDeckDeclaresTheCounterWatcher(t *testing.T) {
	_, root := loadDeck(t)
	fw := deckWatcher(t, root)

	if got := fw.Paths.Get(); len(got) != 1 || got[0] != "counter.gooey" {
		t.Errorf("Paths = %q, want [counter.gooey]", got)
	}
	if got, want := fw.Interval, time.Second; got != want {
		t.Errorf("Interval = %v, want %v — the rate the hand-rolled stamp ran at", got, want)
	}
	if fw.FS == nil {
		t.Error("the watcher got no fs.FS, so counter.gooey resolves against nothing")
	}
	if !gooey.CanExecute(fw.Changed) {
		t.Error("Changed did not bind to ReloadCounter")
	}
}

// Enabled is the gate that used to be an if statement inside the tick.
// Declaring it as a computed is what makes the pause the graph's: the
// watcher stops firing while the prompter is up or the slide is not a
// staged one, with nothing torn down and no restart.
func TestDeckCounterWatcherPausesOffTheVimSlide(t *testing.T) {
	d, root := loadDeck(t)
	fw := deckWatcher(t, root)
	if fw.Enabled == nil {
		t.Fatal("the watcher has no Enabled gate, so it fires on every slide")
	}

	staged := -1
	for i, b := range d.beats {
		if b.Staged() {
			staged = i
			break
		}
	}
	if staged < 0 {
		t.Skip("no staged beat in NARRATION.md to gate against")
	}

	d.GoTo(staged)
	d.prompter.Set(false)
	if !fw.Enabled.Get() {
		t.Error("the watcher is paused on the slide it exists for")
	}
	d.prompter.Set(true)
	if fw.Enabled.Get() {
		t.Error("the watcher still fires with the prompter up")
	}
	d.prompter.Set(false)

	plain := -1
	for i, b := range d.beats {
		if !b.Staged() {
			plain = i
			break
		}
	}
	if plain < 0 {
		return
	}
	d.GoTo(plain)
	if fw.Enabled.Get() {
		t.Error("the watcher still fires on a beat with no staged slide")
	}
}
