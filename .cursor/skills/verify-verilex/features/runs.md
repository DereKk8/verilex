# Runs

`verilex runs` lists this project's runs, one line each: run id, verdict, cleanup state and chain. The cleanup state tells a user whether the run's instance is gone (`done`), still alive (`kept`), handed to another run (`continued`) or never launched (`none`).

## Sub-features

- `runs-list` prints `<run>  <verdict>  cleanup=<state>  <chain>` for every recorded run.
- `runs-cleanup-state` shows `done`, `kept`, `continued` and `none`.
- `runs-liveness` shows a run without a verdict as `running` while its process lives and `died` once it is gone.
- `runs-refused-absent` shows that refused commands record no run.

## How to get to it (user POV)

- Run `verilex runs` in a product checkout.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor.

- **Empty.** Run `"$S/vx" runs-empty verilex runs`. Exit `0` and no output.
- **Done.** Run `"$S/vx" runs-green verilex run 'store-open | item-stored apple'` (run `<A>`), then `"$S/vx" runs-done verilex runs`. One line: `<A>  green  cleanup=done  store-open | item-stored apple`.
- **Done, second view.** Run `"$S/vx" runs-record python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); print(r["verdict"], r["cleanup"], r["chain"])' "$S/home/tally/runs/<A>/run.json"`. It prints `green done store-open | item-stored apple`.
- **Kept and continued.** Use [continue.md](continue.md) `continue-handover`: the kept run shows `cleanup=continued` and the continuing run `cleanup=kept`.
- **None.** Use [run.md](run.md) `run-skip`: the skipped run shows `cleanup=none`.
- **Liveness.** Use [parallel.md](parallel.md) `parallel-own-instance` (`running  cleanup=pending` while held) and `parallel-dead-run` (`died`).
- **Refused absent.** Run `"$S/vx" runs-refused verilex run 'item-stored apple | store-open'`, then `"$S/vx" runs-after-refused verilex runs`. The list is unchanged.

## Gotchas

- Runs are sorted by run id (`<epoch>-<hex>`). Two runs in the same second are not in time order, so find a run by the id its command printed, never with `tail -1`.
- `inconclusive` runs are listed too. Only `verilex propose` skips them when counting uses.
