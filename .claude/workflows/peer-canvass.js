export const meta = {
  name: 'peer-canvass',
  description: 'Before starting work: find out what every other session is touching, and where a collision would land',
  whenToUse: 'Run BEFORE dispatching work in a repo where several Claude sessions are active. Produces a hands-off list and a safe-to-start list, from live repo state rather than from what anyone remembers.',
  phases: [
    { title: 'Audit', detail: 'worktrees, branches, dirty files, open PRs — empirically' },
    { title: 'Map', detail: 'synthesize into hands-off / safe-to-start' },
  ],
}

// Why this exists: sessions collide on files, not on intentions. Asking
// peers what they are "working on" gets you a topic; reading the tree
// gets you a path. This workflow does the second and is meant to run
// ALONGSIDE the first — the coordinator SendMessages peers directly
// (workflow agents cannot), and passes their replies in as `args.replies`
// so the map reconciles what peers SAY with what the tree SHOWS.
//
// COLLISION AVOIDANCE IS THE SMALLER HALF. A peer who owns a subsystem
// is the cheapest design review you will get, and the one most likely to
// know that the thing you are about to build already exists, or that the
// fact you are reasoning from was corrected yesterday. Message them when
// stuck, when a design has two defensible shapes, when you want someone
// to argue with, or when a claim you inherited is load-bearing enough to
// be worth checking. The map below tells you WHO to ask: whoever holds
// the paths you are about to touch has read them more recently than you.
//
// Real returns from doing this: a peer's protocol read cut one issue's
// scope in half; another's measurement overturned a design decision that
// was already written down; a third caught a claim that had gained a
// quantifier in relay. None of that surfaces from a conflict check.
//
// A CONVERSATION DOES NOT NEED AN OUTCOME. "Nothing of mine touches
// that — done here, call back if it changes" is a complete and useful
// reply, and so is ending your own thread that way. Not every exchange
// has to converge on a decision, a handoff, or an action item; some just
// establish that two parties are clear of each other. Say you are done,
// leave the door open, and stop — an exchange kept alive looking for a
// deliverable costs both sides more than the answer was worth.
//
//   Workflow({ name: 'peer-canvass' })
//   Workflow({ name: 'peer-canvass', args: { replies: '...peer summaries...' } })
//
// Read-only. Never edits, commits, or pushes.
const REPO = (typeof args === 'object' && args?.repo) || '/home/elan/repos/WonderForgeLabs/gooey'
const SLUG = (typeof args === 'object' && args?.slug) || 'WonderForgeLabs/gooey'
// Stringify a structured value rather than letting it render as
// [object Object]: `replies` is naturally a per-peer object, and a
// silent [object Object] is worse than an obvious error.
const rawReplies = (typeof args === 'object' && args?.replies) || null
const REPLIES = rawReplies == null
  ? '(none supplied — map from tree state alone, and say so)'
  : (typeof rawReplies === 'string' ? rawReplies : JSON.stringify(rawReplies, null, 2))

const RULES = `You are auditing ${REPO} (GitHub: ${SLUG}).

HARD RULES:
- READ-ONLY. No edits, no commits, no pushes, no PRs, no issues.
- \`command grep\`, not \`grep\` — plain grep honors .gitignore and silently misses .claude/worktrees/.
- \`git status --porcelain -uall\`, never plain --porcelain: it collapses an untracked DIRECTORY to one line and never descends, so a whole directory of work reads as one entry.
- A directory under .claude/worktrees/ with no .git file is an ABANDONED copy, not a worktree: invisible to both \`git worktree list\` and \`git status\`. Compare \`ls\` against \`git worktree list\` and report the difference.
- Report file:line or command output for every claim. Mark anything you did not verify as unverified.`

phase('Audit')

const TREE_SCHEMA = {
  type: 'object',
  required: ['worktrees', 'dirtyPaths', 'abandonedDirs', 'recentBranches'],
  properties: {
    worktrees: {
      type: 'array',
      items: {
        type: 'object',
        required: ['path', 'branch', 'head', 'dirtyCount', 'aheadBehind'],
        properties: {
          path: { type: 'string' }, branch: { type: 'string' }, head: { type: 'string' },
          dirtyCount: { type: 'integer' },
          aheadBehind: { type: 'string', description: 'e.g. "3 ahead, 0 behind origin/main" — compute it, do not guess' },
        },
      },
    },
    dirtyPaths: {
      type: 'array',
      description: 'every uncommitted path across every worktree, with which worktree holds it',
      items: {
        type: 'object',
        required: ['path', 'worktree'],
        properties: { path: { type: 'string' }, worktree: { type: 'string' } },
      },
    },
    abandonedDirs: { type: 'array', items: { type: 'string' } },
    recentBranches: { type: 'array', items: { type: 'string' }, description: 'branches with commits in the last 48h' },
  },
}

const TREE_PROMPT = `${RULES}

Report what is CURRENTLY in flight, from the filesystem and git — not from any narrative.

Do all of:
- \`git worktree list\`, and \`ls .claude/worktrees/\`. Any name in the second and not the first is abandoned; check for a .git file to confirm.
- For EVERY live worktree: branch, HEAD, \`git status --porcelain -uall | wc -l\`, and ahead/behind against origin/main (\`git rev-list --left-right --count origin/main...HEAD\`). Compute ahead/behind — a branch that looks ahead is often far behind.
- Every uncommitted path in every worktree, attributed to its worktree. This is the collision surface; be exhaustive rather than representative.
- \`git log --oneline --since="48 hours ago" --all\` for where work has actually been landing.`

