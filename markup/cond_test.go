package markup

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// condCtx builds a context whose surface covers every operand type the
// grammar accepts, so a test can name what it needs without restating
// the map.
func condCtx() (*Context, map[string]any) {
	vals := map[string]any{
		"A":     prop.NewSource(true),
		"B":     prop.NewSource(true),
		"C":     prop.NewSource(true),
		"Auto":  prop.NewSource(true),
		"Name":  prop.NewSource(""),
		"Email": prop.NewSource(""),
		"Other": prop.NewSource("x"),
		"N":     prop.NewSource(3),
		"M":     prop.NewSource(3),
		"Ratio": prop.NewSource(0.5),
		"Vis":   prop.NewSource(gooey.Visible),
	}
	return &Context{Values: vals}, vals
}

func condOf(t *testing.T, ctx *Context, expr string) *prop.Property[bool] {
	t.Helper()
	h, err := ctx.condHandle(expr)
	if err != nil {
		t.Fatalf("condHandle(%q): %v", expr, err)
	}
	return h
}

// ---------------------------------------------------------------------
// THE SUBSCRIPTION PIN
// ---------------------------------------------------------------------

// TestCondAndKeepsEveryOperandSubscribed is the mutation pin for the
// hazard CLAUDE.md names: a Get on the short-circuit side of && drops
// out of the dependency set on the frames where it does not run,
// because prop rebuilds a computed's deps from scratch on every
// evaluation (Get detaches from p.n.deps first, prop/prop.go:87).
//
// The instrument is Evals(), not a rendered cell and not a damage
// count, and that is a deliberate choice rather than a weaker one — see
// TestCondAndSurvivesTheShortCircuitMutationByValue for the measured
// reason no value-level assertion can see this.
//
// Mutate condAnd to `out = out && k.Get()` and this test fails on the
// last line: with .A false the second Get never runs, .B is not a
// dependency, Set does not dirty the node, and the second Get returns
// the cached value without evaluating.
func TestCondAndKeepsEveryOperandSubscribed(t *testing.T) {
	ctx, vals := condCtx()
	a := vals["A"].(*prop.Property[bool])
	b := vals["B"].(*prop.Property[bool])
	a.Set(false)

	h := condOf(t, ctx, "{{and .A .B}}")
	if h.Get() {
		t.Fatal("and(false, true) = true")
	}
	evals := h.Evals()

	// .B is the operand a short-circuiting `and` would have skipped:
	// .A already decided the result.
	b.Set(false)
	if h.Get() {
		t.Fatal("and(false, false) = true")
	}
	if h.Evals() == evals {
		t.Fatalf("Set on .B did not invalidate the conditional (evals stuck at %d): "+
			"the operand dropped out of the dependency set, which is what a short-circuiting `and` does", evals)
	}
}

// TestCondOrKeepsEveryOperandSubscribed is the same pin for `or`, whose
// short-circuit skips the operand after a TRUE one.
func TestCondOrKeepsEveryOperandSubscribed(t *testing.T) {
	ctx, vals := condCtx()
	b := vals["B"].(*prop.Property[bool])

	h := condOf(t, ctx, "{{or .A .B}}") // .A is true
	if !h.Get() {
		t.Fatal("or(true, true) = false")
	}
	evals := h.Evals()

	b.Set(false)
	if !h.Get() {
		t.Fatal("or(true, false) = false")
	}
	if h.Evals() == evals {
		t.Fatalf("Set on .B did not invalidate the conditional (evals stuck at %d)", evals)
	}
}

