// agents.js: the home page's launcher demo. An orchestrating agent starts three verilex-agent
// runs on the tally sample, one ticket each and one shared ledger. Each brain works in its own
// sandboxed CLI session; verilex runs the words outside it; only verilex's verdict comes back.
// The sessions are drawn, but every verilex command, output, run id and verdict below is from a
// real run, the one docs/launcher.md walks through.
(function () {
  "use strict";
  const VX = (window.VX = window.VX || {});
  const esc = (s) => VX.md.esc(s);
  const reduce = window.matchMedia("(prefers-reduced-motion: reduce)");
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

  const CHAIN = [["store-open", ""], ["item-stored", "apple"], ["item-listed", "apple"]];
  const FIRST = "1791512777-6a4f0dacff78";
  const LEDGER = "~/vx/ledger";

  const LANES = [
    {
      key: "cc", cli: "Claude Code", bin: "claude", harness: "claude-code", ticket: "apple.yaml", home: "~/vx/apple",
      ask: "prove a stored apple is listed",
      calls: [
        ["verilex index --intent 'prove a stored apple is listed'", "intent: 2 claim(s) for 'prove a stored apple is listed'", 2],
        ["verilex plan --claim item-listed --claim item-added", '{"format": "verilex-claim-plan-1", "intent": "given",', 31],
        ["verilex run --claim item-listed --claim item-added", `{"run": "${FIRST}", "project": "tally",`, 148],
      ],
      words: CHAIN, states: ["green", "green", "green"],
      said: "Both claims hold: the apple is stored and listed.",
      verdict: "green", exit: 0, run: FIRST, bytes: 4006, passes: 3, pace: 1,
    },
    {
      key: "cx", cli: "Codex", bin: "codex", harness: "codex", ticket: "store.yaml", home: "~/vx/store",
      ask: "prove the store opens",
      calls: [
        ["verilex index --intent 'prove the store opens'", "intent: 1 claim(s) for 'prove the store opens'", 1],
        ["verilex plan --claim store-opened", '{"format": "verilex-claim-plan-1", "intent": "given",', 36],
        ["verilex run --claim store-opened", '{"run": "1791512778-c4445f6c595f", "project": "tally",', 69],
      ],
      words: [CHAIN[0]], states: ["skip"],
      said: "The store opens. Nothing had to run again.",
      verdict: "green", exit: 0, run: "1791512778-c4445f6c595f", skipped: true, bytes: 1713, passes: 0, pace: 1.45,
    },
    {
      key: "pi", cli: "pi", bin: "pi", harness: "pi", ticket: "diff.yaml", home: "~/vx/diff",
      ask: "prove nothing this change touched broke · diff HEAD",
      calls: [
        ["verilex index --changed bin/tally", "changed: 3 claim(s); 3 of 3 known chain(s) must re-run", 6],
        ["verilex plan --changed bin/tally", '{"format": "verilex-claim-plan-1", "intent": "prove nothing this change touched broke",', 42],
        ["verilex run --changed bin/tally", '{"run": "1791512778-26449ce7a66e", "project": "tally",', 160],
      ],
      words: CHAIN, states: ["green", "green", "red"],
      said: "Looks fine to me: nothing this change touched is broken.",
      verdict: "red", exit: 1, run: "1791512778-26449ce7a66e", bytes: 4428, passes: 2, pace: 1.15,
      reason: "item-listed apple: tally list printed [], not apple",
      claim: {
        got: "tally list printed [], not apple",
        evidence: "~/vx/diff/tally/runs/1791512778-26449ce7a66e/03-item-listed",
        next: "verilex run --fresh --claim 'item-listed' --changed 'bin/tally'",
      },
    },
  ];

  const LOCK = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="5" y="11" width="14" height="10" rx="2"/><path class="shackle" d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>';
  const DB = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><ellipse cx="12" cy="6" rx="7" ry="3"/><path d="M5 6v6c0 1.7 3.1 3 7 3s7-1.3 7-3V6M5 12v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6"/></svg>';
  const command = (l) => `verilex-agent --ticket ${l.ticket} --home ${l.home} --ledger ${LEDGER} --allow-harness`;
  const bytes = (n) => n.toLocaleString("en-US") + " B";

  // Each harness's session, drawn the way its CLI looks: its banner, the prompt, each tool call
  // and its result, and the brain's closing message.
  const STYLE = {
    cc: {
      banner: () => `<div class="cc-banner"><span><span class="cc-star">✻</span> Welcome to <b>Claude Code</b>!</span><span class="dim">cwd: ~/tally</span></div>`,
      user: (l) => `<span class="gut cc-prompt">&gt;</span><span>${esc(l.ask)}</span>`,
      call: (c) => `<span class="gut cc-dot">●</span><span><b>Bash</b>(${esc(c)})</span>`,
      result: (out, more) => `<span class="gut">⎿</span><span class="out">${esc(out)}</span>${more ? `<span class="more">… +${more} lines</span>` : ""}`,
      running: `<span class="gut">⎿</span><span class="out shimmer">Running…</span>`,
      say: (s) => `<span class="gut cc-dot say">●</span><span>${esc(s)}</span>`,
    },
    cx: {
      banner: () => `<div class="cx-banner"><b>&gt;_ Codex</b><span class="dim">directory: ~/tally</span></div>`,
      user: (l) => `<span class="gut cx-prompt">›</span><span>${esc(l.ask)}</span>`,
      call: (c) => `<span class="gut cx-dot">•</span><span><b>Ran</b> ${esc(c)}</span>`,
      result: (out, more) => `<span class="gut">└</span><span class="out">${esc(out)}</span>${more ? `<span class="more">… +${more} lines</span>` : ""}`,
      running: `<span class="gut">└</span><span class="out shimmer">Working…</span>`,
      say: (s) => `<span class="gut cx-dot say">•</span><span>${esc(s)}</span>`,
    },
    pi: {
      banner: () => `<div class="pi-banner"><b>π</b><span>pi</span><span class="dim">~/tally</span></div>`,
      user: (l) => `<span class="gut pi-tag usr">you</span><span>${esc(l.ask)}</span>`,
      call: (c) => `<span class="gut pi-tag">bash</span><span>${esc(c)}</span>`,
      result: (out, more) => `<span class="gut"></span><span class="out">${esc(out)}</span>${more ? `<span class="more">+${more}</span>` : ""}`,
      running: `<span class="gut"></span><span class="out shimmer">running…</span>`,
      say: (s) => `<span class="gut pi-tag me">pi</span><span>${esc(s)}</span>`,
    },
  };

  function laneHTML(l, i) {
    return `
      <article class="lane lane-${l.key}" data-i="${i}">
        <div class="lane-meta">
          <span class="lane-ticket">${esc(l.ticket)}</span>
          <span class="lane-harness"><span class="dim">harness: </span>${esc(l.harness)}</span>
          <span class="seal" title="The brain runs in a sandbox: bubblewrap on Linux, sandbox-exec on macOS">${LOCK}<span data-seal>sandbox</span></span>
        </div>
        <div class="cli cli-${l.key}">
          <div class="cli-bar"><i></i><i></i><i></i><span>${esc(l.bin)} — ~/tally</span></div>
          <div class="cli-body" data-body>${STYLE[l.key].banner(l)}</div>
        </div>
        <div class="vx">
          <div class="vx-head"><b>verilex</b><span><span class="long">runs every word, </span>outside the sandbox</span></div>
          <ol class="vx-chain">${l.words
            .map((w, k) => `<li data-k="${k}"><span class="n">${String(k + 1).padStart(2, "0")}</span><b>${esc(w[0])}</b>${w[1] ? `<em>${esc(w[1])}</em>` : ""}<span class="st"></span></li>`)
            .join("")}</ol>
          <div class="vx-foot" data-foot></div>
        </div>
      </article>`;
  }

  function html() {
    return `
      <div class="stage-bar">
        <div class="term-dots" aria-hidden="true"><i></i><i></i><i></i></div>
        <span class="stage-title">your agent · ~/tally</span>
        <span class="ledger" data-ledger title="--ledger ${LEDGER}: every run reads and writes the same passes">${DB}<span>shared ledger</span><b data-passes>0</b><span>passes</span></span>
        <button type="button" class="replay" data-replay aria-label="Replay the demo">↻ replay</button>
      </div>
      <div class="you">
        <div class="you-label"><span class="beacon"></span>Your agent starts one launcher per ticket</div>
        <div class="you-cmds">${LANES.map((l, i) => `<div class="you-cmd" data-cmd="${i}"><span class="t-prompt">$</span> <span data-typed></span></div>`).join("")}</div>
      </div>
      <svg class="wires out" viewBox="0 0 600 44" preserveAspectRatio="none" aria-hidden="true">
        ${[100, 300, 500].map((x, i) => `<path class="wire" d="M300 0 C300 22 ${x} 22 ${x} 44"/><path class="wire-flow" pathLength="1" data-out="${i}" d="M300 0 C300 22 ${x} 22 ${x} 44"/>`).join("")}
      </svg>
      <div class="lanes">${LANES.map(laneHTML).join("")}</div>
      <svg class="wires in" viewBox="0 0 600 44" preserveAspectRatio="none" aria-hidden="true">
        ${[100, 300, 500].map((x, i) => `<path class="wire" d="M${x} 0 C${x} 22 300 22 300 44"/><path class="wire-flow" pathLength="1" data-in="${i}" d="M${x} 0 C${x} 22 300 22 300 44"/>`).join("")}
      </svg>
      <div class="back">
        <div class="back-head">
          <div class="you-label"><span class="beacon"></span>Back to your agent: verilex's own JSON, nothing else</div>
          <span class="meter"><b data-meter>0 B</b> <span class="dim">· or one line each with --text</span></span>
        </div>
        <div class="rows" data-rows><div class="rows-wait"><span class="shimmer">waiting on three launchers…</span></div></div>
        <div class="split">
          <div><h4>Comes back</h4><ul><li>verilex's verdict, byte for byte</li><li>evidence paths, not the evidence</li><li>the command that retries a red</li></ul></div>
          <div><h4>Stays in each sandbox</h4><ul><li>the brain's reasoning and tool calls</li><li>its last message, never the verdict</li><li>its run files, removed unless --keep-work</li></ul></div>
        </div>
      </div>
      <p class="stage-note">The three sessions are drawn. Every verilex command, output, run id and verdict is from a real run on the tally sample; <a href="#/launcher/example-three-sessions-one-ledger">the launcher docs</a> walk through it.</p>`;
  }

  function rowHTML(l) {
    const json = l.verdict === "red"
      ? `{"run": "${l.run}", …, "verdict": "red", "reason": "${l.reason}", …}`
      : `{"run": "${l.run}", …, "verdict": "green", …${l.skipped ? ', "skipped": true, …' : ""}}`;
    let claim = "";
    if (l.claim) {
      claim = `<div class="claim">${["got", "evidence", "next"]
        .map((k) => `<span class="k">"${k}"</span><span class="v">${VX.md.highlight(JSON.stringify(l.claim[k]), "json").html}</span>`)
        .join("")}</div>`;
    }
    return `<div class="row row-${l.verdict}">
      <span class="pill pill-${l.verdict}">${l.verdict}</span>
      <span class="who">${esc(l.harness)}</span>
      <code class="json">${VX.md.highlight(json, "json").html}</code>
      <span class="size">${bytes(l.bytes)} · exit ${l.exit}</span>
      ${claim}
    </div>`;
  }

  // ---------- the timeline ----------

  let root = null;
  let io = null;
  let token = 0;
  let visible = false;
  let started = false;

  function stale(t) {
    if (t !== token || !root.isConnected) throw new Error("stale");
  }
  const wait = async (ms, t) => {
    await sleep(ms);
    stale(t);
  };
  const $ = (s, el = root) => el.querySelector(s);
  const $$ = (s, el = root) => Array.from(el.querySelectorAll(s));

  function toEnd(lane) {
    const body = $("[data-body]", lane);
    body.scrollTop = body.scrollHeight;
  }

  function line(lane, cls, inner) {
    const div = document.createElement("div");
    div.className = "cli-line " + cls;
    div.innerHTML = inner;
    $("[data-body]", lane).appendChild(div);
    toEnd(lane);
    return div;
  }

  function flow(sel, tone) {
    const p = $(sel);
    if (!p) return;
    p.classList.remove("go");
    void p.getBoundingClientRect();
    p.classList.add("go", tone);
    p.previousElementSibling.classList.add("lit", tone);
  }

  let passes = 0;
  let meter = 0;
  function addPasses(n) {
    if (!n) return;
    passes += n;
    const b = $("[data-passes]");
    b.textContent = String(passes);
    const chip = $("[data-ledger]");
    chip.classList.remove("bump");
    void chip.offsetWidth;
    chip.classList.add("bump");
  }

  async function countTo(target, t) {
    const el = $("[data-meter]");
    const from = meter;
    meter = target;
    const steps = 14;
    for (let s = 1; s <= steps; s++) {
      el.textContent = bytes(Math.round(from + ((target - from) * s) / steps));
      await wait(22, t);
    }
  }

  async function typeCommand(i, t) {
    const el = $(`[data-cmd="${i}"] [data-typed]`);
    $(`[data-cmd="${i}"]`).classList.add("on");
    const full = command(LANES[i]);
    for (let k = 0; k <= full.length; k += 4) {
      el.innerHTML = esc(full.slice(0, k)) + '<span class="caret"></span>';
      await wait(12, t);
    }
    el.innerHTML = VX.md.highlight("$ " + full, "console").html.replace(/^<span class="t-prompt">\$<\/span> /, "");
  }

  async function runLane(i, t, firstDone) {
    const l = LANES[i];
    const lane = $(`.lane[data-i="${i}"]`);
    const S = STYLE[l.key];
    const p = (ms) => wait(ms * l.pace, t);
    flow(`[data-out="${i}"]`, "tone-signal");
    await wait(260, t);
    lane.classList.add("on");
    $("[data-seal]", lane).textContent = "sealing…";
    await p(260);
    lane.classList.add("sealed");
    $("[data-seal]", lane).textContent = "sandbox";
    line(lane, "user", S.user(l));
    await p(340);
    for (let c = 0; c < l.calls.length; c++) {
      const [cmd, out, more] = l.calls[c];
      if (c === 2 && firstDone) {
        await firstDone; // the shared ledger holds the first run's passes by now
        stale(t);
      }
      line(lane, "call", S.call(cmd));
      await p(260);
      if (c < 2) {
        line(lane, "res", S.result(out, more));
        await p(420);
        continue;
      }
      const running = line(lane, "res", S.running);
      lane.classList.add("verilex");
      await runChain(lane, l, t);
      running.innerHTML = S.result(out, more);
      await p(220);
    }
    // The verdict is verilex's, whatever the brain says last.
    const foot = $("[data-foot]", lane);
    foot.innerHTML = `<span class="pill pill-${l.verdict}">${l.verdict}</span><span class="dim">exit ${l.exit}</span><span class="run">run ${l.run}</span>`;
    foot.classList.add("on");
    addPasses(l.passes);
    await p(380);
    const said = line(lane, "say", S.say(l.said));
    await p(320);
    said.classList.add("ignored");
    said.insertAdjacentHTML("beforeend", '<span class="ignored-tag">not the verdict</span>');
    toEnd(lane);
    await wait(260, t);
    flow(`[data-in="${i}"]`, "tone-" + l.verdict);
    await wait(320, t);
    $("[data-rows]").insertAdjacentHTML("beforeend", rowHTML(l));
    lane.classList.add("done", "done-" + l.verdict);
    await countTo(meter + l.bytes, t);
  }

  async function runChain(lane, l, t) {
    const rows = $$(".vx-chain li", lane);
    for (let k = 0; k < rows.length; k++) {
      const st = $(".st", rows[k]);
      if (l.states[k] === "skip") {
        rows[k].classList.add("checking");
        st.textContent = "stamp…";
        await wait(520, t);
        rows[k].classList.remove("checking");
        rows[k].classList.add("skip");
        st.textContent = "skipped";
        rows[k].insertAdjacentHTML("beforeend", `<span class="relies">pass from run ${FIRST}</span>`);
        continue;
      }
      rows[k].classList.add("run");
      st.textContent = "running";
      await wait(560, t);
      rows[k].classList.remove("run");
      rows[k].classList.add(l.states[k]);
      st.textContent = l.states[k];
      if (l.states[k] === "red") {
        rows[k].insertAdjacentHTML("beforeend", `<span class="relies">${VX.md.esc(l.claim.got)}</span>`);
      }
      await wait(140, t);
    }
  }

  function reset() {
    passes = 0;
    meter = 0;
    root.classList.remove("complete");
    root.innerHTML = html();
  }

  async function play() {
    const t = ++token;
    reset();
    try {
      let markFirst;
      const firstDone = new Promise((r) => (markFirst = r));
      const lanes = [];
      for (let i = 0; i < LANES.length; i++) {
        await typeCommand(i, t);
        const lane = runLane(i, t, i === 1 ? firstDone : null).then(() => i === 0 && markFirst());
        lane.catch(() => {}); // a newer play may cut the lanes short before they are awaited
        lanes.push(lane);
        await wait(140, t);
      }
      await Promise.all(lanes);
      root.classList.add("complete");
      await wait(7000, t);
      while (!visible || document.hidden) await wait(500, t);
      play();
    } catch (e) {
      /* a newer play took over */
    }
  }

  // finish renders the end state at once, for readers who prefer less motion.
  function finish() {
    reset();
    root.classList.add("static", "complete");
    LANES.forEach((l, i) => {
      $(`[data-cmd="${i}"]`).classList.add("on");
      $(`[data-cmd="${i}"] [data-typed]`).innerHTML = VX.md.highlight("$ " + command(l), "console").html.replace(/^<span class="t-prompt">\$<\/span> /, "");
      const lane = $(`.lane[data-i="${i}"]`);
      const S = STYLE[l.key];
      lane.classList.add("on", "sealed", "done", "done-" + l.verdict);
      line(lane, "user", S.user(l));
      for (const [cmd, out, more] of l.calls) {
        line(lane, "call", S.call(cmd));
        line(lane, "res", S.result(out, more));
      }
      const said = line(lane, "say ignored", S.say(l.said));
      said.insertAdjacentHTML("beforeend", '<span class="ignored-tag">not the verdict</span>');
      toEnd(lane);
      $$(".vx-chain li", lane).forEach((row, k) => {
        row.classList.add(l.states[k]);
        $(".st", row).textContent = l.states[k] === "skip" ? "skipped" : l.states[k];
        if (l.states[k] === "skip") row.insertAdjacentHTML("beforeend", `<span class="relies">pass from run ${FIRST}</span>`);
        if (l.states[k] === "red") row.insertAdjacentHTML("beforeend", `<span class="relies">${esc(l.claim.got)}</span>`);
      });
      const foot = $("[data-foot]", lane);
      foot.innerHTML = `<span class="pill pill-${l.verdict}">${l.verdict}</span><span class="dim">exit ${l.exit}</span><span class="run">run ${l.run}</span>`;
      foot.classList.add("on");
      passes += l.passes;
      $("[data-rows]").insertAdjacentHTML("beforeend", rowHTML(l));
      meter += l.bytes;
    });
    $$(".wire").forEach((w) => w.classList.add("lit", "tone-signal"));
    LANES.forEach((l, i) => $(`[data-in="${i}"]`).previousElementSibling.classList.replace("tone-signal", "tone-" + l.verdict));
    $("[data-passes]").textContent = String(passes);
    $("[data-meter]").textContent = bytes(meter);
  }

  function mount(el) {
    root = el;
    started = false;
    token++;
    if (io) io.disconnect();
    if (reduce.matches) {
      finish();
      return;
    }
    reset();
    el.addEventListener("click", (ev) => {
      if (ev.target.closest("[data-replay]")) play();
    });
    io = new IntersectionObserver((entries) => {
      visible = entries.some((e) => e.isIntersecting);
      if (visible && !started) {
        started = true;
        play();
      }
    }, { rootMargin: "0px 0px -30% 0px" });
    io.observe(el);
  }

  VX.agents = { mount };
})();
