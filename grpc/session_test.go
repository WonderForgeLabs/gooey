package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/WonderForgeLabs/gooey/input"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// Attach tests: the bidi session against the same hand-run loop, over
// the real wire. The recv helpers make stream assertions read as what
// they are: "the next thing this session hears is …".

type attached struct {
	t      *testing.T
	stream grpcgo.BidiStreamingClient[controlv1.AttachRequest, controlv1.AttachResponse]
	cancel context.CancelFunc
}

func attach(t *testing.T, h *harness, sub *controlv1.Subscription) *attached {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	stream, err := h.sess.Attach(ctx)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := stream.Send(&controlv1.AttachRequest{
		Msg: &controlv1.AttachRequest_Subscribe{Subscribe: sub},
	}); err != nil {
		t.Fatalf("send subscribe: %v", err)
	}
	return &attached{t: t, stream: stream, cancel: cancel}
}

func (a *attached) act(id uint64, act *controlv1.Act) {
	a.t.Helper()
	act.Id = id
	if err := a.stream.Send(&controlv1.AttachRequest{Msg: &controlv1.AttachRequest_Act{Act: act}}); err != nil {
		a.t.Fatalf("send act %d: %v", id, err)
	}
}

// recv returns the next server message, failing the test on error.
func (a *attached) recv() *controlv1.AttachResponse {
	a.t.Helper()
	m, err := a.stream.Recv()
	if err != nil {
		a.t.Fatalf("Recv: %v", err)
	}
	return m
}

func (a *attached) welcome() *controlv1.Welcome {
	a.t.Helper()
	w := a.recv().GetWelcome()
	if w == nil {
		a.t.Fatal("the first server message was not welcome")
	}
	return w
}

func TestAttachWelcomeAndActResults(t *testing.T) {
	h := newHarness(t)
	a := attach(t, h, &controlv1.Subscription{}) // a write-only session
	w := a.welcome()
	if w.AppName != "gooey-test" || w.Columns != 60 || w.Rows != 14 {
		t.Errorf("welcome = %v", w)
	}

	a.act(1, &controlv1.Act{Act: &controlv1.Act_InvokeCommand{
		InvokeCommand: &controlv1.InvokeCommandRequest{Name: "Increment"},
	}})
	res := a.recv().GetResult()
	if res == nil || res.Id != 1 || res.Code != 0 || res.GetInvokeCommand() == nil {
		t.Fatalf("act result = %v", res)
	}
	// The settle barrier holds on the act path too: the unary read
	// right after the in-stream ack sees the repaint.
	if s := h.screen(); !strings.Contains(s, "count is 1") {
		t.Fatalf("the act's frame is not on screen:\n%s", s)
	}

	// A failing act answers in-stream and does not end the session.
	a.act(2, &controlv1.Act{Act: &controlv1.Act_InvokeCommand{
		InvokeCommand: &controlv1.InvokeCommandRequest{Name: "Nope"},
	}})
	res = a.recv().GetResult()
	if res.Id != 2 || codes.Code(res.Code) != codes.NotFound {
		t.Fatalf("failed act result = %v", res)
	}
	a.act(3, &controlv1.Act{Act: &controlv1.Act_SetProperty{
		SetProperty: &controlv1.SetPropertyRequest{Name: "Note", Value: strVal("still here")},
	}})
	if res = a.recv().GetResult(); res.Id != 3 || res.Code != 0 {
		t.Fatalf("the session did not survive the failed act: %v", res)
	}
}

func TestAttachFrameDeltaPrecedesItsActResult(t *testing.T) {
	h := newHarness(t)
	a := attach(t, h, &controlv1.Subscription{Properties: true})
	a.welcome()

	a.act(1, &controlv1.Act{Act: &controlv1.Act_InvokeCommand{
		InvokeCommand: &controlv1.InvokeCommandRequest{Name: "Increment"},
	}})

	// The frame the act caused arrives BEFORE the act's result: the
	// delta is enqueued during the settle barrier, the ack after it.
	first, second := a.recv(), a.recv()
	frame := first.GetFrame()
	if frame == nil {
		t.Fatalf("expected the FrameDelta first, got %v", first)
	}
	if second.GetResult() == nil || second.GetResult().Id != 1 {
		t.Fatalf("expected the act result second, got %v", second)
	}
	var count *controlv1.PropertyChange
	for _, ch := range frame.Changes {
		if ch.Name == "Count" {
			count = ch
		}
	}
	if count == nil || count.Value.GetStringValue() != "1" {
		t.Fatalf("the delta does not carry the settled Count: %v", frame.Changes)
	}
	if frame.Repainted == 0 || len(frame.Damage) == 0 {
		t.Errorf("the delta carries no damage: repainted=%d rects=%d", frame.Repainted, len(frame.Damage))
	}
	if frame.Frame == 0 {
		t.Error("the delta carries no frame sequence number")
	}
}

