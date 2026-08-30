package main

import (
	"encoding/xml"
	"io"
	"os"
	"strings"
	"testing"
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
// here: every top-level <Menu Title="_X"> contributes the mnemonic alt+x,
// and every root-scoped <KeyBinding Gesture="alt+x"> is a collision with
// it. Adding a menu whose letter is already bound fails here, which is
// the direction that will actually happen — the bindings are older than
// the menus that will be added next to them.
func TestNoRootKeyBindingShadowsAMenuTitleMnemonic(t *testing.T) {
	src, err := os.ReadFile("wysiwyg.gooey")
	if err != nil {
		t.Fatal(err)
	}

	dec := xml.NewDecoder(strings.NewReader(string(src)))
	depth := 0
	menuDepth := -1
	mnemonics := map[string]string{} // letter -> menu title
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
			switch e.Name.Local {
			case "Menu":
				// TOP-LEVEL menus only: a nested one is a submenu whose
				// mnemonic is scoped to its open parent, not to the page.
				if menuDepth == -1 {
					menuDepth = depth
					if l, ok := mnemonicLetter(xmlAttr(e, "Title")); ok {
						mnemonics[l] = xmlAttr(e, "Title")
					}
				}
			case "KeyBinding":
				g := xmlAttr(e, "Gesture")
				if strings.HasPrefix(g, "alt+") && len(g) == len("alt+")+1 {
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
		t.Fatalf("found %d menu mnemonics and %d single-letter alt bindings; "+
			"with either at zero this test cannot fail and proves nothing",
			len(mnemonics), len(altBindings))
	}

	for _, g := range altBindings {
		letter := strings.ToLower(strings.TrimPrefix(g, "alt+"))
		if title, clash := mnemonics[letter]; clash {
			t.Errorf("a KeyBinding on %q collides with the %q menu's mnemonic. "+
				"Dispatch offers bindings before mnemonics, so the binding wins "+
				"and the menu silently stops opening — move one of them",
				g, title)
		}
	}
	t.Logf("checked %d alt bindings against %d menu mnemonics",
		len(altBindings), len(mnemonics))
}

// mnemonicLetter reads the accelerator out of a "_File"-style title: the
// character after the first underscore, lowercased.
func mnemonicLetter(title string) (string, bool) {
	i := strings.Index(title, "_")
	if i < 0 || i+1 >= len(title) {
		return "", false
	}
	return strings.ToLower(string(title[i+1])), true
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
