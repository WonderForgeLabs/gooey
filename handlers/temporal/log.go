package temporalhandlers

import "go.temporal.io/sdk/log"

// NopLogger silences the Temporal SDK's default stderr logger.
//
// A gooey TUI owns the terminal: it is in raw mode on the alt screen,
// and anything the SDK prints to stderr — reconnect warnings, poller
// chatter — lands at the cursor and garbles the bottom rows of the UI.
// TUI clients pass this as client.Options.Logger. Headless workers
// should NOT use it; stderr is their UI.
var NopLogger log.Logger = nopLogger{}

type nopLogger struct{}

func (nopLogger) Debug(string, ...interface{}) {}
func (nopLogger) Info(string, ...interface{})  {}
func (nopLogger) Warn(string, ...interface{})  {}
func (nopLogger) Error(string, ...interface{}) {}
