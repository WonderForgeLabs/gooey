"""A Temporal worker that is also an MCP server for CRUD over its own activities.

One process, three jobs:

1.  A Temporal worker polling TEMPORAL_TASK_QUEUE with exactly ONE
    registered activity: `dispatch`, a dynamic activity
    (`@activity.defn(dynamic=True)`) that answers to any activity type
    name and looks the name up in the runtime registry.

2.  An MCP server (streamable HTTP, loopback) whose tools are CRUD over
    that registry: create_activity takes a name and Python SOURCE and
    execs it into a callable; delete_activity removes it; list/get/run
    round out the surface.

3.  A gooey control-plane client holding ONE `SessionService.Attach`
    stream open for this process's whole lifetime (see control.py).
    Creating an activity is not finished when the callable exists — the
    point is that it becomes REACHABLE from a running terminal UI. So
    create_activity also, as acts on that stream:
      - RegisterProperties `Activity.<Name>.Result`, so markup can bind
        it at all;
      - SetProperty Selected, which is what the app's big star button
        reads as its activity TYPE NAME;
    plus one unary PatchMarkup (the only op the `Act` oneof lacks) that
    rewrites the app's ActivityList with a button per activity, each
    bound to ``{{temporal:Activity `<Name>` .Input | into
    .Activity.<Name>.Result}}``.
    delete_activity is the exact inverse and finishes with the
    UnregisterNames act, so the invented name does not outlive the
    activity.

Because the session subscribes to the app's properties, this worker does
not have to assume its writes landed or that nobody else moved anything:
`SESSION.values` is a live mirror, updated when the frame carrying a
change composes. So `Selected` is reconciled rather than overwritten — a
user pressing ctrl+n moves it with no tool call involved, and a delete
only repoints it if the selection actually went dangling.

The framework rule this sits on: commands cannot be registered over the
control plane — behavior needs code, not storage. But the `temporal:`
handler namespace lets markup bind an activity CALL, and the activity is
the behavior. Registering a property plus patching markup is therefore
enough to make a brand-new button that runs brand-new code.

    DANGER — REMOTE CODE EXECUTION BY DESIGN
    create_activity runs whatever Python it is handed, in this process,
    with this process's privileges. There is no sandbox, no allowlist and
    no review step; that is the demo. Everything binds loopback only.
    Do not expose these ports, do not run this on a shared host, and do
    not run it anywhere you would mind an arbitrary `import os` going.

Environment (all set for you by the Go app when it launches this as a
companion — see apps/dynamic-activities/main.go):

    GOOEY_GRPC_ADDR          host:port of the app's gooey.control.v1 server
    GOOEY_ACTIVITY_MCP_ADDR  host:port this MCP server binds  (default 127.0.0.1:7802)
    TEMPORAL_ADDRESS         Temporal frontend                (default 127.0.0.1:7233)
    TEMPORAL_TASK_QUEUE      queue to poll                    (default gooey-dynamic-activities)
"""

from __future__ import annotations

import asyncio
import logging
import os
import sys
import time
from collections.abc import Sequence
from datetime import timedelta
from typing import Any

import uvicorn
from mcp.server.mcpserver import MCPServer
from temporalio import activity
from temporalio.client import Client
from temporalio.common import RawValue
from temporalio.exceptions import ApplicationError
from temporalio.worker import Worker

from control import ControlSession
from registry import (
    ActivityError,
    Registry,
    activity_list_markup,
    reconcile_selection,
    result_property,
)

TASK_QUEUE = os.environ.get("TEMPORAL_TASK_QUEUE", "gooey-dynamic-activities")
TEMPORAL_ADDRESS = os.environ.get("TEMPORAL_ADDRESS", "127.0.0.1:7233")
MCP_ADDR = os.environ.get("GOOEY_ACTIVITY_MCP_ADDR", "127.0.0.1:7802")
GRPC_ADDR = os.environ.get("GOOEY_GRPC_ADDR", "")

# Matches the handler package's DefaultTimeout, so a run started from
# this server's own run_activity tool behaves like one started from the
# star button rather than living by different rules.
SCHEDULE_TO_CLOSE = timedelta(seconds=60)

