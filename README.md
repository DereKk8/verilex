# verilex

verilex lets verifier and QA agents run product verification as a chain of words, Unix-pipe style:

```
verilex run 'store-open | item-stored apple | item-listed apple'
```

A **word** is a reusable, executable piece that drives the product through a surface its users have and promises one product state. Someone who knows the product, but not how it is built, would recognize each word as a real moment in the product's life.

verilex holds execution only. The project's verification skill (a [pstack](https://github.com/cursor/plugins/tree/main/pstack)-style `verify-*` skill with its feature map) keeps the meaning: what a feature is, how a user reaches it, what proves it and its traps. Every word points back at the feature-map entries it puts into action, and verilex never replaces the skill. When a word is not green, verilex prints those entries so the agent can continue by hand.

This slice holds the chain runner, its trust frame, the three verdicts, quiet output and stamp-based skipping. Reusing a kept instance, word invention and curation come later.

## Install

```
go install github.com/DereKk8/verilex/cmd/verilex@latest
```

Use Go 1.27.1 or newer on a Unix system. Add `$(go env GOPATH)/bin` to your `PATH`.
The core is Go; frame steps and words can use any language installed on the product's workstation.

## Commands

| Command | Does |
|---|---|
| `verilex run '<chain>' [--keep] [--fresh] [--json]` | Plans the chain, skips it when every proof stamp matches, else launches an owned instance, runs each word, cleans up |
| `verilex words` | Lists the dictionary with each word's promise, `requires` and `provides` |
| `verilex runs` | Lists this project's runs and whether each instance was cleaned up |
| `verilex cleanup <run>` | Tears down an instance kept with `--keep` |

Exit codes: `0` green, `1` red, `2` inconclusive or refused.

## Verdicts

| Verdict | Means |
|---|---|
| `green` | The product works, backed by evidence. |
| `red` | The product is broken, backed by evidence. |
| `inconclusive` | Environment or harness trouble, or a claim that is not backed. Never reported as a product failure. |

A word claims `pass`, `fail` or `blocked` (see the word contract); verilex judges that claim and turns it into a verdict. A run is red only when a word is red and the doctor still vouches for the instance afterwards; anything that undermines the run makes it inconclusive. `verilex runs` reads runs recorded with the older labels (`pass`, `fail`, `blocked`, `unverified`) as the three verdicts.

## Output

`verilex run` prints the verdict first, then only the steps that are not green, each with a one-line cause and a path to its evidence; green steps are only counted:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
red: 1 green, 1 red, 1 not run; run 1767225600-a1b2c3
  red  item-stored apple: tally said 'added apple' but store.json lacks apple
    evidence: ~/.local/state/verilex/tally/runs/1767225600-a1b2c3/02-item-stored
    verify skill: verify-tally/features/items.md#item-add
