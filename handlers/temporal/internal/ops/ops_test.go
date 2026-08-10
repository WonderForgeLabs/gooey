package ops

// The dashboard proof, in the visibility_binding_test.go arrangement:
// a fake activityStarter stands in for the Temporal server and workers,
// and on its "worker" side it runs the pack's REAL convenience
// activities against a faked WorkflowService. Everything on either side
// — the markup loader, the temporal:Activity provider, the property
// graph, the ItemsView projection, the pack's request building — is
// real. What the fakes pin down is the wire: which requests the
// dashboard causes, and what its screen shows for a known response.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	temporalhandlers "github.com/WonderForgeLabs/gooey/handlers/temporal"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	visibility "github.com/WonderForgeLabs/gooey/packs/temporal-visibility"
	"go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ---- the WorkflowService fake: pages keyed by the token that fetches
// them, plus a recorder for every request the dashboard causes ----

type fakeWFS struct {
	workflowservice.WorkflowServiceClient
	pages map[string]*workflowservice.ListWorkflowExecutionsResponse

	listReqs  []*workflowservice.ListWorkflowExecutionsRequest
	countReqs []*workflowservice.CountWorkflowExecutionsRequest
	descReqs  []*workflowservice.DescribeWorkflowExecutionRequest
}

func (f *fakeWFS) ListWorkflowExecutions(ctx context.Context, req *workflowservice.ListWorkflowExecutionsRequest, _ ...grpc.CallOption) (*workflowservice.ListWorkflowExecutionsResponse, error) {
	f.listReqs = append(f.listReqs, req)
	if resp, ok := f.pages[string(req.NextPageToken)]; ok {
		return resp, nil
	}
	return &workflowservice.ListWorkflowExecutionsResponse{}, nil
}

func (f *fakeWFS) CountWorkflowExecutions(ctx context.Context, req *workflowservice.CountWorkflowExecutionsRequest, _ ...grpc.CallOption) (*workflowservice.CountWorkflowExecutionsResponse, error) {
	f.countReqs = append(f.countReqs, req)
	return &workflowservice.CountWorkflowExecutionsResponse{Count: 42}, nil
}

func (f *fakeWFS) DescribeWorkflowExecution(ctx context.Context, req *workflowservice.DescribeWorkflowExecutionRequest, _ ...grpc.CallOption) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	f.descReqs = append(f.descReqs, req)
	return &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflow.WorkflowExecutionInfo{
			Execution: req.Execution,
			Type:      &common.WorkflowType{Name: "Described"},
		},
	}, nil
}

type actsClient struct {
	client.Client
	wfs *fakeWFS
}

func (c *actsClient) WorkflowService() workflowservice.WorkflowServiceClient { return c.wfs }

// ---- the client the provider sees: ExecuteActivity dispatches to the
// pack's real convenience activities, and the result crosses back the
// way a json/plain string payload does — verbatim into `any` ----

type starterClient struct {
	client.Client
	acts *visibility.Activities
}

func (s *starterClient) ExecuteActivity(ctx context.Context, opts client.StartActivityOptions, activity any, args ...any) (client.ActivityHandle, error) {
	strs := make([]string, len(args))
	for i, a := range args {
		v, ok := a.(string)
		if !ok {
			return nil, fmt.Errorf("markup passed a %T argument; every markup argument crosses as a string", a)
		}
		strs[i] = v
	}
	var (
		out string
		err error
	)
	switch activity {
	case visibility.NameQuery:
		if len(strs) != 3 {
			return nil, fmt.Errorf("visibility.Query wants 3 args, got %d", len(strs))
		}
		out, err = s.acts.Query(ctx, strs[0], strs[1], strs[2])
	case visibility.NameCount:
		if len(strs) != 1 {
			return nil, fmt.Errorf("visibility.Count wants 1 arg, got %d", len(strs))
		}
		out, err = s.acts.Count(ctx, strs[0])
	case visibility.NameDescribe:
		if len(strs) != 2 {
			return nil, fmt.Errorf("visibility.Describe wants 2 args, got %d", len(strs))
		}
		out, err = s.acts.Describe(ctx, strs[0], strs[1])
	default:
		return nil, fmt.Errorf("unexpected activity %v", activity)
	}
	return &fakeHandle{id: opts.ID, result: out, err: err}, nil
}

type fakeHandle struct {
	id     string
	result string
	err    error
}

func (h *fakeHandle) GetID() string    { return h.id }
func (h *fakeHandle) GetRunID() string { return "run-" + h.id }

