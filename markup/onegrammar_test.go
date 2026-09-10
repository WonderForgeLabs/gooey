package markup

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey/components"
)

// ONE VALUE, ONE GRAMMAR — the four places review of #470 found the rule
// stated in a second way, each pinned rather than described.
//
// The sweep in bindsweep_test.go is the derived half and covers the
// declarations. These cover the cases a sweep cannot ask about, because
// they are about a value that IS accepted somewhere: "+3" parses, ""
// parses as absent, and a bool spelled "1" is a real spelling in Go.

// TestTheValidateHarnessReachesTheRule is the floor under the bool arm,
// and it is here because the arm was VACUOUS for seven attributes and
// nothing could see it.
//
// <Validate> is an attachment: attachAll refuses one whose host does not
// wire it, and the sweep harness wrapped every probe in an <HStack>. So
// every <Validate> probe failed with "does not support <Validate>" and
// an arm asserting only `err != nil` counted seven verified refusals
// while the rule underneath was still on strconv.ParseBool.
//
// This asserts the harness reaches the RULE — the refusal has to be
// about the value, not about the host. Without it, someone changing
// harnessFor puts the hole straight back and every count stays the same.
func TestTheValidateHarnessReachesTheRule(t *testing.T) {
	// THE PROBE IS A VALID VALUE, and that is the whole trick. A bad one
	// is refused by buildValidate before attachAll ever runs, so
	// <Validate Required="1"> is a load error in ANY host and says
	// nothing about whether the harness found one — measured: the first
	// version of this test used it and stayed silent when the harness
	// fix was reverted. Only a value that must LOAD separates a wired
	// host from an unwired one.
	src := harnessFor("Required", `<Validate Required="true"/>`)
	if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err != nil {
		t.Fatalf("a VALID <Validate> probe does not load in the sweep harness: %v.\n"+
			"<Validate> is an attachment and attachAll refuses one whose host does not "+
			"wire it, so every probe then fails for that reason — and an arm asserting "+
			"only err != nil counts seven verified refusals it never made. That is how "+
			"the rule stayed on strconv.ParseBool through the arm written to catch "+
			"exactly it", err)
	}

	// AND THE RULE STILL ANSWERS, quoting the value, or no sweep arm can
	// tell its refusal from the harness's.
	bad := harnessFor("Required", `<Validate Required="1"/>`)
	_, err := Build([]byte("<Gooey>"+bad+"</Gooey>"), defaultsContext())
	if err == nil {
		t.Fatal(`<Validate Required="1"> loads: the house bool grammar is not being ` +
			`applied to validation rules`)
	}
	if !strings.Contains(err.Error(), `"1"`) {
		t.Errorf("the refusal does not quote the offending value, so no sweep arm can "+
			"tell it from a harness error: %v", err)
	}
}

// TestTheOneSpellingRuleIsNotARefuseEverything is the non-vacuity arm for
// the three swept ones in bindsweep_test.go
// (TestALiteralExtentCannotBeNegative, TestALiteralIntHasOneSpelling,
// TestALiteralIntAcceptsSurroundingSpace).
//
// Those ask what is REFUSED, and an int reader that refuses everything
// satisfies all three. This is the one line that says the canonical
// spelling still loads. It stays enumerated because it is one claim about
// one value, not a claim about the vocabulary.
func TestTheOneSpellingRuleIsNotARefuseEverything(t *testing.T) {
	ok := `<HStack Gap="3"><Text>a</Text><Text>b</Text></HStack>`
	if _, err := Build([]byte("<Gooey>"+ok+"</Gooey>"), defaultsContext()); err != nil {
		t.Fatalf(`<HStack Gap="3"> no longer loads, so the swept refusal arms are `+
			`about an int reader that refuses everything: %v`, err)
	}
}

// TestTheSameRuleReachesTheValidateInts pins that MinLen and MaxLen read
// through litInt rather than a bare strconv.Atoi of their own.
//
// Three disagreements in one attribute pair: MinLen=" 3 " was a load
// error where every other int trims, MinLen="-3" loaded and meant a
// negative minimum, MinLen="+3" loaded. Found while fixing the
// leading-plus finding — the same defect one element over.
func TestTheSameRuleReachesTheValidateInts(t *testing.T) {
	host := func(rule string) string {
		return `<Gooey><TextBox Text="{{.S}}"><Validate ` + rule + `/></TextBox></Gooey>`
	}
	for _, v := range []string{"-3", "+3"} {
		if _, err := Build([]byte(host(`MinLen="`+v+`"`)), defaultsContext()); err == nil {
			t.Errorf("<Validate MinLen=%q> loads; MinLen is a declared "+
				"KindInt/BindsLiteral attribute and reads like every other one", v)
		}
	}
	// The SPACES arm is the one that goes the other way: every other
	// literal int trims, and this one did not.
	if _, err := Build([]byte(host(`MinLen=" 3 "`)), defaultsContext()); err != nil {
		t.Errorf("<Validate MinLen=\" 3 \"> is a load error, but litInt trims and "+
			"<HStack Gap=\" 3 \"> has always loaded: %v", err)
	}
}

