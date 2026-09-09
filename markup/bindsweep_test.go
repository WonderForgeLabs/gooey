package markup

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// #460. An AttrSpec's Kind and Binds were verified for ELEVEN attributes
// and unverified for the rest.
//
// catalogen.Check — what TestDeclaredVocabularyMatchesTheCode runs —
// cross-checks that attributes EXIST and are READ. It says nothing about
// Kind or Binds. The only thing that did was
// TestCatalogKnowsTheElementsItMustKnow, a hand-written list of eleven
// rows, one per extraction rule rather than one per attribute. So
// corrupting <Frozen Active> from KindBinding/BindsBinding to
// KindText/BindsEither passed the whole suite: Active is not on the list,
// and Bound[T] refuses a literal whatever the declaration says, so the
// declaration and the builder are each other's backstop and mutating
// either alone is silent (#408's lesson, in a new place).
//
// The declaration is not decoration. It feeds the catalog and through it
// the wysiwyg palette, whose entire job is to emit markup that LOADS — so
// a wrong Binds means the palette offers a literal for an attribute that
// will refuse one, and the author gets non-loading markup out of the UI
// meant to prevent exactly that. For a third-party element registered
// through Context.Elements it is worse: its builder need not use Bound[T]
// at all, and then the declaration is the only statement of the rule.
//
// These sweeps are DERIVED. Adding rows to the eleven would restore green
// while leaving intact the mechanism that lost this one — the failure
// CLAUDE.md's Verify section describes, where the loop still exits 0
// while the new thing goes unchecked.

// sweepTarget is one attribute and the element to probe it on.
type sweepTarget struct {
	def  *ElementDef
	attr AttrSpec
}

// sweepTargets is EVERY declared attribute in the vocabulary — and the
// count of tables is the finding.
//
// The three sweeps below iterated def.Attrs only, which is one table of
// three: universalAttrs (Width, Height, Tooltip, Visibility and four
// more) and the attached tables (Grid.*, Canvas.*) belong to no element
// and were never reached. Corrupting <Visibility> from BindsEither to
// BindsLiteral left all three arms green — the same one-table blind spot
// that let #460 through in the first place, reproduced inside the guard
// written for it. Raised in review of #470.
//
// The representative for the two element-less tables is a <Border> and
// not a <Text>, for the reason declaredDefaults spells out
// (defaults_test.go): a Text paints at the left edge of whatever bounds
// it gets, so a change that does not move that edge is invisible to it.
// Nothing here renders, but sharing the choice keeps one answer to
// "which element stands in for the universal table".
func sweepTargets(t *testing.T) []sweepTarget {
	t.Helper()
	var out []sweepTarget
	for _, def := range definedElements() {
		if def.Opaque != "" {
			continue
		}
		for _, a := range def.Attrs {
			out = append(out, sweepTarget{def, a})
		}
	}
	box := elementDefs["Border"]
	if box == nil {
		t.Fatal("no <Border> to stand in for the universal and attached tables")
	}
	for _, a := range universalAttrs {
		out = append(out, sweepTarget{box, a})
	}
	for _, parent := range AttachedParents() {
		for _, a := range AttachedAttrs(parent) {
			out = append(out, sweepTarget{box, a})
		}
	}
	return out
}

// TestTheSweepCoversAllThreeTables is the floor under sweepTargets, and
// it is derived from the same tables rather than from a number.
//
// Losing the universal and attached tables is otherwise SILENT: every arm
// simply has less to iterate, every count shrinks, and nothing goes red —
// which is the shape of the original #460 defect and would be it
// returning inside the guard. Reading the tables independently is what
// makes "the sweep covers all three" a check instead of a claim.
// Raised in review of #470.
func TestTheSweepCoversAllThreeTables(t *testing.T) {
	seen := map[string]bool{}
	for _, tg := range sweepTargets(t) {
		seen[tg.attr.Name] = true
	}
	for _, a := range universalAttrs {
		if !seen[a.Name] {
			t.Errorf("the universal attribute %q is in no sweep target, so nothing "+
				"checks its Kind or Binds", a.Name)
		}
	}
	var attached int
	for _, parent := range AttachedParents() {
		for _, a := range AttachedAttrs(parent) {
			attached++
			if !seen[a.Name] {
				t.Errorf("the attached attribute %q (from <%s>) is in no sweep "+
					"target", a.Name, parent)
			}
		}
	}
	if len(universalAttrs) == 0 || attached == 0 {
		t.Fatal("the universal or attached table is empty, so this floor is vacuous")
	}
}

