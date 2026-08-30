package grpc

import (
	"sync"
	"testing"
	"time"
)

// TestAPanickingHostCallbackDoesNotWedgeTheMailbox bounds the damage a
// host can do to the broadcaster by failing inside its own callback.
//
// Options.OnSessions is documented as arbitrary host code on an
// arbitrary goroutine, and nothing here recovers it. The mailbox's whole
// selling point is that no caller is ever stuck behind that code — but
// b.delivering was cleared by a plain assignment AFTER the loop, so an
// unwind skipped it and left the flag set for the life of the
// broadcaster. Every later add, remove and close then deposited its
// count, saw delivering, and returned: the host went permanently deaf,
// including to the terminal zero that says the endpoint is gone.
//
// The claim is that ONE delivery is lost, not all of them. Found in
// review of #425.
func TestAPanickingHostCallbackDoesNotWedgeTheMailbox(t *testing.T) {
	var got []int
	boom := true
	b := &broadcaster{}
	b.onSessions = func(n int) {
		got = append(got, n)
		if boom {
			boom = false
			panic("host code fails inside OnSessions")
		}
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not escape notify — the fixture proves nothing")
			}
		}()
		b.notify(1, 1, false)
	}()

	// A LATER ORDINARY COUNT. Without the defer this is deposited into
	// the mailbox and never carried, because delivering is still true.
	b.notify(2, 2, false)
	// And the terminal zero, which is the delivery that matters most: a
	// host that never learns the endpoint is gone shows a live
	// connection forever.
	b.notify(0, 3, true)

	want := []int{1, 2, 0}
	if len(got) != len(want) {
		t.Fatalf("the host saw %v, want %v — a count after the failed delivery "+
			"never arrived", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the host saw %v, want %v", got, want)
		}
	}
}

// TestTheTerminalZeroSurvivesAPanickingCallback is the case the previous
// round's comment got wrong.
//
// That comment said a count deposited during a failed delivery "waits
// for the next change", and for an ordinary count it does. For the
// TERMINAL ZERO it does not: closed() latches b.shut on the way in, and
// the latch at the top of notify refuses every later caller — so there
// is no next change, nothing can carry it, and the host shows a live
// connection for good. Permanent silence, not a delay.
//
// The fixture is the interleaving that produces it: the host panics
// while it is being told about count 1, and the endpoint closes during
// that same call, depositing the zero behind it.
func TestTheTerminalZeroSurvivesAPanickingCallback(t *testing.T) {
	var mu sync.Mutex
	var got []int
	done := make(chan struct{})

	b := &broadcaster{}
	b.onSessions = func(n int) {
		mu.Lock()
		got = append(got, n)
		first := len(got) == 1
		mu.Unlock()
		if first {
			// The endpoint goes away WHILE the host is being told about
			// count 1. This deposits the zero and latches shut.
			b.notify(0, 2, true)
			panic("host code fails inside OnSessions")
		}
		if n == 0 {
			close(done)
		}
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not escape notify — the fixture proves nothing")
			}
		}()
		b.notify(1, 1, false)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("the host saw %v and never learned the endpoint was gone. The "+
			"terminal zero sat in the mailbox with b.shut latched, so no later "+
			"caller could carry it", got)
	}
}
