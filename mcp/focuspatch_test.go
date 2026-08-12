package mcp

// Does patching a subtree destroy the focus and caret of a TextBox
// inside it?
//
// This matters for the plugins-as-standalone-activities design
// (docs/specs/2026-08-11-plugins-as-standalone-activities.md), where a
// plugin owns an "island" of the tree and refreshes it — so the writer
// nobody partitions is the USER, typing into a widget inside the very
// island being refreshed. "Ownership by subtree" makes plugin writes
// disjoint from each other; it does nothing about this.
//
// PatchMarkup's own doc is careful about what it preserves:
//
//	the commit is one slot write plus Composer.InvalidateStructure,
//	which re-syncs paint nodes and the input tree at the next frame
//	while keeping every SURVIVING component's node — clean/dirty
//	state, focus and all.
//
// "Surviving" is the word under load. A patched subtree's components
// are replaced, not survivors, so the question is what happens to focus
// when the focused component is the thing being replaced. This test
// records the answer rather than asserting a wish, so that a later fix
// (host rejects a patch whose subtree holds the focus stop) has a
// before-picture to point at.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// islandMarkup is the plugin-island shape: a named container a plugin
// would own and refresh, with a text input inside it that the user may
// be typing into at any moment.
const islandMarkup = `<Gooey>
  <VStack Name="Island">
    <Text Name="Head">island</Text>
    <TextBox Name="Field" Text="{{.Note}}"/>
  </VStack>
</Gooey>`

// islandPatch replaces the island with an equivalent one — the same
// element names, the way a plugin re-sending its panel would. Nothing
// about the user's widget "changed"; it is simply rebuilt.
const islandPatch = `<Gooey>
  <VStack Name="Island">
    <Text Name="Head">island (refreshed)</Text>
    <TextBox Name="Field" Text="{{.Note}}"/>
  </VStack>
</Gooey>`

// focusedName walks a tree_snapshot result and returns the Name= of the
// element carrying focused:true, plus that node's caret if it has one.
// Empty name means nothing on the page is focused.
func focusedName(t *testing.T, tree map[string]any) (name string, caret int, found bool) {
	t.Helper()
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		if found {
			return
		}
		if f, _ := n["focused"].(bool); f {
			name, _ = n["name"].(string)
			if props, ok := n["props"].(map[string]any); ok {
				if c, ok := props["caret"].(float64); ok {
					caret = int(c)
				}
			}
			found = true
			return
		}
		kids, _ := n["children"].([]any)
		for _, k := range kids {
			if km, ok := k.(map[string]any); ok {
				walk(km)
			}
		}
	}
	if root, ok := tree["tree"].(map[string]any); ok {
		walk(root)
	}
	return name, caret, found
}

func snapshotTree(t *testing.T, c *client) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(c.ok("tree_snapshot", nil)), &out); err != nil {
		t.Fatalf("tree_snapshot: %v", err)
	}
	return out
}

