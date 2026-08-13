package settings_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/settings"
)

// The three settings this package was built for, spelled the way an app
// spells them. They are also the three shapes that have to work without
// reflection: a string, a bool, and (for good measure) an int.
const (
	keySource  = "browser.lastSource"
	keyRecord  = "browser.keepRecording"
	keyRestart = "browser.autoRestartApp"
)

// drain runs the dispatcher the way App.Run's loop does — including work
// a drained closure posts in turn.
func drain(d *gooey.Dispatcher) {
	for d.Pending() > 0 {
		d.Drain()
	}
}

// awaitSave blocks until one more document has reached the provider. It
// is the barrier that keeps the asynchronous half of these tests off
// sleeps: Memory signals after every completed Save.
func awaitSave(t *testing.T, m *settings.Memory) {
	t.Helper()
	select {
	case <-m.Saved():
	case <-time.After(5 * time.Second):
		t.Fatalf("no save arrived within 5s (saves so far: %d)", m.Saves())
	}
}

func open(t *testing.T, doc string) (*settings.Memory, *settings.Store) {
	t.Helper()
	m := settings.NewMemory(doc)
	s, err := settings.Open(m)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return m, s
}

func mustValue[T any](t *testing.T, s *settings.Store, key string, def T) *prop.Property[T] {
	t.Helper()
	p, err := settings.Value(s, key, def)
	if err != nil {
		t.Fatalf("Value(%q): %v", key, err)
	}
	return p
}

func decode(t *testing.T, doc string) map[string]any {
	t.Helper()
	if strings.TrimSpace(doc) == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("saved document is not a JSON object: %v\n%s", err, doc)
	}
	return m
}

// ---------------------------------------------------------------------
// Loading: the document wins, the defaults are the fallback.
// ---------------------------------------------------------------------

func TestStoredValuesWinOverDefaults(t *testing.T) {
	_, s := open(t, `{"browser.lastSource":"origin/main","browser.keepRecording":true,"browser.autoRestartApp":true}`)
	src := mustValue(t, s, keySource, "")
	rec := mustValue(t, s, keyRecord, false)
	auto := mustValue(t, s, keyRestart, false)

	if got := src.Get(); got != "origin/main" {
		t.Errorf("%s = %q, want the stored value %q", keySource, got, "origin/main")
	}
	if !rec.Get() {
		t.Errorf("%s = false, want the stored value true", keyRecord)
	}
	if !auto.Get() {
		t.Errorf("%s = false, want the stored value true", keyRestart)
	}
}

// The discrimination half of the pair above: every stored value there
// differs from its default, and every default here differs from that
// stored value, so an implementation that ignored either side fails one
// of the two tests.
func TestDefaultsWinOnAFreshDocument(t *testing.T) {
	_, s := open(t, "")
	src := mustValue(t, s, keySource, "")
	rec := mustValue(t, s, keyRecord, false)
	auto := mustValue(t, s, keyRestart, false)

	if got := src.Get(); got != "" {
		t.Errorf("%s = %q on a fresh document, want the default %q", keySource, got, "")
	}
	if rec.Get() || auto.Get() {
		t.Errorf("bools = %v/%v on a fresh document, want the defaults false/false", rec.Get(), auto.Get())
	}
	if keys := s.Keys(); len(keys) != 3 {
		t.Errorf("Keys() = %v, want the three registered keys", keys)
	}
}

// ---------------------------------------------------------------------
// The pinned decision: a Set that changes nothing costs no disk write.
// ---------------------------------------------------------------------

// prop.Set does not compare values, so fifty identical Sets invalidate
// the watcher fifty times. The store's answer is to compare the encoded
// DOCUMENT once per flush instead — this is the assertion that answer
// exists to satisfy.
func TestNoOpSetsCostNoWrite(t *testing.T) {
	m, s := open(t, `{"browser.keepRecording":true}`)
	rec := mustValue(t, s, keyRecord, false)
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)

	for i := 0; i < 50; i++ {
		rec.Set(true) // already true, and already true on disk
	}
	drain(d)
	stop() // flushes anything dirty, then joins the writer

	if got := m.Saves(); got != 0 {
		t.Fatalf("50 no-op Sets caused %d writes, want 0", got)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("unexpected store error: %v", err)
	}
}