func (h *fakeHandle) Get(ctx context.Context, valuePtr any) error {
	if h.err != nil {
		return h.err
	}
	p, ok := valuePtr.(*any)
	if !ok {
		return errors.New("the provider decodes into any")
	}
	*p = h.result
	return nil
}

func (h *fakeHandle) Describe(context.Context, client.DescribeActivityOptions) (*client.ActivityExecutionDescription, error) {
	return nil, errors.New("not implemented")
}
func (h *fakeHandle) Cancel(context.Context, client.CancelActivityOptions) error       { return nil }
func (h *fakeHandle) Terminate(context.Context, client.TerminateActivityOptions) error { return nil }

// ---- harness ----

const cols, rows = 100, 30

type harness struct {
	t    *testing.T
	wfs  *fakeWFS
	vm   *VM
	ctx  *markup.Context
	disp *gooey.Dispatcher
	comp *gooey.Composer
}

func execInfo(id, wfType string, start time.Time) *workflow.WorkflowExecutionInfo {
	return &workflow.WorkflowExecutionInfo{
		Execution: &common.WorkflowExecution{WorkflowId: id, RunId: "run-" + id},
		Type:      &common.WorkflowType{Name: wfType},
		Status:    enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
		StartTime: timestamppb.New(start),
	}
}

func build(t *testing.T, wfs *fakeWFS) *harness {
	t.Helper()
	acts := visibility.New(&actsClient{wfs: wfs}, visibility.WithNamespace("ops-ns"))
	markup.RegisterHandlers(temporalhandlers.URI,
		temporalhandlers.New(&starterClient{acts: acts}, "test-queue"))
	t.Cleanup(func() { markup.RegisterHandlers(temporalhandlers.URI, nil) })

	h := &harness{t: t, wfs: wfs, disp: gooey.NewDispatcher()}
	h.vm = NewVM("worker in-process", func() {})
	h.ctx = &markup.Context{
		Values:     h.vm.Values(),
		Styles:     Theme(),
		Dispatcher: h.disp,
	}
	h.vm.Attach(h.ctx)

	w, err := markup.Load(Files, PageFile, h.ctx)
	if err != nil {
		t.Fatal(err)
	}
	h.comp = gooey.NewComposer(w, cols, rows)
	return h
}

// settle drains dispatcher work until cond holds — the async side of
// every fetch is a Deliver posted from a goroutine, and one intent may
// post several.
func (h *harness) settle(cond func() bool) {
	h.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		h.disp.Drain()
		if cond() {
			return
		}
		select {
		case <-h.disp.Wake():
		case <-deadline:
			h.t.Fatal("the dashboard never settled")
		}
	}
}

func (h *harness) screen() string {
	frame, _ := h.comp.Frame()
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			sb.WriteRune(frame.Cells.At(x, y).Rune)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (h *harness) executions() *components.ItemsView {
	h.t.Helper()
	v, ok := h.ctx.Named["Executions"].(*components.ItemsView)
	if !ok {
		h.t.Fatalf("no named ItemsView; Named = %v", h.ctx.Named)
	}
	return v
}

// runAndSettle presses enter in the query bar (the initial focus stop)
// and waits for both fetches — list and count — to land.
func (h *harness) runAndSettle() {
	h.t.Helper()
	if !h.comp.HandleKey(input.Named(input.KeyEnter)) {
		h.t.Fatal("enter was not consumed — the query bar's KeyBinding is not wired")
	}
	h.settle(func() bool { return h.vm.RowsJSON.Get() != "" && h.vm.CountJSON.Get() != "" })
}

// ---- tests ----

// The whole front door: enter in the query bar causes a
// ListWorkflowExecutions carrying the query-bar text as its query, the
// page-size scalar, the worker-defaulted namespace — and the response's
// rows land on screen, projected through ItemsOf into the template.
func TestEnterRunsTheQueryAndProjectsTheRows(t *testing.T) {
	start := time.Date(2026, 8, 10, 12, 30, 0, 0, time.UTC)
	wfs := &fakeWFS{pages: map[string]*workflowservice.ListWorkflowExecutionsResponse{
		"": {Executions: []*workflow.WorkflowExecutionInfo{
			execInfo("order-7", "ProcessOrder", start),
			execInfo("billing-3", "BillCustomer", start.Add(time.Minute)),
		}},
	}}
	h := build(t, wfs)
	h.vm.Query.Set(`ExecutionStatus="Running"`)
	h.runAndSettle()

	if len(wfs.listReqs) != 1 || len(wfs.countReqs) != 1 {
		t.Fatalf("list/count requests = %d/%d, want 1/1", len(wfs.listReqs), len(wfs.countReqs))
	}
	list := wfs.listReqs[0]
	if list.Query != `ExecutionStatus="Running"` {
		t.Errorf("list query = %q, want the query bar's text", list.Query)
	}
	if list.PageSize != 25 {
		t.Errorf("page size = %d, want the .PageSize scalar", list.PageSize)
	}
	if list.Namespace != "ops-ns" {
		t.Errorf("namespace = %q, want the worker's", list.Namespace)
	}
	if wfs.countReqs[0].Query != `ExecutionStatus="Running"` {
		t.Errorf("count query = %q, want the query bar's text", wfs.countReqs[0].Query)
	}

	screen := h.screen()
	for _, want := range []string{
		"order-7", "ProcessOrder",
		"billing-3", "BillCustomer",
		"Running",
		"2026-08-10 12:30:00",
		"42 matching", // the count, through protojson's string-typed int64
		"2 shown",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("screen is missing %q:\n%s", want, screen)
		}
	}
}