func TestPatchMarkupDropsFocusInsideThePatchedSubtree(t *testing.T) {
	vm, values := newVM()
	_ = vm
	app := newTestApp(t, islandMarkup, values)
	s, err := New(app, Options{Context: app.ctx, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := newClient(t, s)

	// The user is typing into the island's TextBox.
	c.ok("focus", map[string]any{"name": "Field"})
	c.ok("send_keys", map[string]any{"text": "hello"})

	name, caret, found := focusedName(t, snapshotTree(t, c))
	if !found || name != "Field" {
		t.Fatalf("before patch: focused = %q (found=%v), want \"Field\"", name, found)
	}
	if caret != len("hello") {
		t.Fatalf("before patch: caret = %d, want %d", caret, len("hello"))
	}

	// The plugin refreshes the island it owns. Same names, same shape —
	// this is the benign case, not a destructive edit.
	if out := c.ok("patch_markup", map[string]any{
		"name":   "Island",
		"source": islandPatch,
	}); !strings.Contains(out, "Island") {
		t.Fatalf("patch_markup: %s", out)
	}

	// The typed TEXT survives, because it lives in the bound property,
	// not in the component — the viewmodel is what a patch preserves.
	if got := vm.note.Get(); got != "hello" {
		t.Errorf("bound text after patch = %q, want %q — the property should outlive the widget", got, "hello")
	}

	after, caretAfter, foundAfter := focusedName(t, snapshotTree(t, c))
	t.Logf("after patching the island: focused=%q found=%v caret=%d", after, foundAfter, caretAfter)

	// THE MEASURED FINDING, and it is split — the two halves of "where
	// the user is" do not share a fate.
	//
	// Focus SURVIVES. It is re-resolved through the name table, and the
	// fragment's root must carry the same Name= as the element it
	// replaces, so a plugin that re-sends its panel with stable names
	// keeps the user's focus on the right widget.
	if !foundAfter || after != "Field" {
		t.Errorf("focus did NOT survive the patch: focused=%q found=%v; "+
			"if this is now the behavior the plugin design needs a stronger "+
			"rule than caret restoration", after, foundAfter)
	}
	// The CARET does not. It is component-local state, and the component
	// was replaced — so the user keeps their cursor in the right box and
	// loses their position in it. Typing resumes at offset 0, in the
	// middle of text they already entered.
	//
	// Asserted as the exact value, not merely "different". "Resets to 0"
	// is the claim two design documents cite, and `!= len("hello")` also
	// passes for a caret that landed anywhere else — which would be a
	// different bug wearing this test as evidence that it does not exist.
	if caretAfter != 0 {
		t.Errorf("caret = %d after the patch, want 0; the documented behaviour is "+
			"that the user resumes typing at the START of text they already entered",
			caretAfter)
	}
}

// The name is the address, so the survival above depends on the plugin
// keeping it. A refresh that renames or drops the focused widget — a
// plugin re-rendering a list, or swapping which control it shows — has
// no name to re-resolve, and focus really is lost. This is the case the
// spec's rejection rule has to cover; the stable-name case is benign.
func TestPatchMarkupLosesFocusWhenTheFocusedNameDisappears(t *testing.T) {
	_, values := newVM()
	app := newTestApp(t, islandMarkup, values)
	s, err := New(app, Options{Context: app.ctx, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := newClient(t, s)

	c.ok("focus", map[string]any{"name": "Field"})
	if name, _, found := focusedName(t, snapshotTree(t, c)); !found || name != "Field" {
		t.Fatalf("before patch: focused = %q (found=%v)", name, found)
	}

	// Same island, but the focused widget is gone from it.
	const renamed = `<Gooey>
  <VStack Name="Island">
    <Text Name="Head">island (rebuilt)</Text>
    <TextBox Name="Other" Text="{{.Note}}"/>
  </VStack>
</Gooey>`
	c.ok("patch_markup", map[string]any{"name": "Island", "source": renamed})

	after, _, foundAfter := focusedName(t, snapshotTree(t, c))
	t.Logf("after replacing the focused widget: focused=%q found=%v", after, foundAfter)
	if foundAfter && after == "Field" {
		t.Errorf("focus stayed on %q, which no longer exists in the tree", after)
	}

	// The measured DESTINATION, not merely "not the deleted name".
	//
	// Asserting only the negative was the gap: a run where focus cleared
	// to nothing passed identically, so the test could not distinguish
	// "focus moved somewhere the user did not ask for" from "focus was
	// dropped". Those call for opposite fixes, and only the first is
	// what actually happens — focus lands on the replacement widget, so
	// the next keystroke goes into a box the user never selected.
	if !foundAfter {
		t.Fatalf("focus was cleared entirely; the recorded behaviour is that it MOVES, " +
			"and a rule written for a cleared focus would not address it")
	}
	if after != "Other" {
		t.Errorf("focus moved to %q, want %q — the neighbour that replaced the "+
			"focused widget", after, "Other")
	}
}

// The third case, and the one a name-only rule cannot see: the focused
// NAME survives but its component TYPE changes. "Reject a patch that
// removes the focused name" lets this through — the name is still
// there — and caret preservation keyed on "same name, same type"
// declines to act, because the type differs. So the user who was typing
// into a TextBox is now focused on a Button of the same name, and the
// next keypress does something other than insert a character.
//
// If this is what the tree reports, a name-only rule is too narrow: the
// predicate has to be "the focused name survives AS THE SAME KIND", not
// merely "survives".
func TestPatchMarkupKeepsFocusWhenTheNameSurvivesButTheTypeChanges(t *testing.T) {
	vm, values := newVM()
	app := newTestApp(t, islandMarkup, values)
	s, err := New(app, Options{Context: app.ctx, Timeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := newClient(t, s)

	c.ok("focus", map[string]any{"name": "Field"})
	c.ok("send_keys", map[string]any{"text": "hi"})
	if name, _, found := focusedName(t, snapshotTree(t, c)); !found || name != "Field" {
		t.Fatalf("before patch: focused = %q (found=%v)", name, found)
	}

	// Same island, same name, different kind of control.
	const retyped = `<Gooey>
  <VStack Name="Island">
    <Text Name="Head">island (retyped)</Text>
    <Button Name="Field" Content="now a button" Click="{{.Increment}}"/>
  </VStack>
</Gooey>`
	c.ok("patch_markup", map[string]any{"name": "Island", "source": retyped})

	tree := snapshotTree(t, c)
	after, _, foundAfter := focusedName(t, tree)
	t.Logf("after retyping the focused widget: focused=%q found=%v", after, foundAfter)

	// The name survived the substitution, so a rejection rule written as
	// "refuse a patch that removes the focused name" lets this through.
	if !foundAfter || after != "Field" {
		t.Fatalf("focused=%q found=%v, want the name to survive as %q — without "+
			"that, this test is not exercising the case it is named for",
			after, foundAfter, "Field")
	}

	// THE ASSERTION THIS TEST SHIPPED WITHOUT.
	//
	// Everything above only establishes that the name is still focused.
	// The claim that makes this hazard worth a rule — "the user's next
	// keystroke invokes a command instead of inserting a character" —
	// was logged rather than asserted, so the most alarming statement in
	// two design documents rested on a test that never pressed a key.
	//
	// Enter, once, on a widget the user still believes is their TextBox.
	before := vm.incs
	c.ok("send_keys", map[string]any{"keys": []any{"enter"}})
	if vm.incs == before {
		t.Fatalf("Enter did not invoke the command (incs stayed %d) — if this is now "+
			"the behaviour, the silent-input-corruption claim is wrong and the "+
			"specs that cite it need correcting", before)
	}
	t.Logf("Enter on the retyped widget invoked the bound command %d time(s): "+
		"a keystroke the user intended as text executed an action instead",
		vm.incs-before)

	// And the corruption is silent from the client's side too: the patch
	// that caused it reported success, with nothing in the result naming
	// the type change. That is the gap a warning field or a rejection
	// rule would close.
	if got := vm.note.Get(); got != "hi" {
		t.Errorf("bound text = %q, want %q — the property should be untouched by "+
			"the keystroke, which went to a command rather than the box", got, "hi")
	}
}
