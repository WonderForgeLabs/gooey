package markup

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// AttrSpec.Default claims something checkable — "writing this is the same
// as writing nothing" — so it is checked, by rendering.
//
// The check is two tests and it needs both. The first requires omission
// and the declared default to produce identical cells. On its own that
// test passes trivially for any attribute with no visible effect, which
// is the over-declaration direction: silent, and the exact failure mode
// the whole catalog exists to delete. The second test closes it by
// requiring some OTHER legal value to produce different cells — proving
// the first test could have failed. An attribute that cannot be
// discriminated must not declare a Default at all.
//
// This is stronger than the guard the attribute vocabulary settled for.
// Setting a declared attribute to an absurd value and requiring an error
// reaches 59% of rows, because KindString, KindStyle, KindText and
// KindIdentity accept anything. Rendering tests EFFECT rather than
// acceptance, so it is not capped that way — at the cost of only
// covering effects visible in a static frame, which is why durations,
// commands and runtime bindings deliberately declare no Default.

const (
	defaultsCols = 40
	defaultsRows = 12
)

// defaultsContext is the binding environment every probe builds against.
// The handles are shared between the two builds of a comparison on
// purpose: the point is to vary one attribute, so every other input has
// to be identical, and identical handles are the strongest form of that.
func defaultsContext() *Context {
	return &Context{
		Values: map[string]any{
			"S": prop.NewSource("sample"),
			// A SECOND string handle, distinct from S. probePrereqs
			// needs one: <Frozen> refuses Allow and AllowError resolving
			// to the same property, so a prerequisite seeded from the
			// same source as the attribute under test is rejected by the
			// guard after the one being probed.
			"S2":  prop.NewSource("other"),
			"I":   prop.NewSource(1),
			"B":   prop.NewSource(true),
			"F64": prop.NewSource([]float64{1, 4, 2, 5, 3}),
			"SS":  prop.NewSource([]string{"one", "two"}),
			"C":   prop.NewSource(render.RGB(120, 200, 140)),
			"IS": prop.NewSource(components.ItemsOf([]string{"alpha", "beta"},
				func(s string) map[string]any { return map[string]any{"Label": s} })),
			// A percentage, distinct from the index handle above. Seeding
			// a Gauge or a ProgressBar with 1 puts it at the bottom of the
			// good/warn/crit ramp, where Thresholds="true" and the plain
			// style paint the same colour — a probe that cannot see the
			// attribute it is probing.
			"Pct":  prop.NewSource(85),
			"Noop": gooey.Command(func() {}),
			"Sty":  prop.NewSource(render.Style{Fg: render.RGB(10, 20, 30)}),
			"Img":  prop.NewSource[image.Image](image.NewRGBA(image.Rect(0, 0, 2, 2))),
		},
		// A DISPATCHER, because the probe environment has to be able to
		// build everything the vocabulary declares. <Frozen AllowError>
		// refuses to load without one ("the failure is published from an
		// invalidation, and a Set from inside one would mutate the graph
		// mid-invalidation"), so an attribute the catalog says is
		// bindable was unbindable in every generic probe — the sweep
		// reporting it as UNVERIFIED rather than as a pass is the whole
		// point of that distinction. It is never drained here: nothing
		// in a load-time probe posts, and a Dispatcher that is only
		// constructed starts no goroutine.
		Dispatcher: gooey.NewDispatcher(),
		Styles:     map[string]render.Style{"probe": {Fg: render.RGB(200, 40, 40)}},
		// A REGISTERED HANDLER. Every KindCommand attribute in the
		// vocabulary — eleven of them — was probed with "x", which
		// Context.Command refuses with "no handler \"x\" registered"
		// (usercontrol.go:338). That is the context being empty, not the
		// declaration being wrong, so all eleven landed in the sweep's
		// unverified bucket and no arm ever saw whether they take a
		// literal. Raised in review of #470.
		Handlers: map[string]gooey.Action{"probe": gooey.Command(func() {})},
		// PRESENT, AND NOT EMPTY. <FileWatcher> refuses to build without
		// an FS at all, so its three declarations came back UNVERIFIED in
		// every sweep arm — refused, but by a message about the context
		// rather than about the attribute under test.
		//
		// Empty was enough for that and not for <Image Src>, the one
		// literal in the vocabulary that has to name something in this
		// FS: an empty FS refused it with "file does not exist", the
		// context again. The bytes are encoded here rather than checked
		// in under testdata because buildImage reads through
		// Context.Includes, so they belong beside the FS they are served
		// from. Both raised in review of #470.
		Includes: fstest.MapFS{"probe.png": &fstest.MapFile{Data: probePNG()}},
	}
}