// TestEmptyMeansOmittedForAHandleAndIsAnErrorForAValue is the
// reconciliation, asserted rather than only written in a comment.
//
// <ProgressBar Indeterminate=""/> loads and <ProgressBar Thresholds=""/>
// is a load error, ON THE SAME ELEMENT, and that reads like a bug until
// you see which question each half answers. A handle has a real
// "there is none" state; a whole number does not, so an empty one is a
// typo and "absent" already has a spelling.
//
// Pinned in ONE test, deliberately: split into two they would drift
// apart, and the pair is the claim.
func TestEmptyMeansOmittedForAHandleAndIsAnErrorForAValue(t *testing.T) {
	handle := `<Gooey><ProgressBar Value="{{.Pct}}" Indeterminate=""/></Gooey>`
	if _, err := Build([]byte(handle), defaultsContext()); err != nil {
		t.Errorf("an optional BINDING written empty is a load error: %v.\n"+
			"Empty and absent both mean \"there is no property to bind\", which is a "+
			"state the component supports — refusing it is the <TextBox Error=\"\"> "+
			"bug suppliedAttr exists to fix", err)
	}
	value := `<Gooey><ProgressBar Value="{{.Pct}}" Thresholds=""/></Gooey>`
	if _, err := Build([]byte(value), defaultsContext()); err == nil {
		t.Error("an optional literal BOOL written empty loads. There is no empty bool, " +
			"so it is a typo — and #460 is entirely about a typo in a literal meaning " +
			"false in silence. Omitting the attribute is how you ask for the default")
	}
}

// TestAnEmptyDurationIsALoadError is optDuration joining the literal
// half, which it was not on until review of #470.
//
// Interval="" fell through to the component's default — the same silent
// fallback optDuration's own doc comment refuses one line above, for a
// mistyped duration.
func TestAnEmptyDurationIsALoadError(t *testing.T) {
	if _, err := Build([]byte(`<Gooey><Spinner Interval=""/></Gooey>`), defaultsContext()); err == nil {
		t.Error(`<Spinner Interval=""> loads and silently takes the component's ` +
			`default. An empty duration is a typo, and asking for the default already ` +
			`has a spelling: omit the attribute`)
	}
	// NON-VACUITY, both directions: omitted still loads, and a real
	// duration still loads.
	for _, src := range []string{`<Spinner/>`, `<Spinner Interval="250ms"/>`} {
		if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err != nil {
			t.Errorf("%s no longer loads, so the assertion above is about a reader that "+
				"refuses every duration: %v", src, err)
		}
	}
}

// TestOneIntGrammarReachesEveryIntLiteral is finding 1, and it is DERIVED
// from the catalog rather than written as a list, because a list is what
// let the second grammar exist.
//
// <Image Cols> and <Image Rows> read their literal with a bare
// strconv.Atoi, so `Cols="007"` loaded and meant 7 while `Gap="007"` was
// a load error three lines away. The literal sweep could not see it:
// those two are declared KindBinding (their GoType carries the int), and
// a sweep that reads Kind looks straight past them. So this walks the
// attributes whose LITERAL is a whole number by either spelling of that
// fact.
func TestOneIntGrammarReachesEveryIntLiteral(t *testing.T) {
	targets := intLiteralTargets(t)
	if len(targets) == 0 {
		t.Fatal("no int-literal attributes found: this sweep would pass vacuously")
	}
	// SECOND SPELLINGS OF SEVEN, every one of which strconv.Atoi accepts.
	// They are values that PARSE — the point is not that a bad value is
	// refused, it is that one number has one text.
	//
	// SURROUNDING SPACE IS NOT ONE OF THEM, and running it here is how
	// that got settled: " 7 " loads everywhere, deliberately, because
	// the grammar trims before it compares and TestALiteralIntAccepts
	// SurroundingSpace is the arm that says so. XML attribute whitespace
	// is not part of the value; a leading zero is.
	spellings := []string{"007", "+7"}
	verified := map[string]bool{}
	var unverified []string
	for _, tg := range targets {
		for _, bad := range spellings {
			src := harnessFor(tg.attr.Name, probeElement(t, tg.def, tg.attr.Name, bad))
			_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
			name := tg.def.Name + " " + tg.attr.Name
			if err == nil {
				t.Errorf("<%s %s=%q> loads. Atoi accepts it, so it means 7 — and "+
					"%q is a load error elsewhere in the same vocabulary. One "+
					"number, one text", tg.def.Name, tg.attr.Name, bad, bad)
				continue
			}
			// THE PREDICATE IS THE REFUSAL, not err != nil. A probe can
			// fail because the harness cannot reach the element at all,
			// and counting that as a pass is how a sweep goes green over
			// the bug it was written for.
			if strings.Contains(err.Error(), "a second way to write the same number") {
				verified[name] = true
				continue
			}
			unverified = append(unverified, name+"="+bad+": "+err.Error())
		}
	}
	// THE FLOOR IS THE FINDING'S OWN TWO. Everything above is derived,
	// and a derived sweep whose harness quietly stops reaching anything
	// reports nothing at all — so the two attributes the second grammar
	// actually lived on have to be among the verified, by name.
	for _, want := range []string{"Image Cols", "Image Rows"} {
		if !verified[want] {
			t.Errorf("<%s> was not verified against a second spelling. That is the "+
				"attribute this test exists for, so the sweep is not reaching it:\n\t%s",
				want, strings.Join(unverified, "\n\t"))
		}
	}
	// THE SAME FLOOR THE SWEEP ARMS USE. This logged its unverified
	// count where bindsweep_test.go's arms fail on theirs — so an
	// element that stopped being reachable would shrink this sweep
	// silently, and the named floor above only pins two of them.
	// Raised in review of #470.
	reportUnverified(t, unverified)
	t.Logf("verified %d of %d int-literal attributes", len(verified), len(targets))
}

