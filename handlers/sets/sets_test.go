package sethandlers_test

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	sethandlers "github.com/WonderForgeLabs/gooey/handlers/sets"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func row(b *render.Buffer, y int) string {
	var sb strings.Builder
	for x := 0; x < b.W; x++ {
		sb.WriteRune(b.At(x, y).Rune)
	}
	return sb.String()
}

func grant(t *testing.T) {
	t.Helper()
	markup.RegisterValues(sethandlers.URI, sethandlers.New())
	t.Cleanup(func() { markup.RegisterValues(sethandlers.URI, nil) })
}

func load(t *testing.T, body string, vals map[string]any) (gooey.Component, error) {
	t.Helper()
	src := `<Gooey xmlns:sets="` + sethandlers.URI + `">` + body + `</Gooey>`
	if vals == nil {
		vals = map[string]any{}
	}
	return markup.Build([]byte(src), &markup.Context{Values: vals, Dispatcher: gooey.NewDispatcher()})
}

// paint builds a one-line page and returns what landed on the terminal,
// right-trimmed. Asserting on cells rather than on the property is
// deliberate: it proves the handle reached a paint node.
func paint(t *testing.T, body string, vals map[string]any, w int) string {
	t.Helper()
	c, err := load(t, "<Text>"+body+"</Text>", vals)
	if err != nil {
		t.Fatal(err)
	}
	comp := gooey.NewComposer(c, w, 1)
	comp.Frame()
	return strings.TrimRight(row(comp.Cells(), 0), " ")
}

func TestFunctions(t *testing.T) {
	grant(t)
	vals := map[string]any{
		"Sel":   prop.NewSource("Hover"),
		"Empty": prop.NewSource(""),
		"On":    prop.NewSource(true),
		"Off":   prop.NewSource(false),
		"Base":  prop.NewSource("Focus Pointer Start"),
	}
	cases := []struct{ name, body, want string }{
		// Union, in the vocabulary's own order rather than the order the
		// arguments were written — one spelling per set.
		{"Concat", "{{sets:Concat `Pointer` .Sel}}", "Pointer Hover"},
		{"Concat dedups", "{{sets:Concat `Hover` `Hover` .Sel}}", "Hover"},
		{"Concat with an empty set", "{{sets:Concat .Empty `Nav`}}", "Nav"},
		{"Without", "{{sets:Without .Base `Start`}}", "Focus Pointer"},
		{"Without everything", "{{sets:Without .Base .Base}}", ""},
		{"When on", "{{sets:When .On `Pointer` `Hover`}}", "Pointer Hover"},
		{"When off", "{{sets:When .Off `Pointer` `Hover`}}", ""},
		{"Group", "{{sets:Group `Mouse`}}", "Pointer Hover"},
		{"Has yes", "{{sets:Has .Base `Pointer`}}", "true"},
		{"Has no", "{{sets:Has .Base `Hover`}}", "false"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := paint(t, c.body, vals, 60); got != c.want {
				t.Errorf("%s = %q, want %q", c.body, got, c.want)
			}
		})
	}
}

// TestABareGroupLiteralIsExpandedByTheAlgebra is the hole
// TestTheAlgebraHoldsOverEveryGroup could not see, and it is a FAIL-OPEN
// one.
//
// That test iterates gooey.AllowGroups(), so it exercises the group
// through sets:Group — the spelling this PR fixed. But `All` is also a
// name <Frozen Allow> understands, so the natural way to write
// "everything except Start" is the bare literal, and that path never
// touched AllowGroups:
//
//	Split("All") = ["All"]  ->  "Start" removes nothing  ->  "All"
//	ParseAllow("All") = AllowAll, which CONTAINS AllowStart
//
// The page asked for everything-except-Start and got Start, which is the
// category with a child-process argument behind it. handlers/sets's own
// README describes exactly this failure; the bare spelling was the half
// still open. Found in review of #425.
func TestABareGroupLiteralIsExpandedByTheAlgebra(t *testing.T) {
	grant(t)
	vals := map[string]any{"Unused": prop.NewSource("")}

	got := paint(t, "{{sets:Without `All` `Start`}}", vals, 120)
	set, err := gooey.ParseAllow(got)
	if err != nil {
		t.Fatalf("the difference produced %q, which does not parse: %v", got, err)
	}
	start, err := gooey.ParseAllow("Start")
	if err != nil {
		t.Fatalf("Start does not parse: %v", err)
	}
	if set.Has(start) {
		t.Errorf("sets:Without `All` `Start` = %q, which still GRANTS Start. "+
			"The bare group literal was not expanded, so the difference "+
			"compared \"Start\" against the single token \"All\", removed "+
			"nothing, and evaluated back to the whole set", got)
	}
	if got == "All" {
		t.Errorf("the difference came back as the opaque token %q — the "+
			"algebra never saw inside it", got)
	}

	// AND THE UNION STILL MEANS THE SAME SET, or the expansion above
	// bought correctness in the difference by breaking every other use.
	if u := paint(t, "{{sets:Concat `All`}}", vals, 120); u == "" {
		t.Error("sets:Concat `All` came back empty")
	} else if a, err := gooey.ParseAllow(u); err != nil {
		t.Errorf("sets:Concat `All` = %q, which does not parse: %v", u, err)
	} else if !a.Has(start) {
		t.Errorf("sets:Concat `All` = %q, which no longer contains Start — "+
			"expanding the literal must not shrink the set it denotes", u)
	}
}

