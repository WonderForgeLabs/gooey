package markup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
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
		// THE FOURTH EXCLUSION, and the three tables below it each have
		// a floor while this had none. <Tab> is the only opaque def
		// today and declares no Attrs, so nothing is actually dropped —
		// which is the state that makes this worth writing down rather
		// than the state that makes it safe. An opaque element that
		// ever declares one would leave every arm green with a smaller
		// count, silently. TestNoOpaqueElementDeclaresAnAttribute is
		// the floor. Raised in review of #470.
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

// TestNoOpaqueElementDeclaresAnAttribute is the floor under
// sweepTargets' fourth exclusion.
//
// It is deliberately a HARD floor rather than a log. An opaque element
// that declares an attribute is not a thing to be reported quietly at
// the bottom of a passing run; it is a decision — either the sweep
// learns to build that element, or the exclusion earns a documented
// reason — and nobody makes a decision they were not stopped for.
//
// This is the same argument the three table floors make, which is the
// finding: the exclusion this guards was the one without one.
func TestNoOpaqueElementDeclaresAnAttribute(t *testing.T) {
	seen := 0
	for _, def := range definedElements() {
		if def.Opaque == "" {
			continue
		}
		seen++
		if len(def.Attrs) > 0 {
			names := make([]string, 0, len(def.Attrs))
			for _, a := range def.Attrs {
				names = append(names, a.Name)
			}
			t.Errorf("<%s> is opaque (%s) and declares %v, which sweepTargets "+
				"drops — every arm in this file would stay green with a smaller "+
				"count. Teach the sweep to build it, or record why it cannot be "+
				"probed", def.Name, def.Opaque, names)
		}
	}
	if seen == 0 {
		t.Fatal("no opaque element in the vocabulary, so this floor passes " +
			"vacuously and the exclusion it guards is dead code")
	}
}

