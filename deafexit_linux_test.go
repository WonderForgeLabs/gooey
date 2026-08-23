package gooey

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/prop"
)

// Run must REPORT a decoder that died under it, not merely stop.
//
// The change that made the death observable ended the loop on
// DecoderDone and handed the cause to fail(), which is the optional,
// drop-by-default WithErrorHandler hook — so a caller writing the
// ordinary `if err := app.Run(ctx); err != nil` saw nil and could not
// tell a terminal that stopped delivering keys from a clean Quit(). That
// is the same silent failure one layer up: no longer a hang, but still
// an exit that says nothing.
//
// The assertion is deliberately on the value Run RETURNS, with no error
// handler installed, because that is the only channel every caller has.
func TestRunReportsDecoderDeath(t *testing.T) {
	root := &label{text: prop.NewSource("hi")}
	app, tty := newTestApp(t, root)

	done := start(t, app)
	// A painted frame proves the decoder is up before it is killed: a
	// decoder that never started would end the loop for the same reason
	// and pass this test for the wrong one.
	tty.waitForFrame(t)

	tty.master.Close() // the far end of the pty goes away under the read

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil after the decoder died mid-run; a caller cannot distinguish this from a clean Quit")
		}
		if !strings.Contains(err.Error(), "terminal input stopped") {
			t.Errorf("Run returned %q, want it to name the terminal input as stopped", err)
		}
		// The read error travels with it rather than being flattened
		// into prose, so a caller can errors.Is it — which errno a dead
		// pty produces is the kernel's business (EOF here, EIO on a
		// real terminal), so the assertion is that a cause is wrapped at
		// all, not which one.
		if errors.Unwrap(err) == nil {
			t.Errorf("Run returned %v with nothing wrapped; the decoder's read error must survive to the caller", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the run loop did not end after the decoder died")
	}
}

// The other side of the same contract: an ordinary quit still returns
// nil. Without this, "always return something non-nil" would pass the
// test above and break every caller that checks Run's error.
func TestRunStillReturnsNilOnQuit(t *testing.T) {
	root := &label{text: prop.NewSource("hi")}
	app, tty := newTestApp(t, root)

	done := start(t, app)
	tty.waitForFrame(t)
	app.Quit()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v after Quit, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Quit did not end the run loop")
	}
}
