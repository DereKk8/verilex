# Verdict rule

The launcher keeps the JSON of every `verilex run` the brain makes, in the order the runs finish. The table below turns those runs into one result, and the first row that matches decides. The launcher checks rows 1-8 before it judges any run (`run.Run` in `agent/internal/run`). `verdict.Decide` in `agent/internal/verdict` owns rows 9-33, and `prompt.Rule` states the same rule to the brain. Row NN is the subtest `NN ...` of `TestVerdictRule` in `agent/rule_test.go`, so change a row and its subtest together.

The brain runs only claim plans. The launcher adds `--no-chain` to each `verilex run` and `verilex plan` that the brain asks for, so verilex itself refuses any chain argument, an empty one too, before it runs anything or writes a run record. The launcher keeps no chain parser of its own. A chain must not run inside the launcher, for two reasons:

- A chain's inputs are the brain's choice. A chain can be red on correct code (`store-open | store-open` opens one store twice), and green on inputs that miss a defect.
- A chain's run record would set the word arguments of every later claim plan in the home. For example, after `store-open | item-stored pear | item-listed pear`, `verilex run --claim item-listed` lists `pear`, not `apple`.

## Terms

The rule uses these terms:

- A **planned run** is a `verilex run` whose chain verilex planned from `--claim`, `--named` or `--changed`. verilex says so in its JSON: the format is `verilex-claim-run-1` and `requested.chain` is `null`. Each run that the launcher judges must be a planned run. A run that verilex did not plan means that verilex ignored `--no-chain`, so the result is inconclusive. A claim run without `requested.chain` comes from a verilex that is older than the launcher, and it is inconclusive too (`TestClaimRunWithoutRequestedChainIsInconclusive`).
- A red is a failure that verilex found in the code under test. The project is read-only for the brain, so no later run can undo that red.
- The **floor** is every `--claim` given to the launcher, plus each claim that `verilex index --intent <intent> --json` finds for the intent. The launcher runs this lookup itself before the brain starts, and the prompt lists the floor. A spec without an intent has only the `--claim` floor.
- A run **proves** a claim when the run is green and its `requested` names the claim (`claims` or `named`), or its `claims` or `words` show the claim green.
- An inconclusive run leaves **open** every claim that it selected: its `requested` claims, its `claims` and the claim of each of its words. A last green must prove these claims.
- A green **covers the spec** when it proves every floor claim and its `requested.changed` holds every path that the diff changes. The diff's paths are `git diff --name-only -z --no-renames --relative <diff>` in the project, so both sides of a rename count.

## The rule

The table uses the tally fixture (`tests/fixtures/tally`) and these short forms:

- `apple` is `--intent 'prove a stored apple is listed'`, with the floor `item-listed` and `item-added`. `store` is `--intent 'prove the store opens'`, with the floor `store-opened`. `nothing` is `--intent 'make the export faster'`, which names no claim. `diff` is `--diff HEAD`, with `bin/tally` changed.
- `claim X` is `verilex run --claim X`. `+ changed` adds `--changed bin/tally`. `chain A | B` is `verilex run 'A | B'`. `refused` means that verilex refused the run under `--no-chain`: exit `2`, no JSON and no run record.
- `hide-lists` is `TALLY_DEFECT=hide-lists`, which makes `item-listed` red. `hide-apple` is `TALLY_DEFECT=hide-apple`: `tally list` leaves out only `apple`, so only some inputs reach the defect. A locked store makes verilex inconclusive.
- `run N` means that the launcher prints the JSON of the brain's run N, as verilex printed it. `no JSON` means that the launcher prints `verilex-agent: inconclusive: <reason>` on stderr and nothing on stdout.