// TestEverySweptTextKindRowIsReached keeps sweptDespiteTextKind from
// outliving what it opts in.
//
// Two ways a row goes stale, and the second is the quiet one: the
// attribute stops being declared, or it is given a Kind the switch in
// literalOnlySweep already covers — at which point the row opts in
// something that was coming anyway and reads as a considered exception
// to a rule that no longer needs one. Both are checked.
func TestEverySweptTextKindRowIsReached(t *testing.T) {
	kinds := map[string]Kind{}
	for _, tg := range sweepTargets(t) {
		kinds[tg.def.Name+"."+tg.attr.Name] = tg.attr.Kind
	}
	if len(kinds) == 0 {
		t.Fatal("no declarations found: this guard would pass vacuously")
	}
	for key, why := range sweptDespiteTextKind {
		k, ok := kinds[key]
		if !ok {
			t.Errorf("sweptDespiteTextKind has a row for %s (%s), which no element "+
				"declares any more", key, why)
			continue
		}
		switch k {
		case KindInt, KindBool, KindEnum, KindDuration, KindColor, KindGesture, KindGridLens:
			t.Errorf("sweptDespiteTextKind opts %s in (%s), but its Kind is %v, "+
				"which literalOnlySweep already sweeps — the row narrows nothing "+
				"and should go", key, why, k)
		}
	}
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
//
// ONE PREDICATE SHAPE for every arm, which is finding 6 of review #470.
// This took a func returning a SUBSTRING and matched it bare — the loose
// form TestTheDiscriminatorNeedsBothHalves argues at length is
// insufficient, in the file that argues it. Measured before the change:
// loose 30, strict 30, so nothing was over-counted; that is the same
// state ruleRefusedIt was in before it was tightened, and the same
// reason to tighten it — the hole is not open, and nothing would notice
// it opening.
func bindSweep(t *testing.T, want Binds, value func(AttrSpec) string, refused func(error, string, string) bool) (checked int, unverified []string) {
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
		if !refused(err, a.Name, v) {
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
	n, unverified := bindSweep(t, BindsBinding, literalFor, func(err error, attr, _ string) bool {
		// Bound[T]'s own words (usercontrol.go), which fire exactly when
		// the loader demanded a handle. Any other error means the harness
		// could not reach the declaration — AND the refusal has to name
		// the attribute, or it is some other attribute's on the same
		// probe element.
		msg := err.Error()
		return strings.Contains(msg, "is not a binding expression") &&
			strings.Contains(msg, attr)
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
	}, ruleRefusedIt)
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
	// EVERY TARGET THE RULE ADMITS WAS REACHED, which `checked != 0`
	// does not ask and the log line certainly does not.
	//
	// The opt-in added below for the two float bounds is a lookup inside
	// literalOnlySweep, and deleting it leaves the table in place and
	// the count two lower — a silent cap, the exact shape finding 5 of
	// this round was about, introduced by the fix for finding 4. So the
	// expectation is RE-DERIVED from the vocabulary rather than written
	// down: literalOnlyExpected states the admission rule a second time,
	// and a mutation has to be made in both places to stay quiet.
	//
	// Summed across all three buckets because this asks what the sweep
	// REACHED, not what it verified; the outcome of each is the two
	// checks either side of this one.
	if want := literalOnlyExpected(t); checked+len(unverified)+len(accepted) != want {
		t.Errorf("the sweep reached %d literal-only attributes (%d verified, "+
			"%d unverified, %d accepted); the vocabulary declares %d. A target "+
			"the sweep never visits leaves every arm in this file green with a "+
			"smaller count", checked+len(unverified)+len(accepted), checked,
			len(unverified), len(accepted), want)
	}
	reportUnverified(t, unverified)
	t.Logf("checked %d literal-only attributes", checked)
}

// literalOnlyExpected re-derives from the vocabulary how many attributes
// literalOnlySweep should reach.
//
// It is a DELIBERATE second statement of the admission rule, not a
// helper shared with the sweep — sharing one would make the check
// vacuous, since both sides would move together. The cost is that the
// two switches have to be kept in step by hand; that cost is the
// mechanism, because keeping them in step is a decision somebody is
// stopped to make. Raised in review of #470.
func literalOnlyExpected(t *testing.T) int {
	t.Helper()
	n := 0
	for _, tg := range sweepTargets(t) {
		if tg.attr.Binds != BindsLiteral {
			continue
		}
		switch tg.attr.Kind {
		case KindInt, KindBool, KindEnum, KindDuration, KindColor, KindGesture, KindGridLens:
			n++
		default:
			if _, ok := sweptDespiteTextKind[tg.def.Name+"."+tg.attr.Name]; ok {
				n++
			}
		}
	}
	return n
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

// refusedTheEmptyValue is the discriminator for the arms that probe an
// EMPTY value, and it is the attribute name alone — deliberately, and
// named rather than left as a weaker spelling of ruleRefusedIt inline.
//
// The value cannot be part of it. %q of the empty string is `""`, a
// token a correct refusal legitimately does not carry: <Timer Interval="">
// is refused by the REQUIRED-attribute check before any duration reader
// runs, and its message is `<Timer> needs an Interval (e.g.
// Interval="600ms")`. That is the right refusal for the right reason and
// requiring `""` would file it as unverified.
//
// The name is still a real discriminator here, because the harness
// failures this has to exclude do not carry it: "<HStack> does not
// support <Validate>" and "<Companion> takes no children" name the
// ELEMENT. Raised in review of #470, which asked for one shape — this
// is the second shape, with the reason it cannot be the first.
func refusedTheEmptyValue(err error, attr string) bool {
	return strings.Contains(err.Error(), attr)
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
			if _, ok := sweptDespiteTextKind[tg.def.Name+"."+a.Name]; !ok {
				continue
			}
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
// So the floor is ZERO. An attribute that genuinely cannot be probed
// does not get a quiet line in a log — it gets a named exemption with an
// open issue number, the mechanism "A red suite is yours" asks for, and
// there are none.
//
// THE COUNTS ARE NOT WRITTEN HERE, and they were: this comment listed
// four before-and-after pairs from the round that first met the floor,
// and the next round moved every one of them while the sentence went on
// asserting the old ones. That is the sample-taken-once shape CLAUDE.md
// refuses for module counts, in a comment whose whole subject is a
// number nobody re-derives. Each arm logs its own count on every run;
// read those. Raised in review of #470.
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
	never := func(error, string, string) bool { return false }
	if n, _ := bindSweep(t, BindsBinding, literalFor, never); n != 0 {
		t.Errorf("bindSweep verified %d attributes against a refusal no error can "+
			"contain, so it is counting build failures rather than refusals — "+
			"and every coverage number this file logs is then an overstatement", n)
	}
}

// sweptDespiteTextKind names the text-Kind attributes whose BINDING half
// this arm can still ask about, keyed "<Element>.<Attr>", with the
// reason.
//
// The Kind exclusion above is sound and is not being weakened: for a
// text attribute "{{.S}}" IS a valid literal, so "does it refuse a
// binding?" is not a question that can be asked — <Validate Pattern>
// compiles it as a regexp and is right to. The two float bounds are the
// case where the premise does not hold. They are KindString only
// because there is no KindFloat, and a binding is perfectly
// distinguishable there: ParseFloat refuses it, naming the attribute and
// quoting the value, so ruleRefusedIt verifies rather than shrugging.
//
// Without this, round 6 left two BindsLiteral declarations whose binding
// half no arm checked — the same gap arm 4 was written for after
// Visibility, arriving through a Kind choice instead of a Binds one.
// Raised in review of #470.
//
// It is a per-attribute table, so it carries the guard one needs:
// TestEverySweptTextKindRowIsReached fails on a row naming an attribute
// the vocabulary no longer declares as text, which is both halves — a
// deleted attribute AND one that has since been given a Kind the switch
// already covers, where the row would be silently doing nothing.
var sweptDespiteTextKind = map[string]string{
	"Validate.MinValue": "a float bound in a string-shaped attribute — there is " +
		"no KindFloat, and ParseFloat refuses a binding by name",
	"Validate.MaxValue": "the other half of the same range",
}

// narrowerThanItsKind names the attributes whose literal has a grammar
// their Kind cannot express, keyed "<Element>.<Attr>", with the reason.
//
// Kind is the grammar of the VALUE — a duration, a colour, a whole
// number — and it is the right default for exactly that reason. But some
// attributes are KindText or KindString and still refuse most text,
// because the string has to NAME something: a category from a closed
// vocabulary, a path in the page's FS, a binding path in the context, a
// number inside a string-shaped attribute. For those, "x" is a literal
// of nothing in particular. The probe still ran, the refusal landed in
// the unverified bucket, and the declaration went unchecked — which is
// why this file's one remaining arm was still logging a non-empty
// unverified list. Raised in review of #470.
//
// It is a per-attribute table and that is a cost, so it carries the
// guard a per-attribute table needs: TestEveryNarrowedLiteralIsReached
// fails on a key naming an attribute the vocabulary no longer declares,
// so a row cannot outlive its declaration.
var narrowerThanItsKind = map[string]struct {
	value func() string
	why   string
}{
	"Frozen.Allow":     {lit("Focus"), "a category from a closed vocabulary, not free text"},
	"Companion.Dir":    {lit("."), "a directory that has to exist when the element builds"},
	"Validate.Compare": {lit("S"), "a binding path resolved against the context"},
	"Validate.MinValue": {lit("1"), "a number, in an attribute whose Kind is text " +
		"because either bound alone is legal"},
	"Validate.MaxValue": {lit("9"), "the other half of the same range"},
	"Border.Margin": {lit("1"), "a thickness — one, two or four whole numbers of " +
		"cells, the one universal literal whose grammar is not a single value"},
	// THE TEST BINARY, and it is the reason these are funcs rather than
	// strings. <Companion Path> must name an executable that exists, and
	// no constant does on every machine — TestNoSweepProbeDependsOnAn
	// InstalledBinary is in this file because a probe that needs `true`
	// or `sh` installed is a declaration that goes silently unverified
	// wherever it is not. os.Args[0] is an executable by construction:
	// it is the process running the assertion.
	"Companion.Path": {func() string { return os.Args[0] },
		"an executable resolved through exec.LookPath at load time"},
}

// lit is the constant answer, spelled as a func so the table has one
// shape. See Companion.Path for the row that cannot be one.
func lit(s string) func() string { return func() string { return s } }

// validLiteralFor is a per-KIND table with a named per-attribute
// exception list, and the split is the point. Kind is a closed set the
// type system already names, so the table cannot go stale the way a list
// of attributes does: a new Kind fails to compile its way past the
// switch, and a new attribute needs a row only when its literal is
// narrower than its Kind.
//
// It ended in `return "x"`, which answered for every Kind nobody had
// thought about — including KindBinding, where "x" is not a literal of
// anything. <Image Cols> is KindBinding/BindsEither with an int GoType,
// so the literal sweep probed a cell count with the letter x, got a
// refusal that was nothing to do with Binds, and moved on: a probe that
// looks like coverage and asks no question. A new Kind would have
// inherited the same silence.
//
// So an unanswered Kind is a FAILURE now rather than a default, and for
// KindBinding the answer comes from GoType — which is where that Kind
// records what its literal has to be. Raised in review of #470.
func validLiteralFor(t *testing.T, el string, a AttrSpec) string {
	t.Helper()
	if n, ok := narrowerThanItsKind[el+"."+a.Name]; ok {
		return n.value()
	}
	return kindLiteralFor(t, a)
}

// kindLiteralFor is validLiteralFor WITHOUT the per-attribute narrowing:
// what the Kind alone says a literal looks like.
//
// Split out so a caller can ask the question the narrowing exists to
// answer — would the generic value do? A row that says "narrower than
// its Kind" about an attribute whose Kind already answers is a row that
// narrows nothing, and it would sit there reading like a considered
// exception. TestEveryNarrowedLiteralIsReached asks it of every row.
// Raised in review of #470.
func kindLiteralFor(t *testing.T, a AttrSpec) string {
	t.Helper()
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
	case KindCommand:
		// A REGISTERED HANDLER, and it is registered by defaultsContext
		// for this. Every one of these answered "x" and was refused with
		// "no handler \"x\" registered" — the context being empty, not
		// the declaration being wrong, and eleven declarations went
		// unverified on it. Raised in review of #470.
		return "probe"
	case KindGridLens:
		return "1*"
	case KindEnum:
		if len(a.Enum) > 0 {
			return a.Enum[0]
		}
		return ""
	case KindText, KindString, KindIdentity:
		// Any non-empty text is a literal of these — except where an
		// individual attribute is narrower, which narrowerThanItsKind
		// above answers for.
		return "x"
	case KindBinding:
		// THE KIND SAYS "A HANDLE" AND Binds SAYS "OR A LITERAL", so
		// GoType is the only thing left that knows what the literal is.
		switch a.GoType {
		case "int":
			return "1"
		case "[]string":
			return "a,b"
		case "image.Image":
			return "probe.png" // served by defaultsContext's Includes
		}
		t.Fatalf("<%s> is KindBinding with GoType %q and takes a literal, and this "+
			"function has no literal of that type — so it would be probed with "+
			"whatever the fallthrough said and the declaration would go unchecked",
			a.Name, a.GoType)
		return ""
	}
	t.Fatalf("validLiteralFor has no answer for Kind %q (attribute %s). A Kind "+
		"added without one used to fall through to \"x\", which is a literal of "+
		"nothing in particular: the probe still ran, still looked like coverage, "+
		"and asked no question", a.Kind, a.Name)
	return ""
}

// TestValidLiteralForAnswersInTheAttributesOwnGrammar is the other half
// of finding 3, and it is the half that fails when the closed set is
// closed WRONGLY rather than left open.
//
// Making an unanswered Kind a t.Fatal stops a NEW Kind falling through
// to "x". It does nothing about an existing Kind whose answer is not a
// literal of the type it claims: <Image Cols> answered "x" for as long
// as KindBinding fell through, and the probe still ran, still produced a
// refusal, and still counted as a look at the declaration. Measured —
// putting "x" back for KindBinding's int arm is silent in every sweep,
// because the refusal it earns simply lands in the unverified bucket.
//
// So the value is checked against the grammar it claims to be in, by the
// same parser the loader uses. Kinds whose literal is any text are not
// listed: there is nothing to check, and listing them would be a second
// closed set to keep in step with the first.
func TestValidLiteralForAnswersInTheAttributesOwnGrammar(t *testing.T) {
	var checked int
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds == BindsBinding {
			continue
		}
		v := validLiteralFor(t, tg.def.Name, a)
		if v == "" {
			continue
		}
		// THE TYPE THE PROBE CLAIMS TO BE. KindBinding says "a handle",
		// and where a literal is also allowed its GoType is the only
		// statement of what that literal has to be — which is exactly
		// the place the fallthrough used to answer for.
		want := string(a.Kind)
		if a.Kind == KindBinding {
			want = a.GoType
		}
		var err error
		switch want {
		case string(KindInt): // KindInt and GoType "int" are the same text
			_, err = strconv.Atoi(v)
		case string(KindBool): // likewise KindBool and GoType "bool"
			if v != "true" && v != "false" {
				err = fmt.Errorf("%q is neither \"true\" nor \"false\"", v)
			}
		case string(KindDuration):
			_, err = time.ParseDuration(v)
		case string(KindEnum):
			err = fmt.Errorf("%q is not one of %v", v, a.Enum)
			for _, o := range a.Enum {
				if o == v {
					err = nil
				}
			}
		default:
			continue // any text is a literal of these
		}
		checked++
		if err != nil {
			t.Errorf("validLiteralFor gives <%s %s> the probe %q, which is not a "+
				"%s: %v.\nThe probe still runs and still earns a refusal, so it "+
				"lands in the unverified bucket and looks like a harness limit — "+
				"the declaration goes unchecked and every count stays the same",
				tg.def.Name, a.Name, v, want, err)
		}
	}
	if checked == 0 {
		t.Fatal("no probe value had a grammar to check it against: this arm is vacuous")
	}
	t.Logf("checked %d probe values against their own grammar", checked)
}

// TestTheLiteralArmCountsVerificationsAndNotProbes is finding 2, and it
// is the only thing that can see the difference.
//
// The counter was incremented BEFORE the build, so a probe the harness
// could not construct an element for counted as coverage: 114 logged
// where 92 attributes had actually taken a literal. Nothing could
// notice, because a count is a log line — the arm passes either way, and
// the number is exactly the argument offered for the arm being real.
//
// So it is asserted the way bindSweep's is: drive the sweep with values
// NOTHING can accept and require the verified count to be zero. Under
// the old counting it equals the number of probes, which is every
// literal-taking attribute with a grammar.
func TestTheLiteralArmCountsVerificationsAndNotProbes(t *testing.T) {
	// GRAMMAR KINDS ONLY. A KindText or KindString attribute accepts any
	// non-empty text by definition, so it would be verified here and the
	// arm would be asserting nothing about counting. These seven have a
	// value grammar that this string is outside of.
	unacceptable := func(t *testing.T, _ string, a AttrSpec) string {
		t.Helper()
		switch a.Kind {
		case KindInt, KindBool, KindDuration, KindColor, KindGesture, KindGridLens:
			return "\u00a1not a value\u00a1"
		}
		return ""
	}
	verified, unverified := literalAcceptSweep(t, unacceptable, false)
	if len(unverified) == 0 {
		t.Fatal("no probe was even built, so this arm cannot tell counting from " +
			"verifying")
	}
	if verified != 0 {
		t.Errorf("%d of %d probes counted as VERIFIED while being handed a value "+
			"no grammar accepts. The count is being incremented for making a "+
			"probe rather than for the probe answering, which is the number "+
			"offered as evidence that the sweep is real", verified, verified+len(unverified))
	}
}

// TestNoSweepProbeDependsOnAnInstalledBinary is the floor under the
// UNVERIFIED buckets, and it exists because "unverified" is a category
// that absorbs everything.
//
// <Companion Path> resolves through exec.LookPath, and the element's
// Seed — which probeElement reads for required literals — named a real
// binary. On a machine without it every <Companion> probe in every arm
// came back unverified, indistinguishable from an element the harness
// genuinely cannot construct, and the arms stayed GREEN because
// unverified is not a failure. The sweep would have gone quiet about a
// whole element on somebody else's box and said so nowhere.
//
// Derived rather than a check on that one seed: any future probe value
// that has to exist on the machine fails here by its cause, not by its
// name. Raised in review of #470.
func TestNoSweepProbeDependsOnAnInstalledBinary(t *testing.T) {
	var probed int
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds == BindsBinding {
			continue
		}
		v := validLiteralFor(t, tg.def.Name, a)
		if v == "" {
			continue
		}
		probed++
		src := harnessFor(a.Name, probeElement(t, tg.def, a.Name, v))
		_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
		// THE ATTRIBUTE UNDER TEST IS ALLOWED TO NEED ONE. <Companion
		// Path="x"> is supposed to fail this way — that is the rule
		// working. What may not depend on the environment is the REST of
		// the probe: the required attributes probeElement fills in from
		// the element's Seed, which every other attribute of that
		// element rides on.
		if a.Name == "Path" {
			continue
		}
		if errors.Is(err, exec.ErrNotFound) {
			t.Errorf("the probe for <%s %s> fails because a binary is not installed:"+
				"\n\t%v\nThat comes from another attribute's seed value, so on a "+
				"machine without it this declaration is silently unverified in every "+
				"sweep arm rather than checked", tg.def.Name, a.Name, err)
		}
	}
	if probed == 0 {
		t.Fatal("no probes were built: this floor is vacuous")
	}
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
	// VERIFIED AND UNVERIFIED, counted apart.
	//
	// This was one `checked` incremented BEFORE the build, so a probe
	// the harness could not even construct an element for counted as
	// coverage: it logged 114 where 92 attributes had actually taken a
	// literal. The gap is not noise — it is exactly the set an author
	// would look at the log and believe was checked. A probe verifies
	// the declaration when the literal is ACCEPTED, or when the loader
	// gives the one refusal this arm is about; every other error is the
	// harness failing to reach the element and is now reported as such.
	// Raised in review of #470.
	verified, unverified := literalAcceptSweep(t, validLiteralFor, true)
	if verified == 0 {
		t.Fatal("no literal-taking attribute actually took a literal: this sweep " +
			"would pass vacuously")
	}
	// THE SAME FLOOR AS EVERY OTHER ARM. This one LOGGED its unverified
	// list where the other four fail on theirs, which made
	// reportUnverified's own doc — "the floor is ZERO and it is met
	// today" — false about the file it lives in, and left twenty
	// declarations reported as a number nobody was checking. Raised in
	// review of #470.
	reportUnverified(t, unverified)
	t.Logf("verified %d literal-taking attributes", verified)
}

