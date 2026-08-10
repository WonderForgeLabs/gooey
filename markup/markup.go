// Package markup is the POC of gooey's XAML-analog authoring surface:
// XML elements map to components, attributes to properties, and {{...}}
// expressions (Go-template syntax) to bindings resolved against a
// property registry — no reflection.
//
// A control file may also DECLARE its property surface with
// <x:Property> (see property.go): declared markup properties are
// ordinary dependency properties, registered from markup rather than
// from Go — one property system throughout.
//
// POC scope: builtin builders for Border/Grid/VStack/HStack/Text/Button,
// custom component registration, `{{.Path}}` bindings in text content
// (resolved to *prop.Property[string] handles, becoming computed
// strings), event bindings resolved to gooey.Commands (Click,
// <KeyBinding Command=…>), named elements (Name="...") collected for
// code-behind lookup, and a polling file watcher for hot reload.
package markup

import (
	"encoding/xml"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Element is a parsed markup node.
type Element struct {
	Name string
	// Space is the element name's resolved namespace URI. It is empty
	// for a document with no default xmlns, the default URI for ordinary
	// elements, and the language-services URI for <x:Property> — which
	// is how declarations are told apart from components without
	// reserving any element name.
	Space    string
	Attrs    map[string]string
	Children []Element
	Text     string
	// Props holds this element's PROPERTY ELEMENTS, keyed by the part
	// after the dot: <ItemsView.ItemTemplate> lands under "ItemTemplate"
	// on the ItemsView. They are structured attributes — an attribute
	// whose value is markup rather than a string — so they are handed to
	// the parent's builder and never built as tree children.
	//
	// This is XAML's property-element syntax, and it is the general
	// mechanism, not an ItemTemplate special case: <x:Property> lands
	// here next.
	Props map[string]Element
}

// Builder constructs a component from an element. Custom components receive
// the raw element and can interpret attributes however they like.
type Builder func(e Element, ctx *Context) (gooey.Component, error)

// Context is the binding environment a markup file is built against.
type Context struct {
	// Values resolves {{.Name}} roots. Leaves must be
	// *prop.Property[string] (bound) or string (static).
	Values map[string]any
	// Styles resolves Style="name" attributes.
	Styles map[string]render.Style
	// Components adds custom element builders (e.g. LogPane).
	Components map[string]Builder
	// Handlers is the code-behind side of the event-binding split:
	// Click="OnSave" resolves here, while Click="{{.Save}}" resolves a
	// func in Values. The binding form works in markup-only controls;
	// the bare-name form needs a registry, so it needs code-behind.
	Handlers map[string]gooey.Action
	// Named collects Name="..." components during build — the
	// code-behind lookup surface (Find[T] reads from this).
	Named map[string]gooey.Component
	// Includes, when set, resolves unknown elements by convention: an
	// element <Card/> with no registered builder loads card.gooey from
	// this FS as a markup-only control (see Include). Zero
	// registration, zero code-behind.
	Includes fs.FS
	// Dispatcher marshals handler results onto the UI goroutine. It is
	// required by documents that use handler namespaces
	// ({{net:Get …}}) and unused by everything else.
	Dispatcher *gooey.Dispatcher

	// ns is the document's xmlns prefix → URI table, captured by Build.
	// It is per-document, not per-app: a UserControl's markup declares
	// its own namespaces, so an included file cannot borrow a prefix
	// the page happened to declare.
	ns map[string]string
	// declared is the dependency properties of the control currently
	// being instantiated, installed for the duration of a setup call.
	// See Context.DeclaredProperties.
	declared map[string]any
}

// document is one parsed markup file: its namespace table, the
// dependency properties its root declares, and the single visual child
// that is the control's content. Parsing is separated from building
// because an instantiation site needs the DECLARATIONS before it can
// resolve the attributes that build the context the content is built
// against.
type document struct {
	ns      map[string]string
	decls   declarations
	content Element
}

func parseDocument(src []byte) (*document, error) {
	root, ns, err := parse(src)
	if err != nil {
		return nil, err
	}
	if root.Name != "Gooey" {
		return nil, fmt.Errorf("markup: root element must be <Gooey>, got <%s>", root.Name)
	}
	decls, kids, err := splitDeclarations(root)
	if err != nil {
		return nil, err
	}
	if len(kids) != 1 {
		return nil, fmt.Errorf("markup: <Gooey> must have exactly one child")
	}
	return &document{ns: ns, decls: decls, content: kids[0]}, nil
}

func loadDocument(fsys fs.FS, name string) (*document, error) {
	src, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	doc, err := parseDocument(src)
	if err != nil {
		return nil, &fileError{name: name, err: err}
	}
	return doc, nil
}

// fileError names the file a parse failure came from without producing a
// second "markup: " in the middle of the message. Every error in this
// package leads with that prefix, and a control's failures should read
// the same whether they came from parsing the file or from instantiating
// it: "markup: card.gooey: …".
type fileError struct {
	name string
	err  error
}

func (e *fileError) Error() string {
	return "markup: " + e.name + ": " + strings.TrimPrefix(e.err.Error(), "markup: ")
}

func (e *fileError) Unwrap() error { return e.err }

func (d *document) build(ctx *Context) (gooey.Component, error) {
	// The namespace table belongs to THIS document for the duration of
	// THIS build, and is then restored. Nested Loads (a UserControl
	// instantiated mid-build) would otherwise leave the child's table
	// installed on a shared context, and the page's later siblings would
	// resolve prefixes — that is, capabilities — against the wrong
	// document. Save/restore makes that impossible however a setup func
	// chooses to build its context.
	prev := ctx.ns
	ctx.ns = d.ns
	defer func() { ctx.ns = prev }()

	if ctx.Named == nil {
		ctx.Named = map[string]gooey.Component{}
	}
	return build(d.content, ctx)
}

// Build parses markup and constructs the component tree.
func Build(src []byte, ctx *Context) (gooey.Component, error) {
	doc, err := parseDocument(src)
	if err != nil {
		return nil, err
	}
	return doc.build(ctx)
}

// Find retrieves a named component with its concrete type.
func Find[T gooey.Component](ctx *Context, name string) (T, error) {
	var zero T
	w, ok := ctx.Named[name]
	if !ok {
		return zero, fmt.Errorf("markup: no element named %q", name)
	}
	t, ok := w.(T)
	if !ok {
		return zero, fmt.Errorf("markup: element %q is %T, not %T", name, w, zero)
	}
	return t, nil
}

// Load reads and builds a markup file from any fs.FS — os.DirFS in
// dev, embed.FS in release; the loader cannot tell the difference.
func Load(fsys fs.FS, name string, ctx *Context) (gooey.Component, error) {
	doc, err := loadDocument(fsys, name)
	if err != nil {
		return nil, err
	}
	return doc.build(ctx)
}

// Watch polls name's ModTime in fsys and rebuilds on change, calling
// swap with the new tree. Parse/build errors leave the current tree in
// place. On an immutable FS (embed.FS reports constant zero ModTimes)
// this is a natural no-op — the same call works in dev and release.
// Returns a stop function.
func Watch(fsys fs.FS, name string, ctx *Context, swap func(gooey.Component)) func() {
	stop := make(chan struct{})
	go func() {
		var last time.Time
		if st, err := fs.Stat(fsys, name); err == nil {
			last = st.ModTime()
		}
		t := time.NewTicker(300 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				st, err := fs.Stat(fsys, name)
				if err != nil || !st.ModTime().After(last) {
					continue
				}
				last = st.ModTime()
				ctx.Named = map[string]gooey.Component{}
				w, err := Load(fsys, name, ctx)
				if err != nil {
					continue // keep the old tree on bad edits
				}
				swap(w)
			}
		}
	}()
	return func() { close(stop) }
}

