// Package visibility exposes the Temporal Visibility API as standalone
// activities, proto-true: every activity's input and output ARE the
// `temporal.api.*` request/response messages, taken verbatim from
// go.temporal.io/api. No DTOs, no renamed fields — Temporal's payload
// converter handles proto natively, so a caller in any SDK language
// builds the same message the server itself speaks and gets the
// server's own answer back.
//
//	w := worker.New(c, taskQueue, worker.Options{})
//	visibility.Register(w, visibility.New(c))
//
// Each activity is one RPC. Registered names are the API — the strings
// a caller schedules — and follow `visibility.<RPC name>`:
// visibility.ListWorkflowExecutions, visibility.CountWorkflowExecutions,
// and so on (see the Name constants).
//
// # Namespace defaulting
//
// One rule, applied uniformly: a request whose Namespace field is empty
// (or a nil request) is filled with the worker's namespace —
// WithNamespace, or Temporal's "default". The default is written into
// the request in place, which is safe for activity invocations (the
// request was deserialized fresh for this call) and worth knowing for
// direct Go calls. A request that names a namespace passes through
// untouched.
//
// # Pagination
//
// next_page_token passes through verbatim in both directions. The token
// is opaque server state; page exactly as you would against the raw
// gRPC API.
//
// # Context and heartbeats
//
// These are fast unary calls: no heartbeats are recorded and none are
// needed. The activity context flows straight into the RPC, so the
// invoker's StartToCloseTimeout (or a cancellation) bounds the call.
//
// Calls go through the SDK client's raw service clients
// (client.WorkflowService / client.OperatorService — one shared gRPC
// connection), which skip the SDK's automatic request retries. That is
// deliberate: the caller invoked an activity, and retrying is the
// activity machinery's job, configured exactly once by its retry
// policy.
package visibility

import (
	"context"

	"go.temporal.io/api/operatorservice/v1"
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
	NameListWorkflowExecutions       = "visibility.ListWorkflowExecutions"
	NameListOpenWorkflowExecutions   = "visibility.ListOpenWorkflowExecutions"
	NameListClosedWorkflowExecutions = "visibility.ListClosedWorkflowExecutions"
	NameCountWorkflowExecutions      = "visibility.CountWorkflowExecutions"
	NameGetSearchAttributes          = "visibility.GetSearchAttributes"
	NameDescribeWorkflowExecution    = "visibility.DescribeWorkflowExecution"
	NameListSearchAttributes         = "visibility.ListSearchAttributes"
)

// AllNames lists every activity Register registers, in registration
// order: the seven proto-true activities, then the three scalar
// conveniences (see convenience.go).
func AllNames() []string {
	return []string{
		NameListWorkflowExecutions,
		NameListOpenWorkflowExecutions,
		NameListClosedWorkflowExecutions,
		NameCountWorkflowExecutions,
		NameGetSearchAttributes,
		NameDescribeWorkflowExecution,
		NameListSearchAttributes,
		NameQuery,
		NameCount,
		NameDescribe,
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
		{NameListWorkflowExecutions, a.ListWorkflowExecutions},
		{NameListOpenWorkflowExecutions, a.ListOpenWorkflowExecutions},
		{NameListClosedWorkflowExecutions, a.ListClosedWorkflowExecutions},
		{NameCountWorkflowExecutions, a.CountWorkflowExecutions},
		{NameGetSearchAttributes, a.GetSearchAttributes},
		{NameDescribeWorkflowExecution, a.DescribeWorkflowExecution},
		{NameListSearchAttributes, a.ListSearchAttributes},
		{NameQuery, a.Query},
		{NameCount, a.Count},
		{NameDescribe, a.Describe},
	} {
		r.RegisterActivityWithOptions(reg.fn, activity.RegisterOptions{Name: reg.name})
	}
}

// ListWorkflowExecutions is the query-language path: filter with
// Temporal's visibility query syntax in req.Query.
func (a *Activities) ListWorkflowExecutions(ctx context.Context, req *workflowservice.ListWorkflowExecutionsRequest) (*workflowservice.ListWorkflowExecutionsResponse, error) {
	if req == nil {
		req = &workflowservice.ListWorkflowExecutionsRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().ListWorkflowExecutions(ctx, req)
}

// ListOpenWorkflowExecutions lists currently-running executions,
// filtered by start time and optionally execution or type.
func (a *Activities) ListOpenWorkflowExecutions(ctx context.Context, req *workflowservice.ListOpenWorkflowExecutionsRequest) (*workflowservice.ListOpenWorkflowExecutionsResponse, error) {
	if req == nil {
		req = &workflowservice.ListOpenWorkflowExecutionsRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().ListOpenWorkflowExecutions(ctx, req)
}

// ListClosedWorkflowExecutions lists completed executions, filtered by
// close time and optionally execution, type, or status.
func (a *Activities) ListClosedWorkflowExecutions(ctx context.Context, req *workflowservice.ListClosedWorkflowExecutionsRequest) (*workflowservice.ListClosedWorkflowExecutionsResponse, error) {
	if req == nil {
		req = &workflowservice.ListClosedWorkflowExecutionsRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().ListClosedWorkflowExecutions(ctx, req)
}

// CountWorkflowExecutions counts executions matching req.Query,
// optionally grouped.
func (a *Activities) CountWorkflowExecutions(ctx context.Context, req *workflowservice.CountWorkflowExecutionsRequest) (*workflowservice.CountWorkflowExecutionsResponse, error) {
	if req == nil {
		req = &workflowservice.CountWorkflowExecutionsRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().CountWorkflowExecutions(ctx, req)
}

// GetSearchAttributes is the legacy, cluster-scoped schema RPC — its
// request has no fields at all, so no namespace defaulting applies.
// Prefer ListSearchAttributes for the namespaced schema.
func (a *Activities) GetSearchAttributes(ctx context.Context, req *workflowservice.GetSearchAttributesRequest) (*workflowservice.GetSearchAttributesResponse, error) {
	if req == nil {
		req = &workflowservice.GetSearchAttributesRequest{}
	}
	return a.client.WorkflowService().GetSearchAttributes(ctx, req)
}

// DescribeWorkflowExecution is the detail pane's RPC: config, pending
// activities and children, and the execution's current state.
func (a *Activities) DescribeWorkflowExecution(ctx context.Context, req *workflowservice.DescribeWorkflowExecutionRequest) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	if req == nil {
		req = &workflowservice.DescribeWorkflowExecutionRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().DescribeWorkflowExecution(ctx, req)
}

// ListSearchAttributes is the modern, namespaced schema surface, served
// by the operator service over the same connection.
func (a *Activities) ListSearchAttributes(ctx context.Context, req *operatorservice.ListSearchAttributesRequest) (*operatorservice.ListSearchAttributesResponse, error) {
	if req == nil {
		req = &operatorservice.ListSearchAttributesRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.OperatorService().ListSearchAttributes(ctx, req)
}
