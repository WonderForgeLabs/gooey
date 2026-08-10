package markup

import (
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
)

// UserControl wraps a markup file + code-behind setup as a Builder, so
// a control registers like any custom component and instantiates as an
// element: <StoryList Stories="{{.Stories}}"/>.
//
// Context isolation is the contract: setup returns the instance's OWN
// Context — bindings inside the control's markup resolve against it,
// never against the page. Data crosses the boundary through element
// attributes, resolved in the PARENT context (see Context.BindingValue)
// to property handles the setup wires into its context or components.
// Styles and Components inherit from the parent when the child leaves
// them nil; Named is scoped per instance (like x:Name in templates).
//
// If the control's markup declares dependency properties with
// <x:Property>, they are resolved BEFORE setup runs and installed into
// the context setup returns; setup reads them through
// Context.DeclaredProperties and extends the surface with private
// members. A control that declares nothing behaves exactly as it always
// has: setup owns the whole context.
func UserControl(fsys fs.FS, name string, setup func(e Element, parent *Context) (*Context, error)) Builder {
	return control(fsys, name, setup, false)
}

// Include returns a Builder for a markup-only control — no code-behind.
//
// Without declarations the instance's attributes BECOME the control's
// context: each attribute resolves in the parent context (binding →
// property handle, literal → string) and is exposed under its attribute
// name. So <Card Title="{{.Header}}" Sub="details"/> gives card.gooey a
// context where {{.Title}} is the parent's Header handle and {{.Sub}}
// is a literal. Layout attributes (Width, Margin, Grid.Row, …) still
// apply to the instance and are not passed through.
//
// With <x:Property> declarations the surface is the declarations
// instead: attributes are type-checked against them, absent ones
// materialize their declared defaults, and an undeclared attribute is a
// load error. Same control tier, now with a checked contract.
func Include(fsys fs.FS, name string) Builder {
	return control(fsys, name, nil, true)
}

// control is the shared instantiation path for both control tiers.
//
// Order is the contract (see docs/specs/2026-08-10-markup-declared-
// properties.md): declarations resolve the instance's attributes into a
// pre-populated context first, then setup runs and EXTENDS it. A setup
// that defines a value under a declared name is a load error — one
// source of truth for the public surface, the same reason a property
// system rejects double registration.
func control(fsys fs.FS, name string, setup func(e Element, parent *Context) (*Context, error), passThrough bool) Builder {
	return func(e Element, parent *Context) (gooey.Component, error) {
		doc, err := loadDocument(fsys, name)
		if err != nil {
			return nil, err
		}
		// The declared-surface registry is page-wide (see Context.Declared):
		// it is created on the topmost context the moment any control
		// instantiates, and every child context below shares the same map,
		// so a control built inside a control still records where the page
		// can see it.
		if parent.Declared == nil {
			parent.Declared = map[gooey.Component]DeclaredSurface{}
		}
		if doc.decls.present {
			if err := doc.decls.checkAttrs(name, e); err != nil {
				return nil, err
			}
		}
		declared, err := doc.decls.instantiate(name, e, parent)
		if err != nil {
			return nil, err
		}

		var child *Context
		if setup != nil {
			child, err = runSetup(setup, e, parent, declared)
			if err != nil {
				return nil, fmt.Errorf("markup: control %s: %w", name, err)
			}
		}
		if child == nil {
			child = &Context{}
		}
		if child.Values == nil {
			child.Values = map[string]any{}
		}
		// A control that declares nothing keeps the implicit surface:
		// every attribute passes through, unchecked, as it always has.
		if passThrough && !doc.decls.present {
			if err := passAttrs(e, parent, child.Values); err != nil {
				return nil, fmt.Errorf("markup: control %s: %w", name, err)
			}
		}
		for _, d := range doc.decls.list {
			if _, dup := child.Values[d.Name]; dup {
				return nil, fmt.Errorf("markup: %s: dependency property %q — the code-behind setup also defines %q; declarations own the control's public surface, so a setup may extend it but not redefine it", name, d.Name, d.Name)
			}
			child.Values[d.Name] = declared[d.Name]
		}

		if child.Styles == nil {
			child.Styles = parent.Styles
		}
		if child.Declared == nil {
			child.Declared = parent.Declared
		}
		if child.Components == nil {
			child.Components = parent.Components
		}
		if child.Handlers == nil {
			child.Handlers = parent.Handlers
		}
		if child.Includes == nil {
			child.Includes = parent.Includes
		}
		if child.Dispatcher == nil {
			child.Dispatcher = parent.Dispatcher
		}
		// A control's literal asset paths (Image Src) resolve against
		// the FS its OWN markup came from, the same isolation its
		// bindings get: the file that names the asset is the file the
		// path is relative to.
		child.fsys = fsys
		w, err := doc.build(child)
		if err != nil {
			return nil, err
		}
		if len(doc.decls.list) > 0 {
			surface := DeclaredSurface{Control: name, Props: make([]DeclaredProp, 0, len(doc.decls.list))}
			for _, d := range doc.decls.list {
				surface.Props = append(surface.Props, DeclaredProp{Declaration: d, Handle: declared[d.Name]})
			}
			parent.Declared[w] = surface
		}
		return w, nil
	}
}

