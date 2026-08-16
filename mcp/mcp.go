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
// # One path, one model
//
// Every tool is a thin adapter over the root module's control package —
// the same in-process service the gRPC server fronts (issue #112). A
// tool body parses MCP arguments, calls control.Service, and renders the
// result the way this surface has always rendered it; nothing in this
// package touches a component or a property directly. What stays here is
// exactly what is MCP's own: the transport, the tool schemas, the
// argument parsing, and the rendered wording (tool-facing error strings
// name tools — list_values, not ListValues).
//
// # The concurrency rule
//
// This package exists on the wrong side of gooey's central confinement
// rule and knows it. MCP requests arrive on net/http goroutines; the
// property graph is unlocked and confined to the run loop. So no tool
// body ever runs on the goroutine that received the request: every call
// is marshaled through control.Bridge and executed on the UI loop, and
// the result crosses back as plain data. The Tool type is shaped to
// make that structural rather than remembered — a Tool's Run is *defined*
// as running on the UI goroutine, and dispatch is the only thing that
// ever calls it, so a tool cannot be written the wrong way by accident.
//
// The second half of the rule is that nothing here holds a reference to
// the tree between requests. Results are plain maps of copied values,
// never components or property handles: a *prop.Property read from an http
// goroutine after the response went out would be exactly the bug the
// Dispatcher exists to prevent, and a hot reload would have replaced the
// component anyway.
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

	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/netutil"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Host is what this server needs from a running app: a way onto the UI
// goroutine, the live composition, and the tree-swap seam. It IS the
// control package's Host — one contract, every transport — and
// *gooey.App implements it.
type Host = control.Host

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
	// Grant scopes this endpoint to one island: every tool reaches the
	// granted subtree and the granted value names, and refuses or
	// narrows everything else (control/grant.go). nil is the HOST's own
	// endpoint — the whole app.
	//
	// The enforcement is not in this package and must not be: it lives
	// in control.Service, which every tool body already calls, so MCP and
	// gRPC cannot drift into two different ideas of what an island is.
	// One path, one model applies to the scope as much as to the verbs.
	Grant *control.Grant
}

// Server is a gooey app exposed over MCP.
type Server struct {
	// svc is the shared control-plane service — the same implementation
	// the gRPC server fronts. Its methods are UI-goroutine-only, which is
	// the same contract Tool.Run carries, so tool bodies call it freely.
	svc *control.Service
	// ui is the crossing between http goroutines and the UI goroutine,
	// and it is the only one: every tool call goes through ui.Do.
	ui *control.Bridge

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
	s := &Server{
		svc: control.NewScopedService(host, opts.Context, opts.Grant),
		ui:  control.NewBridge(host.Post, opts.Timeout),
		sdk: newSDKServer(firstNonEmpty(opts.Name, "gooey"), firstNonEmpty(opts.Version, "0.1.0")),
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
	if err := netutil.CheckLoopback("gooey/mcp", addr); err != nil {
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

// The loopback rule itself lives in netutil.CheckLoopback, along with the
// reasoning for every clause. It is public and shared because an
// embedding host that owns its own listener (see New, above) has to be
// able to apply the same rule — apps/kanban maintained a hand-copied
// stand-in for exactly as long as this was unexported.

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

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
