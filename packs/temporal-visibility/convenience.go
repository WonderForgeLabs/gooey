package visibility

// The CONVENIENCE layer: three activities that take scalars and return
// protojson text. The proto-true activities in visibility.go are the
// pack's contract; these are sugar over them for callers whose
// arguments and results must cross as scalars — first among them gooey
// markup, whose {{temporal:Activity …}} expressions pass each argument
// as one string and deliver the result into a string property.
//
// Both halves of that constraint are load-bearing:
//
//   - INPUT: a markup argument is a string read from a property handle
//     at invoke time. A JSON string payload cannot deserialize into a
//     proto request message, so an activity that wants a request built
//     from markup must take the fields as scalars and build the proto
//     itself.
//   - OUTPUT: the markup provider decodes results into `any`, and the
//     SDK's proto-JSON payload converter refuses interface-typed
//     targets (ErrValuePtrMustConcreteType). A proto-returning
//     activity therefore cannot deliver its result through that path
//     at all; protojson TEXT, returned as a string, crosses as an
//     ordinary json/plain payload and arrives verbatim.
//
// Each convenience wraps exactly one core activity — same RPC, same
// namespace defaulting, same verbatim token pass-through — and its
// result is the protojson rendering of the same response the core
// activity returns, so the field names on the wire match Temporal's
// canonical JSON everywhere else. The page token crosses as base64
// text, which is exactly how protojson renders the bytes field, so a
// caller can lift `nextPageToken` from one result and pass it straight
// back as an argument.

import (
	"context"
	"encoding/base64"
	"strconv"

	"go.temporal.io/api/common/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// The registered names of the convenience activities. Where the
// proto-true names end in Temporal's own RPC names, these are short
// verbs — the two surfaces are deliberately impossible to confuse.
const (
	NameQuery    = "visibility.Query"
	NameCount    = "visibility.Count"
	NameDescribe = "visibility.Describe"
)

// invalidArgument is the error type carried by rejections of scalar
// arguments. Non-retryable: a page size that is not a number will not
// become one on the next attempt.
const invalidArgument = "InvalidArgument"

// Query is ListWorkflowExecutions with scalar arguments: a visibility
// query, a page size ("" or "0" for the server default), and a page
// token as base64 text ("" for the first page — pass a result's
// `nextPageToken` verbatim to fetch the page after it). The result is
// the ListWorkflowExecutionsResponse as protojson.
func (a *Activities) Query(ctx context.Context, query, pageSize, pageToken string) (string, error) {
	size, err := parsePageSize(pageSize)
	if err != nil {
		return "", err
	}
	token, err := parsePageToken(pageToken)
	if err != nil {
		return "", err
	}
	resp, err := a.ListWorkflowExecutions(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		Query:         query,
		PageSize:      size,
		NextPageToken: token,
	})
	if err != nil {
		return "", err
	}
	return marshalJSON(resp)
}

// Count is CountWorkflowExecutions with a scalar argument: the same
// visibility query language, no grouping. The result is the
// CountWorkflowExecutionsResponse as protojson (note that protojson
// renders the int64 count as a JSON string).
func (a *Activities) Count(ctx context.Context, query string) (string, error) {
	resp, err := a.CountWorkflowExecutions(ctx, &workflowservice.CountWorkflowExecutionsRequest{
		Query: query,
	})
	if err != nil {
		return "", err
	}
	return marshalJSON(resp)
}

// Describe is DescribeWorkflowExecution with scalar arguments: a
// workflow ID and an optional run ID ("" for the latest run). The
// result is the DescribeWorkflowExecutionResponse as protojson.
func (a *Activities) Describe(ctx context.Context, workflowID, runID string) (string, error) {
	if workflowID == "" {
		return "", temporal.NewNonRetryableApplicationError(
			"visibility.Describe needs a workflow ID", invalidArgument, nil)
	}
	resp, err := a.DescribeWorkflowExecution(ctx, &workflowservice.DescribeWorkflowExecutionRequest{
		Execution: &common.WorkflowExecution{WorkflowId: workflowID, RunId: runID},
	})
	if err != nil {
		return "", err
	}
	return marshalJSON(resp)
}

func parsePageSize(s string) (int32, error) {
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < 0 {
		return 0, temporal.NewNonRetryableApplicationError(
			"visibility: page size "+strconv.Quote(s)+" is not a non-negative number", invalidArgument, err)
	}
	return int32(n), nil
}

func parsePageToken(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, temporal.NewNonRetryableApplicationError(
			"visibility: page token is not base64 (pass a result's nextPageToken verbatim)", invalidArgument, err)
	}
	return b, nil
}

func marshalJSON(m proto.Message) (string, error) {
	b, err := protojson.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
