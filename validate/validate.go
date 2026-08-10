// Package validate is the forms validation vocabulary over the property
// graph — the graph IS the validation framework.
//
// XAML needs three mechanisms for this: ValidationRules on the binding,
// INotifyDataErrorInfo on the viewmodel, and an ErrorTemplate to show
// the result. Here all three collapse into computeds:
//
//   - A validator is a computed string over the source property: empty
//     means valid, anything else is the message to show. Field builds
//     one from a source and a rule list.
//   - The input component reads the error while painting, so the error
//     visual is ordinary paint damage on that one component.
//   - Form-level validity is an aggregate over the field errors (All),
//     feeding a submit command's CanExecute (gooey.NewCommand(...).When)
//     — XAML's CanExecuteChanged, again as a property read.
//
// No events, no interfaces, no reflection: generics and closures.
//
// Rules run against the CURRENT value whenever the error is read, which
// happens at frame time like every computed — a validator is never a
// keystroke hook, it is a description of what "valid" means. A rule
// that reads OTHER properties (a confirm-password rule reading the
// password) subscribes to them by reading them, so cross-field
// validation needs no extra machinery either.
package validate

import (
	"cmp"
	"fmt"
	"regexp"
	"strings"

	"github.com/WonderForgeLabs/gooey/prop"
)

// Rule judges one value: it returns "" when the value passes, or the
// error message when it does not. Any func with this shape is a rule —
// the escape hatch is the type itself, not a wrapper.
type Rule[T any] func(T) string

// Field builds the error property for one input: a computed that reads
// src and runs the rules in order. The FIRST failing rule's message
// wins — order your rules from the most fundamental ("required") to the
// most specific, the way the messages should be revealed to someone
// fixing the field one problem at a time.
//
// Share the result with the input component (TextBox.Error), a
// ValidationMarker, and All: each reader subscribes by reading, so an
// edit repaints exactly the components that show this field's state.
func Field[T any](src *prop.Property[T], rules ...Rule[T]) *prop.Property[string] {
	return prop.NewComputed(func() string {
		v := src.Get()
		for _, r := range rules {
			if msg := r(v); msg != "" {
				return msg
			}
		}
		return ""
	})
}

// All aggregates field errors into one is-valid property: true when
// every field's error is empty. Feed it to a submit command's condition:
//
//	submit := gooey.NewCommand(save).When(validate.All(nameErr, mailErr))
//
// The result is a VALUE-STABILIZED source, not a plain computed, and
// the difference is the submit button's damage bill. Invalidation in
// the graph is eager and value-blind: a plain computed here would dirty
// the button's paint node on every keystroke into any field, valid or
// not. All instead re-evaluates its aggregate when a field invalidates
// and Sets the outward property ONLY when validity actually flipped —
// so a keystroke that does not change validity never touches the
// button, and a flip repaints it exactly once.
//
// The price is that the aggregate (and through it, the dirty field
// validators) evaluates during Set rather than at frame time — the one
// deliberately eager corner of the validation story, bought entirely
// with prop's public API. The fields are about to be evaluated for the
// error display anyway; no work is added, only moved.
func All(fields ...*prop.Property[string]) *prop.Property[bool] {
	agg := prop.NewComputed(func() bool {
		// Every field is read on every evaluation — no early exit. An
		// aggregate that stopped at the first error would drop its
		// subscription to the fields after it (deps are recorded by the
		// Get that actually runs) and go deaf to their changes.
		ok := true
		for _, f := range fields {
			if f.Get() != "" {
				ok = false
			}
		}
		return ok
	})
	out := prop.NewSource(agg.Get()) // the first Get arms agg's subscriptions
	agg.OnInvalidate(func() {
		if v := agg.Get(); v != out.Get() {
			out.Set(v)
		}
	})
	return out
}

// Has is true while field carries an error — the handle an inline
// error row binds its Visibility to (bool bindings map true→Visible,
// false→Collapsed), so the row occupies space only while there is
// something to say:
//
//	<Text Content="{{.NameErr}}" Visibility="{{.NameHasErr}}"/>
func Has(field *prop.Property[string]) *prop.Property[bool] {
	return prop.NewComputed(func() bool { return field.Get() != "" })
}

// Required fails empty input (whitespace-only counts as empty). An
// empty msg means "required".
//
// The other string rules pass on empty input by design, so "optional
// but well-formed when present" is spelled by simply omitting Required.
func Required(msg string) Rule[string] {
	if msg == "" {
		msg = "required"
	}
	return func(s string) string {
		if strings.TrimSpace(s) == "" {
			return msg
		}
		return ""
	}
}

// Len bounds the input's length in runes: at least min, at most max.
// max <= 0 means no upper bound. Empty input passes (see Required). An
// empty msg derives one from the bounds.
func Len(min, max int, msg string) Rule[string] {
	if msg == "" {
		switch {
		case min > 0 && max > 0:
			msg = fmt.Sprintf("must be %d–%d characters", min, max)
		case min > 0:
			msg = fmt.Sprintf("at least %d characters", min)
		default:
			msg = fmt.Sprintf("at most %d characters", max)
		}
	}
	return func(s string) string {
		if s == "" {
			return ""
		}
		n := len([]rune(s))
		if n < min || (max > 0 && n > max) {
			return msg
		}
		return ""
	}
}

// Pattern requires input to match expr, which is compiled ONCE, here at
// construction — never per keystroke. A bad expression panics like
// regexp.MustCompile, and for the same reason: rules are built when the
// viewmodel is, so the panic is a startup error, not a runtime one.
// Empty input passes (see Required). An empty msg means "invalid
// format" — the compiled expression is not a message a person should
// ever be shown.
func Pattern(expr, msg string) Rule[string] {
	re := regexp.MustCompile(expr)
	if msg == "" {
		msg = "invalid format"
	}
	return func(s string) string {
		if s == "" {
			return ""
		}
		if !re.MatchString(s) {
			return msg
		}
		return ""
	}
}

// Range bounds an ordered value inclusively: min <= v <= max. An empty
// msg derives one from the bounds.
func Range[T cmp.Ordered](min, max T, msg string) Rule[T] {
	if msg == "" {
		msg = fmt.Sprintf("must be between %v and %v", min, max)
	}
	return func(v T) string {
		if v < min || v > max {
			return msg
		}
		return ""
	}
}