// parse builds the element tree and, alongside it, the document's xmlns
// prefix → URI table. encoding/xml resolves prefixes on *element* names
// but hands namespace declarations back as ordinary attributes
// (xmlns:net="…" arrives as Space="xmlns", Local="net"), so the mapping
// is tracked here rather than read off the tokens. Declarations are kept
// out of Attrs — they configure the document, they are not properties.
func parse(src []byte) (Element, map[string]string, error) {
	dec := xml.NewDecoder(strings.NewReader(string(src)))
	ns := map[string]string{}
	var stack []*Element
	var root *Element
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			e := Element{Name: t.Name.Local, Space: t.Name.Space, Attrs: map[string]string{}}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" {
					ns[a.Name.Local] = a.Value
					continue
				}
				if a.Name.Space == "" && a.Name.Local == "xmlns" {
					continue // the default namespace is decorative versioning
				}
				e.Attrs[a.Name.Local] = a.Value
			}
			stack = append(stack, &e)
		case xml.EndElement:
			e := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				root = e
			} else {
				p := stack[len(stack)-1]
				if strings.Contains(e.Name, ".") {
					if err := attachProp(p, e); err != nil {
						return Element{}, nil, err
					}
					continue
				}
				p.Children = append(p.Children, *e)
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil {
		return Element{}, nil, fmt.Errorf("markup: no root element")
	}
	return *root, ns, nil
}

