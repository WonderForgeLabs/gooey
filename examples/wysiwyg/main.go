// wysiwyg: a terminal UI builder that edits gooey markup, driven
// entirely by the component catalog.
//
// This is the catalog's first real consumer, and it exists to test the
// SHAPE rather than to be a finished editor. Three claims from
// docs/specs/2026-08-11-component-catalog-and-wysiwyg-builder.md are
// exercised here against running code:
//
//  1. The palette comes from (*markup.Context).Catalog(), and an element
//     whose attributes are not knowable renders DIFFERENTLY from one
//     that takes no attributes. A registered component is deliberately
//     present so that case is visible rather than theoretical.
//
//  2. The inspector comes from markup.AttrsFor(spec, parent) — never
//     from spec.Attrs directly, which is a true statement about the
//     element and a misleading answer to "what can I set here".
//
//  3. Attached properties are scoped to the PARENT. Press `c` and `v` to
//     retype the container between <Canvas> and <VStack> and watch
//     Canvas.Left enter and leave the selected child's attribute list.
//     That is the rule a flat per-element list cannot express, and the
//     one whose absence would have the editor offering positioning that
//     applyLayout silently discards.
//
// # Layout, and the one structural rule
//
//	┌────────────────────────────┬──────────────────┐
//	│ Preview   (REBUILT)        │ Palette          │
//	├────────────────────────────┼──────────────────┤
//	│ Markup output              │ Inspector INPUTS │
//	└────────────────────────────┴──────────────────┘
//
// The preview subtree is thrown away and rebuilt on every edit.
// Replacing a subtree resets component-local state, and a caret is
// component-local: an input inside the rebuilt island would jump to
// offset 0 on every keystroke, so the user's next character lands in the
// middle of their own text. The mitigation is structural, and it is the
// one that can be checked rather than remembered — every input is a
// SIBLING of the preview, asserted by
// TestEditorInputsAreSiblingsOfThePreview.
//
// # Keys
//
//	tab / shift+tab   move between panes
//	up / down         move within a list
//	enter             palette: add the element; attributes: edit it
//	c / v             retype the container (Canvas / VStack)
//	x                 delete the selected element
//	q, ctrl+c         quit
//
// Everything is keyboard-operable on purpose: mouse events cannot be
// exercised under a recording pty, so a demo that needed one could never
// be captured.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

