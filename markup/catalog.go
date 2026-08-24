package markup

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// goTypeOf renders a component's concrete type for the catalog's Go
// field. %T is the package's established spelling for this — Bound's
// type errors and the tree snapshot both use it.
func goTypeOf(c any) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%T", c)
}

// The component catalog answers a question nothing in gooey answered
// before: WHAT ELEMENT TYPES EXIST, AND WHAT CAN I SET ON THEM.
//
// Three introspection questions were already in the system and are
// documented in docs/specs/2026-08-11-plugins-as-standalone-activities.md:
// what is this binary linked with (link time), what is bindable right now
// (the Context), and what is rendering right now (the tree). None of them
// answers this one. The tree can only describe elements somebody already
// wrote; the Context describes values, not the vocabulary that consumes
// them.
//
// The catalog exists primarily so that an unknown attribute can become an
// ERROR. Today only <Companion>, its <Var>, and <Validate> declare their
// attribute vocabularies, so they are the only elements that reject a
// misspelling. Everywhere else — including applyLayout, which switches on
// the attribute key with no default arm — an unrecognized attribute is
// dropped in silence. Writing Left="10" instead of Canvas.Left="10" is
// accepted, ignored, and leaves the element at the origin with nothing on
// screen or in any error to explain it. A palette for a UI builder is the
// catalog's second customer, not its reason.
//
// The table below is GENERATED from markup.go's element switch by
// ./internal/catalogen; see catalog_gen.go. Do not hand-edit it. The
// generator is the drift test: a switch arm it cannot fully explain is a
// build failure, not a silently incomplete entry.

// Origin is where an element came from — its PROVENANCE. It deliberately
// does not answer whether the element's attributes are knowable; that is
// AttrsKnown, and conflating the two is a bug waiting for the day
// Context.Components learns to carry a schema.
type Origin string

const (
	// OriginBuiltin is an element in markup.go's switch. Fixed when this
	// package compiles: every gooey binary at a given version has the
	// same set.
	OriginBuiltin Origin = "builtin"
	// OriginRegistered is a Context.Components entry — a Go builder
	// function supplied by the host app. Per-process, and opaque: a
	// Builder is a func, so its attributes cannot be enumerated.
	OriginRegistered Origin = "registered"
	// OriginInclude is a markup-only control resolved through
	// Context.Includes (<Card/> loads card.gooey). Per-process, and
	// fully knowable: its <x:Property> declarations ARE its attributes.
	OriginInclude Origin = "include"
)

// ChildMode is what an element accepts as children. The distinction
// between ModeNone and ModeAttachments is load-bearing and is why this
// is not a bool: <Button> takes no visual children but does accept a
// nested <Tooltip> or <KeyBinding>, while <AdornmentLayer> accepts
// nothing at all.
type ChildMode string

const (
	ModeLeaf        ChildMode = "leaf"        // parses no children either way
	ModeNone        ChildMode = "none"        // any child is a load error
	ModeAttachments ChildMode = "attachments" // non-visual attachments only
	ModeOne         ChildMode = "one"         // exactly one visual child
	ModeMany        ChildMode = "many"        // any number of visual children
	ModeRestricted  ChildMode = "restricted"  // only the names in ChildSpec.Only
	// ModeUnknown accompanies an opaque element: if the attributes could
	// not be enumerated, neither could the child rule. It is a distinct
	// value rather than a zero-valued ModeLeaf for the same reason
	// AttrsKnown exists — "nothing" and "not known" must not look alike.
	ModeUnknown ChildMode = "unknown"
)

// Kind is an attribute's markup-level type — the grammar a value must
// satisfy, not the Go type behind it. Where a Go type is also meaningful
// (a binding's element type) it travels separately in AttrSpec.GoType.
type Kind string

