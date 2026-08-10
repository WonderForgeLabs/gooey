package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

type story struct{ Title, Date string }

func stories(titles ...string) []story {
	out := make([]story, len(titles))
	for i, t := range titles {
		out[i] = story{Title: t, Date: fmt.Sprintf("d%d", i)}
	}
	return out
}

func projectStory(s story) map[string]any {
	return map[string]any{"Title": s.Title, "Date": s.Date}
}

// titleTemplate is the minimal template: one Text bound to the item's
// Title. Tests that care about damage counts want the row to have as few
// paint nodes as possible, so the numbers mean something.
func titleTemplate(values map[string]any) (gooey.Component, error) {
	title, ok := values["Title"].(*prop.Property[string])
	if !ok {
		return nil, fmt.Errorf("Title is %T", values["Title"])
	}
	return &Text{Content: title}, nil
}

func numbered(n int) []story {
	titles := make([]string, n)
	for i := range titles {
		titles[i] = fmt.Sprintf("item%d", i)
	}
	return stories(titles...)
}

func newList(t *testing.T, items []story, cols, rows int) (*prop.Property[[]story], *prop.Property[int], *ItemsView, *gooey.Composer) {
	t.Helper()
	src := prop.NewSource(items)
	sel := prop.NewSource(0)
	v := &ItemsView{
		Items:     Items(src, projectStory),
		Selected:  sel,
		Template:  titleTemplate,
		Highlight: true,
	}
	return src, sel, v, gooey.NewComposer(v, cols, rows)
}

func rowsOf(b *render.Buffer, n int) []string {
	out := make([]string, n)
	for y := 0; y < n; y++ {
		out[y] = row(b, y)
	}
	return out
}

func TestItemsViewRealizesOnlyTheVisibleWindow(t *testing.T) {
	_, _, v, c := newList(t, numbered(100), 20, 4)
	f, _ := c.Frame()

	if got := len(v.rows); got != 4 {
		t.Fatalf("realized %d rows for a 4-row view, want 4", got)
	}
	want := []string{"item0", "item1", "item2", "item3"}
	if got := rowsOf(f.Cells, 4); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}

// The acceptance bar from the spec: change one item, repaint one row.
func TestOneItemChangeRepaintsOneRow(t *testing.T) {
	src, _, v, c := newList(t, numbered(10), 20, 5)
	c.Frame()
	if _, painted := c.Frame(); painted != 0 {
		t.Fatalf("settled frame painted %d, want 0", painted)
	}

	// Item 3 is not the selected row, so its highlight overlay is off and
	// reads nothing but `selected` — the only nodes that can go dirty are
	// the view (it observes Items) and row 3's Text.
	next := numbered(10)
	next[3].Title = "CHANGED"
	src.Set(next)

	f, painted := c.Frame()
	if painted != 2 {
		t.Fatalf("one-item change painted %d components, want 2 (the view's observer node + row 3's Text)", painted)
	}
	if got := row(f.Cells, 3); got != "CHANGED" {
		t.Fatalf("row 3 = %q, want %q", got, "CHANGED")
	}
	if got := row(f.Cells, 2); got != "item2" {
		t.Fatalf("row 2 = %q — a neighbouring row was disturbed", got)
	}
	if len(v.rows) != 5 {
		t.Fatalf("realized rows = %d, want 5 (no re-realization for a value change)", len(v.rows))
	}
}

// Selection is the list's version of the framework's two-component focus
// guarantee: the row that lost it and the row that gained it.
func TestSelectionMoveRepaintsTwoRowsPlusTheObserver(t *testing.T) {
	_, sel, _, c := newList(t, numbered(10), 20, 5)
	c.Frame()

	sel.Set(1)
	f, painted := c.Frame()
	if painted != 3 {
		t.Fatalf("selection move painted %d components, want 3 (two row highlights + the view's observer node)", painted)
	}
	if !reversedRow(f.Cells, 1, 20) {
		t.Fatalf("row 1 is not highlighted after selection moved to it")
	}
	if reversedRow(f.Cells, 0, 20) {
		t.Fatalf("row 0 is still highlighted after losing selection")
	}
	if got := row(f.Cells, 0); got != "item0" {
		t.Fatalf("row 0 text = %q — deselecting must restyle cells, not erase them", got)
	}
}

