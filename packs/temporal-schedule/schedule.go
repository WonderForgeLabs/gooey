// Package schedule exposes Temporal's Schedule API as standalone
// activities, proto-true: every activity's input and output ARE the
// `temporal.api.*` request/response messages, taken verbatim from
// go.temporal.io/api. No DTOs, no renamed fields — Temporal's payload
// converter handles proto natively, so a caller in any SDK language
// builds the same message the server itself speaks and gets the
// server's own answer back.
//
//	w := worker.New(c, taskQueue, worker.Options{})
//	schedule.Register(w, schedule.New(c))
//
// Schedules are inherently dashboard-shaped — a cron manager is a list,
// a detail pane, and three buttons — and this is the whole surface
// behind that UI.
//
// Each activity is one RPC. Registered names are the API — the strings
// a caller schedules — and follow `schedule.<RPC name>`:
// schedule.ListSchedules, schedule.PatchSchedule, and so on (see the
// Name constants).
//
// # Registration is the capability grant
//
// Create, Update, Patch and Delete mutate, and the pack's own Register
// is the boundary that grants them. A host that wants a read-only
// schedule view has a choice the type system enforces: don't import
// this module, and DeleteSchedule does not exist on its task queue.
// Confirmation UI belongs to the app; the pack's contribution is that a
// host which never called Register has nothing to confirm.
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
// # request_id and conflict_token are the caller's, deliberately
//
// CreateSchedule, UpdateSchedule and PatchSchedule carry a `request_id`
// for server-side deduplication, and UpdateSchedule additionally
// carries a `conflict_token` for optimistic concurrency. The pack fills
// NEITHER.
//
//   - request_id: AN ACTIVITY CAN BE RETRIED. If each attempt carried a
//     fresh id, a retry after an ambiguous failure would apply the
//     mutation twice. Send one stable across attempts of a single
//     invocation and the server dedupes the retry for you.
//   - conflict_token: it comes from a DescribeScheduleResponse, and
//     round-tripping it is what makes an update fail rather than
//     silently clobber a concurrent edit. Only the caller knows which
//     Describe its edit was based on. Leaving it empty updates
//     unconditionally — the server's documented behavior, reached here
//     exactly as over raw gRPC.
//
// # A note on UpdateSchedule's replace semantics
//
// UpdateSchedule REPLACES the schedule's spec, action, policies and
// state wholesale — it is not a merge, and it is not this pack's job to
// make it one. Read with DescribeSchedule, modify the returned
// Schedule, send it back. PatchSchedule is the surgical alternative
// (pause, unpause, trigger-now, backfill) and the one a dashboard's
// buttons should use.
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
// job, configured exactly once by its retry policy — and an SDK-level
// retry of a delete is a retry nobody configured.
package schedule

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
	NameListSchedules             = "schedule.ListSchedules"
	NameCountSchedules            = "schedule.CountSchedules"
	NameDescribeSchedule          = "schedule.DescribeSchedule"
	NameListScheduleMatchingTimes = "schedule.ListScheduleMatchingTimes"
	NameCreateSchedule            = "schedule.CreateSchedule"
	NameUpdateSchedule            = "schedule.UpdateSchedule"
	NamePatchSchedule             = "schedule.PatchSchedule"
	NameDeleteSchedule            = "schedule.DeleteSchedule"
)

