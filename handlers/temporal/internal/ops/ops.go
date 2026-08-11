// Package ops is the Temporal ops dashboard's viewmodel: the property
// surface ops.gooey binds, and the small amount of code-behind a live
// pagination loop genuinely needs.
//
// The division of labor is the demo's whole argument. Everything that
// TALKS TO TEMPORAL is declared in markup, through the ordinary
// temporal:Activity provider, against the visibility pack's convenience
// activities (scalar arguments in, protojson out):
//
//	Click="{{temporal:Activity `visibility.Query` .Query .PageSize .PageToken | into .RowsJSON}}"
//	SelectionChanged="{{temporal:Activity `visibility.Describe` .SelectedWorkflowID .SelectedRunID | into .DescribeJSON}}"
//
// This file never dials, schedules, or decodes an activity. What it
// owns is DERIVATION and INTENT: computeds that parse the protojson the
// activities delivered (rows for the ItemsView, the selected row's IDs,
// the count line), and the three intents markup cannot spell in one
// expression — run (reset to page one, fetch, count), next, prev. Those
// intents keep the page-token history, then invoke the same
// markup-built commands the buttons carry, found through their Name.
//
// The property boundary is the decision record's: protojson text lands
// in a Property[string]; computeds json.Unmarshal it into plain data
// and project rows via components.ItemsOf. Parsing INSIDE the computed
// is what subscribes the list to the fetch — the moment a result is
// Set, exactly the components reading the projection repaint.
package ops

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Files carries the dashboard's markup. Embedded, so the binary runs
// from anywhere — the browser, a scratch dir, an installed PATH.
//
//go:embed ops.gooey
var Files embed.FS

// PageFile is the markup file inside Files.
const PageFile = "ops.gooey"

// DefaultTaskQueue is the queue the dashboard schedules on and the
// companion worker serves — the same queue workers/visibilityworker
// defaults to, so the -with-worker=false deployment needs no flags.
const DefaultTaskQueue = "gooey-visibility"

// DefaultQuery is what the query bar holds at startup. Empty is a legal
// visibility query (everything), but a dashboard that opens on a real
// expression teaches the query language by existing.
const DefaultQuery = `ExecutionStatus="Running"`

// Theme is the dashboard's style table, shared by main and the tests so
// a style name in ops.gooey always resolves.
func Theme() map[string]render.Style {
	return map[string]render.Style{
		"panel":  {Fg: render.RGB(120, 90, 220)},
		"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
		"dim":    {Fg: render.RGB(140, 140, 150)},
	}
}

// Row is one execution as the list shows it — already formatted,
// because the projection is where formatting belongs.
type Row struct {
	ID     string
	RunID  string
	Type   string
	Status string
	Start  string
}

// VM is the dashboard's state: the sources markup writes and the
// computeds it reads. All of it is UI-goroutine state.
type VM struct {
	// The query bar's text and the two scalars every fetch carries.
	Query     *prop.Property[string]
	PageSize  *prop.Property[string]
	PageToken *prop.Property[string]

	// The three `into` targets — protojson lands here.
	RowsJSON     *prop.Property[string]
	CountJSON    *prop.Property[string]
	DescribeJSON *prop.Property[string]

	// Selected is shared with the ItemsView.
	Selected *prop.Property[int]
	// PageNum is 1-based, maintained by the intents below.
	PageNum *prop.Property[int]
	// AutoRefresh gates the markup <Timer> that refreshes every 30s. On
	// by default — a dashboard that opens stale teaches nothing — and
	// bound to the status-row checkbox, so pausing is one keystroke and
	// tears nothing down (the Timer reads it at fire time).
	AutoRefresh *prop.Property[bool]

	rows  *prop.Property[[]Row]
	items *prop.Property[components.ItemSource]

	ctx     *markup.Context
	prevTok []string
	where   string
	quit    func()
}

