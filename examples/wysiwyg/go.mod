// wysiwyg is a SEPARATE MODULE for the same reason examples/gitui and
// examples/kanbandemo are: it imports
// github.com/WonderForgeLabs/gooey/grpc, which carries grpc and
// protobuf — real third-party dependencies the TUI framework's own
// graph must not take on. Core gooey stays at golang.org/x/term, and
// `go build ./...` / `go test ./...` at the repo root skipping this
// directory is the mechanical proof of it.
//
// Run this module's tests explicitly:
//
//	cd examples/wysiwyg && go test ./...
module github.com/WonderForgeLabs/gooey/examples/wysiwyg

go 1.25.6

require (
	github.com/WonderForgeLabs/gooey v0.0.0
	github.com/WonderForgeLabs/gooey/grpc v0.0.0
	google.golang.org/grpc v1.82.1
)

require (
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/WonderForgeLabs/gooey => ../../
	github.com/WonderForgeLabs/gooey/grpc => ../../grpc
)
