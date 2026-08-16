package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Context.Elements: a host component that DECLARES its surface.
//
// The motivating failure is not hypothetical. The wysiwyg palette seeds
// an inserted element's required attributes from AttrSpec.Required +
// GoType, so clicking a palette entry produces markup that loads. A
// component registered through Context.Components has no Attrs to read,
// so the palette emitted `<ActivityBar Name="ActivityBar1"/>` — and the
// rail requires a bound Sel=, so the insert failed to load with an error
// naming an attribute the user was never offered.
//
// meter is shaped like that component on purpose: one REQUIRED
// binding-only attribute of a known GoType, plus one optional literal.

const meterGoType = "int"

func meterDef() *ElementDef {
	return &ElementDef{
		Name:  "Meter",
		Proto: &components.Text{},
		Known: true,
		Doc:   "A host component that declares its surface.",
		Attrs: []AttrSpec{
			{Name: "Level", Kind: KindBinding, Binds: BindsBinding, GoType: meterGoType, Required: true, Origin: OriginRegistered},
			{Name: "Label", Kind: KindString, Binds: BindsLiteral, Origin: OriginRegistered},
		},
		Children: ChildSpec{Mode: ModeLeaf},
		Build: func(e Element, ctx *Context) (gooey.Component, error) {
			h, err := Bound[int](e, ctx, "Level")
			if err != nil {
				return nil, err
			}
			return &components.Text{Content: components.Str(e.Attrs["Label"])}, meterSink(h)
		},
	}
}

// meterSink keeps the handle live without a component that reads it —
// the tests here are about the SEAM, not about painting a gauge.
func meterSink(*prop.Property[int]) error { return nil }

func meterCtx() *Context {
	return &Context{
		Values:   map[string]any{"N": prop.NewSource(3)},
		Styles:   map[string]render.Style{},
		Elements: map[string]*ElementDef{"Meter": meterDef()},
	}
}

// The dispatch itself: a declared host element builds.
//
// Mutation: delete the ctx.Elements arm at the head of buildComponent and
// this fails with `unknown element <Meter>` — the element falls past the
// builtin registry and the Includes convention to the end.
func TestADeclaredHostElementBuilds(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026"><Meter Level="{{.N}}" Label="cpu"/></Gooey>`
	buildOne(t, src, meterCtx())
}

// THE PAYOFF, and the reason this seam is worth more than the palette
// fix that motivated it.
//
// checkAttrs declines on anything Context.Components registers, because
// "a Builder is a func, not a schema" — so until now a typo on a host
// component was accepted, ignored, and reported nowhere, forever. With a
// declaration in hand the identical typo is a load error, and it gets
// the near-miss suggestion for free.
//
// Mutation: drop the ctx.Elements branch from (*Context).spec and this
// fails — spec returns false, checkAttrs returns nil, and the misspelled
// attribute loads clean.
func TestAnUnknownAttributeOnADeclaredHostElementIsALoadError(t *testing.T) {
	src := `<Gooey xmlns="wonderforge.io/gooey/2026"><Meter Level="{{.N}}" Lable="cpu"/></Gooey>`
	_, err := Build([]byte(src), meterCtx())
	if err == nil {
		t.Fatal("a misspelled attribute on a declared host element loaded clean")
	}
	if !strings.Contains(err.Error(), "no such attribute") {
		t.Errorf("error does not report the attribute as unknown: %v", err)
	}
	// The suggestion is the half that makes the error worth having: the
	// answer is one edit away and the vocabulary was already in hand.
	if !strings.Contains(err.Error(), "Label") {
		t.Errorf("error does not suggest the near miss: %v", err)
	}
}

// The discrimination for the test above. Without it, that one passes
// just as well against a seam that rejects EVERY attribute on a declared
// element — including the ones it declares.
func TestADeclaredHostElementStillAcceptsItsOwnAttributes(t *testing.T) {
	for _, attrs := range []string{
		`Level="{{.N}}"`,
		`Level="{{.N}}" Label="cpu"`,
		// The universal surface still applies: a declared host element
		// with a Layout takes Name and the layout attributes, and
		// AttrsFor joins them in rather than each definition restating
		// them.
		`Level="{{.N}}" Name="g" Margin="1,2" HAlign="Center"`,
	} {
		t.Run(attrs, func(t *testing.T) {
			ctx := meterCtx()
			ctx.Named = map[string]gooey.Component{}
			src := `<Gooey xmlns="wonderforge.io/gooey/2026"><Meter ` + attrs + `/></Gooey>`
			if _, err := Build([]byte(src), ctx); err != nil {
				t.Fatalf("declared attributes were rejected: %v", err)
			}
		})
	}
}