# The Name= of the element in dynamicactivities.gooey that this server
# owns. Everything else on the page belongs to the app.
LIST_ELEMENT = "ActivityList"

# The app-owned properties this worker subscribes to. The filter is
# fixed at subscribe time, so these are exactly the names the worker
# needs to REASON about — the per-activity result properties it registers
# later cannot be added to it, and it does not need them: it knows what
# it just wrote there, and the page is what reads them.
WATCHED = ("Selected", "Input", "Activities", "Note", "Output")

EXAMPLE_SOURCE = 'def run(text):\n    return " ".join(w.capitalize() for w in text.split())\n'

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s: %(message)s",
    stream=sys.stdout,
)
log = logging.getLogger("activity-worker")

REGISTRY = Registry()

# Set once the session is attached. Tools await it rather than failing:
# this process is a companion started BEFORE the app's first frame, so an
# early tool call would otherwise race the run loop.
SESSION: ControlSession | None = None
SESSION_READY = asyncio.Event()

# Set when the app says it is closing, so main() can stop the worker and
# the HTTP server instead of waiting to be killed.
SHUTDOWN = asyncio.Event()

_client: Client | None = None
_client_lock = asyncio.Lock()


# ---- the Temporal side -------------------------------------------------


@activity.defn(dynamic=True)
async def dispatch(args: Sequence[RawValue]) -> str:
    """Every activity type name routes here; the name IS the request.

    There is no per-name Python function on this worker — only this
    dispatcher and the registry it reads. An activity created a second
    ago is runnable now, on the worker that has been polling all along.
    """
    name = activity.info().activity_type
    converter = activity.payload_converter()
    argv = [converter.from_payload(a.payload, str) for a in args]
    try:
        return await REGISTRY.invoke(name, argv)
    except ActivityError as exc:
        # Non-retryable: a name that is not in the registry will not
        # appear by being asked again, and the UI wants the message now
        # rather than after the retry budget. (Activities retry by
        # default, which is also why an activity body here should be
        # idempotent — the star button can run it more than once.)
        raise ApplicationError(str(exc), non_retryable=True) from exc
    except Exception as exc:  # noqa: BLE001 - the activity body is user code
        raise ApplicationError(f"{name} raised {exc!r}", non_retryable=True) from exc


async def temporal_client() -> Client:
    global _client
    async with _client_lock:
        if _client is None:
            _client = await Client.connect(TEMPORAL_ADDRESS)
        return _client


# ---- the gooey side ----------------------------------------------------


async def session() -> ControlSession:
    await SESSION_READY.wait()
    assert SESSION is not None
    return SESSION


async def publish(note: str, select: str | None = None) -> None:
    """Push the registry's view of itself into the app, reconciling.

    Order matters and is deliberate: the markup patch is the only step
    that can be rejected by the app, so it goes first and a failure
    leaves nothing half-applied. Everything after it is an act on the
    stream, applied in stream order on the UI goroutine.

    `select` is passed only when the caller has an opinion (create knows
    what it just made). Otherwise the app owns the selection: a user who
    pressed ctrl+n moved it with no tool call, and the live mirror is
    how this worker finds that out. It is repointed only when it has
    actually gone dangling.
    """
    s = await session()
    await s.patch_markup(LIST_ELEMENT, activity_list_markup(REGISTRY))

    names = REGISTRY.names()
    await s.set_string("Activities", "\n".join(names))
    await s.set_string("Selected", reconcile_selection(s.get("Selected"), names, select))
    await s.set_string("Note", note)


async def on_swapped(named: list[str]) -> None:
    """The whole page was replaced; anything this worker patched is gone.

    A PATCH does not come through here (see control.py), so this only
    fires for a real page swap — hot reload, or somebody else's
    swap_markup. If the new page still has an ActivityList, put the
    buttons back; if it does not, this worker has nothing to own on that
    page and says so rather than failing every later tool call.
    """
    if LIST_ELEMENT not in named:
        log.warning("the new page has no %s element; the activity list is not shown", LIST_ELEMENT)
        return
    try:
        await publish(f"page swapped — {len(REGISTRY.names())} activities restored")
    except Exception as exc:  # noqa: BLE001 - a swap is not this worker's failure
        log.warning("could not restore the activity list after a swap: %s", exc)


