package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// The harness hand-runs gooey.App's loop instead of using it, because the
// thing under test IS the goroutine boundary: a test that cannot see
// which goroutine a tool body ran on cannot prove the confinement rule.
// So the loop here owns the Composer and the property graph exactly the
// way App.Run does — frame at the top, drain in the select — and the
// tests assert against that ownership.

const testMarkup = `<Gooey>
  <VStack Gap="1">
    <Text Name="Head">count is {{.Count}}</Text>
    <Button Name="Inc" Content="add one" Click="{{.Increment}}"/>
    <Checkbox Name="Flag" Checked="{{.Flag}}" Label="the flag"/>
    <TextBox Name="Note" Text="{{.Note}}"/>
    <KeyBinding Gesture="ctrl+r" Command="{{.Reset}}"/>
  </VStack>
</Gooey>`

type testApp struct {
	ctx  *markup.Context
	disp *gooey.Dispatcher

	// Everything below is UI-goroutine state: written and read only by
	// run(), or by a closure run() drained. A tool that touched any of it
	// from an http goroutine is a -race failure, which is the point.
	comp       *gooey.Composer
	cols, rows int
	needsFrame bool
	frames     int
	draining   bool

	quit chan struct{}
	done chan struct{}
}

func newTestApp(t *testing.T, src string, values map[string]any) *testApp {
	return newTestAppWith(t, src, values, nil)
}

// newTestAppWith lets a test prepare the context — styles, includes —
// before the page builds against it.
func newTestAppWith(t *testing.T, src string, values map[string]any, prep func(*markup.Context)) *testApp {
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
}

func (a *testApp) run() {
	defer close(a.done)
	for {
		if a.needsFrame {
			a.frames++
			a.comp.Frame()
			a.needsFrame = false
		}
		select {
		case <-a.quit:
			a.comp.Close()
			return
		case <-a.disp.Wake():
			a.draining = true
			a.disp.Drain()
			a.draining = false
		}
	}
}

// Host: Post is the only method safe off the loop, and the only one the
// server calls from an http goroutine.
func (a *testApp) Post(fn func())            { a.disp.Post(fn) }
func (a *testApp) Composer() *gooey.Composer { return a.comp }
func (a *testApp) Swap(root gooey.Component) { a.attach(root) }

// ---- a viewmodel the tests share ----

type viewmodel struct {
	count *prop.Property[int]
	text  *prop.Property[string]
	note  *prop.Property[string]
	flag  *prop.Property[bool]
	tint  *prop.Property[render.Color]
	ratio *prop.Property[float64]
	incs  int
}

func newVM() (*viewmodel, map[string]any) {
	vm := &viewmodel{
		count: prop.NewSource(0),
		note:  prop.NewSource(""),
		flag:  prop.NewSource(false),
		tint:  prop.NewSource(render.RGB(10, 20, 30)),
		ratio: prop.NewSource(0.5),
	}
	vm.text = prop.NewComputed(func() string { return fmt.Sprintf("%d", vm.count.Get()) })
	return vm, map[string]any{
		"Count":     vm.text,
		"Note":      vm.note,
		"Flag":      vm.flag,
		"Tint":      vm.tint,
		"Ratio":     vm.ratio,
		"Label":     "a literal",
		"Increment": gooey.Command(func() { vm.incs++; vm.count.Set(vm.count.Get() + 1) }),
		"Reset":     gooey.Command(func() { vm.count.Set(0) }),
	}
}

// ---- an MCP client ----

// The client speaks the wire protocol by hand rather than through the
// SDK's own mcp.Client. That is deliberate: the SDK client and the SDK
// server would agree with each other whatever they did, so a test built
// from both proves only that the library is self-consistent. These are
// literal JSON-RPC bodies over HTTP, which is what an arbitrary MCP
// client will send.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type client struct {
	t   *testing.T
	url string
	id  int
}

func newClient(t *testing.T, s *Server) *client {
	t.Helper()
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return &client{t: t, url: srv.URL + endpointPath}
}

func (c *client) rpc(method string, params any) rpcResponse {
	c.t.Helper()
	c.id++
	body := map[string]any{"jsonrpc": "2.0", "id": c.id, "method": method}
	if params != nil {
		body["params"] = params
	}
	b, err := json.Marshal(body)
	if err != nil {
		c.t.Fatalf("marshal: %v", err)
	}
	req, _ := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s: %v", method, err)
	}
	defer resp.Body.Close()
	var out rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		c.t.Fatalf("%s: decode: %v", method, err)
	}
	return out
}

// call runs a tool and returns its text content plus whether the tool
// reported an error. A JSON-RPC-level error fails the test: that means
// the protocol envelope was wrong, not the tool.
func (c *client) call(name string, args map[string]any) (string, bool) {
	c.t.Helper()
	resp := c.rpc("tools/call", map[string]any{"name": name, "arguments": args})
	if resp.Error != nil {
		c.t.Fatalf("tools/call %s: rpc error %d: %s", name, resp.Error.Code, resp.Error.Message)
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		c.t.Fatalf("tools/call %s: result is %T", name, resp.Result)
	}
	content, _ := m["content"].([]any)
	if len(content) == 0 {
		c.t.Fatalf("tools/call %s: no content", name)
	}
	first, _ := content[0].(map[string]any)
	text, _ := first["text"].(string)
	isErr, _ := m["isError"].(bool)
	return text, isErr
}

// ok calls a tool and fails if it reported an error.
func (c *client) ok(name string, args map[string]any) string {
	c.t.Helper()
	text, isErr := c.call(name, args)
	if isErr {
		c.t.Fatalf("tools/call %s: unexpected tool error: %s", name, text)
	}
	return text
}

// fails calls a tool expecting a tool-level error containing want.
func (c *client) fails(name string, args map[string]any, want string) string {
	c.t.Helper()
	text, isErr := c.call(name, args)
	if !isErr {
		c.t.Fatalf("tools/call %s: expected an error, got: %s", name, text)
	}
	if !strings.Contains(text, want) {
		c.t.Fatalf("tools/call %s: error %q does not mention %q", name, text, want)
	}
	return text
}