// TestWhenIsFalseForAFalseBool is the row the truthiness rule exists for.
// A bound bool renders as "false" through Arg.String, and "false" is not
// empty — a rule of "non-empty means on" would have made every When on a
// bool permanently on, which is the failure mode where a design surface
// is live when it should be a picture.
func TestWhenIsFalseForAFalseBool(t *testing.T) {
	grant(t)
	vals := map[string]any{"Flag": prop.NewSource(false)}
	if got := paint(t, "{{sets:When .Flag `Pointer`}}", vals, 40); got != "" {
		t.Errorf("When on a false bool = %q, want the empty set", got)
	}
	vals["Flag"].(*prop.Property[bool]).Set(true)
	if got := paint(t, "{{sets:When .Flag `Pointer`}}", vals, 40); got != "Pointer" {
		t.Errorf("When on a true bool = %q, want Pointer: the arm above proves nothing "+
			"without this", got)
	}
}

// TestASetIsRecomputedWhenAnArgumentChanges is the subscription claim,
// and it is asserted through the CELLS: the computed's Gets run inside an
// evaluation, so a Set on an argument repaints the component displaying
// the result, with nothing subscribing by name.
func TestASetIsRecomputedWhenAnArgumentChanges(t *testing.T) {
	grant(t)
	sel := prop.NewSource("")
	c, err := load(t, "<Text>{{sets:Concat `Hover` .Sel}}</Text>",
		map[string]any{"Sel": sel})
	if err != nil {
		t.Fatal(err)
	}
	comp := gooey.NewComposer(c, 40, 1)
	comp.Frame()
	if got := strings.TrimRight(row(comp.Cells(), 0), " "); got != "Hover" {
		t.Fatalf("initial = %q, want %q", got, "Hover")
	}

	sel.Set("Pointer")
	_, painted := comp.Frame()
	if painted != 1 {
		t.Errorf("changing an argument repainted %d components, want 1: the Text that "+
			"displays the set, and nothing else", painted)
	}
	if got := strings.TrimRight(row(comp.Cells(), 0), " "); got != "Pointer Hover" {
		t.Errorf("after the Set = %q, want %q", got, "Pointer Hover")
	}
}

// TestWhenRecordsItsSetsAsDependenciesEvenWhileOff pins that When reads
// both sides BEFORE it branches — and it is a damage assertion because
// that is the only thing that can see the difference.
//
// The obvious test was written first and is deliberately not what is
// here: assert that turning the condition on shows a set that changed
// while it was off. It passes with the reads behind the branch too, and
// it has to. `on` is read unconditionally, so a Set on the condition
// invalidates the computed either way, and the re-evaluation reads the
// sets right then — the short-circuit is monotone, so it is VALUE-SAFE
// and no assertion on what is on screen can fail.
//
// What the hoist actually buys is the dependency itself, and the only
// observable of a recorded dependency is that a Set on it invalidates the
// reader. So that is what is measured: one component repaints. The 0/1
// pair below is the whole test — the second half is what proves the
// composition was capable of reporting a different number.
//
// Keeping the hoist is worth a repaint on a frame whose output did not
// change, because "read every argument before deciding anything" is the
// property that stays true when the deciding stops being monotone; the
// version that is only accidentally correct is the one nobody re-derives
// when they add a function next to this one.
func TestWhenRecordsItsSetsAsDependenciesEvenWhileOff(t *testing.T) {
	grant(t)
	flag := prop.NewSource(false)
	sel := prop.NewSource("Hover")
	c, err := load(t, "<Text>{{sets:When .Flag .Sel}}</Text>",
		map[string]any{"Flag": flag, "Sel": sel})
	if err != nil {
		t.Fatal(err)
	}
	comp := gooey.NewComposer(c, 40, 1)
	comp.Frame()
	if _, painted := comp.Frame(); painted != 0 {
		t.Fatalf("the composition had not settled: %d repainted", painted)
	}

	// The set changes while the condition is off. With the read behind the
	// branch there is no dependency on .Sel and nothing repaints.
	sel.Set("Pointer")
	if _, painted := comp.Frame(); painted != 1 {
		t.Errorf("changing the set while the condition was off repainted %d components, "+
			"want 1: the read is behind the branch, so .Sel is not a dependency", painted)
	}

	// And the value, which is right either way but has to be right.
	flag.Set(true)
	comp.Frame()
	if got := strings.TrimRight(row(comp.Cells(), 0), " "); got != "Pointer" {
		t.Errorf("after turning the condition on the set is %q, want %q", got, "Pointer")
	}
}