func main() {
	addr := flag.String("attach", "", "drive a remote gooey app at this loopback address instead of previewing locally")
	island := flag.String("island", "", "the Name= of the element in the remote app this editor owns")
	flag.Parse()

	ed := newEditor()
	dir := "."
	if _, err := os.Stat(filepath.Join(dir, "wysiwyg.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}
	app := gooey.NewApp(markup.Page(os.DirFS(dir), "wysiwyg.gooey", ed.ctx))
	ed.app = app
	ed.ctx.Dispatcher = app.Dispatcher()
	ed.watchFit(app)

	if *addr != "" {
		if *island == "" {
			gooey.Exit(fmt.Errorf("-attach needs -island: the editor owns exactly one named element in the target and never writes outside it"))
		}
		// The context handed to attach governs the STREAM's lifetime,
		// not just the handshake — Attach(ctx) lives as long as ctx
		// does. A timeout here silently kills every session when it
		// expires, which reads to a user as the editor freezing.
		// Cancellation belongs to the app's lifetime instead.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if err := ed.attach(ctx, *addr, *island); err != nil {
			gooey.Exit(err)
		}
		defer ed.remote.r.Close()
	}
	ed.rebuild()
	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

// ---- the document being edited ----

// node is one element of the edited document. It is deliberately not a
// gooey component: the editor manipulates a document, and the tree is
// derived from it.
type node struct {
	Elem  string
	Attrs map[string]string
	Kids  []*node
	// Slots are property elements — <ItemsView.ItemTemplate> — which
	// are structured attributes rather than children, and which the
	// catalog can report as REQUIRED.
	Slots map[string]*node
}

func (n *node) markup(indent string) string {
	var b strings.Builder
	b.WriteString(indent + "<" + n.Elem)
	keys := make([]string, 0, len(n.Attrs))
	for k := range n.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%q", k, n.Attrs[k])
	}
	if len(n.Kids) == 0 && len(n.Slots) == 0 {
		b.WriteString("/>\n")
		return b.String()
	}
	b.WriteString(">\n")
	slots := make([]string, 0, len(n.Slots))
	for k := range n.Slots {
		slots = append(slots, k)
	}
	sort.Strings(slots)
	for _, s := range slots {
		fmt.Fprintf(&b, "%s  <%s.%s>\n", indent, n.Elem, s)
		b.WriteString(n.Slots[s].markup(indent + "    "))
		fmt.Fprintf(&b, "%s  </%s.%s>\n", indent, n.Elem, s)
	}
	for _, k := range n.Kids {
		b.WriteString(k.markup(indent + "  "))
	}
	b.WriteString(indent + "</" + n.Elem + ">\n")
	return b.String()
}

// ---- the editor ----

type editor struct {
	// ctx builds the EDITOR; docCtx is the vocabulary the DOCUMENT is
	// authored in. See newEditor for why they are separate.
	ctx    *markup.Context
	docCtx *markup.Context

	root     *node
	selected int // index into root.Kids; -1 selects the container itself

	palette    []markup.ElementSpec
	paletteSel *prop.Property[int]
	attrSel    *prop.Property[int]

	// Bindable surface.
	paletteItems *prop.Property[components.ItemSource]
	attrItems    *prop.Property[components.ItemSource]
	source       *prop.Property[string]
	status       *prop.Property[string]
	editName     *prop.Property[string]
	editValue    *prop.Property[string]
	editDoc      *prop.Property[string]
	treeText     *prop.Property[string]
	// fits is false while the terminal is too small to lay the shell out,
	// and it drives Visibility on both roots. cramped is its inverse, and
	// it is a computed rather than a second source: two sources for one
	// fact drift, and the frame where they disagree shows either both
	// roots or neither.
	fits    *prop.Property[bool]
	cramped *prop.Property[bool]
	fitMsg  *prop.Property[string]
	// rev ticks on every edit. The list sources are computed over the
	// document, which is plain Go state and therefore invisible to the
	// property graph — a computed that reads no property records no
	// dependency and caches forever. Reading rev is what gives them
	// something to invalidate on.
	rev *prop.Property[int]

	pv  *preview
	app *gooey.App

	// remote, when set, means the editor is driving ANOTHER app: the
	// document is patched into that app's island instead of previewed
	// in this process. Nil is local mode.
	remote *remoteTarget

	// mu guards lost, which the stream reader writes and the UI reads.
	mu      sync.Mutex
	lost    error
	swapped bool
}

func newEditor() *editor {
	ed := &editor{
		root: &node{Elem: "Canvas", Attrs: map[string]string{"Name": "Root"}, Kids: []*node{
			{Elem: "Text", Attrs: map[string]string{"Name": "T1", "Canvas.Left": "2", "Canvas.Top": "1"}},
			{Elem: "Button", Attrs: map[string]string{"Name": "B1", "Content": "click", "Canvas.Left": "2", "Canvas.Top": "3"}},
		}},
		selected:   0,
		paletteSel: prop.NewSource(0),
		attrSel:    prop.NewSource(0),
		source:     prop.NewSource(""),
		status:     prop.NewSource(""),
		editName:   prop.NewSource(""),
		editValue:  prop.NewSource(""),
		editDoc:    prop.NewSource(""),
		treeText:   prop.NewSource(""),
		fits:       prop.NewSource(true),
		fitMsg:     prop.NewSource(""),
		rev:        prop.NewSource(0),
		pv:         &preview{},
	}

	ed.cramped = prop.NewComputed(func() bool { return !ed.fits.Get() })

	// The list sources are built BEFORE the context, because
	// Context.Values captures each handle BY VALUE: a property created
	// after the map is populated leaves nil in the map, the bindings
	// resolve to nothing, and the palette renders empty with no error
	// anywhere. Both computeds read ed.palette lazily, so it is fine
	// that the palette itself is filled further down.
	ed.paletteItems = prop.NewComputed(func() components.ItemSource {
		ed.rev.Get() // hoisted above everything: the dependency is the point
		return components.ItemsOf(ed.palette, func(e markup.ElementSpec) map[string]any {
			return map[string]any{
				"Name":  e.Name,
				"Attrs": describeAttrs(e),
				"Kids":  string(e.Children.Mode),
			}
		})
	})
	ed.attrItems = prop.NewComputed(func() components.ItemSource {
		// Hoisted ABOVE the rows, and above any early return: a
		// dependency is recorded by the Get that actually runs, so a
		// Get placed after a branch can be skipped and the subscription
		// silently lost.
		ed.rev.Get()
		return components.ItemsOf(ed.attrRows(), func(r attrRow) map[string]any {
			name := r.name
			if r.req {
				// Required is a property of the attribute, so it belongs
				// on the attribute rather than in a separate column that
				// is blank for nine rows in ten.
				name += "*"
			}
			// The modified-from-default marker. A leading glyph rather
			// than a colour, because the one colour cue on this pane
			// already means "loaded into the editor" and giving it a
			// second meaning is the collapse this project keeps
			// cataloguing.
			mark := " "
			if r.modified {
				mark = "•"
			}
			return map[string]any{
				"Mark": mark, "Name": name, "Kind": r.kind,
				"Legal": r.legal, "Value": r.value,
			}
		})
	})

	// A registered component with NO schema, on purpose. Its palette row
	// must read differently from an element that simply takes no
	// attributes — that distinction is the catalog's central honesty
	// rule, and an editor is where it either shows up or is lost.
	// TWO CONTEXTS, and the split is the fix for a crash rather than
	// tidiness.
	//
	// docCtx is the vocabulary the DOCUMENT is authored in. ctx is what
	// the EDITOR's own shell is built with, and it additionally carries
	// the editor's furniture — <Preview>, <LogPane>. Those are not user
	// vocabulary; they are chrome that happens to live in a
	// markup.Context because the editor's page is markup too.
	//
	// Sourcing the palette from ctx conflated "elements this context can
	// build" with "elements the user is authoring with", and those differ
	// by exactly the editor's own furniture. Dropping <Preview> into the
	// document then made the document contain the thing that renders the
	// document: Measure recursed until the stack overflowed and took the
	// terminal with it.
	//
	// A denylist of the two names would re-break the day a third
	// component is added. Separating the contexts cannot: chrome is
	// registered in one place and the palette reads the other.
	ed.ctx = &markup.Context{
		Values: map[string]any{
			"Noop":         gooey.Command(func() {}),
			"PaletteItems": ed.paletteItems,
			"PaletteSel":   ed.paletteSel,
			"AttrItems":    ed.attrItems,
			"AttrSel":      ed.attrSel,
			"Source":       ed.source,
			"Status":       ed.status,
			"EditName":     ed.editName,
			"EditValue":    ed.editValue,
			"EditDoc":      ed.editDoc,
			"TreeText":     ed.treeText,
			"Fits":         ed.fits,
			"Cramped":      ed.cramped,
			"FitMsg":       ed.fitMsg,
			"Add":          gooey.Command(func() { ed.addSelected() }),
			"Delete":       gooey.Command(func() { ed.deleteSelected() }),
			"NextEl":       gooey.Command(func() { ed.selectNext(1) }),
			"PrevEl":       gooey.Command(func() { ed.selectNext(-1) }),
			"ToCanvas":     gooey.Command(func() { ed.retype("Canvas") }),
			"ToVStack":     gooey.Command(func() { ed.retype("VStack") }),
			"BeginEdit":    gooey.Command(func() { ed.beginEdit() }),
			"EditText":     gooey.Command(func() { ed.editSelectedAsText() }),
			"CommitEdit":   gooey.Command(func() { ed.commitEdit() }),
			"Quit":         gooey.Command(func() { ed.app.Quit() }),
		},
		Styles: map[string]render.Style{
			"dim":  {Fg: render.RGB(140, 140, 150)},
			"warn": {Fg: render.RGB(255, 170, 60)},
			// The attribute currently loaded into the editor. It is a
			// SELECTION cue, so it must not wear a warning colour —
			// orange on a perfectly healthy row reads as an error, which
			// is what it did and what got asked about.
			//
			// Deliberately NOT also the modified-from-default
			// indicator. That needs AttrSpec.Default, which does not
			// exist yet, and one cue meaning two things is the collapse
			// this project keeps cataloguing.
			"sel":   {Fg: render.RGB(180, 200, 255), Bold: true},
			"ok":    {Fg: render.RGB(120, 200, 140)},
			"title": {Fg: render.RGB(180, 200, 255), Bold: true},
		},
		Components: map[string]markup.Builder{
			// The island. Declared in markup so the layout — and the
			// sibling rule the layout exists to enforce — is visible in
			// one place rather than assembled in Go.
			"Preview": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				return ed.pv, nil
			},
		},
	}

	// docCtx shares the bindings and styles — a document authored here
	// binds the same names — but deliberately NOT Components. Every
	// entry there is editor chrome; see the comment above.
	ed.docCtx = &markup.Context{
		Values: ed.ctx.Values,
		Styles: ed.ctx.Styles,
		// The DOCUMENT's own registered vocabulary. <LogPane> stands in
		// for what a target app registers — a component the user
		// legitimately authors with, and whose attributes are
		// unknowable because a Builder is a func. That is the case the
		// palette must render differently from "takes no attributes",
		// so it belongs here rather than among the editor's chrome.
		Components: map[string]markup.Builder{
			"LogPane": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				return &components.Text{Content: components.Str("[LogPane]")}, nil
			},
			// <Preview> IS placeable in a document — the palette offers
			// it honestly. It simply builds something else here: an
			// Escher mirror rather than the real pane, so previewing a
			// document that contains a preview is a visual instead of a
			// stack overflow. See mirror.go.
			"Preview": func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
				return &mirror{style: ctx.Styles["dim"]}, nil
			},
		},
	}

	// The palette IS the catalog. Only elements that can appear in a
	// container are offered; the non-visual ones are attachments and
	// belong to a different gesture than "add a child".
	for _, e := range ed.docCtx.Catalog() {
		if e.NonVisual || e.Name == "Tab" {
			continue
		}
		ed.palette = append(ed.palette, e)
	}

	return ed
}

