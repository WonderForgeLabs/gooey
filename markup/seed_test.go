package markup

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
)

// seedable is every element a palette can insert: the ones that build a
// component of their own. A nil Proto means a pseudo-element parsed by
// its parent — <Tab> — which cannot stand alone and so cannot be seeded.
//
// DERIVED from the registry, never a list. A list here would go stale on
// the next element added, and the way it would go stale is by passing.
func seedable(t *testing.T) []*ElementDef {
	t.Helper()

	var out []*ElementDef
	for _, d := range definedElements() {
		if d.Proto == nil {
			continue
		}
		out = append(out, d)
	}
	if len(out) == 0 {
		t.Fatal("the element registry is empty, so every assertion below would " +
			"be vacuous — the walk is broken, not the vocabulary")
	}
	return out
}

// composeSeed builds one element's seed and returns the component the
// seed produced, composed into a real frame.
//
// A visual seed goes under a <Canvas>, which is the container that grants
// free geometry and therefore arranges a child at the size it measures —
// the same situation a palette drop lands in, and the one where a
// zero-measuring element is invisible. A NON-visual seed is an attachment
// and occupies nothing by design, so it is hosted on a <Button>, the
// built-in that accepts attachments and no visual children.
func composeSeed(t *testing.T, d *ElementDef) (gooey.Component, error) {
	t.Helper()

	spec := d.spec()
	src, values, err := Seeded(spec, spec.Name+"1")
	if err != nil {
		return nil, err
	}
	if spec.NonVisual {
		// An attachment occupies no space, so there are no bounds to
		// return and nothing to reach: that it LOADS onto a legal host
		// is the whole check. assertSeedIsLive asks the other half.
		return nil, loadOnSomeHost(src, values)
	}
	host := `<Canvas>` + src + `</Canvas>`
	ctx := &Context{Values: values, Dispatcher: gooey.NewDispatcher()}
	root, err := Build([]byte(`<Gooey>`+host+`</Gooey>`), ctx)
	if err != nil {
		return nil, err
	}
	c := gooey.NewComposer(root, 80, 24)
	t.Cleanup(c.Close)
	c.Frame()

	holder, ok := root.(gooey.Container)
	if !ok {
		t.Fatalf("the host <%s> is not a Container, so this test cannot reach the seeded element",
			map[bool]string{true: "Button", false: "Canvas"}[spec.NonVisual])
	}
	kids := holder.ChildComponents()
	if len(kids) != 1 {
		// An attachment is not a child, so a non-visual seed legitimately
		// leaves the host with none. Reaching it is not what this test
		// asserts about those — that they LOAD is.
		if spec.NonVisual {
			return nil, nil
		}
		t.Fatalf("<%s>'s seed produced %d child component(s) under its host, want 1",
			spec.Name, len(kids))
	}
	return kids[0], nil
}

// loadOnSomeHost builds an attachment onto whichever legal host accepts
// it, and fails only when NONE will.
//
// One universal host is wrong, and the loader says so in as many words:
// "<Button> does not support <TypeAhead>; it belongs on an <ItemsView>",
// "<Validate> ... belongs on an input element with a bound text source".
// Attachments have host requirements, and they are real constraints the
// palette will eventually have to respect too — an editor that offers
// <TypeAhead> on a <Text> is offering a load error.
//
// Trying candidates rather than declaring a host per element keeps this
// derived: the assertion is "there EXISTS a host this seed loads onto",
// which is the property that matters and cannot go stale against a
// hand-written table.
func loadOnSomeHost(src string, values map[string]any) error {
	hosts := []struct{ open, close string }{
		{`<Button Content="host">`, `</Button>`},
		// The template is not decoration: <ItemsView> refuses to load
		// without one, so a host missing it fails for its OWN reason and
		// would be reported as the attachment's problem.
		{`<ItemsView Items="{{.__items}}"><ItemsView.ItemTemplate><Text>{{.Label}}</Text></ItemsView.ItemTemplate>`, `</ItemsView>`},
		{`<TextBox Text="{{.__text}}">`, `</TextBox>`},
	}
	vals := map[string]any{
		"__items": PlaceholderFor("components.ItemSource"),
		"__text":  PlaceholderFor("string"),
	}
	for k, v := range values {
		vals[k] = v
	}

	var errs []string
	for _, h := range hosts {
		// Includes, and therefore ctx.assets(), is not decoration. A
		// palette always has a file system — markup.Load supplies one —
		// so a harness building from raw bytes is testing a situation no
		// palette occupies. <FileWatcher> is the element that notices:
		// it refuses to load without an FS ON PURPOSE, because a watcher
		// quietly watching nothing is the defect it exists to remove.
		// Without this the seed test would report that refusal as a bad
		// seed. Nothing is read from the FS here — watchPaths validates
		// path SHAPE and never stats — so an empty one is enough, and
		// every other element sees no change.
		ctx := &Context{Values: vals, Dispatcher: gooey.NewDispatcher(), Includes: fstest.MapFS{}}
		_, err := Build([]byte(`<Gooey>`+h.open+src+h.close+`</Gooey>`), ctx)
		if err == nil {
			return nil
		}
		errs = append(errs, "  "+h.open+" -> "+err.Error())
	}
	return fmt.Errorf("no legal host accepts this seed:\n%s", strings.Join(errs, "\n"))
}

