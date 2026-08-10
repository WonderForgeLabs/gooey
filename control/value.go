package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
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
func (s *Service) Values() ([]ValueEntry, []string, error) {
	if s.bind == nil {
		return nil, nil, errNoContext
	}
	out := make([]ValueEntry, 0)
	collectEntries(s.bind.Values, "", &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, namesOf(s.bind.Named), nil
}

// Value describes one dotted name, resolved exactly as a {{.A.B}}
// binding resolves.
func (s *Service) Value(name string) (ValueEntry, error) {
	v, err := s.lookup(name)
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
func (s *Service) Set(name string, v Value) error {
	h, err := s.lookup(name)
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
func (s *Service) Invoke(name string) error {
	v, err := s.lookup(name)
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
	}
	return nil, invalidf("registration %q: unknown kind; want one of string, int, bool, float, duration, color, any", r.Name)
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
