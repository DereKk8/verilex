# Cleanup

`verilex cleanup <run>` tears down the instance a `--keep` run left alive, through the project's `cleanup` frame step. It is safe to repeat, explains runs that launched nothing, and refuses unknown runs and runs another run took over.

## Sub-features

- `cleanup-kept` removes a kept instance and records `cleanup=done`.
- `cleanup-repeat` reports an already cleaned run and exits 0.
- `cleanup-none` reports that a skipped run launched nothing.
- `cleanup-refused-unknown` refuses an id that is not a run of the project.
- `cleanup-refused-continued` points at the run that took the instance over.

## How to get to it (user POV)

- Run `verilex cleanup <run>`, usually copied from the `kept:` line of `verilex run --keep`.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor.

- **Kept.** Run `"$S/vx" cl-keep verilex run --keep 'store-open | item-stored apple'` (run `<RUN>`), then `"$S/vx" cl-before ls "$S/stores"`: it lists `tally-<RUN>`.
- **Tear down.** Run `"$S/vx" cl verilex cleanup <RUN>`. Stdout `cleanup: done`, exit `0`.
- **Tear down, second view.** Run `"$S/vx" cl-after ls "$S/stores"` and `"$S/vx" cl-runs verilex runs`. No store is left and `<RUN>` shows `cleanup=done`.
- **Repeat.** Run `"$S/vx" cl-again verilex cleanup <RUN>`. Stdout `verilex: <RUN> was already cleaned up`, exit `0`.
- **Unknown.** Run `"$S/vx" cl-unknown verilex cleanup 123-nope`. Stderr `verilex: refused: 123-nope is not a run of tally`, exit `2`.
- **None.** Use [run.md](run.md) `run-skip`: `verilex cleanup <skipped run>` prints `verilex: <run> launched nothing; it relied on stamps`.
- **Continued.** Use [continue.md](continue.md) `continue-handover`: `refused: <KEPT> was continued by <C>; clean up that run instead`.

## Gotchas

- `verilex cleanup` evidence lands in the run's `frame-cleanup` directory under `VERILEX_HOME`, not in the session evidence. The `vx` transcript is the durable copy.
- The session `cleanup` helper calls `verilex cleanup` for every `cleanup=kept` run before removing `$S`, so a forgotten kept run is still torn down through verilex.
