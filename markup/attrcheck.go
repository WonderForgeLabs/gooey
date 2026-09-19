package markup

import (
	"fmt"
	"sort"
	"strings"
)

// Unknown-attribute rejection: the payoff the component catalog was
// built for.
//
// Before this, an attribute nobody recognized was DROPPED IN SILENCE.
// applyLayout switches on the attribute key with no default arm, and
// most element arms read the attributes they know by name and never look
// at the rest. The motivating case is an attached property spelled bare:
//
//	<Text Left="10" Top="3">BARE</Text>
//
// Canvas.Left and Canvas.Top are the only spellings that work. The bare
// forms were accepted, ignored, and left the element sitting at the
// origin with nothing on screen, and nothing in any error, to say why.
//
// This is a deliberate BEHAVIOR CHANGE and it breaks pages: markup that
// relied on an attribute being ignored now fails to load. That cost was
// accepted knowingly, because the alternative is a class of defect that
// cannot be debugged from what the user can see. Three elements already
// worked this way — <Companion>, its <Var>, and <Validate> — and their
// errors are the model followed here.
//
// The error must name the FIX, not just the fault. "unknown attribute
// Left" is nearly useless when the answer is four characters away, so a
// near-miss suggestion is offered whenever one is close enough to be
// worth printing. That is cheap: the vocabulary is already in hand.

// checkAttrs rejects attributes the element cannot accept. It runs
// beside checkProps, which does the same job for property elements.
// asData says the caller is the element's READER — buildMenuBar or
// buildTabs, consuming it as data — rather than build(), which is about
// to build it. It is the whole of the pseudo-element gate below, and the
// reason it is a parameter instead of a property of the spec is that
// ElementSpec.Pseudo cannot answer it. See the comment on that gate.
func checkAttrs(e Element, ctx *Context, asData bool) error {
	spec, ok := ctx.spec(e.Name)

	// THE UNIVERSAL SET IS NOT PART OF ANY ELEMENT'S OWN VOCABULARY, so
	// this runs BEFORE the two gates below rather than inside either.
	//
	// It began inside the !AttrsKnown early return, where <Tab> was the
	// only pseudo-element that could reach it — <Menu> and <MenuItem>
	// are AttrsKnown and fell through to the exhaustive check, which
	// refuses a universal with "no such attribute; this element takes
	// Title". refuseComponentAttr's own comment calls that wording a lie,
	// and it was being told by two of the three elements the argument
	// was written for. Raised in review of #486; the three now share
	// one sentence, and TestEveryPseudoElementRefusesAUniversalTheSameWay
	// is what keeps them sharing it.
	//
	// PSEUDO IS NOT "BUILDS NO COMPONENT", and the two refusals below
	// rest on that claim — but asData is the whole of what establishes
	// it, and `spec.Pseudo &&` stood here too until #486's round 9.
	// Pseudo is
	// derived as `Proto == nil && (Opaque != "" || ParsedBy != "")`
	// (elementdef.go) and says NOTHING about Build — a host
	// Context.Elements def may carry ParsedBy and a real Build at once,
	// and buildComponent then calls named() on what it returns and
	// attachAll on its attachments. Measured on such a def:
	//
	//	<Deck><Panel Label="a"><Panel.Behaviors><Tooltip Text="x"/>
	//	  </Panel.Behaviors><Text>y</Text></Panel></Deck>
	//	  before: Panel.Build ran, one attachment applied
	//	  after:  markup: <Panel.Behaviors>: … builds no component for
	//	          Behaviors to apply to
	//
	// A page that loaded stopped loading, and the reason given was
	// false. This is the hazard refuseComponentAttr's own doc names for
	// !TakesLayout — refusing off an absent-by-default signal breaks
	// working apps — arriving through ParsedBy instead.
	//
	// THE DISCRIMINATING QUESTION IS THE CALL SITE, not the spec. An
	// element consumed as DATA is one its reader walked out of
	// e.Children: buildTabs reads a <Tab>'s Header itself, buildMenuBar
	// reads a <Menu>'s Title, and nothing downstream will ever call
	// named() or applyLayout() with it — which is exactly the argument
	// the refusal rests on. An element reaching build() is about to be
	// built, whatever the catalog says about who declared it. Raised in
	// review of #486.
	//
	// SO THE SPEC HALF IS GONE, and it was not redundant — it was a
	// hole. Pseudo is derived from the def, and a HOST def that
	// SHADOWS a builtin reader's child carries a Proto, which makes
	// Pseudo false while <Tabs> still reads it as data. Measured on
	// Elements["Tab"] = {Known, Proto: &components.Text{},
	// Attrs: [Header]}, before:
	//
	//	<Tab Header="a" Name="zonk">      no such attribute; this
	//	                                  element takes HAlign, Header…
	//	<Tab.Name>zonk</Tab.Name>         err=nil, Named empty
	//	<Tab.Behaviors><Tooltip…>         err=nil, Named empty
	//
	// The second and third are the #461 silent drop this change exists
	// to close, surviving in the property-element spelling; the first
	// answered with the wording refuseComponentAttr's own doc calls a
	// lie about a name that exists on every other element. After, all
	// three share the reads-as-data sentence.
	//
	// AND THE MEASUREMENT ABOVE DOES NOT RECUR, which is the thing to
	// check before reading this as a revert of it. The <Deck><Panel>
	// case is a def with ParsedBy AND a real Build: Pseudo is true
	// there, but its child reaches build() through BuildChildren, so
	// asData is FALSE and this gate does not fire either way. Measured
	// with the token dropped: that fixture still loads, err=nil. The
	// call site was already the discriminator; the spec test was
	// shadowing a case it could not see. Raised in review of #486.
	if ok && asData {
		if err := refuseComponentAttr(e, spec, ctx); err != nil {
			return err
		}
		// THE PROPERTY-ELEMENT SPELLING OF THE SAME THING, which was
		// silently accepted while the attribute spelling was refused.
		// <Tab.Name>, <Tab.Margin>, <Menu.Name> and <MenuItem.Name> all
		// loaded, were dropped and reported nothing. Measured through
		// Build before the fix, all four. Raised in review of #486.
		//
		// The comment here used to argue that "a pseudo-element never
		// reaches build()", which is what the gate above is now about:
		// a host's does.
		if err := refusePropElement(e, spec, ctx); err != nil {
			return err
		}
	}
	// A MISPLACED ELEMENT IS SOMEBODY ELSE'S ERROR TO REPORT, and this
	// is the gate that defers. checkAttrs runs before the element's own
	// Build, so on `<VStack><Tab Name="Z">…` the refusal above preempted
	// defTab.Build's "<Tab> is only valid directly inside <Tabs>" and
	// told the author to move an attribute — a remedy that leaves the
	// document just as broken, while the diagnosis that would fix it
	// never printed. The placement fault is the larger one and is
	// answerable from the catalog, so it is deferred to rather than
	// raced. See misplaced, whose comment records why it cannot ask
	// spec.Nested for the answer.
	//
	// This paragraph sat above the asData gate rather than this one
	// until review of #486 round 8 — describing a deferral two gates
	// away, and citing acceptedByParent, which misplaced replaced.
	misplacedHere := false
	if ok && spec.Pseudo && !asData {
		if misplaced(e, spec, ctx) {
			// THE DEFERRAL COVERS THE EXHAUSTIVE CHECK TOO, and it
			// reached only <Tab> when it did not. <Menu> and <MenuItem>
			// are AttrsKnown, so standing down from the universal
			// refusal alone dropped them into the gate below, which
			// answered `<VStack><Menu Name="Zonk">` with "no such
			// attribute; this element takes Title" — the wording
			// refuseComponentAttr's comment calls a lie, about the smaller
			// of two faults, while defMenu.Build's "<Menu> is only
			// valid directly inside <MenuBar>" never printed.
			//
			// Measured, not reasoned about: all three misplaced forms
			// were probed through Build before and after. Raised in
			// review of #486 round 2, which is round 1's finding 1 one
			// gate over.
			//
			// IT DEFERS THE UNIVERSALS, NOT THE WHOLE CHECK, and
			// returning nil here was the difference. The deferral is
			// only honest where a diagnosis actually follows, and
			// nothing obliges a host's Build to refuse its own
			// placement — the framework never asks it to and no
			// document says it must. A host def carrying ParsedBy AND a
			// Build that simply builds therefore bought SILENCE:
			// measured on a fourth child added to hostTableCtx,
			//
			//	<VStack><BuiltRow Label="a" Bogus="x"/></VStack>  err=<nil>
			//	<VStack><BuiltRow Labl="a"/></VStack>             err=<nil>
			//
			// — the document loads, the component is built, the typo is
			// dropped, and origin/main refuses both. That is #461's own
			// class arriving through the gate added to close it.
			//
			// The suite could not see it because all three of
			// hostTableCtx's children shared one build closure that
			// opens `if e.parent != "Table"`, so the FIXTURE supplied
			// the deferral target the gate merely assumes.
			//
			// Narrowed to the names cannotApplyTo covers rather than
			// gated on d.Build == nil: the second would stand down only
			// where no Build exists, and defMenu HAS one, so it would
			// hand `<VStack><Menu Name="Zonk">` back the "this element
			// takes Title" lie this deferral was added to remove.
			// Deferring the universals keeps that, while the element's
			// own declared vocabulary is still judged — which is a true
			// sentence wherever it fires. Raised in review of #486
			// round 9.
			misplacedHere = true
		}
	}

	if !ok || !spec.AttrsKnown {
		// A registered Go builder interprets attributes however it
		// likes, and an opaque element's vocabulary was never
		// enumerable. Claiming to validate either would be inventing a
		// rule the catalog cannot support.
		return nil
	}
	if spec.Open {
		// An open element owns its own check and can say more than this
		// one can. <Validate> reports the live rule vocabulary,
		// built-ins plus Context.Rules, which is strictly better than a
		// generic near-miss — so it must run instead of this, not after
		// it.
		return nil
	}
	allowed, attached := ctx.vocabulary(spec, e.parent, !asData)

	names := make([]string, 0, len(e.Attrs))
	for name := range e.Attrs {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if allowed[name] {
			continue
		}
		// An attached property that exists but belongs to a different
		// parent gets its own error: the attribute is spelled correctly
		// and is simply in the wrong place, which is a different
		// mistake from a typo and deserves a different sentence.
		if parent, ok := attached[name]; ok {
			return holdIfMisplaced(misplacedHere, fmt.Errorf("markup: <%s %s=%q>: %s is contributed by a <%s> parent, but this element's parent is %s; it would be ignored here",
				e.Name, name, e.Attrs[name], name, parent, describeParent(e.parent)))
		}
		return holdIfMisplaced(misplacedHere, fmt.Errorf("markup: <%s %s=%q>: no such attribute%s",
			e.Name, name, e.Attrs[name], suggest(name, allowed, attached)))
	}
	return nil
}

