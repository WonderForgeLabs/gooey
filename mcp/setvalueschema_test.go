package mcp

// set_value's published schema has to TYPE its `value` argument.
//
// The failure it prevents is unusual: a correctly-behaved client is the
// thing that breaks. Given an argument with no declared type, a client
// with nothing to validate against serializes everything as a string;
// setValue's coercion then correctly refuses "false" for a bool, and
// every int, bool and float property in the app is read-only from that
// client while strings keep working. The server is right, the client is
// right, and the schema is what is wrong — which is why it survived.
//
// Two halves, and the second is the one worth guarding:
//
//   - a real JSON boolean/number is accepted (the fix works);
//   - a QUOTED "false" is still refused for a bool, naming both types
//     (the diagnostic survives). A "fix" that started coercing "false"
//     to false would pass the first half and destroy the error message
//     that made this findable at all.

import (
	"encoding/json"
	"strings"
	"testing"
)

// declaredType returns the JSON Schema `type` of one argument of one
// published tool, as a client would see it in tools/list.
func declaredType(t *testing.T, s *Server, tool, arg string) any {
	t.Helper()
	for _, tl := range s.tools {
		if tl.Name != tool {
			continue
		}
		props, ok := tl.Schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: schema has no properties", tool)
		}
		spec, ok := props[arg].(map[string]any)
		if !ok {
			t.Fatalf("%s: no argument %q", tool, arg)
		}
		return spec["type"]
	}
	t.Fatalf("no tool named %q", tool)
	return nil
}

func TestSetValueDeclaresItsValueType(t *testing.T) {
	_, _, s, _ := setup(t)

	got := declaredType(t, s, "set_value", "value")
	if got == nil {
		t.Fatal("set_value's `value` has no declared type; a client will " +
			"serialize every argument as a string and no int, bool or float " +
			"property will be writable")
	}
	// A union, because one property may be any of these. Colors ride in
	// as #rrggbb strings, so "string" covers them.
	list, ok := got.([]any)
	if !ok {
		t.Fatalf("`value` type = %#v, want a union of the JSON types it accepts", got)
	}
	want := map[string]bool{"string": true, "boolean": true, "number": true}
	for _, k := range list {
		delete(want, k.(string))
	}
	if len(want) > 0 {
		t.Errorf("`value` union %v is missing %v", list, want)
	}
}

func TestSetValueAcceptsRealJSONScalars(t *testing.T) {
	_, vm, _, c := setup(t)

	// The exact shapes a client emits once the schema tells it the types.
	c.ok("set_value", map[string]any{"name": "Flag", "value": true})
	if !vm.flag.Get() {
		t.Error("a real JSON true did not reach a bool property")
	}
	c.ok("set_value", map[string]any{"name": "Flag", "value": false})
	if vm.flag.Get() {
		t.Error("a real JSON false did not reach a bool property")
	}
	c.ok("set_value", map[string]any{"name": "Ratio", "value": 0.25})
	if got := vm.ratio.Get(); got != 0.25 {
		t.Errorf("float = %v, want 0.25", got)
	}
	c.ok("set_value", map[string]any{"name": "Note", "value": "still a string"})
	if got := vm.note.Get(); got != "still a string" {
		t.Errorf("string = %q", got)
	}
}

func TestSetValueStillRefusesAQuotedBool(t *testing.T) {
	_, vm, _, c := setup(t)

	// The diagnostic. If this ever starts passing, the coercion has been
	// loosened to paper over a client bug, and the next person to hit a
	// schema problem gets silence instead of "got string".
	text, isErr := c.call("set_value", map[string]any{"name": "Flag", "value": "false"})
	if !isErr {
		t.Fatalf("a quoted \"false\" was accepted for a bool property: %s", text)
	}
	if !strings.Contains(text, "boolean") || !strings.Contains(text, "string") {
		t.Errorf("error = %q, want both the wanted and the received type named", text)
	}
	if vm.flag.Get() {
		t.Error("a refused set changed the property anyway")
	}

	// Same for an int. Registering one rather than using the fixture's
	// Count, which is a COMPUTED — setting that panics on the UI goroutine
	// (recovered by the bridge) and would be testing a different rule.
	c.ok("register_properties", map[string]any{"properties": []any{
		map[string]any{"name": "Tally", "type": "int", "value": 1},
	}})
	if text, isErr := c.call("set_value", map[string]any{"name": "Tally", "value": "3"}); !isErr {
		t.Errorf("a quoted \"3\" was accepted for an int: %s", text)
	} else if !strings.Contains(text, "integer") || !strings.Contains(text, "string") {
		t.Errorf("error = %q, want both types named", text)
	}
	// And the real thing still works.
	c.ok("set_value", map[string]any{"name": "Tally", "value": 3})
}

// register_properties' per-registration `value` is deliberately NOT a
// closed union: the `any` kind accepts arbitrary JSON, objects and arrays
// included, so naming only the scalar types would make a conforming
// client refuse a legitimate payload. This pins that intent, so nobody
// "fixes" it by symmetry with set_value.
func TestRegisterPropertiesValueStaysUntypedForAny(t *testing.T) {
	_, _, s, c := setup(t)

	for _, tl := range s.tools {
		if tl.Name != "register_properties" {
			continue
		}
		props := tl.Schema["properties"].(map[string]any)
		items := props["properties"].(map[string]any)["items"].(map[string]any)
		spec := items["properties"].(map[string]any)["value"].(map[string]any)
		if ty, ok := spec["type"]; ok {
			t.Fatalf("registration `value` declares type %v; `any` must accept "+
				"objects and arrays, so a closed union would refuse valid payloads", ty)
		}
		if d, _ := spec["description"].(string); !strings.Contains(d, "NOT a quoted") {
			t.Error("the description must tell a client that scalars are real " +
				"JSON values, since no type does it for them")
		}
	}

	// And the behaviour the description promises: a real JSON object
	// registers under `any`, while a real JSON bool registers under bool.
	c.ok("register_properties", map[string]any{"properties": []any{
		map[string]any{"name": "Cfg", "type": "any", "value": map[string]any{"deep": []any{1, 2}}},
		map[string]any{"name": "OnOff", "type": "bool", "value": true},
	}})
	var out map[string]any
	if err := json.Unmarshal([]byte(c.ok("list_values", nil)), &out); err != nil {
		t.Fatalf("list_values: %v", err)
	}
	var sawBool bool
	for _, v := range out["values"].([]any) {
		e := v.(map[string]any)
		if e["name"] == "OnOff" && e["value"] == true {
			sawBool = true
		}
	}
	if !sawBool {
		t.Error("a registered bool did not come back as a JSON true")
	}
}