// attachProp files a dotted element as a property of its parent. The
// prefix must name the parent — <ItemsView.ItemTemplate> is only legal
// inside an <ItemsView> — which is what keeps the syntax readable: the
// dot says "this is not a child, it is a slot on the element around it",
// and naming the wrong owner is a typo the load must catch.
func attachProp(parent, e *Element) error {
	owner, name, _ := strings.Cut(e.Name, ".")
	if owner != parent.Name {
		return fmt.Errorf("markup: <%s> is a property of <%s>, not of <%s>", e.Name, owner, parent.Name)
	}
	if name == "" || strings.Contains(name, ".") {
		return fmt.Errorf("markup: <%s> is not a property name", e.Name)
	}
	if len(e.Attrs) > 0 {
		return fmt.Errorf("markup: <%s> takes no attributes; it is a slot, not an element", e.Name)
	}
	if _, dup := parent.Props[name]; dup {
		return fmt.Errorf("markup: <%s> given twice", e.Name)
	}
	if parent.Props == nil {
		parent.Props = map[string]Element{}
	}
	parent.Props[name] = *e
	return nil
}

// propElements declares which property elements each builtin accepts.
// The declaration is the point: an unknown one is a LOAD error, so
// <Grid.ItemTemplate> is a typo you hear about at startup rather than a
// child that silently disappeared.
var propElements = map[string]map[string]bool{
	"ItemsView": {"ItemTemplate": true},
	"StatusBar": {"Left": true, "Center": true, "Right": true},
}

// checkProps rejects property elements the element cannot accept. A
// registered custom component is exempt — its builder receives the raw
// Element and decides for itself, the same latitude it has with
// attributes.
func checkProps(e Element, ctx *Context) error {
	if len(e.Props) == 0 {
		return nil
	}
	if _, custom := ctx.Components[e.Name]; custom {
		return nil
	}
	allowed := propElements[e.Name]
	names := make([]string, 0, len(e.Props))
	for name := range e.Props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !allowed[name] {
			return fmt.Errorf("markup: <%s> does not accept the property element <%s.%s>", e.Name, e.Name, name)
		}
	}
	return nil
}

func build(e Element, ctx *Context) (gooey.Component, error) {
	if e.Space == XNamespace {
		return nil, fmt.Errorf("markup: <x:%s> must be a direct child of the root <Gooey>: declarations define the control's type, so they belong on the type, not inside its content", e.Name)
	}
	if err := checkProps(e, ctx); err != nil {
		return nil, err
	}
	w, err := buildComponent(e, ctx)
	if err != nil {
		return nil, err
	}
	if err := applyLayout(e, w); err != nil {
		return nil, err
	}
	return w, nil
}