// describeAttrs is the palette's honesty column, and the reason the
// catalog splits AttrsKnown from Origin. "no attributes" and "attributes
// unknown" are different facts about the app and must not render alike.
func describeAttrs(e markup.ElementSpec) string {
	if !e.AttrsKnown {
		return "? unknown"
	}
	n := len(e.Attrs)
	if e.Open {
		return fmt.Sprintf("%d +open", n)
	}
	if n == 0 {
		return "none"
	}
	return fmt.Sprintf("%d", n)
}

// attrRow is one inspector line. kind/legal/value came from AttrSpec
// all along — Enum, Binds and Required are populated by the catalog and
// were simply never read, which is most of what "the inspector doesn't
// show binding or enum" meant.
// legalValues says how an attribute may be written: the enum's members
// where there are members, otherwise how it binds. A KindEnum row that
// does not list its own values is the catalog knowing something the user
// has to guess.
func legalValues(a markup.AttrSpec) string {
	if len(a.Enum) > 0 {
		return strings.Join(a.Enum, "|")
	}
	switch a.Binds {
	case markup.BindsBinding:
		// Binding-only: a literal is a load error, so say so before the
		// user types one.
		if a.GoType != "" {
			return "{{." + a.GoType + "}}"
		}
		return "{{binding}}"
	case markup.BindsEither:
		return "text or {{…}}"
	}
	return ""
}

