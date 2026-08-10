package mcp

import (
	"context"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
	"unsafe"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// The end-to-end proof: a REAL gooey.App, on a REAL pty, served over a
// REAL http listener, driven by an MCP client that speaks the wire
// protocol. Nothing here is a stand-in — the frames the assertions read
// are the escape sequences a terminal would have received, and the tool
// calls travel over TCP.
//
// The unit tests hand-run the loop so they can see which goroutine a tool
// body executed on; this one uses App.Run precisely because it cannot,
// which is what makes it a different kind of evidence. Between them they
// cover both halves: the confinement is correct, and *gooey.App satisfies
// the Host contract it was written against.

const e2ePage = `<Gooey>
  <VStack Gap="1">
    <Text Name="Head">count is {{.Count}}</Text>
    <Button Name="Inc" Content="add one" Click="{{.Increment}}"/>
    <TextBox Name="Note" Text="{{.Note}}"/>
  </VStack>
</Gooey>`

const e2eSwapped = `<Gooey>
  <VStack Gap="1">
    <Text Name="Banner">swapped, count survived: {{.Count}}</Text>
    <Button Name="Inc" Content="still adds" Click="{{.Increment}}"/>
  </VStack>
</Gooey>`

// e2eGrown binds two names the app never registered: Status arrives via
// register_properties, Pct rides in on the swap's own register argument.
const e2eGrown = `<Gooey>
  <VStack Gap="1">
    <Text Name="Status">{{.Status}}</Text>
    <ProgressBar Name="Bar" Value="{{.Pct}}" Label="pct"/>
  </VStack>
</Gooey>`

func TestEndToEndOverPTY(t *testing.T) {
	tt := newPTY(t, 60, 12)

	count := prop.NewSource(0)
	note := prop.NewSource("")
	text := prop.NewComputed(func() string { return itoa(count.Get()) })

	bind := &markup.Context{
		Values: map[string]any{
			"Count":     text,
			"Note":      note,
			"Increment": gooey.Command(func() { count.Set(count.Get() + 1) }),
		},
		Styles: map[string]render.Style{
			"accent": {Fg: render.RGB(0xff, 0x88, 0x00), Bold: true},
			"dim":    {Dim: true},
		},
	}

	fsys := fstest.MapFS{"page.gooey": &fstest.MapFile{Data: []byte(e2ePage)}}
	app := gooey.NewApp(markup.Page(fsys, "page.gooey", bind), gooey.WithTerminal(tt.open))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ran := make(chan error, 1)
	go func() { ran <- app.Run(ctx) }()
	t.Cleanup(func() {
		app.Quit()
		select {
		case <-ran:
		case <-time.After(3 * time.Second):
			t.Error("App.Run did not return")
		}
	})

	// The app has to be painting before there is anything to serve.
	if !tt.waitFor("count is 0") {
		t.Fatalf("the app never painted:\n%s", tt.text())
	}

	srv, err := Serve(app, Options{Addr: "127.0.0.1:0", Context: bind, Name: "gooey-e2e"})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer srv.Close()
	c := &client{t: t, url: srv.URL()}

	// --- handshake ---
	if init := c.rpc("initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"clientInfo":      map[string]any{"name": "gooey-e2e-test", "version": "1"},
	}); init.Error != nil {
		t.Fatalf("initialize: %v", init.Error)
	}
	if list := c.rpc("tools/list", nil); list.Error != nil {
		t.Fatalf("tools/list: %v", list.Error)
	}

	// --- read: the tree, then the screen ---
	tree := c.json("tree_snapshot", nil)["tree"].(map[string]any)
	if findName(tree, "Inc") == nil || findName(tree, "Note") == nil {
		t.Fatal("the live tree does not carry its Name= identities")
	}
	screen := c.ok("screen_text", nil)
	if !strings.Contains(screen, "count is 0") {
		t.Fatalf("screen_text does not match the running app:\n%s", screen)
	}

	// --- act: click by name, and see the screen change ---
	c.ok("invoke_command", map[string]any{"name": "Increment"})
	if got := c.ok("screen_text", nil); !strings.Contains(got, "count is 1") {
		t.Fatalf("invoke_command did not change the screen:\n%s", got)
	}
	// The same change reached the actual terminal, not just our buffer.
	if !tt.waitFor("count is 1") {
		t.Fatalf("the frame never reached the pty:\n%s", tt.text())
	}

	// --- act: set a value directly ---
	c.ok("set_value", map[string]any{"name": "Note", "value": "set over MCP"})
	if got := c.ok("screen_text", nil); !strings.Contains(got, "set over MCP") {
		t.Fatalf("set_value did not repaint:\n%s", got)
	}

	// --- act: drive focus and the keyboard ---
	c.ok("focus", map[string]any{"name": "Note"})
	c.ok("send_keys", map[string]any{"keys": []any{"end"}})
	c.ok("send_keys", map[string]any{"text": "!"})
	if got := c.ok("screen_text", nil); !strings.Contains(got, "set over MCP!") {
		t.Fatalf("typed text did not land:\n%s", got)
	}
	c.ok("focus", map[string]any{"name": "Inc"})
	c.ok("send_keys", map[string]any{"keys": []any{"enter"}})
	if got := c.ok("screen_text", nil); !strings.Contains(got, "count is 2") {
		t.Fatalf("enter on the focused button did not fire it:\n%s", got)
	}

	// --- read: the style table an agent generates markup from ---
	styles := c.json("list_styles", nil)["styles"].([]any)
	if len(styles) != 2 {
		t.Fatalf("list_styles = %v, want the two registered styles", styles)
	}
	if first := styles[0].(map[string]any); first["name"] != "accent" || first["fg"] != "#ff8800" {
		t.Errorf("list_styles does not match the registered styles: %v", first)
	}

	// --- check: validation never flickers the live page ---
	if !tt.waitFor("count is 2") {
		t.Fatalf("pty behind before validation:\n%s", tt.text())
	}
	bytesBefore := len(tt.text())
	if out := c.json("validate_markup", map[string]any{"source": e2eSwapped}); out["valid"] != true {
		t.Fatalf("validate_markup rejected valid markup: %v", out)
	}
	vres := c.json("validate_markup", map[string]any{"source": `<Gooey><Nope/></Gooey>`})
	if vres["valid"] != false || !strings.Contains(vres["error"].(string), "unknown element") {
		t.Fatalf("validate_markup did not report the typed load error: %v", vres)
	}
	time.Sleep(150 * time.Millisecond) // late bytes would arrive by now
	if got := len(tt.text()); got != bytesBefore {
		t.Errorf("validation put %d bytes on the terminal; a check must be invisible", got-bytesBefore)
	}

	// --- mutate structure: patch one subtree, siblings untouched ---
	c.ok("patch_markup", map[string]any{
		"name":   "Head",
		"source": `<Gooey><Text Name="Head">patched live: {{.Count}}</Text></Gooey>`,
	})
	if !tt.waitFor("patched live: 2") {
		t.Fatalf("the patched subtree never reached the pty:\n%s", tt.text())
	}
	patched := c.ok("screen_text", nil)
	if !strings.Contains(patched, "set over MCP!") {
		t.Fatalf("the sibling TextBox lost its state across the patch:\n%s", patched)
	}
	if strings.Contains(patched, "count is") {
		t.Fatalf("the replaced subtree is still on screen:\n%s", patched)
	}

	// --- structured output round-trips to a wire client ---
	lv := c.rpc("tools/call", map[string]any{"name": "list_values", "arguments": map[string]any{}})
	sc, _ := lv.Result.(map[string]any)["structuredContent"].(map[string]any)
	if sc == nil {
		t.Fatal("list_values carries no structuredContent on the wire")
	}
	if named, _ := sc["named"].([]any); len(named) != 3 {
		t.Errorf("structured named = %v, want the page's three names", sc["named"])
	}

	// --- mutate structure: a bad swap must be inert ---
	c.fails("swap_markup", map[string]any{"source": `<Gooey><Nope/></Gooey>`}, "unknown element")
	if got := c.ok("screen_text", nil); !strings.Contains(got, "patched live: 2") {
		t.Fatalf("a failed swap disturbed the running app:\n%s", got)
	}

	// --- mutate structure: the real swap ---
	c.ok("swap_markup", map[string]any{"source": e2eSwapped})
	got := c.ok("screen_text", nil)
	if !strings.Contains(got, "swapped, count survived: 2") {
		t.Fatalf("the new page did not render, or the state did not survive:\n%s", got)
	}
	if strings.Contains(got, "count is") {
		t.Fatalf("the old tree is still on screen:\n%s", got)
	}
	if !tt.waitFor("swapped, count survived: 2") {
		t.Fatalf("the swapped page never reached the pty:\n%s", tt.text())
	}
	// The swapped-in tree is live: its button drives the same viewmodel.
	c.ok("invoke_command", map[string]any{"name": "Increment"})
	if got := c.ok("screen_text", nil); !strings.Contains(got, "survived: 3") {
		t.Fatalf("the swapped tree is not bound to the viewmodel:\n%s", got)
	}

	// --- grow the viewmodel live (#89): register, then bind ---
	c.ok("register_properties", map[string]any{"properties": []any{
		map[string]any{"name": "Status", "type": "string", "value": "registered live"},
	}})
	// A failed swap rolls its own registrations back atomically...
	c.fails("swap_markup", map[string]any{
		"source":   `<Gooey><Nope/></Gooey>`,
		"register": []any{map[string]any{"name": "Ghost", "type": "int"}},
	}, "unknown element")
	// ...so Ghost is gone, while Status (registered beforehand) stays.
	c.fails("set_value", map[string]any{"name": "Ghost", "value": 1}, `no value named "Ghost"`)
	c.ok("swap_markup", map[string]any{
		"source":   e2eGrown,
		"register": []any{map[string]any{"name": "Pct", "type": "int", "value": 40}},
	})
	if !tt.waitFor("registered live") {
		t.Fatalf("the grown page never reached the pty:\n%s", tt.text())
	}
	// Both registered properties are live sources on the real app.
	c.ok("set_value", map[string]any{"name": "Status", "value": "updated over MCP"})
	c.ok("set_value", map[string]any{"name": "Pct", "value": 80})
	if got := c.ok("screen_text", nil); !strings.Contains(got, "updated over MCP") {
		t.Fatalf("registered properties are not live sources:\n%s", got)
	}

	if app.DecoderLeaked() {
		t.Error("the input decoder leaked")
	}
}

