package taskqueue

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"go.temporal.io/api/enums/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
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

	gotDescribe   *workflowservice.DescribeTaskQueueRequest
	gotPartitions *workflowservice.ListTaskQueuePartitionsRequest

	describeResp *workflowservice.DescribeTaskQueueResponse
	err          error
}

func (f *fakeWorkflowService) DescribeTaskQueue(ctx context.Context, req *workflowservice.DescribeTaskQueueRequest, _ ...grpc.CallOption) (*workflowservice.DescribeTaskQueueResponse, error) {
	f.called = append(f.called, "DescribeTaskQueue")
	f.gotDescribe = req
	if f.err != nil {
		return nil, f.err
	}
	if f.describeResp != nil {
		return f.describeResp, nil
	}
	return &workflowservice.DescribeTaskQueueResponse{}, nil
}

func (f *fakeWorkflowService) ListTaskQueuePartitions(ctx context.Context, req *workflowservice.ListTaskQueuePartitionsRequest, _ ...grpc.CallOption) (*workflowservice.ListTaskQueuePartitionsResponse, error) {
	f.called = append(f.called, "ListTaskQueuePartitions")
	f.gotPartitions = req
	return &workflowservice.ListTaskQueuePartitionsResponse{}, f.err
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

	if _, err := a.DescribeTaskQueue(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListTaskQueuePartitions(ctx, nil); err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string]string{
		"DescribeTaskQueue":       wfs.gotDescribe.Namespace,
		"ListTaskQueuePartitions": wfs.gotPartitions.Namespace,
	} {
		if got != DefaultNamespace {
			t.Errorf("%s: namespace = %q, want %q", name, got, DefaultNamespace)
		}
	}
}

// Each activity calls its OWN RPC.
func TestEachActivityCallsItsOwnRPC(t *testing.T) {
	a, wfs := harness()
	ctx := context.Background()
	if _, err := a.DescribeTaskQueue(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListTaskQueuePartitions(ctx, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"DescribeTaskQueue", "ListTaskQueuePartitions"}
	if !reflect.DeepEqual(wfs.called, want) {
		t.Fatalf("RPCs called %v,\nwant %v", wfs.called, want)
	}
}

func TestWithNamespaceSetsTheDefault(t *testing.T) {
	wfs := &fakeWorkflowService{}
	a := New(&fakeClient{wfs: wfs}, WithNamespace("ops"))
	if _, err := a.DescribeTaskQueue(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotDescribe.Namespace != "ops" {
		t.Fatalf("namespace = %q, want the configured %q", wfs.gotDescribe.Namespace, "ops")
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
			"DescribeTaskQueue",
			func(ctx context.Context, a *Activities) error {
				_, err := a.DescribeTaskQueue(ctx, &workflowservice.DescribeTaskQueueRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotDescribe.Namespace },
		},
		{
			"ListTaskQueuePartitions",
			func(ctx context.Context, a *Activities) error {
				_, err := a.ListTaskQueuePartitions(ctx, &workflowservice.ListTaskQueuePartitionsRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotPartitions.Namespace },
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
// (same pointer, every field untouched) and the response comes back
// verbatim (same pointer). The pack adds nothing and hides nothing.
func TestRequestAndResponsePassThroughVerbatim(t *testing.T) {
	a, wfs := harness()
	wfs.describeResp = &workflowservice.DescribeTaskQueueResponse{
		Pollers: []*taskqueuepb.PollerInfo{{Identity: "worker-1"}},
	}
	req := &workflowservice.DescribeTaskQueueRequest{
		Namespace:     "default",
		TaskQueue:     &taskqueuepb.TaskQueue{Name: "gooey-visibility"},
		TaskQueueType: enums.TASK_QUEUE_TYPE_ACTIVITY,
		ReportStats:   true,
		ReportConfig:  true,
	}
	resp, err := a.DescribeTaskQueue(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if wfs.gotDescribe != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if wfs.gotDescribe.TaskQueue.GetName() != "gooey-visibility" ||
		wfs.gotDescribe.TaskQueueType != enums.TASK_QUEUE_TYPE_ACTIVITY ||
		!wfs.gotDescribe.ReportStats ||
		!wfs.gotDescribe.ReportConfig {
		t.Fatalf("request fields were not passed verbatim: %+v", wfs.gotDescribe)
	}
	if resp != wfs.describeResp {
		t.Fatal("the caller did not receive the RPC's response message")
	}
	if len(resp.Pollers) != 1 || resp.Pollers[0].GetIdentity() != "worker-1" {
		t.Fatalf("pollers = %+v, want the server's answer untouched", resp.Pollers)
	}
}

// report_stats and report_config default to FALSE and the pack never
// sets them. They cost the server real work, and a health dashboard
// polling on a timer must not silently pay for data it did not ask for.
func TestReportFlagsAreNeverSetByThePack(t *testing.T) {
	a, wfs := harness()
	if _, err := a.DescribeTaskQueue(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotDescribe.ReportStats {
		t.Error("report_stats = true on a nil request; the caller must ask for backlog stats")
	}
	if wfs.gotDescribe.ReportConfig {
		t.Error("report_config = true on a nil request; the caller must ask for queue config")
	}

	// And a caller who does ask, gets it.
	if _, err := a.DescribeTaskQueue(context.Background(), &workflowservice.DescribeTaskQueueRequest{ReportStats: true}); err != nil {
		t.Fatal(err)
	}
	if !wfs.gotDescribe.ReportStats {
		t.Error("report_stats = false, want the caller's true")
	}
}

// The partitions request carries the caller's task queue untouched —
// the pack never reaches into a request's sub-messages.
func TestPartitionsCarriesTheTaskQueue(t *testing.T) {
	a, wfs := harness()
	req := &workflowservice.ListTaskQueuePartitionsRequest{
		TaskQueue: &taskqueuepb.TaskQueue{Name: "gooey-workflow"},
	}
	if _, err := a.ListTaskQueuePartitions(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if wfs.gotPartitions.TaskQueue.GetName() != "gooey-workflow" {
		t.Fatalf("task queue = %+v, want the caller's", wfs.gotPartitions.TaskQueue)
	}
	if wfs.gotPartitions.Namespace != DefaultNamespace {
		t.Fatalf("namespace = %q, want defaulted", wfs.gotPartitions.Namespace)
	}
}

// RPC errors are the activity's errors — Temporal's retry machinery is
// the layer that deals with them, not the pack.
func TestErrorsPropagate(t *testing.T) {
	a, wfs := harness()
	boom := errors.New("task queue not found")
	wfs.err = boom

	if _, err := a.DescribeTaskQueue(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("DescribeTaskQueue err = %v, want the RPC's", err)
	}
	if _, err := a.ListTaskQueuePartitions(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("ListTaskQueuePartitions err = %v, want the RPC's", err)
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

// This pack is READ-ONLY by design: registering it must not grant the
// ability to change anything. UpdateTaskQueueConfig exists on
// WorkflowService and belongs to a different capability grant, so the
// inventory is asserted exactly — a mutating RPC added here would be a
// silent privilege escalation for every host that already registered
// the health panel.
func TestInventoryIsExactlyTheReadOnlySurface(t *testing.T) {
	want := map[string]bool{
		"taskqueue.DescribeTaskQueue":       true,
		"taskqueue.ListTaskQueuePartitions": true,
	}
	got := AllNames()
	if len(got) != len(want) {
		t.Fatalf("AllNames has %d entries, want %d: %v", len(got), len(want), got)
	}
	for _, n := range got {
		if !strings.HasPrefix(n, "taskqueue.") {
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
