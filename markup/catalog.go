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
// The table below is DECLARED, one ElementDef literal per element, in
// elements.go — beside the code that consumes it. It used to be
// generated into a committed catalog_gen.go from markup.go's element
// switch, and that file has not existed for some time; ./internal/catalogen
// survives as the CROSS-CHECK rather than the source, comparing each
// declaration against the attributes its Build actually reads. See that
// package's doc for why the two directions of that comparison are not
// symmetric — over-declaring is silent, and nothing but this catches it.

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

	// Role is what an ATTACHED attribute means, independent of what it
	// is called. Empty for every attribute that is not attached.
	//
	// It exists because a visual editor manipulates geometry by MEANING
	// — "put this element in the next column" — while markup carries
	// only names, and the two are not the same vocabulary. Without it an
	// editor has to know that a cell's column is spelled "Grid.Col",
	// which is exactly the element-name knowledge Grant exists to move
	// out of editors. With it the editor asks the parent's grant for
	// RoleCol and writes whatever name comes back, so a third-party
	// container spelling it "Table.Column" needs no editor change.
	Role Role

	Doc string
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
	// ParsedBy names the element whose Build consumes this one, for a
	// pseudo-element that has no Build of its own worth speaking of —
	// <Menu> and <MenuItem> are both read by <MenuBar>. Empty for a
	// pseudo-element that says why through Opaque instead, and empty
	// for everything that builds a component.
	//
	// It is carried onto the spec rather than consumed and dropped at
	// ElementDef because a diagnostic wants to NAME the reader:
	// "<MenuBar> reads <Menu> as data" tells an author where the
	// attribute went, and "its parent reads it as data" does not.
	// Raised in review of #486 — refuseComponentAttr's reason clause leaned
	// on this field while the type it reads did not carry it, so the
	// message it could actually produce was the generic one.
	ParsedBy string
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
	// Icon names an icon for this element in the CONSUMER's icon set —
	// no directory, no extension, no colour. See ElementDef.Icon for
	// why it is a name rather than a picture: rasterizing one needs the
	// nested imagefmt/svg module, and a field that carried an image
	// would put a vector renderer in core's dependency graph.
	//
	// Empty means the element declares no icon. A palette must render
	// that as an absence rather than substituting a default, for the
	// same reason AttrsKnown false is not "no attributes".
	Icon string
	// Grants is the geometry this element confers ON ITS CHILDREN — see
	// Grant. Read it to answer "what does it mean to move an element
	// inside this one?" without knowing the element's name.
	//
	// Children describes what may go IN; Grants describes what happens
	// to it once it is there. The two are independent: <Border> takes a
	// child and grants nothing, <VStack> takes many and grants only
	// order.
	Grants Grant
	// Seed is the markup a palette should insert for a new instance of
	// this element — see ElementDef.Seed for the contract. Empty for an
	// element nobody has seeded, which TestEverySeededElementLoadsAnd
	// OccupiesSpace treats as a failure rather than as a third state:
	// an unseeded element is one a user can add and then not see.
	Seed string
	// Nested reports that this element is legal ONLY inside a parent
	// that names it — <Tab> in <Tabs>, <Menu> in <MenuBar>, <MenuItem>
	// in <Menu>. It is declared so a property grid and the loader know
	// its vocabulary, and it must never be offered on its own: a palette
	// that lists it invites markup that cannot load.
	//
	// DERIVED, not declared, and that is the point. The answer already
	// exists in the vocabulary — Pseudo, and named by some other entry's
	// Children.Only under ModeRestricted — so a field an author sets by
	// hand would be a second copy of a fact the registry already
	// carries, which is the drift ElementDef's own doc comment gives as
	// the reason the behavioural axes are derived too. Read markNested
	// for why it takes BOTH conjuncts; the Only list on its own is the
	// converse of this claim, not this claim.
	//
	// It replaces a hardcoded `e.Name == "Tab"` in the wysiwyg palette.
	// That check was correct and unmaintainable in the same breath: the
	// second nested element was silently offered, and nothing anywhere
	// went red. See markNested.
	Nested bool
	// Pseudo reports that this element builds NO COMPONENT OF ITS OWN:
	// its parent's Build reads it as data and draws the result itself.
	// <Tab>, <Menu> and <MenuItem> are the three today.
	//
	// It matters to anything holding a correspondence between document
	// elements and built components, because for these there is no
	// component to hold — and the failure is not an absence, it is a
	// WRONG PAIRING. A <MenuBar> with one <Menu> hands back exactly one
	// child (its dropdown surface), so a walk pairing children by index
	// maps the <Menu> onto the popup and every question asked of that
	// pairing afterwards is answered about the wrong thing. Counts agree
	// perfectly; nothing looks wrong. The designer's mapNodes is the
	// consumer, and it had that bug.
	//
	// DERIVED from a nil Proto AND a stated reason — Opaque, or a
	// ParsedBy naming the reader. A nil Proto is what "no component"
	// means in an ElementDef, since the behavioural axes are all read
	// off Proto; the second conjunct is what stops this becoming true
	// by accident.
	//
	// The conjunct is not belt-and-braces for the builtins, where
	// TestDeclaredElementsCarryAProtoOrSayWhyNot already forces the
	// reason. It is there for a HOST's def, which that test never sees:
	// it ranges over the builtin registry, and nothing rejects a
	// Context.Elements entry with a real Build and no Proto. Deriving
	// from the nil alone would make such a def silently pseudo —
	// dropped from every palette that filters Nested, and
	// unselectable-through in the designer — with no error anywhere.
	Pseudo bool
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
	// AxesKnown reports that the four axes above were DERIVED FROM A
	// PROTO rather than defaulted, and it is to them what AttrsKnown is
	// to Attrs. Without it every axis reads false for two different
	// facts at once — "the type does not do this" and "no type was
	// given" — and a consumer cannot tell a <Timer>, which really is
	// not laid out, from a host def with a real Build and no Proto,
	// which is. TakesLayout is where that mattered; see its doc for the
	// measurement.
	AxesKnown bool
	// Builds reports that this spec came from an ElementDef carrying a
	// Build — so the loader runs that Build and hands the result to
	// applyLayout. It exists to scope the !AxesKnown arm of TakesLayout
	// to the only specs it was measured on.
	//
	// The three literal-built specs have no Proto either, so !AxesKnown
	// is true of all of them: Context.Components entries
	// (OriginRegistered), Context.Includes controls (OriginInclude),
	// and apps/wysiwyg's bare ElementSpec{Name: elem} fallback for an
	// element the catalog does not know. Without this field
	// TakesLayout(ElementSpec{}) answered true and the designer offered
	// the whole universal row set, plus every attached grant, on
	// unknown elements — measured in review of #486.
	Builds bool
	Doc    string
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