// TestCondNestedSubexpressionStaysSubscribed pushes the pin one level
// down: with the deciding operand false, a short-circuiting `and` never
// Gets the parenthesised `or` node at all, so BOTH of that node's
// operands go dark. This is the case that would bite a real page,
// because the dropped subscription is two edges away from the operator
// that dropped it.
func TestCondNestedSubexpressionStaysSubscribed(t *testing.T) {
	ctx, vals := condCtx()
	a := vals["A"].(*prop.Property[bool])
	b := vals["B"].(*prop.Property[bool])
	c := vals["C"].(*prop.Property[bool])
	a.Set(false)
	b.Set(false)
	c.Set(false)

	h := condOf(t, ctx, "{{and .A (or .B .C)}}")
	if h.Get() {
		t.Fatal("and(false, or(false,false)) = true")
	}
	evals := h.Evals()

	c.Set(true)
	if h.Get() {
		t.Fatal("and(false, or(false,true)) = true")
	}
	if h.Evals() == evals {
		t.Fatalf("Set on .C, two edges below a false `and` operand, did not invalidate the root (evals stuck at %d)", evals)
	}
}

// TestCondAndSurvivesTheShortCircuitMutationByValue is the honest
// companion to the pins above, and it exists to stop someone deleting
// them as redundant.
//
// It walks the whole truth table under every order in which the sources
// can be Set, asserting the VALUE each time — and it passes under the
// short-circuiting mutation too. That is not a hole in the test; it is
// a property of these operators. and/or are monotone, so the operand
// that decided the result is itself subscribed and wakes the node
// before the skipped operand could ever change the answer.
//
// Which is the finding: for and/or over bools, "the output is right" is
// NOT evidence that the dependency set is right, and neither is a
// damage count downstream of the output. Only the subscription itself
// discriminates. An operator added to this grammar that is not monotone
// in all its operands would not get this reprieve.
func TestCondAndSurvivesTheShortCircuitMutationByValue(t *testing.T) {
	for _, order := range [][]string{{"A", "B"}, {"B", "A"}} {
		for _, want := range []struct{ a, b bool }{
			{true, true}, {true, false}, {false, true}, {false, false},
		} {
			ctx, vals := condCtx()
			a := vals["A"].(*prop.Property[bool])
			b := vals["B"].(*prop.Property[bool])
			and := condOf(t, ctx, "{{and .A .B}}")
			or := condOf(t, ctx, "{{or .A .B}}")
			_, _ = and.Get(), or.Get() // establish the first dependency set

			for _, name := range order {
				if name == "A" {
					a.Set(want.a)
				} else {
					b.Set(want.b)
				}
				// A Get BETWEEN the two Sets is what makes the order
				// matter: it re-evaluates, and therefore re-records
				// dependencies, against a half-updated world.
				_, _ = and.Get(), or.Get()
			}
			if got := and.Get(); got != (want.a && want.b) {
				t.Errorf("order %v: and(%v,%v) = %v", order, want.a, want.b, got)
			}
			if got := or.Get(); got != (want.a || want.b) {
				t.Errorf("order %v: or(%v,%v) = %v", order, want.a, want.b, got)
			}
		}
	}
}

// ---------------------------------------------------------------------
// DAMAGE
// ---------------------------------------------------------------------

