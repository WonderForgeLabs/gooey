package strhandlers_test

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	strhandlers "github.com/WonderForgeLabs/gooey/handlers/str"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// row is the row as a terminal would read it — see render.RowText,
// which is where the continuation markers get skipped. Building the
// string here cell by cell rendered them as literal runes, which made
// any fixture holding a wide glyph unassertable.
func row(b *render.Buffer, y int) string { return render.RowText(b, y) }

func grant(t *testing.T) {
	t.Helper()
	markup.RegisterValues(strhandlers.URI, strhandlers.New())
	t.Cleanup(func() { markup.RegisterValues(strhandlers.URI, nil) })
}

func load(t *testing.T, body string, vals map[string]any) (gooey.Component, error) {
	t.Helper()
	src := `<Gooey xmlns:str="` + strhandlers.URI + `">` + body + `</Gooey>`
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
	s := prop.NewSource("  Ada Lovelace  ")
	vals := map[string]any{"S": s, "Empty": prop.NewSource("")}

	cases := []struct{ name, body, want string }{
		{"Upper", "{{str:Upper .S}}", "  ADA LOVELACE"},
		{"Lower", "{{str:Lower .S}}", "  ada lovelace"},
		{"Trim", "{{str:Trim .S}}", "Ada Lovelace"},
		{"Replace", "{{str:Replace .S `Ada` `Grace`}}", "  Grace Lovelace"},
		{"Join", "{{str:Join `-` `a` `b` `c`}}", "a-b-c"},
		{"Join with a path", "{{str:Join `/` `x` .Empty}}", "x/"},
		{"Default falls back", "{{str:Default .Empty `none`}}", "none"},
		{"Default passes through", "{{str:Default .S `none`}}", "  Ada Lovelace"},
		{"Pad widens", "[{{str:Pad `ab` `5`}}]", "[ab   ]"},
		{"Pad never truncates", "[{{str:Pad `abcdef` `3`}}]", "[abcdef]"},
		{"Truncate cuts", "{{str:Truncate .S `6`}}", "  Ada…"},
		{"Truncate leaves short text", "{{str:Truncate `ab` `6`}}", "ab"},
		{"Truncate to one", "{{str:Truncate `abcdef` `1`}}", "…"},
		{"composes with literals", "say {{str:Upper `hi`}} now", "say HI now"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := paint(t, tc.body, vals, 40); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Not bytes: padding by byte length leaves a multibyte name short. These
// runes are multi-byte and ONE column each, so this case pins the byte
// half alone.
//
// It was called TestPadAndTruncateCountRunes, which was the reason given
// for the fix that made them count runes — "a terminal counts cells".
// The reason was right and the unit was not, and the name went on
// asserting the wrong one; see the column half below.
func TestPadAndTruncateDoNotCountBytes(t *testing.T) {
	grant(t)
	if got := paint(t, "[{{str:Pad `héllo` `7`}}]", nil, 20); got != "[héllo  ]" {
		t.Fatalf("Pad got %q", got)
	}
	if got := paint(t, "{{str:Truncate `héllo` `3`}}", nil, 20); got != "hé…" {
		t.Fatalf("Truncate got %q", got)
	}
}

// And not runes either, which is the opposite error and the one that
// survived the byte fix. Both functions are documented in terms of what
// a terminal does — pad fills to a width, truncate is "never wider than
// n" — and a CJK rune is one rune and TWO cells, so a name padded to 12
// runes was 24 cells wide and pushed the column beside it off the row.
func TestPadAndTruncateCountColumns(t *testing.T) {
	grant(t)
	// 世界 is 2 runes and 4 columns, so padding to 6 adds TWO spaces, not
	// four. A rune count writes "世界    " — 8 columns in a 6-column slot.
	if got := paint(t, "[{{str:Pad `世界` `6`}}]", nil, 20); got != "[世界  ]" {
		t.Fatalf("Pad got %q, want %q — 世界 is 4 columns, so 6 leaves room for 2 spaces",
			got, "[世界  ]")
	}
	// Truncate to 5 columns: the ellipsis takes one, leaving 4, which is
	// exactly two glyphs.
	if got := paint(t, "{{str:Truncate `世界世界` `5`}}", nil, 20); got != "世界…" {
		t.Fatalf("Truncate got %q, want %q", got, "世界…")
	}
	// And the short case, where the cut falls INSIDE a glyph: 3 columns
	// leaves 2 for text, and 世 fills them exactly.
	if got := paint(t, "{{str:Truncate `世界世界` `3`}}", nil, 20); got != "世…" {
		t.Fatalf("Truncate got %q, want %q", got, "世…")
	}
	// The odd budget is the honest-short case: 4 columns leaves 3 for
	// text, 世 takes 2, and 界 cannot fit in the remaining 1 — so the
	// result is 3 columns rather than 4. Half a glyph is not drawable.
	if got := paint(t, "{{str:Truncate `世界世界` `4`}}", nil, 20); got != "世…" {
		t.Fatalf("Truncate got %q, want %q — the leftover column stays empty "+
			"rather than carrying half of 界", got, "世…")
	}
}

// The damage contract for a pure function: its argument's Get runs
// inside the computed, so it is a subscription, and changing the
// argument repaints exactly the component displaying the result.
func TestTransformRepaintsOnlyItsReader(t *testing.T) {
	grant(t)
	name := prop.NewSource("ada")
	src := `<VStack>
	  <Text>{{str:Upper .Name}}</Text>
	  <Text>static</Text>
	</VStack>`
	w, err := load(t, src, map[string]any{"Name": name})
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 2)
	c.Frame()
	if got := strings.TrimRight(row(c.Cells(), 0), " "); got != "ADA" {
		t.Fatalf("row 0 = %q, want ADA", got)
	}

	name.Set("grace")
	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("changing the argument painted %d components, want 1", painted)
	}
	if got := strings.TrimRight(row(c.Cells(), 0), " "); got != "GRACE" {
		t.Fatalf("row 0 = %q, want GRACE", got)
	}
}

// The hoisted-Get rule, pinned where it is load-bearing. While Default
// is showing the fallback, the VALUE's Get must still run, or the
// component never notices the value arriving.
func TestDefaultStaysSubscribedToBothArguments(t *testing.T) {
	grant(t)
	v := prop.NewSource("")
	fb := prop.NewSource("(empty)")
	w, err := load(t, "<Text>{{str:Default .V .FB}}</Text>", map[string]any{"V": v, "FB": fb})
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 1)
	c.Frame()
	if got := strings.TrimRight(row(c.Cells(), 0), " "); got != "(empty)" {
		t.Fatalf("row = %q, want the fallback", got)
	}

	// Showing the fallback, but still subscribed to the value.
	v.Set("real")
	_, painted := c.Frame()
	if painted != 1 {
		t.Errorf("the value arriving painted %d components, want 1 — the Get was behind the branch", painted)
	}
	if got := strings.TrimRight(row(c.Cells(), 0), " "); got != "real" {
		t.Fatalf("row = %q, want real", got)
	}

	// And now showing the value, but still subscribed to the fallback.
	fb.Set("(none)")
	_, painted = c.Frame()
	if painted != 1 {
		t.Errorf("the unused fallback changing painted %d components, want 1", painted)
	}
}

