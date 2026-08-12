package main

import (
	"context"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/markup"
)

// Remote mode drives a DIFFERENT app. These tests run the editor against
// a real one over a real loopback listener, because the whole point of
// the mode is that the target's binding context — not the editor's —
// decides whether a document loads.

func attachedEditor(t *testing.T) (*editor, *markup.Context) {
	t.Helper()
	addr, vm := target(t)
	ed := newEditor(editorFS())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	if err := ed.attach(ctx, addr, "Island"); err != nil {
		t.Fatalf("attach: %v", err)
	}
	t.Cleanup(func() { ed.remote.r.Close() })
	return ed, vm
}

// TestRemoteModePatchesTheIslandNotThePage — the editor owns one named
// element and never writes outside it. That is the subtree-ownership
// rule applied to this client, and it is what makes a failed edit unable
// to damage anything the editor does not own.
func TestRemoteModePatchesTheIsland(t *testing.T) {
	ed, _ := attachedEditor(t)

	if !strings.Contains(ed.status.Get(), "wysiwyg-test-target") {
		t.Errorf("status after attach = %q, want the target's identity", ed.status.Get())
	}

	ed.root.Elem = "VStack"
	ed.root.Kids = []*node{{Elem: "Text", Attrs: map[string]string{"Name": "Made"}}}
	ed.rebuild()

	if s := ed.status.Get(); !strings.HasPrefix(s, "✓ patched <Island>") {
		t.Fatalf("status = %q, want a successful island patch", s)
	}

	// The sibling outside the island survived.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	named, err := ed.remote.r.Named(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sawTitle, sawMade bool
	for _, n := range named {
		switch n {
		case "Title":
			sawTitle = true
		case "Made":
			sawMade = true
		}
	}
	if !sawMade {
		t.Errorf("the patched element is not in the target's name table: %v", named)
	}
	if !sawTitle {
		t.Errorf("patching the island disturbed a sibling outside it: %v", named)
	}
}

// TestRemoteModeRenamesTheRootToTheIsland — patch_markup requires the
// fragment root to carry the address. The editor rewrites its document's
// root name on the way out rather than making the user name their root
// after somebody else's element.
func TestRemoteModeRenamesTheRootToTheIsland(t *testing.T) {
	ed, _ := attachedEditor(t)
	ed.root.Attrs["Name"] = "MyRoot"

	frag := ed.fragmentFor("Island")
	if !strings.Contains(frag, `Name="Island"`) {
		t.Errorf("fragment root is not addressed to the island:\n%s", frag)
	}
	// And the document is unchanged — the rename is for the wire only.
	if ed.root.Attrs["Name"] != "MyRoot" {
		t.Errorf("the document's own root name was mutated: %q", ed.root.Attrs["Name"])
	}
}

