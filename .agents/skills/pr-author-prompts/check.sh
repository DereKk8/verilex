#!/usr/bin/env bash
# Refuse an Author prompts comment that a cold reader could not understand.
# Usage: check.sh <comment-file>   (exit 0 = ok, 1 = refused, reasons on stderr)
set -euo pipefail
f=${1:?usage: check.sh <comment-file>}
awk '
  function fail(m) { print "refused: " m > "/dev/stderr"; bad = 1 }
  # A label ("option B", "item 43") means nothing to a reader who never saw the question. The quote must
  # carry [option B: sentence] or [item 43: sentence]; the author words stay unchanged (like [redacted: kind]).
  function label(s, re, kind,   m, t, n) {
    while (match(s, re)) {
      m = substr(s, RSTART, RLENGTH); s = substr(s, RSTART + RLENGTH)
      gsub(/[^a-z0-9]+$/, "", m); n = split(m, t, /[^a-z0-9]+/)
      need[kind " " t[n]] = NR
    }
  }
  function endquote(   k, p, msg) {
    for (k in need) {
      if (k in res && res[k] >= 8) continue
      split(k, p, " "); msg = (k in res) ? "its resolution has " res[k] " words, at least 8 are needed" : "it has no resolution"
      fail("line " need[k] ": the quote refers to " p[1] " " toupper(p[2]) " but " msg "; find what " toupper(p[2]) " meant in the author'\''s earlier words or the task instructions and append [" p[1] " " toupper(p[2]) ": <self-contained sentence of at least 8 words>] to the quote, leaving the author'\''s words unchanged")
    }
    delete need; delete res
  }
  NR == 1 && $0 != "## Author prompts" { fail("first line must be exactly: ## Author prompts") }
  /^\*\*Why this PR exists:\*\*/ { inwhy = 1; seenwhy = 1; if (nq) fail("the Why this PR exists paragraph must come before the quotes") }
  inwhy && /^[[:space:]]*$/ { inwhy = 0 }
  inwhy { sub(/^\*\*Why this PR exists:\*\*/, ""); why = why " " $0 }
  !/^> / && inq { endquote(); inq = 0 }
  /^> / {
    nq++; inq = 1
    s = tolower($0)
    while (match(s, /\[(option|item)[[:space:]]+[a-z0-9]+[[:space:]]*:[^]]*\]/)) {
      m = substr(s, RSTART + 1, RLENGTH - 2)
      s = substr(s, 1, RSTART - 1) " " substr(s, RSTART + RLENGTH)
      k = m; sub(/[[:space:]]*:.*/, "", k); split(k, t, /[[:space:]]+/); k = t[1] " " t[2]
      sub(/^[^:]*:/, "", m); res[k] = split(m, t, /[[:space:]]+/) - (m ~ /^[[:space:]]/) - (m ~ /[[:space:]]$/)
    }
    label(s, "(^|[^a-z0-9_])option[[:space:]]+[a-e1-9]([^a-z0-9_]|$)", "option")
    label(s, "(^|[^a-z0-9_])(go|went|going)[[:space:]]+with[[:space:]]+[a-e]([^a-z0-9_]|$)", "option")
    label(s, "(^|[^a-z0-9_])(pick|choose|chose|take)[[:space:]]+[a-e]([^a-z0-9_]|$)", "option")
    label(s, "^>[[:space:]]*[a-e][[:space:]]*$", "option")
    label(s, "(^|[^a-z0-9_])item[[:space:]]+[0-9]+([^a-z0-9_]|$)", "item")
    label(s, "^>[[:space:]]*[0-9]+\\.[[:space:]]*[a-e]([^a-z0-9_]|$)", "option")
    q = $0; n = 0; short = 0
    while (match(q, /(^|[[:space:]])[0-9]+\.[[:space:]]+[^0-9]*/)) {
      item = substr(q, RSTART, RLENGTH); n++
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", item); if (split(item, w, /[[:space:]]+/) <= 4) short++
      q = substr(q, RSTART + RLENGTH)
    }
    if (n >= 2 && short >= 1) fail("quote " nq " is a numbered list of answers to questions the reader never sees; state the decisions in the Why paragraph and drop or replace the fragment")
  }
  END {
    if (inq) endquote()
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