// intLiteralTargets is every attribute whose literal form is a whole
// number, under BOTH spellings of that fact in the catalog: KindInt, and
// KindBinding with an int GoType where a literal is also allowed.
//
// The second spelling is the one that matters. It is what <Image Cols>
// is, and it is invisible to any sweep that switches on Kind alone.
func intLiteralTargets(t *testing.T) []sweepTarget {
	t.Helper()
	var out []sweepTarget
	for _, tg := range sweepTargets(t) {
		a := tg.attr
		if a.Binds == BindsBinding {
			continue
		}
		if a.Kind == KindInt || (a.Kind == KindBinding && a.GoType == "int") {
			out = append(out, tg)
		}
	}
	return out
}

// TestTheIntGrammarStillAcceptsANumber is the non-vacuity arm for the
// one above, and it is separate because the two claims fail for opposite
// reasons: a reader that refuses everything satisfies the sweep, and a
// reader that accepts everything satisfies this.
func TestTheIntGrammarStillAcceptsANumber(t *testing.T) {
	for _, tc := range []struct {
		src  string
		load bool
	}{
		{`<Image Src="{{.Img}}" Cols="3" Rows="2"/>`, true},
		{`<Image Src="{{.Img}}" Cols="{{.I}}" Rows="2"/>`, true},
		{`<Image Src="{{.Img}}" Cols="0" Rows="2"/>`, false},
		{`<Image Src="{{.Img}}" Cols="-1" Rows="2"/>`, false},
	} {
		_, err := Build([]byte("<Gooey>"+tc.src+"</Gooey>"), defaultsContext())
		if tc.load && err != nil {
			t.Errorf("%s is refused, so the sweep above is about a reader that takes "+
				"no cell count at all: %v", tc.src, err)
		}
		if !tc.load && err == nil {
			t.Errorf("%s loads. A picture nought or minus-one cells wide is not a "+
				"smaller picture", tc.src)
		}
	}
}

// TestAnAbsentCellCountIsNotAnEmptyOne is the last literal int in the
// vocabulary that could not reach emptyLiteralWhy.
//
// cellCount opened `raw := TrimSpace(e.Attrs[attr]); if raw == ""`,
// which collapses "the author wrote nothing" into "there is no
// attribute" — so <Image Cols=""/> was told it "needs Cols", about a
// value the author had just typed. <Timer Interval> was changed for
// exactly this shape one Kind across, and this is the same case for the
// int reader.
//
// BOTH STATES, in one test, because they are one distinction: a fix that
// merely swapped the two messages would satisfy either arm alone.
func TestAnAbsentCellCountIsNotAnEmptyOne(t *testing.T) {
	for _, tc := range []struct{ src, want, notWant string }{
		// ABSENT — the element's own answer, naming what it needs.
		{`<Image Src="{{.Img}}" Rows="2"/>`, "needs Cols", emptyLiteralWhy},
		{`<Image Src="{{.Img}}" Cols="3"/>`, "needs Rows", emptyLiteralWhy},
		// PRESENT AND EMPTY — the grammar's answer, in the words every
		// other literal int uses.
		{`<Image Src="{{.Img}}" Cols="" Rows="2"/>`, emptyLiteralWhy, "needs Cols"},
		{`<Image Src="{{.Img}}" Cols="3" Rows=""/>`, emptyLiteralWhy, "needs Rows"},
		// WHITESPACE IS EMPTY for a cell count, unlike <Validate
		// Pattern=" ">, where one space is an expression. The
		// difference is that a number has no spelling made of spaces.
		{`<Image Src="{{.Img}}" Cols=" " Rows="2"/>`, emptyLiteralWhy, "needs Cols"},
	} {
		_, err := Build([]byte("<Gooey>"+tc.src+"</Gooey>"), defaultsContext())
		if err == nil {
			t.Errorf("%s loads", tc.src)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s is refused with %v; want a message containing %q",
				tc.src, err, tc.want)
		}
		if strings.Contains(err.Error(), tc.notWant) {
			t.Errorf("%s is refused with %v, which is the OTHER state's message — "+
				"absent and present-but-empty have to answer differently",
				tc.src, err)
		}
	}
}

