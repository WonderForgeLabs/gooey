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
// (resolved to typed property handles — see textBindableTypes for the
// scalars a label can render — becoming computed strings), event
// bindings resolved to gooey.Commands (Click,
// <KeyBinding Command=…>), named elements (Name="...") collected for
// code-behind lookup, and a polling file watcher for hot reload.
package markup

import (
	"encoding/xml"
	"fmt"
	"io/fs"
	"path"
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

	// parent is the enclosing element's name, stamped during parsing.
	// It exists for attached-property validation: Grid.Row is only
	// meaningful on a child of a <Grid>, so the check needs to know what
	// the child is actually inside. Unexported because it is derived
	// from the document rather than written in it — a caller
	// constructing an Element by hand has no parent to declare.
	parent string
}

// Builder constructs a component from an element. Custom components receive
// the raw element and can interpret attributes however they like.
type Builder func(e Element, ctx *Context) (gooey.Component, error)

// Context is the binding environment a markup file is built against.
type Context struct {
	// Values resolves {{.Name}} roots. In text position a leaf may be a
	// *prop.Property[T] (bound) or a plain T (static) for any T in
	// textBindableTypes, not string alone; attributes that REQUIRE a
	// binding are narrower and say so at their own call sites.
	Values map[string]any
	// Styles resolves Style="name" attributes.
	Styles map[string]render.Style
	// Components adds custom element builders (e.g. LogPane).
	//
	// A Builder is a FUNC, so an element registered here contributes a
	// name and nothing else: Catalog gives it AttrsKnown false, and
	// checkAttrs declines to validate anything on it. Prefer Elements
	// below when the component has an attribute vocabulary worth
	// declaring; this map remains correct for genuinely dynamic builders
	// and for the many hosts that were written before Elements existed.
	Components map[string]Builder
	// Elements adds custom elements WITH A DECLARATION — the same
	// *ElementDef the built-in vocabulary is made of, so a host's own
	// component is describable rather than merely nameable.
	//
	// The gap this closes is not cosmetic. The first consumer of the
	// catalog was the wysiwyg palette, which seeds an inserted element's
	// REQUIRED attributes (AttrSpec.Required + GoType) so that clicking a
	// palette entry produces markup that loads. A component registered
	// through Components has no Attrs for it to read, so the palette
	// emitted <ActivityBar Name="ActivityBar1"/> — and ActivityBar
	// requires a bound Sel=, so the insert failed to load with an error
	// naming an attribute the user was never offered a way to supply.
	//
	// Declaring also buys the check, and that is the larger half: with a
	// schema in hand, checkAttrs stops declining (attrcheck.go) and an
	// unknown attribute on a registered element becomes a load error with
	// a near-miss suggestion, exactly as on a built-in. Registering
	// through Components means a typo is silently ignored forever.
	//
	// Resolution order is Elements, then Components, then the built-in
	// registry, then the Includes convention. A name in BOTH maps is a
	// load error rather than a silent winner: which one won would
	// otherwise depend on nothing a reader can see.
	Elements map[string]*ElementDef
	// Handlers is the code-behind side of the event-binding split:
	// Click="OnSave" resolves here, while Click="{{.Save}}" resolves a
	// func in Values. The binding form works in markup-only controls;
	// the bare-name form needs a registry, so it needs code-behind.
	Handlers map[string]gooey.Action
	// Rules extends the <Validate> vocabulary: an attribute that is not
	// a built-in rule resolves here, the constructor receives the
	// attribute's literal and may reject it at load. Registration is the
	// grant, like Components and Handlers — rule bodies stay code, pages
	// keep the whole validation story in markup. An app that registers
	//
	//	ctx.Rules = map[string]markup.RuleFunc{
	//	    "Email": func(string) (validate.Rule[string], error) {
	//	        return validate.Pattern(`^[^@\s]+@[^@\s]+$`, "not an email"), nil
	//	    },
	//	}
	//
	// writes <Validate Email="true"/>. Built-in names win over
	// registered ones.
	Rules map[string]RuleFunc
	// Named collects Name="..." components during build — the
	// code-behind lookup surface (Find[T] reads from this).
	Named map[string]gooey.Component
	// Declared collects the resolved <x:Property> surface of every
	// control instance built through this context, keyed by the
	// instance's root component. Unlike Named — which is deliberately
	// scoped per instance — this registry is PAGE-WIDE: child contexts
	// inherit the same map, so nested control instances are visible from
	// the context the page was built against. That is what lets an
	// inspection surface (the MCP tree snapshot) report a control's
	// declared properties without reflection: the declarations are the
	// only property schema a markup-built component has.
	//
	// Created on demand at the first control instantiation; nil until
	// then. Rebuilding a page should reset it the way Named is reset —
	// Page.Build and Watch do — or stale instances accumulate.
	Declared map[gooey.Component]DeclaredSurface
	// Includes, when set, resolves unknown elements by convention: an
	// element <Card/> with no registered builder loads card.gooey from
	// this FS as a markup-only control (see Include). Zero
	// registration, zero code-behind.
	Includes fs.FS
	// Dispatcher marshals handler results onto the UI goroutine. It is
	// required by documents that use handler namespaces
	// ({{net:Get …}}) and unused by everything else.
	Dispatcher *gooey.Dispatcher
	// Dir is the OS directory this document's HOST-SIDE paths resolve
	// against: a <Companion>'s working directory and its log file. Set it
	// to the same directory the page's fs.FS was rooted at —
	//
	//	app = gooey.NewApp(markup.Page(os.DirFS(dir), "page.gooey", ctx))
	//	ctx.Dir = dir
	//
	// — because fs.FS cannot answer the question: os.DirFS(dir) offers no
	// way back to dir, and a child process needs a real path (chdir and
	// open do not take an fs.FS). Document-relative is the right default
	// for a configuration file, the way <Image Src=…> resolves against the
	// FS the document came from.
	//
	// Empty means the process's working directory, which is what a tree
	// built from bytes gets.
	Dir string

	// Variant specializes every document load on the terminal's pixel
	// protocol: with Variant "sixel", "page.gooey" resolves to
	// "page.sixel.gooey" when that file exists and to "page.gooey" when it
	// does not. See VariantOf for why this axis is a FILE rather than a
	// branch inside a component.
	//
	// Set it from the resolved encoder — App.Graphics().Name(), or "cells"
	// where there is none — AFTER capability detection, since before the
	// probe the honest answer is "unknown" and the base document is right.
	//
	// Empty disables the lookup entirely, which is what every existing app
	// gets: one document, loaded by the name it was asked for.
	Variant string

	// fsys is the file system the current document was LOADED from,
	// installed by Load (and by control instantiation) for the duration
	// of the build — the same document-scoped save/restore the xmlns
	// table uses. It is what a literal <Image Src="logo.png"> resolves
	// against: assets ship beside the markup that names them, in
	// whichever FS that markup came from. A tree built from bytes
	// (markup.Build) has none, and falls back to Includes.
	fsys fs.FS
	// ns is the document's xmlns prefix → URI table, captured by Build.
	// It is per-document, not per-app: a UserControl's markup declares
	// its own namespaces, so an included file cannot borrow a prefix
	// the page happened to declare.
	ns map[string]string
	// declared is the dependency properties of the control currently
	// being instantiated, installed for the duration of a setup call.
	// See Context.DeclaredProperties.
	declared map[string]any
	// res is the document's resource environment: the innermost
	// <X.Resources> scope of the element currently being built, plus the
	// document scope (resources.go). The zero value means "no
	// markup-declared resources", and every lookup then falls through to
	// Styles — which is why every existing document loads unchanged.
	res resourceEnv
	// controls is the chain of markup controls currently being
	// instantiated, outermost first — the ANCESTRY of the element being
	// built, not a set of everything seen. A control appearing twice in
	// it is a cycle (control.go), and a cycle here is a stack overflow at
	// LOAD time, before layout ever runs.
	//
	// It lives on the Context rather than in a package variable because
	// markup.Load is not confined to the UI goroutine — a file watcher
	// can load a document on its own goroutine — so shared mutable state
	// here would be a data race, and two concurrent loads would see each
	// other's ancestry.
	//
	// Ancestry, not history, is what makes sibling reuse legal: two
	// <Card/> elements side by side each extend their own copy, so
	// neither sees the other.
	controls []string
}

