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
# Selected by CONTENT (the block that discovers go.mod files), not by
# position, so reordering the Verify section does not silently select the
# wrong block. Zero or two matches is a hard error rather than a guess.
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
      echo "verify.sh: the loop's last line is not a verdict: $verdict" >&2
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