// signedDurationAttrs names every duration attribute for which a
// NEGATIVE value is meaningful rather than wrong, with the reason, keyed
// "<Element>.<Attr>".
//
// It exists because unifying a vocabulary means asking one question one
// way — it does not mean giving every attribute the same answer. The
// first pass at #460 routed <ToastHost Duration> through optDuration and
// so made "-5s" a load error, deleting a documented feature. Raised in
// review of #470.
//
// It is NOT a skip list, and the arms below are what stop it becoming
// one: every entry must name a declared duration attribute, the map must
// not be empty, and each exempted attribute must actually LOAD a
// negative. An entry that stops being true goes red rather than going
// quiet, and somebody has to decide.
var signedDurationAttrs = map[string]string{
	"ToastHost.Duration": "components.ToastHost documents it at the field: " +
		"\"Zero means DefaultToastDuration; negative means sticky — toasts " +
		"stay until dismissed\"",
}

// TestEveryDurationAnswersTheSameWay is finding 5. Two of the nine
// KindDuration attributes read their value by hand rather than through
// optDuration, and hand-rolled is where they disagreed:
// <ToastHost Duration=""> answered with time's own parse wording, and
// <Timer Interval=""> answered "needs an Interval" where the other seven
// say an empty one is a typo and name the spelling that asks for the
// default.
//
// The two probes are deliberately not the same claim. EMPTY is universal
// — every duration attribute in the vocabulary answers it identically,
// with no exemptions. NEGATIVE is the set minus signedDurationAttrs,
// because positivity is a rule about the attribute's meaning and not
// about its grammar.
//
// Derived from Kind, so a tenth duration attribute joins this the day it
// is declared.
func TestEveryDurationAnswersTheSameWay(t *testing.T) {
	var targets []sweepTarget
	for _, tg := range sweepTargets(t) {
		if tg.attr.Kind == KindDuration && tg.attr.Binds != BindsBinding {
			targets = append(targets, tg)
		}
	}
	if len(targets) == 0 {
		t.Fatal("no duration attributes found: this sweep would pass vacuously")
	}
	// Each probe is a value that PARSES as far as the previous reader
	// took it, so a refusal here is about the rule and not about syntax.
	for _, probe := range []struct {
		value, want string
		// signed says this probe asks a question the exempted
		// attributes are entitled to answer differently.
		signed bool
	}{
		{value: "", want: "is a typo"},
		{value: "-5s", want: "must be positive", signed: true},
	} {
		var verified, exempt int
		var unverified []string
		for _, tg := range targets {
			if _, ok := signedDurationAttrs[tg.def.Name+"."+tg.attr.Name]; ok && probe.signed {
				exempt++
				continue
			}
			el := probeElement(t, tg.def, tg.attr.Name, probe.value)
			if probe.value == "" {
				// probeElement OMITS an empty value — `if value != ""` —
				// so the empty arm has to be written in afterwards, or
				// every probe silently asks about ABSENCE instead. Eight
				// of nine durations "loaded" the first time this ran,
				// which is the attribute not being there at all.
				el = withEmptyAttr(el, tg.attr.Name)
			}
			src := harnessFor(tg.attr.Name, el)
			_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
			if err == nil {
				t.Errorf("<%s %s=%q> loads. Every other duration in the vocabulary "+
					"refuses it", tg.def.Name, tg.attr.Name, probe.value)
				continue
			}
			if strings.Contains(err.Error(), probe.want) {
				verified++
				continue
			}
			// THE REFUSAL, NOT err != nil. A probe the harness cannot
			// reach also fails, and counting that as a pass is how a
			// sweep goes green over the bug it was written for.
			unverified = append(unverified, fmt.Sprintf("<%s %s=%q>: %v",
				tg.def.Name, tg.attr.Name, probe.value, err))
		}
		if verified == 0 {
			t.Errorf("no duration attribute was refused %q with %q, so this arm is "+
				"passing on harness errors rather than on the rule", probe.value, probe.want)
		}
		// THE SAME FLOOR THE SWEEP ARMS USE, and it is what turns the
		// count below from a log into a claim. Raised in review of #470.
		reportUnverified(t, unverified)
		t.Logf("%q: %d of %d duration attributes verified (%d exempt)",
			probe.value, verified, len(targets)-exempt, exempt)
		if probe.signed && exempt != len(signedDurationAttrs) {
			t.Errorf("%d of %d signedDurationAttrs entries were reached by this "+
				"sweep. An entry naming an attribute the vocabulary no longer "+
				"declares exempts nothing and hides that it is stale",
				exempt, len(signedDurationAttrs))
		}
	}
	// THE EXEMPTIONS ARE EXERCISED, NOT SKIPPED. Each one has to load
	// the value the sweep above excused it from refusing — otherwise
	// making ToastHost positive-only would pass here by being skipped
	// twice, once in the sweep and once in this file.
	if len(signedDurationAttrs) == 0 {
		t.Error("signedDurationAttrs is empty. If positivity really did become " +
			"universal, delete the exemption machinery rather than emptying it: " +
			"an empty map makes the arm below vacuous and says nothing")
	}
	for key, why := range signedDurationAttrs {
		el, attr, ok := strings.Cut(key, ".")
		if !ok {
			t.Fatalf("signedDurationAttrs key %q is not <Element>.<Attr>", key)
		}
		src := fmt.Sprintf("<%s %s=%q/>", el, attr, "-5s")
		if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err != nil {
			t.Errorf("%s is refused: %v\nIt is exempt from the positivity rule "+
				"because %s. If that stopped being true, the exemption goes with "+
				"it — this arm is where you decide, not where you skip", src, err, why)
		}
	}
	// NON-VACUITY: a real duration still loads.
	for _, src := range []string{`<Spinner Interval="250ms"/>`, `<ToastHost Duration="4s"/>`} {
		if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err != nil {
			t.Errorf("%s is refused: %v", src, err)
		}
	}
}

