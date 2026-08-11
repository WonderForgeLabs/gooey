package grpc

// N concurrent Attach streams against one app.
//
// The broadcaster keeps `sessions map[*session]bool` and gives each
// session its own Subscription, its own name filter, its own `last`
// map and its own bounded `out` channel — so multiple streams are
// supported by construction. Nothing tested it: every other test in
// this package attaches exactly once.
//
// That matters for the plugins-as-standalone-activities design
// (docs/specs/2026-08-11-plugins-as-standalone-activities.md), where N
// plugins each own an island and each hold a stream. Two properties
// carry that model, and "supported by construction but never exercised"
// is how they quietly stop holding:
//
//   - isolation: a session sees only what it subscribed to, even while
//     another session is subscribed to something else;
//   - independence: one session going away does not disturb its
//     neighbours — one plugin dying must not take the others' UI with
//     it.

import (
	"testing"
	"time"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// awaitChange pumps the stream until a FrameDelta carries want, failing
// if any name from reject shows up first (a filter leak) or the deadline
// passes. Act results and other server messages are skipped.
func awaitChange(t *testing.T, a *attached, label, want string, reject ...string) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	type recvd struct{ m *controlv1.AttachResponse }
	for {
		ch := make(chan recvd, 1)
		go func() { m, _ := a.stream.Recv(); ch <- recvd{m} }()
		select {
		case r := <-ch:
			if r.m == nil {
				t.Fatalf("%s: stream ended before %q arrived", label, want)
			}
			if f := r.m.GetFrame(); f != nil {
				for _, c := range f.Changes {
					for _, bad := range reject {
						if c.Name == bad {
							t.Fatalf("%s: filter leaked %q — a session saw another session's property", label, bad)
						}
					}
					if c.Name == want {
						return
					}
				}
			}
		case <-deadline:
			t.Fatalf("%s: timed out waiting for a change to %q", label, want)
		}
	}
}

func TestTwoSessionsAreIsolatedAndIndependent(t *testing.T) {
	h := newHarness(t)

	// Two clients, disjoint interests — the N-plugins-N-islands shape.
	notes := attach(t, h, &controlv1.Subscription{Properties: true, Names: []string{"Note"}})
	notes.welcome()
	counts := attach(t, h, &controlv1.Subscription{Properties: true, Names: []string{"Count"}})
	counts.welcome()

	// One writer touches both properties in the same frame.
	notes.act(1, &controlv1.Act{Act: &controlv1.Act_SetProperty{
		SetProperty: &controlv1.SetPropertyRequest{Name: "Note", Value: strVal("for the notes session")},
	}})
	notes.act(2, &controlv1.Act{Act: &controlv1.Act_InvokeCommand{
		InvokeCommand: &controlv1.InvokeCommandRequest{Name: "Increment"},
	}})

	// Each hears its own and only its own.
	awaitChange(t, notes, "notes session", "Note", "Count")
	awaitChange(t, counts, "counts session", "Count", "Note")

	// One plugin dies. The survivor must keep working — this is the
	// property the whole multi-plugin model rests on.
	notes.cancel()

	counts.act(3, &controlv1.Act{Act: &controlv1.Act_InvokeCommand{
		InvokeCommand: &controlv1.InvokeCommandRequest{Name: "Increment"},
	}})
	awaitChange(t, counts, "counts session after its neighbour left", "Count", "Note")
}
