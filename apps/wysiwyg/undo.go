package main

import "strconv"

// Undo and redo, over the DOCUMENT MODEL.
//
// # The seam is rebuild(), and that is the whole design
//
// Every mutation the editor makes ends in ed.rebuild(): addSelected,
// deleteSelected, retype, commitEdit, cycleValue, the drag's release, and
// whatever is added next. It has to — without it the preview, the
// outline, the CODE tab and the build status all go stale, so a mutator
// that skips it is already broken four ways and visibly so.
//
// So undo is DERIVED there rather than declared at each call site.
// recordHistory runs at the top of rebuild, compares the live document
// against a baseline deep copy, and pushes the baseline when they differ.
//
// The alternative — a mutate(fn) wrapper every mutator must remember to
// call — fails SILENTLY when someone forgets: the edit lands, the screen
// is right, and the only symptom is that ctrl+z skips it. A user who
// learns that undo drops some of their edits is worse off than one who
// has no undo at all, because they trusted it. This is the same rule
// drag.go states for dragLive ("a rule enforced where it is READ cannot
// be forgotten by the next writer"), applied to writes instead of reads.
//
// Two consequences fall out for free rather than needing code:
//
//   - a rebuild that changed no document state records NOTHING, so a
//     selection change, a failed build, a hot reload of the editor's own
//     page, a copy-to-clipboard and a re-push in remote mode all cost no
//     undo step;
//   - a mutator added later is undoable without opting in.
//
// # What is undoable, and what is deliberately not
//
// UNDOABLE — anything that reaches the saved file. That is exactly the
// node tree: Elem, Attrs, Body, Kids and Slots. Add, delete, retype, an
// attribute edit, a value cycle, a body edit, a drag's committed
// position, a paste, a cut.
//
// NOT UNDOABLE, and each for a stated reason:
//
//   - THE SELECTION. It is not document state and never reaches the file.
//     It is RESTORED with a state (see snapshot.sel) so that undoing does
//     not leave you pointing at nothing, but ctrl+z does not walk the
//     selection backwards through ctrl+n presses.
//   - DESIGN vs LIVE. A view mode, not content. Undoing your way out of
//     LIVE mode would be a surprise, and the mode has its own toggle.
//   - WHICH PANE IS DOCKED WHERE, which row the property grid is on, what
//     is in the text input. Chrome. None of it is in the document, and
//     an undo stack that mixed the two would make "undo my last edit"
//     ambiguous — the failure people report about editors that do this.
//   - THE PLACEHOLDER VALUES markup.Seeded registers into ctx.Values when
//     an element is added. See restore for why re-registering them would
//     make redo DESTRUCTIVE rather than leak-free.
//
// The line is "does it reach the file". Everything on the undoable side
// of it is in node; nothing on the other side is.
//
// # ctrl+z is not SIGTSTP here, and taking it costs no suspend
//
// In a terminal ctrl+z is normally the SUSP character and the tty driver
// turns it into SIGTSTP. Not while a gooey app is up: term.Screen.Raw
// calls term.MakeRaw, which clears ISIG
// (vendor/golang.org/x/term/term_unix.go:34), so the driver generates no
// signal from it. The byte 0x1a lands on the tty like any other and
// input.Decode (input/decode.go:33) turns it into an ordinary
// ctrl+z KeyEvent — which, before this file, no gooey app bound and every
// gooey app therefore dropped.
//
// So binding it takes nothing away. App.Suspend (app.go:816) is a
// PROGRAMMATIC hand-off and is not reached by any keystroke, and
// App.onStop's SIGTSTP dance (signals_unix.go:70) still runs for a
// signal delivered from outside — `kill -TSTP`, or a shell's `stop`.
// Both are untouched by this file.
//
// REDO IS ctrl+y AND NOT ALSO ctrl+shift+z, and that is a fact about
// terminals rather than a preference. Shift is not reportable on a
// printable character — the terminal sends the shifted rune instead — so
// ctrl+shift+z arrives as the SAME byte 0x1a as ctrl+z, and the decoder
// lower-cases it deliberately ("ctrl+a, not ctrl+A: the shift is not
// real", input/decode.go:49). input.ParseGesture("ctrl+shift+z") parses
// happily and yields an event with ModShift set that no decode ever
// produces, so the binding would be UNFIREABLE: a line in the markup
// that looks like a feature, that every test asserting "the binding
// exists" would pass, and that can never run.
// TestCtrlShiftZIsUnfireableSoItIsNotBound is what keeps someone from
// adding it back as a kindness.

