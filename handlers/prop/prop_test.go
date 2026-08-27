package prophandlers_test

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	prophandlers "github.com/WonderForgeLabs/gooey/handlers/prop"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// row is the row as a terminal would read it, trailing blanks trimmed.
// The readback itself is render.RowText, which is where the
// continuation markers get skipped: building the string here cell by
// cell rendered them as literal runes, so no fixture in this package
// could hold a wide glyph and be asserted on.
func row(b *render.Buffer, y int) string {
	return strings.TrimRight(render.RowText(b, y), " ")
}

// grant registers the namespace for one test and revokes it afterwards,
// so no test can leak the write capability into the next one.
func grant(t *testing.T) {
	t.Helper()
	markup.RegisterHandlers(prophandlers.URI, prophandlers.New())
	t.Cleanup(func() { markup.RegisterHandlers(prophandlers.URI, nil) })
}

func load(t *testing.T, body string, vals map[string]any) (gooey.Component, error) {
	t.Helper()
	src := `<Gooey xmlns:prop="` + prophandlers.URI + `">` + body + `</Gooey>`
	if vals == nil {
		vals = map[string]any{}
	}
	return markup.Build([]byte(src), &markup.Context{Values: vals, Dispatcher: gooey.NewDispatcher()})
}

// press builds the page, composes one frame, focuses the first focus
// stop and returns the composer ready to receive Enter.
func press(t *testing.T, body string, vals map[string]any, w, h int) *gooey.Composer {
	t.Helper()
	root, err := load(t, body, vals)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, w, h)
	c.Frame()
	c.Focus().SetFocus(c.Focus().Order()[0])
	return c
}

func enter(c *gooey.Composer) { c.HandleKey(input.Named(input.KeyEnter)) }

// The headline case: a Button changes a property with no app code, and
// the change is on screen after ONE Frame with no Dispatcher drain.
//
// That last clause is the UI-goroutine confirmation, not a convenience.
// A Command that marshalled its Set through the Dispatcher would leave
// this cell unchanged until something called Drain; the value being
// visible here is evidence that the mutation ran inline on the
// event-dispatch path, which is the UI goroutine.
func TestSetWritesAStringPropertyInline(t *testing.T) {
	grant(t)
	mode := prop.NewSource("full")
	c := press(t, `<VStack>
	  <Text>{{.Mode}}</Text>
	  <Button Content="brief" Click="{{prop:Set .Mode `+"`brief`"+`}}"/>
	</VStack>`, map[string]any{"Mode": mode}, 20, 2)

	enter(c)
	c.Frame()
	if got := row(c.Cells(), 0); got != "brief" {
		t.Fatalf("row 0 = %q after prop:Set, want brief", got)
	}
	if mode.Get() != "brief" {
		t.Fatalf("Mode = %q, want brief", mode.Get())
	}
}

func TestToggleFlipsABool(t *testing.T) {
	grant(t)
	rec := prop.NewSource(false)
	c := press(t, `<VStack>
	  <Text>{{.Rec}}</Text>
	  <Button Content="rec" Click="{{prop:Toggle .Rec}}"/>
	</VStack>`, map[string]any{"Rec": rec}, 20, 2)

	enter(c)
	c.Frame()
	if got := row(c.Cells(), 0); got != "true" {
		t.Fatalf("row 0 = %q after one Toggle, want true", got)
	}
	enter(c)
	c.Frame()
	if got := row(c.Cells(), 0); got != "false" {
		t.Fatalf("row 0 = %q after two Toggles, want false", got)
	}
}

func TestAddAdvancesACounter(t *testing.T) {
	grant(t)
	count := prop.NewSource(0)
	c := press(t, `<VStack>
	  <Text>{{.Count}}</Text>
	  <Button Content="+1" Click="{{prop:Add .Count `+"`1`"+`}}"/>
	</VStack>`, map[string]any{"Count": count}, 20, 2)

	enter(c)
	enter(c)
	enter(c)
	c.Frame()
	if got := row(c.Cells(), 0); got != "3" {
		t.Fatalf("row 0 = %q after three Adds, want 3", got)
	}
}

// A negative literal is how a page subtracts; there is no Sub, because
// there does not need to be one.
func TestAddTakesANegativeLiteral(t *testing.T) {
	grant(t)
	count := prop.NewSource(10)
	c := press(t, `<VStack>
	  <Text>{{.Count}}</Text>
	  <Button Content="-2" Click="{{prop:Add .Count `+"`-2`"+`}}"/>
	</VStack>`, map[string]any{"Count": count}, 20, 2)
	enter(c)
	c.Frame()
	if got := row(c.Cells(), 0); got != "8" {
		t.Fatalf("row 0 = %q, want 8", got)
	}
}

