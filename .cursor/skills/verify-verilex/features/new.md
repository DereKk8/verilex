# New word

`verilex new <word> --implements <ref>` scaffolds a provisional word whose every `--implements` reference resolves to a feature-map section. Its `run` claims `blocked` until written, so its steps are inconclusive. In a project without `.verilex/`, `new` also creates `config.yaml` and frame stubs that exit 2.

## Sub-features

- `new-word` writes `word.md` and an executable stub `run` under `.verilex/words/<word>/`.
- `new-provisional` lists the new word as `provisional`, and a chain using it ends inconclusive.
- `new-refused-ref` refuses a reference with no matching section and points at `verilex gap`.
- `new-refused-exists` refuses a word that already exists.
- `new-project` scaffolds `.verilex/` (config named after the directory, `launch`, `doctor`, `refresh`, `cleanup` stubs) in a new project.

## How to get to it (user POV)

- Run `verilex new <word> --implements <skill>/<file>#<section> [--implements ...]` in a product checkout.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor.

- **Refused reference.** Run `"$S/vx" new-bad-ref verilex new item-renamed --implements verify-tally/features/items.md#item-rename`. Stderr ``verilex: refused: ... has no section "item-rename"; record a product moment the feature map lacks with `verilex gap` ``, exit `2`.
- **Scaffold.** Run `"$S/vx" new verilex new item-renamed --implements verify-tally/features/items.md#item-add`. Stdout `new: provisional word item-renamed implements verify-tally/features/items.md#item-add`, exit `0`.
- **Scaffold, second view.** Run `"$S/vx" new-files ls -l "$S/tally/.verilex/words/item-renamed"` and `"$S/vx" new-run cat "$S/tally/.verilex/words/item-renamed/run"`. `run` is executable and prints `{"verdict": "blocked", "detail": "item-renamed is a stub; write its run"}` with `exit 2`.
- **Provisional.** Run `"$S/vx" new-words verilex words` (it shows `item-renamed` with `status:   provisional`), then `"$S/vx" new-chain verilex run 'store-open | item-renamed'`: `inconclusive: 1 green, 1 inconclusive` with cause `item-renamed is a stub; write its run`, exit `2`.
- **Refused exists.** Run the scaffold command again as `"$S/vx" new-exists ...`. Stderr `verilex: refused: .verilex/words/item-renamed already exists`, exit `2`.
- **New project.** Run `mkdir -p "$S/fresh/.cursor/skills/verify-fresh/features"` and write `$S/fresh/.cursor/skills/verify-fresh/features/ping.md` with a `## Sub-features` list holding `` `ping-reply` ``. Run `"$S/vx" new-project verilex --project "$S/fresh" new ping-replied --implements verify-fresh/features/ping.md#ping-reply`. Stdout says it created `.verilex with config and frame stubs (launch, doctor, refresh, cleanup)`.
- **New project, second view.** Run `"$S/vx" new-project-files find "$S/fresh/.verilex" -type f` and `"$S/vx" new-project-config cat "$S/fresh/.verilex/config.yaml"`. Seven files exist and the config reads `project: fresh`. Run `"$S/vx" new-project-run verilex --project "$S/fresh" run ping-replied`: `inconclusive: 0 green, 1 not run` with `launch: exit 2` and `cleanup: exit 2; the instance may outlive the run`.
- **Restore.** Run `rm -r "$S/tally/.verilex/words/item-renamed"`.

## Gotchas

- A sub-feature id written as inline code (`` `ping-reply` ``) resolves to its whole feature file, even with no heading of that name.
- A new word directory changes no other word's stamp: an admitted chain still skips afterwards.
- Without `--project`, `vx` runs from `$S/tally`, so `new` always lands in the scratch product.
