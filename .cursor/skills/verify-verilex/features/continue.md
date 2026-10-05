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

## How to get to it (user POV)

- Run `verilex run --keep '<chain>'`, then `verilex run --continue <run> '<chain>' [--keep] [--json]`.

## Driving it with vx

Preconditions:

- A session that passes the doctor. Every step except `continue-refuse-provisional` needs the admitted baseline (`admit-all`).

- **Refuse provisional.** Before `admit-all`, run `"$S/vx" cont-keep-prov verilex run --keep 'store-open | item-stored apple'` (run `<P>`), then `"$S/vx" cont-prov verilex run --continue <P> 'store-open | item-stored apple | item-listed apple'`. Stderr `verilex: refused: store-open: provisional; only admitted words are skipped; the kept instance already holds the effects of store-open ...: run without --continue`, exit `2`. Tear down with `"$S/vx" cont-prov-cleanup verilex cleanup <P>`.
- **Keep.** After `admit-all`, run `"$S/vx" cont-keep verilex run --keep 'store-open | item-stored apple'` (run `<KEPT>`).
- **Refuse fresh.** Run `"$S/vx" cont-fresh verilex run --continue <KEPT> --fresh 'store-open | item-stored apple | item-listed apple'`. Stderr `verilex: refused: --fresh would run every word again on the kept instance; run without --continue`, exit `2`.
- **Prefix.** Run `"$S/vx" cont verilex run --continue <KEPT> --keep 'store-open | item-stored apple | item-listed apple'`. Output `green: 3 green, continued <KEPT>, 2 skipped: proven on its instance by run <KEPT>; run <C>` and a `kept:` line for `<C>`.
- **Prefix, second view.** Run `"$S/vx" cont-store cat "$S/stores/tally-<KEPT>/store.json"`. It holds `{"items": ["apple"]}` with one `apple`: `store-open` and `item-stored` did not run again.
- **Refresh.** Run `"$S/vx" cont-frame python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); print(r["continues"], [f["step"] for f in r["frame"]], [(w["word"], w.get("relies_on", "")) for w in r["words"]])' "$S/home/tally/runs/<C>/run.json"`. It prints `<KEPT> ['refresh', 'doctor'] [('store-open', '<KEPT>'), ('item-stored', '<KEPT>'), ('item-listed', '')]`.
- **Handover.** Run `"$S/vx" cont-runs verilex runs`. `<KEPT>` shows `cleanup=continued` and `<C>` shows `cleanup=kept`. Run `"$S/vx" cont-cleanup-old verilex cleanup <KEPT>`: `refused: <KEPT> was continued by <C>; clean up that run instead`. Run `"$S/vx" cont-again-old verilex run --continue <KEPT> 'store-open'`: `refused: <KEPT> was continued by <C>; continue that run instead`.
- **Read-only rerun.** Run `echo "probe" >> "$S/tally/.verilex/words/item-listed/word.md"`, then `"$S/vx" cont-ro verilex run --continue <C> --keep 'store-open | item-stored apple | item-listed apple'`. Output `green: 3 green, continued <C>, 2 skipped: ...; run <D>`: only `item-listed` ran. Restore with `sed -i '$d' "$S/tally/.verilex/words/item-listed/word.md"`.
- **Refuse state change.** Run `echo "probe" >> "$S/tally/.verilex/words/item-stored/word.md"`, then `"$S/vx" cont-state verilex run --continue <D> 'store-open | item-stored apple | item-listed apple'`. Stderr `verilex: refused: item-stored apple: word changed; the kept instance already holds the effects of item-stored apple ...`, exit `2`. Run `"$S/vx" cont-state-store cat "$S/stores/tally-<KEPT>/store.json"`: still one `apple`. Restore with `sed -i '$d' "$S/tally/.verilex/words/item-stored/word.md"` and confirm `"$S/vx" cont-check verilex check` prints `check: no drift (3 admitted)`.
- **Teardown.** Run `"$S/vx" cont-cleanup verilex cleanup <D>`. It prints `cleanup: done` and `$S/stores` is empty.

## Gotchas

- `--continue` with provisional words always refuses, because the kept instance already holds `store-open`'s effects. Admit first.
- The instance keeps the first run's id: the store stays `$S/stores/tally-<KEPT>` across every continuing run.
- Editing an admitted word also makes it drift-suspect (`the word's files changed since admission`). Restore the file before the next recipe.
- Results proven on a continued instance never enter the ledger. They do not make a fresh `verilex run` skip.
- A refused `--continue` adds no run and leaves the instance and its owner unchanged.