// bindSweep builds every attribute whose declared Binds is `want` with a
// value that must be refused, and reports how many refusals were the
// RIGHT ONE.
//
// THE PREDICATE IS THE REFUSAL, NOT THE FAILURE. This asserted only
// `err != nil` and counted every attribute it touched, so a build that
// failed for a harness reason satisfied the assertion for free while
// still incrementing the coverage number offered as the argument that the
// sweep is real. Measured in review of #470: arm 1 reported 23 and
// verified 20, arm 2 reported 22 and verified 10. Arm 3's own doc comment
// had already stated the rule this pair was breaking.
//
// An attribute that fails for another reason is neither a pass nor a
// failure — it is UNVERIFIED, and the names are logged rather than
// summarised, because "three unverified" tells the next reader nothing
// and "<Companion Error>, <FileWatcher Enabled>, <FileWatcher Path>"
// tells them exactly which declarations the harness cannot reach.
// RETURNS the unverified list rather than reporting it, for the same
// reason literalOnlySweep and boolSpellingSweep do: the harness self-test
// below drives this with a predicate no error can match, which makes
// EVERY declaration unverified on purpose. A floor inside the sweep would
// fire on the one caller that must not trip it.
func bindSweep(t *testing.T, want Binds, value func(AttrSpec) string, refusal func(AttrSpec, string) string) (checked int, unverified []string) {
	t.Helper()
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds != want {
			continue
		}
		v := value(a)
		if v == "" {
			continue
		}
		src := harnessFor(a.Name, probeElement(t, tg.def, a.Name, v))
		_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
		if err == nil {
			t.Errorf("<%s %s=%q> loaded, but %s is declared Binds=%q",
				tg.def.Name, a.Name, v, a.Name, want)
			continue
		}
		if !strings.Contains(err.Error(), refusal(a, v)) {
			unverified = append(unverified,
				fmt.Sprintf("<%s %s=%q>: %v", tg.def.Name, a.Name, v, err))
			continue
		}
		checked++
	}
	return checked, unverified
}

// TestEveryBindOnlyAttributeRefusesALiteral is the sweep the eleven-row
// list was standing in for. It is the one that would have caught the
// <Frozen Active> corruption: Active is BindsBinding, so a literal has to
// be a load error, and the declaration is what says so.
func TestEveryBindOnlyAttributeRefusesALiteral(t *testing.T) {
	n, unverified := bindSweep(t, BindsBinding, literalFor, func(AttrSpec, string) string {
		// Bound[T]'s own words (usercontrol.go), which fire exactly when
		// the loader demanded a handle. Any other error means the harness
		// could not reach the declaration.
		return "is not a binding expression"
	})
	reportUnverified(t, unverified)
	if n == 0 {
		t.Fatal("no bind-only attributes were checked: this sweep would pass vacuously")
	}
	t.Logf("checked %d bind-only attributes", n)
}

// TestEveryLiteralIntOrBoolAttributeRefusesGarbage is the other
// direction, and it is where the sweep found live bugs rather than
// confirming health.
//
// Ten sites read their literal with the error thrown away —
// `gap, _ := strconv.Atoi(e.Attrs["Gap"])` returns 0 on failure, and
// `e.Attrs["Uniform"] == "true"` makes every other value mean false. So
// `Gap="wide"`, `BarWidth="8px"`, `Thresholds="1"` and `Bold="{{.Loud}}"`
// all loaded clean and behaved as if omitted. litInt and litBool refuse
// them now.
//
// Restricted to KindInt and KindBool deliberately: for a TEXT attribute
// every string is a valid literal, including one shaped like a binding,
// so "unparseable" has no meaning there. Four such attributes accept
// `{{.S}}` as literal braces today — see the note at the bottom of this
// file.
func TestEveryLiteralIntOrBoolAttributeRefusesGarbage(t *testing.T) {
	n, unverified := bindSweep(t, BindsLiteral, func(a AttrSpec) string {
		switch a.Kind {
		case KindInt, KindBool:
			return "{{.S}}" // unparseable as either, and the shape an author would try
		}
		return ""
	}, func(a AttrSpec, v string) string {
		// NOT one fixed sentence here, because there is legitimately more
		// than one refusal: litInt and litBool say their piece, and the
		// universal ints go through applyLayout's own parser. What every
		// real refusal has and no harness error has is the OFFENDING
		// VALUE, quoted back. "<Companion> takes no children" does not
		// contain `{{.S}}`; every message about the attribute does.
		return v
	})
	reportUnverified(t, unverified)
	if n == 0 {
		t.Fatal("no literal int/bool attributes were checked: this sweep would pass vacuously")
	}
	t.Logf("checked %d literal int/bool attributes", n)
}

// TestEveryLiteralOnlyAttributeRefusesABinding is the FOURTH direction,
// and without it the sweep had a hole exactly where #460's own mutation
// lands.
//
// Corrupting <Visibility> from BindsEither to BindsLiteral was silent
// against the first three arms and stayed silent after they were extended
// over the universal table, because none of them asks the question a
// BindsLiteral declaration actually makes: that a BINDING is refused
// here. Arm 1 asks it of BindsBinding, arm 3 asks the converse of
// BindsLiteral — and BindsLiteral is the set a widened declaration moves
// INTO, which is the same reasoning that produced arm 3 in the first
// place, one Binds value across.
//
// KIND-RESTRICTED, and the restriction is not a convenience. For a TEXT
// attribute `{{.S}}` is a valid literal — the four attributes named at
// the bottom of this file accept it as braces — so "refuses a binding"
// is not a question that can be asked there at all. These are the kinds
// where a binding is distinguishable from a literal by parsing.
// Raised in review of #470.
func TestEveryLiteralOnlyAttributeRefusesABinding(t *testing.T) {
	checked, unverified, accepted := literalOnlySweep(t, ruleRefusedIt)
	for _, s := range accepted {
		t.Error(s)
	}
	if checked == 0 {
		t.Fatal("no literal-only attributes were checked: this sweep would pass vacuously")
	}
	reportUnverified(t, unverified)
	t.Logf("checked %d literal-only attributes", checked)
}

