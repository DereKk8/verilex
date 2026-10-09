// diagram.js: the diagrams the docs draw in fenced blocks, as themed HTML that reads at any width.
// The fence source stays readable as plain text on GitHub.
//
//   ```tree                         one entry per line, two spaces of indent per level;
//   .verilex/                       a name ending in / is a directory, and two or more
//     config.yaml   the project    spaces separate a name from its note
//   ```
//
//   ```flow                         one step per line, top to bottom, as `label: detail`;
//   mapping                         an indented run of steps is a branch off the step
//   behavioral check                above it that joins the main line again before the
//     decision request: ...         last step, which is where the flow ends
//   .verilex/grouping.yaml: ...
//   ```
(function () {
  "use strict";
  const VX = (window.VX = window.VX || {});

  const ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
  const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ESC[c]);

  const lines = (code) => code.split("\n").filter((l) => l.trim());
  const depth = (line) => Math.floor((line.length - line.trimStart().length) / 2);

  // A tree indents 3ch per level; the icon takes 2.2ch; notes line up 3ch after the longest name.
  const INDENT = 3;
  const ICON = 2.2;

  function tree(code, inline) {
    const rows = lines(code).map((line) => {
      const t = line.trim();
      const m = /^(\S(?:.*?\S)?)(?:\s{2,}(.*))?$/.exec(t);
      return { d: depth(line), name: m[1], note: m[2] || "", dir: m[1].endsWith("/") };
    });
    // last[i]: no later sibling follows row i, so its guide ends at it.
    const last = rows.map((r, i) => {
      for (let j = i + 1; j < rows.length; j++) {
        if (rows[j].d < r.d) return true;
        if (rows[j].d === r.d) return false;
      }
      return true;
    });
    const col = Math.max(...rows.map((r) => r.d * INDENT + ICON + r.name.length)) + 3;
    const open = [];
    let html = "";
    rows.forEach((r, i) => {
      open.length = r.d;
      let guides = "";
      for (let k = 1; k < r.d; k++) guides += `<i class="g${open[k] ? " on" : ""}"></i>`;
      if (r.d > 0) guides += `<i class="g j${last[i] ? " end" : ""}"></i>`;
      open[r.d] = !last[i];
      const icon = r.dir ? "dir" : "file";
      html +=
        `<li class="tree-row ${icon}" style="--name:${(col - r.d * INDENT).toFixed(1)}ch">${guides}` +
        `<span class="tree-entry"><span class="tree-name"><span class="tree-icon" aria-hidden="true"></span><code>${esc(r.name)}</code></span>` +
        (r.note ? `<span class="tree-note">${inline(r.note)}</span>` : "") +
        `</span></li>`;
    });
    return `<figure class="diagram"><ul class="tree">${html}</ul></figure>`;
  }

  // node splits `label: detail` at the first colon outside a code span.
  function node(text, inline, cls) {
    let ticks = 0;
    let at = -1;
    for (let i = 0; i < text.length; i++) {
      if (text[i] === "`") ticks ^= 1;
      else if (!ticks && text[i] === ":" && text[i + 1] === " ") { at = i; break; }
    }
    const label = at < 0 ? text : text.slice(0, at);
    const detail = at < 0 ? "" : text.slice(at + 2);
    return `<li class="flow-node${cls}"><span class="flow-card"><b>${inline(label)}</b>${detail ? `<span>${inline(detail)}</span>` : ""}</span>`;
  }

  function flow(code, inline) {
    const steps = lines(code).map((line) => ({ d: depth(line) > 0, text: line.trim() }));
    let html = "";
    steps.forEach((s, i) => {
      const next = steps[i + 1];
      if (s.d) {
        const first = !steps[i - 1].d;
        html += (first ? `<ol class="flow-branch">` : "") + node(s.text, inline, "") + "</li>";
        if (!next || !next.d) html += "</ol></li>";
        return;
      }
      html += node(s.text, inline, i === steps.length - 1 ? " end" : "");
      if (!next || !next.d) html += "</li>";
    });
    return `<figure class="diagram"><ol class="flow">${html}</ol></figure>`;
  }

  const KINDS = { tree, flow };

  // render returns the HTML of a diagram fence, or null when lang names no diagram.
  // inline(markdown) renders a note or a step's text.
  VX.diagram = {
    render(code, lang, inline) {
      const kind = KINDS[(lang || "").toLowerCase()];
      return kind && code.trim() ? kind(code, inline) : null;
    },
  };
})();
