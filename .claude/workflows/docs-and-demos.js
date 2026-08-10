export const meta = {
  name: 'gooey-docs-and-demos',
  description: 'Re-record all gooey demos as GIFs and write full documentation + README',
  phases: [
    { title: 'Record', detail: 'one agent per demo: asciinema + agg, verify frames' },
    { title: 'Write', detail: 'architecture, getting-started howto, markup reference' },
    { title: 'Assemble', detail: 'demo catalog + root README' },
    { title: 'Verify', detail: 'links, API accuracy, build, fix inline' },
  ],
}

// Re-records every demo GIF and regenerates the documentation set.
// Run it after changes that alter what the demos look like or what the
// docs claim: an input-system change, a new component, a renamed API.
//
//   Workflow({ scriptPath: '.claude/workflows/docs-and-demos.js' })
//   Workflow({ scriptPath: '...', args: { repo: '/path/to/gooey',
//                                         scratch: '/tmp/gooey-rec' } })
//
// PREREQUISITES the caller satisfies before invoking — the recording
// agents assume all of them:
//   * asciinema 2.x, agg, and ImageMagick `convert` on PATH
//   * `go build -o <scratch>/<name> ./cmd/<name>` for every demo
//   * every cmd/**/*.gooey copied into <scratch> beside the binaries
// Recorders use those prebuilt binaries deliberately: a demo rebuilt
// mid-run from a tree someone else is editing produces a broken GIF
// that looks like a choreography bug.
const REPO = (typeof args === 'object' && args?.repo) || '/home/elan/repos/WonderForgeLabs/gooey'
const S = (typeof args === 'object' && args?.scratch) || '/tmp/gooey-recordings'

const RECORDING_RULES = `
RECORDING ENVIRONMENT (follow exactly — these are hard-won session lessons):
- Repo: ${REPO} (Go 1.25). Do not commit or switch branches; leave git state to the caller.
- Prebuilt demo binaries and .gooey files are staged in ${S} (probe, demo, propdemo, logview, markuplog, finder, reader, statedemo + all .gooey files). Use the prebuilt binary; rebuild only if it is missing or crashes at startup.
- If ${S}/record*.sh exists from an earlier run, READ the relevant one and adapt it rather than starting over; otherwise write one following the pattern below and leave it in ${S} for next time.
- Record with: asciinema rec --overwrite --cols <W> --rows <H> -c "<script.sh>" out.cast   (asciinema 2.4)
- Convert with: agg --theme dracula --font-size 14 out.cast ${REPO}/docs/media/demos/<demo>.gif
- Inside record scripts (they run under dash): printf OCTAL escapes ONLY — \\011 tab, \\015 enter, \\033[Z shift-tab, \\177 backspace, \\033[B down-arrow. NEVER \\x hex (dash printf ignores it and the literal text pollutes input).
- Drive the demo keyboard-only (ptys cannot synthesize mouse). Pattern: ( subshell: sleeps + printf keys ) | script -qec "stty cols W rows H; ${S}/<binary> [args]" /dev/null — the stty MUST set the size or the pty is 0x0 and renders nothing.
- The demos use the NEW input system (unified event stream): tab moves focus, enter/space activates, KeyBindings like q quit. If a key seems dead, check the demo's source in ${REPO}/cmd/<demo>/ for its current key map — do not assume.
- VERIFY before declaring success: (1) grep -a the .cast for expected strings; (2) convert -coalesce <gif> ${S}/<demo>-f-%d.png and Read (as image) a mid frame and a late frame to confirm the choreography actually happened on screen. A GIF showing an empty UI or missed keystrokes is a FAILURE — fix the choreography and re-record.
- Final GIF must land at ${REPO}/docs/media/demos/<demo>.gif (overwriting is correct).`

const GIF_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['ok', 'gif', 'headline', 'beats', 'runCmd', 'keys'],
  properties: {
    ok: { type: 'boolean' },
    gif: { type: 'string', description: 'absolute path of the produced GIF' },
    headline: { type: 'string', description: 'one sentence: what this demo proves' },
    beats: { type: 'array', items: { type: 'string' }, description: 'the visible story beats of the GIF, in order' },
    runCmd: { type: 'string', description: 'how a user runs this demo, e.g. go run ./cmd/propdemo' },
    keys: { type: 'string', description: 'the demo key map, e.g. "a/b bump - m toggle - q quit"' },
  },
}

