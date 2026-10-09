// search.js: instant search over the bundle. One index of pages, sections, commands and glossary
// terms for the palette, and BM25 over passages for answers quoted from the docs.
(function () {
  "use strict";
  const VX = (window.VX = window.VX || {});

  const STOP = new Set(
    ("a an and are as at be but by can do does did for from has have how i if in into is it its me my no not of on or " +
      "should so than that the their them then there these this those to was we were what when where which who why will with " +
      "would you your about after before also any each get got just more most much only other over same some such very").split(" ")
  );

  function stem(w) {
    if (w.length > 4 && w.endsWith("ies")) return w.slice(0, -3) + "y";
    let s = w;
    if (s.length > 5 && s.endsWith("ing")) s = s.slice(0, -3);
    else if (s.length > 4 && s.endsWith("ed")) s = s.slice(0, -2);
    else if (s.length > 4 && /(ches|shes|sses|xes)$/.test(s)) s = s.slice(0, -2);
    else if (s.length > 3 && s.endsWith("s") && !s.endsWith("ss")) s = s.slice(0, -1);
    if (s !== w && /([b-df-hj-np-tv-z])\1$/.test(s) && !/(ll|ss|zz)$/.test(s)) s = s.slice(0, -1);
    return s;
  }

  // Terms: lower case, hyphenated words kept whole and split, stop words dropped, stemmed.
  function terms(text, keepStop) {
    const out = [];
    for (const raw of String(text).toLowerCase().split(/[^\p{L}\p{N}_-]+/u)) {
      const w = raw.replace(/^-+|-+$/g, "");
      if (!w) continue;
      const parts = w.includes("-") ? [w, ...w.split("-").filter(Boolean)] : [w];
      for (const p of parts) if (keepStop || !STOP.has(p)) out.push(stem(p));
    }
    return out;
  }

  // Plain text of Markdown, for indexing and snippets.
  function plainText(md) {
    return md
      .replace(/\[([^\]]*)\]\([^)\s]+\)/g, "$1")
      .replace(/`+([^`]*)`+/g, "$1")
      .replace(/\*\*|__/g, "")
      .replace(/^\s*#{1,6}\s+/gm, "")
      .replace(/^\s*[-*+]\s+|^\s*\d+[.)]\s+/gm, "")
      .replace(/^\s*\|?[\s:|-]+\|[\s:|-]*$/gm, "")
      .replace(/\s*\|\s*/g, " · ")
      .replace(/\\(.)/g, "$1")
      .replace(/[ \t]+/g, " ")
      .replace(/(\s*·\s*){2,}/g, " · ")
      .replace(/^\s*·\s*|\s*·\s*$/gm, "")
      .trim();
  }

  let entries = [];
  let passages = [];
  let df = new Map();
  let pdf = new Map();
  let avgLen = 1;

  // build indexes the bundle once. Sections are h2 and h3 with the text under them; passages are
  // the paragraphs, list items, table rows and code blocks of every section.
  function build(bundle) {
    entries = [];
    passages = [];
    for (const page of bundle.pages) {
      entries.push(entry("page", page, null, page.title, page.section, page.summary));
      const lines = page.markdown.split("\n");
      let fence = null;
      let section = { id: null, title: page.title, level: 1, buf: [] };
      let block = [];
      let blockKind = "text";
      let hi = 0;
      const sections = [];
      const flushBlock = () => {
        const raw = block.join("\n").trim();
        if (raw) addPassage(page, section, raw, blockKind);
        block = [];
        blockKind = "text";
      };
      const flushSection = () => {
        flushBlock();
        if (section.id && section.level <= 3) sections.push(section);
      };
      for (const line of lines) {
        const f = /^\s*(```+|~~~+)/.exec(line);
        if (fence) {
          block.push(line);
          if (f && line.trim().startsWith(fence) && /^(`+|~+)$/.test(line.trim())) {
            fence = null;
            flushBlock();
          }
          continue;
        }
        if (f) {
          flushBlock();
          fence = f[1];
          blockKind = "code";
          block.push(line);
          continue;
        }
        const h = /^(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line);
        if (h) {
          flushSection();
          const head = page.headings[hi++] || { id: VX.md.slug(VX.md.plain(h[2])), text: VX.md.plain(h[2]) };
          section = { id: head.id, title: head.text, level: h[1].length, buf: [], parent: section.level < h[1].length ? section.title : null };
          continue;
        }
        section.buf.push(line);
        if (!line.trim()) { flushBlock(); continue; }
        if (/^\s*([-*+]|\d+[.)])\s+/.test(line) && block.length && !/^\s/.test(line)) flushBlock();
        if (/^\s*\|/.test(line)) {
          flushBlock();
          if (!/^\s*\|?[\s:|-]+\|[\s:|-]*$/.test(line)) addPassage(page, section, line, "row");
          continue;
        }
        block.push(line);
      }
      flushSection();
      for (const s of sections) {
        if (s.level === 1) continue;
        const path = s.level === 3 && s.parent ? `${page.title} › ${s.parent}` : page.title;
        entries.push(entry("section", page, s.id, s.title, path, plainText(s.buf.join("\n"))));
      }
    }
    commandsAndTerms(bundle);
    df = docFreq(entries.map((e) => e.all));
    pdf = docFreq(passages.map((p) => p.terms));
    avgLen = passages.reduce((n, p) => n + p.terms.length, 0) / Math.max(1, passages.length);
  }

  function entry(type, page, anchor, title, path, text) {
    const t = terms(title, true);
    const lead = type === "page" ? terms(text) : [];
    return { type, page: page.slug, anchor, title, path, text, titleTerms: t, leadTerms: lead, pathTerms: terms(path), all: new Set([...t, ...terms(path), ...terms(text)]), tf: counts(terms(text)) };
  }

  function addPassage(page, section, raw, kind) {
    const text = kind === "code" ? raw : plainText(raw);
    if (!text || text.length < 3) return;
    const head = terms(section.title);
    passages.push({ page: page.slug, pageTitle: page.title, anchor: section.id, section: section.title, raw, kind, text, terms: [...terms(text), ...head, ...head, ...terms(page.title)] });
  }

  // Commands from the commands table, and terms from the glossary, are entries of their own.
  function commandsAndTerms(bundle) {
    for (const page of bundle.pages) {
      for (const line of page.markdown.split("\n")) {
        const m = /^\|\s*`(verilex [^`]+)`\s*\|\s*(.+?)\s*\|\s*$/.exec(line);
        if (m && page.slug === "commands") {
          entries.push(entry("command", page, null, m[1].replace(/\\\|/g, "|"), "Commands", plainText(m[2])));
        }
        const g = /^\|\s*([a-z][a-z -]+?)\s*\|\s*(.+?)\s*\|\s*$/.exec(line);
        if (g && page.slug === "concepts" && g[1] !== "term") {
          entries.push(entry("term", page, "glossary", g[1], "Glossary", plainText(g[2])));
        }
      }
    }
  }

  function counts(list) {
    const m = new Map();
    for (const t of list) m.set(t, (m.get(t) || 0) + 1);
    return m;
  }

  function docFreq(sets) {
    const m = new Map();
    for (const s of sets) for (const t of new Set(s)) m.set(t, (m.get(t) || 0) + 1);
    return m;
  }

  const idf = (map, n, t) => Math.log(1 + (n - (map.get(t) || 0) + 0.5) / ((map.get(t) || 0) + 0.5));

  // query ranks entries for the palette. The last term also matches as a prefix, so results follow
  // the keystrokes.
  function query(q, limit = 24) {
    const raw = q.trim().toLowerCase();
    if (!raw) return [];
    let qt = terms(raw);
    if (!qt.length) qt = terms(raw, true);
    if (!qt.length) return [];
    const last = qt[qt.length - 1];
    const prefix = !/\s$/.test(q) && last.length >= 2;
    const n = entries.length;
    const out = [];
    for (const e of entries) {
      let score = 0;
      let hits = 0;
      for (const t of qt) {
        const w = idf(df, n, t);
        let s = 0;
        if (e.titleTerms.includes(t)) s += 10 * w;
        else if (t === last && prefix && e.titleTerms.some((x) => x.startsWith(t))) s += 7 * w;
        if (e.pathTerms.includes(t)) s += 2 * w;
        if (e.leadTerms.includes(t)) s += 4 * w;
        const tf = e.tf.get(t) || 0;
        if (tf) s += w * (1 + Math.log(tf));
        else if (t === last && prefix && t.length >= 3) {
          for (const k of e.tf.keys()) if (k.startsWith(t)) { s += 0.6 * w; break; }
        }
        if (s > 0) hits++;
        score += s;
      }
      if (!hits) continue;
      const title = e.title.toLowerCase();
      if (title === raw) score += 40;
      else if (title.startsWith(raw)) score += 22;
      else if (title.includes(raw)) score += 12;
      if (hits === qt.length) score *= 1.6;
      else score *= hits / qt.length;
      score += { page: 3, command: raw.startsWith("verilex") ? 6 : 2, term: 1.5, section: 0 }[e.type];
      out.push({ e, score });
    }
    out.sort((a, b) => b.score - a.score);
    return out.slice(0, limit).map((r) => ({ ...r.e, score: r.score, snippet: snippet(r.e.text, qt) }));
  }

  // passages returns the k passages that best answer a question, at most two from one section.
  function bestPassages(q, k = 4) {
    const qt = terms(q);
    if (!qt.length) return [];
    const n = passages.length;
    const k1 = 1.2;
    const b = 0.75;
    const scored = [];
    for (const p of passages) {
      const tf = counts(p.terms);
      let s = 0;
      let hits = 0;
      for (const t of new Set(qt)) {
        const f = tf.get(t) || 0;
        if (!f) continue;
        hits++;
        s += idf(pdf, n, t) * ((f * (k1 + 1)) / (f + k1 * (1 - b + (b * p.terms.length) / avgLen)));
      }
      if (!s) continue;
      s *= 0.6 + (0.4 * hits) / new Set(qt).size;
      if (p.kind === "code") s *= 0.85;
      scored.push({ p, s });
    }
    scored.sort((a, b2) => b2.s - a.s);
    const out = [];
    const per = new Map();
    for (const { p, s } of scored) {
      const key = p.page + "#" + p.anchor;
      if ((per.get(key) || 0) >= 2) continue;
      per.set(key, (per.get(key) || 0) + 1);
      out.push({ ...p, score: s });
      if (out.length >= k) break;
    }
    return out;
  }

  // snippet is the stretch of text around the first matching term.
  function snippet(text, qt) {
    if (!text) return "";
    const lower = text.toLowerCase();
    let at = -1;
    for (const t of qt) {
      const re = new RegExp("(^|[^\\p{L}\\p{N}])" + escapeRe(t), "u");
      const m = re.exec(lower);
      if (m && (at < 0 || m.index < at)) at = m.index + m[1].length;
    }
    if (at < 0) return text.length > 140 ? text.slice(0, 140) + "…" : text;
    const start = Math.max(0, at - 50);
    const end = Math.min(text.length, at + 110);
    return (start ? "…" : "") + text.slice(start, end) + (end < text.length ? "…" : "");
  }

  const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

  // mark wraps every word that starts with a query term in <mark>, in already escaped HTML text.
  function mark(html, q) {
    const qt = [...new Set(terms(q, false))].filter((t) => t.length >= 2);
    if (!qt.length) return html;
    const re = new RegExp("(^|[^\\p{L}\\p{N}&#;])((?:" + qt.map(escapeRe).join("|") + ")[\\p{L}\\p{N}_-]*)", "giu");
    return html.replace(/(<[^>]+>)|([^<]+)/g, (m, tag, text) => (tag ? tag : text.replace(re, "$1<mark>$2</mark>")));
  }

  VX.search = { build, query, passages: bestPassages, mark, terms, plainText, size: () => ({ entries: entries.length, passages: passages.length }) };
})();