// Moving the selection describes the newly selected execution — the
// SelectionChanged expression reads the selected row's IDs from
// computeds at invoke time.
func TestSelectionDescribesTheSelectedExecution(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	wfs := &fakeWFS{pages: map[string]*workflowservice.ListWorkflowExecutionsResponse{
		"": {Executions: []*workflow.WorkflowExecutionInfo{
			execInfo("order-7", "ProcessOrder", start),
			execInfo("billing-3", "BillCustomer", start),
		}},
	}}
	h := build(t, wfs)
	h.runAndSettle()
	h.comp.Frame()

	view := h.executions()
	h.comp.Focus().SetFocus(view)
	if !h.comp.HandleKey(input.Named(input.KeyDown)) {
		t.Fatal("down did not reach the list")
	}
	h.settle(func() bool { return h.vm.DescribeJSON.Get() != "" })

	if len(wfs.descReqs) != 1 {
		t.Fatalf("describe requests = %d, want 1", len(wfs.descReqs))
	}
	got := wfs.descReqs[0]
	if got.Execution.GetWorkflowId() != "billing-3" || got.Execution.GetRunId() != "run-billing-3" {
		t.Fatalf("described %v, want the newly selected row", got.Execution)
	}
	if got.Namespace != "ops-ns" {
		t.Errorf("describe namespace = %q, want the worker's", got.Namespace)
	}

	// The pane renders the canonical response, pretty-printed.
	screen := h.screen()
	if !strings.Contains(screen, `"workflowId": "billing-3"`) {
		t.Errorf("describe pane is missing the canonical field:\n%s", screen)
	}
}

// Next follows the response's token, prev replays the remembered one —
// the round trip the decision record promises: the token crosses to the
// screen side as base64 text and comes back as the same bytes.
func TestPaginationRoundTripsTheToken(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	wfs := &fakeWFS{pages: map[string]*workflowservice.ListWorkflowExecutionsResponse{
		"": {
			Executions:    []*workflow.WorkflowExecutionInfo{execInfo("page1-a", "T", start)},
			NextPageToken: []byte("tok-2"),
		},
		"tok-2": {
			Executions: []*workflow.WorkflowExecutionInfo{execInfo("page2-a", "T", start)},
		},
	}}
	h := build(t, wfs)
	h.runAndSettle()

	// next: the fetch must carry the server's token, decoded back to the
	// exact bytes it sent.
	h.vm.NextPage()
	h.settle(func() bool { return len(wfs.listReqs) == 2 })
	if string(wfs.listReqs[1].NextPageToken) != "tok-2" {
		t.Fatalf("next fetched with token %q, want the server's bytes", wfs.listReqs[1].NextPageToken)
	}
	h.settle(func() bool { return strings.Contains(h.vm.RowsJSON.Get(), "page2-a") })
	if h.vm.PageNum.Get() != 2 {
		t.Fatalf("page = %d, want 2", h.vm.PageNum.Get())
	}
	if screen := h.screen(); !strings.Contains(screen, "page 2") || !strings.Contains(screen, "page2-a") {
		t.Errorf("screen did not turn the page:\n%s", screen)
	}

	// The last page has no token: next must not fetch.
	h.vm.NextPage()
	h.disp.Drain()
	if len(wfs.listReqs) != 2 {
		t.Fatalf("next off the end fetched anyway: %d requests", len(wfs.listReqs))
	}

	// prev: back to the remembered first page.
	h.vm.PrevPage()
	h.settle(func() bool { return len(wfs.listReqs) == 3 })
	if len(wfs.listReqs[2].NextPageToken) != 0 {
		t.Fatalf("prev fetched with token %q, want the first page's empty token", wfs.listReqs[2].NextPageToken)
	}
	h.settle(func() bool { return strings.Contains(h.vm.RowsJSON.Get(), "page1-a") })
	if h.vm.PageNum.Get() != 1 {
		t.Fatalf("page = %d, want 1", h.vm.PageNum.Get())
	}
	// And below the floor, nothing.
	h.vm.PrevPage()
	h.disp.Drain()
	if len(wfs.listReqs) != 3 {
		t.Fatalf("prev off the start fetched anyway: %d requests", len(wfs.listReqs))
	}
}