// TestEveryNarrowedLiteralIsReached is the guard a per-attribute table
// needs, and it is the reason narrowerThanItsKind is allowed to be one.
//
// A row naming an attribute the vocabulary no longer declares narrows
// nothing. It would sit there reading like coverage while the attribute
// it was written for had been renamed, removed, or given a Kind that
// answers properly — and the sweep above would go on passing, because a
// key that matches nothing simply never fires.
func TestEveryNarrowedLiteralIsReached(t *testing.T) {
	declared := map[string]bool{}
	for _, tg := range sweepTargets(t) {
		declared[tg.def.Name+"."+tg.attr.Name] = true
	}
	if len(declared) == 0 {
		t.Fatal("no declarations found: this guard would pass vacuously")
	}
	for key, n := range narrowerThanItsKind {
		if !declared[key] {
			t.Errorf("narrowerThanItsKind has a row for %s (%s), which no element "+
				"declares as a literal-taking attribute any more", key, n.why)
		}
	}

	// AND EVERY ROW IS STILL EARNING ITS PLACE, which "the attribute is
	// still declared" does not ask.
	//
	// A row narrows the Kind's own answer. If the Kind starts answering —
	// the attribute is given a narrower Kind, or its builder is relaxed —
	// the row stops narrowing anything and becomes a considered-looking
	// exception to a rule that no longer needs one. Nothing would notice:
	// the sweep takes the row's value, it loads, and the count is the
	// same either way. So the check is the counterfactual — probe with
	// the GENERIC value and require that it still fails. Raised in review
	// of #470.
	checked := 0
	for _, tg := range sweepTargets(t) {
		key := tg.def.Name + "." + tg.attr.Name
		n, ok := narrowerThanItsKind[key]
		if !ok {
			continue
		}
		generic := kindLiteralFor(t, tg.attr)
		if generic == "" {
			// The Kind has no answer at all, so the row is load-bearing
			// by construction and there is nothing to compare against.
			continue
		}
		checked++
		src := harnessFor(tg.attr.Name, probeElement(t, tg.def, tg.attr.Name, generic))
		if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err == nil {
			t.Errorf("narrowerThanItsKind narrows %s to %q on the grounds that it is "+
				"%s — and <%s %s=%q>, the value its Kind alone gives, loads. The row "+
				"narrows nothing and should go, or the reason it states is no longer "+
				"the reason",
				key, n.value(), n.why, tg.def.Name, tg.attr.Name, generic)
		}
	}
	// NON-VACUITY. A sweepTargets that stopped producing the narrowed
	// attributes would skip every row above and report nothing.
	if checked == 0 {
		t.Errorf("no narrowed row was reached through sweepTargets, so the "+
			"counterfactual above ran on nothing (%d rows declared)",
			len(narrowerThanItsKind))
	}
}