// The discrimination half: if the store simply never wrote, the test
// above would pass for the wrong reason. Sets that genuinely change the
// document DO reach the provider — once per dispatcher batch, however
// many Sets the batch contained.
func TestChangingSetsWriteOncePerBatch(t *testing.T) {
	m, s := open(t, `{"browser.keepRecording":true}`)
	rec := mustValue(t, s, keyRecord, false)
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)
	defer stop()

	// One batch, three Sets, ending somewhere other than where it began.
	rec.Set(false)
	rec.Set(true)
	rec.Set(false)
	drain(d)
	awaitSave(t, m)
	if got := m.Saves(); got != 1 {
		t.Fatalf("three Sets in one batch caused %d writes, want exactly 1", got)
	}
	// false is the default, so the key leaves the document entirely.
	if got := decode(t, m.Doc()); len(got) != 0 {
		t.Fatalf("document = %v, want {} — a value equal to its default is not persisted", got)
	}

	// A second batch is a second document, and so a second write.
	rec.Set(true)
	drain(d)
	awaitSave(t, m)
	if got := m.Saves(); got != 2 {
		t.Fatalf("a second batch brought the total to %d writes, want 2", got)
	}
	if got := decode(t, m.Doc())[keyRecord]; got != true {
		t.Fatalf("document[%s] = %v, want true", keyRecord, got)
	}
}

// Coalescing is per batch, so the two tests above cannot distinguish
// "one write per batch" from "one write ever". This one drives three
// separate batches and pins the count at three.
func TestEachBatchIsItsOwnWrite(t *testing.T) {
	m, s := open(t, "")
	src := mustValue(t, s, keySource, "")
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)
	defer stop()

	for i, v := range []string{"a", "b", "c"} {
		src.Set(v)
		drain(d)
		awaitSave(t, m)
		if got := m.Saves(); got != i+1 {
			t.Fatalf("after batch %d the provider had %d writes, want %d", i+1, got, i+1)
		}
	}
}

// ---------------------------------------------------------------------
// A setting is an ordinary source property: the damage pin.
// ---------------------------------------------------------------------

// texts stacks one Text per handle — the smallest tree in which "this
// component and no other repainted" is a meaningful statement.
func texts(handles ...*prop.Property[string]) gooey.Component {
	kids := make([]gooey.Component, 0, len(handles))
	for _, h := range handles {
		kids = append(kids, &components.Text{Content: h})
	}
	return &components.VStack{Children: kids}
}

// TestASettingRepaintsExactlyOneComponent is the claim that makes these
// settings gooey settings rather than a config snapshot: the handle is a
// source property, so a change repaints exactly the components that read
// it — not the page, and not one extra node for the store's watcher.
func TestASettingRepaintsExactlyOneComponent(t *testing.T) {
	_, s := open(t, "")
	src := mustValue(t, s, keySource, "origin/main")
	other := mustValue(t, s, "browser.status", "ready")

	c := gooey.NewComposer(texts(src, other), 24, 4)
	c.Frame()

	src.Set("feat/settings-store")
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("changing one setting painted %d components, want exactly 1", painted)
	}
	if got := row(f.Cells, 0); got != "feat/settings-store" {
		t.Fatalf("row 0 = %q — the bound Text did not repaint", got)
	}
	if got := row(f.Cells, 1); got != "ready" {
		t.Fatalf("row 1 = %q — the unrelated Text was disturbed", got)
	}

	// Discrimination: an idle frame paints nothing, so "1" above is a
	// measurement and not the number this tree always reports.
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("an idle frame painted %d components, want 0", painted)
	}
}