// ---- load-time failures ----

func TestLoadErrors(t *testing.T) {
	grant(t)
	vals := map[string]any{"S": prop.NewSource("Hover")}
	cases := []struct{ name, body, want string }{
		{"unknown function", "{{sets:Union .S}}", "unknown function"},
		{"Concat with no arguments", "{{sets:Concat}}", "at least one set"},
		{"Without with one argument", "{{sets:Without .S}}", "at least one set to remove"},
		{"When with no set", "{{sets:When .S}}", "at least one set"},
		{"Group arity", "{{sets:Group `Text` `Keys`}}", "takes 1 argument"},
		{"unknown group", "{{sets:Group `Nope`}}", "unknown group"},
		{"Has arity", "{{sets:Has .S}}", "takes 2 arguments"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := load(t, "<Text>"+c.body+"</Text>", vals)
			if err == nil {
				t.Fatalf("%s built; want a load error", c.body)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error for %s was %v, want it to mention %q", c.body, err, c.want)
			}
		})
	}
}

// TestAValueCallOnAnEventAttributeIsALoadError pins the registry split:
// sets is registered on the PULL side only, so it is unreachable from an
// event attribute.
func TestAValueCallOnAnEventAttributeIsALoadError(t *testing.T) {
	grant(t)
	_, err := load(t, "<Button Content=\"go\" Click=\"{{sets:Concat `Hover`}}\"/>", nil)
	if err == nil {
		t.Fatal("a sets: call on Click built; want a load error")
	}
}

func TestUnknownFunctionNamesTheInventory(t *testing.T) {
	grant(t)
	_, err := load(t, "<Text>{{sets:Union `a`}}</Text>", nil)
	if err == nil {
		t.Fatal("want a load error")
	}
	for _, n := range sethandlers.AllNames() {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("the error omits %q from the inventory: %v", n, err)
		}
	}
}

// ---- the consumer this pack exists for ----

// TestConcatDrivesAFrozenAllowSet is the end-to-end claim, and it is the
// user's own sketch made to run: a set composed in markup out of literals
// and a bound path, landing on <Frozen Allow> and changing what the
// subtree permits.
//
// It lives here rather than in markup/ because the import runs this way —
// this pack imports markup, so markup cannot import it.
func TestConcatDrivesAFrozenAllowSet(t *testing.T) {
	grant(t)
	sel := prop.NewSource("")
	ctx := &markup.Context{Values: map[string]any{
		"Sel": sel,
		"In":  prop.NewSource("in"),
		"Out": prop.NewSource("out"),
	}}
	src := `<Gooey xmlns:sets="` + sethandlers.URI + `">
	  <VStack>
	    <Frozen Allow="{{sets:Concat ` + "`Hover`" + ` .Sel}}">
	      <TextBox Name="inside" Text="{{.In}}"/>
	    </Frozen>
	    <TextBox Name="outside" Text="{{.Out}}"/>
	  </VStack>
	</Gooey>`
	root, err := markup.Build([]byte(src), ctx)
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, 30, 8)
	c.Frame()

	if n := boxStops(c.Focus().Order()); n != 1 {
		t.Fatalf(`{{sets:Concat `+"`Hover`"+` .Sel}} with .Sel empty left %d TextBox `+
			`focus stops, want 1`, n)
	}

	// The bound half of the set arrives, and the subtree becomes
	// reachable in the frame it lands.
	sel.Set("Focus")
	c.Frame()
	if n := boxStops(c.Focus().Order()); n != 2 {
		t.Errorf("one frame after .Sel became Focus there are %d TextBox focus stops, "+
			"want 2: the Composer's frozen observer did not subscribe through the "+
			"sets: computed", n)
	}

	sel.Set("")
	c.Frame()
	if n := boxStops(c.Focus().Order()); n != 1 {
		t.Errorf("revoking the bound half left %d TextBox focus stops, want 1", n)
	}
}

func boxStops(order []gooey.Component) int {
	n := 0
	for _, w := range order {
		if _, ok := w.(*components.TextBox); ok {
			n++
		}
	}
	return n
}