const (
	KindText     Kind = "text"     // literal, or {{.Path}} interpolation
	KindString   Kind = "string"   // literal only, used verbatim
	KindInt      Kind = "int"      // decimal integer
	KindBool     Kind = "bool"     // "true" / "false"
	KindDuration Kind = "duration" // time.ParseDuration, must be positive
	KindColor    Kind = "color"    // #rgb or #rrggbb, or a bound Color handle
	KindStyle    Kind = "style"    // a name from the app's style table
	KindCommand  Kind = "command"  // an event: {{.Fn}} or a code-behind name
	KindEnum     Kind = "enum"     // one of AttrSpec.Enum
	KindGesture  Kind = "gesture"  // input.ParseGesture syntax, e.g. ctrl+s
	KindGridLens Kind = "gridlens" // Grid track spec, e.g. "Auto,1*,20"
	KindBinding  Kind = "binding"  // {{.Path}} only; GoType is the element type
	// KindIdentity is Name, and it is deliberately not KindString.
	//
	// Name is not a settable property — it is the ADDRESS. named() puts
	// it in Context.Named, PatchMarkup resolves by it and requires a
	// fragment root to carry the same one, and focus takes it. Changing
	// it does not change a value; it MOVES the element, invalidating
	// every outstanding patch target and anything holding that address.
	//
	// Emitting it as a plain string would put it in a property inspector
	// as a text field beside Content and Margin, inviting a rename
	// mid-edit that silently breaks the addressing of whatever is
	// patching that subtree. A consumer must decide what a rename means
	// rather than defaulting to a text box.
	KindIdentity Kind = "identity"
)

// Binds says which spellings an attribute accepts. It is separate from
// Kind because the same Kind differs by attribute: <Text> takes either a
// literal or a binding, while <Checkbox Checked> takes only a binding.
type Binds string

const (
	BindsLiteral Binds = "literal"
	BindsBinding Binds = "binding"
	BindsEither  Binds = "either"
)

// AttrSpec is one settable attribute.
type AttrSpec struct {
	Name string
	Kind Kind
	// GoType is the bound handle's element type for KindBinding —
	// "bool", "[]float64", "render.Color". Diagnostic for other kinds.
	GoType string
	Binds  Binds
	// Required makes an absent attribute a load error.
	Required bool
	// Enum lists the accepted literals for KindEnum.
	Enum []string
	// Default is the value that produces the behaviour you get by
	// OMITTING the attribute, written in markup spelling — "0" for
	// Width, "Visible" for Visibility, "cell" for a Button's Chrome. It
	// is what a property grid needs to render the handful of attributes
	// somebody actually changed differently from the rest; without it
	// every row looks equally significant.
	//
	// Empty means "no default worth showing", and that is a deliberate
	// third state rather than a missing value. Declaring one is only
	// allowed where it can be CHECKED, and the check is
	// TestDeclaredDefaultsRenderIdenticallyToOmission: build the element
	// with the attribute absent and with it set to Default, render both
	// into the same bounds, require identical cells. An attribute whose
	// effect is not visible in a static frame — a duration, a command, a
	// binding resolved at runtime — cannot be checked that way, so it
	// declares nothing rather than declaring something unguarded. See
	// TestDeclaredDefaultsAreDiscriminating for why the pair of tests is
	// needed and not just the first.
	Default string
	// Category groups the attribute in a property grid. Empty means
	// DERIVE — see CategoryOf. Deriving by default is what keeps this
	// from becoming 124 hand-written strings that rot; the field exists
	// to override the derivation where it is wrong, not to carry it.
	Category string
	// Origin is the attribute's own provenance. It is almost always the
	// element's, and differs only for an OPEN element: <Validate>'s rule
	// attributes are builtin ones plus whatever Context.Rules adds.
	Origin Origin
	Doc    string
}

// SlotSpec is one property element — a structured attribute whose value
// is markup rather than a string.
type SlotSpec struct {
	Name     string
	Required bool
	Doc      string
}

// BodySpec describes an element whose CONTENT is its XML body rather
// than an attribute: <Text>hello</Text>, not <Text Content="hello"/>.
//
// This exists because the fact was previously stated only in prose.
// defText's Doc said "The content is the element's body, not an
// attribute" and nothing in the data said so, which left a consumer
// two bad options. Keying on ChildSpec.Mode is the tempting one and it
// is wrong: fourteen builtins are ModeLeaf and exactly one reads
// e.Text, so a properties grid built that way offers a content row on
// thirteen elements that discard it. The other option is a hardcoded
// `name == "Text"`, which is the denylist this package keeps deleting.
//
// The fields mirror AttrSpec deliberately — a body is an attribute in
// every respect except where it is written, so a consumer that already
// renders an AttrSpec row can render this one with the same code. In
// particular Binds is NOT decoration: <Text>'s body goes through
// bindText, so {{.Title}} in the body is a live binding, and an editor
// that assumed literal-only would silently downgrade it to text.
//
// Note for anyone rendering a body editor: a body is whitespace-
// significant on ONE line and trimmed when wrapped across lines — see
// bodyText (toolkit.go) for the rule and why it needs no opt-in. So a
// body editor must round-trip leading and trailing spaces rather than
// trimming its own field, and writing a one-line body back as an
// indented multi-line one silently changes what it says.
type BodySpec struct {
	Kind   Kind
	Binds  Binds
	GoType string
	Doc    string
}

