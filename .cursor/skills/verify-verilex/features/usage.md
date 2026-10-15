# Usage ledger

Every accepted `verilex run` records its start and its final result in a durable usage ledger, `$VERILEX_HOME/usage` unless `VERILEX_USAGE_LEDGER` names another directory. `verilex usage` prints totals: runs by verdict, unfinished runs, missed runs, elapsed seconds and caller tokens. A caller attaches its token cost with `--tokens` (on the run, or later on `usage`) and a later review's finding with `--miss`. No skip decision reads the ledger, and cleanup or deleted run evidence never removes it.

## Sub-features

- `usage-totals` prints one totals line and the ledger path; an empty ledger gives zero totals.
- `usage-counting` counts live, skipped, continued and uncovered runs, and never a refusal or a `plan`.
- `usage-tokens` records caller tokens on the run or later; a later annotation replaces the earlier total, and unknown costs are counted apart from zero.
- `usage-miss` marks a green run as a miss, counts it once however often it is marked, and refuses a red, inconclusive or unfinished run.
- `usage-unfinished` counts a run that started and never finished as `unfinished`, not as a verdict.
- `usage-durable` keeps every entry after the run's evidence is gone.
- `usage-json` prints the totals and every entry with its complete run record.

## How to get to it (user POV)

- Run `verilex run ... [--tokens N [--estimated]] [--change REF]`, then `verilex usage [--json]`.
- Run `verilex usage <run> --tokens N [--estimated]` or `verilex usage <run> --miss '<review>'` after the run.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor. `vx` refuses a `VERILEX_USAGE_LEDGER` outside `$S`, so the ledger is `$S/home/usage`.

- **Empty.** Run `"$S/vx" use-empty verilex usage`. Stdout `usage: 0 runs; green 0, red 0, inconclusive 0, unfinished 0; misses 0; 0.000s; tokens 0 (0 runs unknown, 0 runs estimated)` and `ledger: $S/home/usage`, exit `0`.
- **Count.** Run `"$S/vx" use-green verilex run 'store-open | item-stored apple' --tokens 1200 --estimated --change review-1` (green, run `<G>`), `TALLY_DEFECT=drop-adds "$S/vx" use-red verilex run 'store-open | item-stored apple'` (red, run `<RD>`) and `"$S/vx" use-uncovered verilex run --changed README.md` (inconclusive). Then run `"$S/vx" use-refused verilex run 'item-stored apple | store-open'` (refused) and `"$S/vx" use-plan verilex plan 'store-open'`.
- **Count, second view.** Run `"$S/vx" use-totals verilex usage`. It prints `usage: 3 runs; green 1, red 1, inconclusive 1, unfinished 0; misses 0; <seconds>s; tokens 1200 (2 runs unknown, 1 runs estimated)`: the refusal and the plan added nothing.
- **Tokens later.** Run `"$S/vx" use-tokens verilex usage <G> --tokens 1450`: `usage: recorded caller data for <G>`. `verilex usage` now prints `tokens 1450 (2 runs unknown, 0 runs estimated)`: the later total replaced the earlier one.
- **Miss.** Run `"$S/vx" use-miss verilex usage <G> --miss 'review-2 found a wrong add'` and `"$S/vx" use-miss-again verilex usage <G> --miss review-3`. Each prints `usage: recorded caller data for <G>`. Run `"$S/vx" use-miss-red verilex usage <RD> --miss review-4`: stderr `verilex: refused: only a green run can be marked as a Verilex miss`, exit `2`. `verilex usage` prints `misses 1`.
- **JSON.** Run `"$S/vx" use-json verilex usage --json`, then read its `stdout` file with `python3 -c 'import json,sys; u=json.load(open(sys.argv[1])); print(u["totals"]["misses"], u["totals"]["tokens"]); e=[x for x in u["entries"] if x["record"]["run"]==sys.argv[2]][0]; print(e["tokens"], e["change"], e["miss"]["review"])' <evidence>/stdout <G>`. It prints `1 1450` and `{'count': 1450, 'estimated': False} review-1 review-3`.
- **Durable.** Remove the run evidence with `"$S/vx" use-evidence-gone command rm -rf "$S/home/tally/runs"`, then run `"$S/vx" use-after-delete verilex usage`. The totals line is unchanged.
- **Unfinished.** See [parallel.md](parallel.md) **A run ended by SIGTERM**: the killed run counts as `unfinished`.

## Attack scenarios

- **An operator's own usage ledger.** The docs tell an operator to export `VERILEX_USAGE_LEDGER` for their own work, so a verification session started from that shell would add tally runs to the operator's evaluation data. Run `VERILEX_USAGE_LEDGER="$HOME/.local/state/verilex-operator-usage" "$S/vx" atk-usage-outside verilex run 'store-open'`, then `ls -d "$HOME/.local/state/verilex-operator-usage"`. Safe result: `capture: VERILEX_USAGE_LEDGER=... is outside the session; unset it, or point it inside $S`, exit `2`, and the directory does not exist. The same holds for `VERILEX_LEDGER`.
- **Annotations at the same moment.** Several callers attach token costs to one run at once, for example a retry after a timeout. Run `for i in 1 2 3 4 5 6; do "$S/vx" use-race-$i verilex usage <G> --tokens ${i}00 > "$S/race-$i.out" 2>&1 & done; wait`. Safe result: every call exits `0`, `ls "$S/home/usage/<G>/annotations"` holds one more file per call, and the totals count `<G>`'s tokens once, as one of the six values.
- **A refused continue.** An agent retries `--continue` on a run another run already continued. See [continue.md](continue.md) **A retry after a refused continue**: no `unfinished` entry is added. Source: the review of #40 (D-1), fixed in #40.
- **A token count that overflows.** A caller bug passes a huge or negative count. Run `"$S/vx" use-big-1 verilex usage <G> --tokens 9223372036854775807` and the same for `<RD>` (`use-big-2`), then `"$S/vx" use-big-read verilex usage`, and `"$S/vx" use-neg verilex usage <G> --tokens -5`. Safe result: the totals read `tokens >=9223372036854775807` and stay readable, and the negative count is refused with `--tokens needs a nonnegative integer`, exit `2`. Source: the review of #40 (D-2), fixed in #40.
- **A secret in the usage record.** See [verdicts.md](verdicts.md) **A product error message that echoes a token**: the usage entry keeps the word's `detail` whole.

## Gotchas

- `verilex usage` prints `ledger:` with the directory it read. Check that it is under `$S`.
- `tokens_estimated` counts runs, not tokens, and the token total mixes measured and estimated costs.
- A run with no covering claim prints a run id in another form (`<date>T<time>-<hex>`) and launches nothing, yet counts as `inconclusive`.
- The launcher's home is temporary unless `--home` is given, so its usage entries go with it.
