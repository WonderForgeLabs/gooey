// Package batch exposes Temporal's batch-operation API as standalone
// activities, proto-true: every activity's input and output ARE the
// `temporal.api.*` request/response messages, taken verbatim from
// go.temporal.io/api. No DTOs, no renamed fields — Temporal's payload
// converter handles proto natively, so a caller in any SDK language
// builds the same message the server itself speaks and gets the
// server's own answer back.
//
//	w := worker.New(c, taskQueue, worker.Options{})
//	batch.Register(w, batch.New(c))
//
// This is the bulk complement to packs/temporal-workflow: where that
// pack terminates one execution, this one terminates everything a
// visibility query matches. It pairs with a dashboard's query bar —
// type a query, see the matches, act on all of them.
//
// Each activity is one RPC. Registered names are the API — the strings
// a caller schedules — and follow `batch.<RPC name>`:
// batch.StartBatchOperation, batch.DescribeBatchOperation, and so on
// (see the Name constants).
//
// # Registration is the capability grant, and this one is large
//
// A batch terminate reaches every execution its query matches, so the
// ability to START one is a strictly larger grant than the ability to
// terminate a single workflow — which is why this is its own module
// rather than more activities in packs/temporal-workflow. A host
// registers it deliberately or not at all. Confirmation UI belongs to
// the app; the pack's contribution is that a host which never called
// Register has nothing to confirm.
//
// # The operation oneof is the reason proto fidelity pays here
//
// StartBatchOperationRequest.Operation is a oneof over a dozen
// operation types (terminate, signal, cancel, delete, reset, update
// workflow options, and several activity-scoped ones), and it GROWS. A
// hand-rolled DTO would have to enumerate them and would be stale by
// the next server release; passing the proto through means a new
// operation kind reaches callers by bumping go.temporal.io/api, with no
// change to this pack at all.
//
// Likewise `visibility_query` and `target_executions` are mutually
// exclusive, and the pack does not adjudicate between them: the server
// owns that rule, and its error is the honest answer.
//
// # job_id is the caller's, deliberately
//
// StartBatchOperationRequest carries a `job_id` that both names the job
// for later Describe/Stop calls and serves as its idempotency key. The
// pack never fills one. AN ACTIVITY CAN BE RETRIED, and a fresh job_id
// per attempt would start a SECOND batch — over the same query, doing
// the same destructive thing twice. Send a job_id stable across
// attempts of one invocation (derived from the invoking workflow's own
// ID, or activity.GetInfo(ctx).ActivityID) and a retry after an
// ambiguous failure resolves to the job already running rather than a
// duplicate of it.
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
// # Context and heartbeats
//
// All four calls are fast unary calls — including StartBatchOperation,
// which ENQUEUES the job and returns; it does not wait for it. Progress
// is observed by polling DescribeBatchOperation, so a caller wanting to
// wait writes that loop itself (in a workflow, with a timer between
// polls — not by holding an activity open). No heartbeats are recorded
// and none are needed.
//
// Calls go through the SDK client's raw service client
// (client.WorkflowService — the same shared gRPC connection), which
// skips the SDK's automatic request retries. That is deliberate: the
// caller invoked an activity, and retrying is the activity machinery's
// job, configured exactly once by its retry policy — and an SDK-level
// retry of a batch start is a retry nobody configured.
package batch

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
	NameStartBatchOperation    = "batch.StartBatchOperation"
	NameStopBatchOperation     = "batch.StopBatchOperation"
	NameDescribeBatchOperation = "batch.DescribeBatchOperation"
	NameListBatchOperations    = "batch.ListBatchOperations"
)

// AllNames lists every activity Register registers, in registration
// order.
func AllNames() []string {
	return []string{
		NameStartBatchOperation,
		NameStopBatchOperation,
		NameDescribeBatchOperation,
		NameListBatchOperations,
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
// can terminate every execution a visibility query matches. Before it,
// it cannot.
func Register(r worker.ActivityRegistry, a *Activities) {
	for _, reg := range []struct {
		name string
		fn   any
	}{
		{NameStartBatchOperation, a.StartBatchOperation},
		{NameStopBatchOperation, a.StopBatchOperation},
		{NameDescribeBatchOperation, a.DescribeBatchOperation},
		{NameListBatchOperations, a.ListBatchOperations},
	} {
		r.RegisterActivityWithOptions(reg.fn, activity.RegisterOptions{Name: reg.name})
	}
}

// StartBatchOperation enqueues a batch job over a visibility query (or
// an explicit execution list) and returns immediately — it does not
// wait for the job. Poll DescribeBatchOperation for progress. See the
// package note on job_id: it is the caller's, and it must be stable
// across activity attempts or a retry starts a second batch.
func (a *Activities) StartBatchOperation(ctx context.Context, req *workflowservice.StartBatchOperationRequest) (*workflowservice.StartBatchOperationResponse, error) {
	if req == nil {
		req = &workflowservice.StartBatchOperationRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().StartBatchOperation(ctx, req)
}

// StopBatchOperation halts a running batch job. Executions already
// acted on stay acted on — stopping is not undoing.
func (a *Activities) StopBatchOperation(ctx context.Context, req *workflowservice.StopBatchOperationRequest) (*workflowservice.StopBatchOperationResponse, error) {
	if req == nil {
		req = &workflowservice.StopBatchOperationRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().StopBatchOperation(ctx, req)
}

// DescribeBatchOperation reports a job's state and progress counts —
// the RPC a progress UI polls, and the one a caller waiting for a batch
// should loop on with a timer between calls.
func (a *Activities) DescribeBatchOperation(ctx context.Context, req *workflowservice.DescribeBatchOperationRequest) (*workflowservice.DescribeBatchOperationResponse, error) {
	if req == nil {
		req = &workflowservice.DescribeBatchOperationRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().DescribeBatchOperation(ctx, req)
}

// ListBatchOperations lists the namespace's batch jobs, paged by
// next_page_token.
func (a *Activities) ListBatchOperations(ctx context.Context, req *workflowservice.ListBatchOperationsRequest) (*workflowservice.ListBatchOperationsResponse, error) {
	if req == nil {
		req = &workflowservice.ListBatchOperationsRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().ListBatchOperations(ctx, req)
}
