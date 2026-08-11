// Package workflow exposes the Temporal workflow LIFECYCLE ACTS as
// standalone activities, proto-true: every activity's input and output
// ARE the `temporal.api.*` request/response messages, taken verbatim
// from go.temporal.io/api. No DTOs, no renamed fields — Temporal's
// payload converter handles proto natively, so a caller in any SDK
// language builds the same message the server itself speaks and gets
// the server's own answer back.
//
//	w := worker.New(c, taskQueue, worker.Options{})
//	workflow.Register(w, workflow.New(c))
//
// This is the act half of what packs/temporal-visibility reads: a
// dashboard lists executions with visibility.*, then starts, signals,
// queries, cancels, terminates or resets them with workflow.*.
//
// Each activity is one RPC. Registered names are the API — the strings
// a caller schedules — and follow `workflow.<RPC name>`:
// workflow.SignalWorkflowExecution, workflow.TerminateWorkflowExecution,
// and so on (see the Name constants).
//
// # Import name
//
// The Go package is `workflow`, per the pack-naming convention
// (packs/temporal-<domain>, package = domain). It collides by name with
// go.temporal.io/sdk/workflow, which a workflow-side caller imports; a
// file needing both should alias:
//
//	import (
//	    "go.temporal.io/sdk/workflow"
//	    workflowacts "github.com/WonderForgeLabs/gooey/packs/temporal-workflow"
//	)
//
// The pack itself has no such problem: it imports the SDK's client,
// worker and activity packages only, never sdk/workflow.
//
// # Registration is the capability grant
//
// Terminate and Reset are destructive, and the pack's own Register is
// the boundary that grants them. A host that registers only
// packs/temporal-visibility structurally CANNOT terminate anything —
// the activity does not exist on its task queue. Splitting the acts out
// of the read-only pack is what makes that a fact rather than an
// intention. Confirmation UI belongs to the app; the pack's contribution
// is that a host which never called Register has nothing to confirm.
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
// # request_id is the caller's, deliberately
//
// Five of these requests carry a `request_id` for server-side
// deduplication, and the pack never fills one — that field is as much
// the caller's here as it is against the raw gRPC API. It matters most
// for StartWorkflowExecution and SignalWithStartWorkflowExecution:
// AN ACTIVITY CAN BE RETRIED, and if each attempt carried a fresh
// request_id, a retry after an ambiguous failure would start a second
// workflow. Send a request_id that is stable across attempts of one
// invocation — derived from the invoking workflow's own ID, or from
// activity.GetInfo(ctx).ActivityID at the call site — and the server
// dedupes the retry for you. A pack-generated UUID could not know which
// of those the caller meant, and a per-attempt one would be actively
// wrong.
//
// # Context and heartbeats
//
// These are unary calls and no heartbeats are recorded. The activity
// context flows straight into the RPC, so the invoker's
// StartToCloseTimeout (or a cancellation) bounds the call. QueryWorkflow
// is the one to size generously: it is answered by a worker polling the
// target's task queue, so its latency is that worker's, not the
// server's.
//
// Calls go through the SDK client's raw service client
// (client.WorkflowService — the same shared gRPC connection), which
// skips the SDK's automatic request retries. That is deliberate: the
// caller invoked an activity, and retrying is the activity machinery's
// job, configured exactly once by its retry policy. It matters more
// here than in a read-only pack — an SDK-level retry of a terminate is
// a retry nobody configured.
package workflow

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
	NameStartWorkflowExecution           = "workflow.StartWorkflowExecution"
	NameSignalWorkflowExecution          = "workflow.SignalWorkflowExecution"
	NameSignalWithStartWorkflowExecution = "workflow.SignalWithStartWorkflowExecution"
	NameQueryWorkflow                    = "workflow.QueryWorkflow"
	NameRequestCancelWorkflowExecution   = "workflow.RequestCancelWorkflowExecution"
	NameTerminateWorkflowExecution       = "workflow.TerminateWorkflowExecution"
	NameResetWorkflowExecution           = "workflow.ResetWorkflowExecution"
)

