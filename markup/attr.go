package markup

import (
	"fmt"
	"strings"
)

// Attr resolves one attribute of an element as a typed handle: it reads
// the binding expression, resolves it against ctx, and type-asserts the
// result.
//
// This is the receiving half of the data hand-off, and it existed as a
// hand-written generic in ten places before it existed here — cmd/finder
// (three times), cmd/reader, cmd/markuplog, cmd/sysmon, cmd/toolkit,
// apps/wysiwyg's activity rail, and both of the tutorials that teach
// writing a component. markup itself wrote it twice more internally. A
// tutorial called it "the idiom"; ten copies of a nine-line generic is a
// missing export.
//
// Typical use from a Builder:
//
//	value, err := markup.Attr[*prop.Property[int]](ctx, e, "Value")
//
// TWO SEPARATE FAILURES, NAMED SEPARATELY. An ABSENT attribute and a
// PRESENT-but-wrong one are different mistakes by the author, and the
// copies conflated them: resolving "" reports `"" is not a binding
// expression`, which describes the machine's disappointment rather than
// the author's error. One copy went further and ran strconv.Atoi on the
// missing value, so a forgotten Max was reported as a number-parse
// failure. Absent is reported as absent here.
//
// Every message names the ELEMENT as well as the attribute. A page with
// six <Meter>s and one typo is otherwise a hunt.
func Attr[T any](ctx *Context, e Element, name string) (T, error) {
	var zero T
	raw, err := requiredAttr(e, name)
	if err != nil {
		return zero, err
	}
	v, err := ctx.BindingValue(raw)
	if err != nil {
		return zero, fmt.Errorf("markup: <%s %s>: %w", e.Name, name, err)
	}
	t, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("markup: <%s %s>: got %T, want %T", e.Name, name, v, zero)
	}
	return t, nil
}

// requiredAttr is the ONE definition of "absent is reported as absent",
// shared by Attr and by Bound — the resolver every built-in element and
// every out-of-package Builder resolves its handle attributes through.
//
// It is shared rather than written twice because Bound had the bug
// this file's doc comment describes, in the framework's own vocabulary:
// an omitted <Checkbox Checked> reported `<Checkbox Checked="">: ""
// is not a binding expression`, quoting an attribute the author never
// wrote and blaming the binding syntax for it. buildImage and
// optionList had each already hand-patched their own way around it
// (`<Image> needs Src`, `<Segmented> needs Options`), which is what a
// missing shared rule looks like from the outside.
//
// Empty counts as absent. `Checked=""` is not a binding either, and the
// author's mistake is the same one.
func requiredAttr(e Element, name string) (string, error) {
	raw, ok := e.Attrs[name]
	if !ok || raw == "" {
		return "", fmt.Errorf("markup: <%s>: attribute %s is required", e.Name, name)
	}
	return raw, nil
}

// suppliedAttr is the OPTIONAL half of the same rule, and it exists
// because the two halves have to agree on what "absent" means.
//
// requiredAttr counts empty as absent. An optional attribute guarded by
// a bare `if _, ok := e.Attrs[name]; ok` does not: the key exists, so
// the guard opens and Bound then reports `attribute Error is required`
// about an attribute that is nothing of the kind. <TextBox Error=""/>
// was told to supply what it had just supplied.
//
// So an optional attribute is SUPPLIED only when it carries a value.
// Written empty, it means the same as omitted — which is the reading
// buildTabs had already arrived at on its own, hand-rolling this
// TrimSpace for Selected while the eight other optional handles kept
// the bare key check.
func suppliedAttr(e Element, name string) bool {
	return strings.TrimSpace(e.Attrs[name]) != ""
}
