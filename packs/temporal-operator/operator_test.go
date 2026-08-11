package operator

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
)

// The fake embeds the real interface and overrides only what the pack
// calls: a method the pack should never touch panics on the nil
// embedded interface, which is exactly the assertion we want. `called`
// records the RPC name per call, so a mis-wired activity — remove
// reaching add, the one slip that would be genuinely dangerous here —
// fails rather than passing silently.

type fakeOperatorService struct {
	operatorservice.OperatorServiceClient

	called []string

	gotList   *operatorservice.ListSearchAttributesRequest
	gotAdd    *operatorservice.AddSearchAttributesRequest
	gotRemove *operatorservice.RemoveSearchAttributesRequest

	listResp *operatorservice.ListSearchAttributesResponse
	err      error
}

func (f *fakeOperatorService) ListSearchAttributes(ctx context.Context, req *operatorservice.ListSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.ListSearchAttributesResponse, error) {
	f.called = append(f.called, "ListSearchAttributes")
	f.gotList = req
	if f.err != nil {
		return nil, f.err
	}
	if f.listResp != nil {
		return f.listResp, nil
	}
	return &operatorservice.ListSearchAttributesResponse{}, nil
}

func (f *fakeOperatorService) AddSearchAttributes(ctx context.Context, req *operatorservice.AddSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.AddSearchAttributesResponse, error) {
	f.called = append(f.called, "AddSearchAttributes")
	f.gotAdd = req
	return &operatorservice.AddSearchAttributesResponse{}, f.err
}

func (f *fakeOperatorService) RemoveSearchAttributes(ctx context.Context, req *operatorservice.RemoveSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.RemoveSearchAttributesResponse, error) {
	f.called = append(f.called, "RemoveSearchAttributes")
	f.gotRemove = req
	return &operatorservice.RemoveSearchAttributesResponse{}, f.err
}

// The client fake overrides OperatorService only. WorkflowService is
// left on the nil embedded interface deliberately: this pack must never
// touch it, and a call would panic rather than pass.
type fakeClient struct {
	client.Client
	ops *fakeOperatorService
}

func (f *fakeClient) OperatorService() operatorservice.OperatorServiceClient { return f.ops }

func harness() (*Activities, *fakeOperatorService) {
	ops := &fakeOperatorService{}
	return New(&fakeClient{ops: ops}), ops
}

