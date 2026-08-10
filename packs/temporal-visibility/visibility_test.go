package visibility

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.temporal.io/api/common/v1"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
)

// The fakes embed the real interfaces and override only what the pack
// calls: a method the pack should never touch panics on the nil
// embedded interface, which is exactly the assertion we want.

type fakeWorkflowService struct {
	workflowservice.WorkflowServiceClient
	gotList     *workflowservice.ListWorkflowExecutionsRequest
	gotOpen     *workflowservice.ListOpenWorkflowExecutionsRequest
	gotClosed   *workflowservice.ListClosedWorkflowExecutionsRequest
	gotCount    *workflowservice.CountWorkflowExecutionsRequest
	gotGetAttrs *workflowservice.GetSearchAttributesRequest
	gotDescribe *workflowservice.DescribeWorkflowExecutionRequest

	listResp *workflowservice.ListWorkflowExecutionsResponse
	err      error
}

func (f *fakeWorkflowService) ListWorkflowExecutions(ctx context.Context, req *workflowservice.ListWorkflowExecutionsRequest, _ ...grpc.CallOption) (*workflowservice.ListWorkflowExecutionsResponse, error) {
	f.gotList = req
	if f.err != nil {
		return nil, f.err
	}
	if f.listResp != nil {
		return f.listResp, nil
	}
	return &workflowservice.ListWorkflowExecutionsResponse{}, nil
}

func (f *fakeWorkflowService) ListOpenWorkflowExecutions(ctx context.Context, req *workflowservice.ListOpenWorkflowExecutionsRequest, _ ...grpc.CallOption) (*workflowservice.ListOpenWorkflowExecutionsResponse, error) {
	f.gotOpen = req
	return &workflowservice.ListOpenWorkflowExecutionsResponse{}, f.err
}

func (f *fakeWorkflowService) ListClosedWorkflowExecutions(ctx context.Context, req *workflowservice.ListClosedWorkflowExecutionsRequest, _ ...grpc.CallOption) (*workflowservice.ListClosedWorkflowExecutionsResponse, error) {
	f.gotClosed = req
	return &workflowservice.ListClosedWorkflowExecutionsResponse{}, f.err
}

func (f *fakeWorkflowService) CountWorkflowExecutions(ctx context.Context, req *workflowservice.CountWorkflowExecutionsRequest, _ ...grpc.CallOption) (*workflowservice.CountWorkflowExecutionsResponse, error) {
	f.gotCount = req
	return &workflowservice.CountWorkflowExecutionsResponse{Count: 42}, f.err
}

func (f *fakeWorkflowService) GetSearchAttributes(ctx context.Context, req *workflowservice.GetSearchAttributesRequest, _ ...grpc.CallOption) (*workflowservice.GetSearchAttributesResponse, error) {
	f.gotGetAttrs = req
	return &workflowservice.GetSearchAttributesResponse{}, f.err
}

func (f *fakeWorkflowService) DescribeWorkflowExecution(ctx context.Context, req *workflowservice.DescribeWorkflowExecutionRequest, _ ...grpc.CallOption) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	f.gotDescribe = req
	return &workflowservice.DescribeWorkflowExecutionResponse{}, f.err
}

type fakeOperatorService struct {
	operatorservice.OperatorServiceClient
	gotListAttrs *operatorservice.ListSearchAttributesRequest
	err          error
}

func (f *fakeOperatorService) ListSearchAttributes(ctx context.Context, req *operatorservice.ListSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.ListSearchAttributesResponse, error) {
	f.gotListAttrs = req
	return &operatorservice.ListSearchAttributesResponse{}, f.err
}

type fakeClient struct {
	client.Client
	wfs *fakeWorkflowService
	ops *fakeOperatorService
}

func (f *fakeClient) WorkflowService() workflowservice.WorkflowServiceClient {
	return f.wfs
}
func (f *fakeClient) OperatorService() operatorservice.OperatorServiceClient {
	return f.ops
}

func harness() (*Activities, *fakeWorkflowService, *fakeOperatorService) {
	wfs := &fakeWorkflowService{}
	ops := &fakeOperatorService{}
	return New(&fakeClient{wfs: wfs, ops: ops}), wfs, ops
}