// ChildSpec describes what may nest inside an element.
type ChildSpec struct {
	Mode ChildMode
	// Only lists the permitted element names for ModeRestricted.
	Only []string
}

// ElementSpec is one entry in the catalog.
type ElementSpec struct {
	Name string
	// Origin is provenance only. Do NOT branch on it to decide whether
	// Attrs is trustworthy — that is AttrsKnown.
	Origin Origin
	// Go is the component type this element builds, e.g.
	// "*components.Button". Diagnostic; empty for OriginInclude.
	Go string
	// AttrsKnown reports whether Attrs is EXHAUSTIVE. When false, Attrs
	// is merely what could be discovered and may be empty — which is a
	// different statement from "this element takes no attributes", and a
	// consumer that renders the two the same way misreports the app.
	// A palette must key on this field, never on Origin.
	AttrsKnown bool
	// Opaque, when set, is the reason the generator could not enumerate
	// this element — the text of its //gooey:catalog-opaque annotation.
	Opaque string
	// Open reports that the attribute set is extensible at runtime, so
	// entries may carry an Origin different from the element's.
	Open  bool
	Attrs []AttrSpec
	// Slots are property elements: <StatusBar.Left>,
	// <ItemsView.ItemTemplate>. A slot can be REQUIRED — an
	// <ItemsView> without its template does not build — which is why
	// this is not a []string. The first consumer of the catalog
	// discovered that omission by generating markup that would not
	// load.
	Slots []SlotSpec
	// Body is non-nil when the element's content is its XML body rather
	// than an attribute. Nil means "no body content" — the common case,
	// and NOT the same statement as Children.Mode == ModeLeaf.
	Body     *BodySpec
	Children ChildSpec
	// Seed is the markup a palette should insert for a new instance of
	// this element — see ElementDef.Seed for the contract. Empty for an
	// element nobody has seeded, which TestEverySeededElementLoadsAnd
	// OccupiesSpace treats as a failure rather than as a third state:
	// an unseeded element is one a user can add and then not see.
	Seed string
	// NonVisual elements are attachments rather than laid-out children:
	// a parent hangs them off itself and they occupy no space.
	NonVisual bool
	// Focusable reports that the component type CAN be a focus stop.
	// This is a type-level statement: the instance decides at runtime
	// (AcceptsFocus may return false for a disabled control), exactly as
	// the catalog describes types and the tree describes instances.
	Focusable bool
	// Attaches reports that the type can host attachments, and HasLayout
	// that it accepts the layout attributes. Both are true for every
	// built-in, because they come from the embedded gooey.Base, so
	// neither is a useful signal for a palette; they matter only for
	// telling a custom OriginRegistered component apart.
	Attaches  bool
	HasLayout bool
	Doc       string
}

