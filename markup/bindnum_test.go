package markup

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Text bindings over non-string scalars.
//
// Two separate claims live in this file and they fail differently:
//
//   - the FORMAT claim — a bound int renders "42" and not "%!d(int=42)".
//     A cell assertion answers that.
//   - the SUBSCRIPTION claim — the label repaints when, and only when,
//     the number changes. A cell assertion does NOT answer that: read
//     the source eagerly at build time and close over the resulting
//     string, and every cell assertion here still passes on the first
//     frame. Only a damage count separates "the right characters" from
//     "a live binding", which is why TestNumericTextRepaintsExactlyOne
//     is the pin and the rest is decoration without it.

// numDoc and numLine are deliberately private to this file rather than
// borrowed from the neighbours: several agents are editing this package
// at once, and a test that compiles against nobody else's helpers keeps
// its failures attributable.
func numDoc(body string) string {
	return `<Gooey xmlns="wonderforge.io/gooey/2026">` + body + `</Gooey>`
}

func numLine(b *render.Buffer, y, w int) string {
	var sb strings.Builder
	for x := 0; x < w; x++ {
		sb.WriteRune(b.At(x, y).Rune)
	}
	return strings.TrimRight(sb.String(), " ")
}

// scalarCase is one bindable type: a source handle, the text it should
// produce, and a mutation with the text that mutation should produce.
type scalarCase struct {
	name   string // the type name as it appears in textBindableTypes
	handle any
	before string
	after  string
	set    func()
}

// scalarCases covers every type textBindableTypes names, one case each.
// TestTextBindableTypesListMatchesTheSwitch is what holds that "one
// each" to account.
func scalarCases() []scalarCase {
	s := prop.NewSource("ab")
	i := prop.NewSource(41)
	i64 := prop.NewSource(int64(41))
	// 1234567.5 rather than a tidy 1.5: it is the value that tells the
	// three plausible float formats apart. strconv 'f'/-1 — the one
	// textSource documents — gives "1234567.5"; %v and 'g' switch to
	// "1.2345675e+06"; any fixed precision gives "1234567.50". A 1.5
	// renders identically under all three, so it pins nothing.
	f := prop.NewSource(1234567.5)
	b := prop.NewSource(false)
	d := prop.NewSource(90 * time.Second)
	c := prop.NewSource(render.RGB(0xff, 0x88, 0x00))
	return []scalarCase{
		{"string", s, "ab", "cd", func() { s.Set("cd") }},
		{"int", i, "41", "42", func() { i.Set(42) }},
		{"int64", i64, "41", "42", func() { i64.Set(42) }},
		{"float64", f, "1234567.5", "0.5", func() { f.Set(0.5) }},
		{"bool", b, "false", "true", func() { b.Set(true) }},
		{"time.Duration", d, "1m30s", "2m0s", func() { d.Set(2 * time.Minute) }},
		{"render.Color", c, "#ff8800", "", func() { c.Set(render.Color{}) }},
	}
}

// TestTextRendersEveryScalarType is the format claim. Each expectation
// is a decision defended in textSource's doc comment, so a change here
// is a change to that decision, not a test needing an update:
// base 10; shortest round-tripping decimal with no exponent; the Go
// "true"/"false"; Duration's own String; "#rrggbb" with the empty
// string for an unset color.
func TestTextRendersEveryScalarType(t *testing.T) {
	for _, tc := range scalarCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &Context{Values: map[string]any{"V": tc.handle}}
			w, err := Build([]byte(numDoc(`<Text>v=={{.V}}</Text>`)), ctx)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			c := gooey.NewComposer(w, 24, 1)
			f, _ := c.Frame()
			if got, want := numLine(f.Cells, 0, 24), "v=="+tc.before; got != want {
				t.Fatalf("rendered %q, want %q", got, want)
			}
		})
	}
}