type attrRow struct {
	name  string
	kind  string
	legal string // the accepted values, or how the attribute may be written
	value string
	doc   string
	req   bool
	// cat is the property-grid group, from markup.CategoryOf.
	cat string
	// def is the declared default, empty where the catalog declares none.
	def string
	// values is the finite set this attribute may cycle through, or nil
	// where the value is free text. It is the per-Kind editor, resolved
	// against the LIVE context — a style list is whatever the app
	// registered, not a guess — and it is what makes enter mean "next
	// legal value" on the rows that have one.
	values []string
	// modified is the Visual Studio rule: a row differs from its default.
	// It is deliberately NOT the same cue as the selection highlight —
	// one colour meaning two things is the collapse this project keeps
	// cataloguing, and it is why the selection cue was left neutral when
	// this field did not yet exist.
	//
	// An attribute with no declared Default is never "modified", because
	// there is nothing to differ from. That is a third state, not a
	// false: markup.AttrSpec.Default is empty exactly where nothing can
	// check it.
	modified bool
}

// isModified compares the document's value against the declared default.
// Absent means default, and a value equal to the default is not a
// modification even though it was written — which matches what a VS grid
// shows and, more importantly, matches what the markup does.
func isModified(a markup.AttrSpec, value string) bool {
	if a.Default == "" {
		return false
	}
	if value == "" {
		return false
	}
	return value != a.Default
}

