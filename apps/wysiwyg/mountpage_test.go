package main

import (
	"os"
	"testing"

	"github.com/WonderForgeLabs/gooey/markup"
)

// mountPage builds the shipped page against an editor that already
// exists, which buildPage cannot do because it makes its own.
//
// It is what a fixture needs once any of the editor's own state lives in
// the MARKUP rather than in newEditor. ed.props is the case that forced
// it: the property browser's editing surface is a <ValueEditor> element,
// so an editor whose page was never built has a nil one and every
// beginEdit/commitEdit silently does nothing. A test written before that
// change goes on compiling and starts asserting nothing.
func mountPage(t *testing.T, ed *editor) {
	t.Helper()
	src, err := os.ReadFile(PageFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := markup.Build(src, ed.ctx); err != nil {
		t.Fatalf("the editor's own page does not load: %v", err)
	}
	ed.rebuild()
}
