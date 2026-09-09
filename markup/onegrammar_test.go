package markup

import (
	"errors"
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
	t.Logf("verified %d of %d int-literal attributes; %d probes unverified",
		len(verified), len(targets), len(unverified))
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

// TestEveryDurationAnswersTheSameWayIsDerived is finding 5. Two of the
// nine KindDuration attributes read their value by hand rather than
// through optDuration, and hand-rolled is where they disagreed:
// <ToastHost Duration="-5s"> set a negative dismissal delay, and
// <Timer Interval=""> answered "needs an Interval" where the other seven
// say an empty one is a typo and name the spelling that asks for the
// default.
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
	for _, probe := range []struct{ value, want string }{
		{"", "is a typo"},
		{"-5s", "must be positive"},
	} {
		var verified int
		for _, tg := range targets {
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
			}
		}
		if verified == 0 {
			t.Errorf("no duration attribute was refused %q with %q, so this arm is "+
				"passing on harness errors rather than on the rule", probe.value, probe.want)
		}
		t.Logf("%q: %d of %d duration attributes verified", probe.value, verified, len(targets))
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
