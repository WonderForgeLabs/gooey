package visibility

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The conveniences are sugar over the proto-true core: scalar arguments
// in, protojson text out, one core activity per convenience. These
// tests pin the sugar itself — argument parsing, the request each one
// builds, and the canonical-JSON result — the core RPC behavior is
// already pinned by visibility_test.go.

func TestQueryBuildsTheListRequestFromScalars(t *testing.T) {
	a, wfs, _ := harness()
	token := base64.StdEncoding.EncodeToString([]byte("opaque"))

	if _, err := a.Query(context.Background(), `ExecutionStatus="Running"`, "25", token); err != nil {
		t.Fatal(err)
	}
	req := wfs.gotList
	if req.Query != `ExecutionStatus="Running"` {
		t.Errorf("query = %q, want the caller's", req.Query)
	}
	if req.PageSize != 25 {
		t.Errorf("page size = %d, want 25", req.PageSize)
	}
	if string(req.NextPageToken) != "opaque" {
		t.Errorf("page token = %q, want the decoded bytes", req.NextPageToken)
	}
	if req.Namespace != DefaultNamespace {
		t.Errorf("namespace = %q, want defaulted like the core activity", req.Namespace)
	}
}

// Empty scalars are the zero request: server-default page size, first
// page.
func TestQueryEmptyScalarsMeanServerDefaults(t *testing.T) {
	a, wfs, _ := harness()
	if _, err := a.Query(context.Background(), "", "", ""); err != nil {
		t.Fatal(err)
	}
	if wfs.gotList.PageSize != 0 || wfs.gotList.NextPageToken != nil {
		t.Fatalf("empty scalars built %+v, want the zero request", wfs.gotList)
	}
}

// The result is Temporal's canonical JSON — protojson field names, the
// same rendering every other Temporal tool shows — and the page token
// round-trips: the base64 text in the result is exactly what Query
// accepts back as its pageToken argument.
func TestQueryResultIsCanonicalJSONAndTheTokenRoundTrips(t *testing.T) {
	a, wfs, _ := harness()
	wfs.listResp = &workflowservice.ListWorkflowExecutionsResponse{
		Executions: []*workflow.WorkflowExecutionInfo{{
			Execution: &common.WorkflowExecution{WorkflowId: "order-7", RunId: "run-1"},
			Type:      &common.WorkflowType{Name: "ProcessOrder"},
			Status:    enums.WORKFLOW_EXECUTION_STATUS_RUNNING,
			StartTime: timestamppb.New(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)),
		}},
		NextPageToken: []byte("page-2"),
	}
	out, err := a.Query(context.Background(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var m struct {
		Executions []struct {
			Execution struct {
				WorkflowID string `json:"workflowId"`
			} `json:"execution"`
			Type struct {
				Name string `json:"name"`
			} `json:"type"`
			Status    string `json:"status"`
			StartTime string `json:"startTime"`
		} `json:"executions"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, out)
	}
	if len(m.Executions) != 1 {
		t.Fatalf("executions = %d, want 1", len(m.Executions))
	}
	e := m.Executions[0]
	if e.Execution.WorkflowID != "order-7" || e.Type.Name != "ProcessOrder" ||
		e.Status != "WORKFLOW_EXECUTION_STATUS_RUNNING" || !strings.HasPrefix(e.StartTime, "2026-08-10T12:00:00") {
		t.Fatalf("canonical fields are wrong: %+v", e)
	}

	// The round trip: feed the result's token straight back.
	if _, err := a.Query(context.Background(), "", "", m.NextPageToken); err != nil {
		t.Fatal(err)
	}
	if string(wfs.gotList.NextPageToken) != "page-2" {
		t.Fatalf("round-tripped token = %q, want the server's bytes back verbatim", wfs.gotList.NextPageToken)
	}
}

// Bad scalars are the caller's mistake, and retrying will not fix them.
func TestQueryRejectsBadScalarsAsNonRetryable(t *testing.T) {
	a, wfs, _ := harness()
	for name, call := range map[string]func() error{
		"page size not a number": func() error { _, err := a.Query(context.Background(), "", "many", ""); return err },
		"negative page size":     func() error { _, err := a.Query(context.Background(), "", "-1", ""); return err },
		"token not base64":       func() error { _, err := a.Query(context.Background(), "", "", "!!!"); return err },
		"describe without id":    func() error { _, err := a.Describe(context.Background(), "", ""); return err },
	} {
		err := call()
		var appErr *temporal.ApplicationError
		if !errors.As(err, &appErr) || !appErr.NonRetryable() {
			t.Errorf("%s: err = %v, want a non-retryable ApplicationError", name, err)
		}
	}
	if wfs.gotList != nil {
		t.Fatal("a rejected argument must not reach the RPC")
	}
}

func TestCountBuildsTheRequestAndRendersTheCount(t *testing.T) {
	a, wfs, _ := harness()
	out, err := a.Count(context.Background(), `WorkflowType="Order"`)
	if err != nil {
		t.Fatal(err)
	}
	if wfs.gotCount.Query != `WorkflowType="Order"` || wfs.gotCount.Namespace != DefaultNamespace {
		t.Fatalf("request = %+v", wfs.gotCount)
	}
	var m struct {
		Count string `json:"count"`
	}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, out)
	}
	// protojson renders int64 as a JSON string — part of the canonical
	// rendering, so it is pinned here where dashboard code will parse it.
	if m.Count != "42" {
		t.Fatalf("count = %q, want the fake's 42 as a protojson string", m.Count)
	}
}

func TestDescribeBuildsTheExecution(t *testing.T) {
	a, wfs, _ := harness()
	if _, err := a.Describe(context.Background(), "order-7", "run-1"); err != nil {
		t.Fatal(err)
	}
	if wfs.gotDescribe.Execution.GetWorkflowId() != "order-7" || wfs.gotDescribe.Execution.GetRunId() != "run-1" {
		t.Fatalf("execution = %+v", wfs.gotDescribe.Execution)
	}
	// An empty run ID means the latest run — legal, common, passed through.
	if _, err := a.Describe(context.Background(), "order-8", ""); err != nil {
		t.Fatal(err)
	}
	if wfs.gotDescribe.Execution.GetWorkflowId() != "order-8" || wfs.gotDescribe.Execution.GetRunId() != "" {
		t.Fatalf("execution = %+v", wfs.gotDescribe.Execution)
	}
}

// RPC errors pass through the sugar untouched, like everything else.
func TestConvenienceErrorsPropagate(t *testing.T) {
	a, wfs, _ := harness()
	boom := errors.New("visibility store unavailable")
	wfs.err = boom
	if _, err := a.Query(context.Background(), "", "", ""); !errors.Is(err, boom) {
		t.Fatalf("Query err = %v, want the RPC's", err)
	}
	if _, err := a.Count(context.Background(), ""); !errors.Is(err, boom) {
		t.Fatalf("Count err = %v, want the RPC's", err)
	}
	if _, err := a.Describe(context.Background(), "x", ""); !errors.Is(err, boom) {
		t.Fatalf("Describe err = %v, want the RPC's", err)
	}
}
