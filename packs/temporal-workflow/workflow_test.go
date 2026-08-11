package workflow

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.temporal.io/api/common/v1"
	"go.temporal.io/api/query/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
)

// The fake embeds the real interface and overrides only what the pack
// calls: a method the pack should never touch panics on the nil
// embedded interface, which is exactly the assertion we want. `called`
// records the RPC name per call, so a mis-wired activity (terminate
// reaching cancel, say) fails rather than passing silently.

type fakeWorkflowService struct {
	workflowservice.WorkflowServiceClient

	called []string

	gotStart           *workflowservice.StartWorkflowExecutionRequest
	gotSignal          *workflowservice.SignalWorkflowExecutionRequest
	gotSignalWithStart *workflowservice.SignalWithStartWorkflowExecutionRequest
	gotQuery           *workflowservice.QueryWorkflowRequest
	gotCancel          *workflowservice.RequestCancelWorkflowExecutionRequest
	gotTerminate       *workflowservice.TerminateWorkflowExecutionRequest
	gotReset           *workflowservice.ResetWorkflowExecutionRequest

	startResp *workflowservice.StartWorkflowExecutionResponse
	err       error
}

func (f *fakeWorkflowService) StartWorkflowExecution(ctx context.Context, req *workflowservice.StartWorkflowExecutionRequest, _ ...grpc.CallOption) (*workflowservice.StartWorkflowExecutionResponse, error) {
	f.called = append(f.called, "StartWorkflowExecution")
	f.gotStart = req
	if f.err != nil {
		return nil, f.err
	}
	if f.startResp != nil {
		return f.startResp, nil
	}
	return &workflowservice.StartWorkflowExecutionResponse{}, nil
}

func (f *fakeWorkflowService) SignalWorkflowExecution(ctx context.Context, req *workflowservice.SignalWorkflowExecutionRequest, _ ...grpc.CallOption) (*workflowservice.SignalWorkflowExecutionResponse, error) {
	f.called = append(f.called, "SignalWorkflowExecution")
	f.gotSignal = req
	return &workflowservice.SignalWorkflowExecutionResponse{}, f.err
}

func (f *fakeWorkflowService) SignalWithStartWorkflowExecution(ctx context.Context, req *workflowservice.SignalWithStartWorkflowExecutionRequest, _ ...grpc.CallOption) (*workflowservice.SignalWithStartWorkflowExecutionResponse, error) {
	f.called = append(f.called, "SignalWithStartWorkflowExecution")
	f.gotSignalWithStart = req
	return &workflowservice.SignalWithStartWorkflowExecutionResponse{}, f.err
}

func (f *fakeWorkflowService) QueryWorkflow(ctx context.Context, req *workflowservice.QueryWorkflowRequest, _ ...grpc.CallOption) (*workflowservice.QueryWorkflowResponse, error) {
	f.called = append(f.called, "QueryWorkflow")
	f.gotQuery = req
	return &workflowservice.QueryWorkflowResponse{}, f.err
}

func (f *fakeWorkflowService) RequestCancelWorkflowExecution(ctx context.Context, req *workflowservice.RequestCancelWorkflowExecutionRequest, _ ...grpc.CallOption) (*workflowservice.RequestCancelWorkflowExecutionResponse, error) {
	f.called = append(f.called, "RequestCancelWorkflowExecution")
	f.gotCancel = req
	return &workflowservice.RequestCancelWorkflowExecutionResponse{}, f.err
}

func (f *fakeWorkflowService) TerminateWorkflowExecution(ctx context.Context, req *workflowservice.TerminateWorkflowExecutionRequest, _ ...grpc.CallOption) (*workflowservice.TerminateWorkflowExecutionResponse, error) {
	f.called = append(f.called, "TerminateWorkflowExecution")
	f.gotTerminate = req
	return &workflowservice.TerminateWorkflowExecutionResponse{}, f.err
}

