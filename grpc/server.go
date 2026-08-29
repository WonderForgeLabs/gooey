package grpc

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	grpcgo "google.golang.org/grpc"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// The server: gooey.control.v1 over grpc-go, issue #111.
//
// Every RPC is a thin proto adapter over the shared in-process service
// (the root module's control package) — the same implementation the MCP
// tools front (issue #112). Nothing here touches a component or a
// property directly: each call is marshaled onto the app's UI goroutine
// through control.Bridge, which also waits for the settle barrier, so a
// ScreenText immediately after an InvokeCommand sees the new pixels.
// That guarantee is contract text (docs/specs/2026-08-10-grpc-contract.md),
// not an implementation nicety.

// Host is what the server needs from a running app — the control
// package's Host. *gooey.App implements it.
type Host = control.Host

// SessionHost is the optional surface a Host may also implement for
// streaming sessions (SessionService.Attach): the frame-completion,
// swap, input and shutdown seams the FrameDelta/lifecycle/echo channels
// hang off. *gooey.App implements it. A Host without it still serves
// every unary RPC and act; only the server-push channels stay silent.
type SessionHost interface {
	Host
	// AfterFrame registers a hook run after each composed frame — where
	// frame deltas are collected. Must be called on the UI goroutine.
	AfterFrame(fn func())
	// OnSwap registers a hook run when the composition is replaced.
	OnSwap(fn func(gooey.Component))
	// AfterEvent registers an observer of routed input events.
	AfterEvent(fn func(ev input.Event, consumed bool))
	// Done is closed when the app quits.
	Done() <-chan struct{}
}

// Options configure a server.
type Options struct {
	// Addr is the listen address, passed through to net.Listen as given.
	// Default "127.0.0.1:0" (an ephemeral port; read it back from Addr).
	//
	// A control-plane client can do anything the keyboard can, and this
	// server has no authentication, so a non-loopback address exposes an
	// unauthenticated control handle to every host that can reach it.
	// Nothing here prevents that; it is the operator's choice.
	Addr string
	// Context is the markup binding context the app was built against.
	// Without one the name-addressed RPCs report FAILED_PRECONDITION;
	// the tree and screen RPCs still work.
	Context *markup.Context
	// Doc, when set, supplies the markup source of the running page —
	// what GetDeclaredSchema answers for an empty source.
	Doc func() []byte
	// Timeout bounds how long a call waits for the UI goroutine. Default
	// 5s. A timeout means the run loop is blocked or not running; it is
	// reported as DEADLINE_EXCEEDED, never as a hang.
	Timeout time.Duration
	// Name and Version identify this app in the session Welcome.
	Name, Version string
	// Grant scopes this endpoint to one island: every RPC and every act
	// on it reaches the granted subtree and the granted value names, and
	// refuses or narrows everything else (control/grant.go).
	//
	// nil is the HOST's own endpoint — the whole app, as every endpoint
	// behaved before grants existed. That default is deliberate: a host
	// serving itself should not have to opt in to owning its own app.
	//
	// REGISTRATION IS THE GRANT. One endpoint carries one grant, fixed
	// here by the host; a guest never asks for a capability, it connects
	// to an address that already has one. Two guests with disjoint
	// islands are two Serve calls on two loopback ports, which is what
	// lets them drive one app concurrently without interfering.
	Grant *control.Grant
	// OnSessions is called with the live session count every time it
	// changes — a client attaching or detaching — so a host can show
	// whether anything is driving it without polling. nil disables it.
	//
	// IT RUNS ON AN ARBITRARY GOROUTINE, and the asymmetry is the trap:
	// a session JOINS on the UI goroutine (register goes through
	// Bridge.Do) and LEAVES on its own stream goroutine (unregister is a
	// plain defer). So an implementation that touches the property graph
	// directly works for every connect and races on every disconnect —
	// it passes a test that only attaches, and corrupts the graph in
	// production. Marshal with Dispatcher.Post unconditionally; never
	// branch on "am I already on the UI goroutine".
	//
	// It is called with the lock released, so it may block; a callback
	// that blocks forever stalls the session that fired it.
	OnSessions func(n int)
}

// Server is a gooey app exposed over gRPC.
type Server struct {
	host Host
	svc  *control.Service
	ui   *control.Bridge
	bc   *broadcaster
	opts Options

	ln net.Listener
	gs *grpcgo.Server

	// serveMu guards the accept loop's outcome, which the goroutine
	// started by Serve writes and any caller of Serving/ServeError
	// reads.
	serveMu   sync.Mutex
	serveEnd  bool
	serveErr  error
	serveDone bool // Serve was called at all, i.e. we own a listener
}

