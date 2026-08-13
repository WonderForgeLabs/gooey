// Package settings is gooey's external-state seam: one JSON document of
// dotted keys, whose values are ordinary bindable dependency properties.
//
// The shape is vscode's settings.json — a FLAT map with dotted,
// namespaced keys ("browser.lastSource", "browser.autoRestart"), not a
// nested tree. Flat is what makes a key a single opaque string on every
// wire it crosses: the API, the file, a future control-plane RPC.
//
// Three ideas hold the package up.
//
// # The host owns persistence
//
// Provider is the whole persistence contract: bytes in, bytes out. It
// knows nothing about gooey, properties, or even about JSON beyond
// carrying it. File is one implementation; Memory is another; an app
// embedded in a larger program supplies a third that writes wherever
// that program keeps state. Load and Save run OFF the UI goroutine and
// may not touch the property graph.
//
// # A setting is an ordinary source property
//
// Value hands back a *prop.Property[T] — the same handle a viewmodel
// field is, indistinguishable to markup, to a binding, or to a paint
// node. That is the difference between a setting and a configuration
// snapshot: a page binds it, and a change repaints exactly the
// components that read it. There is no settings-specific binding
// mechanism because there must not be one.
//
// # Saving is dirty-tracked, never write-through
//
// prop.Set does not compare values, so a store that wrote through on
// every Set would hit the disk for assignments that changed nothing.
// Instead each setting carries a watcher computed whose invalidation
// hook marks the store dirty — the same trick the Composer's paint
// nodes and armVisibility use — and the actual write is deferred:
//
//   - a Set marks the store dirty and, at most once per dispatcher
//     batch, posts a Flush;
//   - Flush encodes the WHOLE document and compares it, byte for byte,
//     against the last document handed to the provider;
//   - only a document that actually differs reaches Provider.Save, on a
//     dedicated writer goroutine, with the result posted back through
//     the dispatcher.
//
// So the value comparison prop.Set declines to make happens once per
// flush over the whole document, where it is cheap and correct, rather
// than at a call site the property system gives no hook for. (It gives
// none: Property.Set invalidates its DEPENDENTS and never its own node,
// so a source property's OnInvalidate hook can never fire. Observing a
// Set from outside requires a dependent, which is what the watcher is.)
//
// # UI-goroutine confinement
//
// Every method on Store runs on the UI goroutine, because Delete and
// the watchers touch properties. Disk IO runs on the writer goroutine
// Start owns, and results come back through the post func — the
// Dispatcher, exactly as a Timer's tick does. Store.Start has
// gooey.Startable's signature for that reason, though a store is
// normally owned by the app rather than by the tree.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/WonderForgeLabs/gooey/prop"
)

// Provider is the host's persistence seam: the whole document in, the
// whole document out.
//
// Whole-document rather than per-key because that is what makes the
// contract implementable by anything — a file, a row in a database, an
// HTTP PUT, a browser's localStorage — without the host having to model
// keys, types, or partial writes. It is also what makes ordering
// trivial: there is one writer and one payload, so a save can never
// land half-applied.
//
// Both methods are called from a goroutine that is NOT the UI
// goroutine. An implementation must not touch the property graph.
type Provider interface {
	// Load returns the stored document. A nil or empty result means "no
	// document yet" and is not an error — that is how first run works.
	Load() ([]byte, error)
	// Save durably replaces the stored document with doc.
	Save(doc []byte) error
}

// entry is one registered key: the closures that read and reset its
// typed handle, plus the watcher that notices a Set.
//
// The closures are what keep the store reflection-free at its own
// boundary. T is a compile-time parameter of Value, so each entry
// carries functions that already know their own type; the store itself
// never asks a value what it is. This is the same discipline markup's
// propKinds table uses.
type entry struct {
	key    string
	defRaw json.RawMessage
	encode func() (json.RawMessage, error)
	reset  func()
	watch  *prop.Property[int]
}

