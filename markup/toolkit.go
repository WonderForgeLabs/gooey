package markup

import (
	"fmt"
	"strconv"
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

// literalOrBound is the raw-string form of the text rule that BoundText
// (bind.go) exports: a value containing {{.Path}} becomes a computed
// handle, anything else is a literal wrapped as a source, and empty is
// an empty literal rather than nil so a component never has to test for
// both. Prefer BoundText — it takes the element, so its errors can name
// it. This form stays for the two callers whose input is not an
// attribute at all: an element's text content, and the Tooltip=
// shorthand's already-extracted string.
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

// optBool reads an optional bool attribute the way every other markup
// literal reads one — strconv.ParseBool, so "1", "true", "TRUE" and "T"
// all work — and makes anything else a LOAD ERROR.
//
// Erroring matters more here than for most attributes. A bool attribute
// that fell back to false on an unrecognized spelling would turn a typo
// into the silently less safe branch: <Companion CleanEnv="yes"> would
// look like "start this child with an empty environment" and actually
// hand it os.Environ() in full. Absent still means false; PRESENT AND
// UNREADABLE is the case that must not be guessed at.
func optBool(e Element, attr string) (bool, error) {
	raw := strings.TrimSpace(e.Attrs[attr])
	if raw == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("markup: <%s %s=%q>: want a bool (true/false, 1/0)", e.Name, attr, raw)
	}
	return b, nil
}

