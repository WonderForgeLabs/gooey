package main

// The ground the panel hairline is composited against, asserted rather
// than described.
//
// components/panel draws its rule as a dimmer OPAQUE colour on the sixel
// tier, because sixel drops a translucent stroke outright (#254). That
// colour is fg composited onto a ground, and the ground is a guess:
// black, justified by "no ancestor of a <Panel> declares a Background".
// The justification is a fact about wysiwyg.gooey and the components
// around it, so it lives here, where changing either can go red.

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/apps/wysiwyg/components/panel"
)

// TestNoAncestorOfAPanelDeclaresABackground is the claim the opaque
// tier's ground rests on, moved out of prose.
//
// components/panel's over() composites the hairline against BLACK when a
// pane has no ground to composite against, and the justification for
// that guess — written at panel.go and twice in panel_test.go — is that
// no ancestor of a <Panel> in this app declares a Background. It is true
// today: components/activitybar declares the one Background in the app,
// on the rail's VStack, whose only child is the rail image.
//
// It is also a claim about a MARKUP FILE, which anybody can change
// without going near the panel package. Writing `Background=` on a dock
// container would make the guess wrong for every pane under it, in a
// colour nobody chose, and the three comments asserting otherwise would
// go on saying it. Raised in review of #474.
//
// THE WALK IS FROM THE ROOT, not from each Panel upward, because
// ChildComponents only goes down — so the ancestor chain is what this
// carries as it descends. gooey.Container is the seam; a component that
// is not one has no children to reach a Panel through.
//
// AND THE TYPE ASSERTION IS NOT THE QUESTION, which the first version of
// this test got wrong and said so loudly: every <Panel> in the shell has
// a *components.Grid ancestor, and Grid implements gooey.HasBackground
// unconditionally — it has the field, so it has the method, whether or
// not anybody wrote Background= on it. The interface says "this
// container CAN own a surface". The handle says whether it does:
// backgroundProp's own doc is "a nil handle means the container has no
// background", and a non-nil handle whose colour is UNSET fills with the
// nearest ancestor's, which is not a new ground either. So the predicate
// is a set colour, and `implements HasBackground` would have failed here
// against five panes that are perfectly fine.
func TestNoAncestorOfAPanelDeclaresABackground(t *testing.T) {
	_, root := buildPage(t)

	var panels int
	var walk func(c gooey.Component, ancestors []gooey.Component)
	walk = func(c gooey.Component, ancestors []gooey.Component) {
		if _, ok := c.(*panel.Pane); ok {
			panels++
			for _, a := range ancestors {
				if declaresGround(a) {
					t.Errorf("%T is an ancestor of a <Panel> and declares a "+
						"Background. components/panel's over() composites the "+
						"hairline against BLACK on the opaque tier because no "+
						"ancestor does — with one, every pane under it draws its "+
						"rule against a ground nobody chose, and the comments at "+
						"panel.go and panel_test.go still say otherwise", a)
				}
			}
		}
		kids, ok := c.(gooey.Container)
		if !ok {
			return
		}
		for _, k := range kids.ChildComponents() {
			walk(k, append(ancestors, c))
		}
	}
	walk(root, nil)

	// NON-VACUITY. A walk that stopped descending — a container seam
	// that changed, a page that stopped building its regions — reports
	// no problem, which is what this assertion is worth without a floor.
	if panels == 0 {
		t.Fatal("the walk found no <Panel> at all, so it checked no ancestor " +
			"chain. The claim it exists to hold is about panes that are there")
	}
	t.Logf("checked the ancestors of %d panes", panels)
}

// TestTheOneDeclaredBackgroundIsStillThere is the other half, and
// without it the test above passes for a page that declares no
// Background anywhere — including the rail's, whose absence would be a
// visual regression this suite would otherwise wave through.
//
// It is also what stops the walk being read as "backgrounds are banned
// here". One is deliberate, documented at its declaration, and outside
// every Panel's ancestry.
// declaresGround is "this container puts a colour behind its subtree",
// which is narrower than implementing gooey.HasBackground and narrower
// again than holding a handle. See the note on the walk above.
func declaresGround(c gooey.Component) bool {
	hb, ok := c.(gooey.HasBackground)
	if !ok {
		return false
	}
	p := hb.BackgroundProperty()
	return p != nil && p.Get().Set
}

func TestTheOneDeclaredBackgroundIsStillThere(t *testing.T) {
	_, root := buildPage(t)

	var found int
	var walk func(c gooey.Component)
	walk = func(c gooey.Component) {
		if declaresGround(c) {
			found++
		}
		if kids, ok := c.(gooey.Container); ok {
			for _, k := range kids.ChildComponents() {
				walk(k)
			}
		}
	}
	walk(root)

	if found == 0 {
		t.Error("nothing in the shell declares a Background any more. The rail's " +
			"VStack declares one so the column below its icons carries the rail's " +
			"own ground rather than the page's (components/activitybar) — and with " +
			"none at all, TestNoAncestorOfAPanelDeclaresABackground holds " +
			"vacuously")
	}
}
