package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/term"
)

// Copy, cut and paste — which is TWO features that share three key
// bindings, and conflating them is the way to get both wrong.
//
//	THE COMPONENT CLIPBOARD is internal: a deep copy of a *node subtree,
//	pasted back into the document under a new parent. It is what "copy
//	this Button and put another one over there" means, and nothing about
//	it involves the terminal.
//
//	THE SYSTEM CLIPBOARD is markup TEXT crossing the process boundary:
//	out, so a user can paste their document into a chat window; in, so
//	they can paste markup they were sent. It involves the terminal and
//	almost nothing else, and it is asymmetric — see term/clipboard.go.
//
// They meet at exactly one place: `y` does BOTH. Copying a subtree puts
// the nodes on the internal clipboard and its markup on the system one,
// because there is no version of "copy" where a user wants only one of
// those and no way to ask them which. What it does NOT do is pretend
// both succeeded — the status line reports each half.
//
// The paste side does not meet at all, and cannot:
//
//	p                     pastes the internal clipboard (nodes)
//	the terminal's paste  pastes markup text (one input.EventPaste)
//
// There is no key here that reads the system clipboard, because no key
// can. OSC 52 reads are refused by most terminals for good reasons
// (term/clipboard.go says which), so the app cannot fetch the clipboard
// on demand; what it can do is recognise a paste the USER initiates,
// which is what bracketed paste is for. A `ctrl+v` bound to "read the
// system clipboard" would do nothing on most terminals and have no way
// to say so — the silent failure this whole file is arranged to avoid.

// clipboard is what a copy put aside.
//
// It holds a DEEP COPY, never a live pointer into the document. Two
// reasons, and the second is the one that bites: undo replaces ed.root
// wholesale with a fresh copy, so any pointer taken before an undo
// dangles; and a cut-then-paste of a live pointer would alias the same
// node into two places in one tree, where editing one edits both.
type clipboard struct {
	node *node
	// markup is what was ALSO written to the system clipboard, kept so a
	// test can assert the two halves agree without a terminal.
	markup string
}

// deepCopy returns a copy of n sharing nothing with it.
//
// SLOTS ARE THE HALF THAT IS EASY TO MISS. node.Slots holds property
// elements — <ItemsView.ItemTemplate> — which are structured attributes
// rather than children, so a copy that walks Kids and stops loses an
// entire subtree with no error: the paste succeeds, the element builds,
// and its template is simply gone. control.collectSubtree
// (control/markup.go:430) makes the same point from the other end,
// walking children AND attachments together "because a departing
// subtree's names and declared surfaces all leave together".
//
// Attachments need no special case HERE, unlike in collectSubtree,
// because in the design model a <KeyBinding> inside a <Button> is an
// ordinary entry in Kids. The distinction only exists once markup.Build
// has turned them into components.
//
// TEMPORARY NAME. slice-undo's undo.go owns `func (n *node) clone()`
// with these exact semantics; when that file lands this method is
// deleted and its callers point at clone(). Two deep copies in one
// package is the duplicate-local-patch shape, and the only reason there
// are briefly two is that neither branch may be red while the other is
// unmerged.
func (n *node) deepCopy() *node {
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
	for _, k := range n.Kids {
		c.Kids = append(c.Kids, k.deepCopy())
	}
	if n.Slots != nil {
		c.Slots = make(map[string]*node, len(n.Slots))
		for k, v := range n.Slots {
			c.Slots[k] = v.deepCopy()
		}
	}
	return c
}

