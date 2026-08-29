package mcp

import (
	"os"
	"strings"
	"testing"
	"time"
)

// This server is STATELESS by design (transport.go, Stateless: true), so
// it has no sessions and cannot answer "is a client connected". These
// tests pin the two states it CAN answer for, and pin the boundary that
// stops the third from being invented.

func TestServingReportsTheAcceptLoop(t *testing.T) {
	vm, values := newVM()
	app := newTestApp(t, testMarkup, values)
	_ = vm
	s, err := Serve(app, Options{Context: app.ctx, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if !s.Serving() {
		t.Fatal("a served server reports it is not serving")
	}
	if err := s.ServeError(); err != nil {
		t.Fatalf("a healthy server reports ServeError %v", err)
	}

	s.Close()
	deadline := time.After(3 * time.Second)
	for s.Serving() {
		select {
		case <-deadline:
			t.Fatal("Serving() still true after Close; the accept loop's end was never observed")
		case <-time.After(5 * time.Millisecond):
		}
	}
	// A clean Close is not a failure. http.Server.Serve returns
	// ErrServerClosed there, and reporting THAT as an error would light
	// a failure indicator on every ordinary shutdown.
	if err := s.ServeError(); err != nil {
		t.Errorf("Close reported as a failure: %v", err)
	}
}

func TestOnServeEndFiresOnceWithNilForACleanClose(t *testing.T) {
	vm, values := newVM()
	app := newTestApp(t, testMarkup, values)
	_ = vm
	ended := make(chan error, 4)
	s, err := Serve(app, Options{
		Context: app.ctx, Timeout: 5 * time.Second,
		OnServeEnd: func(err error) { ended <- err },
	})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	select {
	case e := <-ended:
		t.Fatalf("OnServeEnd fired while still serving, with %v", e)
	case <-time.After(50 * time.Millisecond):
	}

	s.Close()
	select {
	case e := <-ended:
		if e != nil {
			t.Errorf("clean Close reported %v, want nil", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("OnServeEnd never fired; a host would never learn the URL stopped being real")
	}
	select {
	case e := <-ended:
		t.Errorf("OnServeEnd fired a second time with %v; it is once, at the end", e)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNewWithoutServeIsNotServing(t *testing.T) {
	_, _, s, _ := setup(t)
	if s.Serving() {
		t.Error("a server built with New reports it is serving; it owns no listener")
	}
}

func TestRequestsCountsServedRequests(t *testing.T) {
	_, _, s, c := setup(t)
	if got := s.Requests(); got != 0 {
		t.Fatalf("a fresh server reports %d requests", got)
	}
	c.rpc("tools/list", nil)
	c.rpc("tools/list", nil)
	if got := s.Requests(); got != 2 {
		t.Errorf("Requests() = %d after two calls, want 2", got)
	}
}

// A refused cross-origin probe is an attack, not a client. Counting it
// would hand a host a number that mixes the two, which is a number
// nobody can act on.
func TestARefusedOriginIsNotCountedAsARequest(t *testing.T) {
	_, _, s, c := setup(t)
	c.rpc("tools/list", nil)
	before := s.Requests()

	resp := postRaw(t, c.url, `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`,
		[2]string{"Origin", "http://evil.example.com"})
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("the origin guard let a foreign Origin through (%d); this test proves nothing", resp.StatusCode)
	}
	if got := s.Requests(); got != before {
		t.Errorf("Requests() moved from %d to %d for a REFUSED request", before, got)
	}
}

// THE BOUNDARY. Requests() is cumulative and must never decrease — which
// is exactly why it must not drive a connection indicator. A host that
// coloured a dot from this would show "connected" forever after the
// first call, and would look correct in any demo where nothing has
// disconnected yet.
//
// The assertion is that the number does NOT come back down when the
// client goes away, stated as a property rather than as a comment, so
// that anyone tempted to make it decrement has to change a test that
// says why.
func TestRequestsNeverDecreasesWhenAClientGoesAway(t *testing.T) {
	_, _, s, c := setup(t)
	c.rpc("tools/list", nil)
	high := s.Requests()
	if high == 0 {
		t.Fatal("no requests were counted; this test cannot discriminate")
	}

	// A stateless endpoint has nothing to close. The client simply
	// stops calling — which is indistinguishable, from here, from a
	// client that is idle between calls. That indistinguishability IS
	// the reason there are only two states.
	time.Sleep(50 * time.Millisecond)
	if got := s.Requests(); got != high {
		t.Fatalf("Requests() went from %d to %d with no traffic; it is cumulative, not live", high, got)
	}
}

// The package must not grow a session count without someone changing
// this test, because the moment it has one, the two-state rule above and
// the comment in state.go both stop being true.
func TestThisServerMintsNoSessionIDs(t *testing.T) {
	_, _, s, c := setup(t)
	resp := postRaw(t, c.url, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	defer resp.Body.Close()

	if got := resp.Header.Get("Mcp-Session-Id"); got != "" {
		t.Fatalf("the server minted a session id (%q). It is no longer stateless, so state.go's "+
			"explanation of why there is no session count is now WRONG and a live count is both "+
			"possible and required", got)
	}
	_ = s
}

func TestStatelessnessIsDeclaredWhereItIsRelied(t *testing.T) {
	// state.go's two-state argument rests entirely on Stateless being
	// true in transport.go. If someone flips it, the argument silently
	// becomes false while every other test here still passes — so the
	// dependency is asserted rather than left as prose.
	b, err := os.ReadFile("transport.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, "Stateless: true") {
		t.Fatal("transport.go no longer sets Stateless: true — state.go's claim that this " +
			"server has no sessions to count is now unfounded, and Serving()/Requests() are " +
			"the wrong surface")
	}
}
