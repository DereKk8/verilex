# Run a chain

`verilex run '<chain>'` checks the chain's order, decides whether every step can be skipped on proof stamps, and otherwise launches an owned instance, runs each word through the frame and cleans up. `--keep` leaves the instance alive, `--fresh` forces a live run, `--json` prints the full record.

## Sub-features

- `run-live` launches an instance, runs every word, cleans up and records the run.
- `run-refused` refuses an unknown word or a word whose `requires` nothing earlier provides, before starting anything.
- `run-keep` leaves the instance alive and records `cleanup=kept`.
- `run-fresh` runs live even when every stamp matches.
- `run-json` prints the complete run record, including `rerun` (why it ran live) or `skipped`.
- `run-skip` skips the whole chain, launching nothing, when every word is admitted and every stamp matches a green result with its evidence present.
- `run-skip-invalidated` runs live again when a declared input changes or the relied-on evidence is gone.

## How to get to it (user POV)

- Run `verilex run '<chain>' [--keep] [--fresh] [--json]` in a product checkout.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor. `run-skip` and `run-skip-invalidated` need the admitted baseline (`admit-all`).

- **Live.** Run `"$S/vx" run-live verilex run 'store-open | item-stored apple | item-listed apple'`. Output `green: 3 green; run <RUN>`, exit `0`.
- **Live, second view.** Run `"$S/vx" run-live-runs verilex runs` and `"$S/vx" run-live-stores ls "$S/stores"`. `<RUN>` shows `green  cleanup=done` and no store is left.
- **Refused order.** Run `"$S/vx" run-refused-order verilex run 'item-stored apple | store-open'`. Stderr `verilex: refused: item-stored apple requires store; nothing earlier provides it`, exit `2`.
- **Refused word.** Run `"$S/vx" run-refused-word verilex run 'store-open | nope'`. Stderr `verilex: refused: unknown word 'nope'; ...`, exit `2`.
- **Refused, second view.** Run `"$S/vx" run-refused-runs verilex runs`. No run was added for either refusal.
- **Keep.** Run `"$S/vx" run-keep verilex run --keep 'store-open | item-stored apple'`. The last line is ``kept: tear down with `verilex cleanup <RUN>` ``.
- **Keep, second view.** Run `"$S/vx" run-keep-store cat "$S/stores/tally-<RUN>/store.json"` and `"$S/vx" run-keep-runs verilex runs`. The store holds `{"items": ["apple"]}` and the run shows `cleanup=kept`. Tear it down with `"$S/vx" run-keep-cleanup verilex cleanup <RUN>`.
- **JSON.** Run `"$S/vx" run-json verilex run --json 'store-open | item-stored banana'`. Stdout is one JSON record with `run`, `frame` (`launch`, `doctor`, `cleanup`, each `exit: 0`), `words` with `verdict`, `claim`, `observation` and `stamp`, `"verdict": "green"`, `"cleanup": "done"`, `"evidence_kept": true`, and `rerun` naming why it ran live.
- **Skip (admitted baseline).** After `admit-all`, run `"$S/vx" run-after-admit verilex run 'store-open | item-stored apple | item-listed apple'` (live: admission changed every stamp), then `"$S/vx" run-skip verilex run 'store-open | item-stored apple | item-listed apple'`. The second prints `green: 3 green, skipped: stamps match run <A>; run <B>` where `<A>` is the run-after-admit id.
- **Skip, second view.** Run `"$S/vx" run-skip-runs verilex runs`, `"$S/vx" run-skip-stores ls "$S/stores"` and `"$S/vx" run-skip-cleanup verilex cleanup <B>`. `<B>` shows `cleanup=none`, no store exists, and cleanup prints `verilex: <B> launched nothing; it relied on stamps`.
- **Skip JSON.** Run `"$S/vx" run-skip-json verilex run --json 'store-open | item-stored apple | item-listed apple'`. The record has `"skipped": true`, `"cleanup": "none"`, an empty `frame` and `relies_on` on each word.
- **Fresh.** Run `"$S/vx" run-fresh verilex run --fresh 'store-open | item-stored apple | item-listed apple'`. Output `green: 3 green; run <RUN>` with no `skipped`; `verilex runs` shows it `cleanup=done`.
- **Input changed.** Run `TALLY_DEFECT=none "$S/vx" run-env-changed verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 0, run 3; store-open: env TALLY_DEFECT changed`, so `run` would be live.
- **Evidence gone.** Note `<A>` from `verilex plan`. Run `mv "$S/home/tally/runs/<A>/02-item-stored" "$S/moved"`, then `"$S/vx" run-evidence-gone verilex plan 'store-open | item-stored apple | item-listed apple'`: `item-stored apple: evidence from run <A> is gone`. Move it back with `mv "$S/moved" "$S/home/tally/runs/<A>/02-item-stored"`.

## Gotchas

- Provisional words never skip. A fresh session's chains always run live until `admit-all` (or `propose` and `admit`) has run.
- The first run after an admission is live (`word changed`): the admission record is part of the stamp.
- Only variables in a word's `env` enter its stamp. A variable read only by the frame (`TALLY_ADOPT_STORE`) does not, so use `--fresh` to probe the frame.
- Ledger entries are keyed by chain prefix, so a shorter chain (`store-open | item-stored apple`) also skips after the longer chain was green.
- Skipping is all or nothing: `store-open | item-stored banana` runs both words live even when `store-open` alone is proven.
- The 7-day expiry cannot be reached through the CLI without waiting 7 days. Report it as not driven.
