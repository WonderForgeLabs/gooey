package markup

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
)

// TestEachStackElementBuildsItsOwnType pins the one thing the stack
// definitions were NOT asserting anywhere, which is why a dead branch survived
// in both of them.
//
// defVStack and defHStack each carried `if e.Name == "VStack"`, constructing an
// HStack first and swapping it for a VStack when the name matched. Build is
// reached only through elementDefs[e.Name] (markup.go:802), and
// registerElements panics on a duplicate name, so e.Name inside a definition is
// that definition's own name and nothing else: the test was ALWAYS TRUE in
// defVStack — where the HStack it built first was allocated and thrown away
// every time — and ALWAYS FALSE in defHStack, where it was unreachable code
// that happened to be correct only by never running.
//
// Nothing caught that, because no test asserted the concrete type of an
// <HStack> at all: `grep '\*components.HStack' markup/` found zero hits before
// this file. Deleting a branch nothing pins is how the two ends up swapped, so
// what is asserted here is the concrete type of each, in both directions.
func TestEachStackElementBuildsItsOwnType(t *testing.T) {
	for _, c := range []struct {
		element string
		check   func(*testing.T, gooey.Component)
	}{
		{"VStack", func(t *testing.T, w gooey.Component) {
			s, ok := w.(*components.VStack)
			if !ok {
				t.Fatalf("<VStack> built %T, want *components.VStack", w)
			}
			if s.Gap != 2 || len(s.Children) != 1 {
				t.Errorf("Gap = %d, children = %d; want 2 and 1", s.Gap, len(s.Children))
			}
		}},
		{"HStack", func(t *testing.T, w gooey.Component) {
			s, ok := w.(*components.HStack)
			if !ok {
				t.Fatalf("<HStack> built %T, want *components.HStack", w)
			}
			if s.Gap != 2 || len(s.Children) != 1 {
				t.Errorf("Gap = %d, children = %d; want 2 and 1", s.Gap, len(s.Children))
			}
		}},
	} {
		t.Run(c.element, func(t *testing.T) {
			src := `<Gooey xmlns="wonderforge.io/gooey/2026">
			  <` + c.element + ` Gap="2"><Text>x</Text></` + c.element + `>
			</Gooey>`
			c.check(t, buildOne(t, src, &Context{}))
		})
	}
}
