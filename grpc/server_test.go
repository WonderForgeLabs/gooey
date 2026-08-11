package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// The harness hand-runs gooey.App's loop instead of using it, exactly as
// the MCP unit tests do, because the thing under test IS the goroutine
// boundary: everything below the Host interface is UI-goroutine state,
// and an RPC that touched any of it from a gRPC transport goroutine is a
// -race failure. The calls themselves travel over a real loopback TCP
// listener and a generated client — the wire is not simulated.

const testMarkup = `<Gooey>
  <VStack Gap="1">
    <Text Name="Head">count is {{.Count}}</Text>
    <Button Name="Inc" Content="add one" Click="{{.Increment}}"/>
    <Checkbox Name="Flag" Checked="{{.Flag}}" Label="the flag"/>
    <TextBox Name="Note" Text="{{.Note}}"/>
  </VStack>
</Gooey>`

type testApp struct {
	ctx  *markup.Context
	disp *gooey.Dispatcher

	// UI-goroutine state: written and read only by run(), or by a
	// closure run() drained.
	comp       *gooey.Composer
	cols, rows int
	needsFrame bool
	after      []func()
	onSwap     []func(gooey.Component)
	afterEv    []func(input.Event, bool)

	quit chan struct{}
	done chan struct{}
}

func newTestApp(t *testing.T, src string, values map[string]any, prep func(*markup.Context)) *testApp {
	t.Helper()
	a := &testApp{
		disp: gooey.NewDispatcher(),
		cols: 60, rows: 14,
		quit: make(chan struct{}),
		done: make(chan struct{}),
	}
	a.ctx = &markup.Context{Values: values, Dispatcher: a.disp}
	if prep != nil {
		prep(a.ctx)
	}
	root, err := markup.Build([]byte(src), a.ctx)
	if err != nil {
		t.Fatalf("build markup: %v", err)
	}
	a.attach(root)
	go a.run()
	t.Cleanup(func() {
		close(a.quit)
		<-a.done
	})
	return a
}

func (a *testApp) attach(root gooey.Component) {
	if a.comp != nil {
		a.comp.Close()
	}
	a.comp = gooey.NewComposer(root, a.cols, a.rows)
	a.comp.OnInvalidate(func() { a.needsFrame = true })
	a.comp.Start(a.disp)
	a.needsFrame = true
	for _, fn := range a.onSwap {
		fn(root)
	}
}

func (a *testApp) run() {
	defer close(a.done)
	for {
		if a.needsFrame {
			a.comp.Frame()
			a.needsFrame = false
			for _, fn := range a.after {
				fn()
			}
		}
		select {
		case <-a.quit:
			a.comp.Close()
			return
		case <-a.disp.Wake():
			a.disp.Drain()
		}
	}
}

// Host: Post is the only method safe off the loop.
func (a *testApp) Post(fn func())            { a.disp.Post(fn) }
func (a *testApp) Composer() *gooey.Composer { return a.comp }
func (a *testApp) Swap(root gooey.Component) { a.attach(root) }

// SessionHost: registration happens on the UI goroutine (the server
// Posts it), so plain appends are safe here as they are in gooey.App.
func (a *testApp) AfterFrame(fn func())                  { a.after = append(a.after, fn) }
func (a *testApp) OnSwap(fn func(gooey.Component))       { a.onSwap = append(a.onSwap, fn) }
func (a *testApp) AfterEvent(fn func(input.Event, bool)) { a.afterEv = append(a.afterEv, fn) }
func (a *testApp) Done() <-chan struct{}                 { return a.quit }
func (a *testApp) fireEvent(ev input.Event, consumed bool) { // helper for echo tests
	for _, fn := range a.afterEv {
		fn(ev, consumed)
	}
}

type viewmodel struct {
	count *prop.Property[int]
	text  *prop.Property[string]
	note  *prop.Property[string]
	flag  *prop.Property[bool]
	tint  *prop.Property[render.Color]
	ratio *prop.Property[float64]
	wait  *prop.Property[time.Duration]
	incs  int
}