func (f *fakeWorkflowService) ResetWorkflowExecution(ctx context.Context, req *workflowservice.ResetWorkflowExecutionRequest, _ ...grpc.CallOption) (*workflowservice.ResetWorkflowExecutionResponse, error) {
	f.called = append(f.called, "ResetWorkflowExecution")
	f.gotReset = req
	return &workflowservice.ResetWorkflowExecutionResponse{}, f.err
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

// callAllNil drives every activity with a nil request, in AllNames
// order.
func callAllNil(t *testing.T, a *Activities) {
	t.Helper()
	ctx := context.Background()
	if _, err := a.StartWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SignalWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SignalWithStartWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.QueryWorkflow(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RequestCancelWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.TerminateWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ResetWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

// A nil or namespace-less request gets the worker's namespace — every
// RPC in the pack is namespaced, so the rule has no exceptions here.
func TestNilRequestsGetTheWorkerNamespace(t *testing.T) {
	a, wfs := harness()
	callAllNil(t, a)

	for name, got := range map[string]string{
		"StartWorkflowExecution":           wfs.gotStart.Namespace,
		"SignalWorkflowExecution":          wfs.gotSignal.Namespace,
		"SignalWithStartWorkflowExecution": wfs.gotSignalWithStart.Namespace,
		"QueryWorkflow":                    wfs.gotQuery.Namespace,
		"RequestCancelWorkflowExecution":   wfs.gotCancel.Namespace,
		"TerminateWorkflowExecution":       wfs.gotTerminate.Namespace,
		"ResetWorkflowExecution":           wfs.gotReset.Namespace,
	} {
		if got != DefaultNamespace {
			t.Errorf("%s: namespace = %q, want %q", name, got, DefaultNamespace)
		}
	}
}

// Each activity calls its OWN RPC. Without this, a copy-paste slip
// (terminate reaching cancel) would still pass every namespace test.
func TestEachActivityCallsItsOwnRPC(t *testing.T) {
	a, wfs := harness()
	callAllNil(t, a)

	want := []string{
		"StartWorkflowExecution",
		"SignalWorkflowExecution",
		"SignalWithStartWorkflowExecution",
		"QueryWorkflow",
		"RequestCancelWorkflowExecution",
		"TerminateWorkflowExecution",
		"ResetWorkflowExecution",
	}
	if !reflect.DeepEqual(wfs.called, want) {
		t.Fatalf("RPCs called %v,\nwant %v", wfs.called, want)
	}
}

func TestWithNamespaceSetsTheDefault(t *testing.T) {
	wfs := &fakeWorkflowService{}
	a := New(&fakeClient{wfs: wfs}, WithNamespace("ops"))
	if _, err := a.TerminateWorkflowExecution(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotTerminate.Namespace != "ops" {
		t.Fatalf("namespace = %q, want the configured %q", wfs.gotTerminate.Namespace, "ops")
	}
}

// A request that names its namespace is the caller's business — for
// EVERY activity, not just one. Each activity repeats the same
// `if req.Namespace == ""` guard independently, so an accidental
// unconditional overwrite in any one of them would pass both
// TestNilRequestsGetTheWorkerNamespace (which only exercises the empty
// case) and TestEachActivityCallsItsOwnRPC (which checks routing, not
// fidelity). This is the test that catches it.
func TestExplicitNamespacePassesThrough(t *testing.T) {
	const elsewhere = "elsewhere"

	for _, tc := range []struct {
		name string
		call func(context.Context, *Activities) error
		got  func(*fakeWorkflowService) string
	}{
		{
			"StartWorkflowExecution",
			func(ctx context.Context, a *Activities) error {
				_, err := a.StartWorkflowExecution(ctx, &workflowservice.StartWorkflowExecutionRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotStart.Namespace },
		},
		{
			"SignalWorkflowExecution",
			func(ctx context.Context, a *Activities) error {
				_, err := a.SignalWorkflowExecution(ctx, &workflowservice.SignalWorkflowExecutionRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotSignal.Namespace },
		},
		{
			"SignalWithStartWorkflowExecution",
			func(ctx context.Context, a *Activities) error {
				_, err := a.SignalWithStartWorkflowExecution(ctx, &workflowservice.SignalWithStartWorkflowExecutionRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotSignalWithStart.Namespace },
		},
		{
			"QueryWorkflow",
			func(ctx context.Context, a *Activities) error {
				_, err := a.QueryWorkflow(ctx, &workflowservice.QueryWorkflowRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotQuery.Namespace },
		},
		{
			"RequestCancelWorkflowExecution",
			func(ctx context.Context, a *Activities) error {
				_, err := a.RequestCancelWorkflowExecution(ctx, &workflowservice.RequestCancelWorkflowExecutionRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotCancel.Namespace },
		},
		{
			"TerminateWorkflowExecution",
			func(ctx context.Context, a *Activities) error {
				_, err := a.TerminateWorkflowExecution(ctx, &workflowservice.TerminateWorkflowExecutionRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotTerminate.Namespace },
		},
		{
			"ResetWorkflowExecution",
			func(ctx context.Context, a *Activities) error {
				_, err := a.ResetWorkflowExecution(ctx, &workflowservice.ResetWorkflowExecutionRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotReset.Namespace },
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
	if want := len(AllNames()); want != 7 {
		t.Fatalf("the pack has %d activities but this table covers 7", want)
	}
}

// Proto fidelity, mechanically: the request reaches the RPC verbatim
// (same pointer, every field untouched) and the response comes back
// verbatim (same pointer). The pack adds nothing and hides nothing.
func TestRequestAndResponsePassThroughVerbatim(t *testing.T) {
	a, wfs := harness()
	wfs.startResp = &workflowservice.StartWorkflowExecutionResponse{RunId: "run-from-server"}
	req := &workflowservice.StartWorkflowExecutionRequest{
		Namespace:    "default",
		WorkflowId:   "order-7",
		WorkflowType: &common.WorkflowType{Name: "Order"},
		Identity:     "dashboard",
		RequestId:    "stable-across-attempts",
		CronSchedule: "0 * * * *",
	}
	resp, err := a.StartWorkflowExecution(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if wfs.gotStart != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if wfs.gotStart.WorkflowId != "order-7" ||
		wfs.gotStart.WorkflowType.GetName() != "Order" ||
		wfs.gotStart.Identity != "dashboard" ||
		wfs.gotStart.CronSchedule != "0 * * * *" {
		t.Fatalf("request fields were not passed verbatim: %+v", wfs.gotStart)
	}
	if resp != wfs.startResp {
		t.Fatal("the caller did not receive the RPC's response message")
	}
	if resp.RunId != "run-from-server" {
		t.Fatalf("run_id = %q, want verbatim pass-through", resp.RunId)
	}
}

// request_id is NOT filled by the pack. This is a contract test, not an
// omission: an activity can be retried, and a pack-generated id would
// either differ per attempt (starting a second workflow on retry) or
// silently pick a dedup key the caller never chose. The empty field
// reaches the server exactly as it would over raw gRPC.
func TestRequestIDIsNeverFilledByThePack(t *testing.T) {
	a, wfs := harness()
	ctx := context.Background()

	if _, err := a.StartWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SignalWithStartWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SignalWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RequestCancelWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ResetWorkflowExecution(ctx, nil); err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string]string{
		"StartWorkflowExecution":           wfs.gotStart.RequestId,
		"SignalWithStartWorkflowExecution": wfs.gotSignalWithStart.RequestId,
		"SignalWorkflowExecution":          wfs.gotSignal.RequestId,
		"RequestCancelWorkflowExecution":   wfs.gotCancel.RequestId,
		"ResetWorkflowExecution":           wfs.gotReset.RequestId,
	} {
		if got != "" {
			t.Errorf("%s: request_id = %q, want the caller's empty field untouched", name, got)
		}
	}

	// And a caller's own id survives.
	if _, err := a.StartWorkflowExecution(ctx, &workflowservice.StartWorkflowExecutionRequest{RequestId: "mine"}); err != nil {
		t.Fatal(err)
	}
	if wfs.gotStart.RequestId != "mine" {
		t.Fatalf("request_id = %q, want the caller's %q", wfs.gotStart.RequestId, "mine")
	}
}

// The destructive acts carry the caller's execution and reason
// verbatim — the fields an audit trail is made of.
func TestTerminateCarriesExecutionAndReason(t *testing.T) {
	a, wfs := harness()
	req := &workflowservice.TerminateWorkflowExecutionRequest{
		WorkflowExecution: &common.WorkflowExecution{WorkflowId: "order-7", RunId: "run-1"},
		Reason:            "stuck on a poisoned message",
		Identity:          "ops-dashboard",
	}
	if _, err := a.TerminateWorkflowExecution(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if wfs.gotTerminate.WorkflowExecution.GetWorkflowId() != "order-7" ||
		wfs.gotTerminate.WorkflowExecution.GetRunId() != "run-1" {
		t.Fatalf("execution = %+v, want the caller's", wfs.gotTerminate.WorkflowExecution)
	}
	if wfs.gotTerminate.Reason != "stuck on a poisoned message" || wfs.gotTerminate.Identity != "ops-dashboard" {
		t.Fatalf("reason/identity not passed verbatim: %+v", wfs.gotTerminate)
	}
	if wfs.gotTerminate.Namespace != DefaultNamespace {
		t.Fatalf("namespace = %q, want defaulted", wfs.gotTerminate.Namespace)
	}
}

// Query's nested WorkflowQuery message crosses untouched — the pack
// never reaches into a request's sub-messages.
func TestQueryCarriesTheNestedQueryMessage(t *testing.T) {
	a, wfs := harness()
	req := &workflowservice.QueryWorkflowRequest{
		Execution: &common.WorkflowExecution{WorkflowId: "order-7"},
		Query:     &query.WorkflowQuery{QueryType: "getState"},
	}
	if _, err := a.QueryWorkflow(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if wfs.gotQuery.Query.GetQueryType() != "getState" {
		t.Fatalf("query = %+v, want the caller's", wfs.gotQuery.Query)
	}
}

// RPC errors are the activity's errors — Temporal's retry machinery is
// the layer that deals with them, not the pack.
func TestErrorsPropagate(t *testing.T) {
	a, wfs := harness()
	boom := errors.New("workflow not found")
	wfs.err = boom

	if _, err := a.SignalWorkflowExecution(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("SignalWorkflowExecution err = %v, want the RPC's", err)
	}
	if _, err := a.TerminateWorkflowExecution(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("TerminateWorkflowExecution err = %v, want the RPC's", err)
	}
	if _, err := a.StartWorkflowExecution(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("StartWorkflowExecution err = %v, want the RPC's", err)
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
// statement: every name is prefixed `workflow.`, and the set is exactly
// the seven lifecycle acts — nothing from another pack's surface leaks
// in, and nothing destructive hides under an innocent name.
func TestInventoryIsExactlyTheLifecycleActs(t *testing.T) {
	want := map[string]bool{
		"workflow.StartWorkflowExecution":           true,
		"workflow.SignalWorkflowExecution":          true,
		"workflow.SignalWithStartWorkflowExecution": true,
		"workflow.QueryWorkflow":                    true,
		"workflow.RequestCancelWorkflowExecution":   true,
		"workflow.TerminateWorkflowExecution":       true,
		"workflow.ResetWorkflowExecution":           true,
	}
	got := AllNames()
	if len(got) != len(want) {
		t.Fatalf("AllNames has %d entries, want %d: %v", len(got), len(want), got)
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected registered name %q", n)
		}
		delete(want, n)
	}
	for n := range want {
		t.Errorf("missing registered name %q", n)
	}
}
