# Workflows

Multi-agent workflow scripts for this repo, run with the `Workflow`
tool. They are opt-in: nothing runs them automatically.

## docs-and-demos.js

Re-records every demo GIF and regenerates the documentation set
(`docs/architecture.md`, `docs/getting-started.md`,
`docs/markup-reference.md`, `docs/demos.md`, `README.md`), then
verifies the result — links resolve, documented identifiers exist in
the source, the getting-started program compiles, `go build ./...` and
`go test ./...` are green.

Run it after a change that alters what the demos look like or what the
docs claim: an input-system change, a new component, a renamed API.

Prepare the scratch directory first — the recording agents assume it,
and they use the prebuilt binaries deliberately, so that a tree being
edited elsewhere cannot break a recording mid-run:

```sh
S=/tmp/gooey-recordings
mkdir -p $S
for d in probe demo propdemo logview markuplog finder reader statedemo; do
  go build -o $S/$d ./cmd/$d
done
cp cmd/*/*.gooey $S/
```

Then:

```
Workflow({ scriptPath: '.claude/workflows/docs-and-demos.js' })
```

Optional `args`: `{ repo: '/path/to/gooey', scratch: '/tmp/gooey-rec' }`.

Requires `asciinema` 2.x, `agg`, and ImageMagick `convert` on PATH. The
recorders drive each demo keyboard-only through a pty (`script -qec`
with an explicit `stty` size) because a pty cannot synthesize mouse
input, and each verifies its own GIF by extracting frames before
reporting success.

Cost: 12 agents, roughly 700k output tokens and ~12 minutes wall clock
for a full run.
