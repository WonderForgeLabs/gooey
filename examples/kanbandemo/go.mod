// kanbandemo is a SEPARATE MODULE for the same reason mcp/cmd/mcpdemo
// lives inside mcp/: it imports github.com/WonderForgeLabs/gooey/mcp,
// which brings the MCP SDK's dependency graph (jsonschema-go,
// segmentio/{asm,encoding}, uritemplate, x/{oauth2,time}) that core
// gooey does not carry. `go build ./...` and `go test ./...` at the repo
// root skip this directory, which is the mechanical proof that core
// still builds without any of it.
module github.com/WonderForgeLabs/gooey/examples/kanbandemo

go 1.25.6

require (
	github.com/WonderForgeLabs/gooey v0.0.0
	github.com/WonderForgeLabs/gooey/grpc v0.0.0
	github.com/WonderForgeLabs/gooey/mcp v0.0.0
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/modelcontextprotocol/go-sdk v1.7.0 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/grpc v1.82.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/WonderForgeLabs/gooey => ../../
	github.com/WonderForgeLabs/gooey/grpc => ../../grpc
	github.com/WonderForgeLabs/gooey/mcp => ../../mcp
)
