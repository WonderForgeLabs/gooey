package markup

import (
	"strings"
	"testing"
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

// TestALiteralIntRefusesASignedSpelling is the leading-plus finding.
//
// strconv.Atoi accepts both signs, so Gap="+3" loaded and meant 3 while
// Gap="-3" was refused — the "refused" int quietly had a second
// spelling. Both arms matter: the minus one is what says the fix did not
// simply stop parsing signs at all.
func TestALiteralIntRefusesASignedSpelling(t *testing.T) {
	for _, v := range []string{"+3", "-3"} {
		src := `<HStack Gap="` + v + `"><Text>a</Text><Text>b</Text></HStack>`
		if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err == nil {
			t.Errorf("<HStack Gap=%q> loads. A whole number written literally has one "+
				"spelling; a second one that means the same thing is a value two "+
				"documents disagree about", v)
		}
	}
	// NON-VACUITY: the unsigned spelling must still load, or the test
	// above passes for a reader that refuses every int.
	ok := `<HStack Gap="3"><Text>a</Text><Text>b</Text></HStack>`
	if _, err := Build([]byte("<Gooey>"+ok+"</Gooey>"), defaultsContext()); err != nil {
		t.Fatalf(`<HStack Gap="3"> no longer loads, so the assertions above are about `+
			`an int reader that refuses everything: %v`, err)
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
