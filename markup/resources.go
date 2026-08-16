package markup

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Scoped resources and markup-declared styles.
//
// A palette is the one thing a designer edits and the one thing markup
// could not express: every demo in this repo carried its colors as a Go
// map[string]render.Style, so changing a shade meant a rebuild, and
// cmd/colors carried "#12121e" in two languages with nothing checking
// they agreed. Resources close that gap:
//
//	<Gooey xmlns="wonderforge.io/gooey/2026">
//	  <Gooey.Resources>
//	    <Resource Key="ground" Type="color" Value="#12121e"/>
//	    <Style Key="accent" Fg="#ffaa3c" Bold="true"/>
//	    <Style Key="panel">
//	      <Setter Property="Fg" Resource="ground"/>
//	    </Style>
//	  </Gooey.Resources>
//	  <Border Style="panel" Background="{{.Ground}}">…</Border>
//	</Gooey>
//
// Three commitments, all inherited rather than invented (see
// docs/specs/2026-08-10-styles-and-resources.md):
//
//   - A resource reference is an LVALUE, like every other binding here.
//     The key resolves ONCE, at build, to a *prop.Property[T]; the read
//     of that handle happens inside the style computed, which is itself
//     read inside a paint node. So Set on a resource repaints exactly
//     the components whose appearance depends on it — no dictionary walk
//     at paint time, no invalidation pass, no styling machinery left
//     running after build.
//
//   - A <Style> is a reactive render.Style RECIPE, not a property bag.
//     It materializes as one prop.NewComputed[render.Style] per instance
//     fed into the Style slot every component already has. Zero component
//     changes: components keep reading *prop.Property[render.Style]
//     exactly as they do today.
//
//   - Scoping is LEXICAL and resolved at build. Entering an element with
//     a <X.Resources> slot pushes a scope, leaving pops it, so siblings
//     can never see a scope they are not inside, and an inner definition
//     SHADOWS an outer one by producing a different handle for whoever
//     referenced it there. There are no priority numbers.
//
// # What wins, and why
//
// Context.Styles — the host's Go map — is the OUTERMOST scope, below
// every markup-declared one. A page-declared style therefore beats a
// host-granted style of the same name.
//
// That is not a special case, it is the same rule as everywhere else in
// the chain: the nearest declaration wins, and ctx.Styles is simply the
// furthest. Two consequences make it the right way round. Migration
// works one key at a time — a page can move "accent" out of Go and into
// its own <Gooey.Resources> and see it take effect without first
// deleting the Go entry, which is what makes the demo migration a
// sequence of small safe steps rather than one flip. And the surprising
// direction is the other one: a host grant silently overriding a style
// declared three lines above the element that uses it would make the
// visible declaration the lie.
//
// The tightening from the same rule: an unknown style key is a LOAD
// error (styleNamed), and it stays one — the chain is consulted first,
// ctx.Styles second, and a name found in neither fails the load.
//
// # Control boundaries
//
// Values isolate; resources are AMBIENT. A control's markup binds only
// what crossed its declared surface, but it inherits the theme of the
// site that instantiated it (control(), usercontrol.go). A control file
// may declare its own <Gooey.Resources>, which shadows that ambient
// chain for its subtree — with fresh handles per instantiation, so two
// instances of a control do not share resource state, exactly as two
// instances do not share declared-property defaults.

// resourceEnv is a Context's resource environment. Two scopes, because
// they answer different questions: cur is where a lookup starts while
// building, and moves constantly; root is the document scope, kept so
// Context.Resource can serve a handle to Go code AFTER the build has
// finished and cur has been popped back to nothing.
type resourceEnv struct {
	cur  *resourceScope
	root *resourceScope
}

// resourceScope is one live dictionary in the lexical chain. Entries
// hold `any` deliberately: a scope is where <ControlTemplate> and
// localized string tables will land, and each consumer type-checks its
// own lookups the way boundProp does.
type resourceScope struct {
	parent  *resourceScope
	entries map[string]any // *resourceDef or *styleDef
}

func (s *resourceScope) lookup(key string) any {
	for ; s != nil; s = s.parent {
		if v, ok := s.entries[key]; ok {
			return v
		}
	}
	return nil
}