// runSetup calls a code-behind setup with the declared handles visible
// on the parent context for exactly the duration of the call — the same
// document-scoped save/restore the xmlns table uses, so a setup that
// itself instantiates a control cannot see the wrong declarations.
func runSetup(setup func(e Element, parent *Context) (*Context, error), e Element, parent *Context, declared map[string]any) (*Context, error) {
	prev := parent.declared
	parent.declared = declared
	defer func() { parent.declared = prev }()
	return setup(e, parent)
}

// passAttrs is the undeclared (implicit) surface: attributes resolved in
// the parent and exposed under their own names.
func passAttrs(e Element, parent *Context, vals map[string]any) error {
	for k, v := range e.Attrs {
		if layoutAttr(k) || k == "Name" || k == "Tooltip" {
			// Like the layout attributes, Tooltip="..." decorates the
			// INSTANCE (applyTooltipShorthand attaches it) and does not
			// cross the control boundary as a value.
			continue
		}
		if isHandlerExpr(v) {
			// A handler expression is resolved in the PARENT — that is
			// the document whose xmlns table declares the prefix — and
			// the resulting Command crosses the boundary as an ordinary
			// value, so the child binds it with {{.Attr}} like any other
			// delegate.
			cmd, err := parent.Command(v)
			if err != nil {
				return fmt.Errorf("attribute %s: %w", k, err)
			}
			vals[k] = cmd
		} else if bindRe.MatchString(v) {
			h, err := parent.BindingValue(v)
			if err != nil {
				return fmt.Errorf("attribute %s: %w", k, err)
			}
			vals[k] = h
		} else {
			vals[k] = v
		}
	}
	return nil
}

// DeclaredProperties returns the dependency properties declared by the
// control currently being instantiated, already resolved against the
// instance's attributes into typed *prop.Property handles.
//
// It is valid only inside a UserControl setup func, called on the
// PARENT context handed to that func — the same document-scoped
// hand-off the xmlns table uses. A control with no declarations sees an
// empty map, and outside a setup call it is nil.
//
// Setup reads these to build private computeds over the control's
// public surface. The framework installs them into the control's
// context afterwards, so setup must not copy them into its own Values:
// that is the collision a declared surface rejects.
func (ctx *Context) DeclaredProperties() map[string]any { return ctx.declared }

func layoutAttr(k string) bool {
	switch k {
	case "Width", "Height", "Margin", "HAlign", "VAlign", "Visibility":
		return true
	}
	return strings.HasPrefix(k, "Grid.") || strings.HasPrefix(k, "Canvas.")
}

// Command resolves an event attribute to a gooey.Action — now three
// halves of the event-binding split. A handler expression
// (Click="{{net:Get .Url | into .Body}}") resolves through the
// document's xmlns table to a registered HandlerProvider, so the
// behavior itself is declared in markup. A binding expression
// (Click="{{.Save}}") resolves a func-valued entry in the context, so
// the delegate lives in the viewmodel and markup-only controls can wire
// events with no code-behind at all. A bare name (Click="OnSave")
// resolves against Handlers, the code-behind registry. An empty
// attribute is not an error — it means the element has no command.
//
// The binding form accepts anything that implements Action, which is
// what makes a conditional command transparent to markup: a viewmodel
// that hands out gooey.NewCommand(save).When(dirty) instead of a bare
// func changes nothing in the document, and the Button on the other end
// starts painting itself disabled.
func (ctx *Context) Command(attr string) (gooey.Action, error) {
	if strings.TrimSpace(attr) == "" {
		return nil, nil
	}
	if isHandlerExpr(attr) {
		x, err := parseHandlerExpr(attr)
		if err != nil {
			return nil, err
		}
		return ctx.handlerCommand(x)
	}
	if bindRe.MatchString(attr) {
		v, err := ctx.BindingValue(attr)
		if err != nil {
			return nil, err
		}
		switch f := v.(type) {
		case gooey.Action: // gooey.Command and *gooey.Cmd both land here
			return f, nil
		case func():
			return gooey.Command(f), nil
		}
		return nil, fmt.Errorf("markup: %s is %T; need gooey.Command, *gooey.Cmd or func()", attr, v)
	}
	if c, ok := ctx.Handlers[attr]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("markup: no handler %q registered", attr)
}

// BindingValue resolves an attribute binding like "{{.Stories}}"
// against this context and returns the raw context value — typically a
// *prop.Property[T] handle. This is the parent side of the UserControl
// data hand-off.
func (ctx *Context) BindingValue(attr string) (any, error) {
	m := bindRe.FindStringSubmatch(attr)
	if m == nil {
		return nil, fmt.Errorf("markup: %q is not a binding expression", attr)
	}
	return resolve(ctx.Values, m[1])
}

// WatchAll polls several markup files and calls rebuild on any change.
// One page rebuild covers every control instance, since UserControls
// re-instantiate during Load. Returns a stop function.
func WatchAll(fsys fs.FS, names []string, rebuild func()) func() {
	stop := make(chan struct{})
	go func() {
		last := map[string]time.Time{}
		for _, n := range names {
			if st, err := fs.Stat(fsys, n); err == nil {
				last[n] = st.ModTime()
			}
		}
		t := time.NewTicker(300 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				for _, n := range names {
					st, err := fs.Stat(fsys, n)
					if err != nil || !st.ModTime().After(last[n]) {
						continue
					}
					last[n] = st.ModTime()
					rebuild()
					break
				}
			}
		}
	}()
	return func() { close(stop) }
}
