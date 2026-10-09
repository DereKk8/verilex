// md.js: the Markdown the docs use, rendered to HTML. Headings get GitHub's anchors, code blocks
// get a header and highlighting, diagram fences are drawn by diagram.js, and links are rewritten by
// the caller.
(function () {
  "use strict";
  const VX = (window.VX = window.VX || {});

  const ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
  const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ESC[c]);

  // GitHub's anchor for a heading, the same as Slug in site/docs.go.
  function slug(text) {
    let out = "";
    for (const ch of text.toLowerCase()) {
      if (/[\p{L}\p{N}_-]/u.test(ch)) out += ch;
      else if (ch === " ") out += "-";
    }
    return out;
  }

  // Heading text without inline Markdown, the same as plainText in site/docs.go.
  const plain = (s) => s.replace(/\[([^\]]*)\]\(([^)\s]+)\)/g, "$1").replace(/`/g, "").replace(/\*\*/g, "").replace(/\*/g, "");

  const VERDICTS = new Set(["green", "red", "inconclusive"]);

  // ---------- inline ----------

  function findTicks(s, from, k) {
    for (let i = from; i < s.length; i++) {
      if (s[i] !== "`") continue;
      let j = i;
      while (s[j] === "`") j++;
      if (j - i === k) return i;
      i = j - 1;
    }
    return -1;
  }

  // The next delimiter at or after from, skipping code spans; -1 if none.
  function findDelim(s, from, delim) {
    for (let i = from; i < s.length; i++) {
      const c = s[i];
      if (c === "\\") { i++; continue; }
      if (c === "`") {
        let k = 0;
        while (s[i + k] === "`") k++;
        const close = findTicks(s, i + k, k);
        if (close >= 0) { i = close + k - 1; continue; }
        i += k - 1;
        continue;
      }
      if (s.startsWith(delim, i)) {
        if (delim === "*" && (s[i + 1] === "*" || s[i - 1] === "*")) continue;
        return i;
      }
    }
    return -1;
  }

  function matchLink(s, i) {
    let depth = 0;
    for (let j = i; j < s.length; j++) {
      const c = s[j];
      if (c === "\\") { j++; continue; }
      if (c === "`") {
        let k = 0;
        while (s[j + k] === "`") k++;
        const close = findTicks(s, j + k, k);
        j = close >= 0 ? close + k - 1 : j + k - 1;
        continue;
      }
      if (c === "[") depth++;
      else if (c === "]" && --depth === 0) {
        if (s[j + 1] !== "(") return null;
        const close = s.indexOf(")", j + 2);
        if (close < 0) return null;
        const href = s.slice(j + 2, close).trim();
        if (/\s/.test(href)) return null;
        return { text: s.slice(i + 1, j), href, end: close + 1 };
      }
    }
    return null;
  }

  function codeSpan(code, ctx) {
    if (ctx.cell && /\s/.test(code)) return `<code class="cmd">${words(code).map((w) => `<span>${esc(w)}</span>`).join(" ")}</code>`;
    const cls = VERDICTS.has(code) ? ` class="v v-${code}"` : "";
    return `<code${cls}>${esc(code)}</code>`;
  }

  // words splits a command at the spaces outside brackets, so a table cell wraps a long command
  // between its words and bracketed groups, never inside one.
  function words(code) {
    const out = [];
    let cur = "";
    let depth = 0;
    for (const c of code) {
      if (c === "[") depth++;
      if (c === "]") depth = Math.max(0, depth - 1);
      if (c === " " && depth === 0) {
        if (cur) out.push(cur);
        cur = "";
        continue;
      }
      cur += c;
    }
    if (cur) out.push(cur);
    return out;
  }

  function inline(src, ctx) {
    let out = "";
    let text = "";
    const flush = () => {
      if (text) out += esc(text);
      text = "";
    };
    for (let i = 0; i < src.length; ) {
      const c = src[i];
      if (c === "\\" && /[\\`*_{}[\]()#+\-.!|<>~]/.test(src[i + 1] || "")) {
        text += src[i + 1];
        i += 2;
        continue;
      }
      if (c === "`") {
        let k = 0;
        while (src[i + k] === "`") k++;
        const close = findTicks(src, i + k, k);
        if (close >= 0) {
          flush();
          let code = src.slice(i + k, close).replace(/\n/g, " ");
          if (code.length > 2 && code[0] === " " && code[code.length - 1] === " " && code.trim()) code = code.slice(1, -1);
          out += codeSpan(code, ctx);
          i = close + k;
          continue;
        }
        text += "`".repeat(k);
        i += k;
        continue;
      }
      if (c === "[") {
        const m = matchLink(src, i);
        if (m) {
          flush();
          out += ctx.link(m.href, inline(m.text, ctx), m.text);
          i = m.end;
          continue;
        }
      }
      if (c === "*" && src[i + 1] === "*" && src[i + 2] && src[i + 2] !== " ") {
        const close = findDelim(src, i + 2, "**");
        if (close > i + 2) {
          flush();
          out += "<strong>" + inline(src.slice(i + 2, close), ctx) + "</strong>";
          i = close + 2;
          continue;
        }
      }
      if (c === "*" && src[i + 1] && src[i + 1] !== " " && src[i + 1] !== "*") {
        const close = findDelim(src, i + 1, "*");
        if (close > i + 1 && src[close - 1] !== " ") {
          flush();
          out += "<em>" + inline(src.slice(i + 1, close), ctx) + "</em>";
          i = close + 1;
          continue;
        }
      }
      if (c === "<") {
        const m = /^<(https?:\/\/[^\s>]+)>/.exec(src.slice(i));
        if (m) {
          flush();
          out += ctx.link(m[1], esc(m[1]), m[1]);
          i += m[0].length;
          continue;
        }
      }
      text += c;
      i++;
    }
    flush();
    return out;
  }

  // ---------- highlighting ----------

  const span = (cls, s) => `<span class="t-${cls}">${s}</span>`;

  function hlCommand(cmd) {
    // Tokens: quoted strings, env assignments, flags, pipes, comments, words.
    let out = "";
    let first = true;
    const re = /('(?:[^'])*'?|"(?:\\.|[^"\\])*"?|\s+|#.*$|\|\||&&|[|;]|[^\s'"|;]+)/g;
    let m;
    while ((m = re.exec(cmd))) {
      const t = m[0];
      if (/^\s+$/.test(t)) { out += t; continue; }
      if (t[0] === "#") { out += span("com", esc(t)); continue; }
      if (t[0] === "'" || t[0] === '"') {
        // A chain inside quotes: its pipes are the point.
        out += span("str", esc(t).replace(/\|/g, '<span class="t-pipe">|</span>'));
        first = false;
        continue;
      }
      if (t === "|" || t === "||" || t === "&&" || t === ";") { out += span("pipe", esc(t)); first = true; continue; }
      if (first && /^[A-Z_][A-Z0-9_]*=/.test(t)) {
        const eq = t.indexOf("=");
        out += span("env", esc(t.slice(0, eq))) + span("punct", "=") + span("str", esc(t.slice(eq + 1)));
        continue;
      }
      if (first) { out += span("cmd", esc(t)); first = false; continue; }
      if (/^--?[A-Za-z]/.test(t)) { out += span("flag", esc(t)); continue; }
      if (/^<[^>]+>$/.test(t) || /^\[.*\]$/.test(t)) { out += span("ph", esc(t)); continue; }
      out += esc(t);
    }
    return out;
  }

  function hlOutput(line) {
    let s = esc(line);
    s = s.replace(/^(green|red|inconclusive)(:)/, (_, v, c) => `<span class="t-v t-v-${v}">${v}</span>${c}`);
    s = s.replace(/^(plan|changed|index \w+|check|onboarded|undecided|rejected|kept)(:| )/, (_, w, c) => span("label", w) + c);
    s = s.replace(/^(verilex(?:-agent)?: )(refused|inconclusive)(:)/, (_, a, b, c) => span("dim", a) + `<span class="t-v t-v-inconclusive">${b}</span>` + c);
    s = s.replace(/^(\s+)(red|inconclusive|green|skip|run|proven|trial)(\s\s)/, (_, a, w, b) => a + (VERDICTS.has(w) ? `<span class="t-v t-v-${w}">${w}</span>` : span("label", w)) + b);
    s = s.replace(/^(\s*)(evidence|record|verify skill|decide|review|entry|requires|inputs|proven on|promise|claim|status)(:)/, (_, a, w, c) => a + span("dim", w + c));
    s = s.replace(/\b(\d{10}-[0-9a-f]{6,12})\b/g, (m) => span("id", m));
    s = s.replace(/@([0-9a-f]{12})\b/g, (_, h) => "@" + span("hash", h));
    return s;
  }

  function hlShell(code) {
    let cont = false;
    return code
      .split("\n")
      .map((line) => {
        if (/^\$ /.test(line) || cont) {
          const body = cont ? line : line.slice(2);
          cont = /\\$/.test(line);
          return (line.startsWith("$ ") ? span("prompt", "$") + " " : "") + hlCommand(body);
        }
        return hlOutput(line);
      })
      .join("\n");
  }

  function hlPlainCommands(code) {
    return code
      .split("\n")
      .map((line) => {
        const m = /^(.*?)(\s+#.*)?$/.exec(line);
        return hlCommand(m[1]) + (m[2] ? span("com", esc(m[2])) : "");
      })
      .join("\n");
  }

  function hlYaml(code) {
    return code
      .split("\n")
      .map((line) => {
        if (/^\s*---\s*$/.test(line)) return span("punct", esc(line));
        let body = line;
        let comment = "";
        let q = null;
        for (let i = 0; i < line.length; i++) {
          const c = line[i];
          if (q) { if (c === q) q = null; continue; }
          if (c === '"' || c === "'") { q = c; continue; }
          if (c === "#" && (i === 0 || /\s/.test(line[i - 1]))) {
            body = line.slice(0, i);
            comment = line.slice(i);
            break;
          }
        }
        let out = "";
        const m = /^(\s*(?:-\s+)?)([\w.@{}-]+)(:)(\s|$)(.*)$/.exec(body);
        if (m) out = esc(m[1]) + span("key", esc(m[2])) + span("punct", ":") + m[4] + yamlValue(m[5]);
        else {
          const d = /^(\s*-\s+)(.*)$/.exec(body);
          out = d ? span("punct", esc(d[1])) + yamlValue(d[2]) : yamlValue(body);
        }
        return out + (comment ? span("com", esc(comment)) : "");
      })
      .join("\n");
  }

  function yamlValue(v) {
    const t = v.trim();
    if (!t) return esc(v);
    if (/^(true|false|null|~)$/.test(t)) return span("lit", esc(v));
    if (/^-?\d+(\.\d+)?$/.test(t)) return span("num", esc(v));
    return esc(v).replace(/("[^"]*"|'[^']*')/g, (m) => span("str", m)).replace(/([[\]{},])/g, (m) => span("punct", m));
  }

  function hlJson(code) {
    return esc(code).replace(
      /(&quot;(?:\\.|(?!&quot;).)*?&quot;)(\s*:)?|\b(true|false|null)\b|(-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b)|([{}[\],])/g,
      (m, str, colon, lit, num, punct) => {
        if (str) return colon ? span("key", str) + span("punct", colon) : span("str", str);
        if (lit) return span("lit", lit);
        if (num) return span("num", num);
        return span("punct", punct);
      }
    );
  }

  function hlMarkdown(code) {
    return code
      .split("\n")
      .map((line) => {
        if (/^#{1,6}\s/.test(line)) return span("key", esc(line));
        return esc(line).replace(/`[^`]+`/g, (m) => span("str", m)).replace(/^(\s*[-*]\s)/, (m) => span("punct", m));
      })
      .join("\n");
  }

  function highlight(code, lang) {
    lang = (lang || "").toLowerCase();
    if (lang === "json") return { html: hlJson(code), label: "json" };
    if (lang === "yaml" || lang === "yml") return { html: hlYaml(code), label: "yaml" };
    if (lang === "markdown" || lang === "md") return { html: hlMarkdown(code), label: "markdown" };
    if (lang === "console") return { html: hlShell(code), label: "terminal" };
    if (lang === "bash" || lang === "sh" || lang === "shell") {
      return /^\$ /m.test(code) ? { html: hlShell(code), label: "terminal" } : { html: hlPlainCommands(code), label: "shell" };
    }
    if (!lang && /^\$ /m.test(code)) return { html: hlShell(code), label: "terminal" };
    if (!lang && code.split("\n").every((l) => !l.trim() || /^(go|scripts\/|verilex|printf|sudo|cp|cd|git|cat|chmod|export|codex|claude|ant) /.test(l.trim()))) {
      return { html: hlPlainCommands(code), label: "shell" };
    }
    return { html: esc(code), label: lang || "text" };
  }

  // ---------- blocks ----------

  const indentOf = (l) => {
    let n = 0;
    for (const c of l) {
      if (c === " ") n++;
      else if (c === "\t") n += 4 - (n % 4);
      else break;
    }
    return n;
  };
  const dedent = (l, n) => {
    let i = 0;
    let col = 0;
    while (i < l.length && col < n && (l[i] === " " || l[i] === "\t")) {
      col += l[i] === "\t" ? 4 - (col % 4) : 1;
      i++;
    }
    return l.slice(i);
  };
  const FENCE = /^(\s*)(`{3,}|~{3,})\s*([^`\s]*)[^`]*$/;
  const HEADING = /^(#{1,6})\s+(.*?)\s*#*\s*$/;
  const HR = /^\s{0,3}([-*_])(\s*\1){2,}\s*$/;
  const TABLE_SEP = /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/;

  function listItem(line) {
    const m = /^(\s*)([-*+]|(\d{1,9})[.)])(\s+|$)(.*)$/.exec(line);
    if (!m) return null;
    const spaces = m[4].length;
    const indent = indentOf(m[1]);
    return {
      indent,
      ordered: m[3] !== undefined,
      num: m[3] !== undefined ? parseInt(m[3], 10) : 1,
      content: indent + m[2].length + (spaces > 4 || spaces === 0 ? 1 : spaces),
      text: spaces > 4 ? m[4].slice(1) + m[5] : m[5],
    };
  }

  function isTable(lines, i) {
    return lines[i].includes("|") && i + 1 < lines.length && TABLE_SEP.test(lines[i + 1]) && lines[i + 1].includes("-");
  }

  function startsBlock(lines, i) {
    const l = lines[i];
    return FENCE.test(l) || HEADING.test(l) || HR.test(l) || /^\s{0,3}>/.test(l) || !!listItem(l) || isTable(lines, i);
  }

  function splitRow(line) {
    let s = line.trim();
    if (s.startsWith("|")) s = s.slice(1);
    if (s.endsWith("|") && !s.endsWith("\\|")) s = s.slice(0, -1);
    const cells = [];
    let cur = "";
    for (let i = 0; i < s.length; i++) {
      if (s[i] === "\\" && s[i + 1] === "|") { cur += "|"; i++; continue; }
      if (s[i] === "|") { cells.push(cur.trim()); cur = ""; continue; }
      cur += s[i];
    }
    cells.push(cur.trim());
    return cells;
  }

  function table(lines, i, ctx) {
    const head = splitRow(lines[i]);
    const align = splitRow(lines[i + 1]).map((c) => (/^:-+:$/.test(c) ? "center" : /-+:$/.test(c) ? "right" : ""));
    i += 2;
    const rows = [];
    while (i < lines.length && lines[i].trim() && lines[i].includes("|")) rows.push(splitRow(lines[i++]));
    const inCell = { ...ctx, cell: true };
    const cell = (tag, c, k) => {
      const a = align[k] ? ` style="text-align:${align[k]}"` : "";
      let html = inline(c, inCell);
      const v = /^(green|red|inconclusive)(?=,|$)/.exec(c);
      if (tag === "td" && v) html = `<span class="pill pill-${v[1]}">${v[1]}</span>` + inline(c.slice(v[1].length), inCell);
      return `<${tag}${a}>${html}</${tag}>`;
    };
    const wide = head.length >= 5 ? ' class="wide"' : "";
    let html = `<div class="table"><table${wide}><thead><tr>` + head.map((c, k) => cell("th", c, k)).join("") + "</tr></thead><tbody>";
    for (const r of rows) {
      html += "<tr>";
      for (let k = 0; k < head.length; k++) html += cell("td", r[k] || "", k);
      html += "</tr>";
    }
    return { html: html + "</tbody></table></div>", next: i };
  }

  function list(lines, i, ctx) {
    const first = listItem(lines[i]);
    const items = [];
    let loose = false;
    while (i < lines.length) {
      if (!lines[i].trim()) {
        let j = i;
        while (j < lines.length && !lines[j].trim()) j++;
        const n = j < lines.length && listItem(lines[j]);
        if (n && n.ordered === first.ordered && n.indent < first.content && items.length) {
          loose = true;
          i = j;
          continue;
        }
        break;
      }
      const m = listItem(lines[i]);
      if (!m || m.ordered !== first.ordered || m.indent >= first.content) break;
      const body = [m.text];
      i++;
      let blank = false;
      while (i < lines.length) {
        const l = lines[i];
        if (!l.trim()) {
          let j = i;
          while (j < lines.length && !lines[j].trim()) j++;
          if (j < lines.length && indentOf(lines[j]) >= m.content) {
            for (; i < j; i++) body.push("");
            blank = true;
            continue;
          }
          break;
        }
        if (indentOf(l) >= m.content) { body.push(dedent(l, m.content)); i++; continue; }
        if (listItem(l) || blank || startsBlock(lines, i)) break;
        body.push(l.trim()); // a lazy continuation of the item's paragraph
        i++;
      }
      items.push({ body, blank });
    }
    const tag = first.ordered ? "ol" : "ul";
    const start = first.ordered && first.num !== 1 ? ` start="${first.num}"` : "";
    let html = `<${tag}${start}>`;
    for (const it of items) {
      let inner = blocks(it.body, ctx);
      if (!loose && !it.blank) inner = inner.replace(/^<p>([\s\S]*?)<\/p>/, "$1");
      html += `<li>${inner}</li>`;
    }
    return { html: html + `</${tag}>`, next: i };
  }

  function blocks(lines, ctx) {
    let html = "";
    let i = 0;
    while (i < lines.length) {
      const line = lines[i];
      if (!line.trim()) { i++; continue; }
      let m;
      if ((m = FENCE.exec(line))) {
        const indent = m[1].length;
        const fence = m[2];
        const body = [];
        i++;
        while (i < lines.length) {
          const t = lines[i].trim();
          if (t[0] === fence[0] && t.length >= fence.length && new RegExp("^\\" + fence[0] + "+$").test(t)) { i++; break; }
          body.push(dedent(lines[i], indent));
          i++;
        }
        html += ctx.code(body.join("\n"), m[3]);
        continue;
      }
      if ((m = HEADING.exec(line))) { html += ctx.heading(m[1].length, m[2]); i++; continue; }
      if (HR.test(line)) { html += "<hr>"; i++; continue; }
      if (isTable(lines, i)) {
        const t = table(lines, i, ctx);
        html += t.html;
        i = t.next;
        continue;
      }
      if (/^\s{0,3}>/.test(line)) {
        const q = [];
        while (i < lines.length && lines[i].trim() && /^\s{0,3}>/.test(lines[i])) q.push(lines[i++].replace(/^\s{0,3}>\s?/, ""));
        html += "<blockquote>" + blocks(q, ctx) + "</blockquote>";
        continue;
      }
      if (listItem(line)) {
        const l = list(lines, i, ctx);
        html += l.html;
        i = l.next;
        continue;
      }
      const para = [line.trim()];
      i++;
      while (i < lines.length && lines[i].trim() && !startsBlock(lines, i)) para.push(lines[i++].trim());
      html += "<p>" + inline(para.join("\n"), ctx) + "</p>";
    }
    return html;
  }

  // render turns Markdown into HTML. opts:
  //   link(href, innerHTML, text) -> HTML  rewrites links (default: plain anchors)
  //   headingIds [ids]  anchors in order, from the bundle, so the site and GitHub agree
  //   skipTitle         leave out the first level-1 heading
  //   anchorHref(id)    the href of each heading's anchor link
  // It returns {html, headings: [{level, text, id}]}.
  function render(markdown, opts = {}) {
    const headings = [];
    const seen = new Map();
    let skipped = false;
    const ctx = {
      link:
        opts.link ||
        ((href, inner) => {
          const safe = /^(https?:|mailto:|#|\/|\.)/.test(href) ? href : "#";
          return `<a href="${esc(safe)}">${inner}</a>`;
        }),
      code: (code, lang) => {
        const diagram = VX.diagram && VX.diagram.render(code, lang, (s) => inline(s, ctx));
        if (diagram) return diagram;
        const h = highlight(code, lang);
        return (
          `<div class="code" data-lang="${esc(h.label)}"><div class="code-head"><span class="code-lang">${esc(h.label)}</span>` +
          `<button type="button" class="copy" data-copy aria-label="Copy code">Copy</button></div>` +
          `<pre><code>${h.html}</code></pre></div>`
        );
      },
      heading: (level, raw) => {
        const text = plain(raw);
        let id = opts.headingIds ? opts.headingIds[headings.length] : null;
        if (!id) {
          id = slug(text);
          const n = seen.get(id) || 0;
          seen.set(id, n + 1);
          if (n) id = `${id}-${n}`;
        }
        headings.push({ level, text, id });
        if (level === 1 && opts.skipTitle && !skipped) {
          skipped = true;
          return "";
        }
        const href = opts.anchorHref ? opts.anchorHref(id) : "#" + id;
        const anchor = level > 1 ? `<a class="anchor" href="${esc(href)}" aria-label="Link to this section" data-anchor="${esc(id)}">#</a>` : "";
        return `<h${level} id="${esc(id)}">${anchor}${inline(raw, ctx)}</h${level}>`;
      },
    };
    const html = blocks(markdown.replace(/\r\n?/g, "\n").split("\n"), ctx);
    return { html, headings };
  }

  VX.md = { render, inline: (s, link) => inline(s, { link: link || ((h, i) => i) }), highlight, slug, plain, esc };
})();
