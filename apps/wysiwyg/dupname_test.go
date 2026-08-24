package main

import (
	"strings"
	"testing"
)

// The palette names a new element `<Elem><len(Kids)+1>`, which is a
// COUNTER DERIVED FROM THE CURRENT LENGTH rather than from what is
// already in use. Delete from the middle and the length goes back down,
// so the next add re-issues a name that is still on a sibling.
//
// This test exists to establish whether that is a real defect or a
// harmless one, because the answer depends on what the framework does
// with two identically-named siblings — and "Name" is what markup.Find
// resolves against.
func TestAddingAfterADeleteDoesNotReissueALiveName(t *testing.T) {
	ed, c, _ := designerPageCounting(t)
	ed.doc().Elem = "VStack"
	ed.doc().Attrs = map[string]string{"Name": "Root"}
	ed.doc().Kids = nil
	ed.rebuild()
	c.Frame()

	// Select the Text entry in the palette so addSelected adds Texts.
	idx := -1
	for i, e := range ed.palette {
		if e.Name == "Text" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skip("no <Text> in the palette; this fixture cannot be built")
	}
	ed.paletteSel.Set(idx)
	ed.sel = ed.doc()

	for i := 0; i < 3; i++ {
		ed.addSelected()
		ed.sel = ed.doc()
	}
	if got := kidNames(ed.doc()); got != "Text1,Text2,Text3" {
		t.Fatalf("after three adds the names are %q, want \"Text1,Text2,Text3\"; "+
			"the rest of this test assumes that scheme", got)
	}

	// Delete the MIDDLE one. The length drops to 2.
	ed.sel = ed.doc().Kids[1]
	ed.deleteSelected()
	if got := kidNames(ed.doc()); got != "Text1,Text3" {
		t.Fatalf("after deleting the middle the names are %q, want \"Text1,Text3\"", got)
	}

	// Add again: len(Kids)+1 == 3, and Text3 is still present.
	ed.sel = ed.doc()
	ed.addSelected()

	names := map[string]int{}
	for _, k := range ed.doc().Kids {
		names[k.Attrs["Name"]]++
	}
	for name, n := range names {
		if n > 1 {
			t.Errorf("%d siblings are called %q — the palette re-issued a name that was "+
				"still in use, because it counts children instead of consulting them. "+
				"Name is what markup.Find resolves against, so the second one is "+
				"unaddressable: full outline\n%s", n, name, ed.outline())
		}
	}
	if !strings.HasPrefix(ed.status.Get(), "✓") {
		t.Errorf("the document does not build after the duplicate name: %s", ed.status.Get())
	}
}
