package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"sort"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/imaging"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Kind names one row of markup's propKinds table — the whole type system
// of the control plane, mirrored onto the wire as the contract's
// ValueKind/TypedValue. Adding a kind means adding a propKinds row
// first, then the matching case everywhere a Kind is switched on; the
// two grow in lockstep or not at all.
type Kind int

const (
	KindUnspecified Kind = iota
	KindString
	KindInt
	KindBool
	KindFloat
	KindDuration
	KindColor
	// KindAny is the escape hatch for app types with no markup literal,
	// exactly as in propKinds. Its value crosses as UTF-8 JSON.
	KindAny
	// KindImage is an image.Image, carried as ENCODED bytes and decoded
	// through the imaging registry.
	//
	// It is the one Kind with no propKinds row, and the lockstep rule
	// above does not apply to it. A propKinds row is a parser for a
	// markup LITERAL and there is no way to write a picture inline;
	// markup can still BIND one, since <Image Src="{{.Logo}}">
	// type-checks against *prop.Property[image.Image]. Bindability and
	// literal-spellability are the same axis for every other kind and
	// different for this one.
	//
	// Without it no client can put a picture on a page: markup swapped
	// over the control plane is built from bytes and has no file system,
	// so <Image Src="logo.png"> cannot resolve, and there was no
	// property type to bind Src to instead.
	KindImage
)

// String spells a Kind the way markup does — the Type= attribute values.
func (k Kind) String() string {
	switch k {
	case KindString:
		return "string"
	case KindInt:
		return "int"
	case KindBool:
		return "bool"
	case KindFloat:
		return "float"
	case KindDuration:
		return "duration"
	case KindColor:
		return "color"
	case KindAny:
		return "any"
	case KindImage:
		return "image"
	}
	return "unspecified"
}

// KindOf maps a markup type spelling ("string", "int", ...) to its Kind,
// KindUnspecified for anything off the table.
func KindOf(name string) Kind {
	switch name {
	case "string":
		return KindString
	case "int":
		return KindInt
	case "bool":
		return KindBool
	case "float":
		return KindFloat
	case "duration":
		return KindDuration
	case "color":
		return KindColor
	case "any":
		return KindAny
	case "image":
		return KindImage
	}
	return KindUnspecified
}

// Value carries one value of a propKinds type: the in-process form of
// the contract's TypedValue. Exactly one field beside Kind is
// meaningful, selected by Kind — a struct rather than an interface so it
// stays plain copyable data with no boxing at the boundary.
type Value struct {
	Kind     Kind
	Str      string
	Int      int64
	Bool     bool
	Float    float64
	Duration time.Duration
	Color    render.Color
	// Image is the KindImage payload, already decoded. The wire carries
	// encoded bytes; decoding happens at the adapter so a bad image is
	// one clear error at the boundary rather than a blank picture later.
	Image image.Image
	// JSON is the KindAny payload: one UTF-8 JSON document.
	JSON []byte
}

// Equal reports whether two Values carry the same kind and the same
// payload. It exists because Value holds a byte slice (the KindAny
// JSON), so == does not compile; delta collection needs exactly this.
func (v Value) Equal(o Value) bool {
	if v.Kind != o.Kind {
		return false
	}
	switch v.Kind {
	case KindString:
		return v.Str == o.Str
	case KindInt:
		return v.Int == o.Int
	case KindBool:
		return v.Bool == o.Bool
	case KindFloat:
		return v.Float == o.Float
	case KindDuration:
		return v.Duration == o.Duration
	case KindColor:
		return v.Color == o.Color
	case KindAny:
		return bytes.Equal(v.JSON, o.JSON)
	case KindImage:
		return sameImage(v.Image, o.Image)
	}
	return true
}

func StringValue(v string) Value          { return Value{Kind: KindString, Str: v} }
func IntValue(v int64) Value              { return Value{Kind: KindInt, Int: v} }
func BoolValue(v bool) Value              { return Value{Kind: KindBool, Bool: v} }
func FloatValue(v float64) Value          { return Value{Kind: KindFloat, Float: v} }
func DurationValue(v time.Duration) Value { return Value{Kind: KindDuration, Duration: v} }
func ColorValue(v render.Color) Value     { return Value{Kind: KindColor, Color: v} }
func JSONValue(v []byte) Value            { return Value{Kind: KindAny, JSON: v} }
func ImageValue(v image.Image) Value      { return Value{Kind: KindImage, Image: v} }

