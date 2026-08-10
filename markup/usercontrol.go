package markup

import (
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
)

// UserControl wraps a markup file + code-behind setup as a Builder, so
// a control registers like any custom widget and instantiates as an
// element: <StoryList Stories="{{.Stories}}"/>.
//
// Context isolation is the contract: setup returns the instance's OWN
// Context — bindings inside the control's markup resolve against it,
// never against the page. Data crosses the boundary through element
// attributes, resolved in the PARENT context (see Context.BindingValue)
// to property handles the setup wires into its context or widgets.
// Styles and Widgets inherit from the parent when the child leaves
// them nil; Named is scoped per instance (like x:Name in templates).
func UserControl(fsys fs.FS, name string, setup func(e Element, parent *Context) (*Context, error)) Builder {
	return func(e Element, parent *Context) (gooey.Widget, error) {
		child, err := setup(e, parent)
		if err != nil {
			return nil, fmt.Errorf("markup: control %s: %w", name, err)
		}
		if child.Styles == nil {
			child.Styles = parent.Styles
		}
		if child.Widgets == nil {
			child.Widgets = parent.Widgets
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
		return Load(fsys, name, child)
	}
}

// Include returns a Builder for a markup-only control — no code-behind.
// The instance's attributes BECOME the control's context: each
// attribute resolves in the parent context (binding → property handle,
// literal → string) and is exposed under its attribute name. So
// <Card Title="{{.Header}}" Sub="details"/> gives card.gooey a context
// where {{.Title}} is the parent's Header handle and {{.Sub}} is a
// literal. Layout attributes (Width, Margin, Grid.Row, …) still apply
// to the instance and are not passed through.
func Include(fsys fs.FS, name string) Builder {
	return UserControl(fsys, name, func(e Element, parent *Context) (*Context, error) {
		vals := map[string]any{}
		for k, v := range e.Attrs {
			if layoutAttr(k) || k == "Name" {
				continue
			}
			if isHandlerExpr(v) {
				// A handler expression is resolved in the PARENT — that
				// is the document whose xmlns table declares the prefix —
				// and the resulting Command crosses the boundary as an
				// ordinary value, so the child binds it with {{.Attr}}
				// like any other delegate.
				cmd, err := parent.Command(v)
				if err != nil {
					return nil, fmt.Errorf("attribute %s: %w", k, err)
				}
				vals[k] = cmd
			} else if bindRe.MatchString(v) {
				h, err := parent.BindingValue(v)
				if err != nil {
					return nil, fmt.Errorf("attribute %s: %w", k, err)
				}
				vals[k] = h
			} else {
				vals[k] = v
			}
		}
		return &Context{Values: vals}, nil
	})
}

func layoutAttr(k string) bool {
	switch k {
	case "Width", "Height", "Margin", "HAlign", "VAlign", "Visibility":
		return true
	}
	return len(k) > 5 && k[:5] == "Grid."
}

// Command resolves an event attribute to a gooey.Command — now three
// halves of the event-binding split. A handler expression
// (Click="{{net:Get .Url | into .Body}}") resolves through the
// document's xmlns table to a registered HandlerProvider, so the
// behavior itself is declared in markup. A binding expression
// (Click="{{.Save}}") resolves a func-valued entry in the context, so
// the delegate lives in the viewmodel and markup-only controls can wire
// events with no code-behind at all. A bare name (Click="OnSave")
// resolves against Handlers, the code-behind registry. An empty
// attribute is not an error — it means the element has no command.
func (ctx *Context) Command(attr string) (gooey.Command, error) {
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
		case gooey.Command:
			return f, nil
		case func():
			return gooey.Command(f), nil
		}
		return nil, fmt.Errorf("markup: %s is %T; need gooey.Command or func()", attr, v)
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