// deferredFault is an attribute fault on a MISPLACED element: real, but
// outranked by the placement fault if one actually follows.
//
// THE DEFERRAL USED TO BE A GUESS. checkAttrs runs before the element's
// own Build, so on `<VStack><Tab Name="Z">…` complaining about the
// attribute preempted defTab.Build's "<Tab> is only valid directly
// inside <Tabs>" and told the author to move an attribute — a remedy
// that leaves the document just as broken. The fix was to return nil and
// let Build speak, which is right exactly when Build DOES speak.
//
// Nothing obliges it to. A host's Context.Elements def may carry ParsedBy
// and a Build that simply builds — the framework never asks that Build to
// refuse its own placement and no document says it must — and then
// returning nil bought SILENCE. Measured on such a def:
//
//	<VStack><Built Label="a" Bogus="x"/></VStack>  err=<nil>
//	<VStack><Built Labl="a"/></VStack>             err=<nil>
//
// The document loads, the component is built, the typo is dropped, and
// origin/main refuses both — #461's own class arriving through the gate
// added to close it.
//
// So the fault is HELD rather than dropped, and build() emits it only
// after buildComponent returns without one. The ordering is the whole
// point: a real placement diagnosis still wins, and where none arrives
// the author hears about the attribute instead of nothing.
//
// NARROWING THE CHECK TO THE UNIVERSALS WOULD NOT DO, and was tried:
// TestAHostsPseudoElementIsCheckedWhereItsReaderSaysItBelongs pins that
// a misplaced <Row Bogus="x"> reports its PLACEMENT, which round 2
// established, and judging the element's own vocabulary there reports
// the attribute instead. Holding is what satisfies both. Raised in
// review of #486 round 9.
type deferredFault struct{ err error }

func (d deferredFault) Error() string { return d.err.Error() }
func (d deferredFault) Unwrap() error { return d.err }

func holdIfMisplaced(misplaced bool, err error) error {
	if misplaced {
		return deferredFault{err}
	}
	return err
}