// TestConditionalVisibilityCostsExactlyWhatTheHandWrittenPathCosts is
// the damage pin, written as an A/B rather than a bare number.
//
// A bare `painted == 2` would be a number nobody could evaluate: hiding
// a leaf through a BOOL source already costs 2 (the collapse relayouts
// the stack, so the container repaints alongside the leaf), where
// hiding it through a gooey.Visibility handle set to Hidden costs 1.
// Asserting 2 in isolation would therefore pass even if a conditional
// had doubled the cost of something — the arms are what make the claim
// checkable, and the claim is the framework's own: a bound-visibility
// observer's "painted counts and damage rectangles are identical to the
// literal path's" (Composer.armVisibility).
//
// Both arms are driven by the SAME source property, so any difference
// is the conditional node and nothing else.
//
// The page binds {{.Show}} rather than writing {{not .Empty}} inline,
// which keeps this test on the property GRAPH and off the attribute
// parse: it stays honest about the conditional's cost even if the
// dispatch in applyLayout is later restructured.
// TestConditionalVisibilityLoadsAndCostsWhatABoolHandleCosts in
// condwire_test.go is the same A/B with the handle built by the LOADER
// from the attribute, so the parse is covered there. Two arms, one
// claim, and neither test is redundant with the other.
func TestConditionalVisibilityCostsExactlyWhatTheHandWrittenPathCosts(t *testing.T) {
	const src = `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Text>keep</Text>
	    <Text Visibility="{{.Show}}">rows</Text>
	  </VStack>
	</Gooey>`

	// One source, two views of it: the raw handle, and `not (not it)`
	// — a conditional that is semantically the identity, so the two
	// arms must agree cell for cell as well as count for count.
	empty := prop.NewSource(false)
	ctx := &Context{Values: map[string]any{"Empty": empty}}
	plain := prop.NewSource(true)

	arms := map[string]*prop.Property[bool]{
		"plain":       plain,
		"conditional": condOf(t, ctx, "{{not .Empty}}"),
	}

	type result struct {
		hidePainted, showPainted int
		hideDamage, showDamage   []gooey.Rect
		hidden, shown            string
	}
	got := map[string]result{}

	for name, h := range arms {
		c := gooey.NewComposer(buildOne(t, src, &Context{Values: map[string]any{"Show": h}}), 12, 3)
		fired := 0
		c.OnInvalidate(func() { fired++ })
		c.Frame()
		if row := cellRow(c.Cells(), 1); row != "rows" {
			t.Fatalf("%s: row 1 = %q, want rows", name, row)
		}

		set := func(v bool) {
			if name == "plain" {
				plain.Set(v)
			} else {
				empty.Set(!v) // Empty=true ⇒ {{not .Empty}} is false
			}
		}

		set(false)
		if fired == 0 {
			t.Fatalf("%s: the Set did not invalidate the composition", name)
		}
		var r result
		_, r.hidePainted = c.Frame()
		r.hideDamage = append(r.hideDamage, c.Damage()...)
		r.hidden = cellRow(c.Cells(), 1)

		set(true)
		_, r.showPainted = c.Frame()
		r.showDamage = append(r.showDamage, c.Damage()...)
		r.shown = cellRow(c.Cells(), 1)
		got[name] = r
	}

	a, b := got["plain"], got["conditional"]
	if a.hidePainted != b.hidePainted || a.showPainted != b.showPainted {
		t.Errorf("a conditional does not cost what a plain bool handle costs:\n plain       hide=%d show=%d\n conditional hide=%d show=%d",
			a.hidePainted, a.showPainted, b.hidePainted, b.showPainted)
	}
	if !sameRects(a.hideDamage, b.hideDamage) || !sameRects(a.showDamage, b.showDamage) {
		t.Errorf("damage rectangles differ:\n plain       hide=%v show=%v\n conditional hide=%v show=%v",
			a.hideDamage, a.showDamage, b.hideDamage, b.showDamage)
	}
	// Anchored, so the A/B cannot pass by both arms breaking together —
	// two identical zeros would otherwise satisfy every check above.
	if a.hidePainted == 0 || a.showPainted == 0 {
		t.Fatalf("the plain arm painted nothing (hide=%d show=%d); the A/B proves nothing", a.hidePainted, a.showPainted)
	}
	if b.hidden != "" || b.shown != "rows" {
		t.Errorf("conditional arm rows: hidden=%q shown=%q, want \"\" and \"rows\"", b.hidden, b.shown)
	}
	if a.hidden != b.hidden || a.shown != b.shown {
		t.Errorf("cells differ: plain %q/%q, conditional %q/%q", a.hidden, a.shown, b.hidden, b.shown)
	}
}