// walkNode visits n and everything under it — Kids and Slots both, for
// the reason deepCopy gives.
func walkNode(n *node, fn func(*node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, k := range n.Kids {
		walkNode(k, fn)
	}
	for _, s := range n.Slots {
		walkNode(s, fn)
	}
}

// ---- the component clipboard ----

// copySelected puts the selection on both clipboards.
func (ed *editor) copySelected() {
	n := ed.sel
	if n == nil {
		ed.status.Set("✗ nothing selected to copy")
		return
	}
	if ed.isSurface(n) {
		// Not a refusal for tidiness: the surface is the editor's own
		// workspace and is in no save, so a copy of it would paste the
		// designer's scaffolding into the user's document.
		ed.status.Set("✗ the design surface is not part of the document")
		return
	}
	src := n.markup("")
	ed.clip = clipboard{node: n.deepCopy(), markup: src}
	ed.status.Set("copied " + describeNode(n) + ed.sayCopiedOut(src))
}

// sayCopiedOut is the SYSTEM half of a copy, rendered as the tail of the
// status line.
//
// Never silent in either direction. A copy that reached the terminal
// says so; one that could not says why; and one that was written but
// travels through a multiplexer that swallows OSC 52 by default carries
// the caveat, because otherwise the user sees a confirmation and an
// unchanged clipboard with nothing anywhere to connect the two.
func (ed *editor) sayCopiedOut(src string) string {
	if err := ed.copyToSystem(src); err != nil {
		return " (system clipboard: " + err.Error() + ")"
	}
	if c := term.ClipboardCaveat(); c != "" {
		return " → system clipboard (" + c + ")"
	}
	return " → system clipboard"
}

// cutSelected copies and then deletes — in that order, and only when the
// delete will actually happen.
//
// The order matters and the guard more so. deleteSelected refuses the
// user's root (a document has to have one), so a cut that copied first
// and deleted second would leave the user believing the root had been
// moved to the clipboard while it sat untouched on the canvas — and the
// paste that followed would DUPLICATE it. Asking deletable() first makes
// the refusal the whole gesture.
func (ed *editor) cutSelected() {
	n := ed.sel
	if n == nil {
		ed.status.Set("✗ nothing selected to cut")
		return
	}
	if !ed.deletable(n) {
		ed.status.Set("✗ " + describeNode(n) + " cannot be cut: a document must keep its root")
		return
	}
	src := n.markup("")
	// NEITHER CLIPBOARD IS WRITTEN UNLESS THE DELETE STANDS. deletable()
	// above answers the refusals deleteSelected can see BEFORE trying, but
	// not the one only the loader can: removing a child can make its
	// parent illegal (`<Tab Header=… needs exactly one content child,
	// got 0`), and that is discovered by building. A cut that copied
	// anyway would leave the node on the page and on the clipboard, so the
	// next paste duplicates it under a colliding Name. Reported in review
	// of #454.
	//
	// BOTH, and the first version of this fix withheld only one. The
	// message was built above the guard, and sayCopiedOut is not a
	// formatter — it calls copyToSystem, which writes the OSC 52. So a
	// refused cut left the node on the page and its markup on the SYSTEM
	// clipboard, which is a live route back into the document through the
	// terminal's own paste key (bindClipboardTo → pasteMarkup →
	// insertSubtree). It also overwrote the user's clipboard while the
	// status line read "✗ … cannot be deleted", against sayCopiedOut's own
	// "never silent in either direction". Reported in review of #454,
	// twice. The fix is ordering: nothing above this line touches a
	// clipboard.
	//
	// deleteSelected rebuilds, and rebuild sets the build status — so the
	// message goes on AFTER it or it is overwritten in the same frame by
	// "✓ builds". Learned the hard way: the cut worked and said nothing.
	if !ed.deleteSelected() {
		// deleteSelected has already put its own refusal in the status
		// bar, and it names the loader's reason. Saying anything here
		// would replace a specific message with a vaguer one.
		return
	}
	ed.clip = clipboard{node: n.deepCopy(), markup: src}
	ed.status.Set("cut " + describeNode(n) + ed.sayCopiedOut(src))
}

// deletable mirrors deleteSelected's own refusal. It is a separate
// predicate rather than a return value from deleteSelected because cut
// has to know BEFORE it copies; see cutSelected.
func (ed *editor) deletable(n *node) bool {
	if n == nil {
		return false
	}
	p := ed.parentOf(n)
	return p != nil && !ed.isSurface(p)
}

// pasteClip inserts the internal clipboard under the paste target.
func (ed *editor) pasteClip() {
	if ed.clip.node == nil {
		// Not silence, and not "pasted nothing". An empty clipboard is a
		// real state with a real answer.
		ed.status.Set("✗ clipboard is empty — copy something with y first")
		return
	}
	// A COPY OF THE COPY. Pasting twice must produce two independent
	// subtrees, and the clipboard must survive both — pasting the held
	// node itself would put it in the document and then let the next
	// edit mutate the clipboard through it.
	ed.insertSubtree(ed.clip.node.deepCopy(), "pasted")
}

// insertSubtree is the one place a foreign subtree enters the document,
// shared by the node paste and the markup paste. Both face the identical
// two problems — names collide, and per-instance bindings point at keys
// that belong to another instance — and solving them twice is how the
// two would come to disagree.
func (ed *editor) insertSubtree(n *node, verb string) {
	// The PLAN, not just its landing node, and for the element actually
	// being pasted. Both halves matter and neither is cosmetic.
	//
	// The element, because where an insert may land depends on what it is:
	// planAdd climbs until something can hold THIS element. Passing the
	// pasted node's own name is what makes "paste a <Tab> with a <Tabs>
	// selected" land in the tabs rather than climbing past them.
	//
	// The wrap, because a container can accept an element only through one
	// of its own declared children — <Tabs> takes <Tab> and nothing else.
	// Appending straight into plan.into there writes an illegal child, the
	// rebuild fails, docRoot goes nil, and click-to-select dies for the
	// WHOLE document while the previous tree stays on screen looking
	// pressable. That is the exact failure addplan.go exists to close for
	// the palette; paste reaches the same container by the other gesture.
	plan := ed.planAdd(n.Elem)
	// NOTHING CAN HOLD IT. planAdd returns a zero plan for a Nested
	// element with no legal parent on the page, and appending into a nil
	// node would panic where every other refusal here sets the status.
	if plan.into == nil {
		ed.status.Set("✗ <" + n.Elem + "> has no legal parent on this page")
		return
	}
	into := plan.into
	// NAMESPACES FIRST, BEFORE THE TWO RENAMES, and the order is the
	// fix rather than a tidy-up.
	//
	// rebindInto writes ed.ctx.Values[key], and nothing ever
	// unregisters one — deliberately, so an undone paste can be redone
	// onto the values the user had set. renameInto counts a name owning
	// live handles as taken. So a paste refused AFTER them has already
	// burned the names it would have used: the user fixes the prefix
	// clash the message asked them to fix, pastes again, and gets T3
	// where they would have got T2, with T2's handles registered to
	// nothing. A corrigible error must not cost anything.
	//
	// Nothing here depends on the renames: this reads xmlns attributes
	// and the renames read Name and bindings. Raised in review of #501.
	if err := ed.reconcileNamespaces(n); err != nil {
		ed.status.Set("✗ " + err.Error())
		return
	}
	renamed := ed.renameInto(n)
	if err := ed.rebindInto(n, renamed); err != nil {
		ed.status.Set("✗ " + err.Error())
		return
	}
	// Free geometry only where the PARENT gives it, the same rule
	// addSelected follows: under a <Grid> or a <VStack> a Canvas.Left is
	// silently discarded. A pasted node keeps whatever position it was
	// copied with when it lands on a Canvas, so a paste next to the
	// original does not stack them exactly on top of each other.
	//
	// ASKED OF THE GRANT, not of the name. `into.Elem == "Canvas"` with
	// both attributes spelled as literals covered exactly one container
	// and would have gone on writing the old names if the catalog
	// renamed either. The grant answers which container offsets its
	// children AND what it calls the two axes, which is the same
	// question the drag's Release asks. Found in review of #390.
	if g := ed.grantOf(into.Elem); g.Kind == markup.GrantOffset && n.Attrs != nil {
		x, y := g.Attr(markup.RoleX), g.Attr(markup.RoleY)
		if x != "" && y != "" {
			if _, ok := n.Attrs[x]; ok {
				n.Attrs[y] = fmt.Sprint(len(into.Kids)*2 + 1)
			}
		}
	}
	// The scaffolding, when the plan says the container needs it. The
	// SELECTION stays on the pasted node rather than the wrapper, the same
	// rule addSelected follows: the user pasted a <Button>, so the
	// properties grid must show the button and not the <Tab> that had to
	// exist to hold it.
	add := n
	if plan.wrap != "" {
		w := ed.wrapperNode(into.Elem, plan.wrap)
		w.Kids = []*node{n}
		add = w
	}
	// The accelerator, beside the name. A second <Menu> in a <MenuBar>
	// claiming the same alt gesture is unreachable by keyboard, and so is
	// a second <MenuItem> in a <Menu> claiming the same letter — the
	// guard covers BOTH levels since round 11, and these three comments
	// still named only the first. unshadowMnemonic is the one place all
	// three insertion routes share.
	unshadowMnemonic(into, add)
	prevSel := ed.sel
	into.Kids = append(into.Kids, add)
	ed.sel = n
	// Mutate, then rebuild — the mutation seam every other edit in this
	// editor uses (addSelected, deleteSelected, retype, commitEdit), and
	// the one slice-undo's undo.go hooks. Nothing here opts in to undo;
	// undo is derived at the choke point, which is why a future mutator
	// cannot forget it.
	ed.rebuild()
	// REVERT ON A FAILED REBUILD, the same guard promoteSelected and
	// demoteSelected carry (move.go). This path did not have it, so a
	// paste the vocabulary refuses reported success and left docRoot nil
	// — click-to-select dead for the WHOLE document while the last good
	// tree stayed on screen looking pressable (#403). The gates above
	// make that unreachable through canHold; this is the backstop for
	// every other way a pasted subtree can fail to build.
	if ed.remote == nil && ed.docRoot == nil {
		refused := strings.TrimPrefix(ed.status.Get(), "✗ ")
		into.Kids = into.Kids[:len(into.Kids)-1]
		ed.sel = prevSel
		// BEFORE the rebuild: the refused mutation must not stay on the
		// undo stack, or one ctrl+z re-enters the docRoot==nil state this
		// revert exists to prevent (#454 review).
		ed.abortHistory()
		ed.rebuild()
		// IT DOES NOT SAY WHY, and that is the point. This read
		// "<X> does not go inside <Y>: …", which is a cause this
		// backstop has not established and by its own comment above
		// cannot be: canHold already refused every parenting fault
		// before the append, so everything reaching here failed for
		// some OTHER reason. The namespace work made one of those
		// common — paste a subtree that USES a prefix without its
		// declaration, the ordinary result of copying one element out
		// of a document, and the editor answered:
		//
		//	✗ <Button> does not go inside <Canvas>: markup: …
		//	  undeclared namespace prefix "t"
		//
		// <Button> goes inside <Canvas> perfectly well. The real cause
		// was after the colon all along; the clause in front of it was
		// the wrong noun, which is the same defect nodeOf's five
		// reworded refusals were for. Raised in review of #501.
		ed.status.Set("✗ <" + n.Elem + "> was not pasted into <" + into.Elem +
			">: " + refused)
		return
	}
	ed.status.Set(verb + " " + describeNode(n) + ed.sayRenamed(renamed))
}

func (ed *editor) sayRenamed(renamed map[string]string) string {
	if len(renamed) == 0 {
		return ""
	}
	parts := make([]string, 0, len(renamed))
	for _, k := range sortedKeys(renamed) {
		parts = append(parts, k+"→"+renamed[k])
	}
	return " (renamed " + strings.Join(parts, ", ") + ")"
}

// ---- names ----

// trailingDigits splits "Button12" into "Button" and true.
var trailingDigits = regexp.MustCompile(`^(.*?)([0-9]+)$`)

// renameInto gives every named node in the incoming subtree a name that
// is free in the destination document, and reports what it changed.
//
// A NAME IS AN ADDRESS, not a label. The outline, the property grid,
// hitTest and every {{.Binding}} resolve by it, and markup.Build treats
// a duplicate as a load error — so pasting a copy of <Text Name="T1">
// beside the original does not produce two elements with the same name,
// it produces a document that will not build, and the user sees "✗" with
// no obvious connection to the paste.
//
// The policy is: strip trailing digits to get a base, then take the
// lowest free suffix from 2 up. "T1" pasted next to "T1" becomes "T2",
// and a THIRD becomes "T3" — not "T1_copy_copy". Names already free are
// left alone, which is what makes pasting into a DIFFERENT document
// (where nothing collides) preserve the names the bindings were written
// against.
//
// Names allocated during this walk count as taken from that point on, so
// a subtree containing two colliding names does not get the same
// replacement twice.
func (ed *editor) renameInto(n *node) map[string]string {
	used := map[string]bool{}
	walkNode(ed.root, func(k *node) {
		if k.Attrs != nil && k.Attrs["Name"] != "" {
			used[k.Attrs["Name"]] = true
		}
	})
	// ctx.Values is consulted too, and this is not belt-and-braces. A
	// binding key is <Name>_<Attr>, and nothing ever UNREGISTERS one —
	// deliberately, so an undone paste can be redone onto the values the
	// user had set. So a name can be free in the TREE while still owning
	// live handles, and handing it to a pasted element would silently
	// adopt a deleted element's state.
	for key := range ed.ctx.Values {
		if i := strings.LastIndex(key, "_"); i > 0 {
			used[key[:i]] = true
		}
	}
	renamed := map[string]string{}
	walkNode(n, func(k *node) {
		if k.Attrs == nil {
			return
		}
		name := k.Attrs["Name"]
		if name == "" || !used[name] {
			if name != "" {
				used[name] = true
			}
			return
		}
		next := freeName(name, used)
		used[next] = true
		renamed[name] = next
		k.Attrs["Name"] = next
	})
	return renamed
}

func freeName(name string, used map[string]bool) string {
	base := name
	if m := trailingDigits.FindStringSubmatch(name); m != nil && m[1] != "" {
		base = m[1]
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s%d", base, i)
		if !used[cand] {
			return cand
		}
	}
}

// ---- bindings ----

// bindingRef matches a whole-attribute binding: {{.Path}} and nothing
// else. Deliberately not a substring match anywhere in the value — a
// composite like "on {{.Branch}}" is a template with a literal in it,
// and rewriting a name inside one would change text the user typed.
var bindingRef = regexp.MustCompile(`^\{\{\s*\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}$`)

// rebindInto re-keys the per-instance bindings inside a pasted subtree
// and registers a value for every key it invents.
//
// THE PROBLEM. markup.Seeded gives each new instance its own binding
// keys — <Gauge Name="G1" Value="{{.G1_Value}}"/> — precisely so a
// second Gauge does not move the first one's needle. A COPY inherits the
// original's keys, so without this a pasted Gauge would share state with
// the one it was copied from: both documents load, both elements paint,
// and moving one moves the other. Silent, and exactly the bug the
// per-instance convention exists to prevent.
//
// THE RULE. A binding whose key is <oldName>_<Attr>, where oldName is a
// node this paste renamed, is re-keyed to <newName>_<Attr> through
// markup.SeedKey — the same function Seeded itself uses, so the two
// cannot drift — and a fresh placeholder is registered under the new key
// through markup.SeedPlaceholder, which knows that a command needs a
// real gooey.Action rather than nothing.
//
// A binding that is NOT of that shape is left exactly as it is. It
// refers to something outside the subtree — a viewmodel property the
// user wired up by hand — and the honest thing is to carry the reference
// across and let the build say so if the destination has no such name. A
// paste that silently rewrote or dropped those would be editing the
// user's intent.
func (ed *editor) rebindInto(n *node, renamed map[string]string) error {
	if len(renamed) == 0 {
		return nil
	}
	// Keyed by the OLD name, so a node can find the spec whose attribute
	// list explains what type its bindings want.
	specOf := map[string]markup.ElementSpec{}
	walkNode(n, func(k *node) {
		if k.Attrs == nil {
			return
		}
		// The node's name has ALREADY been rewritten by renameInto, so
		// the reverse map is what connects it back to its old keys.
		for old, next := range renamed {
			if k.Attrs["Name"] == next {
				specOf[old] = ed.specOrBare(k.Elem)
			}
		}
	})

	var firstErr error
	walkNode(n, func(k *node) {
		for attr, val := range k.Attrs {
			m := bindingRef.FindStringSubmatch(val)
			if m == nil {
				continue
			}
			old, suffix, ok := splitSeedKey(m[1], renamed)
			if !ok {
				continue
			}
			key := markup.SeedKey(renamed[old], suffix)
			k.Attrs[attr] = "{{." + key + "}}"
			if _, exists := ed.ctx.Values[key]; exists {
				continue
			}
			h, err := seedValue(specOf[old], suffix)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if h != nil {
				ed.ctx.Values[key] = h
			}
		}
	})
	return firstErr
}

// splitSeedKey decomposes a binding key into the renamed node it belongs
// to and the attribute suffix.
//
// It matches the LONGEST old name that prefixes the key, which is not
// pedantry: "T1" and "T1_Extra" can both be element names in one
// document, and a shortest-match split would re-key
// {{.T1_Extra_Content}} as T1's "Extra_Content" attribute — a key
// nothing registers and a binding that fails to resolve at load.
func splitSeedKey(key string, renamed map[string]string) (old, suffix string, ok bool) {
	for _, name := range sortedKeys(renamed) {
		if !strings.HasPrefix(key, name+"_") {
			continue
		}
		if len(name) > len(old) {
			old, suffix = name, key[len(name)+1:]
		}
	}
	return old, suffix, old != ""
}

// seedValue is the placeholder an attribute's binding needs, or nil when
// it needs none. The spec lookup can fail — an element registered as a
// bare Builder describes no attributes — and a nil handle is the honest
// answer there: carry the binding across unregistered and let the build
// report it, rather than invent a type.
func seedValue(spec markup.ElementSpec, attr string) (any, error) {
	for _, a := range spec.Attrs {
		if a.Name == attr {
			return markup.SeedPlaceholder(spec, a)
		}
	}
	return nil, nil
}

// specOrBare is specOf with a bare fallback instead of an ok — the
// binding path wants a spec it can range over, and "no attributes" is a
// usable answer where "not found" is not.
//
// IT ASKS ed.specs, NOT ed.palette. The palette is what may be INSERTED;
// the catalog is what EXISTS. A paste rebinds attributes on nodes
// already in the document, so the palette's exclusions are the wrong
// filter here.
//
// Latent today — Name is refused on <Menu>/<MenuItem>, and <Tab>
// declares no attributes, so the fallback and the real spec are
// indistinguishable — and silent in the worst direction once #461 makes
// the <Tab> half live: rebindInto has already rewritten the attribute,
// so a nil handle skips the ed.ctx.Values registration and the paste
// lands a binding nothing registers.
//
// The name is half the fix: specFor and specOf were one character apart
// and answered different questions from different sources. The
// palette-vs-catalog history is in
// docs/specs/2026-09-05-pseudo-elements.md.
func (ed *editor) specOrBare(elem string) markup.ElementSpec {
	if e, ok := ed.specOf(elem); ok {
		return e
	}
	return markup.ElementSpec{Name: elem}
}

// ---- the system clipboard ----

// writeSystemClipboard is the OSC 52 write, indirected through a
// variable so a test can make it FAIL.
//
// That is not a convenience. The requirement here is "never confirm a
// copy that did not happen", and a test that cannot produce the
// did-not-happen case can only ever assert the success path — which
// passes just as well when the failure path is missing entirely.
var writeSystemClipboard = func(ed *editor, text string) error {
	if ed.app == nil {
		return fmt.Errorf("no terminal")
	}
	s := ed.app.Screen()
	if s == nil {
		// Nil while suspended (ctrl+z, or a Companion holding the
		// terminal). Reported rather than swallowed: "copied" with no
		// clipboard change is the failure this file is arranged around.
		return fmt.Errorf("terminal suspended")
	}
	return s.SetClipboard(text)
}

// copyToSystem puts text on the system clipboard, or explains why not.
//
// A nil error means the escape was WRITTEN, which is as much as OSC 52
// can ever tell you — there is no acknowledgement, and the read that
// would let us check is the half terminals refuse. term.ClipboardCaveat
// covers the known cases where a written sequence does not arrive.
func (ed *editor) copyToSystem(text string) error {
	return writeSystemClipboard(ed, text)
}

// ---- pasting markup TEXT in ----

// bindClipboard routes bracketed pastes to the editor.
//
// AfterEvent, not a component. A paste is dispatched to the focused
// component and then up its ancestors (gooey.FocusManager.DispatchPaste),
// so a paste into the properties TextBox is that TextBox's — it
// implements PasteHandler and consumes it, which is correct and must
// stay that way. What reaches here is what the TREE DECLINED, which is
// the same rule App.handle already uses for the quit key: the tree gets
// first refusal, the app takes the leftovers.
//
// The alternative — a PasteHandler component wrapping the page — would
// have to sit above the panes to see anything, and would then also
// swallow the pastes the TextBox wants.
func (ed *editor) bindClipboard(app *gooey.App) {
	ed.bindClipboardTo(app.AfterEvent, app.Invalidate)
}

// bindClipboardTo is bindClipboard with its two app dependencies passed
// in, for the same reason hitTest and invalidateFn are injected in
// drag.go: the tests drive Composer.Frame() directly and have no
// *gooey.App, so a hook reachable only through one is a hook no test can
// fire.
func (ed *editor) bindClipboardTo(afterEvent func(func(input.Event, bool)), invalidate func()) {
	afterEvent(func(ev input.Event, consumed bool) {
		if !ev.IsPaste() || consumed {
			return
		}
		ed.pasteMarkup(ev.Paste.Text)
		invalidate()
	})
}

// pasteMarkup inserts markup TEXT — what the terminal handed us when the
// user pressed their own paste key — as a subtree.
//
// Every failure here is REPORTED. Pasted text is arbitrary: it is
// prose as often as it is markup, and a designer that silently ignored a
// paste of the wrong thing would be indistinguishable from one where
// paste is broken.
func (ed *editor) pasteMarkup(src string) {
	src = strings.TrimSpace(src)
	if src == "" {
		ed.status.Set("✗ pasted nothing")
		return
	}
	// The <Gooey> envelope is optional on the way in. Copying out of this
	// editor's CODE tab gives you one; copying a single element out of a
	// file does not, and refusing the second would be refusing the
	// common case.
	n, err := nodeOf(src)
	if err == nil {
		if inner, ok := unwrapGooey(n); ok {
			n = inner
		}
	}
	if err != nil {
		// The NOUN IS THIS CALLER'S, and it used to be patched into
		// nodeOf's message with a strings.Replace of "seed " — which
		// silently did nothing to the one refusal that never said it.
		// nodeOf's messages name what is wrong and leave the noun here
		// (review of #501).
		ed.status.Set("✗ pasted text is not markup: " + err.Error())
		return
	}
	ed.insertSubtree(n, "pasted markup:")
}

// unwrapGooey strips a <Gooey> envelope with exactly one element in it.
//
// More than one and it is refused rather than guessed at: a whole page
// pasted into a selected <Text> has no single answer for where its
// elements go, and picking the first would drop the rest silently.
//
// It takes and returns a NODE rather than re-serializing the child and
// re-parsing it. Not because the round trip would corrupt a body — it
// would not, and believing otherwise was wrong: markup.BodyText's
// multi-line rule is strings.TrimSpace, which is idempotent, so a second
// pass changes nothing and a test written to catch it passes either way
// (mutation-checked). The reason is narrower and real: the round trip is
// a second parse that can FAIL, and a failure there would report a paste
// as unparseable after it had already parsed once.
// THE ENVELOPE'S DECLARATIONS COME WITH IT. <Gooey> is where a
// hand-written document puts its xmlns — it is where markup's own error
// tells the author to put it — and where every file saved before this
// change has it, so dropping the envelope dropped the declarations and
// #472's own bug survived through paste while the open path had been
// fixed. The rule lives in carryDeclarations (main.go) because
// openWorkspaceFile does the same unwrap.
//
// NOT because the CODE tab emits that shape: it no longer does. This
// branch moves the declaration down onto the user's root, and
// TestReopeningTheRebuiltSourceIsStable asserts the root carries it. A
// paste of this editor's own output therefore arrives with the
// declaration already on the child and nothing to carry — the carry is
// for the documents the editor did not write. Raised in review of #501.
func unwrapGooey(n *node) (*node, bool) {
	if n.Elem != "Gooey" || len(n.Kids) != 1 || len(n.Slots) != 0 {
		return nil, false
	}
	carryDeclarations(n, n.Kids[0])
	return n.Kids[0], true
}

// reconcileNamespaces settles a pasted subtree's namespace declarations
// against the document it is landing in, and it is the step
// carryDeclarations needs on THIS side of the seam.
//
// The open path can carry a declaration down blind: the <Gooey>
// envelope is the outermost element, so whether the loader reads
// "child wins" or "last in document order wins" it gets the same
// answer. A paste has neither property. It puts the pasted envelope's
// declaration on a node INSIDE the open document — later in document
// order than the root's own — and markup.parse keeps ONE FLAT,
// document-wide prefix map in which the last declaration wins
// (markup.parse). So a pasted xmlns:t binding t to a different URI
// rebinds t for every expression in the document, including the ones
// the user never touched, and saveOpenFile writes it to disk. Nothing
// reports it, because nothing is wrong as far as the loader is
// concerned: the document is well-formed and every prefix resolves.
//
// Newly reachable with the carry, too — before it the editor could not
// hold two declarations of one prefix at all — which is why this lands
// in the same branch. Raised in review of #501.
//
// Two answers, and the difference is whether the author loses
// anything:
//
//   - the SAME URI: drop the declaration. It says what the document
//     already says, and keeping it leaves a redundant xmlns on every
//     node ever pasted out of the CODE tab.
//   - a DIFFERENT URI: refuse the paste and name both URIs. Rebinding
//     is a decision about expressions the pasted markup does not
//     contain, so it is not one this editor can take on the author's
//     behalf.
//
// ed.doc(), NOT ed.root. ed.root is the design SURFACE, and
// saveOpenFile serialises ed.doc() inside a literal <Gooey> envelope —
// so a declaration held by the surface is one the saved file does not
// carry. Collecting it would make a pasted duplicate look redundant,
// delete it, and write a document whose expressions have no binding.
// No surface declares anything today, so this is the latent half rather
// than a live bug; the two scopes are one level apart and the choice
// belongs written down. Raised in review of #501.
func (ed *editor) reconcileNamespaces(n *node) error {
	doc := map[string]string{}
	collectNamespaces(ed.doc(), doc)
	return reconcileNamespacesInto(n, doc)
}

// SORTED for the same reason collectNamespaces is, and for a different
// consequence: any conflict anywhere refuses, so accept-vs-refuse does
// not depend on the order, but WHICH conflict the message names does —
// and the suite matches on that text. Raised in review of #501.
func reconcileNamespacesInto(n *node, doc map[string]string) error {
	for _, k := range sortedKeys(n.Attrs) {
		v := n.Attrs[k]
		// THE DEFAULT DECLARATION IS NOT A PREFIX BINDING, and this is
		// the skip that says so: isNamespaceAttr matches `xmlns:`+local
		// only, so a plain xmlns leaves here. Neither half of what
		// follows applies to it — it is not compared, because
		// markup.parse skips a plain xmlns outright ("the default
		// namespace is decorative versioning"), so it never enters the
		// flat prefix map and there is nothing for a later one to
		// re-point; and it is not deleted, because XML scoping confines
		// it to the subtree that declares it, which is where it stays.
		//
		// This was a SECOND skip below, `if k == "xmlns"`, with
		// isNamespaceAttr matching the plain form so that it could be
		// reached — an arm whose two paths converged on the same
		// continue, so neither predicate could be observed to disagree
		// with the other. Raised in review of #501.
		//
		// Before that it sat INSIDE the inequality, which left an EQUAL
		// default declaration falling through to the delete below — so
		// pasting <Button xmlns="theirs"/> into a document whose root
		// says xmlns="ours" kept the declaration the first time and
		// stripped it the second, from byte-identical input. The first
		// version refused it outright, which blocked a paste between two
		// documents on different version strings and built its message
		// with TrimPrefix(k, "xmlns:"), asking the author to rename a
		// prefix that does not exist.
		if !isNamespaceAttr(k) {
			continue
		}
		bound, ok := doc[k]
		if !ok {
			continue
		}
		if bound != v {
			// NO DIRECTION IS CLAIMED, because none holds. markup.parse
			// merges every declaration into one flat map in document
			// order, so the winner is whichever is parsed LAST — and
			// where the paste lands decides that. Pasted after the
			// document's own declaration it re-points every existing
			// expression; pasted before one, the document's wins and the
			// PASTED expressions silently mean something else. The old
			// message asserted the first case as the outcome, which is
			// wrong half the time and reads as a promise about which
			// meaning survives. Raised in review of #501.
			return fmt.Errorf("the pasted markup declares %s=%q and this document "+
				"already declares it as %q. One flat prefix map covers the whole "+
				"document and the last declaration parsed wins, so one of the two "+
				"meanings of %s would silently become the other — which one depends "+
				"on where this lands. Rename the prefix in what you are pasting, or "+
				"change the document's own declaration deliberately", k, v, bound,
				strings.TrimPrefix(k, "xmlns:"))
		}
		delete(n.Attrs, k)
	}
	for _, name := range sortedKeys(n.Slots) {
		if err := reconcileNamespacesInto(n.Slots[name], doc); err != nil {
			return err
		}
	}
	for _, k := range n.Kids {
		if err := reconcileNamespacesInto(k, doc); err != nil {
			return err
		}
	}
	return nil
}

// collectNamespaces records every declaration in a subtree. A prefix
// declared twice in one document is already last-wins to the loader, so
// recording the last one here is agreeing with it rather than choosing.
//
// SORTED ON BOTH WALKS, the way node.markup sorts when it writes
// attributes AND slot names back out. "Last wins" is a claim about
// ORDER, and ranging a map has none: one element declaring a prefix
// twice — which an editor that keeps declarations where the author put
// them can produce — resolved to whichever Go's randomized iteration
// reached second, so the same document could accept a paste on one run
// and refuse it on the next. The attribute half was fixed in review of
// #501 and the slot half was not, which left the defect intact on the
// other map walk: four slots declaring xmlns:t to four different URIs
// resolved to all four over 500 runs.
// PREFIXED DECLARATIONS ONLY, because that is what the decision reads.
// This recorded the plain "xmlns" too and reconcileNamespacesInto skips
// k == "xmlns" above the doc[k] lookup, so the entry was unreachable —
// two functions disagreeing about what counts as a declaration, which is
// how the default-namespace bug the skip above records got in. Raised in
// review of #501.
func collectNamespaces(n *node, into map[string]string) {
	for _, k := range sortedKeys(n.Attrs) {
		if isNamespaceAttr(k) {
			into[k] = n.Attrs[k]
		}
	}
	for _, name := range sortedKeys(n.Slots) {
		collectNamespaces(n.Slots[name], into)
	}
	for _, k := range n.Kids {
		collectNamespaces(k, into)
	}
}

// isNamespaceAttr matches a PREFIXED declaration — "xmlns:"+local — and
// nothing else; HasPrefix(k, "xmlns") would also match a plain attribute
// spelled xmlnsFoo, and the plain "xmlns" is not one of these.
//
// IT MATCHED THE PLAIN FORM TOO, AND THE ARM WAS UNOBSERVABLE. Its only
// caller then, reconcileNamespacesInto, skipped `k == "xmlns"` two lines
// later, so returning false here produced the identical result at the
// first continue — a predicate with no behaviour, which two functions
// can then disagree about with nothing red. That is the same class as
// the unreachable plain-xmlns entry already removed from
// collectNamespaces, on the other side of the same pair. The
// default-namespace reasoning moved onto the skip in
// reconcileNamespacesInto, where it is the only thing deciding anything.
//
// AND IT HAS FOUR CALLERS NOW, which is the other half of that round's
// finding. While this matched the plain form it could not be shared —
// carryDeclarations, envelopeAttrs and collectNamespaces must NOT move a
// plain xmlns, so each spelled the prefixed test inline and the comment
// here recorded that as the reason. The narrowing made the reason moot
// and left four identical predicates with nothing explaining why they
// were four; they call this now. Raised in review of #501.
func isNamespaceAttr(k string) bool {
	return strings.HasPrefix(k, "xmlns:")
}

// ---- shared ----

func describeNode(n *node) string {
	if n == nil {
		return "nothing"
	}
	if n.Attrs != nil && n.Attrs["Name"] != "" {
		return "<" + n.Elem + " " + n.Attrs["Name"] + ">"
	}
	return "<" + n.Elem + ">"
}