func TestAttachPropertyFilter(t *testing.T) {
	h := newHarness(t)
	a := attach(t, h, &controlv1.Subscription{Properties: true, Names: []string{"Note"}})
	a.welcome()

	// Changes Count (filtered out) AND Note (subscribed) in one frame.
	a.act(1, &controlv1.Act{Act: &controlv1.Act_InvokeCommand{
		InvokeCommand: &controlv1.InvokeCommandRequest{Name: "Increment"},
	}})
	a.act(2, &controlv1.Act{Act: &controlv1.Act_SetProperty{
		SetProperty: &controlv1.SetPropertyRequest{Name: "Note", Value: strVal("filtered in")},
	}})

	var sawNote bool
	deadline := time.After(3 * time.Second)
	for done := 0; done < 2; {
		type recvd struct{ m *controlv1.AttachResponse }
		ch := make(chan recvd, 1)
		go func() { m, _ := a.stream.Recv(); ch <- recvd{m} }()
		select {
		case r := <-ch:
			if r.m == nil {
				t.Fatal("stream ended early")
			}
			if f := r.m.GetFrame(); f != nil {
				for _, c := range f.Changes {
					if c.Name == "Count" {
						t.Fatalf("the filter leaked %v", c)
					}
					if c.Name == "Note" {
						sawNote = true
					}
				}
			}
			if r.m.GetResult() != nil {
				done++
			}
		case <-deadline:
			t.Fatal("timed out waiting for act results")
		}
	}
	if !sawNote {
		t.Error("the subscribed property's change never arrived")
	}
}

func TestAttachSwapLifecycle(t *testing.T) {
	h := newHarness(t)
	a := attach(t, h, &controlv1.Subscription{Lifecycle: true})
	a.welcome()

	a.act(1, &controlv1.Act{Act: &controlv1.Act_SwapMarkup{
		SwapMarkup: &controlv1.SwapMarkupRequest{
			Source: `<Gooey><Text Name="Banner">new page {{.Count}}</Text></Gooey>`,
		},
	}})

	// Swapped (raised during the act, before its settle barrier
	// completes) then the act's own result, in stream order.
	first, second := a.recv(), a.recv()
	sw := first.GetLifecycle().GetSwapped()
	if sw == nil {
		t.Fatalf("expected the Swapped lifecycle event first, got %v", first)
	}
	if len(sw.Named) != 1 || sw.Named[0] != "Banner" {
		t.Errorf("swapped named = %v", sw.Named)
	}
	res := second.GetResult()
	if res == nil || res.Id != 1 || res.Code != 0 || res.GetSwapMarkup() == nil {
		t.Fatalf("expected the act result second, got %v", second)
	}
}

func TestAttachInputEcho(t *testing.T) {
	h := newHarness(t)
	a := attach(t, h, &controlv1.Subscription{Input: true})
	a.welcome()

	// Remote input echoes: a SendKeys act reports its own keystrokes.
	a.act(1, &controlv1.Act{Act: &controlv1.Act_SendKeys{
		SendKeys: &controlv1.SendKeysRequest{Gestures: []string{"tab"}},
	}})
	first, second := a.recv(), a.recv()
	echo := first.GetInput()
	if echo == nil {
		t.Fatalf("expected the input echo first, got %v", first)
	}
	if g := echo.Event.GetKey().GetGesture(); g != "tab" {
		t.Errorf("echoed gesture = %q", g)
	}
	if second.GetResult() == nil {
		t.Fatalf("expected the act result second, got %v", second)
	}

	// Terminal input echoes through the same channel.
	h.onUI(func() { h.app.fireEvent(input.KeyOf(input.Rune('x')), true) })
	m := a.recv()
	if in := m.GetInput(); in == nil || in.Event.GetKey().GetGesture() != "x" || !in.Consumed {
		t.Fatalf("terminal echo = %v", m)
	}
}

