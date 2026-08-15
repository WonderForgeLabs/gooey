// gitui is a SEPARATE MODULE for the same reason apps/kanban
// is: it imports github.com/WonderForgeLabs/gooey/handlers/exec, which
// is itself a nested module because structured extraction takes gojq —
// a real third-party dependency the TUI framework's own graph must not
// carry. `go build ./...` and `go test ./...` at the repo root skip
// this directory, which is the mechanical proof that core gooey still
// builds without any of it.
module github.com/WonderForgeLabs/gooey/apps/gitui

go 1.25.6

require (
	github.com/WonderForgeLabs/gooey v0.0.0
	github.com/WonderForgeLabs/gooey/handlers/exec v0.0.0
)

require (
	github.com/itchyny/gojq v0.12.17 // indirect
	github.com/itchyny/timefmt-go v0.1.6 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
)

replace (
	github.com/WonderForgeLabs/gooey => ../../
	github.com/WonderForgeLabs/gooey/handlers/exec => ../../handlers/exec
)