// TestEveryPrereqRowIsReached is the guard probePrereqs needs, and it
// asks both halves TestEveryNarrowedLiteralIsReached asks, for the same
// reasons.
//
// A table that only ever ADDS attributes to a probe cannot fail loudly:
// every row makes some probe load, and a row that has stopped mattering
// makes it load just the same. So neither half is optional.
//
// The first half is that the row still names something real. A key
// whose element or attribute the vocabulary no longer declares pairs
// nothing, and probeElement's own t.Fatalf cannot say so — it fires only
// when a probe for that exact key runs, which is precisely what stops
// happening when the declaration goes away.
//
// The second half is the counterfactual, and it is the one with teeth: a
// row whose prerequisite has stopped being required — the element's
// guards were reordered, or the requirement moved into the declaration
// where probeElement's own def.Attrs loop would find it — reads
// identically to a row that is still load-bearing. probeElementBare is
// the only thing that can put the question, and the answer has to be
// that the bare probe FAILS.
func TestEveryPrereqRowIsReached(t *testing.T) {
	declared := map[string]sweepTarget{}
	for _, tg := range sweepTargets(t) {
		declared[tg.def.Name+"."+tg.attr.Name] = tg
	}
	if len(declared) == 0 {
		t.Fatal("no declarations found: this guard would pass vacuously")
	}
	checked := 0
	for key, seeds := range probePrereqs {
		tg, ok := declared[key]
		if !ok {
			t.Errorf("probePrereqs has a row for %s, which no element declares as "+
				"a sweepable attribute any more", key)
			continue
		}
		// The value the row exists to let through. literalFor is what
		// the bind-only arm probes with, and it is the arm the table was
		// added for; a row added for a different arm would need its own
		// case here rather than a wider net.
		value := literalFor(tg.attr)
		if value == "" {
			continue
		}
		checked++
		bare := harnessFor(tg.attr.Name, probeElementBare(t, tg.def, tg.attr.Name, value))
		if _, err := Build([]byte("<Gooey>"+bare+"</Gooey>"), defaultsContext()); err == nil {
			t.Errorf("probePrereqs seeds %v for %s on the grounds that the probe "+
				"cannot be built without them — and the bare probe <%s %s=%q> loads. "+
				"The row seeds nothing the element still needs and should go",
				seeds, key, tg.def.Name, tg.attr.Name, value)
		}
	}
	// NON-VACUITY, the same floor the narrowing guard carries: a
	// sweepTargets that stopped producing these attributes would skip
	// every row and report nothing.
	if checked == 0 {
		t.Errorf("no probePrereqs row was reached through sweepTargets, so the "+
			"counterfactual above ran on nothing (%d rows declared)",
			len(probePrereqs))
	}
}

