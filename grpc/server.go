package grpc

import (
	"fmt"
	"net"
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
	// Addr is the listen address. It must be loopback: a control-plane
	// client can do anything the keyboard can, and v1 has no
	// authentication, so a non-loopback bind is a hard error — remote
	// binds arrive with authentication or not at all. Default
	// "127.0.0.1:0" (an ephemeral port; read it back from Addr).
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
}

// New builds a server without listening, for tests and for hosts that
// own their listener — at which point the loopback guarantee becomes
// that host's problem, which is why Serve is the documented way.
func New(host Host, opts Options) (*Server, error) {
	if host == nil {
		return nil, fmt.Errorf("gooey/grpc: nil host")
	}
	s := &Server{
		host: host,
		svc:  control.NewService(host, opts.Context),
		ui:   control.NewBridge(host.Post, opts.Timeout),
		opts: opts,
	}
	s.svc.Doc = opts.Doc
	s.gs = grpcgo.NewServer()
	controlv1.RegisterControlServiceServer(s.gs, &controlServer{s: s})
	controlv1.RegisterSessionServiceServer(s.gs, &sessionServer{s: s})

	if sh, ok := host.(SessionHost); ok {
		s.bc = newBroadcaster(s.svc, sh)
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
	if err := checkLoopback(addr); err != nil {
		return nil, err
	}
	s, err := New(host, opts)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("gooey/grpc: listen %s: %w", addr, err)
	}
	s.ln = ln
	go s.gs.Serve(ln)
	return s, nil
}

// checkLoopback refuses a non-loopback bind. This is the whole of v1's
// security posture — same rule, same shape as the MCP server's — and it
// is deliberately a hard error rather than a warning: there is no token
// auth yet, so a server reachable from the network is a remote-control
// handle on the user's terminal.
func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("gooey/grpc: bad address %q: %w", addr, err)
	}
	switch host {
	case "localhost":
		return nil
	case "":
		return fmt.Errorf("gooey/grpc: %q binds every interface; v1 is loopback-only (use 127.0.0.1:port)", addr)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("gooey/grpc: %q is not a loopback address; v1 has no authentication, so remote binds are refused", addr)
	}
	return nil
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
