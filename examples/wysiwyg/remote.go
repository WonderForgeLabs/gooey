package main

import (
	"context"
	"fmt"
	"sync"

	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Remote is the editor's transport to a gooey app it is editing.
//
// # Two channels, and why
//
// The editor holds a `SessionService.Attach` stream AND a unary
// `ControlService` client against the same connection, because neither
// alone is sufficient:
//
//   - `Attach` is the live channel. It carries property writes and
//     delivers subscribed deltas, filtered to the names this editor
//     owns. Measured: a delta arrives at 1.73ms, BEFORE the ActResult
//     that acknowledges it.
//   - `PatchMarkup` is not an act. The `Act` oneof carries exactly
//     seven things — set_property, invoke_command, send_keys,
//     send_pointer, set_focus, swap_markup, register_properties — so
//     replacing one named subtree, which is the editor's whole job, has
//     to go through the unary surface.
//
// `swap_markup` IS an act and is not a substitute: it re-runs the
// page's parse-and-bind, so focus, caret and every Name= in the tree
// are reassigned. Name is the ADDRESS, so that invalidates every
// outstanding patch target at once.
//
// # The ordering discipline — REMOVABLE
//
// Acts on one stream are strictly ordered: the server's reader applies
// act N fully, through the settle barrier, before it reads act N+1. But
// a unary call posts from its own gRPC handler goroutine while acts post
// from the session's reader goroutine — two independent goroutines
// racing Dispatcher.Post, neither able to observe the other. **A unary
// PatchMarkup can land between any two acts.**
//
// So the editor serializes: every act is awaited to its ActResult before
// a dependent patch is issued. That is SELF-IMPOSED DISCIPLINE, NOT A
// GUARANTEE THE TRANSPORT OFFERS. If `patch_markup` is added to the
// `Act` oneof, this whole mechanism deletes — see the
// removableSerialization comment on Patch.
//
// It is cheap for a reason worth keeping: since a subscribed delta
// arrives before its own ActResult, waiting for the result costs nothing
// that was not already going to be waited for. The happens-before comes
// free.
type Remote struct {
	conn    *grpc.ClientConn
	control controlv1.ControlServiceClient
	stream  grpc.BidiStreamingClient[controlv1.AttachRequest, controlv1.AttachResponse]

	// AppName is fixed for the session. The SIZE is not: Welcome gives
	// the initial value and Resized updates it, so it lives behind the
	// mutex and is read through Size().
	AppName string

	mu         sync.Mutex
	nextID     uint64
	pending    map[uint64]chan *controlv1.ActResult
	cols, rows int

	// OnDelta is called on the reader goroutine for every subscribed
	// property change. It must not touch the UI directly — marshal
	// through the app's Dispatcher, the same rule every other async
	// source in gooey follows.
	OnDelta func(name string, value *controlv1.TypedValue)

	// OnResized reports a terminal resize. Called on the reader
	// goroutine.
	OnResized func(cols, rows int)

	// OnSwapped reports that the whole page was replaced, and carries
	// the tree's NEW name table. Treat it as total invalidation of every
	// address held — but note the names arrive with the event, so no
	// resync round trip is needed to recover them.
	OnSwapped func(named []string)

	// OnLost reports that the session ended. The only recovery is
	// reconnect-and-RESYNC, never resume: an overflowing session is
	// dropped whole (ResourceExhausted, "a gap in the delta sequence
	// must not be silent") rather than losing individual messages, so a
	// client can never patch up its mirror from what it did receive.
	OnLost func(error)

	done chan struct{}
	// stop ends the stream. The session's lifetime is Close(), not the
	// context that opened it.
	stop context.CancelFunc
}

// Connect attaches to an app and subscribes to the given property names.
//
// ctx bounds the CONNECT ONLY. The stream deliberately does not inherit
// it, because `Attach(ctx)` lives exactly as long as its context does —
// so a caller passing the natural "give up if unreachable" timeout would
// have every session silently die when it expired. That reads to a user
// as the editor freezing mid-session, and it is not a mistake the caller
// should have to know about. The session ends at Close() and nowhere
// else.
//
// The names filter is not an optimization. A Subscription is entirely
// opt-in — an all-defaults one is write-only, receiving act results and
// no deltas at all — and taking every property delta is the naive
// default and the wrong one for a client that owns one island.
func Connect(ctx context.Context, addr string, names []string) (*Remote, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	r := &Remote{
		conn:    conn,
		control: controlv1.NewControlServiceClient(conn),
		pending: map[uint64]chan *controlv1.ActResult{},
		done:    make(chan struct{}),
	}
	streamCtx, stop := context.WithCancel(context.Background())
	r.stop = stop
	r.stream, err = controlv1.NewSessionServiceClient(conn).Attach(streamCtx)
	if err != nil {
		stop()
		conn.Close()
		return nil, fmt.Errorf("attach: %w", err)
	}
	if err := r.stream.Send(&controlv1.AttachRequest{
		Msg: &controlv1.AttachRequest_Subscribe{Subscribe: &controlv1.Subscription{
			Properties: true,
			Names:      names,
			// Lifecycle is NOT optional for an editor, and leaving it
			// off fails silently — an all-defaults Subscription is
			// write-only, so the signals exist the whole time and simply
			// never arrive. Two of them are load-bearing here:
			//
			//   Resized — Welcome carries the size ONCE, at attach. A
			//   long-lived editor that caches it is wrong from the first
			//   resize onward.
			//
			//   Swapped — the page was replaced, by hot reload, by any
			//   client's SwapMarkup, or by the app itself. EVERY Name=
			//   is reassigned, so every patch address the editor holds
			//   is stale. Without this the next patch either fails with
			//   NotFound or, worse, succeeds against a name that now
			//   means something else.
			Lifecycle: true,
		}},
	}); err != nil {
		conn.Close()
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	// The welcome still respects the caller's connect deadline, even
	// though the stream does not: an unreachable or wedged target must
	// not hang Connect forever.
	type hello struct {
		msg *controlv1.AttachResponse
		err error
	}
	greet := make(chan hello, 1)
	go func() {
		m, err := r.stream.Recv()
		greet <- hello{m, err}
	}()
	var first *controlv1.AttachResponse
	select {
	case h := <-greet:
		if h.err != nil {
			stop()
			conn.Close()
			return nil, fmt.Errorf("welcome: %w", h.err)
		}
		first = h.msg
	case <-ctx.Done():
		stop()
		conn.Close()
		return nil, fmt.Errorf("welcome: %w", ctx.Err())
	}
	w := first.GetWelcome()
	if w == nil {
		stop()
		conn.Close()
		return nil, fmt.Errorf("first message was not a welcome")
	}
	r.AppName = w.GetAppName()
	r.cols, r.rows = int(w.GetColumns()), int(w.GetRows())
	go r.read()
	return r, nil
}

// read is the single reader goroutine. Every server message lands here,
// in stream order.
func (r *Remote) read() {
	defer close(r.done)
	for {
		m, err := r.stream.Recv()
		if err != nil {
			r.fail(err)
			return
		}
		switch {
		case m.GetResult() != nil:
			res := m.GetResult()
			r.mu.Lock()
			ch := r.pending[res.GetId()]
			delete(r.pending, res.GetId())
			r.mu.Unlock()
			if ch != nil {
				ch <- res
			}
		case m.GetFrame() != nil:
			if r.OnDelta != nil {
				for _, c := range m.GetFrame().GetChanges() {
					r.OnDelta(c.GetName(), c.GetValue())
				}
			}
		case m.GetLifecycle() != nil:
			ev := m.GetLifecycle()
			if got := ev.GetResized(); got != nil {
				c, rr := int(got.GetColumns()), int(got.GetRows())
				r.mu.Lock()
				r.cols, r.rows = c, rr
				r.mu.Unlock()
				if r.OnResized != nil {
					r.OnResized(c, rr)
				}
			}
			if got := ev.GetSwapped(); got != nil && r.OnSwapped != nil {
				r.OnSwapped(got.GetNamed())
			}
		}
	}
}

// fail releases every waiter so a lost session cannot wedge a caller,
// then reports it once.
func (r *Remote) fail(err error) {
	r.mu.Lock()
	pending := r.pending
	r.pending = map[uint64]chan *controlv1.ActResult{}
	r.mu.Unlock()
	for _, ch := range pending {
		close(ch)
	}
	if r.OnLost != nil {
		r.OnLost(err)
	}
}

// act sends one act and BLOCKS until its ActResult arrives.
//
// Blocking is the point, not a simplification: it is what gives the
// caller a happens-before against anything issued afterwards, including
// a unary call on the other channel.
func (r *Remote) act(ctx context.Context, a *controlv1.Act) (*controlv1.ActResult, error) {
	ch := make(chan *controlv1.ActResult, 1)
	r.mu.Lock()
	r.nextID++
	a.Id = r.nextID
	r.pending[a.Id] = ch
	r.mu.Unlock()

	if err := r.stream.Send(&controlv1.AttachRequest{
		Msg: &controlv1.AttachRequest_Act{Act: a},
	}); err != nil {
		return nil, fmt.Errorf("send act: %w", err)
	}
	select {
	case res, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("session lost before act %d was answered", a.Id)
		}
		if res.GetCode() != 0 {
			return res, fmt.Errorf("act %d: %s", a.Id, res.GetMessage())
		}
		return res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// SetProperty writes one property over the stream and waits for it to be
// applied.
func (r *Remote) SetProperty(ctx context.Context, name string, v *controlv1.TypedValue) error {
	_, err := r.act(ctx, &controlv1.Act{
		Act: &controlv1.Act_SetProperty{SetProperty: &controlv1.SetPropertyRequest{
			Name: name, Value: v,
		}},
	})
	return err
}

// SetPropertyAsync writes a property WITHOUT waiting for it to be
// applied.
//
// This is what an editor wants while someone is typing: a round trip per
// keystroke would put the transport in the interaction loop. Acts on one
// stream stay strictly ordered, so pipelining them is safe among
// themselves.
//
// It is also what makes the barrier in Patch load-bearing rather than
// decorative. With every write awaited, nothing is ever in flight when a
// patch is issued and the barrier can be deleted with no test noticing —
// a guard that cannot fail. With writes pipelined, a patch issued on the
// unary channel really can overtake them.
func (r *Remote) SetPropertyAsync(name string, v *controlv1.TypedValue) error {
	a := &controlv1.Act{
		Act: &controlv1.Act_SetProperty{SetProperty: &controlv1.SetPropertyRequest{
			Name: name, Value: v,
		}},
	}
	ch := make(chan *controlv1.ActResult, 1)
	r.mu.Lock()
	r.nextID++
	a.Id = r.nextID
	r.pending[a.Id] = ch
	r.mu.Unlock()
	return r.stream.Send(&controlv1.AttachRequest{
		Msg: &controlv1.AttachRequest_Act{Act: a},
	})
}

// RegisterProperties grows the target app's binding surface so generated
// markup can bind names the app never pre-registered.
func (r *Remote) RegisterProperties(ctx context.Context, regs []*controlv1.PropertyRegistration) error {
	_, err := r.act(ctx, &controlv1.Act{
		Act: &controlv1.Act_RegisterProperties{
			RegisterProperties: &controlv1.RegisterPropertiesRequest{Properties: regs},
		},
	})
	return err
}

// Validate checks candidate markup against the app's live binding
// context WITHOUT touching the running tree. It is the editor's cheapest
// primitive by a wide margin: a generation loop can be wrong as often as
// it likes and never flicker the page.
func (r *Remote) Validate(ctx context.Context, source string) (bool, string, error) {
	resp, err := r.control.ValidateMarkup(ctx, &controlv1.ValidateMarkupRequest{Source: source})
	if err != nil {
		return false, "", err
	}
	return resp.GetValid(), resp.GetError(), nil
}

// Patch replaces one named subtree.
//
// removableSerialization: the barrier below exists because nothing
// orders a unary call against acts queued on the stream. Two independent
// goroutines race Dispatcher.Post; measured, a unary patch overtook
// 21–47 pipelined acts.
//
// TRANSPORT-CONDITIONAL, not simply removable. Retire it only on the
// path that actually gains ordering:
//
//   - gRPC act stream: once `patch_markup` is in the Act oneof, route
//     the patch through r.act and delete the barrier. Ordering then
//     belongs to the transport.
//   - MCP: `patch_markup` there posts through a plain Bridge.Do, so
//     `set_value` then `patch_markup` is ordered only by which handler
//     goroutine wins. The barrier is REQUIRED on that path and deleting
//     it would reintroduce the race for anything using MCP as the
//     fallback for apps without a control plane.
//
// So the deletion is per-transport. An editor that speaks both keeps
// this for the MCP path after dropping it for the stream.
func (r *Remote) Patch(ctx context.Context, name, source string) ([]string, error) {
	if err := r.barrier(ctx); err != nil {
		return nil, err
	}
	resp, err := r.control.PatchMarkup(ctx, &controlv1.PatchMarkupRequest{
		Name: name, Source: source,
	})
	if err != nil {
		return nil, err
	}
	return resp.GetNamed(), nil
}

// barrier waits for every act already sent to be answered.
//
// It is not a sleep and not a poll: an ActResult means the act ran on
// the UI goroutine and passed the settle barrier, so an empty pending
// map means the app has finished everything this editor asked for.
func (r *Remote) barrier(ctx context.Context) error {
	for {
		r.mu.Lock()
		var wait chan *controlv1.ActResult
		for _, ch := range r.pending {
			wait = ch
			break
		}
		r.mu.Unlock()
		if wait == nil {
			return nil
		}
		select {
		case <-wait:
		case <-r.done:
			return fmt.Errorf("session lost while draining in-flight acts")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Named lists the Name= identities in the running tree — the addresses
// Patch takes.
func (r *Remote) Named(ctx context.Context) ([]string, error) {
	resp, err := r.control.ListValues(ctx, &controlv1.ListValuesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetNamed(), nil
}

// Size is the target's terminal size as of the last Resized, seeded
// from Welcome. Read it per use rather than caching: a long-lived editor
// outlives any one value.
func (r *Remote) Size() (cols, rows int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cols, r.rows
}

// Close ends the session and JOINS the reader goroutine.
//
// The join is the whole point, and it is the framework's stop idiom applied
// to a stream instead of a ticker: close-then-wait, never close alone. A
// Close that only cancelled would return while read() was still inside
// Recv, and that goroutine can still call r.fail — which calls OnLost, an
// app-supplied callback that touches the editor. So without the join,
// "Close returned" does not mean "nothing else will be delivered", and a
// callback can fire against a torn-down editor after teardown looked
// complete. Intermittent by construction: it depends on whether the reader
// happened to be between Recv calls.
//
// ORDER IS LOAD-BEARING. The wait must come after r.stop(), which cancels
// the stream context and is what makes the pending Recv return an error so
// read() can reach its `defer close(r.done)`. Waiting before the cancel
// would block forever on a healthy stream.
//
// The nil guard is for a zero-valued Remote, not for the normal path:
// Connect starts the reader on the line before it returns, and every
// earlier failure returns (nil, err), so any Remote a caller holds has a
// running reader.
func (r *Remote) Close() error {
	if r.stream != nil {
		_ = r.stream.CloseSend()
	}
	if r.stop != nil {
		r.stop()
	}
	err := r.conn.Close()
	if r.done != nil {
		<-r.done
	}
	return err
}
