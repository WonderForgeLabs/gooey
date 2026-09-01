package markup

import (
	"strconv"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// granters is every builtin that confers geometry on its children.
//
// DERIVED from the registry, never a list — the same rule seedable
// follows, and for the same reason: a list here goes stale on the next
// container added, and the way it goes stale is by passing.
func granters(t *testing.T) []*ElementDef {
	t.Helper()

	var out []*ElementDef
	for _, d := range definedElements() {
		if d.Grants.Kind != GrantNone {
			out = append(out, d)
		}
	}
	if len(out) == 0 {
		t.Fatal("no element declares a Grant, so every assertion below would be " +
			"vacuous — the walk is broken, not the vocabulary")
	}
	return out
}

// TestEveryMultiChildElementDeclaresAGrant is the rot-preventer, and it
// is the whole reason Grants is a field rather than a lookup an editor
// does for itself.
//
// A container that takes many children POSITIONS them somehow. If it
// declares no grant, every editor reading the catalog is told GrantNone
// — "this element positions its children itself, there is nothing to
// edit" — which for a new stack is a silent lie that disables the
// designer for it with no error anywhere. That is exactly the failure
// this contract was written to end, so a new ModeMany element with no
// grant has to fail the suite rather than ship quietly.
func TestEveryMultiChildElementDeclaresAGrant(t *testing.T) {
	for _, d := range definedElements() {
		if !d.holdsSeveralChildren() {
			continue
		}
		if d.Grants.Kind == GrantNone {
			t.Errorf("<%s> holds several visual children (Children.Mode %q, and its Proto "+
				"is a gooey.Container) but declares no Grant, so the catalog tells every "+
				"editor its children have no editable geometry. Declare Grants on its "+
				"literal in elements.go: GrantOrder if position is the child's index, "+
				"GrantOffset or GrantCell with the attached attributes if the child "+
				"carries its own", d.Name, d.Children.Mode)
		}
	}
}

// TestARestrictedContainerIsCoveredByTheGrantContracts is the discrimination
// floor under holdsSeveralChildren, and it is why the predicate is derived.
//
// Both contracts around it are LOOPS OVER A REGISTRY: if the predicate
// went back to answering only for ModeMany, every restricted container
// would simply be skipped and both tests would stay green while saying
// nothing about it. A skipped element is indistinguishable from a
// passing one at the loop, so the coverage has to be asserted here
// rather than inferred from the suite.
//
// It asserts a POPULATION, not a name: at least one restricted container
// and at least one restricted non-container must exist for the predicate
// to be discriminating at all, and every restricted container must reach
// the contracts. Deleting <Tabs> does not fail this; deleting the last
// restricted container does, which is the point at which the predicate
// stops being tested by anything.
func TestARestrictedContainerIsCoveredByTheGrantContracts(t *testing.T) {
	var containers, dataOnly []string
	for _, d := range definedElements() {
		if d.Children.Mode != ModeRestricted {
			continue
		}
		if _, ok := d.Proto.(gooey.Container); ok {
			containers = append(containers, d.Name)
		} else {
			dataOnly = append(dataOnly, d.Name)
		}
	}
	if len(containers) == 0 || len(dataOnly) == 0 {
		t.Fatalf("holdsSeveralChildren is not discriminating anything: %d restricted "+
			"containers and %d restricted non-containers. Both sides have to exist "+
			"for the ModeRestricted arm to be under test", len(containers), len(dataOnly))
	}
	for _, d := range definedElements() {
		if d.Children.Mode != ModeRestricted {
			continue
		}
		_, isContainer := d.Proto.(gooey.Container)
		if got := d.holdsSeveralChildren(); got != isContainer {
			t.Errorf("<%s> is ModeRestricted with Proto container=%v, but "+
				"holdsSeveralChildren says %v — the grant contracts either skip a "+
				"container that positions children or demand a grant from an element "+
				"whose children are data", d.Name, isContainer, got)
		}
	}
	t.Logf("restricted containers %v are covered; %v are data-only", containers, dataOnly)
}

// TestOnlyMultiChildElementsGrantGeometry is the converse, and it is
// what stops the field from being sprinkled onto elements where it means
// nothing. A <Border> holds one child and places it; saying it grants
// order would tell an editor that reordering is available where there is
// nothing to reorder.
func TestOnlyMultiChildElementsGrantGeometry(t *testing.T) {
	for _, d := range granters(t) {
		if !d.holdsSeveralChildren() {
			t.Errorf("<%s> declares Grants{Kind: %q} but it does not hold several visual "+
				"children (Children.Mode is %q) — an element that positions at most one "+
				"child, or whose children are not components at all, has no geometry to "+
				"confer on them", d.Name, d.Grants.Kind, d.Children.Mode)
		}
	}
}

// TestGrantKindMatchesTheRolesItCarries pins the two halves of a Grant
// against each other. The Kind says which layout model; Attached says
// which attributes carry it. A GrantCell that forgot RoleCol, or a
// GrantOrder that grew an attached attribute, is a contract that reads
// consistent and behaves otherwise.
func TestGrantKindMatchesTheRolesItCarries(t *testing.T) {
	// required is what the kind cannot mean anything without; optional
	// is what it may additionally carry. Anything else is a mistake.
	required := map[GrantKind][]Role{
		GrantOffset: {RoleX, RoleY},
		GrantCell:   {RoleRow, RoleCol},
		GrantOrder:  nil,
	}
	optional := map[GrantKind][]Role{
		GrantCell: {RoleRowSpan, RoleColSpan},
	}

	for _, d := range granters(t) {
		g := d.Grants
		req, known := required[g.Kind]
		if !known {
			t.Errorf("<%s> declares Grants{Kind: %q}, which is not one of the declared "+
				"GrantKinds — add it to this table and say what roles it carries",
				d.Name, g.Kind)
			continue
		}
		for _, r := range req {
			if g.Attr(r) == "" {
				t.Errorf("<%s> is %q but carries no attribute for role %q; a %q grant "+
					"cannot position a child without one", d.Name, g.Kind, r, g.Kind)
			}
		}
		allowed := map[Role]bool{}
		for _, r := range append(append([]Role(nil), req...), optional[g.Kind]...) {
			allowed[r] = true
		}
		for _, r := range g.Roles() {
			if !allowed[r] {
				t.Errorf("<%s> is %q and carries role %q, which that kind has no meaning "+
					"for", d.Name, g.Kind, r)
			}
		}
		if g.Kind == GrantOrder && len(g.Attached) > 0 {
			t.Errorf("<%s> is GrantOrder but declares %d attached attributes; in the order "+
				"model a child's position IS its index, and an editor that wrote one of "+
				"these would be writing an attribute nothing reads",
				d.Name, len(g.Attached))
		}
	}
}

// TestEveryCellGrantDeclaresItsTrackStructure.
//
// A cell grant says a child carries a row and a column. Those index
// something, and an editor that cannot find WHAT can draw no grid, show
// no track spec, and resize nothing — it is back to knowing that a Grid
// spells its tracks "Rows" and "Cols".
//
// Only GrantCell needs this: an offset grant has no tracks, and an order
// grant has no geometry at all.
func TestEveryCellGrantDeclaresItsTrackStructure(t *testing.T) {
	for _, d := range granters(t) {
		if d.Grants.Kind != GrantCell {
			continue
		}
		spec := d.spec()
		for _, r := range []Role{RoleRowTracks, RoleColTracks} {
			name := AttrByRole(spec, r)
			if name == "" {
				t.Errorf("<%s> grants cells but declares no attribute for role %q, so an "+
					"editor cannot find the track list its children's cells index into",
					d.Name, r)
				continue
			}
			// The attribute must actually be a track list, not merely
			// labelled one.
			var kind Kind
			for _, a := range spec.Attrs {
				if a.Name == name {
					kind = a.Kind
				}
			}
			if kind != KindGridLens {
				t.Errorf("<%s> declares %q as role %q, but its Kind is %q, not %q",
					d.Name, name, r, kind, KindGridLens)
			}
		}
		if AttrByRole(spec, RoleRowTracks) == AttrByRole(spec, RoleColTracks) {
			t.Errorf("<%s> names the same attribute %q for both axes",
				d.Name, AttrByRole(spec, RoleRowTracks))
		}
	}
}

// TestOnlyCellGrantsDeclareTracks is the converse: tracks on an offset
// or order container would advertise structure that does not exist.
func TestOnlyCellGrantsDeclareTracks(t *testing.T) {
	for _, d := range definedElements() {
		if d.Grants.Kind == GrantCell {
			continue
		}
		spec := d.spec()
		for _, r := range []Role{RoleRowTracks, RoleColTracks} {
			if name := AttrByRole(spec, r); name != "" {
				t.Errorf("<%s> is %q but declares %q as role %q; only a cell grant has "+
					"tracks", d.Name, d.Grants.Kind, name, r)
			}
		}
	}
}

// TestEveryAttachedAttributeCarriesAUniqueRole. A role is how an editor
// asks for an attribute without knowing its name, so an attached
// attribute with no role is unreachable by the mechanism the whole
// contract exists to provide, and two attributes sharing one make
// Grant.Attr's answer depend on declaration order.
func TestEveryAttachedAttributeCarriesAUniqueRole(t *testing.T) {
	for _, d := range granters(t) {
		seen := map[Role]string{}
		for _, a := range d.Grants.Attached {
			if a.Role == RoleNone {
				t.Errorf("<%s> grants %q with no Role, so an editor can only reach it by "+
					"its literal name — which is the element-name knowledge this contract "+
					"removes", d.Name, a.Name)
				continue
			}
			if prev, dup := seen[a.Role]; dup {
				t.Errorf("<%s> grants both %q and %q as role %q; Grant.Attr would return "+
					"whichever is declared first", d.Name, prev, a.Name, a.Role)
			}
			seen[a.Role] = a.Name
		}
	}
}

// TestAttachedNamesAreDottedWithTheirGrantingElement. The dotted prefix
// is markup's own syntax rather than a convention invented here, and
// applyLayout reads each attribute by its full dotted spelling — so a
// grant declaring an undotted name declares an attribute that will never
// be honoured.
func TestAttachedNamesAreDottedWithTheirGrantingElement(t *testing.T) {
	for _, d := range granters(t) {
		for _, a := range d.Grants.Attached {
			if !strings.HasPrefix(a.Name, d.Name+".") {
				t.Errorf("<%s> grants %q, which is not prefixed %q. The prefix IS the "+
					"scoping — attrcheck rejects an attached attribute under the wrong "+
					"parent by that name", d.Name, a.Name, d.Name+".")
			}
		}
	}
}

// TestNoTwoElementsGrantTheSameAttribute. attrcheck maps an attached
// name back to the single parent that permits it, so a name granted
// twice makes that answer depend on map iteration order.
func TestNoTwoElementsGrantTheSameAttribute(t *testing.T) {
	owner := map[string]string{}
	for _, d := range granters(t) {
		for _, a := range d.Grants.Attached {
			if prev, dup := owner[a.Name]; dup {
				t.Errorf("both <%s> and <%s> grant %q", prev, d.Name, a.Name)
			}
			owner[a.Name] = d.Name
		}
	}
}

// TestEachRoleReachesTheLayoutFieldItNames is the DISCRIMINATING test,
// and the one the rest of this file would be worthless without.
//
// Every assertion above is about the shape of the declaration: that a
// GrantCell carries a RoleRow, that the name is dotted, that roles are
// unique. All of them pass just as well when RoleRow and RoleCol are
// swapped — the declaration is still well-formed, and an editor built on
// it would move elements along the wrong axis while every test stayed
// green.
//
// So this one goes through the loader: set the attribute a role names,
// build, and require the value to arrive in the Layout field that role
// MEANS. It is the same discipline as
// TestDeclaredDefaultsRenderIdenticallyToOmission — a declaration is
// only allowed where it can be checked against behaviour.
func TestEachRoleReachesTheLayoutFieldItNames(t *testing.T) {
	// The distinct values matter: 3 and 5 rather than 1 and 1, so a
	// swapped pair cannot satisfy the assertion by coincidence.
	read := map[Role]struct {
		set int
		get func(*gooey.Layout) int
	}{
		RoleX:       {3, func(l *gooey.Layout) int { return l.Left }},
		RoleY:       {5, func(l *gooey.Layout) int { return l.Top }},
		RoleRow:     {3, func(l *gooey.Layout) int { return l.Row }},
		RoleCol:     {5, func(l *gooey.Layout) int { return l.Col }},
		RoleRowSpan: {2, func(l *gooey.Layout) int { return l.RowSpan }},
		RoleColSpan: {4, func(l *gooey.Layout) int { return l.ColSpan }},
	}

	for _, d := range granters(t) {
		roles := d.Grants.Roles()
		if len(roles) == 0 {
			continue // GrantOrder: nothing to write, nothing to check
		}
		// One child carrying every attribute this parent grants, each
		// with a different value.
		var attrs []string
		for _, r := range roles {
			w, ok := read[r]
			if !ok {
				t.Fatalf("role %q granted by <%s> has no Layout field in this table; "+
					"a new role needs its meaning pinned here or it is unchecked",
					r, d.Name)
			}
			attrs = append(attrs, d.Grants.Attr(r)+`="`+strconv.Itoa(w.set)+`"`)
		}
		// A cell grant needs enough tracks for row/col 5 to exist. Which
		// attributes spell that is the element's own business, so it is
		// read off the declaration rather than assumed: writing Rows=
		// unconditionally fails the load on <Canvas>, which does not
		// take it.
		var parentAttrs []string
		for _, a := range d.Attrs {
			if a.Kind == KindGridLens {
				parentAttrs = append(parentAttrs, a.Name+`="1,1,1,1,1,1"`)
			}
		}
		src := `<Gooey><` + d.Name + ` ` + strings.Join(parentAttrs, " ") + `>` +
			`<Text ` + strings.Join(attrs, " ") + `>x</Text>` +
			`</` + d.Name + `></Gooey>`

		ctx := &Context{Dispatcher: gooey.NewDispatcher()}
		root, err := Build([]byte(src), ctx)
		if err != nil {
			t.Errorf("<%s>'s granted attributes do not load: %v\n%s", d.Name, err, src)
			continue
		}
		kid := onlyChild(t, root)
		l := gooey.LayoutOf(kid)
		if l == nil {
			t.Errorf("the child under <%s> carries no Layout", d.Name)
			continue
		}
		for _, r := range roles {
			w := read[r]
			if got := w.get(l); got != w.set {
				t.Errorf("<%s> declares %q as role %q, but setting it to %d left the "+
					"Layout field that role names reading %d. The role and the attribute "+
					"disagree: an editor asking for %q would move the element along the "+
					"wrong axis", d.Name, d.Grants.Attr(r), r, w.set, got, r)
			}
		}
	}
}

// onlyChild is the single visual child of a container built for a test.
func onlyChild(t *testing.T, root gooey.Component) gooey.Component {
	t.Helper()

	c, ok := root.(gooey.Container)
	if !ok {
		t.Fatalf("built root %T is not a container", root)
	}
	kids := c.ChildComponents()
	if len(kids) != 1 {
		t.Fatalf("built root has %d children, want 1", len(kids))
	}
	return kids[0]
}

// TestTheCatalogCarriesTheGrant. The editor's whole path is
// Context.Catalog -> ElementSpec.Grants, so a grant that does not
// survive specAs is a contract nothing can read.
func TestTheCatalogCarriesTheGrant(t *testing.T) {
	ctx := &Context{Dispatcher: gooey.NewDispatcher()}
	byName := map[string]ElementSpec{}
	for _, e := range ctx.Catalog() {
		byName[e.Name] = e
	}
	for _, d := range granters(t) {
		e, ok := byName[d.Name]
		if !ok {
			t.Errorf("<%s> grants geometry but is not in the catalog", d.Name)
			continue
		}
		if e.Grants.Kind != d.Grants.Kind {
			t.Errorf("<%s>: catalog says Kind %q, definition says %q",
				d.Name, e.Grants.Kind, d.Grants.Kind)
		}
		if len(e.Grants.Attached) != len(d.Grants.Attached) {
			t.Errorf("<%s>: catalog carries %d attached attributes, definition declares %d",
				d.Name, len(e.Grants.Attached), len(d.Grants.Attached))
		}
	}
}

// TestTheCatalogHandsOutACopyOfTheGrant. A spec is handed out; a
// consumer editing the slice it got back must not be editing the
// registry every later consumer reads.
func TestTheCatalogHandsOutACopyOfTheGrant(t *testing.T) {
	const el = "Grid"
	first := GrantOf(el)
	if len(first.Attached) == 0 {
		t.Fatalf("<%s> grants nothing, so this test checks nothing", el)
	}
	first.Attached[0].Name = "clobbered"

	if got := GrantOf(el).Attached[0].Name; got == "clobbered" {
		t.Errorf("editing the returned grant changed the registry: <%s>'s first attached "+
			"attribute is now %q", el, got)
	}
}

// TestAttachedAttrsAgreesWithTheGrant. AttachedAttrs is the older
// accessor and now reads through the grant; if the two ever disagree,
// one of the catalog's two answers to the same question is wrong.
func TestAttachedAttrsAgreesWithTheGrant(t *testing.T) {
	for _, d := range granters(t) {
		got, want := AttachedAttrs(d.Name), d.Grants.Attached
		if len(got) != len(want) {
			t.Errorf("<%s>: AttachedAttrs returned %d attributes, the grant declares %d",
				d.Name, len(got), len(want))
			continue
		}
		for i := range got {
			if got[i].Name != want[i].Name {
				t.Errorf("<%s>: AttachedAttrs[%d] is %q, the grant declares %q",
					d.Name, i, got[i].Name, want[i].Name)
			}
		}
	}
}

// TestAttachedParentsIsExactlyTheGrantingElements.
func TestAttachedParentsIsExactlyTheGrantingElements(t *testing.T) {
	want := map[string]bool{}
	for _, d := range granters(t) {
		if len(d.Grants.Attached) > 0 {
			want[d.Name] = true
		}
	}
	got := map[string]bool{}
	for _, p := range AttachedParents() {
		got[p] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("<%s> grants attached attributes but AttachedParents omits it", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("AttachedParents names <%s>, which grants no attached attributes", name)
		}
	}
}

// TestAskingAGrantForARoleItLacksIsAnEmptyString. The empty answer is a
// real state an editor must handle — asking a stack where its child's X
// lives is a legitimate question — so it may not become a panic or a
// plausible-looking wrong name.
func TestAskingAGrantForARoleItLacksIsAnEmptyString(t *testing.T) {
	order := GrantOf("VStack")
	if order.Kind != GrantOrder {
		t.Fatalf("<VStack> is %q, not GrantOrder; this test picked the wrong element",
			order.Kind)
	}
	for _, r := range []Role{RoleX, RoleY, RoleRow, RoleCol, RoleNone} {
		if got := order.Attr(r); got != "" {
			t.Errorf("<VStack> grants order and carries no attributes, but Attr(%q) "+
				"returned %q", r, got)
		}
	}
	if got := GrantOf("Grid").Attr(RoleNone); got != "" {
		t.Errorf("Attr(RoleNone) returned %q, want \"\"", got)
	}
	if got := GrantOf("NoSuchElement").Kind; got != GrantNone {
		t.Errorf("an unknown element granted %q, want GrantNone", got)
	}
}
