package markup

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// A brace inside a backtick literal, from #286's review.
//
// The review called this "silently misroutes to the wrong error path"
// and rated it a misattributed diagnostic. Verified rather than
// inherited, and it is exactly that on Visibility — but the probe that
// established it also turned up the reason it matters more than it
// sounds: Visibility is the ONLY attribute with a second parser waiting
// behind the conditional grammar, so it is the only site where a
// non-matching value produces a confident wrong answer instead of a
// vague right one.
//
// THE REPO HAD ALREADY LEARNED THIS AND WRITTEN IT DOWN. The
// value-namespace grammar takes the identical shape of input —
// {{v:Echo `}}` `{{`}} — and TestBacktickLiteralMayContainBraces
// (values_test.go:282) has passed the whole time, because that grammar
// uses a hand-rolled scanner. Its comment gives the reason in one line:
// "This is why the scan is hand-rolled instead of a regexp." The
// conditional grammar then shipped as a regexp and reintroduced the
// exact case that note exists to warn about, from a file that could not
// see it. That the older test's NAME reads like it already covers this
// is the trap: it covers a different grammar, so grepping for "backtick"
// would have been reassuring and wrong.

func braceCtx() *Context {
	return &Context{
		Values: map[string]any{"Name": prop.NewSource("a}b"), "B": prop.NewSource(true)},
		Styles: map[string]render.Style{},
	}
}

func braceSrc(attr string) string {
	return `<Gooey xmlns="wonderforge.io/gooey/2026"><Text Name="t" ` + attr + `>hi</Text></Gooey>`
}

// The bug, at the site where it produced a wrong answer.
//
// Mutation: restore the `[^}]*?` operand class and this fails with
// "unknown visibility" — parseVisibility's error, naming a visibility
// word the user never wrote.
func TestABraceInsideABacktickLiteralIsStillAConditional(t *testing.T) {
	ctx := braceCtx()
	ctx.Named = map[string]gooey.Component{}
	if _, err := Build([]byte(braceSrc("Visibility=\"{{eq .Name `a}b`}}\"")), ctx); err != nil {
		t.Fatalf("a well-formed conditional was rejected: %v", err)
	}
}

// The other half, and the one that makes the fix worth its width: the
// operand really is compared, not merely parsed. Without this, the test
// above passes against a change that routes the expression to the
// conditional path and then mis-lexes the literal.
func TestTheBracedLiteralIsTheValueCompared(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        bool
	}{
		// The brace is INSIDE the literal, so the value that matches is
		// the one containing it. If the lexer stopped at the brace, the
		// operand would be "a" and both rows would come out wrong —
		// which is why both rows are here.
		{"equal", "a}b", true},
		{"truncated at the brace", "a", false},
		{"not equal", "a-b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := braceCtx()
			ctx.Values["Name"] = prop.NewSource(tc.value)
			if got := condOf(t, ctx, "{{eq .Name `a}b`}}").Get(); got != tc.want {
				t.Errorf("eq .Name `a}b` = %v with Name=%q, want %v", got, tc.value, tc.want)
			}
		})
	}
}

// THE REGRESSION GUARD FOR THE FIX ITSELF.
//
// The obvious repair — teach the regex about backtick literals — would
// have stopped matching an UNTERMINATED one, and unterminated is
// currently the case that produces cond.go's own best diagnostic. So
// this pins the error that a narrower fix would have destroyed while
// making the reported bug go away, which is the shape of regression
// that gets shipped.
func TestAnUnterminatedLiteralStillGetsTheLexersOwnDiagnostic(t *testing.T) {
	ctx := braceCtx()
	ctx.Named = map[string]gooey.Component{}
	_, err := Build([]byte(braceSrc("Visibility=\"{{eq .Name `ab}}\"")), ctx)
	if err == nil {
		t.Fatal("an unterminated backtick literal loaded clean")
	}
	if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("error is not the lexer's own: %v", err)
	}
}