const DOC_SCHEMA = {
  type: 'object', additionalProperties: false,
  required: ['file', 'sections'],
  properties: {
    file: { type: 'string', description: 'repo-relative path of the doc written' },
    sections: { type: 'array', items: { type: 'string' }, description: 'H2 section titles in order' },
  },
}

const RECORDERS = [
  {
    demo: 'demo',
    brief: `Re-record cmd/probe + cmd/demo into a single ${REPO}/docs/media/demos/demo.gif (capability detection, then the graphics pipeline). Old script: ${S}/record.sh. It is a plain sequential script, not a keystroke choreography: echo a fake prompt, run ${S}/probe, pause, echo a second prompt, run ${S}/demo --mode=halfblock --hold=5s (the --hold flag exists precisely so this records unattended). 80x24, agg --font-size 16 for this one. Expected in cast: "selected:", "graphics mode : halfblock", and the gradient/ring image rendered as halfblock cells. Note the recording pty reports no graphics support, so halfblock is both the honest result and the only one that survives into a GIF — say so in the headline rather than implying sixel was used.`,
  },
  {
    demo: 'statedemo',
    brief: `Re-record cmd/statedemo (markup-only UI: buttons, checkbox, JSON serialization). Old script: ${S}/record-state.sh. Choreography: ~2s, 's' (manual snapshot of the initial state), ~2s, enter x2 on the focused [count +1] button — the box must visibly NOT change, that staleness is the point, ~1.5s, tab x3 to the checkbox, space (box flips to the live computed JSON), ~2s, shift-tab x3 back to [count +1], enter three times ~0.9s apart (each one now re-serializes reactively), tab to [cycle message], enter (message changes live in the JSON), ~2s, 'q'. 100x34. Expected: "press [ serialize" initially, then "\\"count\\": 5" and "auto — derived through the property graph" in the final frame. The GIF must show both halves: stale-under-manual, then live-under-auto.`,
  },
  {
    demo: 'propdemo',
    brief: `Re-record cmd/propdemo (dependency-property graph demo). Old script: ${S}/record-props.sh (adapt: binary is now ${S}/propdemo and the stats line now reads "components painted last frame=N"). Choreography: ~2s idle (1 Hz tick renders), hammer 'b' 5x while unwatched (NO frames until next tick - the point), press 'a' (instant), press 'm' (toggle watched source), press 'b' twice (now instant), 'q'. 80x24. Expected strings in cast: "components painted last frame", "detail evals". The money proof: events counter jumps +5 in one hop while frames barely move, and painted counter shows ~2 of 8.`,
  },
  {
    demo: 'logview',
    brief: `Re-record cmd/logview (log viewer with pause via conditional dependency). Old script: ${S}/record-logview.sh (binary ${S}/logview; stats line now includes "components painted last frame"). Choreography: ~3s following (stream renders), space (pause - screen freezes while lines keep arriving), 'f' cycle filter twice WHILE PAUSED (UI still alive against frozen snapshot), 'f' back to all, space (resume - one-frame catch-up), 'q'. 100x28. Expected: "PAUSED", "filter: ERROR", "lines arrived". Money proof: lines-arrived jumps ~60 while frames rose ~5 during the pause.`,
  },
  {
    demo: 'markuplog',
    brief: `Re-record cmd/markuplog (markup + hot reload). Old script: ${S}/record-markup.sh — CAREFUL: it seds a live copy of logview.gooey; the current cmd/markuplog/logview.gooey is now a Grid with KeyBindings — copy the CURRENT file fresh to ${S}/live.gooey before recording and adapt the sed patterns to it (Title="logview" still exists to sed; for the insert-a-line edit, insert a <Text Grid.Row=...> compatible with the Grid rows or simply change the star row / title only — verify your edited XML parses by keeping edits minimal). Choreography: ~4s stream, sed edit 1 (title change) -> hot reloads=1 with buffer intact, ~3s, sed edit 2 (second visible change), ~3s, 'q'. 100x28. Expected: "hot reloads=1", the edited title text, "lines arrived" never resetting.`,
  },
  {
    demo: 'finder',
    brief: `Re-record cmd/finder (fuzzy file finder). Old script: ${S}/record-finder.sh (binary ${S}/finder, needs ${S}/finder.gooey next to it OR run from repo dir — the script cd's to the repo; keep that). Choreography: type "compos" char by char (~0.35s apart; watch narrowing + match highlighting), ~1.5s, down-arrow twice (preview follows selection), backspace x6, type "gooey", down once, enter (prints selected path). 110x30. Expected: "matched in", match highlighting visible in frames, final printed path in cast.`,
  },
  {
    demo: 'reader',
    brief: `Re-record cmd/reader (three-pane RSS/Atom reader, UserControls, live network fetch). Old script: ${S}/record-reader.sh — adapt: binary ${S}/reader; IMPORTANT: cd to ${S} and rm -f ${S}/feeds.opml first (first-run writes the default OPML); the reader needs network (hnrss.org, lobste.rs, go.dev, blog.cloudflare.com — all verified reachable). Keys now via the input system: tab cycles pane focus, j/k move in focused pane, enter (while stories pane focused) opens story, 'a' add-feed input mode, 'q' quit. Choreography: ~5.5s fetch fill-in, 'j' (select Lobsters in feeds pane - it has initial focus), tab (stories pane, title shows filled dot), 'jj', enter (reader pane fills), ~3s, tab to feeds, 'a', type https://xkcd.com/atom.xml (~0.09s/char), enter, ~3.5s (xkcd appears in feed list + OPML written), 'q'. After exit print grep -o 'text="[^"]*"' feeds.opml to show the write-back. 120x32. Expected: "Lobsters (", a story title in the reader pane, "xkcd" in cast, 5 feeds in final OPML echo. If a feed fails to fetch (network flake) that is OK as long as at least 2 feeds fill and the story-open beat works.`,
  },
]

