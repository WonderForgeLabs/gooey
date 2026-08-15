#!/bin/sh
# PostToolUse / Write|Edit|MultiEdit|NotebookEdit -- ADVISORY.
#
# Nothing in this repo checks formatting. ci.yml runs `go vet ./...`, which
# says nothing about gofmt; no test in the tree invokes gofmt; the only
# mentions of it in .github/ are `Bash(gofmt:*)` in the two Claude bots'
# allowed_tools. So the ONLY thing standing between an unformatted file and
# main is a reviewer noticing -- which is a thing the merge gate has failed a
# PR over, i.e. the cost lands at the end of the loop rather than at the edit.
#
# Advisory, not blocking: the file is already written by the time PostToolUse
# runs, so there is nothing to block. The point is that the model learns about
# it now instead of from a review comment forty minutes later.
#
# Re-derive the claim in this comment rather than trusting it:
#   command grep -rn gofmt .github/ ; command grep -rln gofmt --include='*_test.go' .

set -u
dir=$(dirname "$0")
. "$dir/lib.sh"

hook_read_input

f=$(hook_field 'tool_input.file_path')
[ -n "$f" ] || exit 0
case "$f" in *.go) ;; *) exit 0 ;; esac
[ -f "$f" ] || exit 0

hook_in_gooey "$(dirname "$f")" || exit 0

command -v gofmt >/dev/null 2>&1 || exit 0

# gofmt -l prints the name of a file whose formatting differs. Empty means
# clean. A syntax error goes to stderr with a non-zero status and no name --
# that is the compiler's job to report, not this hook's.
out=$(gofmt -l "$f" 2>/dev/null) || exit 0
[ -n "$out" ] || exit 0

hook_warn "\`$f\` is not gofmt-clean.

  gofmt -w $f

Nothing mechanical catches this: ci.yml runs \`go vet ./...\`, which does not
check formatting, and no test in the repo invokes gofmt. The next thing to
notice is the PR review."

hook_emit_advisories PostToolUse
