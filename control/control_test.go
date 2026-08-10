package control

import (
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The full service — every operation over a live composition, through
// the bridge, over a real transport — is exercised by the grpc module's
// suite (grpc/server_test.go, grpc/session_test.go) and by MCP's once
// #112 reroutes it. What lives here is the context-only surface, which
// needs no run loop: with no loop running, the test goroutine IS the UI
// goroutine, the same ownership stance the markup tests take.

func testService(values map[string]any) (*Service, *markup.Context) {
	bind := &markup.Context{Values: values}
	return NewService(nopHost{}, bind), bind
}

type nopHost struct{}

func (nopHost) Post(fn func())            { fn() }
func (nopHost) Composer() *gooey.Composer { return nil }
func (nopHost) Swap(gooey.Component)      {}

func TestValuesDescribesEveryKind(t *testing.T) {
	svc, _ := testService(map[string]any{
		"S":   prop.NewSource("hello"),
		"N":   prop.NewSource(7),
		"B":   prop.NewSource(true),
		"F":   prop.NewSource(1.5),
		"D":   prop.NewSource(3 * time.Second),
		"C":   prop.NewSource(render.RGB(9, 8, 7)),
		"A":   prop.NewSource[any](map[string]any{"k": "v"}),
		"Sty": prop.NewSource(render.Style{Bold: true}),
		"Lit": "just text",
		"Cmd": gooey.Command(func() {}),
		"Nested": map[string]any{
			"Inner": prop.NewSource("deep"),
		},
	})
	entries, _, err := svc.Values()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ValueEntry{}
	for _, e := range entries {
		got[e.Name] = e
	}
	want := []struct {
		name string
		kind EntryKind
		typ  Kind
	}{
		{"S", EntryProperty, KindString},
		{"N", EntryProperty, KindInt},
		{"B", EntryProperty, KindBool},
		{"F", EntryProperty, KindFloat},
		{"D", EntryProperty, KindDuration},
		{"C", EntryProperty, KindColor},
		{"A", EntryProperty, KindAny},
		{"Sty", EntryProperty, KindUnspecified}, // off the table: descriptor only
		{"Lit", EntryLiteral, KindString},
		{"Cmd", EntryCommand, KindUnspecified},
		{"Nested.Inner", EntryProperty, KindString},
	}
	for _, w := range want {
		e, ok := got[w.name]
		if !ok {
			t.Fatalf("%q missing", w.name)
		}
		if e.Kind != w.kind || e.Type != w.typ {
			t.Errorf("%q: kind=%v type=%v, want %v/%v", w.name, e.Kind, e.Type, w.kind, w.typ)
		}
	}
	if got["A"].Value == nil || string(got["A"].Value.JSON) != `{"k":"v"}` {
		t.Errorf("any value = %+v", got["A"].Value)
	}
	if got["Sty"].Value != nil {
		t.Error("an off-table handle must cross as a descriptor, never a value")
	}
}

func TestSetChecksTypesAndChangesNothingOnMismatch(t *testing.T) {
	n := prop.NewSource(1)
	svc, _ := testService(map[string]any{"N": n})

	err := svc.Set("N", StringValue("nope"))
	if err == nil || !strings.Contains(err.Error(), "int") {
		t.Fatalf("mismatch error = %v, want both types named", err)
	}
	if n.Get() != 1 {
		t.Error("a failed Set changed the value")
	}
	if e, ok := err.(*Error); !ok || e.Kind != KindInvalidArgument {
		t.Errorf("mismatch kind = %v", err)
	}
	if err := svc.Set("N", IntValue(41)); err != nil || n.Get() != 41 {
		t.Errorf("typed Set: %v (n=%d)", err, n.Get())
	}
	if err := svc.Set("Gone", IntValue(1)); err == nil || err.(*Error).Kind != KindNotFound {
		t.Errorf("unknown name = %v, want NotFound", err)
	}
}

func TestRegisterIsAtomicAndRefusesCollisions(t *testing.T) {
	svc, bind := testService(map[string]any{"Existing": prop.NewSource("x")})

	// A batch with a collision leaves NOTHING behind, including the
	// entries before the bad one.
	err := svc.Register([]Registration{
		{Name: "Fresh", Kind: KindString},
		{Name: "Existing", Kind: KindString},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
	if _, ok := bind.Values["Fresh"]; ok {
		t.Error("a failed batch left its earlier registrations behind")
	}

	// Dotted names materialize scopes, and resolve the way {{.A.B}} does.
	if err := svc.Register([]Registration{{
		Name: "Scope.Level", Kind: KindInt, Initial: &Value{Kind: KindInt, Int: 3},
	}}); err != nil {
		t.Fatal(err)
	}
	e, err := svc.Value("Scope.Level")
	if err != nil || e.Value == nil || e.Value.Int != 3 {
		t.Errorf("registered value = %+v, %v", e, err)
	}

	// A mismatched initial is refused with both kinds named.
	err = svc.Register([]Registration{{
		Name: "Wrong", Kind: KindInt, Initial: &Value{Kind: KindString, Str: "x"},
	}})
	if err == nil || !strings.Contains(err.Error(), "int") {
		t.Errorf("mismatched initial = %v", err)
	}
}

func TestValueEqual(t *testing.T) {
	cases := []struct {
		a, b Value
		eq   bool
	}{
		{StringValue("a"), StringValue("a"), true},
		{StringValue("a"), StringValue("b"), false},
		{IntValue(1), IntValue(1), true},
		{IntValue(1), FloatValue(1), false}, // kinds differ
		{ColorValue(render.RGB(1, 2, 3)), ColorValue(render.RGB(1, 2, 3)), true},
		{ColorValue(render.Color{}), ColorValue(render.RGB(0, 0, 0)), false}, // unset != black
		{JSONValue([]byte(`{"a":1}`)), JSONValue([]byte(`{"a":1}`)), true},
		{JSONValue([]byte(`{"a":1}`)), JSONValue([]byte(`{"a":2}`)), false},
		{DurationValue(time.Second), DurationValue(time.Second), true},
	}
	for i, c := range cases {
		if got := c.a.Equal(c.b); got != c.eq {
			t.Errorf("case %d: Equal = %v, want %v", i, got, c.eq)
		}
	}
}

func TestInvokeRejectsNonCommands(t *testing.T) {
	ran := 0
	svc, _ := testService(map[string]any{
		"Go":  gooey.Command(func() { ran++ }),
		"Lit": "text",
	})
	if err := svc.Invoke("Go"); err != nil || ran != 1 {
		t.Errorf("Invoke: %v (ran=%d)", err, ran)
	}
	if err := svc.Invoke("Lit"); err == nil || err.(*Error).Kind != KindInvalidArgument {
		t.Errorf("invoking a literal = %v", err)
	}
}
