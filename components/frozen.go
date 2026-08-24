package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Frozen is the markup form of gooey.FrozenAllows: a transparent
// one-child container whose subtree renders but does not act, except for
// the categories its Allow set names.
//
// Freezing was a Go-only interface until this type existed, which meant a
// page could not say "this region is a picture" without code-behind — and
// a design surface, the thing freezing was built for, is exactly a page
// that wants to say it about a region it is editing.
//
//	<Frozen Active="{{.DesignMode}}"
//	        Allow="{{sets:Concat `Hover` .Selected}}">
//	  <VStack> … the document being edited … </VStack>
//	</Frozen>
//
// It paints nothing of its own — it is a chrome-only container, so it
// pre-clears nothing and costs no damage beyond its child's — and it
// measures and arranges its child to its own bounds.
//
// # Why the allow set crosses as TEXT
//
// Allow is a *prop.Property[string] holding category names, not a handle
// to a gooey.Allow. That is what lets markup compose it with the tools
// markup already has: {{sets:Concat …}} is an ordinary value-namespace
// expression returning a string, interpolation puts literals and bound
// paths in the same attribute, and every one of those Gets happens inside
// the computed that reads it — so the subscription comes from the read,
// not from a declaration. A *prop.Property[gooey.Allow] would have needed
// its own value-namespace type, a second binding path, and a viewmodel
// holding a type from the framework's core.
//
// The cost is that an unknown category name in a BOUND value cannot be a
// load error. It fails closed — the set becomes gooey.AllowNone, the
// strictest answer — and AllowError reports why. A LITERAL Allow is
// checked at load time by the markup builder, where it belongs.
type Frozen struct {
	gooey.Base
	Child gooey.Component

	// Active decides whether the freeze applies at all. Nil means always
	// frozen, which is what an omitted attribute yields.
	Active *prop.Property[bool]

	// Allow is the category set, as text. Nil means AllowNone: a plain
	// <Frozen> is the bool `Frozen() == true` it replaces.
	Allow *prop.Property[string]

	// The parse cache. It exists because FrozenAllow is called from
	// FocusManager.frozenHostFor on every routed event — including motion,
	// which arrives at pointer speed — and gooey.ParseAllow splits a
	// string. Caching the last text keeps that off the routing hot path.
	//
	// It caches the PARSE, never the read: Allow.Get() below is called
	// unconditionally on every invocation, before the cache is consulted,
	// because that Get is what subscribes the Composer's observer to the
	// property. Skipping it on a cache hit would make the observer's
	// dependency set depend on whether the text happened to change —
	// which is the same defect as a Get behind an early return, and it
	// would go unnoticed until the first time the set changed twice.
	cachedText  string
	cachedAllow gooey.Allow
	cachedErr   error
	cacheValid  bool
}

func (f *Frozen) ChildComponents() []gooey.Component { return []gooey.Component{f.Child} }

func (f *Frozen) SetChild(i int, w gooey.Component) bool {
	if i != 0 {
		return false
	}
	f.Child = w
	return true
}

func (f *Frozen) Measure(avail gooey.Size) gooey.Size {
	return gooey.MeasureChild(f.Child, avail)
}

func (f *Frozen) Arrange(b gooey.Rect) {
	f.Base.Arrange(b)
	gooey.ArrangeChild(f.Child, b)
}

// Render paints nothing. A frozen region looks exactly like the region it
// froze — that is the whole idea — and a container that paints no cells
// of its own stays on the chrome-only pre-clear path.
func (f *Frozen) Render(*gooey.Frame) {}

// Frozen reports whether the freeze is in effect. Reading Active here is
// what subscribes Composer.armFrozen's observer to it, so binding
// Active="{{.DesignMode}}" re-routes input in the frame the mode changes.
func (f *Frozen) Frozen() bool {
	if f.Active == nil {
		return true
	}
	return f.Active.Get()
}

// FrozenAllow is the set of categories that still act inside the subtree.
//
// A nil Allow is gooey.AllowNone — nothing acts — so a bare <Frozen> is
// precisely the bool interface it generalizes.
func (f *Frozen) FrozenAllow() gooey.Allow {
	if f.Allow == nil {
		return gooey.AllowNone
	}
	text := f.Allow.Get()
	if f.cacheValid && text == f.cachedText {
		return f.cachedAllow
	}
	a, err := gooey.ParseAllow(text)
	if err != nil {
		// Fail CLOSED. A set nobody can parse must not be read as
		// permission, and AllowNone is the only answer that cannot grant
		// something the page did not ask for.
		a = gooey.AllowNone
	}
	f.cachedText, f.cachedAllow, f.cachedErr, f.cacheValid = text, a, err, true
	return a
}

// AllowError is the parse failure behind a fail-closed set, or nil.
//
// It reports what FrozenAllow last saw, so it is meaningful only after a
// frame has evaluated the observer. It exists because a bound Allow whose
// value is a typo would otherwise seal a subtree with no explanation
// anywhere — a host, a test or a design surface can read this and say so.
func (f *Frozen) AllowError() error { return f.cachedErr }