// optionList resolves <Segmented Options=…>, which takes either form: a
// binding to the viewmodel's own []string handle, or a literal
// pipe-separated list, which is what a fixed set of choices actually is
// and does not deserve a property in the viewmodel.
func optionList(e Element, ctx *Context) (*prop.Property[[]string], error) {
	raw := e.Attrs["Options"]
	if bindRe.MatchString(raw) {
		return Bound[[]string](e, ctx, "Options")
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
		_, hasAttr := e.Attrs[name]
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
			content, err := BoundText(e, ctx, name)
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

// buildTabs assembles <Tabs Selected="{{.Tab}}"><Tab Header="mcp">…
// </Tab></Tabs>. Selected is optional — absent, the control keeps its
// own selection starting at 0. Each <Tab> takes a Header (literal or
// bound) and exactly one content child. The content's Visibility is
// OWNED by the Tabs (that binding is the whole switching mechanism), so
// a Visibility attribute on a page root is a load error rather than a
// binding that would be silently replaced.
func buildTabs(e Element, ctx *Context) (gooey.Component, error) {
	t := &components.Tabs{}
	var err error
	if suppliedAttr(e, "Selected") {
		if t.Selected, err = Bound[int](e, ctx, "Selected"); err != nil {
			return nil, err
		}
	}
	if t.Changed, err = ctx.Command(e.Attrs["Changed"]); err != nil {
		return nil, fmt.Errorf("markup: <Tabs Changed=%q>: %w", e.Attrs["Changed"], err)
	}
	if t.Style, err = BoundStyle(e, ctx); err != nil {
		return nil, err
	}
	var attach []gooey.Component
	for _, c := range e.Children {
		if c.Name != "Tab" {
			w, err := build(c, ctx)
			if err != nil {
				return nil, err
			}
			if nv, ok := w.(gooey.NonVisual); ok && nv.NonVisual() {
				attach = append(attach, w)
				continue
			}
			return nil, fmt.Errorf("markup: <Tabs> children must be <Tab> elements; got <%s>", c.Name)
		}
		if _, ok := c.Attrs["Header"]; !ok {
			return nil, fmt.Errorf(`markup: <Tab> needs a Header (e.g. Header="log")`)
		}
		header, err := BoundText(c, ctx, "Header")
		if err != nil {
			return nil, err
		}
		if len(c.Children) != 1 {
			return nil, fmt.Errorf("markup: <Tab Header=%q> needs exactly one content child, got %d", c.Attrs["Header"], len(c.Children))
		}
		if _, has := c.Children[0].Attrs["Visibility"]; has {
			return nil, fmt.Errorf("markup: <Tab Header=%q>: a tab page cannot bind its own Visibility — the Tabs owns it", c.Attrs["Header"])
		}
		content, err := build(c.Children[0], ctx)
		if err != nil {
			return nil, err
		}
		t.Items = append(t.Items, components.TabItem{Header: header, Content: content})
	}
	if len(t.Items) == 0 {
		return nil, fmt.Errorf("markup: <Tabs> needs at least one <Tab>")
	}
	if err := attachAll(e, t, attach); err != nil {
		return nil, err
	}
	return t, nil
}

// slotChild builds the single component inside a property element.
func slotChild(e Element, ctx *Context) (gooey.Component, error) {
	if len(e.Children) != 1 {
		return nil, fmt.Errorf("markup: <%s> needs exactly one child element, got %d", e.Name, len(e.Children))
	}
	return build(e.Children[0], ctx)
}

// bodyText applies the whitespace rule for an element's BODY — the
// content written between its tags rather than in an attribute. It is
// the one place that rule lives; every builder whose content is its
// body calls this instead of trimming for itself.
//
// The rule: a body the author wrote on ONE line is taken VERBATIM; a
// body wrapped across lines is trimmed.
//
//	<Text>    Hello</Text>   ->  "    Hello"
//	<Text>
//	  Hello
//	</Text>                  ->  "Hello"
//
// Every body used to go through strings.TrimSpace, which made leading
// and trailing spaces INEXPRESSIBLE. An author drawing ASCII art
// watched every line slide to column 0 with no error to say why, and
// the only way out was abandoning bodies for a <Canvas> with an
// explicit Canvas.Left per line. Twelve <Text> bodies already in this
// repo were quietly losing an indent they had asked for — the detail
// lists in handlers/temporal/internal/wizard/ui/stage-*.gooey, written
// as <Text>  ticket: {{.Ticket}}</Text> and rendered flush left.
//
// This needs no opt-in attribute, and the reason is the part worth
// internalising: INDENTING THE DOCUMENT DOES NOT PUT WHITESPACE INSIDE
// A BODY. The file's indentation lands before the start tag, not after
// it, so a <Text> nested ten levels deep still reports exactly "Hello"
// for <Text>Hello</Text>. The only thing that injects leading
// whitespace into a body is the author breaking the body onto its own
// line — and that case is distinguishable because it contains a
// NEWLINE. The discriminator was in the data all along; TrimSpace threw
// it away.
//
// So a newline means "this whitespace is source formatting" and its
// absence means "this whitespace is content". Two consequences that are
// choices rather than accidents:
//
//   - A one-line body of nothing but spaces is CONTENT. <Text> </Text>
//     is a deliberate one-cell spacer and now paints one styled blank
//     where it used to paint no cell at all.
//   - Trailing whitespace on a one-line body is kept, for the same
//     reason whitespace in the middle of one is kept: the author typed
//     it, and a run of text padded out to a column is a real thing to
//     want. Nothing here can tell a deliberate trailing space from a
//     sloppy one, so it does not guess.
//
// What this deliberately does NOT do is dedent a wrapped body, so a
// MULTI-LINE body still cannot carry leading whitespace — unchanged
// from before, not a regression introduced here. No such body exists in
// the tree, and inventing a dedent rule for a case with no users would
// be guessing; xml:space="preserve" is the standard spelling if it is
// ever wanted, and nothing above forecloses it.
//
// Testing '\n' alone is sufficient: encoding/xml normalises CR and CRLF
// to LF in character data, as the XML spec requires.
func bodyText(raw string) string {
	if strings.ContainsRune(raw, '\n') {
		return strings.TrimSpace(raw)
	}
	return raw
}

// BodyText is bodyText for consumers outside this package — an editor
// that round-trips a body, or a nested module whose element declares a
// BodySpec of its own.
//
// It is exported for the same reason ParseThickness is: the alternative
// is every such consumer restating the rule, and a restatement drifts
// silently. apps/wysiwyg is the first of them, and if it trimmed for
// itself its idea of <Text>Text</Text> would differ from the loader's
// for exactly the bodies whose whitespace is deliberate.
func BodyText(raw string) string { return bodyText(raw) }