// ruleRefusedIt is the discriminator the two hand-rolled arms share with
// bindSweep: a refusal is the RULE's when it quotes the offending value
// back. No harness error contains "{{.B}}" or "TRUE"; every message
// about the attribute does.
//
// A VARIABLE the sweeps take as an argument, not a call they make, and
// that is the whole reason both arms are extracted below. bindSweep
// takes its predicate and TestBindSweepCountsOnlyTheRightRefusal feeds
// it one that can never match — which is the only thing that can see the
// discriminator being removed, because deleting it leaves every arm
// green and every number larger. These two arms hand-rolled their loops,
// so they inherited neither the check nor the test of it, and the review
// found them counting 8 of 13 and 4 of 47 refusals they never made.
// Raised in review of #470, twice.
//
// BOTH HALVES, and the review asked for the second one. A bare
// Contains(err, v) is looser than it reads: the probe values are "1",
// "0", "T" and "{{.S}}", and a one-character value appears in error text
// that has nothing to do with the attribute — a line number, a column
// count, another attribute's value, the word "T" inside a type name. The
// %q quoting is what makes the value a token rather than a substring, and
// the attribute NAME is what says the refusal is about THIS declaration
// rather than a sibling on the same probe element.
//
// It paid immediately: <Grid Rows> and <Grid Cols> dropped to UNVERIFIED
// because components.ParseGridLens answers `grid: bad length "{{.B}}"`,
// which names no attribute on an element that always has both. The loose
// form counted them — the value was in the string — and left an author
// told only that some string is bad. markup.gridLens now refuses in the
// house form and the count is back where it was, which is the shape of a
// tightening that found something rather than one that merely narrowed.
func ruleRefusedIt(err error, attr, v string) bool {
	msg := err.Error()
	return strings.Contains(msg, attr) && strings.Contains(msg, fmt.Sprintf("%q", v))
}

// TestTheDiscriminatorNeedsBothHalves is the arm the review named as
// missing, and it is a unit test of the predicate because nothing else
// can be.
//
// TestTheHandRolledArmsCountOnlyTheRightRefusal feeds the sweeps a
// predicate that never matches, which proves the discriminator is
// CONSULTED. It cannot prove the discriminator DISCRIMINATES: loosening
// it back to a bare substring match leaves every sweep green and every
// count identical — measured, SILENT against the whole package — because
// every refusal in the vocabulary happens to carry both halves today.
// That is the state the review measured and the reason it asked for the
// tightening anyway: the hole is not open, and nothing would notice it
// opening.
//
// So the errors here are SYNTHETIC, on purpose. A real one cannot express
// "carries the value but is about a different attribute" while the
// vocabulary is uniform, and building a probe element that produces one
// would pin the harness rather than the rule.
func TestTheDiscriminatorNeedsBothHalves(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
		want bool
	}{
		{
			// The shape every real refusal has.
			"the rule's own refusal",
			`markup: <Segmented Wrap="1">: Wrap takes "true" or "false"`,
			true,
		},
		{
			// A harness failure about a CHILD, carrying the value by
			// coincidence. probeElement injects <Text> children into most
			// elements, and the probe values are "1", "0", "T" and "TRUE" —
			// single characters and digits turn up everywhere.
			"someone else's error carrying the value",
			`markup: <HStack>: <Text> body "1" is not a component`,
			false,
		},
		{
			// Names the attribute, does not quote the value: a refusal
			// about the attribute for a reason that is not this value.
			"a refusal about the attribute but not the value",
			`markup: <Segmented Wrap>: Wrap needs a bool`,
			false,
		},
		{
			// The value UNQUOTED. This is the one that says the %q is
			// load-bearing rather than decoration: "1" appears inside
			// "31" and inside a line number.
			"the value as a bare substring",
			`markup: <Segmented Wrap>: at line 31`,
			false,
		},
	} {
		if got := ruleRefusedIt(errors.New(tc.msg), "Wrap", "1"); got != tc.want {
			t.Errorf("%s: ruleRefusedIt(%q) = %v, want %v.\n"+
				"A refusal counts only when it names the attribute AND quotes the "+
				"offending value; anything looser counts a harness failure as "+
				"coverage, which is the over-count this review found twice",
				tc.name, tc.msg, got, tc.want)
		}
	}
}

