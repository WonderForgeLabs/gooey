// Package viewport is the seam a control-plane act uses to change a
// running app's size.
//
// It exists because the two requirements pull against each other:
// App.resized must stay unexported — the viewport is not something an
// embedding program should re-target behind the framework's back — while
// control, grpc and mcp all need to reach it. Go has no "protected", and
// an unexported method is unreachable from any other package, so the only
// way to have both is for package gooey to hand a closure over the wall.
// internal/ is the wall: every module whose path starts with
// github.com/WonderForgeLabs/gooey/ may import this package, and nothing
// else can, enforced by the compiler rather than by convention.
//
// Resize takes the host as any because this package cannot name *App
// without an import cycle — gooey imports it, not the other way round.
package viewport

import "errors"

// ErrNotResizable reports a host that cannot have its viewport changed.
// It is not an internal error: control.NewService accepts any Host, and
// composer-only hosts that own no App are a legitimate configuration.
var ErrNotResizable = errors.New("viewport: host does not support resize")

// Resize re-targets host's composition at cols x rows. It is installed by
// package gooey at init; until gooey is linked in it reports
// ErrNotResizable rather than panicking on a nil call.
var Resize = func(host any, cols, rows int) error { return ErrNotResizable }