func newVM() (*viewmodel, map[string]any) {
	vm := &viewmodel{
		count: prop.NewSource(0),
		note:  prop.NewSource(""),
		flag:  prop.NewSource(false),
		tint:  prop.NewSource(render.RGB(10, 20, 30)),
		ratio: prop.NewSource(0.5),
		wait:  prop.NewSource(2 * time.Second),
	}
	vm.text = prop.NewComputed(func() string { return itoa(vm.count.Get()) })
	return vm, map[string]any{
		"Count":     vm.text,
		"Note":      vm.note,
		"Flag":      vm.flag,
		"Tint":      vm.tint,
		"Ratio":     vm.ratio,
		"Wait":      vm.wait,
		"Label":     "a literal",
		"Increment": gooey.Command(func() { vm.incs++; vm.count.Set(vm.count.Get() + 1) }),
		"Reset":     gooey.Command(func() { vm.count.Set(0) }),
		"Boom":      gooey.Command(func() { panic("deliberate") }),
	}
}

// harness stands up an app, a listening server, and a generated client.
type harness struct {
	t    *testing.T
	app  *testApp
	vm   *viewmodel
	srv  *Server
	ctl  controlv1.ControlServiceClient
	sess controlv1.SessionServiceClient
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	vm, values := newVM()
	app := newTestApp(t, testMarkup, values, func(c *markup.Context) {
		c.Styles = map[string]render.Style{
			"accent": {Fg: render.RGB(0xff, 0x88, 0x00), Bold: true},
			"dim":    {Dim: true},
		}
	})
	return attachHarness(t, app, vm, Options{Name: "gooey-test", Version: "0"})
}

func attachHarness(t *testing.T, app *testApp, vm *viewmodel, opts Options) *harness {
	t.Helper()
	opts.Context = app.ctx
	srv, err := Serve(app, opts)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	t.Cleanup(srv.Close)
	conn, err := grpcgo.NewClient(srv.Addr(), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &harness{
		t:    t,
		app:  app,
		vm:   vm,
		srv:  srv,
		ctl:  controlv1.NewControlServiceClient(conn),
		sess: controlv1.NewSessionServiceClient(conn),
	}
}

// onUI runs fn on the app's UI goroutine and waits for it — the tests'
// own bridge, because touching a property from the test goroutine would
// be exactly the confinement violation the server exists to prevent.
func (h *harness) onUI(fn func()) {
	done := make(chan struct{})
	h.app.Post(func() { fn(); close(done) })
	<-done
}

func (h *harness) screen() string {
	h.t.Helper()
	res, err := h.ctl.ScreenText(context.Background(), &controlv1.ScreenTextRequest{})
	if err != nil {
		h.t.Fatalf("ScreenText: %v", err)
	}
	return res.Text
}

func wantCode(t *testing.T, err error, code codes.Code, substr string) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("not a status error: %v", err)
	}
	if st.Code() != code {
		t.Fatalf("code = %v (%s), want %v", st.Code(), st.Message(), code)
	}
	if substr != "" && !strings.Contains(st.Message(), substr) {
		t.Fatalf("message %q does not mention %q", st.Message(), substr)
	}
}

func strVal(s string) *controlv1.TypedValue {
	return &controlv1.TypedValue{Kind: &controlv1.TypedValue_StringValue{StringValue: s}}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ---- transport and security ----

func TestLoopbackOnlyIsAHardError(t *testing.T) {
	vm, values := newVM()
	_ = vm
	app := newTestApp(t, testMarkup, values, nil)
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.1.10:0", "example.com:7788"} {
		if _, err := Serve(app, Options{Addr: addr, Context: app.ctx}); err == nil {
			t.Errorf("Serve(%q) started; a non-loopback bind must be a hard error", addr)
		}
	}
	srv, err := Serve(app, Options{Addr: "localhost:0", Context: app.ctx})
	if err != nil {
		t.Fatalf("Serve(localhost) refused: %v", err)
	}
	srv.Close()
}

// ---- read ----

