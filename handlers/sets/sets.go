// Package sethandlers is the set namespace: composition for the
// name-set attributes markup has grown, of which <Frozen Allow> is the
// first.
//
//	<Gooey xmlns:sets="gooey.dev/handlers/sets">
//	  <Frozen Allow="{{sets:Concat `Hover` `Nav` .Selected}}">
//
// Every function here is a value: it has no effect, it returns a string,
// and it is registered on the pull side only —
//
//	markup.RegisterValues(sethandlers.URI, sethandlers.New())
//
// so writing {{sets:Concat …}} on a Click attribute is a load error, in
// the same voice as writing {{net:Get .Url}} in a Text.
//
// # A set is TEXT, and that is the design
//
// A "set" here is a string of names separated by spaces or commas.
// Nothing in this package knows what a name means. That is what lets it
// stay a value provider — markup.ValueProvider hands back a
// *prop.Property[string], the same handle {{.Path}} produces — and it is
// what lets a set be composed out of literals and bound paths in one
// interpolation with no new binding machinery, no new Kind, and no
// viewmodel holding a framework type.
//
// The cost is that a typo is not a load error here; it is a load error
// where the set is CONSUMED (markup's <Frozen> checks a literal Allow) or
// a fail-closed value at runtime. Pushing validation to the consumer is
// deliberate: this pack is generic, and a pack that validated against one
// consumer's vocabulary could never serve a second one.
//
// # Every function is a computed, and that is the whole design
//
// Each function returns prop.NewComputed over its argument handles. The
// Gets therefore run inside an evaluation, which is what makes them
// subscriptions rather than reads (prop/prop.go's recordRead). For
// <Frozen Allow> that chain matters end to end: the Composer's frozen
// observer calls FrozenAllow(), which Gets the attribute's handle, which
// is this computed, which Gets its arguments — so a Set on .Selected
// re-routes input in the frame it happens, with nothing subscribing to
// anything by name.
//
// The corollary is the trap the whole repo shares: an argument read
// behind a branch drops out of the dependency set on the frames where the
// branch does not run. Every function below reads all of its arguments
// unconditionally, before deciding anything, and When is the one where
// that is load-bearing rather than incidental.
//
// # No nesting, same as everywhere
//
// The pipeline grammar has no nesting, so {{sets:Concat sets:Group `Text`
// .X}} does not parse. Group therefore exists as a one-call convenience
// rather than a composable stage, and the ordinary way to spell a group
// is to write its name — `Text` is already a name <Frozen Allow>
// understands. See docs/specs/2026-08-10-pipeline-grammar-v2.md and
// issue #99.
package sethandlers

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// URI is the namespace URI markup declares to reach this provider.
const URI = "gooey.dev/handlers/sets"

// The pack's function names.
const (
	NameConcat  = "Concat"
	NameWithout = "Without"
	NameWhen    = "When"
	NameGroup   = "Group"
	NameHas     = "Has"
)

var allNames = []string{NameConcat, NameWithout, NameWhen, NameGroup, NameHas}

// AllNames lists every function the pack defines — its inventory, per
// docs/specs/2026-08-10-pack-distribution.md. The error text for an
// unknown function derives from this list, so the two cannot drift.
func AllNames() []string { return append([]string{}, allNames...) }

// Truthy values When accepts, beyond a non-empty set. A bound bool
// renders as "true"/"false" through Arg.String, and "false" is not empty,
// so without this a When on a bool would be permanently on — the bug this
// list exists to prevent.
var falsey = map[string]bool{"": true, "false": true, "0": true, "off": true, "no": true}

// Provider implements markup.ValueProvider for the sets namespace. It
// holds no state and needs no configuration: a pure-function pack has
// nothing to grant beyond its own existence, which is why New takes no
// arguments and why there is no writable variant.
type Provider struct{}

// New builds the provider. Register it with markup.RegisterValues.
func New() *Provider { return &Provider{} }