// resultObject returns the raw tools/call result object, for assertions
// on fields beyond the first text content — structuredContent above all.
func (c *client) resultObject(name string, args map[string]any) map[string]any {
	c.t.Helper()
	resp := c.rpc("tools/call", map[string]any{"name": name, "arguments": args})
	if resp.Error != nil {
		c.t.Fatalf("tools/call %s: rpc error %d: %s", name, resp.Error.Code, resp.Error.Message)
	}
	m, ok := resp.Result.(map[string]any)
	if !ok {
		c.t.Fatalf("tools/call %s: result is %T", name, resp.Result)
	}
	return m
}

func (c *client) json(name string, args map[string]any) map[string]any {
	c.t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(c.ok(name, args)), &out); err != nil {
		c.t.Fatalf("tools/call %s: result is not JSON: %v", name, err)
	}
	return out
}

func setup(t *testing.T) (*testApp, *viewmodel, *Server, *client) {
	t.Helper()
	vm, values := newVM()
	app := newTestApp(t, testMarkup, values)
	s, err := New(app, Options{Context: app.ctx, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return app, vm, s, newClient(t, s)
}

// ---- protocol ----

func TestInitializeAndToolsList(t *testing.T) {
	_, _, s, c := setup(t)

	init := c.rpc("initialize", map[string]any{"protocolVersion": "2025-06-18"})
	if init.Error != nil {
		t.Fatalf("initialize: %v", init.Error)
	}
	res := init.Result.(map[string]any)
	if got := res["protocolVersion"]; got != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want the client's own version echoed back", got)
	}
	if _, ok := res["capabilities"].(map[string]any)["tools"]; !ok {
		t.Error("server did not advertise the tools capability")
	}

	list := c.rpc("tools/list", nil)
	tools := list.Result.(map[string]any)["tools"].([]any)
	if len(tools) != len(s.Tools()) {
		t.Fatalf("tools/list returned %d tools, server has %d", len(tools), len(s.Tools()))
	}
	got := map[string]bool{}
	for _, raw := range tools {
		m := raw.(map[string]any)
		name := m["name"].(string)
		got[name] = true
		if m["description"] == "" {
			t.Errorf("tool %s has no description", name)
		}
		if _, ok := m["inputSchema"].(map[string]any); !ok {
			t.Errorf("tool %s has no inputSchema", name)
		}
	}
	for _, want := range []string{
		"tree_snapshot", "screen_text", "list_values",
		"invoke_command", "set_value", "send_keys", "send_mouse", "focus",
		"swap_markup",
	} {
		if !got[want] {
			t.Errorf("v1 tool %q is missing", want)
		}
	}
}

func TestUnknownMethodAndUnknownTool(t *testing.T) {
	_, _, s, c := setup(t)

	// A method the protocol does not define is refused by the SDK's
	// transport before it reaches any handler: HTTP 400 with a plain-text
	// body, not a JSON-RPC MethodNotFound envelope. (resources/list and
	// prompts/list would not do as the unknown method — the SDK answers
	// those with empty lists whether or not this server has any.)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp := postRaw(t, srv.URL+endpointPath, `{"jsonrpc":"2.0","id":1,"method":"nonsense/list"}`)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "nonsense/list") {
		t.Errorf("unknown method answered %d %q, want 400 naming the method", resp.StatusCode, body)
	}

	// An unknown TOOL is a protocol error too: the client asked for
	// something that does not exist, which is different from a tool that
	// ran and failed. That one IS a JSON-RPC error, and it stays
	// InvalidParams.
	rpc := c.rpc("tools/call", map[string]any{"name": "no_such_tool"})
	if rpc.Error == nil || rpc.Error.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("unknown tool should be a JSON-RPC InvalidParams, got %+v", rpc.Error)
	}
}