async def on_closing() -> None:
    SHUTDOWN.set()


def app_state() -> dict[str, Any]:
    """What the app currently says, from the session's live mirror.

    Reported by the tools so a caller sees the real state of the page
    rather than what this worker last tried to write.
    """
    if SESSION is None:
        return {"attached": False}
    return {
        "attached": not SESSION.closed,
        "frame": SESSION.frame,
        "selected": SESSION.get("Selected"),
        "input": SESSION.get("Input"),
        "last_star_result": SESSION.get("Output"),
    }


# ---- the MCP server ----------------------------------------------------

server = MCPServer(
    name="gooey-dynamic-activities",
    instructions=(
        "CRUD over the Temporal activities of a running worker. create_activity takes "
        "Python SOURCE and makes it a live activity — the worker is not restarted and "
        "there is no per-activity function to deploy. Creating one also registers a "
        "result property in the attached gooey app and patches a button for it into the "
        "app's ActivityList, so the new activity is immediately clickable in a terminal "
        "UI; delete_activity removes the button, the activity and the registered name. "
        "Results include the app's live state (which activity is selected, what is in "
        "its input box) read from a subscribed control-plane session, not from this "
        "server's assumptions. The source must define `def run(text):` (async is fine) "
        "and the name must be an identifier. WARNING: the supplied code runs "
        "unsandboxed in the worker process."
    ),
)


@server.tool(
    description=(
        "Create a live Temporal activity from Python source. `code` must define "
        "`def run(text):` — sync or async — which is what the dynamic dispatcher calls "
        "with the app's Input property as its argument. The activity is runnable the "
        "moment this returns: no worker restart. Also registers Activity.<name>.Result "
        "in the attached gooey app and patches a button for it onto the page. "
        "WARNING: `code` is exec'd unsandboxed in the worker process."
    )
)
async def create_activity(name: str, code: str, description: str = "") -> dict[str, Any]:
    act = REGISTRY.create(name, code, description)
    prop = result_property(name)
    s = await session()
    # Registration first: the markup patch below binds this name, and a
    # fragment naming something unregistered does not build. That is
    # what makes registration load-bearing rather than bookkeeping, and
    # the stream's in-order application is what guarantees it.
    await s.register_string(prop, "")
    try:
        await publish(f"created {name} — press the star, or its own button", select=name)
    except Exception:
        # All-or-nothing across the process boundary: an activity whose
        # name the app rejected is worse than no activity, because the
        # registry would then disagree with the page about what exists.
        REGISTRY.delete(name)
        try:
            await s.unregister(prop)
        except Exception:  # noqa: BLE001 - the original failure is the interesting one
            log.exception("rollback: could not unregister %s", prop)
        raise
    log.info("created activity %s (%d lines)", name, len(code.splitlines()))
    return {"created": act.summary(), "activities": REGISTRY.names(), "app": app_state()}


@server.tool(
    description=(
        "Every activity currently registered on this worker, plus the app's live state "
        "(selected activity, input text, last star result) read off the control-plane session."
    )
)
async def list_activities() -> dict[str, Any]:
    return {"activities": REGISTRY.summaries(), "app": app_state()}


@server.tool(description="One activity, including the source it was created from.")
async def get_activity(name: str) -> dict[str, Any]:
    act = REGISTRY.get(name)
    return {**act.summary(), "code": act.source}


@server.tool(
    description=(
        "Delete an activity: remove it from the worker's registry, patch its button off "
        "the gooey page, and unregister Activity.<name>.Result from the app's binding "
        "context (the UnregisterNames act). Invoking it afterwards fails as an unknown "
        "activity. If the deleted activity was the selected one, the selection is "
        "repointed; otherwise whatever the user selected is left alone."
    )
)
async def delete_activity(name: str) -> dict[str, Any]:
    REGISTRY.delete(name)
    s = await session()
    # Patch first, unregister second. Removal never disturbs the RUNNING
    # tree — a component already bound to a name keeps its handle and
    # keeps rendering — so the button has to go before the name does, or
    # a stale button would linger with nothing behind it. publish() also
    # reconciles the selection against the live mirror.
    await publish(f"deleted {name}")
    await s.unregister(result_property(name))
    log.info("deleted activity %s", name)
    return {"deleted": name, "activities": REGISTRY.names(), "app": app_state()}