// Store is one settings document, live.
//
// UI goroutine only. The one exception is the writer goroutine Start
// owns, which touches nothing but the Provider.
type Store struct {
	prov Provider

	// pass holds every key the loaded document carried that no handle
	// owns. Preserving them verbatim is what lets two versions of an
	// app — or an app and its plugins — share one document without
	// either erasing the other's keys, and it is why Delete has to work
	// on keys this process has never heard of.
	pass    map[string]json.RawMessage
	entries []*entry
	byKey   map[string]*entry

	post    func(func())
	reqs    chan []byte
	started bool

	autoSave bool
	posted   bool // a Flush closure is already queued on the dispatcher
	dirty    bool
	saved    []byte // the last document handed to the provider
	onError  func(error)
	lastErr  error
}

// Open loads the document through p and returns a store ready for
// registrations. It is synchronous and belongs before the UI starts:
// the values have to exist before the properties that carry them do.
func Open(p Provider) (*Store, error) {
	if p == nil {
		return nil, errors.New("settings: Open: nil Provider — the host owns persistence, so a store needs one (settings.File, settings.Memory, or your own)")
	}
	raw, err := p.Load()
	if err != nil {
		return nil, fmt.Errorf("settings: load: %w", err)
	}
	s := &Store{
		prov:     p,
		pass:     map[string]json.RawMessage{},
		byKey:    map[string]*entry{},
		autoSave: true,
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return s, nil
	}
	if err := json.Unmarshal(trimmed, &s.pass); err != nil {
		return nil, fmt.Errorf("settings: document is not a JSON object of dotted keys: %w", err)
	}
	// Compact every value so the byte comparison at save time reflects
	// content and not the whitespace a human editor happened to leave.
	for k, v := range s.pass {
		var buf bytes.Buffer
		if err := json.Compact(&buf, v); err != nil {
			return nil, fmt.Errorf("settings: %q: stored value is not valid JSON: %w", k, err)
		}
		s.pass[k] = json.RawMessage(bytes.Clone(buf.Bytes()))
	}
	return s, nil
}

// Value registers key and returns the bindable handle that owns it.
//
// The handle carries the stored value if the document had one and def
// otherwise. It is an ordinary source property in every respect: Set it
// to write the setting, bind it in markup, read it in a Render.
//
// A stored value that will not decode as T is reported AND survived:
// the returned handle is non-nil and carries def, because a settings
// file someone hand-edited badly must not stop the app from starting —
// but it must not be silent either, which is why the error comes back
// rather than being swallowed. Every other error returns a nil handle.
//
// All registrations must happen before Start, which takes the baseline
// the document is compared against.
func Value[T any](s *Store, key string, def T) (*prop.Property[T], error) {
	if s == nil {
		return nil, fmt.Errorf("settings: Value(%q): nil store", key)
	}
	if key == "" {
		return nil, errors.New("settings: Value: empty key")
	}
	if _, dup := s.byKey[key]; dup {
		return nil, fmt.Errorf("settings: %q is already registered; one key owns one handle, and a second would shadow the first with nothing to say so", key)
	}
	if s.started {
		return nil, fmt.Errorf("settings: %q registered after Start; Start takes the baseline the document is compared against, so every key has to exist by then", key)
	}
	defRaw, err := json.Marshal(def)
	if err != nil {
		return nil, fmt.Errorf("settings: %q: default value is not JSON-encodable: %w", key, err)
	}

	v := def
	var decErr error
	if raw, ok := s.pass[key]; ok {
		var got T
		if err := json.Unmarshal(raw, &got); err != nil {
			decErr = fmt.Errorf("settings: %q: stored value %s is not a %T: %w", key, raw, def, err)
		} else {
			v = got
		}
		// The key now belongs to a handle, so it leaves the pass-through
		// map: pass is exactly "keys nobody in this process owns".
		delete(s.pass, key)
	}

	p := prop.NewSource(v)
	e := &entry{key: key, defRaw: defRaw}
	e.encode = func() (json.RawMessage, error) {
		b, err := json.Marshal(p.Get())
		if err != nil {
			return nil, fmt.Errorf("settings: %q: value is not JSON-encodable: %w", key, err)
		}
		return json.RawMessage(b), nil
	}
	e.reset = func() { p.Set(def) }
	// The watcher is the only way to observe a Set: Property.Set
	// invalidates its dependents, never itself, so a dependent is what a
	// notification has to be made of. Reading p INSIDE this computed is
	// the call-site rule applied deliberately — the read becomes a
	// subscription. It is not a paint node, so damage counts are exactly
	// what they would be for a plain source property.
	e.watch = prop.NewComputed(func() int { p.Get(); return 0 })
	e.watch.OnInvalidate(func() { s.changed() })
	e.watch.Get() // arm: record the dependency
	s.entries = append(s.entries, e)
	s.byKey[key] = e
	return p, decErr
}

