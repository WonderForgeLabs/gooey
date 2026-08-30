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
// COUNTING ALONE IS NOT ENOUGH, and the first version of this test did
// exactly that — while its own comment said "a reordering of either side
// is silent". Swapping two tabs keeps the count and breaks every slot
// between them, so the assertion missed the failure it described. Caught
// in review of #426, which is the review of this test.
//
// Order needs a spelling both sides can be checked against, and the two
// vocabularies are not one: the rail says "designer", the tab header
// says "DESIGN". railOrder below is that mapping, RECORDED — it is the
// one place the correspondence is written down at all, which is what
// makes it worth having rather than a third copy. Changing a slot means
// changing it here, and that is the point: the edit is the review.
func TestTheRailAndTheSideBarAgreeSlotForSlot(t *testing.T) {
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}

	dec := xml.NewDecoder(strings.NewReader(string(src)))
	depth := 0
	inTabs := -1 // the depth of the ActivitySel-bound <Tabs>, or -1
	found := 0
	var headers []string
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
				headers = append(headers, attr(e, "Header"))
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
	if want := len(activitybar.DefaultIcons); len(headers) != want {
		t.Fatalf("the rail declares %d slots and the side bar declares %d tabs "+
			"bound to the same selection. They are matched by INDEX, so the "+
			"extra one on either side is unreachable: rail %v, tabs %v",
			want, len(headers), activitybar.DefaultIcons, headers)
	}
	if len(railOrder) != len(activitybar.DefaultIcons) {
		t.Fatalf("railOrder maps %d slots and the rail declares %d; the mapping "+
			"has to be updated in the same change as the rail",
			len(railOrder), len(activitybar.DefaultIcons))
	}
	for i, ic := range activitybar.DefaultIcons {
		want, ok := railOrder[ic.Name]
		if !ok {
			t.Errorf("rail slot %d is %q, which railOrder does not map to a tab "+
				"header", i, ic.Name)
			continue
		}
		if headers[i] != want {
			t.Errorf("rail slot %d is %q, which selects tab %d — but tab %d is "+
				"%q and %q wants %q. The rail and the side bar are matched by "+
				"index, so this slot opens the wrong pane",
				i, ic.Name, i, i, headers[i], ic.Name, want)
		}
	}
}

// railOrder is the correspondence between a rail slot and the side bar
// tab it selects, and it exists because nothing else in the tree writes
// it down. The rail names what a slot MEANS ("toolbox") and the tab
// header names what it SHOWS ("TOOLS"); they are matched by position and
// by nothing else.
var railOrder = map[string]string{
	"designer": "DESIGN",
	"toolbox":  "TOOLS",
	"markup":   "CODE",
	"problems": "ISSUES",
	"docs":     "DOCS",
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
