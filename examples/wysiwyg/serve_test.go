package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	gooeygrpc "github.com/WonderForgeLabs/gooey/grpc"
)

// The editor SERVES a control plane as well as attaching to one, and this
// file is why that is not a spare feature.
//
// The page is empty on purpose: the four-pane shell is gone and the
// canvas-first one does not exist yet. What replaces "edit the file,
// rebuild, relaunch, look" is patching the running editor through its own
// control plane — so "the empty page is drivable" is not a nice property,
// it is the only route from here to a UI. A test is the difference
// between believing that and knowing it.
//
// Two claims, and the second is the one that could quietly be false:
//
//  1. the shipped page exposes a patch address at all, and
//  2. patching it with a real layout SUCCEEDS against the editor's own
//     context — which is the thing a page served with the wrong context
//     gets wrong, silently, until the first name-addressed call.

// servedEditor stands the editor's shipped page up behind a real gRPC
// server on loopback, exactly as main does, and returns an attached
// client that owns the root.
func servedEditor(t *testing.T) (*editor, *editor) {
	t.Helper()
	host := newEditor(editorFS())
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}
	app := newTestAppIn(t, string(src), host.ctx)
	srv, err := gooeygrpc.Serve(app, gooeygrpc.Options{
		Addr:    "127.0.0.1:0",
		Context: host.ctx,
		Doc:     func() []byte { return src },
		Name:    "gooey-wysiwyg",
		Version: "1",
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	t.Cleanup(srv.Close)

	client := newEditor(editorFS())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	if err := client.attach(ctx, srv.Addr(), "Page"); err != nil {
		t.Fatalf("attach to the editor's own control plane: %v", err)
	}
	t.Cleanup(func() { client.remote.r.Close() })
	return host, client
}

// TestTheEmptyPageIsPatchable — claim 1. The page has to name something
// the control plane can address, or the rebuild has nowhere to land.
func TestTheEmptyPageIsPatchable(t *testing.T) {
	_, client := servedEditor(t)
	if client.remote == nil {
		t.Fatal("attach left no remote target")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	names, err := client.remote.r.Named(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// <Page> is the address the next version of the UI gets written to.
	if !contains(names, "Page") {
		t.Errorf("the served page does not expose <Page>; names = %v", names)
	}
}

// TestTheEditorCanBeBuiltThroughItsOwnControlPlane — claim 2, and the
// whole workflow in one test: patch a real layout into the empty page and
// require it to take.
//
// It patches the FOUR PANES, not a lone <Text>, because that is the case
// with something to get wrong: the panes are controls registered in the
// editor's context, so a server handed the wrong context builds a lone
// Text perfectly and fails on the first <Palette>.
func TestTheEditorCanBeBuiltThroughItsOwnControlPlane(t *testing.T) {
	_, client := servedEditor(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// PatchMarkup replaces the named element, so the fragment's own root
	// has to carry that name — the layout is the same one the structural
	// tests compose, addressed at <Page>.
	frag := strings.Replace(paneLayout, `<Grid Rows=`, `<Grid Name="Page" Rows=`, 1)
	if !strings.Contains(frag, `Name="Page"`) {
		t.Fatal("the fragment was not addressed at <Page>; paneLayout's root changed shape")
	}

	named, err := client.remote.r.Patch(ctx, "Page", frag)
	if err != nil {
		t.Fatalf("patching the editor's own page with its own panes failed: %v", err)
	}
	// The reply is the target's NEW name table. Every pane instance that
	// carried a Name must be in it, or the patch landed something other
	// than what was sent.
	for _, want := range []string{"Page", "Island"} {
		if !contains(named, want) {
			t.Errorf("after the patch the target does not know %q; names = %v", want, named)
		}
	}
}

// TestPatchingWithSomethingTheContextCannotBuildIsRefused is the
// discrimination half: without it the two tests above would pass against
// a server that accepted anything, and "the patch succeeded" would mean
// nothing.
func TestPatchingWithSomethingTheContextCannotBuildIsRefused(t *testing.T) {
	_, client := servedEditor(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// <Nonesuch> is in no vocabulary, editor or document.
	_, err := client.remote.r.Patch(ctx, "Page", `<Gooey><Canvas Name="Page"><Nonesuch Name="X"/></Canvas></Gooey>`)
	if err == nil {
		t.Fatal("the server accepted markup it cannot build; a patch that always succeeds proves nothing")
	}
	// And the app is still alive afterwards — a rejected patch must not
	// take the editor down with it.
	if _, err := client.remote.r.Patch(ctx, "Page", `<Gooey><Canvas Name="Page"><Text Name="Ok">ok</Text></Canvas></Gooey>`); err != nil {
		t.Fatalf("the target did not survive a rejected patch: %v", err)
	}
}
