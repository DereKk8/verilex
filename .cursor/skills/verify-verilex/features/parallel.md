# Parallel runs

Many `verilex run` processes can target one product at once. Each run launches a store of its own, labeled with its run id, and no run drives, takes over or tears down a store another run holds while that run's process lives. One run's cleanup leaves every other run's store alone. A run whose process died holds nothing, so `verilex cleanup` can still tear its store down.

## Sub-features

- `parallel-own-instance` gives each concurrent run a store of its own, whose `owner` label is that run's id.
- `parallel-refuse-running` refuses `verilex cleanup` and `verilex run --continue` on a run that is still going, before anything starts, and records no run.
- `parallel-adopted-instance` stops a run whose launch hands back another run's store at the doctor, before any word runs; its cleanup leaves that store alone.
- `parallel-cleanup-isolated` tears down only the finished run's store while the other runs keep theirs.
- `parallel-one-taker` hands a kept instance to exactly one of several concurrent `--continue` runs; the others are refused and record no run.
- `parallel-dead-run` shows a run whose process died as `died` in `verilex runs`, and lets `verilex cleanup` tear down its store.
- `parallel-plan-shared` lets concurrent `verilex plan --continue` calls on one kept run all answer.

## How to get to it (user POV)

- Start several `verilex run '<chain>'` processes at once in one product checkout, as a launcher or several agents do.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor.
- The probe word, installed with `.cursor/skills/verify-verilex/scripts/hold-word "$S"`. A run that reaches `store-held` touches `$S/hold/<run>` and waits until `touch "$S/hold/<run>.go"`.
- `parallel-one-taker` needs the admitted baseline (`admit-all`). Run it last.

Start background runs with their output in a file, so the calling shell returns while they hold.

- **Own instance.** Run `for i in 1 2 3 4; do "$S/vx" par-run-$i verilex run 'store-open | store-held | item-stored apple' > "$S/par-$i.out" 2>&1 & done`. Then run `"$S/vx" par-held ls "$S/hold"` until it lists four run ids `<A> <B> <C> <D>`.
- **Own instance, second view.** Run `"$S/vx" par-owners sh -c 'for d in "$TALLY_STORES"/*; do echo "$(basename "$d") $(cat "$d/owner")"; done'`. It prints four lines, `tally-<id> <id>`, one for each held run.
- **Refuse running.** Run `"$S/vx" par-cleanup-live verilex cleanup <A>` and `"$S/vx" par-continue-live verilex run --continue <A> 'store-open'`. Each prints stderr `verilex: refused: <A> is still running and owns its instance; wait until it finishes` and exits `2`.
- **Refuse running, second view.** Run `"$S/vx" par-runs-live verilex runs`. It lists only `<A>` to `<D>`, each `running  cleanup=pending`, and `tally-<A>` is still in `$S/stores`.
- **Adopted instance.** Run `TALLY_ADOPT_STORE="$S/stores/tally-<A>" "$S/vx" par-adopt verilex run 'store-open | item-stored apple'`. Output `inconclusive: 0 green, 2 not run; run <E>` with `inconclusive  doctor: refused the instance (exit 1)`, exit `2`.
- **Adopted instance, second view.** Run `"$S/vx" par-adopt-cleanup cat "$S/home/tally/runs/<E>/frame-cleanup/stdout"`: `left $S/stores/tally-<A> alone: not owned by run <E>`. Run `"$S/vx" par-adopt-store cat "$S/stores/tally-<A>/store.json" "$S/stores/tally-<A>/owner"`: `{"items": []}` and `<A>`, unchanged.
- **Cleanup isolated.** Run `touch "$S/hold/<A>.go"`, then `"$S/vx" par-runs-a verilex runs` until `<A>` shows `green  cleanup=done`. Run `"$S/vx" par-stores-after ls "$S/stores"`: it lists `tally-<B>`, `tally-<C>` and `tally-<D>`, and `"$S/vx" par-others cat "$S/stores/tally-<B>/store.json"` prints `{"items": []}`.
- **Finish.** Run `touch "$S/hold/<B>.go" "$S/hold/<C>.go" "$S/hold/<D>.go"`, then wait until each `$S/par-$i.out` ends with `exit 0`. Each holds `green: 3 green; run <id>`. Run `"$S/vx" par-drove python3 -c 'import json,sys; [print(json.load(open(p))["run"], json.load(open(p))["words"][1]["observation"]) for p in sys.argv[1:]]' "$S"/home/tally/runs/<A>/run.json "$S"/home/tally/runs/<B>/run.json`: each run drove `$S/stores/tally-<id> owned by <id>`, its own id. `ls "$S/stores"` is empty.
- **Dead run.** Run `"$S/vx" par-dead verilex run 'store-open | store-held' > "$S/par-dead.out" 2>&1 &`, and wait until `$S/hold` lists its id `<F>`. Kill only this session's verilex: `for p in /proc/[0-9]*; do [ "$(readlink "$p/exe" 2>/dev/null)" = "$S/bin/verilex" ] && kill -KILL "${p#/proc/}"; done`. Then run `touch "$S/hold/<F>.go"` so the orphaned probe word exits.
- **Dead run, second view.** Run `"$S/vx" par-dead-runs verilex runs`: `<F>  died  cleanup=pending`, with `tally-<F>` still in `$S/stores`. Run `"$S/vx" par-dead-cleanup verilex cleanup <F>`: `cleanup: done`, exit `0`, `$S/stores` is empty, and `verilex runs` shows `<F>  died  cleanup=done`.
- **One taker (admitted baseline).** Run `.cursor/skills/verify-verilex/scripts/admit-all "$S"`, then `"$S/vx" par-keep verilex run --keep 'store-open | item-stored apple | item-listed apple'` (run `<K>`). Run `for i in 1 2 3 4 5 6; do "$S/vx" par-take-$i verilex run --continue <K> 'store-open | item-stored apple | item-listed apple' > "$S/take-$i.out" 2>&1 & done; wait`. Exactly one `take-$i.out` holds `green: 3 green, continued <K>, 3 skipped: proven on its instance by run <K>; run <W>` and `exit 0`. Each of the others exits `2` with `verilex: refused: <K> was continued by <W>; continue that run instead` or `verilex: refused: <K>'s instance is in use by another verilex command; try again once it finishes`.
- **Plans never refuse each other.** Before the race, run `for i in 1 2 3 4 5 6; do "$S/vx" par-plan-$i verilex plan --continue <K> 'store-open | item-stored apple | item-listed apple' > "$S/plan-$i.out" 2>&1 & done; wait`. Each `plan-$i.out` holds `plan: skip 3, run 0 on the instance kept by <K>` and `exit 0`.
- **One taker, second view.** Run `"$S/vx" par-take-runs verilex runs`. Only one run continues `<K>`: `<K>` shows `cleanup=continued`, `<W>` shows `green  cleanup=done`, and no other run was added. `ls "$S/stores"` is empty.
- **Restore.** Run `.cursor/skills/verify-verilex/scripts/hold-word "$S" --remove`.

## Gotchas

- Never `wait` for held runs in the call that started them: the call would block until they are released.
- Kill a run only through the session binary path (`$S/bin/verilex`), never by name: other verilex processes on the machine are not yours.
- The order in which concurrent runs reach `$S/hold` and finish is not fixed. Use the ids `ls "$S/hold"` and the `.out` files print, never a position.
- Which refusal a losing `--continue` prints depends on timing: before the winner took the instance over, or while it does. Both are refusals, and both leave no run behind.
- `store-held` is provisional and implements no claim, so chains that hold it always run live.
