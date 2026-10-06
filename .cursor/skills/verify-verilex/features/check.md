# Check

`verilex check` compares each admitted word with the project now: for a word that proves a claim, its onboarding decision (seal, word digest, claim version, planted defects) and the claim's review state; for a word without a claim, its admission record's word digest and stored section hashes. Changed word files, a decision edited by hand, a new planted defect, a claim that needs review, or a changed or missing section makes the word drift-suspect: it always runs and no earlier result is trusted for it. Drift is computed on every call, never cached.

## Sub-features

- `check-clean` reports `check: no drift (N admitted)`.
- `check-claim-review` reports a word whose claim needs review because a requirement sentence the claim maps to changed, and names the new sentence no claim maps yet (see [claims.md](claims.md)).
- `check-section` reports a word without a claim whose implemented feature-map section changed.
- `check-word` reports a word whose own files changed since onboarding.
- `check-runs-live` makes a drift-suspect word's chain run live (`plan` names the drift).
- `check-recovers` clears drift as soon as the requirement, section or files are restored.

## How to get to it (user POV)

- Run `verilex check` in a product checkout after editing the verify skill or words.

## Driving it with vx

Preconditions:

- The admitted baseline (`admit-all`), then one live run of the default chain so `verilex plan` skips it.

- **Clean.** Run `"$S/vx" chk-clean verilex check`. Stdout `check: no drift (3 admitted)`, exit `0`.
- **Claim review.** Run ``sed -i 's/Expect exit 0 and `opened`/Expect exit 0 and `created`/' "$S/tally/.cursor/skills/verify-tally/features/store.md"``, then `"$S/vx" chk-review verilex check`. Stdout `check: 1 of 3 admitted drift-suspect; they always run` and ``  store-open: claim store-opened needs review: verify-tally/features/store.md#store-open: requirement changed or gone: Expect exit 0 and `opened`; `store.json` holds `{"items": []}`.; verify-tally/features/store.md#store-open: requirement no claim maps: Expect exit 0 and `created`; `store.json` holds `{"items": []}`.``
- **Runs live.** Run `"$S/vx" chk-plan verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 0, run 3; store-open: drift-suspect: claim store-opened needs review: <same reasons>`, cut to 300 characters with `…`. Run `"$S/vx" chk-words verilex words`: `store-open` shows `status:   drift-suspect`.
- **Recovers.** Run ``sed -i 's/Expect exit 0 and `created`/Expect exit 0 and `opened`/' "$S/tally/.cursor/skills/verify-tally/features/store.md"``, then `"$S/vx" chk-recovered verilex check` and `"$S/vx" chk-plan-recovered verilex plan 'store-open | item-stored apple | item-listed apple'`. They print `check: no drift (3 admitted)` and `plan: skip 3, run 0`.
- **No drift from prose.** Run `echo "- Drift probe" >> "$S/tally/.cursor/skills/verify-tally/features/store.md"`, then `"$S/vx" chk-prose verilex check`: `check: no drift (3 admitted)`, because the line is outside every sub-feature: it opens with no sub-feature id. Restore with `sed -i '$d' "$S/tally/.cursor/skills/verify-tally/features/store.md"`.
- **Section drift (word without a claim).** Run `"$S/vx" chk-legacy-new verilex new probe-word --implements verify-tally/features/store.md#store-open` and write `$S/tally/.verilex/words/probe-word/run` as `#!/bin/sh` plus `echo '{"verdict": "pass", "observation": "probe saw the store"}'`. Run `"$S/vx" chk-legacy-use-1 verilex run 'store-open | probe-word'` and `chk-legacy-use-2` the same way, then `propose` and `admit` `probe-word` as in [admit.md](admit.md). Run `echo "- Drift probe" >> "$S/tally/.cursor/skills/verify-tally/features/store.md"`, then `"$S/vx" chk-section verilex check`. Stdout `check: 1 of 4 admitted drift-suspect; they always run` and `  probe-word: verify-tally/features/store.md#store-open: section changed`: a word without a claim is anchored on its whole section (for a sub-feature id, the whole file). Restore with `sed -i '$d' "$S/tally/.cursor/skills/verify-tally/features/store.md"` and `command rm -rf "$S/tally/.verilex/words/probe-word"`.
- **Word drift.** Run `echo "probe" >> "$S/tally/.verilex/words/item-listed/word.md"`, then `"$S/vx" chk-word verilex check`. Stdout names `item-listed: the word's files changed since onboarding`. Restore with `sed -i '$d' "$S/tally/.verilex/words/item-listed/word.md"` and confirm `check: no drift (3 admitted)`.

## Gotchas

- `verilex check` exits `0` even when it reports drift. Read stdout, not the exit code.
- The verify skill is not part of any stamp. Only drift (a claim that needs review, or a changed section for a word without a claim) makes a skill edit force a live run.
- Re-admitting a drift-suspect word needs `verilex onboard` again (a new `propose` and `admit` for a word without a claim). [onboard.md](onboard.md) covers drift from a hand-edited decision and from a new planted defect.
