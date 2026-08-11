// Package history exposes Temporal's execution-history reads as
// standalone activities, proto-true: every activity's input and output
// ARE the `temporal.api.*` request/response messages, taken verbatim
// from go.temporal.io/api. No DTOs, no renamed fields — Temporal's
// payload converter handles proto natively, so a caller in any SDK
// language builds the same message the server itself speaks and gets
// the server's own answer back.
//
//	w := worker.New(c, taskQueue, worker.Options{})
//	history.Register(w, history.New(c))
//
// This is the raw event source: paired with
// visibility.DescribeWorkflowExecution it fills a detail view, and it
// is what a timeline component would render.
//
// Each activity is one RPC. Registered names are the API — the strings
// a caller schedules — and follow `history.<RPC name>`:
// history.GetWorkflowExecutionHistory and
// history.GetWorkflowExecutionHistoryReverse (see the Name constants).
//
// # Namespace defaulting
//
// One rule, applied uniformly: a request whose Namespace field is empty
// (or a nil request) is filled with the worker's namespace —
// WithNamespace, or Temporal's "default". The default is written into
// the request in place, which is safe for activity invocations (the
// request was deserialized fresh for this call) and worth knowing for
// direct Go calls. A request that names a namespace passes through
// untouched. This is the pack's ONLY deviation from verbatim
// pass-through, and it only ever fills an empty field.
//
// # Pagination
//
// next_page_token passes through verbatim in both directions. The token
// is opaque server state; page exactly as you would against the raw
// gRPC API. The forward and reverse RPCs have SEPARATE token spaces —
// a token from one is not valid in the other.
//
// # wait_new_event makes this a LONG POLL, and that changes the timeout
//
// This is the pack that is not "a fast unary call". With
// `wait_new_event` set, GetWorkflowExecutionHistory does not return
// until a matching event arrives or the SERVER's long-poll deadline
// expires (tens of seconds). Two consequences the caller owns:
//
//   - Size StartToCloseTimeout ABOVE the server's long-poll timeout.
//     Below it, every poll of a quiet workflow times out mid-flight and
//     the activity retries, turning a cheap wait into a hot loop.
//   - No heartbeats are recorded, and none CAN be: the RPC blocks in a
//     single call with nothing to report partway. HeartbeatTimeout is
//     therefore the wrong knife here — use StartToCloseTimeout, and if
//     you want a bounded wait, bound it with the request's own
//     `wait_new_event`/filter semantics plus the activity's context,
//     which cancels the in-flight RPC.
//
// Without `wait_new_event` (the default) these are ordinary reads and
// return promptly.
//
// Calls go through the SDK client's raw service client
// (client.WorkflowService — the same shared gRPC connection), which
// skips the SDK's automatic request retries. That is deliberate: the
// caller invoked an activity, and retrying is the activity machinery's
// job, configured exactly once by its retry policy.
package history

import (
	"context"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// DefaultNamespace is what fills an empty request namespace when the
// host configured none — Temporal's own default namespace.
const DefaultNamespace = "default"

// The registered activity names. These strings are the pack's public
// API: what any Temporal client, in any language, schedules.
const (
	NameGetWorkflowExecutionHistory        = "history.GetWorkflowExecutionHistory"
	NameGetWorkflowExecutionHistoryReverse = "history.GetWorkflowExecutionHistoryReverse"
)

// AllNames lists every activity Register registers, in registration
// order.
func AllNames() []string {
	return []string{
		NameGetWorkflowExecutionHistory,
		NameGetWorkflowExecutionHistoryReverse,
	}
}

// Activities holds the connected client the activities call through and
// the namespace that fills requests which don't name one.
type Activities struct {
	client    client.Client
	namespace string
}

// Option configures Activities.
type Option func(*Activities)

// WithNamespace sets the namespace filled into requests that leave
// theirs empty — normally the same namespace the worker's client was
// dialed with.
func WithNamespace(ns string) Option {
	return func(a *Activities) {
		if ns != "" {
			a.namespace = ns
		}
	}
}

// New builds the activity set over a connected client.
func New(c client.Client, opts ...Option) *Activities {
	a := &Activities{client: c, namespace: DefaultNamespace}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Register registers every activity in the pack on r under its
// canonical name. r is any SDK worker (worker.Worker satisfies
// worker.ActivityRegistry), so the same call serves a standalone worker
// binary and an embedded one.
func Register(r worker.ActivityRegistry, a *Activities) {
	for _, reg := range []struct {
		name string
		fn   any
	}{
		{NameGetWorkflowExecutionHistory, a.GetWorkflowExecutionHistory},
		{NameGetWorkflowExecutionHistoryReverse, a.GetWorkflowExecutionHistoryReverse},
	} {
		r.RegisterActivityWithOptions(reg.fn, activity.RegisterOptions{Name: reg.name})
	}
}

// GetWorkflowExecutionHistory reads an execution's history oldest-first,
// paged by next_page_token. With req.WaitNewEvent set it becomes a long
// poll — see the package note on timeouts.
func (a *Activities) GetWorkflowExecutionHistory(ctx context.Context, req *workflowservice.GetWorkflowExecutionHistoryRequest) (*workflowservice.GetWorkflowExecutionHistoryResponse, error) {
	if req == nil {
		req = &workflowservice.GetWorkflowExecutionHistoryRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().GetWorkflowExecutionHistory(ctx, req)
}

// GetWorkflowExecutionHistoryReverse reads an execution's history
// newest-first — the cheap way to answer "how did this end?" without
// paging the whole history forward. It has no long-poll flag, and its
// page tokens are a separate space from the forward RPC's.
func (a *Activities) GetWorkflowExecutionHistoryReverse(ctx context.Context, req *workflowservice.GetWorkflowExecutionHistoryReverseRequest) (*workflowservice.GetWorkflowExecutionHistoryReverseResponse, error) {
	if req == nil {
		req = &workflowservice.GetWorkflowExecutionHistoryReverseRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().GetWorkflowExecutionHistoryReverse(ctx, req)
}