// A new run resets to page one: empty token, history cleared.
func TestRunResetsPagination(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	wfs := &fakeWFS{pages: map[string]*workflowservice.ListWorkflowExecutionsResponse{
		"": {
			Executions:    []*workflow.WorkflowExecutionInfo{execInfo("a", "T", start)},
			NextPageToken: []byte("tok-2"),
		},
		"tok-2": {Executions: []*workflow.WorkflowExecutionInfo{execInfo("b", "T", start)}},
	}}
	h := build(t, wfs)
	h.runAndSettle()
	h.vm.NextPage()
	h.settle(func() bool { return len(wfs.listReqs) == 2 })

	h.vm.RunQuery()
	h.settle(func() bool { return len(wfs.listReqs) == 3 })
	if len(wfs.listReqs[2].NextPageToken) != 0 {
		t.Fatalf("run fetched with token %q, want page one", wfs.listReqs[2].NextPageToken)
	}
	if h.vm.PageNum.Get() != 1 {
		t.Fatalf("page = %d, want 1", h.vm.PageNum.Get())
	}
	h.vm.PrevPage()
	h.disp.Drain()
	if len(wfs.listReqs) != 3 {
		t.Fatal("run left stale page history behind")
	}
}

// ctrl+r refetches the CURRENT page — same token — from anywhere on the
// page, through the root-scoped KeyBinding.
func TestRefreshKeepsTheCurrentPage(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	wfs := &fakeWFS{pages: map[string]*workflowservice.ListWorkflowExecutionsResponse{
		"": {
			Executions:    []*workflow.WorkflowExecutionInfo{execInfo("a", "T", start)},
			NextPageToken: []byte("tok-2"),
		},
		"tok-2": {Executions: []*workflow.WorkflowExecutionInfo{execInfo("b", "T", start)}},
	}}
	h := build(t, wfs)
	h.runAndSettle()
	h.vm.NextPage()
	h.settle(func() bool { return len(wfs.listReqs) == 2 })

	if !h.comp.HandleKey(input.KeyEvent{Key: input.KeyRune, Rune: 'r', Mods: input.ModCtrl}) {
		t.Fatal("ctrl+r was not consumed — the root KeyBinding is not wired")
	}
	h.settle(func() bool { return len(wfs.listReqs) == 3 })
	if string(wfs.listReqs[2].NextPageToken) != "tok-2" {
		t.Fatalf("refresh fetched with token %q, want the current page's", wfs.listReqs[2].NextPageToken)
	}
}

// The damage pin: moving the selection repaints the view node and the
// two row highlights — nothing else, before the async describe lands.
func TestSelectionMoveRepaintsExactlyTheRows(t *testing.T) {
	start := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	wfs := &fakeWFS{pages: map[string]*workflowservice.ListWorkflowExecutionsResponse{
		"": {Executions: []*workflow.WorkflowExecutionInfo{
			execInfo("a", "T", start), execInfo("b", "T", start), execInfo("c", "T", start),
		}},
	}}
	h := build(t, wfs)
	h.runAndSettle()
	h.comp.Frame()

	view := h.executions()
	h.comp.Focus().SetFocus(view)
	h.comp.Frame() // focus movement's own damage, out of the way

	h.comp.HandleKey(input.Named(input.KeyDown))
	if _, painted := h.comp.Frame(); painted != 3 {
		h.t.Fatalf("selection move repainted %d nodes, want 3 (view + two highlights)", painted)
	}
}
