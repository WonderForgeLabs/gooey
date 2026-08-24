package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
)

// ElementDef.Icon is a NAME, and these are the pins for the two halves
// of that decision: that every element in the vocabulary states one, and
// that the name survives into the catalog for a registered element as
// well as a builtin.
//
// Why a name and not a picture is argued at ElementDef.Icon. The short
// version is that rasterizing an SVG needs the nested imagefmt/svg
// module, and a field carrying an image would drag a vector renderer
// into every package that imports markup. There is no test for that
// here because there cannot be a useful one: the check is the root
// module's own dependency list, which CLAUDE.md's `go list -deps ./...`
// answers, and which would fail to COMPILE rather than fail a test.

func TestEveryBuiltinElementDeclaresAnIcon(t *testing.T) {
	// The over-declaration direction is not checkable from here — only a
	// human looking at a codicon can say whether "table" suits <Grid> —
	// so this pins the direction that IS mechanical: nobody adds an
	// element and leaves the toolbox with a blank row for it.
	//
	// Empty is a legal value of the field (a registered element is
	// entitled to decline), which is exactly why the builtins need a
	// test: the type system cannot tell "declined" from "forgotten".
	var missing []string
	for _, d := range definedElements() {
		if d.Icon == "" {
			missing = append(missing, d.Name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("elements declaring no Icon: %s\n"+
			"every builtin must name one — see ElementDef.Icon for what the name means",
			strings.Join(missing, ", "))
	}
}

func TestDeclaredIconNamesAreBareNames(t *testing.T) {
	// The whole contract of the field is that it is a NAME: no
	// directory, no extension, no colour. A consumer appends its own
	// extension and resolves against its own asset directory, which is
	// what lets an app swap icon vendors without touching this package.
	// A name carrying ".svg" would work in exactly one consumer and
	// break the next one, silently, by producing "x.svg.svg".
	for _, d := range definedElements() {
		switch {
		case strings.ContainsAny(d.Icon, "/\\"):
			t.Errorf("<%s> Icon %q contains a path separator; the host owns the directory", d.Name, d.Icon)
		case strings.Contains(d.Icon, "."):
			t.Errorf("<%s> Icon %q carries an extension; the host owns the extension", d.Name, d.Icon)
		case strings.TrimSpace(d.Icon) != d.Icon:
			t.Errorf("<%s> Icon %q has surrounding space", d.Name, d.Icon)
		}
	}
}

func TestCatalogCarriesTheDeclaredIcon(t *testing.T) {
	// Both directions in one test, because the interesting claim is that
	// they behave the SAME. A registered element with a declaration is
	// exactly as describable as a builtin — that is the whole point of
	// the Context.Elements seam — so its icon has to travel too, or a
	// palette would silently show icons for gooey's own elements and
	// blanks for the host's.
	ctx := &Context{
		Elements: map[string]*ElementDef{
			"Widget": {
				Name:     "Widget",
				Proto:    &components.Text{},
				Known:    true,
				Icon:     "sparkle",
				Children: ChildSpec{Mode: ModeLeaf},
				Build: func(e Element, ctx *Context) (gooey.Component, error) {
					return &components.Text{Content: components.Str("w")}, nil
				},
			},
		},
	}
	byName := map[string]ElementSpec{}
	for _, e := range ctx.Catalog() {
		byName[e.Name] = e
	}

	reg, ok := byName["Widget"]
	if !ok {
		t.Fatal("the registered element is missing from the catalog")
	}
	if reg.Origin != OriginRegistered {
		t.Fatalf("Widget Origin = %q, want %q", reg.Origin, OriginRegistered)
	}
	if reg.Icon != "sparkle" {
		t.Fatalf("registered element's Icon = %q, want %q", reg.Icon, "sparkle")
	}

	// A builtin, read off the same catalog. The literal name is written
	// out rather than compared against elementDefs, because comparing
	// the table to itself would pass with specAs dropping the field
	// entirely — both sides would be empty.
	btn, ok := byName["Button"]
	if !ok {
		t.Fatal("<Button> is missing from the catalog")
	}
	if btn.Icon != "primitive-square" {
		t.Fatalf("<Button> Icon = %q, want %q", btn.Icon, "primitive-square")
	}
}

func TestAnElementMayDeclineAnIcon(t *testing.T) {
	// The absence has to survive as an absence. A palette must render
	// "this element declares no icon" differently from any icon at all,
	// which is the same honesty rule AttrsKnown carries — so a helpful
	// default substituted anywhere between the declaration and the
	// catalog would be a bug, not a courtesy.
	ctx := &Context{
		Components: map[string]Builder{
			"Opaque": func(e Element, ctx *Context) (gooey.Component, error) {
				return &components.Text{Content: components.Str("o")}, nil
			},
		},
	}
	for _, e := range ctx.Catalog() {
		if e.Name != "Opaque" {
			continue
		}
		if e.Icon != "" {
			t.Fatalf("a Builder registration carries no declaration, so its Icon must be empty; got %q", e.Icon)
		}
		return
	}
	t.Fatal("the registered builder is missing from the catalog")
}