// literalAcceptSweep is the arm's body, EXTRACTED so the classification
// can be driven by a caller that knows the answer — the same reason
// bindSweep is a function, stated in its own doc.
//
// `report` is what separates the two callers: the real arm reports a
// declaration that demands a binding, and the self-test below must not,
// because it is feeding values nothing can accept on purpose.
func literalAcceptSweep(t *testing.T, value func(*testing.T, string, AttrSpec) string, report bool) (verified int, unverified []string) {
	t.Helper()
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds != BindsLiteral && a.Binds != BindsEither {
			continue
		}
		v := value(t, tg.def.Name, a)
		if v == "" {
			continue
		}
		src := harnessFor(a.Name, probeElement(t, tg.def, a.Name, v))
		_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
		if err == nil {
			verified++
			continue
		}
		if strings.Contains(err.Error(), "is not a binding expression") {
			verified++
			if report {
				t.Errorf("<%s %s=%q> is declared Binds=%q, but the loader requires "+
					"a binding:\n\t%v", tg.def.Name, a.Name, v, a.Binds, err)
			}
			continue
		}
		unverified = append(unverified,
			fmt.Sprintf("<%s %s=%q>: %v", tg.def.Name, a.Name, v, err))
	}
	return verified, unverified
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
		if !refusedTheEmptyValue(err, a.Name) {
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

// TestALiteralDurationCannotBeEmpty is the KindDuration arm, and its
// absence is what left two readers on a third grammar.
//
// litIntSweep derives the int rules over every KindInt declaration and
// boolSpellingSweep does the same for KindBool. Durations had ONE
// hand-written line — <Spinner Interval=""> — and TestAnEmptyLiteralIsA-
// LoadError filters to int and bool. So optDuration became strict, five
// of the eight declarations followed it, and two kept reading an empty
// value as "use the default" while the sixth used ParseDuration's own
// message. Four readers, three answers, and the enumerated arm probed
// one of the five that agreed with it.
//
// Measured against the branch before this arm existed:
//
//	<FileWatcher Paths="a.gooey" Interval=""/>  -> loads, keeps 300ms
//	<TypeAhead Key="Title" Timeout=""/>         -> loads, keeps 1s
//	<Spinner Interval=""/>                      -> load error
//
// Raised in review of #470, which is this file's own argument for
// derivation applied to the Kind it stopped at.
func TestALiteralDurationCannotBeEmpty(t *testing.T) {
	var checked int
	var unverified []string
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds != BindsLiteral || a.Kind != KindDuration {
			continue
		}
		src := harnessFor(a.Name, withEmptyAttr(probeElement(t, tg.def, a.Name, ""), a.Name))
		_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
		if err == nil {
			t.Errorf("<%s %s=\"\"> loads and silently keeps the component's own "+
				"default. An empty duration is a typo, and asking for the default "+
				"already has a spelling: omit the attribute", tg.def.Name, a.Name)
			continue
		}
		if !refusedTheEmptyValue(err, a.Name) {
			unverified = append(unverified,
				fmt.Sprintf("<%s %s=\"\">: %v", tg.def.Name, a.Name, err))
			continue
		}
		checked++
	}
	reportUnverified(t, unverified)
	if checked == 0 {
		t.Fatal("no literal durations were checked: this sweep would pass vacuously")
	}
	t.Logf("checked %d literal durations against an empty value", checked)
}

