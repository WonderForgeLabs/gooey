package components

import "github.com/WonderForgeLabs/gooey"

// The gooey.ChildSetter implementations for this package's containers.
//
// They live together rather than beside each container's Measure because
// what matters about them is the property they share: for each of these
// six, ChildComponents returns the child field ITSELF, so the index the
// framework walked with and the index written here are the same by
// construction. That is the interface's one rule, and collecting the
// implementations is what makes it checkable at a glance — and what makes
// the containers that are NOT here visible as a deliberate absence.
//
// Absent on purpose, every one of them because ChildComponents BUILDS its
// list rather than returning a field: ItemsView (rows it realized),
// itemRow, Tabs (the header plus the selected page), MenuBar, StatusBar,
// Segmented, AdornmentLayer and ToastHost (chrome interleaved with
// content). For those, index i in the walk is not an address, so writing
// it back would land a patched subtree in the wrong slot — or in a
// throwaway list that the next Measure rebuilds. They refuse by not
// implementing, and control.PatchMarkup reports the refusal by type name.
func setChildAt(kids []gooey.Component, i int, w gooey.Component) bool {
	if i < 0 || i >= len(kids) {
		return false
	}
	kids[i] = w
	return true
}

func (v *VStack) SetChild(i int, w gooey.Component) bool {
	return setChildAt(v.Children, i, w)
}

func (h *HStack) SetChild(i int, w gooey.Component) bool {
	return setChildAt(h.Children, i, w)
}

func (g *Grid) SetChild(i int, w gooey.Component) bool {
	return setChildAt(g.Children, i, w)
}

func (c *Canvas) SetChild(i int, w gooey.Component) bool {
	return setChildAt(c.Children, i, w)
}

func (b *ButtonBar) SetChild(i int, w gooey.Component) bool {
	return setChildAt(b.Children, i, w)
}

// SetChild on Border takes only index 0: its ChildComponents returns a
// one-element slice built around Child, so 0 is the only address that
// exists. Writing through setChildAt would write into that temporary
// slice and lose the child.
func (b *Border) SetChild(i int, w gooey.Component) bool {
	if i != 0 {
		return false
	}
	b.Child = w
	return true
}