```

`--json` prints the complete run record instead: every step, frame step, stamp and reason.

## The grammar

- A chain is words joined by `|`. Arguments follow the word: `item-stored apple`.
- Each word declares the states it `requires` and `provides`; these encode the product's order of reality. verilex refuses a chain in which a word requires a state that no earlier word provides, before it starts anything:

  ```
  $ verilex run 'item-stored apple | store-open'
  verilex: refused: item-stored apple requires store; nothing earlier provides it
  ```

- Agents are creative by composing existing words into new chains within those rules.

## The trust frame

Every run goes through the project's frame, and no word can opt out of it:

1. **Launch** creates an instance owned by this run. verilex never attaches to an instance it did not launch.
2. **Doctor** confirms the instance is this run's and worth driving, before the first word and again after any failure. A refusal stops the run as `inconclusive`.
3. **Evidence** for every step goes to `~/.local/state/verilex/<project>/runs/<run>/` (override with `VERILEX_HOME`), outside the product and every repository.
4. **Honesty rules** turn a word's claim into `inconclusive` when it is not backed, so it is never green or red:
   - `pass` without a second observation;
   - `fail` without stating that its preconditions held;
   - an exit code that disagrees with the verdict, or no result JSON;
   - a secret pattern (private key block, GitHub, AWS, Slack or Anthropic token, or a project pattern) anywhere in its evidence.
   Workstation and environment trouble is `inconclusive`, never `red`.
5. **Cleanup** always runs, tears down only what the run started, and verilex then confirms the evidence survived.

## Proof stamps and skipping

Every word result carries a proof stamp: a fingerprint of everything the result depended on.

- the word's own directory (`word.md`, `run` and anything beside them, with permissions);
- everything in `.verilex/words/` outside word directories (helpers words share), `.verilex/config.yaml` and `.verilex/frame/`;
- the paths in the word's `inputs` and the values of the variables in its `env`;
- the verilex executable;
- the stamp of the step before it, so a change anywhere upstream reaches every later word.

After a run that is not inconclusive, verilex records each green word in a ledger at `~/.local/state/verilex/<project>/runs/ledger.json`, keyed by the chain prefix that ends in that word. A later `verilex run` skips the chain only when every word's stamp matches a recorded green result and that result's evidence still exists. A skipped run launches nothing and names the run it relies on:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
green: 3 green, skipped: stamps match run 1767225600-a1b2c3; run 1767225900-d4e5f6
```

Anything missing or unclear runs the chain live: a word without `inputs` or with an empty `inputs` list, an input that is missing, an unreadable ledger, evidence that is gone, a stamp that changed while the run was going. Skipping is all or nothing, because each run starts from a fresh instance: a word that has to run needs the effects of every word before it, and every word after it depends on its new result. `--keep` and `--fresh` always run live. `--json` names the first reason a chain ran live in `rerun`.

A stamp covers only what it lists. A word that reads anything else (another file, a service, a tool's version) must declare it in `inputs` or `env`, or leave `inputs` out (or empty) so it is never skipped.

## Adding verilex to a project

```
.verilex/
  config.yaml          project: <name>; optional secret_patterns: [<regex>, ...]
  frame/launch         prints {"instance": <anything>} on stdout
  frame/doctor         exit 0 when the instance is this run's and healthy
  frame/cleanup        removes what this run launched
  words/<word>/word.md contract (YAML frontmatter) and a short description
  words/<word>/run     the executable word
```

Frame steps and words receive these environment variables: `VERILEX_RUN` (the run id; label everything you launch with it), `VERILEX_INSTANCE` (the launch's `instance`, as JSON; `null` if launch failed), `VERILEX_PROJECT_ROOT` and `VERILEX_EVIDENCE` (this step's evidence directory).

A word's contract:

```yaml
---
word: item-stored
promise: A named item is in the store.
args: [name]
requires: [store]
provides: ["item:{name}"]
inputs: [bin/tally]      # product paths the result depends on; omit or leave empty to never skip
env: [TALLY_DEFECT]      # environment variables the result depends on, optional
implements:
  - verify-tally/features/items.md#item-add
timeout: 1800            # seconds, optional
---
```

`run` receives its arguments on the command line and a state document on stdin (`run`, `instance`, `states`, `args`, `evidence`). It writes its action and observation files into its evidence directory and prints one result object on stdout, exiting `0`, `1` or `2` to match:

```json
{"verdict": "pass", "observation": "store.json lists apple"}
{"verdict": "fail", "preconditions_held": true, "detail": "tally said 'added apple' but store.json lacks apple"}
{"verdict": "blocked", "detail": "the store is locked by another process"}
```

`tests/fixtures/tally/` is a complete example: a made-up inventory CLI, its verification skill and its `.verilex/` directory.

## Development

```
go test ./...
go vet ./...
go build -o verilex ./cmd/verilex
```

The Go behavior tests drive the compiled CLI against tally. The sample product and its words
use Python 3, so tests require `python3` and a POSIX shell. The verilex core does not require Python.