// refuseComponentAttr rejects a universal attribute on a pseudo-element,
// and it exists because that is the one judgement an UNENUMERABLE
// element still supports (issue #461).
//
// checkAttrs declines to judge an element whose Attrs are not
// exhaustive, and that is right: the element's own vocabulary is
// genuinely unknown, so any refusal from it would be invented. The
// universal set is a different claim. Name is applied by named(), the
// layout rows by applyLayout() and Tooltip by
// applyTooltipShorthand() — all three beside the element switch, none
// of them by the element — so whether they apply is answered by the
// catalog's structure, not by the element's attribute list.
//
// Pseudo is that answer, and it is safe BECAUSE IT IS AFFIRMATIVE: it
// is derived from a nil Proto *and* a stated reason (elementdef.go), so
// it means "this builds no component of its own", not "we could not
// tell". A pseudo-element's parent reads it as data — buildTabs reads a
// <Tab>'s Header and content itself — so nothing ever reaches named()
// or applyLayout() with it.
//
// !TakesLayout is NOT interchangeable here, and reaching for it would
// break working apps. It is absent-by-default, and the construct that
// shows the difference is a HOST'S Context.Elements ElementDef with a
// real Build and no Proto: ctx.spec resolves it, AttrsKnown is false,
// TakesLayout is false — and Pseudo is false, because no reason is
// stated. build() runs applyLayout on the component it returns, so its
// universals are honoured, and refusing them off the absence would
// break it.
//
// NOT Context.Components, which this comment used to cite. ctx.spec
// returns ok=false for one of those, so both gates short-circuit alike
// and such an element never reaches this function at all — a reader
// checking the claim would find it does not hold and could conclude
// the gate is over-cautious. The fixture in
// TestAnUnenumerableElementThatBuildsOneKeepsItsUniversals is the
// discriminating shape, and it is a Context.Elements def for exactly
// this reason; the first attempt at that test used Components and could
// not tell the two gates apart. Corrected in review of #486.
//
// Grant.AttrsFor already implements the same rule on the other side —
// it withholds the layout rows on !TakesLayout and the identity row on
// Pseudo, so a property grid offers a pseudo-element no universal row
// at all. Before this the two gates disagreed in OPPOSITE directions
// for <Tab>, the one pseudo-element with AttrsKnown false: the designer
// dropped the Name row and the loader accepted it. A Name typed into a
// <Tab> in $EDITOR was accepted, dropped, and had no surface anywhere
// that would reveal it — strictly less discoverable than the state
// #454 improved.
//
// UNIVERSALS WERE NEVER THE WHOLE SET, and stopping there left the high
// half of the same defect open. The argument above is that a
// pseudo-element builds no component, so an attribute meant for a
// component cannot apply — and that is true of far more than
// universalAttrs. Measured on this branch before the widening:
//
//	<Tab Name="Zonk">     → refused
//	<Tab Grid.Row="1">    → LOADED, dropped, silent
//	<Tab Frobnicate="1">  → LOADED, dropped, silent
//
// So the gate asks two questions, in the order of what it can know.
//
// THE NAME IS THE OLDER HALF AND WAS RENAMED WITH THE SET. This was
// refuseUniversal while universalAttrs was the whole of what it refused;
// cannotApplyTo is the authority on the set now, and the name says
// "component attribute" because that is the single question both arms
// ask. Raised in review of #486.
//
// AN ATTACHED PROPERTY IS REFUSED WHATEVER THE CATALOG KNOWS. Grid.Row
// is an instruction to the element's CONTAINER about a component, and a
// pseudo-element has none to instruct — that holds without knowing the
// element's own surface, which is the whole reason it can be asked of
// <Tab>, whose AttrsKnown is false and whose Attrs are empty.
//
// AND NOTHING ELSE, which was measured rather than assumed. The obvious
// widening — "on a spec with AttrsKnown, refuse anything outside
// spec.Attrs" — changes no ACCEPTANCE: <Menu Frobnicate="1"> and
// <MenuItem Grid.Row="1"> are both already refused by the ordinary
// unknown-attribute gate, which can fire precisely because those
// surfaces are declared. All it would do is replace "no such attribute;
// this element takes Title" with this function's sentence, and for a
// name that exists nowhere the first is the better answer — "no such
// attribute" is only a lie for an attribute that exists elsewhere, which
// is the case this function is for.
//
// So the hole is the OPAQUE pseudo-element alone. <Tab> carries Opaque,
// its AttrsKnown is false and its Attrs are empty because buildTabs
// reads "Header" off its children without declaring it, so the ordinary
// gate cannot fire and an unknown name cannot be told from the one the
// builder really reads. <Tab Frobnicate="1"> therefore stays accepted
// and that is stated rather than papered over; #461 is where making the
// surface enumerable is tracked. An attached property is the part that
// needs no surface to adjudicate, which is why it is the whole of this
// change. Raised in review of #486.
func refuseComponentAttr(e Element, spec ElementSpec, ctx *Context) error {
	names := make([]string, 0, len(e.Attrs))
	for name := range e.Attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !cannotApplyTo(name) {
			continue
		}
		// The message names the MECHANISM, not just the fault. "no
		// such attribute" would be a lie — the attribute exists
		// everywhere else — and the author's real question is where
		// to put it instead.
		return fmt.Errorf("markup: <%s %s=%q>: %sso it builds no component for %s to apply to%s",
			e.Name, name, e.Attrs[name],
			readsAsData(e, spec, ctx), name, pseudoRemedy(e, spec, ctx, name))
	}
	return nil
}

// cannotApplyTo reports that this attribute names something only a
// COMPONENT could have, on an element that builds none. See
// refuseComponentAttr for the three cases and why they are ordered so.
// A DOT AND NOTHING ELSE. The first spelling of this excluded an xmlns
// prefix as "the other dotted name an element carries", which is simply
// wrong — a prefixed declaration is xmlns:h, with a COLON — so the
// exclusion could never fire and was guarding against nothing. Measured:
// removing it changes no result anywhere in the package. The arm in
// TestAPseudoElementRefusesAnAttachedProperty stays, because "a
// namespace declaration may sit on any element" is a real rule worth a
// regression pin even though this predicate is not what upholds it.
func cannotApplyTo(name string) bool {
	return isUniversalAttr(name) || strings.Contains(name, ".")
}

// refusePropElement rejects ANY property element on a pseudo-element,
// and "any" is not an over-reach — it is the same sentence
// refuseComponentAttr makes, read through the other spelling.
//
// A property element is consumed by the builder of the element that
// carries it, and a pseudo-element HAS no builder: <Tabs> reads a
// <Tab>'s Header and content itself, <MenuBar> reads <Menu> and
// <MenuItem> as data. So there is nothing on this element for a
// <Tab.Anything> to reach, whatever it is named.
//
// THAT INCLUDES Behaviors AND Resources, which checkProps exempts
// universally and which would therefore have been the one pair left
// silent if this had simply called checkProps. The exemption there is
// earned — buildChildren consumes <X.Behaviors> and pushResources
// consumes <X.Resources>, both from build() — and build() is exactly
// what a pseudo-element does not go through. An exemption that is true
// of every element that builds is not true of one that does not.
//
// It shares readsAsData with refuseComponentAttr so the author gets one
// sentence rather than two dialects of it, and
// TestEveryPseudoElementRefusesAPropertyElement is what keeps them
// sharing it. The REMEDY is not shared verbatim: see propRemedy, which
// is pseudoRemedy plus the one thing that differs between an attribute
// and a property element, which is how you write it where it is going.
func refusePropElement(e Element, spec ElementSpec, ctx *Context) error {
	if len(e.Props) == 0 {
		return nil
	}
	names := make([]string, 0, len(e.Props))
	for name := range e.Props {
		names = append(names, name)
	}
	sort.Strings(names)
	name := names[0]
	return fmt.Errorf("markup: <%s.%s>: %sso it builds no component for %s to apply to%s",
		e.Name, name, readsAsData(e, spec, ctx), name, propRemedy(e, spec, ctx, name))
}

