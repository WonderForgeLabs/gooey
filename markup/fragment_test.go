package markup

import (
	"strings"
	"testing"
	"testing/fstest"
)

// Attached-property scoping at the DOCUMENT ROOT.
//
// These live in markup/ deliberately. The defect they pin was found only
// by mcp/'s suite, and mcp/ is a nested module that the root's `./...`
// does not run — so a regression here would have been invisible to every
// check anyone routinely runs. A defect that only a nested module
// surfaces is a defect that shows up when someone remembers to look.

// TestPatchFragmentMayRestateAttachedProperties is the documented
// patch_markup capability: "layout attributes the fragment does not
// restate are preserved from the old element; restating one takes it
// over."
//
// Every patch fragment is wrapped <Gooey><X Name="…">, so the root's
// syntactic parent is always <Gooey>. Scoping attached properties
// against that rejected every restated layout attribute, for every
// element — the feature, entirely.
func TestPatchFragmentMayRestateAttachedProperties(t *testing.T) {
	ctx := &Context{}
	for _, src := range []string{
		`<Gooey><Text Name="B" Grid.Col="0">moved</Text></Gooey>`,
		`<Gooey><Text Name="B" Grid.Row="2" Grid.RowSpan="3">moved</Text></Gooey>`,
		`<Gooey><Text Name="B" Canvas.Left="4" Canvas.Top="2">moved</Text></Gooey>`,
	} {
		if _, err := Build([]byte(src), ctx); err != nil {
			t.Errorf("a fragment restating its layout was rejected: %v\n%s", err, src)
		}
	}
}

// TestPageRootMayCarryAttachedProperties — the same code path, and it
// was broken too. Build cannot tell a fragment from a whole page,
// because they are the same syntax, so a swap of a page whose root
// carried an attached property failed identically.
func TestPageRootMayCarryAttachedProperties(t *testing.T) {
	src := `<Gooey><VStack Name="Root" Grid.Row="1"><Text>x</Text></VStack></Gooey>`
	if _, err := Build([]byte(src), &Context{}); err != nil {
		t.Errorf("a page root carrying an attached property was rejected: %v", err)
	}
}

// TestAttachedScopingStillHoldsBelowTheRoot — the fix suspends the rule
// only where there is no parent to check. Everywhere else it must still
// catch the misplacement, or the defect the rule exists for is back.
func TestAttachedScopingStillHoldsBelowTheRoot(t *testing.T) {
	_, err := Build([]byte(`<Gooey><VStack Name="R"><Text Canvas.Left="10">x</Text></VStack></Gooey>`), &Context{})
	if err == nil {
		t.Fatal("Canvas.Left under a <VStack> must still be rejected: applyLayout would drop it in silence")
	}
	if !strings.Contains(err.Error(), "contributed by a <Canvas> parent") {
		t.Errorf("error = %v", err)
	}
}

// TestRootStillRejectsUnknownAttributes — only the misplaced-attached
// rule is suspended at the root. A genuine typo there is still an error.
func TestRootStillRejectsUnknownAttributes(t *testing.T) {
	_, err := Build([]byte(`<Gooey><Text Name="B" Lef="2">x</Text></Gooey>`), &Context{})
	if err == nil {
		t.Fatal("an unknown attribute on a fragment root must still be rejected")
	}
	if !strings.Contains(err.Error(), "no such attribute") {
		t.Errorf("error = %v", err)
	}
}

// TestIncludeInstancesAreNotParentScoped records a PRE-EXISTING GAP,
// found while pinning that this fix did not widen to the include path.
//
// An include instance is validated by declarations.checkAttrs, which
// skips every layoutAttr outright — so <Card Grid.Col="0"/> is accepted
// wherever it appears, including under a <VStack> where applyLayout
// will silently drop it. That is the same silent-drop defect the
// attribute work exists to delete, still open on this one path.
//
// Asserted as it BEHAVES rather than as it should, so the gap is
// visible and this test does not pretend to guard something it does
// not. Closing it means teaching that path the parent — the same
// information the root case needs and cannot get at this layer.
func TestIncludeInstancesAreNotParentScoped(t *testing.T) {
	ctx := &Context{Includes: fstest.MapFS{
		"card.gooey": &fstest.MapFile{Data: []byte(`<Gooey><Text>card</Text></Gooey>`)},
	}}
	if _, err := Build([]byte(`<Gooey><Grid Rows="Auto" Cols="1*"><Card Grid.Col="0"/></Grid></Gooey>`), ctx); err != nil {
		t.Errorf("an include under a <Grid> may carry Grid.Col: %v", err)
	}
	// The gap: this SHOULD be an error and is not. If it starts
	// failing, the gap was closed — update the comment, keep the fix.
	if _, err := Build([]byte(`<Gooey><VStack Name="R"><Card Grid.Col="0"/></VStack></Gooey>`), ctx); err != nil {
		t.Errorf("include scoping changed (the recorded gap may be closed): %v", err)
	}
}
