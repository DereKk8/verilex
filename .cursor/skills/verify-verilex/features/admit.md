# Admit

`verilex admit <word> --verdict <file>` records an outside curator's verdict on a proposed packet for a word without a claim. `admit` writes `.verilex/words/<word>/admission.json` and makes the word `admitted`; `reject` keeps the word provisional. Both verdicts are kept beside the packet. verilex refuses a verdict for an unknown packet, one whose word files or sections changed since the packet was built, and any verdict for a word that proves a claim, which joins through [onboarding](onboard.md).

## Sub-features

- `admit-admit` writes `admission.json` with date, curator, packet, counted runs, word digest and the hash of each implemented section.
- `admit-reject` keeps the word provisional and stores the verdict beside the packet.
- `admit-refused-packet` refuses a verdict naming a packet that was never proposed for the word.
- `admit-refused-stale` refuses a verdict when the word's files or the sections it implements changed after the packet was built.
- `admit-refused-claim` refuses a verdict for a word that proves a claim.
- `admit-stamp` makes the next run of a chain with the word live once (`word changed`).

## How to get to it (user POV)

- Run `verilex propose <word>`, hand the packet to a curator, then run `verilex admit <word> --verdict <file>` with the curator's JSON: `{"word": "<word>", "packet": "<PACKET>", "verdict": "admit" | "reject", "curator": "<who>", "reason": "..."}`.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor, with the admitted baseline (`admit-all`).
- The probe word `probe-item` from [propose.md](propose.md), used in two green runs of `store-open | probe-item apple | item-listed apple`, and `"$S/vx" adm-propose verilex propose probe-item` run (packet id `<PACKET>` from its output). Write verdict files under `$S/verdicts/` (`mkdir -p "$S/verdicts"`) and pass absolute paths.

- **Unknown packet.** Write `{"word": "probe-item", "packet": "0000000000000000", "verdict": "admit", "curator": "verify-verilex", "reason": "probe"}` to `$S/verdicts/unknown.json`. Run `"$S/vx" adm-unknown verilex admit probe-item --verdict "$S/verdicts/unknown.json"`. Stderr `verilex: refused: no packet 0000000000000000 was proposed for probe-item`, exit `2`.
- **Reject.** Write a `reject` verdict for `probe-item` with its `<PACKET>` to `$S/verdicts/probe-item-reject.json`. Run `"$S/vx" adm-reject verilex admit probe-item --verdict "$S/verdicts/probe-item-reject.json"`. Stdout `rejected probe-item (curator verify-verilex): stays provisional`.
- **Reject, second view.** Run `"$S/vx" adm-reject-files ls "$S/tally/.verilex/words/probe-item" "$S/home/tally/proposals/probe-item"`. The word has no `admission.json`; the proposals directory holds `<PACKET>.json` and `<PACKET>.verdict.json`.
- **Stale.** Write an `admit` verdict for `probe-item` with its `<PACKET>` to `$S/verdicts/probe-item-admit.json`. Run `echo "probe" >> "$S/tally/.verilex/words/probe-item/word.md"`, then `"$S/vx" adm-stale verilex admit probe-item --verdict "$S/verdicts/probe-item-admit.json"`. Stderr `verilex: refused: probe-item changed since packet <PACKET>; propose it again`, exit `2`. Restore with `sed -i '$d' "$S/tally/.verilex/words/probe-item/word.md"`.
- **Section changed.** Run `echo "- Admit probe" >> "$S/tally/.cursor/skills/verify-tally/features/items.md"`, then run the same admit as `"$S/vx" adm-section ...`. The same refusal, exit `2`: a word without a claim is anchored on its whole section (for a sub-feature id, the whole file). Restore with `sed -i '$d' "$S/tally/.cursor/skills/verify-tally/features/items.md"`.
- **Claim refused.** Write `{"word": "item-stored", "packet": "0000000000000000", "verdict": "admit", "curator": "verify-verilex", "reason": "probe"}` to `$S/verdicts/item-stored.json`. Run `"$S/vx" adm-claim verilex admit item-stored --verdict "$S/verdicts/item-stored.json"`. Stderr ``verilex: refused: item-stored proves claim item-added, so it joins the vocabulary through `verilex onboard item-stored`, not propose and admit``, exit `2`.
- **Admit.** Run `"$S/vx" adm-admit verilex admit probe-item --verdict "$S/verdicts/probe-item-admit.json"`. Stdout `admitted probe-item (curator verify-verilex, 2 runs)`.
- **Admit, second view.** Run `"$S/vx" adm-record cat "$S/tally/.verilex/words/probe-item/admission.json"` and `"$S/vx" adm-words verilex words`. The record names the curator, packet, runs, `word_digest` and `sections` with `verify-tally/features/items.md#item-add`, and `probe-item` shows `status:   admitted`.
- **Stamp.** Run `"$S/vx" adm-plan verilex plan 'store-open | probe-item apple | item-listed apple'`. It prints `plan: skip 0, run 3; probe-item apple: word changed`.

## Gotchas

- `admission.json` lands in the product checkout (`$S/tally/.verilex/words/<word>/`), not in `VERILEX_HOME`.
- `vx` runs from `$S/tally`, so a relative `--verdict` path resolves there. Use absolute paths.
- The tally words prove claims, so they never get an `admission.json`; `admit-all` onboards them.
- Remove the probe word with `command rm -rf "$S/tally/.verilex/words/probe-item"` before a recipe that expects only tally's three words.
