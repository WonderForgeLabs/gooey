package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
)

// TestEveryGrantKindHasADragKind. dragKindFor's switch is this editor's
// entire knowledge of layout models, and its default arm means a
// GrantKind nobody mapped silently becomes "placed by its parent" — the
// designer quietly switching off for a whole class of container, which
// is the exact failure the grant contract was written to end.
//
// The list is DERIVED from the framework's declared constants rather
// than restated, so adding a kind there fails here.
func TestEveryGrantKindHasADragKind(t *testing.T) {
	for _, g := range []markup.GrantKind{
		markup.GrantOffset, markup.GrantCell, markup.GrantOrder,
	} {
		if got := dragKindFor(g); got == DragFixed {
			t.Errorf("GrantKind %q falls through dragKindFor's switch to %q, so a "+
				"container declaring it would not be designable and nothing would say so",
				g, got)
		}
	}
	if got := dragKindFor(markup.GrantNone); got != DragFixed {
		t.Errorf("a GrantNone parent gives its child drag kind %q, want %q",
			got, DragFixed)
	}
}

// TestGrantNoneReadsAsPlacedRatherThanReorderable.
//
// A <Border> holds one child and places it. The old dragKind returned
// "reorder" for it, because its switch named Canvas and Grid and let
// everything else fall through — so the editor told the user a border's
// only child could be reordered among siblings it does not have. The
// catalog distinguishes declared order from no grant at all, and the
// diagnostic has to distinguish them too.
func TestGrantNoneReadsAsPlacedRatherThanReorderable(t *testing.T) {
	ed, c, _ := designerPageCounting(t)
	ed.doc().Elem = "Border"
	ed.doc().Attrs = map[string]string{}
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "aaaa", Attrs: map[string]string{"Name": "A"}},
	}
	ed.rebuild()
	c.Frame()

	if got := ed.grantOf("Border").Kind; got != markup.GrantNone {
		t.Fatalf("<Border> grants %q; this test assumed GrantNone", got)
	}
	if got := ed.dragKind(ed.doc().Kids[0]); got != DragFixed {
		t.Errorf("a child of a <Border> has drag kind %q, want %q — a border places its "+
			"child, so there is nothing to reorder", got, DragFixed)
	}
}

// grantingTable is a third-party container element the framework has
// never heard of, declaring a CELL grant with its own attribute
// spelling.
//
// It builds a components.Grid, which is a detail: what is under test is
// that the EDITOR reads the declaration, not that the layout works.
// Deliberately spelled "Table.R"/"Table.C" rather than anything
// resembling Grid.Row, so a lingering hardcoded name cannot pass.
func grantingTable() *markup.ElementDef {
	return &markup.ElementDef{
		Name:     "Table",
		Proto:    &components.Grid{},
		Known:    true,
		Children: markup.ChildSpec{Mode: markup.ModeMany},
		Grants: markup.Grant{
			Kind: markup.GrantCell,
			Attached: []markup.AttrSpec{
				{Name: "Table.R", Role: markup.RoleRow, Kind: markup.KindInt, Binds: markup.BindsLiteral, Default: "0", Category: markup.CategoryLayout},
				{Name: "Table.C", Role: markup.RoleCol, Kind: markup.KindInt, Binds: markup.BindsLiteral, Default: "0", Category: markup.CategoryLayout},
			},
		},
		Build: func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
			rows, err := components.ParseGridLens("2,2,2")
			if err != nil {
				return nil, err
			}
			cols, err := components.ParseGridLens("8,8")
			if err != nil {
				return nil, err
			}
			kids, _, err := markup.BuildChildren(e, ctx)
			if err != nil {
				return nil, err
			}
			return &components.Grid{Rows: rows, Cols: cols, Children: kids}, nil
		},
	}
}

// TestAThirdPartyContainerIsDesignableWithNoEditorChange is the point of
// the whole contract, and the one test that would have failed before it.
//
// The editor used to switch on the literal strings "Canvas" and "Grid".
// A host registering a container of its own got "reorder" — no drag, no
// geometry, no way to design in it — and gooey's entire markup story is
// that a host adds elements. Nothing below mentions Canvas or Grid: the
// element is registered, the palette re-derived, and the editor's
// answers come from the declaration.
func TestAThirdPartyContainerIsDesignableWithNoEditorChange(t *testing.T) {
	ed, c, _ := designerPageCounting(t)
	ed.docCtx.Elements["Table"] = grantingTable()
	ed.loadPalette()

	ed.doc().Elem = "Table"
	ed.doc().Attrs = map[string]string{}
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "aaaa", Attrs: map[string]string{"Name": "A", "Table.R": "0", "Table.C": "0"}},
	}
	ed.rebuild()
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("the <Table> fixture does not build: %s", got)
	}
	c.Frame()

	n := ed.doc().Kids[0]
	if got := ed.dragKind(n); got != DragCell {
		t.Fatalf("a child of a third-party GrantCell container has drag kind %q, want %q — "+
			"the editor is still deciding from the element NAME", got, DragCell)
	}

	comp := ed.componentFor(n)
	if comp == nil {
		t.Fatal("the child under <Table> was not built")
	}
	b := comp.(interface{ Bounds() gooey.Rect }).Bounds()
	if b.W == 0 {
		t.Fatal("the child under <Table> was never arranged")
	}

	// Drag into a different cell and commit.
	press(c, b.X, b.Y)
	if !ed.drag.active() {
		t.Fatal("a press on a child of a third-party GrantCell container began no drag")
	}
	motion(c, b.X+9, b.Y+3)
	release(c, b.X+9, b.Y+3)

	// The move must be recorded in the container's OWN spelling.
	if n.Attrs["Table.R"] == "0" && n.Attrs["Table.C"] == "0" {
		t.Errorf("the drag left Table.R=%q Table.C=%q — it moved nothing",
			n.Attrs["Table.R"], n.Attrs["Table.C"])
	}
	for _, wrong := range []string{"Grid.Row", "Grid.Col", "Canvas.Left", "Canvas.Top"} {
		if v, ok := n.Attrs[wrong]; ok {
			t.Errorf("the drag wrote %s=%q onto a child of a <Table>, which grants no such "+
				"attribute — applyLayout discards it in silence", wrong, v)
		}
	}
}

