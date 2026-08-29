package main

import (
	"image"
	"sort"
	"strconv"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// THE PER-KIND EDITOR TABLE.
//
// Visual Studio's property grid has one rule behind everything it does:
// an attribute is edited by the widget its TYPE deserves, and the type is
// known before the user touches anything. A colour gets a picker, an
// enumeration a dropdown, a number a spinner, a collection a designer —
// and the cell shows which of those you are about to get, with "…" for
// the ones that open something and "▾" for the ones that drop down.
//
// markup.Kind is that type, already declared, already travelling on every
// AttrSpec. So the mapping is a TABLE keyed by Kind, and the whole of the
// per-Kind behaviour is derived from it: which editor opens, whether it
// floats a surface, and what the row's affordance column shows. Nothing
// in this package writes a list of attribute names, and nothing decides
// "colour" by looking at whether the name contains "Color".
//
// THE TABLE IS TOTAL, AND THAT IS ENFORCED. editorForKind returns a
// second value rather than a default arm, because a default arm is how a
// new Kind gets silently edited as free text: the loader would then
// reject a value the editor itself invited.
// TestEveryDeclaredKindHasAnEditor reads the Kind constants out of
// markup/catalog.go and fails when one of them is missing from here — so
// adding a Kind without choosing its editor is a red test, not a surprise
// on click.
type editorKind int

const (
	// editNone is the zero value and is NOT an editor. It exists so a
	// Kind that fell out of the table is a detectable state rather than
	// an accidental text box.
	editNone editorKind = iota
	// editCaret is a caret in the cell: a TextBox floated over the row.
	// For values only prose can give.
	editCaret
	// editRename is editCaret's twin for KindIdentity. Name is the
	// ADDRESS, not a value — markup.KindIdentity's own doc says a
	// consumer "must decide what a rename means rather than defaulting
	// to a text box" — so the decision is made HERE, visibly, and the
	// editor warns instead of pretending it is Content.
	editRename
	// editStepper is an inline spinner for KindInt. No popup: a number
	// does not need a surface, it needs ◂ and ▸.
	editStepper
	// editChoice is the inline dropdown for a FINITE set — enum, bool,
	// and the app's live style table.
	editChoice
	// editBinding is the name picker for KindBinding and KindCommand:
	// the live context, filtered to what the attribute can accept.
	editBinding
	// editColor is components.ColorPicker floated under the row.
	editColor
	// editTracks is the Grid track editor for KindGridLens — one row per
	// track, editable in place, written back on every keystroke.
	editTracks
	// editGesture CAPTURES a chord instead of asking the user to spell
	// one. "ctrl+shift+pageup" is a sentence nobody types correctly from
	// memory; pressing it is exact by construction.
	editGesture
)

// editors is the table. Every markup.Kind appears exactly once.
//
// A map rather than a switch because the exhaustiveness test needs to
// ENUMERATE what is covered, and a switch cannot be enumerated without
// reflection or a parser. It is read-only after init.
var editors = map[markup.Kind]editorKind{
	// Free text. A caret is the only editor for a value the catalog
	// cannot enumerate.
	markup.KindText:     editCaret,
	markup.KindString:   editCaret,
	markup.KindDuration: editCaret,

	// The address, not a value. See editRename.
	markup.KindIdentity: editRename,

	// A number.
	markup.KindInt: editStepper,

	// Finite sets. All three are enumerable from the RUNNING app — the
	// catalog for enum, two literals for bool, Context.Styles for style
	// — so none of them is ever typed.
	markup.KindBool:  editChoice,
	markup.KindEnum:  editChoice,
	markup.KindStyle: editChoice,

	// Names from the live context, type-checked before they are offered.
	markup.KindBinding: editBinding,
	markup.KindCommand: editBinding,

	// The three that cannot be typed correctly from memory, and so earn
	// the "…".
	markup.KindColor:    editColor,
	markup.KindGridLens: editTracks,
	markup.KindGesture:  editGesture,
}

// editorForKind is the lookup. The bool is the whole point: see the type
// comment.
func editorForKind(k markup.Kind) (editorKind, bool) {
	e, ok := editors[k]
	return e, ok
}

// editorFor is the same question asked of an inspector row.
func (r attrRow) editorFor() (editorKind, bool) { return editorForKind(markup.Kind(r.kind)) }

// floats reports whether this editor puts a SURFACE under the row rather
// than editing inside it. It is what an affordance glyph other than a
// caret means, and it is derived here rather than listed beside the row
// projection so the two cannot disagree.
func (e editorKind) floats() bool {
	switch e {
	case editChoice, editBinding, editColor, editTracks, editGesture:
		return true
	}
	return false
}

// affordance is the cell glyph, Visual Studio's vocabulary:
//
//	…  opens something — a value you cannot type correctly from memory
//	▾  drops down a finite list
//	⇕  steps a number in place
//	   nothing: the cell is the editor, so it is a caret
//
// The distinction between … and ▾ is not decoration. A dropdown promises
// "these are all the values there are"; an ellipsis promises only "there
// is more to this than a line of text". Collapsing them would tell the
// user a colour has a finite list of legal answers.
func (e editorKind) affordance() string {
	switch e {
	case editColor, editTracks, editGesture:
		return "…"
	case editChoice, editBinding:
		return "▾"
	case editStepper:
		return "⇕"
	}
	return " "
}

// rowAffordance is the projection's entry point: the glyph for a row, or
// a bang where no editor is mapped. A row whose Kind has no editor must
// LOOK wrong on the grid — an unmarked row that does nothing on enter is
// the silent failure this table exists to remove.
func rowAffordance(r attrRow) string {
	e, ok := r.editorFor()
	if !ok {
		return "!"
	}
	return e.affordance()
}

// valueSet is the finite list of values an attribute may take, or nil
// where it has none. It is the DATA half of the per-Kind editor: the
// widget renders it, this decides what is in it.
//
// Every list comes from the RUNNING app, which is the point. A style
// list that was a hardcoded table would offer names the app does not
// have and omit the ones it does; asking Context.Styles cannot. The
// introspection questions meet here — the catalog says what the
// attribute is, Context.Styles says which styles exist, Context.Values
// says which commands and which typed handles do — and a property grid
// is those answers in rows.
//
// The dispatch is a switch on a string Kind. No reflection, and the same
// mechanism markup.Bound uses.
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
	case markup.KindBinding:
		return ed.typedBindings(a.GoType)
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

// typedBindings is the KindBinding half, and the TYPE FILTER is the
// whole of its value.
//
// A bind-only attribute takes {{.Path}} and nothing else, and the handle
// behind that path has an element type the loader checks. Offering every
// bindable name would hand the user a load error out of the editor's own
// list the moment they picked a string handle for a Color — the exact
// defect the command picker was built to avoid, one Kind over.
//
// AttrSpec.GoType is the declared element type; goTypeOf answers the same
// question of a live handle. An attribute whose GoType is empty gets NO
// offers rather than unchecked ones: the honest answer to "which of these
// will load?" is "this catalog entry does not say".
func (ed *editor) typedBindings(goType string) []string {
	if goType == "" {
		return nil
	}
	var out []string
	for name, v := range ed.docCtx.Values {
		if goTypeOf(v) == goType {
			out = append(out, "{{."+name+"}}")
		}
	}
	sort.Strings(out)
	return out
}

// goTypeOf names the element type of a live binding handle, in the same
// spelling markup.AttrSpec.GoType uses, or "" for something that is not
// a handle at all.
//
// It is a type switch, which is the mechanism markup.Bound and
// markup.PlaceholderFor both use, and it is the only way to ask this
// question without reflection. That makes it the MIRROR of
// markup.PlaceholderFor — that function turns a GoType into a handle,
// this one turns a handle back into a GoType — and two tables that must
// agree drift the moment nobody checks.
// TestGoTypeOfMirrorsPlaceholderFor runs every markup.PlaceholderTypes()
// entry through both and fails when they disagree, so teaching
// PlaceholderFor a new type and forgetting this one is a red test rather
// than a picker that silently offers nothing.
func goTypeOf(v any) string {
	switch v.(type) {
	case *prop.Property[string]:
		return "string"
	case *prop.Property[int]:
		return "int"
	case *prop.Property[bool]:
		return "bool"
	case *prop.Property[float64]:
		return "float64"
	case *prop.Property[[]float64]:
		return "[]float64"
	case *prop.Property[[]string]:
		return "[]string"
	case *prop.Property[render.Color]:
		return "render.Color"
	case *prop.Property[image.Image]:
		return "image.Image"
	case *prop.Property[components.ItemSource]:
		return "components.ItemSource"
	}
	return ""
}

// GRID TRACKS.
//
// components.ParseGridLens is the reader markup itself uses, so the
// editor and the loader agree on what "Auto,1*,20" means by
// construction. What did not exist is the WRITER, because until a track
// editor existed nothing ever turned tracks back into a spec.

// lensText spells one track the way markup writes it.
func lensText(l components.GridLen) string {
	switch {
	case l.Auto:
		return "Auto"
	case l.Star == 1:
		return "*"
	case l.Star > 0:
		return strconv.FormatFloat(l.Star, 'g', -1, 64) + "*"
	}
	return strconv.Itoa(l.Fixed)
}

// lensSpec is the whole attribute value — the inverse of
// components.ParseGridLens, pinned against it by TestTrackSpecsRoundTrip.
func lensSpec(ls []components.GridLen) string {
	parts := make([]string, 0, len(ls))
	for _, l := range ls {
		parts = append(parts, lensText(l))
	}
	return strings.Join(parts, ",")
}

// lensKind names a track's kind for the editor's own display. It is not
// markup syntax; it is the word beside the number.
func lensKind(l components.GridLen) string {
	switch {
	case l.Auto:
		return "Auto"
	case l.Star > 0:
		return "Star"
	}
	return "Fixed"
}

// nextLensKind cycles Fixed → Auto → Star → Fixed, carrying a usable
// value into each: a track that becomes Star must have a weight or it
// takes no space, and one that becomes Fixed must have cells.
func nextLensKind(l components.GridLen) components.GridLen {
	switch {
	case l.Auto:
		return components.Star(1)
	case l.Star > 0:
		return components.Fixed(10)
	}
	return components.Auto()
}

// adjustLens is what ◂ and ▸ do to one track: the star weight or the
// fixed cell count, never below one. Auto has no number, so it does not
// move — the bool is how the editor says so rather than silently
// swallowing the key.
func adjustLens(l components.GridLen, d int) (components.GridLen, bool) {
	switch {
	case l.Auto:
		return l, false
	case l.Star > 0:
		w := l.Star + float64(d)
		if w < 1 {
			w = 1
		}
		return components.Star(w), true
	}
	n := l.Fixed + d
	if n < 1 {
		n = 1
	}
	return components.Fixed(n), true
}
