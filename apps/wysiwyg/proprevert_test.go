package main

import (
	"strings"
	"testing"
)

// TestARefusedPropertyValueIsRevertedNotLeftBroken is #531. commitEdit
// wrote the value and rebuilt, and nothing looked at the result: a value
// the loader refuses left docRoot nil — click-to-select dead for the
// whole document — and the next insert's backstop was blamed for it.
func TestARefusedPropertyValueIsRevertedNotLeftBroken(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.sel = ed.doc().Kids[0]
	before := ed.sel.Attrs["Canvas.Top"]
	editAttr(t, ed, "Canvas.Top", "not-a-number")

	if ed.docRoot == nil {
		t.Fatal("a refused value left the document unbuilt: click-to-select is dead " +
			"for the whole document and nothing reverted it")
	}
	if got := ed.sel.Attrs["Canvas.Top"]; got != before {
		t.Errorf("Canvas.Top = %q after a refused edit, want the value it had (%q)", got, before)
	}
	st := ed.status.Get()
	if !strings.HasPrefix(st, "✗ Canvas.Top on <") || !strings.Contains(st, "was not changed") {
		t.Errorf("status = %q, want it to name the refused attribute", st)
	}
	// And the refusal is not an undo step: one ctrl+z must not walk back
	// into the broken state.
	ed.undo()
	if ed.docRoot == nil {
		t.Error("ctrl+z after a refused edit restored the refused value")
	}
}

// TestAnAlreadyBrokenDocumentCanBeFixedFromThePane is the condition the
// revert carries that the other six seams do not: the pane is how an
// author repairs a document that does not build, so a write into one
// must stand even while the build still fails.
func TestAnAlreadyBrokenDocumentCanBeFixedFromThePane(t *testing.T) {
	ed, _ := undoFixture(t)
	ed.sel = ed.doc().Kids[0]
	ed.sel.Attrs["Canvas.Top"] = "broken"
	ed.rebuild()
	if ed.docRoot != nil {
		t.Fatal("the fixture's bad value built; this test cannot discriminate")
	}
	editAttr(t, ed, "Canvas.Top", "3")
	if got := ed.sel.Attrs["Canvas.Top"]; got != "3" {
		t.Errorf("Canvas.Top = %q, want the repair to stand", got)
	}
	if ed.docRoot == nil {
		t.Errorf("the repaired document does not build: %s", ed.status.Get())
	}
}
