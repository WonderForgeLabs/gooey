package markup

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// This file is the other half of cond_test.go. That file proves the
// conditional GRAMMAR — parse, type-check, evaluate, stay subscribed —
// against ctx.condHandle called directly. Nothing in it loads a
// document, and it says so: TestConditionalsReachEveryOneWayBoolAttribute
// stops at "the handle has the right static type, so once BindingValue
// dispatches, the attribute binds".
//
// The dispatch is what these tests pin, and the reason they are separate
// assertions rather than one is that there are FOUR sites, not one, and
// three of them are gates that stand in front of BindingValue rather
// than inside it:
//
//	usercontrol.go BindingValue   the choke point — every attribute that
//	                              reaches it needs no further wiring
//	markup.go      applyLayout    Visibility parses a literal vocabulary
//	                              first, so it gates on bindRe
//	usercontrol.go passAttrs      Include/UserControl pass-through
//	markup.go      property.go    a declared <x:Property> surface
//
// A gate that is missed does not fail the same way at each site, which
// is why each test states the failure it would produce. Only one of the
// four is silent, and it is the one worth the most: the pass-through
// stores an unmatched attribute as a literal string, so the child
// receives seventeen characters where it expected a handle.

func condWireCtx() (*Context, *prop.Property[bool], *prop.Property[bool]) {
	a, b := prop.NewSource(true), prop.NewSource(true)
	return &Context{Values: map[string]any{"A": a, "B": b}}, a, b
}

// SITE 1 — Context.BindingValue itself.
//
// <Spinner Enabled> reaches the graph through boundProp[bool], which
// calls BindingValue with no gate of its own. That is the claim being
// checked: an attribute wired the ordinary way needed no conditional
// change at all, and a grammar bolted onto Visibility alone would have
// left this one behind.
//
// Enabled is deliberately not Visibility and not Checked. Not
// Visibility, because that site has its own gate and would prove the
// gate rather than the choke point. Not Checked, because Checked is
// TWO-WAY: a conditional is a computed, prop.Set panics on one, and a
// green test there would be pinning a click-time panic in place.
//
// Mutation: delete the isCondExpr branch at the head of BindingValue and
// this fails at load with `"{{not .A}}" is not a binding expression`.
func TestConditionalBindsAnOrdinaryOneWayBoolAttribute(t *testing.T) {
	ctx, a, _ := condWireCtx()
	ctx.Named = map[string]gooey.Component{}
	src := `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Spinner Name="s" Enabled="{{not .A}}"/>
	</Gooey>`
	buildOne(t, src, ctx)

	s, err := Find[*components.Spinner](ctx, "s")
	if err != nil {
		t.Fatal(err)
	}
	if s.Enabled == nil {
		t.Fatal("Enabled is nil; the attribute did not bind at all")
	}
	if s.Enabled.Get() {
		t.Fatal("not(true) = true")
	}
	// Live, not a snapshot — the handle the component holds is the
	// computed, so the operand's Set is visible through it.
	a.Set(false)
	if !s.Enabled.Get() {
		t.Error("Set on the operand did not reach the component's handle")
	}
}

// SITE 2 — applyLayout's Visibility case, and the one place a
// conditional is worth measuring rather than merely resolving.
//
// The A/B is against the same page driven by a plain bool source. If the
// conditional cost an extra repaint — a second observer, a node the
// Composer armed twice — the counts would diverge. cond_test.go runs
// this comparison on a handle it built itself; this one runs it on the
// handle the LOADER built from the attribute, which is the wiring under
// test.
//
// Mutation: drop `|| isCondExpr(v)` from applyLayout and the load fails
// with an unknown-visibility error — the wrong grammar named, since the
// document wrote a predicate and was told about the word Visible.
func TestConditionalVisibilityLoadsAndCostsWhatABoolHandleCosts(t *testing.T) {
	const shape = `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <VStack>
	    <Text>keep</Text>
	    <Text Visibility=%q>rows</Text>
	  </VStack>
	</Gooey>`

	// Both arms are driven by ONE source, so any divergence is the
	// conditional node. {{not .A}} against a source seeded false is the
	// identity of the plain arm's true.
	a := prop.NewSource(false)
	plain := prop.NewSource(true)

	type arm struct {
		src  string
		ctx  *Context
		set  func(bool)
		name string
	}
	arms := []arm{
		{
			name: "plain",
			src:  `{{.Show}}`,
			ctx:  &Context{Values: map[string]any{"Show": plain}},
			set:  plain.Set,
		},
		{
			name: "conditional",
			src:  `{{not .A}}`,
			ctx:  &Context{Values: map[string]any{"A": a}},
			set:  func(v bool) { a.Set(!v) },
		},
	}

	type result struct {
		hide, show    int
		hidden, shown string
		hideD, showD  []gooey.Rect
	}
	got := map[string]result{}

	for _, arm := range arms {
		src := strings.Replace(shape, "%q", `"`+arm.src+`"`, 1)
		c := gooey.NewComposer(buildOne(t, src, arm.ctx), 12, 3)
		c.Frame()
		if row := cellRow(c.Cells(), 1); row != "rows" {
			t.Fatalf("%s: row 1 = %q, want rows", arm.name, row)
		}
		var r result
		arm.set(false)
		_, r.hide = c.Frame()
		r.hideD = append(r.hideD, c.Damage()...)
		r.hidden = cellRow(c.Cells(), 1)

		arm.set(true)
		_, r.show = c.Frame()
		r.showD = append(r.showD, c.Damage()...)
		r.shown = cellRow(c.Cells(), 1)
		got[arm.name] = r
	}

	p, cnd := got["plain"], got["conditional"]
	// Anchored first: two identical zeros satisfy every comparison
	// below, so the A/B has to prove the plain arm moved at all.
	if p.hide == 0 || p.show == 0 {
		t.Fatalf("the plain arm painted nothing (hide=%d show=%d); the comparison proves nothing", p.hide, p.show)
	}
	if p.hide != cnd.hide || p.show != cnd.show {
		t.Errorf("a conditional attribute does not cost what a bool handle costs:\n plain       hide=%d show=%d\n conditional hide=%d show=%d",
			p.hide, p.show, cnd.hide, cnd.show)
	}
	if !sameRects(p.hideD, cnd.hideD) || !sameRects(p.showD, cnd.showD) {
		t.Errorf("damage differs:\n plain       hide=%v show=%v\n conditional hide=%v show=%v", p.hideD, p.showD, cnd.hideD, cnd.showD)
	}
	if cnd.hidden != "" || cnd.shown != "rows" {
		t.Errorf("conditional arm: hidden=%q shown=%q, want %q and %q", cnd.hidden, cnd.shown, "", "rows")
	}
}