func TestSnapshotTree(t *testing.T) {
	h := newHarness(t)
	res, err := h.ctl.SnapshotTree(context.Background(), &controlv1.SnapshotTreeRequest{})
	if err != nil {
		t.Fatalf("SnapshotTree: %v", err)
	}
	root := res.Root
	if root == nil {
		t.Fatal("no root")
	}
	if !strings.Contains(root.Type, "VStack") {
		t.Errorf("root type = %q", root.Type)
	}
	byName := map[string]*controlv1.TreeNode{}
	var walk func(n *controlv1.TreeNode)
	walk = func(n *controlv1.TreeNode) {
		if n.Name != "" {
			byName[n.Name] = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	for _, name := range []string{"Head", "Inc", "Flag", "Note"} {
		if byName[name] == nil {
			t.Fatalf("named element %q missing from the tree", name)
		}
	}
	inc := byName["Inc"]
	if !inc.Focusable {
		t.Error("the button is not marked focusable")
	}
	if inc.Bounds == nil || inc.Bounds.Width == 0 {
		t.Error("the button has no arranged bounds; was a frame composed?")
	}
	if v := inc.Props["content"]; v.GetStringValue() != "add one" {
		t.Errorf("button content prop = %v", v)
	}

	// A depth limit elides, and says by how much.
	shallow, err := h.ctl.SnapshotTree(context.Background(), &controlv1.SnapshotTreeRequest{Depth: 1})
	if err != nil {
		t.Fatalf("SnapshotTree(depth=1): %v", err)
	}
	if len(shallow.Root.Children) != 0 || shallow.Root.ChildrenElided != 4 {
		t.Errorf("depth=1: children=%d elided=%d, want 0/4",
			len(shallow.Root.Children), shallow.Root.ChildrenElided)
	}
}

func TestScreenText(t *testing.T) {
	h := newHarness(t)
	if s := h.screen(); !strings.Contains(s, "count is 0") {
		t.Fatalf("screen does not show the app:\n%s", s)
	}
	styled, err := h.ctl.ScreenText(context.Background(), &controlv1.ScreenTextRequest{Styled: true})
	if err != nil {
		t.Fatalf("ScreenText(styled): %v", err)
	}
	if !strings.Contains(styled.Text, "\x1b[") {
		t.Error("styled screen carries no escape sequences")
	}
}

func TestListValues(t *testing.T) {
	h := newHarness(t)
	res, err := h.ctl.ListValues(context.Background(), &controlv1.ListValuesRequest{})
	if err != nil {
		t.Fatalf("ListValues: %v", err)
	}
	byName := map[string]*controlv1.ValueInfo{}
	for _, v := range res.Values {
		byName[v.Name] = v
	}
	checks := []struct {
		name string
		kind controlv1.EntryKind
		typ  controlv1.ValueKind
	}{
		{"Count", controlv1.EntryKind_ENTRY_KIND_PROPERTY, controlv1.ValueKind_VALUE_KIND_STRING},
		{"Note", controlv1.EntryKind_ENTRY_KIND_PROPERTY, controlv1.ValueKind_VALUE_KIND_STRING},
		{"Flag", controlv1.EntryKind_ENTRY_KIND_PROPERTY, controlv1.ValueKind_VALUE_KIND_BOOL},
		{"Tint", controlv1.EntryKind_ENTRY_KIND_PROPERTY, controlv1.ValueKind_VALUE_KIND_COLOR},
		{"Ratio", controlv1.EntryKind_ENTRY_KIND_PROPERTY, controlv1.ValueKind_VALUE_KIND_FLOAT},
		{"Wait", controlv1.EntryKind_ENTRY_KIND_PROPERTY, controlv1.ValueKind_VALUE_KIND_DURATION},
		{"Label", controlv1.EntryKind_ENTRY_KIND_LITERAL, controlv1.ValueKind_VALUE_KIND_STRING},
		{"Increment", controlv1.EntryKind_ENTRY_KIND_COMMAND, controlv1.ValueKind_VALUE_KIND_UNSPECIFIED},
	}
	for _, c := range checks {
		v := byName[c.name]
		if v == nil {
			t.Fatalf("%q missing from ListValues", c.name)
		}
		if v.Kind != c.kind || v.Type != c.typ {
			t.Errorf("%q: kind=%v type=%v, want %v/%v", c.name, v.Kind, v.Type, c.kind, c.typ)
		}
	}
	if got := byName["Tint"].Value.GetColorValue(); got == nil || !got.Set || got.Red != 10 {
		t.Errorf("Tint value = %v, want set 10/20/30", byName["Tint"].Value)
	}
	if got := byName["Wait"].Value.GetDurationValue().AsDuration(); got != 2*time.Second {
		t.Errorf("Wait = %v", got)
	}
	if len(res.Named) != 4 {
		t.Errorf("named = %v, want the page's four names", res.Named)
	}
}

func TestGetProperty(t *testing.T) {
	h := newHarness(t)
	res, err := h.ctl.GetProperty(context.Background(), &controlv1.GetPropertyRequest{Name: "Ratio"})
	if err != nil {
		t.Fatalf("GetProperty: %v", err)
	}
	if res.Value.GetValue().GetFloatValue() != 0.5 {
		t.Errorf("Ratio = %v", res.Value)
	}
	_, err = h.ctl.GetProperty(context.Background(), &controlv1.GetPropertyRequest{Name: "Nope"})
	wantCode(t, err, codes.NotFound, "Nope")
}

// ---- act, and the settle barrier ----

func TestSetPropertyRepaintsBeforeAnswering(t *testing.T) {
	h := newHarness(t)
	_, err := h.ctl.SetProperty(context.Background(), &controlv1.SetPropertyRequest{
		Name: "Note", Value: strVal("written over gRPC"),
	})
	if err != nil {
		t.Fatalf("SetProperty: %v", err)
	}
	// No sleeps: the settle barrier means the very next read sees it.
	if s := h.screen(); !strings.Contains(s, "written over gRPC") {
		t.Fatalf("the write is not on screen:\n%s", s)
	}
}

func TestSetPropertyTypeMismatch(t *testing.T) {
	h := newHarness(t)
	_, err := h.ctl.SetProperty(context.Background(), &controlv1.SetPropertyRequest{
		Name:  "Note",
		Value: &controlv1.TypedValue{Kind: &controlv1.TypedValue_BoolValue{BoolValue: true}},
	})
	wantCode(t, err, codes.InvalidArgument, "string")
	_, err = h.ctl.SetProperty(context.Background(), &controlv1.SetPropertyRequest{Name: "Note"})
	wantCode(t, err, codes.InvalidArgument, "typed value")
	_, err = h.ctl.SetProperty(context.Background(), &controlv1.SetPropertyRequest{
		Name: "Nope", Value: strVal("x"),
	})
	wantCode(t, err, codes.NotFound, "Nope")
	// The typed kinds all write.
	if _, err := h.ctl.SetProperty(context.Background(), &controlv1.SetPropertyRequest{
		Name:  "Tint",
		Value: &controlv1.TypedValue{Kind: &controlv1.TypedValue_ColorValue{ColorValue: &controlv1.Color{Set: true, Red: 1, Green: 2, Blue: 3}}},
	}); err != nil {
		t.Fatalf("set color: %v", err)
	}
	if _, err := h.ctl.SetProperty(context.Background(), &controlv1.SetPropertyRequest{
		Name:  "Wait",
		Value: &controlv1.TypedValue{Kind: &controlv1.TypedValue_DurationValue{DurationValue: durationpb.New(time.Minute)}},
	}); err != nil {
		t.Fatalf("set duration: %v", err)
	}
	var tint render.Color
	var wait time.Duration
	h.onUI(func() { tint, wait = h.vm.tint.Get(), h.vm.wait.Get() })
	if tint != (render.Color{Set: true, R: 1, G: 2, B: 3}) || wait != time.Minute {
		t.Error("typed writes did not land")
	}
}

func TestInvokeCommand(t *testing.T) {
	h := newHarness(t)
	if _, err := h.ctl.InvokeCommand(context.Background(), &controlv1.InvokeCommandRequest{Name: "Increment"}); err != nil {
		t.Fatalf("InvokeCommand: %v", err)
	}
	if s := h.screen(); !strings.Contains(s, "count is 1") {
		t.Fatalf("the command did not repaint before the answer:\n%s", s)
	}
	_, err := h.ctl.InvokeCommand(context.Background(), &controlv1.InvokeCommandRequest{Name: "Label"})
	wantCode(t, err, codes.InvalidArgument, "not a command")
	_, err = h.ctl.InvokeCommand(context.Background(), &controlv1.InvokeCommandRequest{Name: "Nope"})
	wantCode(t, err, codes.NotFound, "")
}

func TestPanicOnTheUIGoroutineIsInternalNotFatal(t *testing.T) {
	h := newHarness(t)
	_, err := h.ctl.InvokeCommand(context.Background(), &controlv1.InvokeCommandRequest{Name: "Boom"})
	wantCode(t, err, codes.Internal, "deliberate")
	// The app survived: the loop answers the next call.
	if s := h.screen(); !strings.Contains(s, "count is 0") {
		t.Fatalf("the app did not survive the panic:\n%s", s)
	}
}

func TestBlockedRunLoopIsDeadlineExceeded(t *testing.T) {
	vm, values := newVM()
	_ = vm
	// An app whose loop never drains: the dispatcher accepts posts and
	// nothing runs them.
	app := &testApp{
		disp: gooey.NewDispatcher(),
		cols: 40, rows: 8,
		quit: make(chan struct{}),
		done: make(chan struct{}),
	}
	close(app.done)
	app.ctx = &markup.Context{Values: values}
	root, err := markup.Build([]byte(testMarkup), app.ctx)
	if err != nil {
		t.Fatal(err)
	}
	app.comp = gooey.NewComposer(root, app.cols, app.rows)
	h := attachHarness(t, app, nil, Options{Timeout: 50 * time.Millisecond})
	_, rerr := h.ctl.ScreenText(context.Background(), &controlv1.ScreenTextRequest{})
	wantCode(t, rerr, codes.DeadlineExceeded, "run loop")
}

func TestSendKeys(t *testing.T) {
	h := newHarness(t)
	if _, err := h.ctl.SetFocus(context.Background(), &controlv1.SetFocusRequest{Name: "Note"}); err != nil {
		t.Fatalf("SetFocus: %v", err)
	}
	res, err := h.ctl.SendKeys(context.Background(), &controlv1.SendKeysRequest{Text: "hi"})
	if err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	if res.Sent != 2 || len(res.Consumed) != 2 || !res.Consumed[0] {
		t.Errorf("SendKeys result = %v", res)
	}
	if s := h.screen(); !strings.Contains(s, "hi") {
		t.Fatalf("typed text is not on screen:\n%s", s)
	}
	_, err = h.ctl.SendKeys(context.Background(), &controlv1.SendKeysRequest{Gestures: []string{"ctrl+banana"}})
	wantCode(t, err, codes.InvalidArgument, "")
	_, err = h.ctl.SendKeys(context.Background(), &controlv1.SendKeysRequest{})
	wantCode(t, err, codes.InvalidArgument, "text or gestures")
}

func TestSendPointerClicksByCoordinate(t *testing.T) {
	h := newHarness(t)
	tree, err := h.ctl.SnapshotTree(context.Background(), &controlv1.SnapshotTreeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var inc *controlv1.TreeNode
	var find func(n *controlv1.TreeNode)
	find = func(n *controlv1.TreeNode) {
		if n.Name == "Inc" {
			inc = n
		}
		for _, c := range n.Children {
			find(c)
		}
	}
	find(tree.Root)
	if inc == nil || inc.Bounds == nil {
		t.Fatal("no bounds for the button")
	}
	res, err := h.ctl.SendPointer(context.Background(), &controlv1.SendPointerRequest{
		Event: &controlv1.PointerEvent{
			Kind: controlv1.PointerKind_POINTER_KIND_CLICK,
			X:    inc.Bounds.X + 1, Y: inc.Bounds.Y,
		},
	})
	if err != nil {
		t.Fatalf("SendPointer: %v", err)
	}
	if !res.Consumed {
		t.Error("the click was not consumed")
	}
	if s := h.screen(); !strings.Contains(s, "count is 1") {
		t.Fatalf("the click did not fire the button:\n%s", s)
	}
	_, err = h.ctl.SendPointer(context.Background(), &controlv1.SendPointerRequest{
		Event: &controlv1.PointerEvent{X: 1, Y: 1},
	})
	wantCode(t, err, codes.InvalidArgument, "kind")
}

func TestSetFocus(t *testing.T) {
	h := newHarness(t)
	if _, err := h.ctl.SetFocus(context.Background(), &controlv1.SetFocusRequest{Name: "Inc"}); err != nil {
		t.Fatalf("SetFocus: %v", err)
	}
	tree, _ := h.ctl.SnapshotTree(context.Background(), &controlv1.SnapshotTreeRequest{})
	focused := false
	var find func(n *controlv1.TreeNode)
	find = func(n *controlv1.TreeNode) {
		if n.Name == "Inc" && n.Focused {
			focused = true
		}
		for _, c := range n.Children {
			find(c)
		}
	}
	find(tree.Root)
	if !focused {
		t.Error("the tree does not show the focus SetFocus set")
	}
	_, err := h.ctl.SetFocus(context.Background(), &controlv1.SetFocusRequest{Name: "Nope"})
	wantCode(t, err, codes.NotFound, "")
	_, err = h.ctl.SetFocus(context.Background(), &controlv1.SetFocusRequest{Name: "Head"})
	wantCode(t, err, codes.InvalidArgument, "focus stop")
}

// ---- structure ----

func TestSwapMarkup(t *testing.T) {
	h := newHarness(t)
	h.onUI(func() { h.vm.count.Set(7) })

	// A bad swap is inert.
	_, err := h.ctl.SwapMarkup(context.Background(), &controlv1.SwapMarkupRequest{
		Source: `<Gooey><Nope/></Gooey>`,
	})
	wantCode(t, err, codes.InvalidArgument, "unknown element")
	if s := h.screen(); !strings.Contains(s, "count is 7") {
		t.Fatalf("a failed swap disturbed the app:\n%s", s)
	}
	// SetFocus still resolves the OLD names after the failure.
	if _, err := h.ctl.SetFocus(context.Background(), &controlv1.SetFocusRequest{Name: "Inc"}); err != nil {
		t.Fatalf("the name table did not survive the failed swap: %v", err)
	}

	res, err := h.ctl.SwapMarkup(context.Background(), &controlv1.SwapMarkupRequest{
		Source: `<Gooey><VStack><Text Name="Banner">swapped, count {{.Count}}</Text></VStack></Gooey>`,
	})
	if err != nil {
		t.Fatalf("SwapMarkup: %v", err)
	}
	if len(res.Named) != 1 || res.Named[0] != "Banner" {
		t.Errorf("named = %v", res.Named)
	}
	if s := h.screen(); !strings.Contains(s, "swapped, count 7") {
		t.Fatalf("the new page did not render with surviving state:\n%s", s)
	}
}

func TestSwapMarkupWithRegistrations(t *testing.T) {
	h := newHarness(t)
	res, err := h.ctl.SwapMarkup(context.Background(), &controlv1.SwapMarkupRequest{
		Source: `<Gooey><Text Name="G">hello {{.Greeting}}</Text></Gooey>`,
		Register: []*controlv1.PropertyRegistration{{
			Name: "Greeting", Kind: controlv1.ValueKind_VALUE_KIND_STRING,
			Initial: strVal("world"),
		}},
	})
	if err != nil {
		t.Fatalf("SwapMarkup(register): %v", err)
	}
	_ = res
	if s := h.screen(); !strings.Contains(s, "hello world") {
		t.Fatalf("the registered property did not bind:\n%s", s)
	}
	// A failed swap rolls the registration back.
	_, err = h.ctl.SwapMarkup(context.Background(), &controlv1.SwapMarkupRequest{
		Source: `<Gooey><Nope/></Gooey>`,
		Register: []*controlv1.PropertyRegistration{{
			Name: "Doomed", Kind: controlv1.ValueKind_VALUE_KIND_INT,
		}},
	})
	wantCode(t, err, codes.InvalidArgument, "")
	_, err = h.ctl.GetProperty(context.Background(), &controlv1.GetPropertyRequest{Name: "Doomed"})
	wantCode(t, err, codes.NotFound, "")
}

func TestRegisterProperties(t *testing.T) {
	h := newHarness(t)
	_, err := h.ctl.RegisterProperties(context.Background(), &controlv1.RegisterPropertiesRequest{
		Properties: []*controlv1.PropertyRegistration{{
			Name: "Fresh.Level", Kind: controlv1.ValueKind_VALUE_KIND_INT,
			Initial: &controlv1.TypedValue{Kind: &controlv1.TypedValue_IntValue{IntValue: 42}},
		}},
	})
	if err != nil {
		t.Fatalf("RegisterProperties: %v", err)
	}
	got, err := h.ctl.GetProperty(context.Background(), &controlv1.GetPropertyRequest{Name: "Fresh.Level"})
	if err != nil {
		t.Fatalf("GetProperty after register: %v", err)
	}
	if got.Value.GetValue().GetIntValue() != 42 {
		t.Errorf("Fresh.Level = %v", got.Value)
	}
	// An existing name is refused: one source of truth.
	_, err = h.ctl.RegisterProperties(context.Background(), &controlv1.RegisterPropertiesRequest{
		Properties: []*controlv1.PropertyRegistration{{Name: "Note", Kind: controlv1.ValueKind_VALUE_KIND_STRING}},
	})
	wantCode(t, err, codes.InvalidArgument, "already exists")
	// A mismatched initial is refused.
	_, err = h.ctl.RegisterProperties(context.Background(), &controlv1.RegisterPropertiesRequest{
		Properties: []*controlv1.PropertyRegistration{{
			Name: "Wrong", Kind: controlv1.ValueKind_VALUE_KIND_INT, Initial: strVal("nope"),
		}},
	})
	wantCode(t, err, codes.InvalidArgument, "")
}

func TestUnregisterNames(t *testing.T) {
	h := newHarness(t)
	_, err := h.ctl.RegisterProperties(context.Background(), &controlv1.RegisterPropertiesRequest{
		Properties: []*controlv1.PropertyRegistration{
			{Name: "Fresh.Level", Kind: controlv1.ValueKind_VALUE_KIND_INT},
			{Name: "Fresh.Label", Kind: controlv1.ValueKind_VALUE_KIND_STRING},
		},
	})
	if err != nil {
		t.Fatalf("RegisterProperties: %v", err)
	}
	// A name that does not resolve is NOT_FOUND and the whole batch is
	// refused — the survivor proves nothing was taken out first.
	_, err = h.ctl.UnregisterNames(context.Background(), &controlv1.UnregisterNamesRequest{
		Names: []string{"Fresh.Level", "Fresh.Missing"},
	})
	wantCode(t, err, codes.NotFound, "Fresh.Missing")
	if _, err := h.ctl.GetProperty(context.Background(), &controlv1.GetPropertyRequest{Name: "Fresh.Level"}); err != nil {
		t.Fatalf("a failed batch removed a name anyway: %v", err)
	}

	if _, err := h.ctl.UnregisterNames(context.Background(), &controlv1.UnregisterNamesRequest{
		Names: []string{"Fresh.Level", "Fresh.Label"},
	}); err != nil {
		t.Fatalf("UnregisterNames: %v", err)
	}
	_, err = h.ctl.GetProperty(context.Background(), &controlv1.GetPropertyRequest{Name: "Fresh.Level"})
	wantCode(t, err, codes.NotFound, "")

	// A blank name is INVALID_ARGUMENT rather than a silent no-op.
	_, err = h.ctl.UnregisterNames(context.Background(), &controlv1.UnregisterNamesRequest{Names: []string{"  "}})
	wantCode(t, err, codes.InvalidArgument, "")
}

func TestGetDeclaredSchema(t *testing.T) {
	h := newHarness(t)
	src := `<Gooey xmlns:x="wonderforge.io/gooey/x">
  <x:Property Name="Title" Type="string" Required="true"/>
  <x:Property Name="Limit" Type="int" Default="5"/>
  <Text>{{.Title}}</Text>
</Gooey>`
	res, err := h.ctl.GetDeclaredSchema(context.Background(), &controlv1.GetDeclaredSchemaRequest{Source: src})
	if err != nil {
		t.Fatalf("GetDeclaredSchema: %v", err)
	}
	props := res.Schema.Properties
	if len(props) != 2 {
		t.Fatalf("properties = %v", props)
	}
	if props[0].Name != "Title" || props[0].Type != controlv1.ValueKind_VALUE_KIND_STRING || !props[0].Required {
		t.Errorf("Title = %v", props[0])
	}
	if props[1].Name != "Limit" || props[1].DefaultLiteral != "5" || props[1].Required {
		t.Errorf("Limit = %v", props[1])
	}
	// Empty source: this server was not told the page's document.
	_, err = h.ctl.GetDeclaredSchema(context.Background(), &controlv1.GetDeclaredSchemaRequest{})
	wantCode(t, err, codes.FailedPrecondition, "")
}

func TestGetDeclaredSchemaOfTheRunningDocument(t *testing.T) {
	vm, values := newVM()
	app := newTestApp(t, testMarkup, values, nil)
	h := attachHarness(t, app, vm, Options{Doc: func() []byte { return []byte(testMarkup) }})
	res, err := h.ctl.GetDeclaredSchema(context.Background(), &controlv1.GetDeclaredSchemaRequest{})
	if err != nil {
		t.Fatalf("GetDeclaredSchema(running): %v", err)
	}
	if len(res.Schema.Properties) != 0 {
		t.Errorf("the test page declares nothing, got %v", res.Schema.Properties)
	}
}

func TestPatchMarkup(t *testing.T) {
	h := newHarness(t)
	// Give the sibling TextBox some state to survive.
	if _, err := h.ctl.SetProperty(context.Background(), &controlv1.SetPropertyRequest{
		Name: "Note", Value: strVal("survives"),
	}); err != nil {
		t.Fatal(err)
	}
	res, err := h.ctl.PatchMarkup(context.Background(), &controlv1.PatchMarkupRequest{
		Name:   "Head",
		Source: `<Gooey><Text Name="Head">patched: {{.Count}}</Text></Gooey>`,
	})
	if err != nil {
		t.Fatalf("PatchMarkup: %v", err)
	}
	if len(res.Named) != 4 {
		t.Errorf("named after patch = %v", res.Named)
	}
	s := h.screen()
	if !strings.Contains(s, "patched: 0") {
		t.Fatalf("the patch did not render:\n%s", s)
	}
	if !strings.Contains(s, "survives") {
		t.Fatalf("the sibling lost its state:\n%s", s)
	}

	_, err = h.ctl.PatchMarkup(context.Background(), &controlv1.PatchMarkupRequest{
		Name:   "Head",
		Source: `<Gooey><Text Name="Renamed">x</Text></Gooey>`,
	})
	wantCode(t, err, codes.InvalidArgument, "the name is the address")
	_, err = h.ctl.PatchMarkup(context.Background(), &controlv1.PatchMarkupRequest{
		Name:   "Nope",
		Source: `<Gooey><Text Name="Nope">x</Text></Gooey>`,
	})
	wantCode(t, err, codes.NotFound, "")
}

func TestListStyles(t *testing.T) {
	h := newHarness(t)
	res, err := h.ctl.ListStyles(context.Background(), &controlv1.ListStylesRequest{})
	if err != nil {
		t.Fatalf("ListStyles: %v", err)
	}
	if len(res.Styles) != 2 {
		t.Fatalf("styles = %v", res.Styles)
	}
	first := res.Styles[0]
	if first.Name != "accent" || !first.Bold || first.Fg == nil || !first.Fg.Set || first.Fg.Red != 0xff {
		t.Errorf("accent = %v", first)
	}
	if res.Styles[1].Name != "dim" || !res.Styles[1].Dim || res.Styles[1].Fg != nil {
		t.Errorf("dim = %v", res.Styles[1])
	}
}

func TestValidateMarkup(t *testing.T) {
	h := newHarness(t)
	ok, err := h.ctl.ValidateMarkup(context.Background(), &controlv1.ValidateMarkupRequest{
		Source: `<Gooey><Text Name="T">{{.Count}}</Text></Gooey>`,
	})
	if err != nil {
		t.Fatalf("ValidateMarkup: %v", err)
	}
	if !ok.Valid || len(ok.Named) != 1 || ok.Named[0] != "T" {
		t.Errorf("valid result = %v", ok)
	}
	// Invalid markup is a NORMAL result — the answer, not a failure.
	bad, err := h.ctl.ValidateMarkup(context.Background(), &controlv1.ValidateMarkupRequest{
		Source: `<Gooey><Nope/></Gooey>`,
	})
	if err != nil {
		t.Fatalf("ValidateMarkup(bad) must not be a status error: %v", err)
	}
	if bad.Valid || !strings.Contains(bad.Error, "unknown element") {
		t.Errorf("invalid result = %v", bad)
	}
	// And the check was invisible: the page still stands.
	if s := h.screen(); !strings.Contains(s, "count is 0") {
		t.Fatalf("validation disturbed the app:\n%s", s)
	}
}

func TestWithoutContextTheTreeStillServes(t *testing.T) {
	_, values := newVM()
	app := newTestApp(t, testMarkup, values, nil)
	srv, err := Serve(app, Options{}) // no Context
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	conn, err := grpcgo.NewClient(srv.Addr(), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ctl := controlv1.NewControlServiceClient(conn)
	if _, err := ctl.SnapshotTree(context.Background(), &controlv1.SnapshotTreeRequest{}); err != nil {
		t.Errorf("SnapshotTree without a context: %v", err)
	}
	_, err = ctl.ListValues(context.Background(), &controlv1.ListValuesRequest{})
	wantCode(t, err, codes.FailedPrecondition, "markup context")
}
