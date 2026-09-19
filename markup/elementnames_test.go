package markup

import (
	"strings"
	"testing"
	"testing/fstest"
)

// TestBothEntryPointsRefuseAMismatchedElementName is checkElementNames'
// only coverage, and the reason it needs some is that it had none.
//
// The check went in wired to Load alone. Build is the other public way
// in — parse bytes, build the tree — and it skipped the check entirely,
// so a host registering Elements["Table"] = &ElementDef{Name: "Grid"}
// and building from bytes still got the map-order-dependent grant the
// check exists to refuse. Measured on the head this was written against:
//
//	Build -> <nil>
//	Load  -> markup: Context.Elements["Table"] declares Name "Grid" — …
//
// The arms are BOTH ENTRY POINTS on one context, rather than the check
// called directly, because "which callers run it" is the whole finding:
// a direct call passes on a branch where neither entry point does.
// Raised in review of #486.
func TestBothEntryPointsRefuseAMismatchedElementName(t *testing.T) {
	const doc = `<Gooey><Grid><Text Grid.Row="0">x</Text></Grid></Gooey>`

	mismatched := func() *Context {
		return &Context{Elements: map[string]*ElementDef{
			"Table": {Name: "Grid", Known: true},
		}}
	}
	fsys := fstest.MapFS{"page.gooey": &fstest.MapFile{Data: []byte(doc)}}

	for _, tc := range []struct {
		name string
		run  func(ctx *Context) error
	}{
		{"Build", func(ctx *Context) error {
			_, err := Build([]byte(doc), ctx)
			return err
		}},
		{"Load", func(ctx *Context) error {
			_, err := Load(fsys, "page.gooey", ctx)
			return err
		}},
	} {
		err := tc.run(mismatched())
		if err == nil {
			t.Errorf("%s accepts Elements[\"Table\"] declaring Name \"Grid\". The key "+
				"and the Name are read as one string in three places — the shadow "+
				"map, the builtin loop and granting()'s caller — so the builtin "+
				"<Grid> is not shadowed, both defs grant Grid.Row, and which one "+
				"wins is map order", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), `"Table"`) || !strings.Contains(err.Error(), `"Grid"`) {
			t.Errorf("%s refuses with %q; the message must name both the key and "+
				"the Name, because the fix is to make one match the other and the "+
				"reader cannot tell which is wrong without seeing both",
				tc.name, err)
		}
	}

	// AND IT STANDS DOWN when they agree, which is what stops the check
	// from being a ban on registering elements at all.
	// A key the document does not use, because the point is the check
	// and not the builder: a def registered with Known and no Build
	// fails the load the moment an element resolves to it, which says
	// nothing about key-versus-Name. It said "is a nil deref", which
	// stopped being true in this same change — noBuild turned it into
	// "markup: <Table> builds no component of its own —
	// Context.Elements[\"Table\"] declares no Build". Raised in review
	// of #486.
	agreeing := &Context{Elements: map[string]*ElementDef{
		"Table": {Name: "Table", Known: true},
	}}
	if _, err := Build([]byte(doc), agreeing); err != nil {
		t.Errorf("a registration whose key and Name agree is refused: %v", err)
	}
}

// TestTheElementNameCheckSkipsWhatItCannotRead covers the two shapes the
// loop deliberately lets through, so neither becomes an accident later.
//
// A nil def and an empty Name are both outside what this check is for:
// neither is the key-versus-Name disagreement it exists to catch, and
// refusing them here would turn a pre-parse guard on the grant
// vocabulary into a validator of registration hygiene.
//
// SKIPPED IS NOT SANCTIONED, and a nil def in particular is not a
// supported state — this called it "a host clearing a key", which reads
// as one. Measured when it did: Build and Catalog both panicked on
// Context{Elements: {"Leafy": nil}}, two calls past this check. The
// panic is gone — Context.spec treats a nil def as unregistered, so the
// load fails with an unknown element, and Catalog skips it — but what
// this arm asserts is only that the NAME CHECK stands down, which is a
// statement about scope and not about the shape being usable. Raised in
// review of #486.
func TestTheElementNameCheckSkipsWhatItCannotRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		def  *ElementDef
	}{
		{"a nil def", nil},
		{"a def with no Name", &ElementDef{}},
	} {
		ctx := &Context{Elements: map[string]*ElementDef{"Table": tc.def}}
		if err := ctx.checkElementNames(); err != nil {
			t.Errorf("%s is refused with %v; the check is about a key and a Name "+
				"that DISAGREE, and neither of these states one", tc.name, err)
		}
	}
}

// TestANilElementDefIsRefusedRatherThanDereferenced is the other half of
// the arm above, and without it that arm reads as a licence.
//
// checkElementNames lets a nil def through on purpose — refusing it
// there would make a pre-parse guard on the grant vocabulary into a
// validator of registration hygiene — so the shape reaches the loader,
// and the loader used to take the process down on it. Measured before:
// both Build and Catalog panicked with a nil pointer dereference,
// because buildComponent read d.Build and Context.spec and
// Context.catalog called d.specAs, all on the nil.
//
// A nil def declares nothing, so "unregistered" is the honest answer and
// the element is unknown. That is the same treatment noBuild gave the
// nil Build FIELD in this change — a sentence instead of a SEGV — and
// the reason this is a test rather than a comment is that the panic was
// reachable from a public entry point with a one-key Context. Raised in
// review of #486.
func TestANilElementDefIsRefusedRatherThanDereferenced(t *testing.T) {
	ctx := &Context{Elements: map[string]*ElementDef{"Leafy": nil}}

	_, err := Build([]byte(`<Gooey><VStack><Leafy/></VStack></Gooey>`), ctx)
	if err == nil {
		t.Fatal("a document using a nil-def key loaded, so the key declares " +
			"something after all and the rest of this test is about the wrong " +
			"shape")
	}
	if !strings.Contains(err.Error(), "Leafy") {
		t.Errorf("the refusal does not name the element: %v", err)
	}

	// CATALOG TOO, and separately: it is a read-only enumeration a
	// designer calls without loading anything, so it reaches specAs by
	// its own path and panicked by its own path.
	if got := len(ctx.Catalog()); got == 0 {
		t.Error("Catalog() is empty with one nil def registered — the builtins " +
			"are gone, so the nil is being handled by abandoning the walk " +
			"rather than by skipping the entry")
	}
}