// applyLayout maps the FrameworkElement attributes — and the Grid.*
// attached-property syntax — onto the component's Layout.
func applyLayout(e Element, w gooey.Component) error {
	hl, ok := w.(gooey.HasLayout)
	if !ok {
		return nil
	}
	l := hl.LayoutProps()
	for k, v := range e.Attrs {
		var err error
		switch k {
		case "Width":
			l.Width, err = strconv.Atoi(v)
		case "Height":
			l.Height, err = strconv.Atoi(v)
		case "Margin":
			l.Margin, err = parseThickness(v)
		case "HAlign":
			l.HAlign, err = parseAlign(v)
		case "VAlign":
			l.VAlign, err = parseAlign(v)
		case "Visibility":
			l.Visibility, err = parseVisibility(v)
		case "Grid.Row":
			l.Row, err = strconv.Atoi(v)
		case "Grid.Col":
			l.Col, err = strconv.Atoi(v)
		case "Grid.RowSpan":
			l.RowSpan, err = strconv.Atoi(v)
		case "Grid.ColSpan":
			l.ColSpan, err = strconv.Atoi(v)
		case "Canvas.Left":
			l.Left, err = strconv.Atoi(v)
		case "Canvas.Top":
			l.Top, err = strconv.Atoi(v)
		}
		if err != nil {
			return fmt.Errorf("markup: attribute %s=%q: %w", k, v, err)
		}
	}
	return nil
}

func parseThickness(s string) (gooey.Thickness, error) {
	parts := strings.Split(s, ",")
	ns := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return gooey.Thickness{}, err
		}
		ns[i] = n
	}
	switch len(ns) {
	case 1:
		return gooey.M(ns[0]), nil
	case 2:
		return gooey.MH(ns[0], ns[1]), nil
	case 4:
		return gooey.Thickness{L: ns[0], T: ns[1], R: ns[2], B: ns[3]}, nil
	}
	return gooey.Thickness{}, fmt.Errorf("want 1, 2, or 4 values")
}

func parseAlign(s string) (gooey.Align, error) {
	switch s {
	case "Stretch":
		return gooey.AlignStretch, nil
	case "Start":
		return gooey.AlignStart, nil
	case "Center":
		return gooey.AlignCenter, nil
	case "End":
		return gooey.AlignEnd, nil
	}
	return 0, fmt.Errorf("unknown alignment")
}

func parseVisibility(s string) (gooey.Visibility, error) {
	switch s {
	case "Visible":
		return gooey.Visible, nil
	case "Hidden":
		return gooey.Hidden, nil
	case "Collapsed":
		return gooey.Collapsed, nil
	}
	return 0, fmt.Errorf("unknown visibility")
}

// buildChildren builds an element's children, splitting them into the
// visual ones the parent lays out and the non-visual ones (KeyBindings)
// the framework hangs off the parent as attachments.
func buildChildren(e Element, ctx *Context) (kids, attach []gooey.Component, err error) {
	for _, c := range e.Children {
		w, err := build(c, ctx)
		if err != nil {
			return nil, nil, err
		}
		if nv, ok := w.(gooey.NonVisual); ok && nv.NonVisual() {
			attach = append(attach, w)
		} else {
			kids = append(kids, w)
		}
	}
	return kids, attach, nil
}

func attachAll(e Element, w gooey.Component, attach []gooey.Component) error {
	if len(attach) == 0 {
		return nil
	}
	a, ok := w.(gooey.Attacher)
	if !ok {
		return fmt.Errorf("markup: <%s> cannot host non-visual children", e.Name)
	}
	for _, x := range attach {
		a.Attach(x)
	}
	return nil
}