// valueSet is the per-Kind editor, expressed as data rather than as a
// widget: the finite list of values an attribute may take, or nil where
// it is free text.
//
// Every list comes from the RUNNING app, which is the point. A style
// list that was a hardcoded table would offer names the app does not
// have and omit the ones it does; asking Context.Styles cannot. Three of
// the four introspection questions meet here — the catalog says what the
// attribute is, Context.Styles says which styles exist, Context.Values
// says which commands do — and a property grid is those three answers in
// rows.
//
// The dispatch is a switch on a string Kind. No reflection, and the same
// mechanism boundProp uses.
func (ed *editor) valueSet(a markup.AttrSpec) []string {
	switch a.Kind {
	case markup.KindEnum:
		return a.Enum
	case markup.KindBool:
		return []string{"true", "false"}
	case markup.KindStyle:
		return sortedKeys(ed.docCtx.Styles)
	case markup.KindCommand:
		return ed.commandBindings()
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// commandBindings is every bindable name that is actually an Action,
// spelled the way it has to be written in markup. A Click= offered a
// name that is not a command would produce a load error from a list the
// editor itself supplied.
//
// gooey.Action is an interface, so this is a type assertion — the test
// the framework already applies to the same values, not reflection.
func (ed *editor) commandBindings() []string {
	var out []string
	for name, v := range ed.docCtx.Values {
		if _, ok := v.(gooey.Action); ok {
			out = append(out, "{{."+name+"}}")
		}
	}
	sort.Strings(out)
	return out
}

// cycle is the list enter walks for a row: the legal values, preceded by
// UNSET for anything not required.
//
// Unset has to be in the cycle or an optional attribute could be given a
// value and never taken back, which would make the cycling editor a
// one-way door. A required attribute has no unset state — removing it is
// a load error — so it is not offered one.
func (r attrRow) cycle() []string {
	if len(r.values) == 0 {
		return nil
	}
	if r.req {
		return r.values
	}
	return append([]string{""}, r.values...)
}

// attrRows is claim 2 and claim 3 together: the inspector asks
// AttrsFor(spec, parent), so the answer depends on what the element is
// currently inside.
func (ed *editor) attrRows() []attrRow {
	spec, parent, target := ed.target()
	if target == nil {
		return nil
	}
	var rows []attrRow
	for _, a := range markup.AttrsFor(spec, parent) {
		v := target.Attrs[a.Name]
		rows = append(rows, attrRow{
			name:     a.Name,
			kind:     string(a.Kind),
			legal:    legalValues(a),
			value:    v,
			doc:      a.Doc,
			req:      a.Required,
			cat:      markup.CategoryOf(a),
			def:      a.Default,
			values:   ed.valueSet(a),
			modified: isModified(a, v),
		})
	}
	// Grouped by category, then by name inside a group — the Visual
	// Studio categorised view. AttrsFor returns name order, so this is a
	// stable re-sort rather than a different answer.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].cat != rows[j].cat {
			return categoryRank(rows[i].cat) < categoryRank(rows[j].cat)
		}
		return rows[i].name < rows[j].name
	})
	return rows
}