// readsAsData is the reason clause of the message above: who consumes
// this element, when the catalog knows.
//
// IT NO LONGER SPLICES spec.Opaque, and that is finding 5 of #486's
// round 1. Opaque is a CATALOG-GENERATOR annotation — <Tab>'s reads "a
// pseudo-element: <Tabs> parses a <Tab>'s Header and content itself, so
// this definition exists only to reject one used anywhere else" —
// written to explain to a tool why an element could not be enumerated.
// Dropped into a load error it restated the clause beside it, trailed
// off into registry bookkeeping that means nothing to somebody editing
// a page, and coupled the error's wording to prose with nothing pinning
// it as error copy.
//
// THE CONTAINER, NOT THE PARSER, and the two are not always the same
// element. This asked ParsedBy first, which is the field that names the
// BUILDER — "<MenuBar> reads <Menu> as data" — and for <MenuItem> that
// made one element name two different containers across the two load
// errors an author can hit on it:
//
//	<MenuBar><Menu><MenuItem Margin="2"/>  → <MenuBar> reads <MenuItem> as data…
//	<VStack><MenuItem Margin="2"/>         → <MenuItem> is only valid directly inside <Menu>
//
// Both sentences are true and they answer different questions, which is
// exactly why they may not disagree: an author who hits the first and
// moves the element to a <MenuBar> hits the second. The placement error
// has no choice about which container it names — <Menu> is the only true
// answer to "where does this go" — so this clause is the half that
// moves. Raised in review of #486.
//
// ParsedBy stays what it is and is not edited to agree: it names the
// element whose Build consumes this one, catalogen resolves the
// attribute-drift check through it, and buildMenuBar genuinely walks
// both levels. It is the fallback here for a pseudo-element no container
// names, which is a declaration nothing can reach today (markNested) and
// a shape a host may still register.
//
// namingParent answers for <Tab>, the element this branch exists for and
// the one ParsedBy cannot carry: defTab declares nothing (Known: false)
// and buildTabs reads "Header" off its children, so a ParsedBy on it
// would make catalogen's checkPseudoPool report <Tabs> reading an
// attribute no <Tabs>-parsed element declares. Opaque is <Tab>'s
// annotation precisely because its surface is not enumerable. Raised in
// review of #486 round 2.
func readsAsData(e Element, spec ElementSpec, ctx *Context) string {
	// THE DOCUMENT'S OWN PARENT FIRST, and it is not one source among
	// several — it is the only one that answers the question asked.
	//
	// Both callers run from the READER — buildMenuBar and buildTabs,
	// which walk their own e.Children — so e.parent is the container
	// that consumed this element. That IS the reader. namingParent
	// discards it and searches
	// the catalog for the FIRST element naming this one, and
	// definedElements sorts by name — so "first" means alphabetically
	// first, not the container this element is inside. One more builtin
	// restricted to <MenuItem> sorting before <Menu> (a <ContextMenu>,
	// say) and the refusal would say "<ContextMenu> reads <MenuItem> as
	// data" while defMenuItem.Build goes on saying "only valid directly
	// inside <Menu>" — the two-containers-in-two-errors divergence round
	// 2's finding 6 was filed to remove, reintroduced by the search.
	// Reading e.parent also drops a whole catalog assembly per refusal.
	// Raised in review of #486.
	if p, ok := ctx.spec(e.parent); ok && namesChild(p, spec.Name) {
		return fmt.Sprintf("<%s> reads <%s> as data, ", p.Name, spec.Name)
	}
	// EVERYTHING BELOW IS UNREACHABLE FROM A PARSED DOCUMENT, and that
	// is a property to state rather than a gap to leave implied.
	//
	// Both callers run under `spec.Pseudo && asData`, and asData is
	// passed by the two readers alone — each of which is the
	// ModeRestricted container that names this element, so the branch
	// above answers. markup.go stamps parent on every element the
	// parser produces, and the parser is the only thing in this package
	// that constructs one. So for any document a user can write, the
	// branch above answers and none of these run.
	//
	// They are kept, not deleted, because readsAsData takes an Element by
	// value and nothing stops a future caller — or a test — handing it
	// one built by hand, which is the one shape with no parent. What the
	// fallbacks must NOT be is mistaken for the live path: this was
	// round 3's "ParsedBy has no live reader" finding, reopened when
	// round 4 put the parent branch in front, and the arm that keeps the
	// ParsedBy clause green calls this function directly with a
	// parentless Element. TestTheParserStampsAParentOnEveryElement pins
	// the reachability claim itself, so "dead on the live path" is
	// measured rather than asserted here.
	//
	// A Name-LESS host registration used to reach them for real, because
	// the branch above tests namesChild(p, spec.Name) and spec.Name was
	// empty; ctx.spec now defaults it to the registry key, which closed
	// that route. Raised in review of #486.
	if p := namingParent(spec.Name, ctx); p != "" {
		return fmt.Sprintf("<%s> reads <%s> as data, ", p, spec.Name)
	}
	if spec.ParsedBy != "" {
		return fmt.Sprintf("<%s> reads <%s> as data, ", spec.ParsedBy, spec.Name)
	}
	return fmt.Sprintf("<%s>'s parent reads it as data, ", spec.Name)
}

// namingParent is the catalog element that lists name among the children
// it accepts, or "" when none does.
//
// It answers the question ParsedBy answers, from the other side: a
// pseudo-element is reachable only where some ModeRestricted container
// names it, so that container IS the reader. Over the CATALOG rather
// than over ctx.spec, because the question is about every element in
// scope and not about one whose name is already in hand.
//
// WITHOUT THE INCLUDES, which is the one source that cannot answer.
// includeElements globs, reads and parses every *.gooey under
// ctx.Includes and never sets Children.Mode, so an include spec can
// never satisfy namesChild — this was spending file I/O on an error path
// to build a string, over entries that structurally cannot match. Raised
// in review of #486.
func namingParent(name string, ctx *Context) string {
	for _, p := range ctx.catalog(false) {
		if namesChild(p, name) {
			return p.Name
		}
	}
	return ""
}

// pseudoRemedy is the "put it somewhere else" tail, and it is offered
// only when there IS a somewhere else.
//
// <Tab> holds arbitrary content, so naming it is a real instruction.
// <MenuItem> is ModeLeaf and holds none; prescribing a move to nowhere
// is the same shape as finding 1 of #486's round 1, where a remedy was
// printed for a document whose actual defect was elsewhere.
//
// AND "HOLDS CONTENT" WAS THE WRONG QUESTION FOR <Menu>. Its content is
// ModeRestricted to <MenuItem>, which is itself a pseudo-element that
// refuses the identical attribute — so an author who followed the
// remedy landed on a second load error. Measured:
//
//	<MenuBar><Menu Name="Zonk">…      → …; put it on the content inside
//	<MenuBar><Menu><MenuItem Name=…>  → …no component for Name to apply to
//
// The predicate is therefore "would the content inside ACCEPT this",
// not "is there content inside". Derived over Children.Only rather than
// spelled per element, so a fourth pseudo-element is covered by the
// rule instead of by somebody remembering it. Raised in review of #486
// round 2.
//
// THE DEFAULT ARM IS <Tab>'S LIVE PATH, and it was the one arm with no
// sentence on it. ModeUnknown accompanies an opaque element, and <Tab>
// is the only one in the catalog: its content is arbitrary markup that
// <Tabs> builds as a page, so "put it on the content inside" is a real
// destination and the remedy is right. It reads as a fallthrough and is
// a decision — an opaque element's content is unknown, not absent, and
// withholding advice on "unknown" would leave the one pseudo-element
// with a genuine destination the only one not told about it. ModeOne,
// ModeMany and ModeAttachments land here too and want the same answer
// for the same reason; no pseudo-element carries one today.
//
// These three declarations each had their own paragraph and no blank
// comment line between them, so godoc rendered one block on
// contentRemedy and left this function and reservedOnContent
// undocumented — the same thing that happened to splitPasteMarker's
// neighbours in #445. Raised in review of #486.
func pseudoRemedy(e Element, spec ElementSpec, ctx *Context, name string) string {
	if why, ok := reservedOnContent[spec.Name][name]; ok {
		return why
	}
	// THE CONTENT MOVE IS ONLY SAYABLE FOR A UNIVERSAL — or for the two
	// property elements every element accepts, which is the exemption
	// this guard was missing.
	//
	// Behaviors and Resources are not attributes at all and are not in
	// universalAttrs, so the guard below refused them along with the
	// attached properties it was written for, and <Tab.Behaviors> lost
	// a remedy that was CORRECT: <Text.Behaviors> inside the <Tab>
	// loads. It also left propRemedy's `case name == "Behaviors" ||
	// name == "Resources": return r` able to return only "", against a
	// comment saying the spelling carries over unchanged, and made
	// TestEveryPseudoElementRefusesAPropertyElement's propOnContent
	// branch unreachable — all three silently, with the suite green.
	// Raised in review of #486.
	//
	// It did not need one until the attribute gate widened: cannotApplyTo
	// admitted only universalAttrs, and every universal a component
	// carries is one any other component carries too, so "put it on the
	// content inside" landed somewhere that accepts it. An ATTACHED
	// property is the opposite shape — it is an instruction to a
	// particular PARENT, and the content of a pseudo-element has the
	// pseudo-element for a parent, which contributes nothing. Measured,
	// following the advice:
	//
	//	<Tab Grid.Row="1"><Text>x</Text></Tab>
	//	  -> ... builds no component for Grid.Row to apply to; put it on
	//	     the content inside instead
	//	<Tab><Text Grid.Row="1">x</Text></Tab>
	//	  -> Grid.Row is contributed by a <Grid> parent, but this
	//	     element's parent is <Tab>; it would be ignored here
	//
	// A remedy that walks the author into a second load error is worse
	// than none, and there is no destination to name instead — so the
	// refusal says what is wrong and stops. Raised in review of #486.
	if !isUniversalAttr(name) && name != "Behaviors" && name != "Resources" {
		return ""
	}
	switch spec.Children.Mode {
	case ModeLeaf, ModeNone:
		return ""
	case ModeRestricted:
		for _, n := range spec.Children.Only {
			if acceptsInside(ctx, n, spec.Name, name) {
				return contentRemedy
			}
		}
		return ""
	}
	// EVERYTHING ELSE ASKS THE DOCUMENT, and that is the arm this
	// function shipped without. ModeUnknown is <Tab>'s, and <Tab> is the
	// one pseudo-element whose content is actually PRESENT — an
	// arbitrary subtree <Tabs> builds as a page — so the element with a
	// real destination to name was the one whose destination was never
	// consulted. Reachable on the builtins alone:
	//
	//	<Tabs><Tab Header="a" Margin="2"><Timer Interval="1s"/></Tab></Tabs>
	//	  -> … no component for Margin to apply to; put it on the
	//	     content inside instead
	//	<VStack><Timer Interval="1s" Margin="2"/></VStack>
	//	  -> no such attribute; this element takes Enabled, Interval,
	//	     Name, Tick
	//
	// The catalog cannot answer for ModeUnknown/ModeMany/ModeOne and
	// does not have to: what is inside is a fact of THIS document, and
	// e.Children is it. An element with nothing inside gets no remedy,
	// which is the same answer ModeLeaf gets and for the same reason.
	// Raised in review of #486.
	for _, c := range e.Children {
		if acceptsInside(ctx, c.Name, spec.Name, name) {
			return contentRemedy
		}
	}
	return ""
}