// document is one parsed markup file: its namespace table, the
// dependency properties its root declares, and the single visual child
// that is the control's content. Parsing is separated from building
// because an instantiation site needs the DECLARATIONS before it can
// resolve the attributes that build the context the content is built
// against.
type document struct {
	ns       map[string]string
	decls    declarations
	content  Element
	settings PageSettings
	res      *resourceBlock // <Gooey.Resources>; nil when the document declares none
}

// PageSettings are the declarations that belong to the DOCUMENT rather
// than to any element in it — attributes on <Gooey> itself. They are
// separated from everything else in this package because the host needs
// them BEFORE it builds a tree: an App is constructed with its options
// and only then asked for content, so a knob that changes construction
// cannot be discovered from the built component.
//
// Read them with ReadPageSettings, which parses without building.
type PageSettings struct {
	// Graphics forces the image protocol: kitty, sixel, iterm2 or
	// halfblock. Empty — the default — lets the terminal's capabilities
	// decide, which is right for an app that ships to unknown terminals.
	//
	// It lives in the document because it is a property of the ARTWORK a
	// page carries, not of the machine it runs on: a page built around a
	// detailed SVG wants real pixels wherever it runs, and detection
	// answers for whoever launched the process — which is the wrong
	// terminal whenever the app was started from a script, a recording
	// pty, or a supervisor.
	Graphics string
}