// TestEverySeededElementLoadsAndOccupiesSpace is the whole point of
// ElementDef.Seed, and it is the check that turns a class of bug into one
// that cannot come back.
//
// Before it, the palette's own measurements were: <Image> and <MenuBar>
// did not load at all, and <HStack>, <VStack>, <ButtonBar> and
// <ActivityBar> arrived measuring 0x0 — invisible on the canvas AND
// unselectable, because hitTest never returns a zero-size component, so a
// user could not click the thing they had just added in order to give it
// the content that would have made it appear.
//
// Both halves are asserted because neither implies the other. A seed that
// loads and measures nothing is the four containers; a seed that would
// measure fine but does not load is the other two.
func TestEverySeededElementLoadsAndOccupiesSpace(t *testing.T) {
	for _, d := range seedable(t) {
		t.Run(d.Name, func(t *testing.T) {
			comp, err := composeSeed(t, d)
			if err != nil {
				t.Fatalf("<%s>'s seed does not load, so a palette adding it would "+
					"leave the document broken:\n%v", d.Name, err)
			}
			if d.spec().NonVisual {
				// A non-visual element occupies no space, so "is it
				// there?" has to be asked a different way — and the
				// answer is its ACTION. That is what it is for. A
				// <KeyBinding> with a gesture and no command is a key
				// that does nothing: added, present in the document,
				// and indistinguishable from absent.
				//
				// Checked at the markup level rather than by invoking
				// it, because a load SUCCEEDS only if the command
				// resolved — ctx.Command rejects an unknown name — so a
				// seed that names one and builds has a live Action by
				// construction.
				assertSeedIsLive(t, d)
				return
			}
			// Bounds is not on Component -- it is the separate Bounded
			// interface (composer.go), which every arranged component
			// satisfies through Base.
			b, ok := comp.(gooey.Bounded)
			if !ok {
				t.Fatalf("<%s>'s seed built a %T, which does not report Bounds, "+
					"so this test cannot tell whether it occupies space", d.Name, comp)
			}
			if r := b.Bounds(); r.W <= 0 || r.H <= 0 {
				t.Errorf("<%s>'s seed was arranged at %dx%d. A palette-added element "+
					"that measures nothing is invisible on the canvas and cannot be "+
					"selected either, so the user cannot fix it by hand.",
					d.Name, r.W, r.H)
			}
		})
	}
}

// assertSeedIsLive is the non-visual half of "did adding this element
// actually put something there".
//
// Every command attribute the element declares must be SET by the seed.
// That is the whole assertion, and it is deliberately about the
// declaration rather than about one named attribute: <KeyBinding> spells
// it Command today, and the rule is not about that spelling — it is that
// an element whose reason to exist is behaviour may not be seeded
// without any.
func assertSeedIsLive(t *testing.T, d *ElementDef) {
	t.Helper()

	spec := d.spec()
	var cmds []string
	for _, a := range spec.Attrs {
		if a.Kind == KindCommand {
			cmds = append(cmds, a.Name)
		}
	}
	if len(cmds) == 0 {
		return // nothing to bind; its behaviour is not attribute-shaped
	}
	src, _, err := Seeded(spec, spec.Name+"1")
	if err != nil {
		t.Fatalf("<%s>: %v", spec.Name, err)
	}
	for _, c := range cmds {
		if !strings.Contains(src, c+"=") {
			t.Errorf("<%s>'s seed does not set %s, so a palette adding it "+
				"produces an element that cannot do anything. A non-visual "+
				"element IS its action — one without a command is present in "+
				"the document and indistinguishable from absent, which is the "+
				"same defect as a container that measures 0x0.\nseed: %s",
				spec.Name, c, src)
		}
	}
}