// GrantKind is the geometry a container confers on its CHILDREN — the
// layout model you are designing in when an element sits inside it.
//
// This is a property of the PARENT and never of the element itself. A
// <Text> has no opinion about whether it is positioned by an offset, by
// a cell, or by its index; the container it is in decides, and moving
// that element to a different container changes the answer without
// touching the element at all.
//
// The taxonomy was discovered in an editor before it was named here.
// apps/wysiwyg's dragKind switched on the literal strings "Canvas" and
// "Grid" and treated everything else as order — a correct rule keyed on
// the wrong thing, because an editor that knows element NAMES cannot be
// extended by an app registering a container of its own. Naming it on
// the definition makes the editor's question "what does this parent
// grant?" rather than "is this parent one of the two I know?".
type GrantKind string

const (
	// GrantNone is the zero value: this element confers no geometry.
	//
	// It is NOT the same statement as "this element has no children". A
	// <Border> holds exactly one child and positions it itself, so the
	// child has no geometry to edit — which an editor must report as
	// "the border places this", not as "you may reorder it".
	GrantNone GrantKind = ""

	// GrantOffset is free geometry: each child carries an X and a Y and
	// goes exactly where it is put. <Canvas>. This is the model a
	// direct-manipulation drag is trivial in, and it was the only one
	// gooey's designer could edit.
	GrantOffset GrantKind = "offset"

	// GrantCell is addressed geometry: each child carries a cell, and
	// possibly a span, and the CONTAINER computes the rectangle.
	// <Grid>. A drag snaps, and the editor cannot know where the cells
	// are without asking the layout — see apps/wysiwyg's gridCells,
	// which probes them through the real Grid.Arrange for exactly this
	// reason.
	GrantCell GrantKind = "cell"

	// GrantOrder is implicit geometry: a child has no positional
	// attribute at all, because its POSITION IS ITS INDEX among its
	// siblings. <VStack>, <HStack>, <ButtonBar>.
	//
	// The absence of attached attributes is the defining feature rather
	// than an omission, and it is why Grant.Attached is empty here: an
	// editor moving an element in this model edits the DOCUMENT ORDER,
	// not a value.
	GrantOrder GrantKind = "order"
)

