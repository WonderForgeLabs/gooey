// Package operator exposes Temporal's search-attribute administration
// as standalone activities, proto-true: every activity's input and
// output ARE the `temporal.api.*` request/response messages, taken
// verbatim from go.temporal.io/api. No DTOs, no renamed fields —
// Temporal's payload converter handles proto natively, so a caller in
// any SDK language builds the same message the server itself speaks and
// gets the server's own answer back.
//
//	w := worker.New(c, taskQueue, worker.Options{})
//	operator.Register(w, operator.New(c))
//
// Search attributes are the schema behind every visibility query. This
// pack is how that schema is CHANGED; packs/temporal-visibility is how
// it is read by a dashboard.
//
// Each activity is one RPC on OperatorService. Registered names are the
// API — the strings a caller schedules — and follow `operator.<RPC
// name>`: operator.AddSearchAttributes and so on (see the Name
// constants).
//
// # Admin, deliberately separate
//
// This pack exists as its own module because registration is the
// capability grant. packs/temporal-visibility already exposes
// ListSearchAttributes for reading; a dashboard that reads the schema
// should not thereby be able to remove a field from it. Splitting the
// mutators out is what makes that structural rather than aspirational —
// a host that never imported this module cannot call
// RemoveSearchAttributes, because the activity does not exist on its
// task queue.
//
// # On RemoveSearchAttributes
//
// Removing a search attribute is the most consequential act in this
// pack: existing workflows' values for it stop being queryable, and on
// some persistence backends the operation is not reversible by simply
// re-adding the name. Temporal's own documentation is the authority;
// the pack neither softens the call nor adds a confirmation of its own.
// Confirmation UI belongs to the app. The pack's contribution is that a
// host which never called Register has nothing to confirm.
//
// # ListSearchAttributes appears in two packs, on purpose
//
// operator.ListSearchAttributes and visibility.ListSearchAttributes are
// the same RPC under two registered names, and that is not an accident
// or a conflict — the names differ, so both can be registered on one
// worker without collision. An admin tool that never registers the
// visibility pack still needs to read back what it just wrote, and a
// read-only dashboard still needs the schema without gaining the
// mutators. Each pack is independently complete for its own job.
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
// These are unary calls: no heartbeats are recorded. The activity
// context flows straight into the RPC, so the invoker's
// StartToCloseTimeout (or a cancellation) bounds the call. Size
// AddSearchAttributes generously — on some clusters it does real index
// work rather than a metadata write.
//
// Calls go through the SDK client's raw operator service client
// (client.OperatorService — the same shared gRPC connection as
// WorkflowService), which skips the SDK's automatic request retries.
// That is deliberate: the caller invoked an activity, and retrying is
// the activity machinery's job, configured exactly once by its retry
// policy — and an SDK-level retry of a schema change is a retry nobody
// configured.
package operator

import (
	"context"

	"go.temporal.io/api/operatorservice/v1"
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
	NameListSearchAttributes   = "operator.ListSearchAttributes"
	NameAddSearchAttributes    = "operator.AddSearchAttributes"
	NameRemoveSearchAttributes = "operator.RemoveSearchAttributes"
)

// AllNames lists every activity Register registers, in registration
// order: the read first, then the two mutations.
func AllNames() []string {
	return []string{
		NameListSearchAttributes,
		NameAddSearchAttributes,
		NameRemoveSearchAttributes,
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
// can add and remove search attributes. Before it, it cannot.
func Register(r worker.ActivityRegistry, a *Activities) {
	for _, reg := range []struct {
		name string
		fn   any
	}{
		{NameListSearchAttributes, a.ListSearchAttributes},
		{NameAddSearchAttributes, a.AddSearchAttributes},
		{NameRemoveSearchAttributes, a.RemoveSearchAttributes},
	} {
		r.RegisterActivityWithOptions(reg.fn, activity.RegisterOptions{Name: reg.name})
	}
}

// ListSearchAttributes reads the namespace's search-attribute schema —
// custom, system, and the storage mapping. Registered here as well as
// in packs/temporal-visibility, under a distinct name; see the package
// note.
func (a *Activities) ListSearchAttributes(ctx context.Context, req *operatorservice.ListSearchAttributesRequest) (*operatorservice.ListSearchAttributesResponse, error) {
	if req == nil {
		req = &operatorservice.ListSearchAttributesRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.OperatorService().ListSearchAttributes(ctx, req)
}

// AddSearchAttributes adds custom search attributes, as a map of name to
// IndexedValueType. On some clusters this does real index work rather
// than a metadata write, so size the invocation's StartToCloseTimeout
// with that in mind.
func (a *Activities) AddSearchAttributes(ctx context.Context, req *operatorservice.AddSearchAttributesRequest) (*operatorservice.AddSearchAttributesResponse, error) {
	if req == nil {
		req = &operatorservice.AddSearchAttributesRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.OperatorService().AddSearchAttributes(ctx, req)
}

// RemoveSearchAttributes removes custom search attributes by name.
// Consequential: existing workflows' values for the removed name stop
// being queryable. See the package note.
func (a *Activities) RemoveSearchAttributes(ctx context.Context, req *operatorservice.RemoveSearchAttributesRequest) (*operatorservice.RemoveSearchAttributesResponse, error) {
	if req == nil {
		req = &operatorservice.RemoveSearchAttributesRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.OperatorService().RemoveSearchAttributes(ctx, req)
}
