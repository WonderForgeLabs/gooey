#!/bin/sh
# Does the mirror's assertion discriminate?
#
# The whole point of #198 is that the old check passed in situations that
# meant no review existed, so a suite here that only proves "it exits 0 on a
# good review" would repeat the original mistake at one remove. Every case
# below that expects 0 has a near-miss twin that expects 1, and the two
# differ by exactly one field.
#
# The API is stubbed by a fake `gh` on PATH that answers from fixtures keyed
# by the request path, returning the same JSON shapes the real endpoints do —
# so the jq expressions and the URL parsing are exercised, not bypassed.
#
#   sh .github/scripts/assert-review-rendered_test.sh

set -u
here=$(cd "$(dirname "$0")" && pwd)
S="$here/assert-review-rendered.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/gooey-mirror.XXXXXX")
trap 'rm -rf "$work"' EXIT INT TERM
pass=0; fail=0

HEAD=71855ce0336f75cb6771a095ef9cf393ed15a8ae
OLD=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa

# The three comment shapes seen in the wild, verbatim in structure.
rendered="**Claude finished @ElanHasson's task in 4m 51s** —— [View job](https://github.com/WonderForgeLabs/gooey/actions/runs/31857725687)

---
### Review
- [x] Read diff against \`origin/main\`
- [x] Post final review

**Review complete** — no blocking issues."

unfinished="**Claude finished @ElanHasson's task in 1m 51s** —— [View job](https://github.com/WonderForgeLabs/gooey/actions/runs/31857725687)

---
### Review
- [x] Gather context
- [ ] Post final review"

aborted="**Claude encountered an error after 2s** —— [View job](https://github.com/WonderForgeLabs/gooey/actions/runs/31857725687)

---
I'll analyze this and get back to you."

nolink="A human wrote this comment and it mentions no job at all."

# The sticky is edited IN PLACE, and while the work is in flight it says
# `View job run` — one word different — with a /job/<id> suffix on the URL.
# 79 comments in this repo carry that form. Selecting only the finished label
# made an in-progress review invisible, so it fell to "no review comment
# exists": right verdict, wrong reason. Found in review of #275.
inprogress="**Claude is working…** —— [View job run](https://github.com/WonderForgeLabs/gooey/actions/runs/31857725687/job/94357376955)

---
### Review
- [x] Gather context
- [ ] Read diff against \`origin/main\`
- [ ] Post final review"

# A finished review whose FINDINGS cite another CI run. The link that decides
# which commit was reviewed must be the sticky's own job link, not whichever
# run URL happens to appear last in the prose.
citesanother="**Claude finished @ElanHasson's task in 4m 51s** —— [View job](https://github.com/WonderForgeLabs/gooey/actions/runs/31857725687)

---
### Review
- [x] Post final review

**Review complete** — the flake reproduces in https://github.com/WonderForgeLabs/gooey/actions/runs/99999999999 as well."

# mkgh writes a fake gh whose answers are: the PR's head sha, the comment
# list, and the run's head sha.
#
# Run 31857725687 is the sticky's own job — it answers <run-head-sha>. EVERY
# OTHER run id answers a decoy SHA, which is what makes "findings citing
# another run" a real test: if the wrong URL is picked, the SHA comparison
# fails. With a stub that answered the same SHA for any run, that case passed
# no matter which link won and proved nothing.
STICKYRUN=31857725687
DECOY=dddddddddddddddddddddddddddddddddddddddd
mkgh() { # mkgh <pr-head-sha> <run-head-sha> <comment-body...>
  ph=$1; rh=$2; shift 2
  printf '%s' "$*" > "$work/body.txt"
  cat > "$work/gh" <<EOF
#!/bin/sh
case "\$2" in
  */pulls/*)        printf '{"head":{"sha":"%s"}}' "$ph" ;;
  */issues/*/comments*)
    if [ -s "$work/body.txt" ]; then
      jq -Rs '[{body: .}]' < "$work/body.txt"
    else
      printf '[]'
    fi ;;
  */actions/runs/$STICKYRUN) printf '{"head_sha":"%s"}' "$rh" ;;
  */actions/runs/*)          printf '{"head_sha":"%s"}' "$DECOY" ;;
  *) echo "unstubbed: \$2" >&2; exit 64 ;;
esac
EOF
  chmod +x "$work/gh"
}

# t checks the exit code. tm also checks WHICH guard produced it.
#
# The distinction is not pedantry — it is a real hole this suite had. The
# missing-comment guard could be deleted outright and every case stayed
# green, because an absent comment then falls through to the no-job-link
# guard and exits 1 for a different reason. Exit-code-only assertions cannot
# see that, which is the same shape of blindness #198 is about: the check
# passed, so nobody asked what it checked.
t() { # t <name> <want-exit> <result> <pr-head> <run-head> <body>
  tm "$1" "$2" "$3" "$4" "$5" "$6" ""
}

tm() { # tm <name> <want-exit> <result> <pr-head> <run-head> <body> <want-msg>
  name=$1; want=$2; res=$3; ph=$4; rh=$5; body=$6; msg=${7:-}
  mkgh "$ph" "$rh" "$body"
  GH="$work/gh" sh "$S" WonderForgeLabs/gooey 258 "$res" >"$work/out" 2>&1
  rc=$?
  if [ "$rc" -ne "$want" ]; then
    fail=$((fail+1)); printf '  FAIL %-52s exit=%s want=%s\n' "$name" "$rc" "$want"
    sed 's/^/       /' "$work/out"
    return
  fi
  if [ -n "$msg" ] && ! grep -qF "$msg" "$work/out"; then
    fail=$((fail+1)); printf '  FAIL %-52s exit=%s but not for the stated reason\n' "$name" "$rc"
    printf '       wanted the message to contain: %s\n' "$msg"
    sed 's/^/       /' "$work/out"
    return
  fi
  pass=$((pass+1)); printf '  ok   %-52s exit=%s\n' "$name" "$rc"
}

echo "== the call's own result =="
t "review call failed -> 1"            1 failure   "$HEAD" "$HEAD" "$rendered"
t "review call cancelled -> 1"         1 cancelled "$HEAD" "$HEAD" "$rendered"

echo "== a rendered review for this head =="
# The positive control. Without it every case below would pass against a
# script that always exited 1.
t "success + rendered + same head -> 0" 0 success  "$HEAD" "$HEAD" "$rendered"

echo "== the bypass #198 was filed for =="
# THE case. A label event skips every internal job, so `skipped` arrives with
# no new review — and that is fine ONLY if an existing review covers this
# head. Same input, one field different, opposite answers.
t "skipped + review covers this head -> 0" 0 skipped "$HEAD" "$HEAD" "$rendered"
t "skipped + review covers an OLDER head -> 1" 1 skipped "$HEAD" "$OLD" "$rendered"
tm "skipped + no review comment at all -> 1" 1 skipped "$HEAD" "$HEAD" "" "No review comment"

echo "== ran, but rendered no verdict =="
t "aborted before the first turn -> 1"  1 success  "$HEAD" "$HEAD" "$aborted"
t "checklist left unfinished -> 1"      1 success  "$HEAD" "$HEAD" "$unfinished"

echo "== the sticky's two spellings, and its own link =="
# Still red — an in-progress review has not reviewed anything yet — but for
# the RIGHT reason and with the right message. The verdict was already
# correct before this case existed; what it pins is the diagnosis.
tm "in-progress sticky (\`View job run\`) -> 1" 1 success "$HEAD" "$HEAD" "$inprogress" "never finished"
# The near-miss twin: a run URL in the findings must not outrank the
# sticky's own job link when deciding which commit was reviewed. The stub
# answers with $HEAD for whatever run is asked about, so this passing means
# the pattern matched the sticky link, not the later bare URL.
t  "findings citing another run -> 0"      0 success "$HEAD" "$HEAD" "$citesanother"

echo "== a comment that is not a review =="
# A human comment is not a review, and must not be mistaken for the newest
# one. This reports "No review comment" rather than "carries no job link"
# because the selector DEFINES a review comment as one carrying a job link —
# so a comment without one is not the newest review, it is not a review. The
# message assertion is what established that; with only the exit code both
# readings looked identical, and the wrong one was written down first.
tm "a human comment is not a review -> 1" 1 success "$HEAD" "$HEAD" "$nolink" "No review comment"

echo
if [ "$fail" -eq 0 ]; then
  echo "REVIEW-MIRROR: $pass passed, 0 failed"
else
  echo "REVIEW-MIRROR: $pass passed, $fail FAILED"
fi
[ "$fail" -eq 0 ]
