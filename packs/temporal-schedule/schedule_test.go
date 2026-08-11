package schedule

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	schedulepb "go.temporal.io/api/schedule/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
)

// The fake embeds the real interface and overrides only what the pack
// calls: a method the pack should never touch panics on the nil
// embedded interface, which is exactly the assertion we want. `called`
// records the RPC name per call, so a mis-wired activity (delete
// reaching patch, say) fails rather than passing silently.

type fakeWorkflowService struct {
	workflowservice.WorkflowServiceClient

	called []string

	gotList         *workflowservice.ListSchedulesRequest
	gotCount        *workflowservice.CountSchedulesRequest
	gotDescribe     *workflowservice.DescribeScheduleRequest
	gotMatchingTime *workflowservice.ListScheduleMatchingTimesRequest
	gotCreate       *workflowservice.CreateScheduleRequest
	gotUpdate       *workflowservice.UpdateScheduleRequest
	gotPatch        *workflowservice.PatchScheduleRequest
	gotDelete       *workflowservice.DeleteScheduleRequest

	describeResp *workflowservice.DescribeScheduleResponse
	err          error
}

func (f *fakeWorkflowService) ListSchedules(ctx context.Context, req *workflowservice.ListSchedulesRequest, _ ...grpc.CallOption) (*workflowservice.ListSchedulesResponse, error) {
	f.called = append(f.called, "ListSchedules")
	f.gotList = req
	return &workflowservice.ListSchedulesResponse{}, f.err
}

func (f *fakeWorkflowService) CountSchedules(ctx context.Context, req *workflowservice.CountSchedulesRequest, _ ...grpc.CallOption) (*workflowservice.CountSchedulesResponse, error) {
	f.called = append(f.called, "CountSchedules")
	f.gotCount = req
	return &workflowservice.CountSchedulesResponse{Count: 3}, f.err
}

func (f *fakeWorkflowService) DescribeSchedule(ctx context.Context, req *workflowservice.DescribeScheduleRequest, _ ...grpc.CallOption) (*workflowservice.DescribeScheduleResponse, error) {
	f.called = append(f.called, "DescribeSchedule")
	f.gotDescribe = req
	if f.err != nil {
		return nil, f.err
	}
	if f.describeResp != nil {
		return f.describeResp, nil
	}
	return &workflowservice.DescribeScheduleResponse{}, nil
}

func (f *fakeWorkflowService) ListScheduleMatchingTimes(ctx context.Context, req *workflowservice.ListScheduleMatchingTimesRequest, _ ...grpc.CallOption) (*workflowservice.ListScheduleMatchingTimesResponse, error) {
	f.called = append(f.called, "ListScheduleMatchingTimes")
	f.gotMatchingTime = req
	return &workflowservice.ListScheduleMatchingTimesResponse{}, f.err
}

func (f *fakeWorkflowService) CreateSchedule(ctx context.Context, req *workflowservice.CreateScheduleRequest, _ ...grpc.CallOption) (*workflowservice.CreateScheduleResponse, error) {
	f.called = append(f.called, "CreateSchedule")
	f.gotCreate = req
	return &workflowservice.CreateScheduleResponse{}, f.err
}

func (f *fakeWorkflowService) UpdateSchedule(ctx context.Context, req *workflowservice.UpdateScheduleRequest, _ ...grpc.CallOption) (*workflowservice.UpdateScheduleResponse, error) {
	f.called = append(f.called, "UpdateSchedule")
	f.gotUpdate = req
	return &workflowservice.UpdateScheduleResponse{}, f.err
}

func (f *fakeWorkflowService) PatchSchedule(ctx context.Context, req *workflowservice.PatchScheduleRequest, _ ...grpc.CallOption) (*workflowservice.PatchScheduleResponse, error) {
	f.called = append(f.called, "PatchSchedule")
	f.gotPatch = req
	return &workflowservice.PatchScheduleResponse{}, f.err
}