// probePNG is a 1x1 image encoded as PNG, for the one attribute whose
// literal must be a decodable file rather than merely a path.
func probePNG() []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// bindingFor is the placeholder binding for a required attribute of a
// declared Go type. An unknown type is a hard failure rather than a skip:
// a new required attribute that this cannot seed would otherwise silently
// drop its element out of the drift check.
func bindingFor(t *testing.T, a AttrSpec) string {
	t.Helper()
	if a.Name == "Value" && a.GoType == "int" {
		// Value is a percentage on every element that declares it;
		// Selected, the other required int, is an index.
		return "{{.Pct}}"
	}
	switch a.GoType {
	case "string":
		return "{{.S}}"
	case "int":
		return "{{.I}}"
	case "bool":
		return "{{.B}}"
	case "[]float64":
		return "{{.F64}}"
	case "[]string":
		return "{{.SS}}"
	case "render.Color":
		return "{{.C}}"
	case "components.ItemSource":
		return "{{.IS}}"
	case "image.Image":
		// <Image Src> is REQUIRED and binding-only, so probeElement has
		// to seed it before any of Image's other attributes can be
		// probed at all. Until this arm existed the Fatalf below fired
		// and took the whole run with it — which is what that message
		// asks for, and it went unanswered because nothing had reason
		// to probe <Image> generically.
		//
		// TWO SWEEPS ARRIVED HERE INDEPENDENTLY, #314's Binds sweep and
		// #460's, and each wrote this arm on its own branch. That they
		// needed the same placeholder for the same reason is the
		// argument for the arm, not a duplication to pick a winner
		// from: an element that drops out of a generic probe drops out
		// of every one.
		return "{{.Img}}"
	}
	t.Fatalf("no placeholder for required attribute %s of type %q — "+
		"add one, or the element silently stops being checked", a.Name, a.GoType)
	return ""
}

// attrSpec finds a declared attribute by name. It exists so probePrereqs
// is checked against the declaration rather than trusted: a row naming an
// attribute the element does not declare is a stale table, and a stale
// table here reads as coverage.
func attrSpec(def *ElementDef, name string) (AttrSpec, bool) {
	for _, a := range def.Attrs {
		if a.Name == name {
			return a, true
		}
	}
	return AttrSpec{}, false
}

// literalFor is the placeholder literal for a required non-binding
// attribute.
func literalFor(a AttrSpec) string {
	switch a.Kind {
	case KindDuration:
		return "50ms"
	case KindInt:
		return "1"
	case KindBool:
		return "true"
	case KindGesture:
		return "ctrl+p"
	case KindEnum:
		if len(a.Enum) > 0 {
			return a.Enum[0]
		}
	}
	return "x"
}

// probePrereqs names attributes that a probe of ANOTHER attribute cannot
// reach without, keyed "Element.Attribute" and valued with the
// attributes to seed as BINDINGS.
//
// Required is the declaration for "this element does not build without
// it", and it is per element, not per probe. A prerequisite here is the
// narrower thing Required cannot say: <Frozen> builds perfectly well
// with neither Allow nor AllowError, but AllowError alone is refused —
// "without a BOUND Allow: the only failure it can report is an
// unparseable set" (elements.go) — and that guard runs BEFORE the
// bind-only check, so <Frozen AllowError="x"> never reaches the rule the
// sweep is asking about and comes back UNVERIFIED.
//
// Kept as a table with a reason per row rather than a rule, because
// there is no rule: this is one element's ordering of its own two
// guards. It is the same class as the Name seeding below — a
// requirement with nowhere in the vocabulary to be written — and it is
// deliberately awkward to add to, so that the next entry has to argue
// for itself. Found by merging #459 into #314: the sweep is what
// reported it, which is the sweep working.
//
// THE ROW WRITES THE BINDING, rather than asking bindingFor for one, and
// DISTINCTNESS IS WHY. Seeding Allow from the same handle the probe
// binds AllowError to is refused by <Frozen>'s next guard — "one
// property cannot be both the allow set and the place its parse failure
// is reported" — so a prerequisite derived from the attribute's type
// would trade one unverified row for another. Measured: bindingFor gave
// both `{{.S}}` and the sweep went red on the aliasing check. S2 exists
// for this.
var probePrereqs = map[string]map[string]string{
	"Frozen.AllowError": {"Allow": "{{.S2}}"},
}

