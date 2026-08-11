package namespace

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
)

// The fake embeds the real interface and overrides only what the pack
// calls: a method the pack should never touch panics on the nil
// embedded interface, which is exactly the assertion we want. `called`
// records the RPC name per call, so a mis-wired activity fails rather
// than passing silently.

type fakeWorkflowService struct {
	workflowservice.WorkflowServiceClient

	called []string

	gotDescribe   *workflowservice.DescribeNamespaceRequest
	gotList       *workflowservice.ListNamespacesRequest
	gotCluster    *workflowservice.GetClusterInfoRequest
	gotSystemInfo *workflowservice.GetSystemInfoRequest

	systemInfoResp *workflowservice.GetSystemInfoResponse
	err            error
}

func (f *fakeWorkflowService) DescribeNamespace(ctx context.Context, req *workflowservice.DescribeNamespaceRequest, _ ...grpc.CallOption) (*workflowservice.DescribeNamespaceResponse, error) {
	f.called = append(f.called, "DescribeNamespace")
	f.gotDescribe = req
	return &workflowservice.DescribeNamespaceResponse{}, f.err
}

func (f *fakeWorkflowService) ListNamespaces(ctx context.Context, req *workflowservice.ListNamespacesRequest, _ ...grpc.CallOption) (*workflowservice.ListNamespacesResponse, error) {
	f.called = append(f.called, "ListNamespaces")
	f.gotList = req
	return &workflowservice.ListNamespacesResponse{}, f.err
}

func (f *fakeWorkflowService) GetClusterInfo(ctx context.Context, req *workflowservice.GetClusterInfoRequest, _ ...grpc.CallOption) (*workflowservice.GetClusterInfoResponse, error) {
	f.called = append(f.called, "GetClusterInfo")
	f.gotCluster = req
	return &workflowservice.GetClusterInfoResponse{ClusterName: "active"}, f.err
}

func (f *fakeWorkflowService) GetSystemInfo(ctx context.Context, req *workflowservice.GetSystemInfoRequest, _ ...grpc.CallOption) (*workflowservice.GetSystemInfoResponse, error) {
	f.called = append(f.called, "GetSystemInfo")
	f.gotSystemInfo = req
	if f.err != nil {
		return nil, f.err
	}
	if f.systemInfoResp != nil {
		return f.systemInfoResp, nil
	}
	return &workflowservice.GetSystemInfoResponse{}, nil
}

type fakeClient struct {
	client.Client
	wfs *fakeWorkflowService
}

func (f *fakeClient) WorkflowService() workflowservice.WorkflowServiceClient { return f.wfs }

func harness() (*Activities, *fakeWorkflowService) {
	wfs := &fakeWorkflowService{}
	return New(&fakeClient{wfs: wfs}), wfs
}

