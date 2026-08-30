package main

import (
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/components"
)

// TestNoRootKeyBindingShadowsAMenuTitleMnemonic is the guard for the
// hazard the alt+ move cluster introduced.
//
// Dispatch offers a KeyBinding the event BEFORE the mnemonics get their
// turn, and a binding on the page root is on every path — so a root
// `alt+h` beats a `_Help` menu to alt+h, and the menu simply stops
// opening. Nothing is a load error and nothing goes red: two claims on
// one gesture, resolved by dispatch order, exactly the silent-collision
// class #427 is about.
//
// The check is derived from the shipped page rather than from a list
// here: every top-level <Menu> contributes its accelerator as alt+<that
// letter>, and every root-scoped <KeyBinding Gesture="alt+x"> is a
// collision with it. Adding a menu whose letter is already bound fails
// here, which is the direction that will actually happen — the bindings
// are older than the menus that will be added next to them.
//
// # Two ways this guard was blind, both found in review of #428
//
// THE LETTER CAME FROM A LOCAL RULE, not the framework's. It required an
// explicit "_" marker; components.splitMnemonic falls back to the first
// letter or digit, because "a menu bar without accelerators is broken
// furniture". So <Menu Title="Help"> — the likelier spelling, given the
// fallback — claimed alt+h, lost it to the root binding, and this test
// stayed green about the exact collision it is named for. It also read
// "__Tools" (an escaped underscore) as claiming '_' and indexed BYTES,
// so "_Über" produced half a UTF-8 sequence. It now asks
// components.MenuMnemonic, which is a window onto the same splitMnemonic
// the painter and the dispatcher use: a second copy of this rule is the
// defect, not the fix.
//
// AND "ROOT-SCOPED" WAS NOT CHECKED. The <Menu> arm tracked depth to
// reject submenus while the <KeyBinding> arm collected every binding in
// the file. That is not the docstring's premise: a KeyBinding is scoped
// by its HOST component and only fires while the focused chain passes
// through it, so a pane-scoped alt+<letter> is not on every path and is
// not a shadow. Every binding in the page is a root child today, so this
// changes nothing now and stops a future pane-scoped binding being
// reported as a collision it isn't.
func TestNoRootKeyBindingShadowsAMenuTitleMnemonic(t *testing.T) {
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}

	dec := xml.NewDecoder(strings.NewReader(string(src)))
	depth := 0
	menuDepth := -1
	// hostDepth is the depth of the page's root COMPONENT — the single
	// element inside <Gooey>. A KeyBinding one level below it is scoped
	// to the root and therefore on every path; anything deeper belongs to
	// a pane.
	hostDepth := -1
	mnemonics := map[rune]string{} // accelerator -> menu title
	var altBindings []string
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
			if depth == 2 && hostDepth == -1 && e.Name.Local != "Gooey" {
				hostDepth = depth
			}
			switch e.Name.Local {
			case "Menu":
				// TOP-LEVEL menus only: a nested one is a submenu whose
				// mnemonic is scoped to its open parent, not to the page.
				if menuDepth == -1 {
					menuDepth = depth
					title := xmlAttr(e, "Title")
					if a, ok := components.MenuMnemonic(title); ok {
						mnemonics[a] = title
					}
				}
			case "KeyBinding":
				if depth != hostDepth+1 {
					continue // pane-scoped: not on every path
				}
				g := xmlAttr(e, "Gesture")
				// RUNES, not bytes: alt+ü is one key and two bytes, and a
				// byte-length test drops it from the sweep.
				if rest, ok := strings.CutPrefix(g, "alt+"); ok && len([]rune(rest)) == 1 {
					altBindings = append(altBindings, g)
				}
			}
		case xml.EndElement:
			if menuDepth == depth {
				menuDepth = -1
			}
			depth--
		}
	}

	if len(mnemonics) == 0 || len(altBindings) == 0 {
		t.Fatalf("found %d menu mnemonics and %d single-letter root alt bindings; "+
			"with either at zero this test cannot fail and proves nothing",
			len(mnemonics), len(altBindings))
	}

	for _, g := range altBindings {
		letter := []rune(strings.ToLower(strings.TrimPrefix(g, "alt+")))[0]
		if title, clash := mnemonics[letter]; clash {
			t.Errorf("a KeyBinding on %q collides with the %q menu's mnemonic. "+
				"Dispatch offers bindings before mnemonics, so the binding wins "+
				"and the menu silently stops opening — move one of them",
				g, title)
		}
	}
	t.Logf("checked %d root alt bindings against %d menu mnemonics",
		len(altBindings), len(mnemonics))
}

// TestTheGuardReadsMnemonicsTheWayTheFrameworkDoes is the discrimination
// half, and it is what the local rule could not have: the four spellings
// where a hand-written "character after the underscore" disagrees with
// what a running MenuBar actually accelerates.
//
// Every row is a title the guard above may meet. A guard that agrees
// with the framework on all four is one whose green means something.
func TestTheGuardReadsMnemonicsTheWayTheFrameworkDoes(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  rune
	}{
		{"_File", 'f'},
		{"E_xit", 'x'},
		// NO MARKER — the fallback, and the case the old guard could not
		// see at all.
		{"Help", 'h'},
		// AN ESCAPED underscore is a literal, so the accelerator falls
		// back to the first letter. The old guard answered '_'.
		{"__Tools", 't'},
		// NON-ASCII, where byte indexing returned half a sequence.
		{"_Über", 'ü'},
		{"Über", 'ü'},
		// A digit is an accelerator too.
		{"3D", '3'},
	} {
		got, ok := components.MenuMnemonic(tc.title)
		if !ok {
			t.Errorf("MenuMnemonic(%q) claims no accelerator; the framework "+
				"accelerates it with %q", tc.title, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("MenuMnemonic(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
	// And a title with nothing to accelerate really does report none, or
	// the loop above passes against a function that always answers true.
	if _, ok := components.MenuMnemonic("!!!"); ok {
		t.Error("a title with no letter or digit claims an accelerator")
	}
}

// xmlAttr reads one attribute off a start element. Named apart from the
// docs-tab branch's identically-shaped helper so the two can land in
// either order.
func xmlAttr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
