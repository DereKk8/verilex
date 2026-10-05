# Gap

`verilex gap '<description>'` writes a note to the product's `.verilex/gaps/` for the verify skill's owner when the feature map has no section for a product moment. verilex never edits the verify skill itself.

## Sub-features

- `gap-note` writes a Markdown note named after the date and description and prints its path.
- `gap-repeat` keeps both notes when the same description is recorded twice.
- `gap-refused-empty` refuses an empty description.
- `gap-no-stamp` leaves every word's stamp unchanged.

## How to get to it (user POV)

- Run `verilex gap '<description>'`, usually after `verilex new` refused a reference.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor. `gap-no-stamp` needs the admitted baseline and a skipping chain (see [run.md](run.md) `run-skip`).

- **Note.** Run `"$S/vx" gap verilex gap 'A user renames an item'`. Stdout `gap recorded for the verify skill's owner: $S/tally/.verilex/gaps/<date>-<time>-a-user-renames-an-item.md`, exit `0`.
- **Note, second view.** Run `"$S/vx" gap-note cat <that path>`. It starts `# Gap: A user renames an item` and tells the owner to add a section, then `verilex new <word> --implements ...`.
- **Repeat.** Run the same `gap` command again as `"$S/vx" gap-twice ...`, then `"$S/vx" gap-list ls "$S/tally/.verilex/gaps"`. Two notes exist; the second ends `-2.md`.
- **Refused.** Run `"$S/vx" gap-empty verilex gap ''`. Stderr `verilex: refused: describe the product moment the feature map lacks`, exit `2`.
- **No stamp change.** In an admitted baseline where `verilex plan` skips the default chain, record a gap, then run `"$S/vx" gap-plan verilex plan 'store-open | item-stored apple | item-listed apple'`. It still prints `plan: skip 3, run 0`.

## Gotchas

- Gap notes land in the product checkout, not in `VERILEX_HOME`. Session cleanup removes them with `$S`.
- The verify-tally feature map stays unchanged: `"$S/vx" gap-skill-unchanged diff -r "$S/tally/.cursor" "$PWD/tests/fixtures/tally/.cursor"` prints nothing and exits `0`.