// TestTheTrackListRefusalCarriesItsCause is finding 6, and it cannot be
// asserted on the message: %v and %w render identically, which is why a
// single wrap in this file could differ from every sibling for as long
// as it did.
//
// The difference is only visible through the chain, so that is what is
// walked: errors.Is and errors.As stop at the markup layer when the
// cause is formatted rather than wrapped.
func TestTheTrackListRefusalCarriesItsCause(t *testing.T) {
	const bad = "1*,zzz"
	inner := func() string {
		_, err := components.ParseGridLens(bad)
		if err == nil {
			t.Fatalf("ParseGridLens(%q) is not an error any more, so this test "+
				"asserts nothing", bad)
		}
		return err.Error()
	}()

	src := `<Gooey><Grid Cols="` + bad + `"><Text>a</Text></Grid></Gooey>`
	err := func() error {
		_, err := Build([]byte(src), defaultsContext())
		return err
	}()
	if err == nil {
		t.Fatalf("%s loads", src)
	}
	if err.Error() == inner {
		t.Fatalf("the markup layer added nothing, so unwrapping below proves "+
			"nothing about wrapping: %v", err)
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		if e.Error() == inner {
			return
		}
	}
	t.Errorf("the refusal for %s does not carry ParseGridLens's error in its "+
		"chain, so errors.Is and errors.As stop at the markup layer:\n\tgot   %v"+
		"\n\tcause %s\nEvery sibling wrap in elements.go uses %%w; this one used "+
		"%%v, and the two render the same text", src, err, inner)
}

