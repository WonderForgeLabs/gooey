"""The gooey control plane, as this worker uses it: one Attach stream.

`SessionService.Attach` is a bidi stream held open for the worker's whole
lifetime, and it is the primary surface here — not the unary
`ControlService` calls, which would be polling with extra steps. The
contract's own framing (docs/specs/2026-08-10-grpc-contract.md, issue
#49): "push replaces polling — a subscribed client hears about a change
when the frame containing it composes, not on the next 400ms tick."

What the stream buys, concretely:

*   **State sync.** Subscribing to `properties` (with a `names` filter,
    because everything is opt-in and an all-defaults Subscription is
    write-only) means `values` below is a live mirror of the app's side
    of the conversation. The worker can look before it writes, and can
    reconcile against what the app did on its own — a user pressing
    ctrl+n moves `Selected` with no tool call involved.

*   **Ordering.** Acts "are applied in stream order on the UI goroutine
    — the remote mirror of the one ordered input stream". Register-then-
    patch-then-select is applied in that order by construction, which
    concurrent unary calls do not give you.

*   **Read-after-write with no barrier of our own.** The FrameDelta for
    an act's frame is enqueued DURING that act's settle barrier, so it
    lands in the queue before the act's own ActResult. By the time
    `act()` returns, the mirror already reflects the write.

*   **Lifetime.** Cancelling the stream ends the session, and the app
    ends it from its side (a `Closing` lifecycle event) when it shuts
    down. Companion lifetime and session lifetime coincide with no
    bookkeeping.

Two things still cross as unary calls, both deliberately:

*   **A one-shot `ListValues` at attach.** A session's property baseline
    is captured at registration, so its deltas carry CHANGES since
    attach, never the world — and `Welcome` "deliberately carries
    identity, not data". Seeding the mirror once is the documented way
    to have a complete picture, and it happens before the reader starts
    so it can never overwrite a newer delta.

*   **`PatchMarkup`.** The `Act` oneof enumerates SetProperty,
    InvokeCommand, SendKeys, SendPointer, SetFocus, SwapMarkup,
    RegisterProperties and UnregisterNames — but *not* PatchMarkup,
    which was added to the unary surface later and never backfilled onto
    the stream. So markup patching does not get the stream's ordering
    guarantee, and callers have to sequence it by hand. When the oneof
    grows a `patch_markup` variant, this becomes an act like the rest.

The generated stubs come from the `gooey-control` package under
clients/python — buf output, committed, never hand-edited.
"""

from __future__ import annotations

import asyncio
import logging
from collections.abc import Awaitable, Callable, Iterable, Sequence
from typing import Any

import grpc
from gooey.control.v1 import control_pb2, control_pb2_grpc, session_pb2, session_pb2_grpc, types_pb2

log = logging.getLogger("control")


class ActError(RuntimeError):
    """An act the app refused, answered in-stream rather than by a status.

    A failed act does not tear down the session — that is the point of
    ActResult carrying a code — so this is an ordinary exception the
    caller can catch and keep using the session.
    """

    def __init__(self, code: int, message: str, what: str) -> None:
        super().__init__(f"{what} failed ({grpc.StatusCode(code).name if _known(code) else code}): {message}")
        self.code = code
        self.message = message
        self.what = what


class SessionClosed(RuntimeError):
    """The stream ended — the app quit, or the worker is shutting down."""


def _known(code: int) -> bool:
    try:
        grpc.StatusCode(code)
    except ValueError:
        return False
    return True


def _plain(tv: types_pb2.TypedValue) -> Any:
    """A TypedValue as an ordinary Python value.

    Only the kinds this demo's properties actually use are unpacked
    specially; anything else falls back to the raw message rather than
    guessing, so a future kind shows up as something obviously
    unhandled instead of as a silently wrong string.
    """
    kind = tv.WhichOneof("kind")
    if kind is None:
        return None
    if kind in ("string_value", "int_value", "bool_value", "float_value"):
        return getattr(tv, kind)
    if kind == "any_json":
        return tv.any_json.decode("utf-8", "replace")
    return tv


