package main

import (
	"fmt"
	"sync"

	"github.com/WonderForgeLabs/gooey/prop"
)

// What the dot beside "grpc" and "mcp" in the status bar reports, and
// how it comes to know it.
//
// THE DOT IS A CONNECTION LIGHT, because that is what it looks like.
// A coloured dot sitting immediately left of a service name is read as
// "is that service up", and there is no caption anywhere that could
// talk a user out of that reading. So it either reports connection
// state or it is not a dot — a dot that is confidently wrong about the
// thing it appears to report is worse than no dot at all, and worse
// than the same fact in words.
//
// This file is the correction of exactly that mistake. The first
// version of the status strip put the last COPY OUTCOME on this dot,
// because at the time nothing about either server's liveness was
// askable from this process. That was an honest reading of the surface
// and the wrong conclusion: the fix was to make liveness askable
// (grpc.Server.Sessions/Serving/ServeError, mcp.Server.Serving/
// ServeError/Requests, and a change callback on each), not to repoint
// the glyph. The copy feedback is real and still shown — it moved to
// copyNotice in statusaddr.go, which has its own cells, its own words
// and NO DOT.
//
// NOTHING HERE POLLS. Both servers call back when their state changes,
// the callback posts to the UI goroutine, and the write lands on an
// ordinary source property that the chip's Render reads — so the read
// IS the damage declaration and a change repaints that one chip. A
// ticker would repaint on a clock instead of on change (prop.Set does
// not compare, so a per-frame Set of an unchanged value still
// invalidates every dependent), which is the one thing the property
// graph exists to avoid.

// linkState is one control-plane endpoint's connection state.
type linkState int

const (
	// linkDown is the ZERO VALUE on purpose, and the direction matters.
	//
	// An endpoint whose observation was never wired up, or wired up and
	// silently broken, shows red rather than green. Red is wrong in the
	// direction a user checks; green is wrong in the direction a user
	// trusts. The reachable down state is an accept loop that DIED after
	// a successful start — a bind failure never gets here, because
	// main() calls gooey.Exit before the page is ever built.
	linkDown linkState = iota

	// linkIdle is serving with zero live sessions. GRPC ONLY.
	linkIdle

	// linkActive is serving with at least one live session. GRPC ONLY.
	//
	// gRPC has a live count to report because it has a long-lived
	// streaming Attach RPC: a client is attached or it is not, for
	// minutes at a time, and grpc.Server.Sessions counts them under the
	// broadcaster's own mutex.
	linkActive

	// linkServing is serving, with nothing said about clients. MCP ONLY,
	// and it is the whole of what MCP has to say.
	//
	// THERE ARE TWO MCP STATES AND YOU MUST NOT ADD A THIRD. The
	// streamable-HTTP handler runs with Stateless: true
	// (mcp/transport.go): every POST is independent, GET and DELETE are
	// answered 405, and Mcp-Session-Id appears nowhere in the package.
	// "Is a client connected" HAS NO ANSWER here — a client is connected
	// for the few milliseconds of a request and not connected the rest
	// of the time. Giving mcp an idle/active split means inventing a
	// fact, and it would be a fact the demo agrees with (nothing has
	// disconnected yet) right up until it is wrong.
	//
	// mcp.Server.Requests is the number that tempts you into it. It is
	// real, and it is CUMULATIVE — it never decreases — so driving a
	// colour from it produces a light that goes green on the first call
	// and stays green forever, including after the last client left and
	// including after the listener died. A count that only goes up is a
	// broken gauge that looks fine in a demo. It is surfaced, but as a
	// NUMBER WITH A LABEL that says what it is ("serving, 12 calls so
	// far"), in the context menu, never as a colour and never as this
	// dot. TestTheMCPDotIgnoresTheRequestCount is the pin.
	//
	// The asymmetry between the two servers is not an inconsistency to
	// smooth over. It is the difference between a stateful stream and a
	// stateless request/response, showing through.
	linkServing
)