const PR_SCHEMA = {
  type: 'object',
  required: ['prs', 'contendedFiles'],
  properties: {
    prs: {
      type: 'array',
      items: {
        type: 'object',
        required: ['number', 'title', 'branch', 'mergeable', 'files', 'blockedOn'],
        properties: {
          number: { type: 'integer' }, title: { type: 'string' }, branch: { type: 'string' },
          mergeable: { type: 'string' }, files: { type: 'array', items: { type: 'string' } },
          blockedOn: { type: 'string' },
        },
      },
    },
    contendedFiles: {
      type: 'array',
      description: 'files appearing in more than one PR, or in a PR AND dirty somewhere',
      items: {
        type: 'object',
        required: ['path', 'claimants', 'consequence'],
        properties: { path: { type: 'string' }, claimants: { type: 'string' }, consequence: { type: 'string' } },
      },
    },
  },
}

const PR_PROMPT = `${RULES}

Report every open PR and, crucially, WHICH FILES each one touches — \`gh pr view N --json files\`.

For each: number, title, branch, mergeable state, file list, and what it is blocked on (read the checks and the latest comments; a PR can be blocked by a conflict, a red check, an unrendered review, or an upstream dependency in another repo).

Then compute **contendedFiles**: any path claimed by more than one PR, or claimed by a PR while also dirty in some worktree. For each, say what actually happens if someone edits it now — "the other PR becomes unmergeable", "a rebase will conflict badly", "harmless, different sections".

A PR whose whole diff is a single file is especially fragile: note it, because an unrelated edit to that file can make it unrecoverable.`

const [treeState, prState] = await parallel([
  () => agent(TREE_PROMPT, { schema: TREE_SCHEMA, phase: 'Audit', label: 'tree-state' }),
  () => agent(PR_PROMPT, { schema: PR_SCHEMA, phase: 'Audit', label: 'pr-claims' }),
])

if (!treeState || !prState) {
  log(`WARNING: audit incomplete — tree-state=${treeState ? 'ok' : 'FAILED'}, pr-claims=${prState ? 'ok' : 'FAILED'}. The map below is built on partial data.`)
}

phase('Map')

const MAP_SCHEMA = {
  type: 'object',
  // whoToAsk is REQUIRED on purpose: the prompt itself says it is "easy
  // to skip", and an optional field that is easy to skip gets skipped.
  required: ['handsOff', 'safeToStart', 'reconciliation', 'staleClaims', 'whoToAsk'],
  properties: {
    handsOff: {
      type: 'array',
      items: {
        type: 'object',
        required: ['path', 'owner', 'why'],
        properties: { path: { type: 'string' }, owner: { type: 'string' }, why: { type: 'string' } },
      },
    },
    safeToStart: { type: 'array', items: { type: 'string' }, description: 'areas with no claimant right now' },
    reconciliation: { type: 'string', description: 'where peer reports and tree state DISAGREE — the most valuable output' },
    staleClaims: { type: 'array', items: { type: 'string' }, description: 'branches/worktrees that look active but are far behind or abandoned' },
    whoToAsk: {
      type: 'array',
      description: 'who to consult for help or design review on each area — not who to avoid',
      items: {
        type: 'object',
        required: ['area', 'peer', 'whatTheyKnow'],
        properties: {
          area: { type: 'string' },
          peer: { type: 'string', description: 'worktree/branch/PR that identifies them' },
          whatTheyKnow: { type: 'string', description: 'what they have read recently that you have not' },
        },
      },
    },
  },
}

const map = await agent(`${RULES}

Produce the hands-off map a dispatcher needs before assigning work.

TREE STATE:
${JSON.stringify(treeState, null, 2)}

PR CLAIMS:
${JSON.stringify(prState, null, 2)}

WHAT PEERS SAID THEY ARE DOING:
${REPLIES}

Produce:
- **handsOff** — concrete paths, who holds them, and what breaks if touched. Prefer specific files over whole directories; "hands off markup/" is only correct if the evidence supports it.
- **safeToStart** — areas with no claimant. Be willing to say "nothing large is safe right now".
- **reconciliation** — where the peer reports and the tree DISAGREE. This is the highest-value field: a peer saying "I'm not touching X" while X is dirty in their worktree, or claiming a branch is ahead when it is 88 behind. Name each disagreement and say which source you trust and why.
- **staleClaims** — branches or worktrees that look live but are abandoned or far behind, so a dispatcher does not route around a ghost.
- **whoToAsk** — for each significant area, which peer to CONSULT. This is the constructive half and it is easy to skip: a peer holding a path has read it more recently than whoever is about to change it, and is the cheapest available design review. Say what each one has actually looked at — "read \`session.proto\` today", "measured the back-pressure behaviour", "owns the only committed client of this API" — so a dispatcher knows who to ask about what, not merely whom to avoid.

Where you have no evidence either way, say so. An unverified "probably fine" is worse than an admitted gap.`,
  { schema: MAP_SCHEMA, phase: 'Map', label: 'conflict-map' })

log(`hands-off: ${map.handsOff.length} paths · safe: ${map.safeToStart.length} areas · disagreements: ${map.reconciliation ? 'see reconciliation' : 'none'}`)

return { map, treeState, prState }