class ControlSession:
    """One Attach stream to a running gooey app, held for the worker's life.

    Not reconnecting: this process is a companion of the app it talks
    to, so a session that ends means the app is gone and the worker's
    job is over. Reconnection logic would be pretending otherwise.
    """

    def __init__(
        self,
        address: str,
        watch: Sequence[str] = (),
        on_swapped: Callable[[list[str]], Awaitable[None]] | None = None,
        on_closing: Callable[[], Awaitable[None]] | None = None,
    ) -> None:
        self.address = address
        self.watch = tuple(watch)
        self.values: dict[str, Any] = {}
        self.frame = 0
        self.welcome: session_pb2.Welcome | None = None

        self._on_swapped = on_swapped
        self._on_closing = on_closing
        self._channel = grpc.aio.insecure_channel(address)
        self._session = session_pb2_grpc.SessionServiceStub(self._channel)
        # PatchMarkup only — see the module docstring.
        self._unary = control_pb2_grpc.ControlServiceStub(self._channel)

        self._stream: Any = None
        self._reader: asyncio.Task[None] | None = None
        self._write_lock = asyncio.Lock()
        self._next_id = 0
        self._pending: dict[int, asyncio.Future[session_pb2.ActResult]] = {}
        self._closed = asyncio.Event()
        self._callbacks: set[asyncio.Task[None]] = set()

    # ---- lifecycle ----

    async def attach(self, timeout: float = 30.0) -> session_pb2.Welcome:
        """Open the session, retrying until the app's run loop answers.

        The handshake IS the readiness probe, so no separate poll is
        needed: `Attach` registers the session ON the UI goroutine (it
        reads the property baseline and the screen geometry there), and
        this process is started as a companion BEFORE the app's first
        frame. Early attempts fail with DEADLINE_EXCEEDED against the
        bridge's own timeout; retrying turns that into a startup wait
        rather than a spurious failure on the first tool call.
        """
        deadline = asyncio.get_running_loop().time() + timeout
        delay = 0.05
        while True:
            try:
                return await self._attach_once()
            except grpc.aio.AioRpcError as exc:
                if asyncio.get_running_loop().time() >= deadline:
                    raise RuntimeError(
                        f"gooey control plane at {self.address} never accepted a session: {exc}"
                    ) from exc
                await asyncio.sleep(delay)
                delay = min(delay * 2, 1.0)

    async def _attach_once(self) -> session_pb2.Welcome:
        stream = self._session.Attach()
        # The first message MUST be subscribe; a second one is
        # INVALID_ARGUMENT, so it happens here and nowhere else.
        await stream.write(
            session_pb2.AttachRequest(
                subscribe=session_pb2.Subscription(
                    properties=True,
                    # Filtered on purpose: an unfiltered subscription would
                    # deliver every computed the page recomputes, per frame.
                    # NOTE this list is fixed for the life of the session —
                    # names registered LATER (Activity.<Name>.Result) cannot
                    # be added to it without reattaching.
                    names=list(self.watch),
                    # Markup swaps and shutdown. Not `frames` (we do not want
                    # a message per composed frame) and not `input` (the
                    # worker has no business watching keystrokes).
                    lifecycle=True,
                )
            )
        )
        first = await stream.read()
        if first is grpc.aio.EOF:
            raise grpc.aio.AioRpcError(
                grpc.StatusCode.UNAVAILABLE, None, None, details="the session ended before welcome"
            )
        welcome = first.welcome
        if first.WhichOneof("msg") != "welcome":
            raise RuntimeError(f"the first server message was {first.WhichOneof('msg')}, not welcome")

        self._stream = stream
        self.welcome = welcome
        self.frame = welcome.frame

        # One-shot bootstrap, and the one other place a unary call is
        # right. The contract says so outright: "Initial state is read
        # through the unary surface (or the first FrameDelta); Welcome
        # deliberately carries identity, not data." A session's baseline
        # is captured at registration, so its deltas carry CHANGES since
        # attach, never the world — without this read the mirror would
        # only ever know about names something happened to touch.
        #
        # Sequenced before the reader starts, so it can never overwrite a
        # newer delta: anything the app changed in between is already
        # queued server-side and lands as a delta the moment the reader
        # begins.
        await self._bootstrap()

        self._reader = asyncio.create_task(self._read_loop(), name="control-session-reader")
        log.info(
            "attached to %s (%s %s, %dx%d) at frame %d",
            self.address, welcome.app_name, welcome.app_version,
            welcome.columns, welcome.rows, welcome.frame,
        )
        return welcome

    async def _bootstrap(self) -> None:
        """Seed the mirror with the current value of every watched name."""
        res = await self._unary.ListValues(control_pb2.ListValuesRequest())
        watched = set(self.watch)
        for info in res.values:
            if watched and info.name not in watched:
                continue
            if info.HasField("value"):
                self.values[info.name] = _plain(info.value)

    async def aclose(self) -> None:
        """End the session. Cancelling the stream is all it takes."""
        for task in list(self._callbacks):
            task.cancel()
        if self._stream is not None:
            self._stream.cancel()
        if self._reader is not None:
            self._reader.cancel()
            try:
                await self._reader
            except asyncio.CancelledError:
                pass
        await self._channel.close()
        self._closed.set()

    @property
    def closed(self) -> bool:
        return self._closed.is_set()

    async def wait_closed(self) -> None:
        await self._closed.wait()

    # ---- the reader ----

    async def _read_loop(self) -> None:
        """Every AttachResponse variant, handled.

        `welcome` is consumed by the handshake and would be a protocol
        error here; the other four are results, frame deltas, lifecycle
        events and input echoes.
        """
        try:
            while True:
                resp = await self._stream.read()
                if resp is grpc.aio.EOF:
                    return
                kind = resp.WhichOneof("msg")
                if kind == "result":
                    self._settle(resp.result)
                elif kind == "frame":
                    self._apply_frame(resp.frame)
                elif kind == "lifecycle":
                    self._lifecycle(resp.lifecycle)
                elif kind == "input":
                    # Not subscribed; a server that sent one anyway is
                    # not an error, it is just not our business.
                    pass
                elif kind == "welcome":
                    log.warning("a second welcome arrived mid-session; ignoring")
        except asyncio.CancelledError:
            raise
        except grpc.aio.AioRpcError as exc:
            if exc.code() is not grpc.StatusCode.CANCELLED:
                log.warning("control session ended: %s", exc)
        finally:
            self._closed.set()
            # Nothing will ever answer these now.
            for fut in self._pending.values():
                if not fut.done():
                    fut.set_exception(SessionClosed("the control session ended"))
            self._pending.clear()

    def _settle(self, res: session_pb2.ActResult) -> None:
        fut = self._pending.pop(res.id, None)
        if fut is None:
            log.warning("an ActResult arrived for unknown id %d", res.id)
            return
        if not fut.done():
            fut.set_result(res)

    def _apply_frame(self, delta: session_pb2.FrameDelta) -> None:
        self.frame = delta.frame
        for change in delta.changes:
            self.values[change.name] = _plain(change.value)

    def _spawn(self, coro: Any, what: str) -> None:
        """Run a subscriber callback OFF the reader task.

        This is load-bearing, not tidiness. A callback that reacts to an
        event by sending an act — which is the whole point of hearing
        about a swap — waits for the ActResult that settles it, and only
        the reader can settle it. Awaiting the callback here would
        deadlock the session on its first swap: the patch (unary) would
        land, the follow-up acts would hang forever, and every later
        event would go unread with the stream still nominally healthy.
        Strong references are kept until each task finishes so nothing
        is collected mid-flight.
        """
        task = asyncio.create_task(coro, name=what)
        self._callbacks.add(task)

        def _finished(t: asyncio.Task[None]) -> None:
            self._callbacks.discard(t)
            if not t.cancelled() and t.exception() is not None:
                log.exception("control session %s callback failed", what, exc_info=t.exception())

        task.add_done_callback(_finished)

    def _lifecycle(self, ev: session_pb2.LifecycleEvent) -> None:
        which = ev.WhichOneof("event")
        if which == "closing":
            log.info("the app is shutting down")
            self._closed.set()
            if self._on_closing is not None:
                self._spawn(self._on_closing(), "closing")
        elif which == "swapped":
            # The whole page was replaced (hot reload, or somebody else's
            # swap_markup). Anything this worker patched in is gone, so
            # whoever owns that markup has to put it back. A PATCH does
            # not come through here — control.Service.PatchMarkup only
            # calls host.Swap when the patched element IS the root — so
            # there is no recursion between this and our own patching.
            log.info("the page was swapped; %d named elements", len(ev.swapped.named))
            if self._on_swapped is not None:
                self._spawn(self._on_swapped(list(ev.swapped.named)), "swapped")
        elif which == "resized":
            log.debug("resized to %dx%d", ev.resized.columns, ev.resized.rows)

    # ---- acts ----

    async def act(self, what: str, **oneof: Any) -> session_pb2.ActResult:
        """Send one Act and wait for the ActResult carrying the same id.

        By the time this returns, the FrameDelta for the act's frame has
        already been applied to `values` — the contract enqueues it
        during the settle barrier, ahead of the acknowledgement.
        """
        if self._stream is None:
            raise SessionClosed("the control session is not attached")
        if self.closed:
            raise SessionClosed("the control session has ended")
        loop = asyncio.get_running_loop()
        async with self._write_lock:
            self._next_id += 1
            act_id = self._next_id
            fut: asyncio.Future[session_pb2.ActResult] = loop.create_future()
            self._pending[act_id] = fut
            try:
                await self._stream.write(
                    session_pb2.AttachRequest(act=session_pb2.Act(id=act_id, **oneof))
                )
            except Exception:
                self._pending.pop(act_id, None)
                raise
        res = await fut
        if res.code != 0:
            raise ActError(res.code, res.message, what)
        return res

    async def register_string(self, name: str, initial: str = "") -> None:
        await self.act(
            f"register {name}",
            register_properties=control_pb2.RegisterPropertiesRequest(
                properties=[
                    types_pb2.PropertyRegistration(
                        name=name,
                        kind=types_pb2.ValueKind.VALUE_KIND_STRING,
                        initial=types_pb2.TypedValue(string_value=initial),
                    )
                ]
            ),
        )

    async def unregister(self, *names: str) -> None:
        await self.act(
            f"unregister {', '.join(names)}",
            unregister_names=control_pb2.UnregisterNamesRequest(names=list(names)),
        )

    async def set_string(self, name: str, value: str) -> bool:
        """Set a string property, skipping a write the app already has.

        `prop.Set` does NOT compare values — an identical Set still
        invalidates every dependent and costs a repaint — so looking at
        the mirror first is a real saving, not a micro-optimization. It
        only applies to watched names; anything else is written blind
        because there is nothing to compare against.
        """
        if name in self.values and self.values[name] == value:
            return False
        await self.act(
            f"set {name}",
            set_property=control_pb2.SetPropertyRequest(
                name=name, value=types_pb2.TypedValue(string_value=value)
            ),
        )
        return True

    def get(self, name: str, default: str = "") -> str:
        """The app's current value for a watched name, from the mirror."""
        v = self.values.get(name, default)
        return v if isinstance(v, str) else default

    # ---- the unary holdout ----

    async def patch_markup(self, name: str, source: str) -> Iterable[str]:
        """Replace one named element's subtree.

        Unary because the `Act` oneof has no `patch_markup` variant —
        see the module docstring. Ordering against the stream's acts is
        therefore NOT guaranteed by the transport, so callers must await
        this before sending acts that depend on it (and after the acts
        it depends on, which is why registration happens first).
        """
        res = await self._unary.PatchMarkup(
            control_pb2.PatchMarkupRequest(name=name, source=source)
        )
        return res.named
