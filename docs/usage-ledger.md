# Usage ledger

Every accepted `verilex run` records its start and final result in a usage ledger. This is evaluation data, not the proof-stamp ledger in `internal/ledger`: no skip decision reads it, and its entries never expire.

The default is `$VERILEX_HOME/usage`, or `~/.local/state/verilex/usage` when `VERILEX_HOME` is unset. The directory sits outside each project's `runs/`, so instance cleanup and deletion of run evidence do not delete usage data. For our own work, point every caller at one persistent directory:

```sh
export VERILEX_USAGE_LEDGER=/persistent/verilex-usage
```

Use this setting with `verilex-agent`, too. Its default run home is temporary, so a usage ledger inside that home would disappear when the launcher removes the home. The explicit directory remains on the host and outside the brain's private home. Keep the directory outside word inputs and restrict write access. verilex does not provision or back up storage.

## Run and read

```sh
verilex run --changed internal/cli/cli.go --change 'branch or review URL'
verilex usage
verilex usage --json
```

The ledger spans products and state homes that use that directory. Each entry holds the complete judged run record, including product, word claims and versions, requested changes, verdict, gaps and warnings. The entry also holds elapsed seconds, caller token cost, a caller change reference when supplied, and the starting Git revision and tracked diff SHA-256 when available. The diff hash includes staged and unstaged tracked changes, not untracked file contents. Use `--change` to identify an uncommitted or non-Git change unambiguously.

A request with no covering claim gets an inconclusive usage entry and prints a run ID, but launches nothing and adds no instance to `verilex runs`. Refusals before execution, `plan`, and onboarding trials do not count as usage runs. Starts without a recorded completion count as `unfinished`, not red or green. Final results include live, skipped, and continued runs.

`usage` prints totals for runs by verdict, unfinished runs, distinct missed runs, elapsed seconds and caller tokens. Unknown token costs are counted separately from zero. Estimated costs are labelled separately from measured costs. `--json` returns the totals and entries. An empty ledger returns zero totals.

## Caller token costs and later misses

verilex cannot see agent tokens. The caller supplies a total, either on the run or after the verification step finishes:

```sh
verilex run --claim chain-runs-live --tokens 1200 --estimated
verilex usage RUN --tokens 1450 --estimated
verilex usage RUN --miss 'later review URL or finding'
```

Omit `--estimated` only for a measured token total. A later token annotation replaces the earlier total for that run, so totals never count the same run's tokens twice. Annotation files retain the earlier reports. A later zero-trust reviewer marks a green run as a miss when the change was wrong. Red, inconclusive and unfinished runs cannot receive a miss. Repeated miss annotations count one missed run and retain each review note.

Run snapshots use atomic replacement. Each annotation has its own file, so concurrent runs and annotations cannot overwrite one another. The latest timestamped token or review annotation supplies the read view. The ledger keeps evidence references, not extra evidence files. Those references can stop working if run evidence is deleted.

If recording fails, verilex prints `usage ledger write failed` on stderr and preserves the product verdict and exit code. Treat that warning as lost usage data and fix the storage. A crash before the start snapshot is published cannot be recorded. The first version does not import older runs, infer missing token costs, publish evaluations, or supply a dashboard.