// time.Duration rides Add's ~int64 arm, and its literal is a duration
// string rather than a count of nanoseconds — the parse is per target
// type, decided at load.
func TestAddOnADuration(t *testing.T) {
	grant(t)
	d := prop.NewSource(time.Second)
	c := press(t, `<VStack>
	  <Text>{{.Timeout}}</Text>
	  <Button Content="+" Click="{{prop:Add .Timeout `+"`250ms`"+`}}"/>
	</VStack>`, map[string]any{"Timeout": d}, 20, 2)
	enter(c)
	c.Frame()
	if got := row(c.Cells(), 0); got != "1.25s" {
		t.Fatalf("row 0 = %q, want 1.25s", got)
	}
}

func TestSetOnEveryTargetType(t *testing.T) {
	cases := []struct {
		name string
		val  any
		lit  string
		want string
	}{
		{"string", prop.NewSource("a"), "z", "z"},
		{"bool", prop.NewSource(false), "true", "true"},
		{"int", prop.NewSource(0), "42", "42"},
		{"int64", prop.NewSource(int64(0)), "42", "42"},
		{"float64", prop.NewSource(0.0), "1.5", "1.5"},
		{"duration", prop.NewSource(time.Duration(0)), "90s", "1m30s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			grant(t)
			c := press(t, `<VStack>
			  <Text>{{.V}}</Text>
			  <Button Content="set" Click="{{prop:Set .V `+"`"+tc.lit+"`"+`}}"/>
			</VStack>`, map[string]any{"V": tc.val}, 20, 2)
			enter(c)
			c.Frame()
			if got := row(c.Cells(), 0); got != tc.want {
				t.Fatalf("row 0 = %q, want %q", got, tc.want)
			}
		})
	}
}

// A bound operand keeps lvalue semantics: the handle is resolved at
// load, but READ at click, so changing the step between clicks changes
// what the next click adds.
func TestBoundOperandIsReadAtClickTime(t *testing.T) {
	grant(t)
	count, step := prop.NewSource(0), prop.NewSource(2)
	c := press(t, `<VStack>
	  <Text>{{.Count}}</Text>
	  <Button Content="+" Click="{{prop:Add .Count .Step}}"/>
	</VStack>`, map[string]any{"Count": count, "Step": step}, 20, 2)

	enter(c)
	step.Set(10)
	enter(c)
	c.Frame()
	if got := row(c.Cells(), 0); got != "12" {
		t.Fatalf("row 0 = %q, want 12 (2 then 10)", got)
	}
}

// A context may hold a constant where a handle would go, and an operand
// bound to one is as good as a literal — the same tolerance the text
// path has for a constant in a binding.
func TestConstantOperand(t *testing.T) {
	grant(t)
	c := press(t, `<VStack>
	  <Text>{{.Count}}</Text>
	  <Button Content="+" Click="{{prop:Add .Count .Step}}"/>
	</VStack>`, map[string]any{"Count": prop.NewSource(0), "Step": 3}, 20, 2)
	enter(c)
	enter(c)
	c.Frame()
	if got := row(c.Cells(), 0); got != "6" {
		t.Fatalf("row 0 = %q, want 6", got)
	}
}

func TestSetFromABoundHandle(t *testing.T) {
	grant(t)
	mode, other := prop.NewSource("full"), prop.NewSource("brief")
	c := press(t, `<VStack>
	  <Text>{{.Mode}}</Text>
	  <Button Content="copy" Click="{{prop:Set .Mode .Other}}"/>
	</VStack>`, map[string]any{"Mode": mode, "Other": other}, 20, 2)
	enter(c)
	c.Frame()
	if got := row(c.Cells(), 0); got != "brief" {
		t.Fatalf("row 0 = %q, want brief", got)
	}
}

// ── The Set-does-not-compare decision, pinned by damage count ─────────
//
// prop.Set invalidates unconditionally, so without the guard in mutate
// re-selecting the mode a page is ALREADY in repaints every component
// that reads it. These two tests are the same page: one real change, one
// redundant click. Only the painted count can tell them apart — the
// cells are identical in both.

func TestRealSetPaintsOnlyItsReaders(t *testing.T) {
	grant(t)
	mode := prop.NewSource("full")
	c := press(t, `<VStack>
	  <Text>{{.Mode}}</Text>
	  <Text>{{.Mode}}</Text>
	  <Text>static</Text>
	  <Button Content="brief" Click="{{prop:Set .Mode `+"`brief`"+`}}"/>
	</VStack>`, map[string]any{"Mode": mode}, 20, 4)

	enter(c)
	_, painted := c.Frame()
	if painted != 2 {
		t.Fatalf("a real prop:Set painted %d components, want 2 (both readers, and nothing else)", painted)
	}
}

