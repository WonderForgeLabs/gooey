#!/bin/sh
# Run gooey's whole-tree verification: the root module, then every nested
# module.
#
# THIS SCRIPT CONTAINS NO COPY OF THE MODULE LIST OR OF THE DISCOVERY
# COMMAND. It extracts the loop out of CLAUDE.md and runs that. CLAUDE.md is
# the authority, `TestCLAUDEMDVerifyLoopReachesEveryNestedModule` pins it
# against a walk of the tree, and `TestCIWorkflowAndCLAUDEMDShareOneDiscovery`
# pins it against ci.yml. A second transcription of the loop in this file
# would be a third copy for those tests to not cover -- which is the exact
# defect ("a hand-maintained claim outliving what it described") the plugin
# exists to remove.
#
# Usage:  verify.sh [--nested-only|--root-only|--print]
#
# Exit 0 only when everything asked for is green. Read the exit code, not the
# scroll.

set -u

root=${CLAUDE_PROJECT_DIR:-}
if [ -z "$root" ] || [ ! -f "$root/CLAUDE.md" ]; then
  root=$(git rev-parse --show-toplevel 2>/dev/null) || {
    echo "verify.sh: not inside a git worktree and CLAUDE_PROJECT_DIR is unset" >&2
    exit 64
  }
fi
cd "$root" || exit 64

if [ ! -f CLAUDE.md ]; then
  echo "verify.sh: no CLAUDE.md at $root -- this is not the gooey repo" >&2
  exit 64
fi

mode=${1:-all}

# Pull the fenced `sh` block that does the module discovery out of CLAUDE.md.
#
# Selected by CONTENT -- the block containing `-name go.mod` -- and NOT by
# position. The Verify section already opens with a different ```sh block
# (`go vet ./...`), so "take the first one" and "take the last one" are both
# wrong today, and reordering the section would silently change which block a
# positional selector picked.
#
# ZERO OR TWO MATCHES IS A HARD ERROR, AND THAT IS THE LOAD-BEARING PART.
# Do not "simplify" it to `head -1`, `tail -1`, or a match that takes whatever
# it finds first. The whole reason this script extracts the loop instead of
# carrying a copy is that a second, drifted copy is the failure mode -- so if
# someone adds another ```sh block that also discovers go.mod, the right
# behaviour is to STOP and make a human look, not to pick one and report green
# over a loop nobody chose. A selector that silently picks would reintroduce
# exactly the defect this script exists to avoid.
#
# Both branches are covered by the grading test: zero blocks and two blocks
# each exit 65, and a run with no verdict line at all exits 1 rather than 0
# (it fails SHUT). If you change this function, re-run those.
extract_loop() {
  awk '
    /^```sh$/     { inblock = 1; buf = ""; hit = 0; next }
    /^```$/       { if (inblock && hit) { n++; blocks[n] = buf } inblock = 0; next }
    inblock       { buf = buf $0 "\n"; if ($0 ~ /-name go\.mod/) hit = 1 }
    END {
      if (n != 1) { printf "EXPECTED-1-GOT-%d\n", n > "/dev/stderr"; exit 3 }
      printf "%s", blocks[1]
    }
  ' CLAUDE.md
}

loop=$(extract_loop) || {
  echo "verify.sh: could not find exactly one go.mod-discovery block in CLAUDE.md." >&2
  echo "  The Verify section changed shape. Read CLAUDE.md and run it by hand;" >&2
  echo "  do not fall back to a module list from memory." >&2
  exit 65
}

if [ "$mode" = "--print" ]; then
  printf '%s' "$loop"
  exit 0
fi

rc=0

if [ "$mode" != "--nested-only" ]; then
  echo "=== root module: go vet ./... ==="
  go vet ./... || rc=1
  echo "=== root module: go test ./... ==="
  go test ./... || rc=1
fi

if [ "$mode" != "--root-only" ]; then
  echo "=== nested modules (discovered, not enumerated) ==="
  # The loop prints its own verdict line and leaves $fails on disk when red.
  # It does not set a useful exit status by itself -- that is why the last
  # line is the check -- so the verdict is what we grade.
  #
  # `tee`, not `out=$(...)`. This takes minutes across the whole tree, and a
  # command substitution shows nothing at all until it finishes, which reads
  # as a hang and gets killed. Streaming to the terminal AND to a file gets
  # both the progress and the verdict.
  #
  # mktemp for the same reason the loop itself uses one: /tmp is shared by
  # every worktree on this machine and this repo runs five to fifteen at once.
  log=$(mktemp "${TMPDIR:-/tmp}/gooey-verify-log.XXXXXX")
  sh -c "$loop" 2>&1 | tee "$log"
  verdict=$(tail -n 1 "$log")
  rm -f "$log"
  case "$verdict" in
    "all nested modules green") ;;
    FAILED:*) rc=1 ;;
    *)
      # Fail SHUT: no verdict means nothing here can say the tree compiled.
      # Name the likely cause, because the most common one is not a broken
      # loop at all -- it is an OLDER CLAUDE.md. The verdict lines this arm
      # grades ("all nested modules green" / "FAILED: …") were added to the
      # loop by #261; on any branch that predates it the loop ends with a
      # bare `echo "FAIL $m"` per module and prints nothing at all when
      # everything passes, so a perfectly green tree lands here.
      echo "verify.sh: the loop's last line is not a verdict: $verdict" >&2
      echo "  This is a RED, and it says nothing about whether the tree" >&2
      echo "  compiled -- only that the loop did not report." >&2
      echo "  Most likely: CLAUDE.md's Verify loop predates the one that" >&2
      echo "  prints a verdict line. Compare what it ends with:" >&2
      echo "      \"\${CLAUDE_PLUGIN_ROOT}\"/scripts/verify.sh --print | tail -n 8" >&2
      echo "  If it has no verdict, read the scroll above by hand -- and note" >&2
      echo "  that the loop cannot set an exit status of its own, which is the" >&2
      echo "  whole reason the verdict line exists." >&2
      rc=1
      ;;
  esac
fi

if [ "$rc" -eq 0 ]; then
  echo "VERIFY: green"
else
  echo "VERIFY: RED -- see above"
fi
exit "$rc"