// acceptsInside reports whether child, sitting inside parent, would
// accept name — the claim "put it on the content inside" makes.
//
// PER ATTRIBUTE, NOT PER ELEMENT, and that is the finding. The
// predicate here was !Pseudo, which reads as "the content builds a
// component, so a universal lands on it" — true of the universal set
// only where the content HAS a Layout. <Timer>, <KeyBinding>,
// <Tooltip>, <Validate>, <TypeAhead>, <ValidationMarker> and
// <Companion> all build components and all take Name, and none of them
// takes a layout row. Measured on a host <Panel> restricted to <Timer>:
//
//	<Deck><Panel Margin="2"/></Deck>
//	  -> … no component for Margin to apply to; put it on the content
//	     inside instead
//	<VStack><Timer Margin="2" Interval="1s"/></VStack>
//	  -> no such attribute; this element takes Enabled, Interval, Name,
//	     Tick
//
// which is the walk-from-one-refusal-to-another the remedy discipline
// exists to stop, arriving through the one axis the element-level
// predicate cannot see. Name on the same <Panel> is a real instruction,
// so the answer genuinely differs by attribute. Raised in review of
// #486.
//
// ctx.vocabulary IS the acceptance rule, asked with this pseudo-element
// as the destination's parent — the same call checkAttrs makes on the
// prescribed document, so the advice is checked against the gate that
// will judge it rather than against a model of that gate.
//
// Behaviors and Resources are not attributes and are in no vocabulary;
// propElements accepts them on every element that builds one, which is
// what !Pseudo answers for. An unresolvable name is treated as
// accepting: the remedy is advice, and withholding it on a catalog gap
// is the worse failure of the two.
func acceptsInside(ctx *Context, child, parent, name string) bool {
	s, ok := ctx.spec(child)
	if !ok {
		return true
	}
	if s.Pseudo {
		return false
	}
	if name == "Behaviors" || name == "Resources" {
		return true
	}
	allowed, _ := ctx.vocabulary(s, parent, true)
	return allowed[name]
}

// contentRemedy is the prescription itself, named so the guard over it
// can recognise it rather than re-spelling it. A test grepping the
// sentence would also match reservedOnContent's answer, which says the
// content CANNOT take the attribute and shares most of its words.
const contentRemedy = "; put it on the content inside instead"

// reservedOnContent names the universals a pseudo-element's PARENT owns
// ON THE CONTENT INSIDE, with the sentence to say instead of the move.
//
// The remedy is a BEHAVIOURAL claim and Children.Mode is a STRUCTURAL
// fact, and for one attribute they disagree. A <Tab>'s content is an
// ordinary element that takes every universal — except Visibility,
// which buildTabs refuses on a page root because the <Tabs> binds it to
// "selected == me". So `<Tab Visibility="Hidden">` was refused with "put
// it on the content inside instead" and doing that hit a second load
// error: the author walked from one refusal to another, by advice.
// Nothing in the catalog says a container reserves an attribute on its
// children, so this cannot be derived — but it can be GUARDED, and
// TestTheContentRemedyIsAPlaceThatAccepts runs every universal through
// both positions and fails on a row that is stale as well as on a
// reservation with no row. Raised in review of #486.
var reservedOnContent = map[string]map[string]string{
	"Tab": {
		"Visibility": "; the <Tabs> binds every page's Visibility to the selection, " +
			"so it cannot go on the content inside either — set Tabs' Selected to choose the page",
	},
}