func TestHighlightSurvivesAContentChangeOnTheSelectedRow(t *testing.T) {
	src, _, _, c := newList(t, numbered(10), 20, 5)
	c.Frame()

	// Row 0 is selected. Its Text repaints; the overlay must repaint
	// AFTER it, or the newly painted cells lose the highlight.
	next := numbered(10)
	next[0].Title = "fresh"
	src.Set(next)
	f, _ := c.Frame()

	if got := row(f.Cells, 0); got != "fresh" {
		t.Fatalf("row 0 = %q", got)
	}
	if !reversedRow(f.Cells, 0, 20) {
		t.Fatalf("selected row lost its highlight when its content changed")
	}
}

func reversedRow(b *render.Buffer, y, w int) bool {
	for x := 0; x < w; x++ {
		if !b.At(x, y).Style.Reverse {
			return false
		}
	}
	return true
}

func TestWindowFollowsSelectionAndReusesRows(t *testing.T) {
	_, sel, v, c := newList(t, numbered(20), 20, 4)
	c.Frame()

	sel.Set(3) // still inside the window: nothing scrolls
	c.Frame()
	if v.top != 0 {
		t.Fatalf("top = %d after selecting the last visible row, want 0", v.top)
	}
	before := append([]*itemRow(nil), v.rows...)

	sel.Set(4) // one past the window: scroll by one, reuse three rows
	f, _ := c.Frame()
	if v.top != 1 {
		t.Fatalf("top = %d after selecting one past the window, want 1", v.top)
	}
	for i := 0; i < 3; i++ {
		if v.rows[i] != before[i+1] {
			t.Fatalf("row %d was rebuilt instead of reused across a one-line scroll", i)
		}
	}
	want := []string{"item1", "item2", "item3", "item4"}
	if got := rowsOf(f.Cells, 4); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %v, want %v", got, want)
	}

	// Scrolling back up keeps the selection visible from the other side.
	sel.Set(0)
	c.Frame()
	if v.top != 0 {
		t.Fatalf("top = %d after selecting item 0, want 0", v.top)
	}
}

func TestShrinkingTheListDropsRowsAndClearsTheirCells(t *testing.T) {
	src, _, v, c := newList(t, numbered(6), 20, 6)
	f, _ := c.Frame()
	if got := row(f.Cells, 5); got != "item5" {
		t.Fatalf("row 5 = %q", got)
	}

	src.Set(numbered(2))
	f, _ = c.Frame()
	if len(v.rows) != 2 {
		t.Fatalf("realized %d rows for a 2-item list, want 2", len(v.rows))
	}
	for y := 2; y < 6; y++ {
		if got := row(f.Cells, y); got != "" {
			t.Fatalf("row %d = %q — a dropped row left its cells behind", y, got)
		}
	}
}

func TestItemShapeChangeRebuildsTheRow(t *testing.T) {
	src := prop.NewSource([]story{{Title: "a"}})
	// A projection whose key set depends on the item is the case a row
	// cannot absorb: different keys mean different bindings.
	shape := prop.NewSource(false)
	v := &ItemsView{
		Items: Items(src, func(s story) map[string]any {
			m := map[string]any{"Title": s.Title}
			if shape.Get() {
				m["Extra"] = "x"
			}
			return m
		}),
		Selected: prop.NewSource(0),
		Template: titleTemplate,
	}
	c := gooey.NewComposer(v, 20, 3)
	c.Frame()
	first := v.rows[0]

	shape.Set(true)
	src.Set([]story{{Title: "b"}})
	f, _ := c.Frame()
	if v.rows[0] == first {
		t.Fatalf("row was reused across a change of item shape")
	}
	if got := row(f.Cells, 0); got != "b" {
		t.Fatalf("row 0 = %q", got)
	}
}