// changed is the watcher hook. It runs on the UI goroutine, inside the
// Set that caused it.
func (s *Store) changed() {
	s.dirty = true
	if !s.autoSave || s.post == nil || s.posted {
		return
	}
	// At most one queued flush: any number of Sets in one dispatcher
	// batch collapse into a single encode-and-compare.
	s.posted = true
	s.post(func() {
		s.posted = false
		s.Flush()
	})
}

// AutoSave controls whether a Set schedules its own save. It is on by
// default, because a setting that persists only when someone remembers
// to call Flush is the drift this package exists to remove.
//
// Turn it off when a handle is bound to something that changes on every
// keystroke: the document comparison suppresses no-op writes but not
// genuinely-different ones, so a TextBox bound straight to a setting
// would write once per character. With it off, call Flush on commit.
func (s *Store) AutoSave(on bool) { s.autoSave = on }

// Dirty reports whether anything has been Set since the last flush.
func (s *Store) Dirty() bool { return s.dirty }

// OnError installs the handler for failures that have no caller to
// return to — a provider that would not write, a flush with no writer.
// Without one the last such error is retained and readable from Err.
func (s *Store) OnError(fn func(error)) { s.onError = fn }

// Err returns the last error that had nowhere else to go, or nil.
func (s *Store) Err() error { return s.lastErr }

func (s *Store) fail(err error) {
	s.lastErr = err
	if s.onError != nil {
		s.onError(err)
	}
}

// Keys lists every key in the document — registered and pass-through
// alike — sorted.
func (s *Store) Keys() []string {
	out := make([]string, 0, len(s.pass)+len(s.entries))
	for k := range s.pass {
		out = append(out, k)
	}
	for _, e := range s.entries {
		out = append(out, e.key)
	}
	sort.Strings(out)
	return out
}

// Raw reads one key as JSON, whether or not a handle owns it. A
// registered key reads through its handle, so Raw and Get can never
// disagree.
//
// It is the untyped half of the CRUD surface, for tooling that walks a
// document it has no types for. The typed read is the handle.
func (s *Store) Raw(key string) (json.RawMessage, bool) {
	if e, ok := s.byKey[key]; ok {
		raw, err := e.encode()
		if err != nil {
			return nil, false
		}
		return raw, true
	}
	v, ok := s.pass[key]
	return v, ok
}

// Delete removes a key and reports whether there was one.
//
// For a registered key that means resetting its handle to the default
// it was created with — "delete this setting" is "forget it, go back to
// the default", and the reset is an ordinary Set, so anything bound to
// it repaints. For a pass-through key it means dropping it from the
// document.
//
// There is deliberately no untyped Write. A write has to reach the
// typed handle or the bound UI silently diverges from the document, and
// reaching it from an untyped key would take reflection.
func (s *Store) Delete(key string) bool {
	if e, ok := s.byKey[key]; ok {
		e.reset() // fires the watcher, which marks the store dirty
		return true
	}
	if _, ok := s.pass[key]; ok {
		delete(s.pass, key)
		s.changed()
		return true
	}
	return false
}

// snapshot re-arms every watcher and encodes the document.
func (s *Store) snapshot() ([]byte, error) {
	// Re-arm first, as one pass: a computed fires its invalidate hook
	// once per clean-to-dirty transition, so without this Get the NEXT
	// Set would be silent. This is also where the coalescing lives — any
	// number of Sets between two flushes cost exactly one notification.
	//
	// These Gets run outside any evaluation, so by the call-site rule
	// they record nothing: re-arming never makes the store part of some
	// other node's dependency set.
	for _, e := range s.entries {
		e.watch.Get()
	}
	doc := make(map[string]json.RawMessage, len(s.pass)+len(s.entries))
	for k, v := range s.pass {
		doc[k] = v
	}
	for _, e := range s.entries {
		raw, err := e.encode()
		if err != nil {
			return nil, err
		}
		if bytes.Equal(raw, e.defRaw) {
			// A value equal to its default is absent from the document,
			// exactly as vscode omits an unedited setting. Writing it out
			// would freeze today's default into every user's file and
			// silently veto tomorrow's.
			continue
		}
		doc[e.key] = raw
	}
	// json.Marshal sorts string map keys, so the document is byte-stable
	// for a given set of values — which is what makes the comparison in
	// Flush a valid "nothing changed" test rather than a coin flip.
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("settings: encode document: %w", err)
	}
	return append(buf, '\n'), nil
}

