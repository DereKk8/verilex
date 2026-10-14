#!/usr/bin/env bash
# Refuse an Author prompts comment that a cold reader could not understand.
# Usage: check.sh <comment-file>   (exit 0 = ok, 1 = refused, reasons on stderr)
set -euo pipefail
f=${1:?usage: check.sh <comment-file>}
awk '
  function fail(m) { print "refused: " m > "/dev/stderr"; bad = 1 }
  # A label ("option B", "item 43") means nothing to a reader who never saw the question. The quote must
  # carry [option B: sentence] or [item 43: sentence]; the author words stay unchanged (like [redacted: kind]).
  # label: record each explicit "option X" (X one uppercase letter) and "item N" (N a number) in a line, and the
  # further labels listed right after it ("option B or C", "items 4, 5 and 6"). The label is matched case-sensitively,
  # so "an option a user can set" is prose. Any other way to refer to a choice the author must resolve by hand.
  function label(s, o, re, kind, ln,   base, m, tl, lbl, orig, end, t, n) {
    base = 0
    while (match(s, re)) {
      m = substr(s, RSTART, RLENGTH); sub(/[^a-z0-9]+$/, "", m); tl = length(m)
      n = split(m, t, /[^a-z0-9]+/); lbl = t[n]; end = base + RSTART - 1 + tl
      orig = substr(o, end - length(lbl) + 1, length(lbl)); s = substr(s, RSTART + tl); base = end
      if (kind == "option" && orig !~ /^[A-Z]$/) continue
      need[kind " " lbl] = ln
      while (match(s, /^([[:space:]]*,[[:space:]]*(or[[:space:]]+|and[[:space:]]+)?|[[:space:]]+(or|and)[[:space:]]+)([a-z]|[0-9]+)([^a-z0-9_]|$)/)) {
        m = substr(s, RSTART, RLENGTH); sub(/[^a-z0-9]+$/, "", m); tl = length(m)
        n = split(m, t, /[^a-z0-9]+/); lbl = t[n]; end = base + RSTART - 1 + tl
        orig = substr(o, end - length(lbl) + 1, length(lbl))
        if (kind == "option" ? orig !~ /^[A-Z]$/ : lbl !~ /^[0-9]+$/) break
        need[kind " " lbl] = ln; s = substr(s, RSTART + tl); base = end
      }
    }
  }
  # endquote: one quote is its consecutive "> " lines, so a resolution may wrap across them.
  function endquote(   k, p, msg, s, o, m, t, B, E, lines, n, i, ln) {
    o = blk; s = tolower(o)
    while (match(s, /\[(option|item)[[:space:]]+[a-z0-9]+[[:space:]]*:([^][]|\[[^]]*\])*\]/)) {
      m = substr(s, RSTART + 1, RLENGTH - 2)
      o = substr(o, 1, RSTART - 1) " " substr(o, RSTART + RLENGTH)
      s = substr(s, 1, RSTART - 1) " " substr(s, RSTART + RLENGTH)
      k = m; sub(/[[:space:]]*:.*/, "", k); split(k, t, /[[:space:]]+/); k = t[1] " " t[2]
      sub(/^[^:]*:/, "", m); gsub(/\n>/, " ", m)
      res[k] = split(m, t, /[[:space:]]+/) - (m ~ /^[[:space:]]/) - (m ~ /[[:space:]]$/)
    }
    B = "(^|[^a-z0-9_])"; E = "([^a-z0-9_]|$)"
    n = split(s, lines, "\n"); split(o, olines, "\n")
    for (i = 1; i <= n; i++) {
      ln = blkstart + i - 1; s = lines[i]; o = olines[i]
      label(s, o, B "options?[[:space:]]+[a-z]" E, "option", ln)
      label(s, o, B "items?[[:space:]]+[0-9]+" E, "item", ln)
    }
    for (k in need) {
      if (k in res && res[k] >= 8) continue
      split(k, p, " "); msg = (k in res) ? "its resolution has " res[k] " words, at least 8 are needed" : "it has no resolution"
      fail("line " need[k] ": the quote refers to " p[1] " " toupper(p[2]) " but " msg "; find what " toupper(p[2]) " meant in the author'\''s earlier words or the task instructions and append [" p[1] " " toupper(p[2]) ": <self-contained sentence of at least 8 words>] to the quote, leaving the author'\''s words unchanged")
    }
    delete need; delete res; blk = ""
  }
  NR == 1 && $0 != "## Author prompts" { fail("first line must be exactly: ## Author prompts") }
  /^\*\*Why this PR exists:\*\*/ { inwhy = 1; seenwhy = 1; if (nq) fail("the Why this PR exists paragraph must come before the quotes") }
  inwhy && /^[[:space:]]*$/ { inwhy = 0 }
  inwhy { sub(/^\*\*Why this PR exists:\*\*/, ""); why = why " " $0 }
  !/^> / && inq { endquote(); inq = 0 }
  /^> / {
    nq++; inq = 1
    if (blk == "") blkstart = NR
    blk = blk (blk == "" ? "" : "\n") $0
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