// SourceImage is a decoded image that still carries the bytes it came
// from. It IS an image.Image, so it binds to <Image Src> like any other
// and nothing downstream needs to know about it.
//
// It exists because decoding is lossy in the direction that matters
// here: given only an image.Image, a reader cannot answer "what did the
// client send?" — it can only re-encode, which is a different file, and
// puts an encoder on the ListValues and frame-delta paths to produce it.
// Keeping the source bytes beside the pixels makes read-back exact and
// free, at the cost of holding the encoded form for as long as the
// property lives.
//
// A picture constructed in-process (not decoded from bytes) is an
// ordinary image.Image with no source, and read-back reports no bytes
// for it rather than inventing some.
type SourceImage struct {
	image.Image
	Bytes []byte
}

// ImageBytesOf returns the encoded bytes an image was decoded from, or
// nil when it did not come from any — a type assertion rather than an
// interface method, so image.Image stays the currency everywhere else.
func ImageBytesOf(img image.Image) []byte {
	if si, ok := img.(SourceImage); ok {
		return si.Bytes
	}
	return nil
}

// sameImage answers "is this the same picture" for delta collection,
// which runs once per frame per session — so it must be cheap, and it
// must never panic.
//
// The panic is the reason this is not a bare `==`. Comparing two
// interface values panics when their dynamic type is identical and NOT
// comparable, and SourceImage is exactly that: a struct embedding a
// []byte. Its fields are exported, so any caller can build one with nil
// Bytes without going through DecodedImageValue — and then two of them
// on either side of `==` take down the UI goroutine, which is every
// session's UI, not just the offender's.
//
// So: source bytes when both carry them (exact and cheap), pointer
// identity for the standard image types, which are the ones a real app
// holds, and "changed" for anything this cannot compare safely. The
// asymmetry decides the default — a spurious "changed" costs one
// repaint, and guessing the other way costs the process.
//
// reflect.Type.Comparable would answer this in one line and is not
// available: no reflection in core is invariant #1, and this is the
// "just use reflection here" case CLAUDE.md calls a design change
// rather than a shortcut.
func sameImage(a, b image.Image) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if ab, bb := ImageBytesOf(a), ImageBytesOf(b); ab != nil && bb != nil {
		return bytes.Equal(ab, bb)
	}
	// An allowlist, not a denylist: every arm here is a POINTER type and
	// therefore comparable. A type absent from it is reported changed
	// rather than compared, which is why adding an image type to the
	// framework can never introduce the panic this function exists to
	// prevent.
	switch at := a.(type) {
	case *image.RGBA:
		bt, ok := b.(*image.RGBA)
		return ok && at == bt
	case *image.NRGBA:
		bt, ok := b.(*image.NRGBA)
		return ok && at == bt
	case *image.RGBA64:
		bt, ok := b.(*image.RGBA64)
		return ok && at == bt
	case *image.NRGBA64:
		bt, ok := b.(*image.NRGBA64)
		return ok && at == bt
	case *image.Gray:
		bt, ok := b.(*image.Gray)
		return ok && at == bt
	case *image.Gray16:
		bt, ok := b.(*image.Gray16)
		return ok && at == bt
	case *image.Alpha:
		bt, ok := b.(*image.Alpha)
		return ok && at == bt
	case *image.Alpha16:
		bt, ok := b.(*image.Alpha16)
		return ok && at == bt
	case *image.CMYK:
		bt, ok := b.(*image.CMYK)
		return ok && at == bt
	case *image.YCbCr:
		bt, ok := b.(*image.YCbCr)
		return ok && at == bt
	case *image.NYCbCrA:
		bt, ok := b.(*image.NYCbCrA)
		return ok && at == bt
	case *image.Paletted:
		bt, ok := b.(*image.Paletted)
		return ok && at == bt
	case *image.Uniform:
		bt, ok := b.(*image.Uniform)
		return ok && at == bt
	}
	return false
}