func TestRedundantSetPaintsNothing(t *testing.T) {
	grant(t)
	mode := prop.NewSource("full")
	c := press(t, `<VStack>
	  <Text>{{.Mode}}</Text>
	  <Text>{{.Mode}}</Text>
	  <Text>static</Text>
	  <Button Content="brief" Click="{{prop:Set .Mode `+"`brief`"+`}}"/>
	</VStack>`, map[string]any{"Mode": mode}, 20, 4)

	enter(c) // full → brief, a real change
	c.Frame()

	enter(c) // brief → brief, nothing changed
	_, painted := c.Frame()
	if painted != 0 {
		t.Fatalf("a redundant prop:Set painted %d components, want 0 — mutate must compare before it Sets, because prop.Set does not", painted)
	}
	if got := row(c.Cells(), 0); got != "brief" {
		t.Fatalf("row 0 = %q; the guard must skip the write, not the value", got)
	}
}

// The same guard on Add, where the redundant case is a zero delta.
func TestAddOfZeroPaintsNothing(t *testing.T) {
	grant(t)
	c := press(t, `<VStack>
	  <Text>{{.Count}}</Text>
	  <Button Content="+0" Click="{{prop:Add .Count `+"`0`"+`}}"/>
	</VStack>`, map[string]any{"Count": prop.NewSource(7)}, 20, 2)

	enter(c)
	_, painted := c.Frame()
	if painted != 0 {
		t.Fatalf("prop:Add of 0 painted %d components, want 0", painted)
	}
}

// ── Load-time type checking ──────────────────────────────────────────

// A computed target is the case that matters most, because prop.Set
// PANICS on one: without a load check the page builds clean and takes
// the app down on the first click. It is also the mechanism a host uses
// to publish a property markup may read and may not write, so the same
// test proves both halves.
func TestComputedIsAReadOnlyProjection(t *testing.T) {
	grant(t)
	src := prop.NewSource(3)
	doubled := prop.NewComputed(func() int { return src.Get() * 2 })
	vals := map[string]any{"Src": src, "Doubled": doubled}

	// Readable: the projection renders like any other binding.
	root, err := load(t, "<Text>{{.Doubled}}</Text>", vals)
	if err != nil {
		t.Fatalf("a computed must still be readable: %v", err)
	}
	c := gooey.NewComposer(root, 10, 1)
	c.Frame()
	if got := row(c.Cells(), 0); got != "6" {
		t.Fatalf("row = %q, want 6", got)
	}

	// Not writable, by any of the three operations.
	for _, body := range []string{
		"<Button Content=\"x\" Click=\"{{prop:Set .Doubled `9`}}\"/>",
		"<Button Content=\"x\" Click=\"{{prop:Add .Doubled `1`}}\"/>",
	} {
		if _, err := load(t, body, vals); err == nil {
			t.Fatalf("%s loaded clean; writing a computed must be a LOAD error, not a click-time panic", body)
		} else if !strings.Contains(err.Error(), "COMPUTED") {
			t.Fatalf("error %q does not say the target is computed", err)
		}
	}
}

func TestToggleOnAComputedBoolIsALoadError(t *testing.T) {
	grant(t)
	on := prop.NewSource(true)
	derived := prop.NewComputed(func() bool { return !on.Get() })
	_, err := load(t, "<Button Content=\"x\" Click=\"{{prop:Toggle .Derived}}\"/>",
		map[string]any{"Derived": derived})
	if err == nil {
		t.Fatal("prop:Toggle over a computed bool loaded clean")
	}
	if !strings.Contains(err.Error(), "COMPUTED") {
		t.Fatalf("error %q does not say the target is computed", err)
	}
}