// literalOnlySweep is TestEveryLiteralOnlyAttributeRefusesABinding's
// body, with the discriminator injected. accepted holds the failures,
// unbuilt, so the caller reports them and a meta-test can ignore them.
func literalOnlySweep(t *testing.T, refused func(error, string, string) bool) (checked int, unverified, accepted []string) {
	t.Helper()
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds != BindsLiteral {
			continue
		}
		switch a.Kind {
		case KindInt, KindBool, KindEnum, KindDuration, KindColor, KindGesture, KindGridLens:
		default:
			continue
		}
		// THREE BINDINGS, and it takes all three. BindsLiteral says NO
		// binding is accepted, so one probe only asks about one type: a
		// single "{{.S}}" is refused by <Border Visibility> for being the
		// wrong TYPE, not for being a binding, and the corruption this arm
		// exists to catch — Visibility widened from BindsEither — hides
		// behind that refusal. bindVisibility takes a bool handle, so
		// "{{.B}}" is the probe that separates them.
		//
		// Not bindingFor: it insists on a resolvable handle of the
		// attribute's GoType, and a literal-only attribute like
		// <Border Chrome> has none — which is a t.Fatal there rather than
		// a skip here.
		var took, why string
		verified := false
		for _, v := range []string{"{{.S}}", "{{.I}}", "{{.B}}"} {
			src := harnessFor(a.Name, probeElement(t, tg.def, a.Name, v))
			_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
			if err == nil {
				took = v
				break
			}
			why = err.Error()
			if refused(err, a.Name, v) {
				verified = true
			}
		}
		if took == "" && !verified {
			unverified = append(unverified,
				fmt.Sprintf("<%s %s>: %v", tg.def.Name, a.Name, why))
			continue
		}
		if took != "" {
			accepted = append(accepted, fmt.Sprintf(
				"<%s %s=%q> loads, but %s is declared Binds=BindsLiteral. Either "+
					"the loader accepts a binding the catalog says it will not — so the "+
					"wysiwyg palette will refuse an author the binding the loader would "+
					"have taken — or the declaration should be BindsEither",
				tg.def.Name, a.Name, took, a.Name))
			continue
		}
		checked++
	}
	return checked, unverified, accepted
}

// TestALiteralBoolRefusesTheParseBoolSpellings is the strictness half,
// and it is derived because the laxity it guards against arrived by
// having two helpers rather than by having a wrong table.
//
// optBool read a component's bool through strconv.ParseBool while litBool
// read the same kind of attribute strictly, so <Segmented Wrap="1">
// loaded and <ProgressBar Thresholds="1"> did not — one vocabulary, two
// answers. Restoring ParseBool inside litBool is otherwise SILENT
// (measured): every other bool test uses `{{.S}}`, which ParseBool also
// refuses. "1" is the value that separates the two grammars.
// Raised in review of #470; the readers outside component attributes are
// #473.
func TestALiteralBoolRefusesTheParseBoolSpellings(t *testing.T) {
	checked, unverified, accepted := boolSpellingSweep(t, ruleRefusedIt)
	for _, s := range accepted {
		t.Error(s)
	}
	if checked == 0 {
		t.Fatal("no literal bool attributes were checked: this test would pass vacuously")
	}
	reportUnverified(t, unverified)
	t.Logf("checked %d literal bool attributes", checked)
}

// boolSpellingSweep is the body, with the discriminator injected. See
// ruleRefusedIt for why that is not an implementation detail.
func boolSpellingSweep(t *testing.T, refused func(error, string, string) bool) (checked int, unverified, accepted []string) {
	t.Helper()
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds != BindsLiteral || a.Kind != KindBool {
			continue
		}
		// SAME DISCRIMINATOR AS bindSweep, and this arm was the reason
		// the review found it missing twice. It asserted err != nil and
		// nothing else, so a probe the harness could not even reach
		// counted as a verified refusal: EIGHT of thirteen were, and
		// seven of those were <Validate>, failing on "<HStack> does not
		// support <Validate>" while this arm — whose entire subject is
		// the strictness of a bool — reported them checked.
		//
		// That is not a hypothetical over-count. It is what hid the bug
		// this arm exists for: parseRuleBool was still on
		// strconv.ParseBool, so <Validate Required="1"> loaded clean the
		// whole time. Give the attachment a real host and the arm goes
		// red; give it a real host and the discriminator and it is red
		// for a reason it can name.
		verified := false
		var why []string
		for _, v := range []string{"1", "0", "TRUE", "T"} {
			src := harnessFor(a.Name, probeElement(t, tg.def, a.Name, v))
			_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
			if err == nil {
				accepted = append(accepted, fmt.Sprintf(
					"<%s %s=%q> loads. A bool the document can spell five ways "+
						"is a bool that reads differently in two files, and on a "+
						"security switch like <Companion CleanEnv> the extra spellings "+
						"are extra near-misses", tg.def.Name, a.Name, v))
				continue
			}
			if refused(err, a.Name, v) {
				verified = true
				continue
			}
			why = append(why, fmt.Sprintf("<%s %s=%q>: %v", tg.def.Name, a.Name, v, err))
		}
		if !verified {
			unverified = append(unverified, why...)
			continue
		}
		checked++
	}
	return checked, unverified, accepted
}

