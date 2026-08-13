// Package strhandlers is the string namespace: the pure-function half
// of the value-namespace mechanism.
//
//	<Gooey xmlns:str="gooey.dev/handlers/str">
//	  <Text>{{str:Upper .User}}</Text>
//	  <Text>{{str:Pad .Name `12`}}{{str:Truncate .Note `40`}}</Text>
//
// Every function here is a value: it has no effect, it returns a
// string, and it is registered on the pull side only —
//
//	markup.RegisterValues(strhandlers.URI, strhandlers.New())
//
// so writing {{str:Upper .X}} on a Click attribute is a load error, in
// the same voice as writing {{net:Get .Url}} in a Text.
//
// # Why this is not a value converter (yet)
//
// docs/specs/2026-08-10-pipeline-grammar-v2.md reserves pipe stages for
// binding converters — {{.Bytes | human | pad 8}} — under issue #99.
// This pack deliberately does NOT invent that syntax. It occupies the
// call form the grammar already has, which means it also inherits that
// grammar's one real limitation: there is no nesting, so
// {{str:Upper env:Get `USER`}} does not parse. Compose in Go, or wait
// for #99. The overlap between str:Default here and env:Get's optional
// fallback argument is the visible cost of that gap, and it is recorded
// rather than hidden.
//
// # Every function is a computed, and that is the whole design
//
// Each function returns prop.NewComputed over its argument handles. The
// Gets therefore run inside an evaluation, which is what makes them
// subscriptions rather than reads (prop/prop.go's recordRead), so
// {{str:Upper .User}} repaints exactly the components that display it,
// only when .User changes. Nothing in this package tracks anything.
//
// The corollary is the trap the whole repo shares: an argument read
// behind a branch drops out of the dependency set on the frames where
// the branch does not run. Every function below reads all of its
// arguments unconditionally, before deciding anything, and Default is
// the one where that is load-bearing rather than incidental.
package strhandlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// URI is the namespace URI markup declares to reach this provider.
const URI = "gooey.dev/handlers/str"

// The pack's function names.
const (
	NameUpper    = "Upper"
	NameLower    = "Lower"
	NameTrim     = "Trim"
	NameReplace  = "Replace"
	NameJoin     = "Join"
	NameDefault  = "Default"
	NamePad      = "Pad"
	NameTruncate = "Truncate"
)

var allNames = []string{
	NameUpper, NameLower, NameTrim, NameReplace,
	NameJoin, NameDefault, NamePad, NameTruncate,
}

// AllNames lists every function the pack defines — its inventory, per
// docs/specs/2026-08-10-pack-distribution.md. The error text for an
// unknown function derives from this list, so the two cannot drift.
func AllNames() []string { return append([]string{}, allNames...) }

// Ellipsis is what Truncate appends when it cuts. It counts toward the
// requested width, so a truncated result is never wider than asked.
const Ellipsis = "…"

// Provider implements markup.ValueProvider for the str namespace. It
// holds no state and needs no configuration: a pure-function pack has
// nothing to grant beyond its own existence, which is why New takes no
// arguments and why there is no writable variant.
type Provider struct{}

// New builds the provider. Register it with markup.RegisterValues.
func New() *Provider { return &Provider{} }

// NewValue resolves one {{str:…}} expression at load time.
func (p *Provider) NewValue(c *markup.Call) (*prop.Property[string], error) {
	switch c.Fn {
	case NameUpper:
		return unary(c, strings.ToUpper)
	case NameLower:
		return unary(c, strings.ToLower)
	case NameTrim:
		return unary(c, strings.TrimSpace)
	case NameReplace:
		return replace(c)
	case NameJoin:
		return join(c)
	case NameDefault:
		return fallback(c)
	case NamePad:
		return width(c, NamePad, pad)
	case NameTruncate:
		return width(c, NameTruncate, truncate)
	}
	return nil, fmt.Errorf("unknown function %q; str provides: %s", c.Fn, strings.Join(allNames, ", "))
}

func unary(c *markup.Call, f func(string) string) (*prop.Property[string], error) {
	if len(c.Args) != 1 {
		return nil, fmt.Errorf("%s takes 1 argument, got %d", c.Fn, len(c.Args))
	}
	a := c.Args[0]
	return prop.NewComputed(func() string { return f(a.String()) }), nil
}

func replace(c *markup.Call) (*prop.Property[string], error) {
	if len(c.Args) != 3 {
		return nil, fmt.Errorf("Replace takes 3 arguments (the text, the old substring, the new one), got %d", len(c.Args))
	}
	s, old, new_ := c.Args[0], c.Args[1], c.Args[2]
	return prop.NewComputed(func() string {
		return strings.ReplaceAll(s.String(), old.String(), new_.String())
	}), nil
}

func join(c *markup.Call) (*prop.Property[string], error) {
	if len(c.Args) < 2 {
		return nil, fmt.Errorf("Join takes a separator and at least one value, got %d argument(s)", len(c.Args))
	}
	sep, vals := c.Args[0], c.Args[1:]
	return prop.NewComputed(func() string {
		parts := make([]string, len(vals))
		for i, a := range vals {
			parts[i] = a.String()
		}
		return strings.Join(parts, sep.String())
	}), nil
}

func fallback(c *markup.Call) (*prop.Property[string], error) {
	if len(c.Args) != 2 {
		return nil, fmt.Errorf("Default takes 2 arguments (the value and its fallback), got %d", len(c.Args))
	}
	v, fb := c.Args[0], c.Args[1]
	return prop.NewComputed(func() string {
		// Both reads hoisted above the branch: a conditional Get is a
		// dropped dependency, and here it would make a component that
		// is currently showing the fallback deaf to the value coming
		// back — the empty-to-non-empty transition would never repaint.
		s, d := v.String(), fb.String()
		if s == "" {
			return d
		}
		return s
	}), nil
}

// width parses the load-time integer operand shared by Pad and
// Truncate. It must be a backtick literal: a width is layout
// configuration, and the house rule (pipeline grammar v2, "Bound
// operands") is that configuration is resolved when the page loads.
func width(c *markup.Call, fn string, f func(string, int) string) (*prop.Property[string], error) {
	if len(c.Args) != 2 {
		return nil, fmt.Errorf("%s takes 2 arguments (the text and a width), got %d", fn, len(c.Args))
	}
	a, w := c.Args[0], c.Args[1]
	if !w.IsLiteral() {
		return nil, fmt.Errorf("%s width must be a backtick literal, not .%s — a width is load-time configuration, resolved when the page loads", fn, w.Path)
	}
	n, err := strconv.Atoi(strings.TrimSpace(w.String()))
	if err != nil {
		return nil, fmt.Errorf("%s width `%s` is not an integer", fn, w.String())
	}
	if n < 1 {
		return nil, fmt.Errorf("%s width must be at least 1, got %d", fn, n)
	}
	return prop.NewComputed(func() string { return f(a.String(), n) }), nil
}

// pad right-pads to n runes, counting runes rather than bytes because
// the terminal counts cells and a multibyte name would otherwise pad
// short. Text already at or over the width is returned unchanged —
// padding never truncates; Truncate does that.
func pad(s string, n int) string {
	if d := n - len([]rune(s)); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// truncate cuts to n runes, spending the last one on the ellipsis when
// it actually cuts, so the result is never wider than n.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return Ellipsis
	}
	return string(r[:n-1]) + Ellipsis
}