// resourceDef is one live <Resource>: the declared type and the source
// property this instantiation materialized for it. The handle is a
// source rather than a constant because that is what makes a theme
// swappable at runtime — Context.Resource hands it to Go code, one Set
// repaints its readers.
type resourceDef struct {
	key    string
	typ    string
	handle any // *prop.Property[T] for the propKinds row named by typ
}

// styleDef is one live <Style>: setters already resolved against the
// scope it was declared in. Applying it to an instance is materialize(),
// which builds the per-instance computed.
type styleDef struct {
	key     string
	target  string
	setters []styleSetter
}

// styleSetter mutates the accumulating style. A Value= setter closes
// over a literal parsed at LOAD; a Resource= setter closes over a
// handle, and its Get runs inside the style computed — which is what
// makes the resource a dependency of exactly the components that read
// the style.
type styleSetter func(*render.Style)

// resourceBlock is a PARSED <X.Resources> block — declarations only, no
// state. It is parsed once per document and instantiated once per
// push, so a control that appears twice on a page gets two independent
// sets of handles.
type resourceBlock struct {
	order  []string // declaration order, for deterministic instantiation
	scalar map[string]*resourceDecl
	styles map[string]*styleDecl
}

// resourceDecl is one parsed <Resource>. The literal is kept rather than
// the value because each instantiation coerces it afresh into its own
// source; the coercion is nevertheless CHECKED at parse, so a bad
// literal fails the file that declares it.
type resourceDecl struct {
	key string
	typ string
	lit string
}

type styleDecl struct {
	key     string
	target  string
	setters []setterDecl
}

// setterDecl is one parsed <Setter> (or one attribute of the shorthand
// form). Exactly one of lit and res is meaningful: a literal is baked
// into fn at parse, a resource key is resolved when the block is
// instantiated and the scope exists.
type setterDecl struct {
	field string
	res   string
	fn    styleSetter
}

// styleField is one row of the style type system: what a setter may
// address and how its value is obtained. Six rows, the fields of
// render.Style, each closing over a concrete type — the propKinds
// discipline at field granularity, and the reason there is no
// reflection here.
type styleField struct {
	// literal parses a Value= into a mutator at LOAD time.
	literal func(lit string) (styleSetter, error)
	// bind wraps a resource into a mutator, type-checking the handle it
	// carries. The type assertion is the whole check: a declared type
	// that cannot drive this field is a load error naming both.
	bind func(d *resourceDef) (styleSetter, error)
}

func colorField(assign func(*render.Style, render.Color)) styleField {
	return styleField{
		literal: func(lit string) (styleSetter, error) {
			c, err := parseHexColor(lit)
			if err != nil {
				return nil, err
			}
			return func(s *render.Style) { assign(s, c) }, nil
		},
		bind: func(d *resourceDef) (styleSetter, error) {
			h, ok := d.handle.(*prop.Property[render.Color])
			if !ok {
				return nil, fmt.Errorf("wants a color resource, and %q is declared Type=%q", d.key, d.typ)
			}
			return func(s *render.Style) { assign(s, h.Get()) }, nil
		},
	}
}

func boolField(assign func(*render.Style, bool)) styleField {
	return styleField{
		literal: func(lit string) (styleSetter, error) {
			b, err := strconv.ParseBool(lit)
			if err != nil {
				return nil, fmt.Errorf("want true or false")
			}
			return func(s *render.Style) { assign(s, b) }, nil
		},
		bind: func(d *resourceDef) (styleSetter, error) {
			h, ok := d.handle.(*prop.Property[bool])
			if !ok {
				return nil, fmt.Errorf("wants a bool resource, and %q is declared Type=%q", d.key, d.typ)
			}
			return func(s *render.Style) { assign(s, h.Get()) }, nil
		},
	}
}