// gooeyAttrs are the attributes <Gooey> itself accepts. Anything else on
// the root is a load error rather than a silent no-op, for the same
// reason it is on every other element: an attribute that does nothing
// looks exactly like one that works.
var gooeyAttrs = map[string]bool{"Graphics": true}

// graphicsModes is the closed set Graphics accepts. Keep it in step with
// the encoders a host can install; an unknown value fails at load rather
// than falling back, because falling back silently is how a page ends up
// rendering as blocks with no explanation.
var graphicsModes = map[string]bool{
	"kitty": true, "sixel": true, "iterm2": true, "halfblock": true,
}

// ReadPageSettings parses a document far enough to read its <Gooey>
// attributes and no further. It builds nothing, binds nothing, and needs
// no Context — so a host can consult it while assembling the options it
// will construct the App with.
func ReadPageSettings(fsys fs.FS, name string) (PageSettings, error) {
	doc, err := loadDocument(fsys, name)
	if err != nil {
		return PageSettings{}, err
	}
	return doc.settings, nil
}

func parseDocument(src []byte) (*document, error) {
	root, ns, err := parse(src)
	if err != nil {
		return nil, err
	}
	if root.Name != "Gooey" {
		return nil, fmt.Errorf("markup: root element must be <Gooey>, got <%s>", root.Name)
	}
	settings, err := readGooeyAttrs(root.Attrs)
	if err != nil {
		return nil, err
	}
	res, err := rootResources(root)
	if err != nil {
		return nil, err
	}
	decls, kids, err := splitDeclarations(root)
	if err != nil {
		return nil, err
	}
	if len(kids) != 1 {
		return nil, fmt.Errorf("markup: <Gooey> must have exactly one child")
	}
	return &document{ns: ns, decls: decls, content: kids[0], settings: settings, res: res}, nil
}

// readGooeyAttrs validates the root's own attributes. xmlns declarations
// are consumed by the parser before this sees them, so anything left is
// either a document setting or a mistake.
func readGooeyAttrs(attrs map[string]string) (PageSettings, error) {
	var s PageSettings
	for k, v := range attrs {
		if !gooeyAttrs[k] {
			return s, fmt.Errorf("markup: <Gooey> has no attribute %q; it takes %s", k, quotedKeys(gooeyAttrs))
		}
		switch k {
		case "Graphics":
			if !graphicsModes[v] {
				return s, fmt.Errorf("markup: <Gooey Graphics=%q>: unknown mode; want %s", v, quotedKeys(graphicsModes))
			}
			s.Graphics = v
		}
	}
	return s, nil
}

func quotedKeys(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, strconv.Quote(k))
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// Variant is the per-protocol suffix a document may specialize on:
// "kitty", "sixel", "iterm2", or "cells" where there is no pixel plane.
//
// It is Xamarin's platform-specific XAML, applied to the axis that
// actually varies in a terminal. A component's own tier check is binary —
// pixels or cells, `f.Graphics == nil || f.CellW <= 0 || f.CellH <= 0` —
// and that is the right shape for a component, because what changes
// between protocols is not what the component IS. It is what the terminal
// can be asked for, and that is where the protocols actually differ:
//
//   - sixel has NO ALPHA and 256 registers, so a translucent shadow is
//     simply not expressible; a transparent pixel is one with no register;
//   - kitty has real alpha AND image identity (graphics.IDEncoder), so a
//     placement can be moved or deleted without resending pixels;
//   - cells has neither, and every pixel component falls back to runes.
//
// All three conditions in that check are load-bearing: an encoder scales
// to cols*CellW × rows*CellH, so a protocol pinned without capabilities
// behind it asks for an image of zero pixels — and taking the pixel
// branch is what stops halfblock from painting the cells underneath, so
// the failure is a blank rectangle with no error anywhere.
//
// This paragraph used to end by NAMING the components that follow the
// rule, and the census went stale twice in two directions (#260): first
// listing image.go as compliant while image.go asked only the first
// condition (#251), then correcting the conditions while keeping the
// list, which promptly missed paint/shapes (#258). It was wrong a third
// way nobody caught, because a list invites you to check membership
// rather than content: two of the four files it named do not write that
// expression at all, they write its De Morgan dual, and a third reaches
// the cell size through locals. Derive it instead —
//
//	git grep -n -e 'Graphics == nil' -e 'Graphics != nil' -- '*.go'
//
// — and read what you get rather than counting it. Not every hit is a
// tier decision: Composer.placementOps asks Graphics alone and is right
// to, because it decides whether placements exist at all rather than
// which tier to paint. That exception is why this is a grep for a reader
// and not an assertion in a test.
//
// A layout that wants to spend those differently — a denser grid where
// chrome is cheap, a plainer one where it is not — says so in a FILE
// rather than in a branch inside a component. That keeps the difference
// where a designer can see it, which is the whole argument for markup.
//
// Empty means no specialization: every document resolves to its base name.
func VariantOf(protocol string) string { return protocol }

