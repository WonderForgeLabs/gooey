package main

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/markup"
)

// This began as a probe for a proposed markup.Context.Decorate seam —
// does a decorator between a container and a named element survive
// PatchMarkup? — and the answer was no, for everything: control.childSlot
// was a closed type switch over six concrete containers, so ANY component
// between a container and a named element broke patching for the whole
// subtree beneath it.
//
// IT NOW PINS THE FIX RATHER THAN THE LIMITATION. childSlot asks for
// gooey.ChildSetter, so a decorator that can address its own child is
// patchable through, and one that cannot is refused BY ITS OWN
// DECLARATION rather than by not being on a list. The two arms below are
// exactly that pair, and the second is what keeps the first from being a
// claim that everything is patchable now.
//
// It lives in this module rather than in control/ because that is where
// it was written; the general constraint it measures is the framework's,
// and the interface's own contract is stated on gooey.ChildSetter.

// decorator stands in for whatever a Decorate seam would wrap an element
// in: a transparent one-child container that changes nothing visible.
// It implements gooey.ChildSetter, which is all it takes to be patchable
// through — one method, and the honest claim that index 0 is an address
// it can write.
type decorator struct {
	gooey.Base
	child gooey.Component
}

func (d *decorator) ChildComponents() []gooey.Component { return []gooey.Component{d.child} }
func (d *decorator) Measure(a gooey.Size) gooey.Size    { return gooey.MeasureChild(d.child, a) }
func (d *decorator) Arrange(b gooey.Rect)               { d.Base.Arrange(b); gooey.ArrangeChild(d.child, b) }
func (d *decorator) Render(*gooey.Frame)                {}

func (d *decorator) SetChild(i int, w gooey.Component) bool {
	if i != 0 {
		return false
	}
	d.child = w
	return true
}

// opaqueBox is the OTHER half of the contract: a one-child container that
// does not implement gooey.ChildSetter, standing in for every container
// whose ChildComponents builds a list — ItemsView's realized rows, Tabs'
// header-plus-page — where index i is not an address.
type opaqueBox struct {
	gooey.Base
	child gooey.Component
}

func (o *opaqueBox) ChildComponents() []gooey.Component { return []gooey.Component{o.child} }
func (o *opaqueBox) Measure(a gooey.Size) gooey.Size    { return gooey.MeasureChild(o.child, a) }
func (o *opaqueBox) Arrange(b gooey.Rect)               { o.Base.Arrange(b); gooey.ArrangeChild(o.child, b) }
func (o *opaqueBox) Render(*gooey.Frame)                {}

type probeHost struct{ c *gooey.Composer }

func (h *probeHost) Post(fn func())            { fn() }
func (h *probeHost) Composer() *gooey.Composer { return h.c }
func (h *probeHost) Swap(gooey.Component)      {}

// TestPatchMarkupThroughADecorator measures what it costs to put a
// component between a container and a named element — which is what a
// Decorate seam, and a design surface's selection chrome, would do to
// every named element in the document.
func TestPatchMarkupThroughADecorator(t *testing.T) {
	// Baseline: the same page WITHOUT a decorator has to patch, or the
	// probe proves nothing about decorators.
	t.Run("undecorated patches", func(t *testing.T) {
		ctx := &markup.Context{}
		root, err := markup.Build([]byte(
			`<Gooey><VStack><Text Name="T">before</Text></VStack></Gooey>`), ctx)
		if err != nil {
			t.Fatal(err)
		}
		svc := control.NewService(&probeHost{gooey.NewComposer(root, 40, 10)}, ctx)
		if _, err := svc.PatchMarkup("T", `<Gooey><Text Name="T">after</Text></Gooey>`); err != nil {
			t.Fatalf("an undecorated named element must patch: %v", err)
		}
	})

	t.Run("decorated patches", func(t *testing.T) {
		ctx := &markup.Context{}
		root, err := markup.Build([]byte(
			`<Gooey><VStack><Text Name="T">before</Text></VStack></Gooey>`), ctx)
		if err != nil {
			t.Fatal(err)
		}
		// Splice a decorator in, exactly where Decorate would put one:
		// the container's child slot holds the wrapper, Named still holds
		// the inner element (register-inner-return-outer).
		vs := findVStack(root)
		if vs == nil {
			t.Fatal("no VStack in the built page")
		}
		inner := vs.Children[0]
		dec := &decorator{child: inner}
		vs.Children[0] = dec
		if ctx.Named["T"] != inner {
			t.Fatalf("the probe must keep Named on the INNER element, got %T", ctx.Named["T"])
		}

		svc := control.NewService(&probeHost{gooey.NewComposer(root, 40, 10)}, ctx)
		if _, err := svc.PatchMarkup("T", `<Gooey><Text Name="T">after</Text></Gooey>`); err != nil {
			t.Fatalf("a decorator implementing gooey.ChildSetter must be patchable through: %v", err)
		}
		// The refusal being gone is only half of it. The replacement has to
		// land in the DECORATOR's slot: the VStack must still hold the
		// decorator, and the decorator must now hold the fresh element.
		// Without SetChild being what wrote it, a patch could "succeed" by
		// overwriting the VStack slot and dropping the decorator on the
		// floor — which would be the same bug wearing a green test.
		if vs.Children[0] != gooey.Component(dec) {
			t.Fatalf("the patch replaced the decorator instead of its child: %T", vs.Children[0])
		}
		fresh := ctx.Named["T"]
		if fresh == nil {
			t.Fatal("the patch left no element named T")
		}
		if fresh == inner {
			t.Fatal("the patch did not replace the element at all")
		}
		if dec.child != fresh {
			t.Fatalf("the decorator holds %T, but Named[\"T\"] is %T", dec.child, fresh)
		}
	})

	t.Run("a container that cannot address its children is refused", func(t *testing.T) {
		ctx := &markup.Context{}
		root, err := markup.Build([]byte(
			`<Gooey><VStack><Text Name="T">before</Text></VStack></Gooey>`), ctx)
		if err != nil {
			t.Fatal(err)
		}
		vs := findVStack(root)
		if vs == nil {
			t.Fatal("no VStack in the built page")
		}
		vs.Children[0] = &opaqueBox{child: vs.Children[0]}

		svc := control.NewService(&probeHost{gooey.NewComposer(root, 40, 10)}, ctx)
		_, err = svc.PatchMarkup("T", `<Gooey><Text Name="T">after</Text></Gooey>`)
		if err == nil {
			t.Fatal("expected PatchMarkup to refuse a parent that is not a gooey.ChildSetter")
		}
		if !strings.Contains(err.Error(), "cannot rewrite") ||
			!strings.Contains(err.Error(), "ChildSetter") {
			t.Fatalf("the refusal must name the interface the parent did not implement, got: %v", err)
		}
		t.Logf("measured refusal: %v", err)
	})
}

func findVStack(w gooey.Component) *components.VStack {
	if v, ok := w.(*components.VStack); ok {
		return v
	}
	if c, ok := w.(gooey.Container); ok {
		for _, ch := range c.ChildComponents() {
			if v := findVStack(ch); v != nil {
				return v
			}
		}
	}
	return nil
}
