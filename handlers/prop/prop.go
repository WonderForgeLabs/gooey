// Package prophandlers is the property namespace: state a page can
// change without any app code behind it.
//
//	<Gooey xmlns:prop="gooey.dev/handlers/prop">
//	  <Button Content="+1"    Click="{{prop:Add .Count `1`}}"/>
//	  <Button Content="rec"   Click="{{prop:Toggle .KeepRecording}}"/>
//	  <Button Content="brief" Click="{{prop:Set .Mode `brief`}}"/>
//
// Until this pack existed, a markup-only control could DISPLAY every
// kind of state and change none of it: the only route to a Set was a
// viewmodel delegate bound with Click="{{.Fn}}", which is Go code, which
// an Include cannot have. A tab strip, a mode selector, a checkbox row —
// each needed a hand-written one-line closure per button whose entire
// body was p.Set(x). That is the gap this closes, and its whole extent:
// this namespace mutates properties the page already names. It cannot
// reach anything else.
//
// # It is a handler namespace, and there is no value half
//
// Every function here has an effect and returns nothing, so the pack
// registers on the PUSH side only:
//
//	markup.RegisterHandlers(prophandlers.URI, prophandlers.New())
//
// Provider deliberately does not implement markup.ValueProvider, so a
// host cannot register a mutation as a value even by mistake — the
// compiler refuses. From the page's side, {{prop:Set .Mode `a`}} written
// in a Text is a load error saying the namespace is event-only, which is
// markup/values.go's message, earned for free.
//
// The asymmetry is the point. Reading is what a binding already is:
// {{.Count}} needs no grant because the binding context IS the read
// surface. Writing is the new capability, so writing is what gets
// granted, and the read half would be a redundant second spelling of the
// thing markup does natively.
//
// # What registering this grants
//
// The grant is coarse by construction and it is worth stating plainly:
// registering this namespace lets the document WRITE any settable
// property reachable by a path in its own binding context — every name
// in Context.Values, and everything a path walks to from there. There is
// no per-property allowlist, because unlike env's variable names the
// operand here is a path into a context the host assembled itself. The
// context IS the allowlist; assembling it is where a host decides.
//
// So "read without write" is not only possible, it is the default: a
// host that never calls RegisterHandlers for this URI has a page that
// can bind and display everything and change nothing. And write-without-
// read does not exist, which is not a gap — you cannot bind a path you
// were not given, so anything writable was already readable.
//
// Withholding write on ONE property, with the rest writable, has a
// mechanism too, and it is a real one rather than a convention: expose
// that property as a COMPUTED. prop:Set over a computed is a load error
// (see settable below), so a derived handle is a genuinely read-only
// projection that markup cannot write and the type system does not have
// to encode.
//
// # Why Add is here, when arithmetic is not
//
// gooey has kept arithmetic out of the binding grammar on purpose: there
// is no {{.Count + 1}}, and this pack does not add one. Add is not an
// expression, it is an OPERATION on a property — the same category as
// Toggle, which nobody would call boolean algebra. The distinction is
// checkable rather than rhetorical:
//
//   - an expression is a value: it composes, nests, and can appear
//     wherever a binding appears. Add cannot. It has no value position,
//     it does not nest, and its first operand must be an lvalue.
//   - Add's result is defined by the property's own prior value, so it
//     exists only where there is a property to read it from.
//
// Toggle is Add's proof: b → !b is the same shape as n → n+d, and the
// grammar already had to pick one. Refusing Add while providing Toggle
// would leave a counter as the one piece of state a markup-only control
// cannot advance, for a distinction the page cannot see.
//
// What would cross the line is a bound expression as the OPERAND —
// {{prop:Add .Count (.Step * 2)}} — and nothing here parses that. The
// operand is a literal or one handle.
//
// # Every operation guards against a no-op write
//
// prop.Set does not compare (prop/prop.go): setting a property to the
// value it already holds still invalidates every dependent and still
// costs a repaint. Each command here therefore reads, computes, compares
// and returns early when nothing changed.
//
// This is not micro-optimism, it is the difference between a redundant
// click costing nothing and costing a subtree. The idiom this pack
// exists for is a row of Set buttons over one property — a mode
// selector, a tab strip — where clicking the ALREADY selected item is
// the single most common redundant event a UI receives, and where the
// dependents are every component that reads the mode. handlers/prop
// tests pin the count at 0 for a redundant Set and at the readers alone
// for a real one.
//
// # UI-goroutine confinement
//
// Nothing here starts a goroutine and nothing here touches the
// Dispatcher. A Command runs inline on the event-dispatch path
// (FocusManager.Dispatch → the component's HandleKey → Action.Execute),
// which is the UI goroutine by construction, so Get and Set are both
// legal where they are written and the repaint lands in the same frame
// as the keystroke rather than the next one. The tests assert exactly
// that: the new value is on screen after ONE Frame with no Drain.
package prophandlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// URI is the namespace URI markup declares to reach this provider.
const URI = "gooey.dev/handlers/prop"