func callAllNil(t *testing.T, a *Activities) {
	t.Helper()
	ctx := context.Background()
	if _, err := a.ListSearchAttributes(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddSearchAttributes(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoveSearchAttributes(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

// A nil or namespace-less request gets the worker's namespace — every
// RPC here is namespaced, so the rule has no exceptions.
func TestNilRequestsGetTheWorkerNamespace(t *testing.T) {
	a, ops := harness()
	callAllNil(t, a)

	for name, got := range map[string]string{
		"ListSearchAttributes":   ops.gotList.Namespace,
		"AddSearchAttributes":    ops.gotAdd.Namespace,
		"RemoveSearchAttributes": ops.gotRemove.Namespace,
	} {
		if got != DefaultNamespace {
			t.Errorf("%s: namespace = %q, want %q", name, got, DefaultNamespace)
		}
	}
}

// Each activity calls its OWN RPC. Here that matters more than usual:
// add and remove take near-identical requests, and a slip between them
// would destroy schema rather than create it.
func TestEachActivityCallsItsOwnRPC(t *testing.T) {
	a, ops := harness()
	callAllNil(t, a)

	want := []string{"ListSearchAttributes", "AddSearchAttributes", "RemoveSearchAttributes"}
	if !reflect.DeepEqual(ops.called, want) {
		t.Fatalf("RPCs called %v,\nwant %v", ops.called, want)
	}
}

// Everything in this pack goes through OperatorService, never
// WorkflowService. The client fake leaves WorkflowService on the nil
// embedded interface, so a stray call panics — this test is what makes
// that guarantee observable rather than incidental.
func TestEverythingGoesThroughTheOperatorService(t *testing.T) {
	a, ops := harness()
	callAllNil(t, a)
	if len(ops.called) != len(AllNames()) {
		t.Fatalf("OperatorService saw %d calls, want all %d activities", len(ops.called), len(AllNames()))
	}
}

func TestWithNamespaceSetsTheDefault(t *testing.T) {
	ops := &fakeOperatorService{}
	a := New(&fakeClient{ops: ops}, WithNamespace("ops-ns"))
	if _, err := a.RemoveSearchAttributes(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if ops.gotRemove.Namespace != "ops-ns" {
		t.Fatalf("namespace = %q, want the configured %q", ops.gotRemove.Namespace, "ops-ns")
	}
}

// A request that names its namespace is the caller's business — for
// EVERY activity, not just one. Each repeats the same
// `if req.Namespace == ""` guard independently, so an accidental
// unconditional overwrite in any one of them would pass both
// TestNilRequestsGetTheWorkerNamespace (which only exercises the empty
// case) and TestEachActivityCallsItsOwnRPC (which checks routing, not
// fidelity). On an admin pack this is a safety property: a mutation
// must land in the namespace the caller named, never in the worker's.
func TestExplicitNamespacePassesThrough(t *testing.T) {
	const elsewhere = "elsewhere"

	for _, tc := range []struct {
		name string
		call func(context.Context, *Activities) error
		got  func(*fakeOperatorService) string
	}{
		{
			"ListSearchAttributes",
			func(ctx context.Context, a *Activities) error {
				_, err := a.ListSearchAttributes(ctx, &operatorservice.ListSearchAttributesRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeOperatorService) string { return f.gotList.Namespace },
		},
		{
			"AddSearchAttributes",
			func(ctx context.Context, a *Activities) error {
				_, err := a.AddSearchAttributes(ctx, &operatorservice.AddSearchAttributesRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeOperatorService) string { return f.gotAdd.Namespace },
		},
		{
			"RemoveSearchAttributes",
			func(ctx context.Context, a *Activities) error {
				_, err := a.RemoveSearchAttributes(ctx, &operatorservice.RemoveSearchAttributesRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeOperatorService) string { return f.gotRemove.Namespace },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, ops := harness()
			// The worker's namespace differs from the caller's, so a
			// test passing on "both happen to be default" is impossible.
			a.namespace = "the-workers-namespace"
			if err := tc.call(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			if got := tc.got(ops); got != elsewhere {
				t.Fatalf("namespace = %q, want the caller's %q", got, elsewhere)
			}
		})
	}

	// The table must cover every activity in the pack — if AllNames
	// grows, this fails until the table does too.
	if want := len(AllNames()); want != 3 {
		t.Fatalf("the pack has %d activities but this table covers 3", want)
	}
}

// Proto fidelity, mechanically: the request reaches the RPC verbatim
// (same pointer, the attribute map untouched) and the response comes
// back verbatim (same pointer).
func TestAddPassesTheAttributeMapVerbatim(t *testing.T) {
	a, ops := harness()
	req := &operatorservice.AddSearchAttributesRequest{
		SearchAttributes: map[string]enums.IndexedValueType{
			"OrderTotal": enums.INDEXED_VALUE_TYPE_DOUBLE,
			"Tenant":     enums.INDEXED_VALUE_TYPE_KEYWORD,
		},
	}
	if _, err := a.AddSearchAttributes(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if ops.gotAdd != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if len(ops.gotAdd.SearchAttributes) != 2 ||
		ops.gotAdd.SearchAttributes["OrderTotal"] != enums.INDEXED_VALUE_TYPE_DOUBLE ||
		ops.gotAdd.SearchAttributes["Tenant"] != enums.INDEXED_VALUE_TYPE_KEYWORD {
		t.Fatalf("search attributes were not passed verbatim: %+v", ops.gotAdd.SearchAttributes)
	}
	if ops.gotAdd.Namespace != DefaultNamespace {
		t.Fatalf("namespace = %q, want defaulted", ops.gotAdd.Namespace)
	}
}

// Remove takes a list of names, and the pack neither filters nor
// reorders it. A pack that "helpfully" dropped a name would be a
// silent partial delete.
func TestRemovePassesTheNameListVerbatim(t *testing.T) {
	a, ops := harness()
	req := &operatorservice.RemoveSearchAttributesRequest{
		SearchAttributes: []string{"OrderTotal", "Tenant"},
	}
	if _, err := a.RemoveSearchAttributes(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if ops.gotRemove != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if !reflect.DeepEqual(ops.gotRemove.SearchAttributes, []string{"OrderTotal", "Tenant"}) {
		t.Fatalf("search attributes = %v, want the caller's list verbatim", ops.gotRemove.SearchAttributes)
	}
}

func TestListReturnsTheServersResponse(t *testing.T) {
	a, ops := harness()
	ops.listResp = &operatorservice.ListSearchAttributesResponse{
		CustomAttributes: map[string]enums.IndexedValueType{
			"OrderTotal": enums.INDEXED_VALUE_TYPE_DOUBLE,
		},
	}
	resp, err := a.ListSearchAttributes(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp != ops.listResp {
		t.Fatal("the caller did not receive the RPC's response message")
	}
	if resp.CustomAttributes["OrderTotal"] != enums.INDEXED_VALUE_TYPE_DOUBLE {
		t.Fatalf("custom attributes = %+v, want the server's answer untouched", resp.CustomAttributes)
	}
}

// RPC errors are the activity's errors — Temporal's retry machinery is
// the layer that deals with them, not the pack.
func TestErrorsPropagate(t *testing.T) {
	a, ops := harness()
	boom := errors.New("search attribute already exists")
	ops.err = boom

	if _, err := a.AddSearchAttributes(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("AddSearchAttributes err = %v, want the RPC's", err)
	}
	if _, err := a.RemoveSearchAttributes(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("RemoveSearchAttributes err = %v, want the RPC's", err)
	}
}

type fakeRegistry struct {
	names []string
}

func (r *fakeRegistry) RegisterActivity(a any) {
	panic("the pack must register with explicit names, never RegisterActivity")
}
func (r *fakeRegistry) RegisterActivityWithOptions(a any, options activity.RegisterOptions) {
	if a == nil || reflect.ValueOf(a).IsNil() {
		panic("registered a nil activity func")
	}
	r.names = append(r.names, options.Name)
}
func (r *fakeRegistry) RegisterDynamicActivity(a any, options activity.DynamicRegisterOptions) {
	panic("the pack registers concrete activities only")
}

// The registered names ARE the API: what a caller in any language
// schedules. AllNames is the published inventory, and Register must
// match it exactly, in order.
func TestRegisterUsesTheCanonicalNames(t *testing.T) {
	r := &fakeRegistry{}
	a, _ := harness()
	Register(r, a)
	if !reflect.DeepEqual(r.names, AllNames()) {
		t.Fatalf("registered %v,\nwant %v", r.names, AllNames())
	}
}

// Registration is the capability grant, so the inventory is a security
// statement: exactly these three names, every one `operator.`-prefixed.
// The prefix is also what lets this pack and packs/temporal-visibility
// both register ListSearchAttributes on one worker — the RPC is shared,
// the registered names are not, so there is no collision.
func TestInventoryIsExactlyTheSearchAttributeSurface(t *testing.T) {
	want := map[string]bool{
		"operator.ListSearchAttributes":   true,
		"operator.AddSearchAttributes":    true,
		"operator.RemoveSearchAttributes": true,
	}
	got := AllNames()
	if len(got) != len(want) {
		t.Fatalf("AllNames has %d entries, want %d: %v", len(got), len(want), got)
	}
	for _, n := range got {
		if !strings.HasPrefix(n, "operator.") {
			t.Errorf("name %q is not scoped to the pack — it could collide with another pack's registration", n)
		}
		if !want[n] {
			t.Errorf("unexpected registered name %q", n)
		}
		delete(want, n)
	}
	for n := range want {
		t.Errorf("missing registered name %q", n)
	}
}
