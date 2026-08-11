package batch

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	batchpb "go.temporal.io/api/batch/v1"
	"go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
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

	gotStart    *workflowservice.StartBatchOperationRequest
	gotStop     *workflowservice.StopBatchOperationRequest
	gotDescribe *workflowservice.DescribeBatchOperationRequest
	gotList     *workflowservice.ListBatchOperationsRequest

	describeResp *workflowservice.DescribeBatchOperationResponse
	err          error
}

func (f *fakeWorkflowService) StartBatchOperation(ctx context.Context, req *workflowservice.StartBatchOperationRequest, _ ...grpc.CallOption) (*workflowservice.StartBatchOperationResponse, error) {
	f.called = append(f.called, "StartBatchOperation")
	f.gotStart = req
	return &workflowservice.StartBatchOperationResponse{}, f.err
}

func (f *fakeWorkflowService) StopBatchOperation(ctx context.Context, req *workflowservice.StopBatchOperationRequest, _ ...grpc.CallOption) (*workflowservice.StopBatchOperationResponse, error) {
	f.called = append(f.called, "StopBatchOperation")
	f.gotStop = req
	return &workflowservice.StopBatchOperationResponse{}, f.err
}

func (f *fakeWorkflowService) DescribeBatchOperation(ctx context.Context, req *workflowservice.DescribeBatchOperationRequest, _ ...grpc.CallOption) (*workflowservice.DescribeBatchOperationResponse, error) {
	f.called = append(f.called, "DescribeBatchOperation")
	f.gotDescribe = req
	if f.err != nil {
		return nil, f.err
	}
	if f.describeResp != nil {
		return f.describeResp, nil
	}
	return &workflowservice.DescribeBatchOperationResponse{}, nil
}

