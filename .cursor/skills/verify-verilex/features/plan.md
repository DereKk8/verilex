# Plan

`verilex plan '<chain>'` makes the same skip decision `verilex run` would and runs nothing. It prints how many steps would be skipped and run, the run each skipped step relies on, and the first reason the chain must run live. `--continue <run>` plans against a kept instance; `--json` prints the decision for every step. Without a chain, `verilex plan --claim <claim> [--named <claim>] [--changed <change>]` plans claims and a diff: skip with fingerprints, run, topological order, and touched claims not picked.

## Sub-features

- `plan-skip` reports `skip N, run 0` with the run each step relies on.
- `plan-live` reports the first reason the chain runs live (no green result, provisional word, word changed, changed env, evidence gone, drift-suspect).
- `plan-refused` refuses a badly ordered chain with the same message and exit code as `run`.
- `plan-matches-run` predicts what the next `run` does.
- `plan-continue` plans on a kept instance: the proven prefix skipped, the rest to run, or the `--continue` refusal.
- `plan-json` prints `steps` with `skipped` and `relies_on`, plus `rerun` and `continues` when they apply.
- `plan-claims` plans derived claims, named claims and a diff: skip with fingerprints, run, order, chain and unpicked.
- `plan-no-intent` with only `--changed` selects exactly the claims the diff touches.
- `plan-named` includes every `--named` claim even when it was not derived and the diff did not touch it.
- `plan-dependency` treats `config:<key>`, `image:<pin>` and `runbook:<ref>` as diff entries. A config key or image pin runs live.

## How to get to it (user POV)

- Run `verilex plan '<chain>' [--continue <run>] [--json]` in a product checkout.
- Run `verilex plan [--claim <claim> ...] [--named <claim> ...] [--changed <change> ...] [--json]` to plan claims and a diff. Do not pass a chain with those flags.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor. Everything after **Live (provisional)** needs the admitted baseline (`admit-all`).

- **Live (pristine).** Before any run, run `"$S/vx" plan-pristine verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 0, run 3; store-open: no green result on record` and exit `0`.
- **Live (provisional).** Run `"$S/vx" plan-first-run verilex run 'store-open | item-stored apple | item-listed apple'` (green), then `"$S/vx" plan-provisional verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 0, run 3; store-open: provisional; only admitted words are skipped` and exit `0`.
- **Live after admission.** Run `admit-all`, then `"$S/vx" plan-after-admit verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 0, run 3; store-open: word admission changed`.
- **Plan matches run.** Run `"$S/vx" plan-run verilex run 'store-open | item-stored apple | item-listed apple'` (live, run `<A>`), then `"$S/vx" plan-skip verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 3, run 0` and three `skip  <step>  relies on run <A>` lines. Then `"$S/vx" plan-skip-run verilex run 'store-open | item-stored apple | item-listed apple'` prints `skipped: stamps match run <A>`.
- **Plan runs nothing.** Run `"$S/vx" plan-runs verilex runs` before and after a `plan` call. The list is unchanged.
- **No green result.** Run `"$S/vx" plan-newarg verilex plan 'store-open | item-stored pear'`. It prints `plan: skip 0, run 2; item-stored pear: no green result on record`.
- **Refused.** Run `"$S/vx" plan-refused verilex plan 'item-stored apple | store-open'`. Stderr `verilex: refused: item-stored apple requires store, pinned by claim item-added; nothing earlier provides it`, exit `2`.
- **JSON.** Run `"$S/vx" plan-json verilex plan --json 'store-open | item-stored kiwi'`. Stdout is `{"steps": [{"step": "store-open", "skipped": false}, ...], "rerun": "item-stored kiwi: no green result on record"}`.
- **Continue.** Run `"$S/vx" plan-keep verilex run --keep 'store-open | item-stored apple'` (run `<KEPT>`), then `"$S/vx" plan-continue verilex plan --continue <KEPT> 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 2, run 1 on the instance kept by <KEPT>; item-listed apple: not run on the kept instance yet` and two `relies on run <KEPT>` lines.
- **Continue JSON.** Run `"$S/vx" plan-continue-json verilex plan --json --continue <KEPT> 'store-open | item-stored apple | item-listed apple'`. The object has `"continues": "<KEPT>"` and `item-listed apple` with `"skipped": false`.
- **Continue, second view.** Run `"$S/vx" plan-continue-store cat "$S/stores/tally-<KEPT>/store.json"` and `"$S/vx" plan-continue-runs verilex runs`. The store still holds `{"items": ["apple"]}` and `<KEPT>` still shows `cleanup=kept`: plan did not touch the instance. Tear it down with `"$S/vx" plan-cleanup verilex cleanup <KEPT>`.
- **Claim plan.** On an admitted baseline, run `"$S/vx" plan-claim-live verilex run 'store-open | item-stored apple | item-listed apple'` (run `<A>`). Then `"$S/vx" plan-claims verilex plan --claim item-added --named store-opened --changed .verilex/words/item-listed/run`. Stdout has `plan: skip 2, run 0; 1 touched claim not picked`, `skip  store-opened  store-open  relies on run <A>`, `skip  item-added  item-stored apple  relies on run <A>`, `order: store-opened, item-added`, `chain: store-open | item-stored apple` and `unpicked  item-listed`. Exit `0`. Run `"$S/vx" plan-claims-runs verilex runs` before and after that plan. The list is unchanged.
- **Claim plan JSON.** Run `"$S/vx" plan-claims-json verilex plan --json --claim item-added --named store-opened --changed .verilex/words/item-listed/run`. The object has `"format": "verilex-claim-plan-1"`, `"intent": "given"`, `"selected": ["item-added", "store-opened"]`, `"unpicked": ["item-listed"]` and `"warning": "1 touched claim not picked"`. `skip[0].fingerprints.claim` is a hex fingerprint and `skip[0].relies_on` is `<A>`.
- **No intent.** Run `"$S/vx" plan-no-intent verilex plan --json --changed .verilex/words/item-listed/run`. `intent` is `prove nothing this change touched broke`, `selected` and `touched` are `["item-listed"]`, and `unpicked` is `[]`.
- **Config key runs live.** Run `"$S/vx" plan-config verilex plan --json --changed config:tally.list`. `selected` is `["item-listed"]` and `run[0].reason` is `config key tally.list changed`.
- **Image pin and runbook.** Run `"$S/vx" plan-image verilex plan --json --changed image:ghcr.io/example/store:1`: `selected` is `["store-opened"]` and the run reason starts `image pin `. Run `"$S/vx" plan-runbook verilex plan --json --changed runbook:verify-tally/features/items.md#item-add`: `selected` is `["item-added"]`.

## Gotchas

- `plan` exits `0` whether it would skip or run. Read the `skip N, run M` line, not the exit code.
- `plan --continue` refuses exactly as `run --continue` would, including with provisional words (see [continue.md](continue.md)).
- `no green result on record` is checked before `provisional`, so a pristine session never shows the provisional reason.
- A `plan` right after `admit-all` reports `word admission changed`, not `provisional`: the seal of each word's onboarding decision is part of its stamp.
- A claim plan before any run of a word that takes arguments is refused: `item-stored needs arguments name; run a chain that binds them, then plan again`. The admitted baseline's onboarded chains bind `apple`.
- Execute the printed `chain` with the same flags. Do not run only the `run` list.
- `--json` of a claim plan has `"format": "verilex-claim-plan-1"`. A chain plan's JSON is still `steps`.