// TestEveryElementDeclaresASeed is the half that keeps the test above
// from failing OPEN.
//
// Without it a new element with no Seed is not a failure — Seeded errors,
// the subtest reports "does not load", and that reads like a bug in the
// seed rather than an absent one. Worse, an author who reads "no Seed" as
// "seeds are optional" gets exactly the element the palette cannot show.
func TestEveryElementDeclaresASeed(t *testing.T) {
	for _, d := range seedable(t) {
		if strings.TrimSpace(d.Seed) == "" {
			t.Errorf("<%s> declares no Seed. Every element a palette can insert "+
				"needs one — an unseeded element is one a user can add and then "+
				"not see. See ElementDef.Seed.", d.Name)
		}
	}
}

// rootAttrs is the text of the seed's outermost start tag — everything
// from "<" to the ">" that closes it. Deliberately textual: the seed is
// a template, not yet a document (its {{.Attr}} references have not been
// keyed), so parsing it as XML here would test a different string than
// the one the rule is about.
func rootAttrs(seed string) string {
	i := strings.Index(seed, "<")
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(seed[i:], '>')
	if j < 0 {
		return seed[i:]
	}
	return seed[i : i+j+1]
}

// A seed may not carry an attribute whose meaning depends on the parent.
//
// Canvas.Left and Canvas.Top are legal only under a <Canvas> and are
// discarded in silence anywhere else, which is the exact defect the
// catalog exists to delete. They belong to whoever INSERTS the element,
// because only it knows the real target — and it has AttrsFor(spec,
// parent) to ask.
//
// This is not hypothetical tidiness: the seeds are authored against a
// Canvas host in the test above, so a stray Canvas.Left would look
// correct there and be dropped the moment a user added the element inside
// a <Grid>.
// Only the seed's ROOT is checked, and the distinction is the whole
// subtlety. <Canvas>'s seed has to give its own children Canvas.Left,
// because there the Canvas IS the parent granting the geometry — that is
// a seed doing its job, not a seed guessing about a parent it does not
// have. The rule bites on the OUTERMOST element, whose parent is
// whatever the user dropped it into.
func TestNoSeedCarriesAParentDependentAttribute(t *testing.T) {
	// DERIVED from the registry, never listed here. A written list is a
	// sample taken once: it named Canvas.Left/Top and Grid.Row/Col, so a
	// new attached property — or a new parent contributing any — was
	// unguarded the day it was added, and nothing went red to say so.
	// AttrSpec.Name for an attached property is ALREADY qualified —
	// "Canvas.Left", not "Left" — so the parent must not be prepended
	// again. Doing so yields "Canvas.Canvas.Left", which matches nothing
	// and leaves this test passing while checking for a string no seed
	// can contain. It went green that way on the first attempt here.
	var attached []string
	for _, p := range AttachedParents() {
		for _, a := range AttachedAttrs(p) {
			attached = append(attached, a.Name)
		}
	}
	// The sweep must not be able to pass by finding nothing. A registry
	// that contributed no attached properties would make every subtest
	// below vacuous while still reporting green.
	if len(attached) == 0 {
		t.Fatal("no attached properties in the registry, so this test cannot fail; " +
			"either AttachedParents/AttachedAttrs regressed or the vocabulary lost them")
	}

	for _, d := range seedable(t) {
		root := rootAttrs(d.Seed)
		for _, bad := range attached {
			if strings.Contains(root, bad+"=") {
				t.Errorf("<%s>'s seed sets %s, which is only meaningful under one "+
					"parent and is silently discarded under any other. Geometry is "+
					"the inserter's job.", d.Name, bad)
			}
		}
	}
}

// A container's seed names its children inline, and those children are
// taken VERBATIM rather than re-seeded from their own element's seed.
//
// The rule matters because the obvious alternative — expand each child
// from its own seed — has no fixed point: <Border>'s seed would grow a
// <Text>, whose seed grows a body, and a seed that is a recipe applied
// recursively is a seed that keeps growing. It is also what makes a
// container able to state what its children are FOR: <MenuBar>'s <Menu>
// needs a Title, and only <MenuBar> knows what to call it.
func TestAContainerSeedKeepsItsOwnChildren(t *testing.T) {
	d, ok := elementDefs["VStack"]
	if !ok {
		t.Skip("no <VStack> in the registry")
	}
	src, values, err := Seeded(d.spec(), "V1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(src, "<Text") < 2 {
		t.Fatalf("<VStack>'s seed is %q; this test needs it to name more than one "+
			"child, or it is not testing what it claims", src)
	}
	ctx := &Context{Values: values, Dispatcher: gooey.NewDispatcher()}
	root, err := Build([]byte(`<Gooey>`+src+`</Gooey>`), ctx)
	if err != nil {
		t.Fatal(err)
	}
	kids := root.(gooey.Container).ChildComponents()
	if len(kids) < 2 {
		t.Errorf("<VStack>'s seed built %d child component(s); the children it names "+
			"inline are the ones that must appear, not a re-seeded substitute", len(kids))
	}
}