// Role is the MEANING of an attribute, so an editor can manipulate
// geometry without knowing any attribute's name. See AttrSpec.Role.
//
// Two families, distinguished by whose attribute carries them:
//
//   - the CHILD's attached attributes — RoleX through RoleColSpan —
//     which say where one child sits, and live on Grant.Attached;
//   - the CONTAINER's own attributes — RoleRowTracks, RoleColTracks —
//     which say what the cells ARE, and live on the element's Attrs.
//
// The second family is what lets an editor draw and edit a grid's
// structure. Without it, showing "Auto / 1* / 20" against the tracks
// they produce would mean knowing that a Grid spells them "Rows" and
// "Cols", which is the element-name knowledge this whole contract
// removes.
type Role string

const (
	RoleNone Role = ""
	// RoleX and RoleY are a GrantOffset child's position.
	RoleX Role = "x"
	RoleY Role = "y"
	// RoleRow, RoleCol and their spans are a GrantCell child's cell.
	RoleRow     Role = "row"
	RoleCol     Role = "col"
	RoleRowSpan Role = "rowspan"
	RoleColSpan Role = "colspan"
	// RoleRowTracks and RoleColTracks are the CONTAINER's own
	// attributes declaring its track structure — the thing a child's
	// RoleRow indexes into.
	RoleRowTracks Role = "rowtracks"
	RoleColTracks Role = "coltracks"
)

// AttrByRole is the name of e's OWN attribute carrying a role, or "" if
// it declares none. The container-side counterpart to Grant.Attr.
func AttrByRole(e ElementSpec, r Role) string {
	if r == RoleNone {
		return ""
	}
	for _, a := range e.Attrs {
		if a.Role == r {
			return a.Name
		}
	}
	return ""
}

// Grant is what an element confers on its children: which layout model
// they are positioned by, and the attached attributes that carry it.
//
// Attached lives HERE, on the granting element's own definition, rather
// than in a map keyed by parent name — which is what it was. That map
// was the side table the colocation doctrine in ElementDef warns about,
// and it had already drifted the way that doctrine predicts: it was the
// only mention of Grid.RowSpan anywhere, three hundred lines from the
// <Grid> literal and connected to it by nothing, so an element with
// attached properties had two unrelated places to remember.
type Grant struct {
	// Kind is the layout model. GrantNone means this element positions
	// its children itself, or has none.
	Kind GrantKind

	// Attached are the attributes this parent contributes to each of its
	// children — Grid.Row from a <Grid>, Canvas.Left from a <Canvas>.
	//
	// These are the reason the catalog cannot be a flat attribute list
	// per element: their validity depends on the PARENT, not on the
	// element carrying them. Canvas.Left is meaningful on a child of a
	// <Canvas> and meaningless anywhere else, where applyLayout's
	// missing default arm discards it in silence — the very defect this
	// catalog exists to fix. A consumer that offers Canvas.Left on a
	// child of a <VStack> is promising positioning that will never
	// happen.
	//
	// The dotted prefix IS the parent name — that is markup's own
	// syntax, not a convention invented here — and applyLayout reads
	// each one by its full dotted spelling.
	Attached []AttrSpec
}

// Attr is the name of the attached attribute carrying a role, or "" if
// this grant does not carry it.
//
// THIS is the call an editor makes instead of writing "Grid.Row", and
// the empty answer is a real one that must be handled: asking a
// GrantOrder parent for RoleX is a legitimate question whose answer is
// "there isn't one".
func (g Grant) Attr(r Role) string {
	if r == RoleNone {
		return ""
	}
	for _, a := range g.Attached {
		if a.Role == r {
			return a.Name
		}
	}
	return ""
}