func sameRects(a, b []gooey.Rect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestConditionalSetOnAnUndecidingOperandCostsOneFrame measures what
// the hoisted form actually costs, so the trade is on the record rather
// than assumed: a Set on an operand that cannot change the result still
// wakes the composition and still runs a frame — it just paints
// nothing, because no paint node's value changed.
func TestConditionalSetOnAnUndecidingOperandCostsOneFrame(t *testing.T) {
	ctx, _ := condCtx()
	a := prop.NewSource(false)
	b := prop.NewSource(true)
	ctx.Values["A"], ctx.Values["B"] = a, b
	ctx.Values["Cond"] = condOf(t, ctx, "{{and .A .B}}")

	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Text>keep</Text>
	    <Text Visibility="{{.Cond}}">rows</Text>
	  </VStack>
	</Gooey>`
	c := gooey.NewComposer(buildOne(t, src, ctx), 12, 3)
	c.Frame()

	// .A is false, so the result is false whatever .B does.
	b.Set(false)
	_, painted := c.Frame()
	if painted != 0 {
		t.Errorf("Set on an operand that cannot change the result painted %d components, want 0", painted)
	}
}

// ---------------------------------------------------------------------
// GRAMMAR
// ---------------------------------------------------------------------

func TestCondOperators(t *testing.T) {
	// Each case sets the sources it names, then asserts the predicate.
	for _, tc := range []struct {
		expr string
		set  map[string]any
		want bool
	}{
		{"{{not .Auto}}", map[string]any{"Auto": true}, false},
		{"{{not .Auto}}", map[string]any{"Auto": false}, true},
		{"{{and .A .B}}", map[string]any{"A": true, "B": false}, false},
		{"{{and .A .B .C}}", map[string]any{"A": true, "B": true, "C": true}, true},
		{"{{and .A .B .C}}", map[string]any{"A": true, "B": true, "C": false}, false},
		{"{{or .A .B .C}}", map[string]any{"A": false, "B": false, "C": true}, true},
		{"{{or .A .B .C}}", map[string]any{"A": false, "B": false, "C": false}, false},

		// eq/ne against a literal, one per accepted operand type.
		{"{{eq .Name `x`}}", map[string]any{"Name": "x"}, true},
		{"{{ne .Name `x`}}", map[string]any{"Name": "x"}, false},
		{"{{eq .Name ``}}", map[string]any{"Name": ""}, true},
		{"{{eq .N `3`}}", map[string]any{"N": 3}, true},
		{"{{ne .N `3`}}", map[string]any{"N": 4}, true},
		{"{{eq .A `true`}}", map[string]any{"A": true}, true},
		{"{{eq .A `false`}}", map[string]any{"A": true}, false},
		// The literal may be written first; eq/ne are symmetric.
		{"{{eq `x` .Name}}", map[string]any{"Name": "x"}, true},

		// eq/ne between two handles of the same type.
		{"{{eq .N .M}}", map[string]any{"N": 3, "M": 3}, true},
		{"{{ne .N .M}}", map[string]any{"N": 3, "M": 4}, true},
		{"{{eq .Name .Other}}", map[string]any{"Name": "a", "Other": "a"}, true},
		{"{{eq .A .B}}", map[string]any{"A": true, "B": false}, false},

		// Nesting, only through parentheses.
		{"{{and .A (or .B .C)}}", map[string]any{"A": true, "B": false, "C": true}, true},
		{"{{and .A (or .B .C)}}", map[string]any{"A": true, "B": false, "C": false}, false},
		{"{{or (and .A .B) (and .B .C)}}", map[string]any{"A": false, "B": true, "C": true}, true},
		{"{{not (and .A .B)}}", map[string]any{"A": true, "B": false}, true},
		{"{{and (not .A) (not .B)}}", map[string]any{"A": false, "B": false}, true},

		// cmd/toolkit's real shape: "both fields are still blank".
		{"{{and (eq .Name ``) (eq .Email ``)}}",
			map[string]any{"Name": "", "Email": ""}, true},
		{"{{and (eq .Name ``) (eq .Email ``)}}",
			map[string]any{"Name": "", "Email": "e"}, false},

		// Whitespace is not significant.
		{"{{  and   .A    .B  }}", map[string]any{"A": true, "B": true}, true},
		{"  {{and .A (or .B .C)}}  ", map[string]any{"A": true, "B": true}, true},
	} {
		t.Run(tc.expr+"/"+describeSet(tc.set), func(t *testing.T) {
			ctx, vals := condCtx()
			for k, v := range tc.set {
				switch x := v.(type) {
				case bool:
					vals[k].(*prop.Property[bool]).Set(x)
				case int:
					vals[k].(*prop.Property[int]).Set(x)
				case string:
					vals[k].(*prop.Property[string]).Set(x)
				}
			}
			if got := condOf(t, ctx, tc.expr).Get(); got != tc.want {
				t.Errorf("%s with %v = %v, want %v", tc.expr, tc.set, got, tc.want)
			}
		})
	}
}

// describeSet names a subtest by the world it sets up, in a stable
// order — map iteration order would otherwise rename subtests run to
// run and make a failure hard to re-run by name.
func describeSet(m map[string]any) string {
	var parts []string
	for _, k := range []string{"A", "B", "C", "Auto", "Name", "Email", "Other", "N", "M"} {
		if v, ok := m[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}

// A conditional stays live: it is a computed over the operand handles,
// so a later Set is visible through it without rebuilding anything.
func TestCondHandleIsLiveNotASnapshot(t *testing.T) {
	ctx, vals := condCtx()
	auto := vals["Auto"].(*prop.Property[bool])
	h := condOf(t, ctx, "{{not .Auto}}")
	if h.Get() {
		t.Fatal("not(true) = true")
	}
	auto.Set(false)
	if !h.Get() {
		t.Fatal("not(false) = false — the conditional snapshotted its operand instead of reading it")
	}
}

// Two elements binding the same expression get two handles. This is not
// an accident to be optimized away later: prop.OnInvalidate holds ONE
// hook, so a shared handle would silently drop the first observer armed
// on it.
func TestCondHandleIsNotMemoized(t *testing.T) {
	ctx, _ := condCtx()
	// Named, rather than comparing the two calls inline: staticcheck
	// reads identical operands either side of == as the mistake SA4000
	// usually is, and the point here is that two identical CALLS must
	// not return one pointer.
	first := condOf(t, ctx, "{{not .Auto}}")
	second := condOf(t, ctx, "{{not .Auto}}")
	if first == second {
		t.Error("condHandle returned the same computed twice; prop.OnInvalidate has a single slot, so observers would collide")
	}
}

// ---------------------------------------------------------------------
// DISJOINTNESS
// ---------------------------------------------------------------------

// isCondExpr decides, for every one-way bool attribute at once, whether
// Context.BindingValue takes the conditional path. Getting its boundary
// wrong does not produce a conditional error — it steals an attribute
// from a grammar that already worked.
func TestIsCondExprIsDisjointFromTheOtherTwoGrammars(t *testing.T) {
	for _, tc := range []struct {
		attr string
		want bool
	}{
		{"{{not .Auto}}", true},
		{"{{and .A .B}}", true},
		{"{{or .A .B}}", true},
		{"{{eq .N `3`}}", true},
		{"{{ne .N .M}}", true},
		{"{{and (eq .Name ``) (eq .Email ``)}}", true},
		// An unknown operator IS a conditional, so it gets the
		// conditional error rather than "not a binding expression".
		{"{{xor .A .B}}", true},

		// A value binding starts with a dot.
		{"{{.Auto}}", false},
		{"{{ .Some.Nested.Path }}", false},
		// A handler expression has a colon after its prefix — including
		// the spaced spelling, which the operator regexp alone would
		// read as the operator "net".
		{"{{net:Get .Url | into .Body}}", false},
		{"{{net : Get .Url | into .Body}}", false},
		// A lone bare word keeps the error it already had.
		{"{{ nonsense }}", false},
		{"{{nope}}", false},
		// Not an expression at all.
		{"", false},
		{"Collapsed", false},
		{"OnSave", false},
		{"4,2", false},
		{"#ff8800", false},
		{"prefix {{and .A .B}} suffix", false},
	} {
		if got := isCondExpr(tc.attr); got != tc.want {
			t.Errorf("isCondExpr(%q) = %v, want %v", tc.attr, got, tc.want)
		}
	}
}

// TestConditionalsReachEveryOneWayBoolAttribute records the wiring this
// grammar needs outside its own file, and checks the half that is
// checkable without loading a document.
//
// Context.BindingValue is the single choke point for every one-way
// attribute — boundProp, Attr[T] and bindVisibility all funnel through
// it — so one dispatch at its head is what makes conditionals work
// everywhere at once. FOUR sites carry that dispatch, because three
// others gate on bindRe BEFORE reaching BindingValue and would
// otherwise never arrive: applyLayout's Visibility case, passAttrs, and
// resolveDeclared. condwire_test.go covers all four end to end, with a
// mutation per site.
//
// This test is the unit half of that: every expression a bool attribute
// would carry is recognized by isCondExpr AND compiles against a real
// context. It is what tells you, when a condwire test goes red, whether
// the grammar broke or only the wiring did.
func TestConditionalsReachEveryOneWayBoolAttribute(t *testing.T) {
	ctx, _ := condCtx()
	for _, expr := range []string{
		"{{not .Auto}}",                        // cmd/state's Visibility
		"{{and (eq .Name ``) (eq .Email ``)}}", // cmd/toolkit's IsEnabled
		"{{or .A (and .B (not .C))}}",          // arbitrary nesting
	} {
		if !isCondExpr(expr) {
			t.Fatalf("%q is not recognized as a conditional", expr)
		}
		// The static type is the assertion: bindVisibility's switch and
		// boundProp[bool]'s assertion both accept exactly this, so once
		// BindingValue dispatches here the attribute binds.
		var h *prop.Property[bool]
		h, err := ctx.condHandle(expr)
		if err != nil {
			t.Fatalf("condHandle(%q): %v", expr, err)
		}
		if h == nil {
			t.Fatalf("condHandle(%q) returned a nil handle", expr)
		}
	}
}

// ---------------------------------------------------------------------
// LOAD-TIME ERRORS
// ---------------------------------------------------------------------

// Everything resolvable fails at LOAD. A conditional that names a
// missing path, a wrong operand type, a bad arity or an unknown
// function must not survive to become a surprise at paint time.
func TestCondLoadErrors(t *testing.T) {
	for _, tc := range []struct{ expr, want string }{
		// arity
		{"{{not .A .B}}", "exactly 1 operand"},
		{"{{and .A}}", "at least 2 operands, got 1"},
		{"{{or .A}}", "at least 2 operands, got 1"},
		{"{{eq .N}}", "exactly 2 operands, got 1"},
		{"{{eq .N .M .N}}", "exactly 2 operands, got 3"},
		{"{{ne .N}}", "exactly 2 operands, got 1"},

		// unknown function
		{"{{xor .A .B}}", `unknown conditional function "xor"`},
		{"{{gt .N `3`}}", `unknown conditional function "gt"`},
		{"{{if .A .B}}", `unknown conditional function "if"`},

		// operand types
		{"{{not .Name}}", "and/or/not take a *prop.Property[bool]"},
		{"{{and .A .N}}", "and/or/not take a *prop.Property[bool]"},
		{"{{and .A `true`}}", "a literal is only an eq/ne operand"},
		{"{{eq .Ratio `0.5`}}", "eq/ne compare *prop.Property[bool], [int] or [string]"},
		{"{{eq .Vis `Visible`}}", "eq/ne compare *prop.Property[bool], [int] or [string]"},
		{"{{eq .N .Name}}", "eq/ne compare two handles of the SAME type"},
		{"{{eq .A .Name}}", "eq/ne compare two handles of the SAME type"},
		{"{{eq .N `three`}}", "is not a int"},
		// A bool literal is spelled exactly one way. These four are
		// what strconv.ParseBool would have accepted, and each is a
		// second spelling of a value the document can already write —
		// which is how one predicate comes to read differently in two
		// files. A text binding renders a bool as "true"/"false"
		// (textSource); this is the round-trip of that and nothing
		// wider.
		{"{{eq .A `yes`}}", "want `true` or `false`"},
		{"{{eq .A `1`}}", "want `true` or `false`"},
		{"{{eq .A `TRUE`}}", "want `true` or `false`"},
		{"{{eq .A `t`}}", "want `true` or `false`"},
		{"{{eq .A `False`}}", "want `true` or `false`"},
		{"{{eq `a` `b`}}", "at least one operand must be a .Path"},
		{"{{eq (and .A .B) .A}}", "compares .Paths and `literals`"},

		// unresolved paths, at every depth
		{"{{not .Missing}}", `"Missing" not found in context`},
		{"{{and .A .Missing}}", `"Missing" not found in context`},
		{"{{and .A (or .B .Missing)}}", `"Missing" not found in context`},
		{"{{eq .Missing `x`}}", `"Missing" not found in context`},
		{"{{eq .Name .Missing}}", `"Missing" not found in context`},

		// structure
		{"{{and .A (or .B .C}}", "unclosed ("},
		{"{{and .A .B)}}", "unexpected )"},
		{"{{and .A) .B}}", "unexpected )"},
		{"{{not .A .B .C}}", "exactly 1 operand"},
		{"{{and (not .A) extra}}", "unexpected word extra"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			ctx, _ := condCtx()
			h, err := ctx.condHandle(tc.expr)
			if err == nil {
				t.Fatalf("condHandle(%q) built %v; want a load error", tc.expr, h)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("condHandle(%q) = %v\nwant it to mention %q", tc.expr, err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.expr) && !strings.Contains(err.Error(), strings.TrimSpace(tc.expr)) {
				t.Errorf("condHandle(%q) = %v; the message does not quote the expression", tc.expr, err)
			}
		})
	}
}

// An unresolved path inside a conditional stays an *UnresolvedError.
// The control plane tells "this document reached past its grant" apart
// from "this document has a typo" with errors.As, and a conditional
// that reformatted the error would silently move every such case into
// the second bucket.
func TestCondUnresolvedPathStaysTyped(t *testing.T) {
	ctx, _ := condCtx()
	_, err := ctx.condHandle("{{and .A (or .B .Missing)}}")
	if err == nil {
		t.Fatal("want an error")
	}
	var ue *UnresolvedError
	if !errors.As(err, &ue) {
		t.Fatalf("error %v is not an *UnresolvedError", err)
	}
	if ue.Path != "Missing" {
		t.Errorf("UnresolvedError.Path = %q, want Missing", ue.Path)
	}
}

// A conditional resolves nested paths the same way a value binding
// does, through resolve() — so a control's context of contexts works
// without a second path syntax.
func TestCondResolvesNestedPaths(t *testing.T) {
	inner := prop.NewSource(false)
	ctx := &Context{Values: map[string]any{
		"Form": map[string]any{"Dirty": inner},
	}}
	h := condOf(t, ctx, "{{not .Form.Dirty}}")
	if !h.Get() {
		t.Fatal("not(.Form.Dirty=false) = false")
	}
	inner.Set(true)
	if h.Get() {
		t.Fatal("nested path did not stay live")
	}
}

// The handler grammar keeps its own errors: a paren is now a token
// rather than part of a bare word, so the message names parentheses
// instead of blaming a word the author did not write.
func TestHandlerExprRejectsParensByName(t *testing.T) {
	_, err := parseHandlerExpr("{{t:Run (.A) | into .Out}}")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "parentheses") {
		t.Errorf("err = %v; want it to name parentheses", err)
	}
}
