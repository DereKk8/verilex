# verilex verification map

This directory is the maintained source for verifying the user-facing behavior of the verilex CLI. Read the index before driving verilex, then use the matching feature file as the recipe.

## Baseline preconditions

- A session from `.cursor/skills/verify-verilex/scripts/launch` (or `launch --bin PATH` for a given build), with `S` set to the printed session directory.
- `.cursor/skills/verify-verilex/scripts/doctor "$S"` prints `doctor: ok` and the expected binary source and sha256.
- The scratch product `$S/tally` is unmodified, except where a recipe edits it and restores it.
- Recipes marked "admitted baseline" first need `.cursor/skills/verify-verilex/scripts/admit-all "$S"` to print `admitted words: 3`.
- Never drive a session this run did not launch, and never point `VERILEX_HOME` at `~/.local/state/verilex`.

## Driving conventions

- Run every command through `"$S/vx" <label> ...` from the repository root.
- `<RUN>`, `<KEPT>` and `<PACKET>` stand for ids printed by an earlier step of the same recipe. Copy them from that output.
- Treat commands as literal. Keep the quoted chain and flags unchanged.
- Restore any edit to `$S/tally` before the next recipe, and confirm with `verilex check` (no drift) or `"$S/vx" restored diff -r "$S/tally/.verilex" "$PWD/tests/fixtures/tally/.verilex"` (only `admission.json` files and `gaps/` differ).
- The default chain is `store-open | item-stored apple | item-listed apple`. `item-listed` is the only `read_only` word.

## Proof and skip reporting

- Capture the command and its result, then a second observation from another `vx` call.
- Exit codes: `run` gives `0` green, `1` red, `2` inconclusive or refused. `plan`, `words`, `runs`, `check`, `gap`, `new`, `propose`, `admit` give `0` on success and `2` when refused.
- A refusal prints `verilex: refused: ...` on stderr, exits `2`, and adds no run to `verilex runs`.
- Record the feature file and sub-feature id with every evidence directory.
- Report an unreachable path with the attempted command and the unmet precondition.
- Do not report a skipped entry point as verified through a different path.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the user-visible behavior. It then uses exactly four H2 sections in this order: `Sub-features`, `How to get to it (user POV)`, `Driving it with vx`, `Gotchas`. Keep implementation details out of the map.

## Features

- [Verdicts and exit codes](verdicts.md) - green, red and inconclusive, the honesty rules, and environment trouble never reported as red.
- [Quiet output](output.md) - verdict first, failures only, evidence by reference.
- [Run a chain](run.md) - `verilex run` live, refused, `--keep`, `--fresh`, `--json`, and stamp-based skipping.
- [Plan](plan.md) - `verilex plan`, the same skip decision without running, including `--continue` and `--json`.
- [Continue a kept instance](continue.md) - `verilex run --continue`: proven prefix skipped, refresh and doctor, refusals.
- [Words](words.md) - `verilex words`: the dictionary with lifecycle status.
- [Runs](runs.md) - `verilex runs`: every run with verdict and cleanup state.
- [Cleanup](cleanup.md) - `verilex cleanup`: tear down a kept instance.
- [New word](new.md) - `verilex new`: scaffold a provisional word, and a whole `.verilex/` in a new project.
- [Propose](propose.md) - `verilex propose`: the curator packet after two counted uses.
- [Admit](admit.md) - `verilex admit`: record a curator's admit or reject verdict.
- [Gap](gap.md) - `verilex gap`: note a product moment the feature map lacks.
- [Check](check.md) - `verilex check`: report drift-suspect admitted words.
