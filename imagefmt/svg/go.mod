// SVG rasterization is a SEPARATE MODULE on purpose: turning vector
// paths into pixels needs a real renderer (oksvg + rasterx), and
// third-party dependencies do not belong in a TUI framework's core
// graph. Core gooey's imaging registry knows nothing about SVG; an app
// opts in with a blank import of this module, whose init registers the
// format:
//
//	import _ "github.com/WonderForgeLabs/gooey/imagefmt/svg"
//
// `go build ./...` and `go test ./...` at the repo root skip this
// directory entirely — nested modules are excluded from the parent —
// which is the mechanical proof that core decodes images without it.
module github.com/WonderForgeLabs/gooey/imagefmt/svg

go 1.25.6

require (
	github.com/WonderForgeLabs/gooey v0.0.0-20260822170725-f67f0f6cff61
	github.com/srwiley/oksvg v0.0.0-20221011165216-be6e8873101c
	github.com/srwiley/rasterx v0.0.0-20220730225603-2ab79fcdd4ef
)

require (
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

replace github.com/WonderForgeLabs/gooey => ../..
