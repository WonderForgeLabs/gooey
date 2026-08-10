package markup

import (
	"fmt"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
)

// Builders for the toolkit components (ProgressBar, Spinner, Toggle,
// Segmented, StatusBar, ButtonBar) and the small attribute helpers they
// share. They live beside itemsview.go for the same reason: the switch
// in buildComponent stays a readable index of the element vocabulary,
// and anything with a real decision in it gets a name.

// literalOrBound is the "text attribute" rule every built-in follows: a
// value containing {{.Path}} becomes a computed handle, anything else is
// a literal wrapped as a source. An absent attribute is an empty
// literal, not nil, so a component never has to test for both.
func literalOrBound(raw string, ctx *Context) (*prop.Property[string], error) {
	p, err := bindText(raw, ctx)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return components.Str(raw), nil
	}
	return p, nil
}

// optDuration reads an optional time.ParseDuration attribute. Absent
// means "the component's default"; present and wrong is a load error,
// because a mistyped interval that silently became zero would look like
// a component that does not animate.
func optDuration(e Element, attr string) (time.Duration, error) {
	raw := strings.TrimSpace(e.Attrs[attr])
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("markup: <%s %s=%q>: %w", e.Name, attr, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("markup: <%s %s=%q>: must be positive", e.Name, attr, raw)
	}
	return d, nil
}

// optionList resolves <Segmented Options=…>, which takes either form: a
// binding to the viewmodel's own []string handle, or a literal
// pipe-separated list, which is what a fixed set of choices actually is
// and does not deserve a property in the viewmodel.
func optionList(e Element, ctx *Context) (*prop.Property[[]string], error) {
	raw := e.Attrs["Options"]
	if bindRe.MatchString(raw) {
		return boundProp[[]string](e, ctx, "Options")
	}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf(`markup: <Segmented> needs Options (e.g. Options="Day|Week|Month")`)
	}
	parts := strings.Split(raw, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return components.Strs(parts), nil
}

// statusSections is the slot order, which is also the order the sections
// are laid out in.
var statusSections = []string{"Left", "Center", "Right"}

// buildStatusBar fills the three slots. Each accepts either shape: an
// attribute, which is the promoted shorthand for "a dim line of text",
// or a property element holding any component at all. Giving one slot
// both is a load error rather than a precedence rule — there is no
// reading of <StatusBar Left="x"><StatusBar.Left>…</StatusBar.Left>
// that is not a mistake.
func buildStatusBar(e Element, ctx *Context) (gooey.Component, error) {
	bar := &components.StatusBar{}
	dst := map[string]*gooey.Component{
		"Left": &bar.Left, "Center": &bar.Center, "Right": &bar.Right,
	}
	for _, name := range statusSections {
		text, hasAttr := e.Attrs[name]
		slot, hasSlot := e.Props[name]
		if hasAttr && hasSlot {
			return nil, fmt.Errorf("markup: <StatusBar> %s is given as both an attribute and <StatusBar.%s>", name, name)
		}
		switch {
		case hasSlot:
			w, err := slotChild(slot, ctx)
			if err != nil {
				return nil, err
			}
			*dst[name] = w
		case hasAttr:
			content, err := literalOrBound(text, ctx)
			if err != nil {
				return nil, err
			}
			*dst[name] = components.StatusText(content)
		}
	}
	kids, attach, err := buildChildren(e, ctx)
	if err != nil {
		return nil, err
	}
	if len(kids) > 0 {
		return nil, fmt.Errorf("markup: <StatusBar> takes no direct children; its content goes in <StatusBar.Left>, <StatusBar.Center> or <StatusBar.Right>")
	}
	if err := attachAll(e, bar, attach); err != nil {
		return nil, err
	}
	return bar, nil
}

// slotChild builds the single component inside a property element.
func slotChild(e Element, ctx *Context) (gooey.Component, error) {
	if len(e.Children) != 1 {
		return nil, fmt.Errorf("markup: <%s> needs exactly one child element, got %d", e.Name, len(e.Children))
	}
	return build(e.Children[0], ctx)
}
