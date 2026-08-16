package markup

import (
	"fmt"

	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// The attribute-resolution rules of the markup dialect, exported because
// a Builder registered in Context.Components is the SAME kind of caller
// as a builtin's builder and needs the same rules (#266).
//
// The line drawn here is "what a third-party builder cannot write for
// itself". Context.BindingValue has always been exported, so resolving
// {{.Path}} to some `any` was already possible; what was not was
// everything the dialect layers on top of that lookup, all of which
// lived behind unexported helpers:
//
//   - the typed handle plus the load error naming both types (Bound),
//   - INTERPOLATION and value-namespace calls, whose scanner is
//     unexported, so "Hi {{.Who}}!" resolved by hand through
//     BindingValue silently drops the literal parts (BoundText),
//   - the #rgb/#rrggbb literal, whose parser is unexported, so a third
//     party either duplicated it or refused a literal every builtin
//     accepts (BoundColor),
//   - the Style="name" lookup and its reactive twin (BoundStyle).
//
// These are free functions rather than Context methods because Bound is
// generic and Go has no generic methods; the rest follow its shape so a
// builder reads uniformly. The element is passed whole so every error
// can name the element and attribute it came from — a third-party
// component's load errors then look like a builtin's.
//
// All four resolve ONCE, at build time, to handles rather than values.
// That is the lvalue semantics of the design: a component that took its
// attribute through this surface shares the viewmodel's node, so its
// Render's Get is a subscription and a Set repaints exactly it.

// Bound resolves an attribute that must be a typed property HANDLE
// rather than text: <Checkbox Checked="{{.Auto}}"/> shares the
// viewmodel's property with the component, so the component's Render
// reads it and its toggle Sets it — the only sense in which gooey has
// two-way binding, and the reason it needs no converter machinery.
//
// The type assertion is the whole type check. There is no reflection
// here: T is known at the call site, so a mismatched viewmodel property
// is a load-time error naming both types. An attribute that is not a
// binding expression at all — a literal in a handle position — is a load
// error too, rather than a zero value the component would paint.
func Bound[T any](e Element, ctx *Context, attr string) (*prop.Property[T], error) {
	raw := e.Attrs[attr]
	v, err := ctx.BindingValue(raw)
	if err != nil {
		return nil, fmt.Errorf("markup: <%s %s=%q>: %w", e.Name, attr, raw, err)
	}
	h, ok := v.(*prop.Property[T])
	if !ok {
		var want *prop.Property[T]
		return nil, fmt.Errorf("markup: <%s %s=%q> is %T; need %T", e.Name, attr, raw, v, want)
	}
	return h, nil
}

// BoundText is the "text attribute" rule every built-in follows, in the
// shape a Builder holds its input: an attribute containing {{.Path}}
// bindings or {{ns:Func …}} value-namespace calls becomes a computed
// handle over its parts, and anything else is a literal wrapped as a
// source. An ABSENT attribute is the empty literal, never nil, so a
// component never has to test for both a handle and a raw string.
//
// This is the resolver a third party could not approximate: matching
// {{.Path}} by hand and calling BindingValue returns the handle and
// silently discards the literal text around it.
func BoundText(e Element, ctx *Context, attr string) (*prop.Property[string], error) {
	return literalOrBound(e.Attrs[attr], ctx)
}

// BoundColor resolves a color attribute: the #rgb/#rrggbb literal markup
// already speaks (propKinds "color"), or a binding to the viewmodel's own
// *prop.Property[render.Color] handle. An absent attribute yields nil —
// for Background that keeps the container on the chrome-only damage
// path, so only pages that declare a fill pay for one, and a third-party
// component gets the same "absent means keep your default" contract.
func BoundColor(e Element, ctx *Context, attr string) (*prop.Property[render.Color], error) {
	raw, ok := e.Attrs[attr]
	if !ok || raw == "" {
		return nil, nil
	}
	if bindRe.MatchString(raw) {
		return Bound[render.Color](e, ctx, attr)
	}
	col, err := parseHexColor(raw)
	if err != nil {
		return nil, fmt.Errorf("markup: <%s %s=%q>: %w", e.Name, attr, raw, err)
	}
	return components.Col(col), nil
}

// BoundStyle resolves the Style attribute, which accepts either form: a
// bare name is the static lookup in Context.Styles, and a binding
// expression yields the viewmodel's own *prop.Property[render.Style]
// handle. The bound form is what makes a style REACTIVE — a computed
// style over an accent color repaints the components that read it,
// through the ordinary property graph, with no styling system involved.
//
// The attribute name is not a parameter because Style is one attribute
// with one meaning: a component with a second style-shaped attribute
// wants Bound[render.Style] for it, not a second Style.
func BoundStyle(e Element, ctx *Context) (*prop.Property[render.Style], error) {
	raw := e.Attrs["Style"]
	if bindRe.MatchString(raw) {
		return Bound[render.Style](e, ctx, "Style")
	}
	return styleHandle(e, ctx, "Style", raw)
}
