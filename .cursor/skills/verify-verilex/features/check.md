# Check

`verilex check` compares each admitted word's stored section hashes and word digest with the project now. A changed or missing section, or changed word files, makes the word drift-suspect: it always runs and no earlier result is trusted for it. Drift is computed on every call, never cached.

## Sub-features

- `check-clean` reports `check: no drift (N admitted)`.
- `check-section` reports a word whose implemented feature-map section changed.
- `check-word` reports a word whose own files changed since admission.
- `check-runs-live` makes a drift-suspect word's chain run live (`plan` names the drift).
- `check-recovers` clears drift as soon as the section or files are restored.

## How to get to it (user POV)

- Run `verilex check` in a product checkout after editing the verify skill or words.

## Driving it with vx

Preconditions:

- The admitted baseline (`admit-all`), then one live run of the default chain so `verilex plan` skips it.

- **Clean.** Run `"$S/vx" chk-clean verilex check`. Stdout `check: no drift (3 admitted)`, exit `0`.
- **Section drift.** Run `echo "- Drift probe" >> "$S/tally/.cursor/skills/verify-tally/features/store.md"`, then `"$S/vx" chk-section verilex check`. Stdout `check: 1 of 3 admitted drift-suspect; they always run` and `  store-open: verify-tally/features/store.md#store-open: section changed`.
- **Runs live.** Run `"$S/vx" chk-plan verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 0, run 3; store-open: drift-suspect: verify-tally/features/store.md#store-open: section changed`. Run `"$S/vx" chk-words verilex words`: `store-open` shows `status:   drift-suspect`.
- **Recovers.** Run `sed -i '$d' "$S/tally/.cursor/skills/verify-tally/features/store.md"`, then `"$S/vx" chk-recovered verilex check` and `"$S/vx" chk-plan-recovered verilex plan 'store-open | item-stored apple | item-listed apple'`. They print `check: no drift (3 admitted)` and `plan: skip 3, run 0`.
- **Word drift.** Run `echo "probe" >> "$S/tally/.verilex/words/item-listed/word.md"`, then `"$S/vx" chk-word verilex check`. Stdout names `item-listed: the word's files changed since admission`. Restore with `sed -i '$d' "$S/tally/.verilex/words/item-listed/word.md"` and confirm `check: no drift (3 admitted)`.

## Gotchas

- `verilex check` exits `0` even when it reports drift. Read stdout, not the exit code.
- A feature-map section is not part of any stamp. Only drift makes a section edit force a live run.
- Re-admitting a drift-suspect word needs a new `propose` and `admit`.
