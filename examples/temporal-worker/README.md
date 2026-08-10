# Dynamic UI over a Temporal standalone activity

A Python Temporal worker with one **dynamic activity** (`@activity.defn(dynamic=True)`)
that answers to any activity type name a caller invents — `GenerateUI`, `BuildDashboard`,
whatever fits the moment. The activity type name plus a topic string are handed to Claude,
which writes gooey markup describing a UI panel about that topic. The activity then:

1. Pushes the markup live into a running gooey app via `swap_markup` on its MCP endpoint
   (`http://127.0.0.1:7777/mcp` by default — see gooey's `docs/specs/2026-08-10-mcp-server.md`).
2. Returns the markup as the activity's result, for a caller that wants the value directly
   instead of (or in addition to) the live push.

No workflow is involved — this is a **standalone activity**
(<https://docs.temporal.io/standalone-activity>): the client starts it directly with
`client.execute_activity(...)`, and whichever worker is polling the task queue picks it up.

## Setup

```sh
cd examples/temporal-worker
python3 -m venv .venv && source .venv/bin/activate
pip install -U -r requirements.txt
cp .env.example .env   # then fill in a credential, if you don't already have one active
```

Needs a Temporal server (`temporal server start-dev` if you don't have one running), a
`claude` CLI on `PATH` (the Claude Agent SDK shells out to it — see below), and, for the
live push to do anything visible, a gooey app serving MCP on `GOOEY_MCP_URL`
(`cmd/mcpdemo` in this repo is a ready-made target).

## Run it

```sh
# terminal 1
python worker.py

# terminal 2 — activity type name is made up on the spot; the worker has no
# GenerateUI function, only the dynamic handler that answers to any name
python trigger.py GenerateUI "the current state of this conversation"
```

The gooey app's page swaps live if one is running; either way `trigger.py` prints the
generated markup.

### One shell instead of two: examples/kanbandemo -with-worker

Running this worker by hand next to a target app is two shells and, in practice, the
manually-started process outliving the demo — the same shape
`docs/specs/2026-08-10-companions.md` describes for the Temporal wizard's dev server.
`examples/kanbandemo` wires this worker in as a `gooey.CompanionCmd`: the Go app starts
it before its first frame and kills it (the whole process group, not just the direct
child) when it quits, so there is nothing left polling after the demo closes.

```sh
cd examples/kanbandemo
go run . -mcp 127.0.0.1:7778 -with-worker -worker-python /path/to/.venv/bin/python
```

`-worker-task-queue` (default `kanbandemo-dynamic-ui`) keeps a companion-launched worker
here from colliding with one you run by hand against the generic
`gooey-dynamic-ui-task-queue`. The companion's stdout/stderr are redirected to
`examples/temporal-worker/kanbandemo-worker.log` — a gooey app owns the terminal, so the
worker's output never goes to the tty directly (see the "Output goes to `os.DevNull`"
note in `docs/specs/2026-08-10-companions.md`). Trigger it the same way, pointed at the
companion's queue:

```sh
cd examples/temporal-worker
TEMPORAL_TASK_QUEUE=kanbandemo-dynamic-ui python trigger.py GenerateUI "the kanban board's todo column"
```

## Why this shape

- **Dynamic activity, not one function per UI kind.** `generate_ui` in `activities.py` is
  registered once; the caller's activity-type-name choice *is* the request, decoded from
  `Sequence[RawValue]` via `activity.payload_converter()` and read back via
  `activity.info().activity_type`.
- **Markup generation is constrained to bindingless elements** (`Border`/`Grid`/`VStack`/
  `HStack`/`Text`/`Button` with no `{{ }}` expressions) so the generated page is safe to
  push into *any* running gooey app's `swap_markup` — it never risks referencing a
  property or command that app's viewmodel doesn't have, which would otherwise reject the
  whole page atomically.
- **Generation goes through the Claude Agent SDK, not the raw Messages API.** `query()`
  from `claude_agent_sdk` shells out to the `claude` CLI, which resolves credentials the
  same way the CLI itself does. A `CLAUDE_CODE_OAUTH_TOKEN` (from `claude setup-token`) is
  scoped for that path — calling `client.messages.create()` directly with one authenticates
  fine but trips a much tighter rate limit than the CLI's own traffic gets.