// AllNames lists every activity Register registers, in registration
// order.
func AllNames() []string {
	return []string{
		NameStartWorkflowExecution,
		NameSignalWorkflowExecution,
		NameSignalWithStartWorkflowExecution,
		NameQueryWorkflow,
		NameRequestCancelWorkflowExecution,
		NameTerminateWorkflowExecution,
		NameResetWorkflowExecution,
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
//
// This call is the capability grant: after it, the host's task queue
// can terminate and reset workflows. Before it, it cannot.
func Register(r worker.ActivityRegistry, a *Activities) {
	for _, reg := range []struct {
		name string
		fn   any
	}{
		{NameStartWorkflowExecution, a.StartWorkflowExecution},
		{NameSignalWorkflowExecution, a.SignalWorkflowExecution},
		{NameSignalWithStartWorkflowExecution, a.SignalWithStartWorkflowExecution},
		{NameQueryWorkflow, a.QueryWorkflow},
		{NameRequestCancelWorkflowExecution, a.RequestCancelWorkflowExecution},
		{NameTerminateWorkflowExecution, a.TerminateWorkflowExecution},
		{NameResetWorkflowExecution, a.ResetWorkflowExecution},
	} {
		r.RegisterActivityWithOptions(reg.fn, activity.RegisterOptions{Name: reg.name})
	}
}

// StartWorkflowExecution starts a new execution. See the package note on
// request_id: it is the caller's, and it must be stable across activity
// attempts or a retry starts a second workflow.
func (a *Activities) StartWorkflowExecution(ctx context.Context, req *workflowservice.StartWorkflowExecutionRequest) (*workflowservice.StartWorkflowExecutionResponse, error) {
	if req == nil {
		req = &workflowservice.StartWorkflowExecutionRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().StartWorkflowExecution(ctx, req)
}

// SignalWorkflowExecution delivers a signal to a running execution.
func (a *Activities) SignalWorkflowExecution(ctx context.Context, req *workflowservice.SignalWorkflowExecutionRequest) (*workflowservice.SignalWorkflowExecutionResponse, error) {
	if req == nil {
		req = &workflowservice.SignalWorkflowExecutionRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().SignalWorkflowExecution(ctx, req)
}

// SignalWithStartWorkflowExecution signals an execution, starting it
// first if it is not already running. Same request_id caution as
// StartWorkflowExecution.
func (a *Activities) SignalWithStartWorkflowExecution(ctx context.Context, req *workflowservice.SignalWithStartWorkflowExecutionRequest) (*workflowservice.SignalWithStartWorkflowExecutionResponse, error) {
	if req == nil {
		req = &workflowservice.SignalWithStartWorkflowExecutionRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().SignalWithStartWorkflowExecution(ctx, req)
}

// QueryWorkflow runs a synchronous query against an execution.
func (a *Activities) QueryWorkflow(ctx context.Context, req *workflowservice.QueryWorkflowRequest) (*workflowservice.QueryWorkflowResponse, error) {
	if req == nil {
		req = &workflowservice.QueryWorkflowRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().QueryWorkflow(ctx, req)
}

// RequestCancelWorkflowExecution requests cancellation — the graceful
// act, which the workflow observes and may handle. Its ungraceful
// counterpart is TerminateWorkflowExecution.
func (a *Activities) RequestCancelWorkflowExecution(ctx context.Context, req *workflowservice.RequestCancelWorkflowExecutionRequest) (*workflowservice.RequestCancelWorkflowExecutionResponse, error) {
	if req == nil {
		req = &workflowservice.RequestCancelWorkflowExecutionRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().RequestCancelWorkflowExecution(ctx, req)
}

// TerminateWorkflowExecution ends an execution immediately, without
// giving it a chance to run cleanup. Destructive: see the package note
// on Register as the capability boundary.
func (a *Activities) TerminateWorkflowExecution(ctx context.Context, req *workflowservice.TerminateWorkflowExecutionRequest) (*workflowservice.TerminateWorkflowExecutionResponse, error) {
	if req == nil {
		req = &workflowservice.TerminateWorkflowExecutionRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().TerminateWorkflowExecution(ctx, req)
}

// ResetWorkflowExecution rewinds an execution to an earlier event and
// replays from there, producing a new run. Destructive: see the package
// note on Register as the capability boundary.
func (a *Activities) ResetWorkflowExecution(ctx context.Context, req *workflowservice.ResetWorkflowExecutionRequest) (*workflowservice.ResetWorkflowExecutionResponse, error) {
	if req == nil {
		req = &workflowservice.ResetWorkflowExecutionRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().ResetWorkflowExecution(ctx, req)
}