// postRaw sends one body with the headers the streamable-HTTP transport
// requires, so a test can assert on status codes rather than on a decoded
// result. Extra headers are added, not set, so a case can send the same
// header twice.
func postRaw(t *testing.T, url, body string, header ...[2]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for _, h := range header {
		req.Header.Add(h[0], h[1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestNotificationGetsNoBody(t *testing.T) {
	_, _, s, _ := setup(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := postRaw(t, srv.URL+endpointPath, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("notification answered %d, want 202 with no body", resp.StatusCode)
	}
}

// TestNoStreamAndNoSession pins the two transport shapes this server
// declines. It runs stateless — one app, one tree, the same one for every
// client — so there is no session to delete, and nothing here is
// server-initiated, so there is no stream to hold open. Both are 405
// rather than 404 or a hang, which is how a client is told to stop
// asking.
func TestNoStreamAndNoSession(t *testing.T) {
	_, _, s, _ := setup(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req, _ := http.NewRequest(method, srv.URL+endpointPath, nil)
		req.Header.Set("Accept", "application/json, text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s answered %d, want 405", method, resp.StatusCode)
		}
	}

	// A tools/call with no handshake and no session id still works: that
	// is what stateless buys, and it is what lets a one-shot client (curl,
	// a shell script) drive the app.
	resp := postRaw(t, srv.URL+endpointPath,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"screen_text","arguments":{}}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bare tools/call answered %d, want 200", resp.StatusCode)
	}
	var out rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Error != nil {
		t.Fatalf("bare tools/call: %+v", out.Error)
	}
}

// TestCheckOrigin enumerates the trust boundary. It is a table rather
// than a few spot checks because the interesting cases are the ones that
// LOOK inert: "null", unparseable junk and file:// all parse to an empty
// hostname, and an earlier version of checkOrigin allowed exactly that,
// which made the whole guard a no-op for the attack it exists to stop.
func TestCheckOrigin(t *testing.T) {
	const port = "7788"
	cases := []struct {
		origin string
		want   bool
		why    string
	}{
		{"http://localhost:7788", true, "the same-app page"},
		{"http://127.0.0.1:7788", true, "loopback by address"},
		{"http://[::1]:7788", true, "loopback by v6 address"},
		{"https://localhost:7788", true, "https is allowed; host and port carry the security"},

		{"null", false, "sandboxed iframes and file:// pages send this"},
		{"NULL", false, "and the check must not be case-sensitive"},
		{"", false, "present but empty is malformed, not absent"},
		{"   ", false, "whitespace is not absent either"},
		{"garbage", false, "unparseable junk parses to an empty hostname"},
		{"file:///etc/passwd", false, "also an empty hostname"},
		{"chrome-extension://abc", false, "scheme outside the allowlist"},
		{"//localhost:7788", false, "scheme-less, and it DOES parse to hostname localhost"},

		{"http://evil.com", false, "a plain foreign origin"},
		{"https://evil.com:7788", false, "matching the port does not help"},
		{"http://127.0.0.1.evil.com:7788", false, "a DNS name that merely starts like loopback"},
		{"http://localhost.evil.com:7788", false, "and one that starts like localhost"},
		{"http://localhost:7788@evil.com", false, "the userinfo trick: real host is evil.com"},
		{"http://localhost:abc", false, "an invalid port fails to parse"},

		{"http://localhost:3000", false, "another local service must not drive this app"},
		{"http://localhost", false, "port 80 is not our port"},
		{"http://127.0.0.1:7789", false, "off by one port"},
	}
	for _, tc := range cases {
		if got := checkOrigin(tc.origin, port); got != tc.want {
			t.Errorf("checkOrigin(%q) = %v, want %v — %s", tc.origin, got, tc.want, tc.why)
		}
	}

	// With no knowable port (a Handler mounted by an embedding host) the
	// hostname rules still hold; only the port rule relaxes.
	if !checkOrigin("http://localhost:3000", "") {
		t.Error("with no expected port, any loopback port should pass")
	}
	if checkOrigin("http://evil.com:3000", "") {
		t.Error("relaxing the port must not relax the hostname")
	}
}

// TestOriginHeaderHandling covers the header layer that the table cannot:
// absent vs present, and a duplicated header.
func TestOriginHeaderHandling(t *testing.T) {
	_, _, s, _ := setup(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	post := func(origins ...string) int {
		t.Helper()
		header := make([][2]string, 0, len(origins))
		for _, o := range origins {
			header = append(header, [2]string{"Origin", o})
		}
		resp := postRaw(t, srv.URL+endpointPath, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, header...)
		defer resp.Body.Close()
		return resp.StatusCode
	}

	// httptest listens on 127.0.0.1:<ephemeral>; the server has no
	// listener of its own here, so the expected port comes from r.Host.
	_, port, err := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}

	if got := post(); got != http.StatusOK {
		t.Errorf("no Origin header got %d, want 200 — non-browser clients must work", got)
	}
	if got := post("http://localhost:" + port); got != http.StatusOK {
		t.Errorf("same-app origin got %d, want 200", got)
	}
	for _, bad := range []string{"null", "https://evil.example.com", "garbage", "http://localhost:1"} {
		if got := post(bad); got != http.StatusForbidden {
			t.Errorf("Origin %q got %d, want 403 — this is the DNS-rebinding guard", bad, got)
		}
	}
	if got := post("http://localhost:"+port, "http://localhost:"+port); got != http.StatusForbidden {
		t.Errorf("duplicated Origin got %d, want 403 — ambiguous headers must not be guessed at", got)
	}
}

// TestServeEnforcesOwnPort proves the port rule uses the REAL listener
// when the server owns one, not just whatever Host the client sent.
func TestServeEnforcesOwnPort(t *testing.T) {
	_, values := newVM()
	app := newTestApp(t, testMarkup, values)
	s, err := Serve(app, Options{Addr: "127.0.0.1:0", Context: app.ctx})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	_, port, err := net.SplitHostPort(s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	if !checkOrigin("http://localhost:"+port, s.expectedPort(&http.Request{Host: "attacker.example:1"})) {
		t.Error("the listener's own port should be the expected port")
	}
	if checkOrigin("http://localhost:1", s.expectedPort(&http.Request{Host: "attacker.example:1"})) {
		t.Error("a spoofed Host must not be able to relax the port rule when we own a listener")
	}
}

func TestServeRefusesNonLoopback(t *testing.T) {
	vm, values := newVM()
	_ = vm
	app := newTestApp(t, testMarkup, values)
	for _, addr := range []string{"0.0.0.0:0", ":8080", "8.8.8.8:80"} {
		if _, err := Serve(app, Options{Addr: addr}); err == nil {
			t.Errorf("Serve(%q) succeeded; v1 has no auth, so non-loopback binds must be refused", addr)
		}
	}
	srv, err := Serve(app, Options{Addr: "127.0.0.1:0", Context: app.ctx})
	if err != nil {
		t.Fatalf("Serve on loopback: %v", err)
	}
	defer srv.Close()
	if !strings.HasPrefix(srv.URL(), "http://127.0.0.1:") {
		t.Errorf("URL = %q", srv.URL())
	}
}

// ---- read tools ----

func TestTreeSnapshot(t *testing.T) {
	_, _, _, c := setup(t)
	out := c.json("tree_snapshot", nil)

	root := out["tree"].(map[string]any)
	if root["type"] != "*components.VStack" {
		t.Errorf("root type = %v", root["type"])
	}
	byName := map[string]map[string]any{}
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		if name, ok := n["name"].(string); ok {
			byName[name] = n
		}
		for _, k := range []string{"children", "attached"} {
			kids, _ := n[k].([]any)
			for _, ch := range kids {
				walk(ch.(map[string]any))
			}
		}
	}
	walk(root)

	for _, want := range []string{"Head", "Inc", "Flag", "Note"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("named element %q missing from the snapshot; got %v", want, keys(byName))
		}
	}
	if got := byName["Head"]["props"].(map[string]any)["text"]; got != "count is 0" {
		t.Errorf("Head text = %v, want the bound content", got)
	}
	if got := byName["Inc"]["props"].(map[string]any)["content"]; got != "add one" {
		t.Errorf("Inc content = %v", got)
	}
	if got := byName["Flag"]["props"].(map[string]any)["checked"]; got != false {
		t.Errorf("Flag checked = %v", got)
	}
	// Focus is framework state, not markup state: the first focus stop
	// holds it, and the snapshot has to say so.
	if byName["Inc"]["focused"] != true {
		t.Errorf("Inc should hold focus at startup, got %v", byName["Inc"]["focused"])
	}
	if byName["Head"]["focusable"] == true {
		t.Error("a Text is not a focus stop")
	}
	b := byName["Inc"]["bounds"].(map[string]any)
	if b["w"].(float64) <= 0 || b["h"].(float64) <= 0 {
		t.Errorf("Inc has no arranged bounds: %v", b)
	}
	// The KeyBinding is an attachment, not a child, and must serialize as
	// one — it is part of the app's behavior surface.
	kb := findType(root, "*gooey.KeyBinding")
	if kb == nil {
		t.Fatal("the KeyBinding attachment is missing from the snapshot")
	}
	if got := kb["props"].(map[string]any)["gesture"]; got != "ctrl+r" {
		t.Errorf("KeyBinding gesture = %v", got)
	}
}

func TestTreeSnapshotDepth(t *testing.T) {
	_, _, _, c := setup(t)
	out := c.json("tree_snapshot", map[string]any{"depth": 1})
	root := out["tree"].(map[string]any)
	if _, ok := root["children"]; ok {
		t.Error("depth=1 should not descend into children")
	}
	if root["childrenElided"].(float64) < 1 {
		t.Error("an elided subtree should say how many children it hid")
	}
}

func TestScreenText(t *testing.T) {
	_, _, _, c := setup(t)
	screen := c.ok("screen_text", nil)
	if !strings.Contains(screen, "count is 0") {
		t.Errorf("screen does not show the bound text:\n%s", screen)
	}
	if !strings.Contains(screen, "[ add one ]") {
		t.Errorf("screen does not show the button:\n%s", screen)
	}
	if !strings.Contains(screen, "[ ] the flag") {
		t.Errorf("screen does not show the checkbox:\n%s", screen)
	}
	styled := c.ok("screen_text", map[string]any{"styled": true})
	if !strings.Contains(styled, "\x1b[") {
		t.Error("styled screen_text should contain ANSI escapes")
	}
}

func TestListValues(t *testing.T) {
	_, _, _, c := setup(t)
	out := c.json("list_values", nil)
	byName := map[string]map[string]any{}
	for _, raw := range out["values"].([]any) {
		v := raw.(map[string]any)
		byName[v["name"].(string)] = v
	}
	cases := []struct{ name, kind, typ string }{
		{"Count", "property", "string"},
		{"Note", "property", "string"},
		{"Flag", "property", "boolean"},
		{"Tint", "property", "color"},
		{"Ratio", "property", "number"},
		{"Label", "literal", "string"},
	}
	for _, tc := range cases {
		v, ok := byName[tc.name]
		if !ok {
			t.Errorf("%s missing from list_values", tc.name)
			continue
		}
		if v["kind"] != tc.kind || v["type"] != tc.typ {
			t.Errorf("%s: kind/type = %v/%v, want %s/%s", tc.name, v["kind"], v["type"], tc.kind, tc.typ)
		}
	}
	if byName["Increment"]["kind"] != "command" {
		t.Errorf("Increment kind = %v, want command", byName["Increment"]["kind"])
	}
	if byName["Tint"]["value"] != "#0a141e" {
		t.Errorf("Tint value = %v, want the hex form", byName["Tint"]["value"])
	}
	named := out["named"].([]any)
	if len(named) != 4 {
		t.Errorf("named elements = %v, want the four Name= attributes", named)
	}
}

// ---- act tools ----

func TestInvokeCommand(t *testing.T) {
	_, vm, _, c := setup(t)
	c.ok("invoke_command", map[string]any{"name": "Increment"})
	c.ok("invoke_command", map[string]any{"name": "Increment"})

	screen := c.ok("screen_text", nil)
	if !strings.Contains(screen, "count is 2") {
		t.Errorf("two invocations did not reach the screen:\n%s", screen)
	}
	if vm.incs != 2 {
		t.Errorf("command ran %d times, want 2", vm.incs)
	}

	c.fails("invoke_command", map[string]any{"name": "Nope"}, `no value named "Nope"`)
	c.fails("invoke_command", map[string]any{"name": "Count"}, "not a command")
	c.fails("invoke_command", nil, `missing required argument "name"`)
}

func TestSetValue(t *testing.T) {
	_, vm, _, c := setup(t)

	c.ok("set_value", map[string]any{"name": "Note", "value": "written by a client"})
	c.ok("set_value", map[string]any{"name": "Flag", "value": true})
	c.ok("set_value", map[string]any{"name": "Ratio", "value": 0.25})
	c.ok("set_value", map[string]any{"name": "Tint", "value": "#ff8800"})

	if got := vm.note.Get(); got != "written by a client" {
		t.Errorf("Note = %q", got)
	}
	if !vm.flag.Get() {
		t.Error("Flag was not set")
	}
	if vm.ratio.Get() != 0.25 {
		t.Errorf("Ratio = %v", vm.ratio.Get())
	}
	if got := vm.tint.Get(); got != render.RGB(0xff, 0x88, 0x00) {
		t.Errorf("Tint = %v", got)
	}

	screen := c.ok("screen_text", nil)
	if !strings.Contains(screen, "written by a client") || !strings.Contains(screen, "[x] the flag") {
		t.Errorf("set_value did not repaint:\n%s", screen)
	}

	// Error paths: a mismatch names both sides and changes nothing.
	c.fails("set_value", map[string]any{"name": "Flag", "value": "yes"}, "boolean property")
	c.fails("set_value", map[string]any{"name": "Note", "value": 3}, "string property")
	c.fails("set_value", map[string]any{"name": "Tint", "value": "orange"}, "not a #rrggbb color")
	c.fails("set_value", map[string]any{"name": "Missing", "value": 1}, `no value named "Missing"`)
	c.fails("set_value", map[string]any{"name": "Increment", "value": 1}, "set_value handles")
	if !vm.flag.Get() || vm.note.Get() != "written by a client" {
		t.Error("a rejected set_value changed state anyway")
	}
}

// TestSetValueOnComputed proves an MCP client cannot crash the app. Set
// on a computed property panics by design; the bridge has to turn that
// into an error on the UI goroutine rather than let it unwind the loop.
func TestSetValueOnComputed(t *testing.T) {
	app, _, _, c := setup(t)
	c.fails("set_value", map[string]any{"name": "Count", "value": "9"}, "Set on computed property")

	// The app is still alive and still serving.
	before := c.ok("screen_text", nil)
	if !strings.Contains(before, "count is 0") {
		t.Errorf("app state disturbed:\n%s", before)
	}
	select {
	case <-app.done:
		t.Fatal("the run loop died from a tool panic")
	default:
	}
}

func TestSendKeys(t *testing.T) {
	_, vm, _, c := setup(t)

	// Focus starts on the button: enter runs its command.
	c.ok("send_keys", map[string]any{"keys": []any{"enter"}})
	if vm.count.Get() != 1 {
		t.Errorf("enter on the focused button did not fire it (count=%d)", vm.count.Get())
	}

	// Tab twice to reach the TextBox, then type.
	c.ok("send_keys", map[string]any{"keys": []any{"tab", "tab"}})
	c.ok("send_keys", map[string]any{"text": "hello"})
	if got := vm.note.Get(); got != "hello" {
		t.Errorf("typed text landed as %q, want %q", got, "hello")
	}
	if !strings.Contains(c.ok("screen_text", nil), "hello") {
		t.Error("typed text is not on screen")
	}

	// A KeyBinding on an ancestor fires from a focused descendant.
	c.ok("send_keys", map[string]any{"keys": []any{"ctrl+r"}})
	if vm.count.Get() != 0 {
		t.Errorf("ctrl+r binding did not reset (count=%d)", vm.count.Get())
	}

	c.fails("send_keys", map[string]any{"keys": []any{"ctrl+nope"}}, "unknown key")
	c.fails("send_keys", nil, "needs text or keys")
}

func TestSendMouse(t *testing.T) {
	_, vm, _, c := setup(t)
	snap := c.json("tree_snapshot", nil)
	inc := findName(snap["tree"].(map[string]any), "Inc")
	b := inc["bounds"].(map[string]any)
	x, y := int(b["x"].(float64))+1, int(b["y"].(float64))

	c.ok("send_mouse", map[string]any{"kind": "click", "x": x, "y": y})
	if vm.count.Get() != 1 {
		t.Errorf("clicking the button did not fire it (count=%d)", vm.count.Get())
	}

	// Hovering shows up in the snapshot, which is how an agent can tell
	// where the pointer is.
	c.ok("send_mouse", map[string]any{"kind": "move", "x": x, "y": y})
	if findName(c.json("tree_snapshot", nil)["tree"].(map[string]any), "Inc")["hovered"] != true {
		t.Error("hover is not reflected in the snapshot")
	}

	c.fails("send_mouse", map[string]any{"kind": "wiggle", "x": 0, "y": 0}, "unknown mouse kind")
	c.fails("send_mouse", map[string]any{"kind": "click", "x": "left", "y": 0}, "whole number")
}

func TestFocus(t *testing.T) {
	_, _, _, c := setup(t)
	c.ok("focus", map[string]any{"name": "Note"})
	snap := c.json("tree_snapshot", nil)["tree"].(map[string]any)
	if findName(snap, "Note")["focused"] != true {
		t.Error("focus did not move to Note")
	}
	if findName(snap, "Inc")["focused"] == true {
		t.Error("the previously focused component still claims focus")
	}
	c.fails("focus", map[string]any{"name": "Head"}, "not a focus stop")
	c.fails("focus", map[string]any{"name": "Ghost"}, `no element named "Ghost"`)
}

// ---- structural mutation ----

const swappedMarkup = `<Gooey>
  <VStack Gap="1">
    <Text Name="Banner">swapped: {{.Count}}</Text>
    <Button Name="Inc" Content="still adds" Click="{{.Increment}}"/>
  </VStack>
</Gooey>`

func TestSwapMarkup(t *testing.T) {
	_, vm, _, c := setup(t)
	c.ok("invoke_command", map[string]any{"name": "Increment"})
	c.ok("invoke_command", map[string]any{"name": "Increment"})

	out := c.json("swap_markup", map[string]any{"source": swappedMarkup})
	if out["swapped"] != true {
		t.Fatalf("swap_markup: %v", out)
	}

	screen := c.ok("screen_text", nil)
	if !strings.Contains(screen, "swapped: 2") {
		t.Errorf("new markup did not render:\n%s", screen)
	}
	if strings.Contains(screen, "the flag") {
		t.Errorf("old tree is still on screen:\n%s", screen)
	}
	// The viewmodel is the app's state and it lives in the Context, not
	// the tree, so a whole-page swap must not reset it.
	if vm.count.Get() != 2 {
		t.Errorf("count = %d after the swap, want the state to survive", vm.count.Get())
	}
	// And the new tree is live, not a snapshot.
	c.ok("invoke_command", map[string]any{"name": "Increment"})
	if !strings.Contains(c.ok("screen_text", nil), "swapped: 3") {
		t.Error("the swapped-in tree is not bound to the viewmodel")
	}
	// Names are rebuilt from the new document.
	named := c.json("list_values", nil)["named"].([]any)
	if len(named) != 2 {
		t.Errorf("named = %v, want the two names in the new markup", named)
	}
}

func TestSwapMarkupFailureKeepsOldTree(t *testing.T) {
	_, _, _, c := setup(t)
	before := c.ok("screen_text", nil)

	for _, bad := range []struct{ src, want string }{
		{`<Gooey><VStack><Nope/></VStack></Gooey>`, "unknown element"},
		{`<Gooey><Text>{{.Missing}}</Text></Gooey>`, "not found in context"},
		{`<VStack/>`, "root element must be <Gooey>"},
		{`<Gooey><Text>a</Text><Text>b</Text></Gooey>`, "exactly one child"},
		{`<Gooey><Button Click="{{.Count}}"/></Gooey>`, "need gooey.Command"},
	} {
		c.fails("swap_markup", map[string]any{"source": bad.src}, bad.want)
	}

	if after := c.ok("screen_text", nil); after != before {
		t.Errorf("a failed swap changed the screen:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	// The name table has to survive too, or the tools that address
	// elements by name would be collateral damage of a typo.
	if named := c.json("list_values", nil)["named"].([]any); len(named) != 4 {
		t.Errorf("named = %v after failed swaps, want the original four", named)
	}
	if _, isErr := c.call("focus", map[string]any{"name": "Note"}); isErr {
		t.Error("focus by name broke after a failed swap")
	}
}

// ---- the concurrency contract ----

// TestToolsRunOnTheUIGoroutine is the direct proof. The run loop sets a
// plain, unsynchronized bool while it is draining; a probe tool reads it.
// If the tool body ran on the http goroutine the read is both false and a
// data race, so this test fails twice over under -race.
func TestToolsRunOnTheUIGoroutine(t *testing.T) {
	app, _, s, c := setup(t)

	var sawDraining, sawComposer bool
	s.register(&Tool{
		Name:        "probe",
		Description: "test-only",
		Run: func(args) (any, error) {
			sawDraining = app.draining
			sawComposer = app.comp != nil
			return map[string]any{"ok": true}, nil
		},
	})

	c.ok("probe", nil)
	if !sawDraining {
		t.Error("the tool body did not run inside the run loop's Drain — it was not marshaled")
	}
	if !sawComposer {
		t.Error("the tool could not see the live composition")
	}
}

// TestSettleBeforeResponse is the guarantee that makes the whole surface
// usable without sleeps: a write returns only after the frame that shows
// it has been composed, so the very next read sees it. It is asserted by
// frame count rather than by content, because content could pass by luck.
func TestSettleBeforeResponse(t *testing.T) {
	app, _, s, c := setup(t)

	var framesAtCall int
	s.register(&Tool{Name: "frames", Run: func(args) (any, error) {
		framesAtCall = app.frames
		return app.frames, nil
	}})

	c.ok("frames", nil)
	before := framesAtCall
	c.ok("invoke_command", map[string]any{"name": "Increment"})
	c.ok("frames", nil)
	if framesAtCall <= before {
		t.Errorf("frames %d → %d: invoke_command returned before its repaint was composed", before, framesAtCall)
	}
	if !strings.Contains(c.ok("screen_text", nil), "count is 1") {
		t.Error("the settled frame does not show the write")
	}
}

// TestConcurrentToolCalls is the -race catcher. Requests arrive on many
// http goroutines at once while a Timer in the tree mutates the SAME
// properties the tools read. Every one of those touches has to happen on
// the run loop; a single one that does not is a reported race.
func TestConcurrentToolCalls(t *testing.T) {
	vm, values := newVM()
	app := newTestApp(t, `<Gooey>
      <VStack Gap="1">
        <Text Name="Head">count is {{.Count}}</Text>
        <Button Name="Inc" Content="add one" Click="{{.Increment}}"/>
        <Checkbox Name="Flag" Checked="{{.Flag}}" Label="the flag"/>
        <TextBox Name="Note" Text="{{.Note}}"/>
        <Timer Interval="1ms" Tick="{{.Increment}}"/>
      </VStack>
    </Gooey>`, values)

	s, err := New(app, Options{Context: app.ctx, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	const workers, calls = 8, 25
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			c := &client{t: t, url: srv.URL + endpointPath}
			for i := 0; i < calls; i++ {
				switch (w + i) % 6 {
				case 0:
					c.call("tree_snapshot", nil)
				case 1:
					c.call("screen_text", nil)
				case 2:
					c.call("list_values", nil)
				case 3:
					c.call("invoke_command", map[string]any{"name": "Increment"})
				case 4:
					c.call("set_value", map[string]any{"name": "Note", "value": fmt.Sprintf("w%d-%d", w, i)})
				case 5:
					c.call("send_keys", map[string]any{"keys": []any{"tab"}})
				}
			}
		}(w)
	}
	wg.Wait()

	// The timer and the clients both incremented; the only claim worth
	// making is that every increment landed, which it cannot have done if
	// any of them raced.
	done := make(chan int, 1)
	app.Post(func() { done <- vm.count.Get() })
	select {
	case n := <-done:
		if n < workers*calls/6 {
			t.Errorf("count = %d, fewer than the client-driven increments alone", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run loop stopped draining")
	}
}

// TestTimeoutWhenLoopIsBlocked proves a wedged app degrades to an error
// instead of a hung client.
func TestTimeoutWhenLoopIsBlocked(t *testing.T) {
	app, _, _, _ := setup(t)
	s, err := New(app, Options{Context: app.ctx, Timeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, s)

	release := make(chan struct{})
	app.Post(func() { <-release })
	defer close(release)

	text, isErr := c.call("screen_text", nil)
	if !isErr || !strings.Contains(text, "timed out") {
		t.Errorf("a blocked loop should time out, got isError=%v %q", isErr, text)
	}
}

// TestNoMarkupContext covers an app served without a Context: the tree
// and screen still work, the name-addressed tools explain themselves.
func TestNoMarkupContext(t *testing.T) {
	_, values := newVM()
	app := newTestApp(t, testMarkup, values)
	s, err := New(app, Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, s)

	if !strings.Contains(c.ok("screen_text", nil), "add one") {
		t.Error("screen_text needs no markup context")
	}
	c.ok("tree_snapshot", nil)
	for _, tool := range []string{"list_values", "focus", "swap_markup", "invoke_command"} {
		c.fails(tool, map[string]any{"name": "x", "source": "<Gooey/>"}, "without a markup context")
	}
}

// ---- helpers ----

func keys(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func findName(n map[string]any, name string) map[string]any {
	if n["name"] == name {
		return n
	}
	for _, k := range []string{"children", "attached"} {
		kids, _ := n[k].([]any)
		for _, ch := range kids {
			if got := findName(ch.(map[string]any), name); got != nil {
				return got
			}
		}
	}
	return nil
}

func findType(n map[string]any, typ string) map[string]any {
	if n["type"] == typ {
		return n
	}
	for _, k := range []string{"children", "attached"} {
		kids, _ := n[k].([]any)
		for _, ch := range kids {
			if got := findType(ch.(map[string]any), typ); got != nil {
				return got
			}
		}
	}
	return nil
}

// ---- patch_markup ----

func TestPatchMarkup(t *testing.T) {
	_, vm, _, c := setup(t)
	c.ok("invoke_command", map[string]any{"name": "Increment"})
	c.ok("invoke_command", map[string]any{"name": "Increment"})
	c.ok("set_value", map[string]any{"name": "Note", "value": "sibling state"})
	c.ok("focus", map[string]any{"name": "Note"})

	out := c.json("patch_markup", map[string]any{
		"name": "Head",
		"source": `<Gooey><VStack Name="Head">
		  <Text>patched says {{.Count}}</Text>
		  <Text>second line</Text>
		</VStack></Gooey>`,
	})
	if out["patched"] != "Head" {
		t.Fatalf("patch_markup: %v", out)
	}

	screen := c.ok("screen_text", nil)
	if !strings.Contains(screen, "patched says 2") || !strings.Contains(screen, "second line") {
		t.Errorf("the patched subtree did not render:\n%s", screen)
	}
	if strings.Contains(screen, "count is") {
		t.Errorf("the replaced subtree is still on screen:\n%s", screen)
	}
	// THE point of patch over swap: the siblings were never rebuilt, so
	// their state — a TextBox's text lives in the viewmodel, but its
	// caret, focus and identity live in the component — survives.
	if !strings.Contains(screen, "sibling state") {
		t.Errorf("the sibling TextBox lost its content:\n%s", screen)
	}
	tree := c.json("tree_snapshot", nil)["tree"].(map[string]any)
	note := findName(tree, "Note")
	if note == nil || note["focused"] != true {
		t.Error("focus did not survive on the untouched sibling")
	}
	head := findName(tree, "Head")
	if head == nil || head["type"] != "*components.VStack" {
		t.Errorf("Head does not address the new subtree: %v", head)
	}
	// The new subtree is live, and the untouched siblings still are too.
	c.ok("invoke_command", map[string]any{"name": "Increment"})
	if got := c.ok("screen_text", nil); !strings.Contains(got, "patched says 3") {
		t.Errorf("the patched subtree is not bound to the viewmodel:\n%s", got)
	}
	if vm.count.Get() != 3 {
		t.Errorf("count = %d, want 3", vm.count.Get())
	}
	// Patch again by the same address: the name survived the patch.
	c.ok("patch_markup", map[string]any{
		"name":   "Head",
		"source": `<Gooey><Text Name="Head">patched twice: {{.Count}}</Text></Gooey>`,
	})
	if got := c.ok("screen_text", nil); !strings.Contains(got, "patched twice: 3") {
		t.Errorf("the address did not survive iteration:\n%s", got)
	}
}

func TestPatchMarkupFailureIsInert(t *testing.T) {
	_, _, _, c := setup(t)
	c.ok("focus", map[string]any{"name": "Note"})
	before := c.ok("screen_text", nil)

	for _, bad := range []struct {
		name, src, want string
	}{
		{"Nope", `<Gooey><Text Name="Nope">x</Text></Gooey>`, "no element named"},
		{"Head", `<Gooey><Wat Name="Head"/></Gooey>`, "unknown element"},
		{"Head", `<Gooey><Text>x</Text></Gooey>`, "must carry Name"},
		{"Head", `<Gooey><Text Name="Other">x</Text></Gooey>`, "the patch address"},
		{"Head", `<Gooey><VStack Name="Head"><Text Name="Inc">x</Text></VStack></Gooey>`, "already names an element"},
	} {
		c.fails("patch_markup", map[string]any{"name": bad.name, "source": bad.src}, bad.want)
	}

	if after := c.ok("screen_text", nil); after != before {
		t.Errorf("a failed patch changed the screen:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if named := c.json("list_values", nil)["named"].([]any); len(named) != 4 {
		t.Errorf("named = %v after failed patches, want the original four", named)
	}
	tree := c.json("tree_snapshot", nil)["tree"].(map[string]any)
	if n := findName(tree, "Note"); n == nil || n["focused"] != true {
		t.Error("focus did not survive the failed patches")
	}
}

func TestPatchMarkupUnsupportedParent(t *testing.T) {
	vm, values := newVM()
	_ = vm
	app := newTestApp(t, `<Gooey>
	  <StatusBar>
	    <StatusBar.Left><Text Name="L">left</Text></StatusBar.Left>
	  </StatusBar>
	</Gooey>`, values)
	s, err := New(app, Options{Context: app.ctx, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, s)
	c.fails("patch_markup", map[string]any{
		"name":   "L",
		"source": `<Gooey><Text Name="L">new</Text></Gooey>`,
	}, "cannot rewrite")
}

// The layout rule: a fragment describes the panel's content; its cell in
// the parent's grid is preserved unless the fragment restates it — per
// attribute, so restating one does not surrender the others.
func TestPatchMarkupPreservesLayout(t *testing.T) {
	_, values := newVM()
	app := newTestApp(t, `<Gooey>
	  <Grid Rows="*,*" Cols="*,*">
	    <Text Name="A" Grid.Row="0" Grid.Col="0">a</Text>
	    <Text Name="B" Grid.Row="1" Grid.Col="1" Width="7">b</Text>
	  </Grid>
	</Gooey>`, values)
	s, err := New(app, Options{Context: app.ctx, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, s)

	layoutOfB := func() map[string]any {
		tree := c.json("tree_snapshot", nil)["tree"].(map[string]any)
		b := findName(tree, "B")
		if b == nil {
			t.Fatal("B vanished")
		}
		l, _ := b["layout"].(map[string]any)
		return l
	}

	c.ok("patch_markup", map[string]any{
		"name":   "B",
		"source": `<Gooey><Text Name="B">patched</Text></Gooey>`,
	})
	l := layoutOfB()
	if l["gridRow"] != 1.0 || l["gridCol"] != 1.0 || l["width"] != 7.0 {
		t.Errorf("unstated layout was not preserved: %v", l)
	}

	c.ok("patch_markup", map[string]any{
		"name":   "B",
		"source": `<Gooey><Text Name="B" Grid.Col="0">moved</Text></Gooey>`,
	})
	l = layoutOfB()
	if _, has := l["gridCol"]; has {
		t.Errorf("restated Grid.Col=0 did not take over: %v", l)
	}
	if l["gridRow"] != 1.0 || l["width"] != 7.0 {
		t.Errorf("restating one attribute surrendered the others: %v", l)
	}
}

// ---- list_styles ----

func TestListStyles(t *testing.T) {
	_, values := newVM()
	app := newTestAppWith(t, testMarkup, values, func(ctx *markup.Context) {
		ctx.Styles = map[string]render.Style{
			"accent": {Fg: render.RGB(0xff, 0x88, 0x00), Bold: true},
			"panel":  {Bg: render.RGB(0x10, 0x20, 0x30)},
		}
	})
	s, err := New(app, Options{Context: app.ctx, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, s)

	out := c.json("list_styles", nil)
	styles, _ := out["styles"].([]any)
	if len(styles) != 2 {
		t.Fatalf("styles = %v, want the two registered styles", out)
	}
	accent := styles[0].(map[string]any)
	panel := styles[1].(map[string]any)
	if accent["name"] != "accent" || panel["name"] != "panel" {
		t.Fatalf("styles are not sorted by name: %v", styles)
	}
	if accent["fg"] != "#ff8800" || accent["bold"] != true {
		t.Errorf("accent = %v, want its set attributes reported", accent)
	}
	if _, has := accent["bg"]; has {
		t.Errorf("accent reports an unset attribute: %v", accent)
	}
	if panel["bg"] != "#102030" {
		t.Errorf("panel = %v", panel)
	}
}

// ---- validate_markup ----

func TestValidateMarkup(t *testing.T) {
	app, _, s, c := setup(t)
	s.register(&Tool{Name: "frames", Description: "test-only", Run: func(args) (any, error) {
		return app.frames, nil
	}})
	framesNow := func() int {
		n, err := strconv.Atoi(strings.TrimSpace(c.ok("frames", nil)))
		if err != nil {
			t.Fatalf("frames: %v", err)
		}
		return n
	}
	before := framesNow()
	namedBefore := c.json("list_values", nil)["named"]

	out := c.json("validate_markup", map[string]any{"source": swappedMarkup})
	if out["valid"] != true {
		t.Fatalf("validate_markup rejected valid markup: %v", out)
	}
	named, _ := out["named"].([]any)
	if len(named) != 2 {
		t.Errorf("named = %v, want the document's two names", out["named"])
	}

	out = c.json("validate_markup", map[string]any{"source": `<Gooey><Text>{{.Missing}}</Text></Gooey>`})
	if out["valid"] != false {
		t.Fatalf("validate_markup accepted invalid markup: %v", out)
	}
	if errText, _ := out["error"].(string); !strings.Contains(errText, "not found in context") {
		t.Errorf("error = %v, want the typed load error", out["error"])
	}

	// The whole point: checking markup never flickers the live page. No
	// frame composed, no name table disturbed, the screen untouched.
	if after := framesNow(); after != before {
		t.Errorf("validation painted %d frame(s); it must paint none", after-before)
	}
	if got := fmt.Sprint(c.json("list_values", nil)["named"]); got != fmt.Sprint(namedBefore) {
		t.Errorf("validation disturbed the name table: %v", got)
	}
	if !strings.Contains(c.ok("screen_text", nil), "count is 0") {
		t.Error("validation disturbed the screen")
	}
}

// ---- structured output ----

func TestStructuredContentAndOutputSchemas(t *testing.T) {
	_, _, _, c := setup(t)

	// tools/list publishes outputSchema exactly for the data tools.
	list := c.rpc("tools/list", nil)
	hasSchema := map[string]bool{}
	for _, raw := range list.Result.(map[string]any)["tools"].([]any) {
		m := raw.(map[string]any)
		_, ok := m["outputSchema"].(map[string]any)
		hasSchema[m["name"].(string)] = ok
	}
	for _, want := range []string{"tree_snapshot", "list_values", "list_styles", "validate_markup"} {
		if !hasSchema[want] {
			t.Errorf("%s publishes no outputSchema", want)
		}
	}
	for _, not := range []string{"screen_text", "swap_markup", "patch_markup", "send_keys"} {
		if hasSchema[not] {
			t.Errorf("%s publishes an outputSchema it should not", not)
		}
	}

	// A data tool's result arrives twice and the two agree: text for
	// clients that only read text, structuredContent for schema-checked
	// consumption.
	res := c.resultObject("list_values", nil)
	sc, ok := res["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("list_values has no structuredContent: %v", res)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	var fromText map[string]any
	if err := json.Unmarshal([]byte(text), &fromText); err != nil {
		t.Fatalf("text content is not JSON: %v", err)
	}
	if fmt.Sprint(sc["named"]) != fmt.Sprint(fromText["named"]) {
		t.Errorf("structuredContent and text disagree:\n%v\n%v", sc["named"], fromText["named"])
	}
	if vals, ok := sc["values"].([]any); !ok || len(vals) == 0 {
		t.Errorf("structured values = %v", sc["values"])
	}

	// screen_text stays text-only.
	if res := c.resultObject("screen_text", nil); res["structuredContent"] != nil {
		t.Error("screen_text grew structuredContent; its result is text")
	}

	// tree_snapshot's structured form carries the tree.
	res = c.resultObject("tree_snapshot", nil)
	sc, _ = res["structuredContent"].(map[string]any)
	if sc == nil {
		t.Fatal("tree_snapshot has no structuredContent")
	}
	if tree, _ := sc["tree"].(map[string]any); tree == nil || tree["type"] == "" {
		t.Errorf("structured tree = %v", sc["tree"])
	}
}

// ---- declared properties in the snapshot ----

func TestTreeSnapshotDeclaredProperties(t *testing.T) {
	_, values := newVM()
	includes := fstest.MapFS{
		"card.gooey": &fstest.MapFile{Data: []byte(`<Gooey xmlns:x="wonderforge.io/gooey/x">
  <x:Property Name="Title" Type="string" Default="untitled"/>
  <x:Property Name="Count" Type="int" Default="3"/>
  <x:Property Name="Tint" Type="color" Default="#ff8800"/>
  <Text>{{.Title}}</Text>
</Gooey>`)},
	}
	app := newTestAppWith(t, `<Gooey>
	  <VStack>
	    <Card Name="C" Title="hello"/>
	    <Text Name="Plain">count is {{.Count}}</Text>
	  </VStack>
	</Gooey>`, values, func(ctx *markup.Context) {
		ctx.Includes = includes
	})
	s, err := New(app, Options{Context: app.ctx, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t, s)

	tree := c.json("tree_snapshot", nil)["tree"].(map[string]any)
	card := findName(tree, "C")
	if card == nil {
		t.Fatal("the control instance is not in the snapshot")
	}
	if card["control"] != "card.gooey" {
		t.Errorf("control = %v, want card.gooey", card["control"])
	}
	decls, _ := card["declared"].([]any)
	if len(decls) != 3 {
		t.Fatalf("declared = %v, want the three declarations", card["declared"])
	}
	byName := map[string]map[string]any{}
	for _, d := range decls {
		m := d.(map[string]any)
		byName[m["name"].(string)] = m
	}
	if got := byName["Title"]; got["type"] != "string" || got["value"] != "hello" {
		t.Errorf("Title = %v, want the instantiation-site value", got)
	}
	if got := byName["Count"]; got["type"] != "int" || got["value"] != 3.0 {
		t.Errorf("Count = %v, want the declared default", got)
	}
	if got := byName["Tint"]; got["type"] != "color" || got["value"] != "#ff8800" {
		t.Errorf("Tint = %v", got)
	}
	// An ordinary component still has no declared surface — the ceiling
	// stays where it was for anything without declarations.
	if plain := findName(tree, "Plain"); plain == nil || plain["declared"] != nil {
		t.Errorf("Plain grew a declared surface: %v", plain)
	}
}
