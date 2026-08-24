package markup

import (
	"fmt"
	"image"
	"image/color"
	"sort"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Seeding a new element.
//
// A palette that adds <VStack> to a canvas has to decide what the new one
// looks like, and until this file existed the answer was three tables in
// the EDITOR, none of them per element:
//
//   - a per-Kind literal guess, which is where "x" came from as the value
//     of every required string attribute;
//   - a per-GoType placeholder switch, which had no arm for image.Image,
//     so <Image> could not be added at all;
//   - a per-ChildMode child, which hardcoded Header="tab" because it was
//     written for <Tabs> — so <MenuBar>'s <Menu>, which wants Title, would
//     not load either.
//
// Measured on the palette of 28: two elements that did not load, four
// that measured 0x0 — invisible on the canvas and, because hitTest never
// returns a zero-size component, unselectable in order to be fixed.
//
// The answer is per element and it is markup, because the answer has to
// cover more than attributes: an empty <VStack> measures nothing whatever
// its attributes say. See ElementDef.Seed for the contract.
//
// What markup CANNOT carry is a live handle. A bind-only attribute
// (<Checkbox Checked>, <Gauge Value>) takes {{.Path}} and nothing else,
// and ten of the fourteen required attributes in the vocabulary are that
// shape — so a seed names the binding and the inserter registers the
// value. Seeded does both halves, and it is the only place that knows the
// naming convention, so an editor and this package's own tests cannot
// disagree about it.

// PlaceholderFor returns a zero-ish source property for a declared
// GoType, for seeding a binding nobody has wired yet.
//
// The TABLE is the type check — keyed by the declared GoType spelling,
// the same string-not-reflection mechanism Bound uses, and the reason
// none of this needs reflection. It lives here rather than in a
// palette because AttrSpec.GoType is declared here: a type this cannot
// answer for is a gap in the vocabulary, and
// TestEverySeededElementLoadsAndOccupiesSpace fails on it. In the editor
// that same gap was a status line reading "no placeholder for
// image.Image" beside an add that silently did nothing.
//
// The values are not all zero. A Sparkline bound to an empty slice and a
// Segmented bound to no options are both technically seeded and both
// render as nothing, which is the failure this exists to prevent — so
// collection types get a couple of entries and Color gets a visible one.
// Nil for a type with no placeholder, which callers must treat as an
// error rather than as an empty binding.
func PlaceholderFor(goType string) any {
	if f, ok := placeholders[goType]; ok {
		return f()
	}
	return nil
}

// placeholders is the ONE table. PlaceholderFor looks up in it and
// PlaceholderTypes returns its keys, so the two cannot disagree — a
// switch beside a hand-written list of the same type names is a mirror
// nothing pins, and the drift is silent: the only symptom is the error
// at the bottom of `placeholder` naming fewer types than exist, sending
// an author to write a literal for something already supported.
//
// FACTORIES, not values. Each call must hand back a FRESH source: two
// seeded elements sharing one property is the same defect the
// {{.Name_Attr}} rename exists to prevent, and it is silent in the same
// way, because both documents load and one checkbox simply ticks the
// other.
var placeholders = map[string]func() any{
	"string":  func() any { return prop.NewSource("") },
	"int":     func() any { return prop.NewSource(0) },
	"bool":    func() any { return prop.NewSource(false) },
	"float64": func() any { return prop.NewSource(0.0) },
	// Collection types get a couple of entries and Color a visible one:
	// a Sparkline bound to an empty slice and a Segmented bound to no
	// options are both technically seeded and both render as nothing,
	// which is the failure this exists to prevent.
	"[]float64":    func() any { return prop.NewSource([]float64{1, 2, 3, 2}) },
	"[]string":     func() any { return prop.NewSource([]string{"one", "two"}) },
	"render.Color": func() any { return prop.NewSource(render.RGB(120, 200, 140)) },
	// A real image, because components.Image scales its source and a nil
	// one is a nil dereference during paint rather than a blank
	// rectangle. Small and opaque: it exists to occupy the element's
	// cells, not to be looked at.
	"image.Image": func() any {
		img := image.NewRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				img.Set(x, y, color.RGBA{R: 120, G: 200, B: 140, A: 255})
			}
		}
		return prop.NewSource[image.Image](img)
	},
	"components.ItemSource": func() any {
		return prop.NewSource(components.ItemsOf([]string{"one", "two"},
			func(s string) map[string]any { return map[string]any{"Label": s} }))
	},
}

