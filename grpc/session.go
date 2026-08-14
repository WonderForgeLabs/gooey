package grpc

import (
	"io"
	"sync"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/control"
	"github.com/WonderForgeLabs/gooey/input"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

// SessionService.Attach: one bidi stream per client.
//
// The client side is ONE ordered stream of acts — the remote mirror of
// the terminal's one ordered input stream. A dedicated reader goroutine
// applies them strictly in arrival order, each through the same bridge
// (UI goroutine + settle barrier) the unary surface uses, and answers
// each with one id-correlated ActResult, in-stream so a failed act does
// not tear down the session.
//
// The server side is framed on composed frames. The broadcaster hangs
// off App.AfterFrame: when a frame has been composed and flushed, it —
// still on the UI goroutine — reads the subscribed properties (plain
// Gets outside any evaluation: reads, not subscriptions, per the
// call-site rule), diffs them against what each session last saw, pairs
// the changes with the frame's damage rects and repaint count, and
// enqueues ONE FrameDelta per session. Everything one frame changed
// travels in one message, so the torn read issue #49 guards against is
// unrepresentable. Collection never Sets anything and never composes,
// so the app's own damage counts are exactly what they would be with no
// session attached.
//
// Ordering: the FrameDelta for an act's frame is enqueued DURING the
// act's settle barrier, so it lands in the session's queue before the
// act's own ActResult — a client always sees the frame its act caused,
// then the acknowledgement.
//
// Backpressure: pushes must never block the UI goroutine. Each session
// has a bounded queue; a client that stops reading loses its session
// (RESOURCE_EXHAUSTED) rather than stalling the app or silently losing
// a delta from the middle of the sequence.

const sessionQueue = 256

type sessionServer struct {
	controlv1.UnimplementedSessionServiceServer
	s *Server
}

type session struct {
	sub    *controlv1.Subscription
	filter map[string]bool // property-name filter; nil means all

	// last is what this session has been told, per property name.
	// UI-goroutine-only: written at registration and at frame time.
	last map[string]control.Value

	out      chan *controlv1.AttachResponse
	lost     chan struct{}
	lostOnce sync.Once
}

// push enqueues a server-initiated message without ever blocking: the
// caller is the UI goroutine, and a full queue means the client stopped
// reading — the session is lost, not the frame.
func (ss *session) push(m *controlv1.AttachResponse) {
	select {
	case ss.out <- m:
	default:
		ss.lose()
	}
}

func (ss *session) lose() { ss.lostOnce.Do(func() { close(ss.lost) }) }

func (s *sessionServer) Attach(stream grpcgo.BidiStreamingServer[controlv1.AttachRequest, controlv1.AttachResponse]) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	sub := first.GetSubscribe()
	if sub == nil {
		return status.Error(codes.InvalidArgument, "the first message on a session must be subscribe")
	}
	sess := &session{
		sub:  sub,
		out:  make(chan *controlv1.AttachResponse, sessionQueue),
		lost: make(chan struct{}),
	}
	if len(sub.GetNames()) > 0 {
		sess.filter = make(map[string]bool, len(sub.GetNames()))
		for _, n := range sub.GetNames() {
			sess.filter[n] = true
		}
	}

	// Register on the UI goroutine: the property baseline is read there,
	// and the welcome's screen size and frame seq are UI state.
	var welcome *controlv1.Welcome
	if err := s.s.ui.Do(func() error {
		welcome = s.register(sess)
		return nil
	}); err != nil {
		return statusOf(err)
	}
	defer s.unregister(sess)

	if err := stream.Send(&controlv1.AttachResponse{
		Msg: &controlv1.AttachResponse_Welcome{Welcome: welcome},
	}); err != nil {
		return err
	}

	// The reader: acts, in stream order, each answered in-stream.
	readErr := make(chan error, 1)
	go func() { readErr <- s.readActs(stream, sess) }()

	var done <-chan struct{} // nil (never ready) without a lifetime host
	if s.s.bc != nil {
		done = s.s.bc.done
	}
	for {
		select {
		case m := <-sess.out:
			if err := stream.Send(m); err != nil {
				return err
			}
		case <-sess.lost:
			return status.Error(codes.ResourceExhausted,
				"the session fell behind: its event queue overflowed, and a gap in the delta sequence must not be silent")
		case err := <-readErr:
			if err == io.EOF {
				return nil // client finished sending and the session's job is done
			}
			return err
		case <-done:
			// The app is shutting down: say so, then end the stream.
			_ = stream.Send(&controlv1.AttachResponse{
				Msg: &controlv1.AttachResponse_Lifecycle{Lifecycle: &controlv1.LifecycleEvent{
					Event: &controlv1.LifecycleEvent_Closing{Closing: &controlv1.Closing{}},
				}},
			})
			return nil
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// register runs ON THE UI GOROUTINE: capture the property baseline (so
// the first FrameDelta carries changes, not the world), read the screen
// geometry, and join the broadcast set.
func (s *sessionServer) register(sess *session) *controlv1.Welcome {
	w := &controlv1.Welcome{
		AppName:    s.s.opts.Name,
		AppVersion: s.s.opts.Version,
	}
	if c := s.s.host.Composer(); c != nil {
		cols, rows := c.Size()
		w.Columns, w.Rows = int32(cols), int32(rows)
	}
	if s.s.bc != nil {
		w.Frame = s.s.bc.frame
		sess.last = s.s.bc.baseline(sess.filter)
		s.s.bc.add(sess)
	}
	return w
}

func (s *sessionServer) unregister(sess *session) {
	if s.s.bc != nil {
		s.s.bc.remove(sess)
	}
}

func (s *sessionServer) readActs(stream grpcgo.BidiStreamingServer[controlv1.AttachRequest, controlv1.AttachResponse], sess *session) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return err
		}
		if req.GetSubscribe() != nil {
			return status.Error(codes.InvalidArgument, "a session subscribes once; the first message was it")
		}
		act := req.GetAct()
		if act == nil {
			return status.Error(codes.InvalidArgument, "every message after subscribe must be an act")
		}
		res := s.apply(act)
		// Blocking send: act results are flow-controlled by the client's
		// own request rate, and unlike a frame push this goroutine is
		// allowed to wait for the writer.
		select {
		case sess.out <- &controlv1.AttachResponse{Msg: &controlv1.AttachResponse_Result{Result: res}}:
		case <-sess.lost:
			return nil
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// apply runs one act through the same service methods the unary surface
// uses — same bridge, same settle barrier, same codes — and packages
// the outcome as an in-stream ActResult.
func (s *sessionServer) apply(act *controlv1.Act) *controlv1.ActResult {
	res := &controlv1.ActResult{Id: act.GetId()}
	fail := func(err error) *controlv1.ActResult {
		st := status.Convert(statusOf(err))
		res.Code = uint32(st.Code())
		res.Message = st.Message()
		return res
	}
	svc, ui := s.s.svc, s.s.ui
	switch a := act.Act.(type) {
	case *controlv1.Act_SetProperty:
		v, err := valueFromProto(a.SetProperty.GetValue())
		if err != nil {
			return fail(err)
		}
		if err := ui.Do(func() error { return svc.Set(a.SetProperty.GetName(), v) }); err != nil {
			return fail(err)
		}
		res.Result = &controlv1.ActResult_SetProperty{SetProperty: &controlv1.SetPropertyResponse{}}
	case *controlv1.Act_InvokeCommand:
		if err := ui.Do(func() error { return svc.Invoke(a.InvokeCommand.GetName()) }); err != nil {
			return fail(err)
		}
		res.Result = &controlv1.ActResult_InvokeCommand{InvokeCommand: &controlv1.InvokeCommandResponse{}}
	case *controlv1.Act_SendKeys:
		var consumed []bool
		if err := ui.Do(func() (err error) {
			consumed, err = svc.SendKeys(a.SendKeys.GetText(), a.SendKeys.GetGestures())
			return
		}); err != nil {
			return fail(err)
		}
		res.Result = &controlv1.ActResult_SendKeys{SendKeys: &controlv1.SendKeysResponse{
			Sent: int32(len(consumed)), Consumed: consumed,
		}}
	case *controlv1.Act_SendPointer:
		p, err := pointerFromProto(a.SendPointer.GetEvent())
		if err != nil {
			return fail(err)
		}
		var consumed bool
		if err := ui.Do(func() (err error) {
			consumed, err = svc.SendPointer(p)
			return
		}); err != nil {
			return fail(err)
		}
		res.Result = &controlv1.ActResult_SendPointer{SendPointer: &controlv1.SendPointerResponse{Consumed: consumed}}
	case *controlv1.Act_SetFocus:
		if err := ui.Do(func() error { return svc.Focus(a.SetFocus.GetName()) }); err != nil {
			return fail(err)
		}
		res.Result = &controlv1.ActResult_SetFocus{SetFocus: &controlv1.SetFocusResponse{}}
	case *controlv1.Act_SwapMarkup:
		regs, err := registrationsFromProto(a.SwapMarkup.GetRegister())
		if err != nil {
			return fail(err)
		}
		var named []string
		if err := ui.Do(func() (err error) {
			named, err = svc.SwapMarkup(a.SwapMarkup.GetSource(), regs)
			return
		}); err != nil {
			return fail(err)
		}
		res.Result = &controlv1.ActResult_SwapMarkup{SwapMarkup: &controlv1.SwapMarkupResponse{Named: named}}
	case *controlv1.Act_RegisterProperties:
		regs, err := registrationsFromProto(a.RegisterProperties.GetProperties())
		if err != nil {
			return fail(err)
		}
		if err := ui.Do(func() error { return svc.Register(regs) }); err != nil {
			return fail(err)
		}
		res.Result = &controlv1.ActResult_RegisterProperties{RegisterProperties: &controlv1.RegisterPropertiesResponse{}}
	case *controlv1.Act_UnregisterNames:
		if err := ui.Do(func() error { return svc.Unregister(a.UnregisterNames.GetNames()) }); err != nil {
			return fail(err)
		}
		res.Result = &controlv1.ActResult_UnregisterNames{UnregisterNames: &controlv1.UnregisterNamesResponse{}}
	case *controlv1.Act_PatchMarkup:
		// The reason this is an act and not only a unary call: an editing
		// client sets a property and then patches the subtree that reads
		// it, and on this stream those two cannot race — acts are applied
		// in stream order on the UI goroutine. Split across transports
		// they can, because nothing orders a unary call against queued
		// acts.
		var named []string
		if err := ui.Do(func() (err error) {
			named, err = svc.PatchMarkup(a.PatchMarkup.GetName(), a.PatchMarkup.GetSource())
			return
		}); err != nil {
			return fail(err)
		}
		res.Result = &controlv1.ActResult_PatchMarkup{PatchMarkup: &controlv1.PatchMarkupResponse{Named: named}}
	default:
		res.Code = uint32(codes.InvalidArgument)
		res.Message = "the act carries no request"
	}
	return res
}

// ---- the broadcaster ----

// broadcaster is the frame-time collector shared by every session. Its
// hooks all run on the UI goroutine; only the session set itself is
// touched from stream goroutines, under the mutex. Property reads at
// frame time are plain Gets outside any evaluation — the call-site rule
// makes them reads, not subscriptions, which is what keeps the control
// plane out of the app's damage graph.
type broadcaster struct {
	svc  *control.Service
	host SessionHost
	done <-chan struct{}

	mu       sync.Mutex
	sessions map[*session]bool

	// UI-goroutine-only state.
	frame      uint64
	cols, rows int
	sized      bool
}

func newBroadcaster(svc *control.Service, host SessionHost) *broadcaster {
	return &broadcaster{
		svc:      svc,
		host:     host,
		done:     host.Done(),
		sessions: map[*session]bool{},
	}
}

func (b *broadcaster) add(ss *session) {
	b.mu.Lock()
	b.sessions[ss] = true
	b.mu.Unlock()
}

func (b *broadcaster) remove(ss *session) {
	b.mu.Lock()
	delete(b.sessions, ss)
	b.mu.Unlock()
}

func (b *broadcaster) snapshot() []*session {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*session, 0, len(b.sessions))
	for ss := range b.sessions {
		out = append(out, ss)
	}
	return out
}

// prime seeds the size baseline so the first observed frame is not
// mistaken for a resize (and a real resize before any other frame is
// not swallowed). UI goroutine only; a composer that does not exist yet
// leaves priming to the first frame.
func (b *broadcaster) prime() {
	if c := b.host.Composer(); c != nil {
		b.cols, b.rows = c.Size()
		b.sized = true
	}
}

// baseline reads the current value of every property a session's filter
// admits — the "already seen" state a new session diffs its first frame
// against. UI goroutine only.
func (b *broadcaster) baseline(filter map[string]bool) map[string]control.Value {
	out := map[string]control.Value{}
	entries, _, err := b.svc.Values()
	if err != nil {
		return out // no context: there are no properties to baseline
	}
	for _, e := range entries {
		if e.Value == nil || (filter != nil && !filter[e.Name]) {
			continue
		}
		out[e.Name] = *e.Value
	}
	return out
}

// afterFrame is the App.AfterFrame hook: called on the UI goroutine
// right after a frame was composed and flushed. It reads, diffs and
// enqueues; it never Sets, so it can never schedule a frame of its own.
func (b *broadcaster) afterFrame() {
	b.frame++
	sessions := b.snapshot()

	comp := b.host.Composer()
	var damage []*controlv1.Rect
	if comp != nil {
		// Scoped endpoints see their island's damage, not the app's.
		for _, r := range b.svc.VisibleDamage(comp.Damage()) {
			damage = append(damage, rectToProto(r))
		}
		// A size change always composes a frame, so detecting it here —
		// rather than through a hook the App does not have — keeps
		// Resized ordered against the FrameDelta of the resize frame.
		cols, rows := comp.Size()
		if b.sized && (cols != b.cols || rows != b.rows) {
			b.emitLifecycle(sessions, &controlv1.LifecycleEvent{
				Event: &controlv1.LifecycleEvent_Resized{Resized: &controlv1.Resized{
					Columns: int32(cols), Rows: int32(rows),
				}},
			})
		}
		b.cols, b.rows, b.sized = cols, rows, true
	}
	if len(sessions) == 0 {
		return
	}

	// One read of the bindable surface serves every session; each
	// session then diffs against what it last saw through its own
	// filter. Values() errors only without a context, in which case
	// property deltas simply do not exist.
	var entries []control.ValueEntry
	if anySubscribes(sessions) {
		entries, _, _ = b.svc.Values()
	}

	for _, ss := range sessions {
		var changes []*controlv1.PropertyChange
		if ss.sub.GetProperties() {
			for _, e := range entries {
				if e.Value == nil || (ss.filter != nil && !ss.filter[e.Name]) {
					continue
				}
				if prev, seen := ss.last[e.Name]; seen && prev.Equal(*e.Value) {
					continue
				}
				ss.last[e.Name] = *e.Value
				changes = append(changes, &controlv1.PropertyChange{
					Name:  e.Name,
					Value: valueToProto(*e.Value),
				})
			}
		}
		if len(changes) == 0 && !ss.sub.GetFrames() {
			continue
		}
		ss.push(&controlv1.AttachResponse{Msg: &controlv1.AttachResponse_Frame{Frame: &controlv1.FrameDelta{
			Frame:     b.frame,
			Changes:   changes,
			Damage:    damage,
			Repainted: int32(len(damage)),
		}}})
	}
}

func anySubscribes(sessions []*session) bool {
	for _, ss := range sessions {
		if ss.sub.GetProperties() {
			return true
		}
	}
	return false
}

// afterSwap is the App.OnSwap hook: the page was replaced — by a
// client's SwapMarkup, by hot reload, by the app itself — and every
// lifecycle subscriber hears it with the new name table, on the same
// ordered stream as the frames.
func (b *broadcaster) afterSwap(gooey.Component) {
	// The name table comes from the SERVICE, not from the context
	// directly, so a scoped endpoint reports the names inside its island
	// rather than handing a guest the host's whole address book. It is
	// the same sorted list for an unscoped server.
	var named []string
	if _, n, err := b.svc.Values(); err == nil {
		named = n
	}
	b.emitLifecycle(b.snapshot(), &controlv1.LifecycleEvent{
		Event: &controlv1.LifecycleEvent_Swapped{Swapped: &controlv1.Swapped{Named: named}},
	})
}

func (b *broadcaster) emitLifecycle(sessions []*session, ev *controlv1.LifecycleEvent) {
	for _, ss := range sessions {
		if !ss.sub.GetLifecycle() {
			continue
		}
		ss.push(&controlv1.AttachResponse{Msg: &controlv1.AttachResponse_Lifecycle{Lifecycle: ev}})
	}
}

// afterEvent is the App.AfterEvent hook: terminal input, echoed to
// input subscribers as consumed.
//
// A SCOPED endpoint does not echo it. Terminal input is the HOST's
// keystrokes — everything the user types, anywhere on the page,
// including into their own fields — and an island grant is not a grant
// to watch the user type. A guest still gets the echo of its OWN
// injections (echoRemote), which is what the echo is for on that side:
// confirmation that what it sent was dispatched, and whether the tree
// took it.
func (b *broadcaster) afterEvent(ev input.Event, consumed bool) {
	if b.svc.Grant() != nil {
		return
	}
	b.echo(ev, consumed)
}

// echoRemote is the service's injection hook: input a client sent
// (SendKeys, SendPointer) echoes exactly as terminal input does —
// one stream, terminal and remote interleaved as dispatched.
func (b *broadcaster) echoRemote(e control.EchoEvent) {
	b.echo(e.Event, e.Consumed)
}

func (b *broadcaster) echo(ev input.Event, consumed bool) {
	pe := inputEventToProto(ev)
	if pe == nil {
		return
	}
	for _, ss := range b.snapshot() {
		if !ss.sub.GetInput() {
			continue
		}
		ss.push(&controlv1.AttachResponse{Msg: &controlv1.AttachResponse_Input{Input: &controlv1.InputEcho{
			Event:    pe,
			Consumed: consumed,
		}}})
	}
}
