// app.js: the docs app. Hash routes (#/page/anchor), the navigation, the page view with its table of
// contents, the search palette, keyboard shortcuts, the theme, and the home page's live terminal.
(function () {
  "use strict";
  const VX = window.VX;
  const B = window.VERILEX_DOCS;
  const md = VX.md;
  const esc = md.esc;
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => Array.from(el.querySelectorAll(s));
  const pages = new Map(B.pages.map((p) => [p.slug, p]));
  const order = B.pages.map((p) => p.slug);
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)");
  const isMac = /Mac|iPhone|iPad|iPod/.test(navigator.platform || navigator.userAgent);
  const store = {
    get(k) { try { return localStorage.getItem(k); } catch (e) { return null; } },
    set(k, v) { try { if (v == null) localStorage.removeItem(k); else localStorage.setItem(k, v); } catch (e) { /* blocked */ } },
  };
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

  const ICON = {
    file: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/></svg>',
    spark: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9z"/></svg>',
    copy: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="9" y="9" width="11" height="11" rx="2"/><path d="M5 15V6a2 2 0 0 1 2-2h8"/></svg>',
    clock: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/></svg>',
    edit: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linejoin="round"><path d="M4 20h4L19 9l-4-4L4 16z"/></svg>',
    up: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M12 19V5M6 11l6-6 6 6"/></svg>',
    arrow: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M5 12h14M13 6l6 6-6 6"/></svg>',
    search: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>',
  };

  // ---------- links ----------

  const hrefFor = (slug, anchor) => "#/" + (slug || "") + (slug && anchor ? "/" + anchor : "");
  const repoFile = (path, anchor) => `${B.repo}/blob/${B.branch}/${path}${anchor ? "#" + anchor : ""}`;

  function normalize(path) {
    const out = [];
    for (const part of path.split("/")) {
      if (!part || part === ".") continue;
      if (part === "..") out.pop();
      else out.push(part);
    }
    return out.join("/");
  }

  // resolve maps a link in the docs (or in an answer) to where it goes in the app.
  function resolve(href, fromFile) {
    if (href.startsWith("doc:")) {
      const [slug, anchor] = href.slice(4).split("#");
      return pages.has(slug) ? { href: hrefFor(slug, anchor), slug, anchor } : { href: hrefFor("") };
    }
    if (/^[a-z][a-z0-9+.-]*:/i.test(href)) return { href, external: true };
    const [path, anchor] = href.split("#");
    if (!path) return { href: hrefFor(current.slug, anchor), slug: current.slug, anchor };
    const base = (fromFile || "docs/README.md").split("/").slice(0, -1).join("/");
    const full = normalize(base + "/" + path);
    const m = /^docs\/([^/]+)\.md$/.exec(full);
    if (m && pages.has(m[1])) return { href: hrefFor(m[1], anchor), slug: m[1], anchor };
    if (full === "docs/README.md" || full === "docs") return { href: hrefFor("") };
    return { href: repoFile(full, anchor), external: true };
  }

  function linker(fromFile) {
    return (href, inner) => {
      const r = resolve(href, fromFile);
      return r.external
        ? `<a href="${esc(r.href)}" target="_blank" rel="noopener">${inner}</a>`
        : `<a href="${esc(r.href)}">${inner}</a>`;
    };
  }

  // citeLinker renders the doc: links of an answer as chips that name the section they open.
  function citeLinker(cited) {
    return (href, inner, text) => {
      const r = resolve(href, "docs/README.md");
      if (r.external) return `<a href="${esc(r.href)}" target="_blank" rel="noopener">${inner}</a>`;
      if (r.slug) cited.set(r.href, { slug: r.slug, anchor: r.anchor, text });
      return `<a class="cite" href="${esc(r.href)}">${inner}</a>`;
    };
  }

  // lead turns an index summary into a sentence: the index writes them after a colon, in lower case.
  const lead = (s) => (/^[a-z]/.test(s) ? s[0].toUpperCase() + s.slice(1) : s);

  function sectionTitle(slug, anchor) {
    const p = pages.get(slug);
    if (!p) return "";
    const h = anchor && p.headings.find((x) => x.id === anchor);
    return h && h.level > 1 ? `${p.title} › ${h.text}` : p.title;
  }

  // ---------- toast and copy ----------

  function toast(text) {
    const root = $("#toast-root");
    root.innerHTML = "";
    const t = document.createElement("div");
    t.className = "toast";
    t.textContent = text;
    root.appendChild(t);
    setTimeout(() => t.remove(), 2000);
  }

  async function copy(text, label) {
    try {
      await navigator.clipboard.writeText(text);
      toast(label || "Copied");
      return true;
    } catch (e) {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      let ok = false;
      try { ok = document.execCommand("copy"); } catch (err) { ok = false; }
      ta.remove();
      toast(ok ? label || "Copied" : "Select the text and copy it");
      return ok;
    }
  }

  document.addEventListener("click", (ev) => {
    const btn = ev.target.closest("[data-copy]");
    if (btn) {
      const text = btn.dataset.copyText || codeText(btn.closest(".code"));
      copy(text).then((ok) => {
        if (!ok || !btn.classList.contains("copy")) return;
        btn.textContent = "Copied";
        btn.classList.add("done");
        setTimeout(() => { btn.textContent = "Copy"; btn.classList.remove("done"); }, 1600);
      });
      return;
    }
    const anchor = ev.target.closest("a.anchor[data-anchor]");
    if (anchor) {
      const url = location.href.split("#")[0] + anchor.getAttribute("href");
      copy(url, "Link copied");
    }
  });

  // A terminal block copies its commands, not the output.
  function codeText(block) {
    if (!block) return "";
    const text = block.querySelector("pre").innerText.replace(/\n$/, "");
    if (block.dataset.lang === "terminal") {
      const cmds = text.split("\n").filter((l) => l.startsWith("$ ")).map((l) => l.slice(2));
      if (cmds.length) return cmds.join("\n");
    }
    return text;
  }

  // ---------- theme ----------

  const THEMES = ["system", "light", "dark"];
  function setTheme(t) {
    if (t === "system") document.documentElement.removeAttribute("data-theme");
    else document.documentElement.setAttribute("data-theme", t);
    store.set("vx-theme", t === "system" ? null : t);
    const btn = $("#theme");
    btn.setAttribute("aria-label", "Theme: " + t);
    btn.title = "Theme: " + t;
    const dark = t === "dark" || (t === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    $('meta[name="theme-color"]').setAttribute("content", dark ? "#06080c" : "#f5f7fa");
  }
  function cycleTheme() {
    const now = document.documentElement.getAttribute("data-theme") || "system";
    const next = THEMES[(THEMES.indexOf(now) + 1) % THEMES.length];
    const apply = () => setTheme(next);
    if (document.startViewTransition && !reduce.matches) document.startViewTransition(apply);
    else apply();
    toast("Theme: " + next);
  }

  // ---------- sidebar ----------

  function buildSidebar() {
    const nav = $("#sidebar");
    let html = "";
    for (const s of B.sections) {
      html += `<div class="nav-section"><div class="nav-label">${esc(s.title)}</div><div class="nav">`;
      for (const slug of s.pages) html += `<a href="${hrefFor(slug)}" data-slug="${esc(slug)}">${esc(pages.get(slug).title)}</a>`;
      html += "</div></div>";
    }
    html += `<div class="sidebar-foot">
      <span>${B.pages.length} pages · ${B.static ? "static build" : "read live from docs/"}</span>
      <a href="${esc(B.repo)}" target="_blank" rel="noopener">github.com/DereKk8/verilex</a>
    </div>`;
    nav.innerHTML = html;
    for (const n of $$(".nav", nav)) {
      const m = document.createElement("span");
      m.className = "nav-marker";
      m.style.opacity = "0";
      n.prepend(m);
    }
    nav.addEventListener("click", (ev) => {
      if (ev.target.closest("a")) document.body.classList.remove("nav-open");
    });
  }

  function markSidebar(slug) {
    for (const a of $$("#sidebar .nav a")) {
      const on = a.dataset.slug === slug;
      if (on) a.setAttribute("aria-current", "page");
      else a.removeAttribute("aria-current");
    }
    for (const nav of $$("#sidebar .nav")) {
      const marker = $(".nav-marker", nav);
      const a = $('a[aria-current="page"]', nav);
      if (!a) { marker.style.opacity = "0"; continue; }
      marker.style.opacity = "1";
      marker.style.height = a.offsetHeight + "px";
      marker.style.transform = `translateY(${a.offsetTop}px)`;
      const side = $("#sidebar");
      const r = a.getBoundingClientRect();
      if (r.top < 80 || r.bottom > innerHeight - 40) a.scrollIntoView({ block: "center" });
      void side;
    }
  }

  // ---------- views ----------

  const main = $("#main");
  const shell = $("#shell");
  const toc = $("#toc");
  let current = { slug: null, anchor: null };
  let first = true;

  function view(render) {
    const go = () => {
      render();
      const pg = $(".page", main);
      if (pg && (first || !document.startViewTransition || reduce.matches)) pg.classList.add("enter");
    };
    if (!first && document.startViewTransition && !reduce.matches) document.startViewTransition(go);
    else go();
    first = false;
  }

  function footer() {
    return `<footer class="footer">
      <span>verilex docs · ${B.static ? "built from" : "served live from"} docs/ · ${B.pages.length} pages</span>
      <span><kbd>${isMac ? "⌘" : "Ctrl"}</kbd> <kbd>K</kbd> search · <kbd>${isMac ? "⌘" : "Ctrl"}</kbd> <kbd>I</kbd> ask · <kbd>[</kbd> <kbd>]</kbd> pages</span>
    </footer>`;
  }

  function renderPage(slug) {
    const p = pages.get(slug);
    const r = md.render(p.markdown, {
      headingIds: p.headings.map((h) => h.id),
      skipTitle: true,
      link: linker(p.file),
      anchorHref: (id) => hrefFor(slug, id),
    });
    const i = order.indexOf(slug);
    const prev = i > 0 ? pages.get(order[i - 1]) : null;
    const next = i < order.length - 1 ? pages.get(order[i + 1]) : null;
    const words = p.markdown.split(/\s+/).length;
    const minutes = Math.max(1, Math.round(words / 230));
    shell.classList.remove("home");
    main.innerHTML = `
      <article class="page">
        <div class="article">
          <div class="crumbs"><span>${esc(p.section)}</span><span class="sep">|</span><span>${esc(p.title)}</span></div>
          <h1 class="title">${esc(p.title)}</h1>
          <p class="lead">${md.inline(lead(p.summary), linker(p.file))}</p>
          <div class="meta">
            <a class="chip" href="${esc(repoFile(p.file))}" target="_blank" rel="noopener">${ICON.file}${esc(p.file)}</a>
            <span class="chip">${ICON.clock}${minutes} min read</span>
            <button class="chip signal" type="button" data-ask-page>${ICON.spark}Ask about this page</button>
            <button class="chip" type="button" data-copy data-copy-text="${esc(p.markdown)}">${ICON.copy}Copy as Markdown</button>
          </div>
          <div class="prose">${r.html}</div>
        </div>
        <nav class="pager" aria-label="Pages">
          ${prev ? `<a class="prev" href="${hrefFor(prev.slug)}"><small>← Previous</small>${esc(prev.title)}</a>` : "<span></span>"}
          ${next ? `<a class="next" href="${hrefFor(next.slug)}"><small>Next →</small>${esc(next.title)}</a>` : ""}
        </nav>
      </article>
      ${footer()}`;
    renderToc(slug, r.headings.filter((h) => h.level === 2 || h.level === 3));
    document.title = `${p.title} · verilex docs`;
  }

  function renderToc(slug, hs) {
    const p = pages.get(slug);
    toc.innerHTML = `
      <div class="toc-head"><svg class="ring" viewBox="0 0 20 20"><circle class="track" cx="10" cy="10" r="8"/><circle class="bar" cx="10" cy="10" r="8" stroke-dasharray="50.27" stroke-dashoffset="50.27"/></svg>On this page</div>
      ${hs.length ? `<ol><span class="toc-marker" style="opacity:0"></span>${hs.map((h) => `<li class="l${h.level}"><a href="${hrefFor(slug, h.id)}" data-id="${esc(h.id)}">${esc(h.text)}</a></li>`).join("")}</ol>` : ""}
      <div class="toc-actions">
        <button class="chip signal" type="button" data-ask-page>${ICON.spark}Ask about this page</button>
        <a class="chip" href="${esc(repoFile(p.file))}" target="_blank" rel="noopener">${ICON.edit}Edit on GitHub</a>
        <button class="chip" type="button" data-top>${ICON.up}Back to top</button>
      </div>`;
  }

  // ---------- scroll: progress, contents, flash ----------

  let ticking = false;
  function onScroll() {
    if (ticking) return;
    ticking = true;
    requestAnimationFrame(() => {
      ticking = false;
      const max = document.documentElement.scrollHeight - innerHeight;
      const p = max > 0 ? Math.min(1, scrollY / max) : 0;
      $("#progress").style.setProperty("--p", current.slug ? p : 0);
      const bar = $(".ring .bar", toc);
      if (bar) bar.style.strokeDashoffset = String(50.27 * (1 - p));
      spy();
    });
  }

  function spy() {
    const links = $$(".toc li a", toc);
    if (!links.length) return;
    let active = links[0];
    const atEnd = innerHeight + scrollY >= document.documentElement.scrollHeight - 4;
    for (const a of links) {
      const h = document.getElementById(a.dataset.id);
      if (!h) continue;
      const top = h.getBoundingClientRect().top;
      if (top < 140 || (atEnd && top < innerHeight * 0.6)) active = a;
    }
    if (current.anchor && atEnd) {
      const target = links.find((a) => a.dataset.id === current.anchor);
      const h = target && document.getElementById(current.anchor);
      if (h && h.getBoundingClientRect().top < innerHeight) active = target;
    }
    for (const a of links) a.classList.toggle("on", a === active);
    const marker = $(".toc-marker", toc);
    if (marker) {
      marker.style.opacity = "1";
      marker.style.height = active.offsetHeight + "px";
      marker.style.transform = `translateY(${active.parentElement.offsetTop}px)`;
    }
  }

  function scrollToAnchor(anchor, smooth) {
    const el = anchor && document.getElementById(anchor);
    if (!el) {
      window.scrollTo({ top: 0, behavior: "instant" });
      return;
    }
    el.scrollIntoView({ block: "start", behavior: smooth && !reduce.matches ? "smooth" : "instant" });
    el.classList.remove("flash");
    void el.offsetWidth;
    el.classList.add("flash");
  }

  // ---------- routing ----------

  function parse() {
    const h = decodeURIComponent(location.hash.replace(/^#/, ""));
    if (h.startsWith("/")) {
      const [slug, anchor] = h.slice(1).split("/");
      return { slug: pages.has(slug) ? slug : null, anchor: anchor || null };
    }
    // A bare #page is the only deep link some hosts pass through.
    if (pages.has(h)) return { slug: h, anchor: null };
    return { slug: null, anchor: null };
  }

  function route() {
    const r = parse();
    if (r.slug && r.slug === current.slug) {
      current = r;
      scrollToAnchor(r.anchor, true);
      return;
    }
    const sameHome = !r.slug && !current.slug && !first;
    current = r;
    if (sameHome) return;
    view(() => {
      if (r.slug) renderPage(r.slug);
      else renderHome();
      markSidebar(r.slug);
      scrollToAnchor(r.anchor, false);
      onScroll();
    });
    VX.ask && VX.ask.pageChanged(r.slug);
  }

  // ---------- home ----------

  const CHAIN = "store-open | item-stored apple | item-listed apple";
  const NODES = [
    { w: "store-open", a: "", c: "provides store" },
    { w: "item-stored", a: "apple", c: "store → item:{apple}" },
    { w: "item-listed", a: "apple", c: "requires item:{apple}" },
  ];
  // Every scene replays output verilex printed on the tally sample product (docs/getting-started.md).
  const SCENES = [
    { id: "live", label: "live run", cmd: `verilex run '${CHAIN}'`, mode: "run", states: ["green", "green", "green"], exit: 0,
      out: ["green: 3 green; run 1791503104-22eb1c4bc654"] },
    { id: "red", label: "red", cmd: `TALLY_DEFECT=drop-adds verilex run '${CHAIN}'`, mode: "run", states: ["green", "red", "off"], exit: 1,
      out: ["red: 1 green, 1 red, 1 not run; run 1791503105-d713d0012bbc",
        "  red  item-stored apple: tally said 'added apple' but store.json lacks apple",
        "    evidence: ~/.local/state/verilex/tally/runs/1791503105-d713d0012bbc/02-item-stored",
        "    verify skill: verify-tally/features/items.md#item-add"] },
    { id: "refused", label: "refused", cmd: "verilex run 'item-stored apple | store-open'", mode: "refuse",
      nodes: [NODES[1], NODES[0]], states: ["amber", "off"], exit: 2,
      out: ["verilex: refused: item-stored apple requires store, pinned by claim item-added; nothing earlier provides it"] },
    { id: "skip", label: "skipped", cmd: `verilex run '${CHAIN}'`, mode: "skip", states: ["skip", "skip", "skip"], exit: 0,
      stamp: "109ae749f74b45560dac1fbc3e14ce6bc79d57c1fc83e51856bccc1400123a51",
      out: ["green: 3 green, skipped: stamps match run 1791503107-17d0dc90a0a2; run 1791503107-87c51fb6e590"] },
  ];

  function renderHome() {
    shell.classList.add("home");
    toc.innerHTML = "";
    document.title = "verilex docs";
    const sections = B.sections
      .map((s) => `<div class="section-card"><h3>${esc(s.title)}</h3>${s.pages
        .map((slug) => {
          const p = pages.get(slug);
          return `<a href="${hrefFor(slug)}">${esc(p.title)}<span>${md.inline(lead(p.summary))}</span></a>`;
        })
        .join("")}</div>`)
      .join("");
    const k = isMac ? "⌘" : "Ctrl";
    main.innerHTML = `
      <div class="page home-page">
        <section class="hero">
          <div>
            <div class="eyebrow"><span class="dot"></span>Verification as a chain of words</div>
            <h1>Product verification, <span class="pipe">piped</span>.<span class="cursor" aria-hidden="true"></span></h1>
            <p class="sub">verilex lets verifier and QA agents run product verification as a chain of words, Unix-pipe style. Every run ends <b class="t-v-green">green</b>, <b class="t-v-red">red</b> or <b class="t-v-inconclusive">inconclusive</b>, with its evidence on disk, and a chain whose proof still stands is skipped.</p>
            <div class="cta">
              <a class="btn primary" href="${hrefFor("getting-started")}">Get started ${ICON.arrow}</a>
              <button class="btn" type="button" data-open-search>${ICON.search}Search <kbd>${isMac ? "⌘K" : "Ctrl K"}</kbd></button>
              <button class="btn" type="button" data-open-ask>${ICON.spark}Ask the docs <kbd>${isMac ? "⌘I" : "Ctrl I"}</kbd></button>
            </div>
            <div class="install"><code><span class="t-prompt">$</span> <span class="t-cmd">go</span> install github.com/DereKk8/verilex/cmd/verilex@latest</code><button class="copy" type="button" data-copy data-copy-text="go install github.com/DereKk8/verilex/cmd/verilex@latest">Copy</button></div>
          </div>
          <div class="term" id="term">
            <div class="term-head">
              <div class="term-dots" aria-hidden="true"><i></i><i></i><i></i></div>
              <div class="term-tabs" role="tablist" aria-label="Scenes">${SCENES.map((s, i) => `<button type="button" role="tab" data-scene="${i}" aria-selected="false">${s.label}</button>`).join("")}</div>
            </div>
            <div class="term-body">
              <div class="term-cmd" id="term-cmd"></div>
              <div class="chain" id="term-chain"></div>
              <div class="term-out" id="term-out" aria-live="polite"></div>
            </div>
            <div class="term-foot"><span class="exit" id="term-exit">exit –</span><span>~/tally</span><span class="stamp" id="term-stamp"></span></div>
          </div>
        </section>

        <section class="band">
          <div class="band-head"><h2>Three verdicts, never a guess</h2><p>An environment failure is never reported as a product failure. <a href="${hrefFor("verdicts")}">Verdicts →</a></p></div>
          <div class="verdicts">
            <div class="verdict g"><header><code>green</code><span>exit<b>0</b></span></header><p>The product works, backed by evidence.</p></div>
            <div class="verdict r"><header><code>red</code><span>exit<b>1</b></span></header><p>The product is broken, backed by evidence.</p></div>
            <div class="verdict i"><header><code>inconclusive</code><span>exit<b>2</b></span></header><p>Environment or harness trouble, or a word's report that is not backed. Never reported as a product failure.</p></div>
          </div>
        </section>

        <section class="band">
          <div class="band-head"><h2>Anatomy of a run</h2><p>Every run goes through the project's frame, and no word can opt out of it. <a href="${hrefFor("trust-frame")}">The trust frame →</a></p></div>
          <div class="frame${reduce.matches ? "" : " live"}">
            <div class="frame-step" style="--i:0"><i>01 · frame/launch</i><b>launch</b><span>Creates an instance owned by this run, labeled with its run id.</span></div>
            <div class="frame-step" style="--i:1"><i>02 · frame/doctor</i><b>doctor</b><span>Confirms the instance is this run's and worth driving, before the first word and after any failure.</span></div>
            <div class="frame-step" style="--i:2"><i>03 · words/*/run</i><b>words</b><span>Each word drives the product and reports pass, fail or blocked.</span></div>
            <div class="frame-step" style="--i:3"><i>04 · honesty rules</i><b>evidence</b><span>Kept outside the product. A report it does not back is inconclusive.</span></div>
            <div class="frame-step" style="--i:4"><i>05 · frame/cleanup</i><b>cleanup</b><span>Always runs, and tears down only what the run started.</span></div>
          </div>
        </section>

        <section class="band">
          <div class="band-head"><h2>The docs</h2><p>${B.pages.length} pages. <kbd>${k}</kbd> <kbd>K</kbd> finds any section, command or term as you type.</p></div>
          <div class="sections">${sections}</div>
        </section>

        <section class="band">
          <div class="keys">
            <span><kbd>${k}</kbd><kbd>K</kbd> or <kbd>/</kbd> search</span>
            <span><kbd>${k}</kbd><kbd>I</kbd> ask the docs</span>
            <span><kbd>[</kbd><kbd>]</kbd> previous and next page</span>
            <span><kbd>esc</kbd> close</span>
          </div>
        </section>
      </div>
      ${footer()}`;
    terminal.mount($("#term"));
  }

  // The home terminal plays real verilex runs: typing, words lighting up as they run, the verdict.
  const terminal = (() => {
    let token = 0;
    let root = null;
    let auto = true;
    let scene = 0;

    const wait = async (ms, t) => {
      await sleep(ms);
      if (t !== token) throw new Error("stale");
    };

    function nodesHTML(s) {
      const ns = s.nodes || NODES;
      return ns
        .map((n, i) => (i ? '<span class="pipe-link" aria-hidden="true">|</span>' : "") +
          `<div class="node" data-i="${i}"><span class="state"></span><b>${esc(n.w)}${n.a ? " <em>" + esc(n.a) + "</em>" : ""}</b><small>${esc(n.c)}</small></div>`)
        .join("");
    }

    function setExit(code) {
      const e = $("#term-exit", root);
      e.className = "exit" + (code == null ? "" : " e" + code);
      e.textContent = code == null ? "exit –" : "exit " + code;
    }

    function output(lines) {
      const out = $("#term-out", root);
      out.innerHTML = lines.map((l) => `<div class="line">${md.highlight(l, "console").html || "&nbsp;"}</div>`).join("");
    }

    async function play(i, t) {
      const s = SCENES[i];
      $$(".term-tabs button", root).forEach((b, k) => b.setAttribute("aria-selected", String(k === i)));
      const chain = $("#term-chain", root);
      chain.innerHTML = nodesHTML(s);
      $("#term-out", root).innerHTML = "";
      $("#term-stamp", root).innerHTML = "";
      setExit(null);
      const cmd = $("#term-cmd", root);
      const nodes = $$(".node", chain);
      const pipes = $$(".pipe-link", chain);
      const fin = () => {
        cmd.innerHTML = `<span class="t-prompt">$</span> ${md.highlight("$ " + s.cmd, "console").html.replace(/^<span class="t-prompt">\$<\/span> /, "")}`;
        nodes.forEach((n, k) => n.classList.add(s.states[k]));
        output(s.out);
        setExit(s.exit);
        if (s.stamp) $("#term-stamp", root).innerHTML = `stamp <b>${s.stamp.slice(0, 16)}…</b> matches`;
      };
      if (reduce.matches) return fin();

      // type the command
      const full = s.cmd;
      const step = Math.max(1, Math.round(full.length / 42));
      for (let k = 0; k <= full.length; k += step) {
        cmd.innerHTML = `<span class="t-prompt">$</span> ${esc(full.slice(0, k))}<span class="caret"></span>`;
        await wait(16, t);
      }
      cmd.innerHTML = `<span class="t-prompt">$</span> ${md.highlight("$ " + full, "console").html.replace(/^<span class="t-prompt">\$<\/span> /, "")}`;
      await wait(260, t);

      if (s.mode === "run") {
        for (let k = 0; k < nodes.length; k++) {
          if (s.states[k] === "off") { nodes[k].classList.add("off"); continue; }
          if (k) {
            pipes[k - 1].classList.remove("flow");
            void pipes[k - 1].offsetWidth;
            pipes[k - 1].classList.add("flow");
            await wait(220, t);
          }
          nodes[k].classList.add("run");
          await wait(520, t);
          nodes[k].classList.remove("run");
          nodes[k].classList.add(s.states[k]);
          if (s.states[k] === "red") {
            for (let j = k + 1; j < nodes.length; j++) nodes[j].classList.add("off");
            break;
          }
          await wait(120, t);
        }
      } else if (s.mode === "refuse") {
        nodes[0].classList.add("amber");
        nodes[1].classList.add("off");
        await wait(200, t);
      } else if (s.mode === "skip") {
        const stamp = $("#term-stamp", root);
        const hex = "0123456789abcdef";
        for (let r = 0; r < 14; r++) {
          const fixed = Math.floor((r / 13) * 16);
          let txt = s.stamp.slice(0, fixed);
          for (let c = fixed; c < 16; c++) txt += hex[(Math.random() * 16) | 0];
          stamp.innerHTML = `stamp <b>${txt}…</b> ${r < 13 ? "checking" : "matches"}`;
          await wait(45, t);
        }
        for (let k = 0; k < nodes.length; k++) {
          nodes[k].classList.add("skip");
          if (k < pipes.length) pipes[k].classList.add("flow");
          await wait(90, t);
        }
      }
      await wait(160, t);
      const out = $("#term-out", root);
      out.innerHTML = "";
      for (const l of s.out) {
        out.insertAdjacentHTML("beforeend", `<div class="line">${md.highlight(l, "console").html}</div>`);
        await wait(140, t);
      }
      setExit(s.exit);
    }

    async function loop(start) {
      const t = ++token;
      scene = start;
      try {
        for (;;) {
          await play(scene, t);
          if (reduce.matches) return;
          await wait(3600, t);
          while (!auto || document.hidden || !root || !root.isConnected) await wait(500, t);
          scene = (scene + 1) % SCENES.length;
        }
      } catch (e) {
        /* a newer scene took over */
      }
    }

    function mount(el) {
      root = el;
      auto = true;
      el.addEventListener("click", (ev) => {
        const b = ev.target.closest("[data-scene]");
        if (!b) return;
        auto = false;
        loop(Number(b.dataset.scene));
        setTimeout(() => { auto = true; }, 20000);
      });
      el.addEventListener("pointermove", (ev) => {
        const r = el.getBoundingClientRect();
        el.style.setProperty("--mx", ((ev.clientX - r.left) / r.width) * 100 + "%");
        el.style.setProperty("--my", ((ev.clientY - r.top) / r.height) * 100 + "%");
      });
      el.addEventListener("mouseenter", () => { auto = false; });
      el.addEventListener("mouseleave", () => { auto = true; });
      loop(reduce.matches ? 3 : 0);
    }

    return { mount };
  })();

  // ---------- search palette ----------

  const palette = (() => {
    let root = null;
    let sel = 0;
    let items = [];
    let indexed = false;
    let lastFocus = null;

    const TYPE = { page: ["▤", "Page"], section: ["§", "Section"], command: ["$", "Command"], term: ["≡", "Term"] };
    const JUMP = ["getting-started", "concepts", "commands", "claims", "verdict-rule"];
    const ASK = ["When does verilex skip a chain?", "What makes a run inconclusive instead of red?", "How do I add verilex to a project?"];

    function ensureIndex() {
      if (indexed) return;
      VX.search.build(B);
      indexed = true;
    }

    function open(q) {
      ensureIndex();
      if (root) {
        $("input", root).focus();
        return;
      }
      lastFocus = document.activeElement;
      const host = $("#palette-root");
      host.innerHTML = `
        <div class="scrim" data-close></div>
        <div class="palette" role="dialog" aria-modal="true" aria-label="Search the docs">
          <div class="palette-input">
            ${ICON.search}
            <input id="palette-q" type="text" placeholder="Search pages, sections, commands, terms…" autocomplete="off" spellcheck="false" aria-controls="palette-results" aria-autocomplete="list">
            <div class="seg" role="group" aria-label="Mode"><button type="button" aria-pressed="true">Search</button><button type="button" data-to-ask aria-pressed="false">Ask</button></div>
          </div>
          <div class="results" id="palette-results" role="listbox"></div>
          <div class="palette-foot">
            <span><kbd>↑</kbd><kbd>↓</kbd> move</span><span><kbd>↵</kbd> open</span><span><kbd>tab</kbd> ask</span><span><kbd>esc</kbd> close</span>
            <span class="speed" id="palette-speed"></span>
          </div>
        </div>`;
      root = host;
      const input = $("input", root);
      input.value = q || "";
      input.addEventListener("input", () => update(input.value));
      input.addEventListener("keydown", keys);
      root.addEventListener("click", (ev) => {
        if (ev.target.closest("[data-close]")) return close();
        if (ev.target.closest("[data-to-ask]")) return toAsk(input.value);
        const r = ev.target.closest(".result");
        if (r) {
          ev.preventDefault();
          choose(Number(r.dataset.i));
        }
      });
      root.addEventListener("mousemove", (ev) => {
        const r = ev.target.closest(".result");
        if (r && Number(r.dataset.i) !== sel) select(Number(r.dataset.i), false);
      });
      update(input.value);
      input.focus();
      input.select();
    }

    function close() {
      if (!root) return;
      root.innerHTML = "";
      root = null;
      if (lastFocus && lastFocus.focus) lastFocus.focus();
    }

    function toAsk(q) {
      close();
      VX.ask.open({ question: q.trim() || null });
    }

    function update(q) {
      const list = $("#palette-results", root);
      const t0 = performance.now();
      const hits = q.trim() ? VX.search.query(q, 24) : [];
      const ms = performance.now() - t0;
      items = [];
      let html = "";
      const question = /\?\s*$/.test(q) || q.trim().split(/\s+/).length >= 4;
      const askItem = () => {
        items.push({ ask: q.trim() });
        return `<a class="result ask-row" data-i="${items.length - 1}" role="option" aria-selected="false" href="#"><span class="r-icon">${ICON.spark}</span><span><span class="r-title">Ask the docs: “${esc(q.trim())}”</span><span class="r-path">answers grounded in these pages</span></span><span class="r-go">tab</span></a>`;
      };
      if (!q.trim()) {
        html += '<div class="group-label">Jump to</div>';
        for (const slug of JUMP) {
          const p = pages.get(slug);
          if (!p) continue;
          items.push({ href: hrefFor(slug) });
          html += row(items.length - 1, "page", esc(p.title), esc(p.section), esc(lead(p.summary)), "");
        }
        html += '<div class="group-label">Ask the docs</div>';
        for (const a of ASK) {
          items.push({ ask: a });
          html += `<a class="result ask-row" data-i="${items.length - 1}" role="option" aria-selected="false" href="#"><span class="r-icon">${ICON.spark}</span><span><span class="r-title">${esc(a)}</span></span><span class="r-go">↵</span></a>`;
        }
        $("#palette-speed", root).textContent = `${VX.search.size().entries} entries indexed`;
      } else {
        if (question) html += askItem();
        for (const h of hits) {
          items.push({ href: hrefFor(h.page, h.anchor), anchor: h.anchor });
          const title = VX.search.mark(esc(h.title), q);
          const snip = h.type === "page" ? "" : VX.search.mark(esc(h.snippet), q);
          html += row(items.length - 1, h.type, title, esc(h.path), snip, h.type === "command" ? "mono" : "");
        }
        if (!question) html += askItem();
        if (!hits.length) html = `<div class="empty">No page, section, command or term matches “${esc(q)}”. Ask the docs instead.</div>` + html;
        $("#palette-speed", root).textContent = `${hits.length} result${hits.length === 1 ? "" : "s"} · ${ms < 1 ? ms.toFixed(2) : ms.toFixed(1)} ms`;
      }
      list.innerHTML = html;
      select(0, false);
    }

    function row(i, type, title, path, snip, cls) {
      const [icon, label] = TYPE[type];
      return `<a class="result" data-i="${i}" role="option" aria-selected="false" href="${esc(items[i].href)}">
        <span class="r-icon" title="${label}">${icon}</span>
        <span><span class="r-title ${cls}">${title}</span><span class="r-path">${label} · ${path}</span>${snip ? `<span class="r-snip">${snip}</span>` : ""}</span>
        <span class="r-go">↵</span></a>`;
    }

    function select(i, scroll = true) {
      const rows = $$(".result", root);
      if (!rows.length) return;
      sel = (i + rows.length) % rows.length;
      rows.forEach((r, k) => r.setAttribute("aria-selected", String(k === sel)));
      if (scroll) rows[sel].scrollIntoView({ block: "nearest" });
    }

    function choose(i) {
      const it = items[i];
      if (!it) return;
      if (it.ask != null) return toAsk(it.ask);
      close();
      if (location.hash === it.href) route();
      else location.hash = it.href;
    }

    function keys(ev) {
      if (ev.key === "ArrowDown") { ev.preventDefault(); select(sel + 1); }
      else if (ev.key === "ArrowUp") { ev.preventDefault(); select(sel - 1); }
      else if (ev.key === "Enter") { ev.preventDefault(); choose(sel); }
      else if (ev.key === "Tab") { ev.preventDefault(); toAsk(ev.target.value); }
      else if (ev.key === "Escape") { ev.preventDefault(); close(); }
    }

    return { open, close, isOpen: () => !!root, warm: ensureIndex };
  })();

  // ---------- events ----------

  function typing(el) {
    return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
  }

  document.addEventListener("keydown", (ev) => {
    const mod = isMac ? ev.metaKey : ev.ctrlKey;
    const key = ev.key.toLowerCase();
    if (mod && key === "k") {
      ev.preventDefault();
      if (palette.isOpen()) palette.close();
      else palette.open();
      return;
    }
    if (mod && key === "i") {
      ev.preventDefault();
      palette.close();
      VX.ask.toggle();
      return;
    }
    if (ev.key === "Escape") {
      if (palette.isOpen()) return palette.close();
      if (document.body.classList.contains("nav-open")) return document.body.classList.remove("nav-open");
      if (VX.ask.isOpen()) return VX.ask.close();
    }
    if (typing(ev.target) || mod || ev.altKey) return;
    if (ev.key === "/") {
      ev.preventDefault();
      palette.open();
    } else if ((ev.key === "[" || ev.key === "]") && current.slug) {
      const i = order.indexOf(current.slug) + (ev.key === "]" ? 1 : -1);
      if (i >= 0 && i < order.length) location.hash = hrefFor(order[i]);
    }
  });

  document.addEventListener("click", (ev) => {
    if (ev.target.closest("[data-open-search]")) palette.open();
    else if (ev.target.closest("[data-open-ask]")) VX.ask.open({});
    else if (ev.target.closest("[data-ask-page]")) VX.ask.open({ page: current.slug });
    else if (ev.target.closest("[data-top]")) window.scrollTo({ top: 0, behavior: reduce.matches ? "instant" : "smooth" });
    else if (ev.target.closest(".nav-scrim")) document.body.classList.remove("nav-open");
  });

  $("#open-search").addEventListener("click", () => palette.open());
  $("#open-ask").addEventListener("click", () => VX.ask.toggle());
  $("#theme").addEventListener("click", cycleTheme);
  $("#menu").addEventListener("click", () => {
    const open = document.body.classList.toggle("nav-open");
    $("#menu").setAttribute("aria-expanded", String(open));
  });
  window.addEventListener("hashchange", route);
  window.addEventListener("scroll", onScroll, { passive: true });
  window.addEventListener("resize", () => markSidebar(current.slug));
  if (!isMac) {
    $("#kbd-k").textContent = "Ctrl K";
    $("#kbd-i").textContent = "Ctrl I";
  }

  // ---------- start ----------

  VX.app = {
    resolve, linker, citeLinker, sectionTitle, hrefFor, toast, copy, pages,
    current: () => current.slug,
    go: (href) => { location.hash = href; },
    isMac,
  };
  setTheme(document.documentElement.getAttribute("data-theme") || "system");
  buildSidebar();
  VX.ask.init(VX.app);
  route();
  // Build the search index while the reader looks at the first page, not on the first keystroke.
  (window.requestIdleCallback || ((f) => setTimeout(f, 300)))(() => palette.warm());
})();
