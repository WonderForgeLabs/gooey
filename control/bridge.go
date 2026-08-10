package control

import (
	"fmt"
	"time"
)

// Bridge is the crossing between a transport's goroutines and the UI
// goroutine, and it is the only one: every operation a transport
// forwards to a Service goes through Do; nothing else may touch a
// component or a property.
//
// It is the MCP server's bridge promoted to the shared layer, because
// the settle barrier it implements is contract text now: every RPC
// "marshals the call onto the app's UI goroutine, waits for the settle
// barrier, and only then answers".
type Bridge struct {
	post    func(func())
	timeout time.Duration
}

// NewBridge builds a bridge that posts through post (Host.Post) and
// gives the UI goroutine timeout per round; timeout <= 0 means 5s.
func NewBridge(post func(func()), timeout time.Duration) *Bridge {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Bridge{post: post, timeout: timeout}
}

// Do runs fn on the UI goroutine and returns once the UI has SETTLED.
//
// It waits twice, and the second wait is the interesting one. The first
// is fn itself. The second is a bare barrier — a closure that does
// nothing but come back — and it exists because Dispatcher.Drain takes a
// snapshot of its queue: a closure posted while a drain is running lands
// in the NEXT drain, and the run loop composes a frame between two
// drains. So waiting for the barrier waits for the repaint that fn's
// Sets asked for. That is what lets a screen read immediately after a
// command invocation see the new pixels instead of the previous frame,
// and it is why the end-to-end proofs need no sleeps.
//
// A panic inside fn is recovered and returned as a *PanicError. A remote
// client must not be able to kill the app: without this, an operation
// that hit a nil handle would unwind through Drain, out of the run loop,
// and take the terminal with it.
//
// A round that outlives the timeout returns a *TimeoutError: the run
// loop is blocked or not running, and that is reported, never hung on.
func (b *Bridge) Do(fn func() error) error {
	err, ok := b.round(fn)
	if !ok {
		return &TimeoutError{Timeout: b.timeout}
	}
	if err != nil {
		return err
	}
	if _, ok := b.round(nil); !ok {
		return &TimeoutError{Timeout: b.timeout}
	}
	return nil
}

func (b *Bridge) round(fn func() error) (error, bool) {
	// Buffered so a closure that arrives after we gave up on it can still
	// complete and be collected instead of parking a goroutine forever.
	done := make(chan error, 1)
	b.post(func() {
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = &PanicError{Value: r}
			}
			done <- err
		}()
		if fn != nil {
			err = fn()
		}
	})
	timer := time.NewTimer(b.timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return err, true
	case <-timer.C:
		return nil, false
	}
}

// TimeoutError reports that the UI goroutine did not answer in time.
type TimeoutError struct{ Timeout time.Duration }

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("timed out after %s waiting for the UI goroutine: the app's run loop is blocked or not running", e.Timeout)
}

// PanicError reports that an operation panicked on the UI goroutine and
// was recovered before it could unwind the run loop.
type PanicError struct{ Value any }

func (e *PanicError) Error() string {
	return fmt.Sprintf("panic on the UI goroutine: %v", e.Value)
}
