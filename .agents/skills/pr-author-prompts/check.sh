#!/usr/bin/env bash
# Refuse an Author prompts comment that a cold reader could not understand.
# Usage: check.sh <comment-file>   (exit 0 = ok, 1 = refused, reasons on stderr)
set -euo pipefail
f=${1:?usage: check.sh <comment-file>}
awk '
  function fail(m) { print "refused: " m > "/dev/stderr"; bad = 1 }
  NR == 1 && $0 != "## Author prompts" { fail("first line must be exactly: ## Author prompts") }
  /^\*\*Why this PR exists:\*\*/ { inwhy = 1; seenwhy = 1; if (nq) fail("the Why this PR exists paragraph must come before the quotes") }
  inwhy && /^[[:space:]]*$/ { inwhy = 0 }
  inwhy { sub(/^\*\*Why this PR exists:\*\*/, ""); why = why " " $0 }
  /^> / {
    nq++
    q = $0; n = 0; short = 0
    while (match(q, /(^|[[:space:]])[0-9]+\.[[:space:]]+[^0-9]*/)) {
      item = substr(q, RSTART, RLENGTH); n++
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", item); if (split(item, w, /[[:space:]]+/) <= 4) short++
      q = substr(q, RSTART + RLENGTH)
    }
    if (n >= 2 && short >= 1) fail("quote " nq " is a numbered list of answers to questions the reader never sees; state the decisions in the Why paragraph and drop or replace the fragment")
  }
  END {
    if (!seenwhy) fail("missing **Why this PR exists:** paragraph (the problem, the intended outcome, and the context, readable without the conversation)")
    else {
      words = split(why, w, /[[:space:]]+/) - 1
      if (words < 30) fail("Why this PR exists has " words " words; a cold reader needs the problem, the intended outcome and the context (at least 30 words)")
      w1 = why; sub(/^[[:space:]]+/, "", w1)
      if (w1 !~ /^The author (wants|wanted|asked|asks|needs|needed|decided|chose|requested) /) fail("Why this PR exists must start from the author'\''s own desire, for example: The author wants ... (derive it from the author'\''s prompts, not from your diagnosis)")
      l = tolower(why)
      if (l ~ /as discussed|see above|the above|this session|that report|these decisions|those answers/) fail("Why this PR exists points at context the reader does not have; say it instead")
    }
    if (!nq) fail("no author quote; keep at least one prompt in the author'\''s own words")
    exit bad
  }
' "$f"
