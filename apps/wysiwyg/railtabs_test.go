package main

import (
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/activitybar"
)

// railSelBinding is the expression both the rail and the side bar's Tabs
// are bound to. Sharing one property is what makes clicking slot N show
// pane N — and it is also what makes the correspondence POSITIONAL, with
// nothing but this test to hold it.
const railSelBinding = "{{.ActivitySel}}"

// TestTheRailAndTheSideBarAgreeOnHowManySlotsThereAre closes the seam a
// fifth rail slot had to be threaded through twice.
//
// activitybar.DefaultIcons declares the rail, and the activitybar tests
// derive everything from it — which covers the rail's own rendering and
// stops exactly here. The side bar's <Tabs> is bound to the same
// ActivitySel, so rail slot N selects tab N by index and by nothing
// else: a sixth icon, or a reordering of either side, opens a tab
// nobody can reach or reaches a tab nobody named, silently.
//
// Counting is the assertion the coupling actually supports. The two
// vocabularies are not one — the rail says "designer", the tab says
// "DESIGN" — so a name comparison would be a second spelling to keep in
// sync rather than a check. Found in review of #426.
func TestTheRailAndTheSideBarAgreeOnHowManySlotsThereAre(t *testing.T) {
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}

	dec := xml.NewDecoder(strings.NewReader(string(src)))
	depth := 0
	inTabs := -1 // the depth of the ActivitySel-bound <Tabs>, or -1
	found := 0
	tabs := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("the shipped page does not parse as XML: %v", err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			depth++
			switch {
			case e.Name.Local == "Tabs" && attr(e, "Selected") == railSelBinding:
				found++
				inTabs = depth
			// DIRECT children only. The page holds a second <Tabs> — the
			// bottom panel's — and every tab body may hold arbitrary
			// nesting, so a bare name match would count whatever those
			// contain.
			case e.Name.Local == "Tab" && inTabs >= 0 && depth == inTabs+1:
				tabs++
			}
		case xml.EndElement:
			if inTabs == depth {
				inTabs = -1
			}
			depth--
		}
	}

	if found != 1 {
		t.Fatalf("%d <Tabs> are bound to %s, want exactly 1 — this test counts "+
			"the children of that one element and cannot mean anything otherwise",
			found, railSelBinding)
	}
	if want := len(activitybar.DefaultIcons); tabs != want {
		t.Errorf("the rail declares %d slots and the side bar declares %d tabs "+
			"bound to the same selection. They are matched by INDEX, so the "+
			"extra one on either side is unreachable: %v",
			want, tabs, activitybar.DefaultIcons)
	}
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