// endpointLink is one endpoint's observable state, owned by the EDITOR
// rather than by the strip.
//
// It has to outlive the page: a hot reload builds a new <ServeAddrs/>
// and therefore new chips, while the servers behind them are the same
// processes they were. State on the chip would be reset to "down" by
// every save of wysiwyg.gooey.
type endpointLink struct {
	// state is an ordinary source property. The chip's Render reads it,
	// so the read is the subscription and a transition repaints that
	// chip and nothing else.
	state *prop.Property[linkState]

	// detail is a SENTENCE for the context menu, and it is a function
	// rather than a property on purpose: it is evaluated at the moment
	// the user opens the menu, off the paint path, so a cumulative
	// request count can appear there — labelled as a count — without any
	// of it becoming something the status bar samples on a clock.
	//
	// Nil until the endpoint is bound, and nil forever for a strip built
	// without servers behind it (every unit test that fabricates an
	// endpoint list).
	detail func() string
}

func newEndpointLink() *endpointLink {
	return &endpointLink{state: prop.NewSource(linkDown)}
}

// set writes the state, guarding against the no-op.
//
// prop.Set does not compare values (prop/prop.go): setting a property to
// what it already holds still invalidates every dependent and still
// costs a repaint. A server that fires its callback twice for one
// transition — grpc fires OnSessions(0) both when the last client
// leaves and again when the accept loop ends — would otherwise repaint
// the chip twice for one visible change.
//
// The Get here is in a dispatcher closure, outside any evaluation, so it
// subscribes to nothing.
func (l *endpointLink) set(s linkState) {
	if l.state.Get() == s {
		return
	}
	l.state.Set(s)
}

// link is the editor's registry of endpoint state, keyed by the label
// the status bar shows ("grpc", "mcp"). Created on demand so the order
// of "main wires the server" and "the page builds the strip" does not
// matter — whichever asks first makes it.
//
// UI goroutine only, like every other editor field: main calls it before
// app.Run and the markup builder calls it during a build.
func (ed *editor) link(label string) *endpointLink {
	if ed.links == nil {
		ed.links = map[string]*endpointLink{}
	}
	l := ed.links[label]
	if l == nil {
		l = newEndpointLink()
		ed.links[label] = l
	}
	return l
}

// linkSource is the half of a control-plane server every endpoint has.
// An interface rather than the concrete *grpc.Server / *mcp.Server so
// the transitions can be tested without opening a socket — which is the
// only way to exercise the disconnect path deterministically, and the
// disconnect path is the one that races.
type linkSource interface {
	Serving() bool
	ServeError() error
}

// sessionSource is a linkSource with a LIVE session count: gRPC.
type sessionSource interface {
	linkSource
	Sessions() int
}

// statelessSource is a linkSource with a CUMULATIVE request count and no
// session concept at all: MCP. The method is named Requests rather than
// Sessions in the server for the same reason it is read here into a
// sentence rather than a colour.
type statelessSource interface {
	linkSource
	Requests() int64
}

// linkWatch turns a server's change callbacks into property writes on
// the UI goroutine. One per endpoint.
type linkWatch struct {
	link *endpointLink

	// mu guards srv, sessions and post — written by bind on the main
	// goroutine, read by the callbacks from whatever goroutine fired
	// them.
	//
	// The window is real rather than theoretical: grpc.Serve starts its
	// accept goroutine and RETURNS, so a client connecting in that
	// instant fires OnSessions before bind has run. Without the lock
	// that is a data race on an interface value, which the race detector
	// catches and a demo never does.
	mu       sync.Mutex
	srv      linkSource
	sessions func() int
	post     func(func())
}

func newLinkWatch(l *endpointLink) *linkWatch { return &linkWatch{link: l} }

// bindSessions hands the watch a gRPC server: three states.
func (w *linkWatch) bindSessions(post func(func()), srv sessionSource) {
	w.bind(post, srv, srv.Sessions, func() string { return sessionDetail(srv) })
}

// bindStateless hands the watch an MCP server: two states. Passing no
// session function is what SAYS there are two — see linkServing.
func (w *linkWatch) bindStateless(post func(func()), srv statelessSource) {
	w.bind(post, srv, nil, func() string { return statelessDetail(srv) })
}

// bind installs the server and the only legal route back to the property
// graph, then takes the first reading.
//
// Called from main on the main goroutine before app.Run. The first
// reading goes through post like every other one rather than writing
// directly, so there is exactly ONE path into the state and no "except
// at startup" arm for a later reader to miss.
func (w *linkWatch) bind(post func(func()), srv linkSource, sessions func() int, detail func() string) {
	w.mu.Lock()
	w.srv, w.post, w.sessions = srv, post, sessions
	w.mu.Unlock()
	w.link.detail = detail
	w.notify()
}

