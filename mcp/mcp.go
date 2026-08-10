// Package mcp turns a running gooey app into an MCP server: an agent (or
// any Model Context Protocol client) can inspect the live component tree,
// read the screen, drive input, set viewmodel state, and replace the
// page's markup. The automation surface, the accessibility surface and
// the live-edit surface are one protocol.
//
// It is opt-in and loopback-only:
//
//	srv, err := mcp.Serve(app, mcp.Options{Addr: "127.0.0.1:7777", Context: ctx})
//	defer srv.Close()
//
// # The concurrency rule
//
// This package exists on the wrong side of gooey's central confinement
// rule and knows it. MCP requests arrive on net/http goroutines; the
// property graph is unlocked and confined to the run loop. So no tool
// body ever runs on the goroutine that received the request: every call
// is marshaled through the app's Dispatcher and executed on the UI loop,
// and the result crosses back as plain data. The Tool type is shaped to
// make that structural rather than remembered — a Tool's Run is *defined*
// as running on the UI goroutine, and dispatch is the only thing that
// ever calls it, so a tool cannot be written the wrong way by accident.
//
// The second half of the rule is that nothing here holds a reference to
// the tree between requests. Snapshots are plain maps of copied values,
// never widgets or property handles: a *prop.Property read from an http
// goroutine after the response went out would be exactly the bug the
// Dispatcher exists to prevent, and a hot reload would have replaced the
// widget anyway.
//
// # A nested module
//
// The protocol is the official SDK's, modelcontextprotocol/go-sdk, and
// that SDK brings eight modules with it — jsonschema-go,
// segmentio/{asm,encoding}, uritemplate, golang-jwt, x/{oauth2,time} —
// to a framework whose own graph is golang.org/x/term. So this package
// is a SEPARATE GO MODULE, exactly like handlers/temporal: `go build
// ./...` and `go test ./...` at the repo root skip it, which is the
// mechanical proof that core gooey still builds without any of it. An
// app opts in by requiring github.com/WonderForgeLabs/gooey/mcp.
//
// The SDK derives tool schemas from Go structs by reflection on its
// ergonomic path. That path is not used here — schemas are written out
// (see Tool.Schema) and attached through the explicit
// mcp.Server.AddTool — but the SDK's reflection elsewhere in its own
// machinery is accepted at this protocol boundary. gooey's own
// no-reflection rule is about the framework, and the framework does not
// import this.
package mcp

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Host is what this server needs from a running app: a way onto the UI
// goroutine, the live composition, and the tree-swap seam. *gooey.App
// implements it.
//
// It is an interface rather than a *gooey.App so the server can be
// tested against a hand-run loop — which is the only honest way to test
// the confinement rule, since the test has to own the goroutine that
// drains the Dispatcher.
type Host interface {
	// Post queues fn to run on the UI goroutine. Safe from any goroutine.
	Post(fn func())
	// Composer is the live composition, replaced by every swap — read it
	// per call, never cache it.
	Composer() *gooey.Composer
	// Swap replaces the live composition with a new tree.
	Swap(root gooey.Widget)
}

// Options configure a server.
type Options struct {
	// Addr is the listen address. It must be loopback: an MCP client can
	// do anything the keyboard can, and v1 has no authentication. Default
	// "127.0.0.1:0" (an ephemeral port; read it back from Addr).
	Addr string
	// Context is the markup binding context the app was built against.
	// It supplies the Named element table, the bindable values, the
	// commands, and the environment swap_markup builds against. Without
	// one the name-addressed tools report that the app has no markup
	// context; the tree and screen tools still work.
	Context *markup.Context
	// Timeout bounds how long a tool waits for the UI goroutine. Default
	// 5s. A timeout means the run loop is blocked or not running; it is
	// reported as a tool error, never as a hang.
	Timeout time.Duration
	// Name and Version identify this server to clients.
	Name, Version string
}

// Server is a gooey app exposed over MCP.
type Server struct {
	host Host
	bind *markup.Context
	ui   *bridge

	// sdk is the protocol side: the official SDK's server, which owns
	// the JSON-RPC framing, the handshake and tools/list. Our tools are
	// mirrored onto it by register.
	sdk *mcpsdk.Server

	// tools is our own inventory, kept for Tools() and sorted by name.
	// Lookup by name is the SDK's job now.
	tools []*Tool

	ln   net.Listener
	http *http.Server
}

