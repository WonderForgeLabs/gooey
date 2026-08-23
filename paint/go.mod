// paint is a SEPARATE MODULE because it depends on a real 2D graphics
// library — github.com/fogleman/gg, which brings freetype with it — and
// core gooey has exactly two direct requirements by doctrine
// (docs/specs/2026-08-10-pack-distribution.md). Root `go build ./...` and
// `go test ./...` skip this directory, which is the mechanical proof that
// core still draws without any of it.
//
// The dependency is the whole point rather than a cost to minimise: gg
// already has brushes, gradients, pens, caps, joins and dashes, and
// writing a second one inside core would be worse in every way than
// taking it here.
//
// Run this module's checks explicitly:
//
//	cd paint && go vet ./... && go test ./...
module github.com/WonderForgeLabs/gooey/paint

go 1.25.6

require (
	github.com/WonderForgeLabs/gooey v0.0.0-20260822170725-f67f0f6cff61
	github.com/fogleman/gg v1.3.0
)

require (
	github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/term v0.45.0 // indirect
)

replace github.com/WonderForgeLabs/gooey => ../