// propRemedy is pseudoRemedy's answer respelled for the PROPERTY-ELEMENT
// case, and the respelling is the whole of it.
//
// pseudoRemedy answers for an attribute, where "put it on the content
// inside" means writing `<Text Name="x">`. A property element is a
// different syntax with a different rule: checkProps accepts <X.Foo>
// only where propElements[X] lists Foo, and the two it accepts on
// EVERYTHING are Behaviors and Resources. So for the other seven
// universals the move is real and the spelling is not — an author who
// copied <Tab.Name> onto the content and wrote <Text.Name> hit
// checkProps' refusal instead, which is the same walked-from-one-error-
// to-another this file's whole remedy discipline exists to stop.
// Raised in review of #486.
//
// AND THE DESTINATION IS ONLY DERIVABLE FOR A UNIVERSAL.
// refusePropElement refuses ANY property element on a pseudo-element —
// deliberately, and wider than refuseComponentAttr — so this is reached for
// names the catalog answers nothing about, and it prescribed the content
// move for every one of them. Measured before the guard below:
//
//	<Tab.Frobnicate>       → …put it on the content inside instead…
//	<Text Frobnicate="z">  → no such attribute; this element takes Bold, …
//
// <Tab.Header> was sharper still: Header is the one attribute a <Tab>
// genuinely takes and it is REQUIRED, and the remedy sent it to the
// content. Both are the walk-from-one-error-to-another this function
// exists to stop, reintroduced by the scope difference between the two
// refusals. A universal is accepted by every element with a Layout, so
// for those the destination is known; for anything else nothing here can
// say the move lands, and saying nothing is the honest answer. Raised in
// review of #486.
func propRemedy(e Element, spec ElementSpec, ctx *Context, name string) string {
	r := pseudoRemedy(e, spec, ctx, name)
	switch {
	case name == "Behaviors" || name == "Resources":
		// The two property elements every element accepts: the spelling
		// carries over to the destination unchanged.
		return r
	case r == contentRemedy:
		// THE NAME IS A UNIVERSAL HERE, by construction rather than by
		// check. pseudoRemedy returns anything other than "" only for a
		// universal or for Behaviors/Resources, and the case above has
		// already consumed those two — so the `isUniversalAttr(name)`
		// this arm used to ask was always true, and the two statements
		// that followed it were dead. Proven by replacing them with a
		// panic: the whole markup suite stayed green. Raised in review
		// of #486, which is the same dead-arm-with-a-live-comment shape
		// round 4 fixed one function over.
		//
		// The content move is sayable here precisely BECAUSE the name is
		// universal: acceptance at the destination is derivable without
		// a schema, which it is for nothing else.
		return r + ", written as an attribute: a property element names a property " +
			"of the element carrying it, so <" + spec.Name + "." + name + "> is not a " +
			"form that moves"
	case r != "":
		return r // a reservation has its own sentence
	}
	if !sayableHere(name) {
		return ""
	}
	return attributeHere(spec, name)
}

// sayableHere reports whether attributeHere's advice survives being
// followed.
//
// attributeHere's doc argues that prescribing the attribute spelling is
// safe because "if the element does not take the name, the attribute
// gate answers with its own list, which is a better error". That is true
// for a name the vocabulary gate sees — <Menu.Frobnicate> reaches
// suggest(), measured:
//
//	<MenuBar><Menu Title="_F" Frobnicate="x">…
//	  -> no such attribute; this element takes Title
//
// — and FALSE for anything cannotApplyTo covers, because
// refuseComponentAttr intercepts those before the vocabulary gate runs
// and answers with no advice at all. Measured:
//
//	<Menu.Name>x</Menu.Name>
//	  -> ... builds no component for Name to apply to; write it as an
//	     attribute on this element instead, <Menu Name="…">, …
//	<Menu Name="x">            ← exactly what that advised
//	  -> ... builds no component for Name to apply to
//
// Walking an author from one refusal into another is the class this
// whole function exists to close, reintroduced through the one gate
// attributeHere's argument did not account for. Raised in review of
// #486.
//
// THE EXAMPLE IS <Menu> AND NOT <Tab>, which is what this said and was
// measurably false. <Tab> is the one pseudo-element with AttrsKnown
// false, so checkAttrs returns before the vocabulary gate and
// suggest() is never reached: `<Tabs><Tab Header="a" Frobnicate="x">`
// loads with err=nil and the attribute is dropped. attributeHere's own
// doc twenty lines below says exactly that, and
// TestARemedyOnAnUncheckedSurfaceSaysTheNameCanBeDropped pins it — so
// the sentence here was standing as a counter-example to the caveat it
// sits above. <Menu> and <MenuItem> are AttrsKnown, so they are the
// names for which the claim holds. Raised in review of #486 round 9.
func sayableHere(name string) bool { return !cannotApplyTo(name) }

// attributeHere is what can be said when no DESTINATION can be named:
// write it as an attribute on this same element.
//
// SILENCE WAS THE ALTERNATIVE, and <Tab.Header> is why that was wrong
// twice over. Header is the one attribute a <Tab> genuinely takes and it
// is REQUIRED, and the property-element spelling of it was refused with
// "<Tabs> reads <Tab> as data, so it builds no component for Header to
// apply to" and no advice at all — a sentence that reads as "there is
// nowhere for this to go" about the one name with an obvious home. The
// same silence covered <Menu.Frobnicate> and <MenuItem.Frobnicate>,
// where pseudoRemedy names no content because a <Menu> holds only
// pseudo-elements. Raised in review of #486.
//
// IT PROMISES NOTHING ABOUT ACCEPTANCE, which is what makes it sayable
// where the content move is not. The content remedy asserts a
// destination takes the attribute, and outside universalAttrs nothing
// here can derive that. This asserts only that the attribute spelling on
// THIS element is the only form that could work — and if the element
// does not take the name, the attribute gate answers with its own list,
// which is a better error than this one rather than a second blank
// refusal to walk to.
//
// EXCEPT WHERE THERE IS NO GATE, which is the one case that argument did
// not account for: a spec whose AttrsKnown is false. checkAttrs returns
// before the vocabulary gate for those, so an unrecognized attribute is
// not refused with a list — it is ACCEPTED and dropped, which is the
// #461 class this whole change exists to close. <Tab> is the only
// pseudo-element in that state, and it is reachable by following this
// very remedy:
//
//	<Tab.Frobnicate>z</Tab.Frobnicate>  -> refused, advising the
//	                                       attribute spelling
//	<Tab Frobnicate="z">                -> loads, and is dropped
//
// Silence is not the answer either — <Tab.Header> is a real property
// with an obvious home — and nothing here can tell Header from
// Frobnicate, because that is exactly what AttrsKnown false means. So
// the advice stands and says what it cannot promise. Raised in review of
// #486.
func attributeHere(spec ElementSpec, name string) string {
	tail := "> takes one — a property element names a property of the " +
		"element carrying it, and this element builds none"
	if !spec.AttrsKnown {
		tail = "> consumes it — a property element names a property of the " +
			"element carrying it, and this element builds none. <" + spec.Name +
			"> declares no attribute vocabulary this package can check, so a " +
			"name it does not consume is accepted there and silently dropped " +
			"rather than refused"
	}
	return "; write it as an attribute on this element instead, <" +
		spec.Name + " " + name + "=\"…\">, if <" + spec.Name + tail
}

// isUniversalAttr reports whether name is one of the attributes every
// element with a Layout accepts, which is the only set whose acceptance
// at a destination this package can derive without a schema.
func isUniversalAttr(name string) bool {
	for _, a := range universalAttrs {
		if a.Name == name {
			return true
		}
	}
	return false
}

// misplaced reports that the catalog states a home for this element and
// the document did not put it there, which is the precondition for
// standing down: where it holds, the element's own Build is about to say
// something more useful — "<Tab> is only valid directly inside <Tabs>" —
// and checkAttrs, which runs first, must not talk over it.
//
// THREE ANSWERS, AND THE THIRD IS WHY THIS IS NOT THE TWO-ANSWER
// PREDICATE IT REPLACED. The first two are the ones that had: the parent
// names this element among its children, or the catalog says the home is
// somewhere else. The third is a catalog that says NOTHING about where the element
// belongs — a host def stating its reason with Opaque, under a container
// that enumerates nothing. There is no placement diagnosis to defer to
// there, so standing down bought silence: measured on a <Table>
// (ModeMany, BuildChildren) holding an Opaque-declared <ORow>,
//
//	<Table><ORow Label="a" Bogus="x"/></Table>
//	  origin/main:  no such attribute; this element takes Label
//	  before this:  <nil>
//
// which is #461's own silent-drop class, with Opaque substituted for the
// ParsedBy the previous round covered. Deferring requires a destination;
// "the catalog knows no home" is not one. Raised in review of #486.
//
// ASKED OF THE PARENT, NOT OF spec.Nested, and that distinction is the
// whole reason this function is not two lines shorter. Nested says
// exactly what the first branch wants — "builds nothing AND some
// container names it" — but it is DERIVED BY markNested OVER THE
// ASSEMBLED CATALOG, and ctx.spec returns a per-def spec that markNested
// has never touched. So ctx.spec("Tab").Nested is false for the one
// element the field was added for, while ctx.Catalog()'s entry for the
// same name is true. The first version of this check read spec.Nested
// and was therefore dead: it returned false for every element, the
// refusal fired regardless of placement, and the finding it was written
// for was unfixed with every test green. Measured, not reasoned about.
func misplaced(e Element, spec ElementSpec, ctx *Context) bool {
	if p, ok := ctx.spec(e.parent); ok && namesChild(p, spec.Name) {
		return false
	}
	home := declaredHome(spec, ctx)
	return home != "" && home != e.parent
}