// ImageLimits is what the control plane will accept as an image from a
// client. Both transports decode on the app's single UI goroutine, so a
// decode that runs long stalls input, layout and every other session's
// frames — and Bridge.round's timeout bounds the WAITING, not the
// decode, so the stall outlives it. One policy, stated once, because two
// transports with two ceilings is the same as having the looser one.
//
// The numbers are deliberately generous rather than tight: a phone
// photograph (4032x3024, about 12.2 megapixels) must still work, since
// refusing an ordinary picture would push callers toward pre-scaling
// and hide the limit's purpose. Anything past this is not a terminal
// image, and the two caps are both load-bearing — bytes alone admit the
// bomb, whose entire trick is a small file that declares enormous
// dimensions.
func ImageLimits() imaging.Limits {
	return imaging.Limits{
		MaxBytes:  16 << 20,   // 16 MiB encoded
		MaxPixels: 16_000_000, // ~64 MiB as RGBA
	}
}

// DecodedImageValue is ImageValue for a picture that came from bytes:
// it keeps the source so a reader can hand back exactly what arrived.
func DecodedImageValue(img image.Image, src []byte) Value {
	return Value{Kind: KindImage, Image: SourceImage{Image: img, Bytes: src}}
}

// EntryKind classifies one name in the binding context.
type EntryKind int

const (
	EntryOther EntryKind = iota
	EntryProperty
	EntryCommand
	EntryLiteral
)

// ValueEntry describes one dotted name in the binding context — the
// in-process ValueInfo.
type ValueEntry struct {
	Name string
	Kind EntryKind
	// Type is the propKinds row when the entry is a property on the
	// table; KindUnspecified for off-table handles (style, float
	// slices), commands and literals.
	Type Kind
	// Value is the current value when it is representable; nil
	// otherwise.
	Value *Value
	// GoType is the entry's %T — diagnostic only, never parsed.
	GoType string
}