// The watcher the store arms is a computed, and a computed that is not a
// paint node must not show up in the count. Same tree, same change, one
// tree wired to settings handles and one to plain sources: the numbers
// have to match.
func TestSettingHandlesDamageIdenticallyToAPlainSource(t *testing.T) {
	_, s := open(t, "")
	a := mustValue(t, s, "a", "one")
	b := mustValue(t, s, "b", "two")
	withStore := gooey.NewComposer(texts(a, b), 24, 4)
	withStore.Frame()

	pa, pb := prop.NewSource("one"), prop.NewSource("two")
	plain := gooey.NewComposer(texts(pa, pb), 24, 4)
	plain.Frame()

	a.Set("changed")
	pa.Set("changed")
	_, withStoreCount := withStore.Frame()
	_, plainCount := plain.Frame()
	if withStoreCount != plainCount {
		t.Fatalf("a settings handle painted %d components where a plain source painted %d; the watcher is leaking into the paint graph", withStoreCount, plainCount)
	}
	if withStoreCount != 1 {
		t.Fatalf("both painted %d components — the comparison is only meaningful at 1", withStoreCount)
	}
}

// ---------------------------------------------------------------------
// CRUD over the document.
// ---------------------------------------------------------------------

func TestKeysAndRawSpanRegisteredAndPassThrough(t *testing.T) {
	_, s := open(t, `{"plugin.alpha":{"nested":[1,2]},"browser.keepRecording":true}`)
	rec := mustValue(t, s, keyRecord, false)

	// Joined rather than DeepEqual-ed on purpose: the reflect package
	// appears in this repo only under generated protobuf and the
	// activity packs, and a settings test is not the place to add an
	// occurrence — including one the invariant grep would trip over.
	const want = "browser.keepRecording,plugin.alpha"
	if got := strings.Join(s.Keys(), ","); got != want {
		t.Fatalf("Keys() = %q, want %q", got, want)
	}
	// A registered key reads through its handle, so Raw and Get cannot
	// disagree even after a Set.
	rec.Set(false)
	raw, ok := s.Raw(keyRecord)
	if !ok || string(raw) != "false" {
		t.Fatalf("Raw(%s) = %q,%v after Set(false), want \"false\",true", keyRecord, raw, ok)
	}
	if raw, ok := s.Raw("plugin.alpha"); !ok || string(raw) != `{"nested":[1,2]}` {
		t.Fatalf("Raw(plugin.alpha) = %q,%v", raw, ok)
	}
	if _, ok := s.Raw("nope"); ok {
		t.Fatal("Raw of an absent key reported ok")
	}
}

// A document is shared: a plugin's keys, or a newer version's, must
// survive a save by a process that has never heard of them.
func TestUnregisteredKeysSurviveASave(t *testing.T) {
	m, s := open(t, `{"plugin.alpha":{"nested":[1,2]},"browser.keepRecording":false}`)
	rec := mustValue(t, s, keyRecord, false)
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)

	rec.Set(true)
	drain(d)
	awaitSave(t, m)
	stop()

	got := decode(t, m.Doc())
	if got[keyRecord] != true {
		t.Fatalf("document[%s] = %v, want true", keyRecord, got[keyRecord])
	}
	alpha, ok := got["plugin.alpha"].(map[string]any)
	if !ok {
		t.Fatalf("plugin.alpha = %#v, want the object it was loaded as — an unowned key was dropped", got["plugin.alpha"])
	}
	if nested, _ := alpha["nested"].([]any); len(nested) != 2 {
		t.Fatalf("plugin.alpha.nested = %#v, want the two elements it was loaded with", alpha["nested"])
	}
}

// The discrimination half: pass-through is not "echo whatever was
// loaded". Delete on an unowned key removes it, which an echoing
// implementation could not do.
func TestDeleteRemovesAnUnregisteredKey(t *testing.T) {
	m, s := open(t, `{"plugin.alpha":{"nested":[1,2]},"plugin.beta":1}`)
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)

	if !s.Delete("plugin.alpha") {
		t.Fatal("Delete of a present key reported false")
	}
	if s.Delete("plugin.alpha") {
		t.Fatal("Delete of an already-deleted key reported true")
	}
	drain(d)
	awaitSave(t, m)
	stop()

	got := decode(t, m.Doc())
	if _, still := got["plugin.alpha"]; still {
		t.Fatalf("plugin.alpha survived Delete: %v", got)
	}
	if got["plugin.beta"] != float64(1) {
		t.Fatalf("plugin.beta = %v, want 1 — Delete took a key with it", got["plugin.beta"])
	}
}

