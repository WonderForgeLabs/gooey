package validate

import (
	"math"
	"testing"

	"github.com/WonderForgeLabs/gooey/prop"
)

func TestStringRules(t *testing.T) {
	cases := []struct {
		name string
		rule Rule[string]
		in   string
		want string
	}{
		{"required fails empty", Required(""), "", "required"},
		{"required fails whitespace", Required(""), "   ", "required"},
		{"required passes content", Required(""), "x", ""},
		{"required custom message", Required("name it"), "", "name it"},

		{"len passes empty", Len(3, 0, ""), "", ""},
		{"len fails short", Len(3, 0, ""), "ab", "at least 3 characters"},
		{"len passes at min", Len(3, 0, ""), "abc", ""},
		{"len fails long", Len(0, 2, ""), "abc", "at most 2 characters"},
		{"len both bounds message", Len(2, 4, ""), "abcde", "must be 2–4 characters"},
		{"len counts runes not bytes", Len(0, 3, ""), "héllo", "at most 3 characters"},
		{"len custom message", Len(3, 0, "too short"), "a", "too short"},

		{"pattern passes empty", Pattern(`^[a-z]+$`, ""), "", ""},
		{"pattern fails mismatch", Pattern(`^[a-z]+$`, ""), "ab1", "invalid format"},
		{"pattern passes match", Pattern(`^[a-z]+$`, ""), "ab", ""},
		{"pattern custom message", Pattern(`@`, "not an email"), "nope", "not an email"},
	}
	for _, c := range cases {
		if got := c.rule(c.in); got != c.want {
			t.Errorf("%s: rule(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// The DataAnnotations vocabulary, boundaries and unicode included. Each
// case is (rule, input, wanted message) — "" meaning the input passes.
func TestDataAnnotationRules(t *testing.T) {
	cases := []struct {
		name string
		rule Rule[string]
		in   string
		want string
	}{
		// EmailAddress — stricter than .NET (a@b fails: no dotted domain).
		{"email passes empty", EmailAddress(""), "", ""},
		{"email plain", EmailAddress(""), "a@b.co", ""},
		{"email subdomain", EmailAddress(""), "someone@mail.example.com", ""},
		{"email plus tag", EmailAddress(""), "a+tag@b.co", ""},
		{"email no dot in domain", EmailAddress(""), "a@b", "not a valid email address"},
		{"email no at", EmailAddress(""), "ab.co", "not a valid email address"},
		{"email two ats", EmailAddress(""), "a@b@c.co", "not a valid email address"},
		{"email space", EmailAddress(""), "a b@c.co", "not a valid email address"},
		{"email trailing dot", EmailAddress(""), "a@b.", "not a valid email address"},
		{"email unicode local and IDN domain", EmailAddress(""), "ünïcode@exämple.dé", ""},
		{"email custom message", EmailAddress("bad mail"), "nope", "bad mail"},

		// Url — host required; the empty-hostname bypass is closed.
		{"url passes empty", URL(""), "", ""},
		{"url https", URL(""), "https://example.com", ""},
		{"url with path and query", URL(""), "http://example.com/a/b?c=d", ""},
		{"url ftp", URL(""), "ftp://files.example.com", ""},
		{"url uppercase scheme", URL(""), "HTTPS://example.com", ""},
		{"url no scheme", URL(""), "example.com", "not a valid URL"},
		{"url bad scheme", URL(""), "file:///etc/passwd", "not a valid URL"},
		{"url empty host", URL(""), "http://", "not a valid URL"},
		{"url space", URL(""), "http://exa mple.com", "not a valid URL"},

		// Phone — 7..15 digits, extension excluded from the count.
		{"phone passes empty", Phone(""), "", ""},
		{"phone plain", Phone(""), "5550101", ""},
		{"phone formatted", Phone(""), "+1 (555) 010-9999", ""},
		{"phone with extension", Phone(""), "555 010 9999 x42", ""},
		{"phone six digits", Phone(""), "555010", "not a valid phone number"},
		{"phone sixteen digits", Phone(""), "1234567890123456", "not a valid phone number"},
		{"phone letters", Phone(""), "555-CALL", "not a valid phone number"},

		// CreditCard — Luhn plus a 12..19 length window.
		{"card passes empty", CreditCard(""), "", ""},
		{"card visa test number", CreditCard(""), "4111111111111111", ""},
		{"card spaced", CreditCard(""), "4111 1111 1111 1111", ""},
		{"card dashed", CreditCard(""), "4111-1111-1111-1111", ""},
		{"card amex 15", CreditCard(""), "378282246310005", ""},
		{"card bad checksum", CreditCard(""), "4111111111111112", "not a valid card number"},
		{"card too short but Luhn-valid", CreditCard(""), "0", "not a valid card number"},
		{"card letters", CreditCard(""), "4111a11111111111", "not a valid card number"},

		// Digits / Integer.
		{"digits passes empty", Digits(""), "", ""},
		{"digits plain", Digits(""), "0123", ""},
		{"digits rejects sign", Digits(""), "-1", "digits only"},
		{"digits rejects unicode digit", Digits(""), "٣٤", "digits only"},
		{"integer signed", Integer(""), "-42", ""},
		{"integer plus", Integer(""), "+42", ""},
		{"integer rejects decimal", Integer(""), "4.2", "must be a whole number"},

		// NumberRange — Range for a string input.
		{"number passes empty", NumberRange(1, 10, ""), "", ""},
		{"number at min", NumberRange(1, 10, ""), "1", ""},
		{"number at max", NumberRange(1, 10, ""), "10", ""},
		{"number below", NumberRange(1, 10, ""), "0.9", "must be between 1 and 10"},
		{"number above", NumberRange(1, 10, ""), "10.1", "must be between 1 and 10"},
		{"number unparseable", NumberRange(1, 10, ""), "ten", "must be between 1 and 10"},
		{"number surrounding space", NumberRange(1, 10, ""), " 5 ", ""},

		// MinLen / MaxLen, the annotation-named spellings of Len.
		{"minlen short", MinLen(3, ""), "ab", "at least 3 characters"},
		{"minlen ok", MinLen(3, ""), "abc", ""},
		{"maxlen long", MaxLen(2, ""), "abc", "at most 2 characters"},
		{"maxlen counts runes", MaxLen(3, ""), "héllo", "at most 3 characters"},
	}
	for _, c := range cases {
		if got := c.rule(c.in); got != c.want {
			t.Errorf("%s: rule(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestNumberRangeOneSided(t *testing.T) {
	atLeast := NumberRange(18, math.Inf(1), "")
	if got := atLeast("17"); got != "must be at least 18" {
		t.Errorf("one-sided min: %q", got)
	}
	if got := atLeast("1000000"); got != "" {
		t.Errorf("one-sided min, large value: %q", got)
	}
	atMost := NumberRange(math.Inf(-1), 100, "")
	if got := atMost("101"); got != "must be at most 100" {
		t.Errorf("one-sided max: %q", got)
	}
	if got := atMost("-5000"); got != "" {
		t.Errorf("one-sided max, small value: %q", got)
	}
}

// Compare is the confirm-password rule: the read inside it subscribes
// the confirmation field to the original, so editing the original
// re-validates the confirmation.
func TestCompareRule(t *testing.T) {
	password := prop.NewSource("secret")
	confirm := prop.NewSource("secret")
	errP := Field(confirm, Compare(password, ""))
	if got := errP.Get(); got != "" {
		t.Fatalf("matching: %q", got)
	}
	password.Set("changed")
	if got := errP.Get(); got != "does not match" {
		t.Fatalf("after editing the original: %q — the subscription did not carry", got)
	}
	confirm.Set("changed")
	if got := errP.Get(); got != "" {
		t.Fatalf("re-matched: %q", got)
	}
	// Non-string comparables work too — Compare is generic.
	a, b := prop.NewSource(3), prop.NewSource(4)
	if got := Field(b, Compare(a, "differs")).Get(); got != "differs" {
		t.Fatalf("int compare: %q", got)
	}
}

func TestRangeRule(t *testing.T) {
	r := Range(1, 10, "")
	if got := r(0); got != "must be between 1 and 10" {
		t.Errorf("below min: %q", got)
	}
	if got := r(11); got != "must be between 1 and 10" {
		t.Errorf("above max: %q", got)
	}
	if got := r(1); got != "" {
		t.Errorf("at min: %q, want valid", got)
	}
	if got := r(10); got != "" {
		t.Errorf("at max: %q, want valid", got)
	}
	rf := Range(0.5, 1.5, "out of range")
	if got := rf(2.0); got != "out of range" {
		t.Errorf("float custom message: %q", got)
	}
}

func TestPatternCompilesOnceAndPanicsEarly(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a bad expression must panic at construction, like regexp.MustCompile")
		}
	}()
	Pattern(`[`, "")
}

// The first failing rule wins: messages reveal one problem at a time,
// in the order the rules were declared.
func TestFieldFirstFailingRuleWins(t *testing.T) {
	src := prop.NewSource("")
	err := Field(src, Required(""), Len(3, 0, ""))
	if got := err.Get(); got != "required" {
		t.Fatalf("empty: %q, want the Required message first", got)
	}
	src.Set("ab")
	if got := err.Get(); got != "at least 3 characters" {
		t.Fatalf("short: %q, want the Len message once Required passes", got)
	}
	src.Set("abc")
	if got := err.Get(); got != "" {
		t.Fatalf("valid: %q, want empty", got)
	}
}

// A validator is a computed: it evaluates lazily and only when its
// source changed.
func TestFieldIsLazy(t *testing.T) {
	src := prop.NewSource("x")
	err := Field(src, Required(""))
	err.Get()
	n := err.Evals()
	err.Get()
	if err.Evals() != n {
		t.Fatal("a clean field re-evaluated on Get")
	}
	src.Set("y")
	err.Get()
	if err.Evals() != n+1 {
		t.Fatal("a dirtied field did not re-evaluate")
	}
}

// A rule that reads another property subscribes to it — cross-field
// validation through the ordinary graph.
func TestFieldCrossFieldRule(t *testing.T) {
	password := prop.NewSource("secret")
	confirm := prop.NewSource("secret")
	err := Field(confirm, func(s string) string {
		if s != password.Get() {
			return "passwords differ"
		}
		return ""
	})
	if got := err.Get(); got != "" {
		t.Fatalf("matching: %q", got)
	}
	password.Set("changed")
	if got := err.Get(); got != "passwords differ" {
		t.Fatalf("after the OTHER field changed: %q — the rule's read did not subscribe", got)
	}
}

func TestAllAggregates(t *testing.T) {
	a := prop.NewSource("")
	b := prop.NewSource("")
	fa := Field(a, Required(""))
	fb := Field(b, Required(""))
	ok := All(fa, fb)
	if ok.Get() {
		t.Fatal("two empty required fields reported valid")
	}
	a.Set("x")
	if ok.Get() {
		t.Fatal("one invalid field left, still reported valid")
	}
	b.Set("y")
	if !ok.Get() {
		t.Fatal("all fields valid, reported invalid")
	}
	b.Set("")
	if ok.Get() {
		t.Fatal("a field went invalid again, still reported valid")
	}
}

// The stabilization contract: All's outward property changes ONLY when
// validity flips, so a dependent (a submit button's paint node) is not
// invalidated by keystrokes that leave validity as it was.
func TestAllInvalidatesDependentsOnlyOnFlip(t *testing.T) {
	src := prop.NewSource("")
	err := Field(src, Required(""), Len(3, 0, ""))
	ok := All(err)

	// dep stands in for the submit button's paint node.
	dep := prop.NewComputed(func() bool { return ok.Get() })
	dep.Get()
	n := dep.Evals()

	src.Set("a") // "required" -> "at least 3 characters": still invalid
	if dep.Get(); dep.Evals() != n {
		t.Fatal("an edit that kept validity false re-evaluated the dependent")
	}
	src.Set("ab") // message unchanged, still invalid
	if dep.Get(); dep.Evals() != n {
		t.Fatal("an edit that changed nothing re-evaluated the dependent")
	}
	src.Set("abc") // the flip
	if dep.Get(); dep.Evals() != n+1 {
		t.Fatal("the validity flip did not reach the dependent")
	}
	src.Set("abcd") // still valid
	if dep.Get(); dep.Evals() != n+1 {
		t.Fatal("an edit that kept validity true re-evaluated the dependent")
	}
}

// Several aggregates may watch the same field: All subscribes through
// ordinary dependency edges, not by claiming the field's OnInvalidate
// hook, so a per-section gate and a whole-form gate coexist.
func TestTwoAllsShareAField(t *testing.T) {
	a := prop.NewSource("")
	b := prop.NewSource("x")
	fa := Field(a, Required(""))
	fb := Field(b, Required(""))
	section := All(fa)
	form := All(fa, fb)
	if section.Get() || form.Get() {
		t.Fatal("empty required field: both gates should be false")
	}
	a.Set("filled")
	if !section.Get() {
		t.Fatal("section gate missed the shared field's change")
	}
	if !form.Get() {
		t.Fatal("form gate missed the shared field's change")
	}
}