// ---- a minimal pty ----

// The root package has one of these for its own App tests, unexported.
// This is the cut-down copy: no suspend, no resize, so no keeper handle
// and no open counting — just a terminal for a real app to paint into.
type ptyPair struct {
	master *os.File
	name   string

	mu  sync.Mutex
	buf strings.Builder
	scr *render.Screen
}

func newPTY(t *testing.T, cols, rows int) *ptyPair {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	var unlock int32
	if err := ptyIoctl(m, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		m.Close()
		t.Skipf("TIOCSPTLCK: %v", err)
	}
	var n int32
	if err := ptyIoctl(m, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		m.Close()
		t.Skipf("TIOCGPTN: %v", err)
	}
	p := &ptyPair{master: m, name: "/dev/pts/" + itoa(int(n)), scr: render.NewScreen(cols, rows)}

	ws := struct{ rows, cols, xpix, ypix uint16 }{rows: uint16(rows), cols: uint16(cols)}
	if err := ptyIoctl(m, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); err != nil {
		m.Close()
		t.Skipf("TIOCSWINSZ: %v", err)
	}
	// Drain continuously: a full master buffer would block the app's flush.
	go func() {
		b := make([]byte, 4096)
		for {
			k, err := m.Read(b)
			if k > 0 {
				p.mu.Lock()
				p.buf.Write(b[:k])
				p.scr.Write(b[:k])
				p.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { m.Close() })
	return p
}

func (p *ptyPair) open() (*term.Screen, error) {
	f, err := os.OpenFile(p.name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	return term.FromFile(f), nil
}

func (p *ptyPair) text() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}

// screen is what a terminal fed these bytes would be showing. The flush
// is incremental, so the wire holds differences and only the model holds
// the screen.
func (p *ptyPair) screen() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scr.Text()
}

// waitFor polls the modelled screen. Frames are asynchronous by
// construction — the run loop decides when to paint — so the test waits
// for the screen to say something rather than for a duration.
func (p *ptyPair) waitFor(want string) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.screen(), want) {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func ptyIoctl(f *os.File, req, arg uintptr) error {
	c, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if cerr := c.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
	}); cerr != nil {
		return cerr
	}
	if errno != 0 {
		return errno
	}
	return nil
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
