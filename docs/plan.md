# Plan

`verilex plan '<chain>'` makes the same decision `verilex run` would, through the same code, and runs nothing. It cites the run each skipped step relies on, or gives the first reason the chain runs live:

```
$ verilex plan 'store-open | item-stored apple | item-listed apple'
plan: skip 3, run 0
  skip  store-open  relies on run 1767225600-a1b2c3d4e5f6
  skip  item-stored apple  relies on run 1767225600-a1b2c3d4e5f6
  skip  item-listed apple  relies on run 1767225600-a1b2c3d4e5f6

$ verilex plan 'store-open | item-stored apple | item-listed apple'
plan: skip 0, run 3; item-listed apple: word changed
```

A refused chain is refused by `plan` too, with the same message and exit code `2`. `--json` prints the decision for every step.

## Claim plan

`verilex plan` without a chain plans claims and a diff. The caller passes claims it derived from its intent (`--claim`, repeatable), named claims (`--named`, repeatable) and the diff (`--changed`, repeatable). Named claims are always included and never limit the derived claims. With no `--claim`, the intent is "prove nothing this change touched broke" and the selected claims are exactly the claims the diff touches, plus any named claims. `verilex run` with the same flags executes `chain` and carries a missed-claim warning in the verdict. Pass a chain, or claims and a diff, not both. With `--no-chain`, `plan` and `run` refuse any chain argument, an empty one too, before anything runs or any run record is written (exit `2`), so a caller such as a launcher can allow only claim plans. Without it, an empty chain argument is ignored. A chain `plan` or `run` may still take `--changed`: a config key or image pin hit runs that chain live, and on a run, touched claims the chain did not prove are uncovered.

A diff entry is a file path, or `config:<key>`, `image:<pin>` or `runbook:<ref>` (`path:<file>` is a path). The diff intersects word dependencies: paths (the word directory, its inputs, its claim file, the feature files its sources point at, declared `depends.paths`, and the files every word shares), config keys (`depends.config_keys`), image pins (`depends.images`) and runbook section refs and hashes (claim sources and `depends.runbook`). `depends.paths` is in the proof stamp, so a change to that file cannot skip from a pass recorded before it. A config key or image pin is not in the proof stamp, so a hit runs live. A runbook hit uses the claim-sources fingerprint: a matching stamp still skips. `--continue` is refused with a claim plan, and on a chain whose diff hits a config key or image pin: a kept instance's history cannot prove a change the stamp does not cover.

`--json` prints `verilex-claim-plan-1`. A different `format` is a breaking change; a launcher should refuse any other value. Execute `chain` through `verilex run` with the same `--claim`, `--named` and `--changed` flags. Do not run only the `run` list: an earlier claim can still be proved while a later one must run, and the chain still has to provide every required state.

```json
{
  "format": "verilex-claim-plan-1",
  "intent": "prove nothing this change touched broke",
  "selected": ["item-listed"],
  "skip": [{"claim": "item-listed", "word": "item-listed", "step": "item-listed apple", "fingerprints": {"claim": "<hex>", "word": "<hex>"}, "stamp": "<hex>", "relies_on": "<run>"}],
  "run": [{"claim": "item-added", "word": "item-stored", "step": "item-stored apple", "reason": "word changed"}],
  "order": ["store-opened", "item-added", "item-listed"],
  "chain": "store-open | item-stored apple | item-listed apple",
  "touched": ["item-listed"],
  "unpicked": ["store-opened"],
  "warning": "1 touched claim not picked",
  "rerun": "item-stored apple: word changed"
}
```

- `intent` is `prove nothing this change touched broke` when no `--claim` was given, otherwise `given`.
- `selected` is the claims the plan proves, sorted. It equals `touched` when there is no intent and no named claim outside the diff.
- `skip` and `run` partition `selected`, in chain order. `skip` means a pass stands for that step's stamp and claim version and is younger than 7 days; `fingerprints` are that pass's components and `relies_on` is the run that recorded it. `run` is why no pass stands, or why a dependency the stamp does not fingerprint changed.
- `order` is every claim `chain` proves, topological from word `requires` and `provides`. `chain` may include words that only provide a state a selected claim requires. `verilex run` still skips the whole chain only when every step's stamp matches; otherwise it runs the chain live.
- `unpicked` are claims the diff touches that were not selected.
- `unmapped` are diff entries, as given, that hit no word and no claim: a typo, or a change nothing in `.verilex` depends on. `unclaimed` are touched words outside `chain` that prove no claim. `unrun` are touched words outside `chain` whose claim `chain` proves through another word. Each is omitted when empty.
- `warning` counts all four, joined by `, `: `1 touched claim not picked`, `1 change no word covers`, `1 touched word with no claim`, `1 touched word not run` (plural `N touched claims not picked` and so on). It is omitted when all four are empty.
- `rerun` is why `verilex run` executes every word in `chain`, and is omitted when the run skips them all. With `rerun`, a claim in `skip` still has a standing pass, but its step runs again. The human plan then starts `plan: whole chain runs live` and lists those claims as `proven`, not `skip`.
- An unknown claim, a stale pin, a claim with no word, or a word whose arguments no recorded chain binds is refused (exit `2`) and runs nothing.

`verilex run --json` with these flags prints the run record plus three fields, omitted on a chain run given no diff:

- `claims`: one object per claim the chain proved or failed: `claim`, `proves` (`<claim>@<version>`), `word`, `step`, `verdict`, `evidence`. A claim that is not green also has `expected` (the claim's observation), `got` (what the word reported) and `next` (the command that retries it).
- `uncovered`: touched claims this run did not prove, each `{claim, next}`.
- `unmapped`, `unclaimed` and `unrun`: as in the plan, for this run's chain. A chain run given `--changed` computes them for its own chain.
- `warning`: `1 touched claim not covered` or `N touched claims not covered`, joined by `, ` with the counts of `unmapped`, `unclaimed` and `unrun` as in the plan. The human verdict line carries the same phrase (`green: 1 green, with 1 touched claim not covered; run <id>`) and lists each entry on its own line. Exit codes stay `0`, `1` and `2`; a warning does not change them. A launcher that returns a verdict should return its warning with it.

When the diff touches no claim and no claim was given or named, nothing can prove the change, so the verdict is inconclusive, not green. `verilex run` launches nothing and prints `inconclusive: no claim covers this change; fall back to the product verify skill`, exit `2`. A mistyped path or key lands here too. If the diff touched a word that proves no claim, the reason names that word instead. `verilex plan` prints `plan: inconclusive: <reason>` and also exits `2`; its JSON keeps `verilex-claim-plan-1` and adds `inconclusive`, the reason. `verilex run --json` prints `format` `verilex-claim-run-1`, `verdict` `inconclusive`, `reason`, `requested`, `touched`, `unmapped` and `unclaimed`, and records no run. When another entry does map, an entry that hits nothing is not dropped: it is `unmapped` and counted in the warning.

A claim-plan run record, and a chain run given `--changed`, adds `format` (`verilex-claim-run-1`), `requested` (`claims`, `named`, `changed`, and `chain`: the chain the caller gave, or `null` when verilex planned the chain, also for an empty chain argument) and `touched`, so a launcher reads what verilex was asked, and who wrote the chain, from verilex, not from the agent. `claims` lists every selected claim, including one that did not run (`verdict` `inconclusive`, `got` `not run`). A claim that ran, even red, is not also `uncovered`. `verilex runs --json` lists each run's `run`, `verdict`, `warning`, `cleanup` and `chain`, and the human `runs` line ends with `warning: <phrase>` when one was recorded.