// Values describes the bindable surface: every dotted name in the
// binding context, sorted, plus the Name= identities of the current
// tree.
//
// A SCOPED service NARROWS rather than refuses: a guest is shown the
// values its grant reaches and the names inside its island, which is
// also the surface its markup may bind. Hiding beats refusing here —
// the listing is what a client uses to discover what it may do, so a
// listing that showed the whole app and then refused most of it would
// be a map of the host's state handed to a guest that cannot use it.
//
// This is also what scopes the streaming FrameDelta: the broadcaster
// diffs whatever Values reports, so a scoped session's property deltas
// are its granted values and no others, with no extra filtering code.
func (s *Service) Values() ([]ValueEntry, []string, error) {
	if s.bind == nil {
		return nil, nil, errNoContext
	}
	out := make([]ValueEntry, 0)
	if s.scoped() {
		collectEntries(s.grantedValues(), "", &out)
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out, s.islandNames(), nil
	}
	collectEntries(s.bind.Values, "", &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, namesOf(s.bind.Named), nil
}

// islandNames is the Name= identities inside the granted island — the
// addresses a scoped guest may patch and focus.
func (s *Service) islandNames() []string {
	set := s.islandSet()
	out := make([]string, 0, len(set))
	for n, w := range s.bind.Named {
		if set[w] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Value describes one dotted name, resolved exactly as a {{.A.B}}
// binding resolves.
func (s *Service) Value(name string) (ValueEntry, error) {
	v, err := s.resolveValue(name)
	if err != nil {
		return ValueEntry{}, err
	}
	if _, ok := v.(map[string]any); ok {
		return ValueEntry{}, notFoundf("%q is a nested scope, not a value; ListValues shows its members", name)
	}
	return describe(name, v), nil
}

func collectEntries(vals map[string]any, prefix string, out *[]ValueEntry) {
	for k, v := range vals {
		name := k
		if prefix != "" {
			name = prefix + "." + k
		}
		if m, ok := v.(map[string]any); ok {
			collectEntries(m, name, out)
			continue
		}
		*out = append(*out, describe(name, v))
	}
}

// describe classifies one context entry by type switch, the same way
// everything else in gooey inspects a value. The Gets here are plain
// reads on the UI goroutine — outside any evaluation, they record
// nothing.
func describe(name string, v any) ValueEntry {
	e := ValueEntry{Name: name, GoType: fmt.Sprintf("%T", v)}
	set := func(k Kind, val Value) {
		e.Kind, e.Type, e.Value = EntryProperty, k, &val
	}
	switch h := v.(type) {
	case *prop.Property[string]:
		set(KindString, StringValue(h.Get()))
	case *prop.Property[bool]:
		set(KindBool, BoolValue(h.Get()))
	case *prop.Property[int]:
		set(KindInt, IntValue(int64(h.Get())))
	case *prop.Property[float64]:
		set(KindFloat, FloatValue(h.Get()))
	case *prop.Property[time.Duration]:
		set(KindDuration, DurationValue(h.Get()))
	case *prop.Property[render.Color]:
		set(KindColor, ColorValue(h.Get()))
	case *prop.Property[image.Image]:
		set(KindImage, ImageValue(h.Get()))
	case *prop.Property[any]:
		// The escape hatch: the current value crosses as JSON where it
		// can, and stays a descriptor where it cannot.
		e.Kind, e.Type = EntryProperty, KindAny
		if b, err := json.Marshal(h.Get()); err == nil {
			val := JSONValue(b)
			e.Value = &val
		}
	case *prop.Property[render.Style], *prop.Property[[]float64]:
		// Off the propKinds table: descriptor only, same ceiling as
		// everywhere else.
		e.Kind = EntryProperty
	case gooey.Command:
		e.Kind = EntryCommand
	case func():
		e.Kind = EntryCommand
	case gooey.Action:
		e.Kind = EntryCommand
	case string:
		e.Kind, e.Type = EntryLiteral, KindString
		val := StringValue(h)
		e.Value = &val
	default:
		e.Kind = EntryOther
	}
	return e
}

// Set writes one named source property. The Value's Kind must match the
// handle's type; a mismatch is an error naming both sides, and nothing
// changes — the type switch IS the type check, exactly as it is for
// markup bindings.
// A scoped session may write only its GRANTED values. Note what that
// deliberately does NOT mean: "a property my island binds" is not the
// rule. An island commonly READS host state — a status line bound to
// {{.App.Status}} — and being able to display a value is not authority
// to write it. The two are separate grants because they are separate
// capabilities, and only the value list confers the write.
func (s *Service) Set(name string, v Value) error {
	h, err := s.resolveValue(name)
	if err != nil {
		return err
	}
	switch p := h.(type) {
	case *prop.Property[string]:
		if v.Kind != KindString {
			return setMismatch(name, KindString, v.Kind)
		}
		p.Set(v.Str)
	case *prop.Property[bool]:
		if v.Kind != KindBool {
			return setMismatch(name, KindBool, v.Kind)
		}
		p.Set(v.Bool)
	case *prop.Property[int]:
		if v.Kind != KindInt {
			return setMismatch(name, KindInt, v.Kind)
		}
		n := int(v.Int)
		if int64(n) != v.Int {
			return invalidf("%q: %d is outside this host's int range", name, v.Int)
		}
		p.Set(n)
	case *prop.Property[float64]:
		if v.Kind != KindFloat {
			return setMismatch(name, KindFloat, v.Kind)
		}
		p.Set(v.Float)
	case *prop.Property[time.Duration]:
		if v.Kind != KindDuration {
			return setMismatch(name, KindDuration, v.Kind)
		}
		p.Set(v.Duration)
	case *prop.Property[render.Color]:
		if v.Kind != KindColor {
			return setMismatch(name, KindColor, v.Kind)
		}
		p.Set(v.Color)
	case *prop.Property[image.Image]:
		if v.Kind != KindImage {
			return setMismatch(name, KindImage, v.Kind)
		}
		p.Set(v.Image)
	case *prop.Property[any]:
		if v.Kind != KindAny {
			return setMismatch(name, KindAny, v.Kind)
		}
		var av any
		if err := json.Unmarshal(v.JSON, &av); err != nil {
			return invalidf("%q: the any payload is not valid JSON: %v", name, err)
		}
		p.Set(av)
	default:
		return invalidf("%q is %T, which SetProperty cannot write; the settable kinds are %s",
			name, h, "string, int, bool, float, duration, color and any")
	}
	return nil
}

func setMismatch(name string, want, got Kind) *Error {
	return invalidf("%q is a %s property; got a %s value", name, want, got)
}

// Invoke runs a named command from the binding context.
//
// Commands live in the same namespace as properties, so a command is
// granted exactly the way a property is. That is the whole rule, and it
// is the right one: a command is host code, and letting a guest run it
// because its island happens to have a Button bound to it would make
// PatchMarkup an escalation path — patch in a Button, invoke it, run
// anything.
func (s *Service) Invoke(name string) error {
	v, err := s.resolveValue(name)
	if err != nil {
		return err
	}
	switch cmd := v.(type) {
	case gooey.Command:
		cmd()
	case func():
		cmd()
	case gooey.Action:
		// An Action's Run is a no-op while CanExecute is false, the same
		// contract a Button holds it to.
		cmd.Run()
	default:
		return invalidf("%q is %T, not a command; ListValues shows which names are commands", name, v)
	}
	return nil
}

// ---- runtime property registration (issue #89) ----

// Registration asks for a fresh typed source property in the binding
// context. Commands cannot be registered — behavior needs code, not
// storage.
type Registration struct {
	Name string
	Kind Kind
	// Initial, when non-nil, must carry the same Kind; nil means the
	// kind's zero value.
	Initial *Value
}

// Register materializes new typed source properties. A name that
// already exists — at any depth of the dotted path — is an error: the
// context stays the one source of truth. All-or-nothing: a bad
// registration leaves the context untouched.
func (s *Service) Register(regs []Registration) error {
	if s.bind == nil {
		return errNoContext
	}
	rollback, err := s.register(regs)
	if err != nil {
		rollback()
		return err
	}
	return nil
}

// register applies regs and returns an undo. On error the caller must
// run the rollback; on success the rollback is kept by SwapMarkup for
// its own failure path and discarded by Register.
func (s *Service) register(regs []Registration) (rollback func(), err error) {
	type created struct {
		parent map[string]any
		key    string
	}
	var undo []created
	rollback = func() {
		for i := len(undo) - 1; i >= 0; i-- {
			delete(undo[i].parent, undo[i].key)
		}
	}
	for _, r := range regs {
		if strings.TrimSpace(r.Name) == "" {
			return rollback, invalidf("a property registration needs a name")
		}
		// A scoped guest may only grow the namespace it was granted.
		// Without this a guest registers a top-level name and then writes
		// it — which would make register_properties a way to mint a
		// capability out of nothing, and the grant would bound only the
		// names that happened to exist at attach time.
		if err := s.mayTouchValue(r.Name); err != nil {
			return rollback, err
		}
		h, err := sourceFor(r)
		if err != nil {
			return rollback, err
		}
		segs := strings.Split(r.Name, ".")
		m := s.bind.Values
		if m == nil {
			return rollback, errNoContext
		}
		for i, seg := range segs[:len(segs)-1] {
			cur, ok := m[seg]
			if !ok {
				fresh := map[string]any{}
				m[seg] = fresh
				undo = append(undo, created{parent: m, key: seg})
				m = fresh
				continue
			}
			inner, ok := cur.(map[string]any)
			if !ok {
				return rollback, invalidf("cannot register %q: %q already names a %T, not a scope",
					r.Name, strings.Join(segs[:i+1], "."), cur)
			}
			m = inner
		}
		leaf := segs[len(segs)-1]
		if _, exists := m[leaf]; exists {
			return rollback, invalidf("cannot register %q: the name already exists; the context is the one source of truth", r.Name)
		}
		m[leaf] = h
		undo = append(undo, created{parent: m, key: leaf})
	}
	return rollback, nil
}

// Unregister removes names from the binding context — Register's
// inverse, and the delete half of a CRUD surface over the viewmodel. A
// client that can grow the context must be able to shrink it again, or
// every generated name leaks for the life of the process.
//
// All-or-nothing, like Register: a name that does not resolve fails the
// whole batch and puts back anything already taken out.
//
// It does not track provenance — a name the app itself installed is
// removable too, because the context is the one source of truth and
// nothing in it records who wrote it. What this does NOT do is disturb
// the running tree: a component bound to a removed name still holds its
// property handle directly and keeps rendering and updating. Removal
// only takes the name out of scope for markup built AFTERWARDS, which
// is exactly what "unbind it from future pages" means.
func (s *Service) Unregister(names []string) error {
	if s.bind == nil {
		return errNoContext
	}
	if s.bind.Values == nil {
		return errNoContext
	}
	type removed struct {
		parent map[string]any
		key    string
		val    any
	}
	// No rollback closure any more, and its absence is the point: with
	// resolution finished before the first delete, there is no failure
	// path left that runs after a mutation. All-or-nothing used to be
	// maintained by undoing; it is now maintained by not starting.
	// RESOLVE EVERYTHING FIRST, THEN DELETE. A batch is a set, and a set
	// has no order, so the same set must succeed or fail the same way
	// however it is written down.
	//
	// Resolving and deleting in one pass made that false. Unregister
	// ["scope", "scope.child"] deleted the parent, then failed to resolve
	// the child — its scope had just been removed by the previous
	// iteration — and rolled the whole batch back with "no such name" for
	// a name that existed when the call was made. Written the other way
	// round, ["scope.child", "scope"], the identical request succeeded.
	// A caller holding a set of names cannot be expected to sort it into
	// the one order the implementation tolerates.
	//
	// Two passes fix it by construction: every lookup happens against the
	// map as the caller found it, so no resolution can be invalidated by a
	// deletion that is part of the same request.
	targets := make([]removed, 0, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return invalidf("cannot unregister: a name is required")
		}
		segs := strings.Split(name, ".")
		m := s.bind.Values
		ok := true
		for _, seg := range segs[:len(segs)-1] {
			inner, isScope := m[seg].(map[string]any)
			if !isScope {
				ok = false
				break
			}
			m = inner
		}
		leaf := segs[len(segs)-1]
		var val any
		if ok {
			val, ok = m[leaf]
		}
		if !ok {
			// Nothing has been deleted yet, so there is nothing to roll
			// back — the all-or-nothing guarantee holds trivially here.
			return notFoundf("cannot unregister %q: no such name", name)
		}
		targets = append(targets, removed{parent: m, key: leaf, val: val})
	}
	for _, t := range targets {
		delete(t.parent, t.key)
	}
	return nil
}

// sourceFor builds the fresh *prop.Property[T] a registration asks for —
// a type switch over Kind, one case per propKinds row.
func sourceFor(r Registration) (any, error) {
	init := r.Initial
	if init != nil && init.Kind != r.Kind {
		return nil, invalidf("registration %q: the initial value is a %s, not a %s", r.Name, init.Kind, r.Kind)
	}
	switch r.Kind {
	case KindString:
		var v string
		if init != nil {
			v = init.Str
		}
		return prop.NewSource(v), nil
	case KindInt:
		var v int64
		if init != nil {
			v = init.Int
		}
		n := int(v)
		if int64(n) != v {
			return nil, invalidf("registration %q: %d is outside this host's int range", r.Name, v)
		}
		return prop.NewSource(n), nil
	case KindBool:
		var v bool
		if init != nil {
			v = init.Bool
		}
		return prop.NewSource(v), nil
	case KindFloat:
		var v float64
		if init != nil {
			v = init.Float
		}
		return prop.NewSource(v), nil
	case KindDuration:
		var v time.Duration
		if init != nil {
			v = init.Duration
		}
		return prop.NewSource(v), nil
	case KindColor:
		var v render.Color
		if init != nil {
			v = init.Color
		}
		return prop.NewSource(v), nil
	case KindAny:
		var v any
		if init != nil {
			if err := json.Unmarshal(init.JSON, &v); err != nil {
				return nil, invalidf("registration %q: the any payload is not valid JSON: %v", r.Name, err)
			}
		}
		return prop.NewSource(v), nil
	case KindImage:
		// nil is a legitimate starting value: a page can bind <Image
		// Src="{{.Logo}}"> before anything has supplied a picture, and
		// Image renders nothing for a nil source rather than failing.
		var v image.Image
		if init != nil {
			v = init.Image
		}
		return prop.NewSource(v), nil
	}
	return nil, invalidf("registration %q: unknown kind; want one of string, int, bool, float, duration, color, any, image", r.Name)
}

// namesOf is the sorted Name= identities of a name table.
func namesOf(named map[string]gooey.Component) []string {
	out := make([]string, 0, len(named))
	for n := range named {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
