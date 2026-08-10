package validate

import (
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