// onSessions is grpc.Options.OnSessions. It IGNORES the count, and that
// is deliberate rather than sloppy.
//
// The callback is the TRIGGER; the accessor is the TRUTH. Two
// disconnects racing would deliver their counts to the dispatcher in
// whichever order they got there, and applying a stale n would leave the
// dot claiming a session that has gone. Re-reading Sessions() on the UI
// goroutine cannot be out of date, because there is nothing after it to
// be out of date with respect to.
func (w *linkWatch) onSessions(int) { w.notify() }

// onServeEnd is mcp.Options.OnServeEnd, and it ignores the error for the
// same reason: ServeError() is read at apply time instead.
func (w *linkWatch) onServeEnd(error) { w.notify() }

// notify is what a server callback calls. IT RUNS ON AN ARBITRARY
// GOROUTINE and must never touch a property.
//
// The asymmetry that makes an unconditional Post mandatory: with gRPC a
// session REGISTERS on the UI goroutine (grpc/session.go, through the
// bridge) and UNREGISTERS on its own stream goroutine (a plain defer).
// So a direct Set here works for every connect and races on every
// disconnect — it passes a test that only attaches, and corrupts the
// graph in production. There is no "am I already on the UI goroutine"
// branch here and there must never be one; TestEveryNotificationIsPosted
// and the -race run of TestADisconnectFromAnotherGoroutineIsMarshalled
// are the pins.
func (w *linkWatch) notify() {
	w.mu.Lock()
	post := w.post
	w.mu.Unlock()
	if post == nil {
		// Not bound yet. Nothing is dropped: bind takes its own reading
		// straight after installing post, and apply reads the server's
		// current state rather than a queued delta.
		return
	}
	post(w.apply)
}

// apply reads the server's CURRENT state and writes it. UI GOROUTINE
// ONLY — it is only ever reached through Dispatcher.Post.
func (w *linkWatch) apply() {
	w.mu.Lock()
	srv, sessions := w.srv, w.sessions
	w.mu.Unlock()
	w.link.set(linkStateOf(srv, sessions))
}

// linkStateOf is the whole mapping, in one place so a test can drive it
// without a dispatcher.
//
// A nil sessions function means the endpoint HAS NO SESSION CONCEPT —
// that is how MCP's two states are spelled, and adding an arm here that
// gives it linkIdle or linkActive is the change linkServing's comment
// asks you not to make.
func linkStateOf(srv linkSource, sessions func() int) linkState {
	switch {
	case srv == nil || !srv.Serving():
		return linkDown
	case sessions == nil:
		return linkServing
	case sessions() > 0:
		return linkActive
	default:
		return linkIdle
	}
}

// sessionDetail is the gRPC endpoint's context-menu sentence: a LIVE
// count, which is a thing this server can honestly report.
func sessionDetail(srv sessionSource) string {
	if !srv.Serving() {
		return downDetail(srv)
	}
	switch n := srv.Sessions(); n {
	case 0:
		return "serving, no clients attached"
	case 1:
		return "serving, 1 client attached"
	default:
		return fmt.Sprintf("serving, %d clients attached", n)
	}
}

// statelessDetail is the MCP endpoint's sentence. The count is named
// "calls" and qualified "so far", because that is exactly what it is:
// cumulative, never decreasing, and not a statement about anything
// being connected now. It is a sentence rather than a colour for that
// reason — see linkServing.
func statelessDetail(srv statelessSource) string {
	if !srv.Serving() {
		return downDetail(srv)
	}
	switch n := srv.Requests(); n {
	case 0:
		return "serving, no calls yet"
	case 1:
		return "serving, 1 call so far"
	default:
		return fmt.Sprintf("serving, %d calls so far", n)
	}
}

// downDetail says WHY, when the server knows. A clean Close leaves
// ServeError nil, so "not serving" is the shutdown case and "stopped:"
// is a listener that died under a running app.
func downDetail(srv linkSource) string {
	if err := srv.ServeError(); err != nil {
		return "stopped: " + err.Error()
	}
	return "not serving"
}