// New builds a server without listening. The zero-network path: tests
// drive Handler directly, and a host that already owns an http.Server
// can mount it wherever it likes — at which point the loopback guarantee
// becomes that host's problem, which is why Serve is the documented way.
func New(host Host, opts Options) (*Server, error) {
	if host == nil {
		return nil, fmt.Errorf("gooey/mcp: nil host")
	}
	to := opts.Timeout
	if to <= 0 {
		to = 5 * time.Second
	}
	s := &Server{
		host: host,
		bind: opts.Context,
		ui:   &bridge{post: host.Post, timeout: to},
		sdk:  newSDKServer(firstNonEmpty(opts.Name, "gooey"), firstNonEmpty(opts.Version, "0.1.0")),
	}
	s.register(s.v1Tools()...)
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
		return nil, fmt.Errorf("gooey/mcp: listen %s: %w", addr, err)
	}
	s.ln = ln
	s.http = &http.Server{Handler: s.Handler()}
	go s.http.Serve(ln)
	return s, nil
}

// checkLoopback refuses a non-loopback bind. This is the whole of v1's
// security posture and it is deliberately a hard error rather than a
// warning: there is no token auth yet, so a server reachable from the
// network is a remote-control handle on the user's terminal. Remote binds
// arrive with authentication or not at all.
func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("gooey/mcp: bad address %q: %w", addr, err)
	}
	switch host {
	case "localhost", "":
		if host == "" {
			return fmt.Errorf("gooey/mcp: %q binds every interface; v1 is loopback-only (use 127.0.0.1:port)", addr)
		}
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("gooey/mcp: %q is not a loopback address; v1 has no authentication, so remote binds are refused", addr)
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

// URL is the endpoint an MCP client connects to.
func (s *Server) URL() string {
	if s.ln == nil {
		return ""
	}
	return "http://" + s.Addr() + endpointPath
}

// Close stops the server. It does not touch the app.
func (s *Server) Close() error {
	if s.http == nil {
		return nil
	}
	return s.http.Close()
}

// Tools lists the registered tools, sorted by name.
func (s *Server) Tools() []*Tool { return s.tools }

func (s *Server) register(ts ...*Tool) {
	for _, t := range ts {
		s.tools = append(s.tools, t)
		s.bindTool(t)
	}
	sort.Slice(s.tools, func(i, j int) bool { return s.tools[i].Name < s.tools[j].Name })
}

// ---- the marshaling primitive ----

// bridge is the crossing between http goroutines and the UI goroutine,
// and it is the only one. Every tool in this package goes through do;
// nothing else in the package touches a widget or a property.
type bridge struct {
	post    func(func())
	timeout time.Duration
}

// do runs fn on the UI goroutine and returns once the UI has SETTLED.
//
// It waits twice, and the second wait is the interesting one. The first
// is fn itself. The second is a bare barrier — a closure that does
// nothing but come back — and it exists because Dispatcher.Drain takes a
// snapshot of its queue: a closure posted while a drain is running lands
// in the NEXT drain, and the run loop composes a frame between two
// drains. So waiting for the barrier waits for the repaint that fn's Sets
// asked for. That is what lets screen_text be called immediately after
// invoke_command and see the new pixels instead of the previous frame,
// and it is why the end-to-end proof does not need sleeps.
//
// A panic inside fn is recovered and returned as an error. An MCP client
// must not be able to kill the app: without this, a tool that hit a nil
// handle would unwind through Drain, out of the run loop, and take the
// terminal with it.
func (b *bridge) do(fn func() error) error {
	err, ok := b.round(fn)
	if !ok {
		return errTimeout(b.timeout)
	}
	if err != nil {
		return err
	}
	if _, ok := b.round(nil); !ok {
		return errTimeout(b.timeout)
	}
	return nil
}

func (b *bridge) round(fn func() error) (error, bool) {
	// Buffered so a closure that arrives after we gave up on it can still
	// complete and be collected instead of parking a goroutine forever.
	done := make(chan error, 1)
	b.post(func() {
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic on the UI goroutine: %v", r)
			}
			done <- err
		}()
		if fn != nil {
			err = fn()
		}
	})
	timer := time.NewTimer(b.timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err, true
	case <-timer.C:
		return nil, false
	}
}

func errTimeout(d time.Duration) error {
	return fmt.Errorf("timed out after %s waiting for the UI goroutine: the app's run loop is blocked or not running", d)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