// AllNames lists every activity Register registers, in registration
// order: the four reads first, then the four mutations.
func AllNames() []string {
	return []string{
		NameListSchedules,
		NameCountSchedules,
		NameDescribeSchedule,
		NameListScheduleMatchingTimes,
		NameCreateSchedule,
		NameUpdateSchedule,
		NamePatchSchedule,
		NameDeleteSchedule,
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
// can delete schedules. Before it, it cannot.
func Register(r worker.ActivityRegistry, a *Activities) {
	for _, reg := range []struct {
		name string
		fn   any
	}{
		{NameListSchedules, a.ListSchedules},
		{NameCountSchedules, a.CountSchedules},
		{NameDescribeSchedule, a.DescribeSchedule},
		{NameListScheduleMatchingTimes, a.ListScheduleMatchingTimes},
		{NameCreateSchedule, a.CreateSchedule},
		{NameUpdateSchedule, a.UpdateSchedule},
		{NamePatchSchedule, a.PatchSchedule},
		{NameDeleteSchedule, a.DeleteSchedule},
	} {
		r.RegisterActivityWithOptions(reg.fn, activity.RegisterOptions{Name: reg.name})
	}
}

// ListSchedules is the cron manager's list, paged by next_page_token,
// optionally filtered by a visibility query.
func (a *Activities) ListSchedules(ctx context.Context, req *workflowservice.ListSchedulesRequest) (*workflowservice.ListSchedulesResponse, error) {
	if req == nil {
		req = &workflowservice.ListSchedulesRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().ListSchedules(ctx, req)
}

// CountSchedules counts schedules matching a query — the status-bar
// companion to ListSchedules.
func (a *Activities) CountSchedules(ctx context.Context, req *workflowservice.CountSchedulesRequest) (*workflowservice.CountSchedulesResponse, error) {
	if req == nil {
		req = &workflowservice.CountSchedulesRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().CountSchedules(ctx, req)
}

// DescribeSchedule is the detail pane's RPC: spec, action, policies,
// state, recent and upcoming actions — and the conflict_token an
// UpdateSchedule should carry back.
func (a *Activities) DescribeSchedule(ctx context.Context, req *workflowservice.DescribeScheduleRequest) (*workflowservice.DescribeScheduleResponse, error) {
	if req == nil {
		req = &workflowservice.DescribeScheduleRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().DescribeSchedule(ctx, req)
}

// ListScheduleMatchingTimes answers "when would this spec fire?" over a
// time range, without creating anything — the preview a cron editor
// wants beside the spec field.
func (a *Activities) ListScheduleMatchingTimes(ctx context.Context, req *workflowservice.ListScheduleMatchingTimesRequest) (*workflowservice.ListScheduleMatchingTimesResponse, error) {
	if req == nil {
		req = &workflowservice.ListScheduleMatchingTimesRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().ListScheduleMatchingTimes(ctx, req)
}

// CreateSchedule creates a schedule. See the package note on
// request_id: it is the caller's, and it must be stable across activity
// attempts or a retry creates twice.
func (a *Activities) CreateSchedule(ctx context.Context, req *workflowservice.CreateScheduleRequest) (*workflowservice.CreateScheduleResponse, error) {
	if req == nil {
		req = &workflowservice.CreateScheduleRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().CreateSchedule(ctx, req)
}

// UpdateSchedule REPLACES the schedule's spec, action, policies and
// state — read with DescribeSchedule, modify, send back, and carry that
// Describe's conflict_token to make a concurrent edit fail rather than
// be clobbered.
func (a *Activities) UpdateSchedule(ctx context.Context, req *workflowservice.UpdateScheduleRequest) (*workflowservice.UpdateScheduleResponse, error) {
	if req == nil {
		req = &workflowservice.UpdateScheduleRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().UpdateSchedule(ctx, req)
}

// PatchSchedule is the surgical mutation — pause, unpause, trigger
// immediately, backfill a range — and the one a dashboard's buttons
// should use in preference to a whole-schedule Update.
func (a *Activities) PatchSchedule(ctx context.Context, req *workflowservice.PatchScheduleRequest) (*workflowservice.PatchScheduleResponse, error) {
	if req == nil {
		req = &workflowservice.PatchScheduleRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().PatchSchedule(ctx, req)
}

// DeleteSchedule removes a schedule. Destructive: see the package note
// on Register as the capability boundary.
func (a *Activities) DeleteSchedule(ctx context.Context, req *workflowservice.DeleteScheduleRequest) (*workflowservice.DeleteScheduleResponse, error) {
	if req == nil {
		req = &workflowservice.DeleteScheduleRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().DeleteSchedule(ctx, req)
}
