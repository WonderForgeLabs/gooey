package gooey

// Startable is implemented by non-visual elements that own a background
// goroutine. The Composer discovers them while walking the tree and
// starts them when the composition goes live; Composer.Close stops them.
//
// The post parameter is the ONLY way a started element may reach the
// property graph: it queues a func onto the UI goroutine (Dispatcher.Post).
// A Startable that touches properties from its own goroutine violates
// UI-goroutine confinement, and nothing in the framework will catch it —
// the properties are unlocked by design.
type Startable interface {
	Start(post func(func())) (stop func())
}
