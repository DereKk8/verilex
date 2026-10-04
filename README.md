# verilex

verilex lets verifier and QA agents run product verification as a chain of words, Unix-pipe style:

```
verilex run 'store-open | item-stored apple | item-listed apple'
```

A **word** is a reusable, executable piece that drives the product through a surface its users have and promises one product state. Someone who knows the product, but not how it is built, would recognize each word as a real moment in the product's life.

verilex holds execution only. The project's verification skill (a [pstack](https://github.com/cursor/plugins/tree/main/pstack)-style `verify-*` skill with its feature map) keeps the meaning: what a feature is, how a user reaches it, what proves it and its traps. Every word points back at the feature-map entries it puts into action, and verilex never replaces the skill. When a word does not pass, verilex prints those entries so the agent can continue by hand.

This is the first slice: the chain runner and its trust frame. Dependency-based skipping, word invention and curation come later.

## Install

```
go install github.com/DereKk8/verilex/cmd/verilex@latest
```

Use Go 1.27.1 or newer on a Unix system. Add `$(go env GOPATH)/bin` to your `PATH`.
The core is Go; frame steps and words can use any language installed on the product's workstation.

## Commands

| Command | Does |
|---|---|
| `verilex run '<chain>' [--keep] [--json]` | Plans the chain, launches an owned instance, runs each word, cleans up |
| `verilex words` | Lists the dictionary with each word's promise, `requires` and `provides` |
| `verilex runs` | Lists this project's runs and whether each instance was cleaned up |
| `verilex cleanup <run>` | Tears down an instance kept with `--keep` |

Exit codes: `0` pass, `1` fail, `2` blocked, unverified, or refused.

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
2. **Doctor** confirms the instance is this run's and worth driving, before the first word and again after any failure. A refusal stops the run as `blocked`.
3. **Evidence** for every step goes to `~/.local/state/verilex/<project>/runs/<run>/` (override with `VERILEX_HOME`), outside the product and every repository.
4. **Honesty rules** turn a word's claim into `unverified` when it is not backed:
   - `pass` without a second observation;
   - `fail` without stating that its preconditions held;
   - an exit code that disagrees with the verdict, or no result JSON;
   - a secret pattern (private key block, GitHub, AWS, Slack or Anthropic token, or a project pattern) anywhere in its evidence.
   Workstation and environment trouble is `blocked`, never `fail`.
5. **Cleanup** always runs, tears down only what the run started, and verilex then confirms the evidence survived.

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