// probeElement writes the element under test with every required
// attribute, slot and child seeded, plus the one attribute the probe is
// varying. An empty value omits the attribute, which is the whole point
// of the omission side of the comparison.
func probeElement(t *testing.T, def *ElementDef, attr, value string) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "<%s", def.Name)
	// EVERY PROBE NAMES ITSELF, and it is not decoration.
	//
	// Name is a UNIVERSAL attribute and is not declared Required, because
	// for almost every element it is not — but <Companion> refuses to
	// build without one ("it is what errors call the service"). Required
	// is declared per element and Name lives in the universal table, so
	// there is nowhere for that requirement to be written today, and the
	// loop below — which reads def.Attrs — could not learn it. Every
	// <Companion> probe therefore failed on the missing Name and came
	// back UNVERIFIED in all five sweep arms, including the one whose own
	// comment argues from <Companion CleanEnv> being a security switch.
	//
	// A name is harmless everywhere else: it is KindIdentity, it renders
	// nothing, and each probe is its own single-element document, so
	// there is no uniqueness to collide with. Skipped only when Name is
	// the attribute under test. Raised in review of #470.
	if attr != "Name" {
		b.WriteString(` Name="probe"`)
	}
	for _, a := range def.Attrs {
		if !a.Required || a.Name == attr {
			continue
		}
		if a.Binds == BindsLiteral {
			// THE SEED FIRST. literalFor answers by KIND, which is a
			// shape and not a value: "x" is a fine string for a Label and
			// useless for a <Companion Path>, which has to resolve to an
			// executable. The element's own Seed is markup that loads by
			// construction, so where it states a value for a required
			// attribute that value is the one to write. Declaring a
			// Default instead is not available — a declared default is
			// checked by RENDERING it, which a non-visual element cannot
			// do. Raised in review of #470.
			v := seedValue(def.Seed, a.Name)
			if v == "" {
				v = literalFor(a)
			}
			fmt.Fprintf(&b, " %s=%q", a.Name, v)
			continue
		}
		fmt.Fprintf(&b, " %s=%q", a.Name, bindingFor(t, a))
	}
	for name, expr := range probePrereqs[def.Name+"."+attr] {
		a, ok := attrSpec(def, name)
		if !ok {
			t.Fatalf("probePrereqs names <%s %s>, which %s does not declare",
				def.Name, name, def.Name)
		}
		if a.Required {
			continue // already seeded by the loop above
		}
		fmt.Fprintf(&b, " %s=%q", a.Name, expr)
	}
	if value != "" {
		fmt.Fprintf(&b, " %s=%q", attr, value)
	}
	b.WriteString(">")
	for _, s := range def.Slots {
		if !s.Required {
			continue
		}
		fmt.Fprintf(&b, "<%s.%s><Text>{{.Label}}</Text></%s.%s>",
			def.Name, s.Name, def.Name, s.Name)
	}
	switch def.Children.Mode {
	case ModeLeaf:
		// A leaf's body is its content, and an empty <Text/> paints
		// nothing at any alignment, size or visibility. An empty probe
		// made seven universal attributes look like they had no effect.
		b.WriteString("ab")
	case ModeOne:
		b.WriteString("<Text>one</Text>")
	case ModeMany:
		// TWO children of DIFFERENT widths. One child makes Gap
		// unobservable; two equal-width children make Uniform
		// unobservable.
		b.WriteString("<Text>one</Text><Text>seven</Text>")
	case ModeRestricted:
		// ONLY WHEN THE ELEMENT DECLARES NO SLOTS, and the exclusion is
		// the fix for a blind spot rather than a shortcut.
		//
		// <Companion> is ModeRestricted over {Arg, Var} AND declares the
		// slots <Companion.Args> and <Companion.Env>; buildCompanion
		// requires the children to be inside them. Writing them as
		// DIRECT children made every <Companion> probe fail with a
		// message about children, so <Companion CleanEnv> came back
		// UNVERIFIED in all five sweep arms — including the one whose own
		// comment argues from CleanEnv being a security switch. Measured
		// in review of #470.
		//
		// The harness cannot place them correctly either, and that is a
		// fact about the catalog rather than a limitation here: NOTHING
		// DECLARES WHICH SLOT HOSTS WHICH CHILD. <Arg> goes in Args and
		// <Var> in Env because companion.go says so in Go. So the probe
		// omits them — legal, since neither slot is Required — and the
		// element builds, which is what the attribute sweeps need.
		if len(def.Slots) == 0 {
			// THE ELEMENT'S OWN SEED SUPPLIES THE CHILDREN, and
			// fabricating them is what this replaced.
			//
			// The old line wrote `<%s Header="h">` for every name in
			// Only. `Header` is <Tab>'s required attribute; <Menu> needs
			// a Title, so every <MenuBar> probe failed with "<Menu>
			// needs a Title" — the harness's own fabricated child
			// refusing, in an arm about MenuBar's attributes.
			//
			// A restricted child cannot be built the way the parent is,
			// either: <Menu> and <MenuItem> have no ElementDef at all
			// (markup.go:1112 reads them in MenuBar's builder), so there
			// is no declaration to seed from. The Seed is markup that
			// loads by construction and states the children the element
			// actually wants — the same argument seedValue makes for
			// required attributes, one level down. Raised in review of
			// #470.
			b.WriteString(seedChildren(t, def))
		}
	}
	fmt.Fprintf(&b, "</%s>", def.Name)
	return b.String()
}