// TestTheMarginGrammarIsTheIntGrammar is finding 1 of the second review
// round, and Margin is the attribute that hid longest.
//
// It is universal, so it is on every element in the vocabulary, and it
// reached none of the int sweeps: parseThickness takes a STRING of one,
// two or four numbers, so <Border Margin> is declared as text and the
// arms that walk KindInt cannot see it. Underneath, it read bare
// strconv.Atoi — the exact reader #460 was filed about, still there
// after the sweep that was supposed to have found all of them.
//
// The three consequences are asserted apart, because they fail for
// different reasons and a single "is refused" arm would pass on any one
// of them.
func TestTheMarginGrammarIsTheIntGrammar(t *testing.T) {
	for _, tc := range []struct {
		v    string
		load bool
		want string
	}{
		{v: "1", load: true},
		{v: "1,2", load: true},
		{v: "1,2,3,4", load: true},
		{v: "0", load: true},
		// A SECOND SPELLING, refused three lines away for <HStack Gap>
		// and loading here, meaning 7, until review of #470.
		{v: "007", want: "is spelled"},
		{v: "+7", want: "is spelled"},
		{v: "1,007", want: "is spelled"},
		// NEGATIVE. It parses, so nothing downstream refuses it:
		// ArrangeChild adds Margin.L to the slot's X and subtracts L+R
		// from its width, so a negative margin arranges the child
		// outside the rect that clips it.
		{v: "-1", want: "cannot be negative"},
		{v: "1,-2,3,4", want: "cannot be negative"},
		// AND THE MESSAGE IS ABOUT MARKUP. The old one wrapped
		// strconv's own text, so an author reading a load error about
		// their document was shown the name of a Go function.
		{v: "x", want: "not a whole number of cells"},
	} {
		src := `<Gooey><Border Margin="` + tc.v + `"><Text>a</Text></Border></Gooey>`
		_, err := Build([]byte(src), defaultsContext())
		switch {
		case tc.load && err != nil:
			t.Errorf(`Margin=%q is refused: %v`, tc.v, err)
		case !tc.load && err == nil:
			t.Errorf(`Margin=%q loads. Every other literal int in the vocabulary `+
				`refuses it`, tc.v)
		case !tc.load && !strings.Contains(err.Error(), tc.want):
			t.Errorf(`Margin=%q is refused with %v; want a message containing %q`,
				tc.v, err, tc.want)
		case !tc.load && strings.Contains(err.Error(), "strconv"):
			t.Errorf(`Margin=%q leaks strconv's own wording into a load error `+
				`about a document: %v`, tc.v, err)
		}
	}
}

// TestAnEmptyLiteralSaysTheSameThingWhicheverReaderSeesIt is the parity
// the two int readers did not have, on the one value an author is most
// likely to leave behind mid-edit.
//
// Both refused `Gap=""` and `Margin=""` before this, so a "both are
// refused" arm would have passed on the defect. What differed is the
// SENTENCE: litIntGrammar said the value "would silently lay out as 0",
// describing what would happen if it were accepted rather than what
// does, and parseThickness said `"" is not a whole number of cells`,
// which is true and no use to somebody who wrote Margin="" meaning
// "none". Raised in review of #470.
//
// It compares the two messages rather than matching each against a
// literal, because a literal in a test is a third copy of the sentence
// and would go stale with the other two.
func TestAnEmptyLiteralSaysTheSameThingWhicheverReaderSeesIt(t *testing.T) {
	msg := func(src string) string {
		t.Helper()
		_, err := Build([]byte(src), defaultsContext())
		if err == nil {
			t.Fatalf("%s loads. An empty literal is a half-typed document, not a "+
				"zero", src)
		}
		return err.Error()
	}
	gap := msg(`<Gooey><HStack Gap=""><Text>a</Text></HStack></Gooey>`)
	margin := msg(`<Gooey><Border Margin=""><Text>a</Text></Border></Gooey>`)
	if !strings.Contains(gap, emptyLiteralWhy) || !strings.Contains(margin, emptyLiteralWhy) {
		t.Errorf("the two readers explain an empty value differently:\n\tGap:    %s\n\tMargin: %s",
			gap, margin)
	}
	// AND BOTH STILL NAME THEIR ELEMENT. A shared sentence is worth
	// nothing if one of the two loses the context around it — which is
	// the other half of the same finding, below.
	for _, c := range []struct{ what, got, want string }{
		{"Gap", gap, "<HStack"}, {"Margin", margin, "<Border"},
	} {
		if !strings.Contains(c.got, c.want) {
			t.Errorf("%s's refusal does not name its element (%s): %s", c.what, c.want, c.got)
		}
	}
}

// TestTheMarginRefusalNamesItsElementAndItsPosition is the rest of the
// Margin finding, and both halves are about a document with more than
// one of something.
//
// The generic attribute wrap names the attribute and the value and NOT
// the element, so a page with a dozen <Border>s reported `attribute
// Margin="x"` and left the author to find which one — while litInt, the
// same grammar three lines up, has named its element all along. And a
// four-value margin reported only the offending number, which in
// "4,2,x,2" is one of four values the author has to try in turn.
//
// Raised in review of #470.
func TestTheMarginRefusalNamesItsElementAndItsPosition(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want []string
	}{
		{"x", []string{"<Border", `Margin="x"`}},
		{"1,x", []string{"<Border", "vertical"}},
		{"x,1", []string{"<Border", "horizontal"}},
		{"4,2,x,2", []string{"<Border", "right"}},
		{"4,-2,4,2", []string{"<Border", "top", "cannot be negative"}},
		{"4,2,4,007", []string{"<Border", "bottom", "is spelled"}},
	} {
		src := `<Gooey><Border Margin="` + tc.v + `"><Text>a</Text></Border></Gooey>`
		_, err := Build([]byte(src), defaultsContext())
		if err == nil {
			t.Errorf("Margin=%q loads", tc.v)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("Margin=%q is refused with %v; want it to name %q", tc.v, err, w)
			}
		}
	}
	// A SINGLE VALUE KEEPS THE SHORTER SENTENCE. Naming "value 1 of 1"
	// would be noise, and this is what stops the position from being
	// added unconditionally.
	_, err := Build([]byte(`<Gooey><Border Margin="x"><Text>a</Text></Border></Gooey>`),
		defaultsContext())
	if err == nil {
		t.Fatal("Margin=\"x\" loads")
	}
	if strings.Contains(err.Error(), " of 1") {
		t.Errorf("a one-value margin reports a position: %v", err)
	}
}

