package main

import (
	"context"
	"fmt"
	"time"
)

// Remote mode: the editor drives a DIFFERENT app instead of previewing
// the document in its own process.
//
// The document model and the whole catalog-driven UI are unchanged —
// palette, inspector, parent scoping and markup emission all work on a
// document, not on a tree. Only what happens to the emitted markup
// changes: locally it is built into the preview island; remotely it is
// validated against the target's live binding context and then patched
// into the target's island.
//
// # The island is the contract
//
// The editor owns ONE named element in the target and never writes
// outside it. That is the subtree-ownership rule the plugin spec argues
// for, applied to this client: it turns concurrent writers into disjoint
// writers, and it means a failed edit can never damage anything the
// editor does not own.
//
// # Validate first, always
//
// validate_markup runs the target's real parse-and-bind path without
// touching the running tree, so a generation loop can be wrong as often
// as it likes and never flicker the page. Since unknown-attribute
// rejection landed, that path also catches the attribute mistakes this
// editor is most likely to make.
type remoteTarget struct {
	r      *Remote
	island string
}

// attach connects the editor to a remote app and takes over rebuild.
func (ed *editor) attach(ctx context.Context, addr, island string) error {
	r, err := Connect(ctx, addr, nil)
	if err != nil {
		return err
	}
	ed.remote = &remoteTarget{r: r, island: island}

	// A dropped session must be visible in the editor rather than
	// silently stopping edits from landing. Recovery is reconnect-and-
	// resync, never resume: an overflowing session is dropped whole, so
	// the editor can never patch up its view from what it did receive.
	//
	// This fires on the STREAM READER's goroutine, so it must not touch
	// a property. It records the loss under a mutex and asks the UI
	// goroutine to surface it; rebuild reads it too, so the loss still
	// appears when there is no App at all — a drop before the app is
	// running, or a test. Posting to a nil App is what the first version
	// did, and it panicked.
	r.OnLost = func(err error) {
		ed.mu.Lock()
		ed.lost = err
		ed.mu.Unlock()
		if ed.app != nil {
			ed.app.Post(ed.showLost)
		}
	}
	// A swap reassigns every Name= in the target, so every address this
	// editor holds is stale at once. The names arrive WITH the event, so
	// recovery needs no round trip — but the document must be treated as
	// invalidated rather than merged.
	r.OnSwapped = func(named []string) {
		ed.mu.Lock()
		ed.swapped = true
		ed.mu.Unlock()
		if ed.app != nil {
			ed.app.Post(func() {
				ed.status.Set(fmt.Sprintf("✗ target page was replaced — %d new names; every patch address is stale", len(named)))
			})
		}
	}
	cols, rows := r.Size()
	ed.status.Set(fmt.Sprintf("attached to %s (%dx%d), island <%s>",
		r.AppName, cols, rows, island))
	return nil
}

// showLost surfaces a dropped session. UI goroutine only.
func (ed *editor) showLost() {
	if err := ed.lostSession(); err != nil {
		ed.status.Set("✗ session lost: " + err.Error() + " — reconnect and resync")
	}
}

// lostSession reports whether the stream has dropped.
func (ed *editor) lostSession() error {
	ed.mu.Lock()
	defer ed.mu.Unlock()
	return ed.lost
}

// pushRemote is rebuild's remote half: validate the document against the
// target, then patch it into the island.
//
// The fragment root must carry the island's Name — the name is the
// address and must survive the patch — so the document's root element is
// renamed on the way out rather than the editor forcing the user to name
// their root after the target's island.
func (ed *editor) pushRemote(src string) {
	if err := ed.lostSession(); err != nil {
		ed.status.Set("✗ session lost: " + err.Error() + " — reconnect and resync")
		return
	}
	t := ed.remote
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	frag := ed.fragmentFor(t.island)
	ok, loadErr, err := t.r.Validate(ctx, frag)
	if err != nil {
		ed.status.Set("✗ validate: " + err.Error())
		return
	}
	if !ok {
		// The target rejected it. This is the normal case while editing
		// and must never disturb what is already on its screen.
		ed.status.Set("✗ " + loadErr)
		return
	}
	named, err := t.r.Patch(ctx, t.island, frag)
	if err != nil {
		ed.status.Set("✗ patch: " + err.Error())
		return
	}
	ed.status.Set(fmt.Sprintf("✓ patched <%s> — %d names live", t.island, len(named)))
}

// fragmentFor wraps the edited document as a patch fragment addressed to
// the island. Layout attributes the fragment does not restate are
// preserved from the old element, so the island keeps its position in
// the target's layout: a fragment describes content, and where it sits
// is the parent's business.
func (ed *editor) fragmentFor(island string) string {
	saved := ed.root.Attrs["Name"]
	ed.root.Attrs["Name"] = island
	defer func() { ed.root.Attrs["Name"] = saved }()
	return "<Gooey>\n" + ed.root.markup("  ") + "</Gooey>\n"
}