func callAllNil(t *testing.T, a *Activities) {
	t.Helper()
	ctx := context.Background()
	if _, err := a.DescribeNamespace(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListNamespaces(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetClusterInfo(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetSystemInfo(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

// DescribeNamespace is the ONLY namespaced RPC here, so it is the only
// one defaulting touches. The other three are cluster-scoped: their
// request messages have no namespace field at all, and a nil request
// becomes the empty request and nothing more.
func TestOnlyDescribeIsNamespaced(t *testing.T) {
	a, wfs := harness()
	callAllNil(t, a)

	if wfs.gotDescribe.Namespace != DefaultNamespace {
		t.Errorf("DescribeNamespace: namespace = %q, want %q", wfs.gotDescribe.Namespace, DefaultNamespace)
	}
	for name, got := range map[string]any{
		"ListNamespaces": wfs.gotList,
		"GetClusterInfo": wfs.gotCluster,
		"GetSystemInfo":  wfs.gotSystemInfo,
	} {
		if got == nil {
			t.Errorf("%s: the RPC never ran", name)
		}
	}
}

// Each activity calls its OWN RPC.
func TestEachActivityCallsItsOwnRPC(t *testing.T) {
	a, wfs := harness()
	callAllNil(t, a)

	want := []string{"DescribeNamespace", "ListNamespaces", "GetClusterInfo", "GetSystemInfo"}
	if !reflect.DeepEqual(wfs.called, want) {
		t.Fatalf("RPCs called %v,\nwant %v", wfs.called, want)
	}
}

func TestWithNamespaceSetsTheDefault(t *testing.T) {
	wfs := &fakeWorkflowService{}
	a := New(&fakeClient{wfs: wfs}, WithNamespace("ops"))
	if _, err := a.DescribeNamespace(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotDescribe.Namespace != "ops" {
		t.Fatalf("namespace = %q, want the configured %q", wfs.gotDescribe.Namespace, "ops")
	}
}

// A request that names its namespace is the caller's business. Only
// DescribeNamespace has the field, so the table has one row — but it is
// written as a table, and length-checked against the pack's namespaced
// surface, so adding a namespaced RPC without covering it fails here.
func TestExplicitNamespacePassesThrough(t *testing.T) {
	const elsewhere = "elsewhere"

	for _, tc := range []struct {
		name string
		call func(context.Context, *Activities) error
		got  func(*fakeWorkflowService) string
	}{
		{
			"DescribeNamespace",
			func(ctx context.Context, a *Activities) error {
				_, err := a.DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotDescribe.Namespace },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, wfs := harness()
			// The worker's namespace differs from the caller's, so a
			// test passing on "both happen to be default" is impossible.
			a.namespace = "the-workers-namespace"
			if err := tc.call(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			if got := tc.got(wfs); got != elsewhere {
				t.Fatalf("namespace = %q, want the caller's %q", got, elsewhere)
			}
		})
	}

	// One of the pack's four activities is namespaced. If that changes,
	// this fails until the table above does too.
	if want := len(AllNames()); want != 4 {
		t.Fatalf("the pack has %d activities; re-check which of them are namespaced", want)
	}
}

// Proto fidelity, mechanically: the request reaches the RPC verbatim
// (same pointer) and the response comes back verbatim (same pointer).
// The pack adds nothing and hides nothing.
func TestRequestAndResponsePassThroughVerbatim(t *testing.T) {
	a, wfs := harness()
	wfs.systemInfoResp = &workflowservice.GetSystemInfoResponse{
		ServerVersion: "1.31.0",
		Capabilities: &workflowservice.GetSystemInfoResponse_Capabilities{
			SupportsSchedules:  true,
			EagerWorkflowStart: true,
		},
	}
	req := &workflowservice.GetSystemInfoRequest{}
	resp, err := a.GetSystemInfo(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if wfs.gotSystemInfo != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if resp != wfs.systemInfoResp {
		t.Fatal("the caller did not receive the RPC's response message")
	}
	// Capability detection is this pack's second job: the server's own
	// Capabilities message must reach the caller unflattened, because a
	// UI gates newer features on it.
	if resp.ServerVersion != "1.31.0" ||
		!resp.Capabilities.GetSupportsSchedules() ||
		!resp.Capabilities.GetEagerWorkflowStart() {
		t.Fatalf("capabilities = %+v, want the server's answer untouched", resp.Capabilities)
	}
}

// Pagination is the proto's: next_page_token crosses verbatim.
func TestListPagesWithTheServersToken(t *testing.T) {
	a, wfs := harness()
	req := &workflowservice.ListNamespacesRequest{
		PageSize:      25,
		NextPageToken: []byte("prior-token"),
	}
	if _, err := a.ListNamespaces(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if wfs.gotList != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if string(wfs.gotList.NextPageToken) != "prior-token" || wfs.gotList.PageSize != 25 {
		t.Fatalf("request fields were not passed verbatim: %+v", wfs.gotList)
	}
}

// RPC errors are the activity's errors — Temporal's retry machinery is
// the layer that deals with them, not the pack.
func TestErrorsPropagate(t *testing.T) {
	a, wfs := harness()
	boom := errors.New("namespace not found")
	wfs.err = boom

	if _, err := a.DescribeNamespace(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("DescribeNamespace err = %v, want the RPC's", err)
	}
	if _, err := a.GetSystemInfo(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("GetSystemInfo err = %v, want the RPC's", err)
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

// This pack is READ-ONLY by design: registering an environment picker
// must not grant the ability to reconfigure or deprecate a namespace.
// RegisterNamespace, UpdateNamespace and DeprecateNamespace exist on
// WorkflowService and belong to a different capability grant, so the
// inventory is asserted exactly — a mutating RPC added here would be a
// silent privilege escalation for every host that already registered
// the picker.
func TestInventoryIsExactlyTheReadOnlySurface(t *testing.T) {
	want := map[string]bool{
		"namespace.DescribeNamespace": true,
		"namespace.ListNamespaces":    true,
		"namespace.GetClusterInfo":    true,
		"namespace.GetSystemInfo":     true,
	}
	got := AllNames()
	if len(got) != len(want) {
		t.Fatalf("AllNames has %d entries, want %d: %v", len(got), len(want), got)
	}
	for _, n := range got {
		if !strings.HasPrefix(n, "namespace.") {
			t.Errorf("name %q is not scoped to the pack", n)
		}
		if !want[n] {
			t.Errorf("unexpected registered name %q — this pack must stay read-only", n)
		}
		delete(want, n)
	}
	for n := range want {
		t.Errorf("missing registered name %q", n)
	}
}