// TestARuleThatCanNeverFireIsNeverInstalled is the class the NaN bound
// belonged to, swept across the two other members review of #470 found.
//
// The framework's rule is that accepted-but-ignored markup is refused.
// Both of these were accepted AND ignored, in different ways:
//
//   - <Validate Pattern=""/> compiles, and the empty expression matches
//     at every position of every string, so the rule is installed and
//     can never fire.
//   - <Validate MaxLen="0"/> installs NOTHING: validate.Len reads 0 as
//     "no bound in this direction", which is how either half of the pair
//     is made optional, so the author's "must be empty" produced a rule
//     list with no length rule in it.
//
// The loading arms are not decoration — they are what separates "refuses
// the degenerate value" from "refuses the attribute".
func TestARuleThatCanNeverFireIsNeverInstalled(t *testing.T) {
	for _, tc := range []struct {
		attrs string
		load  bool
		want  string
	}{
		{attrs: `Pattern=""`, want: "matches every string"},
		{attrs: `Pattern="^a+$"`, load: true},
		// ONE SPACE IS AN EXPRESSION. The refusal is on the empty string
		// and not on a trimmed one, and this is the arm that says so.
		{attrs: `Pattern=" "`, load: true},
		{attrs: `MinLen="0"`, want: "has to be positive"},
		{attrs: `MaxLen="0"`, want: "has to be positive"},
		{attrs: `MinLen="1"`, load: true},
		{attrs: `MaxLen="1"`, load: true},
		{attrs: `MinLen="1" MaxLen="4"`, load: true},
		// THE INVERTED PAIR. The row above is the one this table had,
		// and it is the reason nothing saw this: an ordered pair proves
		// a pair is accepted and says nothing about which order.
		//
		// validate.Len rejects anything shorter than the minimum or
		// longer than the maximum, so 5..3 rejects every non-empty
		// value and the field can never become valid — the outcome the
		// NUMERIC block thirty lines away already refused. Raised in
		// review of #470.
		{attrs: `MinLen="5" MaxLen="3"`, want: "the range is empty"},
		// EQUAL IS A RANGE OF ONE, not an empty one, and this arm is
		// what stops the fix being written with >=.
		{attrs: `MinLen="3" MaxLen="3"`, load: true},
		// THE NUMERIC HALF, here rather than in its own table, because
		// what this finding was about is the two blocks DISAGREEING.
		// Asserting them side by side is what a future divergence trips
		// over.
		{attrs: `MinValue="5" MaxValue="3"`, want: "the range is empty"},
		{attrs: `MinValue="3" MaxValue="5"`, load: true},
	} {
		src := harnessFor("Required", `<Validate `+tc.attrs+`/>`)
		_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
		switch {
		case tc.load && err != nil:
			t.Errorf("<Validate %s/> is refused: %v", tc.attrs, err)
		case !tc.load && err == nil:
			t.Errorf("<Validate %s/> loads and declares nothing. Accepted-but-ignored "+
				"markup is the failure mode this package refuses", tc.attrs)
		case !tc.load && err != nil && !strings.Contains(err.Error(), tc.want):
			t.Errorf("<Validate %s/> is refused with %v; want a message containing %q",
				tc.attrs, err, tc.want)
		}
	}
}