func TestStrLoadErrors(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"unknown function", "{{str:Frobnicate .S}}",
			`unknown function "Frobnicate"; str provides: Upper, Lower, Trim, Replace, Join, Default, Pad, Truncate`},
		{"unary arity", "{{str:Upper .S .S}}", "Upper takes 1 argument, got 2"},
		{"Replace arity", "{{str:Replace .S `a`}}", "Replace takes 3 arguments"},
		{"Join needs a value", "{{str:Join `-`}}", "Join takes a separator and at least one value"},
		{"Default arity", "{{str:Default .S}}", "Default takes 2 arguments"},
		{"Pad arity", "{{str:Pad .S}}", "Pad takes 2 arguments (the text and a width), got 1"},
		{"bound width", "{{str:Pad .S .W}}", "Pad width must be a backtick literal, not .W"},
		{"width not an integer", "{{str:Pad .S `wide`}}", "Pad width `wide` is not an integer"},
		{"width below one", "{{str:Truncate .S `0`}}", "Truncate width must be at least 1, got 0"},
		{"negative width", "{{str:Pad .S `-3`}}", "Pad width must be at least 1, got -3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			grant(t)
			_, err := load(t, "<Text>"+tc.body+"</Text>", map[string]any{
				"S": prop.NewSource("s"), "W": prop.NewSource("4"),
			})
			if err == nil {
				t.Fatalf("%s loaded clean; expected an error mentioning %q", tc.body, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// str is registered on the pull side only, so an event attribute cannot
// reach it — the mirror image of net:Get in a Text.
func TestStrOnAnEventAttributeIsALoadError(t *testing.T) {
	grant(t)
	_, err := load(t, "<Button Content=\"x\" Click=\"{{str:Upper `a`}}\"/>", nil)
	if err == nil {
		t.Fatal("a pure value function bound to Click")
	}
	if !strings.Contains(err.Error(), "no registered handler provider") {
		t.Fatalf("error %q does not name the missing handler grant", err)
	}
}

func TestAllNamesMatchesTheDispatch(t *testing.T) {
	grant(t)
	for _, n := range strhandlers.AllNames() {
		// Every advertised name must fail on ARITY, never on being
		// unknown: the inventory and the dispatch cannot drift.
		_, err := load(t, "<Text>{{str:"+n+"}}</Text>", nil)
		if err == nil {
			t.Fatalf("%s accepted zero arguments", n)
		}
		if strings.Contains(err.Error(), "unknown function") {
			t.Fatalf("AllNames advertises %q but NewValue does not resolve it", n)
		}
	}
}
