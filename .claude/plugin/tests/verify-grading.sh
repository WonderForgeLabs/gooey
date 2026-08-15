#!/bin/sh
# Does verify.sh's verdict grading actually discriminate?
#
# The script's whole job is to convert a loop that CANNOT set a useful exit
# status -- the pipe puts its body in a subshell -- into one that can. So the
# thing to test is not "does it run go", it is "does it grade the verdict
# line, and does it fail SHUT when there is no verdict at all".
#
# Five fake repos, identical except for what their CLAUDE.md loop prints. No
# Go is compiled; the loop is a stub. That is deliberate -- a test that ran
# the real suite would take ten minutes and would prove nothing extra about
# the grading.
#
#   sh .claude/plugin/tests/verify-grading.sh

set -u
here=$(cd "$(dirname "$0")" && pwd)
V="$(dirname "$here")/scripts/verify.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/gooey-vgrade.XXXXXX")
trap 'rm -rf "$work"' EXIT INT TERM
pass=0; fail=0

# A fake repo whose CLAUDE.md has the two fenced blocks the real one has:
# a `go vet` block first (which the selector must NOT pick) and a discovery
# block second, whose last line we control.
mk() {
  d=$1; rm -rf "$d"; mkdir -p "$d"
  printf 'module github.com/WonderForgeLabs/gooey\n' > "$d/go.mod"
  {
    printf '# fake\n\n## Verify\n\n```sh\ngo vet ./...\n```\n\n'
    printf '```sh\n# discovery: -name go.mod\nfind . -name go.mod > /dev/null\n'
    printf 'echo "%s"\n```\n' "$2"
  } > "$d/CLAUDE.md"
}

t() { # t <name> <loop-last-line> <want-exit>
  mk "$work/r" "$2"
  CLAUDE_PROJECT_DIR="$work/r" sh "$V" --nested-only >"$work/out" 2>&1
  rc=$?
  if [ "$rc" -eq "$3" ]; then
    pass=$((pass+1)); printf '  ok   %-44s exit=%s\n' "$1" "$rc"
  else
    fail=$((fail+1)); printf '  FAIL %-44s exit=%s want=%s\n' "$1" "$rc" "$3"
    sed 's/^/       /' "$work/out"
  fi
}

e() { # e <name> <claude.md-body> <want-exit>
  d="$work/x"; rm -rf "$d"; mkdir -p "$d"
  printf 'module github.com/WonderForgeLabs/gooey\n' > "$d/go.mod"
  printf '%b' "$2" > "$d/CLAUDE.md"
  CLAUDE_PROJECT_DIR="$d" sh "$V" --print >/dev/null 2>&1
  rc=$?
  if [ "$rc" -eq "$3" ]; then
    pass=$((pass+1)); printf '  ok   %-44s exit=%s\n' "$1" "$rc"
  else
    fail=$((fail+1)); printf '  FAIL %-44s exit=%s want=%s\n' "$1" "$rc" "$3"
  fi
}

echo "== grading the verdict line =="
t "green verdict -> 0"                    'all nested modules green'    0
t "FAILED verdict -> 1"                   'FAILED: handlers/exec'       1
t "no verdict at all -> 1 (fails SHUT)"   'some go output scrolled by'  1

echo "== selecting the block =="
# The discrimination arm for the selector: a file whose ONLY fenced block is
# the `go vet` one. A selector that took "the first sh block" would happily
# return it and this case would pass with the wrong answer.
e "only a non-discovery block -> error"   '# f\n\n```sh\ngo vet ./...\n```\n' 65
e "no fenced block at all -> error"       '# f\n\nnothing here\n'             65
e "TWO discovery blocks -> error"         '# f\n\n```sh\n# -name go.mod\n:\n```\n```sh\n# -name go.mod\n:\n```\n' 65
# The positive control: one discovery block, correctly found. Without this,
# every case above would pass against a verify.sh that always exited 65.
e "exactly one discovery block -> 0"      '# f\n\n```sh\ngo vet ./...\n```\n```sh\n# -name go.mod\n:\n```\n' 0

echo
if [ "$fail" -eq 0 ]; then
  echo "VERIFY-GRADING: $pass passed, 0 failed"
else
  echo "VERIFY-GRADING: $pass passed, $fail FAILED"
fi
[ "$fail" -eq 0 ]
