package markup

import "fmt"

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
	raw, ok := e.Attrs[name]
	if !ok || raw == "" {
		return zero, fmt.Errorf("markup: <%s>: attribute %s is required", e.Name, name)
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
