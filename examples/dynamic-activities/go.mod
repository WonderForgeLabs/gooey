// dynamic-activities is a SEPARATE MODULE for the reason every demo that
// reaches outside the framework is: it imports gooey/mcp (the MCP SDK's
// graph), gooey/grpc (grpc-go + protobuf) and gooey/handlers/temporal
// (the Temporal SDK). None of that belongs in core gooey's dependency
// graph, and `go build ./...` at the repo root skips this directory,
// which is the mechanical proof that core still builds without it.
module github.com/WonderForgeLabs/gooey/examples/dynamic-activities

go 1.25.6

require (
	github.com/WonderForgeLabs/gooey v0.0.0
	github.com/WonderForgeLabs/gooey/grpc v0.0.0
	github.com/WonderForgeLabs/gooey/handlers/temporal v0.0.0
	github.com/WonderForgeLabs/gooey/mcp v0.0.0
	go.temporal.io/sdk v1.47.0
)

require (
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c // indirect
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef // indirect
)

require (
	github.com/WonderForgeLabs/gooey/imagefmt/svg v0.0.0
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/facebookgo/clock v0.0.0-20150410010913-600d898af40a // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.2 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.22.0 // indirect
	github.com/modelcontextprotocol/go-sdk v1.7.0 // indirect
	github.com/nexus-rpc/nexus-proto-annotations v0.1.0 // indirect
	github.com/nexus-rpc/sdk-go v0.6.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/robfig/cron v1.2.0 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.temporal.io/api v1.63.4 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/grpc v1.82.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/WonderForgeLabs/gooey => ../../
	github.com/WonderForgeLabs/gooey/grpc => ../../grpc
	github.com/WonderForgeLabs/gooey/handlers/temporal => ../../handlers/temporal
	github.com/WonderForgeLabs/gooey/mcp => ../../mcp
	github.com/WonderForgeLabs/gooey/packs/temporal-visibility => ../../packs/temporal-visibility
)

replace github.com/WonderForgeLabs/gooey/imagefmt/svg => ../../imagefmt/svg