// seedChildren is the body of an element's Seed — everything between its
// root open and close tags.
//
// EMPTY IS A FAILURE, not a skip: an element that restricts its children
// and whose seed shows none would silently probe as childless, and a
// builder that needs one would refuse for that reason in every arm.
func seedChildren(t *testing.T, def *ElementDef) string {
	t.Helper()
	open := "<" + def.Name
	close := "</" + def.Name + ">"
	i := strings.Index(def.Seed, open)
	j := strings.Index(def.Seed, ">")
	k := strings.LastIndex(def.Seed, close)
	if i != 0 || j < 0 || k < j {
		t.Fatalf("<%s> restricts its children to %v and its Seed %q has no body to "+
			"take them from", def.Name, def.Children.Only, def.Seed)
	}
	body := def.Seed[j+1 : k]
	if strings.TrimSpace(body) == "" {
		t.Fatalf("<%s> restricts its children to %v and its Seed %q shows none, so "+
			"every probe of its attributes builds a childless element",
			def.Name, def.Children.Only, def.Seed)
	}
	return body
}

// seedValue is the literal an element's own Seed writes for attr, or ""
// when the seed does not mention it or writes a binding there.
//
// A regexp over the seed rather than a parse: the seed is one element
// with quoted attributes, the test only needs a literal, and reaching for
// the document parser here would make the harness depend on the thing it
// exists to probe.
func seedValue(seed, attr string) string {
	m := regexp.MustCompile(`\b` + regexp.QuoteMeta(attr) + `="([^"]*)"`).FindStringSubmatch(seed)
	if len(m) != 2 || strings.Contains(m[1], "{{") {
		return ""
	}
	return m[1]
}

// renderProbe builds src and composes it into a fixed rect, returning the
// cell plane. Compose is the one-shot path, so nothing is started and
// nothing ticks: two calls with the same input give the same cells.
func renderProbe(t *testing.T, ctx *Context, src string) *render.Buffer {
	t.Helper()
	w, err := Build([]byte("<Gooey>"+src+"</Gooey>"), ctx)
	if err != nil {
		t.Fatalf("build %s: %v", src, err)
	}
	f := gooey.Compose(w, term.Caps{Cols: defaultsCols, Rows: defaultsRows}, nil)
	return f.Cells
}