// TestTheEditorNamesNoContainerElement is the structural half, and it is
// what stops the taxonomy growing a second copy again.
//
// Release used to spell "Grid.Row" and "Canvas.Left" as literals while
// dragKind spelled "Grid" and "Canvas" — two copies of one rule, in one
// file, each able to drift from the catalog independently. The
// assertions above prove the behaviour; this proves there is nowhere
// left for the old shape to hide.
// It scans STRING LITERALS via go/parser rather than grepping the file,
// and the difference is load-bearing: this file's comments discuss the
// old "Grid.Row" spelling at length, and a grep would either fail on the
// prose explaining the fix or force the prose to avoid naming what it
// fixed. The literals are the claim; the comments are allowed to say so.
func TestTheEditorNamesNoContainerElement(t *testing.T) {
	banned := map[string]bool{
		"Canvas": true, "Grid": true, "VStack": true, "HStack": true,
		"Canvas.Left": true, "Canvas.Top": true, "Grid.Row": true, "Grid.Col": true,
		"Grid.RowSpan": true, "Grid.ColSpan": true,
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "drag.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing drag.go: %v", err)
	}
	found := 0
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		found++
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		if banned[v] {
			t.Errorf("drag.go:%d contains the string literal %q. Geometry comes from the "+
				"parent's markup.Grant now — ask ed.grantOf(parent).Attr(role) rather than "+
				"spelling a container's vocabulary in the editor",
				fset.Position(lit.Pos()).Line, v)
		}
		return true
	})
	if found == 0 {
		t.Fatal("no string literals found in drag.go at all, so this test asserts " +
			"nothing — the walk is broken, not the file")
	}
}

// A LOST WRITE NAMES EVERY ROLE IT LOST, IN A FIXED ORDER.
//
// Release used to collect the pending writes in a `map[markup.Role]int`
// and range over it. Go randomises that order, which cost determinism
// twice over: the two `d.node.Attrs` writes landed in an arbitrary
// order, and `lost` was built by assignment rather than append, so it
// kept whichever role the runtime happened to visit LAST. The same drag
// on the same document reported "Row" on one run and "Col" on the next,
// and losing BOTH roles was indistinguishable from losing one.
//
// The failure mode is the reason this is worth a test rather than a
// tidy-up: a message that varies run to run cannot be asserted, so the
// sentence users read was the one thing here nothing could pin.
//
// Reproducing a lost write means doing what the code's own comment says
// causes it — the document changing under the gesture. The drag begins
// against a parent that grants the cell roles, and the parent is
// swapped for one that grants nothing before the release.
func TestALostWriteNamesEveryRoleInAFixedOrder(t *testing.T) {
	ed, c, _ := designerPageCounting(t)
	ed.docCtx.Elements["Table"] = grantingTable()
	ed.loadPalette()

	ed.doc().Elem = "Table"
	ed.doc().Attrs = map[string]string{}
	ed.doc().Kids = []*node{
		{Elem: "Text", Body: "aaaa", Attrs: map[string]string{"Name": "A", "Table.R": "0", "Table.C": "0"}},
	}
	ed.rebuild()
	if got := ed.status.Get(); !strings.HasPrefix(got, "✓") {
		t.Fatalf("the <Table> fixture does not build: %s", got)
	}
	c.Frame()

	n := ed.doc().Kids[0]
	comp := ed.componentFor(n)
	if comp == nil {
		t.Fatal("the child under <Table> was not built")
	}
	b := comp.(interface{ Bounds() gooey.Rect }).Bounds()
	if b.W == 0 {
		t.Fatal("the child under <Table> was never arranged")
	}

	press(c, b.X, b.Y)
	if !ed.drag.active() {
		t.Fatal("a press on a child of a granting container began no drag")
	}
	motion(c, b.X+9, b.Y+3)

	// The document changes under the gesture: the parent is no longer a
	// granting container, so BOTH roles resolve to an empty attribute
	// name at release.
	ed.doc().Elem = "Canvas"
	delete(ed.docCtx.Elements, "Table")
	release(c, b.X+9, b.Y+3)

	msg := ed.dragHint.Get()
	if msg == "" {
		t.Fatal("a drag that could write neither role reported nothing — " +
			"dropping the write silently is the defect this message exists to remove")
	}
	// BOTH roles, not one. With the map the loop overwrote `lost`, so
	// exactly one name survived and this assertion fails whichever one
	// the runtime picked.
	for _, role := range []markup.Role{markup.RoleRow, markup.RoleCol} {
		if !strings.Contains(msg, string(role)) {
			t.Errorf("the lost-write message is %q — it does not name %q. Both roles "+
				"were lost, and a message naming one of them makes losing both look "+
				"identical to losing one", msg, role)
		}
	}
	// And in a FIXED order, which is what makes the sentence assertable
	// at all. RoleRow is written before RoleCol at the switch, so the
	// message must follow the switch rather than map iteration.
	if i, j := strings.Index(msg, string(markup.RoleRow)),
		strings.Index(msg, string(markup.RoleCol)); i > j {
		t.Errorf("the message is %q — it names %q before %q. The order comes from the "+
			"switch, not from map iteration, so it must not vary between runs",
			msg, markup.RoleCol, markup.RoleRow)
	}
}
