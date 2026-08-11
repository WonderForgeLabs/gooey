// Package namespace exposes Temporal's namespace and cluster
// introspection as standalone activities, proto-true: every activity's
// input and output ARE the `temporal.api.*` request/response messages,
// taken verbatim from go.temporal.io/api. No DTOs, no renamed fields —
// Temporal's payload converter handles proto natively, so a caller in
// any SDK language builds the same message the server itself speaks and
// gets the server's own answer back.
//
//	w := worker.New(c, taskQueue, worker.Options{})
//	namespace.Register(w, namespace.New(c))
//
// Two jobs, both small: the environment picker at the top of a
// dashboard (which namespaces exist, what is this one configured to
// do), and CAPABILITY DETECTION — GetSystemInfo reports what the server
// supports, which is how a UI decides whether to offer a newer feature
// at all rather than offering it and failing.
//
// Each activity is one RPC. Registered names are the API — the strings
// a caller schedules — and follow `namespace.<RPC name>`:
// namespace.ListNamespaces, namespace.GetSystemInfo, and so on (see the
// Name constants).
//
// # Read-only, on purpose
//
// All four RPCs observe; none change anything. RegisterNamespace,
// UpdateNamespace and DeprecateNamespace exist on WorkflowService and
// are deliberately NOT in this pack: under the
// registration-is-a-capability-grant split, a host that wants an
// environment picker should not thereby gain the ability to deprecate a
// namespace. A namespace-admin surface belongs in its own pack,
// registered deliberately.
//
// # Namespace defaulting, and where it does not apply
//
// The usual rule — a request whose Namespace field is empty (or a nil
// request) is filled with the worker's namespace, WithNamespace or
// Temporal's "default" — applies to DescribeNamespace, the one RPC here
// that names a namespace. It is the pack's ONLY deviation from verbatim
// pass-through, and it only ever fills an empty field. The default is
// written into the request in place, which is safe for activity
// invocations (the request was deserialized fresh for this call) and
// worth knowing for direct Go calls.
//
// ListNamespaces, GetClusterInfo and GetSystemInfo are CLUSTER-scoped:
// their request messages have no namespace field at all, so there is
// nothing to default. A nil request becomes the empty request and
// nothing more.
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
package namespace

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
	NameDescribeNamespace = "namespace.DescribeNamespace"
	NameListNamespaces    = "namespace.ListNamespaces"
	NameGetClusterInfo    = "namespace.GetClusterInfo"
	NameGetSystemInfo     = "namespace.GetSystemInfo"
)

// AllNames lists every activity Register registers, in registration
// order.
func AllNames() []string {
	return []string{
		NameDescribeNamespace,
		NameListNamespaces,
		NameGetClusterInfo,
		NameGetSystemInfo,
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
// dialed with. Only DescribeNamespace has such a field; the other three
// RPCs here are cluster-scoped.
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
		{NameDescribeNamespace, a.DescribeNamespace},
		{NameListNamespaces, a.ListNamespaces},
		{NameGetClusterInfo, a.GetClusterInfo},
		{NameGetSystemInfo, a.GetSystemInfo},
	} {
		r.RegisterActivityWithOptions(reg.fn, activity.RegisterOptions{Name: reg.name})
	}
}

// DescribeNamespace reports one namespace's config, replication state
// and retention. The only namespaced RPC in this pack, and so the only
// one namespace defaulting touches.
func (a *Activities) DescribeNamespace(ctx context.Context, req *workflowservice.DescribeNamespaceRequest) (*workflowservice.DescribeNamespaceResponse, error) {
	if req == nil {
		req = &workflowservice.DescribeNamespaceRequest{}
	}
	if req.Namespace == "" {
		req.Namespace = a.namespace
	}
	return a.client.WorkflowService().DescribeNamespace(ctx, req)
}

// ListNamespaces is the environment picker's list, paged by
// next_page_token. Cluster-scoped: its request has no namespace field,
// so no defaulting applies.
func (a *Activities) ListNamespaces(ctx context.Context, req *workflowservice.ListNamespacesRequest) (*workflowservice.ListNamespacesResponse, error) {
	if req == nil {
		req = &workflowservice.ListNamespacesRequest{}
	}
	return a.client.WorkflowService().ListNamespaces(ctx, req)
}

// GetClusterInfo reports cluster identity and the supported client
// version range. Cluster-scoped: no namespace field, no defaulting.
func (a *Activities) GetClusterInfo(ctx context.Context, req *workflowservice.GetClusterInfoRequest) (*workflowservice.GetClusterInfoResponse, error) {
	if req == nil {
		req = &workflowservice.GetClusterInfoRequest{}
	}
	return a.client.WorkflowService().GetClusterInfo(ctx, req)
}

// GetSystemInfo reports the server's capabilities — the RPC a UI should
// consult before offering a newer feature. Its Capabilities message is
// how a dashboard decides whether workflow update, eager start, and
// friends are available on THIS server rather than discovering it from
// a failure. Cluster-scoped: no namespace field, no defaulting.
func (a *Activities) GetSystemInfo(ctx context.Context, req *workflowservice.GetSystemInfoRequest) (*workflowservice.GetSystemInfoResponse, error) {
	if req == nil {
		req = &workflowservice.GetSystemInfoRequest{}
	}
	return a.client.WorkflowService().GetSystemInfo(ctx, req)
}