// NewVM builds the surface. where is the status line's worker location
// ("worker in-process" / "worker elsewhere"); quit ends the app.
func NewVM(where string, quit func()) *VM {
	vm := &VM{
		Query:        prop.NewSource(DefaultQuery),
		PageSize:     prop.NewSource("25"),
		PageToken:    prop.NewSource(""),
		RowsJSON:     prop.NewSource(""),
		CountJSON:    prop.NewSource(""),
		DescribeJSON: prop.NewSource(""),
		Selected:     prop.NewSource(0),
		PageNum:      prop.NewSource(1),
		AutoRefresh:  prop.NewSource(true),
		where:        where,
		quit:         quit,
	}
	// Parsing inside the computed subscribes the projection to the
	// fetch; ItemsOf inside the same computed is the dependency rule
	// from the ItemsView doc applied as written.
	vm.rows = prop.NewComputed(func() []Row { return parseRows(vm.RowsJSON.Get()) })
	vm.items = prop.NewComputed(func() components.ItemSource {
		return components.ItemsOf(vm.rows.Get(), func(r Row) map[string]any {
			return map[string]any{
				"WorkflowId": r.ID, "Type": r.Type, "Status": r.Status, "Start": r.Start,
			}
		})
	})
	return vm
}

// Attach hands the VM the page's context, which is where the named
// buttons carrying the markup-built commands live. Called once, before
// the page loads; the lookups themselves happen per intent, so a hot
// reload's fresh components are found, not stale ones.
func (vm *VM) Attach(ctx *markup.Context) { vm.ctx = ctx }

// Values is the binding surface ops.gooey resolves against.
func (vm *VM) Values() map[string]any {
	return map[string]any{
		"Query":     vm.Query,
		"PageSize":  vm.PageSize,
		"PageToken": vm.PageToken,

		"RowsJSON":     vm.RowsJSON,
		"CountJSON":    vm.CountJSON,
		"DescribeJSON": vm.DescribeJSON,

		"Items":       vm.items,
		"Selected":    vm.Selected,
		"AutoRefresh": vm.AutoRefresh,

		"SelectedWorkflowID": prop.NewComputed(func() string { return vm.selectedRow().ID }),
		"SelectedRunID":      prop.NewComputed(func() string { return vm.selectedRow().RunID }),

		"DescribeText": prop.NewComputed(vm.describeText),
		"Status":       prop.NewComputed(vm.statusLine),
		"Count":        prop.NewComputed(vm.countText),
		"PageInfo":     prop.NewComputed(vm.pageInfo),

		"RunQuery": gooey.Command(vm.RunQuery),
		"NextPage": gooey.Command(vm.NextPage),
		"PrevPage": gooey.Command(vm.PrevPage),
		"Refresh":  gooey.Command(vm.Refresh),
		"Quit":     gooey.Command(func() { vm.quit() }),
	}
}

// ---- intents ----
//
// Each one is bookkeeping plus an invocation of the markup-built
// command — the fetch itself stays declared in the document, and these
// read properties only from the UI goroutine, where every Action runs.

// RunQuery is the query bar's enter and the run button: back to page
// one, fetch, recount.
func (vm *VM) RunQuery() {
	vm.PageToken.Set("")
	vm.prevTok = nil
	vm.PageNum.Set(1)
	vm.click("FetchBtn")
	vm.click("CountBtn")
}

// NextPage follows the current result's nextPageToken, remembering
// where it came from. No token, no page: the intent simply does not
// fire at the end.
func (vm *VM) NextPage() {
	next := nextTokenOf(vm.RowsJSON.Get())
	if next == "" {
		return
	}
	vm.prevTok = append(vm.prevTok, vm.PageToken.Get())
	vm.PageToken.Set(next)
	vm.PageNum.Set(vm.PageNum.Get() + 1)
	vm.click("FetchBtn")
}

// PrevPage pops the history NextPage kept. The server's tokens are
// opaque and forward-only, so "previous" is a client-side memory — the
// same trick every Temporal UI plays.
func (vm *VM) PrevPage() {
	n := len(vm.prevTok)
	if n == 0 {
		return
	}
	vm.PageToken.Set(vm.prevTok[n-1])
	vm.prevTok = vm.prevTok[:n-1]
	vm.PageNum.Set(vm.PageNum.Get() - 1)
	vm.click("FetchBtn")
}