// styleFields is the whole vocabulary a <Setter> may address. Adding a
// field is adding a row; there is nowhere else to touch, and nothing
// asks a value what type it is.
var styleFields = map[string]styleField{
	"Fg":        colorField(func(s *render.Style, c render.Color) { s.Fg = c }),
	"Bg":        colorField(func(s *render.Style, c render.Color) { s.Bg = c }),
	"Bold":      boolField(func(s *render.Style, b bool) { s.Bold = b }),
	"Dim":       boolField(func(s *render.Style, b bool) { s.Dim = b }),
	"Underline": boolField(func(s *render.Style, b bool) { s.Underline = b }),
	"Reverse":   boolField(func(s *render.Style, b bool) { s.Reverse = b }),
}

func styleFieldNames() []string {
	out := make([]string, 0, len(styleFields))
	for n := range styleFields {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// styleStates are the sections the grammar reserves for state overlays.
// They parse to a LOAD ERROR rather than being ignored: markup this
// package accepts and does not honour is the failure mode the whole
// strict-attribute discipline exists to remove, and a pane that simply
// never highlights on focus is indistinguishable from a design choice.
var styleStates = map[string]bool{"Focus": true, "Hover": true, "Disabled": true}

// rootResources parses the document scope — the <Gooey.Resources> block
// on the root element — and rejects every other property element there.
//
// The rejection is the second half of the feature. <Gooey.Anything> was
// silently discarded before this existed: attachProp files it under the
// root's Props and nothing ever read them, so a mistyped document scope
// would have loaded clean and themed nothing.
func rootResources(root Element) (*resourceBlock, error) {
	names := make([]string, 0, len(root.Props))
	for name := range root.Props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name != "Resources" {
			return nil, fmt.Errorf("markup: <Gooey> does not accept the property element <Gooey.%s>; the root's only slot is <Gooey.Resources>", name)
		}
	}
	return parseResourceBlock(root.Props["Resources"])
}

// parseResourceBlock parses a <X.Resources> slot. The zero Element — a
// missing slot — is not an error and yields no block, which is the
// common case and costs nothing.
func parseResourceBlock(slot Element) (*resourceBlock, error) {
	if slot.Name == "" {
		return nil, nil
	}
	b := &resourceBlock{scalar: map[string]*resourceDecl{}, styles: map[string]*styleDecl{}}
	for _, c := range slot.Children {
		switch c.Name {
		case "Resource":
			d, err := parseResourceDecl(c)
			if err != nil {
				return nil, err
			}
			if err := b.claim(d.key, "Resource"); err != nil {
				return nil, err
			}
			b.scalar[d.key] = d
		case "Style":
			d, err := parseStyleDecl(c)
			if err != nil {
				return nil, err
			}
			if err := b.claim(d.key, "Style"); err != nil {
				return nil, err
			}
			b.styles[d.key] = d
		default:
			return nil, fmt.Errorf("markup: <%s> holds <Resource> and <Style> elements only, got <%s>", slot.Name, c.Name)
		}
	}
	return b, nil
}

// claim records a key and rejects a duplicate. Two definitions of one
// key in one scope means one of them is unreachable, and which one wins
// would depend on document order — a coin flip dressed as a rule. An
// inner SCOPE redefining an outer key is the supported spelling for
// that, and it says where the override applies.
func (b *resourceBlock) claim(key, what string) error {
	if _, dup := b.scalar[key]; dup {
		return fmt.Errorf("markup: <%s Key=%q>: %q is already defined in this <Resources> block; shadow it in a nested <X.Resources> instead", what, key, key)
	}
	if _, dup := b.styles[key]; dup {
		return fmt.Errorf("markup: <%s Key=%q>: %q is already defined in this <Resources> block; shadow it in a nested <X.Resources> instead", what, key, key)
	}
	b.order = append(b.order, key)
	return nil
}

var resourceAttrs = map[string]bool{"Key": true, "Type": true, "Value": true}

func parseResourceDecl(e Element) (*resourceDecl, error) {
	if len(e.Children) > 0 || strings.TrimSpace(e.Text) != "" {
		return nil, fmt.Errorf("markup: <Resource> takes no content; it is Key, Type and Value")
	}
	names := make([]string, 0, len(e.Attrs))
	for k := range e.Attrs {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if !resourceAttrs[k] {
			return nil, fmt.Errorf("markup: <Resource> has no attribute %q; it takes Key, Type, Value", k)
		}
	}
	d := &resourceDecl{
		key: strings.TrimSpace(e.Attrs["Key"]),
		typ: strings.TrimSpace(e.Attrs["Type"]),
		lit: e.Attrs["Value"],
	}
	if d.key == "" {
		return nil, fmt.Errorf("markup: <Resource> needs a Key")
	}
	if d.typ == "" {
		return nil, fmt.Errorf("markup: <Resource Key=%q> needs a Type (one of %s)", d.key, strings.Join(resourceKindNames(), ", "))
	}
	// `any` is excluded for the reason it takes no Default on
	// <x:Property>: a resource IS a literal, and `any` has no literal
	// syntax to be one.
	if d.typ == "any" {
		return nil, fmt.Errorf("markup: <Resource Key=%q>: Type=\"any\" has no literal syntax, so it cannot be a resource; a resource is defined by its Value", d.key)
	}
	k, ok := propKinds[d.typ]
	if !ok {
		return nil, fmt.Errorf("markup: <Resource Key=%q>: unknown Type %q (want one of %s)", d.key, d.typ, strings.Join(resourceKindNames(), ", "))
	}
	// A bind-only row has no literal syntax either, and the check has to be
	// explicit rather than left to the coercion below: kindOf's source
	// SHORT-CIRCUITS on an empty literal and hands back the zero-valued
	// handle without consulting the parse closure at all. So
	// <Resource Key="k" Type="style" Value=""/> would have loaded clean and
	// produced a live *prop.Property[render.Style] resource nobody declared
	// the value of — the accepted-but-meaningless markup this package
	// refuses everywhere else.
	if k.bindOnly {
		return nil, fmt.Errorf("markup: <Resource Key=%q>: Type=%q has no literal syntax, so it cannot be a resource; a resource is defined by its Value (want one of %s)", d.key, d.typ, strings.Join(resourceKindNames(), ", "))
	}
	if _, ok := e.Attrs["Value"]; !ok {
		return nil, fmt.Errorf("markup: <Resource Key=%q> needs a Value", d.key)
	}
	// Coerce now: a bad literal is a defect in the file that declares
	// it, and it should fail there rather than at whichever element
	// happens to reference the key.
	if _, err := k.source(d.lit); err != nil {
		return nil, fmt.Errorf("markup: <Resource Key=%q Value=%q>: not a %s: %w", d.key, d.lit, d.typ, err)
	}
	return d, nil
}

// resourceKindNames is kindNames minus the rows a resource cannot use:
// the bind-only ones and `any`, which are exactly the types with no
// literal spelling. Naming them in the "want one of" list would advertise
// a Type the very next line rejects.
func resourceKindNames() []string {
	out := make([]string, 0, len(propKinds))
	for n, k := range propKinds {
		if n == "any" || k.bindOnly {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// parseStyleDecl reads a <Style>, in either spelling:
//
//	<Style Key="accent" Fg="#ffaa3c" Bold="true"/>
//
//	<Style Key="panel">
//	  <Setter Property="Fg" Resource="ground"/>
//	</Style>
//
// The attribute form is sugar over the same styleFields table, and it
// exists because the migration this feature is for is a palette of flat
// entries: four <Style> lines beat twenty <Setter> lines, and the whole
// point is that a designer edits this file. The <Setter> form is the one
// that grows — it is what carries Resource= references, and it is the
// spelling the state sections will extend.
func parseStyleDecl(e Element) (*styleDecl, error) {
	d := &styleDecl{
		key:    strings.TrimSpace(e.Attrs["Key"]),
		target: strings.TrimSpace(e.Attrs["TargetType"]),
	}
	if d.key == "" {
		// TargetType alone selects by element type, which is the
		// implicit-matching half of the design and is not built yet.
		// Accepting it would produce a style that matches nothing.
		if d.target != "" {
			return nil, fmt.Errorf("markup: <Style TargetType=%q> needs a Key: implicit type matching is not implemented yet, so a style with no Key would match nothing", d.target)
		}
		return nil, fmt.Errorf("markup: <Style> needs a Key")
	}
	seen := map[string]bool{}
	for _, k := range sortedAttrNames(e) {
		if k == "Key" || k == "TargetType" {
			continue
		}
		f, ok := styleFields[k]
		if !ok {
			return nil, fmt.Errorf("markup: <Style Key=%q> has no attribute %q; it takes Key, TargetType and the style fields %s (or <Setter> children)", d.key, k, strings.Join(styleFieldNames(), ", "))
		}
		fn, err := f.literal(e.Attrs[k])
		if err != nil {
			return nil, fmt.Errorf("markup: <Style Key=%q %s=%q>: %w", d.key, k, e.Attrs[k], err)
		}
		seen[k] = true
		d.setters = append(d.setters, setterDecl{field: k, fn: fn})
	}
	for name := range e.Props {
		if styleStates[name] {
			return nil, fmt.Errorf("markup: <Style Key=%q>: the <Style.%s> state section is not implemented yet; declare a second style and switch between them with a bound Style=\"{{.Handle}}\" until it is", d.key, name)
		}
		return nil, fmt.Errorf("markup: <Style Key=%q> does not accept the property element <Style.%s>", d.key, name)
	}
	for _, c := range e.Children {
		if c.Name != "Setter" {
			return nil, fmt.Errorf("markup: <Style Key=%q> holds <Setter> elements only, got <%s>", d.key, c.Name)
		}
		s, err := parseSetter(d.key, c)
		if err != nil {
			return nil, err
		}
		if seen[s.field] {
			return nil, fmt.Errorf("markup: <Style Key=%q>: %s is set twice, once as an attribute and once as a <Setter>", d.key, s.field)
		}
		seen[s.field] = true
		d.setters = append(d.setters, s)
	}
	if len(d.setters) == 0 {
		return nil, fmt.Errorf("markup: <Style Key=%q> sets nothing; give it style attributes or <Setter> children", d.key)
	}
	return d, nil
}

var setterAttrs = map[string]bool{"Property": true, "Value": true, "Resource": true}

func parseSetter(key string, e Element) (setterDecl, error) {
	var s setterDecl
	for _, k := range sortedAttrNames(e) {
		if !setterAttrs[k] {
			return s, fmt.Errorf("markup: <Style Key=%q>: <Setter> has no attribute %q; it takes Property and exactly one of Value or Resource", key, k)
		}
	}
	s.field = strings.TrimSpace(e.Attrs["Property"])
	if s.field == "" {
		return s, fmt.Errorf("markup: <Style Key=%q>: <Setter> needs a Property (one of %s)", key, strings.Join(styleFieldNames(), ", "))
	}
	f, ok := styleFields[s.field]
	if !ok {
		return s, fmt.Errorf("markup: <Style Key=%q>: <Setter Property=%q>: no style field %q; want one of %s", key, s.field, s.field, strings.Join(styleFieldNames(), ", "))
	}
	lit, hasLit := e.Attrs["Value"]
	res, hasRes := e.Attrs["Resource"]
	switch {
	case hasLit && hasRes:
		return s, fmt.Errorf("markup: <Style Key=%q>: <Setter Property=%q> takes Value or Resource, not both", key, s.field)
	case !hasLit && !hasRes:
		return s, fmt.Errorf("markup: <Style Key=%q>: <Setter Property=%q> needs a Value or a Resource", key, s.field)
	case hasRes:
		s.res = strings.TrimSpace(res)
		if s.res == "" {
			return s, fmt.Errorf("markup: <Style Key=%q>: <Setter Property=%q> has an empty Resource", key, s.field)
		}
	default:
		fn, err := f.literal(lit)
		if err != nil {
			return s, fmt.Errorf("markup: <Style Key=%q>: <Setter Property=%q Value=%q>: %w", key, s.field, lit, err)
		}
		s.fn = fn
	}
	return s, nil
}

func sortedAttrNames(e Element) []string {
	out := make([]string, 0, len(e.Attrs))
	for k := range e.Attrs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// instantiate turns a parsed block into a live scope under parent.
//
// Two passes, and the order is the contract: every <Resource> gets its
// handle first, and only then are the <Style> setters resolved — so a
// style may reference a resource declared BELOW it in the same block,
// and a file does not have to be read bottom-up to be written.
//
// Handles are fresh on every call. That is what keeps two instances of
// one control from sharing resource state, and it is why the block
// stores literals rather than values.
func (b *resourceBlock) instantiate(parent *resourceScope) (*resourceScope, error) {
	if b == nil {
		return parent, nil
	}
	s := &resourceScope{parent: parent, entries: make(map[string]any, len(b.order))}
	for _, key := range b.order {
		d, ok := b.scalar[key]
		if !ok {
			continue
		}
		h, err := propKinds[d.typ].source(d.lit)
		if err != nil {
			// Unreachable: parseResourceDecl coerced the same literal.
			return nil, fmt.Errorf("markup: <Resource Key=%q Value=%q>: not a %s: %w", d.key, d.lit, d.typ, err)
		}
		s.entries[key] = &resourceDef{key: d.key, typ: d.typ, handle: h}
	}
	for _, key := range b.order {
		d, ok := b.styles[key]
		if !ok {
			continue
		}
		live := &styleDef{key: d.key, target: d.target}
		for _, sd := range d.setters {
			if sd.res == "" {
				live.setters = append(live.setters, sd.fn)
				continue
			}
			fn, err := resolveSetterResource(d.key, sd, s)
			if err != nil {
				return nil, err
			}
			live.setters = append(live.setters, fn)
		}
		s.entries[key] = live
	}
	return s, nil
}

// resolveSetterResource binds a Resource= setter to a handle from the
// scope the STYLE was declared in — lexical capture, like a closure. A
// style carried into a subtree keeps reading the resources it was
// written against, which is what makes shadowing an override of the
// subtree rather than a rewrite of the style.
func resolveSetterResource(key string, sd setterDecl, s *resourceScope) (styleSetter, error) {
	v := s.lookup(sd.res)
	if v == nil {
		return nil, fmt.Errorf("markup: <Style Key=%q>: <Setter Property=%q Resource=%q>: no resource named %q is in scope", key, sd.field, sd.res, sd.res)
	}
	d, ok := v.(*resourceDef)
	if !ok {
		return nil, fmt.Errorf("markup: <Style Key=%q>: <Setter Property=%q Resource=%q>: %q is a <Style>, not a <Resource>; a setter takes a value, not an appearance", key, sd.field, sd.res, sd.res)
	}
	fn, err := styleFields[sd.field].bind(d)
	if err != nil {
		return nil, fmt.Errorf("markup: <Style Key=%q>: <Setter Property=%q Resource=%q>: %s %w", key, sd.field, sd.res, sd.field, err)
	}
	return fn, nil
}

// materialize builds the per-instance style computed. This is the whole
// runtime of the styling system: the setters run, the Resource= ones
// Get their handles — inside an evaluation, so those Gets SUBSCRIBE —
// and the result feeds the Style slot the component already reads in its
// own paint node. A Set on a resource dirties this computed, which
// dirties exactly the paint nodes that read it.
func (d *styleDef) materialize() *prop.Property[render.Style] {
	setters := d.setters
	return prop.NewComputed(func() render.Style {
		var s render.Style
		for _, set := range setters {
			set(&s)
		}
		return s
	})
}

// checkTarget enforces the promise a TargetType makes when a Key is also
// present: the style is explicit, and using it on another element type
// is a mistake worth hearing about at load.
func (d *styleDef) checkTarget(e Element, attr string) error {
	if d.target == "" || d.target == e.Name {
		return nil
	}
	return fmt.Errorf("markup: <%s %s=%q>: that style declares TargetType=%q and <%s> is not one", e.Name, attr, d.key, d.target, e.Name)
}

// pushDocumentResources installs a document's <Gooey.Resources> block.
// It is the same push as an element's, plus one thing: when this context
// has no scope yet — a PAGE, as opposed to a control instantiated inside
// one — the resulting scope is remembered as the document scope, which
// is what Context.Resource serves after the build has finished.
//
// Recording unconditionally (rather than only once) is deliberate: Watch
// rebuilds the page against the same Context, and the handles from the
// previous build are gone with the tree that read them.
func (ctx *Context) pushDocumentResources(b *resourceBlock) (func(), error) {
	root := ctx.res.cur == nil
	pop, err := ctx.push(b)
	if err != nil {
		return nil, err
	}
	if root {
		ctx.res.root = ctx.res.cur
	}
	return pop, nil
}

// pushResources installs an element's <X.Resources> slot for the
// duration of that element's build — including its children, which is
// what makes shadowing lexical. A missing slot pushes nothing and costs
// one comparison.
func (ctx *Context) pushResources(slot Element) (func(), error) {
	b, err := parseResourceBlock(slot)
	if err != nil {
		return nil, err
	}
	return ctx.push(b)
}

func (ctx *Context) push(b *resourceBlock) (func(), error) {
	if b == nil {
		return func() {}, nil
	}
	prev := ctx.res.cur
	s, err := b.instantiate(prev)
	if err != nil {
		return nil, err
	}
	ctx.res.cur = s
	return func() { ctx.res.cur = prev }, nil
}

// Resource returns a resource handle declared by the page's own markup —
// the dark-mode toggle, and the only runtime half of this feature:
//
//	if h, ok := ctx.Resource("ground").(*prop.Property[render.Color]); ok {
//	    h.Set(render.RGB(0xff, 0xff, 0xff))
//	}
//
// One Set, and exactly the components whose resolved style read that
// handle repaint. The type assertion at the call site is the type check;
// there is no reflection and no dictionary.
//
// It serves the DOCUMENT scope of the last page built against this
// context. A subtree scope is reachable only from inside its own
// subtree, by construction, and a <Style> has no single handle to hand
// out — it materializes per instance — so a style key returns nil.
// Anything else unknown returns nil too.
func (ctx *Context) Resource(key string) any {
	if ctx.res.root == nil {
		return nil
	}
	d, ok := ctx.res.root.entries[key].(*resourceDef)
	if !ok {
		return nil
	}
	return d.handle
}

// styleHandle resolves a Style attribute NAME to the handle a component
// reads. It is the single seam the whole styling system hangs from: a
// markup-declared style materializes its computed here, and everything
// else falls through to the host's Go map exactly as before.
//
// The cascade, in order: nearest markup scope, then Context.Styles, then
// a load error. A bound Style="{{.Handle}}" never reaches this — it is
// resolved one level up, in bindStyle, and still bypasses the system
// entirely.
func styleHandle(e Element, ctx *Context, attr, name string) (*prop.Property[render.Style], error) {
	def, err := lookupStyle(e, ctx, attr, name)
	if err != nil {
		return nil, err
	}
	if def != nil {
		return def.materialize(), nil
	}
	st, err := styleNamed(e, ctx, attr, name)
	if err != nil {
		return nil, err
	}
	return components.Sty(st), nil
}

// styleValue is styleHandle for the components that take a style as a
// VALUE rather than a handle — ToastHost, Tooltip, TextBox's accent and
// invalid styles. They were non-reactive before markup styles existed
// and they stay so; what changes is only that they can now name a style
// the page declared. The snapshot Get runs outside any evaluation, so it
// records no dependency — the Get call site is what decides that.
func styleValue(e Element, ctx *Context, attr, name string) (render.Style, error) {
	def, err := lookupStyle(e, ctx, attr, name)
	if err != nil {
		return render.Style{}, err
	}
	if def != nil {
		return def.materialize().Get(), nil
	}
	return styleNamed(e, ctx, attr, name)
}

// lookupStyle walks the scope chain. A miss returns (nil, nil) — the
// caller falls through to Context.Styles, whose own miss is the load
// error. A HIT on a key that turns out to be a <Resource> is an error
// here rather than a fall-through, because falling through would report
// "no style named X" about a name the document plainly defines.
func lookupStyle(e Element, ctx *Context, attr, name string) (*styleDef, error) {
	if name == "" {
		return nil, nil
	}
	v := ctx.res.cur.lookup(name)
	if v == nil {
		return nil, nil
	}
	d, ok := v.(*styleDef)
	if !ok {
		r := v.(*resourceDef)
		return nil, fmt.Errorf("markup: <%s %s=%q>: %q is a <Resource Type=%q>, not a <Style>; a resource holds a value, a style holds an appearance", e.Name, attr, name, name, r.typ)
	}
	if err := d.checkTarget(e, attr); err != nil {
		return nil, err
	}
	return d, nil
}