// declaredHome is the element the catalog says reads this one: its own
// ParsedBy, or the ModeRestricted container naming it. Empty means the
// catalog states no placement rule at all, which is a different answer
// from "somewhere else" and the two are not interchangeable — see
// misplaced.
func declaredHome(spec ElementSpec, ctx *Context) string {
	if spec.ParsedBy != "" {
		return spec.ParsedBy
	}
	return namingParent(spec.Name, ctx)
}

// namesChild is the relation every pseudo-element rule is phrased in
// terms of: a ModeRestricted container listing name among the children
// it accepts. A pseudo-element is reachable only where some container
// names it, so this predicate is what "legal here" and "who reads this"
// both reduce to.
//
// THREE CALLERS, NOT FIVE, and the difference is worth stating because a
// review counted five copies of it. misplaced asks it of ONE named
// parent and namingParent asks it of the whole catalog — the same
// predicate, two questions, which is why one reads ctx.spec and the
// other reads Catalog(); that is not an inconsistent source.
//
// The other two are different relations wearing similar code.
// markNested (catalog.go) inverts it — it collects every name any
// container mentions, in one pass over the specs it is in the middle of
// assembling, so it cannot ask Catalog() anything without recursing
// into itself. acceptsAUniversal (pseudouniversal_test.go) asks whether
// an element's OWN children are all pseudo-elements, which is a
// question about the far side of the relation and gives a different
// answer. legalParent, in the same test file, is namingParent restated,
// and it is no longer anybody's independent answer — the reader
// assertion reads the document's own tag (enclosingTag) since review of
// #486 found a re-typed loop agreeing with the loop it copied, this
// comment included. What legalParent still does is supply a parent for
// the fixtures that need one to build a document at all. Raised in
// review of #486.
func namesChild(spec ElementSpec, name string) bool {
	if spec.Children.Mode != ModeRestricted {
		return false
	}
	for _, n := range spec.Children.Only {
		if n == name {
			return true
		}
	}
	return false
}

func describeParent(parent string) string {
	if parent == "" {
		return "the document root"
	}
	return "<" + parent + ">"
}

// vocabulary is everything settable on this element in this context: its
// own attributes, the universal set if it has a Layout, and the attached
// properties its actual parent contributes. It also returns every OTHER
// attached property, keyed by the parent that would contribute it, so a
// misplaced one can be reported as misplaced rather than as unknown.
// builds says the element is about to be BUILT rather than consumed as
// data, which is the only thing that decides whether Name is in its
// vocabulary. See the Name paragraph below.
func (ctx *Context) vocabulary(spec ElementSpec, parentName string, builds bool) (allowed map[string]bool, attached map[string]string) {
	allowed = make(map[string]bool, len(spec.Attrs)+len(universalAttrs)+4)
	// NAME IS UNIVERSAL EXCEPT WHERE THERE IS NOTHING TO ADDRESS —
	// hoisted above the TakesLayout gate because every element that
	// BUILDS one can be named, which a pseudo-element does not.
	// buildMenuBar reads <Menu> and <MenuItem> as data and never calls
	// named(), so <MenuItem Name="Save"> loaded clean and ctx.Named
	// stayed empty forever: accepted, dropped, no error anywhere.
	//
	// THE WRITE IS GUARDED RATHER THAN COMPUTED, and that is not style.
	// This map's contract is ABSENT-means-disallowed, because suggest()
	// ranges over its KEYS and never reads the value. So
	// `allowed["Name"] = false` refuses correctly while both messages go
	// on advertising Name — "did you mean Name?" on the element that had
	// just started refusing it. Every other write here is a bare
	// `= true`, so the contract held implicitly until this line asked a
	// question whose answer could be false.
	//
	// AND "BUILDS ONE" IS THE CALL SITE, NOT Pseudo. A host's
	// Context.Elements def may carry ParsedBy and a real Build at once,
	// and buildComponent calls named() on what that Build returns — so
	// withholding Name off the derivation alone refused an attribute
	// that addresses something real. checkAttrs passes !asData here for
	// the same reason it gates the two pseudo refusals on it. Raised in
	// review of #486.
	//
	// `builds` IS THE WHOLE ANSWER, and an `|| !spec.Pseudo` beside it
	// was a silent drop rather than a redundancy. The only caller that
	// can reach this with builds == false is checkAttrs on a child its
	// parent reads as data, and a reader — buildTabs, buildMenuBar, or
	// a host's own Build walking e.Children — never calls named(). So
	// the disjunct admitted Name on exactly the accepted-and-dropped
	// shape #461 exists to close: a host def that shadows Tab, Menu or
	// MenuItem with a Proto is not Pseudo, took Name="…", and left
	// ctx.Named empty with no error. acceptsInside is the other caller
	// and passes builds == true, so it is unaffected. Measured in
	// review of #486 and pinned by
	// TestAShadowingHostDefStillRefusesNameWhereItsReaderTakesOver.
	if builds {
		allowed["Name"] = true
	}
	for _, a := range spec.Attrs {
		allowed[a.Name] = true
	}
	if spec.Open {
		for _, a := range ctx.openAttrs(spec) {
			allowed[a.Name] = true
		}
	}
	attached = map[string]string{}
	if !TakesLayout(spec) {
		return allowed, attached
	}
	for _, a := range universalAttrs {
		if a.Name == "Name" {
			// NAME IS DECIDED ABOVE, ON `builds`, AND THIS LOOP WAS
			// OVERRIDING IT. Name is in universalAttrs, so every
			// element that takes layout got the row back here
			// regardless of the guarded write — which made that write
			// reachable only for the layout-less minority and left the
			// silent drop it exists to close wide open for everything
			// else. Measured in review of #486: with the guard
			// tightened but this loop untouched, a host <Tab> def with
			// a *components.Text Proto still took Name="zonk" under
			// <Tabs>, dropped it, and reported nothing.
			//
			// Skipping rather than re-writing keeps ONE decision site.
			// `allowed["Name"] = builds` would not work here for the
			// reason the paragraph above gives: suggest() ranges over
			// the map's KEYS, so a false value still advertises Name in
			// "did you mean".
			continue
		}
		allowed[a.Name] = true
	}
	// The DOCUMENT ROOT has no layout parent to scope against: its
	// syntactic parent is the <Gooey> wrapper, which is not one.
	//
	// This is not a convenience. At the root the answer does not exist
	// AT THIS LAYER. A patch fragment's real parent lives in the
	// target's live tree, which Build has never seen and which
	// PatchMarkup only resolves after the fragment builds; a whole
	// page's root has no layout parent at all. Build cannot even tell
	// the two apart — they are the same syntax.
	//
	// So every attached property is permitted at the root rather than
	// rejected. patch_markup documents restating a layout attribute as a
	// FEATURE — "layout attributes the fragment does not restate are
	// preserved from the old element; restating one takes it over" — and
	// enforcing a parent rule at a position with no parent broke every
	// such patch, plus every swap of a page whose root carried one.
	//
	// Only the misplaced-attached rule is suspended, and only here:
	// unknown attributes are still rejected at the root like anywhere
	// else.
	if parentName == "Gooey" || parentName == "" {
		for _, d := range ctx.granting() {
			for _, a := range d.Grants.Attached {
				allowed[a.Name] = true
			}
		}
		return allowed, attached
	}
	for _, d := range ctx.granting() {
		for _, a := range d.Grants.Attached {
			if d.Name == parentName {
				allowed[a.Name] = true
				continue
			}
			attached[a.Name] = d.Name
		}
	}
	return allowed, attached
}