// Refresh refetches the current page and recounts, tokens untouched.
func (vm *VM) Refresh() {
	vm.click("FetchBtn")
	vm.click("CountBtn")
}

// click runs the markup-built command a named button carries. The
// button IS the declaration — invoking it from an intent is the same
// command a pointer click runs, capability grant and all.
func (vm *VM) click(name string) {
	if vm.ctx == nil {
		return
	}
	b, ok := vm.ctx.Named[name].(*components.Button)
	if !ok || !gooey.CanExecute(b.Click) {
		return
	}
	b.Click.Run()
}

// ---- derivation ----

func (vm *VM) selectedRow() Row {
	rows := vm.rows.Get()
	i := vm.Selected.Get()
	if i < 0 || i >= len(rows) {
		return Row{}
	}
	return rows[i]
}

func (vm *VM) describeText() string {
	s := vm.DescribeJSON.Get()
	if s == "" {
		return "select an execution — up/down moves, enter re-describes"
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s // an ERROR: line from the provider, shown as delivered
	}
	return buf.String()
}

func (vm *VM) statusLine() string {
	if s := vm.RowsJSON.Get(); strings.HasPrefix(s, "ERROR:") {
		return clip(s, 120)
	}
	return fmt.Sprintf("%s · %d shown", vm.where, len(vm.rows.Get()))
}

// countText is the live count label, its own {{.Count}} binding in the
// status bar. Parsing INSIDE the computed subscribes the label to
// CountJSON, so it re-renders the moment any count lands — and counts
// land on every path that refreshes: run, refresh, ctrl+r, alt+r, and
// the 30s timer. That is what makes the label track reality instead of
// the last time somebody pressed run.
func (vm *VM) countText() string {
	var c struct {
		Count string `json:"count"`
	}
	s := vm.CountJSON.Get()
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return "? matching" // nothing landed yet, or an ERROR: line
	}
	if c.Count == "" {
		// A response arrived but protojson omits a zero int64 — that is a
		// real answer, not an unknown.
		return "0 matching"
	}
	return c.Count + " matching"
}

func (vm *VM) pageInfo() string {
	more := "end"
	if nextTokenOf(vm.RowsJSON.Get()) != "" {
		more = "more ^n"
	}
	return fmt.Sprintf("page %d · %s", vm.PageNum.Get(), more)
}

// listPayload is the slice of ListWorkflowExecutionsResponse's
// canonical JSON the dashboard reads — protojson field names, verbatim.
type listPayload struct {
	Executions []struct {
		Execution struct {
			WorkflowID string `json:"workflowId"`
			RunID      string `json:"runId"`
		} `json:"execution"`
		Type struct {
			Name string `json:"name"`
		} `json:"type"`
		Status    string `json:"status"`
		StartTime string `json:"startTime"`
	} `json:"executions"`
	NextPageToken string `json:"nextPageToken"`
}

func parseRows(s string) []Row {
	var p listPayload
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return nil
	}
	rows := make([]Row, 0, len(p.Executions))
	for _, e := range p.Executions {
		rows = append(rows, Row{
			ID:     e.Execution.WorkflowID,
			RunID:  e.Execution.RunID,
			Type:   e.Type.Name,
			Status: friendlyStatus(e.Status),
			Start:  friendlyTime(e.StartTime),
		})
	}
	return rows
}

func nextTokenOf(s string) string {
	var p listPayload
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return ""
	}
	return p.NextPageToken
}

// friendlyStatus turns WORKFLOW_EXECUTION_STATUS_RUNNING into Running —
// the canonical enum name, shortened for a 10-cell column, never
// remapped.
func friendlyStatus(s string) string {
	s = strings.TrimPrefix(s, "WORKFLOW_EXECUTION_STATUS_")
	if s == "" || s == "UNSPECIFIED" {
		return "?"
	}
	return s[:1] + strings.ToLower(s[1:])
}

func friendlyTime(s string) string {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02 15:04:05")
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