func buildComponent(e Element, ctx *Context) (gooey.Component, error) {
	if b, ok := ctx.Components[e.Name]; ok {
		w, err := b(e, ctx)
		return named(e, ctx, w, err)
	}
	switch e.Name {
	case "Border":
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		if len(kids) != 1 {
			return nil, fmt.Errorf("markup: <Border> needs exactly one child")
		}
		child := kids[0]
		title, err := bindText(e.Attrs["Title"], ctx)
		if err != nil {
			return nil, err
		}
		if title == nil {
			title = components.Str(e.Attrs["Title"])
		}
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := bindColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		b := &components.Border{
			Child:      child,
			Title:      title,
			Style:      style,
			Background: background,
		}
		if err := attachAll(e, b, attach); err != nil {
			return nil, err
		}
		return named(e, ctx, b, nil)
	case "Grid":
		rows, err := components.ParseGridLens(e.Attrs["Rows"])
		if err != nil {
			return nil, err
		}
		cols, err := components.ParseGridLens(e.Attrs["Cols"])
		if err != nil {
			return nil, err
		}
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := bindColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		g := &components.Grid{Rows: rows, Cols: cols, Children: kids, Background: background}
		if err := attachAll(e, g, attach); err != nil {
			return nil, err
		}
		return named(e, ctx, g, nil)
	case "VStack", "HStack":
		gap, _ := strconv.Atoi(e.Attrs["Gap"])
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := bindColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		var w gooey.Component = &components.HStack{Children: kids, Gap: gap, Background: background}
		if e.Name == "VStack" {
			w = &components.VStack{Children: kids, Gap: gap, Background: background}
		}
		if err := attachAll(e, w, attach); err != nil {
			return nil, err
		}
		return named(e, ctx, w, nil)
	case "Canvas":
		// Children carry their own Canvas.Left/Canvas.Top, parsed into
		// Layout by applyLayout like any other attached property.
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		background, err := bindColor(e, ctx, "Background")
		if err != nil {
			return nil, err
		}
		c := &components.Canvas{Children: kids, Background: background}
		if err := attachAll(e, c, attach); err != nil {
			return nil, err
		}
		return named(e, ctx, c, nil)
	case "ItemsView":
		v, err := buildItemsView(e, ctx)
		return named(e, ctx, v, err)
	case "Checkbox":
		checked, err := boundProp[bool](e, ctx, "Checked")
		if err != nil {
			return nil, err
		}
		label, err := bindText(e.Attrs["Label"], ctx)
		if err != nil {
			return nil, err
		}
		if label == nil {
			label = components.Str(e.Attrs["Label"])
		}
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		return named(e, ctx, &components.Checkbox{
			Checked: checked,
			Label:   label,
			Style:   style,
		}, nil)
	case "Gauge":
		value, err := boundProp[int](e, ctx, "Value")
		if err != nil {
			return nil, err
		}
		label, err := bindText(e.Attrs["Label"], ctx)
		if err != nil {
			return nil, err
		}
		if label == nil {
			label = components.Str(e.Attrs["Label"])
		}
		g := &components.Gauge{Value: value, Label: label}
		g.Width, _ = strconv.Atoi(e.Attrs["BarWidth"])
		// Style is an override for the threshold ramp, so it is applied
		// only when the attribute is actually present.
		if _, ok := e.Attrs["Style"]; ok {
			if g.Style, err = bindStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return named(e, ctx, g, nil)
	case "Sparkline":
		series, err := boundProp[[]float64](e, ctx, "Values")
		if err != nil {
			return nil, err
		}
		s := &components.Sparkline{Values: series}
		s.Rows, _ = strconv.Atoi(e.Attrs["Height"])
		s.Width, _ = strconv.Atoi(e.Attrs["BarWidth"])
		if _, ok := e.Attrs["Style"]; ok {
			if s.Style, err = bindStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return named(e, ctx, s, nil)
	case "TextBox":
		text, err := boundProp[string](e, ctx, "Text")
		if err != nil {
			return nil, err
		}
		changed, err := ctx.Command(e.Attrs["Changed"])
		if err != nil {
			return nil, fmt.Errorf("markup: <TextBox Changed=%q>: %w", e.Attrs["Changed"], err)
		}
		tb := &components.TextBox{Text: text, Changed: changed}
		if p, ok := e.Attrs["Prompt"]; ok {
			prompt, err := bindText(p, ctx)
			if err != nil {
				return nil, err
			}
			if prompt == nil {
				prompt = components.Str(p)
			}
			tb.Prompt = prompt
		}
		if _, ok := e.Attrs["Style"]; ok {
			if tb.Style, err = bindStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		if a, ok := e.Attrs["AccentStyle"]; ok {
			tb.AccentStyle = components.Sty(ctx.Styles[a])
		}
		return named(e, ctx, tb, nil)
	case "ColorPicker":
		color, err := boundProp[render.Color](e, ctx, "Value")
		if err != nil {
			return nil, err
		}
		return named(e, ctx, &components.ColorPicker{Value: color}, nil)
	case "Button":
		content, err := bindText(e.Attrs["Content"], ctx)
		if err != nil {
			return nil, err
		}
		if content == nil {
			content = components.Str(e.Attrs["Content"])
		}
		click, err := ctx.Command(e.Attrs["Click"])
		if err != nil {
			return nil, fmt.Errorf("markup: <Button Click=%q>: %w", e.Attrs["Click"], err)
		}
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		chrome, ok := components.ParseButtonChrome(e.Attrs["Chrome"])
		if !ok {
			return nil, fmt.Errorf("markup: <Button Chrome=%q>: unknown chrome; want one of %s",
				e.Attrs["Chrome"], strings.Join(components.ButtonChromeNames, ", "))
		}
		return named(e, ctx, &components.Button{
			Content: content,
			Style:   style,
			Click:   click,
			Chrome:  chrome,
		}, nil)
	case "ProgressBar":
		value, err := boundProp[int](e, ctx, "Value")
		if err != nil {
			return nil, err
		}
		label, err := literalOrBound(e.Attrs["Label"], ctx)
		if err != nil {
			return nil, err
		}
		p := &components.ProgressBar{Value: value, Label: label}
		p.Width, _ = strconv.Atoi(e.Attrs["BarWidth"])
		p.Thresholds = e.Attrs["Thresholds"] == "true"
		// Indeterminate is optional, and its absence is load-bearing: a
		// bar that can never be indeterminate starts no goroutine.
		if _, ok := e.Attrs["Indeterminate"]; ok {
			if p.Indeterminate, err = boundProp[bool](e, ctx, "Indeterminate"); err != nil {
				return nil, err
			}
		}
		if p.Tick, err = optDuration(e, "Tick"); err != nil {
			return nil, err
		}
		if _, ok := e.Attrs["Style"]; ok {
			if p.Style, err = bindStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return named(e, ctx, p, nil)
	case "Spinner":
		label, err := literalOrBound(e.Attrs["Label"], ctx)
		if err != nil {
			return nil, err
		}
		s := &components.Spinner{Label: label}
		if raw, ok := e.Attrs["Frames"]; ok {
			frames, known := components.SpinnerFrames(raw)
			if !known {
				return nil, fmt.Errorf("markup: <Spinner Frames=%q>: unknown frame set; want one of %s",
					raw, strings.Join(components.SpinnerNames, ", "))
			}
			s.Frames = frames
		}
		if s.Interval, err = optDuration(e, "Interval"); err != nil {
			return nil, err
		}
		if _, ok := e.Attrs["Enabled"]; ok {
			if s.Enabled, err = boundProp[bool](e, ctx, "Enabled"); err != nil {
				return nil, err
			}
		}
		if _, ok := e.Attrs["Style"]; ok {
			if s.Style, err = bindStyle(e, ctx); err != nil {
				return nil, err
			}
		}
		return named(e, ctx, s, nil)
	case "Toggle":
		checked, err := boundProp[bool](e, ctx, "Checked")
		if err != nil {
			return nil, err
		}
		label, err := literalOrBound(e.Attrs["Label"], ctx)
		if err != nil {
			return nil, err
		}
		changed, err := ctx.Command(e.Attrs["Changed"])
		if err != nil {
			return nil, fmt.Errorf("markup: <Toggle Changed=%q>: %w", e.Attrs["Changed"], err)
		}
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		return named(e, ctx, &components.Toggle{
			Checked: checked, Label: label, Changed: changed, Style: style,
		}, nil)
	case "Segmented":
		selected, err := boundProp[int](e, ctx, "Selected")
		if err != nil {
			return nil, err
		}
		options, err := optionList(e, ctx)
		if err != nil {
			return nil, err
		}
		changed, err := ctx.Command(e.Attrs["Changed"])
		if err != nil {
			return nil, fmt.Errorf("markup: <Segmented Changed=%q>: %w", e.Attrs["Changed"], err)
		}
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		return named(e, ctx, &components.Segmented{
			Options: options, Selected: selected, Changed: changed, Style: style,
		}, nil)
	case "StatusBar":
		bar, err := buildStatusBar(e, ctx)
		return named(e, ctx, bar, err)
	case "ButtonBar":
		kids, attach, err := buildChildren(e, ctx)
		if err != nil {
			return nil, err
		}
		bar := &components.ButtonBar{Children: kids, Separator: e.Attrs["Separator"]}
		bar.Gap, _ = strconv.Atoi(e.Attrs["Gap"])
		bar.Uniform = e.Attrs["Uniform"] == "true"
		if err := attachAll(e, bar, attach); err != nil {
			return nil, err
		}
		return named(e, ctx, bar, nil)
	case "KeyBinding":
		g, err := input.ParseGesture(e.Attrs["Gesture"])
		if err != nil {
			return nil, fmt.Errorf("markup: <KeyBinding Gesture=%q>: %w", e.Attrs["Gesture"], err)
		}
		cmd, err := ctx.Command(e.Attrs["Command"])
		if err != nil {
			return nil, fmt.Errorf("markup: <KeyBinding Gesture=%q>: %w", e.Attrs["Gesture"], err)
		}
		return named(e, ctx, &gooey.KeyBinding{Gesture: g, Command: cmd}, nil)
	case "Timer":
		// Non-visual like KeyBinding: buildChildren routes it to the
		// parent as an attachment, and the Composer starts it.
		raw := strings.TrimSpace(e.Attrs["Interval"])
		if raw == "" {
			return nil, fmt.Errorf("markup: <Timer> needs an Interval (e.g. Interval=\"600ms\")")
		}
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("markup: <Timer Interval=%q>: %w", raw, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("markup: <Timer Interval=%q>: must be positive", raw)
		}
		tick, err := ctx.Command(e.Attrs["Tick"])
		if err != nil {
			return nil, fmt.Errorf("markup: <Timer Tick=%q>: %w", e.Attrs["Tick"], err)
		}
		t := &components.Timer{Interval: d, Tick: tick}
		// Enabled is optional; absent means always enabled. When present
		// it is a live bool handle, so the graph can pause the timer.
		if _, ok := e.Attrs["Enabled"]; ok {
			if t.Enabled, err = boundProp[bool](e, ctx, "Enabled"); err != nil {
				return nil, err
			}
		}
		return named(e, ctx, t, nil)
	case "Text":
		style, err := bindStyle(e, ctx)
		if err != nil {
			return nil, err
		}
		if e.Attrs["Bold"] == "true" {
			// Bold composes over either form of Style, so it wraps the
			// handle rather than mutating a value — a bound style stays
			// live and still gets its bold.
			base := style
			style = prop.NewComputed(func() render.Style {
				s := base.Get()
				s.Bold = true
				return s
			})
		}
		t := &components.Text{Style: style}
		content := strings.TrimSpace(e.Text)
		if src, err := bindText(content, ctx); err != nil {
			return nil, err
		} else if src != nil {
			t.Content = src
		} else {
			t.Content = components.Str(content)
		}
		return named(e, ctx, t, nil)
	default:
		if ctx.Includes != nil {
			file := strings.ToLower(e.Name) + ".gooey"
			if _, err := fs.Stat(ctx.Includes, file); err == nil {
				w, err := Include(ctx.Includes, file)(e, ctx)
				return named(e, ctx, w, err)
			}
		}
		return nil, fmt.Errorf("markup: unknown element <%s>", e.Name)
	}
}

func named(e Element, ctx *Context, w gooey.Component, err ...error) (gooey.Component, error) {
	if len(err) > 0 && err[0] != nil {
		return nil, err[0]
	}
	if n := e.Attrs["Name"]; n != "" {
		ctx.Named[n] = w
	}
	return w, nil
}

var bindRe = regexp.MustCompile(`\{\{\s*\.([A-Za-z0-9_.]+)\s*\}\}`)

// bindText turns content with {{.Path}} expressions into a computed
// string property. Pure-literal content returns (nil, nil). Resolution
// happens once at build time — handles, not values — so evaluation
// does no lookups; this is the "lvalue semantics" of the design.
func bindText(content string, ctx *Context) (*prop.Property[string], error) {
	m := bindRe.FindAllStringSubmatchIndex(content, -1)
	if len(m) == 0 {
		return nil, nil
	}
	type part struct {
		lit string
		p   *prop.Property[string]
	}
	var parts []part
	pos := 0
	for _, idx := range m {
		if idx[0] > pos {
			parts = append(parts, part{lit: content[pos:idx[0]]})
		}
		path := content[idx[2]:idx[3]]
		v, err := resolve(ctx.Values, path)
		if err != nil {
			return nil, err
		}
		switch h := v.(type) {
		case *prop.Property[string]:
			parts = append(parts, part{p: h})
		case string:
			parts = append(parts, part{lit: h})
		default:
			return nil, fmt.Errorf("markup: {{.%s}} is %T; need *prop.Property[string] or string", path, v)
		}
		pos = idx[1]
	}
	if pos < len(content) {
		parts = append(parts, part{lit: content[pos:]})
	}
	return prop.NewComputed(func() string {
		var sb strings.Builder
		for _, p := range parts {
			if p.p != nil {
				sb.WriteString(p.p.Get())
			} else {
				sb.WriteString(p.lit)
			}
		}
		return sb.String()
	}), nil
}

// bindStyle resolves the Style attribute, which accepts either form:
// a bare name is the static lookup in Context.Styles, and a binding
// expression yields the viewmodel's own *prop.Property[render.Style]
// handle. The bound form is what makes a style REACTIVE — a computed
// style over an accent color repaints the components that read it, through
// the ordinary property graph, with no styling system involved.
func bindStyle(e Element, ctx *Context) (*prop.Property[render.Style], error) {
	raw := e.Attrs["Style"]
	if bindRe.MatchString(raw) {
		return boundProp[render.Style](e, ctx, "Style")
	}
	return components.Sty(ctx.Styles[raw]), nil
}

// bindColor resolves a color attribute: the #rgb/#rrggbb literal markup
// already speaks (propKinds "color"), or a binding to the viewmodel's own
// *prop.Property[render.Color] handle. An absent attribute yields nil —
// for Background that keeps the container on the chrome-only damage
// path, so only pages that declare a fill pay for one.
func bindColor(e Element, ctx *Context, attr string) (*prop.Property[render.Color], error) {
	raw, ok := e.Attrs[attr]
	if !ok || raw == "" {
		return nil, nil
	}
	if bindRe.MatchString(raw) {
		return boundProp[render.Color](e, ctx, attr)
	}
	col, err := parseHexColor(raw)
	if err != nil {
		return nil, fmt.Errorf("markup: <%s %s=%q>: %w", e.Name, attr, raw, err)
	}
	return components.Col(col), nil
}

// boundProp resolves an attribute that must be a typed property HANDLE
// rather than text: <Checkbox Checked="{{.Auto}}"/> shares the
// viewmodel's property with the component, so the component's Render reads it
// and its toggle Sets it — the only sense in which gooey has two-way
// binding, and the reason it needs no converter machinery.
//
// The type assertion is the whole type check. There is no reflection
// here: T is known at the call site, so a mismatched viewmodel property
// is a load-time error naming both types.
func boundProp[T any](e Element, ctx *Context, attr string) (*prop.Property[T], error) {
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

func resolve(values map[string]any, path string) (any, error) {
	segs := strings.Split(path, ".")
	var cur any = values
	for _, s := range segs {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("markup: cannot resolve %q past %T", path, cur)
		}
		cur, ok = m[s]
		if !ok {
			return nil, fmt.Errorf("markup: %q not found in context", path)
		}
	}
	return cur, nil
}
