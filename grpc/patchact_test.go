package grpc

// PatchMarkup as an ACT, not only a unary call.
//
// The motivating client is an editor, whose characteristic operation is
// "set a property, then patch the subtree that reads it". Those two must
// not race. On one Attach stream they cannot: the read loop applies each
// act through the bridge and waits for the settle barrier before reading
// the next, so act N is fully applied — and its frame composed — before
// act N+1 is off the wire.
//
// Split across transports they CAN race, and nothing orders them: a
// unary call posts from its own gRPC handler goroutine while acts post
// from the session's reader goroutine, and the Dispatcher takes them in
// whatever order they arrive. That asymmetry is the whole reason this
// act exists, so the ordering half is what these tests pin.

import (
	"strings"
	"testing"
	"time"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// islandPage is the shape an editor works in: a named subtree it
// replaces, and a property that subtree reads.
const islandPage = `<Gooey>
  <VStack Name="Root">
    <VStack Name="Island">
      <Text>{{.Note}}</Text>
    </VStack>
  </VStack>
</Gooey>`

func TestPatchMarkupIsAnActAndOrdersWithSetProperty(t *testing.T) {
	h := newHarness(t)

	a := attach(t, h, &controlv1.Subscription{})
	a.welcome()

	// Establish the island the editor owns.
	a.act(1, &controlv1.Act{Act: &controlv1.Act_SwapMarkup{
		SwapMarkup: &controlv1.SwapMarkupRequest{Source: islandPage},
	}})
	if r := awaitResult(t, a, 1); r.GetCode() != 0 {
		t.Fatalf("swap: %d %s", r.GetCode(), r.GetMessage())
	}

	// The operation that had no safe spelling before: write the property,
	// then patch the subtree that reads it, both as acts on one stream.
	a.act(2, &controlv1.Act{Act: &controlv1.Act_SetProperty{
		SetProperty: &controlv1.SetPropertyRequest{Name: "Note", Value: strVal("written first")},
	}})
	a.act(3, &controlv1.Act{Act: &controlv1.Act_PatchMarkup{
		PatchMarkup: &controlv1.PatchMarkupRequest{
			Name:   "Island",
			Source: `<Gooey><VStack Name="Island"><Text Name="Echo">{{.Note}}</Text></VStack></Gooey>`,
		},
	}})

	if r := awaitResult(t, a, 2); r.GetCode() != 0 {
		t.Fatalf("set: %d %s", r.GetCode(), r.GetMessage())
	}
	r := awaitResult(t, a, 3)
	if r.GetCode() != 0 {
		t.Fatalf("patch: %d %s", r.GetCode(), r.GetMessage())
	}
	// The act answers with the same response the unary surface returns,
	// so a client gets the new name table without a second round trip.
	named := r.GetPatchMarkup().GetNamed()
	if len(named) == 0 {
		t.Fatal("the patch act returned no name table")
	}
	var sawEcho bool
	for _, n := range named {
		if n == "Echo" {
			sawEcho = true
		}
	}
	if !sawEcho {
		t.Errorf("named = %v, want the patched-in Echo", named)
	}
}

func TestPatchMarkupActFailsInStreamWithoutKillingTheSession(t *testing.T) {
	h := newHarness(t)
	a := attach(t, h, &controlv1.Subscription{})
	a.welcome()

	// An editor generates invalid addresses routinely — a stale name, a
	// subtree already replaced. That must be one failed act, not a dead
	// session, or every mistake costs a reconnect and a resync.
	a.act(1, &controlv1.Act{Act: &controlv1.Act_PatchMarkup{
		PatchMarkup: &controlv1.PatchMarkupRequest{
			Name:   "NoSuchElement",
			Source: `<Gooey><VStack Name="NoSuchElement"><Text>x</Text></VStack></Gooey>`,
		},
	}})
	r := awaitResult(t, a, 1)
	if r.GetCode() == 0 {
		t.Fatal("patching an unknown name succeeded")
	}
	if !strings.Contains(strings.ToLower(r.GetMessage()), "no element named") {
		t.Errorf("message = %q, want one naming the missing element", r.GetMessage())
	}

	// The session is still usable — the proof that the failure was
	// in-stream rather than terminal.
	a.act(2, &controlv1.Act{Act: &controlv1.Act_SetProperty{
		SetProperty: &controlv1.SetPropertyRequest{Name: "Note", Value: strVal("still alive")},
	}})
	if r := awaitResult(t, a, 2); r.GetCode() != 0 {
		t.Fatalf("session died after a failed patch: %d %s", r.GetCode(), r.GetMessage())
	}
}

// awaitResult pumps until the ActResult for id arrives, skipping frames
// and other server-initiated messages.
func awaitResult(t *testing.T, a *attached, id uint64) *controlv1.ActResult {
	t.Helper()
	deadline := time.After(3 * time.Second)
	type recvd struct{ m *controlv1.AttachResponse }
	for {
		ch := make(chan recvd, 1)
		go func() { m, _ := a.stream.Recv(); ch <- recvd{m} }()
		select {
		case r := <-ch:
			if r.m == nil {
				t.Fatalf("stream ended before the result for act %d", id)
			}
			if res := r.m.GetResult(); res != nil && res.GetId() == id {
				return res
			}
		case <-deadline:
			t.Fatalf("timed out waiting for the result of act %d", id)
		}
	}
}