// categoryRank puts the groups in the order a person reads them rather
// than alphabetically: what the thing is, then where it sits, then how it
// looks, then what it does.
func categoryRank(c string) int {
	switch c {
	case markup.CategoryDesign:
		return 0
	case markup.CategoryCommon:
		return 1
	case markup.CategoryLayout:
		return 2
	case markup.CategoryAppearance:
		return 3
	case markup.CategoryEvents:
		return 4
	}
	return 5
}

// target resolves the selection to (spec, parent element name, node).
func (ed *editor) target() (markup.ElementSpec, string, *node) {
	n := ed.root
	parent := ""
	if ed.selected >= 0 && ed.selected < len(ed.root.Kids) {
		n = ed.root.Kids[ed.selected]
		parent = ed.root.Elem
	}
	for _, e := range ed.palette {
		if e.Name == n.Elem {
			return e, parent, n
		}
	}
	return markup.ElementSpec{Name: n.Elem}, parent, n
}

// ---- edits ----

func (ed *editor) rebuild() {
	// Tick FIRST, so every derived list recomputes even if the build
	// below fails and returns early.
	ed.rev.Set(ed.rev.Get() + 1)
	src := "<Gooey>\n" + ed.root.markup("  ") + "</Gooey>\n"
	ed.source.Set(src)
	ed.treeText.Set(ed.outline())

	if ed.remote != nil {
		// Driving another app: the target's live binding context is the
		// only authority on whether this document loads, so validate
		// against IT rather than against the editor's own context.
		ed.pushRemote(src)
		return
	}

	// Built against the DOCUMENT vocabulary, so a document can never
	// contain the editor's own chrome.
	w, err := markup.Build([]byte(src), ed.docCtx)
	if err != nil {
		// A load error is normal while editing and must never take the
		// editor down with it. The previous preview stays on screen.
		ed.status.Set("✗ " + err.Error())
		return
	}
	ed.status.Set("✓ builds")
	ed.pv.swap(w)
}

func (ed *editor) outline() string {
	var b strings.Builder
	marker := "  "
	if ed.selected < 0 {
		marker = "> "
	}
	fmt.Fprintf(&b, "%s<%s>\n", marker, ed.root.Elem)
	for i, k := range ed.root.Kids {
		marker = "  "
		if i == ed.selected {
			marker = "> "
		}
		fmt.Fprintf(&b, "%s  <%s Name=%q>\n", marker, k.Elem, k.Attrs["Name"])
	}
	return b.String()
}

func (ed *editor) addSelected() {
	i := ed.paletteSel.Get()
	if i < 0 || i >= len(ed.palette) {
		return
	}
	spec := ed.palette[i]
	n := &node{Elem: spec.Name, Attrs: map[string]string{
		"Name": fmt.Sprintf("%s%d", spec.Name, len(ed.root.Kids)+1),
	}}
	ed.seedRequired(spec, n)
	for _, sl := range spec.Slots {
		if !sl.Required {
			continue
		}
		if n.Slots == nil {
			n.Slots = map[string]*node{}
		}
		n.Slots[sl.Name] = &node{Elem: "Text", Attrs: map[string]string{}}
	}
	// A couple of shapes the catalog still cannot express: <Border>
	// needs exactly one child, and <Tabs> needs a <Tab>. Children.Mode
	// says ModeOne and ModeRestricted respectively, so the information
	// IS there — turning it into a default subtree is editor work, not a
	// catalog gap.
	switch spec.Children.Mode {
	case markup.ModeOne:
		n.Kids = append(n.Kids, &node{Elem: "Text", Attrs: map[string]string{}})
	case markup.ModeRestricted:
		for _, only := range spec.Children.Only {
			n.Kids = append(n.Kids, &node{
				Elem:  only,
				Attrs: map[string]string{"Header": "tab"},
				Kids:  []*node{{Elem: "Text", Attrs: map[string]string{}}},
			})
		}
	}
	if ed.root.Elem == "Canvas" {
		n.Attrs["Canvas.Left"] = "2"
		n.Attrs["Canvas.Top"] = fmt.Sprint(len(ed.root.Kids)*2 + 1)
	}
	ed.root.Kids = append(ed.root.Kids, n)
	ed.selected = len(ed.root.Kids) - 1
	ed.rebuild()
}

