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
	c := &node{Elem: n.Elem, Space: n.Space, Body: n.Body}
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
		// THE POP RETAINS, exactly as the delete-splice did: len drops
		// and the refused subtree stays in the vacated slot, reachable
		// from a live parent, with nothing that re-inserts it. Raised in
		// review of #456.
		clear(into.Kids[len(into.Kids):cap(into.Kids)])
		ed.sel = prevSel
		// BEFORE the rebuild: the refused mutation must not stay on the
		// undo stack, or one ctrl+z re-enters the docRoot==nil state this
		// revert exists to prevent (#454 review).
		ed.abortHistory()
		ed.rebuild()
		// IT DOES NOT SAY WHY, and that is the point. This read
		// "<X> does not go inside <Y>: …", a cause this backstop has
		// not established. What it knows is that a rebuild failed; it
		// knows nothing about whose fault that is. Three different
		// causes reach this line:
		//
		//   - THE PARENTING, despite canHold. `canHold` answers false
		//     only where the catalog KNOWS the child is refused —
		//     ModeUnknown and ModeOne both answer true and let the
		//     insert be tried, which addplan.go argues for on the
		//     grounds that the revert message names both elements. A
		//     second child pasted into a ModeOne <Border> is a real
		//     parenting fault arriving here.
		//   - THE PASTED NODE'S OWN CONTENT, which is what the
		//     namespace work made common: paste a subtree that USES a
		//     prefix without its declaration — the ordinary result of
		//     copying one element out of a document — and the editor
		//     answered "✗ <Button> does not go inside <Canvas>: markup:
		//     … undeclared namespace prefix \"t\"". <Button> goes
		//     inside <Canvas> perfectly well.
		//   - A FAULT ALREADY IN THE DOCUMENT, because docRoot is the
		//     signal and nothing resets it. The properties pane has no
		//     revert of its own, so a value it refuses leaves the build
		//     failed and the next paste is reverted and blamed for it.
		//     That is #531, filed rather than left here: six mutators
		//     share this revert and commitEdit is the seventh with none,
		//     and a live defect recorded only in a comment dies with the
		//     comment. Raised in review of #501.
		//
		// The neutral verb is the only clause true of all three, and it
		// still names both elements so an author with several panes
		// open knows which paste failed. An earlier draft of this
		// comment justified the reword with "canHold already refused
		// every parenting fault before the append": canHold's
		// "Permissive where the catalog is silent, because the build is
		// the gate" says the opposite in its own words, and
		// TestCanHoldIsPermissiveWhereTheCatalogIsSilent pins it.
		// Corrected in review of #501.
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
	var envelopeWhy string
	if err == nil {
		if inner, ok, why := unwrapGooey(n); ok {
			n = inner
		} else {
			envelopeWhy = why
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
	// AND A REFUSED ENVELOPE SAYS WHY. unwrapGooey decides the refusal
	// and now carries the sentence with it, rather than this caller
	// re-deriving one arm of it. Raised in review of #522.
	if envelopeWhy != "" {
		ed.status.Set("✗ not pasted: " + envelopeWhy)
		return
	}
	// A DECLARATION PASTED ON ITS OWN NEVER REACHED unwrapGooey. It
	// returns immediately when n.Elem != "Gooey", so <x:Property/> with
	// no envelope is not an envelope refusal: it fell through to
	// insertSubtree, planAdd found no spec for "Property", node.markup
	// wrote it with no prefix, and the rebuild answered "markup: unknown
	// element <Property>" — verbatim the string splitDecls' own doc
	// calls out as the one the author must not be shown. The document
	// survives (the revert-on-failed-rebuild guard above holds), so this
	// was a message defect rather than a data one.
	//
	// THIS BRANCH IS WHAT MADE IT DETECTABLE: n.Space now survives
	// deepCopy, so the namespace is still here to test. Raised in review
	// of #522.
	if why := bareDeclWhy(n); why != "" {
		ed.status.Set("✗ not pasted: " + why)
		return
	}
	ed.insertSubtree(n, "pasted markup:")
}

// bareDeclWhy is the refusal for a declaration pasted WITHOUT an
// envelope, and "" for anything else. It is the same three-arm question
// splitDecls asks of an envelope's children, asked of a lone node.
func bareDeclWhy(n *node) string {
	switch {
	case n.Space == markup.XNamespace && n.Elem != "Property":
		// THE PASTED NODE'S OWN BINDING, not the literal "x". Since
		// #472 a pasted node carries its xmlns:* declarations as
		// ordinary attributes, so <d:Foo xmlns:d="…x"/> is reported as
		// <d:Foo> rather than as an element the clipboard does not
		// hold. Raised in review of #522.
		prefix, bound := declBinding(n.Attrs)
		return alienDeclMsg([]*node{n}, prefix, bound)
	case n.Space == markup.XNamespace:
		// THE SAME BINDING THE ARM ABOVE READS. This one kept the
		// literal "x" when its sibling was moved onto declBinding, and
		// the test arm beside it could not see the difference: its
		// fixture binds x:, so the hardcoded string and the read one
		// print alike. A p:-bound declaration was reported as
		// <x:Property> — a prefix the clipboard does not hold, and one
		// that is actively wrong when the open document binds x: to
		// something else. Unbound falls back to markup's own literal,
		// which is what the "unprefixed" case below tells the author to
		// write. Raised in review of #522.
		prefix := declBindingOr(n.Attrs, declFallbackPrefix)
		return "<" + prefix + ":Property> is a dependency property declaration, not an " +
			"element: it belongs on a document's <Gooey> root, where it " +
			"defines that control's public surface, and a paste inserts one " +
			"element into the selection. Open the file it came from instead."
	case n.Elem == "Property":
		return bareDeclMsg(1)
	}
	return ""
}

// unwrapGooey strips a <Gooey> envelope with exactly one element in it.
//
// More than one and it is refused rather than guessed at: a whole page
// pasted into a selected <Text> has no single answer for where its
// elements go, and picking the first would drop the rest silently.
//
// THAT INCLUDES A DOCUMENT WITH <x:Property> DECLARATIONS, and the
// asymmetry with openWorkspaceFile is deliberate rather than missed.
// Opening one partitions the declarations off onto the envelope (#517),
// because the file HAS an envelope to keep them on. A paste lands in a
// document that already has its own, and a declaration silently merged
// into it would change the target control's public surface without the
// user asking; dropping it instead would lose it. Refusing the unwrap
// says so, and leaves the pasted text where the user can see it.
//
// It takes and returns a NODE rather than re-serializing the child and
// re-parsing it. Not because the round trip would corrupt a body — it
// would not, and believing otherwise was wrong: markup.BodyText's
// multi-line rule is strings.TrimSpace, which is idempotent, so a second
// pass changes nothing and a test written to catch it passes either way
// (mutation-checked). The reason is narrower and real: the round trip is
// a second parse that can FAIL, and a failure there would report a paste
// as unparseable after it had already parsed once.
//
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
// IT RETURNS THE REASON IT REFUSED, and the reason is the whole point
// of the paragraph above: the envelope falls through to insertSubtree,
// which reports "markup: unknown element <Gooey>" to somebody who has
// just copied a valid file. The first repair explained only the
// declaration arm, from pasteMarkup, which left the case this comment is
// actually written about — a whole page pasted into a selected <Text> —
// reporting the unknown-element string verbatim. Deciding the refusal
// and explaining it in two places is what let them diverge; they are one
// place now. An empty reason means this is not an envelope at all, which
// is not a refusal. Raised in review of #522.
func unwrapGooey(n *node) (inner *node, ok bool, why string) {
	if n.Elem != "Gooey" {
		return nil, false, ""
	}
	decls, kids, bare := splitDecls(n)
	// Hoisted rather than called in the guard and again in the body:
	// two walks are two places that have to keep agreeing about what an
	// alien is, and openWorkspaceFile already reads it once. Raised in
	// review of #522.
	alien := alienDecls(decls)
	switch {
	case len(bare) > 0:
		// Before the declaration arm, because a document whose
		// declarations are misnamespaced has a fault of its own and
		// the paste refusal would otherwise describe the document
		// wrongly — it has no declarations markup can see. Raised in
		// review of #522.
		return nil, false, bareDeclMsg(len(bare))
	case len(alien) > 0:
		// BEFORE THE DECLARATION ARM, because these are not
		// declarations: markup refuses <x:Foo> outright. Calling them
		// declarations here would send the author to read about merging
		// a public surface for an element that has none.
		prefix, bound := declBinding(n.Attrs)
		return nil, false, alienDeclMsg(alien, prefix, bound)
	case len(decls) > 0:
		noun := "declarations"
		if len(decls) == 1 {
			noun = "declaration"
		}
		return nil, false, fmt.Sprintf("this document declares %d property %s on its "+
			"<Gooey>, and a paste lands inside a document that already has an "+
			"envelope of its own — merging them would change this control's "+
			"public surface. Open the file instead, or paste just the element "+
			"you want.", len(decls), noun)
	case len(n.Slots) != 0:
		return nil, false, "this <Gooey> carries property-element content of its " +
			"own, which belongs to the document it came from rather than to any " +
			"element in this one. Paste just the element you want."
	case len(kids) != 1:
		return nil, false, fmt.Sprintf("this is a whole document with %d root "+
			"elements, and a paste inserts ONE element into the selection. "+
			"Open the file instead, or copy just the element you want.", len(kids))
	}
	// kids[0], NOT n.Kids[0]. The two are equal only because the arms
	// above returned on every child splitDecls filed elsewhere, which
	// makes this line's correctness a property of the arm ORDERING —
	// and the ordering is exactly what the comments above it argue
	// about. Add or reorder an arm and carryDeclarations silently starts
	// carrying the envelope's binding onto a declaration and returning a
	// declaration as the pasted element. Raised in review of #522.
	carryDeclarations(n, kids[0])
	return kids[0], true, ""
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
//
// AND ed.envAttrs, WHICH IS A THIRD SCOPE AND NOT A SECOND. The
// paragraph above enumerated two and there are four — three when this
// was written, and the fourth below is what a growing envelope did to
// the count, which is the reason a reader should not take the number
// from prose here either: ed.root is excluded because it is NOT
// in the save, and ed.envAttrs is included for the mirror-image reason
// — it is what the saved <Gooey> carries and it is not reachable from
// ed.doc(). Open a document whose envelope keeps xmlns:x (an element
// prefix stays there through an open, which
// TestAnElementPrefixStaysOnTheEnvelopeThroughAnOpen measures), paste a
// fragment binding x to something else, and with only ed.doc()
// collected there is no conflict to report: the second binding lands
// inside the document and is written to disk. Small blast radius today
// — x: is an element prefix and the <x:Property> elements it exists for
// are siblings of the content root — but the enumeration was one scope
// short, not the reach. Raised in review of #501.
//
// AND A FOURTH, WHICH IS ed.envDecls AND THE BINDING MINTED BESIDE IT.
// The third-scope paragraph above was written when the saved envelope
// was gooeyOpen(ed.envAttrs) and nothing else. It is now
// envelopeHead(ed.envAttrs, ed.envDecls) (main.go), which writes two
// bindings ed.envAttrs does not hold: each declaration's own xmlns:*,
// re-emitted by declAttrs, and a freshly minted xmlns:<prefix> on
// <Gooey> whenever declPrefix reports the document binds the namespace
// nowhere the save will still carry. Both land in the file and both
// enter markup.parse's one flat document-wide table, so both are
// rebindable by a paste, and neither was compared.
//
// Measured on this branch before the fix, through openWorkspaceFile and
// pasteMarkup: a file whose <Gooey> binds nothing and whose
// <p:Property> carries its own xmlns:p accepted a paste binding p: to a
// different URI and wrote it to disk, while the byte-identical paste
// into a document holding that binding on the envelope was refused.
// The editor's answer turned on which of two places the binding had
// reached the file from, which is not a distinction the author can see.
// Raised in review of #522.
//
// envelopeNamespaces rather than a second reading of ed.envDecls here:
// what matters is what the SAVE writes, minting and declAttrs' drops
// included, and that decision lives in one function beside the writer.
// A mirror of it in this file is how the enumeration went one scope
// short twice.
//
// SEEDED FIRST, then overwritten by the document's own. The envelope is
// the outermost element and its declaration children sit between it and
// the content root, so if a prefix is declared in both, the document's
// declaration is the later one and markup.parse's last-wins is what
// this has to agree with.
func (ed *editor) reconcileNamespaces(n *node) error {
	doc := map[string]string{}
	envelopeNamespaces(ed.envAttrs, ed.envDecls, doc)
	collectNamespaces(ed.doc(), doc)
	return reconcileNamespacesInto(n, doc, map[string]string{})
}

// SORTED for the same reason collectNamespaces is, and for a different
// consequence: any conflict anywhere refuses, so accept-vs-refuse does
// not depend on the order, but WHICH conflict the message names does —
// and the suite matches on that text. Raised in review of #501.
func reconcileNamespacesInto(n *node, doc, own map[string]string) error {
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
		bound, fromDoc := doc[k]
		ok := fromDoc
		if !ok {
			bound, ok = own[k]
		}
		if !ok {
			// RECORDED, so the REST OF THE FRAGMENT is compared against
			// it. Without this line the walk only ever compares a
			// declaration against the DOCUMENT's, so a fragment holding
			// two bindings of one prefix — an outer xmlns:t="urn:A" and
			// an inner xmlns:t="urn:B", or two siblings — sailed
			// through and landed both in the file. Measured before the
			// fix: reconcileNamespacesInto returned nil for
			// `<Canvas xmlns:t="urn:A"><Button xmlns:t="urn:B"/></Canvas>`
			// and for the sibling spelling of it. That is the exact
			// rebind this function exists to refuse, caught when the
			// second binding is the document's and missed when both are
			// the paste's, and newly reachable for the same reason the
			// document-vs-paste case is: before this branch the editor
			// dropped declarations on the read, so it could not hold two
			// bindings of one prefix at all.
			//
			// ONE MAP, NOT A COPY PER SUBTREE, because markup.parse
			// keeps ONE FLAT ns map for the whole document
			// (markup.parse's `a.Name.Space == "xmlns"` arm — every
			// xmlns: attribute at any
			// depth, no scoping, last wins). A per-subtree copy would
			// fix the nesting case and leave the sibling one, where the
			// loader's table conflicts just as hard.
			own[k] = v
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
			//
			// AND THE MECHANISM IS NOT ONE MECHANISM, which is why this
			// branches. The sentence above is true of a prefix that names
			// EXPRESSIONS — a handler or value namespace, resolved
			// through markup.parse's flat ns table. It is not how the x
			// namespace resolves: x: names ELEMENTS, and encoding/xml
			// has already applied real XML subtree scoping to
			// t.Name.Space before markup sees the token, which is the
			// distinction carryDeclarations refuses to move
			// markup.XNamespace on and TestTheXPropertyRefusalNamesTheRoot
			// is about. Two bindings of x: do not merge and one does not
			// win: both stand, and which one an element means depends on
			// where it sits.
			//
			// The refusal is the same either way, and deliberately so —
			// parse's flat table takes EVERY xmlns attribute at any
			// depth, x: included, so the second binding still re-points
			// any expression under that prefix even where the elements
			// scope. What changes is only what the author is told, and a
			// message that tells them why is making a claim this repo
			// holds under test like any other. The one end-to-end
			// exercise of this refusal,
			// TestAPasteCannotRebindAPrefixTheEnvelopeHolds, rebinds x:
			// — so the arm that reached it was the arm whose explanation
			// was wrong. That test asserts this text now. Raised in
			// review of #501.
			// THE CLOSING CLAUSE IS PER-BRANCH, because the two
			// refusals below do not share its fact. "which one depends
			// on where this lands" is the DOCUMENT-vs-paste mechanism:
			// there the winner genuinely turns on whether the fragment
			// is inserted before or after the document's own
			// declaration. On the fragment-internal branch both
			// bindings are inside the clipboard, their relative
			// document order is fixed by the fragment itself, and
			// markup.parse's flat last-wins table therefore always
			// hands the prefix to the later declaration IN THE
			// FRAGMENT, wherever it lands.
			//
			// Shared, it told an author the outcome was
			// position-dependent while the second half of the same
			// sentence correctly told them the conflict was entirely
			// inside what they copied — two clauses of one message
			// contradicting each other. Same class as the party clause
			// the round before removed: a clause asserting something
			// the code has not established. Pinned now by
			// TestAPasteCannotRebindAPrefixAgainstITSELF, which
			// asserted the party and the remedy and nothing about the
			// mechanism, so this wording was free to drift. Raised in
			// review of #501.
			tail := " — the later of the two in what you pasted wins, " +
				"wherever it lands"
			if fromDoc {
				tail = " — which one depends on where this lands"
			}
			mech := "One flat prefix map covers the whole document and the last " +
				"declaration parsed wins, so one of the two meanings of " +
				strings.TrimPrefix(k, "xmlns:") + " would silently become the " +
				"other" + tail
			if bound == markup.XNamespace || v == markup.XNamespace {
				mech = "Expressions under " + strings.TrimPrefix(k, "xmlns:") +
					" read one flat document-wide table that this second " +
					"declaration re-points, so one of their two meanings would " +
					"silently become the other. This prefix also names " +
					"ELEMENTS, which XML scopes to the subtree that declares " +
					"them — so in the saved file, once it leaves the designer, " +
					"both bindings would stand and what an element means would " +
					"depend on where it sits"
			}
			// WHICH PARTY HOLDS THE OTHER BINDING IS TRACKED, NOT
			// ASSUMED. `own` above records a declaration the FRAGMENT
			// made, so that the rest of the fragment is compared against
			// it — which means a conflict here can be entirely inside
			// the clipboard, with the document declaring nothing at all.
			// The single message this used to have asserted the document
			// as the other party regardless, and sent the author to look
			// for a declaration that is not in their file: measured into
			// the default page, whose source is a <Gooey> over a bare
			// <Canvas Name="Root"> and which declares nothing, pasting
			// `<Canvas xmlns:t="urn:A"><Button xmlns:t="urn:B"/></Canvas>`
			// said `this document already declares it as "urn:A"`. The
			// sibling and cousin spellings said the same. That is the
			// misattribution class this branch spent round 14 removing
			// from three backstop seams — a clause before the colon
			// naming a party the code has not established — arriving at
			// the message the round before had just added a branch to.
			//
			// The REMEDY is the half that actually cost the author
			// something: "change the document's own declaration" is not
			// available when both bindings are in the clipboard, and the
			// one they can fix — the outer declaration in what they are
			// pasting — was the one being named as the document's.
			// Raised in review of #501.
			if !fromDoc {
				return fmt.Errorf("the pasted markup declares %s twice, as %q "+
					"and as %q. %s. Rename one of the two in what you are "+
					"pasting — this conflict is entirely inside what you "+
					"copied, so the open document has nothing to change",
					k, bound, v, mech)
			}
			return fmt.Errorf("the pasted markup declares %s=%q and this document "+
				"already declares it as %q. %s. Rename the prefix in what you are "+
				"pasting, or change the document's own declaration deliberately",
				k, v, bound, mech)
		}
		delete(n.Attrs, k)
	}
	for _, name := range sortedKeys(n.Slots) {
		if err := reconcileNamespacesInto(n.Slots[name], doc, own); err != nil {
			return err
		}
	}
	for _, k := range n.Kids {
		if err := reconcileNamespacesInto(k, doc, own); err != nil {
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
// attributes AND slot names back out — and only ONE of those two sorts
// is load-bearing, which the round that added them got wrong in the
// comment. "Last wins" is a claim about ORDER, and ranging a map has
// none, so the SLOTS walk was a real defect: four slots declaring
// xmlns:t to four different URIs resolved to all four over 500 runs,
// and the same document accepted a paste on one run and refused it on
// the next. The ATTRIBUTES walk cannot hold that bug at all. n.Attrs is
// a map keyed by attribute name, so one element declaring a prefix
// twice is not a state this model can represent — the second spelling
// overwrote the first long before this walk — and sorting distinct keys
// cannot change which URI a prefix ends up with. That sort is here for
// agreement with node.markup, not for correctness, and saying otherwise
// credited a guard with catching something it never could. Corrected in
// review of #501.
//
// PREFIXED DECLARATIONS ONLY, because that is what the decision reads.
// This recorded the plain "xmlns" too, and reconcileNamespacesInto
// SKIPPED k == "xmlns" above the doc[k] lookup at the time, so the entry
// was unreachable — two functions disagreeing about what counts as a
// declaration, which is how the default-namespace bug that skip records
// got in. PAST TENSE, because that skip is gone: the predicate there is
// `!isNamespaceAttr(k)` now, and isNamespaceAttr's own doc carries why.
// Written in the present it made two comments in one file disagree about
// what the code does, which is the class this branch spends its rounds
// deleting. Raised in review of #501, both halves.
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
// AND IT IS SHARED NOW, which is the other half of that round's finding.
// While this matched the plain form it could not be: every caller that
// must NOT move a plain xmlns spelled the prefixed test inline, and the
// comment here recorded that as the reason. The narrowing made the reason
// moot and left identical predicates scattered with nothing explaining
// why; they call this instead.
//
// NO COUNT OF THEM HERE. This said "FOUR CALLERS NOW" and named three,
// and the very commit that wrote it added a fifth — which is the
// hand-maintained count this branch had already removed from gooeyOpen's
// doc two commits earlier, and the one CLAUDE.md's Verify section refuses
// in prose: a number in a comment is a sample taken once. The reason the
// predicate is shared is the paragraph's point and it survives without an
// arithmetic claim; `grep -n 'isNamespaceAttr(' apps/wysiwyg/*.go` is the
// current answer. Raised in review of #501, twice.
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
