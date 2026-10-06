# Continue a kept instance

`verilex run --continue <run> '<chain>'` takes over the instance an earlier `--keep` run left alive. It skips the longest prefix of the chain the instance's history already proves, runs the project's `refresh` frame step and the doctor, runs the remaining words on that instance, and cleans up unless `--keep` is given again. It refuses when a word that changes state would run twice or out of order.

## Sub-features

- `continue-prefix` skips the proven prefix and runs only the rest on the kept instance.
- `continue-refresh` runs `refresh` then `doctor` instead of `launch`.
- `continue-handover` marks the kept run `cleanup=continued`; `cleanup` and `--continue` on it point at the run that took over.
- `continue-readonly-rerun` re-runs a changed `read_only` word on the kept instance.
- `continue-refuse-state` refuses when a changed state-changing word already drove the instance.
- `continue-refuse-provisional` refuses when the history holds a provisional state-changing word.
- `continue-refuse-fresh` refuses `--fresh` with `--continue`.
- `continue-refuse-unknown` refuses an id that is not a run, or a run that kept no instance.
- `continue-json` prints the continuing run's full record.
- `continue-refresh-fail` makes the run inconclusive when `refresh` refuses the kept instance.
- `continue-refuse-running` refuses a run that is still going.
- `continue-one-taker` hands a kept instance to exactly one of several concurrent continuing runs.

## How to get to it (user POV)

- Run `verilex run --keep '<chain>'`, then `verilex run --continue <run> '<chain>' [--keep] [--json]`.

## Driving it with vx

Preconditions:

- A session that passes the doctor. Every step except `continue-refuse-provisional` needs the admitted baseline (`admit-all`).

