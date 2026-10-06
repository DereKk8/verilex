# Plan

`verilex plan '<chain>'` makes the same skip decision `verilex run` would and runs nothing. It prints how many steps would be skipped and run, the run each skipped step relies on, and the first reason the chain must run live. `--continue <run>` plans against a kept instance; `--json` prints the decision for every step.

## Sub-features

- `plan-skip` reports `skip N, run 0` with the run each step relies on.
- `plan-live` reports the first reason the chain runs live (no green result, provisional word, word changed, changed env, evidence gone, drift-suspect).
- `plan-refused` refuses a badly ordered chain with the same message and exit code as `run`.
- `plan-matches-run` predicts what the next `run` does.
- `plan-continue` plans on a kept instance: the proven prefix skipped, the rest to run, or the `--continue` refusal.
- `plan-json` prints `steps` with `skipped` and `relies_on`, plus `rerun` and `continues` when they apply.

## How to get to it (user POV)

- Run `verilex plan '<chain>' [--continue <run>] [--json]` in a product checkout.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor. Everything after **Live (provisional)** needs the admitted baseline (`admit-all`).

- **Live (pristine).** Before any run, run `"$S/vx" plan-pristine verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 0, run 3; store-open: no green result on record` and exit `0`.
- **Live (provisional).** Run `"$S/vx" plan-first-run verilex run 'store-open | item-stored apple | item-listed apple'` (green), then `"$S/vx" plan-provisional verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 0, run 3; store-open: provisional; only admitted words are skipped` and exit `0`.
- **Live after admission.** Run `admit-all`, then `"$S/vx" plan-after-admit verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 0, run 3; store-open: word changed`.
- **Plan matches run.** Run `"$S/vx" plan-run verilex run 'store-open | item-stored apple | item-listed apple'` (live, run `<A>`), then `"$S/vx" plan-skip verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 3, run 0` and three `skip  <step>  relies on run <A>` lines. Then `"$S/vx" plan-skip-run verilex run 'store-open | item-stored apple | item-listed apple'` prints `skipped: stamps match run <A>`.
- **Plan runs nothing.** Run `"$S/vx" plan-runs verilex runs` before and after a `plan` call. The list is unchanged.
- **No green result.** Run `"$S/vx" plan-newarg verilex plan 'store-open | item-stored pear'`. It prints `plan: skip 0, run 2; item-stored pear: no green result on record`.
- **Refused.** Run `"$S/vx" plan-refused verilex plan 'item-stored apple | store-open'`. Stderr `verilex: refused: item-stored apple requires store, pinned by claim item-added; nothing earlier provides it`, exit `2`.
- **JSON.** Run `"$S/vx" plan-json verilex plan --json 'store-open | item-stored kiwi'`. Stdout is `{"steps": [{"step": "store-open", "skipped": false}, ...], "rerun": "item-stored kiwi: no green result on record"}`.
- **Continue.** Run `"$S/vx" plan-keep verilex run --keep 'store-open | item-stored apple'` (run `<KEPT>`), then `"$S/vx" plan-continue verilex plan --continue <KEPT> 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 2, run 1 on the instance kept by <KEPT>; item-listed apple: not run on the kept instance yet` and two `relies on run <KEPT>` lines.
- **Continue JSON.** Run `"$S/vx" plan-continue-json verilex plan --json --continue <KEPT> 'store-open | item-stored apple | item-listed apple'`. The object has `"continues": "<KEPT>"` and `item-listed apple` with `"skipped": false`.
- **Continue, second view.** Run `"$S/vx" plan-continue-store cat "$S/stores/tally-<KEPT>/store.json"` and `"$S/vx" plan-continue-runs verilex runs`. The store still holds `{"items": ["apple"]}` and `<KEPT>` still shows `cleanup=kept`: plan did not touch the instance. Tear it down with `"$S/vx" plan-cleanup verilex cleanup <KEPT>`.

## Gotchas

- `plan` exits `0` whether it would skip or run. Read the `skip N, run M` line, not the exit code.
- `plan --continue` refuses exactly as `run --continue` would, including with provisional words (see [continue.md](continue.md)).
- `no green result on record` is checked before `provisional`, so a pristine session never shows the provisional reason.
- A `plan` right after `admit-all` reports `word changed`, not `provisional`: the admission record is part of the stamp.