// DefaultHistoryLimit is how many undo steps are kept when -history is
// not given.
//
// The bound is the point rather than the number. An editing session is
// unbounded in length and each step holds a whole copy of the document,
// so an unbounded stack is a leak that grows for as long as the editor
// is open. 100 is the depth at which nobody notices the ceiling on a
// document small enough to hand-edit; -history is there for the person
// who does.
const DefaultHistoryLimit = 100

// snapshot is one recorded state of the document.
//
// It holds a deep copy of the WHOLE tree rather than a delta. That is the
// deliberate trade: a delta would be smaller, and it would need an
// inverse for every mutator — which is a rule the next mutator's author
// has to know about, i.e. exactly the silently-forgettable thing this
// file exists to avoid. The document is a few dozen nodes of short
// strings; a copy of it is cheap and it is total.
type snapshot struct {
	root *node
	// sel is the selection as an INDEX CHAIN from the surface down, and
	// it is a path rather than a *node because restoring swaps the whole
	// tree: a pointer recorded here would name a node in a tree nobody
	// holds any more. hasSel separates "the selection was here" from
	// "nothing was selected", which is a real state in this editor rather
	// than a missing value.
	sel    []int
	hasSel bool
	// label names the edit that PRODUCED this state, so undo can say what
	// it undid. Empty for a mutator that did not go through applyEdit,
	// and for the state the editor opens with.
	label string
	// key is WHAT the edit that produced this state touched — one
	// attribute of one node — and it is what lets a run of keystrokes
	// into the same field be one undo step instead of one per rune. Empty
	// means "not coalescable", which is every structural edit and every
	// edit that moved more than one attribute. See editKey.
	key string
}

// history is the undo and redo stacks plus the baseline they are
// measured against.
type history struct {
	// limit is the maximum number of undo steps. 0 disables undo
	// entirely; it is never negative (see setHistoryLimit).
	limit int
	// undo is oldest-first: the top of the stack is the last element.
	undo []snapshot
	// redo is bounded WITHOUT its own limit, and that is derived rather
	// than assumed. It grows only when undo pops, so it cannot exceed
	// the undo stack's depth at the moment of the first undo, which is
	// at most limit; and any recorded edit clears it. See
	// TestTheRedoStackIsBoundedByTheSameLimit.
	redo []snapshot
	// base is the state as of the last record — the one the live
	// document is compared against. Its root is owned by history and is
	// never the tree the editor is editing.
	base snapshot
	// started is false until the first rebuild, which establishes the
	// baseline without recording anything. A bool rather than base.root
	// == nil so that "the opening state" is a stated condition.
	started bool
	// pending is the label applyEdit attached to the edit in flight. It
	// is consumed by the next record, whether or not that record pushes.
	pending string
}

// history returns the editor's stacks, creating them on first use.
//
// Lazily rather than in newEditor so that every construction path gets
// one — the tests build editors directly, and so does anything added
// later. A nil history would mean an editor whose edits are silently not
// undoable, which is the failure this file is built to make impossible.
func (ed *editor) history() *history {
	if ed.hist == nil {
		ed.hist = &history{limit: DefaultHistoryLimit}
	}
	return ed.hist
}

// setHistoryLimit sets the maximum undo depth, evicting immediately if
// the new bound is smaller than what is already held.
//
// A negative limit is clamped to 0 rather than rejected here; main
// rejects it at the flag, which is where a person can be told about it.
func (ed *editor) setHistoryLimit(n int) {
	if n < 0 {
		n = 0
	}
	h := ed.history()
	h.limit = n
	h.evict()
}

// applyEdit is the OPTIONAL labelled form of an edit: it runs fn, calls
// rebuild, and names the resulting history entry so undo can say what it
// undid.
//
// It is a convenience and not the seam. Undo works identically for a
// mutator that does the same two things by hand, because the recording
// happens inside rebuild — the only thing a caller gains here is the
// label. It exists because two other slices asked for a named entry
// point and a label has to live somewhere.
func (ed *editor) applyEdit(label string, fn func()) {
	ed.history().pending = label
	fn()
	ed.rebuild()
}