// TestALiteralDurationStillTakesADuration is the accept direction, and
// it is what keeps the arm above off "refuse every duration".
//
// It also covers the half the empty arm cannot: a reader that refuses
// the empty string by hand and then parses everything else its own way
// satisfies the sweep while still being a second grammar. A real
// duration has to load on every declaration.
func TestALiteralDurationStillTakesADuration(t *testing.T) {
	var checked int
	var refused []string
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds != BindsLiteral || a.Kind != KindDuration {
			continue
		}
		src := harnessFor(a.Name, probeElement(t, tg.def, a.Name, "250ms"))
		if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err != nil {
			refused = append(refused,
				fmt.Sprintf("<%s %s=\"250ms\">: %v", tg.def.Name, a.Name, err))
			continue
		}
		checked++
	}
	for _, r := range refused {
		t.Errorf("%s is refused, so the empty-value arm above is about a reader "+
			"that refuses every duration", r)
	}
	if checked == 0 {
		t.Fatal("no literal durations were checked: this sweep would pass vacuously")
	}
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
		// THE SHARED PREDICATE, not a weaker spelling of it. This
		// checked the quoted value alone; the empty-literal arm below
		// checked the attribute name alone. Three strengths of one idea
		// in one file is the drift ruleRefusedIt exists to stop.
		if !ruleRefusedIt(err, a.Name, value) {
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