// TestTextRendersPlainScalarValues covers the same vocabulary held as a
// CONSTANT rather than a handle. The context has always accepted a bare
// string there; a bare int being a load error while a bare string works
// is the wart this arm removes.
func TestTextRendersPlainScalarValues(t *testing.T) {
	ctx := &Context{Values: map[string]any{
		"N": 42,
		"F": 0.5,
		"B": true,
		"D": 250 * time.Millisecond,
		"C": render.RGB(0, 0x80, 0xff),
	}}
	w, err := Build([]byte(numDoc(`<Text>{{.N}}/{{.F}}/{{.B}}/{{.D}}/{{.C}}</Text>`)), ctx)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	c := gooey.NewComposer(w, 40, 1)
	f, _ := c.Frame()
	if got, want := numLine(f.Cells, 0, 40), "42/0.5/true/250ms/#0080ff"; got != want {
		t.Fatalf("rendered %q, want %q", got, want)
	}
}

// numPage is two sibling labels in a VStack: one bound to the scalar
// under test, one bound to a string nothing touches. The neighbour is
// load-bearing — with a single label, "exactly one repainted" and
// "everything repainted" are the same number.
//
// A VStack gives each label a full-width row, so a value whose width
// changes ("false" → "true") cannot move the neighbour's bounds and
// dirty it for a reason that has nothing to do with subscription.
const numPage = `<Gooey xmlns="wonderforge.io/gooey/2026">
  <VStack>
    <Text>v={{.V}}</Text>
    <Text>{{.Other}}</Text>
  </VStack>
</Gooey>`

// TestNumericTextRepaintsExactlyOne is the subscription pin, and it is
// the only assertion in this file that can tell a live binding from a
// snapshot.
//
// The mechanism it pins: textSource returns a CLOSURE holding the
// handle's Get, and that closure is called from inside the text
// computed's evaluation, so prop.node.recordRead sees the computed on
// evalStack and records the edge. Convert eagerly instead — read
// h.Get() at build time, close over the string — and the load
// succeeds, the first frame is pixel-identical, and the label is deaf
// forever. The three counts below are what notice:
//
//	frame 1 after a Set   → exactly 1 (the label, not its neighbour)
//	frame 2 with no Set   → exactly 0 (nothing repaints on a whim)
//
// It runs over EVERY bindable type, so making any single arm of the
// switch eager turns exactly that subtest red.
func TestNumericTextRepaintsExactlyOne(t *testing.T) {
	for _, tc := range scalarCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &Context{Values: map[string]any{
				"V":     tc.handle,
				"Other": prop.NewSource("neighbour"),
			}}
			w, err := Build([]byte(numPage), ctx)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			c := gooey.NewComposer(w, 24, 2)

			// The first frame paints the whole tree, and its count is
			// what makes the "exactly 1" below mean anything: if the
			// whole page were one paint node, 1 and "everything" would
			// be the same number and the assertion would be vacuous.
			if _, painted := c.Frame(); painted < 2 {
				t.Fatalf("the first frame painted %d components; the page needs more than "+
					"one paint node for \"exactly 1\" to be a subset", painted)
			}
			if _, painted := c.Frame(); painted != 0 {
				t.Fatalf("a second frame with no change painted %d components, want 0", painted)
			}

			tc.set()
			f, painted := c.Frame()
			if painted != 1 {
				t.Fatalf("setting the source repainted %d components, want exactly 1 "+
					"(the bound label; its neighbour and the VStack must stay clean)", painted)
			}
			if got, want := numLine(f.Cells, 0, 24), "v="+tc.after; got != want {
				t.Fatalf("after the Set the label reads %q, want %q", got, want)
			}
			if got := numLine(f.Cells, 1, 24); got != "neighbour" {
				t.Fatalf("the neighbour row reads %q, want %q", got, "neighbour")
			}

			// And it settles: a fourth frame paints nothing again, so the
			// count of 1 above was a repaint and not a treadmill.
			if _, painted := c.Frame(); painted != 0 {
				t.Fatalf("the frame after the repaint painted %d components, want 0", painted)
			}
		})
	}
}

// TestUnsupportedTextTypeNamesWhatWorks: the error for a genuinely
// unrenderable type has to say what IS renderable, because the reader
// is holding a viewmodel and needs to know whether to change the field
// or reach for a converter.
func TestUnsupportedTextTypeNamesWhatWorks(t *testing.T) {
	ctx := &Context{Values: map[string]any{
		"Pic": prop.NewSource(image.Image(image.NewRGBA(image.Rect(0, 0, 1, 1)))),
	}}
	_, err := Build([]byte(numDoc(`<Text>{{.Pic}}</Text>`)), ctx)
	if err == nil {
		t.Fatal("binding an image property into text loaded clean")
	}
	msg := err.Error()
	if !strings.Contains(msg, "image.Image") {
		t.Errorf("the error does not name the offending type: %v", err)
	}
	for _, name := range strings.Split(textBindableTypes, ", ") {
		if !strings.Contains(msg, name) {
			t.Errorf("the error does not offer %q as an alternative: %v", name, err)
		}
	}
}

