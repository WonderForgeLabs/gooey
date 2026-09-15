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
	// and not the builder: a def registered with Known and no Build is
	// a nil deref the moment an element resolves to it, which says
	// nothing about key-versus-Name.
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
// A nil def is a host clearing a key, and an empty Name is a def that
// never declared one — neither is the key-versus-Name disagreement the
// check is about, and refusing them would turn a pre-parse guard on the
// grant vocabulary into a validator of registration hygiene. Raised in
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
