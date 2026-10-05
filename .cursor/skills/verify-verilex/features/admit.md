# Admit

`verilex admit <word> --verdict <file>` records an outside curator's verdict on a proposed packet. `admit` writes `.verilex/words/<word>/admission.json` and makes the word `admitted`; `reject` keeps the word provisional. Both verdicts are kept beside the packet. verilex refuses a verdict for an unknown packet or one whose word files or sections changed since the packet was built.

## Sub-features

- `admit-admit` writes `admission.json` with date, curator, packet, counted runs, word digest and section hashes.
- `admit-reject` keeps the word provisional and stores the verdict beside the packet.
- `admit-refused-packet` refuses a verdict naming a packet that was never proposed for the word.
- `admit-refused-stale` refuses a verdict when the word's files changed after the packet was built.
- `admit-stamp` makes the next run of a chain with the word live once (`word changed`).

## How to get to it (user POV)

- Run `verilex propose <word>`, hand the packet to a curator, then run `verilex admit <word> --verdict <file>` with the curator's JSON: `{"word": "<word>", "packet": "<PACKET>", "verdict": "admit" | "reject", "curator": "<who>", "reason": "..."}`.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor, with two green runs of `store-open | item-stored apple | item-listed apple`, and `verilex propose` run for each word (packet ids `<PACKET>` from its output). Write verdict files under `$S/verdicts/` and pass absolute paths.

- **Unknown packet.** Write `{"word": "store-open", "packet": "0000000000000000", "verdict": "admit", "curator": "verify-verilex", "reason": "probe"}` to `$S/verdicts/unknown.json`. Run `"$S/vx" adm-unknown verilex admit store-open --verdict "$S/verdicts/unknown.json"`. Stderr `verilex: refused: no packet 0000000000000000 was proposed for store-open`, exit `2`.
- **Reject.** Write a `reject` verdict for `item-listed` with its `<PACKET>`. Run `"$S/vx" adm-reject verilex admit item-listed --verdict "$S/verdicts/item-listed-reject.json"`. Stdout `rejected item-listed (curator verify-verilex): stays provisional`.
- **Reject, second view.** Run `"$S/vx" adm-reject-files ls "$S/tally/.verilex/words/item-listed" "$S/home/tally/proposals/item-listed"`. The word has no `admission.json`; the proposals directory holds `<PACKET>.json` and `<PACKET>.verdict.json`.
- **Stale.** Run `echo "probe" >> "$S/tally/.verilex/words/item-stored/word.md"`, write an `admit` verdict for `item-stored` with its `<PACKET>`, and run `"$S/vx" adm-stale verilex admit item-stored --verdict "$S/verdicts/item-stored-admit.json"`. Stderr `verilex: refused: item-stored changed since packet <PACKET>; propose it again`, exit `2`. Restore with `sed -i '$d' "$S/tally/.verilex/words/item-stored/word.md"`; after that the same verdict file admits.
- **Admit.** Write `admit` verdicts for all three words with their `<PACKET>` ids (the rejected `item-listed` packet can still be admitted). Run `"$S/vx" adm-<word> verilex admit <word> --verdict "$S/verdicts/<word>-admit.json"` for each. Stdout `admitted <word> (curator verify-verilex, N runs)`.
- **Admit, second view.** Run `"$S/vx" adm-record cat "$S/tally/.verilex/words/store-open/admission.json"` and `"$S/vx" adm-words verilex words`. The record names the curator, packet, runs, `word_digest` and the `verify-tally/features/store.md#store-open` section hash, and all three words show `status:   admitted`.
- **Stamp.** Run `"$S/vx" adm-plan verilex plan 'store-open | item-stored apple | item-listed apple'`. It prints `plan: skip 0, run 3; store-open: word changed`.

## Gotchas

- `admit-all` drives this recipe's happy path for all three words. Use it when another feature only needs the admitted baseline.
- `admission.json` lands in the product checkout (`$S/tally/.verilex/words/<word>/`), not in `VERILEX_HOME`.
- `vx` runs from `$S/tally`, so a relative `--verdict` path resolves there. Use absolute paths.