// recordHistory is the hook, and it runs at the TOP of rebuild.
//
// The top rather than the bottom, for two reasons that are both about
// remote mode. rebuild RETURNS EARLY when the editor is driving another
// app (main.go, `if ed.remote != nil`), so a hook at the bottom would
// never run there and remote editing would have no undo at all. And
// fragmentFor (remotemode.go) temporarily renames the surface for the
// duration of the push, restoring it with a defer — a hook that ran
// while that rename was live would record a document the user never
// wrote.
// IT READS ed.sel AND MUST NEVER WRITE IT, and that is a cross-slice
// contract now rather than an implementation detail.
//
// The properties pane's caret editor commits live, so rebuild — and
// therefore this — runs on EVERY RUNE, and the editor retires itself when
// ed.sel stops being the node it opened on. Re-resolving the selection
// here, even to the "corresponding" node in an equivalent tree, would
// hand back a fresh pointer on every keystroke and retire the editor on
// the second character of every value anyone types. Their
// TestTypingDoesNotRetireTheEditorItIsTypingInto is that assertion from
// the other side; ours is
// TestAnUndoThatDoesNothingLeavesTheSelectionPointerALONE.
//
// restore is the ONLY writer of ed.sel in this file — checkable with
// `grep -n 'ed\.sel = ' undo.go`, which must report lines inside restore
// and nowhere else.
func (ed *editor) recordHistory() {
	sel, hasSel := ed.selPath()
	ed.history().record(ed.root, sel, hasSel)
}

func (h *history) record(root *node, sel []int, hasSel bool) {
	label := h.pending
	h.pending = ""

	if !h.started {
		// The state the editor opens with. It becomes the baseline and
		// nothing is pushed: there is no edit behind it to undo.
		h.started = true
		h.base = snapshot{root: root.clone(), sel: sel, hasSel: hasSel}
		return
	}
	// A rebuild that changed no document state is not an edit. This is
	// what makes selection changes, failed builds and re-pushes free,
	// and it is why no caller has to declare whether it mutated.
	if h.base.root.equal(root) {
		return
	}
	// Without this, undo restores the selection as it was when that state
	// was last RECORDED, and moving the selection does not record — so
	// selecting B and renaming it and pressing ctrl+z would put the rename
	// back and leave A selected, which is a jump the user did not ask for.
	// Refreshing makes it "the selection you had at the moment you left
	// this state", which is both more accurate and what an editor is
	// expected to do.
	//
	// Guarded on the path still RESOLVING, because it need not: after an
	// add, the selection is the new node and its path does not exist in
	// the state before the add. Then the state keeps the selection it
	// already had. It is an index path, so in principle it can resolve to
	// a different element than the one selected — that ambiguity is
	// inherent in addressing by position, and the cost of being wrong is
	// a selection one element off, not a lost edit.
	// COALESCE A RUN OF EDITS INTO ONE STEP, where they are the same
	// person doing the same thing to the same field.
	//
	// A live editor writes on every keystroke — the properties pane's
	// caret editor fires per rune, its stepper and colour editors per
	// arrow — so "change Rows from 1,1* to 1,2*,1" is half a dozen
	// rebuilds. Recorded one-per-rebuild, ctrl+z walks the value back a
	// character at a time, which is the same defect as a drag undoing a
	// cell at a time and it arrives through a different door.
	//
	// The run is defined WITHOUT the clock. Time-based coalescing is
	// untestable and fails on a loaded machine; this merges when the edit
	// touches the same single attribute of the same node as the edit that
	// produced the current state, with the selection unmoved. Structural
	// edits never merge (editKey returns ""), so add, delete, retype and
	// paste are always their own step, and neither is a drag — it writes
	// two attributes at once.
	//
	// Derived here rather than requested by the caller, for the same
	// reason the recording itself is: a "start a new undo group" call
	// that a future editor forgets to make fails silently.
	// NO SEPARATE "did the selection move" CONJUNCT, and that is derived
	// rather than an omission: the key CARRIES the node's index path, so
	// an edit to a different element already has a different key. A
	// selection check next to it would be code no test could ever make
	// fail — which is how it was found (mutating it to always-true
	// changed nothing).
	key := editKey(h.base.root, root)
	if key != "" && key == h.base.key {
		h.base = snapshot{root: root.clone(), sel: sel, hasSel: hasSel,
			label: h.base.label, key: key}
		h.redo = nil
		// AND IF THE RUN CAME BACK TO WHERE IT STARTED, it was not an
		// edit at all. Esc in the properties pane restores the value the
		// row held when the editor opened, and it does so by WRITING it —
		// so a cancelled edit is a second document change rather than a
		// rollback. Without this, the history would hold an entry whose
		// undo changes nothing visible, and the user would press ctrl+z
		// and watch nothing happen.
		if n := len(h.undo); n > 0 && h.undo[n-1].root.equal(root) {
			h.undo[n-1] = snapshot{}
			h.undo = h.undo[:n-1]
		}
		return
	}

	// The outgoing state's selection is refreshed to what is selected
	// NOW, where that still names a node in it.
	if hasSel && nodeAtPath(h.base.root, sel) != nil {
		h.base.sel, h.base.hasSel = sel, true
	}
	h.push(h.base)
	h.base = snapshot{root: root.clone(), sel: sel, hasSel: hasSel, label: label, key: key}
	// THE ONE PLACE REDO IS CLEARED, and it is the classic bug's fix. A
	// new edit after an undo makes every redone state unreachable: the
	// branch they belonged to is gone. Clearing anywhere else — in undo,
	// on a keystroke, on a timer — either drops redo too early (so
	// undo/redo cannot be alternated) or too late (so ctrl+y after an
	// edit replays a state from a branch the user abandoned, silently
	// discarding the edit they just made).
	h.redo = nil
}