// Roles is every role this grant carries, sorted, for a consumer that
// wants to enumerate rather than ask.
func (g Grant) Roles() []Role {
	out := make([]Role, 0, len(g.Attached))
	for _, a := range g.Attached {
		if a.Role != RoleNone {
			out = append(out, a.Role)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// AttachedAttrs is what this grant contributes to a child, copied.
//
// This is the form a Context consumer must use. The package-level
// AttachedAttrs takes a parent NAME and resolves it in the builtin
// registry, so it answers "nothing" for a container the host registered
// — an editor asking it about the document's own vocabulary gets a
// confident wrong answer rather than an error. A caller that already
// holds the parent's ElementSpec (from Context.Catalog) holds the grant
// too, and should ask it directly. Found in review of #390 (issue #418).
func (g Grant) AttachedAttrs() []AttrSpec {
	out := make([]AttrSpec, len(g.Attached))
	copy(out, g.Attached)
	return out
}

// AttrsFor is the package-level AttrsFor with the parent already
// resolved — the same join, reached without a registry lookup. See
// AttachedAttrs above for why a Context consumer needs this form.
//
// THE AGREEMENT WITH THE LOADER RUNS ONE WAY FOR LAYOUT, and the other
// way is a gap rather than a guarantee. This paragraph used to say
// "nothing this offers fails to load, because the TakesLayout gate here
// and Context.vocabulary's are the same predicate on the same spec".
// Both clauses stopped being true in round 11 of #486: vocabulary
// gained a disjunct this has no equivalent of (attrcheck.go), and
// applyLayout gained a refusal for a def whose Build returns a
// component with no Layout — which nothing here can anticipate, because
// what Build RETURNS is unknowable before it runs. Measured on the
// Bareish fixture, parent Grid:
//
//	AttrsFor offers Margin, Width, Height, Grid.Row (among others)
//	Build(<Bareish Label="a" Margin="2"/>) -> refused by name
//
// That is the catalog lying about the target, which is what TakesLayout
// exists to prevent, and it reaches a surface: apps/wysiwyg builds its
// property inspector from this call. It is not closable from here, so
// it is stated like the Name gap below and pinned by
// TestTheGridOffersLayoutRowsTheLoaderWillRefuse, which goes red the
// moment either gate moves. Raised in review of #486.
//
// THE CONVERSE IS CLOSED. It used to be
// unguarded: TakesLayout read HasLayout, which ElementDef.axes derives
// from the PROTO, so a HOST's def with a real Build and no Proto
// answered false while build() ran applyLayout on the component that
// Build returns — it LOADED with the margin honoured and this offered
// no row for it. AxesKnown is what separated "does not" from "cannot
// say"; TakesLayout answers true where nobody can say otherwise, so the
// row is offered and the load agrees. Measured both ways in
// TestTheDesignerAndTheLoaderAgreeAboutAnUnknowableDef, which is what
// goes red if either side drifts. Raised in review of #486 and closed
// in round 10 of it.
//
// AND THE PARENT IS THE OTHER THING THIS CANNOT SEE, which is a second
// unguarded direction rather than a restatement of the first. Whether
// the loader honours Name turns on whether the element is read AS DATA,
// and that is a fact about its PARENT — Context.vocabulary gets it from
// checkAttrs' asData, and this function is handed a spec and nothing
// else. A HOST def that SHADOWS a builtin reader's child is where the
// two come apart, because a shadowing def carries a Proto, so Pseudo is
// false and TakesLayout is true and the universalAttrs branch below
// adds Name. Measured on Elements["Tab"] = {Known, Proto:
// &components.Text{}, Attrs: [Header]}:
//
//	AttrsFor(that <Tab>)  -> [HAlign Header Height Margin Name
//	                          Tooltip VAlign Visibility Width]
//	Build(<Tabs><Tab Header="a" Name="zonk">…)
//	                      -> markup: <Tab Name="zonk">: <Tabs> reads
//	                         <Tab> as data, so it builds no component
//	                         for Name to apply to
//
// So the grid offers a row that fails to load, which is the direction
// the sentence below calls the one that must agree. It cannot be closed
// from here without the parent, and adding a parent parameter would
// make a catalog query depend on where the element is about to be put —
// so it is STATED and pinned. The layout direction above USED to be
// stated the same way and is now closed, which is why this paragraph
// no longer points at it as the precedent. Raised in review of #486.
func (g Grant) AttrsFor(e ElementSpec) []AttrSpec {
	out := append([]AttrSpec(nil), e.Attrs...)
	if TakesLayout(e) {
		out = append(out, universalAttrs...)
		out = append(out, g.Attached...)
	} else if !e.Pseudo {
		// Name is universal even where the layout surface is not: every
		// element that BUILDS one can be addressed. A pseudo-element is
		// consumed as data and never reaches named(), so offering the row
		// invites an edit the loader accepts and nothing honours. The
		// loader withholds it for the same reason (Context.vocabulary),
		// and in THIS direction the two must agree or the grid offers a
		// row that fails to load.
		//
		// THE OTHER DIRECTION IS NOT GUARDED, and the sentence above
		// used to claim it was. Context.vocabulary decides Name on the
		// CALL SITE — `builds`, which is `!asData` — and this function
		// has only the spec. For the builtins the two answers coincide,
		// because buildTabs and buildMenuBar are the readers and they
		// pass asData. For a HOST def carrying ParsedBy AND a real Build
		// they do not: the parent's Build routes children through
		// BuildChildren, the child reaches build(), named() runs, and
		// the grid still offers nothing. Measured:
		//
		//	AttrsFor(hostDeckCtx's <Panel>)            -> [Label]
		//	Build(`<Deck><Panel Label="a" Name="p">…`) -> ctx.Named["p"]
		//
		// Carrying a has-a-Build bit onto the spec does not close it:
		// defTab, defMenu and defMenuItem all have a non-nil Build whose
		// only job is to refuse, so the bit is true for exactly the
		// three elements the row must stay off — measured, it reddens
		// TestTheDesignerOffersNoUniversalRowOnAPseudoElement and
		// TestTheCatalogDoesNotOfferNameOnAPseudoElement. The fact that
		// separates the two is whether Build RETURNS a component, which
		// no field can state. So the gap is pinned rather than closed,
		// by TestTheDesignerOffersNoNameRowWhereTheLoaderHonoursOne —
		// which is also what goes red if somebody closes it. Raised in
		// review of #486.
		//
		// THE AGREEMENT WAS ONCE CONDITIONED ON AttrsKnown, and is not
		// any more (#461). checkAttrs returned early on !AttrsKnown, so
		// for an OPAQUE pseudo-element this gate dropped the row while
		// the loader accepted it: <Tab Name="Zonk"> loaded clean and was
		// dropped, with no designer surface left to reveal it.
		// checkAttrs now refuses every attribute only a COMPONENT could
		// carry BEFORE either of its gates — the universal set and any
		// attached property, which is what cannotApplyTo admits — because
		// neither belongs to the element's own surface and so both
		// survive not knowing it (refuseComponentAttr, attrcheck.go).
		//
		// TestNoPseudoElementAcceptsAUniversalAttribute and
		// TestTheDesignerOffersNoUniversalRowOnAPseudoElement assert the
		// two halves over the catalog rather than over a list, so a
		// fourth pseudo-element joins both without an edit.
		//
		// THAT IS TRUE OF THE GATES AND WAS NOT TRUE OF THE WIRING, and
		// the distinction is worth keeping because the sentence above
		// used to be written without it. Both of those tests call
		// checkAttrs themselves, so neither can see a BUILDER that never
		// calls it — which is the defect #461 actually was.
		// TestEveryPseudoElementIsRefusedThroughAWholeLoad covers that,
		// and its cases are hand-written markup with a derived
		// completeness check, because a document cannot be generated
		// from a spec. Raised in review of #486.
		for _, a := range universalAttrs {
			if a.Kind == KindIdentity {
				out = append(out, a)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// grantOf is the registry lookup behind AttachedAttrs and AttrsFor.
// Builtins only — a Context-registered element is reachable through
// Context.Catalog, whose ElementSpec carries the same Grant, and whose
// Grant carries the two methods above.
func grantOf(element string) Grant {
	if d := elementDefs[element]; d != nil {
		return d.Grants
	}
	return Grant{}
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
	return grantOf(parent).AttachedAttrs()
}

// AttachedParents lists the elements that contribute attached
// properties, sorted.
func AttachedParents() []string {
	out := []string{}
	for _, d := range definedElements() {
		if len(d.Grants.Attached) > 0 {
			out = append(out, d.Name)
		}
	}
	sort.Strings(out)
	return out
}

// GrantOf is the layout model an element confers on its children, by
// element name. Builtins only; a consumer with a Context should read
// ElementSpec.Grants off Context.Catalog instead, which also answers for
// elements the host registered.
func GrantOf(element string) Grant {
	g := grantOf(element)
	g.Attached = append([]AttrSpec(nil), g.Attached...)
	return g
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
	return grantOf(parent).AttrsFor(e)
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
// THE UNKNOWABLE DEF ANSWERS TRUE, and that conjunct is what makes this
// predicate match the loader in BOTH directions rather than one. HasLayout
// is derived from Proto, so a def with a real Build and no Proto reports
// false for "the type does not accept layout" and for "nobody can say"
// alike — and build() runs applyLayout on whatever that Build returns.
// Answering false there refused Margin, Width, Grid.Row and every other
// attached name on an element that honours all of them: measured in review
// of #486 on one def, flipping Known alone, <Built Label="a" Grid.Row="0"
// Width="7"/> inside a <Grid> was a load error with Known true and read
// back Grid.Row=0 Width=7 with Known false. AxesKnown is the question
// AttrsKnown already asks about the element's own attributes, asked about
// the behavioural axes instead.
//
// Pseudo is the third conjunct rather than a consequence of the first two.
// A pseudo-element also has no Proto, so !AxesKnown holds for it — but it
// builds NO component, which is a reason to withhold the layout surface
// that survives knowing everything about it.
// AND THE ARM IS SCOPED TO A DEF THAT BUILDS, because "nobody can say"
// is true of three literal-built spec shapes that have no Build here at
// all — registered components, include controls, and the bare
// ElementSpec{Name: elem} an unknown element gets. TakesLayout of the
// zero value answered true without this conjunct. Review of #486
// measured the consequence on a registered builder: AttrsFor offered
// [HAlign Height Margin Name Tooltip VAlign Visibility Width] where it
// had offered Name alone.
//
// What remains inside the arm — a def whose Build returns a component
// with no Layout — is no longer silent either: applyLayout REPORTS the
// layout attributes it cannot apply rather than returning nil, so the
// answer here is "accepted and honoured, or refused by name", never
// "accepted and dropped".
func TakesLayout(e ElementSpec) bool {
	return (e.HasLayout || (!e.AxesKnown && e.Builds)) && !e.NonVisual && !e.Pseudo
}

// BuiltinElements returns the DECLARED table: the element vocabulary
// this build of gooey compiled in, with no reference to any app. Callers
// that have a Context should use Context.Catalog instead, which is the
// only answer that matches what a given app can actually build.
func BuiltinElements() []ElementSpec {
	defs := definedElements()
	out := make([]ElementSpec, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.spec())
	}
	markNested(out)
	return out
}

// markNested derives Nested, which is what makes "may this be placed on
// its own?" an answer the vocabulary gives rather than one a palette
// hardcodes.
//
// TWO CONJUNCTS, AND THE SECOND IS NOT REDUNDANT. An element is nested
// when it is Pseudo *and* some other entry names it in Children.Only
// under ModeRestricted.
//
// The Only list alone is the CONVERSE of what is wanted and they are not
// equivalent. ModeRestricted says "this container accepts only these";
// Nested says "this element is accepted only inside a container that
// names it". A host declaring a toolbar with Only: ["Button"] — an
// entirely reasonable container — would mark <Button> nested and
// vanish it from every palette, silently, which is the same class of
// failure this field exists to remove, inverted. Restricting the
// container says nothing about where its child may otherwise go.
//
// Pseudo is what closes that: an element that builds no component of its
// own cannot stand anywhere, because there is nothing for it to be. A
// <Button> under a restricted toolbar keeps its Proto and stays on
// offer; <Tab>, <Menu> and <MenuItem> have none and do not.
//
// Pseudo alone would nearly do, and the Only conjunct is kept because
// the two claims are different: Pseudo is "builds nothing", Nested is
// "has exactly one legal home". An element with no component and no
// parent naming it is a declaration nothing can reach, which is a bug
// to notice rather than a palette entry to hide.
//
// ASSIGNED, NOT OR-ED. This runs twice over overlapping data —
// BuiltinElements marks, then Catalog re-runs over a list seeded from
// it — so a conditional set would ACCUMULATE rather than derive: a
// host shadowing <Tabs> with an unrestricted declaration leaves nothing
// restricting to <Tab>, and <Tab> would still arrive already marked
// from the builtin pass. Assigning unconditionally makes this
// idempotent and makes the field's "DERIVED" claim literally true.
//
// A name is looked up rather than trusted: Only may name an element that
// does not exist in this catalog — a host's restricted container
// referring to something it did not register — and the loader reports
// that at build time. Nothing here needs to.
//
// Run over the ASSEMBLED list rather than over the registry, so a host's
// own declared container contributes to it on the same terms as a
// builtin. That is the half a registry-only derivation would miss.
func markNested(specs []ElementSpec) {
	named := make(map[string]bool)
	for _, e := range specs {
		if e.Children.Mode != ModeRestricted {
			continue
		}
		for _, name := range e.Children.Only {
			named[name] = true
		}
	}
	for i := range specs {
		specs[i].Nested = specs[i].Pseudo && named[specs[i].Name]
	}
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
func (ctx *Context) Catalog() []ElementSpec { return ctx.catalog(true) }

// catalog is Catalog with the Includes source made optional, for the one
// caller that asks a question no include can answer — see namingParent.
// Everything else about the assembly, the collision order included, is
// identical, so the two cannot disagree about which element wins a name.
func (ctx *Context) catalog(withIncludes bool) []ElementSpec {
	// MEMOIZED FOR THE DURATION OF ONE BUILD, and only the
	// withIncludes=false form, which is the one the load path asks for
	// repeatedly. document.build clears and restores the field the same
	// way it does ctx.ns, so a nested Load (a UserControl mid-build)
	// cannot hand its parent a catalog assembled against a different
	// Context.Elements, and a later rebuild against the same Context
	// re-assembles.
	//
	// THE COST IS PER ELEMENT, which is what makes this worth a field.
	// misplaced -> declaredHome -> namingParent asks for this catalog,
	// and misplaced's first line short-circuits only when the PARENT
	// NAMES the child — so a ModeRestricted container costs nothing,
	// and an Opaque pseudo-element under a plain BuildChildren
	// container pays a full assembly (BuiltinElements plus markNested)
	// for every child. Measured on that shape before the memo, a
	// document of N such rows:
	//
	//	rows    assemblies    load before    load after
	//	   1              1        0.08ms        0.08ms
	//	  50             50        3.4ms         0.28ms
	//	 200            200        13.4ms        0.91ms
	//
	// Linear in rows times the whole catalog, against one assembly per
	// load now. For scale, the same 200-row document where declaredHome
	// short-circuits on ParsedBy — and so never asks — is 0.6ms.
	// Raised in review of #486.
	if !withIncludes && ctx.catalogNoIncludes != nil && *ctx.catalogNoIncludes != nil {
		return *ctx.catalogNoIncludes
	}
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
		if d == nil {
			// A nil def declares nothing to enumerate, and specAs on it
			// is a nil dereference — measured, Catalog() panicked on
			// Context{Elements: {"Leafy": nil}}. This is a read-only
			// enumeration a designer calls without loading anything, so
			// it skips rather than reporting; Context.spec is where the
			// same shape turns into a named load error. Raised in
			// review of #486.
			continue
		}
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
	if withIncludes {
		out = append(out, ctx.includeElements(seen)...)
	}
	// After every source has contributed, so a host's restricted
	// container marks its own pseudo-children too.
	markNested(out)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if !withIncludes && ctx.catalogNoIncludes != nil {
		*ctx.catalogNoIncludes = out
	}
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