// Catalog describes a declared host element as REGISTERED but KNOWN —
// which is the pair a palette reads. Origin is provenance; AttrsKnown is
// whether Attrs can be trusted, and the catalog's own doc says never to
// branch on the first for the second. This element is the case that
// distinguishes them.
func TestCatalogDescribesADeclaredHostElement(t *testing.T) {
	var got *ElementSpec
	for _, e := range meterCtx().Catalog() {
		if e.Name == "Meter" {
			s := e
			got = &s
		}
	}
	if got == nil {
		t.Fatal("Meter is absent from the catalog")
	}
	if got.Origin != OriginRegistered {
		t.Errorf("Origin = %q, want %q — it is the host's, not this build's", got.Origin, OriginRegistered)
	}
	if !got.AttrsKnown {
		t.Error("AttrsKnown is false; a declared element is exactly as knowable as a builtin one")
	}
	var level *AttrSpec
	for i, a := range got.Attrs {
		if a.Name == "Level" {
			level = &got.Attrs[i]
		}
	}
	if level == nil {
		t.Fatal("the declared Level attribute did not reach the catalog")
	}
	// These three fields are what the palette actually consumes:
	// Required decides whether to seed at all, Binds whether to write a
	// literal or a binding, GoType which handle to create.
	if !level.Required || level.Binds != BindsBinding || level.GoType != meterGoType {
		t.Errorf("Level = {Required:%v Binds:%q GoType:%q}, want {true %q %q}",
			level.Required, level.Binds, level.GoType, BindsBinding, meterGoType)
	}
}

// A host declaration that SHADOWS a built-in must be what the catalog
// describes, because it is what will build.
//
// This test exists because the first draft of Catalog got it wrong, and
// the failure is instructive: it added the built-ins first and skipped
// the collision, so a document containing <Gauge> built the HOST's
// element while the catalog described gooey's — same name, different
// attributes, different Go type. A palette reading that would offer
// attributes the real component rejects, and the load error would name
// an element the author never wrote.
//
// The name is deliberately a real built-in. Picking one accidentally is
// how the collision was found in the first place.
func TestADeclaredHostElementShadowsTheBuiltinInTheCatalog(t *testing.T) {
	const shadowed = "Gauge"
	if _, builtin := elementDefs[shadowed]; !builtin {
		t.Fatalf("%s is no longer a built-in; this test needs a name that collides", shadowed)
	}
	d := meterDef()
	d.Name = shadowed
	ctx := meterCtx()
	ctx.Elements = map[string]*ElementDef{shadowed: d}

	var seen int
	var got ElementSpec
	for _, e := range ctx.Catalog() {
		if e.Name == shadowed {
			seen++
			got = e
		}
	}
	// Exactly one entry: two would make "which does <Gauge> build?"
	// unanswerable from the catalog, which is the question it exists to
	// answer.
	if seen != 1 {
		t.Fatalf("the catalog has %d entries named %s, want exactly 1", seen, shadowed)
	}
	if got.Origin != OriginRegistered {
		t.Errorf("Origin = %q, want %q — the host's declaration is what builds", got.Origin, OriginRegistered)
	}
	for _, a := range got.Attrs {
		if a.Name == "Level" {
			return
		}
	}
	t.Errorf("the catalog describes the built-in's attributes, not the host's: %v", got.Attrs)
}

