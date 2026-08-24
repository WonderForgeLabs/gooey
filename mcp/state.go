package mcp

import "sync"

// What this server can and cannot tell a host about its clients.
//
// THERE IS NO SESSION COUNT HERE, and its absence is a design fact
// rather than a gap somebody forgot to fill.
//
// The streamable-HTTP handler runs with Stateless: true
// (transport.go). Every POST is independent, the SDK synthesizes the
// initialized state for a request that arrives without a handshake, GET
// and DELETE are answered 405, and no Mcp-Session-Id is ever minted —
// the string appears nowhere in this package. That is deliberate: there
// is exactly one app behind this server and it is the same app for every
// client, so a session has nothing to hold.
//
// The consequence for a status indicator is worth stating plainly,
// because the obvious implementation is wrong. "Is a client connected"
// HAS NO ANSWER for a stateless HTTP endpoint. A client is connected for
// the few milliseconds of a request and not connected the rest of the
// time, and there is no third thing to observe. So a host gets TWO
// states from this package — serving, or not — and inventing a third
// means inventing a fact.
//
// Requests is offered alongside, and it is real, but it is CUMULATIVE
// and never decreases. It answers "has anything ever talked to this
// endpoint", which is a genuinely useful thing to know and is NOT
// connection state. Rendering it as a live indicator would be the
// broken-gauge shape: a number that only goes up looks correct in a
// demo, where nothing has disconnected yet, and is wrong the first time
// something does. Label it as a count or do not show it.
//
// Compare grpc.Server.Sessions, which IS a live count, because the gRPC
// control plane has a long-lived streaming Attach RPC and therefore has
// something to count. The asymmetry between the two servers is not an
// inconsistency to be smoothed over; it is the difference between a
// stateful stream and a stateless request/response, showing through.

// serveState is the accept loop's outcome, shared by Serving and
// ServeError.
type serveState struct {
	mu      sync.Mutex
	started bool
	ended   bool
	err     error
}

func (s *serveState) start() {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
}

func (s *serveState) end(err error) {
	s.mu.Lock()
	s.ended, s.err = true, err
	s.mu.Unlock()
}

func (s *serveState) serving() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started && !s.ended
}

func (s *serveState) failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Serving reports whether the accept loop is still up.
//
// False before Serve is called, and false once the loop has returned for
// any reason including Close. A host showing this endpoint's URL should
// show this too: a URL whose listener is gone is an instruction to
// connect somewhere that will refuse.
func (s *Server) Serving() bool { return s.state.serving() }

// ServeError is why the accept loop stopped, or nil if it has not
// stopped or stopped cleanly.
//
// http.Server.Serve returns ErrServerClosed on a clean Close, which is
// not a failure and is normalised to nil by the goroutine in Serve — so
// a non-nil error here means the listener DIED while the app kept
// running, which is the one reachable "not listening" state. A bind
// failure never reaches it: Serve reports that to its caller before
// returning a Server at all.
func (s *Server) ServeError() error { return s.state.failure() }

// Requests is how many requests this server has served since it started,
// counted after the origin guard, so a rejected cross-origin probe is
// not counted as a client using the app.
//
// CUMULATIVE. It never decreases, and it is not a connection count — see
// the file comment for why this server cannot have one.
func (s *Server) Requests() int64 { return s.requests.Load() }

// countRequest is called by the origin guard once a request has been
// allowed through. Atomic rather than mutex-guarded because it is on the
// per-request path and is read without any other state.
func (s *Server) countRequest() { s.requests.Add(1) }