func TestAttachFirstMessageMustSubscribe(t *testing.T) {
	h := newHarness(t)
	stream, err := h.sess.Attach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&controlv1.AttachRequest{Msg: &controlv1.AttachRequest_Act{Act: &controlv1.Act{Id: 1}}}); err != nil {
		t.Fatal(err)
	}
	_, err = stream.Recv()
	wantCode(t, err, codes.InvalidArgument, "subscribe")
}

func TestAttachSecondSubscribeEndsTheSession(t *testing.T) {
	h := newHarness(t)
	a := attach(t, h, &controlv1.Subscription{})
	a.welcome()
	if err := a.stream.Send(&controlv1.AttachRequest{
		Msg: &controlv1.AttachRequest_Subscribe{Subscribe: &controlv1.Subscription{}},
	}); err != nil {
		t.Fatal(err)
	}
	var err error
	for err == nil {
		_, err = a.stream.Recv()
	}
	wantCode(t, err, codes.InvalidArgument, "once")
}

func TestAttachClosingOnAppQuit(t *testing.T) {
	vm, values := newVM()
	app := newTestApp(t, testMarkup, values, nil)
	h := attachHarness(t, app, vm, Options{})
	a := attach(t, h, &controlv1.Subscription{Lifecycle: true})
	a.welcome()

	close(app.quit) // the app quits out from under the session
	got := a.recv()
	if got.GetLifecycle().GetClosing() == nil {
		t.Fatalf("expected Closing, got %v", got)
	}
	if _, err := a.stream.Recv(); err == nil {
		t.Error("the stream did not end after Closing")
	}
	// The harness cleanup will close app.quit again — swap in a fresh
	// channel, after the loop has provably exited so nothing reads the
	// field concurrently.
	<-app.done
	app.quit = make(chan struct{})
}

func TestAttachResizeLifecycle(t *testing.T) {
	h := newHarness(t)
	a := attach(t, h, &controlv1.Subscription{Lifecycle: true})
	a.welcome()

	h.onUI(func() {
		h.app.cols, h.app.rows = 80, 20
		h.app.comp.Resize(80, 20)
		h.app.needsFrame = true
	})
	// The resize composes a frame; the frame emits Resized.
	deadline := time.After(3 * time.Second)
	for {
		type recvd struct {
			m   *controlv1.AttachResponse
			err error
		}
		ch := make(chan recvd, 1)
		go func() { m, err := a.stream.Recv(); ch <- recvd{m, err} }()
		select {
		case r := <-ch:
			if r.err != nil {
				t.Fatalf("Recv: %v", r.err)
			}
			if rs := r.m.GetLifecycle().GetResized(); rs != nil {
				if rs.Columns != 80 || rs.Rows != 20 {
					t.Errorf("resized = %v", rs)
				}
				return
			}
		case <-deadline:
			t.Fatal("the Resized lifecycle event never arrived")
		}
	}
}

func TestAttachWithoutSubscriptionsHearsNothing(t *testing.T) {
	h := newHarness(t)
	quiet := attach(t, h, &controlv1.Subscription{})
	quiet.welcome()

	// Cause frames, input and a swap.
	if _, err := h.ctl.InvokeCommand(context.Background(), &controlv1.InvokeCommandRequest{Name: "Increment"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ctl.SendKeys(context.Background(), &controlv1.SendKeysRequest{Gestures: []string{"tab"}}); err != nil {
		t.Fatal(err)
	}

	// The quiet session hears none of it: a recv with a short deadline
	// times out rather than yielding a push.
	got := make(chan *controlv1.AttachResponse, 1)
	go func() {
		m, _ := quiet.stream.Recv()
		got <- m
	}()
	select {
	case m := <-got:
		t.Fatalf("a write-only session was pushed %v", m)
	case <-time.After(300 * time.Millisecond):
	}
}
