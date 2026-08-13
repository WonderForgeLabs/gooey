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
// PatchMarkup? — and is kept because the answer is a general constraint
// worth pinning: NOTHING may sit between a container and a named
// element, decorator or otherwise.
//
// It lives in this module rather than in control/ because control/ is
// another session's. It asserts a LIMITATION, so it will fail the day
// control.childSlot stops being a closed type switch — which would be
// the fix, not a regression, and the failure message should send whoever
// hits it here rather than at the pin.

// decorator stands in for whatever a Decorate seam would wrap an element
// in: a transparent one-child container that changes nothing visible.
type decorator struct {
	gooey.Base
	child gooey.Component
}

func (d *decorator) ChildComponents() []gooey.Component { return []gooey.Component{d.child} }
func (d *decorator) Measure(a gooey.Size) gooey.Size    { return gooey.MeasureChild(d.child, a) }
func (d *decorator) Arrange(b gooey.Rect)               { d.Base.Arrange(b); gooey.ArrangeChild(d.child, b) }
func (d *decorator) Render(*gooey.Frame)                {}

type probeHost struct{ c *gooey.Composer }

func (h *probeHost) Post(fn func())            { fn() }
func (h *probeHost) Composer() *gooey.Composer { return h.c }
func (h *probeHost) Swap(gooey.Component)      {}

// TestPatchMarkupThroughADecoratorIsRefused measures the cost of putting
// ANY component between a container and a named element.
//
// control.childSlot (control/markup.go:307) is a closed type switch over
// six concrete container types, so a decorator declared anywhere else can
// never join it — and PatchMarkup locates its target's slot through that
// switch. This is what a Decorate seam would create for every named
// element in the document.
func TestPatchMarkupThroughADecoratorIsRefused(t *testing.T) {
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

	t.Run("decorated is refused", func(t *testing.T) {
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
		vs.Children[0] = &decorator{child: inner}
		if ctx.Named["T"] != inner {
			t.Fatalf("the probe must keep Named on the INNER element, got %T", ctx.Named["T"])
		}

		svc := control.NewService(&probeHost{gooey.NewComposer(root, 40, 10)}, ctx)
		_, err = svc.PatchMarkup("T", `<Gooey><Text Name="T">after</Text></Gooey>`)
		if err == nil {
			t.Fatal("expected PatchMarkup to fail through a decorator")
		}
		if !strings.Contains(err.Error(), "cannot rewrite") {
			t.Fatalf("expected the childSlot refusal, got: %v", err)
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
