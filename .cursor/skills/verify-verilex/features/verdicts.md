# Verdicts and exit codes

Every `verilex run` ends in one of three verdicts with a matching exit code: `green` (0) when the product works with evidence, `red` (1) when it is broken with evidence, and `inconclusive` (2) for environment or harness trouble or a word claim that is not backed. Environment trouble is never reported as red.

## Sub-features

- `verdict-green` reports green and exits 0 when every word passes with a second observation.
- `verdict-red` reports red and exits 1 when a word fails with its preconditions held and the doctor still vouches for the instance.
- `verdict-inconclusive-env` reports inconclusive and exits 2 when the product's environment blocks a word.
- `verdict-inconclusive-frame` reports inconclusive and exits 2 when the frame doctor refuses the instance.
- `verdict-honesty` turns unbacked claims into inconclusive: `pass` without an observation, `fail` without `preconditions_held`, an exit code that disagrees with the claim, stdout that is not one result JSON object, and a secret pattern in evidence.

## How to get to it (user POV)

- Run `verilex run '<chain>'` and read the first word of output and the exit code.
- Write a word's `run` that claims `pass`, `fail` or `blocked`, then run a chain that uses it.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor.

- **Green.** Run `"$S/vx" green verilex run 'store-open | item-stored apple | item-listed apple'`. Output is `green: 3 green; run <RUN>` and exit `0`.
- **Green, second view.** Run `"$S/vx" green-runs verilex runs`. The line for `<RUN>` reads `green  cleanup=done`.
- **Red.** Run `TALLY_DEFECT=drop-adds "$S/vx" red verilex run 'store-open | item-stored apple | item-listed apple'`. Output starts `red: 1 green, 1 red, 1 not run`, names `item-stored apple: tally said 'added apple' but store.json lacks apple`, and exit is `1`.
- **Red, second view.** Run `"$S/vx" red-store cat "$S/home/tally/runs/<RUN>/02-item-stored/store.json"` with the red run id. It prints `[]`: the item really was not stored.
- **Environment trouble.** Run `TALLY_SIMULATE_LOCK=1 "$S/vx" env-blocked verilex run 'store-open | item-stored apple'`. Output starts `inconclusive: 1 inconclusive, 1 not run` with cause `... is locked by another process`, and exit is `2`, not `1`.
- **Doctor refuses.** Run `mkdir -p "$S/foreign"`, then `TALLY_ADOPT_STORE="$S/foreign" "$S/vx" doctor-refuses verilex run --fresh 'store-open | item-stored apple'`. Output is `inconclusive: 0 green, 2 not run` with `inconclusive  doctor: refused the instance (exit 1)` and exit `2`.
- **Doctor refuses, second view.** Run `"$S/vx" foreign-untouched ls -A "$S/foreign"`. It prints nothing: verilex neither drove nor removed a store it did not launch.
- **Honesty probes.** Create a probe word with `"$S/vx" probe-new verilex new probe-word --implements verify-tally/features/items.md#item-add`. For each row, write `$S/tally/.verilex/words/probe-word/run` as shown, then run `"$S/vx" honesty-<case> verilex run 'store-open | probe-word'`. Each exits `2` with the listed cause:

  | Case | `run` body after `#!/bin/sh` | Cause printed |
  |---|---|---|
  | stub | as scaffolded by `new` | `probe-word is a stub; write its run` |
  | no-observation | `echo '{"verdict": "pass"}'` | `pass without a second observation` |
  | no-preconditions | `echo '{"verdict": "fail", "detail": "broken"}'; exit 1` | `fail without stating that its preconditions held` |
  | exit-mismatch | `echo '{"verdict": "pass", "observation": "saw it"}'; exit 1` | `exit 1 disagrees with verdict pass` |
  | no-json | `echo not json` | `stdout is not one result JSON object` |
  | secret | `echo "token ghp_$(printf 'A%.0s' $(seq 36))" > "$VERILEX_EVIDENCE/leak.txt"; echo '{"verdict": "pass", "observation": "wrote leak.txt"}'` | `secret pattern in evidence leak.txt` |

- **Backed fail is red.** Write `echo '{"verdict": "fail", "preconditions_held": true, "detail": "rename lost"}'; exit 1` and run `"$S/vx" honesty-backed-fail verilex run 'store-open | probe-word'`. Output is `red: 1 green, 1 red` and exit `1`.
- **Honesty, second view.** Run `"$S/vx" honesty-runs verilex runs`. Each probe run is listed with the verdict it printed.
- **Restore.** Run `rm -r "$S/tally/.verilex/words/probe-word"`.

## Gotchas

- An exit code mismatch is reported before a missing `preconditions_held`. The `no-preconditions` probe must exit `1`.
- Never write a token-shaped literal into a repository file. Build the fake token at runtime, as in the `secret` row.
- `TALLY_ADOPT_STORE` is read only by the frame, so it is not in any word's stamp. Without `--fresh`, a matching ledger skips the chain and the doctor never runs.
- `verdict-red` needs `TALLY_DEFECT`, which the tally words declare in `env`, so the red run is live even after an admitted green run.
