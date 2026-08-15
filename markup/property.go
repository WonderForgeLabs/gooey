package markup

import (
	"fmt"
	"image"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Markup-declared dependency properties.
//
// A control file may declare its own property surface:
//
//	<Gooey xmlns="wonderforge.io/gooey/2026"
//	       xmlns:x="wonderforge.io/gooey/x">
//	  <x:Property Name="Title"   Type="string" Required="true"/>
//	  <x:Property Name="Caption" Type="string" Default="no caption"/>
//	  <Border Title="{{.Title}}">…</Border>
//	</Gooey>
//
// Each declaration materializes the identical artifact code-behind wires
// today — a *prop.Property[T] node. There is ONE property system: these
// are ordinary dependency properties that happen to be registered from
// markup, which is what `DependencyProperty.Register` is on the code
// tier. XAML 2009 specified x:Property and WPF never shipped it; this
// is that feature, arriving in a framework whose property graph was
// built for it.
//
// At an instantiation site each declaration resolves one of three ways:
//
//   - attribute bound → the parent's existing handle passes through,
//     type-checked against the declared Type;
//   - attribute literal → coerced by Type and wrapped as a fresh source;
//   - attribute absent → a fresh per-instance source carrying the
//     declared Default (markup-defined, typed, bindable local state);
//     absent + Required is a load error.
//
// Declaring anything at all makes the control STRICT: an undeclared
// attribute at the instantiation site is a load error, because the
// declarations are now the control's public surface. A file with no
// declarations keeps today's pass-through behavior exactly.
//
// The type table is a plain type-switch (below), not reflection: `Type`
// selects a closure that knows its own T, and `any` is the escape hatch
// for app types the framework has never heard of.
//
// Bindability and literal-spellability are SEPARATE axes, and the table
// says so. Most rows have both: a color is `#08f` in an attribute and a
// *prop.Property[render.Color] through a binding. Three do not —
// `style`, `image` and `series` are BIND-ONLY, declarable and
// type-checked but with no attribute spelling, because their literal
// would have to be resolved against something the declaration parser
// cannot see (a style NAME against the page's Context.Styles, an image
// PATH against the page's fs.FS) or does not exist at all. A bind-only
// row is still a full row: an absent optional attribute materializes
// the type's zero handle, a bound one is type-checked, and a literal or
// a Default is a load error that says to bind instead.
//
// That distinction is the whole point of the three rows. Before them,
// every control taking something more interesting than a scalar had to
// declare Type="any", which accepts anything and checks nothing — so
// <Card Trend="{{.Title}}"/> loaded clean and failed one level down
// inside <Sparkline Values>, naming an element the page's author never
// wrote. `any` is now for genuine app types ([]*Feed, map[string]bool),
// not for every handle the table happened to lack.

// XNamespace is gooey's language-services namespace — the XAML `x:`
// analog. A document declaring xmlns:x="wonderforge.io/gooey/x" may use
// <x:Property> to declare dependency properties on its root.
const XNamespace = "wonderforge.io/gooey/x"

// Declaration is one <x:Property> — a dependency property registered
// from markup.
type Declaration struct {
	// Name is the attribute callers set at the instantiation site and
	// the path the control's own markup binds ({{.Title}}).
	Name string
	// Type is the declared type as spelled in markup.
	Type string
	// Default is the literal used when the attribute is absent. It is
	// coerced by Type at parse time, so a bad default fails the load of
	// the CONTROL, not of the page that instantiates it.
	Default string
	// Required makes an absent attribute a load error.
	Required bool

	kind propKind
}

// propKind is one row of the type table: everything the loader needs to
// know about a declared type without ever asking a value what it is.
type propKind struct {
	// source makes a fresh per-instance property from a literal ("" for
	// the type's zero value).
	source func(lit string) (any, error)
	// check reports whether a bound value is a handle of this type.
	check func(v any) bool
	// want names the handle type check wants, for the error message.
	want string
	// bindOnly marks a type with no attribute spelling: it crosses a
	// control boundary as a handle or not at all. source still makes the
	// zero-valued handle for an absent optional attribute — that is the
	// three-way rule, unchanged — but a literal at the instantiation
	// site and a Default on the declaration are both load errors.
	bindOnly bool
}

// kindOf builds a type-table row for T. T is a compile-time parameter,
// so the resulting closures do their work with a type assertion and a
// typed constructor — the same "no reflection" discipline boundProp
// uses for builtin attributes.
func kindOf[T any](parse func(string) (T, error)) propKind {
	var want *prop.Property[T]
	return propKind{
		source: func(lit string) (any, error) {
			var v T
			if lit != "" {
				p, err := parse(lit)
				if err != nil {
					return nil, err
				}
				v = p
			}
			return prop.NewSource(v), nil
		},
		check: func(v any) bool { _, ok := v.(*prop.Property[T]); return ok },
		want:  fmt.Sprintf("%T", want),
	}
}

// bindKindOf builds a BIND-ONLY row for T: a type markup can declare
// and type-check but cannot spell in an attribute.
//
// The parse closure is a backstop, not a path. Every caller checks
// bindOnly first and produces an error naming the element, so this one
// only fires if a future caller forgets — in which case it fails loudly
// rather than materializing a silent zero value, which is the failure
// mode this whole row type exists to remove.
func bindKindOf[T any](typeName string) propKind {
	k := kindOf(func(string) (T, error) {
		var zero T
		return zero, fmt.Errorf("%s has no literal syntax; bind a handle", typeName)
	})
	k.bindOnly = true
	return k
}

// propKinds is the whole type system of markup declarations, and adding
// a type is adding a row.
//
// It used to say "there is nowhere else to touch", which is true of the
// LOADER and false at the boundary. Two other tables are keyed by these
// same spellings and do not derive from this map: control.KindOf
// (control/value.go) mirrors the rows onto the wire, whose own doc calls
// the lockstep rule out; and declKind (catalog.go) maps a declared Type
// onto a catalog Kind for the component catalog. A new row is invisible
// to both until they gain a case — the failure is silent and cosmetic
// (a declared property described as the wrong kind), never a bad load,
// which is exactly why it is easy to miss.
var propKinds = map[string]propKind{
	"string":   kindOf(func(s string) (string, error) { return s, nil }),
	"int":      kindOf(strconv.Atoi),
	"bool":     kindOf(strconv.ParseBool),
	"float":    kindOf(func(s string) (float64, error) { return strconv.ParseFloat(s, 64) }),
	"duration": kindOf(time.ParseDuration),
	"color":    kindOf(parseHexColor),

	// A style crosses as the viewmodel's own *prop.Property[render.Style]
	// — the handle that makes a style REACTIVE. Bind-only because the
	// literal for a style is a NAME resolved against Context.Styles, and
	// the declaration parser has no context: Declarations() is a pure
	// function of bytes (it doubles as the wire schema), so a
	// Default="panel" could not be checked when the CONTROL loads, only
	// on whichever page happened to omit the attribute. Half a literal
	// is worse than none, and passing the handle loses nothing: today's
	// Type="any" + Tint="accent" already fails one level down, because
	// the inner <Text Style="{{.Tint}}"> wants the same handle.
	"style": bindKindOf[render.Style]("style"),

	// No way to write a picture inline, and a path literal would be
	// resolved against the instantiating page's fs.FS — the same
	// context the declaration parser does not have. <Image Src="logo.png">
	// keeps that spelling because it IS the page. control.KindImage has
	// said all of this since before there was a row for it.
	"image": bindKindOf[image.Image]("image"),

	// A series is what a Sparkline plots. Bind-only follows the element
	// vocabulary rather than inventing a spelling: <Sparkline Values> is
	// declared BindsBinding/GoType "[]float64" (elements.go), so a
	// comma-list here would let a declared property accept text the very
	// component it feeds refuses. A literal would buy the empty-state
	// default and the zero handle already is one — an absent optional
	// series is a nil slice, which plots as nothing.
	//
	// Spelled "series", not the MCP tool surface's "number[]": that
	// vocabulary is deliberately JSON's, this one is markup's.
	"series": bindKindOf[[]float64]("series"),

	// `any` is the escape hatch for app types the framework has never
	// heard of: a bound attribute passes through whatever handle the
	// parent holds, unchecked, exactly as an untyped Include attribute
	// does today. It is NOT bind-only — a literal still becomes a
	// *prop.Property[any] holding the string — and it is not the place
	// to put a type the table could name.
	"any": {
		source: func(lit string) (any, error) {
			if lit == "" {
				return prop.NewSource[any](nil), nil
			}
			return prop.NewSource[any](lit), nil
		},
		check: func(any) bool { return true },
		want:  "any value",
	},
}

func kindNames() []string {
	names := make([]string, 0, len(propKinds))
	for n := range propKinds {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// parseHexColor reads the one color literal markup has: #rgb or #rrggbb.
func parseHexColor(s string) (render.Color, error) {
	h := strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	if len(h) != 6 {
		return render.Color{}, fmt.Errorf("want #rgb or #rrggbb")
	}
	n, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return render.Color{}, fmt.Errorf("want #rgb or #rrggbb")
	}
	return render.RGB(uint8(n>>16), uint8(n>>8), uint8(n)), nil
}

// Declarations parses src and returns the <x:Property> declarations its
// root declares, in document order. Nothing is built and no context is
// touched: this is the read-only half of the declaration block's double
// life as a wire schema (the control plane's GetDeclaredSchema serves
// it). A document with no declarations returns an empty slice; a
// malformed document returns the same load error building it would.
func Declarations(src []byte) ([]Declaration, error) {
	doc, err := parseDocument(src)
	if err != nil {
		return nil, err
	}
	return doc.decls.list, nil
}

// DeclaredSurface is one control instance's markup-declared dependency
// properties, as resolved for that instance: the declarations that make
// up the control's public surface plus the live handles this instance
// carries for them. Entries land in Context.Declared as controls build.
type DeclaredSurface struct {
	// Control is the markup file the declarations came from, e.g.
	// "card.gooey" — the contract's identity.
	Control string
	Props   []DeclaredProp
}

// DeclaredProp pairs one <x:Property> declaration with the handle the
// instance resolved for it — the parent's bound handle, a fresh source
// wrapping a literal, or a fresh source carrying the default, per the
// three-way rule above.
type DeclaredProp struct {
	Declaration
	// Handle is the instance's value for the declaration: a
	// *prop.Property[T] for the table types, or a gooey.Action when a
	// Type="any" attribute was a handler expression.
	Handle any
}

// declarations is a control file's declared surface.
type declarations struct {
	list   []Declaration
	byName map[string]Declaration
	// present records that the file declared a surface at all, which is
	// what turns on strict attribute checking. It is distinct from
	// len(list) != 0 only in the degenerate empty-block case.
	present bool
}

func (ds declarations) names() []string {
	out := make([]string, 0, len(ds.list))
	for _, d := range ds.list {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}

// splitDeclarations separates <x:Property> declarations from the root's
// visual children. Declarations sit on the root because the root IS the
// control's type definition.
func splitDeclarations(root Element) (declarations, []Element, error) {
	ds := declarations{byName: map[string]Declaration{}}
	var kids []Element
	for _, c := range root.Children {
		switch {
		case c.Space == XNamespace:
			if c.Name != "Property" {
				return ds, nil, fmt.Errorf("markup: unknown language element <x:%s>; the %s namespace declares <x:Property> only", c.Name, XNamespace)
			}
			d, err := parseDeclaration(c)
			if err != nil {
				return ds, nil, err
			}
			if _, dup := ds.byName[d.Name]; dup {
				return ds, nil, fmt.Errorf("markup: dependency property %q declared twice", d.Name)
			}
			ds.list = append(ds.list, d)
			ds.byName[d.Name] = d
			ds.present = true
		case c.Name == "Property":
			// The likely typo: the element is right, the namespace is
			// missing, and without this it would be read as a component.
			return ds, nil, fmt.Errorf("markup: <Property> is a dependency property declaration; write it as <x:Property> and add xmlns:x=%q to the root element", XNamespace)
		default:
			kids = append(kids, c)
		}
	}
	return ds, kids, nil
}

var declAttrs = map[string]bool{"Name": true, "Type": true, "Default": true, "Required": true}

func parseDeclaration(e Element) (Declaration, error) {
	var d Declaration
	names := make([]string, 0, len(e.Attrs))
	for k := range e.Attrs {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if !declAttrs[k] {
			return d, fmt.Errorf("markup: <x:Property> has no attribute %q; it takes Name, Type, Default, Required", k)
		}
	}
	d.Name = strings.TrimSpace(e.Attrs["Name"])
	if d.Name == "" {
		return d, fmt.Errorf("markup: <x:Property> needs a Name")
	}
	if d.Name == "Name" || d.Name == "Tooltip" || layoutAttr(d.Name) {
		return d, fmt.Errorf("markup: dependency property %q — the name is reserved: instance attributes named Name and Tooltip and the layout attributes belong to the element, not to the control", d.Name)
	}
	d.Type = strings.TrimSpace(e.Attrs["Type"])
	if d.Type == "" {
		return d, fmt.Errorf("markup: dependency property %q — needs a Type (one of %s)", d.Name, strings.Join(kindNames(), ", "))
	}
	k, ok := propKinds[d.Type]
	if !ok {
		return d, fmt.Errorf("markup: dependency property %q — unknown Type %q (want one of %s)", d.Name, d.Type, strings.Join(kindNames(), ", "))
	}
	d.kind = k
	if raw, ok := e.Attrs["Required"]; ok {
		b, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return d, fmt.Errorf("markup: dependency property %q — Required=%q is not a bool", d.Name, raw)
		}
		d.Required = b
	}
	if def, ok := e.Attrs["Default"]; ok {
		d.Default = def
		if d.Required {
			return d, fmt.Errorf("markup: dependency property %q — Required and Default are exclusive: a default is what makes an attribute optional", d.Name)
		}
		if d.Type == "any" {
			return d, fmt.Errorf("markup: dependency property %q — Type=\"any\" has no literal syntax, so it takes no Default; bind it or mark it Required", d.Name)
		}
		if d.kind.bindOnly {
			// The point of refusing is WHERE it would have failed. A
			// Default for a bind-only type would have to resolve against
			// the instantiating page — its styles, its file system — so
			// a typo in the CONTROL would surface on whichever page
			// omitted the attribute, and never on one that always sets
			// it. Absent still means the zero handle; only a written
			// Default is an error.
			return d, fmt.Errorf("markup: dependency property %q — Type=%q has no literal syntax, so it takes no Default: it crosses a control boundary as a handle, never as text; bind it or mark it Required (absent means the zero %s)", d.Name, d.Type, d.Type)
		}
		// Coerce now: a bad default is a defect in the CONTROL, and it
		// should fail when the control loads rather than at whichever
		// instantiation site happens to omit the attribute.
		if _, err := d.kind.source(def); err != nil {
			return d, fmt.Errorf("markup: dependency property %q — Default=%q is not a %s: %w", d.Name, def, d.Type, err)
		}
	}
	return d, nil
}

// instantiate resolves an instance's attributes into the declared
// dependency properties, per the three-way rule. file names the control
// so an error points at the contract that was broken, not at the page.
func (ds declarations) instantiate(file string, e Element, parent *Context) (map[string]any, error) {
	if len(ds.list) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(ds.list))
	for _, d := range ds.list {
		v, err := d.resolve(file, e, parent)
		if err != nil {
			return nil, err
		}
		out[d.Name] = v
	}
	return out, nil
}

func (d Declaration) resolve(file string, e Element, parent *Context) (any, error) {
	raw, present := e.Attrs[d.Name]
	if !present {
		if d.Required {
			return nil, fmt.Errorf("markup: %s: dependency property %q — required attribute missing on <%s>", file, d.Name, e.Name)
		}
		// A fresh per-instance source: markup-defined, typed, bindable
		// local state. Two instances of the control do NOT share it.
		return d.kind.source(d.Default)
	}
	switch {
	case isHandlerExpr(raw):
		// Behavior declared in markup crosses the boundary as a Command,
		// which has no declared type of its own — so it needs the escape
		// hatch, and saying so beats a confusing type error later.
		if d.Type != "any" {
			return nil, fmt.Errorf("markup: %s: dependency property %q — %s=%q is a handler expression, which needs Type=\"any\", not %q", file, d.Name, d.Name, raw, d.Type)
		}
		cmd, err := parent.Command(raw)
		if err != nil {
			return nil, fmt.Errorf("markup: %s: dependency property %q — %w", file, d.Name, err)
		}
		return cmd, nil
	// A conditional resolves through the same call and is then checked
	// against the declared Type like any other handle — so Type="bool"
	// accepts {{and .A .B}}, and Type="string" rejects it by naming both
	// types rather than by failing to parse the expression as a literal.
	case bindRe.MatchString(raw) || isCondExpr(raw):
		v, err := parent.BindingValue(raw)
		if err != nil {
			return nil, fmt.Errorf("markup: %s: dependency property %q — %w", file, d.Name, err)
		}
		if !d.kind.check(v) {
			// Name the element the page's author actually wrote. The
			// contract belongs to `file`, but the mistake is on <Card>,
			// and an error that names only the control sends a reader
			// into a file they did not edit.
			return nil, fmt.Errorf("markup: %s: dependency property %q — <%s %s=%q> is %T; Type=%q needs %s", file, d.Name, e.Name, d.Name, raw, v, d.Type, d.kind.want)
		}
		return v, nil
	default:
		if d.kind.bindOnly {
			return nil, fmt.Errorf("markup: %s: dependency property %q — <%s %s=%q>: Type=%q has no literal syntax; pass a handle, as %s=\"{{.Something}}\"", file, d.Name, e.Name, d.Name, raw, d.Type, d.Name)
		}
		v, err := d.kind.source(raw)
		if err != nil {
			return nil, fmt.Errorf("markup: %s: dependency property %q — <%s %s=%q> is not a %s: %w", file, d.Name, e.Name, d.Name, raw, d.Type, err)
		}
		return v, nil
	}
}

// checkAttrs is strict mode: once a control declares a surface, an
// attribute it did not declare is a typo, and a typo that silently does
// nothing is the failure mode markup-only controls had until now.
// Element-level attributes (Name, layout, Grid.*) are the element's, not
// the control's, so they are never checked here.
func (ds declarations) checkAttrs(file string, e Element) error {
	var unknown []string
	for k := range e.Attrs {
		if k == "Name" || k == "Tooltip" || layoutAttr(k) {
			continue
		}
		if _, ok := ds.byName[k]; ok {
			continue
		}
		unknown = append(unknown, k)
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	k := unknown[0]
	return fmt.Errorf("markup: <%s %s=%q>: %s declares no dependency property %q (declared: %s)",
		e.Name, k, e.Attrs[k], file, k, strings.Join(ds.names(), ", "))
}
