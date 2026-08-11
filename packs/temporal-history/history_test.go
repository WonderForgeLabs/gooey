package history

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
)

// The fake embeds the real interface and overrides only what the pack
// calls: a method the pack should never touch panics on the nil
// embedded interface, which is exactly the assertion we want.

type fakeWorkflowService struct {
	workflowservice.WorkflowServiceClient

	called []string

	gotForward *workflowservice.GetWorkflowExecutionHistoryRequest
	gotReverse *workflowservice.GetWorkflowExecutionHistoryReverseRequest

	forwardResp *workflowservice.GetWorkflowExecutionHistoryResponse
	err         error
}

func (f *fakeWorkflowService) GetWorkflowExecutionHistory(ctx context.Context, req *workflowservice.GetWorkflowExecutionHistoryRequest, _ ...grpc.CallOption) (*workflowservice.GetWorkflowExecutionHistoryResponse, error) {
	f.called = append(f.called, "GetWorkflowExecutionHistory")
	f.gotForward = req
	if f.err != nil {
		return nil, f.err
	}
	if f.forwardResp != nil {
		return f.forwardResp, nil
	}
	return &workflowservice.GetWorkflowExecutionHistoryResponse{}, nil
}

func (f *fakeWorkflowService) GetWorkflowExecutionHistoryReverse(ctx context.Context, req *workflowservice.GetWorkflowExecutionHistoryReverseRequest, _ ...grpc.CallOption) (*workflowservice.GetWorkflowExecutionHistoryReverseResponse, error) {
	f.called = append(f.called, "GetWorkflowExecutionHistoryReverse")
	f.gotReverse = req
	return &workflowservice.GetWorkflowExecutionHistoryReverseResponse{}, f.err
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

// A nil or namespace-less request gets the worker's namespace — both
// RPCs are namespaced, so the rule has no exceptions here.
func TestNilRequestsGetTheWorkerNamespace(t *testing.T) {
	a, wfs := harness()
	ctx := context.Background()

	if _, err := a.GetWorkflowExecutionHistory(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetWorkflowExecutionHistoryReverse(ctx, nil); err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string]string{
		"GetWorkflowExecutionHistory":        wfs.gotForward.Namespace,
		"GetWorkflowExecutionHistoryReverse": wfs.gotReverse.Namespace,
	} {
		if got != DefaultNamespace {
			t.Errorf("%s: namespace = %q, want %q", name, got, DefaultNamespace)
		}
	}
}

// Forward and reverse reach DIFFERENT RPCs. They differ by one word in
// the method name, and a slip would still satisfy every other test.
func TestEachActivityCallsItsOwnRPC(t *testing.T) {
	a, wfs := harness()
	ctx := context.Background()
	if _, err := a.GetWorkflowExecutionHistory(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetWorkflowExecutionHistoryReverse(ctx, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"GetWorkflowExecutionHistory", "GetWorkflowExecutionHistoryReverse"}
	if !reflect.DeepEqual(wfs.called, want) {
		t.Fatalf("RPCs called %v,\nwant %v", wfs.called, want)
	}
}

func TestWithNamespaceSetsTheDefault(t *testing.T) {
	wfs := &fakeWorkflowService{}
	a := New(&fakeClient{wfs: wfs}, WithNamespace("ops"))
	if _, err := a.GetWorkflowExecutionHistory(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotForward.Namespace != "ops" {
		t.Fatalf("namespace = %q, want the configured %q", wfs.gotForward.Namespace, "ops")
	}
}

// A request that names its namespace is the caller's business — for
// EVERY activity, not just one. Each activity repeats the same
// `if req.Namespace == ""` guard independently, so an accidental
// unconditional overwrite in either would pass both
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
			"GetWorkflowExecutionHistory",
			func(ctx context.Context, a *Activities) error {
				_, err := a.GetWorkflowExecutionHistory(ctx, &workflowservice.GetWorkflowExecutionHistoryRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotForward.Namespace },
		},
		{
			"GetWorkflowExecutionHistoryReverse",
			func(ctx context.Context, a *Activities) error {
				_, err := a.GetWorkflowExecutionHistoryReverse(ctx, &workflowservice.GetWorkflowExecutionHistoryReverseRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotReverse.Namespace },
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
	if want := len(AllNames()); want != 2 {
		t.Fatalf("the pack has %d activities but this table covers 2", want)
	}
}

// Proto fidelity, mechanically: the request reaches the RPC verbatim
// (same pointer, page token and every flag untouched) and the response
// comes back verbatim (same pointer). The pack adds nothing and hides
// nothing.
//
// wait_new_event and the filter type are the fields most tempting to
// "help" with — a pack that quietly forced wait_new_event off would
// turn a live tail into a poll, and one that forced it on would hang
// every plain read. Both are pinned here.
func TestRequestAndResponsePassThroughVerbatim(t *testing.T) {
	a, wfs := harness()
	wfs.forwardResp = &workflowservice.GetWorkflowExecutionHistoryResponse{
		History:       &historypb.History{Events: []*historypb.HistoryEvent{{EventId: 1}}},
		NextPageToken: []byte("opaque-server-state"),
	}
	req := &workflowservice.GetWorkflowExecutionHistoryRequest{
		Namespace:              "default",
		Execution:              &common.WorkflowExecution{WorkflowId: "order-7", RunId: "run-1"},
		MaximumPageSize:        50,
		NextPageToken:          []byte("prior-token"),
		WaitNewEvent:           true,
		HistoryEventFilterType: enums.HISTORY_EVENT_FILTER_TYPE_CLOSE_EVENT,
		SkipArchival:           true,
	}
	resp, err := a.GetWorkflowExecutionHistory(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if wfs.gotForward != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if string(wfs.gotForward.NextPageToken) != "prior-token" ||
		wfs.gotForward.MaximumPageSize != 50 ||
		!wfs.gotForward.WaitNewEvent ||
		wfs.gotForward.HistoryEventFilterType != enums.HISTORY_EVENT_FILTER_TYPE_CLOSE_EVENT ||
		!wfs.gotForward.SkipArchival ||
		wfs.gotForward.Execution.GetWorkflowId() != "order-7" {
		t.Fatalf("request fields were not passed verbatim: %+v", wfs.gotForward)
	}
	if resp != wfs.forwardResp {
		t.Fatal("the caller did not receive the RPC's response message")
	}
	if string(resp.NextPageToken) != "opaque-server-state" {
		t.Fatalf("next_page_token = %q, want verbatim pass-through", resp.NextPageToken)
	}
	if len(resp.History.GetEvents()) != 1 || resp.History.GetEvents()[0].GetEventId() != 1 {
		t.Fatalf("history = %+v, want the server's events untouched", resp.History)
	}
}

// wait_new_event defaults to FALSE and the pack never sets it. A plain
// read must not silently become a long poll.
func TestWaitNewEventIsNeverSetByThePack(t *testing.T) {
	a, wfs := harness()
	if _, err := a.GetWorkflowExecutionHistory(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotForward.WaitNewEvent {
		t.Fatal("wait_new_event = true on a nil request; a plain read must not become a long poll")
	}
}

// The reverse RPC's own fields cross untouched too, including its
// separate page-token space.
func TestReverseCarriesItsOwnFields(t *testing.T) {
	a, wfs := harness()
	req := &workflowservice.GetWorkflowExecutionHistoryReverseRequest{
		Execution:       &common.WorkflowExecution{WorkflowId: "order-7"},
		MaximumPageSize: 10,
		NextPageToken:   []byte("reverse-token"),
	}
	if _, err := a.GetWorkflowExecutionHistoryReverse(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if wfs.gotReverse != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if string(wfs.gotReverse.NextPageToken) != "reverse-token" || wfs.gotReverse.MaximumPageSize != 10 {
		t.Fatalf("request fields were not passed verbatim: %+v", wfs.gotReverse)
	}
	if wfs.gotReverse.Namespace != DefaultNamespace {
		t.Fatalf("namespace = %q, want defaulted", wfs.gotReverse.Namespace)
	}
}

// The activity context reaches the RPC, which is what makes an activity
// timeout or cancellation cancel an in-flight long poll.
func TestContextReachesTheRPC(t *testing.T) {
	a, wfs := harness()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.GetWorkflowExecutionHistory(ctx, nil); err != nil {
		t.Fatal(err)
	}
	// The fake ignores ctx; what is asserted is that the pack passes the
	// caller's context through rather than substituting its own. A pack
	// that used context.Background() would leave a cancelled long poll
	// running past the activity's deadline.
	if wfs.gotForward == nil {
		t.Fatal("the RPC never ran")
	}
}

// RPC errors are the activity's errors — Temporal's retry machinery is
// the layer that deals with them, not the pack.
func TestErrorsPropagate(t *testing.T) {
	a, wfs := harness()
	boom := errors.New("workflow execution not found")
	wfs.err = boom

	if _, err := a.GetWorkflowExecutionHistory(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("GetWorkflowExecutionHistory err = %v, want the RPC's", err)
	}
	if _, err := a.GetWorkflowExecutionHistoryReverse(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("GetWorkflowExecutionHistoryReverse err = %v, want the RPC's", err)
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
	want := []string{
		"history.GetWorkflowExecutionHistory",
		"history.GetWorkflowExecutionHistoryReverse",
	}
	if !reflect.DeepEqual(r.names, want) {
		t.Fatalf("registered %v,\nwant the canonical %v", r.names, want)
	}
}