// cellsDiffer reports the first differing cell, for a message that names
// a coordinate instead of dumping two screens.
func cellsDiffer(a, b *render.Buffer) (int, int, bool) {
	for y := 0; y < defaultsRows; y++ {
		for x := 0; x < defaultsCols; x++ {
			if a.At(x, y) != b.At(x, y) {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// harnessFor wraps the element under test in a parent that gives the
// attribute meaning. Attached properties are the reason this is not one
// string: Grid.Row means nothing outside a <Grid>, and an explicit Width
// means nothing to an element that is the only thing in its slot.
func harnessFor(attr, el string) string {
	switch {
	case strings.HasPrefix(el, "<Validate"):
		// A HOST, NOT A CONTAINER. <Validate> is an attachment whose
		// builder is the input's: attachAll refuses one nobody wired,
		// with "does not support <Validate>; it belongs on an input
		// element with a bound text source". Inside the default <HStack>
		// harness EVERY probe of a Validate attribute therefore failed
		// for that reason, and an arm counting "the build failed" as
		// "the rule refused" counted seven vacuous checks — which is
		// exactly the defect bindSweep's refusal-text discriminator
		// exists to make visible. TextBox is the element whose builder
		// calls wireValidate (elements.go). Raised in review of #470.
		return `<TextBox Text="{{.S}}">` + el + `</TextBox>`
	case strings.HasPrefix(el, "<TypeAhead"):
		// THE SAME GAP ONE ATTACHMENT OVER. <TypeAhead> belongs on an
		// <ItemsView> — attachAll refuses one anywhere else with "does
		// not support <TypeAhead>; it belongs on an <ItemsView>" — so
		// inside the default harness every probe of a TypeAhead
		// attribute failed for the host's reason and not the rule's.
		// Found by the KindDuration sweep, which is the first arm to
		// reach this element at all. Raised in review of #470, the
		// second time.
		return `<ItemsView Items="{{.IS}}">` + el +
			`<ItemsView.ItemTemplate><Text>{{.Label}}</Text></ItemsView.ItemTemplate></ItemsView>`
	case strings.HasPrefix(attr, "Grid."):
		return `<Grid Rows="1*,1*" Cols="1*,1*">` + el + `<Text Grid.Row="1" Grid.Col="1">z</Text></Grid>`
	case strings.HasPrefix(attr, "Canvas."):
		return `<Canvas>` + el + `<Text Canvas.Left="8" Canvas.Top="3">z</Text></Canvas>`
	case attr == "HAlign" || attr == "VAlign":
		// Alignment needs a slot BIGGER than the content. Inside a stack
		// a child gets its measured size and there is nothing to align
		// within, so both values render the same and the probe proves
		// nothing.
		return `<Grid Rows="1*,1*" Cols="1*,1*">` + el + `</Grid>`
	case attr == "Height":
		// A vertical sibling is what an explicit height displaces.
		return `<Grid Rows="1*,1*" Cols="1*,1*"><VStack Grid.Row="0" Grid.Col="0">` +
			el + `<Text>z</Text></VStack></Grid>`
	}
	// A horizontal sibling after the element under test is what makes
	// Width, Margin and every element-own size attribute observable:
	// they move where the next thing lands.
	return `<Grid Rows="1*,1*" Cols="1*,1*"><HStack Grid.Row="0" Grid.Col="0">` +
		el + `<Text>z</Text></HStack></Grid>`
}

// defaultProbes is every declared Default in the vocabulary, paired with
// the harness it has to be judged in.
type defaultProbe struct {
	owner string // element name, or "" for the universal/attached tables
	attr  AttrSpec
	def   *ElementDef
}

func declaredDefaults(t *testing.T) []defaultProbe {
	t.Helper()
	var out []defaultProbe
	for _, def := range definedElements() {
		spec := def.spec()
		for _, a := range def.Attrs {
			if a.Default == "" {
				continue
			}
			if !TakesLayout(spec) {
				// A non-visual element paints nothing, so neither test
				// below can say anything about it. Declaring a Default
				// there would be unguarded by construction.
				t.Errorf("<%s %s> declares Default=%q but the element is "+
					"non-visual: nothing can check it", def.Name, a.Name, a.Default)
				continue
			}
			out = append(out, defaultProbe{owner: def.Name, attr: a, def: def})
		}
	}
	// The universal and attached tables belong to no element, so they are
	// probed once through a representative one — and it has to be a
	// <Border>, not a <Text>.
	//
	// A Text paints at the left edge of whatever bounds it is given, so
	// a bounds change that does not move that edge is invisible to it:
	// Grid.ColSpan="2" widens the slot and the text stays put, and the
	// probe reports "no effect" for an attribute that plainly has one. A
	// Border draws its own edges AT its bounds, so every change to the
	// rect it was arranged into shows up.
	box := elementDefs["Border"]
	for _, a := range universalAttrs {
		if a.Default != "" {
			out = append(out, defaultProbe{attr: a, def: box})
		}
	}
	for _, parent := range AttachedParents() {
		for _, a := range AttachedAttrs(parent) {
			if a.Default != "" {
				out = append(out, defaultProbe{attr: a, def: box})
			}
		}
	}
	return out
}

func (p defaultProbe) name() string {
	if p.owner == "" {
		return p.attr.Name
	}
	return p.owner + "." + p.attr.Name
}

func TestDeclaredDefaultsRenderIdenticallyToOmission(t *testing.T) {
	probes := declaredDefaults(t)
	if len(probes) == 0 {
		t.Fatal("no declared defaults: this test would pass vacuously")
	}
	for _, p := range probes {
		t.Run(p.name(), func(t *testing.T) {
			ctx := defaultsContext()
			absent := renderProbe(t, ctx, harnessFor(p.attr.Name,
				probeElement(t, p.def, p.attr.Name, "")))
			explicit := renderProbe(t, ctx, harnessFor(p.attr.Name,
				probeElement(t, p.def, p.attr.Name, p.attr.Default)))
			if x, y, differs := cellsDiffer(absent, explicit); differs {
				t.Fatalf("%s=%q is declared the default but does not render like "+
					"omission: first difference at (%d,%d)",
					p.attr.Name, p.attr.Default, x, y)
			}
		})
	}
}

// otherValue is a legal value for a that is NOT its declared default. It
// is what makes the identity test able to fail.
func otherValue(a AttrSpec) string {
	switch a.Kind {
	case KindEnum:
		for _, v := range a.Enum {
			if v != a.Default {
				return v
			}
		}
	case KindBool:
		if a.Default == "false" {
			return "true"
		}
		return "false"
	case KindInt:
		// An attached property has to stay inside the harness it is
		// judged in: Grid.Row="7" in a two-row grid is outside the grid,
		// which is a different statement from "row 1 differs from row 0".
		switch a.Name {
		case "Grid.Row", "Grid.Col":
			return "1"
		case "Grid.RowSpan", "Grid.ColSpan":
			return "2"
		case "Canvas.Left", "Canvas.Top":
			return "3"
		}
		if a.Default != "7" {
			return "7"
		}
		return "3"
	case KindString:
		// Margin is the only KindString carrying a Default; a thickness
		// of 2 moves everything it touches.
		if a.Default != "2" {
			return "2"
		}
		return "3"
	}
	return ""
}

func TestDeclaredDefaultsAreDiscriminating(t *testing.T) {
	for _, p := range declaredDefaults(t) {
		t.Run(p.name(), func(t *testing.T) {
			other := otherValue(p.attr)
			if other == "" {
				t.Fatalf("%s declares Default=%q but no other legal value can be "+
					"generated for a %s — nothing can prove the identity test could "+
					"fail, so the declaration is unguarded and must be dropped",
					p.attr.Name, p.attr.Default, p.attr.Kind)
			}
			ctx := defaultsContext()
			def := renderProbe(t, ctx, harnessFor(p.attr.Name,
				probeElement(t, p.def, p.attr.Name, p.attr.Default)))
			alt := renderProbe(t, ctx, harnessFor(p.attr.Name,
				probeElement(t, p.def, p.attr.Name, other)))
			if _, _, differs := cellsDiffer(def, alt); !differs {
				t.Fatalf("%s renders identically at %q and %q, so "+
					"TestDeclaredDefaultsRenderIdenticallyToOmission cannot fail for "+
					"this row: either the probe does not exercise the attribute, or "+
					"it has no static effect and must not declare a Default",
					p.attr.Name, p.attr.Default, other)
			}
		})
	}
}

// TestEveryDeclaredDefaultIsCategorised is the cheap half: a grid groups
// by CategoryOf, and a row that derives to Common when it is really
// Layout is a presentation bug nobody would notice.
func TestCategoryOfDerivesAndOverrides(t *testing.T) {
	for _, a := range universalAttrs {
		want := CategoryLayout
		switch a.Name {
		case "Name":
			want = CategoryDesign
		case "Tooltip":
			want = CategoryCommon
		}
		if got := CategoryOf(a); got != want {
			t.Errorf("CategoryOf(%s) = %q, want %q", a.Name, got, want)
		}
	}
	for _, parent := range AttachedParents() {
		for _, a := range AttachedAttrs(parent) {
			if got := CategoryOf(a); got != CategoryLayout {
				t.Errorf("CategoryOf(%s) = %q, want %q", a.Name, got, CategoryLayout)
			}
		}
	}
	derived := map[Kind]string{
		KindCommand:  CategoryEvents,
		KindStyle:    CategoryAppearance,
		KindColor:    CategoryAppearance,
		KindIdentity: CategoryDesign,
		KindText:     CategoryCommon,
		KindInt:      CategoryCommon,
	}
	for kind, want := range derived {
		if got := CategoryOf(AttrSpec{Kind: kind}); got != want {
			t.Errorf("CategoryOf(Kind %s) = %q, want %q", kind, got, want)
		}
	}
	// The declared field overrides the derivation, which is the only
	// reason it exists.
	if got := CategoryOf(AttrSpec{Kind: KindCommand, Category: CategoryLayout}); got != CategoryLayout {
		t.Errorf("declared Category did not override: got %q", got)
	}
}