// Flush encodes the document and, only if it differs from the last one
// the provider was given, queues a save on the writer goroutine.
//
// UI goroutine only. It never blocks and never touches the disk.
func (s *Store) Flush() {
	buf, err := s.snapshot()
	if err != nil {
		s.fail(err)
		return
	}
	s.dirty = false
	if bytes.Equal(buf, s.saved) {
		// The comparison prop.Set declines to make, made once, here.
		return
	}
	if !s.started {
		s.fail(errors.New("settings: save requested before Start: there is no writer goroutine to save on — call Store.Start(post) once the dispatcher exists, or Store.SaveNow for a synchronous write"))
		// saved is deliberately NOT advanced: a document that never
		// reached a provider must not suppress the next attempt to send
		// the very same document.
		return
	}
	s.saved = buf
	s.request(buf)
}

// SaveNow writes synchronously on the calling goroutine, skipping the
// writer entirely. It is for the shutdown path and for hosts with no
// dispatcher: at exit there is no loop left to post a completion back
// to, and blocking is exactly what you want.
func (s *Store) SaveNow() error {
	buf, err := s.snapshot()
	if err != nil {
		return err
	}
	s.dirty = false
	if bytes.Equal(buf, s.saved) {
		return nil
	}
	if err := s.prov.Save(buf); err != nil {
		s.saved = nil // a failed write must not suppress its own retry
		return fmt.Errorf("settings: save: %w", err)
	}
	s.saved = buf
	return nil
}

func (s *Store) request(buf []byte) {
	// One slot, newest wins. A superseded document must never reach the
	// provider after the document that replaced it, and dropping it is
	// free: the newer buffer is a complete document, not a delta.
	for {
		select {
		case s.reqs <- buf:
			return
		case <-s.reqs:
		}
	}
}

// Start brings the store's writer goroutine up and returns the stop
// func. post is Dispatcher.Post — the only route a save result has back
// to the property graph.
//
// Start also takes the baseline the document is compared against, which
// is why every handle has to be registered by then: the baseline is
// what makes a Set that changes nothing cost no disk write at all.
//
// Both Start and the returned stop run on the UI goroutine. stop
// flushes anything still dirty, then closes and JOINS the writer, so
// after it returns the provider is guaranteed to have seen the last
// document and guaranteed never to be called again.
func (s *Store) Start(post func(func())) (stop func()) {
	if s.started {
		return func() {}
	}
	s.post = post
	s.started = true
	reqs := make(chan []byte, 1)
	done := make(chan struct{})
	stopped := make(chan struct{})
	s.reqs = reqs
	prov := s.prov

	go func() {
		defer close(stopped)
		save := func(buf []byte) {
			err := prov.Save(buf)
			if post == nil || err == nil {
				return
			}
			// Post, never apply: this goroutine may not touch the graph.
			post(func() {
				s.saved = nil // let the same document be retried
				s.fail(fmt.Errorf("settings: save: %w", err))
			})
		}
		for {
			select {
			case <-done:
				// One final non-blocking take: quitting must not lose the
				// change that prompted the quit.
				select {
				case buf := <-reqs:
					save(buf)
				default:
				}
				return
			case buf := <-reqs:
				save(buf)
			}
		}
	}()

	if s.dirty {
		// Something was Set before the writer existed; that change is
		// real and has to reach the provider, so no baseline is taken.
		s.Flush()
	} else if buf, err := s.snapshot(); err == nil {
		s.saved = buf
	}

	return func() {
		if !s.started {
			return
		}
		if s.dirty {
			s.Flush()
		}
		s.started = false
		close(done)
		<-stopped
	}
}