// universalAttrs is the surface every element with a Layout accepts,
// applied beside the element switch rather than by any one element:
// Name by named(), Tooltip by applyTooltipShorthand(), and the rest by
// applyLayout().
//
// It is declared here because those three functions read it by name and
// nothing else states it. TestEveryUniversalAttrIsRead pins that each
// row is actually consumed — the over-declaration direction, which
// rejection cannot see.
// The defaults below are read off gooey.Layout's zero value
// (layout.go:36) and applyLayout's parsers: AlignStretch and Visible are
// the iota-zero constants, and a zero Width/Height/Margin is "auto".
var universalAttrs = []AttrSpec{
	{Name: "HAlign", Kind: KindEnum, Binds: BindsLiteral, Enum: []string{"Center", "End", "Start", "Stretch"}, Default: "Stretch", Category: CategoryLayout, Origin: OriginBuiltin},
	{Name: "Height", Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
	{Name: "Margin", Kind: KindString, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
	{Name: "Name", Kind: KindIdentity, Binds: BindsLiteral, Origin: OriginBuiltin},
	{Name: "Tooltip", Kind: KindText, Binds: BindsEither, Origin: OriginBuiltin},
	{Name: "VAlign", Kind: KindEnum, Binds: BindsLiteral, Enum: []string{"Center", "End", "Start", "Stretch"}, Default: "Stretch", Category: CategoryLayout, Origin: OriginBuiltin},
	{Name: "Visibility", Kind: KindEnum, Binds: BindsEither, Enum: []string{"Collapsed", "Hidden", "Visible"}, Default: "Visible", Category: CategoryLayout, Origin: OriginBuiltin},
	{Name: "Width", Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
}

// attachedAttrs are contributed BY A PARENT to its children, keyed by
// the parent's element name. The dotted prefix IS the parent name —
// that is markup's own syntax, not a convention invented here — and
// applyLayout reads each one by its full dotted spelling.
//
// Canvas.Left is meaningful on a child of a <Canvas> and nowhere else,
// where applyLayout's missing default arm discards it in silence. That
// silence is the defect this whole catalog exists to remove, so the
// scoping has to be expressible rather than flattened into one list.
var attachedAttrs = map[string][]AttrSpec{
	"Canvas": {
		{Name: "Canvas.Left", Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
		{Name: "Canvas.Top", Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
	},
	"Grid": {
		// A zero span means one track (layout.go:47), so "0" — not "1" —
		// is the value that reproduces omission.
		{Name: "Grid.Col", Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
		{Name: "Grid.ColSpan", Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
		{Name: "Grid.Row", Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
		{Name: "Grid.RowSpan", Kind: KindInt, Binds: BindsLiteral, Default: "0", Category: CategoryLayout, Origin: OriginBuiltin},
	},
}

// The property-grid categories. They are Visual Studio's, minus the ones
// gooey has no members for, because a group with nothing in it is a
// distinction the model does not support.
const (
	CategoryLayout     = "Layout"
	CategoryAppearance = "Appearance"
	CategoryEvents     = "Events"
	CategoryDesign     = "Design"
	CategoryCommon     = "Common"
)

// CategoryOf is an attribute's property-grid group: the declared
// Category where there is one, otherwise derived from Kind.
//
// Derivation is the default on purpose. Hand-writing a category on all
// 124 rows would be 124 things that can drift, for grouping that is
// mostly mechanical — an event is an event because it is KindCommand,
// and a style or a colour is Appearance because of what it is. The
// declared field is the override for the rows where that reasoning is
// wrong, and the universal and attached tables use it because their
// membership in Layout comes from where they live rather than from their
// Kind: Margin is a KindString and Grid.Row a KindInt.
//
// Name derives to Design rather than Common because it is the address,
// not a value — the same fact KindIdentity exists to carry.
func CategoryOf(a AttrSpec) string {
	if a.Category != "" {
		return a.Category
	}
	switch a.Kind {
	case KindCommand:
		return CategoryEvents
	case KindStyle, KindColor:
		return CategoryAppearance
	case KindIdentity:
		return CategoryDesign
	}
	return CategoryCommon
}

// UniversalAttrs are the attributes accepted by every element that has
// a Layout — Name, Tooltip, and the FrameworkElement surface. They are
// applied by named, applyTooltipShorthand and applyLayout rather than by
// any arm of the element switch, so they are deliberately NOT repeated
// in each ElementSpec.Attrs; join them in wherever HasLayout is true.
//
// HasLayout is exactly that join key. It is not a discriminator between
// built-ins (they all have a Layout, via the embedded gooey.Base) — it
// is the flag that says an element takes this set at all, and position
// and size are the primary interaction in a visual editor.
func UniversalAttrs() []AttrSpec {
	out := make([]AttrSpec, len(universalAttrs))
	copy(out, universalAttrs)
	return out
}

// AttachedAttrs returns the attributes a parent element contributes to
// its children — Grid.Row from a <Grid>, Canvas.Left from a <Canvas>.
//
// These are the reason the catalog cannot be a flat attribute list per
// element: their validity depends on the PARENT, not on the element
// carrying them. Canvas.Left is meaningful on a child of a <Canvas> and
// meaningless anywhere else, where applyLayout's missing default arm
// discards it in silence — the very defect this catalog exists to fix.
// A consumer that offers Canvas.Left on a child of a <VStack> is
// promising positioning that will never happen.
func AttachedAttrs(parent string) []AttrSpec {
	src := attachedAttrs[parent]
	out := make([]AttrSpec, len(src))
	copy(out, src)
	return out
}

// AttachedParents lists the elements that contribute attached
// properties, sorted.
func AttachedParents() []string {
	out := make([]string, 0, len(attachedAttrs))
	for p := range attachedAttrs {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// AttrsFor is the whole answer to "what may I set on this element, here"
// — the element's own attributes, the universal set if it has a Layout,
// and whatever its parent contributes. Pass an empty parent for an
// element at the root.
//
// A consumer should call THIS rather than reading ElementSpec.Attrs
// directly: the per-element list alone is a true statement about the
// element and a misleading answer to the question actually being asked.
func AttrsFor(e ElementSpec, parent string) []AttrSpec {
	out := append([]AttrSpec(nil), e.Attrs...)
	if TakesLayout(e) {
		out = append(out, universalAttrs...)
		out = append(out, attachedAttrs[parent]...)
	} else {
		// Name is universal even where the layout surface is not: every
		// element can be addressed.
		for _, a := range universalAttrs {
			if a.Kind == KindIdentity {
				out = append(out, a)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// TakesLayout reports whether an element actually accepts the universal
// layout surface — and it is deliberately not the same as HasLayout.
//
// HasLayout is a TYPE-level fact: every built-in satisfies gooey.HasLayout
// because they all embed gooey.Base. But a NON-VISUAL element has no
// bounds to place, so Width and Grid.Row are meaningless on a <Timer>
// however the interfaces read. companionAttrs reached this conclusion
// first and omits them deliberately.
//
// This exists because the two came apart in exactly the way that
// misleads: the rejection path already refused layout attributes on
// non-visual elements while AttrsFor still offered them, so the catalog
// advertised attributes the loader would refuse. A palette built on that
// would let someone set Width on a <Timer> and then fail the load — the
// catalog lying about the target, through the artifact built to stop it.
// Found by TestDeclaredVocabularyElementsKeepTheirExactSet.
func TakesLayout(e ElementSpec) bool { return e.HasLayout && !e.NonVisual }

// BuiltinElements returns the generated table: the element vocabulary
// this build of gooey compiled in, with no reference to any app. Callers
// that have a Context should use Context.Catalog instead, which is the
// only answer that matches what a given app can actually build.
func BuiltinElements() []ElementSpec {
	defs := definedElements()
	out := make([]ElementSpec, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.spec())
	}
	return out
}

// Catalog is the full element vocabulary available to THIS context: the
// compiled-in built-ins, plus the host's registered Go builders, plus
// every markup-only control reachable through Includes. Sorted by name.
//
// The three sources have different lifetimes and different knowability,
// and the entries say so rather than presenting a flat list that quietly
// mixes them. In particular a registered component contributes a NAME
// AND NOTHING ELSE — Builder is an opaque func — so its entry carries
// AttrsKnown false, which a consumer must distinguish from an element
// that genuinely takes no attributes.
func (ctx *Context) Catalog() []ElementSpec {
	builtins := BuiltinElements()
	out := make([]ElementSpec, 0, len(builtins)+len(ctx.Elements)+len(ctx.Components))
	seen := make(map[string]bool, len(builtins))

	// DECLARED HOST ELEMENTS FIRST, and the order is the whole
	// correctness argument rather than a preference.
	//
	// buildComponent consults Context.Elements BEFORE the built-in
	// registry, so a host declaration of a name gooey also defines is
	// what actually builds. Adding builtins first and skipping the
	// collision — which is what the Components loop below does — would
	// leave the catalog describing an element the document will never
	// get: right name, wrong attributes, wrong Go type. A palette
	// reading that would offer attributes the real component rejects.
	//
	// The Components loop keeps its skip because it has nothing better
	// to offer: an opaque builder's entry is a name and a disclaimer, so
	// replacing a builtin's real vocabulary with it would lose
	// information rather than correct it. That asymmetry is the point of
	// declaring.
	for name, d := range ctx.Elements {
		seen[name] = true
		out = append(out, d.specAs(OriginRegistered))
	}
	for _, e := range builtins {
		if seen[e.Name] {
			continue
		}
		if e.Open {
			e.Attrs = ctx.openAttrs(e)
		}
		seen[e.Name] = true
		out = append(out, e)
	}
	for name := range ctx.Components {
		if seen[name] {
			// A registered builder shadows the built-in of the same name:
			// buildComponent consults Context.Components FIRST.
			continue
		}
		seen[name] = true
		out = append(out, ElementSpec{
			Name:       name,
			Origin:     OriginRegistered,
			AttrsKnown: false,
			Doc:        "Registered by the host app. Its attributes cannot be enumerated: a Builder is a func, not a schema.",
		})
	}
	out = append(out, ctx.includeElements(seen)...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// openAttrs rebuilds an open element's attribute list for this context.
// Only <Validate> is open today: its vocabulary is the built-in rules
// plus every name in Context.Rules, which is exactly what the load error
// at validateRuleNames already reports.
func (ctx *Context) openAttrs(e ElementSpec) []AttrSpec {
	attrs := make([]AttrSpec, len(e.Attrs), len(e.Attrs)+len(ctx.Rules))
	copy(attrs, e.Attrs)
	known := make(map[string]bool, len(attrs))
	for _, a := range attrs {
		known[a.Name] = true
	}
	extra := make([]string, 0, len(ctx.Rules))
	for name := range ctx.Rules {
		if !known[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		attrs = append(attrs, AttrSpec{
			Name: name, Kind: KindString, Binds: BindsLiteral,
			Origin: OriginRegistered,
			Doc:    "A rule registered by the host app through Context.Rules.",
		})
	}
	return attrs
}

// includeElements enumerates the markup-only controls reachable through
// Includes. Unlike a registered builder these are FULLY knowable: a
// control's <x:Property> declarations are its public surface, which is
// the same data DeclaredSchema serves over the control plane.
//
// A control whose own markup fails to parse is reported as an entry with
// AttrsKnown false rather than dropped: the file is on disk and <Card/>
// will be attempted, so hiding it would misdescribe the app.
func (ctx *Context) includeElements(seen map[string]bool) []ElementSpec {
	if ctx.Includes == nil {
		return nil
	}
	names, err := fs.Glob(ctx.Includes, "*.gooey")
	if err != nil {
		return nil
	}
	sort.Strings(names)
	out := make([]ElementSpec, 0, len(names))
	for _, file := range names {
		name := controlElementName(file)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		e := ElementSpec{Name: name, Origin: OriginInclude, AttrsKnown: true,
			Doc: "A markup-only control loaded from " + file + "."}
		src, err := fs.ReadFile(ctx.Includes, file)
		if err != nil {
			e.AttrsKnown, e.Opaque = false, "could not be read: "+err.Error()
			out = append(out, e)
			continue
		}
		decls, err := Declarations(src)
		if err != nil {
			e.AttrsKnown, e.Opaque = false, "does not parse: "+err.Error()
			out = append(out, e)
			continue
		}
		for _, d := range decls {
			e.Attrs = append(e.Attrs, AttrSpec{
				Name:     d.Name,
				Kind:     declKind(d.Type),
				GoType:   d.Type,
				Binds:    BindsEither,
				Required: d.Required,
				Origin:   OriginInclude,
			})
		}
		out = append(out, e)
	}
	return out
}

// controlElementName maps card.gooey to the element name <Card>, which
// is the convention Include resolves by.
func controlElementName(file string) string {
	base := strings.TrimSuffix(path.Base(file), ".gooey")
	if base == "" {
		return ""
	}
	return strings.ToUpper(base[:1]) + base[1:]
}

// declKind maps an <x:Property> declared type onto a catalog Kind. The
// spellings are propKinds' own rows.
func declKind(t string) Kind {
	switch t {
	case "int":
		return KindInt
	case "bool":
		return KindBool
	case "float":
		return KindString
	case "duration":
		return KindDuration
	case "color":
		return KindColor
	}
	return KindText
}
