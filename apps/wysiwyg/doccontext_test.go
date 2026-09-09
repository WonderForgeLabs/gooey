package main

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// The designer builds the document being edited with a DIFFERENT context
// from the one it builds its own chrome with, and only the chrome's got
// the Dispatcher. So the canvas refused markup that is legal in a real
// app, and refused it at LOAD — while the palette went on offering the
// attribute, because the properties pane is driven from ElementDef.Attrs
// and knows nothing about the context the thing will be built in.
//
// #462. Found by review of #459, whose <Frozen AllowError="{{.Err}}"> is
// the first PLAIN ATTRIBUTE to need a Dispatcher; the class is older —
// `{{ns:Fn}}` has needed one since handlers landed
// (markup/handlers.go:193), which is what the tests here use, so they
// pin the property on main rather than waiting for that PR.

// TestEveryContextTheEditorBuildsWithGetsTheDispatcher is the general
// version, and the general version is the point: the bug is not "AllowError
// is broken", it is "a context the editor builds trees with can be wired
// up short". Naming ed.ctx and ed.docCtx individually would pass again the
// day a third context is added and forgotten, which is exactly how this one
// arrived.
func TestEveryContextTheEditorBuildsWithGetsTheDispatcher(t *testing.T) {
	ed := newEditor(editorFS())
	d := gooey.NewDispatcher()

	for i, c := range ed.contexts() {
		if c == nil {
			t.Fatalf("contexts()[%d] is nil", i)
		}
		if c.Dispatcher != nil {
			t.Fatalf("contexts()[%d] already has a Dispatcher before wiring; "+
				"this test cannot see the bug it exists for", i)
		}
	}

	ed.setDispatcher(d)

	for i, c := range ed.contexts() {
		if c.Dispatcher == nil {
			t.Errorf("contexts()[%d] has no Dispatcher after wiring", i)
		}
	}
}

// TestTheContextListCoversTheOnesTheEditorActuallyUses is the half that
// stops contexts() being satisfied by returning a short list. It names the
// two the editor has today; if a third is added, this fails and the adder
// has to decide whether it belongs in contexts() — which is the decision
// the bug was made of.
func TestTheContextListCoversTheOnesTheEditorActuallyUses(t *testing.T) {
	ed := newEditor(editorFS())
	got := ed.contexts()

	for _, want := range []struct {
		name string
		c    *markup.Context
	}{
		{"ed.ctx (the editor's own chrome)", ed.ctx},
		{"ed.docCtx (the document being edited)", ed.docCtx},
	} {
		found := false
		for _, c := range got {
			if c == want.c {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("contexts() does not include %s, so nothing wires it", want.name)
		}
	}
}

// TestTheCanvasBuildsMarkupARealAppAccepts is the behavioural half, and
// the one that would actually have caught #462: it goes through docCtx
// with markup whose ONLY problem was the missing Dispatcher.
//
// A handler expression rather than <Frozen AllowError>, because the
// defect is the context and not the attribute — and because this way the
// test pins the property on main instead of on the PR that exposed it.
func TestTheCanvasBuildsMarkupARealAppAccepts(t *testing.T) {
	const uri = "urn:gooey:test:462"
	markup.RegisterHandlers(uri, markup.HandlerFunc(
		func(c *markup.Call) (gooey.Command, error) {
			return gooey.Command(func() {}), nil
		}))
	t.Cleanup(func() { markup.RegisterHandlers(uri, nil) })

	page := `<Gooey xmlns:t="` + uri + `"><VStack>` +
		`<Button Name="B" Content="go" Click="{{t:Fire}}"/>` +
		`</VStack></Gooey>`

	ed := newEditor(editorFS())
	ed.setDispatcher(gooey.NewDispatcher())

	// THE DOCUMENT context, which is the one the canvas uses. Building
	// through ed.ctx instead would pass with the bug present.
	if _, err := markup.Build([]byte(page), ed.docCtx); err != nil {
		t.Fatalf("the canvas refuses markup a real app accepts: %v", err)
	}
}

// TestTheEditorsOwnContextWasNeverTheBrokenOne pins the asymmetry the
// issue describes, so a "fix" that wired docCtx by unwiring ed.ctx — or a
// test above that passed because both were nil — is caught.
func TestTheEditorsOwnContextWasNeverTheBrokenOne(t *testing.T) {
	ed := newEditor(editorFS())
	ed.setDispatcher(gooey.NewDispatcher())

	if ed.ctx.Dispatcher == nil {
		t.Error("the editor's own context lost its Dispatcher")
	}
	if ed.docCtx.Dispatcher == nil {
		t.Error("the document context has no Dispatcher")
	}
	if ed.ctx.Dispatcher != ed.docCtx.Dispatcher {
		t.Error("the two contexts hold different Dispatchers; handler results " +
			"must land on the one UI goroutine, not on two")
	}
}