// PlaceholderTypes lists the GoTypes PlaceholderFor answers for, sorted.
// For a test that wants to report the whole gap rather than the first
// one it hits.
//
// Derived from the table rather than written out, so adding a type is
// one edit in one place.
func PlaceholderTypes() []string {
	out := make([]string, 0, len(placeholders))
	for k := range placeholders {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Seeded is the markup a palette should insert for a new instance of
// spec, named name, together with the values its bindings need.
//
// A seed refers to a bind-only attribute by its BARE name — {{.Checked}}
// — and this rewrites that to {{.<name>_Checked}} and registers a
// placeholder under the same key. The rename is what keeps two instances
// from sharing state: without it a second <Checkbox> would tick the first
// one, which is not a subtle bug but is a silent one, because both
// documents load.
//
// Returning the values rather than mutating a Context is deliberate. The
// caller owns registration — it may be adding to a live app through the
// control plane, or building a throwaway context in a test — and a
// function that reached into Context.Values would make those two paths
// different code.
func Seeded(spec ElementSpec, name string) (string, map[string]any, error) {
	if strings.TrimSpace(spec.Seed) == "" {
		return "", nil, fmt.Errorf("markup: <%s> has no Seed, so a palette has nothing to insert for it", spec.Name)
	}
	if name == "" {
		return "", nil, fmt.Errorf("markup: seeding <%s> needs a name to key its bindings by", spec.Name)
	}

	src := spec.Seed
	values := map[string]any{}
	for _, a := range spec.Attrs {
		ref := "{{." + a.Name + "}}"
		if !strings.Contains(src, ref) {
			continue
		}
		h, err := placeholder(spec, a)
		if err != nil {
			return "", nil, err
		}
		if h == nil {
			continue
		}
		key := name + "_" + a.Name
		src = strings.ReplaceAll(src, ref, "{{."+key+"}}")
		values[key] = h
	}
	return src, values, nil
}

// placeholder is the value a seed's {{.Attr}} needs registering under.
// Nil (with no error) for an attribute that needs none.
//
// Two kinds need one, for the same reason by different routes. A
// bind-only attribute takes {{.Path}} and nothing else, so there is no
// literal to write. A COMMAND has no literal worth writing either: a
// non-visual element IS its action — a <KeyBinding> with a gesture and
// no command is a key that does nothing, which is the same defect as a
// <VStack> that measures 0x0. You can add it and it is not there.
//
// The command placeholder is a real gooey.Action rather than an empty
// attribute, so CanExecute is true and the element the palette added is
// LIVE the moment it lands. It does nothing yet, and that is the part
// the user repoints; what it is not is inert.
func placeholder(spec ElementSpec, a AttrSpec) (any, error) {
	if a.Kind == KindCommand {
		return gooey.Command(func() {}), nil
	}
	// Keyed off what the SEED asked for, not off Binds. An attribute
	// that merely MAY bind still needs a value when the seed chose to
	// bind it — <Image Src> is BindsEither and carries an image.Image,
	// so treating "not bind-only" as "needs nothing" would leave
	// {{.Src}} rewritten to nothing and the document unloadable.
	if h := PlaceholderFor(a.GoType); h != nil {
		return h, nil
	}
	return nil, fmt.Errorf("markup: <%s>'s seed binds %s, whose Go type is %q, and nothing can be seeded for that; write a literal in the seed instead, or teach PlaceholderFor (it knows %s)",
		spec.Name, a.Name, a.GoType, strings.Join(PlaceholderTypes(), ", "))
}