// Every case here is a LOAD error whose message names the rule it broke.
// err != nil would pass for all of them and prove nothing about which
// check fired.
func TestPropLoadErrors(t *testing.T) {
	vals := map[string]any{
		"Mode":  prop.NewSource("full"),
		"Count": prop.NewSource(0),
		"Rec":   prop.NewSource(false),
		"Konst": 5,
		"Flag":  true,
	}
	cases := []struct {
		name string
		body string
		want string
	}{
		{"Toggle over a string names both types",
			"<Button Content=\"x\" Click=\"{{prop:Toggle .Mode}}\"/>",
			"Toggle needs *prop.Property[bool]; .Mode is *prop.Property[string]"},
		{"Add over a bool names what Add takes",
			"<Button Content=\"x\" Click=\"{{prop:Add .Rec `1`}}\"/>",
			"Add needs a numeric property"},
		{"Add over a string names what Add takes",
			"<Button Content=\"x\" Click=\"{{prop:Add .Mode `1`}}\"/>",
			"Add needs a numeric property (*prop.Property over int, int64, float64 or time.Duration); .Mode is *prop.Property[string]"},
		{"Toggle over a bool constant is not a handle",
			"<Button Content=\"x\" Click=\"{{prop:Toggle .Flag}}\"/>",
			"Toggle needs *prop.Property[bool]; .Flag is bool"},
		{"a constant is not a property handle",
			"<Button Content=\"x\" Click=\"{{prop:Set .Konst `9`}}\"/>",
			"Set needs a settable property handle"},
		{"a literal target",
			"<Button Content=\"x\" Click=\"{{prop:Set `Mode` `a`}}\"/>",
			"first argument must be a bound path"},
		{"an unparseable int literal",
			"<Button Content=\"x\" Click=\"{{prop:Set .Count `abc`}}\"/>",
			"`abc` is not a valid int value"},
		{"an unparseable bool literal",
			"<Button Content=\"x\" Click=\"{{prop:Set .Rec `maybe`}}\"/>",
			"`maybe` is not a valid bool value"},
		{"an operand handle of the wrong type",
			"<Button Content=\"x\" Click=\"{{prop:Set .Count .Mode}}\"/>",
			"Set .Count needs a int operand"},
		{"Add operand of the wrong type",
			"<Button Content=\"x\" Click=\"{{prop:Add .Count .Mode}}\"/>",
			"Add .Count needs a int operand"},
		{"Toggle arity",
			"<Button Content=\"x\" Click=\"{{prop:Toggle .Rec `1`}}\"/>",
			"Toggle takes 1 argument(s) (the bool property to flip), got 2"},
		{"Set arity",
			"<Button Content=\"x\" Click=\"{{prop:Set .Mode}}\"/>",
			"Set takes 2 argument(s) (the property and its new value), got 1"},
		{"Add arity",
			"<Button Content=\"x\" Click=\"{{prop:Add .Count `1` `2`}}\"/>",
			"Add takes 2 argument(s) (the property and the amount to add), got 3"},
		{"unknown function",
			"<Button Content=\"x\" Click=\"{{prop:Increment .Count}}\"/>",
			`unknown function "Increment"; prop provides: Set, Toggle, Add`},
		{"a mutation in a value position",
			"<Text>{{prop:Set .Mode `a`}}</Text>",
			"registered as a HANDLER namespace (event-only)"},
		{"an unknown path",
			"<Button Content=\"x\" Click=\"{{prop:Set .Nope `a`}}\"/>",
			"Nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			grant(t)
			_, err := load(t, tc.body, vals)
			if err == nil {
				t.Fatalf("%s loaded clean; expected an error mentioning %q", tc.body, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// Without the grant the page is read-only: the same document that works
// above fails to load, and the message says the host has to register the
// namespace. This is the whole capability story, checkable.
func TestWithoutTheGrantThePageIsReadOnly(t *testing.T) {
	vals := map[string]any{"Mode": prop.NewSource("full")}

	// Reading needs no grant at all.
	if _, err := load(t, "<Text>{{.Mode}}</Text>", vals); err != nil {
		t.Fatalf("reading a property needed a grant: %v", err)
	}
	// Writing does.
	_, err := load(t, "<Button Content=\"x\" Click=\"{{prop:Set .Mode `a`}}\"/>", vals)
	if err == nil {
		t.Fatal("prop:Set loaded with no registered provider")
	}
	if !strings.Contains(err.Error(), "no registered handler provider") {
		t.Fatalf("error %q does not name the missing grant", err)
	}
}

// The pack is push-only by TYPE, not by convention: a host cannot
// register it as a value namespace because it does not implement the
// interface. If someone adds a NewValue method, this fails.
func TestProviderIsNotAValueProvider(t *testing.T) {
	var p any = prophandlers.New()
	if _, ok := p.(markup.HandlerProvider); !ok {
		t.Fatal("Provider does not implement markup.HandlerProvider")
	}
	if _, ok := p.(markup.ValueProvider); ok {
		t.Fatal("Provider implements markup.ValueProvider; prop: is a mutation namespace and must have no value half")
	}
}

func TestAllNamesIsTheInventory(t *testing.T) {
	if got := strings.Join(prophandlers.AllNames(), ","); got != "Set,Toggle,Add" {
		t.Fatalf("AllNames()=%q", got)
	}
	n := prophandlers.AllNames()
	n[0] = "MUTATED"
	if prophandlers.AllNames()[0] != "Set" {
		t.Fatal("AllNames() handed out the package's own slice")
	}
}