// TestTheHandRolledArmsCountOnlyTheRightRefusal is
// TestBindSweepCountsOnlyTheRightRefusal for the two arms that could not
// use bindSweep.
//
// This is the test whose ABSENCE is the finding. Removing the
// discriminator from either arm leaves the whole package green and every
// logged count LARGER, which is exactly the state review of #470 found
// and nothing in the suite could see — the same reasoning that produced
// TestBindSweepCountsOnlyTheRightRefusal, one arm across, twice.
func TestTheHandRolledArmsCountOnlyTheRightRefusal(t *testing.T) {
	never := func(error, string, string) bool { return false }
	if n, _, _ := boolSpellingSweep(t, never); n != 0 {
		t.Errorf("boolSpellingSweep verified %d attributes against a refusal that can "+
			"never match, so it is counting build failures rather than refusals", n)
	}
	if n, _, _ := literalOnlySweep(t, never); n != 0 {
		t.Errorf("literalOnlySweep verified %d attributes against a refusal that can "+
			"never match, so it is counting build failures rather than refusals", n)
	}
}

// reportUnverified is every arm's report, and it FAILS.
//
// It used to log. The argument for logging was that an attribute the
// harness cannot reach is a gap in the harness rather than a defect in
// the declaration, and failing on it would make the sweep
// un-extendable — which was reasonable while there were nine such gaps
// and no way to close them.
//
// It is the wrong shape and review of #470 named why: a report nothing
// checks lets coverage shrink silently. An element that becomes
// unreachable makes every arm's count drop, the logged list grow, and
// the suite stay green — the exact silent-loop failure this file's
// header invokes CLAUDE.md for, one level up, on the numbers the file's
// whole coverage argument rests on.
//
// So the floor is ZERO and it is met today: closing the <Companion> and
// <FileWatcher> gaps took every arm to nought unverified, and the counts
// rose 22→23, 29→30, 44→47, 12→13. An attribute that genuinely cannot be
// probed does not get a quiet line in a log — it gets a named exemption
// with an open issue number, the mechanism "A red suite is yours" asks
// for, and there are none today.
func reportUnverified(t testing.TB, unverified []string) {
	t.Helper()
	if len(unverified) == 0 {
		return
	}
	t.Errorf("%d declarations UNVERIFIED — the build failed for another reason, so "+
		"the rule itself was not exercised on them and this arm's coverage count "+
		"is that much smaller than it reads. Reach them, or record a named "+
		"exemption with an open issue number:\n\t%s",
		len(unverified), strings.Join(unverified, "\n\t"))
}

// countingTB records whether reportUnverified reported, without failing
// the test that is asking. A subtest cannot do this job: a failing
// subtest fails its parent, which is the answer being measured.
type countingTB struct {
	testing.TB
	errs int
}

func (c *countingTB) Helper()               {}
func (c *countingTB) Errorf(string, ...any) { c.errs++ }
func (c *countingTB) Logf(string, ...any)   {}

// TestTheUnverifiedFloorIsAFloor is the arm on the arm.
//
// reportUnverified FAILING is the whole of the change it carries, and
// nothing else in this file can see it: every sweep passes it an empty
// slice today, so switching it back to t.Logf is SILENT — measured
// against the whole package. Both directions are asserted, because
// "reports something" is satisfied by a function that reports always.
func TestTheUnverifiedFloorIsAFloor(t *testing.T) {
	var full countingTB
	reportUnverified(&full, []string{"<Nonesuch Attr>: a harness failure"})
	if full.errs == 0 {
		t.Error("reportUnverified accepted a non-empty unverified list. Coverage can " +
			"then shrink to nothing with every arm still green, which is the " +
			"failure the counts in this file exist to make visible")
	}

	var empty countingTB
	reportUnverified(&empty, nil)
	if empty.errs != 0 {
		t.Error("reportUnverified failed on an EMPTY list, so the arm above would " +
			"pass for a function that fails unconditionally")
	}
}

// TestBindSweepCountsOnlyTheRightRefusal is a test OF THE HARNESS, and it
// is here because the harness is the thing this file's coverage argument
// rests on.
//
// bindSweep's whole correction in review of #470 was to stop counting a
// build that failed for an unrelated reason. Reverting that — dropping
// the refusal-text check — leaves every arm green and every number
// larger, which is precisely the state the review measured and the state
// nothing in the suite could see. Feeding it a predicate that can never
// match is the one thing that can.
func TestBindSweepCountsOnlyTheRightRefusal(t *testing.T) {
	never := func(AttrSpec, string) string { return "\x00 no error says this" }
	if n, _ := bindSweep(t, BindsBinding, literalFor, never); n != 0 {
		t.Errorf("bindSweep verified %d attributes against a refusal no error can "+
			"contain, so it is counting build failures rather than refusals — "+
			"and every coverage number this file logs is then an overstatement", n)
	}
}

// validLiteralFor is a per-KIND table, not a per-attribute one. That
// distinction is the whole point: Kind is a closed set the type system
// already names, so this cannot go stale the way a list of attributes
// does — a new Kind fails to compile its way past the switch, a new
// attribute does not need a row.
func validLiteralFor(a AttrSpec) string {
	switch a.Kind {
	case KindDuration:
		return "50ms"
	case KindInt:
		return "1"
	case KindBool:
		return "true"
	case KindGesture:
		return "ctrl+p"
	case KindColor:
		return "#4a9"
	case KindStyle:
		return "probe" // registered by defaultsContext
	case KindGridLens:
		return "1*"
	case KindEnum:
		if len(a.Enum) > 0 {
			return a.Enum[0]
		}
		return ""
	}
	return "x"
}

