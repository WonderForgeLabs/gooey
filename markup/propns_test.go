package markup

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The handler-argument vocabulary, pinned.
//
// resolveArg used to accept *prop.Property[string] and string and
// nothing else, which made {{str:Pad .Count `4`}} a load failure over a
// perfectly renderable int and put every non-string property out of
// reach of every namespace. It now defers to textSource — the same type
// switch {{.Path}} uses in a Text — so the two paths accept the same
// values and render them identically.
//
// "Identically" is the part worth a test rather than a sentence: two
// hand-matched float formats (strconv 'g' next to strconv 'f') would
// agree on every number anyone tries by hand and disagree at 1e21.

const propnsURI = "gooey.dev/handlers/propns-test"

// propnsEcho is a value provider whose result IS its argument's
// Arg.String(), so what it renders is exactly what a handler would read.
func propnsEcho(t *testing.T) {
	t.Helper()
	RegisterValues(propnsURI, ValueFunc(func(c *Call) (*prop.Property[string], error) {
		if len(c.Args) != 1 {
			return nil, errPropnsArity
		}
		a := c.Args[0]
		return prop.NewComputed(func() string { return a.String() }), nil
	}))
	t.Cleanup(func() { RegisterValues(propnsURI, nil) })
}

var errPropnsArity = errPropns("echo takes 1 argument")

type errPropns string

func (e errPropns) Error() string { return string(e) }

func propnsLoad(t *testing.T, body string, vals map[string]any) (gooey.Component, error) {
	t.Helper()
	src := `<Gooey xmlns:t="` + propnsURI + `">` + body + `</Gooey>`
	return Build([]byte(src), &Context{Values: vals, Dispatcher: gooey.NewDispatcher()})
}

func propnsRow(b *render.Buffer, y int) string {
	var sb strings.Builder
	for x := 0; x < b.W; x++ {
		sb.WriteRune(b.At(x, y).Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

// One row renders the same handle twice: once through a binding, once
// through a namespace argument. They must agree, whatever the type.
func TestHandlerArgRendersLikeTheTextPath(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want string
	}{
		{"string handle", prop.NewSource("ada"), "ada"},
		{"int handle", prop.NewSource(7), "7"},
		{"int64 handle", prop.NewSource(int64(-9)), "-9"},
		{"bool handle", prop.NewSource(true), "true"},
		{"duration handle", prop.NewSource(1500 * time.Millisecond), "1.5s"},
		{"float handle", prop.NewSource(1.5), "1.5"},

		// The case that separates 'f' from 'g'. Under 'g' this renders
		// "1e+21"; the text path uses 'f', so a hand-written second float
		// format in Arg.String would show up right here and nowhere else.
		{"a float big enough to expose the format", prop.NewSource(1e21), "1000000000000000000000"},

		// Constants, not handles: a context may hold either, and it
		// would be a wart for a string constant to work and an int
		// constant to be a load error.
		{"int constant", 7, "7"},
		{"string constant", "ada", "ada"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			propnsEcho(t)
			w, err := propnsLoad(t, "<Text>{{t:Echo .V}}|{{.V}}</Text>", map[string]any{"V": tc.val})
			if err != nil {
				t.Fatalf("a %T argument failed the load: %v", tc.val, err)
			}
			c := gooey.NewComposer(w, 60, 1)
			c.Frame()
			got := propnsRow(c.Cells(), 0)
			viaArg, viaBinding, ok := strings.Cut(got, "|")
			if !ok {
				t.Fatalf("row = %q", got)
			}
			if viaArg != viaBinding {
				t.Fatalf("Arg.String()=%q but the binding renders %q — the two paths must not have separate formats", viaArg, viaBinding)
			}
			if viaArg != tc.want {
				t.Fatalf("rendered %q, want %q", viaArg, tc.want)
			}
		})
	}
}

// The widening is a widening, not a removal: a value no binding can
// render is still a load error, and the message says what the rule is.
func TestHandlerArgStillRejectsWhatNoBindingCanRender(t *testing.T) {
	propnsEcho(t)
	for _, v := range []any{
		[]string{"a"},
		map[string]int{"a": 1},
		prop.NewSource([]int{1}),
		struct{ X int }{1},
	} {
		_, err := propnsLoad(t, "<Text>{{t:Echo .V}}</Text>", map[string]any{"V": v})
		if err == nil {
			t.Fatalf("a %T argument loaded clean", v)
		}
		if !strings.Contains(err.Error(), "a value a binding can render") {
			t.Fatalf("error %q does not name the rule", err)
		}
	}
}

// A non-string argument is a HANDLE, not a snapshot. The widening would
// be a trap if it resolved the value at load: a handler firing twice
// must see the property's value each time.
func TestNonStringHandlerArgKeepsLvalueSemantics(t *testing.T) {
	var seen []string
	RegisterHandlers(propnsURI, HandlerFunc(func(c *Call) (gooey.Command, error) {
		a := c.Args[0]
		return func() { seen = append(seen, a.String()) }, nil
	}))
	t.Cleanup(func() { RegisterHandlers(propnsURI, nil) })

	n := prop.NewSource(1)
	w, err := propnsLoad(t, `<Button Content="go" Click="{{t:Report .N}}"/>`, map[string]any{"N": n})
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(w, 20, 1)
	c.Frame()
	c.Focus().SetFocus(c.Focus().Order()[0])

	c.HandleKey(input.Named(input.KeyEnter))
	n.Set(42)
	c.HandleKey(input.Named(input.KeyEnter))

	if strings.Join(seen, ",") != "1,42" {
		t.Fatalf("handler saw %v, want [1 42] — the argument snapshotted instead of holding the handle", seen)
	}
}