const WRITERS = [
  {
    file: 'docs/architecture.md',
    brief: `Write ${REPO}/docs/architecture.md — the deep architecture guide. READ FIRST (do not skip): README.md, render/cell.go, render/ansi.go, graphics/graphics.go, term/term.go, prop/prop.go, component.go, components/, composer.go, layout.go, grid.go, input.go, mouse.go, input/ (all files), term/keys.go, markup/markup.go, markup/usercontrol.go, docs/specs/*.md. Cover, each grounded in the actual code with real type/function names: (1) the two rendering planes (cell plane + N pixel protocols, capability detection handshake); (2) the property system — lazy dirty-tracking graph, sources vs computeds, dependency recording during evaluation, read-vs-subscription distinction, contrast with WPF eager notification; (3) the component model — Component/Container/Base, measure-arrange sandwich (MeasureChild/ArrangeChild), Layout/FrameworkElement properties, Grid star sizing, attached properties; (4) the Composer — per-component paint nodes, discovered AffectsRender, damage semantics, why containers do not pre-clear, layout-outside-eval-context; (5) the input system — unified Event stream, focus manager + FocusState damage pattern, routed bubbling, KeyBindings as attachments, mouse hit-testing/hover/click synthesis; (6) markup — three loading tiers (dev watcher / embed.FS / future gooey gen), fs.FS seam, binding DSL with lvalue semantics, UserControl context isolation + attribute hand-off, Include markup-only controls; (7) forward pointers to the specs (x:Property, remote handlers/Temporal) clearly marked as designed-not-built. Write in the repo's documentation voice: prose-first, real code excerpts, honest about POC limits.`,
  },
  {
    file: 'docs/getting-started.md',
    brief: `Write ${REPO}/docs/getting-started.md — the hands-on HOWTO. Build a small app step by step, with COMPILABLE code at each step verified against the real API (read component.go, components/, composer.go, prop/prop.go, markup/markup.go, markup/usercontrol.go, input.go, term/term.go, term/keys.go, input/*.go and mirror cmd/statedemo/main.go for the canonical host loop). Steps: (1) hello world in pure Go composition (tree + Composer + Flush + event loop skeleton); (2) make it live: sources, computeds, Text bound via Content property; (3) move the UI to markup: .gooey file, Context (Values/Styles), Load, hot reload with Watch; (4) interactivity: Button + Command, KeyBinding, focus (tab), a custom component implementing Component+Base+FocusState+HandleKey; (5) componentize: UserControl with attribute hand-off, and the markup-only Include variant; (6) where to go next (demos, architecture doc, specs). Every code block must use APIs that exist RIGHT NOW - verify each call you write against the source; if unsure, check how cmd/statedemo or cmd/reader does it. Include the standard host-loop boilerplate exactly once, explained line by line, then reference it.`,
  },
  {
    file: 'docs/markup-reference.md',
    brief: `Write ${REPO}/docs/markup-reference.md — the complete markup language reference. READ FIRST: markup/markup.go, markup/usercontrol.go, markup/usercontrol_test.go, layout.go, components/, input.go plus every .gooey file under cmd/ (they are the living examples). Document: root <Gooey> element; every built-in element with ALL its attributes (Border: Title/Style; Grid: Rows/Cols syntax Auto|N|w*; VStack/HStack: Gap; Text: Style/Bold + text content; Button: Content/Click/Style; Image; KeyBinding: Gesture/Command and its attachment/scoping semantics); the universal layout attributes (Width, Height, Margin 1/2/4-value, HAlign/VAlign values, Visibility values, Grid.Row/Col/RowSpan/ColSpan attached syntax); the binding DSL ({{.Path}} lvalue semantics, mixed literal+binding text content, event bindings {{.Cmd}} vs bare-name handlers, what resolves where); Styles (named lookup from Context - be honest it is not a styling system yet); custom component registration; UserControl instantiation and the attribute hand-off contract; Include / ctx.Includes convention resolution; Name= and Find; gesture syntax for KeyBinding (read ParseGesture in input/ for the exact accepted forms). End with a short "designed, not yet implemented" section: x:Property declarations, xmlns handler namespaces, DataTemplates - one line each pointing at docs/specs/. Use real examples lifted from the cmd/ .gooey files.`,
  },
]