// TestEveryAttributeThatSaysItTakesALiteralAcceptsOne is the arm that
// actually closes #460, and the first version of this file did not have
// it — which the issue's own mutation caught.
//
// A sweep that iterates `Binds == BindsBinding` READS THE DECLARATION IT
// IS VERIFYING. Corrupt <Frozen Active> from BindsBinding to BindsEither
// and it simply drops out of that sweep: nothing is checked, nothing goes
// red, and the guard written to catch that mutation is silent on it. Only
// the converse closes the loop — an attribute that CLAIMS to take a
// literal must actually take one — because that is the set the corrupted
// declaration moves INTO.
//
// THE PREDICATE IS THE REFUSAL TEXT, not build success. Plenty of valid
// literals are rejected here for reasons that are nothing to do with
// Binds: Click="x" needs a registered handler, Allow="x" is not one of
// the categories. Those are harness limits. The one failure that means
// "the declaration is wrong" is Bound[T]'s — `%q is not a binding
// expression` (usercontrol.go:372) — which fires exactly when the loader
// demanded a handle where the catalog promised a literal.
//
// NO PRE-FILTER. An earlier version built each element with the
// attribute OMITTED first and skipped it when that failed, to exclude
// elements the harness cannot reach. That gate dropped 35 of 100
// attributes and bought nothing — the predicate below is already the
// specific refusal, so an unreachable element simply produces a different
// error and falls through the `if` harmlessly. It was a second, weaker
// filter standing in front of one that works.
//
// And the 35 were not unreachable elements. probeElement omits a REQUIRED
// attribute when it is the one under test, so every required
// BindsLiteral/BindsEither attribute failed the gate by construction:
// <Timer Interval>, <Segmented Wrap>, <MenuBar>, <KeyBinding> and
// <TypeAhead> are all perfectly reachable WITH a value and dropped out
// only because omitting them is a load error. The gate excluded the
// attributes whose declaration matters most. Raised in review of #470.
func TestEveryAttributeThatSaysItTakesALiteralAcceptsOne(t *testing.T) {
	var checked int
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds != BindsLiteral && a.Binds != BindsEither {
			continue
		}
		v := validLiteralFor(a)
		if v == "" {
			continue
		}
		checked++
		src := harnessFor(a.Name, probeElement(t, tg.def, a.Name, v))
		_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), "is not a binding expression") {
			t.Errorf("<%s %s=%q> is declared Binds=%q, but the loader requires "+
				"a binding:\n\t%v", tg.def.Name, a.Name, v, a.Binds, err)
		}
	}
	if checked == 0 {
		t.Fatal("no literal-taking attributes were checked: this sweep would pass vacuously")
	}
	t.Logf("checked %d literal-taking attributes", checked)
}

// TestTheRefusalNamesTheAttributeAndTheSilentConsequence. A load error
// that does not say WHICH attribute, and what would have happened
// instead, sends the author looking at the wrong line — and "it laid out
// as 0" is the part they cannot deduce.
func TestTheRefusalNamesTheAttributeAndTheSilentConsequence(t *testing.T) {
	for _, tc := range []struct{ src, attr, consequence string }{
		{`<VStack Gap="wide"><Text>a</Text></VStack>`, "Gap", `Gap="0"`},
		{`<ButtonBar Uniform="yes"><Button Content="a"/></ButtonBar>`, "Uniform", `"false"`},
		{`<Text Bold="{{.B}}">ab</Text>`, "Bold", `"false"`},
	} {
		_, err := Build([]byte("<Gooey>"+tc.src+"</Gooey>"), defaultsContext())
		if err == nil {
			t.Errorf("%s loaded clean", tc.src)
			continue
		}
		if !strings.Contains(err.Error(), tc.attr) {
			t.Errorf("the refusal for %s does not name the attribute:\n\t%v", tc.src, err)
		}
		if !strings.Contains(err.Error(), tc.consequence) {
			t.Errorf("the refusal for %s does not say what it would have done "+
				"silently (%s):\n\t%v", tc.src, tc.consequence, err)
		}
	}
}

// TestALiteralIntOrBoolStillLoads is what stops the two sweeps above
// being satisfied by refusing everything.
func TestALiteralIntOrBoolStillLoads(t *testing.T) {
	for _, src := range []string{
		`<VStack Gap="2"><Text>a</Text></VStack>`,
		`<HStack Gap="0"><Text>a</Text></HStack>`,
		`<Text Bold="true">ab</Text>`,
		`<Text Bold="false">ab</Text>`,
		`<ButtonBar Uniform="true"><Button Content="a"/></ButtonBar>`,
		`<Text>ab</Text>`, // omitted entirely: the declared Default
	} {
		if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err != nil {
			t.Errorf("%s is refused: %v", src, err)
		}
	}
}

