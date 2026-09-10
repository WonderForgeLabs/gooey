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
	// document.build restores ctx.arms.sinks to the outer map when it
	// returns — so reading it inside the factory would consult whatever
	// scope happens to be current then, not the page whose arms matter.
	// Raised in review of #459.
	//
	// arms.outer FIRST, and the name is the reason. ctx.arms.sinks is the
	// page's map only when this <ItemsView> is built at page level; for a
	// list declared INSIDE another list's item template it is the outer
	// row's deliberately row-local map, so the inner rows got an
	// arms.outer pointing at a row and the page's arms were invisible to
	// them. Load time was still covered by collide; scroll time was not,
	// and an inner row then erased a live page failure with no refusal
	// anywhere. arms.outer is the page's map at every depth, because a
	// row context copies it through unchanged.
	//
	// The outer ROW's own arms are not lost by this: they go on the
	// document's nested record, where a second arm on the same sink is
	// now a load error in its own right. Raised in review of #459.
	pageArmed := ctx.arms.outer
	if pageArmed == nil {
		pageArmed = ctx.arms.sinks
	}
	pageNested := ctx.arms.nested
	// AND THE PAGE'S PENDING-ARM CARRIER, captured here rather than read
	// inside the factory. It is not what the row ARMS through — the row
	// builds its own, below — it is how the row answers "am I the
	// load-time probe": open means ItemsView.Validate is realizing its
	// throwaway row inside the page build, closed means the composer is
	// realizing a real one after Build returned. Read per row instead of
	// captured, ctx.arms.pending would be nil at scroll time, which is
	// the same answer by accident rather than by rule, and it would make
	// the reply depend on what a caller left on the page's Context.
	// Raised in review of #459.
	pagePending := ctx.arms.pending
	factory := func(values map[string]any) (gooey.Component, error) {
		rowPending := &deferredArms{open: true}
		item := &Context{
			Values:     values,
			Styles:     ctx.Styles,
			Components: ctx.Components,
			Handlers:   ctx.Handlers,
			Includes:   ctx.Includes,
			Dispatcher: ctx.Dispatcher,
			Named:      map[string]gooey.Component{},
			// THE ROW'S ARM SCOPE, CONSTRUCTED rather than inherited,
			// and this is the only place in the package that does not
			// simply copy the parent's. Every member diverges from what
			// `child.arms = parent.arms` would give it, for a different
			// reason, and building it in one literal is what makes the
			// four reasons readable as a set.
			arms: armScope{
				// ROW-SCOPED, and not merely non-nil. This is the only
				// *Context in the package built outside document.build,
				// so it is the one place the sinks map arrives nil — and
				// elements.go WRITES to it, which panicked with
				// "assignment to entry in nil map" for a <Frozen
				// AllowError> in a template. A panic inside Build is the
				// exact defect the Settable() guard was added to remove,
				// and the timing here is worse: Validate realizes one
				// throwaway row at load, so a collection that is
				// non-empty then panics during Build, while a table fed
				// by a timer is empty at load and the same markup panics
				// on FIRST SCROLL — inside the composer, where a panic
				// skips Screen.Restore.
				//
				// Sharing the PAGE's map would be the wrong scope rather
				// than the expensive one: this factory runs per row
				// realization and never unregisters, so a scrolling list
				// would accumulate an entry per row and then refuse its
				// own second row. A fresh map per row keeps the guard
				// meaningful WITHIN a row — two <Frozen> in one template
				// sharing a sink still collide, which is the real
				// page-shape — and lets each row arm the same template
				// sink, whose handle a projection normally makes per-row.
				//
				// NORMALLY, and the qualifier is the residual hole:
				// nothing here enforces it. components/itemsview.go
				// passes a *prop.Property[string] found in a row map
				// straight through, so a projection that hands every row
				// ONE shared handle gets no load-time signal and the rows
				// overwrite each other's message at runtime.
				//
				// THE REASON IS NOT THAT THE HANDLES ARE
				// INDISTINGUISHABLE. This comment said that, and it is
				// false: the sinks map and nestedArms both key by
				// *prop.Property[string], so a shared handle IS the same
				// pointer and a per-row handle is not — which is exactly
				// how two item TEMPLATES arming one sink is caught. The
				// real reason a two-ROW collision has no load-time signal
				// is narrower: ItemsView.Validate realizes exactly ONE
				// row during the build, so a second arm on the same
				// handle never happens while the nested record is open.
				//
				// That leaves two levers for whoever closes it — realize
				// a second row in Validate, or record scroll-time arms
				// behind the detach seam the spec scopes at :248 —
				// rather than the "closed by nature" the old sentence
				// implied. Round five retired the same wrong reason for
				// the alias guard; docs/markup-reference.md states what
				// is enforced. Raised in review of #459, twice.
				sinks: map[*prop.Property[string]]string{},
				// ROW-LOCAL for the same reason sinks is, and with the
				// same consequence: a row's Allow is judged against the
				// row's own sinks, not the page's. The page-versus-row
				// direction that IS covered is the sink one, through
				// outer and nested below. A row whose Allow set is the
				// page's failure channel is the remaining gap, and it is
				// named in the test file rather than left to be
				// discovered.
				allows: map[*prop.Property[string]]string{},
				// The PAGE's arms, visible but not writable, which is
				// the collision this row-local scoping left open: a
				// <Frozen> on the page and a <Frozen> in the template
				// arming one handle built clean, and the row's priming
				// publish erased the page's message during Build.
				outer: pageArmed,
				// The document's record, so a row realized BEFORE the
				// page's own <Frozen> — which the load-time throwaway row
				// always is when the list is declared first — is still
				// judged. It is a pointer whose flag is false once the
				// build returns, so scroll-time rows record nothing.
				nested: pageNested,
				// AND A CARRIER OF ITS OWN, because a ROW is a build that
				// can fail like any other and the page's carrier answers
				// the wrong question for it.
				//
				// Two discarded rows were arming, and both are the same
				// mistake — a subscription outliving the tree it was made
				// for. Sharing pagePending made the load-time row's arm
				// run whenever the PAGE succeeded; the load-time row is
				// ItemsView.Validate's throwaway probe, which is
				// discarded however well it builds, so its <Frozen> was
				// left publishing into the row's sink with an observer
				// subscribed to a component nothing holds. And at scroll
				// time pagePending is closed, so the arm would run WHERE
				// IT WAS BUILT — before the rest of the row could fail. A
				// row refused halfway left the same debris.
				//
				// A per-row carrier answers both: nothing is armed until
				// the row is a row.
				pending: rowPending,
			},
			ns:  ns,
			res: res,
		}
		w, err := build(row, item)
		if err != nil {
			// DROPPED, not run. The row is not going into any tree, so
			// neither is its subscription.
			return nil, err
		}
		// THE PROBE IS NOT A ROW. ItemsView.Validate realizes one
		// throwaway row during the page build to fail a bad template
		// binding at load, and discards it; the only factory call that
		// happens while the page's carrier is still open IS that probe,
		// because every real row is realized by the composer after Build
		// has returned. So the page's own open/closed flag answers "am I
		// the probe" without a second piece of state to keep in step.
		//
		// The probe still RECORDS, which is the half that must not be
		// dropped with it: arms.nested.record and the collide check run
		// where the <Frozen> is built, not here, so a page-versus-
		// template collision is still a load error. What the probe no
		// longer does is subscribe and publish.
		if !pagePending.inFlight() {
			rowPending.open = false
			rowPending.run()
		}
		return w, nil
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