func TestListKeysMoveTheSelection(t *testing.T) {
	_, sel, v, c := newList(t, numbered(30), 20, 5)
	c.Frame()

	cases := []struct {
		ev   input.KeyEvent
		want int
	}{
		{input.Named(input.KeyDown), 1},
		{input.Rune('j'), 2},
		{input.Rune('k'), 1},
		{input.Named(input.KeyUp), 0},
		{input.Named(input.KeyPageDown), 4},
		{input.Named(input.KeyEnd), 29},
		{input.Named(input.KeyPageUp), 25},
		{input.Named(input.KeyHome), 0},
	}
	for _, tc := range cases {
		if !v.HandleKey(tc.ev) {
			t.Fatalf("%v was not consumed", tc.ev)
		}
		c.Frame()
		if got := sel.Get(); got != tc.want {
			t.Fatalf("after %v selection = %d, want %d", tc.ev, got, tc.want)
		}
	}
}

func TestEnterAndSecondClickActivate(t *testing.T) {
	_, sel, v, c := newList(t, numbered(10), 20, 5)
	opened := 0
	v.Activate = func() { opened++ }
	c.Frame()

	if !v.HandleKey(input.Named(input.KeyEnter)) {
		t.Fatal("enter was not consumed")
	}
	if opened != 1 {
		t.Fatalf("enter activated %d times, want 1", opened)
	}

	// First click on an unselected row selects it and does NOT activate.
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 2, Y: 3})
	c.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, X: 2, Y: 3})
	c.Frame()
	if sel.Get() != 3 {
		t.Fatalf("click selected %d, want 3", sel.Get())
	}
	if opened != 1 {
		t.Fatalf("first click activated (%d); it should only select", opened)
	}
	// Second click on the same row activates.
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 2, Y: 3})
	c.HandleMouse(input.MouseEvent{Kind: input.MouseRelease, X: 2, Y: 3})
	if opened != 2 {
		t.Fatalf("second click activated %d times, want 2", opened)
	}
}

func TestWheelMovesTheSelection(t *testing.T) {
	_, sel, v, c := newList(t, numbered(30), 20, 5)
	c.Frame()

	v.HandleMouse(input.MouseEvent{Kind: input.WheelDown, X: 2, Y: 1})
	if got := sel.Get(); got != wheelStep {
		t.Fatalf("wheel down selected %d, want %d", got, wheelStep)
	}
	v.HandleMouse(input.MouseEvent{Kind: input.WheelUp, X: 2, Y: 1})
	if got := sel.Get(); got != 0 {
		t.Fatalf("wheel up selected %d, want 0", got)
	}
}

func TestClickRoutesThroughTheRealizedTree(t *testing.T) {
	// The rows did not exist when the FocusManager was built. Routing a
	// click to them at all is the input half of the structural re-sync.
	_, sel, v, c := newList(t, numbered(10), 20, 5)
	c.Frame()
	if hit := c.Focus().HitTest(2, 2); hit == nil {
		t.Fatal("hit test found nothing over a realized row")
	}
	c.HandleMouse(input.MouseEvent{Kind: input.MousePress, X: 2, Y: 2})
	if sel.Get() != 2 {
		t.Fatalf("click on row 2 selected %d", sel.Get())
	}
	if c.Focus().Focused() != gooey.Component(v) {
		t.Fatalf("clicking a row did not focus the view")
	}
}

func TestItemsAdapterProjectsTypedSlices(t *testing.T) {
	p := prop.NewSource(stories("a", "b"))
	src := Items(p, projectStory).Get()
	if src.Len() != 2 {
		t.Fatalf("Len = %d, want 2", src.Len())
	}
	if got := src.At(1)["Title"]; got != "b" {
		t.Fatalf("At(1).Title = %v, want b", got)
	}
	if src.At(5) != nil {
		t.Fatal("At past the end must return nil, not panic")
	}
	p.Set(stories("a", "b", "c"))
	if got := Items(p, projectStory).Get().Len(); got != 3 {
		t.Fatalf("Len after Set = %d, want 3", got)
	}
}

