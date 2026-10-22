#!/usr/bin/env bash
# Refuse an Author prompts comment that a cold reader could not understand.
# Usage: check.sh <comment-file>   (exit 0 = ok, 1 = refused, reasons on stderr)
set -euo pipefail
f=${1:?usage: check.sh <comment-file>}
awk '
  function fail(m) { print "refused: " m > "/dev/stderr"; bad = 1 }
  # Content-word overlap is a relevance warning, not a semantic judgment. Exclude
  # session-wide goals so "land all active work" cannot justify an unrelated quote.
  function content(s, out,   n, a, i, w, count) {
    delete out
    s = tolower(s); gsub(/\[redacted:[^]]*\]/, " ", s)
    gsub(/[^a-z0-9]+/, " ", s); n = split(s, a, /[[:space:]]+/)
    for (i = 1; i <= n; i++) {
      w = a[i]
      if (length(w) < 4 || w in stop) continue
      sub(/s$/, "", w)
      if (!(w in out)) { out[w] = 1; count++ }
    }
    return count
  }
  # Known overloaded Verilex terms. Definitions belong in the Why, before quotes.
  # Backticks name literal commands/paths, not unexplained prose terminology.
  function jargon(s, quote,   re, term, tail, def, key, words, a) {
    s = tolower(s); gsub(/`[^`]*`/, " ", s)
    gsub(/(author.s|their|own|your|my) (own )?words|plain words/, " ", s)
    re = "(^|[^a-z0-9_])(canary guards?|onboarding evidence|proof stamps?|drift-suspect|words?|claims?|frames?|brains?|chains?|onboarding|admission|admitted|provisional|inconclusive)([^a-z0-9_]|$)"
    while (match(s, re)) {
      term = substr(s, RSTART, RLENGTH); sub(/^[^a-z0-9]+/, "", term); sub(/[^a-z0-9]+$/, "", term)
      tail = substr(s, RSTART + RLENGTH - 1)
      key = term; sub(/s$/, "", key)
      if (!(key in defined)) {
        def = tail; sub(/^[[:space:]]*\(/, "", def); sub(/\).*/, "", def)
        words = split(def, a, /[[:space:]]+/)
        if (!quote && tail ~ /^[[:space:]]*\([^)]*\)/ && words >= 3) defined[key] = 1
        else fail("define Verilex term \"" term "\" on first use in the Why as " term " (plain definition of at least three words)")
      }
      s = tail
    }
  }
  BEGIN {
    n = split("about above active agent agents all also and are asked author because before being both branch can change changes check checks concrete defect defects each every exists failure failures first fix fixed fixes fixing for from goal goals had has have immediately into issue its just land landed landing main make makes merge merged most needs next only our pass please prompt prompts pull reason request requested required requires review reviews running safely safe safety session should some task tasks test tests than that the their them then there these they this those through validation verilex want wanted wants what when where which while will with work working would your", a, " ")
    for (i = 1; i <= n; i++) stop[a[i]] = 1
  }
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
  function endquote(   k, p, msg, pad, s, o, m, t, B, E, lines, n, i, ln) {
    quotes[++quote_count] = blk
    o = blk; s = tolower(o)
    while (match(s, /\[(option|item)[[:space:]]+[a-z0-9]+[[:space:]]*:([^][]|\[[^]]*\])*\]/)) {
      m = substr(s, RSTART + 1, RLENGTH - 2)
      # keep the newlines, so a label after a wrapped resolution reports its own line
      pad = substr(s, RSTART, RLENGTH); gsub(/[^\n]/, "", pad); pad = " " pad
      o = substr(o, 1, RSTART - 1) pad substr(o, RSTART + RLENGTH)
      s = substr(s, 1, RSTART - 1) pad substr(s, RSTART + RLENGTH)
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
  /^[[:space:]]*(\*\*)?(Context|Background)(\*\*)?[[:space:]]*:/ { fail("do not add a Context or Background label or paragraph; put facts the reader needs into the Why this PR exists paragraph") }
  /^> [[:space:]]*(\*\*)?(Context|Background)(\*\*)?[[:space:]]*:/ { fail("do not quote a Context or Background pointer; put orchestrator briefing into the Why paragraph and quote only the author") }
  /^\*\*Why this PR exists:\*\*/ { inwhy = 1; seenwhy = 1; if (nq) fail("the Why this PR exists paragraph must come before the quotes") }
  inwhy && /^[[:space:]]*$/ { inwhy = 0; if (seenwhy) between = 1 }
  inwhy { sub(/^\*\*Why this PR exists:\*\*/, ""); why = why " " $0 }
  between && /^> / { between = 0 }
  between && !/^[[:space:]]*$/ { fail("only the Why paragraph and author quotes belong in the comment; put background into the Why paragraph, not a separate block") }
  !/^> / && inq { endquote(); inq = 0; pastq = 1 }
  pastq && !/^> / && !/^[[:space:]]*$/ && !inwhy { fail("only the Why paragraph and author quotes belong in the comment") }
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
      # A finite action vocabulary plus topic words catches goal-only paragraphs.
      # This does not establish that the stated problem or change is truthful.
      if (l !~ /(^|[^a-z])(add(s|ed)?|remov(e|es|ed)|mov(e|es|ed)|keep(s)?|preserv(e|es|ed)|replac(e|es|ed)|report(s|ed)?|refus(e|es|ed)|reject(s|ed)?|accept(s|ed)?|count(s|ed|ing)?|rebuilt|rebuild(s)?|implement(s|ed)?|provid(e|es|ed)|carry|ties|list(s|ed)?|detect(s|ed)?|writ(e|es)|read(s)?|clean(s|ed)?|scan(s|ned)?|show(s)?|stop(s|ped)?|fix(es|ed)?)([^a-z]|$)/ || content(why, topic) < 2)
        fail("Why must name a concrete behavior or problem, with an action (for example adds, removes, preserves, refuses or fixes) and at least two topic words, not only a shared goal")
      jargon(why, 0)
      for (i = 1; i <= quote_count; i++) {
        content(quotes[i], qw); related = 0
        for (k in qw) if (k in topic) related = 1
        if (!related) fail("quote " i " has no topic word in common with the Why beyond shared goals; omit unrelated prompts, or explain their PR-specific meaning in the Why")
        jargon(quotes[i], 1)
      }
      if (nq && l ~ /no pr-specific author prompt is available[.]/) fail("the no-prompt statement cannot accompany quotes")
    }
    if (!nq && tolower(why) !~ /no pr-specific author prompt is available[.]/) fail("no relevant author quote; include one or state in the Why: No PR-specific author prompt is available.")
    exit bad
  }
' "$f"
