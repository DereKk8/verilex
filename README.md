# verilex

verilex lets verifier and QA agents run product verification as a chain of words, Unix-pipe style:

```
verilex run 'store-open | item-stored apple | item-listed apple'
```

A **word** is a reusable, executable piece that drives the product through a surface its users have and promises one product state. Someone who knows the product, but not how it is built, would recognize each word as a real moment in the product's life.

verilex holds execution only. The project's verification skill (a [pstack](https://github.com/cursor/plugins/tree/main/pstack)-style `verify-*` skill with its feature map) keeps the meaning: what a feature is, how a user reaches it, what proves it and its traps. Every word points back at the feature-map entries it puts into action, and verilex never replaces the skill. When a word is not green, verilex prints those entries so the agent can continue by hand.

This slice holds the chain runner, its trust frame, the three verdicts, quiet output, stamp-based skipping, `verilex plan`, live reuse of a kept instance (`verilex run --continue`), and the word lifecycle (invention and curation).

## Install

```
go install github.com/DereKk8/verilex/cmd/verilex@latest
```

Use Go 1.27.1 or newer on a Unix system. Add `$(go env GOPATH)/bin` to your `PATH`.
The core is Go; frame steps and words can use any language installed on the product's workstation.

## Commands

| Command | Does |
|---|---|
| `verilex run '<chain>' [--keep] [--fresh] [--json]` | Plans the chain, skips it when every proof stamp matches and every word is admitted, else launches an owned instance, runs each word, cleans up |
| `verilex plan '<chain>' [--continue <run>] [--json]` | Runs nothing; prints whether the chain would be skipped or run live, the run each skipped step relies on, and the first reason it must run live |
| `verilex run --continue <run> '<chain>' [--keep] [--json]` | Takes over the instance `<run>` kept: `refresh`, doctor, then runs only the words that instance does not already prove |
| `verilex words` | Lists the dictionary with each word's promise, `requires`, `provides` and lifecycle status |
| `verilex runs` | Lists this project's runs and whether each instance was cleaned up |
| `verilex cleanup <run>` | Tears down an instance kept with `--keep` |
| `verilex new <word> --implements <ref>` | Scaffolds a provisional word that implements a feature-map section |
| `verilex propose <word>` | Builds a curator packet for a word used in at least two runs |
| `verilex admit <word> --verdict <file>` | Records an outside curator's `admit` or `reject` verdict |
| `verilex gap '<description>'` | Notes a product moment the feature map has no section for |
| `verilex check` | Reports admitted words that are drift-suspect |

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

1. **Launch** creates an instance owned by this run. verilex never attaches to an instance it did not launch; `--continue` only takes over one that an earlier `--keep` run launched.
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

- the word's own directory (`word.md`, `run`, its `admission.json` and anything beside them, with permissions);
- everything in `.verilex/words/` outside word directories (helpers words share), `.verilex/config.yaml` and `.verilex/frame/`;
- the paths in the word's `inputs` and the values of the variables in its `env`;
- the verilex executable;
- the stamp of the step before it, so a change anywhere upstream reaches every later word.