func TestReservedRowValuesReachTheTemplate(t *testing.T) {
	var selected *prop.Property[bool]
	v := &ItemsView{
		Items:    Items(prop.NewSource(stories("only")), projectStory),
		Selected: prop.NewSource(0),
		Template: func(values map[string]any) (gooey.Component, error) {
			selected = values[SelectedKey].(*prop.Property[bool])
			if _, ok := values[HoveredKey].(*prop.Property[bool]); !ok {
				return nil, fmt.Errorf("%s missing", HoveredKey)
			}
			return titleTemplate(values)
		},
	}
	c := gooey.NewComposer(v, 20, 3)
	c.Frame()
	if selected == nil || !selected.Get() {
		t.Fatal("the selected row's _selected handle is not true")
	}
}

func TestTemplateErrorIsReportedNotPanicked(t *testing.T) {
	v := &ItemsView{
		Items:    Items(prop.NewSource(stories("a")), projectStory),
		Template: func(map[string]any) (gooey.Component, error) { return nil, fmt.Errorf("boom") },
	}
	if err := v.Validate(); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Validate() = %v, want the template error", err)
	}
	c := gooey.NewComposer(v, 30, 3)
	f, _ := c.Frame()
	if v.Err() == nil {
		t.Fatal("a realization error was swallowed")
	}
	if got := row(f.Cells, 0); !strings.Contains(got, "boom") {
		t.Fatalf("row 0 = %q, want the error painted into the view", got)
	}
}

func TestUnselectableListStillRenders(t *testing.T) {
	v := &ItemsView{
		Items:    Items(prop.NewSource(stories("a", "b")), projectStory),
		Template: titleTemplate,
	}
	c := gooey.NewComposer(v, 20, 3)
	f, _ := c.Frame()
	if got := rowsOf(f.Cells, 2); got[0] != "a" || got[1] != "b" {
		t.Fatalf("rows = %v", got)
	}
	if v.HandleKey(input.Named(input.KeyDown)) {
		t.Fatal("a list with no Selected binding must not consume arrows")
	}
}

func TestEmptyListRealizesNothing(t *testing.T) {
	src := prop.NewSource([]story{})
	v := &ItemsView{Items: Items(src, projectStory), Selected: prop.NewSource(0), Template: titleTemplate}
	c := gooey.NewComposer(v, 20, 4)
	c.Frame()
	if len(v.rows) != 0 {
		t.Fatalf("realized %d rows for an empty list", len(v.rows))
	}
	src.Set(numbered(2))
	f, _ := c.Frame()
	if len(v.rows) != 2 {
		t.Fatalf("realized %d rows after the list filled, want 2", len(v.rows))
	}
	if got := row(f.Cells, 1); got != "item1" {
		t.Fatalf("row 1 = %q", got)
	}
}

func TestMultiLineTemplateSetsRowHeight(t *testing.T) {
	v := &ItemsView{
		Items:    Items(prop.NewSource(numbered(10)), projectStory),
		Selected: prop.NewSource(0),
		Template: func(values map[string]any) (gooey.Component, error) {
			title := values["Title"].(*prop.Property[string])
			return &VStack{Children: []gooey.Component{
				&Text{Content: title},
				&Text{Content: Str("---")},
			}}, nil
		},
	}
	c := gooey.NewComposer(v, 20, 6)
	f, _ := c.Frame()
	if v.rowH != 2 {
		t.Fatalf("rowH = %d for a two-line template, want 2", v.rowH)
	}
	if len(v.rows) != 3 {
		t.Fatalf("realized %d rows in 6 lines at 2 lines each, want 3", len(v.rows))
	}
	want := []string{"item0", "---", "item1", "---", "item2", "---"}
	if got := rowsOf(f.Cells, 6); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %v, want %v", got, want)
	}
}
