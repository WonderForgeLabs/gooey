package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// leaf is a distinguishable child. Nothing about it matters except that
// two of them are different pointers.
type csLeaf struct{ gooey.Base }

func (l *csLeaf) Measure(a gooey.Size) gooey.Size { return gooey.Size{} }
func (l *csLeaf) Render(*gooey.Frame)             {}

// TestEveryChildSetterAddressesTheListTheFrameworkWalked derives the
// interface's one rule instead of trusting the roster in childset.go.
//
// That rule is: for a ChildSetter, ChildComponents returns the child
// slice ITSELF, so the index the framework walked with and the index
// SetChild writes are the same address. The containers that must NOT
// implement it are the ones whose ChildComponents BUILDS a list — index
// i there is a position in a temporary, so a patch would land in the
// wrong slot or in a slice the next Measure throws away.
//
// The comment in childset.go states all of this and nothing enforced it.
// A container that grew a SetChild would be silently patchable into the
// wrong slot, and a container that stopped returning its field would
// break the rule from the other side.
//
// So this asserts the PROPERTY, per container, by writing through
// SetChild and reading back through ChildComponents — the same two doors
// control.PatchMarkup uses. A container that builds its list fails
// because the write lands in the temporary and the read does not see it.
func TestEveryChildSetterAddressesTheListTheFrameworkWalked(t *testing.T) {
	a, b := &csLeaf{}, &csLeaf{}

	// Every container in this package that implements ChildSetter, with
	// two children and the index to overwrite. Listed by CONSTRUCTION
	// rather than by name: each entry has to be built to be exercised,
	// and a container missing from here is caught by the converse test
	// below rather than by a name comparison.
	cases := []struct {
		name string
		make func() gooey.Component
		idx  int
	}{
		{"VStack", func() gooey.Component {
			return &VStack{Children: []gooey.Component{a, b}}
		}, 1},
		{"HStack", func() gooey.Component {
			return &HStack{Children: []gooey.Component{a, b}}
		}, 1},
		{"Grid", func() gooey.Component {
			return &Grid{Children: []gooey.Component{a, b}}
		}, 1},
		{"Canvas", func() gooey.Component {
			return &Canvas{Children: []gooey.Component{a, b}}
		}, 1},
		{"ButtonBar", func() gooey.Component {
			return &ButtonBar{Children: []gooey.Component{a, b}}
		}, 1},
		{"Border", func() gooey.Component { return &Border{Child: a} }, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.make()
			cs, ok := c.(gooey.ChildSetter)
			if !ok {
				t.Fatalf("%s is in the ChildSetter table and does not implement it",
					tc.name)
			}
			cont, ok := c.(gooey.Container)
			if !ok {
				t.Fatalf("%s implements ChildSetter without being a Container",
					tc.name)
			}
			if n := len(cont.ChildComponents()); n <= tc.idx {
				t.Fatalf("the %s fixture has %d children, so index %d is not "+
					"an address to test", tc.name, n, tc.idx)
			}

			repl := &csLeaf{}
			if !cs.SetChild(tc.idx, repl) {
				t.Fatalf("SetChild(%d) refused an index its own "+
					"ChildComponents offers", tc.idx)
			}
			if got := cont.ChildComponents()[tc.idx]; got != gooey.Component(repl) {
				t.Errorf("after SetChild(%d), ChildComponents()[%d] is %p, want "+
					"%p — the write went somewhere the framework's walk does "+
					"not read, which is a patch landing in a temporary",
					tc.idx, tc.idx, got, repl)
			}
			// Out of range must be refused rather than panic or grow.
			if cs.SetChild(-1, repl) || cs.SetChild(len(cont.ChildComponents())+1, repl) {
				t.Error("SetChild accepted an index outside the list")
			}
		})
	}
}

// TestContainersThatBuildTheirListRefuseToBeChildSetters is the converse,
// and it is the half that goes red when somebody adds a SetChild to the
// wrong container.
//
// These all interleave chrome with content or realize rows on demand, so
// index i in their ChildComponents is a position in a value that did not
// exist a moment ago. The refusal IS the safety: control.PatchMarkup
// reports it by type name, and a SetChild added here would replace that
// clear error with a patch that silently lands in the wrong place.
//
// Written as typed nils so it costs no construction: implementing an
// interface is a property of the TYPE, and a nil pointer of that type
// answers the assertion exactly. It also means a renamed or deleted type
// stops the test COMPILING, which is the failure the prose roster in
// childset.go could not produce.
func TestContainersThatBuildTheirListRefuseToBeChildSetters(t *testing.T) {
	refusers := []struct {
		name string
		v    gooey.Component
	}{
		{"ItemsView", (*ItemsView)(nil)},
		{"Tabs", (*Tabs)(nil)},
		{"MenuBar", (*MenuBar)(nil)},
		{"StatusBar", (*StatusBar)(nil)},
		{"Segmented", (*Segmented)(nil)},
		{"AdornmentLayer", (*AdornmentLayer)(nil)},
		{"ToastHost", (*ToastHost)(nil)},
	}
	for _, r := range refusers {
		if _, ok := r.v.(gooey.ChildSetter); ok {
			t.Errorf("%s implements gooey.ChildSetter, but its "+
				"ChildComponents BUILDS its list — index i is a position in "+
				"a temporary, so a patch through it lands in the wrong slot "+
				"or in a slice the next Measure discards. See childset.go.",
				r.name)
		}
	}
	// Discrimination: the assertion above passes trivially if the
	// interface is unsatisfiable, so prove a real container answers yes.
	var yes gooey.Component = &VStack{}
	if _, ok := yes.(gooey.ChildSetter); !ok {
		t.Fatal("no container satisfies gooey.ChildSetter, so the refusals " +
			"above assert nothing")
	}
}
