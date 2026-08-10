package format_test

import (
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/format"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The constructors are ordinary computeds, so the contract to prove is
// the graph one: a Set on the source repaints exactly the component
// reading the formatted string, and a formatter nobody reads never
// evaluates.

func TestBytesSetRepaintsOnlyItsReader(t *testing.T) {
	size := prop.NewSource(int64(1536))
	other := prop.NewSource("static")
	sized := &components.Text{Content: format.Bytes(size)}
	root := &components.VStack{Children: []gooey.Component{
		sized,
		&components.Text{Content: other},
	}}
	c := gooey.NewComposer(root, 20, 4)

	if _, painted := c.Frame(); painted != 3 { // vstack + 2 texts
		t.Fatalf("first frame painted %d, want 3", painted)
	}

	// Same-width change: only the Text reading the formatted property
	// repaints.
	size.Set(4608)
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("after Set painted %d components, want 1", painted)
	}
	if got := textRow(f, 0); got != "4.5 KiB" {
		t.Fatalf("row0 = %q, want %q", got, "4.5 KiB")
	}

	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("clean frame painted %d, want 0", painted)
	}
}

func TestUnreadFormatterNeverEvaluates(t *testing.T) {
	size := prop.NewSource(int64(1 << 20))
	s := format.Bytes(size)
	size.Set(2 << 20)
	if s.Evals() != 0 {
		t.Fatalf("unread formatter evaluated %d times, want 0", s.Evals())
	}
	if got := s.Get(); got != "2.0 MiB" {
		t.Fatalf("Get = %q, want %q", got, "2.0 MiB")
	}
	if s.Evals() != 1 {
		t.Fatalf("evaluated %d times after one Get, want 1", s.Evals())
	}
}

func TestRelTimeClockPairing(t *testing.T) {
	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	created := prop.NewSource(t0)
	clock := prop.NewSource(t0.Add(10 * time.Second))
	label := format.RelTime(created, clock)

	if got := label.Get(); got != "10 seconds ago" {
		t.Fatalf("initial = %q, want %q", got, "10 seconds ago")
	}

	// Advancing the CLOCK alone re-derives the string — this is the
	// anti-drift contract: nothing about the source changed.
	clock.Set(t0.Add(2 * time.Minute))
	if got := label.Get(); got != "2 minutes ago" {
		t.Fatalf("after clock tick = %q, want %q", got, "2 minutes ago")
	}

	// And the source stays live too.
	created.Set(t0.Add(2*time.Minute - time.Second))
	if got := label.Get(); got != "1 second ago" {
		t.Fatalf("after source set = %q, want %q", got, "1 second ago")
	}
}

func TestRelTimeClockTickRepaintsReader(t *testing.T) {
	t0 := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	created := prop.NewSource(t0)
	clock := prop.NewSource(t0.Add(time.Minute))
	root := &components.VStack{Children: []gooey.Component{
		&components.Text{Content: format.RelTime(created, clock)},
		&components.Text{Content: prop.NewSource("other")},
	}}
	c := gooey.NewComposer(root, 20, 4)
	c.Frame()

	// The Timer/app.Every pairing posts clock.Set on the UI loop; the
	// frame after a tick repaints exactly the relative-time reader.
	clock.Set(t0.Add(2 * time.Minute))
	f, painted := c.Frame()
	if painted != 1 {
		t.Fatalf("after clock tick painted %d components, want 1", painted)
	}
	if got := textRow(f, 0); got != "2 minutes ago" {
		t.Fatalf("row0 = %q, want %q", got, "2 minutes ago")
	}
}

func TestRelTimeNilClockFreezesUntilSourceChanges(t *testing.T) {
	// A nil clock reads time.Now() at evaluation, so the value is
	// exact per source change and static in between — the documented
	// limit.
	created := prop.NewSource(time.Now().Add(-5 * time.Minute))
	label := format.RelTime(created, nil)
	if got := label.Get(); got != "5 minutes ago" {
		t.Fatalf("nil clock = %q, want %q", got, "5 minutes ago")
	}
	evals := label.Evals()
	if got := label.Get(); got != "5 minutes ago" || label.Evals() != evals {
		t.Fatalf("second Get re-evaluated (evals %d→%d) or changed (%q)", evals, label.Evals(), got)
	}
	created.Set(time.Now().Add(-3 * time.Hour))
	if got := label.Get(); got != "3 hours ago" {
		t.Fatalf("after source set = %q, want %q", got, "3 hours ago")
	}
}

func TestSprintfAndFunc(t *testing.T) {
	n := prop.NewSource(255)
	hex := format.Sprintf("0x%02x", n)
	if got := hex.Get(); got != "0xff" {
		t.Fatalf("Sprintf = %q, want %q", got, "0xff")
	}
	n.Set(7)
	if got := hex.Get(); got != "0x07" {
		t.Fatalf("Sprintf after Set = %q, want %q", got, "0x07")
	}

	on := prop.NewSource(true)
	state := format.Func(on, func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	})
	if got := state.Get(); got != "on" {
		t.Fatalf("Func = %q, want %q", got, "on")
	}
	on.Set(false)
	if got := state.Get(); got != "off" {
		t.Fatalf("Func after Set = %q, want %q", got, "off")
	}
}

// textRow reads one row of a frame's cell buffer as a trimmed string.
func textRow(f *gooey.Frame, y int) string {
	var out []rune
	for x := 0; x < f.Cells.W; x++ {
		out = append(out, f.Cells.At(x, y).Rune)
	}
	s := string(out)
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}