| # | Spec | The brain's runs, in order | Verdict | Exit | Why |
|---|---|---|---|---|---|
| 1 | `nothing` | none: the brain does not start | inconclusive, no JSON | 2 | The intent names no claim, and no `--claim` stands for it. So no verilex run can prove the intent. |
| 2 | `--diff HEAD..HEAD`, no intent and no `--claim` | none: the brain does not start | inconclusive, no JSON | 2 | The diff changes no file, and no intent or `--claim` names a claim. So there is nothing to prove. |
| 3 | `apple`, `verilex index` fails | none: the brain does not start | inconclusive, no JSON | 2 | The floor is unknown. The launcher ran the lookup itself, so the failure is the environment's, not a refusal. |
| 4 | `apple`, `time_budget: 1s`, `verilex index` hangs | none: the brain does not start | inconclusive, no JSON | 2 | The floor is unknown. The lookup stops when the time budget ends or after 30 seconds, whichever comes first. |
| 5 | `store`, the sandbox tool fails | none: the brain does not start | inconclusive, no JSON | 2 | The brain runs only in a sandbox. See [Sandbox](sandbox.md). |
| 6 | `store`, the sandbox tool starts the brain without a sandbox | none: the brain does not start | inconclusive, no JSON | 2 | The check inside the sandbox finds that the brain could write the project. |
| 7 | `store`, a file in the project changes on the host during the run | `claim store-opened` green | inconclusive, no JSON | 2 | The verdict can be about code that is no longer the code under test. |
| 8 | `store`, a `verilex run --keep` in the launcher's home, outside the launcher, during the run | `claim store-opened` green | inconclusive, no JSON | 2 | A run in the home did not come through the launcher, and it kept its instance. |
| 9 | `store`, a fake verilex | run 1 prints red and exits 0, run 2 green | inconclusive, no JSON | 2 | An exit that disagrees with the JSON is an environment failure, never a verdict. |
| 10 | `store`, a fake verilex | run 1 prints a claim run whose `requested.chain` is `store-open`, run 2 green | inconclusive, no JSON | 2 | verilex ran a chain although the launcher passed `--no-chain`. That is an environment failure, never a verdict. |
| 11 | `apple`, `hide-lists` | `claim item-listed` red, `claim store-opened` green | red, run 1 | 1 | A red is final. A later green on other claims cannot undo it. |
| 12 | `diff`, `hide-lists` | `verilex run --changed bin/tally ''` refused, `verilex run --changed bin/tally` red | red, run 2 | 1 | `--no-chain` refuses an empty chain argument too, so the launcher and verilex cannot read it differently. |
| 13 | `apple`, `hide-lists` | `claim item-listed` red, a forged pass written to the ledger's path, `claim item-listed` red | red, run 2 | 1 | The brain cannot reach the ledger, so the second run is not skipped. Of two reds, the launcher returns the last. |
| 14 | `apple`, `time_budget: 2s`, `hide-lists` | `claim item-listed` red, then the budget ends | red, run 1 | 1 | A red stands when the budget ends after it. |
| 15 | `apple`, `time_budget: 2s` | `claim item-listed` green, then the budget ends | inconclusive, no JSON | 2 | The brain did not finish, so only a red can decide. |
| 16 | `diff`, `time_budget: 1s`, the sandbox tool never answers | none: the budget ends before the brain starts | inconclusive, no JSON | 2 | The budget ended before the brain made a run, so there is no verdict. |
| 17 | `store` | a refused `verilex run --keep` and a `verilex plan` | inconclusive, no JSON | 2 | No verilex run printed a verdict. The brain's own message is not a verdict. |
| 18 | `store` | `chain store-open \| store-open` refused | inconclusive, no JSON | 2 | A refused chain is no run, so there is no verdict. |
| 19 | `apple`, `hide-apple` | `chain store-open \| item-stored pear \| item-listed pear + changed` refused, `claim item-listed` red | red, run 2 | 1 | The chain left no run record, so the claim plan lists `apple`, the input of the admitted chain, and finds the defect. |
| 20 | `apple`, `hide-apple` | the chain of row 19 refused, `verilex run --fresh --claim item-listed` red | red, run 2 | 1 | The same as row 19 with `--fresh`, which runs live. |
| 21 | `apple`, the store locked for run 2 | `claim item-listed` green, `claim item-listed` inconclusive | inconclusive, run 2 | 2 | The last run is the result when it is not green. |
| 22 | `store` | `chain store-open \| store-open` refused, `claim store-opened` green | green, run 2 | 0 | The green covers the spec, and the refused chain is no run. |
| 23 | `store` | `claim store-opened` green, `chain store-open \| store-open` refused | green, run 1 | 0 | Row 22 in the other order. |
| 24 | `apple`, the store locked for run 1 | `claim item-listed` inconclusive, `claim item-listed` green | green, run 2 | 0 | The green proves every claim that the inconclusive run selected, and covers the spec. |
| 25 | `store`, a fake verilex | run 1 inconclusive, asked for `item-listed` and selecting `store-opened`, `item-added` and `item-listed`, run 2 green on `store-opened` | inconclusive, no JSON | 2 | The green does not prove `item-listed` or `item-added`, which the inconclusive run selected. |
| 26 | `apple` | `claim item-listed` green | green, run 1 | 0 | The green covers the spec. Its words prove both floor claims. |
| 27 | `apple` | `claim item-listed` green, `claim store-opened` green | inconclusive, no JSON | 2 | The last green proves neither floor claim. An earlier green does not count, because the last run is the result. |
| 28 | `store`, `--claim item-listed` | `claim store-opened` green | inconclusive, no JSON | 2 | The green does not prove `item-listed`, a floor claim from `--claim`. |
| 29 | `nothing`, `--claim store-opened` | `claim store-opened` green | green, run 1 | 0 | `--claim` stands for an intent that names no claim. |
| 30 | `diff` | `claim store-opened` green | inconclusive, no JSON | 2 | The green was not asked about `bin/tally`. |
| 31 | `diff` | `verilex run --changed bin/tally` green | green, run 1 | 0 | The green covers the spec. It proves every claim that the diff touches, so verilex prints no warning. |
| 32 | `diff` | `claim store-opened + changed` green | green, run 1 | 0 | The green covers the spec. verilex's own warning, `2 touched claims not covered`, stays in the JSON. |
| 33 | `store`, `--diff HEAD` with a rename and a non-ASCII file name | `claim store-opened`, with `--changed` for each changed path in the prompt | green, run 1 | 0 | Both sides of the rename and the non-ASCII name reach the prompt and `requested.changed`. |