// A nil or namespace-less request gets the worker's namespace — every
// namespaced RPC, one rule.
func TestNilRequestsGetTheWorkerNamespace(t *testing.T) {
	a, wfs, ops := harness()
	ctx := context.Background()

	if _, err := a.ListWorkflowExecutions(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListOpenWorkflowExecutions(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListClosedWorkflowExecutions(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CountWorkflowExecutions(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DescribeWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListSearchAttributes(ctx, nil); err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string]string{
		"ListWorkflowExecutions":       wfs.gotList.Namespace,
		"ListOpenWorkflowExecutions":   wfs.gotOpen.Namespace,
		"ListClosedWorkflowExecutions": wfs.gotClosed.Namespace,
		"CountWorkflowExecutions":      wfs.gotCount.Namespace,
		"DescribeWorkflowExecution":    wfs.gotDescribe.Namespace,
		"ListSearchAttributes":         ops.gotListAttrs.Namespace,
	} {
		if got != DefaultNamespace {
			t.Errorf("%s: namespace = %q, want %q", name, got, DefaultNamespace)
		}
	}
}

func TestWithNamespaceSetsTheDefault(t *testing.T) {
	wfs := &fakeWorkflowService{}
	a := New(&fakeClient{wfs: wfs}, WithNamespace("ops"))
	if _, err := a.CountWorkflowExecutions(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotCount.Namespace != "ops" {
		t.Fatalf("namespace = %q, want the configured %q", wfs.gotCount.Namespace, "ops")
	}
}

// A request that names its namespace is the caller's business.
func TestExplicitNamespacePassesThrough(t *testing.T) {
	a, wfs, _ := harness()
	req := &workflowservice.ListWorkflowExecutionsRequest{Namespace: "elsewhere"}
	if _, err := a.ListWorkflowExecutions(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if wfs.gotList.Namespace != "elsewhere" {
		t.Fatalf("namespace = %q, want the caller's %q", wfs.gotList.Namespace, "elsewhere")
	}
}

// Proto fidelity, mechanically: the request reaches the RPC verbatim
// (same pointer, page token untouched) and the response comes back
// verbatim (same pointer). The pack adds nothing and hides nothing.
func TestRequestAndResponsePassThroughVerbatim(t *testing.T) {
	a, wfs, _ := harness()
	wfs.listResp = &workflowservice.ListWorkflowExecutionsResponse{
		NextPageToken: []byte("opaque-server-state"),
	}
	req := &workflowservice.ListWorkflowExecutionsRequest{
		Namespace:     "default",
		Query:         `WorkflowType = "Order" AND ExecutionStatus = "Running"`,
		PageSize:      25,
		NextPageToken: []byte("prior-token"),
	}
	resp, err := a.ListWorkflowExecutions(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if wfs.gotList != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if string(wfs.gotList.NextPageToken) != "prior-token" || wfs.gotList.Query != req.Query || wfs.gotList.PageSize != 25 {
		t.Fatalf("request fields were not passed verbatim: %+v", wfs.gotList)
	}
	if resp != wfs.listResp {
		t.Fatal("the caller did not receive the RPC's response message")
	}
	if string(resp.NextPageToken) != "opaque-server-state" {
		t.Fatalf("next_page_token = %q, want verbatim pass-through", resp.NextPageToken)
	}
}

func TestDescribeCarriesTheExecution(t *testing.T) {
	a, wfs, _ := harness()
	req := &workflowservice.DescribeWorkflowExecutionRequest{
		Execution: &common.WorkflowExecution{WorkflowId: "order-7", RunId: "run-1"},
	}
	if _, err := a.DescribeWorkflowExecution(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if wfs.gotDescribe.Execution.GetWorkflowId() != "order-7" || wfs.gotDescribe.Execution.GetRunId() != "run-1" {
		t.Fatalf("execution = %+v, want the caller's", wfs.gotDescribe.Execution)
	}
	if wfs.gotDescribe.Namespace != DefaultNamespace {
		t.Fatalf("namespace = %q, want defaulted", wfs.gotDescribe.Namespace)
	}
}

// GetSearchAttributes is the legacy cluster-scoped RPC: no namespace
// field exists, so the only defaulting is nil → empty request.
func TestGetSearchAttributesToleratesNil(t *testing.T) {
	a, wfs, _ := harness()
	if _, err := a.GetSearchAttributes(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotGetAttrs == nil {
		t.Fatal("the RPC never ran")
	}
}

// RPC errors are the activity's errors — Temporal's retry machinery is
// the layer that deals with them, not the pack.
func TestErrorsPropagate(t *testing.T) {
	a, wfs, ops := harness()
	boom := errors.New("visibility store unavailable")
	wfs.err, ops.err = boom, boom

	if _, err := a.ListWorkflowExecutions(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("ListWorkflowExecutions err = %v, want the RPC's", err)
	}
	if _, err := a.ListSearchAttributes(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("ListSearchAttributes err = %v, want the RPC's", err)
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
	a, _, _ := harness()
	Register(r, a)
	if !reflect.DeepEqual(r.names, AllNames()) {
		t.Fatalf("registered %v,\nwant %v", r.names, AllNames())
	}
}
