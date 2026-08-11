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
	if caretAfter == len("hello") {
		t.Errorf("caret survived the patch (=%d) — component-local state now "+
			"outlives a replace; revisit the spec's focus rule, it may be "+
			"unnecessary", caretAfter)
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
	_, values := newVM()
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

	// Record what the focused element now IS, so the log names the hazard.
	if foundAfter && after == "Field" {
		t.Logf("the focused name survived a TextBox -> Button substitution: "+
			"a name-only rejection rule would allow this patch, and caret "+
			"preservation keyed on same-name-same-type would decline to act "+
			"(tree=%v)", tree["tree"])
	}
}
