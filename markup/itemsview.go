package markup

import (
	"fmt"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// buildItemsView is the markup side of DataTemplates, and the one place
// where a template becomes a factory:
//
//	<ItemsView Items="{{.Stories}}" Selected="{{.Sel}}" Activate="{{.Open}}">
//	  <ItemsView.ItemTemplate>
//	    <HStack Gap="1">
//	      <Text>{{.Title}}</Text>
//	      <Text Style="dim">{{.Published}}</Text>
//	    </HStack>
//	  </ItemsView.ItemTemplate>
//	</ItemsView>
//
// The template's Element subtree is CAPTURED, not built. Each item gets
// its own instance, built against its own Context whose Values are that
// item's handles — UserControl isolation, applied per row: inside the
// template, dot is the ITEM, and the page's own values are deliberately
// out of reach. A row cannot accidentally bind the page's selected index
// where it meant the item's; it has no name for it.
//
// Everything else the document carries — styles, registered components,
// code-behind handlers, includes, the dispatcher, the xmlns table — is
// inherited, so a template may place a registered component exactly like
// any other markup. That is what lets a migrated control keep its custom
// cell (finder's match highlighting) while the template does the placing.
func buildItemsView(e Element, ctx *Context) (gooey.Component, error) {
	items, err := Bound[components.ItemSource](e, ctx, "Items")
	if err != nil {
		return nil, err
	}
	tmpl, ok := e.Props["ItemTemplate"]
	if !ok {
		return nil, fmt.Errorf("markup: <ItemsView> needs an <ItemsView.ItemTemplate>")
	}
	if len(tmpl.Children) != 1 {
		return nil, fmt.Errorf("markup: <ItemsView.ItemTemplate> needs exactly one child element, got %d", len(tmpl.Children))
	}
	row := tmpl.Children[0]

	kids, attach, err := buildChildren(e, ctx)
	if err != nil {
		return nil, err
	}
	if len(kids) > 0 {
		return nil, fmt.Errorf("markup: <ItemsView> takes no visual children; its rows come from <ItemsView.ItemTemplate>")
	}

	// The namespace table is captured HERE, at build time, for the same
	// reason Build saves and restores it: a row is instantiated long
	// after this document finished loading, and a handler expression
	// inside the template must still resolve against the prefixes THIS
	// document declared.
	ns := ctx.ns
	// The resource scope is captured beside it, for the same reason and
	// with a sharper failure if it is not: a row is realized long after
	// the scope that declared its styles was popped, so a template naming
	// a page-declared <Style> would resolve against an empty chain at
	// SCROLL time.
	//
	// Half-hidden, which is why it is worth stating. Validate builds one
	// throwaway row at load, so a collection that is non-empty then does
	// catch the error at load — but a table fed by a timer is empty at
	// load, and the same typo surfaces on first scroll instead.
	res := ctx.res
	// The PAGE's armed set, captured here for the same reason as the two
	// above: a row is realized long after this build finished, and
	// document.build restores ctx.armedSinks to the outer map when it
	// returns — so reading it inside the factory would consult whatever
	// scope happens to be current then, not the page whose arms matter.
	// Raised in review of #459.
	pageArmed := ctx.armedSinks
	pageNested := ctx.armedNested
	factory := func(values map[string]any) (gooey.Component, error) {
		item := &Context{
			Values:     values,
			Styles:     ctx.Styles,
			Components: ctx.Components,
			Handlers:   ctx.Handlers,
			Includes:   ctx.Includes,
			Dispatcher: ctx.Dispatcher,
			Named:      map[string]gooey.Component{},
			// ROW-SCOPED, and not merely non-nil. This is the only
			// *Context in the package built outside document.build, so it
			// is the one place armedSinks arrives nil — and elements.go
			// WRITES to it, which panicked with "assignment to entry in
			// nil map" for a <Frozen AllowError> in a template. A panic
			// inside Build is the exact defect the Settable() guard was
			// added to remove, and the timing here is worse: Validate
			// realizes one throwaway row at load, so a collection that is
			// non-empty then panics during Build, while a table fed by a
			// timer is empty at load and the same markup panics on FIRST
			// SCROLL — inside the composer, where a panic skips
			// Screen.Restore.
			//
			// Sharing the PAGE's map would be the wrong scope rather than
			// the expensive one: this factory runs per row realization
			// and never unregisters, so a scrolling list would accumulate
			// an entry per row and then refuse its own second row. A
			// fresh map per row keeps the guard meaningful WITHIN a row —
			// two <Frozen> in one template sharing a sink still collide,
			// which is the real page-shape — and lets each row arm the
			// same template sink, whose handle a projection normally makes
			// per-row.
			//
			// NORMALLY, and the qualifier is the residual hole: nothing
			// here enforces it. components/itemsview.go passes a
			// *prop.Property[string] found in a row map straight through,
			// so a projection that hands every row ONE shared handle gets
			// no load-time signal and the rows overwrite each other's
			// message at runtime. That is not fixable from this side — the
			// handles are indistinguishable by the time they arrive — and
			// docs/markup-reference.md states it rather than claiming it
			// cannot happen.
			//
			// The PAGE's arms are visible through armedOuter, which is the
			// collision this scoping left open: a <Frozen> on the page and
			// a <Frozen> in the template arming one handle built clean,
			// and the row's priming publish erased the page's message
			// during Build.
			//
			// Raised in review of #459, twice.
			armedSinks: map[*prop.Property[string]]string{},
			armedOuter: pageArmed,
			// The document's record, so a row realized BEFORE the page's
			// own <Frozen> — which the load-time throwaway row always is
			// when the list is declared first — is still judged. It is a
			// pointer whose flag is false once the build returns, so
			// scroll-time rows record nothing. Raised in review of #459.
			armedNested: pageNested,
			ns:          ns,
			res:         res,
		}
		return build(row, item)
	}

	v := &components.ItemsView{
		Items:    items,
		Template: factory,
		// The house highlight steps aside for a template that names the
		// reserved value: mentioning _selected is how a template says it
		// is drawing selection itself.
		Highlight: !mentions(row, components.SelectedKey),
	}
	if suppliedAttr(e, "Selected") {
		if v.Selected, err = Bound[int](e, ctx, "Selected"); err != nil {
			return nil, err
		}
	}
	if v.Activate, err = ctx.Command(e.Attrs["Activate"]); err != nil {
		return nil, fmt.Errorf("markup: <ItemsView Activate=%q>: %w", e.Attrs["Activate"], err)
	}
	if v.SelectionChanged, err = ctx.Command(e.Attrs["SelectionChanged"]); err != nil {
		return nil, fmt.Errorf("markup: <ItemsView SelectionChanged=%q>: %w", e.Attrs["SelectionChanged"], err)
	}
	// Focusable is XAML's spelling; the Go field is the zero-defaulted
	// inverse. Only the two boolean words are accepted — a typo here
	// would otherwise silently leave the view in the tab order.
	switch e.Attrs["Focusable"] {
	case "", "true":
	case "false":
		v.NoFocus = true
	default:
		return nil, fmt.Errorf("markup: <ItemsView Focusable=%q>: want \"true\" or \"false\"", e.Attrs["Focusable"])
	}
	if err := attachAll(e, v, attach); err != nil {
		return nil, err
	}
	// One throwaway row against the first item, so a template binding
	// that does not resolve fails the LOAD like every other binding here.
	// An empty collection has nothing to check against; those errors
	// surface at first realization and are painted into the view.
	if err := v.Validate(); err != nil {
		return nil, fmt.Errorf("markup: <ItemsView.ItemTemplate>: %w", err)
	}
	return v, nil
}

// mentions reports whether name appears anywhere in an element subtree —
// in an attribute value, in text content, or in a property element. It is
// a textual test on purpose: the question is whether the template AUTHOR
// referred to a reserved value, and at this point the bindings inside it
// have not been resolved against anything.
func mentions(e Element, name string) bool {
	if strings.Contains(e.Text, name) {
		return true
	}
	for _, v := range e.Attrs {
		if strings.Contains(v, name) {
			return true
		}
	}
	for _, p := range e.Props {
		if mentions(p, name) {
			return true
		}
	}
	for _, c := range e.Children {
		if mentions(c, name) {
			return true
		}
	}
	return false
}