// seedRequired gives every required attribute a value, which is what
// makes "click a palette entry" produce markup that actually loads.
//
// This is the axis the catalog was missing until an editor tried to use
// it. "What can I set on this element" turns out to be half the
// question: eleven elements produced markup that would not build when
// added bare, because a binding-only attribute with no value fails at
// bind time. AttrSpec.Required is the other half.
//
// A required BINDING needs somewhere to bind to, so the editor grows the
// viewmodel — the in-process equivalent of register_properties. The Go
// type comes from AttrSpec.GoType, dispatched by a switch on the type's
// spelling: a string compared against known names, not reflection.
func (ed *editor) seedRequired(spec markup.ElementSpec, n *node) {
	for _, a := range markup.AttrsFor(spec, ed.root.Elem) {
		if !a.Required {
			continue
		}
		if a.Binds == markup.BindsLiteral {
			n.Attrs[a.Name] = literalFor(a)
			continue
		}
		name := fmt.Sprintf("%s_%s", n.Attrs["Name"], a.Name)
		if h := newHandle(a.GoType); h != nil {
			ed.ctx.Values[name] = h
			n.Attrs[a.Name] = "{{." + name + "}}"
			continue
		}
		// No handle for this type: say so rather than emit a binding
		// that cannot resolve.
		ed.status.Set("✗ no placeholder for " + a.GoType)
	}
}

// newHandle makes a zero-valued source property for a declared GoType.
// The switch is the whole type check — the same mechanism boundProp
// uses, and the reason none of this needs reflection.
func newHandle(goType string) any {
	switch goType {
	case "string":
		return prop.NewSource("")
	case "int":
		return prop.NewSource(0)
	case "bool":
		return prop.NewSource(false)
	case "[]float64":
		return prop.NewSource([]float64{1, 2, 3, 2})
	case "[]string":
		return prop.NewSource([]string{"one", "two"})
	case "render.Color":
		return prop.NewSource(render.RGB(120, 200, 140))
	case "components.ItemSource":
		return prop.NewSource(components.ItemsOf([]string{"one", "two"},
			func(s string) map[string]any { return map[string]any{"Label": s} }))
	}
	return nil
}

func literalFor(a markup.AttrSpec) string {
	switch a.Kind {
	case markup.KindDuration:
		return "600ms"
	case markup.KindInt:
		return "1"
	case markup.KindBool:
		return "true"
	case markup.KindEnum:
		if len(a.Enum) > 0 {
			return a.Enum[0]
		}
	}
	return "x"
}

func (ed *editor) deleteSelected() {
	if ed.selected < 0 || ed.selected >= len(ed.root.Kids) {
		return
	}
	ed.root.Kids = append(ed.root.Kids[:ed.selected], ed.root.Kids[ed.selected+1:]...)
	if ed.selected >= len(ed.root.Kids) {
		ed.selected = len(ed.root.Kids) - 1
	}
	ed.rebuild()
}

// retype is the experiment. Changing the container changes which
// ATTACHED properties its children may carry, and the inspector has to
// follow — Canvas.Left is meaningful under a <Canvas> and silently
// dropped under a <VStack>.
func (ed *editor) retype(elem string) {
	if ed.root.Elem == elem {
		return
	}
	ed.root.Elem = elem
	for _, k := range ed.root.Kids {
		// Attributes the new parent does not contribute are removed
		// rather than left to be ignored. Leaving them is what the old
		// loader did, and it is the defect this whole change deletes.
		for _, p := range markup.AttachedParents() {
			if p == elem {
				continue
			}
			for _, a := range markup.AttachedAttrs(p) {
				delete(k.Attrs, a.Name)
			}
		}
		if elem == "Canvas" {
			if _, ok := k.Attrs["Canvas.Left"]; !ok {
				k.Attrs["Canvas.Left"] = "2"
				k.Attrs["Canvas.Top"] = "1"
			}
		}
	}
	ed.rebuild()
}

