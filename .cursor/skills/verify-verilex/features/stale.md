# Stale proof sweep

`verilex stale` lists claims whose planned proof chains cannot be skipped now. It checks every grouped claim, including dormant claims, uses the claim planner's words and arguments, and runs the shared skip decision. The sweep runs no words and changes no state.

## Sub-features

- `stale-quiet` prints nothing and exits `0` when every claim's chain can be skipped, or there are no claims.
- `stale-unproven` prints one `unproven  <claim>: <reason>` line per chain that must run, and exits `1`.
- `stale-providers` includes a dependent claim when a provider prevents its whole chain from skipping.
- `stale-inconclusive` prints `inconclusive  <claim>: <reason>` for a chain it cannot plan or a ledger it cannot read. It continues through other claims and exits `2`.
- `stale-read-only` leaves product files, state, ledger, usage records and instances unchanged.

## How to get to it (user POV)

Run `verilex stale` inside a product checkout, or `verilex --project <dir> stale` from elsewhere. No flags or operands are required.

## Driving it with vx

Use a fresh session that passes the doctor.

1. `"$S/vx" stale-pristine verilex stale` exits `2`. `item-added` and `item-listed` are inconclusive because no chain binds their arguments. `store-opened` is unproven because no green result exists. `"$S/vx" stale-pristine-runs verilex runs` prints nothing.
2. `"$S/vx" stale-use verilex run 'store-open | item-stored apple | item-listed apple'` records a green. `"$S/vx" stale-provisional verilex stale` exits `1`, with one line each for `item-added`, `item-listed` and `store-opened`: `store-open: provisional; only admitted words are skipped`.
3. Run `.cursor/skills/verify-verilex/scripts/admit-all "$S"`, then `"$S/vx" stale-admitted-live verilex run 'store-open | item-stored apple | item-listed apple'`. `"$S/vx" stale-clean verilex stale` prints nothing, exit `0`. `"$S/vx" stale-clean-plan verilex plan --claim item-listed` says `skip 1, run 0`.
4. Save `"$S/tally/.verilex/words/item-stored/run"` outside the product. Append `# changed` to that script. `"$S/vx" stale-provider verilex stale` exits `1` with exactly two lines: `unproven  item-added: item-stored apple: word changed` and `unproven  item-listed: item-stored apple: word changed`. `"$S/vx" stale-provider-plan verilex plan --claim item-listed` says its whole chain runs live for that reason.
5. Compare `"$S/vx" stale-runs-before verilex runs` and `"$S/vx" stale-runs-after verilex runs` around another sweep. Compare file names, contents and modes under `$S/tally`, `$S/home`, `$S/config` and `$S/stores` before and after too. Nothing changes. Restore the saved script, then `"$S/vx" stale-restored verilex stale` again prints nothing, exit `0`.
6. Save `"$S/tally/.verilex/words/item-stored"` outside the product, then remove that exact word directory. `"$S/vx" stale-missing-provider verilex stale` exits `2`: `item-added` has no word, and `item-listed` has no provider for `item:apple`. `"$S/vx" stale-missing-provider-runs verilex runs` shows no new run. Restore the saved directory.

## Attack scenarios

- **An unmounted shared ledger.** An operator runs the sweep while the share behind `VERILEX_LEDGER` is not mounted. Run `VERILEX_LEDGER="$S/unmounted-share" "$S/vx" atk-stale-unmounted verilex stale`, then `"$S/vx" atk-stale-unmounted-dir ls -d "$S/unmounted-share"`. Safe result: never exit `0`. Today every claim is `unproven ... no green result on record`, exit `1`, and the directory is not created. Source: the review of #50 (O2).
- **A sweep while runs record passes.** Agents sweep in a loop while other agents run chains into the same ledger. After `admit-all`, start `for i in 1 2 3 4; do "$S/vx" atk-stale-busy-$i verilex run --fresh 'store-open | item-stored kiwi | item-listed kiwi' > "$S/busy-$i.out" 2>&1 & done`, then run `for i in $(seq 25); do "$S/vx" atk-stale-sweep-$i verilex stale > /dev/null; done` and `wait`. Safe result: every sweep exits `0` or `1`, none prints `damaged` or `unreadable`, every `stderr` file is empty, and each `busy-$i.out` holds a green. Source: the review of #50 (S7, O1).
- **Words onboarded by an older verilex.** After an upgrade, every word onboarded before verilex hashed permissions is drift-suspect. Build the old binary as in [check.md](check.md) **Older record**, onboard `store-open` with it, run the default chain once live, then `"$S/vx" atk-stale-old verilex stale`. Safe result: `store-opened` is listed (exit `1`), never a clean exit `0`. Source: #51, #52.

## Gotchas

- Exit `1` means unproven, not a red product verdict. Exit `0` is not a new live verdict.
- A matching pass for a selected claim is not enough if a provider prevents its chain from skipping.
- The first reason comes from the shared chain decision. A changed word can report `word changed` before `drift-suspect`. Once a fresh green matches that changed stamp, drift still prevents reuse.
- The sweep checks declared stamp inputs only. It does not infer external config changes or image updates that no stamp fingerprints. Pass those changes to `plan` or `run` with `--changed`.
- Missing bindings, stale claim pins and missing providers are inconclusive because the planner cannot form the proof chain. Other claims are still checked.
