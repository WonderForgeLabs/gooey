package markup

import (
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

// bindSweep is every attribute of every non-opaque built-in, with the
// harness needed to build it. Opaque elements (//gooey:catalog-opaque)
// are excluded by their own Opaque field rather than by a second list, so
// the exclusion cannot drift from the one the generator applies.
func bindSweep(t *testing.T, want Binds, value func(AttrSpec) string) (checked int) {
	t.Helper()
	for _, def := range definedElements() {
		if def.Opaque != "" {
			continue
		}
		for _, a := range def.Attrs {
			if a.Binds != want {
				continue
			}
			v := value(a)
			if v == "" {
				continue
			}
			src := harnessFor(a.Name, probeElement(t, def, a.Name, v))
			if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err == nil {
				t.Errorf("<%s %s=%q> loaded, but %s is declared Binds=%q",
					def.Name, a.Name, v, a.Name, want)
			}
			checked++
		}
	}
	return checked
}

// TestEveryBindOnlyAttributeRefusesALiteral is the sweep the eleven-row
// list was standing in for. It is the one that would have caught the
// <Frozen Active> corruption: Active is BindsBinding, so a literal has to
// be a load error, and the declaration is what says so.
func TestEveryBindOnlyAttributeRefusesALiteral(t *testing.T) {
	n := bindSweep(t, BindsBinding, literalFor)
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
	n := bindSweep(t, BindsLiteral, func(a AttrSpec) string {
		switch a.Kind {
		case KindInt, KindBool:
			return "{{.S}}" // unparseable as either, and the shape an author would try
		}
		return ""
	})
	if n == 0 {
		t.Fatal("no literal int/bool attributes were checked: this sweep would pass vacuously")
	}
	t.Logf("checked %d literal int/bool attributes", n)
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
// UNREACHABLE ELEMENTS ARE SKIPPED BY BUILDING THEM FIRST, not by name.
// <Companion> takes no children, <Image> needs a file system, <Validate>
// belongs on an input — none can be built by this harness at all, and a
// list of them would be the enumeration this whole file exists to avoid.
// Building with the attribute OMITTED asks the question directly, and the
// skip count is logged so a silent collapse in coverage is visible.
func TestEveryAttributeThatSaysItTakesALiteralAcceptsOne(t *testing.T) {
	var checked, skipped int
	for _, def := range definedElements() {
		if def.Opaque != "" {
			continue
		}
		for _, a := range def.Attrs {
			if a.Binds != BindsLiteral && a.Binds != BindsEither {
				continue
			}
			v := validLiteralFor(a)
			if v == "" {
				continue
			}
			omitted := harnessFor(a.Name, probeElement(t, def, a.Name, ""))
			if _, err := Build([]byte("<Gooey>"+omitted+"</Gooey>"), defaultsContext()); err != nil {
				skipped++
				continue
			}
			checked++
			src := harnessFor(a.Name, probeElement(t, def, a.Name, v))
			_, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext())
			if err == nil {
				continue
			}
			if strings.Contains(err.Error(), "is not a binding expression") {
				t.Errorf("<%s %s=%q> is declared Binds=%q, but the loader requires "+
					"a binding:\n\t%v", def.Name, a.Name, v, a.Binds, err)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no literal-taking attributes were checked: this sweep would pass vacuously")
	}
	t.Logf("checked %d literal-taking attributes (%d unreachable in this harness)",
		checked, skipped)
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

// TestAnEmptyLiteralMeansOmitted pins the third state explicitly. Empty
// is how the designer writes "not set", and turning it into a load error
// would make the palette emit markup that does not load — the exact
// failure the declaration exists to prevent.
func TestAnEmptyLiteralMeansOmitted(t *testing.T) {
	for _, src := range []string{
		`<VStack Gap=""><Text>a</Text></VStack>`,
		`<Text Bold="">ab</Text>`,
	} {
		if _, err := Build([]byte("<Gooey>"+src+"</Gooey>"), defaultsContext()); err != nil {
			t.Errorf("%s is refused, but an empty attribute means omitted: %v", src, err)
		}
	}
}

// STILL UNSWEPT, and deliberately so: four BindsLiteral TEXT attributes
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