// TestRemoteModeRejectsWithoutTouchingTheTarget — a load error is the
// normal case while editing. It must be reported and must leave the
// target's screen exactly as it was, which is what validate-before-patch
// buys.
func TestRemoteModeRejectsWithoutTouchingTheTarget(t *testing.T) {
	ed, _ := attachedEditor(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	before, err := ed.remote.r.Named(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// An unknown attribute — a load error only since rejection landed.
	ed.root.Elem = "VStack"
	ed.root.Kids = []*node{{Elem: "Text", Attrs: map[string]string{"Lef": "2"}}}
	ed.rebuild()

	if s := ed.status.Get(); !strings.Contains(s, "no such attribute") {
		t.Errorf("status = %q, want the target's own load error", s)
	}
	after, err := ed.remote.r.Named(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Errorf("a rejected edit changed the target's tree: %v -> %v", before, after)
	}
}

// TestRemoteModeBindsAgainstTheTargetsContext is the reason validation
// happens remotely. The editor's own context and the target's are
// different: `{{.Body}}` exists there and not here, so a document that
// is valid for the target would be REJECTED by a local build.
func TestRemoteModeBindsAgainstTheTargetsContext(t *testing.T) {
	ed, _ := attachedEditor(t)

	ed.root.Elem = "VStack"
	ed.root.Kids = []*node{{Elem: "Text", Attrs: map[string]string{"Name": "Bound"}}}
	// A binding only the TARGET has.
	ed.root.Kids[0].Attrs["Tooltip"] = "{{.Body}}"
	ed.rebuild()

	if s := ed.status.Get(); !strings.HasPrefix(s, "✓ patched") {
		t.Fatalf("status = %q — a binding the target has must validate there", s)
	}
	// The same document against the EDITOR's context fails, which is the
	// whole argument for validating remotely.
	src := "<Gooey>\n" + ed.root.markup("  ") + "</Gooey>\n"
	if _, err := markup.Build([]byte(src), ed.ctx); err == nil {
		t.Error("expected the editor's own context to lack .Body; if it has it, this test proves nothing")
	}
}

// TestLocalModeStillPreviewsInProcess — attaching is opt-in, and the
// local path must be untouched by its existence.
func TestLocalModeStillPreviewsInProcess(t *testing.T) {
	ed := newEditor(editorFS())
	if ed.remote != nil {
		t.Fatal("a fresh editor must be in local mode")
	}
	ed.rebuild()
	if s := ed.status.Get(); s != "✓ builds" {
		t.Errorf("local status = %q, want the in-process build", s)
	}
	if ed.pv.Child() == nil {
		t.Error("local mode did not populate the preview island")
	}
}

// TestSwappedInvalidatesEveryAddress — a page swap reassigns every Name=
// in the target, so every patch address the editor holds becomes stale
// at once.
//
// This is worth a test rather than a comment because the failure is
// SILENT in the worst possible way: an all-defaults Subscription is
// write-only, so an editor that forgets Lifecycle never learns the page
// changed. Its next patch then either fails with NotFound or — far worse
// — succeeds against a name that now means something else.
//
// Note what the event carries: the NEW name table. So recovery needs no
// resync round trip, only the discipline of treating the document as
// invalidated rather than merged.
func TestSwappedInvalidatesEveryAddress(t *testing.T) {
	ed, _ := attachedEditor(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Replace the target's whole page from another client.
	other, err := Connect(ctx, ed.remote.r.conn.Target(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	swapped := make(chan []string, 4)
	ed.remote.r.OnSwapped = func(named []string) { swapped <- named }

	page := `<Gooey><VStack Name="Root2"><Text Name="Fresh">{{.Body}}</Text></VStack></Gooey>`
	if _, err := other.act(ctx, &controlv1.Act{
		Act: &controlv1.Act_SwapMarkup{SwapMarkup: &controlv1.SwapMarkupRequest{Source: page}},
	}); err != nil {
		t.Fatalf("swap: %v", err)
	}

	select {
	case named := <-swapped:
		if len(named) == 0 {
			t.Error("Swapped carried no names; recovery would need a resync round trip")
		}
		var sawFresh, sawIsland bool
		for _, n := range named {
			switch n {
			case "Fresh":
				sawFresh = true
			case "Island":
				sawIsland = true
			}
		}
		if !sawFresh {
			t.Errorf("new name table lacks the swapped-in element: %v", named)
		}
		if sawIsland {
			t.Errorf("the editor's island survived a whole-page swap: %v", named)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no Swapped event — is Lifecycle in the Subscription? " +
			"an all-defaults subscription is write-only and this failure is silent")
	}
}

// TestWelcomeSeedsTheSizeAndSizeIsReadPerUse — Welcome carries the size
// ONCE, at attach. An editor that caches it as ground truth is wrong
// from the first resize onward, so the size lives behind the mutex and
// is read through Size().
func TestWelcomeSeedsTheSize(t *testing.T) {
	ed, _ := attachedEditor(t)
	cols, rows := ed.remote.r.Size()
	if cols != 80 || rows != 24 {
		t.Errorf("size = %dx%d, want the harness's 80x24 from Welcome", cols, rows)
	}
	if !strings.Contains(ed.status.Get(), "80x24") {
		t.Errorf("status = %q, want the attached size", ed.status.Get())
	}
}

// TestSessionOutlivesTheContextThatOpenedIt.
//
// The context handed to Connect governs the STREAM, not the handshake:
// Attach(ctx) lives exactly as long as ctx does. Passing a timeout meant
// for "give up if the target is unreachable" therefore kills every
// session when it expires, and to a user that reads as the editor
// freezing part-way through a session.
//
// Found by running the real binary — every test in this file used a
// generous timeout and finished in milliseconds, so none of them ever
// held a session long enough to notice. That is the same shape as the
// other blind spots here: the tests exercised the code, not the way it
// is used.
//
// This asserts the session is still usable after the kind of deadline
// that would previously have governed it.
func TestSessionOutlivesTheContextThatOpenedIt(t *testing.T) {
	addr, _ := target(t)

	// A SHORT-lived context, exactly as a careless caller would pass for
	// "connect or give up".
	dial, cancelDial := context.WithTimeout(context.Background(), 300*time.Millisecond)
	r, err := Connect(dial, addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// Let it expire, and cancel it explicitly for good measure.
	time.Sleep(400 * time.Millisecond)
	cancelDial()
	time.Sleep(100 * time.Millisecond)

	// The session must still work. If Connect captured that context for
	// the stream's lifetime, this fails with a cancelled/deadline error.
	use, cancelUse := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelUse()
	if _, err := r.Named(use); err != nil {
		t.Fatalf("session died with the context that opened it: %v", err)
	}
	if err := r.SetProperty(use, "Body", str("still attached")); err != nil {
		t.Fatalf("acts stopped working after the connect context expired: %v", err)
	}
}