// granting is every element in scope that contributes attached
// attributes — the builtins, plus whatever the host registered.
//
// Host-registered elements are included, and that direction is safe:
// a container that declares a Grant can only ADD names to `allowed`,
// so this accepts markup that was previously rejected and can never
// reject markup that previously loaded. Leaving them out would mean a
// host could declare Table.Column in its catalog, watch a palette offer
// it, and then have the loader refuse the result — the catalog lying
// about the target, which is the defect the catalog exists to remove.
func (ctx *Context) granting() []*ElementDef {
	// Registered first, and the shadowing runs in that direction:
	// buildComponent consults Context.Elements BEFORE the builtins, so
	// for a name declared in both, the registered definition is the one
	// that actually builds — and therefore the one whose grant is real.
	// Taking the builtin's grant here would validate against a
	// vocabulary the build never uses.
	out := make([]*ElementDef, 0, len(elementDefs)+len(ctx.Elements))
	shadowed := map[string]bool{}
	for name, d := range ctx.Elements {
		if d == nil {
			continue
		}
		shadowed[name] = true
		if len(d.Grants.Attached) > 0 {
			out = append(out, d)
		}
	}
	for _, d := range definedElements() {
		if len(d.Grants.Attached) > 0 && !shadowed[d.Name] {
			out = append(out, d)
		}
	}
	return out
}

// checkElementNames rejects a registered element whose map KEY and
// ElementDef.Name disagree.
//
// THE TWO ARE USED AS IF THEY WERE ONE and nothing made them be. The
// shadowing above is keyed on the map key, the builtin loop tests
// `shadowed[d.Name]`, and granting()'s caller matches `d.Name ==
// parentName`. Register `Elements["Table"] = &ElementDef{Name: "Grid"}`
// and every one of those reads a different string: the builtin <Grid>
// is NOT shadowed, so it and the host def both land in the list, both
// granting Grid.Row, and `attached[a.Name]` resolves to whichever the
// map yielded last. Context.Catalog has the identical split.
//
// The mismatch is a host bug in every case — buildComponent looks up
// Context.Elements BY THE KEY, so a def whose Name is something else
// can never match the element it is registered under, and its grant
// silently never applies. There is no reading under which the two
// should differ, which is why this rejects rather than picking one.
//
// It is a LOAD error, in keeping with the rule that everything
// resolvable fails at load rather than as a surprise on click.
func (ctx *Context) checkElementNames() error {
	names := make([]string, 0, len(ctx.Elements))
	for name, d := range ctx.Elements {
		if d == nil || d.Name == "" || d.Name == name {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names) // a map range would name an arbitrary one first
	n := names[0]
	return fmt.Errorf(
		"markup: Context.Elements[%q] declares Name %q — the key and the Name must "+
			"agree, because elements are looked up by the key and their grants are "+
			"matched by the Name, so a mismatch registers a vocabulary nothing can "+
			"reach", n, ctx.Elements[n].Name)
}

// spec finds the catalog entry for an element name in this context.
//
// The order mirrors buildComponent's, and it has to: a spec describing a
// different element from the one that will build is worse than no spec,
// because the attribute check would then reject valid markup.
func (ctx *Context) spec(name string) (ElementSpec, bool) {
	// A host element with a DECLARATION is checkable like any built-in.
	// This is the half of Context.Elements that matters most — an
	// unknown attribute on a registered component used to be ignored
	// forever, and the near-miss suggestion works here for free.
	// AND NIL IS NOT REGISTERED. A nil *ElementDef declares nothing, so
	// there is no spec to answer with — and specAs on it is a nil
	// dereference INSIDE A LOAD, which is what this used to be.
	// Measured before: Context{Elements: {"Leafy": nil}} panicked from
	// both Build and Catalog. checkElementNames lets a nil through
	// deliberately (refusing it would make a pre-parse guard on the
	// grant vocabulary into a validator of registration hygiene, which
	// its own doc argues), so the shape reaches here and has to be
	// survivable rather than sanctioned: treating it as unregistered
	// makes the element unknown and the load fails by name. Same class
	// as the d.Build == nil dereference noBuild turned into a sentence.
	// Raised in review of #486.
	if d, ok := ctx.Elements[name]; ok && d != nil {
		sp := d.specAs(OriginRegistered)
		// THE REGISTRY KEY IS THE ELEMENT'S NAME WHEN THE DEF DOES NOT
		// CARRY ONE, and checkElementNames explicitly permits that: its
		// loop is `if d == nil || d.Name == "" || d.Name == name`, so a
		// def registered as Elements["Leafy"] with no Name is legal and
		// specAs copies the empty string straight through.
		//
		// Every refusal message in this file reads spec.Name, and the
		// clause beside it reads e.Name, so the pair rendered "<Holder>
		// reads <> as data" and "< Frob=…">, if <> takes one". The same
		// empty key silently missed reservedOnContent[spec.Name] and
		// namesChild(p, spec.Name). Defaulting HERE fixes the lookups
		// and the messages together, where threading e.Name would have
		// fixed only the sentences it was threaded into. Raised in
		// review of #486.
		if sp.Name == "" {
			sp.Name = name
		}
		return sp, true
	}
	if _, custom := ctx.Components[name]; custom {
		return ElementSpec{}, false
	}
	if d, ok := elementDefs[name]; ok {
		return d.spec(), true
	}
	return ElementSpec{}, false
}

// suggest offers the closest spelling when one is close enough to be
// worth printing. The motivating case is Left vs Canvas.Left, where the
// answer differs by a prefix rather than by a letter, so a suffix match
// is checked before edit distance.
func suggest(name string, allowed map[string]bool, attached map[string]string) string {
	for a, parent := range attached {
		if strings.HasSuffix(a, "."+name) {
			return fmt.Sprintf("; did you mean %s? (it is contributed by a <%s> parent)", a, parent)
		}
	}
	var best string
	bestD := 3 // never suggest something more than two edits away
	for a := range allowed {
		if strings.HasSuffix(a, "."+name) {
			return fmt.Sprintf("; did you mean %s?", a)
		}
		if d := distance(name, a); d < bestD {
			best, bestD = a, d
		}
	}
	if best != "" {
		return fmt.Sprintf("; did you mean %s?", best)
	}
	names := make([]string, 0, len(allowed))
	for a := range allowed {
		names = append(names, a)
	}
	sort.Strings(names)
	return "; this element takes " + strings.Join(names, ", ")
}

// distance is Levenshtein, case-insensitive, capped by the caller's
// threshold rather than by an early exit — the strings are attribute
// names, so they are short.
func distance(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
