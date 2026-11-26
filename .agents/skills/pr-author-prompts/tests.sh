#!/usr/bin/env bash
# Exercise the public checker on real comments and controlled cold-read failures.
set -euo pipefail
cd "$(dirname "$0")/../../.."
check="$PWD/.agents/skills/pr-author-prompts/check.sh"
fixtures="$PWD/.agents/skills/pr-author-prompts/testdata"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fails=0
expect() {
  local want=$1 name=$2 file=$3 reason=${4:-} got
  if "$check" "$file" >"$work/out" 2>"$work/err"; then got=ok; else got=refused; fi
  if [ "$got" != "$want" ] || { [ -n "$reason" ] && ! grep -Fq "$reason" "$work/err"; }; then
    printf 'FAIL %s: want %s (%s), got %s\n' "$name" "$want" "$reason" "$got"
    cat "$work/err"
    fails=$((fails + 1))
  fi
}
comment() { printf '## Author prompts\n\n**Why this PR exists:** %s\n\n%s\n' "$1" "$2" >"$work/comment.md"; }
why='The author wants failure messages to explain what went wrong without opening another file. A long path hides the reason after a 300-character cut. This PR moves the reason before the path and adds a regression test.'
expect refused live-89 "$fixtures/verilex-89-before.md" 'define Verilex term "canary guard"'
expect refused live-91 "$fixtures/verilex-91-before.md" 'no topic word in common'
expect refused live-84 "$fixtures/verilex-84-before.md" 'no topic word in common'
expect ok rewritten-89 "$fixtures/verilex-89-after.md"
# These historical comments remain accepted in full, not only for label syntax.
expect ok existing-good-25 scripts/testdata/author-prompts/verilex-25.md
expect ok existing-good-26 scripts/testdata/author-prompts/verilex-26.md
comment "$why" '> keep the reason before a long path'
expect ok concrete "$work/comment.md"
comment "$why" '> Your task is going to safely land all of the active running work.'
expect refused unrelated-quote "$work/comment.md" 'no topic word in common'
comment "$why" $'> keep the reason before a long path\n\n> Your task is going to safely land all of the active running work.'
expect refused unrelated-second-quote "$work/comment.md" 'quote 2 has no topic word in common'
comment 'The author wants all active work landed safely. The author requires every task to pass review and validation before the change can be merged. The author wants every defect fixed immediately.' '> land all active work safely'
expect refused goal-only "$work/comment.md" 'concrete behavior or problem'
comment "$why" ''
expect refused missing-quote-without-explanation "$work/comment.md" 'No PR-specific author prompt is available.'
comment "$why No PR-specific author prompt is available." '> keep the reason before a long path'
expect refused contradictory-no-prompt "$work/comment.md" 'no-prompt statement cannot accompany quotes'
for term in 'canary guard' 'onboarding evidence' word words claim frame brain chain 'proof stamp' drift-suspect admitted provisional inconclusive onboarding admission; do
  comment "$why The $term needs attention." '> keep the reason before a long path'
  expect refused "undefined-$term" "$work/comment.md" 'define Verilex term'
  comment "$why The $term (an executable verification check) needs attention." '> keep the reason before a long path'
  expect ok "defined-$term" "$work/comment.md"
done
comment "$why A word (one check) records the reason." '> keep the reason before a long path'
expect refused thin-definition "$work/comment.md" 'define Verilex term'
comment "$why A word runs a check. A word (an executable verification check) records the reason." '> keep the reason before a long path'
expect refused late-definition "$work/comment.md" 'define Verilex term'
comment "$why" '> keep the word reason before a long path'
expect refused undefined-in-quote "$work/comment.md" 'define Verilex term'
comment "$why A word (an executable verification check) records the reason." '> keep the word reason before a long path'
expect ok quote-defined-in-why "$work/comment.md"
comment "$why The command is \`verilex words\`." '> keep the reason before a long path'
expect ok command-is-literal "$work/comment.md"
comment "$why The paragraph keeps the author’s own words intact." '> keep the reason before a long path'
expect ok ordinary-words "$work/comment.md"
comment "$why The paragraph uses plain words to explain the reason." '> keep the reason before a long path'
expect ok plain-words "$work/comment.md"
# Posting must refuse before any forge call, and preserve the accepted body on
# both create and update. The fake transport observes the public script boundary.
mkdir "$work/bin"
cat >"$work/bin/gh-axi" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$GH_AXI_TEST_LOG"
if [ "$1" = api ] && [ "$2" = --paginate ]; then
  printf '%s\n' "${GH_AXI_TEST_ID:-}"
elif [ "$1" = api ] && [ "$2" = -X ] && [ "$3" = PATCH ]; then
  [ "$4" = 'repos/{owner}/{repo}/issues/comments/987' ]
  [ "$5" = --raw-field ]
  printf '%s\n' "${6#body=}" >"$GH_AXI_TEST_BODY"
elif [ "$1" = pr ] && [ "$2" = comment ] && [ "$4" = --body-file ]; then
  cp "$5" "$GH_AXI_TEST_BODY"
else
  exit 99
fi
SH
chmod +x "$work/bin/gh-axi"
export PATH="$work/bin:$PATH" GH_AXI_TEST_LOG="$work/forge.log" GH_AXI_TEST_BODY="$work/posted.md"
post="$PWD/.agents/skills/pr-author-prompts/post.sh"
if "$post" 123 "$fixtures/verilex-89-before.md" >"$work/out" 2>"$work/err"; then
  echo 'FAIL post-refuses: accepted the weak comment'; fails=$((fails + 1))
fi
if [ -e "$GH_AXI_TEST_LOG" ]; then
  echo 'FAIL post-refuses: contacted the forge'; fails=$((fails + 1))
fi
"$post" 123 "$fixtures/verilex-89-after.md" >"$work/out"
if ! cmp -s "$fixtures/verilex-89-after.md" "$GH_AXI_TEST_BODY"; then
  echo 'FAIL post-create: body differs'; fails=$((fails + 1))
fi
export GH_AXI_TEST_ID=987
"$post" 123 "$fixtures/verilex-89-after.md" >"$work/out"
if ! cmp -s "$fixtures/verilex-89-after.md" "$GH_AXI_TEST_BODY"; then
  echo 'FAIL post-update: body differs'; fails=$((fails + 1))
fi
[ "$fails" -eq 0 ] || { echo "cold-read checks: $fails failed"; exit 1; }
echo 'cold-read checks: ok'
