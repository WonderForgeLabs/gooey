package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

// The screens themselves are worker-side assets. The workflow never
// contains a line of markup: it asks LoadStageMarkup for the stage it is
// entering and hands the answer to whoever queries. That keeps the UI on
// the same footing as every other piece of the application — something an
// activity produced — and it means redesigning a screen is a worker
// deploy, not a client release.
//
//go:embed ui
var uiFS embed.FS

// Request is the thing being provisioned. Codes, not labels: the display
// strings live in the UI values map, and these are what activities act on.
type Request struct {
	Tier   string `json:"tier"`
	Region string `json:"region"`
}

// Choice is DescribeChoice's answer: the normalized value plus the words
// the screen should use for it. Even the label on the summary line came
// from a worker.
type Choice struct {
	Field  string `json:"field"`
	Value  string `json:"value"`
	Label  string `json:"label"`
	Notice string `json:"notice"`
}

// Validation is the gate between stage one and stage two.
type Validation struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// Quote is what the review screen shows.
type Quote struct {
	Estimate string `json:"estimate"`
	Lead     string `json:"lead"`
}

// StepResult is one provisioning step's outcome. Worker and Attempt are
// there to be visible on screen: proof the line changed because something
// ran somewhere else, not because the terminal decided to animate.
type StepResult struct {
	Detail  string `json:"detail"`
	Ticket  string `json:"ticket,omitempty"`
	Worker  string `json:"worker"`
	Attempt int32  `json:"attempt"`
}

var tiers = map[string]string{
	"small":  "small — 2 vCPU / 4 GiB",
	"medium": "medium — 4 vCPU / 16 GiB",
	"large":  "large — 16 vCPU / 64 GiB",
}

var regions = map[string]string{
	"us-east":  "us-east — Ashburn",
	"eu-west":  "eu-west — Dublin",
	"ap-south": "ap-south — Mumbai",
}

var prices = map[string]string{
	"small":  "$38/mo",
	"medium": "$150/mo",
	"large":  "$610/mo",
}

// LoadStageMarkup returns the gooey source for one stage of the wizard.
//
// The stage name is treated as untrusted even though the only caller is
// the workflow: this activity serves UI to whatever renders it, and a
// stage name is a lookup key, never a path.
func LoadStageMarkup(ctx context.Context, stage string) (string, error) {
	if !isStageName(stage) {
		return "", temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("bad stage name %q", stage), "BadStage", nil)
	}
	b, err := uiFS.ReadFile("ui/stage-" + stage + ".gooey")
	if err != nil {
		return "", temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("no markup for stage %q", stage), "UnknownStage", err)
	}
	return string(b), nil
}

func isStageName(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// DescribeChoice turns a raw button press into the words the screen will
// show for it. Trivial work, deliberately placed out here: the terminal
// sent `tier` and `large`, and everything those two strings MEAN was
// decided by a worker.
func DescribeChoice(ctx context.Context, field, value string) (Choice, error) {
	var table map[string]string
	switch field {
	case "tier":
		table = tiers
	case "region":
		table = regions
	default:
		return Choice{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("unknown field %q", field), "UnknownField", nil)
	}
	label, ok := table[value]
	if !ok {
		return Choice{}, temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("%q is not a %s this catalog offers", value, field), "UnknownValue", nil)
	}
	return Choice{
		Field:  field,
		Value:  value,
		Label:  label,
		Notice: fmt.Sprintf("%s set to %s", field, label),
	}, nil
}

// ValidateRequest is the gate the continue button hits. It answers with a
// verdict rather than an error, because "you have not picked a region
// yet" is a normal outcome of a wizard, not a failure of the system.
func ValidateRequest(ctx context.Context, req Request) (Validation, error) {
	if err := pause(ctx, 400*time.Millisecond); err != nil {
		return Validation{}, err
	}
	var missing []string
	if req.Tier == "" {
		missing = append(missing, "a size")
	}
	if req.Region == "" {
		missing = append(missing, "a region")
	}
	if len(missing) > 0 {
		return Validation{Message: "still need " + strings.Join(missing, " and ")}, nil
	}
	if req.Tier == "large" && req.Region == "ap-south" {
		return Validation{Message: "large is not available in ap-south — pick another size or region"}, nil
	}
	return Validation{OK: true, Message: "validated on " + workerName() + " — continuing to review"}, nil
}

// PriceRequest is why the review screen has numbers on it.
func PriceRequest(ctx context.Context, req Request) (Quote, error) {
	if err := pause(ctx, 500*time.Millisecond); err != nil {
		return Quote{}, err
	}
	lead := "about 4 minutes"
	if req.Tier == "large" {
		lead = "about 20 minutes"
	}
	return Quote{Estimate: prices[req.Tier], Lead: lead}, nil
}

// ReserveCapacity, ProvisionResource and NotifyOwner are the three steps
// the third screen watches. They take the same arguments so the workflow
// can drive them from one loop; the sleeps stand in for work that would
// really take this long.

func ReserveCapacity(ctx context.Context, req Request, ticket string) (StepResult, error) {
	if err := pause(ctx, 1200*time.Millisecond); err != nil {
		return StepResult{}, err
	}
	return step(ctx, fmt.Sprintf("%s capacity held in %s", req.Tier, req.Region), ""), nil
}

func ProvisionResource(ctx context.Context, req Request, ticket string) (StepResult, error) {
	if err := pause(ctx, 1500*time.Millisecond); err != nil {
		return StepResult{}, err
	}
	info := activity.GetInfo(ctx)
	// Derived from the workflow run, so a retry of this activity produces
	// the same ticket — the at-least-once story again.
	id := fmt.Sprintf("PRV-%s-%s", strings.ToUpper(req.Region), shortID(info.WorkflowExecution.RunID))
	return step(ctx, "instance created", id), nil
}

func NotifyOwner(ctx context.Context, req Request, ticket string) (StepResult, error) {
	if err := pause(ctx, 800*time.Millisecond); err != nil {
		return StepResult{}, err
	}
	return step(ctx, "owner notified about "+ticket, ""), nil
}

func step(ctx context.Context, detail, ticket string) StepResult {
	info := activity.GetInfo(ctx)
	return StepResult{
		Detail:  detail,
		Ticket:  ticket,
		Worker:  workerName(),
		Attempt: info.Attempt,
	}
}

// pause sleeps but stays cancellable, so a terminated workflow does not
// leave the worker holding a slot for the rest of the nap.
func pause(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func workerName() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("%s pid %d", host, os.Getpid())
}

func shortID(s string) string {
	s = strings.ReplaceAll(s, "-", "")
	if len(s) > 6 {
		s = s[:6]
	}
	return strings.ToUpper(s)
}
