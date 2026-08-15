package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	gooeygrpc "github.com/WonderForgeLabs/gooey/grpc"
	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The transport tests run against a REAL app with a real control plane,
// because every claim this layer rests on had to be measured rather than
// read — four relayed claims about it needed correction, including the
// one that says patch_markup is an act. It isn't.

const targetPage = `<Gooey>
  <VStack Name="Root">
    <Text Name="Title">{{.Title}}</Text>
    <VStack Name="Island">
      <Text Name="Body">{{.Body}}</Text>
    </VStack>
  </VStack>
</Gooey>
`

// target stands up an app with a gRPC server on a random loopback port
// and returns its address.
func target(t *testing.T) (string, *markup.Context) {
	t.Helper()
	app := newTestApp(t, targetPage, map[string]any{
		"Title": prop.NewSource("original title"),
		"Body":  prop.NewSource("original body"),
	})
	srv, err := gooeygrpc.Serve(app, gooeygrpc.Options{
		Addr: "127.0.0.1:0", Context: app.ctx, Name: "wysiwyg-test-target", Version: "1",
	})
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	t.Cleanup(srv.Close)
	return srv.Addr(), app.ctx
}

func TestRemoteAttachAndWrite(t *testing.T) {
	addr, vm := target(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	r, err := Connect(ctx, addr, []string{"Body"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.AppName != "wysiwyg-test-target" {
		t.Errorf("welcome app name = %q", r.AppName)
	}

	deltas := make(chan string, 8)
	r.OnDelta = func(name string, v *controlv1.TypedValue) {
		if name == "Body" {
			deltas <- v.GetStringValue()
		}
	}

	if err := r.SetProperty(ctx, "Body", str("written over Attach")); err != nil {
		t.Fatalf("SetProperty: %v", err)
	}
	// The write is applied by the time SetProperty returns: an ActResult
	// means the act ran on the UI goroutine and passed the settle
	// barrier.
	if got := vm.Values["Body"].(*prop.Property[string]).Get(); got != "written over Attach" {
		t.Errorf("app property = %q, want the value just written", got)
	}
	select {
	case got := <-deltas:
		if got != "written over Attach" {
			t.Errorf("delta = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Error("no subscribed delta for a filtered name")
	}
}

// TestRemoteFilterExcludesUnsubscribedNames — the names filter is what
// keeps an island-scoped editor from taking every property delta in the
// app.
func TestRemoteFilterExcludesUnsubscribedNames(t *testing.T) {
	addr, _ := target(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	r, err := Connect(ctx, addr, []string{"Body"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	seen := make(chan string, 8)
	r.OnDelta = func(name string, _ *controlv1.TypedValue) { seen <- name }

	if err := r.SetProperty(ctx, "Title", str("not subscribed")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetProperty(ctx, "Body", str("subscribed")); err != nil {
		t.Fatal(err)
	}
	// Body was written second, so if Title were going to leak it would
	// already have arrived by the time Body's delta does.
	select {
	case name := <-seen:
		if name != "Body" {
			t.Errorf("unsubscribed name %q leaked into a filtered session", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no delta at all")
	}
}

// TestRemotePatchReplacesOneSubtree is the editor's characteristic
// operation, and it goes through the UNARY surface because patch_markup
// is not an act.
func TestRemotePatchReplacesOneSubtree(t *testing.T) {
	addr, _ := target(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	r, err := Connect(ctx, addr, []string{"Body"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	frag := `<Gooey><VStack Name="Island"><Text Name="Body">{{.Body}}</Text><Text Name="Added">new</Text></VStack></Gooey>`
	if ok, loadErr, err := r.Validate(ctx, frag); err != nil {
		t.Fatalf("Validate: %v", err)
	} else if !ok {
		t.Fatalf("fragment does not validate: %s", loadErr)
	}
	named, err := r.Patch(ctx, "Island", frag)
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if !contains(named, "Added") {
		t.Errorf("named after patch = %v, want the new element", named)
	}
	// The sibling outside the island is untouched.
	if !contains(named, "Title") {
		t.Errorf("patching the island disturbed a sibling: %v", named)
	}
}

// TestPatchIsOrderedAgainstPipelinedWrites asserts the one property the
// editor's whole loop depends on: that a patch observes the writes
// issued before it.
//
// The MECHANISM behind that property changes with the transport, and the
// name is deliberately about the property rather than the mechanism:
//
//   - On origin/main today, patch_markup is not an act, so a unary patch
//     posts from its own gRPC handler goroutine while acts post from the
//     session reader's. Nothing orders them. The barrier in Patch drains
//     in-flight acts first, and THIS TEST IS ITS PROOF.
//   - When patch_markup lands as an act, the barrier is deleted and the
//     stream's own ordering supplies the property. The test then guards
//     against silently regressing to the unary channel instead.
//
// Verified in BOTH configurations by deleting the mechanism and watching
// this fail — 21 to 47 pipelined acts overtaken each time. That check is
// the point of the test, not a formality: an earlier version awaited
// every write, so nothing was ever in flight, and it passed with the
// barrier removed while looking exactly like evidence.
//
// The editor pipelines writes while someone types, because a round trip
// per keystroke would put the transport inside the interaction loop. So
// the ordering asserted here is the one it actually relies on.
func TestPatchIsOrderedAgainstPipelinedWrites(t *testing.T) {
	addr, vm := target(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	r, err := Connect(ctx, addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	body := vm.Values["Body"].(*prop.Property[string])
	frag := `<Gooey><VStack Name="Island"><Text Name="Body">{{.Body}}</Text></VStack></Gooey>`

	for round := 0; round < 12; round++ {
		var last string
		for i := 0; i < 60; i++ {
			last = fmt.Sprintf("round%d-write%d", round, i)
			if err := r.SetPropertyAsync("Body", str(last)); err != nil {
				t.Fatalf("SetPropertyAsync: %v", err)
			}
		}
		if _, err := r.Patch(ctx, "Island", frag); err != nil {
			t.Fatalf("Patch: %v", err)
		}
		// Patch returning means every pipelined write has been applied.
		// Without the barrier the patch returns while writes are still
		// queued behind it, and this reads a stale value.
		if got := body.Get(); got != last {
			t.Fatalf("round %d: property = %q, want %q — the patch overtook pipelined writes; is the barrier still in Patch?",
				round, got, last)
		}
	}
}

// TestPatchRejectsAnUnknownAddressWithoutKillingTheApp — an editor
// generates invalid addresses routinely, so a bad one has to be an error
// rather than a fault.
func TestPatchRejectsAnUnknownAddressWithoutKillingTheApp(t *testing.T) {
	addr, _ := target(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	r, err := Connect(ctx, addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.Patch(ctx, "NoSuchName", `<Gooey><Text Name="NoSuchName">x</Text></Gooey>`)
	if err == nil {
		t.Fatal("patching an unknown address must be an error")
	}
	if !strings.Contains(err.Error(), "NoSuchName") {
		t.Errorf("error does not name the address: %v", err)
	}
	// Still serving.
	if _, err := r.Named(ctx); err != nil {
		t.Errorf("app unreachable after a rejected patch: %v", err)
	}
}

// TestInvalidMarkupIsCheapToDiscover — validate never touches the tree,
// which is what lets a generation loop be wrong as often as it needs to.
func TestInvalidMarkupIsCheapToDiscover(t *testing.T) {
	addr, _ := target(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	r, err := Connect(ctx, addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	before, err := r.Named(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Unknown attribute — a load error only since rejection landed.
	ok, loadErr, err := r.Validate(ctx, `<Gooey><Text Name="X" Lef="2">x</Text></Gooey>`)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("markup with an unknown attribute must not validate")
	}
	if !strings.Contains(loadErr, "no such attribute") {
		t.Errorf("load error = %q", loadErr)
	}
	after, err := r.Named(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Error("validate disturbed the running tree")
	}
}

func str(s string) *controlv1.TypedValue {
	return &controlv1.TypedValue{Kind: &controlv1.TypedValue_StringValue{StringValue: s}}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
