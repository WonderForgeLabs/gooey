package gooey

// Dynamic is implemented by containers whose CHILD SET changes while the
// composition is live — a virtualized list realizing the rows it can
// actually see, and later anything else that builds its children from
// data rather than from markup.
//
// Until now the retained tree was structurally static: the Composer
// walked it once, gave every component a paint node, and the FocusManager
// built the input tree from the same walk. Replacing a tree meant
// replacing the whole composition (that is what hot reload does). A list
// cannot work that way — it does not know how many rows fit until it has
// been arranged, and it must not throw the composition away to add one.
//
// So the framework hands a Dynamic container a hook at build time. Call
// it after changing what ChildComponents() returns; the next Frame
// re-syncs paint nodes and the input tree BEFORE painting, and the sync
// KEEPS the node of every component that is still there, with its clean
// or dirty state intact. That is what preserves the damage guarantee
// across a structural change: realizing one new row paints one new row,
// not the tree.
//
// The hook may be called from Measure/Arrange — that is the point, since
// that is when a list learns its size — and it is safe to call when
// nothing actually changed; the sync is a diff.
type Dynamic interface{ SetStructureHook(func()) }
