# Activity islands: server-driven UI without a workflow (design)

Direction from Elan 2026-08-10 (his build, dictated across two
messages): a Temporal server-driven UI "without a workflow, but with
only the standalone activities" — then composed as "multiple instances
of it in different panels to show islands, with the two panels and
then a third one controlling it so you see source."

## The architecture (third of three)

| | temporaldemo | wizarddemo | activity islands |
|---|---|---|---|
| State lives | terminal | workflow | client-carried blob (or store) |
| Markup lives | terminal | workflow | activities' return values |
| Transition | activity call | signal | activity call |
| Durability | per-call | full app resume | per-step retry safety |
| Server constraints | none | determinism, versioning | none |

Every screen fetch and every transition is one standalone activity:

    Screen(state, action) → { nextState, markup, values }

Activities stay pure functions of (state, action) — retry-safe by
construction under at-least-once, no determinism rules, no
query/signal machinery. What is given up: the durable
resume-across-sessions the wizard accidentally demonstrated (run
019FEA picking up a prior session's choices). If cross-session
resume matters later, the state blob moves to an external store keyed
by session id — an activity concern, invisible to the client.

## The islands composition

One gooey page, three panels:

- **Two island panels** — independent instances of the same generic
  activity-driven region: separate state blobs, separate versions,
  same activity surface. Each island is a UserControl instance whose
  context isolation IS the island boundary (nothing shared but the
  screen). They advance independently — the demo's point is watching
  two server-driven regions live side by side out of sync.
- **One inspector/control panel** — "controlling it so you see
  source": selects an island, shows the SOURCE the server most
  recently served it (the markup text + values/state blob), live as
  it changes; and can drive the selected island (send its actions)
  so cause and effect — press here, source changes there — is
  visible in one frame.

Mechanics available today: the `temporal:` provider (fresh-ID
ExecuteActivity + Dispatcher delivery), wizardui's version-swap client
loop, markup-only UserControls with attribute hand-off, and the
inspector's source view is just a Text bound to the island's
last-served markup property.

## Build notes

- Lives in handlers/temporal (SDK dependency) — WAIT for the
  companion-services agent's restructure to land first (workers moving
  to workers/, wizardui gaining --with-worker) to avoid collision.
- The worker side is a natural CompanionFunc once that lands: the
  islands app self-hosts its activity worker, one shell + dev server.
- Elan may build this one himself; this spec records the dictated
  design either way.