// The unchanged half, stated so a future change cannot quietly move it:
// a Components registration still contributes a name and nothing else,
// and its attributes are still unchecked. That is CORRECT — a Builder
// really is opaque — and it is the behaviour every host written before
// this seam still relies on.
func TestAComponentsRegistrationIsStillOpaque(t *testing.T) {
	ctx := &Context{
		Values: map[string]any{"N": prop.NewSource(3)},
		Styles: map[string]render.Style{},
		Components: map[string]Builder{
			"Opaque": func(e Element, ctx *Context) (gooey.Component, error) {
				return &components.Text{Content: components.Str("x")}, nil
			},
		},
	}
	for _, e := range ctx.Catalog() {
		if e.Name != "Opaque" {
			continue
		}
		if e.AttrsKnown {
			t.Error("a Builder registration claims its attributes are known")
		}
	}
	// The typo that a declared element now rejects is still accepted
	// here, and silently. This assertion is the CONTRAST that gives
	// TestAnUnknownAttributeOnADeclaredHostElementIsALoadError its
	// meaning — without it, that test could be passing because of some
	// unrelated strictness rather than because of the declaration.
	src := `<Gooey xmlns="wonderforge.io/gooey/2026"><Opaque Lable="cpu"/></Gooey>`
	if _, err := Build([]byte(src), ctx); err != nil {
		t.Fatalf("an opaque builder's attributes became checked: %v", err)
	}
}

// One name, two registrations: refused, not resolved. Which map wins
// would otherwise depend on the order two `if`s happen to be written in,
// and the loser's registration would be dead code nobody could see —
// the same reason registerElements panics on a duplicate builtin.
func TestAnElementRegisteredBothWaysIsALoadError(t *testing.T) {
	ctx := meterCtx()
	ctx.Components = map[string]Builder{
		"Meter": func(e Element, ctx *Context) (gooey.Component, error) {
			return &components.Text{Content: components.Str("shadow")}, nil
		},
	}
	src := `<Gooey xmlns="wonderforge.io/gooey/2026"><Meter Level="{{.N}}"/></Gooey>`
	_, err := Build([]byte(src), ctx)
	if err == nil {
		t.Fatal("an element registered in both maps built silently; one registration is unreachable")
	}
	if !strings.Contains(err.Error(), "both") {
		t.Errorf("error does not say the element is registered twice: %v", err)
	}
}

// ---------------------------------------------------------------------
// ResolveStyle
// ---------------------------------------------------------------------

// ResolveStyle exists because two out-of-package builders index
// Context.Styles directly (apps/wysiwyg/components/panel,
// paint/shapes), and a bare map index yields the ZERO Style on a
// misspelled name: the element loads, paints unstyled, and reports
// nothing. The in-package check could not reach them because it was
// unexported.
func TestResolveStyleReportsAnUnregisteredName(t *testing.T) {
	ctx := &Context{Values: map[string]any{}, Styles: map[string]render.Style{
		"panel": {Fg: render.RGB(1, 2, 3)},
	}}
	e := Element{Name: "Panel", Attrs: map[string]string{"Style": "pannel"}}

	if _, err := ResolveStyle(e, ctx, "Style", "pannel"); err == nil {
		t.Fatal("a misspelled style name resolved silently — the bare-map-index bug")
	}
	// The registered name still resolves, and to the right value: a
	// helper that rejected everything would pass the check above.
	got, err := ResolveStyle(e, ctx, "Style", "panel")
	if err != nil {
		t.Fatalf("a registered style name did not resolve: %v", err)
	}
	if got.Fg != render.RGB(1, 2, 3) {
		t.Errorf("resolved the wrong style: %+v", got)
	}
}

// An omitted attribute is not a typo. Callers pass e.Attrs["Style"]
// straight through, so the empty string reaches here whenever the
// document simply did not set one, and the zero Style is the right
// answer for that. Rejecting it would make Style= mandatory on every
// element that adopts this helper.
func TestResolveStyleAcceptsAnAbsentAttribute(t *testing.T) {
	ctx := &Context{Values: map[string]any{}, Styles: map[string]render.Style{}}
	e := Element{Name: "Panel", Attrs: map[string]string{}}
	got, err := ResolveStyle(e, ctx, "Style", "")
	if err != nil {
		t.Fatalf("an absent Style= was rejected: %v", err)
	}
	if got != (render.Style{}) {
		t.Errorf("an absent Style= resolved to %+v, want the zero Style", got)
	}
}
