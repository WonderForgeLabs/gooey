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
	"math"
	"net/url"
	"regexp"
	"strconv"
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

// MinLen and MaxLen are the one-sided spellings of Len, named after the
// annotations they answer (StringLength's MinimumLength / MaximumLength)
// so a form's rules read like its data contract.
func MinLen(n int, msg string) Rule[string] { return Len(n, 0, msg) }
func MaxLen(n int, msg string) Rule[string] { return Len(0, n, msg) }

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

// ---- DataAnnotations parity ----
//
// These are the .NET System.ComponentModel.DataAnnotations vocabulary as
// rules: same names, same jobs, gooey's semantics stated in each
// comment. Where we deliberately differ from .NET's implementation the
// comment says so — .NET's validators are famously permissive, and a
// terminal form gets more use out of a rule that actually rejects
// nonsense than out of bug-compatibility with a 2008 regex.
//
// Every pattern here is compiled once, at package init: a stock rule
// costs no compilation even at construction.
//
// All of them PASS empty input, like every rule but Required — "optional
// but well-formed when present" stays the default reading.

var (
	// One @, something either side, a dot in the domain, no whitespace.
	// STRICTER than .NET's EmailAddressAttribute, which accepts "a@b"
	// (no dot at all); LOOSER than RFC 5322, which no regex should
	// attempt — quoted local parts, comments and address literals are
	// out of scope on purpose. Unicode passes: only @ and whitespace are
	// excluded, so IDN domains and non-ASCII local parts are accepted
	// rather than silently rejected.
	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s.]+(\.[^@\s.]+)+$`)
	// scheme://host[/rest] with a plausible host. .NET's UrlAttribute
	// accepts http/https/ftp; so do we, and we additionally REQUIRE a
	// non-empty host, because url.Parse is happy to return an empty
	// hostname for junk like "http://" or "file://x" and an
	// allow-anything URL rule is worse than none.
	urlRe = regexp.MustCompile(`^(?i)(https?|ftp)://[^\s/?#@]+(\.[^\s/?#@]+)*(/[^\s]*)?$`)
	// Digits and the separators a person actually types, with an
	// optional leading + and an optional extension. Digit COUNT is
	// checked separately (see Phone).
	phoneRe = regexp.MustCompile(`^\+?[0-9 ().\-]+((x|ext\.?|extension)\s*[0-9]+)?$`)
	digitRe = regexp.MustCompile(`^[0-9]+$`)
	intRe   = regexp.MustCompile(`^[+-]?[0-9]+$`)
)

// EmailAddress requires one @ with a dotted domain — see emailRe for
// exactly how this differs from .NET's. Default message: "not a valid
// email address".
func EmailAddress(msg string) Rule[string] {
	return matchRule(emailRe, msg, "not a valid email address")
}

// URL requires an http, https or ftp URL with a host. Markup spells it
// Url (the annotation's name); Go spells it URL (the language's).
// Default message: "not a valid URL".
func URL(msg string) Rule[string] {
	if msg == "" {
		msg = "not a valid URL"
	}
	return func(s string) string {
		if s == "" {
			return ""
		}
		if !urlRe.MatchString(s) {
			return msg
		}
		// The regex settles the shape; url.Parse settles that the host
		// survives parsing (an empty Host is the classic bypass).
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return msg
		}
		return ""
	}
}

// Phone accepts digits with the usual separators, an optional leading +
// and an optional extension, and requires 7 to 15 digits in the number
// itself — E.164's maximum, and enough to reject a stray year or zip
// code. .NET's PhoneAttribute has no digit-count rule at all; this is a
// deliberate tightening. Default message: "not a valid phone number".
func Phone(msg string) Rule[string] {
	if msg == "" {
		msg = "not a valid phone number"
	}
	return func(s string) string {
		if s == "" {
			return ""
		}
		if !phoneRe.MatchString(s) {
			return msg
		}
		// Count only the digits of the number, not the extension —
		// "+1 (555) 010-9999 x42" is a 11-digit number, not a 13-digit one.
		main := s
		if i := strings.IndexAny(strings.ToLower(s), "xe"); i >= 0 {
			main = s[:i]
		}
		n := 0
		for _, r := range main {
			if r >= '0' && r <= '9' {
				n++
			}
		}
		if n < 7 || n > 15 {
			return msg
		}
		return ""
	}
}

// CreditCard checks the Luhn checksum over 12–19 digits, ignoring spaces
// and dashes. Like .NET's CreditCardAttribute it is a typo catcher, not
// an authorization: no issuer prefixes, no network rules. Unlike .NET's
// it enforces a length window, which rejects the digit strings Luhn
// happily accepts (a bare "0" passes the checksum). Default message:
// "not a valid card number".
func CreditCard(msg string) Rule[string] {
	if msg == "" {
		msg = "not a valid card number"
	}
	return func(s string) string {
		if s == "" {
			return ""
		}
		digits := make([]int, 0, len(s))
		for _, r := range s {
			switch {
			case r >= '0' && r <= '9':
				digits = append(digits, int(r-'0'))
			case r == ' ' || r == '-':
			default:
				return msg
			}
		}
		if len(digits) < 12 || len(digits) > 19 {
			return msg
		}
		// Luhn, right to left: double every second digit, casting out
		// nines, and the total must be a multiple of ten.
		sum := 0
		for i := len(digits) - 1; i >= 0; i-- {
			d := digits[i]
			if (len(digits)-i)%2 == 0 {
				if d *= 2; d > 9 {
					d -= 9
				}
			}
			sum += d
		}
		if sum%10 != 0 {
			return msg
		}
		return ""
	}
}

// Compare requires the value to equal another property's — the
// confirm-password rule, and .NET's CompareAttribute. The read inside
// the rule is what subscribes the field to the OTHER property, so
// editing the original re-validates the confirmation with no wiring at
// all. Default message: "does not match".
func Compare[T comparable](other *prop.Property[T], msg string) Rule[T] {
	if msg == "" {
		msg = "does not match"
	}
	return func(v T) string {
		if v != other.Get() {
			return msg
		}
		return ""
	}
}

// Digits requires an unsigned run of ASCII digits — a PIN, a zip, an
// account number. Note that this is deliberately NOT unicode-digit
// aware: a field that accepts Devanagari digits and then hands them to
// strconv is a bug waiting to happen. Default message: "digits only".
func Digits(msg string) Rule[string] {
	return matchRule(digitRe, msg, "digits only")
}

// Integer requires an optionally signed integer. Default message: "must
// be a whole number".
func Integer(msg string) Rule[string] {
	return matchRule(intRe, msg, "must be a whole number")
}

// NumberRange is Range for a STRING input — the shape a text field
// binds, and what .NET's RangeAttribute does when it lands on a string
// property. Unparseable input fails with the same message; use
// math.Inf(-1)/math.Inf(1) for a one-sided bound. Default message
// derives from the bounds.
func NumberRange(min, max float64, msg string) Rule[string] {
	if msg == "" {
		switch {
		case math.IsInf(min, -1):
			msg = fmt.Sprintf("must be at most %v", max)
		case math.IsInf(max, 1):
			msg = fmt.Sprintf("must be at least %v", min)
		default:
			msg = fmt.Sprintf("must be between %v and %v", min, max)
		}
	}
	return func(s string) string {
		if s == "" {
			return ""
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil || v < min || v > max {
			return msg
		}
		return ""
	}
}

// matchRule is the shared body of the fixed-pattern rules: pass empty,
// else require a match.
func matchRule(re *regexp.Regexp, msg, def string) Rule[string] {
	if msg == "" {
		msg = def
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
