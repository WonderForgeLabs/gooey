// wysiwyg is a SEPARATE MODULE for the same reason apps/gitui and
// apps/kanban are: it imports
// github.com/WonderForgeLabs/gooey/grpc and .../mcp, which carry grpc,
// protobuf and the MCP SDK — real third-party dependencies the TUI
// framework's own graph must not take on. It needs BOTH directions of
// the control plane: grpc as a client (-attach, driving another app) and
// grpc + mcp as a server (-serve/-mcp, being driven, which is how the
// editor's own UI gets built). It also imports .../paint, and through it
// fogleman/gg and freetype, to draw the panel chrome — the same doctrine,
// which is why paint is a nested module too. Core gooey stays at
// golang.org/x/term and golang.org/x/image, and
// `go build ./...` / `go test ./...` at the repo root skipping this
// directory is the mechanical proof of it.
//
// Run this module's tests explicitly:
//
//	cd apps/wysiwyg && go test ./...
module github.com/WonderForgeLabs/gooey/apps/wysiwyg

go 1.25.6

require (
	github.com/WonderForgeLabs/gooey v0.0.0-20260822170725-f67f0f6cff61
	github.com/WonderForgeLabs/gooey/grpc v0.0.0
	github.com/WonderForgeLabs/gooey/imagefmt/svg v0.0.0-00010101000000-000000000000
	github.com/WonderForgeLabs/gooey/mcp v0.0.0-00010101000000-000000000000
	github.com/WonderForgeLabs/gooey/paint v0.0.0-00010101000000-000000000000
	github.com/fogleman/gg v1.3.0
	google.golang.org/grpc v1.83.1
)

require (
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/modelcontextprotocol/go-sdk v1.7.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/WonderForgeLabs/gooey => ../../
	github.com/WonderForgeLabs/gooey/grpc => ../../grpc
	github.com/WonderForgeLabs/gooey/imagefmt/svg => ../../imagefmt/svg
	github.com/WonderForgeLabs/gooey/mcp => ../../mcp
	github.com/WonderForgeLabs/gooey/paint => ../../paint
)