func (f *fakeWorkflowService) DeleteSchedule(ctx context.Context, req *workflowservice.DeleteScheduleRequest, _ ...grpc.CallOption) (*workflowservice.DeleteScheduleResponse, error) {
	f.called = append(f.called, "DeleteSchedule")
	f.gotDelete = req
	return &workflowservice.DeleteScheduleResponse{}, f.err
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
	if _, err := a.ListSchedules(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CountSchedules(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DescribeSchedule(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListScheduleMatchingTimes(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateSchedule(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateSchedule(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PatchSchedule(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteSchedule(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

// A nil or namespace-less request gets the worker's namespace — every
// schedule RPC is namespaced, so the rule has no exceptions here.
func TestNilRequestsGetTheWorkerNamespace(t *testing.T) {
	a, wfs := harness()
	callAllNil(t, a)

	for name, got := range map[string]string{
		"ListSchedules":             wfs.gotList.Namespace,
		"CountSchedules":            wfs.gotCount.Namespace,
		"DescribeSchedule":          wfs.gotDescribe.Namespace,
		"ListScheduleMatchingTimes": wfs.gotMatchingTime.Namespace,
		"CreateSchedule":            wfs.gotCreate.Namespace,
		"UpdateSchedule":            wfs.gotUpdate.Namespace,
		"PatchSchedule":             wfs.gotPatch.Namespace,
		"DeleteSchedule":            wfs.gotDelete.Namespace,
	} {
		if got != DefaultNamespace {
			t.Errorf("%s: namespace = %q, want %q", name, got, DefaultNamespace)
		}
	}
}

// Each activity calls its OWN RPC. With eight near-identical bodies a
// copy-paste slip is the likeliest bug, and it would pass every
// namespace test.
func TestEachActivityCallsItsOwnRPC(t *testing.T) {
	a, wfs := harness()
	callAllNil(t, a)

	want := []string{
		"ListSchedules",
		"CountSchedules",
		"DescribeSchedule",
		"ListScheduleMatchingTimes",
		"CreateSchedule",
		"UpdateSchedule",
		"PatchSchedule",
		"DeleteSchedule",
	}
	if !reflect.DeepEqual(wfs.called, want) {
		t.Fatalf("RPCs called %v,\nwant %v", wfs.called, want)
	}
}

func TestWithNamespaceSetsTheDefault(t *testing.T) {
	wfs := &fakeWorkflowService{}
	a := New(&fakeClient{wfs: wfs}, WithNamespace("ops"))
	if _, err := a.DeleteSchedule(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if wfs.gotDelete.Namespace != "ops" {
		t.Fatalf("namespace = %q, want the configured %q", wfs.gotDelete.Namespace, "ops")
	}
}

// A request that names its namespace is the caller's business — for
// EVERY activity, not just one. Each activity repeats the same
// `if req.Namespace == ""` guard independently, so an accidental
// unconditional overwrite in any one of the eight would pass both
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
			"ListSchedules",
			func(ctx context.Context, a *Activities) error {
				_, err := a.ListSchedules(ctx, &workflowservice.ListSchedulesRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotList.Namespace },
		},
		{
			"CountSchedules",
			func(ctx context.Context, a *Activities) error {
				_, err := a.CountSchedules(ctx, &workflowservice.CountSchedulesRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotCount.Namespace },
		},
		{
			"DescribeSchedule",
			func(ctx context.Context, a *Activities) error {
				_, err := a.DescribeSchedule(ctx, &workflowservice.DescribeScheduleRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotDescribe.Namespace },
		},
		{
			"ListScheduleMatchingTimes",
			func(ctx context.Context, a *Activities) error {
				_, err := a.ListScheduleMatchingTimes(ctx, &workflowservice.ListScheduleMatchingTimesRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotMatchingTime.Namespace },
		},
		{
			"CreateSchedule",
			func(ctx context.Context, a *Activities) error {
				_, err := a.CreateSchedule(ctx, &workflowservice.CreateScheduleRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotCreate.Namespace },
		},
		{
			"UpdateSchedule",
			func(ctx context.Context, a *Activities) error {
				_, err := a.UpdateSchedule(ctx, &workflowservice.UpdateScheduleRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotUpdate.Namespace },
		},
		{
			"PatchSchedule",
			func(ctx context.Context, a *Activities) error {
				_, err := a.PatchSchedule(ctx, &workflowservice.PatchScheduleRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotPatch.Namespace },
		},
		{
			"DeleteSchedule",
			func(ctx context.Context, a *Activities) error {
				_, err := a.DeleteSchedule(ctx, &workflowservice.DeleteScheduleRequest{Namespace: elsewhere})
				return err
			},
			func(f *fakeWorkflowService) string { return f.gotDelete.Namespace },
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
	if want := len(AllNames()); want != 8 {
		t.Fatalf("the pack has %d activities but this table covers 8", want)
	}
}

// Proto fidelity, mechanically: the request reaches the RPC verbatim
// (same pointer, page token untouched) and the response comes back
// verbatim (same pointer). The pack adds nothing and hides nothing.
func TestRequestAndResponsePassThroughVerbatim(t *testing.T) {
	a, wfs := harness()
	wfs.describeResp = &workflowservice.DescribeScheduleResponse{
		ConflictToken: []byte("opaque-server-state"),
		Schedule:      &schedulepb.Schedule{},
	}
	req := &workflowservice.DescribeScheduleRequest{
		Namespace:  "default",
		ScheduleId: "nightly-rollup",
	}
	resp, err := a.DescribeSchedule(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if wfs.gotDescribe != req {
		t.Fatal("the RPC did not receive the caller's request message")
	}
	if wfs.gotDescribe.ScheduleId != "nightly-rollup" {
		t.Fatalf("request fields were not passed verbatim: %+v", wfs.gotDescribe)
	}
	if resp != wfs.describeResp {
		t.Fatal("the caller did not receive the RPC's response message")
	}
	if string(resp.ConflictToken) != "opaque-server-state" {
		t.Fatalf("conflict_token = %q, want verbatim pass-through", resp.ConflictToken)
	}
}

// Pagination is the proto's: next_page_token crosses verbatim in both
// directions, never interpreted.
func TestListPagesWithTheServersToken(t *testing.T) {
	a, wfs := harness()
	req := &workflowservice.ListSchedulesRequest{
		MaximumPageSize: 25,
		NextPageToken:   []byte("prior-token"),
		Query:           `ScheduleId STARTS_WITH "nightly"`,
	}
	if _, err := a.ListSchedules(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if string(wfs.gotList.NextPageToken) != "prior-token" ||
		wfs.gotList.MaximumPageSize != 25 ||
		wfs.gotList.Query != `ScheduleId STARTS_WITH "nightly"` {
		t.Fatalf("request fields were not passed verbatim: %+v", wfs.gotList)
	}
}

// request_id and conflict_token are NOT filled by the pack. Contract
// tests, not omissions: an activity can be retried, so a pack-generated
// request_id would apply a mutation twice; and a pack-invented
// conflict_token would either clobber a concurrent edit or fail one the
// caller never intended to guard.
func TestRequestIDAndConflictTokenAreNeverFilledByThePack(t *testing.T) {
	a, wfs := harness()
	ctx := context.Background()

	if _, err := a.CreateSchedule(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateSchedule(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PatchSchedule(ctx, nil); err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string]string{
		"CreateSchedule": wfs.gotCreate.RequestId,
		"UpdateSchedule": wfs.gotUpdate.RequestId,
		"PatchSchedule":  wfs.gotPatch.RequestId,
	} {
		if got != "" {
			t.Errorf("%s: request_id = %q, want the caller's empty field untouched", name, got)
		}
	}
	if wfs.gotUpdate.ConflictToken != nil {
		t.Errorf("UpdateSchedule: conflict_token = %q, want the caller's empty field untouched", wfs.gotUpdate.ConflictToken)
	}

	// And the caller's own values survive — the round trip that makes
	// optimistic concurrency work.
	if _, err := a.UpdateSchedule(ctx, &workflowservice.UpdateScheduleRequest{
		RequestId:     "mine",
		ConflictToken: []byte("from-describe"),
	}); err != nil {
		t.Fatal(err)
	}
	if wfs.gotUpdate.RequestId != "mine" || string(wfs.gotUpdate.ConflictToken) != "from-describe" {
		t.Fatalf("caller's idempotency fields not passed verbatim: %+v", wfs.gotUpdate)
	}
}

// PatchSchedule's nested SchedulePatch — the pause/unpause/trigger a
// dashboard's buttons send — crosses untouched. The pack never reaches
// into a request's sub-messages.
func TestPatchCarriesTheNestedPatchMessage(t *testing.T) {
	a, wfs := harness()
	req := &workflowservice.PatchScheduleRequest{
		ScheduleId: "nightly-rollup",
		Patch: &schedulepb.SchedulePatch{
			Pause: "operator paused from the dashboard",
		},
		Identity: "cron-manager",
	}
	if _, err := a.PatchSchedule(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if wfs.gotPatch.Patch.GetPause() != "operator paused from the dashboard" {
		t.Fatalf("patch = %+v, want the caller's", wfs.gotPatch.Patch)
	}
	if wfs.gotPatch.Identity != "cron-manager" {
		t.Fatalf("identity = %q, want the caller's", wfs.gotPatch.Identity)
	}
}

// RPC errors are the activity's errors — Temporal's retry machinery is
// the layer that deals with them, not the pack.
func TestErrorsPropagate(t *testing.T) {
	a, wfs := harness()
	boom := errors.New("schedule not found")
	wfs.err = boom

	if _, err := a.DescribeSchedule(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("DescribeSchedule err = %v, want the RPC's", err)
	}
	if _, err := a.DeleteSchedule(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("DeleteSchedule err = %v, want the RPC's", err)
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
// statement: exactly these eight names, every one `schedule.`-prefixed,
// and the four mutations named for what they do.
func TestInventoryIsExactlyTheScheduleSurface(t *testing.T) {
	want := map[string]bool{
		"schedule.ListSchedules":             true,
		"schedule.CountSchedules":            true,
		"schedule.DescribeSchedule":          true,
		"schedule.ListScheduleMatchingTimes": true,
		"schedule.CreateSchedule":            true,
		"schedule.UpdateSchedule":            true,
		"schedule.PatchSchedule":             true,
		"schedule.DeleteSchedule":            true,
	}
	got := AllNames()
	if len(got) != len(want) {
		t.Fatalf("AllNames has %d entries, want %d: %v", len(got), len(want), got)
	}
	for _, n := range got {
		if !strings.HasPrefix(n, "schedule.") {
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