// Deleting a registered key is "forget it, go back to the default", and
// because the reset is an ordinary Set, anything bound to it repaints.
func TestDeleteResetsTheHandleAndRepaints(t *testing.T) {
	_, s := open(t, `{"browser.lastSource":"origin/main"}`)
	src := mustValue(t, s, keySource, "none")
	c := gooey.NewComposer(texts(src), 24, 2)
	c.Frame()

	if !s.Delete(keySource) {
		t.Fatal("Delete of a registered key reported false")
	}
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("Delete painted %d components, want 1", painted)
	}
	if got := src.Get(); got != "none" {
		t.Fatalf("handle = %q after Delete, want the default %q", got, "none")
	}
	if got := row(f.Cells, 0); got != "none" {
		t.Fatalf("row 0 = %q after Delete", got)
	}
	if !s.Dirty() {
		t.Fatal("Delete left the store clean")
	}
}

// ---------------------------------------------------------------------
// Lifetime.
// ---------------------------------------------------------------------

// Quitting must not lose the change that prompted the quit: stop flushes
// what is dirty and then JOINS the writer, so the provider has certainly
// seen the document by the time stop returns.
func TestStopFlushesThePendingChange(t *testing.T) {
	m, s := open(t, "")
	src := mustValue(t, s, keySource, "")
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)

	src.Set("origin/main")
	stop() // no Drain at all: the posted flush never ran

	if got := m.Saves(); got != 1 {
		t.Fatalf("stop wrote %d documents, want 1", got)
	}
	if got := decode(t, m.Doc())[keySource]; got != "origin/main" {
		t.Fatalf("document[%s] = %v after stop, want %q", keySource, got, "origin/main")
	}
}

// The discrimination half: stop is not an unconditional write.
func TestStopWritesNothingWhenNothingChanged(t *testing.T) {
	m, s := open(t, `{"browser.lastSource":"origin/main"}`)
	mustValue(t, s, keySource, "")
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)
	drain(d)
	stop()

	if got := m.Saves(); got != 0 {
		t.Fatalf("stop wrote %d documents with nothing dirty, want 0", got)
	}
}

func TestAutoSaveOffDefersToAnExplicitFlush(t *testing.T) {
	m, s := open(t, "")
	s.AutoSave(false)
	src := mustValue(t, s, keySource, "")
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)
	defer stop()

	src.Set("a")
	src.Set("b")
	drain(d)
	if got := m.Saves(); got != 0 {
		t.Fatalf("AutoSave(false) still wrote %d documents", got)
	}
	if !s.Dirty() {
		t.Fatal("the store did not notice the Sets at all")
	}

	s.Flush()
	awaitSave(t, m)
	if got := m.Saves(); got != 1 {
		t.Fatalf("an explicit Flush wrote %d documents, want 1", got)
	}
	if got := decode(t, m.Doc())[keySource]; got != "b" {
		t.Fatalf("document[%s] = %v, want the last value %q", keySource, got, "b")
	}
}

// The watcher fires once per clean-to-dirty transition, so a flush that
// forgot to re-arm would make every Set after the first one silent. Two
// consecutive batches, each producing its own write, is what proves the
// re-arm happens.
func TestTheWatcherIsReArmedAfterEveryFlush(t *testing.T) {
	m, s := open(t, "")
	s.AutoSave(false)
	src := mustValue(t, s, keySource, "")
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)
	defer stop()

	src.Set("a")
	s.Flush()
	awaitSave(t, m)
	if s.Dirty() {
		t.Fatal("Flush left the store dirty")
	}

	src.Set("b")
	if !s.Dirty() {
		t.Fatal("a Set after a Flush did not mark the store dirty — the watcher was never re-armed")
	}
	s.Flush()
	awaitSave(t, m)
	if got := decode(t, m.Doc())[keySource]; got != "b" {
		t.Fatalf("document[%s] = %v, want %q", keySource, got, "b")
	}
}