Each row also checks that every run record in the launcher's home is a planned run, and counts the chains that verilex refused.

In rows 14 and 15, the test ends the budget itself, after the brain records its run. `run.Run` takes the budget timer from its caller, and these rows pass a timer that only the test ends. So the brain's run comes before the end of the budget, however long the launcher takes to set up. The shipped launcher passes the wall clock (`run.WallClock`), and rows 4 and 16 use the wall clock end to end.

Row 7 checks every file that git tracks or would track, and ignores the files that git ignores. A file counts as changed when it was added, removed, written or touched between the start of the brain and its end, even if its bytes were put back. Row 8 also matches a kept instance in the home. The launcher tears down every instance that it finds in the home.

A red or inconclusive verdict is returned as verilex printed it. Exit codes are verilex's: `0` green, `1` red, `2` inconclusive or refused. `--text` prints the verdict line and each gap instead of the JSON.

One deadline, the run spec's `time_budget`, covers the intent lookup, `--suggest` and the run. No child that the brain started holds the launcher past it. Each `verilex ticket` and `verilex index` command that the launcher runs for itself also stops after 30 seconds, and the launcher is then inconclusive.

## Limits

The rule has these limits:

- The floor is only as good as the index's intent lookup. `verilex index --intent` returns up to five claims, best first, weak matches included, and the floor holds all of them. So on a large product, a brain that proves only the claims that it judged relevant can get inconclusive and must run more claims. This costs time, but it never gives a false green or a false red.
- The last run is the run that finished last. When the brain runs verilex in parallel, timing decides which run is last, so the same runs can give green or inconclusive. They cannot give a false green, because the last run must cover the spec alone.
- The brain cannot run a chain, so it cannot reach a defect that no admitted word's inputs reach. verilex proves a claim through its admitted words, so such a defect is outside the claim. Explore with chains in plain verilex, outside the launcher.
- On a diff, row 32 is green although two touched claims stay unproven, and verilex's warning in the JSON says so. A floor of the claims that the diff touches would close this gap. The launcher does not set that floor.
