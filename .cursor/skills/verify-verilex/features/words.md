# Words

`verilex words` lists the project's dictionary: each word with its arguments, promise, the claim version and entry point it proves, `requires` and `provides` (its own and those its claim pins), and lifecycle status (`provisional`, `admitted` or `drift-suspect`).

## Sub-features

- `words-list` prints every word with args, promise, claim pin, requires and provides.
- `words-status` shows each word's current lifecycle status, recomputed on every call.
- `words-refused` refuses outside a verilex project.

## How to get to it (user POV)

- Run `verilex words` in a product checkout.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor.

- **List.** Run `"$S/vx" words verilex words`. Exit `0`. It prints `item-listed name`, `item-stored name` and `store-open`, each followed by `promise:`, `claim:    <claim>@<version> via cli`, `requires: ... provides: ...` and `status:   provisional`. `item-listed` shows `claim:    item-listed@<version> via cli` and `requires: item:{name}  provides: -`; `item-stored` shows `claim:    item-added@<version> via cli`.
- **List, second view.** Run `"$S/vx" words-files ls "$S/tally/.verilex/words"`. It lists the three word directories, matching the three listed words, plus `tally_word.py`, the helper they share.
- **Status after admission.** Run `admit-all`, then `"$S/vx" words-admitted verilex words`. All three show `status:   admitted`.
- **Status after drift.** Run ``sed -i 's/Expect exit 0 and `opened`/Expect exit 0 and `created`/' "$S/tally/.cursor/skills/verify-tally/features/store.md"``, then `"$S/vx" words-drift verilex words`. `store-open` shows `status:   drift-suspect`: its claim `store-opened` needs review. Restore with ``sed -i 's/Expect exit 0 and `created`/Expect exit 0 and `opened`/' "$S/tally/.cursor/skills/verify-tally/features/store.md"``; `verilex words` shows `admitted` again.
- **Refused.** Run `"$S/vx" words-no-project verilex --project "$S/stores" words`. Stderr `verilex: refused: no .verilex/config.yaml in <dir> or its parents`, exit `2`.

## Attack scenarios

- **A hand-edited word file with a YAML error.** An agent edits `word.md` by hand and drops a bracket. Run `command cp "$S/tally/.verilex/words/item-listed/word.md" "$S/word.saved"` and `sed -i 's/^args: \[name\]$/args: [name/' "$S/tally/.verilex/words/item-listed/word.md"`. Run `"$S/vx" atk-words-broken verilex words` and `"$S/vx" atk-words-broken-run verilex run 'store-open'`. Safe result: each exits `2` with `verilex: refused: <path>/item-listed/word.md: malformed YAML frontmatter: ...`, naming the file, and no run is added. Restore the saved file.
- **A word directory name with a `|` or a path.** See [new.md](new.md) **A word name that is a path**. A hand-made directory with `|` in its name is refused as an argument (issue #49).

## Gotchas

- Words are listed in name order, not chain order.
- A word scaffolded by `verilex new` shows its `TODO:` promise until someone edits `word.md`.