func (f *fakeWorkflowService) ListBatchOperations(ctx context.Context, req *workflowservice.ListBatchOperationsRequest, _ ...grpc.CallOption) (*workflowservice.ListBatchOperationsResponse, error) {
	f.called = append(f.called, "ListBatchOperations")
	f.gotList = req
	return &workflowservice.ListBatchOperationsResponse{}, f.err
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
	if _, err := a.StartBatchOperation(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.StopBatchOperation(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DescribeBatchOperation(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListBatchOperations(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

// A nil or namespace-less request gets the worker's namespace — every
// batch RPC is namespaced, so the rule has no exceptions here.
func TestNilRequestsGetTheWorkerNamespace(t *testing.T) {
	a, wfs := harness()
	callAllNil(t, a)

	for name, got := range map[string]string{
		"StartBatchOperation":    wfs.gotStart.Namespace,
		"StopBatchOperation":     wfs.gotStop.Namespace,
		"DescribeBatchOperation": wfs.gotDescribe.Namespace,
		"ListBatchOperations":    wfs.gotList.Namespace,
	} {
		if got != DefaultNamespace {
			t.Errorf("%s: namespace = %q, want %q", name, got, DefaultNamespace)
		}
	}
}

// Each activity calls its OWN RPC.
func TestEachActivityCallsItsOwnRPC(t *testing.T) {
	a, wfs := harness()
	callAllNil(t, a)

	want := []string{
		"StartBatchOperation",
		"StopBatchOperation",
		"DescribeBatchOperation",
		"ListBatchOperations",
	}
	if !reflect.DeepEqual(wfs.called, want) {
		t.Fatalf("RPCs called %v,\nwant %v", wfs.called, want)
	}
}

func TestWithNamespaceSetsTheDefault(t *testing.T) {
	wfs := &fakeWorkflowService{}
	a := New(&fakeClient{wfs: wfs}, WithNamespace("ops"))
	if _, err := a.StartBatchOperation(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotStart.Namespace != "ops" {
		t.Fatalf("namespace = %q, want the configured %q", wfs.gotStart.Namespace, "ops")
	}
}

// A request that names its namespace is the caller's business — for
// EVERY activity, not just one. Each repeats the same
// `if req.Namespace == ""` guard independently, so an accidental
// unconditional overwrite in any one of them would pass both
// TestNilRequestsGetTheWorkerNamespace (which only exercises the empty
// case) and TestEachActivityCallsItsOwnRPC (which checks routing, not
// fidelity). Here it is a safety property with unusually large blast
// radius: a batch terminate landing in the worker's namespace instead
// of the caller's would act on the wrong population entirely.
func TestExplicitNamespacePassesThrough(t *testing.T) {
	const elsewhere = "elsewhere"

	for _, tc := range []struct {
		name string
		call func(context.Context, *Activities) error
		got  func(*fakeWorkflowService) string
	}{
		{
			"StartBatchOperation",
			func(ctx context.Context, a *Activities) error {
				_, err := a.StartBatchOperation(ctx, &workflowservice.StartBatchOperationRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotStart.Namespace },
		},
		{
			"StopBatchOperation",
			func(ctx context.Context, a *Activities) error {
				_, err := a.StopBatchOperation(ctx, &workflowservice.StopBatchOperationRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotStop.Namespace },
		},
		{
			"DescribeBatchOperation",
			func(ctx context.Context, a *Activities) error {
				_, err := a.DescribeBatchOperation(ctx, &workflowservice.DescribeBatchOperationRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotDescribe.Namespace },
		},
		{
			"ListBatchOperations",
			func(ctx context.Context, a *Activities) error {
				_, err := a.ListBatchOperations(ctx, &workflowservice.ListBatchOperationsRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotList.Namespace },
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

	// The table must cover every activity in the pack — if AllNames
	// grows, this fails until the table does too.
	if want := len(AllNames()); want != 4 {
		t.Fatalf("the pack has %d activities but this table covers 4", want)
	}
}

// Proto fidelity, mechanically: the request reaches the RPC verbatim
// (same pointer, every field untouched) and the response comes back
// verbatim (same pointer). The pack adds nothing and hides nothing.
func TestRequestAndResponsePassThroughVerbatim(t *testing.T) {
	a, wfs := harness()
	wfs.describeResp = &workflowservice.DescribeBatchOperationResponse{
		JobId:                  "reap-2026-08-11",
		State:                  enums.BATCH_OPERATION_STATE_RUNNING,
		CompleteOperationCount: 12,
	}
	req := &workflowservice.DescribeBatchOperationRequest{
		Namespace: "default",
		JobId:     "reap-2026-08-11",
	}
	resp, err := a.DescribeBatchOperation(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if wfs.gotDescribe != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if resp != wfs.describeResp {
		t.Fatal("the caller did not receive the RPC's response message")
	}
	if resp.State != enums.BATCH_OPERATION_STATE_RUNNING || resp.CompleteOperationCount != 12 {
		t.Fatalf("progress = %+v, want the server's answer untouched", resp)
	}
}

// The operation oneof is the reason proto fidelity pays here: it spans
// a dozen operation types and grows. This pins that the pack carries
// the caller's chosen variant through untouched, alongside the query
// that selects the population and the rate limit that bounds the blast.
func TestStartCarriesTheOperationOneofAndQuery(t *testing.T) {
	a, wfs := harness()
	req := &workflowservice.StartBatchOperationRequest{
		VisibilityQuery:        `ExecutionStatus = "Running" AND WorkflowType = "Order"`,
		JobId:                  "reap-2026-08-11",
		Reason:                 "stuck past their SLA",
		MaxOperationsPerSecond: 5,
		Operation: &workflowservice.StartBatchOperationRequest_TerminationOperation{
			TerminationOperation: &batchpb.BatchOperationTermination{Identity: "reaper"},
		},
	}
	if _, err := a.StartBatchOperation(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if wfs.gotStart != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if wfs.gotStart.VisibilityQuery != `ExecutionStatus = "Running" AND WorkflowType = "Order"` ||
		wfs.gotStart.Reason != "stuck past their SLA" ||
		wfs.gotStart.MaxOperationsPerSecond != 5 {
		t.Fatalf("request fields were not passed verbatim: %+v", wfs.gotStart)
	}
	term, ok := wfs.gotStart.Operation.(*workflowservice.StartBatchOperationRequest_TerminationOperation)
	if !ok {
		t.Fatalf("operation = %T, want the caller's termination variant", wfs.gotStart.Operation)
	}
	if term.TerminationOperation.GetIdentity() != "reaper" {
		t.Fatalf("termination operation = %+v, want the caller's", term.TerminationOperation)
	}
}

// job_id is NOT filled by the pack. A contract test, not an omission:
// an activity can be retried, and a fresh job_id per attempt would
// start a SECOND batch over the same query — the same destructive thing
// done twice. The caller sends one stable across attempts so a retry
// resolves to the job already running.
func TestJobIDIsNeverFilledByThePack(t *testing.T) {
	a, wfs := harness()
	ctx := context.Background()

	if _, err := a.StartBatchOperation(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotStart.JobId != "" {
		t.Errorf("job_id = %q, want the caller's empty field untouched", wfs.gotStart.JobId)
	}

	// And a caller's own id survives — the round trip that makes a
	// retried start idempotent.
	if _, err := a.StartBatchOperation(ctx, &workflowservice.StartBatchOperationRequest{JobId: "mine"}); err != nil {
		t.Fatal(err)
	}
	if wfs.gotStart.JobId != "mine" {
		t.Fatalf("job_id = %q, want the caller's %q", wfs.gotStart.JobId, "mine")
	}
}

// visibility_query and target_executions are mutually exclusive, and
// the SERVER owns that rule. The pack must not adjudicate: a request
// setting both reaches the RPC with both set, so the server's error is
// what the caller sees rather than a pack-invented one.
func TestThePackDoesNotAdjudicateMutuallyExclusiveFields(t *testing.T) {
	a, wfs := harness()
	req := &workflowservice.StartBatchOperationRequest{
		VisibilityQuery:  `ExecutionStatus = "Running"`,
		TargetExecutions: []*common.Execution{{}},
	}
	if _, err := a.StartBatchOperation(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if wfs.gotStart.VisibilityQuery == "" || len(wfs.gotStart.TargetExecutions) != 1 {
		t.Fatalf("the pack dropped one of two mutually exclusive fields: %+v", wfs.gotStart)
	}
}

// Pagination is the proto's: next_page_token crosses verbatim.
func TestListPagesWithTheServersToken(t *testing.T) {
	a, wfs := harness()
	req := &workflowservice.ListBatchOperationsRequest{
		PageSize:      25,
		NextPageToken: []byte("prior-token"),
	}
	if _, err := a.ListBatchOperations(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if string(wfs.gotList.NextPageToken) != "prior-token" || wfs.gotList.PageSize != 25 {
		t.Fatalf("request fields were not passed verbatim: %+v", wfs.gotList)
	}
}

// RPC errors are the activity's errors — Temporal's retry machinery is
// the layer that deals with them, not the pack.
func TestErrorsPropagate(t *testing.T) {
	a, wfs := harness()
	boom := errors.New("batch job not found")
	wfs.err = boom

	if _, err := a.StartBatchOperation(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("StartBatchOperation err = %v, want the RPC's", err)
	}
	if _, err := a.DescribeBatchOperation(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("DescribeBatchOperation err = %v, want the RPC's", err)
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

// Registration is the capability grant, and this pack's is the largest
// in the epic — a batch terminate reaches every execution a query
// matches. The inventory is asserted exactly: these four names, every
// one `batch.`-prefixed.
func TestInventoryIsExactlyTheBatchSurface(t *testing.T) {
	want := map[string]bool{
		"batch.StartBatchOperation":    true,
		"batch.StopBatchOperation":     true,
		"batch.DescribeBatchOperation": true,
		"batch.ListBatchOperations":    true,
	}
	got := AllNames()
	if len(got) != len(want) {
		t.Fatalf("AllNames has %d entries, want %d: %v", len(got), len(want), got)
	}
	for _, n := range got {
		if !strings.HasPrefix(n, "batch.") {
			t.Errorf("name %q is not scoped to the pack", n)
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
