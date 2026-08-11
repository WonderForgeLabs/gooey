// Package taskqueue exposes Temporal's task-queue introspection as
// standalone activities, proto-true: every activity's input and output
// ARE the `temporal.api.*` request/response messages, taken verbatim
// from go.temporal.io/api. No DTOs, no renamed fields — Temporal's
// payload converter handles proto natively, so a caller in any SDK
// language builds the same message the server itself speaks and gets
// the server's own answer back.
//
//	w := worker.New(c, taskQueue, worker.Options{})
//	taskqueue.Register(w, taskqueue.New(c))
//
// This is the are-my-workers-alive panel. It pairs with gooey's
// companion-worker story: an app that runs its own worker in-process
// can watch that worker's health through the same server everything
// else goes through, with no side channel.
//
// Each activity is one RPC. Registered names are the API — the strings
// a caller schedules — and follow `taskqueue.<RPC name>`:
// taskqueue.DescribeTaskQueue and taskqueue.ListTaskQueuePartitions
// (see the Name constants).
//
// # Read-only, on purpose
//
// Both RPCs here observe; neither changes anything. `UpdateTaskQueueConfig`
// exists on WorkflowService and is deliberately NOT in this pack — under
// the registration-is-a-capability-grant split, a host that wants a
// health panel should not thereby gain the ability to reconfigure a
// queue. If a config-writing surface is wanted, it belongs in its own
// pack, registered deliberately.
//
// # report_stats and report_config are the caller's, deliberately
//
// DescribeTaskQueue answers cheaply by default and returns backlog
// statistics only when req.ReportStats is set (and task-queue config
// only with req.ReportConfig). The pack sets NEITHER: they cost the
// server real work, and a pack that quietly forced them on would make
// every poll of a health dashboard more expensive than the caller
// asked for. A dashboard that wants backlog depth asks for it. Both
// flags need a recent enough server; an older one simply returns the
// response without those fields, which is the server's answer to give,
// not the pack's to translate.
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
// These are fast unary calls: no heartbeats are recorded and none are
// needed. The activity context flows straight into the RPC, so the
// invoker's StartToCloseTimeout (or a cancellation) bounds the call.
//
// Calls go through the SDK client's raw service client
// (client.WorkflowService — the same shared gRPC connection), which
// skips the SDK's automatic request retries. That is deliberate: the
// caller invoked an activity, and retrying is the activity machinery's
// job, configured exactly once by its retry policy.
package taskqueue

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
	NameDescribeTaskQueue       = "taskqueue.DescribeTaskQueue"
	NameListTaskQueuePartitions = "taskqueue.ListTaskQueuePartitions"
)

// AllNames lists every activity Register registers, in registration
// order.
func AllNames() []string {
	return []string{
		NameDescribeTaskQueue,
		NameListTaskQueuePartitions,
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
		{NameDescribeTaskQueue, a.DescribeTaskQueue},
		{NameListTaskQueuePartitions, a.ListTaskQueuePartitions},
	} {
		r.RegisterActivityWithOptions(reg.fn, activity.RegisterOptions{Name: reg.name})
	}
}

// DescribeTaskQueue reports the pollers currently attached to a task
// queue, and — when req.ReportStats is set — its backlog statistics.
// See the package note: the pack never sets that flag for you.
func (a *Activities) DescribeTaskQueue(ctx context.Context, req *workflowservice.DescribeTaskQueueRequest) (*workflowservice.DescribeTaskQueueResponse, error) {
	if req == nil {
		req = &workflowservice.DescribeTaskQueueRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().DescribeTaskQueue(ctx, req)
}

// ListTaskQueuePartitions reports how a task queue is partitioned
// across the cluster — the shape behind a queue's throughput.
func (a *Activities) ListTaskQueuePartitions(ctx context.Context, req *workflowservice.ListTaskQueuePartitionsRequest) (*workflowservice.ListTaskQueuePartitionsResponse, error) {
	if req == nil {
		req = &workflowservice.ListTaskQueuePartitionsRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().ListTaskQueuePartitions(ctx, req)
}
