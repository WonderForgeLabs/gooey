// Package slugify is the compute half of the temporaldemo pair: one
// activity and the registration that serves it.
//
// Nothing here knows a UI exists. The demo's markup names "Slugify" as a
// string and a worker happens to serve that name on the same queue —
// that indirection is the whole point. Run more copies, anywhere with a
// route to the server, and the terminal's button starts fanning out
// across them.
//
// It is a package rather than a main so that the same registration can
// be either its own process (workers/temporalworker) or a companion of the
// UI that calls it (cmd/temporaldemo --with-worker). The activity cannot
// tell the difference, which is the point being made twice.
package slugify

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// DefaultTaskQueue is the queue temporaldemo's markup reaches.
const DefaultTaskQueue = "gooey-demo"

// Result is what the activity returns. It is deliberately a struct, not
// a string: the provider does not know the activity's return type
// (markup named it, so Go never saw it), so a composite result comes
// back as JSON — and the worker fields are visible proof in the UI that
// the work happened out there rather than in the terminal.
//
// When the worker is a companion of the UI, "out there" is the same
// process, and the fields say so — same host, same pid. That is not a
// cheat: the call still went to the server and came back through a task
// queue, and pointing the demo at a worker on another machine changes
// nothing but those two strings.
type Result struct {
	Slug      string `json:"slug"`
	Worker    string `json:"worker"`
	TaskQueue string `json:"taskQueue"`
	Attempt   int32  `json:"attempt"`
	At        string `json:"at"`
}

// Slugify is an ordinary activity function: the same code would run
// unchanged from inside a workflow. Standalone execution is a property
// of how it is *invoked*, not of how it is written.
func Slugify(ctx context.Context, in string) (Result, error) {
	info := activity.GetInfo(ctx)
	host, _ := os.Hostname()

	var b strings.Builder
	prevDash := true // leading dashes are suppressed
	for _, r := range strings.ToLower(in) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		case !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return Result{}, fmt.Errorf("nothing sluggable in %q", in)
	}

	return Result{
		Slug:      slug,
		Worker:    host + " pid " + fmt.Sprint(os.Getpid()),
		TaskQueue: info.TaskQueue,
		Attempt:   info.Attempt,
		At:        time.Now().Format("15:04:05"),
	}, nil
}

// Run serves Slugify until ctx is done. Registered under its function
// name, which is the string the markup asks for.
func Run(ctx context.Context, c client.Client, taskQueue string) error {
	if taskQueue == "" {
		taskQueue = DefaultTaskQueue
	}
	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterActivity(Slugify)

	interrupt := make(chan interface{})
	returned := make(chan struct{})
	defer close(returned)
	go func() {
		select {
		case <-ctx.Done():
			close(interrupt)
		case <-returned:
		}
	}()
	return w.Run(interrupt)
}