phase('Record')
// Recorders and writers are fully independent — one big parallel wave,
// each assigned to its phase group explicitly.
const wave = await parallel([
  ...RECORDERS.map(r => () => agent(
    `${r.brief}\n${RECORDING_RULES}\nReport honestly: if a beat did not land on screen, re-choreograph and re-record (up to 3 attempts) before reporting ok=false.`,
    { label: `record:${r.demo}`, phase: 'Record', schema: GIF_SCHEMA },
  )),
  ...WRITERS.map(w => () => agent(
    `${w.brief}\nRepo: ${REPO} (do not commit; leave git state to the caller). Write the file with your file tools. Markdown, no emoji, sentence-case headings. Link between docs with relative paths (architecture.md, getting-started.md, markup-reference.md, demos.md which will exist, ../README.md, specs/<file>). GIFs live in docs/media/demos (media/demos/<name>.gif from docs/).`,
    { label: `write:${w.file}`, phase: 'Write', schema: DOC_SCHEMA },
  )),
])

const gifs = wave.slice(0, RECORDERS.length).filter(Boolean)
const docs = wave.slice(RECORDERS.length).filter(Boolean)

// Catalog order — the narrative order docs/demos.md presents, which is
// not the order the recorders finish in.
const ORDER = ['demo', 'propdemo', 'logview', 'markuplog', 'finder', 'reader', 'statedemo']
const byOrder = (a, b) =>
  ORDER.indexOf(a.gif.replace(/.*\//, '').replace('.gif', '')) -
  ORDER.indexOf(b.gif.replace(/.*\//, '').replace('.gif', ''))

phase('Assemble')
const catalog = await agent(
  `Write ${REPO}/docs/demos.md — the demo catalog. One section per demo in this order: probe/demo, propdemo, logview, markuplog, finder, reader, statedemo. Data (from just-completed recordings; trust it): ${JSON.stringify([...gifs].sort(byOrder))}. For each: embed the GIF with a relative path from docs/ (media/demos/<name>.gif), the headline, the beats as a short narrated walkthrough, the run command, the key map, and one sentence on which framework subsystem it exercises (read the demo's main.go header comments in ${REPO}/cmd/<demo>/ for accurate framing). Also mention cmd/markupdemo does not exist — the markup demo is cmd/markuplog. Markdown, no emoji.`,
  { label: 'write:docs/demos.md', phase: 'Assemble', schema: DOC_SCHEMA },
)

const readme = await agent(
  `Rewrite ${REPO}/README.md as the project's front door. READ FIRST: the current README.md fully (PRESERVE its technical content by MOVING it: the rendering-modes finding, dependency-properties, layout, and input sections now live in docs/architecture.md — the README keeps only compressed versions with links), docs/architecture.md, docs/getting-started.md, docs/markup-reference.md, docs/demos.md, docs/specs/ (list them), and skim cmd/. Structure: (1) title + one-paragraph pitch: gooey is a POC XAML-like TUI framework for Go — retained visual tree, lazy dependency-property graph, XML markup with Go-template bindings, hot reload, UserControls, routed input with mouse support, damage-tracked rendering, sixel/kitty graphics; (2) a showcase: 2-3 best GIFs inline from docs/media/demos (reader.gif, statedemo.gif, finder.gif) with one-line captions; (3) quick start: go run ./cmd/statedemo + a minimal .gooey snippet + pointer to getting-started; (4) feature matrix vs modern XAML (rows: retained tree+measure/arrange, dependency properties, bindings, markup+hot reload, UserControls, Grid/star sizing, commands+KeyBindings, focus+mouse, styles/templates, x:Property, remote handlers - columns: gooey status [done/partial/designed/missing] with one-line notes; be strictly honest, read docs/specs to know what is designed-only); (5) demo table linking docs/demos.md with the GIF names; (6) docs index (the four docs + specs); (7) POC-honest limits paragraph (full-repaint flush, static-tree Composer, no styling system, no templates, single-file watcher polling); (8) architecture-decision one-liners with links into architecture.md. Keep it tight — the README sells and routes; the docs explain. No emoji. Do not invent features. There is an existing Input section written by another agent — its content should be reflected in the feature matrix and architecture links, and the detailed text moved/absorbed appropriately.`,
  { label: 'write:README.md', phase: 'Assemble', schema: DOC_SCHEMA },
)

phase('Verify')
const verdict = await agent(
  `You are the verification pass for freshly written gooey documentation. Repo: ${REPO} (do not commit; leave git state to the caller). Check, fixing small problems DIRECTLY with your edit tools and reporting anything larger: (1) run: cd ${REPO} && go build ./... && go test ./... — must be green (if a demo cmd fails to build, report it, do not fix framework code); (2) every relative link and image path in README.md and docs/*.md resolves to a real file (fix broken ones); (3) every GIF referenced exists in docs/media/demos and is >10KB; (4) API accuracy spot-check: pick 10 code identifiers/snippets across the docs (function names, types, markup attributes) and grep the source to confirm each exists with that exact name/signature — fix doc-side errors, never touch source; (5) consistency: the feature matrix claims in README match what docs/architecture.md says and what docs/specs mark as designed-only; (6) the getting-started code blocks: extract the FIRST complete program into ${S}/gs-check/main.go (module github.com/WonderForgeLabs/gooey is the parent — create a go.mod with a replace directive pointing at ${REPO}, or simpler: build it as a package inside ${REPO}/cmd/gscheck temporarily, go build it, then DELETE cmd/gscheck) — it must compile; fix the doc if it does not. Return a summary: checks passed, fixes applied (file:what), problems needing human attention.`,
  { label: 'verify:docs', phase: 'Verify', effort: 'high' },
)

return {
  gifs: [...gifs].sort(byOrder).map(g => ({ demo: g.gif, ok: g.ok, headline: g.headline })),
  docs: [...docs, catalog, readme].filter(Boolean).map(d => d.file),
  verification: verdict,
}