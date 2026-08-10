package grpc

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
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// The end-to-end proof: a REAL gooey.App, on a REAL pty, served by the
// real gRPC server, driven by the generated Go client over TCP. Nothing
// is a stand-in — the frames the assertions read are the escape
// sequences a terminal would have received, and every call crosses the
// wire. The unit tests hand-run the loop to see the goroutine boundary;
// this one uses App.Run precisely because it cannot, which is what
// makes it a different kind of evidence: the confinement is correct AND
// *gooey.App satisfies the Host and SessionHost contracts.

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
	if !tt.waitFor("count is 0") {
		t.Fatalf("the app never painted:\n%s", tt.text())
	}

	srv, err := Serve(app, Options{Context: bind, Name: "gooey-e2e", Version: "1"})
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	defer srv.Close()
	conn, err := grpcgo.NewClient(srv.Addr(), grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer conn.Close()
	ctl := controlv1.NewControlServiceClient(conn)
	bg := context.Background()

	// --- read: the tree carries its Name= identities ---
	tree, err := ctl.SnapshotTree(bg, &controlv1.SnapshotTreeRequest{})
	if err != nil {
		t.Fatalf("SnapshotTree: %v", err)
	}
	names := map[string]bool{}
	var walk func(*controlv1.TreeNode)
	walk = func(n *controlv1.TreeNode) {
		if n.Name != "" {
			names[n.Name] = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree.Root)
	if !names["Head"] || !names["Inc"] || !names["Note"] {
		t.Fatalf("named elements missing: %v", names)
	}

	// --- the settle barrier, on the wire: a screen read immediately
	// after a command sees the new pixels, no sleeps anywhere ---
	if _, err := ctl.InvokeCommand(bg, &controlv1.InvokeCommandRequest{Name: "Increment"}); err != nil {
		t.Fatalf("InvokeCommand: %v", err)
	}
	screen, err := ctl.ScreenText(bg, &controlv1.ScreenTextRequest{})
	if err != nil {
		t.Fatalf("ScreenText: %v", err)
	}
	if !strings.Contains(screen.Text, "count is 1") {
		t.Fatalf("the settled screen does not show the invoke:\n%s", screen.Text)
	}
	// And the same frame reached the actual terminal.
	if !tt.waitFor("count is 1") {
		t.Fatalf("the frame never reached the pty:\n%s", tt.text())
	}

	// --- SetProperty repaints before answering ---
	if _, err := ctl.SetProperty(bg, &controlv1.SetPropertyRequest{
		Name: "Note", Value: strVal("set over gRPC"),
	}); err != nil {
		t.Fatalf("SetProperty: %v", err)
	}
	if s, _ := ctl.ScreenText(bg, &controlv1.ScreenTextRequest{}); !strings.Contains(s.Text, "set over gRPC") {
		t.Fatalf("SetProperty did not repaint:\n%s", s.Text)
	}

	// --- the session: acts, frame deltas, damage counts, swap ---
	stream, err := controlv1.NewSessionServiceClient(conn).Attach(bg)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	send := func(id uint64, act *controlv1.Act) {
		t.Helper()
		act.Id = id
		if err := stream.Send(&controlv1.AttachRequest{Msg: &controlv1.AttachRequest_Act{Act: act}}); err != nil {
			t.Fatalf("send act %d: %v", id, err)
		}
	}
	recv := func() *controlv1.AttachResponse {
		t.Helper()
		m, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		return m
	}
	if err := stream.Send(&controlv1.AttachRequest{Msg: &controlv1.AttachRequest_Subscribe{
		Subscribe: &controlv1.Subscription{Properties: true, Frames: true, Lifecycle: true},
	}}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	welcome := recv().GetWelcome()
	if welcome == nil || welcome.AppName != "gooey-e2e" || welcome.Columns != 60 || welcome.Rows != 12 {
		t.Fatalf("welcome = %v", welcome)
	}

	// Focus the button, then the textbox. The second move's FrameDelta
	// must carry the framework's damage guarantee: a focus move repaints
	// EXACTLY the two components that changed, and the wire says so.
	send(1, &controlv1.Act{Act: &controlv1.Act_SetFocus{SetFocus: &controlv1.SetFocusRequest{Name: "Inc"}}})
	waitResult(t, recv, 1)
	send(2, &controlv1.Act{Act: &controlv1.Act_SetFocus{SetFocus: &controlv1.SetFocusRequest{Name: "Note"}}})
	var focusDelta *controlv1.FrameDelta
	for focusDelta == nil {
		m := recv()
		if f := m.GetFrame(); f != nil {
			focusDelta = f
		}
		if r := m.GetResult(); r != nil && r.Code != 0 {
			t.Fatalf("SetFocus failed: %v", r)
		}
	}
	if focusDelta.Repainted != 2 {
		t.Errorf("focus move repainted %d components (damage %v), want exactly 2",
			focusDelta.Repainted, focusDelta.Damage)
	}
	waitResult(t, recv, 2)

	// An invoke: its FrameDelta arrives before its ActResult and carries
	// the settled property.
	send(3, &controlv1.Act{Act: &controlv1.Act_InvokeCommand{
		InvokeCommand: &controlv1.InvokeCommandRequest{Name: "Increment"},
	}})
	m := recv()
	delta := m.GetFrame()
	if delta == nil {
		t.Fatalf("expected the act's FrameDelta first, got %v", m)
	}
	var sawCount bool
	for _, c := range delta.Changes {
		if c.Name == "Count" && c.Value.GetStringValue() == "2" {
			sawCount = true
		}
	}
	if !sawCount {
		t.Fatalf("the delta does not carry the settled Count: %v", delta.Changes)
	}
	if delta.Frame <= welcome.Frame {
		t.Errorf("frame %d is not beyond the welcome's %d", delta.Frame, welcome.Frame)
	}
	waitResult(t, recv, 3)

	// A swap through the session: Swapped lifecycle, then the result;
	// the new page renders over the surviving viewmodel, on the real pty.
	send(4, &controlv1.Act{Act: &controlv1.Act_SwapMarkup{
		SwapMarkup: &controlv1.SwapMarkupRequest{Source: e2eSwapped},
	}})
	var swapped *controlv1.Swapped
	for swapped == nil {
		m := recv()
		if sw := m.GetLifecycle().GetSwapped(); sw != nil {
			swapped = sw
		}
		if r := m.GetResult(); r != nil {
			if r.Code != 0 {
				t.Fatalf("SwapMarkup act failed: %v", r)
			}
			if swapped == nil {
				t.Fatal("the act result arrived before the Swapped lifecycle event")
			}
		}
	}
	if len(swapped.Named) != 2 {
		t.Errorf("swapped named = %v", swapped.Named)
	}
	if !tt.waitFor("swapped, count survived: 2") {
		t.Fatalf("the swapped page never reached the pty:\n%s", tt.text())
	}
	// The swapped-in tree is live and bound to the same viewmodel.
	if _, err := ctl.InvokeCommand(bg, &controlv1.InvokeCommandRequest{Name: "Increment"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := ctl.ScreenText(bg, &controlv1.ScreenTextRequest{}); !strings.Contains(s.Text, "survived: 3") {
		t.Fatalf("the swapped tree is not bound to the viewmodel:\n%s", s.Text)
	}

	if app.DecoderLeaked() {
		t.Error("the input decoder leaked")
	}
}

// waitResult reads until the id's ActResult arrives, tolerating
// interleaved frame pushes (the session subscribed to every frame).
func waitResult(t *testing.T, recv func() *controlv1.AttachResponse, id uint64) {
	t.Helper()
	for {
		m := recv()
		if r := m.GetResult(); r != nil {
			if r.Id != id || r.Code != 0 {
				t.Fatalf("act result = %v, want ok for id %d", r, id)
			}
			return
		}
	}
}

// ---- a minimal pty ----

// The same cut-down pty the MCP e2e uses (mcp/e2e_linux_test.go): no
// suspend, no resize — just a terminal for a real app to paint into,
// with a render.Screen modelling what a terminal fed those bytes shows.
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

// screen is what a terminal fed these bytes would be showing: the flush
// is incremental, so the wire holds differences and only the model
// holds the screen.
func (p *ptyPair) screen() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scr.Text()
}

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