@server.tool(
    description=(
        "Run an activity through Temporal right now and return its result — the same "
        "standalone-activity execution the app's star button performs, without needing "
        "the UI. Omit `text` to use whatever is currently in the app's input box."
    )
)
async def run_activity(name: str, text: str | None = None) -> dict[str, Any]:
    if name not in REGISTRY:
        raise ActivityError(f"no activity named {name!r}")
    if text is None:
        # Read the app's live input rather than defaulting to "": the
        # session mirror is what the star button would have sent.
        text = SESSION.get("Input") if SESSION is not None else ""
    client = await temporal_client()
    result = await client.execute_activity(
        name,
        text,
        id=f"mcp-{name}-{time.time_ns()}",
        task_queue=TASK_QUEUE,
        schedule_to_close_timeout=SCHEDULE_TO_CLOSE,
        summary=f"mcp {name}",
    )
    return {"activity": name, "input": text, "result": result}


# ---- wiring ------------------------------------------------------------


async def connect_session() -> None:
    """Attach the session and seed the page with an empty list."""
    global SESSION
    if not GRPC_ADDR:
        log.warning("GOOEY_GRPC_ADDR is unset; running headless (no app to patch)")
        return
    s = ControlSession(GRPC_ADDR, watch=WATCHED, on_swapped=on_swapped, on_closing=on_closing)
    await s.attach()
    SESSION = s
    SESSION_READY.set()
    await publish(f"activity MCP server on http://{MCP_ADDR}/mcp — call create_activity")


async def control_lifetime() -> None:
    """Attach, then live until the app says it is closing.

    One task, so that a session that fails to attach brings the process
    down (a companion with no app to drive has no job) while a session
    that attaches fine does NOT look like a completed task to main's
    FIRST_COMPLETED wait.
    """
    await connect_session()
    await SHUTDOWN.wait()
    log.info("the app closed; stopping the worker and the MCP server")


async def main() -> None:
    host, _, port = MCP_ADDR.rpartition(":")
    app = server.streamable_http_app(json_response=True, stateless_http=True)
    http = uvicorn.Server(
        uvicorn.Config(app, host=host or "127.0.0.1", port=int(port), log_config=None)
    )

    client = await temporal_client()
    worker = Worker(client, task_queue=TASK_QUEUE, activities=[dispatch])

    log.info(
        "worker on %r at %s; MCP on http://%s/mcp; example: "
        'create_activity(name="Titlecase", code=%r)',
        TASK_QUEUE,
        TEMPORAL_ADDRESS,
        MCP_ADDR,
        EXAMPLE_SOURCE,
    )

    tasks = [
        asyncio.create_task(worker.run(), name="temporal-worker"),
        asyncio.create_task(http.serve(), name="mcp-http"),
        asyncio.create_task(control_lifetime(), name="control-session"),
    ]
    # Whichever finishes first ends the process: the app closing, a
    # server falling over, or the session refusing to attach. The Go app
    # kills this process group at teardown anyway; reacting to the
    # Closing lifecycle event just means the worker stops cleanly a
    # moment earlier, with its own log line, instead of being shot.
    done, pending = await asyncio.wait(tasks, return_when=asyncio.FIRST_COMPLETED)

    # uvicorn gets asked to stop rather than cancelled: cancelling it
    # mid-lifespan makes it log the CancelledError as an ERROR traceback,
    # which is a lie about an ordinary shutdown. Everything else takes a
    # cancel, and uvicorn takes one too if it will not leave politely.
    http.should_exit = True
    graceful = [t for t in pending if t.get_name() == "mcp-http"]
    if graceful:
        _, still = await asyncio.wait(graceful, timeout=5)
        pending = set(pending) - set(graceful) | still
    for task in pending:
        task.cancel()
    await asyncio.gather(*pending, return_exceptions=True)
    if SESSION is not None:
        await SESSION.aclose()
    for task in done:
        task.result()  # re-raise whatever brought the process down


if __name__ == "__main__":
    asyncio.run(main())