// The pack's function names. All three are handler functions: they
// appear on an event attribute and nowhere else.
const (
	NameSet    = "Set"
	NameToggle = "Toggle"
	NameAdd    = "Add"
)

var allNames = []string{NameSet, NameToggle, NameAdd}

// AllNames lists every function the pack defines — its inventory, per
// docs/specs/2026-08-10-pack-distribution.md. The error text for an
// unknown function derives from this list, so the two cannot drift.
func AllNames() []string { return append([]string{}, allNames...) }

// Provider implements markup.HandlerProvider for the prop namespace. It
// holds no state and needs no configuration: the grant's extent is the
// binding context the host assembled, not anything this struct could
// carry, which is why New takes no arguments.
//
// It has no NewValue method on purpose — see the package comment.
type Provider struct{}

// New builds the provider. Register it with markup.RegisterHandlers.
func New() *Provider { return &Provider{} }

// NewCommand resolves one {{prop:…}} expression at load time. Every
// failure below is a load failure: the target's type, whether it is
// settable, the operand's type, and whether a literal operand parses are
// all decided here, so a page that loads cannot panic on click.
func (p *Provider) NewCommand(c *markup.Call) (gooey.Command, error) {
	want := 2
	if c.Fn == NameToggle {
		want = 1
	}
	switch c.Fn {
	case NameSet, NameToggle, NameAdd:
		if len(c.Args) != want {
			return nil, fmt.Errorf("%s takes %d argument(s) (%s), got %d", c.Fn, want, argsFor(c.Fn), len(c.Args))
		}
	default:
		return nil, fmt.Errorf("unknown function %q; prop provides: %s", c.Fn, strings.Join(allNames, ", "))
	}

	tgt := c.Args[0]
	if tgt.IsLiteral() {
		return nil, fmt.Errorf("%s writes to a property, so its first argument must be a bound path like .Count, not a backtick literal", c.Fn)
	}

	switch c.Fn {
	case NameToggle:
		return toggle(tgt)
	case NameSet:
		return set(tgt, c.Args[1])
	default:
		return add(tgt, c.Args[1])
	}
}

func argsFor(fn string) string {
	switch fn {
	case NameToggle:
		return "the bool property to flip"
	case NameSet:
		return "the property and its new value"
	default:
		return "the property and the amount to add"
	}
}

func toggle(tgt markup.Arg) (gooey.Command, error) {
	h, ok := tgt.Raw.(*prop.Property[bool])
	if !ok {
		return nil, fmt.Errorf("Toggle needs *prop.Property[bool]; .%s is %T", tgt.Path, tgt.Raw)
	}
	if err := settable(NameToggle, tgt, h.Settable()); err != nil {
		return nil, err
	}
	return mutate(h, func(b bool) bool { return !b }), nil
}

func set(tgt, val markup.Arg) (gooey.Command, error) {
	switch h := tgt.Raw.(type) {
	case *prop.Property[string]:
		return assign(h, tgt, val, func(s string) (string, error) { return s, nil })
	case *prop.Property[bool]:
		return assign(h, tgt, val, parseBool)
	case *prop.Property[int]:
		return assign(h, tgt, val, parseInt)
	case *prop.Property[int64]:
		return assign(h, tgt, val, parseInt64)
	case *prop.Property[float64]:
		return assign(h, tgt, val, parseFloat)
	case *prop.Property[time.Duration]:
		return assign(h, tgt, val, parseDuration)
	}
	return nil, fmt.Errorf("Set needs a settable property handle (*prop.Property over string, bool, int, int64, float64 or time.Duration); .%s is %T", tgt.Path, tgt.Raw)
}

func add(tgt, val markup.Arg) (gooey.Command, error) {
	switch h := tgt.Raw.(type) {
	case *prop.Property[int]:
		return addTo(h, tgt, val, parseInt)
	case *prop.Property[int64]:
		return addTo(h, tgt, val, parseInt64)
	case *prop.Property[float64]:
		return addTo(h, tgt, val, parseFloat)
	case *prop.Property[time.Duration]:
		return addTo(h, tgt, val, parseDuration)
	}
	return nil, fmt.Errorf("Add needs a numeric property (*prop.Property over int, int64, float64 or time.Duration); .%s is %T", tgt.Path, tgt.Raw)
}

