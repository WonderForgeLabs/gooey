package settings_test

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// ---------------------------------------------------------------------
// SetRaw: the dynamic-key surface a STATE document needs.
// ---------------------------------------------------------------------

// TestSetRawCreatesAKeyNoHandleOwns is the state-store case in
// miniature: a key nobody registered appears at runtime, persists, and
// is readable — raw in the same run, typed in the next.
func TestSetRawCreatesAKeyNoHandleOwns(t *testing.T) {
	m, s := open(t, "")
	if err := s.SetRaw("state.scroll.main_go", []byte(`42`)); err != nil {
		t.Fatalf("SetRaw: %v", err)
	}

	raw, ok := s.Raw("state.scroll.main_go")
	if !ok || string(raw) != "42" {
		t.Fatalf("Raw = %q, %v — the key SetRaw wrote is not readable back", raw, ok)
	}
	found := false
	for _, k := range s.Keys() {
		if k == "state.scroll.main_go" {
			found = true
		}
	}
	if !found {
		t.Fatal("Keys() does not list the dynamic key")
	}

	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)
	drain(d)
	stop()
	if got := decode(t, m.Doc())["state.scroll.main_go"]; got != float64(42) {
		t.Fatalf("document holds %v, want 42 — the dynamic key never persisted", got)
	}

	// The next run claims the key with a typed handle, which is the
	// write-raw-today, register-tomorrow lifecycle a state document has.
	_, s2 := open(t, m.Doc())
	if got := mustValue(t, s2, "state.scroll.main_go", 0).Get(); got != 42 {
		t.Fatalf("a later Value read %d, want the 42 written raw", got)
	}
}

// TestSetRawOnARegisteredKeyReachesTheHandle pins the guarantee that
// makes SetRaw safe to expose at all: a raw write and the typed handle
// can never disagree, because the write routes through the handle.
// The repaint is the proof — if SetRaw only patched the document, the
// bound Text would still show the old value and paint nothing.
func TestSetRawOnARegisteredKeyReachesTheHandle(t *testing.T) {
	_, s := open(t, "")
	src := mustValue(t, s, keySource, "origin/main")
	other := mustValue(t, s, "browser.status", "ready")

	c := gooey.NewComposer(texts(src, other), 24, 4)
	c.Frame()

	if err := s.SetRaw(keySource, []byte(`"feat/x"`)); err != nil {
		t.Fatalf("SetRaw: %v", err)
	}
	if got := src.Get(); got != "feat/x" {
		t.Fatalf("handle reads %q after SetRaw, want %q — the raw write bypassed it", got, "feat/x")
	}
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("SetRaw on a bound key painted %d components, want exactly 1", painted)
	}
	if got := row(f.Cells, 0); got != "feat/x" {
		t.Fatalf("row 0 = %q — the bound Text did not repaint to the raw-written value", got)
	}
}

// TestSetRawTypeMismatchChangesNothing: the raw value must decode as the
// handle's type or the write is refused whole — a shaped error naming
// the key and the expected type, an unchanged handle, and zero writes.
func TestSetRawTypeMismatchChangesNothing(t *testing.T) {
	m, s := open(t, "")
	rec := mustValue(t, s, keyRecord, false)
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)

	err := s.SetRaw(keyRecord, []byte(`"definitely not a bool"`))
	if err == nil {
		t.Fatal("SetRaw accepted a string for a bool-typed handle")
	}
	if !strings.Contains(err.Error(), keyRecord) || !strings.Contains(err.Error(), "bool") {
		t.Fatalf("error %q names neither the key nor the expected type; it is read by someone staring at one call site", err)
	}
	if rec.Get() != false {
		t.Fatal("the handle changed on a refused write")
	}
	drain(d)
	stop()
	if got := m.Saves(); got != 0 {
		t.Fatalf("a refused SetRaw caused %d writes, want 0", got)
	}
}

// TestSetRawErrorShapes: the two refusals that have nothing to do with
// types. Asserting the message shape, not err != nil — a SetRaw that
// refused everything would pass an existence check.
func TestSetRawErrorShapes(t *testing.T) {
	_, s := open(t, "")
	if err := s.SetRaw("", []byte(`1`)); err == nil || !strings.Contains(err.Error(), "empty key") {
		t.Fatalf("empty key: err = %v, want a message saying so", err)
	}
	err := s.SetRaw("state.x", []byte(`{not json`))
	if err == nil || !strings.Contains(err.Error(), "state.x") || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("invalid JSON: err = %v, want a message naming the key and the problem", err)
	}
	if _, ok := s.Raw("state.x"); ok {
		t.Fatal("a refused SetRaw left a value behind")
	}
}

// TestSetRawSharesTheFlushEconomics: dynamic keys ride the same
// dirty-tracked, compared, coalesced save path as typed Sets — several
// raw writes in a batch are one write, and rewriting the value already
// on disk is zero.
func TestSetRawSharesTheFlushEconomics(t *testing.T) {
	m, s := open(t, "")
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)
	defer stop()

	for i := 0; i < 3; i++ {
		if err := s.SetRaw("state.layout", []byte(`"wide"`)); err != nil {
			t.Fatalf("SetRaw: %v", err)
		}
	}
	drain(d)
	awaitSave(t, m)
	if got := m.Saves(); got != 1 {
		t.Fatalf("three raw writes in one batch caused %d saves, want exactly 1", got)
	}

	// The same value again: dirty, flushed, compared, and NOT saved.
	if err := s.SetRaw("state.layout", []byte(`"wide"`)); err != nil {
		t.Fatalf("SetRaw: %v", err)
	}
	drain(d)
	if got := m.Saves(); got != 1 {
		t.Fatalf("rewriting the stored value caused %d saves, want still 1", got)
	}
}

// TestSetRawCompactsWhatRawReadsBack. The first version of this test
// asserted that whitespace differences cost no extra save, and it
// PASSED WITH THE COMPACTION DELETED: json's encoder re-compacts every
// RawMessage on marshal, so the flush comparison is whitespace-immune
// with or without SetRaw's own compaction. What the compaction actually
// buys is pinned here instead — Raw hands back canonical bytes, so two
// tools that wrote the same object in different whitespace read the
// same bytes and can compare them — plus early validation, which
// TestSetRawErrorShapes pins.
func TestSetRawCompactsWhatRawReadsBack(t *testing.T) {
	_, s := open(t, "")
	if err := s.SetRaw("state.win", []byte(`{ "w": 80,  "h": 24 }`)); err != nil {
		t.Fatalf("SetRaw: %v", err)
	}
	raw, ok := s.Raw("state.win")
	if !ok {
		t.Fatal("the key SetRaw wrote is not readable back")
	}
	if got := string(raw); got != `{"w":80,"h":24}` {
		t.Fatalf("Raw = %q, want the compact %q — SetRaw stored the caller's whitespace", got, `{"w":80,"h":24}`)
	}
}
