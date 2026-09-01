package main

import (
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/activitybar"
	"github.com/WonderForgeLabs/gooey/render"
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

// TestEveryRailSlotsHeaderReachesTheCellPlane is the half the test above
// cannot do, and the gap was not theoretical: the DOCS tab shipped with a
// header that never reached the screen.
//
// The test above reads headers out of the markup SOURCE with
// encoding/xml. That answers "is the fifth tab declared" and cannot
// answer "can anybody see it" — and since #409 clipped every component
// to its own bounds, the difference is the whole failure. The side bar
// lives in a DockPane of a DECLARED width, four headers already filled
// it, and the fifth was cut off silently at every terminal size. The tab
// stayed reachable from the activity rail; the side bar simply offered
// no evidence it existed.
//
// So this one reads the strip back off a composed frame with
// render.RowText, and it is derived from railOrder rather than from a
// list of its own — the sixth tab is red here before it is invisible in
// front of somebody.
//
// The size is 160x48, the same the other page tests compose at. Width is
// what matters and the pane's width is a CONSTANT, so a bigger terminal
// does not rescue a header that does not fit.
func TestEveryRailSlotsHeaderReachesTheCellPlane(t *testing.T) {
	_, root := buildPage(t)
	comp := gooey.NewComposer(root, 160, 48)
	f, _ := comp.Frame()

	// The strip is whichever row carries the first slot's header. Found
	// rather than hardcoded: the dock is movable and the row is not a
	// property of this test.
	first := railOrder[activitybar.DefaultIcons[0].Name]
	strip, at := "", -1
	for y := 0; y < 48; y++ {
		if s := render.RowText(f.Cells, y); strings.Contains(s, first) {
			strip, at = strings.TrimRight(s, " "), y
			break
		}
	}
	if at < 0 {
		t.Fatalf("no row on the composed frame carries %q; the side bar's tab "+
			"strip is not on screen at all", first)
	}
	for i, ic := range activitybar.DefaultIcons {
		want := railOrder[ic.Name]
		if !strings.Contains(strip, want) {
			t.Errorf("rail slot %d (%q) selects the tab headed %q, and that header "+
				"is not on the cell plane. Row %d reads\n\t%s\nThe pane is clipped "+
				"to a declared width, so no terminal size fixes this: the slot is "+
				"reachable only from the rail, with nothing in the side bar saying "+
				"it exists", i, ic.Name, want, at, strip)
		}
	}
}

// TestTheDocsReaderOutgrowsItsList pins the ORDERING that the DOCS tab's
// Rows="5,*" comment claims — "the list is for finding a page and the
// page is for reading it, so the reader gets whatever is left" — which
// was false as shipped: at Rows="8,*" the reader got six lines against
// the list's eight.
//
// An ordering and not a count, deliberately. The absolute numbers are a
// function of every pane sharing the LEFT slot and of the terminal's
// height, so pinning "nine lines" would go red on an unrelated change to
// EXPLORER and teach the next reader to update it. What must not silently
// invert is which of the two halves is bigger.
//
// BOTH SIDES ARE COUNTED OFF THE CELL PLANE, and the first version of
// this test compared the body's rows against len(docList) instead —
// which is the number of PAGES, not the height of the track showing
// them. With a one-page fixture that read 6 > 1 and passed against the
// very split it was written to reject. So the fixture has more pages
// than the track can hold, and the track's height is what gets counted.
func TestTheDocsReaderOutgrowsItsList(t *testing.T) {
	ed, root := buildPage(t)
	comp := gooey.NewComposer(root, 160, 48)
	comp.Frame()

	// More pages than any plausible track, so the list fills its own
	// height and the count below measures the TRACK rather than the
	// fixture. Distinct one-per-row labels, so a row is countable.
	pages := make([]docPage, 40)
	files := fstest.MapFS{}
	for i := range pages {
		name := "PAGE" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".md"
		pages[i] = docPage{Path: name, Label: name}
		files[name] = &fstest.MapFile{Data: []byte(strings.Repeat("BODYLINE\n", 600))}
	}
	ed.docsRoot.Set(files)
	ed.docList.Set(pages)
	ed.docsSel.Set(0)
	ed.activitySel.Set(4)
	f, _ := comp.Frame()

	body, list := 0, 0
	for y := 0; y < 48; y++ {
		s := render.RowText(f.Cells, y)
		if strings.Contains(s, "BODYLINE") {
			body++
		}
		if strings.Contains(s, "PAGE") {
			list++
		}
	}
	if body == 0 || list == 0 {
		t.Fatalf("the DOCS tab shows %d body lines and %d list rows; it is not "+
			"showing a page at all", body, list)
	}
	if body <= list {
		t.Errorf("the reader shows %d lines and the list %d rows. The pane's own "+
			"comment says the list is for finding a page and the page is for "+
			"reading it, so the reader gets whatever is left — that is the "+
			"minority share, and the split has inverted", body, list)
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
