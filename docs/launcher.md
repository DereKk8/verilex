# Companion launcher

`verilex-agent` is a separate module in `agent/`. The core does not import it and does not start a harness. The launcher reads the run spec through `verilex ticket`, starts the brain that spec names with the verilex skill and the intent, and prints the JSON of one `verilex run` the brain made, chosen by the [verdict rule](verdict-rule.md). That JSON is verilex's own verdict, byte for byte, including its missed-claim warning (`warning`, `uncovered`, `unmapped`, `unclaimed`, `unrun`). The brain's message is not the verdict. A real harness starts only with `--allow-harness`. A named executable (`--brain`) is how tests and bots supply a brain without one.

## Usage

```
go build -o verilex-agent ./agent/cmd/verilex-agent
verilex-agent --ticket deep.yaml --project . --skill skills/verilex/SKILL.md
verilex-agent --diff main...HEAD --claim item-listed --harness claude-code --model <model> --allow-harness
```

## What the brain may run

The project must be in a git work tree. The brain's `verilex` command is the launcher's, and the only one its sandbox shows: it may run `index`, `plan`, `run`, `words`, `claims`, `runs`, `ticket` and `check`, never with `--keep` or `--continue`. Every `run` and `plan` carries the run spec as `--ticket`, so the run record names the brain, and `--no-chain`, so verilex refuses a chain that the brain writes.

With `--suggest`, the brain is first asked, with the launcher's `verilex` blocked, for extra claims; they reach the run prompt as suggestions, never as a verdict or a ceiling. Each run gets its own state home, so two runs do not share a mutable instance. Pass the same `--ledger` to keep skip savings. A second run that reuses a home still in use is refused.

## Example: three sessions, one ledger

An orchestrating agent verifies the tally sample with three launchers, one ticket each. Each ticket names its harness and what to prove; a [profile](tickets.md#profiles) can supply the model:

| Ticket | `harness` | Proves |
|---|---|---|
| `apple.yaml` | `claude-code` | `intent: prove a stored apple is listed` |
| `store.yaml` | `codex` | `intent: prove the store opens` |
| `diff.yaml` | `pi` | `diff: HEAD`, a change that broke `tally list` |

Each launcher gets a home of its own, so the evidence its verdict names outlives the run, and all three share one ledger:

```
verilex-agent --ticket apple.yaml --home ~/vx/apple --ledger ~/vx/ledger --text --allow-harness
verilex-agent --ticket store.yaml --home ~/vx/store --ledger ~/vx/ledger --text --allow-harness
verilex-agent --ticket diff.yaml --home ~/vx/diff --ledger ~/vx/ledger --text --allow-harness
```

Inside its sandbox, each brain looks up the claims and asks verilex to plan and run them. `log/verilex.log`, kept with `--keep-work`, shows what the first brain asked, with the flags the launcher added:

```
2026-10-09T02:26:17Z verilex --project ~/tally index --intent prove a stored apple is listed -> exit 0
2026-10-09T02:26:17Z verilex --project ~/tally plan --no-chain --ticket <run>/ticket.yaml --json --claim item-listed --claim item-added -> exit 0
2026-10-09T02:26:17Z verilex --project ~/tally run --no-chain --ticket <run>/ticket.yaml --json --claim item-listed --claim item-added -> exit 0
```

verilex plans the chain, `store-open | item-stored apple | item-listed apple` here, and runs every word under the [trust frame](trust-frame.md). With `--text`, each launcher prints only verilex's verdict line:

```
green; run 1791512777-6a4f0dacff78
green; run 1791512778-c4445f6c595f; skipped
red; run 1791512778-26449ce7a66e; item-listed apple: tally list printed [], not apple
```

The second run launched nothing: the first recorded passes in the shared ledger, and its one step relies on run `1791512777-6a4f0dacff78`. The third exits `1`, whatever its brain wrote last. Without `--text`, each launcher prints verilex's JSON for its run, about 2 to 4 KB here. A claim that is not green says what it expected and got, where its evidence is, and how to retry it:

```json
{"claim": "item-listed", "proves": "item-listed@b8fa4e44bc73", "word": "item-listed", "step": "item-listed apple", "verdict": "red",
 "evidence": "~/vx/diff/tally/runs/1791512778-26449ce7a66e/03-item-listed",
 "expected": "The listing matches store.json.", "got": "tally list printed [], not apple",
 "next": "verilex run --fresh --claim 'item-listed' --changed 'bin/tally'"}
```

The orchestrating agent reads the verdict and opens the evidence only when it needs to. Each brain's reasoning, tool calls and messages stay in its sandbox, and the launcher removes the run's files at the end unless `--keep-work` is given.
