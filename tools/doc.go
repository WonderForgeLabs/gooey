// Package tools exists to own buf's dependency graph.
//
// buf runs as `go tool buf` so that it resolves through the workspace's
// vendor/ instead of the module proxy — see the `contract` job in
// .github/workflows/ci.yml for the measurement that motivated it. But a
// `tool` directive drags the tool's ENTIRE dependency graph into the
// go.mod that holds it, as `// indirect` requires, and MVS hands those
// to every consumer of that module. In the root go.mod that meant
// importing gooey obliged you to buf, Docker's CLI, quic-go, cel-go and
// ~90 more, and forced upgrades of anything you shared with them.
//
// So the directive lives here, in a module nobody imports. go.work
// includes this directory, so `go work vendor` still vendors buf into
// the one root vendor/ and CI still runs it with the network off.
//
// This package has no code. It exists because a Go module needs at
// least one package for `go vet ./...` to have something to say, and
// because a bare go.mod with a tool directive and no explanation is the
// kind of file somebody deletes.
package tools