// number is Add's domain. time.Duration satisfies it through ~int64,
// which is what makes {{prop:Add .Timeout `5s`}} fall out of the same
// code as {{prop:Add .Count `1`}}.
type number interface{ ~int | ~int64 | ~float64 }

// assign builds Set for one target type. The operand is either a
// literal — parsed HERE, so `abc` into an int fails the load — or a
// handle of the SAME type. A handle of a different type is refused
// rather than coerced: rendering an int into a string property would be
// a silent conversion inside a mutation, and the page can be explicit
// instead.
func assign[T comparable](h *prop.Property[T], tgt, val markup.Arg, parse func(string) (T, error)) (gooey.Command, error) {
	if err := settable(NameSet, tgt, h.Settable()); err != nil {
		return nil, err
	}
	next, err := operand(NameSet, tgt, val, parse)
	if err != nil {
		return nil, err
	}
	return mutate(h, func(T) T { return next() }), nil
}

// addTo builds Add for one numeric target type.
func addTo[T number](h *prop.Property[T], tgt, val markup.Arg, parse func(string) (T, error)) (gooey.Command, error) {
	if err := settable(NameAdd, tgt, h.Settable()); err != nil {
		return nil, err
	}
	next, err := operand(NameAdd, tgt, val, parse)
	if err != nil {
		return nil, err
	}
	return mutate(h, func(cur T) T { return cur + next() }), nil
}

// operand resolves the second argument to a function returning a value
// of the target's type, failing the load if it is neither a parseable
// literal nor a handle of that type. The returned function reads its
// handle at INVOKE time, so a bound operand keeps lvalue semantics —
// {{prop:Add .Count .Step}} follows .Step's current value.
func operand[T any](fn string, tgt, val markup.Arg, parse func(string) (T, error)) (func() T, error) {
	if val.IsLiteral() {
		v, err := parse(val.String())
		if err != nil {
			return nil, fmt.Errorf("%s .%s: `%s` is not a valid %T value: %w", fn, tgt.Path, val.String(), *new(T), err)
		}
		return func() T { return v }, nil
	}
	if src, ok := val.Raw.(*prop.Property[T]); ok {
		return src.Get, nil
	}
	if v, ok := val.Raw.(T); ok {
		return func() T { return v }, nil
	}
	return nil, fmt.Errorf("%s .%s needs a %T operand: a backtick literal, or a handle of the same type — .%s is %T",
		fn, tgt.Path, *new(T), val.Path, val.Raw)
}

// settable turns prop.Property.Settable into the load error it exists
// for. Set on a computed panics, so without this check a mutation
// written against a derived property would build clean and take the
// whole app down on its first click.
//
// The message names the alternative because a computed target is often
// not a mistake: it is a host deliberately publishing a read-only
// projection, and the page needs to be told which source to write.
func settable(fn string, tgt markup.Arg, ok bool) error {
	if ok {
		return nil
	}
	return fmt.Errorf("%s cannot write .%s: it is a COMPUTED property (%T), which derives its value and has no setter — writing it would panic. Write the source it derives from, or ask the host to publish a settable handle",
		fn, tgt.Path, tgt.Raw)
}

// mutate is every operation's Command: read, compute, compare, and Set
// only on a real change.
//
// The comparison is the whole reason this function exists. prop.Set does
// not compare, so re-selecting the mode a page is already in would
// otherwise invalidate every dependent and repaint them all for no
// visible difference. T is constrained to comparable, which every type
// the switches above admit satisfies.
//
// The Get here is a plain READ, not a subscription: it runs on the
// event-dispatch path with no computed on prop's evalStack, so
// recordRead returns immediately. The call site decides, and this call
// site is an event handler.
func mutate[T comparable](h *prop.Property[T], next func(T) T) gooey.Command {
	return func() {
		cur := h.Get()
		v := next(cur)
		if v == cur {
			return
		}
		h.Set(v)
	}
}

func parseBool(s string) (bool, error)              { return strconv.ParseBool(strings.TrimSpace(s)) }
func parseInt(s string) (int, error)                { return strconv.Atoi(strings.TrimSpace(s)) }
func parseInt64(s string) (int64, error)            { return strconv.ParseInt(strings.TrimSpace(s), 10, 64) }
func parseFloat(s string) (float64, error)          { return strconv.ParseFloat(strings.TrimSpace(s), 64) }
func parseDuration(s string) (time.Duration, error) { return time.ParseDuration(strings.TrimSpace(s)) }
