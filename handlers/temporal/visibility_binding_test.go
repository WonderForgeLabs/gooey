package temporalhandlers

// The binding proof for the temporal-visibility activity pack: a
// visibility response — a real temporal.api.* proto, produced by the
// pack's real activity code — lands in a gooey page through the
// ordinary temporal:Activity provider.
//
// The seam is the same one temporal_test.go uses: a fake
// activityStarter stands in for the Temporal server and workers.
// Everything on either side of it is real. On the "worker" side the
// fake runs the pack's ListWorkflowExecutions against a faked
// WorkflowService; on the way back it crosses the property boundary the
// decision record mandates — protojson at the edge — before the
// provider renders it into the bound property. What the page ends up
// holding is Temporal's canonical JSON, with Temporal's field names.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	visibility "github.com/WonderForgeLabs/gooey/packs/temporal-visibility"
	"go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

// visibilityStarter "executes" a visibility activity the way a worker
// would: it calls the pack's real activity function, then converts the
// proto result the way the payload-converter edge does — protojson,
// decoded back as plain data for the provider's any-typed Get.
type visibilityStarter struct {
	acts *visibility.Activities
}

func (v *visibilityStarter) ExecuteActivity(ctx context.Context, opts client.StartActivityOptions, activity any, args ...any) (client.ActivityHandle, error) {
	if activity != visibility.NameListWorkflowExecutions {
		return nil, errors.New("this fake serves exactly one activity name")
	}
	resp, err := v.acts.ListWorkflowExecutions(ctx, nil)
	if err != nil {
		return nil, err
	}
	// The property boundary: proto → protojson → plain data. This is
	// what the real converter chain yields for a json/protobuf payload
	// decoded into `any`.
	canonical, err := protojson.Marshal(resp)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(canonical, &out); err != nil {
		return nil, err
	}
	return &fakeHandle{id: opts.ID, result: out}, nil
}

// wfsClient fakes exactly one RPC; everything else panics via the nil
// embedded interface.
type wfsClient struct {
	workflowservice.WorkflowServiceClient
	resp *workflowservice.ListWorkflowExecutionsResponse
	got  *workflowservice.ListWorkflowExecutionsRequest
}

func (f *wfsClient) ListWorkflowExecutions(ctx context.Context, req *workflowservice.ListWorkflowExecutionsRequest, _ ...grpc.CallOption) (*workflowservice.ListWorkflowExecutionsResponse, error) {
	f.got = req
	return f.resp, nil
}

type visClient struct {
	client.Client
	wfs *wfsClient
}

func (c *visClient) WorkflowService() workflowservice.WorkflowServiceClient { return c.wfs }

const visibilityPage = `<Gooey xmlns="wonderforge.io/gooey/2026"
       xmlns:temporal="gooey.dev/handlers/temporal">
  <VStack>
    <Button Content="list" Click="{{temporal:Activity ` +
	"`visibility.ListWorkflowExecutions`" + ` | into .Output}}"/>
    <Text>{{.Output}}</Text>
  </VStack>
</Gooey>`

func TestVisibilityResponseLandsInAGooeyPage(t *testing.T) {
	wfs := &wfsClient{resp: &workflowservice.ListWorkflowExecutionsResponse{
		Executions: []*workflow.WorkflowExecutionInfo{{
			Execution: &common.WorkflowExecution{WorkflowId: "order-7", RunId: "run-1"},
			Type:      &common.WorkflowType{Name: "ProcessOrder"},
			Status:    enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
		}},
		NextPageToken: []byte("more"),
	}}
	acts := visibility.New(&visClient{wfs: wfs}, visibility.WithNamespace("gooey-ns"))

	h := build(t, visibilityPage, &visibilityStarter{acts: acts})
	h.clickAndSettle()

	// The markup carried no request, so the pack defaulted the namespace
	// to the worker's — uniformly, per the decision record.
	if wfs.got.Namespace != "gooey-ns" {
		t.Fatalf("namespace = %q, want the worker's %q", wfs.got.Namespace, "gooey-ns")
	}

	// The page's bound property now holds Temporal's canonical JSON:
	// protojson field names, exactly what every other Temporal tool
	// shows for the same response.
	got := h.out.Get()
	for _, want := range []string{
		`"workflowId": "order-7"`,
		`"runId": "run-1"`,
		`"name": "ProcessOrder"`,
		`"status": "WORKFLOW_EXECUTION_STATUS_RUNNING"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("page property is missing %s; got:\n%s", want, got)
		}
	}
	// Pagination survives the boundary too: protojson renders the
	// opaque token as base64, still round-trippable by a caller.
	if !strings.Contains(got, `"nextPageToken"`) {
		t.Errorf("next_page_token did not cross the property boundary; got:\n%s", got)
	}
}