// SITE 3 — the Include/UserControl pass-through, and the only one of the
// four whose omission is SILENT.
//
// passAttrs sorts each attribute into handler / binding / literal. A
// conditional that matches none of the first two lands in the literal
// arm and is stored as its own source text, so the child's context holds
// the string "{{and .A .B}}" under the name Ready. What the child does
// with that depends on the child; here it binds it to Visibility, which
// is one of the few places that would notice.
//
// Mutation: drop `|| isCondExpr(v)` from passAttrs and this fails at
// load — `<Text Visibility="{{.Ready}}"> is string`. That error naming
// `string` is the tell for this whole class: the value crossed the
// boundary intact, as text.
func TestConditionalCrossesTheIncludeBoundaryAsAHandle(t *testing.T) {
	fsys := fstest.MapFS{
		"row.gooey": {Data: []byte(`<Gooey xmlns="wonderforge.io/gooey/2026">
		  <VStack>
		    <Text>always</Text>
		    <Text Visibility="{{.Ready}}">both</Text>
		  </VStack>
		</Gooey>`)},
	}
	ctx, a, b := condWireCtx()
	w, err := loadPage(t, fsys, `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Row Ready="{{and .A .B}}"/>
	</Gooey>`, ctx)
	if err != nil {
		t.Fatal(err)
	}

	c := gooey.NewComposer(w, 12, 3)
	c.Frame()
	if row := cellRow(c.Cells(), 1); row != "both" {
		t.Fatalf("and(true, true) did not show the row: row 1 = %q", row)
	}
	// Either operand hides it, which is what makes this an `and` rather
	// than a pass-through of the first one.
	for _, tc := range []struct {
		name string
		p    *prop.Property[bool]
	}{{"A", a}, {"B", b}} {
		tc.p.Set(false)
		c.Frame()
		if row := cellRow(c.Cells(), 1); row != "" {
			t.Errorf("with .%s false the row is still %q", tc.name, row)
		}
		tc.p.Set(true)
		c.Frame()
	}
}

// SITE 4a — a declared <x:Property> surface accepts a conditional, and
// the declared Type is what checks it.
//
// Mutation: drop `|| isCondExpr(raw)` from resolveDeclared and the
// expression falls to the literal arm, where Type="bool" parses it with
// strconv.ParseBool and fails with `is not a bool` — a message about
// literals, for something that was never one.
func TestConditionalSatisfiesADeclaredBoolProperty(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Ready" Type="bool"/>`,
		`<VStack><Text>always</Text><Text Visibility="{{.Ready}}">gated</Text></VStack>`)
	ctx, a, _ := condWireCtx()
	w, err := loadPage(t, fsys, `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Card Ready="{{and .A .B}}"/>
	</Gooey>`, ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 12, 3)
	c.Frame()
	if row := cellRow(c.Cells(), 1); row != "gated" {
		t.Fatalf("row 1 = %q, want gated", row)
	}
	a.Set(false)
	c.Frame()
	if row := cellRow(c.Cells(), 1); row != "" {
		t.Errorf("row 1 after the operand went false = %q, want erased", row)
	}
}

// SITE 4b — the discrimination for 4a. A conditional is a bool handle
// and nothing else, so a surface that declared some other type must
// reject it BY TYPE.
//
// Without this the positive test above passes just as well against a
// declaration path that accepts anything: "it loaded" is not evidence
// that the declared type did any work. The error text is the assertion
// — it has to name what arrived, which is only possible if the
// expression was compiled before it was checked.
func TestAConditionalOnANonBoolDeclaredPropertyIsATypeError(t *testing.T) {
	fsys := cardFS(`  <x:Property Name="Ready" Type="string"/>`, `<Text>{{.Ready}}</Text>`)
	ctx, _, _ := condWireCtx()
	_, err := loadPage(t, fsys, `<Gooey xmlns="wonderforge.io/gooey/2026">
	  <Card Ready="{{and .A .B}}"/>
	</Gooey>`, ctx)
	if err == nil {
		t.Fatal("a conditional satisfied Type=\"string\"")
	}
	// Not "is not a bool" and not "is not a string": both of those are
	// the LITERAL arm's message, and either one means the expression was
	// never compiled.
	if !strings.Contains(err.Error(), "*prop.Property[bool]") {
		t.Errorf("error does not name the type that arrived: %v", err)
	}
}