// ---------------------------------------------------------------------
// Errors, by shape.
// ---------------------------------------------------------------------

func wantErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error mentioning %q, got none", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not mention %q", err, want)
	}
}

func TestOpenErrorShapes(t *testing.T) {
	_, err := settings.Open(nil)
	wantErr(t, err, "settings: Open: nil Provider")

	m := settings.NewMemory("")
	m.FailLoad(errors.New("disk on fire"))
	_, err = settings.Open(m)
	wantErr(t, err, "settings: load: disk on fire")

	_, err = settings.Open(settings.NewMemory(`[1,2,3]`))
	wantErr(t, err, "settings: document is not a JSON object of dotted keys")

	_, err = settings.Open(settings.NewMemory(`{"a": nope}`))
	wantErr(t, err, "settings: document is not a JSON object of dotted keys")

	// The discrimination half: the shapes above are reachable, and the
	// documents that should load, load.
	for _, doc := range []string{"", "   ", "null", "{}", `{"a":1}`} {
		if _, err := settings.Open(settings.NewMemory(doc)); err != nil {
			t.Fatalf("Open(%q) failed: %v", doc, err)
		}
	}
}

func TestValueErrorShapes(t *testing.T) {
	_, err := settings.Value[string](nil, "k", "")
	wantErr(t, err, `settings: Value("k"): nil store`)

	_, s := open(t, "")
	_, err = settings.Value(s, "", "")
	wantErr(t, err, "settings: Value: empty key")

	mustValue(t, s, keySource, "")
	_, err = settings.Value(s, keySource, "")
	wantErr(t, err, `settings: "browser.lastSource" is already registered`)

	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)
	defer stop()
	_, err = settings.Value(s, "late.key", 0)
	wantErr(t, err, `settings: "late.key" registered after Start`)
}

// A hand-edited settings file with the wrong type in it must not stop
// the app, and must not be silent either. Both halves are asserted: the
// error names the key and both types, AND the handle is usable.
func TestAMistypedStoredValueIsReportedAndSurvived(t *testing.T) {
	_, s := open(t, `{"browser.lastSource":42,"browser.keepRecording":true}`)

	src, err := settings.Value(s, keySource, "fallback")
	wantErr(t, err, `settings: "browser.lastSource": stored value 42 is not a string`)
	if src == nil {
		t.Fatal("Value returned a nil handle for a mistyped stored value; the app has nothing to bind")
	}
	if got := src.Get(); got != "fallback" {
		t.Fatalf("handle = %q, want the default %q", got, "fallback")
	}

	// Discrimination: the sibling key in the same document decodes
	// cleanly, so the error above is about the value and not about the
	// document.
	rec, err := settings.Value(s, keyRecord, false)
	if err != nil {
		t.Fatalf("a well-typed sibling key also failed: %v", err)
	}
	if !rec.Get() {
		t.Fatal("the well-typed sibling did not take its stored value")
	}
}

func TestSaveFailureIsReportedAndRetried(t *testing.T) {
	m, s := open(t, "")
	src := mustValue(t, s, keySource, "")
	var seen []error
	s.OnError(func(err error) { seen = append(seen, err) })
	d := gooey.NewDispatcher()
	stop := s.Start(d.Post)
	defer stop()

	m.FailSave(errors.New("read-only file system"))
	src.Set("a")
	drain(d)
	// The failing Save never signals Saved, so settle by joining on a
	// second document that does not fail.
	deadline := time.Now().Add(5 * time.Second)
	for len(seen) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		drain(d)
	}
	if len(seen) != 1 {
		t.Fatalf("a failing provider produced %d reported errors, want 1", len(seen))
	}
	wantErr(t, seen[0], "settings: save: read-only file system")
	wantErr(t, s.Err(), "settings: save: read-only file system")

	// The retry half: a failed write must not be remembered as the
	// document on disk, or setting the SAME value again would compare
	// equal and never be written.
	m.FailSave(nil)
	src.Set("a")
	drain(d)
	awaitSave(t, m)
	if got := decode(t, m.Doc())[keySource]; got != "a" {
		t.Fatalf("document[%s] = %v after the retry, want %q", keySource, got, "a")
	}
}

