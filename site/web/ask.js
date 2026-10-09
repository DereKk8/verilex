// ask.js: Ask the docs. An answer comes from one provider:
//   - a plan the reader signed in to (Claude, ChatGPT or Grok), through the docs server on their machine;
//   - the Claude API key the docs server was started with;
//   - the reader's own claude.ai account, when the page runs as a claude.ai artifact;
//   - the docs alone: the passages that match best, quoted, with no model and no account.
(function () {
  "use strict";
  const VX = (window.VX = window.VX || {});
  const B = window.VERILEX_DOCS;
  const $ = (s, el = document) => el.querySelector(s);
  const esc = (s) => VX.md.esc(s);
  let app = null;

  const DOCS_ONLY = {
    id: "docs", name: "Docs only", plan: "Quotes the passages that match best. No model and no account.", via: "this page",
    kind: "local", installed: true, signedIn: true, enabled: true, detail: "always available",
  };
  const TILE = { claude: "Cl", claudeai: "Cl", codex: "Cx", grok: "Gk", api: "{ }", docs: "§" };
  const CLONE = "git clone https://github.com/DereKk8/verilex && cd verilex && scripts/docs";

  const state = {
    providers: [DOCS_ONLY], current: DOCS_ONLY, turns: [], busy: false, ctl: null, sampler: null,
    page: null, loaded: false, local: false, server: false, signin: null,
  };
  const store = {
    get(k) { try { return localStorage.getItem(k); } catch (e) { return null; } },
    set(k, v) { try { localStorage.setItem(k, v); } catch (e) { /* blocked */ } },
  };
  const usable = (p) => p.enabled !== false && p.installed && p.signedIn !== false;

  // ---------- providers ----------

  async function loadProviders(refresh) {
    const list = [];
    if (window.claude && typeof window.claude.use === "function") {
      try {
        const s = state.sampler || (await window.claude.use("sample"));
        if (s) {
          state.sampler = s;
          list.push({ id: "claudeai", name: "Claude", plan: "Your claude.ai account. claude.ai asks you to allow this page first.", via: "claude.ai",
            kind: "subscription", installed: true, signedIn: null, enabled: true, detail: "answers on your own account" });
        }
      } catch (e) { /* no sampling in this view */ }
    }
    if (!B.static && /^https?:$/.test(location.protocol)) {
      try {
        const r = await fetch("api/status" + (refresh ? "?refresh=1" : ""), { headers: { accept: "application/json" } });
        if (r.ok) {
          const j = await r.json();
          state.server = true;
          state.local = j.local;
          list.push(...j.providers);
        }
      } catch (e) { /* no docs server: a static copy */ }
    }
    list.push(DOCS_ONLY);
    state.providers = list;
    state.loaded = true;
    const saved = list.find((p) => p.id === store.get("vx-provider"));
    state.current = saved && usable(saved) ? saved : list.find(usable) || DOCS_ONLY;
    paintProvider();
  }

  function stateOf(p) {
    if (p.enabled === false) return ["off", "off"];
    if (!p.installed) return ["off", "not installed"];
    if (p.signedIn === false) return ["warn", "not signed in"];
    if (p.signedIn === null || p.signedIn === undefined) return ["ok", p.detail || "ready"];
    return ["ok", p.detail || "signed in"];
  }

  function paintProvider() {
    const btn = $("#ask-provider");
    if (!btn) return;
    const p = state.current;
    const [dot] = stateOf(p);
    btn.innerHTML = `<span class="dot ${dot}"></span><span class="name">${esc(p.name)}${p.via && p.id !== "docs" ? " · " + esc(p.via) : ""}</span><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="m6 9 6 6 6-6"/></svg>`;
    const mode = $("#ask-mode");
    if (mode) mode.textContent = p.id === "docs" ? "quotes the docs, no model" : "answers cite the docs";
  }

  function choose(p) {
    state.current = p;
    store.set("vx-provider", p.id);
    paintProvider();
    closeSheet();
    app.toast(`Answering with ${p.name}${p.id === "docs" ? "" : " · " + p.via}`);
    $("#ask-input").focus();
  }

  // ---------- panel ----------

  const panel = () => $("#ask");

  function build() {
    const el = panel();
    if (el.dataset.built) return;
    el.dataset.built = "1";
    el.innerHTML = `
      <div class="ask-head">
        <h2>Ask the docs</h2>
        <button class="provider-btn" id="ask-provider" type="button" aria-label="Choose how Ask answers"></button>
        <span class="spacer"></span>
        <button class="icon-btn" id="ask-new" type="button" aria-label="New conversation" title="New conversation"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg></button>
        <button class="icon-btn" id="ask-close" type="button" aria-label="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M6 6l12 12M18 6 6 18"/></svg></button>
      </div>
      <div class="ask-body" id="ask-body"></div>
      <div class="composer">
        <form id="ask-form">
          <label class="sr" for="ask-input">Your question</label>
          <textarea id="ask-input" rows="1" placeholder="Ask anything about verilex…"></textarea>
          <button class="send" id="ask-send" type="submit" aria-label="Send" disabled><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 19V5M5 12l7-7 7 7"/></svg></button>
        </form>
        <div class="hint"><span>↵ send · ⇧↵ new line</span><span id="ask-mode"></span></div>
      </div>`;
    const input = $("#ask-input");
    input.addEventListener("input", () => {
      input.style.height = "auto";
      input.style.height = Math.min(160, input.scrollHeight) + "px";
      $("#ask-send").disabled = !state.busy && !input.value.trim();
    });
    input.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter" && !ev.shiftKey && !ev.isComposing) {
        ev.preventDefault();
        submit();
      }
    });
    $("#ask-form").addEventListener("submit", (ev) => {
      ev.preventDefault();
      if (state.busy) stop();
      else submit();
    });
    $("#ask-close").addEventListener("click", close);
    $("#ask-new").addEventListener("click", reset);
    $("#ask-provider").addEventListener("click", () => ($(".sheet", panel()) ? closeSheet() : openSheet()));
    el.addEventListener("click", (ev) => {
      const s = ev.target.closest("[data-suggest]");
      if (s) send(s.dataset.suggest);
      const a = ev.target.closest("a[href^='#/']");
      if (a && window.innerWidth < 1280) close();
    });
    paintProvider();
    empty();
  }

  function empty() {
    const page = state.page && app.pages.get(state.page);
    const ideas = [
      page ? `Explain “${page.title}” in five bullets.` : "When does verilex skip a chain instead of running it?",
      "What makes a run inconclusive rather than red?",
      "How do I write my first word for a new product?",
      "What does --continue refuse, and why?",
      "What can the brain reach inside the sandbox?",
    ];
    $("#ask-body").innerHTML = `
      <div class="ask-empty">
        <h3>Ask anything.<br>Get the spec back.</h3>
        <p>Answers come from these ${B.pages.length} pages and link to the sections they use.${page ? ` You are on <b>${esc(page.title)}</b>.` : ""}</p>
        <div class="suggest">${ideas.map((q) => `<button type="button" data-suggest="${esc(q)}">${esc(q)}</button>`).join("")}</div>
      </div>`;
  }

  function open(opts = {}) {
    build();
    const el = panel();
    if (opts.page !== undefined) state.page = opts.page;
    else state.page = app.current();
    el.classList.add("open");
    el.setAttribute("aria-hidden", "false");
    document.body.classList.add("ask-open");
    if (!state.turns.length && !state.busy) empty();
    if (!state.loaded) loadProviders(false);
    const input = $("#ask-input");
    const page = state.page && app.pages.get(state.page);
    input.placeholder = page ? `Ask about ${page.title}…` : "Ask anything about verilex…";
    setTimeout(() => input.focus(), 60);
    if (opts.question) send(opts.question);
  }

  function close() {
    const el = panel();
    el.classList.remove("open");
    el.setAttribute("aria-hidden", "true");
    document.body.classList.remove("ask-open");
    closeSheet();
  }

  const isOpen = () => panel().classList.contains("open");
  const toggle = () => (isOpen() ? close() : open({}));

  function reset() {
    stop();
    state.turns = [];
    empty();
    $("#ask-input").focus();
  }

  function stop() {
    if (state.ctl) state.ctl.abort();
  }

  function submit() {
    const input = $("#ask-input");
    const q = input.value.trim();
    if (!q || state.busy) return;
    input.value = "";
    input.style.height = "auto";
    send(q);
  }

  function setBusy(b) {
    state.busy = b;
    const s = $("#ask-send");
    s.classList.toggle("stop", b);
    s.disabled = !b && !$("#ask-input").value.trim();
    s.setAttribute("aria-label", b ? "Stop" : "Send");
    s.innerHTML = b
      ? '<svg viewBox="0 0 24 24" fill="currentColor"><rect x="7" y="7" width="10" height="10" rx="2"/></svg>'
      : '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 19V5M5 12l7-7 7 7"/></svg>';
  }

  // ---------- one answer ----------

  async function send(question) {
    if (state.busy) return;
    if (!state.loaded) await loadProviders(false);
    const body = $("#ask-body");
    if (!state.turns.length) body.innerHTML = "";
    const p = state.current;
    body.insertAdjacentHTML("beforeend", `<div class="msg user"><div class="bubble">${esc(question)}</div></div>`);
    const msg = document.createElement("div");
    msg.className = "msg bot";
    msg.innerHTML = `<div class="by"><span class="mono-tile">${esc(TILE[p.id] || "?")}</span><span class="who">${esc(p.name)}${p.id !== "docs" ? " · " + esc(p.via) : ""}</span></div>
      <div class="status-line"><span class="scan"></span><span class="shimmer" data-status>${p.id === "docs" ? "Searching the docs" : `Reading ${B.pages.length} pages`}</span><span data-clock></span></div>
      <div class="prose" data-answer></div>`;
    body.appendChild(msg);
    msg.scrollIntoView({ block: "end", behavior: "smooth" });

    state.turns.push({ role: "user", content: question });
    const ctl = new AbortController();
    state.ctl = ctl;
    setBusy(true);
    const t0 = performance.now();
    const clock = setInterval(() => {
      const c = $("[data-clock]", msg);
      if (c) c.textContent = ((performance.now() - t0) / 1000).toFixed(1) + "s";
    }, 100);
    const view = answerView(msg);
    try {
      if (p.id === "docs") {
        quote(question, msg);
        state.turns.pop();
      } else {
        const text = p.id === "claudeai" ? await viaSample(ctl, view) : await viaServer(p, ctl, view);
        state.turns.push({ role: "assistant", content: text });
        view.finish(text, performance.now() - t0);
      }
    } catch (e) {
      state.turns.pop();
      if (e && e.name === "AbortError") view.stopped();
      else view.fail(e && e.message ? e.message : String(e), p);
    } finally {
      clearInterval(clock);
      state.ctl = null;
      setBusy(false);
    }
  }

  // answerView renders a streaming answer at most once a frame.
  function answerView(msg) {
    const out = $("[data-answer]", msg);
    const status = $(".status-line", msg);
    let text = "";
    let queued = false;
    let model = "";
    const paint = (final) => {
      const cited = new Map();
      out.innerHTML = VX.md.render(text, { link: app.citeLinker(cited) }).html;
      out.classList.toggle("caret-live", !final);
      return cited;
    };
    return {
      status(s) {
        const el = $("[data-status]", msg);
        if (el) el.textContent = s;
      },
      model(m) {
        model = m;
        const who = $(".who", msg);
        if (who && m && !who.textContent.includes(m)) who.textContent += " · " + m;
      },
      text(t) {
        text = t;
        if (status) status.hidden = true;
        if (queued) return;
        queued = true;
        requestAnimationFrame(() => {
          queued = false;
          paint(false);
          const body = $("#ask-body");
          if (body.scrollHeight - body.scrollTop - body.clientHeight < 160) body.scrollTop = body.scrollHeight;
        });
      },
      finish(t, ms) {
        text = t;
        if (status) status.remove();
        const cited = paint(true);
        out.classList.remove("caret-live");
        const chips = [...cited.values()].map((c) => `<a class="chip" href="${esc(app.hrefFor(c.slug, c.anchor))}">${esc(app.sectionTitle(c.slug, c.anchor))}</a>`);
        msg.insertAdjacentHTML("beforeend", `<div class="answer-foot">${chips.length ? `<span class="label">Sources</span>${chips.join("")}` : ""}
          <button class="chip" type="button" data-copy data-copy-text="${esc(t)}">Copy</button><span class="chip" title="Time to the whole answer">${(ms / 1000).toFixed(1)}s${model ? " · " + esc(model) : ""}</span></div>`);
      },
      stopped() {
        if (status) status.remove();
        out.classList.remove("caret-live");
        if (text) paint(true);
        msg.insertAdjacentHTML("beforeend", '<div class="answer-foot"><span class="label">Stopped</span></div>');
      },
      fail(message, p) {
        if (status) status.remove();
        out.classList.remove("caret-live");
        const fix = p.id === "docs" ? "" : ' <button class="chip" type="button" data-pick-provider>Choose another way to answer</button>';
        msg.insertAdjacentHTML("beforeend", `<div class="note error">${esc(message)}${fix}</div>`);
        const pick = $("[data-pick-provider]", msg);
        if (pick) pick.addEventListener("click", openSheet);
      },
    };
  }

  // viaServer streams an answer from the docs server: a signed-in plan or the API key.
  async function viaServer(p, ctl, view) {
    const res = await fetch("api/ask", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ provider: p.id, messages: state.turns, page: state.page || app.current() || "" }),
      signal: ctl.signal,
    });
    if (!res.ok) throw new Error((await res.text()).trim() || `The docs server answered ${res.status}`);
    let text = "";
    let failed = null;
    await readEvents(res, (event, data) => {
      if (event === "delta") {
        text += data.text;
        view.text(text);
      } else if (event === "status") {
        view.status(data.state === "thinking" ? "Thinking" : "Writing");
      } else if (event === "meta" && data.model) {
        view.model(data.model);
      } else if (event === "error") {
        failed = data.message;
      }
    });
    if (failed) throw new Error(failed);
    if (!text) throw new Error("The answer came back empty. Ask again, or choose another way to answer.");
    return text;
  }

  // viaSample asks Claude on the reader's own claude.ai account, when the page runs there.
  async function viaSample(ctl, view) {
    const page = state.page || app.current();
    const turns = state.turns.map((t, i) => ({
      role: t.role,
      content: i === state.turns.length - 1 && page ? `(I am reading the page \`${page}\`.)\n\n${t.content}` : t.content,
    }));
    const input = [{ role: "user", content: (B.askRules || "") + "\n\n" + corpus() }, ...turns];
    view.status("Waiting for Claude");
    try {
      const r = await state.sampler(input, { signal: ctl.signal, cache: false, onText: ({ text }) => view.text(text) });
      return r.text;
    } catch (e) {
      if (e && e.code === "cancelled") {
        const err = new Error("stopped");
        err.name = "AbortError";
        throw err;
      }
      const why = {
        not_granted: "This page is not allowed to use Claude in this view. Allow it from the artifact's permissions, or answer with the docs only.",
        sampling_disabled: "Claude is not available for this account here. Answer with the docs only.",
        rate_limited: "Claude is busy for this account right now. Wait a moment and ask again.",
        session_expired: "Your claude.ai session ended. Sign in again, then ask.",
        refused: "Claude declined to answer this question. Try rephrasing it, or search the docs.",
        prompt_too_large: "The question and the docs are too long together. Start a new conversation.",
      }[e && e.code];
      throw new Error(why || "Claude could not answer right now. Ask again in a moment.");
    }
  }

  let corpusText = null;
  // corpus is the docs as the docs server sends them: every page, each heading tagged with its anchor.
  function corpus() {
    if (corpusText) return corpusText;
    let s = "# The verilex documentation\n\n";
    for (const p of B.pages) {
      s += `<page id="${p.slug}" title="${p.title}" section="${p.section}">\n`;
      let h = 0;
      let fence = null;
      for (const line of p.markdown.split("\n")) {
        const f = /^\s*(```+|~~~+)/.exec(line);
        if (fence) {
          if (f && line.trim().startsWith(fence) && /^(`+|~+)$/.test(line.trim())) fence = null;
        } else if (f) {
          fence = f[1];
        } else if (/^#{1,6}\s/.test(line) && p.headings[h]) {
          s += `${line} {#${p.headings[h++].id}}\n`;
          continue;
        }
        s += line + "\n";
      }
      s += "</page>\n\n";
    }
    corpusText = s;
    return s;
  }

  // quote answers from the docs alone: the passages that match best, as written.
  function quote(question, msg) {
    VX.search.build && !VX.search.size().entries && VX.search.build(B);
    const found = VX.search.passages(question, 4);
    $(".status-line", msg).remove();
    const out = $("[data-answer]", msg);
    if (!found.length) {
      out.innerHTML = `<div class="note">Nothing in the docs matches that. Try other words, or press <kbd>${app.isMac ? "⌘" : "Ctrl"}</kbd> <kbd>K</kbd> to search.</div>`;
      return;
    }
    let html = "";
    for (const p of found) {
      const page = app.pages.get(p.page);
      const src = p.kind === "row" ? p.text : p.raw;
      const body = VX.md.render(src, { link: app.linker(page.file) }).html;
      html += `<div class="quote"><a class="src" href="${esc(app.hrefFor(p.page, p.anchor))}">${esc(app.sectionTitle(p.page, p.anchor))}</a><div class="prose">${VX.search.mark(body, question)}</div></div>`;
    }
    out.innerHTML = html;
    const better = state.server || state.sampler ? "Choose a model to get a written answer." : "Run the docs on your machine to answer with your own Claude, ChatGPT or Grok plan.";
    msg.insertAdjacentHTML("beforeend", `<div class="answer-foot"><span class="label">Quoted from the docs</span><button class="chip" type="button" data-pick-provider>${better}</button></div>`);
    $("[data-pick-provider]", msg).addEventListener("click", openSheet);
  }

  // readEvents parses a server-sent event stream from a fetch response.
  async function readEvents(res, on) {
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += dec.decode(value, { stream: true });
      let i;
      while ((i = buf.indexOf("\n\n")) >= 0) {
        const block = buf.slice(0, i);
        buf = buf.slice(i + 2);
        let event = "message";
        let data = "";
        for (const line of block.split("\n")) {
          if (line.startsWith("event: ")) event = line.slice(7);
          else if (line.startsWith("data: ")) data += line.slice(6);
        }
        try { on(event, data ? JSON.parse(data) : {}); } catch (e) { /* a malformed event is skipped */ }
      }
    }
  }

  // ---------- providers sheet and sign-in ----------

  function closeSheet() {
    const s = $(".sheet", panel());
    if (s) s.remove();
    if (state.signin) state.signin.abort();
    state.signin = null;
  }

  function openSheet() {
    closeSheet();
    const sheet = document.createElement("div");
    sheet.className = "sheet";
    panel().appendChild(sheet);
    paintSheet(sheet);
    if (state.server) loadProviders(true).then(() => sheet.isConnected && paintSheet(sheet));
  }

  function paintSheet(sheet) {
    const rows = state.providers
      .map((p) => {
        const [dot, label] = stateOf(p);
        let act = "";
        if (p.enabled === false) act = "";
        else if (!p.installed) act = `<button class="btn small" type="button" data-install="${esc(p.id)}">Install</button>`;
        else if (p.signedIn === false && p.canLogin) act = `<button class="btn small primary" type="button" data-login="${esc(p.id)}">Sign in</button>`;
        else if (p.signedIn === false) act = `<button class="btn small" type="button" data-howto="${esc(p.id)}">How to sign in</button>`;
        else act = `<button class="btn small${p.id === state.current.id ? "" : " primary"}" type="button" data-use="${esc(p.id)}">${p.id === state.current.id ? "In use" : "Use"}</button>`;
        if (p.canLogin && p.signedIn === true) act += `<button class="chip" type="button" data-login="${esc(p.id)}">Switch account</button>`;
        return `<div class="provider${p.id === state.current.id ? " current" : ""}${p.enabled === false || !p.installed ? " disabled" : ""}">
          <span class="mono-tile">${esc(TILE[p.id] || "?")}</span>
          <div><div class="meta-line">${esc(p.name)}${p.via && p.id !== "docs" ? ` <span class="chip" style="height:20px;font-size:10.5px">${esc(p.via)}</span>` : ""}</div>
            <div class="plan">${esc(p.plan || "")}</div>
            <div class="state-line"><span class="dot ${dot}"></span>${esc(p.enabled === false ? p.reason || "off" : label)}</div></div>
          <div class="act">${act}</div>
        </div>`;
      })
      .join("");
    const local = state.server && state.local;
    const hint = local
      ? `<p class="note" style="margin-top:14px">Plans answer on this machine, through the CLI you signed in to. The docs server never sees your token: it runs <code>claude</code>, <code>codex</code> or <code>grok</code> with every tool off, in an empty folder, with the docs as its only input.</p>`
      : `<div class="note" style="margin-top:14px">Answer with your own <b>Claude</b>, <b>ChatGPT</b> or <b>Grok</b> plan: run the docs on your machine and sign in there. Plans are personal, so a page served to others never uses one.
          <div class="install" style="margin-top:10px"><code>${esc(CLONE)}</code><button class="copy" type="button" data-copy data-copy-text="${esc(CLONE)}">Copy</button></div></div>`;
    sheet.innerHTML = `<h3>Answer with</h3><p>Pick the model that writes answers. Every answer is grounded in these ${B.pages.length} pages.</p>
      <div class="providers">${rows}</div><div id="signin-slot"></div>${hint}`;
    sheet.onclick = (ev) => {
      const t = ev.target.closest("button");
      if (!t) return;
      const find = (id) => state.providers.find((p) => p.id === id);
      if (t.dataset.use) choose(find(t.dataset.use));
      else if (t.dataset.login) signIn(find(t.dataset.login), sheet);
      else if (t.dataset.install) {
        const p = find(t.dataset.install);
        if (/^(npm|brew|curl) /.test(p.install || "")) app.copy(p.install, "Copied: " + p.install);
        else app.toast(p.install || "See the vendor's site to install it");
      } else if (t.dataset.howto) {
        const p = find(t.dataset.howto);
        app.copy(p.signIn, "Copied: " + p.signIn + " — run it in a terminal, then reopen this menu");
      }
    };
  }

  // signIn runs the vendor's own sign-in command on the docs server and shows what it asks for.
  async function signIn(p, sheet) {
    if (state.signin) state.signin.abort();
    const ctl = new AbortController();
    state.signin = ctl;
    const slot = $("#signin-slot", sheet);
    const paste = p.id === "claude";
    slot.innerHTML = `<div class="signin">
      <h4>Sign in to ${esc(p.name)} with ${esc(p.via)}</h4>
      <ol>
        <li>Your browser opens ${esc(p.name)}'s sign-in page. If it does not, use the button below.</li>
        ${paste ? "<li>If the page shows a code after you sign in, paste it here.</li>" : "<li>If a code appears below, enter it on the sign-in page.</li>"}
      </ol>
      <div data-url></div><div data-code></div>
      ${paste ? '<form data-paste><input type="text" placeholder="Paste the code" aria-label="Sign-in code" autocomplete="one-time-code"><button class="btn small primary" type="submit">Send</button></form>' : ""}
      <div class="waiting"><span class="scan"></span><span data-wait>Starting <code>${esc(p.signIn)}</code>…</span></div>
      <details><summary>What the CLI printed</summary><pre data-log></pre></details>
    </div>`;
    const box = $(".signin", slot);
    const form = $("[data-paste]", box);
    if (form) {
      form.addEventListener("submit", async (ev) => {
        ev.preventDefault();
        const input = $("input", form);
        const code = input.value.trim();
        if (!code) return;
        const r = await fetch("api/login/input", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ provider: p.id, text: code }) });
        $("[data-wait]", box).textContent = r.ok ? "Code sent. Finishing sign-in…" : "The sign-in is no longer running. Start it again.";
        input.value = "";
      });
    }
    try {
      const res = await fetch("api/login", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ provider: p.id }), signal: ctl.signal });
      if (!res.ok) throw new Error((await res.text()).trim());
      await readEvents(res, (event, data) => {
        if (event === "output") {
          const log = $("[data-log]", box);
          log.textContent += data.text;
          log.scrollTop = log.scrollHeight;
          $("[data-wait]", box).textContent = "Waiting for you to sign in…";
        } else if (event === "url") {
          $("[data-url]", box).innerHTML = `<a class="btn small primary" href="${esc(data.url)}" target="_blank" rel="noopener" style="margin-bottom:12px">Open the sign-in page ↗</a>`;
        } else if (event === "code") {
          $("[data-code]", box).innerHTML = `<div class="devcode">${esc(data.code)}</div>`;
        } else if (event === "status") {
          const done = data.signedIn === true;
          $(".waiting", box).innerHTML = done
            ? `<span class="dot ok"></span><span>Signed in. ${esc(p.name)} answers from now on.</span>`
            : `<span class="dot warn"></span><span>Sign-in did not finish. Start it again, or run <code>${esc(p.signIn)}</code> in a terminal.</span>`;
          if (done) {
            loadProviders(true).then(() => {
              const fresh = state.providers.find((x) => x.id === p.id);
              if (fresh) choose(fresh);
            });
          }
        }
      });
    } catch (e) {
      if (e.name === "AbortError") return;
      $(".waiting", box).innerHTML = `<span class="dot warn"></span><span>${esc(e.message || "The sign-in could not start.")}</span>`;
    } finally {
      if (state.signin === ctl) state.signin = null;
    }
  }

  function pageChanged(slug) {
    if (!panel().dataset.built) return;
    state.page = slug;
    const input = $("#ask-input");
    const page = slug && app.pages.get(slug);
    input.placeholder = page ? `Ask about ${page.title}…` : "Ask anything about verilex…";
    if (!state.turns.length && !state.busy) empty();
  }

  VX.ask = {
    init(a) {
      app = a;
      // Learn the providers in the background, so the first question does not wait for them.
      setTimeout(() => loadProviders(false), 600);
    },
    open, close, toggle, isOpen, pageChanged,
  };
})();
