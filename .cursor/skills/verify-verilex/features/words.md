# Words

`verilex words` lists the project's dictionary: each word with its arguments, promise, `requires`, `provides` and lifecycle status (`provisional`, `admitted` or `drift-suspect`).

## Sub-features

- `words-list` prints every word with args, promise, requires and provides.
- `words-status` shows each word's current lifecycle status, recomputed on every call.
- `words-refused` refuses outside a verilex project.

## How to get to it (user POV)

- Run `verilex words` in a product checkout.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor.

- **List.** Run `"$S/vx" words verilex words`. Exit `0`. It prints `item-listed name`, `item-stored name` and `store-open`, each followed by `promise:`, `requires: ... provides: ...` and `status:   provisional`. `item-listed` shows `requires: item:{name}  provides: -`.
- **List, second view.** Run `"$S/vx" words-files ls "$S/tally/.verilex/words"`. It lists the three word directories, matching the three listed words, plus `tally_word.py`, the helper they share.
- **Status after admission.** Run `admit-all`, then `"$S/vx" words-admitted verilex words`. All three show `status:   admitted`.
- **Status after drift.** Run `echo "- probe" >> "$S/tally/.cursor/skills/verify-tally/features/store.md"`, then `"$S/vx" words-drift verilex words`. `store-open` shows `status:   drift-suspect`. Restore with `sed -i '$d' "$S/tally/.cursor/skills/verify-tally/features/store.md"`; `verilex words` shows `admitted` again.
- **Refused.** Run `"$S/vx" words-no-project verilex --project "$S/stores" words`. Stderr `verilex: refused: no .verilex/config.yaml in <dir> or its parents`, exit `2`.

## Gotchas

- Words are listed in name order, not chain order.
- A word scaffolded by `verilex new` shows its `TODO:` promise until someone edits `word.md`.