func TestFlushBeforeStartIsReportedRatherThanDropped(t *testing.T) {
	m, s := open(t, "")
	src := mustValue(t, s, keySource, "")
	src.Set("a")
	s.Flush()

	wantErr(t, s.Err(), "settings: save requested before Start")
	if got := m.Saves(); got != 0 {
		t.Fatalf("a flush with no writer wrote %d documents", got)
	}

	// The discrimination half: SaveNow is the documented way to write
	// without a writer goroutine, and it works from the same state.
	if err := s.SaveNow(); err != nil {
		t.Fatalf("SaveNow: %v", err)
	}
	if got := decode(t, m.Doc())[keySource]; got != "a" {
		t.Fatalf("document[%s] = %v after SaveNow, want %q", keySource, got, "a")
	}
}

func TestSaveNowReportsProviderFailure(t *testing.T) {
	m, s := open(t, "")
	src := mustValue(t, s, keySource, "")
	src.Set("a")
	m.FailSave(errors.New("quota exceeded"))
	wantErr(t, s.SaveNow(), "settings: save: quota exceeded")

	// Discrimination: with the provider healthy the same call succeeds.
	m.FailSave(nil)
	if err := s.SaveNow(); err != nil {
		t.Fatalf("SaveNow after the provider recovered: %v", err)
	}
	if got := m.Saves(); got != 1 {
		t.Fatalf("provider saw %d writes, want 1", got)
	}
}

// ---------------------------------------------------------------------
// The file provider.
// ---------------------------------------------------------------------

func TestFileProviderRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	p := settings.File(path)

	// A missing file is a fresh install, not an error.
	doc, err := p.Load()
	if err != nil {
		t.Fatalf("Load of a missing file: %v", err)
	}
	if len(doc) != 0 {
		t.Fatalf("Load of a missing file returned %q", doc)
	}

	s, err := settings.Open(p)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	src := mustValue(t, s, keySource, "")
	auto := mustValue(t, s, keyRestart, false)
	src.Set("origin/main")
	auto.Set(true)
	if err := s.SaveNow(); err != nil {
		t.Fatalf("SaveNow: %v", err)
	}

	// The atomic-rename temp file must not be left behind.
	ents, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "settings.json" {
		names := make([]string, len(ents))
		for i, e := range ents {
			names[i] = e.Name()
		}
		t.Fatalf("directory holds %v, want only settings.json", names)
	}

	// Reopening is where the round trip is actually proved: a second
	// store, built from the file alone, has to see both values — and the
	// defaults it is given are the opposite of both, so it cannot pass by
	// falling back.
	s2, err := settings.Open(settings.File(path))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	src2 := mustValue(t, s2, keySource, "unset")
	auto2 := mustValue(t, s2, keyRestart, false)
	if got := src2.Get(); got != "origin/main" {
		t.Errorf("%s = %q after reopen, want %q", keySource, got, "origin/main")
	}
	if !auto2.Get() {
		t.Errorf("%s = false after reopen, want true", keyRestart)
	}
}

func TestFileProviderErrorShapes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := settings.Open(settings.File(path))
	wantErr(t, err, "settings: document is not a JSON object of dotted keys")

	// A directory where the file should be: Load must say which path.
	asDir := filepath.Join(dir, "adir")
	if err := os.Mkdir(asDir, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = settings.Open(settings.File(asDir))
	wantErr(t, err, "settings: load: read "+asDir)

	_, err = settings.UserFile("", "settings.json")
	wantErr(t, err, "settings: UserFile")

	// Discrimination: the same path with valid content opens cleanly.
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Open(settings.File(path)); err != nil {
		t.Fatalf("a valid document failed to open: %v", err)
	}
}

func row(b *render.Buffer, y int) string {
	var sb strings.Builder
	for x := 0; x < b.W; x++ {
		sb.WriteRune(b.At(x, y).Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}