// push adds a state and enforces the bound.
func (h *history) push(s snapshot) {
	h.undo = append(h.undo, s)
	h.evict()
}

// evict drops the OLDEST entries until the stack fits the limit.
//
// The dropped snapshots are zeroed rather than left beyond the new
// length. Reslicing alone keeps them alive through the backing array,
// which would make the bound a limit on how far you can undo and not on
// how much the editor holds — half a bound, and the wrong half, since
// the reason for having one is the memory.
func (h *history) evict() {
	if len(h.undo) <= h.limit {
		return
	}
	drop := len(h.undo) - h.limit
	copy(h.undo, h.undo[drop:])
	for i := h.limit; i < len(h.undo); i++ {
		h.undo[i] = snapshot{}
	}
	h.undo = h.undo[:h.limit]
}

// CanUndo and CanRedo report whether the stacks have anything in them.
func (ed *editor) CanUndo() bool { return len(ed.history().undo) > 0 }
func (ed *editor) CanRedo() bool { return len(ed.history().redo) > 0 }

// undo steps back one edit.
//
// The message says what was undone, using the label of the state being
// LEFT — that state is the one the edit produced, so its label names the
// edit. Saying nothing would be worse than saying little: a gesture that
// changes the screen without explanation and a gesture that does nothing
// look the same from the keyboard, which is the diagnostic gap
// beginDrag's refusal message exists to close.
func (ed *editor) undo() {
	h := ed.history()
	if len(h.undo) == 0 {
		ed.sayDrag("nothing to undo")
		return
	}
	prev := h.undo[len(h.undo)-1]
	h.undo[len(h.undo)-1] = snapshot{}
	h.undo = h.undo[:len(h.undo)-1]
	undone := h.base.label
	h.redo = append(h.redo, h.base)
	ed.restore(prev)
	ed.sayDrag(stepMsg("undone", undone))
}

// redo steps forward again.
//
// It pushes through push rather than appending, so the bound holds on
// this side too. That is belt and braces — undo and redo conserve the
// total, so the undo stack cannot grow past where it already was — but a
// second rule for the same invariant is how the first one gets forgotten.
func (ed *editor) redo() {
	h := ed.history()
	if len(h.redo) == 0 {
		ed.sayDrag("nothing to redo")
		return
	}
	next := h.redo[len(h.redo)-1]
	h.redo[len(h.redo)-1] = snapshot{}
	h.redo = h.redo[:len(h.redo)-1]
	h.push(h.base)
	ed.restore(next)
	ed.sayDrag(stepMsg("redone", next.label))
}

func stepMsg(verb, label string) string {
	if label == "" {
		return verb
	}
	return verb + ": " + label
}