func (ed *editor) selectNext(d int) {
	n := len(ed.root.Kids)
	if n == 0 {
		return
	}
	ed.selected = (ed.selected + d + n) % n
	ed.rebuild()
}

// selectedRow is the inspector row under the cursor, and whether there
// is one.
func (ed *editor) selectedRow() (attrRow, bool) {
	rows := ed.attrRows()
	i := ed.attrSel.Get()
	if i < 0 || i >= len(rows) {
		return attrRow{}, false
	}
	return rows[i], true
}

// beginEdit is enter on a row, and it DISPATCHES BY KIND — which is what
// "behave like the Visual Studio property grid" decomposes into.
//
// A row with a finite value set advances to the next one and commits.
// Typing "Center" into a field that accepts four literals is the
// terminal impersonating a text editor for something that is a choice,
// and it is how a typo becomes a load error instead of an impossibility.
// Everything else loads the text input as before.
//
// The text path stays reachable for every row through `e`
// (editSelectedAsText), because a cycling editor must not be the only
// way in: KindStyle and KindCommand are BindsEither, so the finite list
// is the common case and not the whole grammar.
func (ed *editor) beginEdit() {
	r, ok := ed.selectedRow()
	if !ok {
		return
	}
	if len(r.cycle()) > 0 {
		ed.cycleValue(r)
		return
	}
	ed.editAsText(r)
}

// editSelectedAsText is the escape hatch: the raw value in the text
// input, whatever the Kind.
func (ed *editor) editSelectedAsText() {
	if r, ok := ed.selectedRow(); ok {
		ed.editAsText(r)
	}
}

func (ed *editor) editAsText(r attrRow) {
	ed.editName.Set(r.name)
	ed.editValue.Set(r.value)
	ed.describe(r)
}

// cycleValue advances a finite-valued attribute to its next value and
// commits immediately. There is nothing to confirm: every member of the
// cycle came from the catalog or the live context, so all of them build.
func (ed *editor) cycleValue(r attrRow) {
	_, _, target := ed.target()
	if target == nil {
		return
	}
	vals := r.cycle()
	next := vals[0]
	for i, v := range vals {
		if v == r.value {
			next = vals[(i+1)%len(vals)]
			break
		}
	}
	if next == "" {
		delete(target.Attrs, r.name)
	} else {
		target.Attrs[r.name] = next
	}
	// Keep the text input pointed at the row being cycled, so `e` and the
	// input agree with what the list is showing.
	ed.editName.Set(r.name)
	ed.editValue.Set(next)
	ed.rebuild()
	if r2, ok := ed.selectedRow(); ok {
		ed.describe(r2)
	}
}

// describe fills the description pane: Doc where the catalog has prose,
// the legal values where it does not — so it is never blank while Doc is
// unpopulated. The category and the default ride along here rather than
// as two more columns: the pane has room and a 46-cell list does not.
func (ed *editor) describe(r attrRow) {
	head := r.cat + " · " + r.name
	if d := r.doc; d != "" {
		ed.editDoc.Set(head + " — " + d)
		return
	}
	tail := r.legal
	if r.def != "" {
		if tail != "" {
			tail += ", "
		}
		tail += "default " + r.def
	}
	if n := len(r.cycle()); n > 0 {
		if tail != "" {
			tail += ", "
		}
		tail += fmt.Sprintf("enter cycles %d", n)
	}
	if tail != "" {
		ed.editDoc.Set(head + ": " + tail)
		return
	}
	ed.editDoc.Set(head)
}

func (ed *editor) commitEdit() {
	name := ed.editName.Get()
	if name == "" {
		return
	}
	_, _, target := ed.target()
	if target == nil {
		return
	}
	if v := ed.editValue.Get(); v == "" {
		delete(target.Attrs, name)
	} else {
		target.Attrs[name] = v
	}
	ed.rebuild()
}
