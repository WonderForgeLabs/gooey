// Package prop is the POC of gooey's dependency-property system.
//
// Unlike WPF's DependencyProperty (eager: Set → change callbacks fire
// immediately through the tree), this is a lazy dirty-tracking graph in
// the Slint lineage:
//
//   - Source properties hold a value. Set marks dependents dirty; it
//     computes nothing.
//   - Computed properties hold a function. Get evaluates only if dirty,
//     and *evaluation itself records dependencies*: any property read
//     during compute() adds an edge from that property to this one.
//     Re-evaluation re-records, so conditional reads (if mode { a }
//     else { b }) track exactly the branch that was taken.
//   - Invalidation propagates dirty flags up the dependent graph and
//     fires OnInvalidate hooks (the render scheduler); values are
//     recomputed on demand at frame time, so updates batch per frame
//     for free.
//
// Properties are confined to the UI goroutine; no locking.
package prop

type node struct {
	dirty      bool
	dependents map[*node]struct{}
	deps       []*node
	onInvalid  func()
}

// evalStack is the active-evaluation stack: reads record an edge to the
// computed property currently on top.
var evalStack []*node

func (n *node) recordRead() {
	if len(evalStack) == 0 {
		return
	}
	top := evalStack[len(evalStack)-1]
	if top == n {
		return
	}
	if n.dependents == nil {
		n.dependents = make(map[*node]struct{})
	}
	n.dependents[top] = struct{}{}
	top.deps = append(top.deps, n)
}

func (n *node) invalidate() {
	if n.dirty {
		return
	}
	n.dirty = true
	if n.onInvalid != nil {
		n.onInvalid()
	}
	for d := range n.dependents {
		d.invalidate()
	}
}

type Property[T any] struct {
	n       node
	value   T
	compute func() T
	evals   int
}

// NewSource creates a settable value property.
func NewSource[T any](v T) *Property[T] {
	return &Property[T]{value: v}
}

// NewComputed creates a derived property. It starts dirty and evaluates
// on first Get.
func NewComputed[T any](f func() T) *Property[T] {
	p := &Property[T]{compute: f}
	p.n.dirty = true
	return p
}

// Get returns the current value, recomputing if dirty. Reads made
// during an enclosing computed evaluation are recorded as dependencies.
func (p *Property[T]) Get() T {
	p.n.recordRead()
	if p.compute != nil && p.n.dirty {
		// Detach from previous dependencies before re-recording.
		for _, d := range p.n.deps {
			delete(d.dependents, &p.n)
		}
		p.n.deps = p.n.deps[:0]
		evalStack = append(evalStack, &p.n)
		p.value = p.compute()
		evalStack = evalStack[:len(evalStack)-1]
		p.n.dirty = false
		p.evals++
	}
	return p.value
}

// Set assigns a source property's value and invalidates dependents.
func (p *Property[T]) Set(v T) {
	if p.compute != nil {
		panic("prop: Set on computed property")
	}
	p.value = v
	for d := range p.n.dependents {
		d.invalidate()
	}
}

// OnInvalidate registers a hook fired when this property transitions
// clean → dirty (typically the render scheduler).
func (p *Property[T]) OnInvalidate(fn func()) { p.n.onInvalid = fn }

// Evals reports how many times a computed property has evaluated —
// exposed so the POC can prove laziness.
func (p *Property[T]) Evals() int { return p.evals }