// NewValue resolves one {{sets:…}} expression at load time.
func (p *Provider) NewValue(c *markup.Call) (*prop.Property[string], error) {
	switch c.Fn {
	case NameConcat:
		return concat(c)
	case NameWithout:
		return without(c)
	case NameWhen:
		return when(c)
	case NameGroup:
		return group(c)
	case NameHas:
		return has(c)
	}
	return nil, fmt.Errorf("unknown function %q; sets provides: %s", c.Fn, strings.Join(allNames, ", "))
}

// concat is the union, and it is the one function the Allow vocabulary
// actually needs: composition of a name set is union, because naming more
// categories permits strictly more.
func concat(c *markup.Call) (*prop.Property[string], error) {
	if len(c.Args) < 1 {
		return nil, fmt.Errorf("Concat takes at least one set, got %d", len(c.Args))
	}
	args := c.Args
	return prop.NewComputed(func() string {
		var all []string
		for _, a := range args {
			all = append(all, expandSet(a.String())...)
		}
		return Canonical(all)
	}), nil
}

// without is the difference: everything in the first set that none of the
// later ones names. It is what expresses "the usual permissions, minus
// what this mode takes away" without the page restating the base set.
func without(c *markup.Call) (*prop.Property[string], error) {
	if len(c.Args) < 2 {
		return nil, fmt.Errorf("Without takes a set and at least one set to remove, got %d argument(s)", len(c.Args))
	}
	base, rest := c.Args[0], c.Args[1:]
	return prop.NewComputed(func() string {
		drop := map[string]bool{}
		for _, a := range rest {
			for _, n := range expandSet(a.String()) {
				drop[n] = true
			}
		}
		var out []string
		for _, n := range expandSet(base.String()) {
			if !drop[n] {
				out = append(out, n)
			}
		}
		return Canonical(out)
	}), nil
}

// when is the conditional: the union of the sets, or nothing, according
// to a condition. It is what a design surface writes —
// Allow="{{sets:When .DesignMode `Pointer` `Hover`}}" — and it is the
// reason a bare {{.Flag}} is not enough: an attribute holding "true"
// would be an unknown category name, not a permission.
func when(c *markup.Call) (*prop.Property[string], error) {
	if len(c.Args) < 2 {
		return nil, fmt.Errorf("When takes a condition and at least one set, got %d argument(s)", len(c.Args))
	}
	cond, rest := c.Args[0], c.Args[1:]
	return prop.NewComputed(func() string {
		// Both sides read BEFORE the branch. A Get behind the condition
		// would drop out of the dependency set on every frame the
		// condition was false, so turning design mode on would show the
		// set as of whenever it was last evaluated — and turning it on is
		// exactly the frame the set has to be right.
		on := !falsey[strings.TrimSpace(strings.ToLower(cond.String()))]
		var all []string
		for _, a := range rest {
			all = append(all, expandSet(a.String())...)
		}
		if !on {
			return ""
		}
		return Canonical(all)
	}), nil
}

// group expands one group name — the gooey.Allow vocabulary's unions —
// into the primitive names it stands for.
//
// The expansion comes from gooey.AllowGroups() rather than from a table
// here, which is the one place this generic pack knows anything about a
// consumer. A copy of the expansion would be a second statement of the
// constants in gooey/allow.go, and the second copy is the one that goes
// stale — the failure being a page silently granted the wrong
// permissions, which is the worst kind this repo has.
func group(c *markup.Call) (*prop.Property[string], error) {
	if len(c.Args) != 1 {
		return nil, fmt.Errorf("Group takes 1 argument (the group name), got %d", len(c.Args))
	}
	a := c.Args[0]
	if a.IsLiteral() {
		// A literal group name is resolvable at load time, so resolve it
		// there: an unknown one is a load error naming the vocabulary,
		// which is the bargain markup makes everywhere else.
		if _, ok := gooey.AllowGroups()[a.String()]; !ok {
			return nil, fmt.Errorf("unknown group %q; the groups are: %s",
				a.String(), strings.Join(GroupNames(), ", "))
		}
	}
	return prop.NewComputed(func() string {
		return Canonical(gooey.AllowGroups()[a.String()])
	}), nil
}