// resolve picks the most specific file that exists: "page.kitty.gooey"
// before "page.gooey". A missing variant is not an error — it is the
// ordinary case, and falling back is the point.
//
// The suffix goes before the extension rather than after so the files sort
// together and keep their .gooey type: page.gooey, page.kitty.gooey.
func resolveVariant(fsys fs.FS, name, variant string) string {
	if variant == "" {
		return name
	}
	v := variantName(name, variant)
	if v == name {
		return name
	}
	if _, err := fs.Stat(fsys, v); err != nil {
		return name
	}
	return v
}

// variantName inserts the variant before the final extension.
func variantName(name, variant string) string {
	if variant == "" {
		return name
	}
	ext := path.Ext(name)
	if ext == "" {
		return name + "." + variant
	}
	return strings.TrimSuffix(name, ext) + "." + variant + ext
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
	// The document's own scope, pushed for this build and popped after —
	// the same save/restore the xmlns table above gets, and for the same
	// reason: a control instantiated mid-build must not leave its scope
	// installed on a shared context.
	pop, err := ctx.pushDocumentResources(d.res)
	if err != nil {
		return nil, err
	}
	defer pop()

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
//
// The name is resolved through Context.Variant first, so a page with a
// protocol-specific sibling gets it and one without is unaffected.
func Load(fsys fs.FS, name string, ctx *Context) (gooey.Component, error) {
	name = resolveVariant(fsys, name, ctx.Variant)
	doc, err := loadDocument(fsys, name)
	if err != nil {
		return nil, err
	}
	prev := ctx.fsys
	ctx.fsys = fsys
	defer func() { ctx.fsys = prev }()
	return doc.build(ctx)
}

// assets is the FS literal asset paths (Image Src) resolve against: the
// FS the document was loaded from, else the Includes FS — a tree built
// from raw bytes still has a natural place for its pictures to live.
func (ctx *Context) assets() fs.FS {
	if ctx.fsys != nil {
		return ctx.fsys
	}
	return ctx.Includes
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
				ctx.Declared = nil
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
				if a.Name.Space == "xmlns" && a.Value == "" {
					return Element{}, nil, fmt.Errorf("markup: <%s xmlns:%s=\"\">: namespace prefix %q cannot be empty", t.Name.Local, a.Name.Local, a.Name.Local)
				}
			}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" {
					ns[a.Name.Local] = a.Value
					continue
				}
				if a.Name.Space == "" && a.Name.Local == "xmlns" {
					continue // the default namespace is decorative versioning
				}
				if a.Name.Space != "" {
					return Element{}, nil, namespacedAttrError(t.Name.Local, a)
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
				// The parent's name travels with the child so an
				// attached property can be checked against the parent
				// that actually contributes it: Canvas.Left is
				// meaningful under a <Canvas> and nowhere else.
				e.parent = p.Name
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

func namespacedAttrError(element string, attr xml.Attr) error {
	name := attr.Name.Local
	if attr.Name.Space == "http://www.w3.org/XML/1998/namespace" {
		name = "xml:" + name
	} else {
		name = "{" + attr.Name.Space + "}" + name
	}
	return fmt.Errorf("markup: <%s %s=%q>: namespaced attributes are not supported; use an unprefixed attribute", element, name, attr.Value)
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
	"Companion": {"Args": true, "Env": true},
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
		// <X.Behaviors> is universal: every element may carry the
		// attachment slot (buildChildren consumes it), the same way every
		// element may carry bare non-visual children. <X.Resources> is
		// universal for the same reason: it is an ELEMENT-level slot, like
		// Name and the layout attributes, and no builder ever sees it.
		if name == "Behaviors" || name == "Resources" {
			continue
		}
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
	// The element's own <X.Resources> covers the element itself and
	// everything built beneath it — children build inside buildComponent
	// — and is popped on the way out. That is what makes shadowing
	// LEXICAL rather than a priority number.
	pop, err := ctx.pushResources(e.Props["Resources"])
	if err != nil {
		return nil, err
	}
	defer pop()

	if err := checkAttrs(e, ctx); err != nil {
		return nil, err
	}
	w, err := buildComponent(e, ctx)
	if err != nil {
		return nil, err
	}
	if err := applyLayout(e, w, ctx); err != nil {
		return nil, err
	}
	if err := applyTooltipShorthand(e, w, ctx); err != nil {
		return nil, err
	}
	return w, nil
}

// applyTooltipShorthand is the Tooltip="..." attribute every element
// accepts: sugar for a child <Tooltip Text="..."/>, attached the same
// way. Like the layout attributes it belongs to the ELEMENT, not to the
// component's own attribute vocabulary, which is why it is applied here
// beside applyLayout and filtered from control pass-through.
func applyTooltipShorthand(e Element, w gooey.Component, ctx *Context) error {
	raw, ok := e.Attrs["Tooltip"]
	if !ok || raw == "" {
		return nil
	}
	text, err := literalOrBound(raw, ctx)
	if err != nil {
		return err
	}
	a, ok := w.(gooey.Attacher)
	if !ok {
		return fmt.Errorf("markup: <%s Tooltip=%q>: <%s> cannot host attachments", e.Name, raw, e.Name)
	}
	a.Attach(&components.Tooltip{Text: text})
	return nil
}

// applyLayout maps the FrameworkElement attributes — and the Grid.*
// attached-property syntax — onto the component's Layout. Visibility is
// the one layout attribute that binds (ctx is here for it): the three
// literal names keep parsing exactly as before, and a {{...}} expression
// resolves to a live handle at build time, lvalue semantics like every
// other binding.
func applyLayout(e Element, w gooey.Component, ctx *Context) error {
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
			// isCondExpr is asked here and not left to bindVisibility
			// because THIS is the fork between "resolve a handle" and
			// "parse the word Visible": without it a conditional falls
			// through to parseVisibility and fails as an unknown
			// visibility word, naming the wrong grammar in the error.
			if bindRe.MatchString(v) || isCondExpr(v) {
				// The bind error carries its own element context; the
				// generic attribute wrap below would only repeat it.
				if err := bindVisibility(e, ctx, l, v); err != nil {
					return err
				}
				continue
			}
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

// ParseThickness reads MAUI's Thickness syntax — "4", "4,2", or
// "4,2,4,2" for left,top,right,bottom — for a custom component that takes
// a Padding or an inset of its own.
//
// Exported so a component outside this package spells its padding the way
// Margin is already spelled everywhere else. A second parser would drift:
// this is the one that decides whether "1,2" means horizontal/vertical or
// left/top, and the answer has to be the same in every element.
func ParseThickness(s string) (gooey.Thickness, error) { return parseThickness(s) }

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

// bindVisibility resolves Visibility="{{...}}" to a live handle. Two
// handle types are accepted: *prop.Property[gooey.Visibility] for the
// full three-state surface, and *prop.Property[bool] mapped true→Visible
// / false→Collapsed — the XAML BooleanToVisibilityConverter default,
// built in because show/hide state is almost always a bool and gooey has
// no converter layer. The type switch is the whole type check, same as
// Bound: a mismatched handle is a load-time error naming both
// acceptable types.
func bindVisibility(e Element, ctx *Context, l *gooey.Layout, raw string) error {
	v, err := ctx.BindingValue(raw)
	if err != nil {
		return fmt.Errorf("markup: <%s Visibility=%q>: %w", e.Name, raw, err)
	}
	switch h := v.(type) {
	case *prop.Property[gooey.Visibility]:
		l.BindVisibility(h)
	case *prop.Property[bool]:
		l.BindVisibilityBool(h)
	default:
		return fmt.Errorf("markup: <%s Visibility=%q> is %T; need *prop.Property[gooey.Visibility] or *prop.Property[bool] (true=Visible, false=Collapsed)", e.Name, raw, v)
	}
	return nil
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
//
// The <X.Behaviors> property element is MAUI's explicit spelling of the
// same slot: its children are attachments only, appended to the very
// list the bare form feeds — two spellings, one downstream path. Bare
// non-visual children stay as the terse shorthand.
// BuildChildren builds an element's children for a REGISTERED component's
// Builder, splitting them the way every builtin container gets them:
// visual children in kids, non-visual ones (KeyBindings, Tooltips,
// Companions) in attach, for the caller to return from Attachments.
//
// It exists because a Builder receives an Element whose Children are
// unbuilt markup, and until this was exported there was no way for a
// custom component to have markup children at all. The tiers that could
// — Include and UserControl — build a FILE, so their children come from
// the control's own markup rather than from the instantiation site. A
// custom container wants the opposite.
//
// A caller that ignores attach silently drops every KeyBinding written
// inside it, which is the same class of defect as dropping an unknown
// attribute: return them from Attachments, or reject them.
func BuildChildren(e Element, ctx *Context) (kids, attach []gooey.Component, err error) {
	return buildChildren(e, ctx)
}

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
	if b, ok := e.Props["Behaviors"]; ok {
		for _, c := range b.Children {
			w, err := build(c, ctx)
			if err != nil {
				return nil, nil, err
			}
			nv, ok := w.(gooey.NonVisual)
			if !ok || !nv.NonVisual() {
				return nil, nil, fmt.Errorf("markup: <%s.Behaviors> holds non-visual attachments only; <%s> is a visual child and belongs in <%s> itself", e.Name, c.Name, e.Name)
			}
			attach = append(attach, w)
		}
	}
	return kids, attach, nil
}

func attachAll(e Element, w gooey.Component, attach []gooey.Component) error {
	for _, x := range attach {
		// A Validate nobody wired means the host's builder does not speak
		// validation — attaching it silently would be a rule that never
		// runs.
		if v, ok := x.(*Validate); ok && v.Error == nil {
			return fmt.Errorf("markup: <%s> does not support <Validate>; it belongs on an input element with a bound text source", e.Name)
		}
		// A TypeAhead searches its host's items, so a host with no items
		// is a search that would silently never fire. Unlike Validate —
		// whose wiring is done by the host's own builder before this
		// point — the host link is made later, by the input-tree walk, so
		// the check is on the host TYPE rather than on the attachment.
		if _, ok := x.(*components.TypeAhead); ok {
			if _, list := w.(*components.ItemsView); !list {
				return fmt.Errorf("markup: <%s> does not support <TypeAhead>; it belongs on an <ItemsView>", e.Name)
			}
		}
	}
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
	// A host DECLARATION outranks a host builder, and a name in both is
	// refused rather than resolved. Two registrations for one element
	// means one of them is unreachable, and which one wins would depend
	// on the order these ifs happen to be written in — the same reason
	// registerElements panics on a duplicate builtin.
	if d, ok := ctx.Elements[e.Name]; ok {
		if _, dup := ctx.Components[e.Name]; dup {
			return nil, fmt.Errorf("markup: <%s> is registered in both Context.Elements and Context.Components; one of them is unreachable, so declare it once", e.Name)
		}
		w, err := d.Build(e, ctx)
		return named(e, ctx, w, err)
	}
	if b, ok := ctx.Components[e.Name]; ok {
		w, err := b(e, ctx)
		return named(e, ctx, w, err)
	}
	// The element registry (elements.go) is the vocabulary: each entry
	// carries what may be set on the element beside the code that reads
	// it. Naming is applied HERE, once, so no definition repeats it.
	if d, ok := elementDefs[e.Name]; ok {
		w, err := d.Build(e, ctx)
		return named(e, ctx, w, err)
	}
	// No definition and no registered builder: the Includes
	// convention is the last resort.
	if ctx.Includes != nil {
		file := strings.ToLower(e.Name) + ".gooey"
		if _, err := fs.Stat(ctx.Includes, file); err == nil {
			w, err := Include(ctx.Includes, file)(e, ctx)
			return named(e, ctx, w, err)
		}
	}
	return nil, fmt.Errorf("markup: unknown element <%s>", e.Name)
}

// buildMenuBar consumes its children as DATA rather than as components:
// <Menu> and <MenuItem> are declarations of the bar's contents, the way
// Grid track lists are, so they never enter the visual tree and never
// reach the general builder. Gesture attributes are validated through
// input.ParseGesture at load time — a hint that cannot parse is a typo
// you hear about at startup — and stored in the canonical spelling.
func buildMenuBar(e Element, ctx *Context) (gooey.Component, error) {
	style, err := BoundStyle(e, ctx)
	if err != nil {
		return nil, err
	}
	var menus []components.Menu
	for _, c := range e.Children {
		if c.Name != "Menu" {
			return nil, fmt.Errorf("markup: <MenuBar> children must be <Menu> elements, got <%s>", c.Name)
		}
		title := strings.TrimSpace(c.Attrs["Title"])
		if title == "" {
			return nil, fmt.Errorf("markup: <Menu> needs a Title")
		}
		menu := components.Menu{Title: title}
		for _, ic := range c.Children {
			if ic.Name != "MenuItem" {
				return nil, fmt.Errorf("markup: <Menu> children must be <MenuItem> elements, got <%s>", ic.Name)
			}
			if ic.Attrs["Separator"] == "true" {
				menu.Items = append(menu.Items, components.MenuItem{Separator: true})
				continue
			}
			it := components.MenuItem{Text: ic.Attrs["Text"]}
			if it.Text == "" {
				return nil, fmt.Errorf("markup: <MenuItem> needs Text (or Separator=\"true\")")
			}
			if g := ic.Attrs["Gesture"]; g != "" {
				ev, err := input.ParseGesture(g)
				if err != nil {
					return nil, fmt.Errorf("markup: <MenuItem Gesture=%q>: %w", g, err)
				}
				it.Gesture = ev.String()
			}
			cmd, err := ctx.Command(ic.Attrs["Command"])
			if err != nil {
				return nil, fmt.Errorf("markup: <MenuItem Command=%q>: %w", ic.Attrs["Command"], err)
			}
			it.Action = cmd
			menu.Items = append(menu.Items, it)
		}
		menus = append(menus, menu)
	}
	return &components.MenuBar{Menus: menus, Style: style}, nil
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

// textBindableTypes names, for the load error, every type a {{.Path}}
// in text position can render. It is prose, so it can drift from the
// switch in textSource — TestTextBindableTypesListMatchesTheSwitch
// binds a handle of every name in it and refuses every type absent
// from it, which is what keeps the two honest.
const textBindableTypes = "string, int, int64, float64, bool, time.Duration, render.Color"

// hexDigits backs colorText. Hand-rolled rather than fmt.Sprintf
// because this runs on every evaluation of a text computed, i.e. inside
// the repaint path.
const hexDigits = "0123456789abcdef"

// colorText renders a color the way the rest of the repo already
// writes one: "#rrggbb", and the EMPTY STRING for an unset color.
//
// Both halves are borrowed, not invented. "#rrggbb" is the only color
// literal gooey's markup accepts (paint.ParseColor documents it as
// exactly that, kept identical so a Stroke and a Style are written the
// same way), so a color that is displayed can be pasted back into an
// attribute. The empty string for Set==false is the control plane's
// own answer (mcp's hexColor), which matters because a theme editor
// can read the same color through markup and through the MCP surface
// and must not see two different renderings of "no color".
func colorText(c render.Color) string {
	if !c.Set {
		return ""
	}
	b := [7]byte{'#',
		hexDigits[c.R>>4], hexDigits[c.R&0xf],
		hexDigits[c.G>>4], hexDigits[c.G&0xf],
		hexDigits[c.B>>4], hexDigits[c.B&0xf],
	}
	return string(b[:])
}

// textSource turns one resolved binding value into a string producer,
// or reports that its type has no text rendering.
//
// THE RETURNED CLOSURE MUST ONLY BE CALLED FROM INSIDE AN EVALUATING
// COMPUTED. That is the whole mechanism: the handle's Get lives in the
// closure body rather than here, so it runs with the text computed on
// prop's evalStack and records a dependency (prop/prop.go recordRead).
// Converting eagerly — reading h.Get() at build time and closing over
// the string — compiles, loads, paints the right characters once, and
// then the label never updates again. Nothing in the framework catches
// that; only a damage-count assertion does.
//
// The formats are deliberately the CANONICAL form of each type, not a
// display policy:
//
//   - int/int64: base 10. There is no other candidate.
//   - float64: strconv 'f' with -1 precision — the shortest decimal
//     that parses back to the same float64. Lossless, and 'f' never
//     switches to an exponent, so a plain 1234567.5 does not surface
//     as "1.2345675e+06" the way %v would. It does mean 1.0/3 renders
//     all seventeen digits; a label that wants two decimals wants a
//     converter, not a different default here.
//   - bool: "true"/"false", the Go form. "Yes"/"No" and "on"/"off" are
//     display policy and, worse, a localization question.
//   - time.Duration: the type's own String — "1h32m0s", "320ms" —
//     which round-trips through time.ParseDuration. format.DurationShort
//     renders the prettier "1h32m", and being prettier is exactly why
//     it is a choice the author makes rather than one baked in here.
//   - render.Color: see colorText.
//
// Anything more opinionated belongs to the converter-stage grammar the
// format package is already written for ({{.Size | bytes}}, #99) or to
// a format constructor in the viewmodel. This switch exists to delete
// the strconv adapter that every numeric label needs today, not to
// become a formatting language.
func textSource(v any) (func() string, bool) {
	switch h := v.(type) {
	case *prop.Property[string]:
		return h.Get, true
	case *prop.Property[int]:
		return func() string { return strconv.Itoa(h.Get()) }, true
	case *prop.Property[int64]:
		return func() string { return strconv.FormatInt(h.Get(), 10) }, true
	case *prop.Property[float64]:
		return func() string { return strconv.FormatFloat(h.Get(), 'f', -1, 64) }, true
	case *prop.Property[bool]:
		return func() string { return strconv.FormatBool(h.Get()) }, true
	case *prop.Property[time.Duration]:
		return func() string { return h.Get().String() }, true
	case *prop.Property[render.Color]:
		return func() string { return colorText(h.Get()) }, true

	// The same vocabulary as plain values. A context may hold a
	// constant instead of a handle, and it would be a wart for a
	// string constant to render while an int constant is a load error.
	case string:
		return func() string { return h }, true
	case int:
		s := strconv.Itoa(h)
		return func() string { return s }, true
	case int64:
		s := strconv.FormatInt(h, 10)
		return func() string { return s }, true
	case float64:
		s := strconv.FormatFloat(h, 'f', -1, 64)
		return func() string { return s }, true
	case bool:
		s := strconv.FormatBool(h)
		return func() string { return s }, true
	case time.Duration:
		s := h.String()
		return func() string { return s }, true
	case render.Color:
		s := colorText(h)
		return func() string { return s }, true
	}
	return nil, false
}

// bindText turns content with {{.Path}} bindings and {{ns:Func …}}
// value-namespace calls into a computed string property. Pure-literal
// content returns (nil, nil). Resolution happens once at build time —
// handles, not values — so evaluation does no lookups; this is the
// "lvalue semantics" of the design.
//
// The computed reads every part's handle on each evaluation, so a
// change to any of them repaints exactly the components that display
// this text — including the handle a value provider built, whose own
// argument Gets are subscriptions for the same reason.
//
// A bound path need not be a string. textSource converts the scalar
// types listed in textBindableTypes, so <Text>count: {{.N}}</Text> over
// a *prop.Property[int] is ordinary markup; the conversion is a closure
// called from inside this computed, so it subscribes exactly as a
// string handle does.
//
// Content that contains a brace expression this package cannot resolve
// is a LOAD ERROR, not literal text; see scan.go for why.
func bindText(content string, ctx *Context) (*prop.Property[string], error) {
	segs, err := scanBindings(content)
	if err != nil {
		return nil, err
	}
	type part struct {
		lit string
		get func() string // nil for a literal segment
	}
	var parts []part
	dynamic := false
	for _, sg := range segs {
		switch sg.kind {
		case segLiteral:
			parts = append(parts, part{lit: sg.text})
		case segPath:
			dynamic = true
			v, err := resolve(ctx.Values, sg.text)
			if err != nil {
				return nil, err
			}
			get, ok := textSource(v)
			if !ok {
				return nil, fmt.Errorf("markup: {{.%s}} is %T; text renders %s — as a *prop.Property[T] handle or a plain value", sg.text, v, textBindableTypes)
			}
			parts = append(parts, part{get: get})
		case segCall:
			dynamic = true
			h, err := ctx.valueHandle(sg.call)
			if err != nil {
				return nil, err
			}
			parts = append(parts, part{get: h.Get})
		}
	}
	if !dynamic {
		return nil, nil
	}
	return prop.NewComputed(func() string {
		var sb strings.Builder
		for _, p := range parts {
			if p.get != nil {
				sb.WriteString(p.get())
			} else {
				sb.WriteString(p.lit)
			}
		}
		return sb.String()
	}), nil
}

// styleNamed resolves a style NAME against ctx.Styles and fails on one
// that was never registered.
//
// Every one of the six style lookups used to be a bare map index, so a
// missing name yielded the zero Style and the page loaded clean and
// painted unstyled. `Style="dmi"` for `dim` was not an error, it was
// plain text — and the symptom reads as somebody's deliberate choice
// rather than a typo. Two independent readers of this repo hit it on the
// same afternoon; one A/B'd it, loading a page with `Style="accentt"`
// successfully while a mistyped BINDING in the same file failed the load
// instantly.
//
// That inconsistency is the bug. The rule is that everything resolvable
// resolves at load — checkAttrs already rejects an unknown attribute
// NAME, so rejecting an unknown style VALUE is the same promise applied
// one level down.
//
// An EMPTY attribute stays valid and means the zero style. Absent and
// empty are "no style"; only a name that was typed and does not exist is
// an error.
func styleNamed(e Element, ctx *Context, attr, name string) (render.Style, error) {
	if name == "" {
		return render.Style{}, nil
	}
	st, ok := ctx.Styles[name]
	if !ok {
		return render.Style{}, fmt.Errorf("markup: <%s %s=%q>: no style named %q is registered", e.Name, attr, name, name)
	}
	return st, nil
}

// UnresolvedError is a binding path the context does not contain. It is
// typed, rather than the plain fmt.Errorf it used to be, so a caller can
// tell "this document names something that is not here" apart from every
// other load failure WITHOUT matching on message text.
//
// The caller that needs it is the control plane: an endpoint scoped to
// an island builds markup against a PRUNED value surface, and has to
// report a document that reached past the grant differently from a
// document with a typo. errors.As plus one map lookup answers that; the
// alternative it replaced was building the document a second time
// against the full surface to see whether it would have worked, which
// runs every load-time side effect in the document twice.
type UnresolvedError struct{ Path string }

func (e *UnresolvedError) Error() string {
	return fmt.Sprintf("markup: %q not found in context", e.Path)
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
			return nil, &UnresolvedError{Path: path}
		}
	}
	return cur, nil
}