// TestAnEmptyBoundIsNotAnUnreadableOne is the distinction litIntGrammar
// spent a commit drawing, stopping one element short of <Validate>'s
// numeric bounds.
//
// "want a number" is the UNREADABLE-value message, and an empty value is
// not unreadable — it is an attribute nobody finished writing. The float
// reader is a single ParseFloat, which cannot tell the two apart, so the
// split has to be made before it.
//
// Asserted against emptyLiteralWhy itself rather than a quoted phrase,
// because the whole point of that constant is that every reader says the
// same words: a fix that wrote a new sentence here would pass a quoted
// assertion and reintroduce the divergence. Raised in review of #470.
func TestAnEmptyBoundIsNotAnUnreadableOne(t *testing.T) {
	for _, attr := range []string{"MinValue", "MaxValue"} {
		for _, v := range []string{"", " "} {
			src := harnessFor(attr, `<Validate `+attr+`="`+v+`"/>`)
			_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
			if err == nil {
				t.Errorf("<Validate %s=%q/> loads", attr, v)
				continue
			}
			if !strings.Contains(err.Error(), emptyLiteralWhy) {
				t.Errorf("<Validate %s=%q/> is refused with %v; want the shared "+
					"empty-literal sentence every other reader gives", attr, v, err)
			}
			// AND NOT THE UNREADABLE ONE, which is the message it used
			// to give and the half a positive assertion cannot see.
			if strings.Contains(err.Error(), "want a number") {
				t.Errorf("<Validate %s=%q/> still answers with the unreadable-value "+
					"message: %v", attr, v, err)
			}
		}
	}
}

// TestTheUnboundedBoundRefusalNamesWhatActuallyHappens is finding 3 of
// round five, and it is a message test because the message was the bug.
//
// The refusal said every non-finite bound "can never fire". That is true
// of NaN and exactly backwards for MinValue="+Inf" and MaxValue="-Inf",
// which fire on every value there is — an author told the opposite of
// what their document does goes looking in the wrong place. There are
// three outcomes, not one, and each has its own sentence now.
func TestTheUnboundedBoundRefusalNamesWhatActuallyHappens(t *testing.T) {
	for _, tc := range []struct{ attr, v, want string }{
		{"MinValue", "NaN", "never fires"},
		{"MaxValue", "NaN", "never fires"},
		// THE DEFAULT, WRITTEN OUT: a minimum of -Inf and a maximum of
		// +Inf are the bounds the attribute already carries when absent.
		{"MinValue", "-Inf", "the bound you already had"},
		{"MaxValue", "+Inf", "the bound you already had"},
		// AND THE OTHER WAY ROUND, which is the half the old message got
		// wrong: these fire on everything.
		{"MinValue", "+Inf", "nothing can satisfy"},
		{"MaxValue", "-Inf", "nothing can satisfy"},
	} {
		src := harnessFor(tc.attr, `<Validate `+tc.attr+`="`+tc.v+`"/>`)
		_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
		if err == nil {
			t.Errorf("<Validate %s=%q/> loads", tc.attr, tc.v)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("<Validate %s=%q/> is refused with %v; want it to say %q — the "+
				"consequence differs by case, and one sentence for all four said the "+
				"opposite of what happens for half of them", tc.attr, tc.v, err, tc.want)
		}
	}
}

// TestABoundThatCanNeverFireIsALoadError is finding 2 of the second
// round, and the reason it is a load error rather than a lint is that
// NOTHING downstream can see it.
//
// validate.NumberRange compares the field's value against the bounds,
// and every comparison against NaN is false — so MinValue="NaN" installs
// a rule that passes whatever is typed, and the marker never appears.
// The empty-range check beside it (minV > maxV) is blind for the same
// reason: NaN > NaN is false too, so a NaN bound reads as a perfectly
// ordinary range.
//
// The accepting arm is not decoration. The canonical-spelling rule that
// governs ints is deliberately NOT applied to a float, because "1.50"
// and "1e3" are honest spellings that say different things about
// precision and scale — so those have to keep loading, or the refusal
// above is a different rule from the one that was written.
func TestABoundThatCanNeverFireIsALoadError(t *testing.T) {
	for _, tc := range []struct {
		attr, v string
		load    bool
	}{
		{attr: "MinValue", v: "NaN"},
		{attr: "MaxValue", v: "NaN"},
		{attr: "MinValue", v: "+Inf"},
		{attr: "MaxValue", v: "-Inf"},
		{attr: "MinValue", v: "1", load: true},
		{attr: "MinValue", v: "1.50", load: true},
		{attr: "MinValue", v: "1e3", load: true},
		{attr: "MinValue", v: "-2", load: true},
	} {
		src := `<Gooey><TextBox Text="{{.S}}"><Validate ` + tc.attr + `="` + tc.v +
			`"/></TextBox></Gooey>`
		_, err := Build([]byte(src), defaultsContext())
		if tc.load && err != nil {
			t.Errorf(`<Validate %s=%q> is refused: %v`, tc.attr, tc.v, err)
		}
		if !tc.load && err == nil {
			t.Errorf(`<Validate %s=%q> loads. It installs a bound no value can be `+
				`on the wrong side of, so the field validates everything and the `+
				`marker never appears`, tc.attr, tc.v)
		}
	}
}
