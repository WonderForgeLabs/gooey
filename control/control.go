// Package control is the in-process control-plane service for a running
// gooey app: one implementation of the gooey.control.v1 surface
// (docs/specs/2026-08-10-grpc-contract.md) that every transport fronts.
// The gRPC server (the nested grpc/ module) is a proto adapter over this
// package; the MCP server (mcp/) is scheduled to become one (issue
// #112). One path, one model: a tool or an RPC does what this package
// does, or it does not exist.
//
// It lives in the ROOT module on purpose. The transports are nested
// modules because their SDKs are heavy; the logic they share is plain Go
// over the framework's own interfaces, so the root hosts it and both
// import it — the alternative, one transport calling the other over
// loopback, would put a network hop inside what is semantically a
// function call.
//
// # The concurrency rule
//
// This package inherits gooey's central confinement rule: the property
// graph is unlocked and confined to the UI goroutine, so every Service
// method is UI-GOROUTINE-ONLY. A transport receives requests on its own
// goroutines and marshals each call through Bridge.Do, which runs it on
// the UI loop and — the second half of the guarantee — returns only
// after the frame the call's Sets asked for has been composed (the
// settle barrier). Nothing a Service method returns holds a component or
// a property handle: results are plain copied data, safe to serialize
// from any goroutine afterwards.
//
// Every property Get in this package happens outside any computed
// evaluation, so it reads a value and records nothing — the call-site
// rule. A snapshot that subscribed would wire the control plane into the
// damage graph and repaint the app every time a client looked at it.
package control

import (
	"fmt"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
)

// Host is what the service needs from a running app: a way onto the UI
// goroutine, the live composition, and the tree-swap seam. *gooey.App
// implements it. It is an interface so the service can be tested against
// a hand-run loop — the only honest way to test the confinement rule.
type Host interface {
	// Post queues fn to run on the UI goroutine. Safe from any goroutine.
	Post(fn func())
	// Composer is the live composition, replaced by every swap — read it
	// per call, never cache it.
	Composer() *gooey.Composer
	// Swap replaces the live composition with a new tree.
	Swap(root gooey.Component)
}

// Service implements the control-plane operations against one app. All
// methods are UI-goroutine-only (see the package comment); transports
// call them through Bridge.Do.
type Service struct {
	host Host
	bind *markup.Context

	// Doc, when set, supplies the markup source the running page was
	// built from — what DeclaredSchema falls back to when asked about
	// "the running document". Optional: a host that builds its tree in
	// Go has no source to offer. UI-goroutine-only, like everything else.
	Doc func() []byte

	// Echo, when set, receives every input event this service injects
	// (SendKeys, SendPointer) after it was dispatched, with whether the
	// tree consumed it. It is how a streaming transport echoes remote
	// input alongside the terminal's. Called on the UI goroutine.
	Echo func(ev EchoEvent)
}

// NewService builds a service over host. bind is the markup binding
// context the app was built against; nil is legal and leaves only the
// tree and screen operations available, exactly as the MCP server
// degrades.
func NewService(host Host, bind *markup.Context) *Service {
	return &Service{host: host, bind: bind}
}

// Bind is the markup context this service addresses names against.
func (s *Service) Bind() *markup.Context { return s.bind }

// ---- errors ----

// ErrorKind classifies a service failure the way the wire contract
// does, so a transport can map it without parsing text: gRPC turns
// these into status codes, MCP into tool errors.
type ErrorKind int

const (
	// KindInvalidArgument: a type mismatch (the message names both
	// sides), bad markup, a bad gesture, a bad registration.
	KindInvalidArgument ErrorKind = iota
	// KindNotFound: no such name in the binding context or the tree.
	KindNotFound
	// KindFailedPrecondition: the app has no markup context or no live
	// composition — the request is well-formed but the app cannot answer.
	KindFailedPrecondition
)

// Error is a classified service failure.
type Error struct {
	Kind ErrorKind
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

func invalidf(format string, args ...any) *Error {
	return &Error{Kind: KindInvalidArgument, Msg: fmt.Sprintf(format, args...)}
}

func notFoundf(format string, args ...any) *Error {
	return &Error{Kind: KindNotFound, Msg: fmt.Sprintf(format, args...)}
}

func preconditionf(format string, args ...any) *Error {
	return &Error{Kind: KindFailedPrecondition, Msg: fmt.Sprintf(format, args...)}
}

var errNoContext = &Error{
	Kind: KindFailedPrecondition,
	Msg:  "this app was served without a markup context, so it has no named values to address",
}

// ---- shared lookups ----

func (s *Service) composer() (*gooey.Composer, error) {
	c := s.host.Composer()
	if c == nil {
		return nil, preconditionf("the app has no live composition yet: it is not running")
	}
	return c, nil
}

// lookup resolves a dotted path in the binding context, the same way a
// {{.A.B}} binding does.
func (s *Service) lookup(path string) (any, error) {
	if s.bind == nil {
		return nil, errNoContext
	}
	var cur any = s.bind.Values
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, notFoundf("cannot resolve %q past %T", path, cur)
		}
		cur, ok = m[seg]
		if !ok {
			return nil, notFoundf("no value named %q in the app's context; ListValues shows what there is", path)
		}
	}
	return cur, nil
}
