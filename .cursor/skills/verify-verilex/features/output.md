# Quiet output

`verilex run` prints the verdict first, then only the steps that are not green, each with a one-line cause, a path to its evidence and the verify-skill section the word implements. Green steps are only counted. `--json` replaces this with the complete record.

## Sub-features

- `output-green` prints one line: `green: N green; run <RUN>`.
- `output-failures-only` lists only non-green steps, each with cause, `evidence:` and `verify skill:` lines, and counts steps not run.
- `output-frame` lists a frame step that failed (launch, doctor, refresh, cleanup) as an inconclusive step with its evidence.
- `output-skip` names the run or runs a skipped chain relies on.
- `output-kept` ends a kept run with the exact `verilex cleanup` command.

## How to get to it (user POV)

- Run `verilex run '<chain>'` and read stdout.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor.

- **Green is one line.** Run `"$S/vx" out-green verilex run 'store-open | item-stored apple | item-listed apple'`. Stdout is exactly one line, `green: 3 green; run <RUN>`.
- **Green, second view.** Run `"$S/vx" out-green-json verilex run --json --fresh 'store-open | item-stored apple | item-listed apple'`. The record lists all three words with `"verdict": "green"`, so the quiet line hid only green steps.
- **Failures only.** Run `TALLY_DEFECT=drop-adds "$S/vx" out-red verilex run 'store-open | item-stored apple | item-listed apple'`. Stdout is four lines: `red: 1 green, 1 red, 1 not run; run <RUN>`, `  red  item-stored apple: <cause>`, `    evidence: $S/home/tally/runs/<RUN>/02-item-stored`, `    verify skill: verify-tally/features/items.md#item-add`. `store-open` and `item-listed` are not named.
- **Evidence by reference.** Run `"$S/vx" out-red-evidence ls "$S/home/tally/runs/<RUN>/02-item-stored"`. The printed evidence path exists and holds `actions.log` and `store.json`.
- **Frame step.** Run `mkdir -p "$S/foreign"`, then `TALLY_ADOPT_STORE="$S/foreign" "$S/vx" out-frame verilex run --fresh 'store-open'`. Stdout names `inconclusive  doctor: refused the instance (exit 1)` and its `frame-doctor` evidence path.
- **Kept.** Run `"$S/vx" out-kept verilex run --keep 'store-open'`. The last line is ``kept: tear down with `verilex cleanup <RUN>` ``. Then run `"$S/vx" out-kept-cleanup verilex cleanup <RUN>`; it prints `cleanup: done`.
- **Skip line.** In an admitted baseline, see [run.md](run.md) `run-skip`: `green: 3 green, skipped: stamps match run <A>; run <B>`.

## Gotchas

- A skipped chain can cite several runs, as `stamps match runs <A>, <B>`, when different prefixes were last proven by different runs.
- Causes are cut to one line. Read the evidence directory for the full detail.
- The evidence paths printed point into `VERILEX_HOME`, which session cleanup removes. Copy them first if the proof needs them.