// restore installs a recorded state as the live document.
//
// TWO CLONES, and they are not one clone used twice. The tree the editor
// edits from here must share nothing with the snapshot still sitting in
// the stacks, or the next keystroke would reach through and mutate
// history — the recorded state would silently become whatever the user
// did after returning to it, and undoing to it twice would give two
// different documents.
//
// THE BASELINE IS INSTALLED BEFORE THE REBUILD, and that placement is
// what makes redo survive. rebuild calls record, which pushes only when
// the live tree differs from the baseline; setting the baseline to the
// state being restored means record sees no change, pushes nothing, and —
// crucially — does not clear the redo stack. Clearing redo lives in
// exactly one place, and this is how restoring stays outside it.
//
// The drag needs no attention here even though it holds a *node: after
// this the dragged node is from a tree nobody holds, so parentOf returns
// nil for it and dragLive (drag.go) drops the whole gesture on the next
// pointer report. That check exists for deleteSelected and covers this
// for the same reason it was written — the stale pointer is the
// invariant, not the mutator that made it stale.
//
// THE PLACEHOLDER VALUES ARE LEFT REGISTERED, on purpose, and this is
// the note for whoever later reads ctx.Values growing as a leak to fix.
// addSelected registers what markup.Seeded returns (e.g.
// "Button2_Content") and undoing the add leaves the entry behind. That
// is right, not sloppy:
//
//   - an entry nothing binds is INERT — it is a property handle in a
//     map, with no subscriber, no paint node reading it and no cost per
//     frame;
//   - unregistering would force redo to re-register, and a re-registered
//     handle is a FRESH one, so every value the user had set on the old
//     one is lost. Redo would stop being the inverse of undo and start
//     being destructive. Keeping the handle is exactly what makes the
//     round trip lossless;
//   - it does not accumulate anyway: the keys are derived from the
//     element's generated name, which is a function of the target's child
//     count, so add → undo → add reuses the SAME key rather than minting
//     a new one.
func (ed *editor) restore(s snapshot) {
	h := ed.history()
	// The clone here is DEFENSIVE and currently unobservable: undo and
	// redo both pop before calling, so nothing else holds s.root and
	// aliasing it would be harmless. Mutating the clone away therefore
	// survives the suite — an equivalent mutant, not a gap, and it is
	// noted so the next reader does not chase it. It stops being
	// equivalent the moment anything restores a snapshot still held in a
	// stack, which is exactly the kind of caller a peek-without-pop
	// optimisation would add.
	ed.root = s.root.clone()
	ed.sel = nil
	if s.hasSel {
		// nil when the path no longer resolves, which is the editor's
		// own "nothing selected" state rather than a sentinel. ctrl+n
		// and ctrl+p are the way back out of it.
		ed.sel = nodeAtPath(ed.root, s.sel)
	}
	// key is deliberately NOT carried: a restored state starts a fresh
	// run. Inheriting it would let the next keystroke into the same field
	// MERGE into the state undo just restored, so no new entry would be
	// pushed and ctrl+z could not get back out again.
	h.base = snapshot{root: ed.root.clone(), sel: s.sel, hasSel: s.hasSel, label: s.label}
	h.pending = ""
	ed.rebuild()
}

// selPath is the current selection as an index chain.
func (ed *editor) selPath() ([]int, bool) {
	if ed.sel == nil {
		return nil, false
	}
	return pathTo(ed.root, ed.sel)
}

// pathTo walks Kids only, which is not an omission: Slots hold property
// elements (<ItemsView.ItemTemplate>) and nothing in the editor can
// select one — parentIn does not walk them either, so a slot child has no
// parent as far as the editor is concerned and cannot be selected,
// deleted or dragged. clone DOES copy them, because they are part of the
// saved document even though they are not reachable by the pointer.
func pathTo(at, want *node) ([]int, bool) {
	if at == want {
		return []int{}, true
	}
	for i, k := range at.Kids {
		if p, ok := pathTo(k, want); ok {
			return append([]int{i}, p...), true
		}
	}
	return nil, false
}

// nodeAtPath resolves an index chain, or nil if it does not fit the tree.
func nodeAtPath(at *node, path []int) *node {
	for _, i := range path {
		if at == nil || i < 0 || i >= len(at.Kids) {
			return nil
		}
		at = at.Kids[i]
	}
	return at
}

// clone returns a deep copy of n: Attrs, Kids and Slots all copied,
// sharing nothing with the original.
//
// SLOTS ARE THE HALF THAT IS EASY TO MISS. They are property elements
// rather than children, nothing walks them except node.markup, and a copy
// that took Kids and left Slots aliased would share an
// <ItemsView.ItemTemplate> subtree between the live document and every
// snapshot of it — so editing the template would silently rewrite
// history, and undoing would appear to work everywhere except inside a
// template.
//
// Nil-ness is preserved rather than normalised to empty. It makes no
// difference to node.markup, and a copy that differs structurally from
// its original is a copy the equality check has to be taught to forgive.
func (n *node) clone() *node {
	if n == nil {
		return nil
	}
	c := &node{Elem: n.Elem, Body: n.Body}
	if n.Attrs != nil {
		c.Attrs = make(map[string]string, len(n.Attrs))
		for k, v := range n.Attrs {
			c.Attrs[k] = v
		}
	}
	if n.Kids != nil {
		c.Kids = make([]*node, len(n.Kids))
		for i, k := range n.Kids {
			c.Kids[i] = k.clone()
		}
	}
	if n.Slots != nil {
		c.Slots = make(map[string]*node, len(n.Slots))
		for k, v := range n.Slots {
			c.Slots[k] = v.clone()
		}
	}
	return c
}

