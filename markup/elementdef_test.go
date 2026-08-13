package markup

import "testing"

// The behavioural axes are DERIVED from ElementDef.Proto by type
// assertion, not declared as booleans. This pins that the derivation
// works, because the failure mode is silent: a nil or wrong Proto yields
// all-false, which reads exactly like a legitimately plain element.
//
// That is not hypothetical, and <Validate> has now been wrong TWICE for
// two different reasons under the generator. First it reported
// NonVisual:false, because its Go type is declared inside this package
// so the component literal carried no package qualifier and every axis
// defaulted to false. Then, once that was fixed by reading the method
// set from the AST, it reported Attaches:false and HasLayout:false —
// because an AST method scan sees only DIRECTLY DECLARED methods, and
// <Validate> gets Attach and LayoutProps from an embedded gooey.Base.
//
// A runtime type assertion has neither failure mode: it asks the same
// question the framework itself asks, of the same type, including
// everything promoted through embedding. That is the accuracy argument
// for deriving these from a Proto rather than generating them.
func TestElementAxesAreDerivedFromTheProto(t *testing.T) {
	cases := []struct {
		name                                      string
		nonVisual, focusable, attaches, hasLayout bool
	}{
		{"Button", false, true, true, true},
		{"Text", false, false, true, true},
		{"Companion", true, false, true, true},
		{"Validate", true, false, true, true}, // embeds gooey.Base: see below
	}
	for _, c := range cases {
		d, ok := elementDefs[c.name]
		if !ok {
			t.Errorf("<%s> is not in the element registry", c.name)
			continue
		}
		nv, f, a, hl := d.axes()
		if nv != c.nonVisual || f != c.focusable || a != c.attaches || hl != c.hasLayout {
			t.Errorf("<%s> axes = nonVisual:%v focusable:%v attaches:%v hasLayout:%v, want %v %v %v %v",
				c.name, nv, f, a, hl, c.nonVisual, c.focusable, c.attaches, c.hasLayout)
		}
	}
}

// TestDeclaredElementsCarryAProtoOrSayWhyNot — a nil Proto makes every
// axis false, which is indistinguishable from a plain visual element. It
// is legal only for a pseudo-element, which must also say it is not
// fully known.
func TestDeclaredElementsCarryAProtoOrSayWhyNot(t *testing.T) {
	for _, d := range definedElements() {
		if d.Proto != nil {
			continue
		}
		if d.Known {
			t.Errorf("<%s> has no Proto but claims Known: every behavioural axis will silently read false", d.Name)
		}
		if d.Opaque == "" {
			t.Errorf("<%s> has no Proto and no reason recorded", d.Name)
		}
	}
}

// TestCompanionDefMatchesItsLegacyTable — <Companion> is validated at
// load against companionAttrs by checkCompanionAttrs, and described to
// consumers by its ElementDef. Two tables, one truth. They must agree,
// or the loader and the catalog disagree about the same element.
//
// This is the drift the restructure is meant to make impossible for
// everything else; <Companion> keeps both because its check predates the
// registry, so it needs the guard the colocation would otherwise give.
func TestCompanionDefMatchesItsLegacyTable(t *testing.T) {
	d := elementDefs["Companion"]
	if d == nil {
		t.Fatal("<Companion> is not in the element registry")
	}
	declared := map[string]bool{}
	for _, a := range d.Attrs {
		declared[a.Name] = true
	}
	for n := range companionAttrs {
		if n == "Name" {
			continue // universal, not part of any element's own set
		}
		if !declared[n] {
			t.Errorf("companionAttrs has %q but the ElementDef does not: the loader accepts an attribute the catalog never offers", n)
		}
		delete(declared, n)
	}
	for n := range declared {
		t.Errorf("the ElementDef offers %q but companionAttrs rejects it: the catalog offers an attribute the loader refuses", n)
	}
}

// TestOpenElementDeclaresOnlyItsBuiltinHalf — an Open element's Attrs is
// the static half; the Context supplies the rest. If someone ever
// "helpfully" bakes Context.Rules into the literal, the element stops
// being context-sensitive and starts refusing valid markup.
func TestOpenElementDeclaresOnlyItsBuiltinHalf(t *testing.T) {
	d := elementDefs["Validate"]
	if d == nil {
		t.Fatal("<Validate> is not in the element registry")
	}
	if !d.Open {
		t.Fatal("<Validate> must be Open")
	}
	if len(d.Attrs) != len(validateBuiltins) {
		t.Errorf("<Validate> declares %d attrs but validateBuiltins has %d; the literal and the list buildValidate checks must be the same set",
			len(d.Attrs), len(validateBuiltins))
	}
}

// TestDynamicAttrElementsAreExactlyTheseOnes pins the cross-check's
// escape hatch.
//
// DynamicAttrs skips an element in internal/catalogen, so an element
// that acquires it stops being guarded against over-declaration — the
// one direction rejection cannot see. That is sometimes necessary, and
// it must never be quiet: adding one is an edit to this list, with a
// reviewer looking at it.
//
// Both current entries range over e.Attrs rather than reading names, and
// both had a declared table before the registry existed. That is not a
// coincidence — ranging is WHY they needed one.
func TestDynamicAttrElementsAreExactlyTheseOnes(t *testing.T) {
	want := map[string]bool{"StatusBar": true, "Validate": true}
	got := map[string]bool{}
	for _, d := range definedElements() {
		if d.DynamicAttrs == "" {
			continue
		}
		got[d.Name] = true
	}
	for n := range want {
		if !got[n] {
			t.Errorf("<%s> no longer has DynamicAttrs; remove it from this list", n)
		}
	}
	for n := range got {
		if !want[n] {
			t.Errorf("<%s> gained DynamicAttrs, so it is no longer checked for over-declaration. "+
				"That is allowed, but add it here with why its attributes cannot be read by name.", n)
		}
	}
}
