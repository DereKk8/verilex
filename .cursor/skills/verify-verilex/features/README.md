# verilex verification map

This directory is the maintained source for verifying the user-facing behavior of the verilex CLI. Read the index before driving verilex, then use the matching feature file as the recipe.

## Baseline preconditions

- A session from `.cursor/skills/verify-verilex/scripts/launch` (or `launch --bin PATH` for a given build), with `S` set to the printed session directory.
- `.cursor/skills/verify-verilex/scripts/doctor "$S"` prints `doctor: ok` and the expected binary source and sha256.
- The scratch product `$S/tally` is unmodified, except where a recipe edits it and restores it.
- A recipe that says "A fresh session" needs a session no earlier recipe used: relaunch rather than reuse, because earlier runs change use counts, ledger skips and expected run counts.
- Recipes marked "admitted baseline" first need `.cursor/skills/verify-verilex/scripts/admit-all "$S"` to print `admitted words: 3`.
- Never drive a session this run did not launch, and never point `VERILEX_HOME` at `~/.local/state/verilex`.

## Driving conventions

- Run every command through `"$S/vx" <label> ...` from the repository root.
- `<RUN>`, `<KEPT>` and `<PACKET>` stand for ids printed by an earlier step of the same recipe. Copy them from that output.
- Treat commands as literal. Keep the quoted chain and flags unchanged.
- Restore any edit to `$S/tally` before the next recipe, and confirm with `verilex check` (no drift) or `"$S/vx" restored diff -r "$S/tally/.verilex" "$PWD/tests/fixtures/tally/.verilex"` (only `grouping.yaml`, `gaps/` and any `admission.json` of a probe word differ).
- The default chain is `store-open | item-stored apple | item-listed apple`. `item-listed` is the only `read_only` word.
- A step that opens with a sub-feature id (`` `run-live`: ``) is a source of one of verilex's own canary claims in `.verilex/claims/`. After editing one, run `scripts/ci canary` and do what `verilex claims` asks.

## Proof and skip reporting

- Capture the command and its result, then a second observation from another `vx` call.
- Exit codes: `run` gives `0` green, `1` red, `2` inconclusive or refused. `plan`, `words`, `runs`, `check`, `index`, `gap`, `new`, `propose`, `admit` give `0` on success and `2` when refused. `onboard` gives `0` onboarded or undecided, `1` rejected, `2` inconclusive or refused.
- A refusal prints `verilex: refused: ...` on stderr, exits `2`, and adds no run to `verilex runs`.
- Record the feature file and sub-feature id with every evidence directory.
- Report an unreachable path with the attempted command and the unmet precondition.
- Do not report a skipped entry point as verified through a different path.
- Report one attack line per scenario tried; see [Attack scenarios](#attack-scenarios).

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the user-visible behavior. It then uses exactly five H2 sections in this order: `Sub-features`, `How to get to it (user POV)`, `Driving it with vx`, `Attack scenarios`, `Gotchas`. Keep implementation details out of the map.

## Attack scenarios

`Attack scenarios` is where the verifier tries to break the feature on purpose. List the few scenarios most worth trying, most likely times most damaging first. Each bullet names who triggers it and how it really happens, the exact `vx` commands that try it, the safe expected result, and the past bug it came from (a PR, an issue, or the review that found it) when there is one.

The realism bar: keep a scenario only when a real user, agent, operator or real failure could cause it in normal use. Examples are a retry after a timeout, a double run, two agents at once, a process killed mid-run, old records read by a newer verilex, a copy or checkout that resets file times or modes, a typo in a hand-edited file, and a product error message that echoes a token. Leave out theoretical attacks, except where verilex guards trust: secrets in evidence, a shared ledger that other writers can reach, and the launcher's sandbox. There a realistic attacker counts, such as another writer of the shared ledger or a brain that tries to forge a pass.

- Do not open an attack bullet with a sub-feature id in code: that would make it part of the sub-feature's text, and a canary claim's source.
- Every real bug in a mapped feature that no scenario would have caught becomes a scenario, citing the bug. Drop a scenario that fails the realism bar.
- A scenario whose safe result does not hold stays in the map. Its attack line says `safe: broken` until the product is fixed. Name the open issue in the scenario when one tracks it.
- Report each scenario as [the skill's Report](../SKILL.md#report) says.

## Features

- [Verdicts and exit codes](verdicts.md) - green, red and inconclusive, the honesty rules, environment trouble never reported as red, the missed-claim warning and the failing link.
- [Quiet output](output.md) - verdict first, failures only, evidence by reference.
- [Run a chain](run.md) - `verilex run` live, refused, `--keep`, `--fresh`, `--json`, and stamp-based skipping.
- [Plan](plan.md) - `verilex plan`, the same skip decision without running, including `--continue`, `--json`, and a claim plan for derived claims, named claims and a diff.
- [Stale proof sweep](stale.md) - `verilex stale`: unproven claims, provider chains, inconclusive decisions, distinct exit codes and no state changes.
- [Continue a kept instance](continue.md) - `verilex run --continue`: proven prefix skipped, refresh and doctor, refusals.
- [Run tickets](tickets.md) - `verilex ticket`, `run --ticket` and `plan --ticket`: precedence, named profiles, refusals naming the field, and concurrent runs that share no ticket state.
- [Words](words.md) - `verilex words`: the dictionary with lifecycle status.
- [Runs](runs.md) - `verilex runs`: every run with verdict and cleanup state.
- [Usage ledger](usage.md) - `verilex usage`: durable run totals, caller token costs and later misses, kept through cleanup.
- [Cleanup](cleanup.md) - `verilex cleanup`: tear down a kept instance.
- [New word](new.md) - `verilex new`: scaffold a provisional word, and a whole `.verilex/` in a new project.
- [Onboard](onboard.md) - `verilex onboard`: admit a word that proves a claim through the mapping, non-proof, mechanical match, planted-defect gate and behavioral check, and answer an undecided word's decision request.
- [Index](index.md) - `verilex index`: the generated three-tier word index, intent lookup and change lookup.
- [Propose](propose.md) - `verilex propose`: the curator packet for a word without a claim after two counted uses.
- [Admit](admit.md) - `verilex admit`: record a curator's admit or reject verdict for a word without a claim.
- [Gap](gap.md) - `verilex gap`: note a product moment the feature map lacks.
- [Check](check.md) - `verilex check`: report drift-suspect admitted words.
- [Claims](claims.md) - `verilex claims`: claim versions, stale pins, review of changed, moved and unmapped requirements and of changed prose, drift after a remap, coverage only through admitted words, and pinned rules.
- [Parallel runs](parallel.md) - concurrent runs each own an instance; cleanup and `--continue` refuse a run still going; one taker per kept instance; a run that died.
- [Shared ledger](ledger.md) - stateless instances share passes through `VERILEX_LEDGER`; concurrent recording with none lost or forged; refusal of unowned, off-contract, other-version and damaged passes; age-out.
- [Self-verification scenarios](self.md) - admitted canary checks for index tiers, changed paths, ledger reuse and integrity, drift, plan bindings, launcher diff requests, verification sessions and CI branch refs.
- [Launcher](agent.md) - `verilex-agent` returns verilex's own JSON verdict, lets an intent go green only when its judge finds the claims prove all of it (stamped and reused until drift), is inconclusive on a green that does not cover the spec (a diff's touched claims are part of the floor, so a green that skips one is never returned), runs the brain in a sandbox where the project is read-only and the ledger, the home, the real verilex and the host's network are out of reach, limits what the brain may run, keeps each run's home private, and shares skip savings through one ledger.