// equal reports whether two documents are the same document.
//
// STRUCTURAL, deliberately, and not a comparison of node.markup output.
// Serialising would be the shorter code and it would have a silent hole:
// markup drops the Body of a node that also has Kids ("mutually
// exclusive here"), so two states differing only in that dropped body
// serialise identically and the edit between them would not be
// undoable — with no error, which is the exact failure class this file
// exists to remove. Comparing the model has no such gap.
//
// nil and empty compare equal for Attrs, Kids and Slots, because they
// mean the same document and clone is not the only thing that builds a
// node.
func (n *node) equal(o *node) bool {
	if n == nil || o == nil {
		return n == o
	}
	if n.Elem != o.Elem || n.Body != o.Body {
		return false
	}
	if len(n.Attrs) != len(o.Attrs) {
		return false
	}
	for k, v := range n.Attrs {
		if w, ok := o.Attrs[k]; !ok || w != v {
			return false
		}
	}
	if len(n.Kids) != len(o.Kids) {
		return false
	}
	for i := range n.Kids {
		if !n.Kids[i].equal(o.Kids[i]) {
			return false
		}
	}
	if len(n.Slots) != len(o.Slots) {
		return false
	}
	for k, v := range n.Slots {
		w, ok := o.Slots[k]
		if !ok || !v.equal(w) {
			return false
		}
	}
	return true
}

// editKey names WHAT changed between two document states, when that is
// exactly one attribute (or the body) of exactly one node. It is the
// coalescing key: two consecutive edits with the same non-empty key are
// the same person typing into the same field.
//
// "" means "do not coalesce", and it is returned for every change that is
// not a single-attribute edit — anything structural, and anything that
// moved two attributes at once. That last case is not a limitation but
// the reason drags stay separate: a drag writes Canvas.Left AND
// Canvas.Top, so two consecutive drags of the same element are two undo
// steps, which is what a person who dragged twice expects.
//
// Being conservative is the safe direction. A key wrongly returned EMPTY
// costs one extra undo step; a key wrongly SHARED merges two edits the
// user thinks are separate and makes one of them unreachable.
func editKey(old, cur *node) string {
	var found []string
	diffKeys(old, cur, "", &found)
	// The empty check is belt and braces over the count: diffKeys spells
	// "structural" by appending TWO entries, so a structural change can
	// never leave a count of one — which means mutating that 2 to a 1 is
	// an EQUIVALENT mutant, not an untested path. Stated because a
	// mutation run will report it as a survivor and the next reader
	// deserves to know it is not a gap.
	if len(found) != 1 || found[0] == "" {
		return ""
	}
	return found[0]
}

// diffKeys collects a key per difference, stopping as soon as it has more
// than one — the caller only ever distinguishes "exactly one" from "not
// exactly one", so there is no reason to walk the rest of the tree.
//
// A structural difference appends TWO keys rather than one, which is how
// "never coalescable" is spelled without a second return value: it can
// never leave the caller with a count of exactly one.
func diffKeys(old, cur *node, path string, found *[]string) {
	if len(*found) > 1 {
		return
	}
	if old == nil || cur == nil || old.Elem != cur.Elem ||
		len(old.Kids) != len(cur.Kids) || len(old.Slots) != len(cur.Slots) {
		*found = append(*found, "", "")
		return
	}
	if old.Body != cur.Body {
		*found = append(*found, path+"\x00"+BodyRowName)
	}
	// Two loops in opposite directions, and they cannot double-count: the
	// first sees added and changed, the second only removed.
	for k, v := range cur.Attrs {
		if w, ok := old.Attrs[k]; !ok || w != v {
			*found = append(*found, path+"\x00"+k)
		}
	}
	for k := range old.Attrs {
		if _, ok := cur.Attrs[k]; !ok {
			*found = append(*found, path+"\x00"+k)
		}
	}
	for i := range cur.Kids {
		diffKeys(old.Kids[i], cur.Kids[i], path+"/"+strconv.Itoa(i), found)
	}
	for k, v := range cur.Slots {
		diffKeys(old.Slots[k], v, path+"/"+k, found)
	}
}