// New builds a server without listening, for tests and for hosts that
// own their listener — and from then on, who can reach this surface is
// that host's decision alone.
func New(host Host, opts Options) (*Server, error) {
	if host == nil {
		return nil, fmt.Errorf("gooey/grpc: nil host")
	}
	s := &Server{
		host: host,
		svc:  control.NewScopedService(host, opts.Context, opts.Grant),
		ui:   control.NewBridge(host.Post, opts.Timeout),
		opts: opts,
	}
	s.svc.Doc = opts.Doc
	s.gs = grpcgo.NewServer()
	controlv1.RegisterControlServiceServer(s.gs, &controlServer{s: s})
	controlv1.RegisterSessionServiceServer(s.gs, &sessionServer{s: s})

	if sh, ok := host.(SessionHost); ok {
		s.bc = newBroadcaster(s.svc, sh, opts.OnSessions)
		// The echo hook makes the service report the input it injects —
		// remote input echoes exactly as terminal input does. Set before
		// anything can call the service, on this goroutine, so no race.
		s.svc.Echo = s.bc.echoRemote
		// Hook registration appends to the App's hook lists, which the
		// run loop reads without a lock — so it happens ON the UI
		// goroutine, whenever the loop gets there. Sessions attached
		// before then miss nothing: no frame has been observed yet.
		sh.Post(func() {
			sh.AfterFrame(s.bc.afterFrame)
			sh.OnSwap(s.bc.afterSwap)
			sh.AfterEvent(s.bc.afterEvent)
			s.bc.prime()
		})
	}
	return s, nil
}

// Serve starts a server on opts.Addr and returns once it is listening.
// Close shuts it down.
func Serve(host Host, opts Options) (*Server, error) {
	addr := opts.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	// addr is used as given. This server has no authentication, so a
	// non-loopback address exposes an unauthenticated control handle;
	// that is the operator's choice, not something this package prevents.
	s, err := New(host, opts)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("gooey/grpc: listen %s: %w", addr, err)
	}
	s.ln = ln
	// Under the mutex even though Serve returns before any caller can
	// reach Serving(): this package is in the -race tier precisely
	// because "nobody could call it yet" is an argument, not a
	// guarantee, and an unsynchronised write paired with a locked read
	// is still a race the detector will (correctly) fail on.
	s.serveMu.Lock()
	s.serveDone = true
	s.serveMu.Unlock()
	// The accept loop's OUTCOME is captured rather than dropped on the
	// floor. grpc-go's Serve returns nil after Stop, so a non-nil error
	// here is a listener that DIED while the app kept running — the one
	// reachable "not listening" state, since a bind failure is reported
	// by Serve itself before this point and never reaches the UI at all.
	// Until this was captured, an app whose control plane had gone away
	// went on displaying its address, and the only symptom was that
	// clients stopped being able to connect to a port the status bar
	// still advertised.
	go func() {
		err := s.gs.Serve(ln)
		// ErrServerStopped is a CLEAN shutdown, not a failure, and this
		// normalisation is not defensive — it fixes a real race that a
		// mutation run exposed. grpc-go's Serve returns nil when Stop
		// interrupts a running accept loop, but ErrServerStopped when
		// Stop lands BEFORE the loop starts; this goroutine is launched
		// asynchronously, so a fast Close (a test, a short-lived app)
		// hits the second case. Without this, an ordinary shutdown
		// sometimes lights a failure indicator and sometimes does not —
		// intermittently, which is the worst way for it to be wrong.
		if errors.Is(err, grpcgo.ErrServerStopped) {
			err = nil
		}
		s.serveMu.Lock()
		s.serveEnd, s.serveErr = true, err
		s.serveMu.Unlock()
		// opts is written once in New and never again, so reading the
		// callback needs no lock — and taking one here would be a claim
		// that opts is mutable, which is the kind of misleading
		// synchronisation that outlives the person who added it.
		cb := s.opts.OnSessions
		// The count is zero once the loop is gone: every stream it was
		// carrying is finished. Reported through the same callback so a
		// host that only listens for counts still sees the endpoint go
		// quiet, rather than holding the last count forever.
		if cb != nil {
			cb(0)
		}
	}()
	return s, nil
}

// Sessions is the number of clients currently attached.
//
// Taken under the broadcaster's own mutex. Zero for a server built with
// New rather than Serve, and for a host that is not a SessionHost —
// neither can carry a session, so zero is the true answer rather than a
// missing one.
func (s *Server) Sessions() int {
	if s.bc == nil {
		return 0
	}
	return s.bc.count()
}

// Serving reports whether the accept loop is still up.
//
// False before Serve is called, and false once the loop has returned for
// any reason including Close. A host showing an endpoint's address
// should show this too: an address whose listener is gone is an
// instruction to connect somewhere that will refuse.
func (s *Server) Serving() bool {
	s.serveMu.Lock()
	defer s.serveMu.Unlock()
	return s.serveDone && !s.serveEnd
}

// ServeError is why the accept loop stopped, or nil if it has not
// stopped or stopped cleanly (Close).
func (s *Server) ServeError() error {
	s.serveMu.Lock()
	defer s.serveMu.Unlock()
	return s.serveErr
}

// Addr is the address the server is listening on, empty if it was built
// with New rather than Serve.
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Close stops the server, ending every open session. It does not touch
// the app.
func (s *Server) Close() {
	if s.gs != nil {
		s.gs.Stop()
	}
}
