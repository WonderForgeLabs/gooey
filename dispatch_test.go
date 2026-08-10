package gooey_test

import (
	"sync"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
)

func TestDispatcherRunsPostedWorkInOrder(t *testing.T) {
	d := gooey.NewDispatcher()
	var got []int
	for i := 0; i < 3; i++ {
		d.Post(func() { got = append(got, i) })
	}
	if d.Pending() != 3 {
		t.Fatalf("Pending()=%d, want 3", d.Pending())
	}
	if n := d.Drain(); n != 3 {
		t.Fatalf("Drain()=%d, want 3", n)
	}
	if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("ran %v, want [0 1 2]", got)
	}
	if n := d.Drain(); n != 0 {
		t.Fatalf("second Drain()=%d, want 0", n)
	}
}

// The point of the dispatcher: a property Set that originates on another
// goroutine still happens on the UI goroutine.
func TestDispatcherMarshalsSetsOntoTheLoopGoroutine(t *testing.T) {
	d := gooey.NewDispatcher()
	body := prop.NewSource("")
	painted := 0
	view := prop.NewComputed(func() string { painted++; return "body: " + body.Get() })
	view.Get() // subscribe

	go func() { d.Post(func() { body.Set("fetched") }) }()

	select {
	case <-d.Wake():
		d.Drain()
	case <-time.After(2 * time.Second):
		t.Fatal("no wake signal from a Post on another goroutine")
	}
	if got := view.Get(); got != "body: fetched" {
		t.Fatalf("view=%q, want %q", got, "body: fetched")
	}
	if painted != 2 {
		t.Fatalf("computed evaluated %d times, want 2", painted)
	}
}

// A burst of Posts may collapse into one wake signal; the Drain it
// triggers must still take every queued func.
func TestDispatcherWakeCoalescesWithoutLosingWork(t *testing.T) {
	d := gooey.NewDispatcher()
	const n = 200
	var wg sync.WaitGroup
	var mu sync.Mutex
	ran := 0
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			d.Post(func() { mu.Lock(); ran++; mu.Unlock() })
		}()
	}
	wg.Wait()

	deadline := time.After(2 * time.Second)
	for ran < n {
		select {
		case <-d.Wake():
			d.Drain()
		case <-deadline:
			t.Fatalf("ran %d of %d before timeout", ran, n)
		}
	}
	if ran != n {
		t.Fatalf("ran %d, want %d", ran, n)
	}
}

// A drained func may Post again; the new work lands in the next drain,
// so one Drain always terminates.
func TestDispatcherPostFromInsideDrain(t *testing.T) {
	d := gooey.NewDispatcher()
	steps := 0
	d.Post(func() {
		steps++
		d.Post(func() { steps++ })
	})
	if n := d.Drain(); n != 1 {
		t.Fatalf("first Drain()=%d, want 1", n)
	}
	if steps != 1 {
		t.Fatalf("steps=%d after first drain, want 1", steps)
	}
	select {
	case <-d.Wake():
	case <-time.After(time.Second):
		t.Fatal("re-post did not signal a wake")
	}
	if n := d.Drain(); n != 1 {
		t.Fatalf("second Drain()=%d, want 1", n)
	}
	if steps != 2 {
		t.Fatalf("steps=%d, want 2", steps)
	}
}