// TestAnEmptyLiteralIsALoadError pins the third state, and it is the
// OPPOSITE of what this test used to assert.
//
// It was TestAnEmptyLiteralMeansOmitted, enumerating <VStack Gap=""> and
// <Text Bold="">, and it was wrong twice over.
//
// WRONG ABOUT THE TREE. Height is both a Sparkline attribute and a
// universal one, so applyLayout parses it as well — <Sparkline
// BarWidth=""> loaded while <Sparkline Height=""> was a load error, the
// same element answering two ways about the same empty string, and
// Width="" and Margin="" were errors on main all along. Being enumerated
// rather than swept, the test picked the two attributes that agreed with
// it. That is finding 3 with a live consequence.
//
// WRONG ABOUT THE REASON. Both it and litInt justified accepting empty as
// "how the designer writes 'not set'". The designer does the opposite:
// apps/wysiwyg deletes the attribute when its value goes empty and never
// emits X="". A rationale citing a mechanism that does the reverse is
// worse than none, because it reads as evidence.
//
// So empty is a load error everywhere, which is the answer the universal
// ints already gave, and it is DERIVED — every literal int and bool in
// the vocabulary, not two of them. Raised in review of #470.
func TestAnEmptyLiteralIsALoadError(t *testing.T) {
	var checked int
	var unverified []string
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds != BindsLiteral {
			continue
		}
		if a.Kind != KindInt && a.Kind != KindBool {
			continue
		}
		src := harnessFor(a.Name, withEmptyAttr(probeElement(t, tg.def, a.Name, ""), a.Name))
		_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
		if err == nil {
			t.Errorf("<%s %s=\"\"> loads. An empty literal is a typo, not an "+
				"omission — the designer deletes an attribute it has no value "+
				"for — and Width=\"\" has always been a load error, so accepting "+
				"it here makes one vocabulary answer two ways", tg.def.Name, a.Name)
			continue
		}
		// The refusal has to NAME the attribute, which is also what
		// separates it from a harness failure. An error that does not is
		// unverified rather than wrong: <Companion> cannot be built here
		// at all, and its message is about children.
		if !strings.Contains(err.Error(), a.Name) {
			unverified = append(unverified,
				fmt.Sprintf("<%s %s=\"\">: %v", tg.def.Name, a.Name, err))
			continue
		}
		checked++
	}
	reportUnverified(t, unverified)
	if checked == 0 {
		t.Fatal("no literal int/bool attributes were checked: this test would pass vacuously")
	}
	t.Logf("checked %d literal int/bool attributes", checked)
}

// withEmptyAttr writes `Attr=""` onto an element probeElement built with
// that attribute omitted. It cannot go through probeElement's value
// parameter, because there an empty string IS the omission — which is the
// distinction this whole test is about.
func withEmptyAttr(el, attr string) string {
	i := strings.IndexAny(el, " >")
	return el[:i] + " " + attr + `=""` + el[i:]
}

// TestOmittingTheAttributeStillMeansTheDefault is the discrimination half
// of the test above: without it, "empty is an error" could be satisfied
// by an attribute that is required, and the third state would collapse
// into two.
func TestOmittingTheAttributeStillMeansTheDefault(t *testing.T) {
	for _, src := range []string{
		`<VStack><Text>a</Text></VStack>`,
		`<Text>ab</Text>`,
		`<ButtonBar><Button Content="a"/></ButtonBar>`,
	} {
		if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err != nil {
			t.Errorf("%s is refused, but an OMITTED attribute means its declared "+
				"Default: %v", src, err)
		}
	}
}

// litIntSweep runs one spelling against EVERY declared literal int in the
// vocabulary and reports how many refusals named the attribute.
//
// Derived rather than enumerated, and the two arms below were the last
// enumerated ones in this file. That is not tidiness: <Width>, <Height>,
// <Grid.*> and <Canvas.*> read their value in applyLayout, not in litInt,
// so a hand-written list of <VStack Gap="-3"> rows could not see that
// <Border Width="-3"> LOADED and was then silently treated as auto —
// #460's own defect definition, in the table this file's sweepTargets was
// extended to cover. Review of #470 measured it; the swept form goes red
// on it immediately.
func litIntSweep(t *testing.T, value string) (checked int, unverified, accepted []string) {
	t.Helper()
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds != BindsLiteral || a.Kind != KindInt {
			continue
		}
		src := harnessFor(a.Name, probeElement(t, tg.def, a.Name, value))
		_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
		if err == nil {
			accepted = append(accepted, fmt.Sprintf("<%s %s=%q>", tg.def.Name, a.Name, value))
			continue
		}
		// THE VALUE, QUOTED, is what separates a refusal about this
		// attribute from a harness failure — the same discriminator arm 2
		// uses, and for the same reason.
		if !strings.Contains(err.Error(), fmt.Sprintf("%q", value)) {
			unverified = append(unverified,
				fmt.Sprintf("<%s %s=%q>: %v", tg.def.Name, a.Name, value, err))
			continue
		}
		checked++
	}
	return checked, unverified, accepted
}