// has is set membership, rendered as "true"/"false" so it can drive a
// Visibility or a bound bool the way any other text binding does.
func has(c *markup.Call) (*prop.Property[string], error) {
	if len(c.Args) != 2 {
		return nil, fmt.Errorf("Has takes 2 arguments (the set and a name), got %d", len(c.Args))
	}
	set, name := c.Args[0], c.Args[1]
	return prop.NewComputed(func() string {
		s, n := expandSet(set.String()), strings.TrimSpace(name.String())
		for _, e := range s {
			if e == n {
				return "true"
			}
		}
		return "false"
	}), nil
}

// GroupNames lists the group names Group accepts, sorted. Derived from
// gooey.AllowGroups for the same reason group is.
func GroupNames() []string {
	g := gooey.AllowGroups()
	out := make([]string, 0, len(g))
	for n := range g {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Split reads a set: names separated by spaces or commas, in any order.
// Exported because the encoding is the pack's public contract — a
// consumer parsing an attribute this pack produced should split it the
// same way, and gooey.ParseAllow does.
func Split(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
}

// expandSet reads a set the way the ALGEBRA must see it: Split, then
// every group name replaced by the names it stands for.
//
// # Why the algebra cannot use Split directly
//
// A group name is one token that means several. Split is the ENCODING
// contract — it mirrors gooey.ParseAllow and must keep doing so — but
// arithmetic over unexpanded tokens is arithmetic over the wrong set,
// and the failure is silent and open:
//
//	Without("All", "Start")  ->  Split("All") = ["All"]
//	                         ->  "Start" != "All", so nothing is removed
//	                         ->  "All"  ->  ParseAllow -> AllowAll ⊇ AllowStart
//
// A page that asked for "everything except Start" GRANTED Start, and
// Start is the category with a child-process argument behind it. That
// is the exact failure handlers/sets/README.md describes and that this
// PR closed for `sets:Group "All"` by making AllowGroups expand — the
// bare literal, which never passes through AllowGroups, was the half
// left open. TestTheAlgebraHoldsOverEveryGroup iterates AllowGroups()
// and so cannot see it.
//
// Expanding here rather than in Split is what keeps both true: a
// consumer splitting an attribute this pack produced still splits it
// the way ParseAllow does, and the operators still compute over the
// set the names actually denote.
//
// A group whose expansion is itself (None, or a future group of
// nameless bits — see gooey.AllowGroups) maps to itself, so this
// terminates and leaves those tokens alone. Found in review of #425.
func expandSet(s string) []string {
	names := Split(s)
	groups := gooey.AllowGroups()
	out := make([]string, 0, len(names))
	for _, n := range names {
		if g, ok := groups[n]; ok {
			out = append(out, g...)
			continue
		}
		out = append(out, n)
	}
	return out
}

// Canonical renders a set: deduplicated, ordered, single-space
// separated.
//
// One spelling per set matters more here than it looks. prop.Set does not
// compare values, but Composer's frozen sweep DOES compare the parsed
// Allow — and an unstable spelling would parse to the same Allow anyway,
// so the sweep would stay quiet. What an unstable spelling would actually
// break is the cache in components.Frozen, which compares TEXT to avoid
// re-parsing on every routed event: a set that reordered itself per
// evaluation would miss that cache on every pointer motion.
//
// Ordering is gooey.SortAllowNames — the vocabulary's own order, with
// names it does not know sorted after, alphabetically, so a set of names
// from some other consumer still canonicalizes rather than being lost.
func Canonical(names []string) string {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	gooey.SortAllowNames(out)
	return strings.Join(out, " ")
}