After a run that is not inconclusive, verilex records each green word in a ledger at `~/.local/state/verilex/<project>/runs/ledger.json`, keyed by the chain prefix that ends in that word. A later `verilex run` skips the chain only when every word is admitted (see [the word lifecycle](#the-word-lifecycle)), every word's stamp matches a recorded green result, that result's evidence still exists, and it was recorded less than 7 days ago. A skipped run launches nothing and names the run it relies on:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
green: 3 green, skipped: stamps match run 1767225600-a1b2c3; run 1767225900-d4e5f6
```

Anything missing or unclear runs the chain live: a word without `inputs` or with an empty `inputs` list, an input that is missing, an unreadable ledger, evidence that is gone, a green result 7 days old or older, a stamp that changed while the run was going. A **provisional** or **drift-suspect** word, or one whose admission record cannot be read, also runs the chain live, however well its stamp matches: its results are still recorded, but none is trusted until the word is admitted and unchanged. Skipping is all or nothing, because each run starts from a fresh instance: a word that has to run needs the effects of every word before it, and every word after it depends on its new result. `--keep` and `--fresh` always run live. `--json` names the first reason a chain ran live in `rerun`, for example `item-listed apple: provisional; only admitted words are skipped`.

A stamp covers only what it lists. A word that reads anything else (another file, a service, a tool's version) must declare it in `inputs` or `env`, or leave `inputs` out (or empty) so it is never skipped.

### Plan

`verilex plan '<chain>'` makes the same decision `verilex run` would, through the same code, and runs nothing. It cites the run each skipped step relies on, or gives the first reason the chain runs live:

```
$ verilex plan 'store-open | item-stored apple | item-listed apple'
plan: skip 3, run 0
  skip  store-open  relies on run 1767225600-a1b2c3
  skip  item-stored apple  relies on run 1767225600-a1b2c3
  skip  item-listed apple  relies on run 1767225600-a1b2c3

$ verilex plan 'store-open | item-stored apple | item-listed apple'
plan: skip 0, run 3; item-listed apple: word changed
```

A refused chain is refused by `plan` too, with the same message and exit code `2`. `--json` prints the decision for every step.

### Continuing a kept instance

`verilex run --keep` leaves its instance alive and records its **history**: every word that drove it, in order, with its stamp and verdict. `verilex run --continue <run> '<chain>'` takes that instance over instead of launching one:

1. It decides, before touching anything, which steps the instance already proves. It skips the longest prefix of the chain whose steps each match the instance's next history entry with the same stamp, under the same rules as above (green, evidence present, younger than 7 days, every word admitted). Every step after that prefix runs live on the instance.
2. It runs the project's `refresh` frame step, which brings the instance up to the current checkout while keeping its states, then the doctor. A failing `refresh` or doctor makes the run `inconclusive`.
3. It runs the remaining words and cleans the instance up, unless `--keep` is given again.

```
$ verilex run --continue 1767225600-a1b2c3 --keep 'store-open | item-stored apple | item-listed apple'
green: 3 green, continued 1767225600-a1b2c3, 2 skipped: proven on its instance by run 1767225600-a1b2c3; run 1767225900-d4e5f6
kept: tear down with `verilex cleanup 1767225900-d4e5f6`
```

A word that changes the instance is not safe to run twice, so `--continue` never runs one on top of effects the chain before it would not have built. When the first word that must run live would follow history that still holds a word that changes state (because that word itself changed, is held, or its result expired), `--continue` refuses before anything starts and asks for a fresh run:

```
$ verilex run --continue 1767225600-a1b2c3 'store-open | item-stored apple | item-listed apple'
verilex: refused: item-stored apple: word changed; the kept instance already holds the effects of item-stored apple from run 1767225600-a1b2c3, and a word that changes state never runs twice or out of order on one instance: run without --continue
```

Only words whose contract declares `read_only: true` are passed over in that history: they observe the instance and never change it, so a changed read-only word runs again on the kept instance, as above. A change to the frame (including `refresh` itself), to shared word files, to verilex or to an input changes every stamp, so it always ends in that refusal. `--fresh` cannot be combined with `--continue`.

The continuing run owns the instance from then on: the kept run shows `cleanup=continued` in `verilex runs`, and `verilex cleanup` and `--continue` on it point at the run that took over. Results proven on a continued instance stand only in that instance's history, never in the ledger, so they never skip a chain on a fresh instance.

## The word lifecycle

A word is **provisional** until an outside curator admits it. verilex never calls a model and never edits a verify skill.

1. **Invent.** `verilex new item-renamed --implements verify-tally/features/items.md#item-add` writes `.verilex/words/item-renamed/` with a contract stub and a `run` that claims `blocked` until written, so its steps are `inconclusive`. Every `--implements` must resolve to a feature-map section, or `new` refuses. In a project without `.verilex/`, `new` also creates `config.yaml` and frame stubs (`launch`, `doctor`, `refresh`, `cleanup`) that exit 2, so every run is `inconclusive` until they are written. `refresh` brings a kept instance up to the current checkout, keeping its states; only `--continue` calls it.
2. **Use.** Provisional words run in chains like any other, always live. A use counts when the word's step was `green` or `red` in a live run whose verdict was `green` or `red`. Inconclusive steps, inconclusive runs and skipped runs do not count, and a run counts once however often the word appears in it.
3. **Propose.** `verilex propose <word>` refuses a word with counted uses in fewer than two different runs. Otherwise it writes one self-contained JSON packet to `~/.local/state/verilex/<project>/proposals/<word>/<id>.json`: the word's files, its uses with verdicts and evidence paths, the text and hash of each feature-map section it implements, the whole dictionary with each word's status, and instructions for the curator.
4. **Admit.** The curator writes a verdict file:

   ```json
   {"word": "item-renamed", "packet": "<id>", "verdict": "admit", "curator": "<model that judged>", "reason": "..."}
   ```

   `verilex admit <word> --verdict <file>` refuses a verdict whose packet is unknown, or whose word files or sections changed since the packet was built. Both verdicts are kept beside the packet. Only `admit` writes `.verilex/words/<word>/admission.json` (date, curator, packet, counted runs, word digest, and the hash of each implemented section); a rejected word stays as it was. The admission record is part of the word's stamp, so admitting a word runs its chains live once more before they can be skipped.
5. **Drift.** `verilex check` compares each admitted word's stored section hashes and word digest with the project now. A changed or missing section, or changed word files, makes the word **drift-suspect**: it always runs, and no earlier result is trusted for it, even though a feature-map section is not part of its stamp. Drift is computed on every call, never cached. Propose a drift-suspect word again to re-admit it.

`verilex gap 'A user renames an item'` writes a note to `.verilex/gaps/` for the verify skill's owner when the feature map has no section for a product moment.

A reference is `<skill>/<file>#<section>`, looked up under the project's skill directories. The section is the heading whose slug matches, up to the next heading of the same or a higher level. A sub-feature id written as inline code in the file (`` `item-add` ``) selects the whole feature file, since a sub-feature's meaning spreads across its file.

## Adding verilex to a project

```
.verilex/
  config.yaml          project: <name>; optional secret_patterns: [<regex>, ...];
                       optional skills: [<dir>, ...] (default .cursor/skills, .claude/skills, .agents/skills)
  frame/launch         prints {"instance": <anything>} on stdout
  frame/doctor         exit 0 when the instance is this run's and healthy
  frame/refresh        brings a kept instance up to the current checkout, keeping its states;
                       needed only by `verilex run --continue`
  frame/cleanup        removes what this run launched
  words/<word>/word.md contract (YAML frontmatter) and a short description
  words/<word>/run     the executable word
  words/<word>/admission.json  written by `verilex admit`; absent while the word is provisional
  gaps/                notes from `verilex gap` for the verify skill's owner
```

Frame steps and words receive these environment variables: `VERILEX_RUN` (the id of the run that launched the instance, which a continuing run keeps; label everything you launch with it), `VERILEX_INSTANCE` (the launch's `instance`, as JSON; `null` if launch failed), `VERILEX_PROJECT_ROOT` and `VERILEX_EVIDENCE` (this step's evidence directory).

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
read_only: false         # optional; true when the word only observes the instance (it then provides no states)
---
```

`run` receives its arguments on the command line and a state document on stdin (`run`, the same id as `VERILEX_RUN`; `instance`, `states`, `args`, `evidence`). It writes its action and observation files into its evidence directory and prints one result object on stdout, exiting `0`, `1` or `2` to match:

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