// TestALiteralExtentCannotBeNegative is the range half of litInt, and it
// is a separate test because it is a separate claim: unreadable and
// out-of-range are two ways to be silently wrong, and the first version
// of the helper refused only the first.
//
// Every literal int is a measurement in cells. <VStack Gap="-3"> parsed
// clean, reached `y += v.Gap` and overlapped the children the gap exists
// to separate; <Gauge BarWidth="-5"> handed layout a gooey.Size{W: -5};
// <Border Width="-3"> loaded and behaved exactly as if the attribute had
// been omitted, because layout guards on l.Width > 0.
func TestALiteralExtentCannotBeNegative(t *testing.T) {
	checked, unverified, accepted := litIntSweep(t, "-3")
	reportUnverified(t, unverified)
	for _, s := range accepted {
		t.Errorf("%s loads. A negative measurement parses, so nothing else would "+
			"refuse it: layout quietly overlaps what it was meant to separate, "+
			"addresses no cell, or places a child outside the rect that clips it", s)
	}
	if checked == 0 {
		t.Fatal("no literal ints were checked: this sweep would pass vacuously")
	}
	t.Logf("checked %d literal ints against a negative spelling", checked)
}

// TestALiteralIntHasOneSpelling is the canonical-form half.
//
// strconv.Atoi accepts a leading sign and leading zeros, so Gap="+3" and
// Gap="007" both loaded and meant 3 and 7 while Gap="-3" was refused — a
// "refused" int with two silent second spellings, in the change whose
// argument is that one value has one grammar. The first fix named `+` and
// missed `007`; litInt compares against strconv.Itoa's output now, which
// is the parser's own inverse and cannot drift from it.
func TestALiteralIntHasOneSpelling(t *testing.T) {
	for _, v := range []string{"+3", "007", "-0"} {
		t.Run(v, func(t *testing.T) {
			checked, unverified, accepted := litIntSweep(t, v)
			reportUnverified(t, unverified)
			for _, s := range accepted {
				t.Errorf("%s loads. A whole number written literally has one "+
					"spelling; a second one that means the same thing is a value "+
					"two documents disagree about", s)
			}
			if checked == 0 {
				t.Fatal("no literal ints were checked: this sweep would pass vacuously")
			}
		})
	}
}

// TestALiteralIntAcceptsSurroundingSpace is the accept direction, and it
// is the arm that found the sharpest asymmetry in review of #470.
//
// litInt trims. applyLayout did not — and Height sits in BOTH tables, so
// <Sparkline Height=" 2 "/> was a load error EVEN THOUGH litInt trims,
// because the same attribute was read again, untrimmed, on the way out.
// litInt's trim was dead on it. Without this arm the two readers can
// disagree again and every refusal arm above stays green, since they only
// ever ask what is REFUSED.
func TestALiteralIntAcceptsSurroundingSpace(t *testing.T) {
	var checked int
	var refused []string
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds != BindsLiteral || a.Kind != KindInt {
			continue
		}
		// The seed's own value where there is one, so an attribute whose
		// number has to mean something still gets one that does; " 1 "
		// otherwise, which every extent, count and index accepts.
		v := seedValue(tg.def.Seed, a.Name)
		if v == "" {
			v = "1"
		}
		src := harnessFor(a.Name, probeElement(t, tg.def, a.Name, " "+v+" "))
		if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err != nil {
			refused = append(refused,
				fmt.Sprintf("<%s %s=%q>: %v", tg.def.Name, a.Name, " "+v+" ", err))
			continue
		}
		checked++
	}
	for _, s := range refused {
		t.Errorf("%s is refused. litInt trims, so an attribute that does not is "+
			"being read by a second parser — which is how <Border Height=\" 2 \"> "+
			"and <Sparkline Height=\" 2 \"> came to answer differently about the "+
			"same string:\n\t%s", s, s)
	}
	if checked == 0 {
		t.Fatal("no literal ints were checked: this sweep would pass vacuously")
	}
	t.Logf("checked %d literal ints for a trimmed spelling", checked)
}

// STILL UNSWEPT, ONE: a Kind WIDENED TO THE CATCH-ALL. Corrupting
// <Width> from KindInt to KindString is silent against every arm here —
// measured, not assumed. The int arms stop iterating it, and no arm can
// replace them, because KindString is what this vocabulary uses for a
// STRUCTURED literal with no Kind of its own: <Margin> is KindString and
// takes "1", "1,2" or "1,2,3,4", so "a KindString attribute accepts any
// string" is false for a legitimate declaration and cannot be the check.
//
// The direction is real — a widened Kind reaches the wysiwyg properties
// pane as a free text box where a number belonged — and closing it means
// giving the structured literals their own Kinds rather than writing a
// cleverer sweep. Recorded rather than guessed at, so the next reader
// does not take the arms below for total coverage.
// Raised in review of #470.

// STILL UNSWEPT, TWO: four BindsLiteral TEXT attributes
// accept a binding expression as literal braces —
//
//	<ButtonBar Separator="{{.S}}">
//	<StatusBar Left="{{.S}}">  Center=  Right=
//
// For a text attribute `{{.S}}` IS a valid literal, so this is not the
// dropped-error bug above; it is a question about the DECLARATION. Either
// those attributes should be BindsEither — a status bar's text is exactly
// the thing an app wants bound — or the loader should refuse a
// binding-shaped literal there. That is a design call, not a fix, so it
// is filed rather than decided here.