// TestTextBindableTypesListMatchesTheSwitch is the drift guard. The
// error message names its types in prose, and prose cannot be derived
// from a type switch without reflection — which is exactly what this
// package may not do. So the correspondence is asserted instead, in
// both directions:
//
//   - every name in textBindableTypes has a case here that BUILDS;
//   - a type absent from the list FAILS to build.
//
// The second half is what makes the first half discriminating: without
// it, a list naming every type in Go would pass.
func TestTextBindableTypesListMatchesTheSwitch(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range scalarCases() {
		if covered[tc.name] {
			t.Fatalf("scalarCases lists %q twice", tc.name)
		}
		covered[tc.name] = true

		ctx := &Context{Values: map[string]any{"V": tc.handle}}
		if _, err := Build([]byte(numDoc(`<Text>{{.V}}</Text>`)), ctx); err != nil {
			t.Errorf("%s is named in textBindableTypes but does not bind: %v", tc.name, err)
		}
	}
	for _, name := range strings.Split(textBindableTypes, ", ") {
		if !covered[name] {
			t.Errorf("textBindableTypes promises %q, but no case here proves it binds", name)
		}
	}
	if len(covered) != len(strings.Split(textBindableTypes, ", ")) {
		t.Errorf("%d cases cover %d promised types", len(covered), len(strings.Split(textBindableTypes, ", ")))
	}

	// The other direction: types the list does NOT name must not bind.
	// []float64 is a real viewmodel type here (Sparkline histories), and
	// render.Style is the other property type the control plane carries
	// without a text form.
	unsupported := map[string]any{
		"[]float64":    prop.NewSource([]float64{1, 2}),
		"render.Style": prop.NewSource(render.Style{Bold: true}),
		"uint8":        uint8(7),
	}
	// err != nil is not enough here. Any load failure at all would
	// satisfy it — a typo in numDoc, an unrelated regression in <Text>,
	// a resolve error — and the arm would keep passing while proving
	// nothing about the type switch. So the SHAPE is asserted: the
	// message must name the offending type and offer the supported list,
	// which is only produced on the one path that matters.
	for name, h := range unsupported {
		ctx := &Context{Values: map[string]any{"V": h}}
		_, err := Build([]byte(numDoc(`<Text>{{.V}}</Text>`)), ctx)
		if err == nil {
			t.Errorf("%s binds into text but textBindableTypes does not name it", name)
			continue
		}
		if msg := err.Error(); !strings.Contains(msg, name) || !strings.Contains(msg, textBindableTypes) {
			t.Errorf("%s failed to load, but not with the unsupported-type error: %v", name, err)
		}
	}
}

// TestMixedScalarsInOneTextEachSubscribe: a label interpolating two
// different scalar types subscribes to BOTH. One conversion arm built
// eagerly while the other is live still paints the right first frame
// and still repaints on the live one — this is the case where a
// single-binding test would report success.
func TestMixedScalarsInOneTextEachSubscribe(t *testing.T) {
	n := prop.NewSource(1)
	d := prop.NewSource(time.Second)
	ctx := &Context{Values: map[string]any{"N": n, "D": d}}
	w, err := Build([]byte(numDoc(`<Text>{{.N}} in {{.D}}</Text>`)), ctx)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	c := gooey.NewComposer(w, 24, 1)
	c.Frame()

	n.Set(2)
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("setting the int repainted %d components, want 1", painted)
	}
	if got := numLine(f.Cells, 0, 24); got != "2 in 1s" {
		t.Fatalf("after the int Set the label reads %q, want %q", got, "2 in 1s")
	}

	d.Set(90 * time.Second)
	f, painted = c.Frame()
	if painted != 1 {
		t.Fatalf("setting the duration repainted %d components, want 1", painted)
	}
	if got := numLine(f.Cells, 0, 24); got != "2 in 1m30s" {
		t.Fatalf("after the duration Set the label reads %q, want %q", got, "2 in 1m30s")
	}
}
