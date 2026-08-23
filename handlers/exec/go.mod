// The exec handler namespace is a SEPARATE MODULE on purpose — the
// same deps⇒nested rule that quarantines handlers/temporal. Structured
// extraction takes github.com/itchyny/gojq (pure Go, but a real
// dependency), and none of that belongs in the TUI framework's own
// graph. Core gooey stays at golang.org/x/term; an app opts into this
// module only when it wants markup to run local commands.
//
// `go build ./...` and `go test ./...` at the repo root skip this
// directory entirely — nested modules are excluded from the parent —
// which is the mechanical proof that core builds without gojq.
module github.com/WonderForgeLabs/gooey/handlers/exec

go 1.25.6

require (
	github.com/WonderForgeLabs/gooey v0.0.0-20260822170725-f67f0f6cff61
	github.com/itchyny/gojq v0.12.17
)

require (
	github.com/itchyny/timefmt-go v0.1.6 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
)

replace github.com/WonderForgeLabs/gooey => ../..
