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
	ed.clip = clipboard{node: n.deepCopy(), markup: src}
	msg := "cut " + describeNode(n) + ed.sayCopiedOut(src)
	// deleteSelected rebuilds, and rebuild sets the build status — so the
	// message goes on AFTER it or it is overwritten in the same frame by
	// "✓ builds". Learned the hard way: the cut worked and said nothing.
	ed.deleteSelected()
	ed.status.Set(msg)
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
	into := plan.into
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
	if into.Elem == "Canvas" && n.Attrs != nil {
		if _, ok := n.Attrs["Canvas.Left"]; ok {
			n.Attrs["Canvas.Top"] = fmt.Sprint(len(into.Kids)*2 + 1)
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
	into.Kids = append(into.Kids, add)
	ed.sel = n
	// Mutate, then rebuild — the mutation seam every other edit in this
	// editor uses (addSelected, deleteSelected, retype, commitEdit), and
	// the one slice-undo's undo.go hooks. Nothing here opts in to undo;
	// undo is derived at the choke point, which is why a future mutator
	// cannot forget it.
	ed.rebuild()
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
				specOf[old] = ed.specFor(k.Elem)
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

func (ed *editor) specFor(elem string) markup.ElementSpec {
	for _, e := range ed.palette {
		if e.Name == elem {
			return e
		}
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
		// nodeOf's messages are written for SEEDS, which is this repo's
		// own markup, so they say "seed" where a user needs "pasted
		// markup". Re-labelled here rather than by adding a noun
		// parameter to nodeOf, whose other caller genuinely is a seed.
		ed.status.Set("✗ pasted text is not markup: " +
			strings.Replace(err.Error(), "seed ", "pasted markup ", 1))
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
func unwrapGooey(n *node) (*node, bool) {
	if n.Elem != "Gooey" || len(n.Kids) != 1 || len(n.Slots) != 0 {
		return nil, false
	}
	return n.Kids[0], true
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