- **Refuse provisional.** Before `admit-all`, run `"$S/vx" cont-keep-prov verilex run --keep 'store-open | item-stored apple'` (run `<P>`), then `"$S/vx" cont-prov verilex run --continue <P> 'store-open | item-stored apple | item-listed apple'`. Stderr `verilex: refused: store-open: provisional; only admitted words are skipped; the kept instance already holds the effects of store-open ...: run without --continue`, exit `2`. Tear down with `"$S/vx" cont-prov-cleanup verilex cleanup <P>`.
- **Keep.** After `admit-all`, run `"$S/vx" cont-keep verilex run --keep 'store-open | item-stored apple'` (run `<KEPT>`).
- **Refuse fresh.** Run `"$S/vx" cont-fresh verilex run --continue <KEPT> --fresh 'store-open | item-stored apple | item-listed apple'`. Stderr `verilex: refused: --fresh would run every word again on the kept instance; run without --continue`, exit `2`.
- **Refuse unknown.** Run `"$S/vx" cont-unknown verilex run --continue 123-nope 'store-open'`. Stderr `verilex: refused: 123-nope is not a run of tally`, exit `2`. For a run that was cleaned up (any `cleanup=done` run `<DONE>`), `"$S/vx" cont-not-kept verilex run --continue <DONE> 'store-open'` prints `refused: <DONE> kept no instance (cleanup=done); run with --keep first`.
- **Prefix.** Run `"$S/vx" cont verilex run --continue <KEPT> --keep 'store-open | item-stored apple | item-listed apple'`. Output `green: 3 green, continued <KEPT>, 2 skipped: proven on its instance by run <KEPT>; run <C>` and a `kept:` line for `<C>`.
- **Prefix, second view.** Run `"$S/vx" cont-store cat "$S/stores/tally-<KEPT>/store.json"`. It holds `{"items": ["apple"]}` with one `apple`: `store-open` and `item-stored` did not run again.
- **Refresh.** Run `"$S/vx" cont-frame python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); print(r["continues"], [f["step"] for f in r["frame"]], [(w["word"], w.get("relies_on", "")) for w in r["words"]])' "$S/home/tally/runs/<C>/run.json"`. It prints `<KEPT> ['refresh', 'doctor'] [('store-open', '<KEPT>'), ('item-stored', '<KEPT>'), ('item-listed', '')]`.
- **Handover.** Run `"$S/vx" cont-runs verilex runs`. `<KEPT>` shows `cleanup=continued` and `<C>` shows `cleanup=kept`. Run `"$S/vx" cont-cleanup-old verilex cleanup <KEPT>`: `refused: <KEPT> was continued by <C>; clean up that run instead`. Run `"$S/vx" cont-again-old verilex run --continue <KEPT> 'store-open'`: `refused: <KEPT> was continued by <C>; continue that run instead`.
- **Read-only rerun.** Run `echo "probe" >> "$S/tally/.verilex/words/item-listed/word.md"`, then `"$S/vx" cont-ro verilex run --continue <C> --keep 'store-open | item-stored apple | item-listed apple'`. Output `green: 3 green, continued <C>, 2 skipped: ...; run <D>`: only `item-listed` ran. Restore with `sed -i '$d' "$S/tally/.verilex/words/item-listed/word.md"`.
- **Refuse state change.** Run `echo "probe" >> "$S/tally/.verilex/words/item-stored/word.md"`, then `"$S/vx" cont-state verilex run --continue <D> 'store-open | item-stored apple | item-listed apple'`. Stderr `verilex: refused: item-stored apple: word changed; the kept instance already holds the effects of item-stored apple ...`, exit `2`. Run `"$S/vx" cont-state-store cat "$S/stores/tally-<KEPT>/store.json"`: still one `apple`. Restore with `sed -i '$d' "$S/tally/.verilex/words/item-stored/word.md"` and confirm `"$S/vx" cont-check verilex check` prints `check: no drift (3 admitted)`.
- **Teardown.** Run `"$S/vx" cont-cleanup verilex cleanup <D>`. It prints `cleanup: done` and `$S/stores` is empty.
- **JSON.** Run `"$S/vx" cont-json-keep verilex run --keep 'store-open | item-stored apple'` (run `<K2>`), then `"$S/vx" cont-json verilex run --continue <K2> --keep --json 'store-open | item-stored apple | item-listed apple'`. Exit `0`. Read the `stdout` file in the evidence directory that `vx` printed: `"verdict": "green"`, `"continues"` and `"owner"` are `<K2>`, `store-open` and `item-stored` carry `"relies_on": "<K2>"`, `"rerun"` is `item-listed apple: not run on the kept instance yet`, and `"cleanup": "kept"`. Note the continuing run `<C2>` from `"run"`.
- **Refresh fails.** Run `echo foreign > "$S/stores/tally-<K2>/owner"`, then `"$S/vx" cont-refresh-fail verilex run --continue <C2> 'store-open | item-stored apple | item-listed apple'`. Output `inconclusive: 0 green, 3 not run, continued <C2>; run <R>` with `inconclusive  refresh: exit 1` and its `frame-refresh` evidence path, exit `2`.
- **Refresh fails, second view.** Run `"$S/vx" cont-refresh-why cat "$S/home/tally/runs/<R>/frame-refresh/stderr"`: `refresh: ... is not owned by run <K2>; refusing to touch it`. Run `"$S/vx" cont-refresh-runs verilex runs`: `<C2>` shows `cleanup=continued` and `<R>` shows `inconclusive  cleanup=done`, yet `ls "$S/stores"` still lists `tally-<K2>`, because the tally cleanup frame refuses a store it does not own. The session `cleanup` helper removes it and reports `1 store(s) were still alive`.

- **Running and one taker.** Use [parallel.md](parallel.md) `parallel-refuse-running` and `parallel-one-taker`.

## Gotchas

- `--continue` with provisional words always refuses, because the kept instance already holds `store-open`'s effects. Admit first.
- The instance keeps the first run's id: the store stays `$S/stores/tally-<KEPT>` across every continuing run.
- Editing an admitted word also makes it drift-suspect (`the word's files changed since onboarding`). Restore the file before the next recipe.
- Results proven on a continued instance never enter the ledger. They do not make a fresh `verilex run` skip.
- After **Refresh fails**, no run owns the tampered store, so `verilex cleanup` cannot reach it. Only the session `cleanup` helper removes it.
- A refused `--continue` adds no run and leaves the instance and its owner unchanged.
