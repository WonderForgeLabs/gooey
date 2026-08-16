package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The page is the demo's UI, and nothing in the build compiles it: a
// renamed value, a style name that is not registered, an unparseable
// gesture or a <CoreGauges Values=…> pointing at the wrong type are all
// load errors that would otherwise first appear as a demo that no longer
// starts. markup.Load resolves every one of them.
//
// markup/corpus_test.go already checks this file's ATTRIBUTE NAMES
// against the element vocabulary. It deliberately does not build, because
// building needs this context — which is the half that check cannot make
// and this one can.
func TestPageLoadsAgainstItsContext(t *testing.T) {
	ctx, stats := newContext(gooey.Command(func() {}))
	if stats == nil {
		t.Fatal("newContext returned a nil stats handle")
	}
	if _, err := markup.Load(pageFS, "sysmon.gooey", ctx); err != nil {
		t.Fatalf("loading sysmon.gooey: %v", err)
	}
}
