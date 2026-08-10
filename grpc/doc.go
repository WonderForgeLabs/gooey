// Package grpc is gooey's control-plane wire module — a SEPARATE
// MODULE on purpose, exactly like mcp/ and handlers/temporal: grpc-go
// and protobuf are quarantined here so the root framework's dependency
// graph stays small. `go build ./...` at the repo root never sees this
// directory.
//
// gen/gooey/control/v1 holds the Go generated from
// proto/gooey/control/v1 (package controlv1, import path pinned by
// go_package). It is committed; regeneration is an explicit act:
//
//	go run github.com/bufbuild/buf/cmd/buf@v1.72.0 generate --template proto/buf.gen.yaml
//
// run from the repo root. CI enforces no drift. The server
// implementation (issue #111) lands in this module beside gen/.
package grpc
