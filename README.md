# verilex

verilex lets verifier and QA agents run product verification as a chain of words, Unix-pipe style:

```
verilex run 'store-open | item-stored apple | item-listed apple'
```

A **word** is a reusable, executable piece that drives the product through a surface its users have and promises one product state. Someone who knows the product, but not how it is built, would recognize each word as a real moment in the product's life.

verilex holds execution only. The project's verification skill (a [pstack](https://github.com/cursor/plugins/tree/main/pstack)-style `verify-*` skill with its feature map) keeps the meaning: what a feature is, how a user reaches it, what proves it and its traps. A **claim** pins one piece of that meaning for verilex: what is true once a word passes, and the evidence that proves it. Every word proves a claim (or, in older projects, points straight at feature-map entries), and verilex never replaces the skill. When a word is not green, verilex prints the entries behind it so the agent can continue by hand.

This slice holds the chain runner, its trust frame, the three verdicts, quiet output, stamp-based skipping, `verilex plan`, live reuse of a kept instance (`verilex run --continue`), claims, run tickets, the word lifecycle (invention, onboarding and curation), and the generated word index.

## Install

```
go install github.com/DereKk8/verilex/cmd/verilex@latest
```

Use Go 1.27.1 or newer on a Unix system. Add `$(go env GOPATH)/bin` to your `PATH`.
The core is Go; frame steps and words can use any language installed on the product's workstation.

## Commands

| Command | Does |
|---|---|
| `verilex run '<chain>' [--keep] [--fresh] [--ticket <file>] [--json]` | Plans the chain, skips it when every proof stamp matches and every word is admitted, else launches an owned instance, runs each word, cleans up |
| `verilex run [--claim <claim> ...] [--named <claim> ...] [--changed <change> ...] [--json]` | Runs the chain from the claim plan. The verdict names any touched claim the run did not prove |
| `verilex plan '<chain>' [--continue <run>] [--ticket <file>] [--json]` | Runs nothing; prints whether the chain would be skipped or run live, the run each skipped step relies on, and the first reason it must run live |
| `verilex plan [--claim <claim> ...] [--named <claim> ...] [--changed <change> ...] [--json]` | Plans claims and a diff: skip (with the fingerprints that prove it), run, chain order, and touched claims not picked. No `--claim` means prove nothing the diff touched broke |
| `verilex run --continue <run> '<chain>' [--keep] [--json]` | Takes over the instance `<run>` kept: `refresh`, doctor, then runs only the words that instance does not already prove |
| `verilex ticket <file> [--json]` | Validates a run ticket and prints each field with the level it came from |
| `verilex words` | Lists the dictionary with each word's promise, claim, `requires`, `provides` and lifecycle status |
| `verilex claims [--json]` | Lists each claim's current version, the words that prove it, and why it needs review |
| `verilex runs` | Lists this project's runs and whether each instance was cleaned up; a run without a verdict shows `running` while its process lives and `died` once it is gone |
| `verilex cleanup <run>` | Tears down an instance kept with `--keep`, or left behind by a run whose process died; refuses a run that is still going |
| `verilex new <word> --implements <ref>` | Scaffolds a provisional word that implements a feature-map section |
| `verilex onboard <word> [--json]` | Admits a word that proves a claim: checks its uses, mapping and evidence, matches its claim against the vocabulary, gates it on planted defects, and records the decision in `.verilex/grouping.yaml` |
| `verilex onboard <word> --same-as <claim> \| --distinct` | Answers the decision request an undecided onboarding left: the word's claim says the same as that grouped claim, or stays a claim of its own |
| `verilex index [<claim> [<word>]] [--json]` | Prints the word index for this product: active claims, a claim's words, or one word's details |
| `verilex index --intent '<text>' [--json]` | Finds the claims an intent names |
| `verilex index --changed <change>... [--json]` | Lists the claims a diff affects, and for each known chain whether `plan` would run it. A change is a file path, `config:<key>`, `image:<pin>` or `runbook:<ref>` |
| `verilex propose <word>` | Builds a curator packet for a word without a claim, used in at least two runs |
| `verilex admit <word> --verdict <file>` | Records an outside curator's `admit` or `reject` verdict on such a packet |
| `verilex gap '<description>'` | Notes a product moment the feature map has no section for |
| `verilex check` | Reports admitted words that are drift-suspect, including those whose claim needs review |

Exit codes: `0` green, `1` red, `2` inconclusive or refused.

## Verdicts

| Verdict | Means |
|---|---|
| `green` | The product works, backed by evidence. |
| `red` | The product is broken, backed by evidence. |
| `inconclusive` | Environment or harness trouble, or a word's report that is not backed. Never reported as a product failure. |

A word reports `pass`, `fail` or `blocked` (see the word contract); verilex judges that report and turns it into a verdict. A run is red only when a word is red and the doctor still vouches for the instance afterwards; anything that undermines the run makes it inconclusive. `verilex runs` reads runs recorded with the older labels (`pass`, `fail`, `blocked`, `unverified`) as the three verdicts.

## Output

`verilex run` prints the verdict first, then only the steps that are not green, each with a one-line cause and a path to its evidence; green steps are only counted:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
red: 1 green, 1 red, 1 not run; run 1767225600-a1b2c3d4e5f6
  red  item-stored apple: tally said 'added apple' but store.json lacks apple
    evidence: ~/.local/state/verilex/tally/runs/1767225600-a1b2c3d4e5f6/02-item-stored
    verify skill: verify-tally/features/items.md#item-add
```

`--json` prints the complete run record instead: every step, frame step, stamp and reason.

## The grammar

- A chain is words joined by `|`. Arguments follow the word: `item-stored apple`.
- Each word declares the states it `requires` and `provides`, and inherits those its claim pins; these encode the product's order of reality. verilex refuses a chain in which a word requires a state that no earlier word provides, before it starts anything:

  ```
  $ verilex run 'item-stored apple | store-open'
  verilex: refused: item-stored apple requires store, pinned by claim item-added; nothing earlier provides it
  ```

- Agents are creative by composing existing words into new chains within those rules.

## The trust frame

Every run goes through the project's frame, and no word can opt out of it:

1. **Launch** creates an instance owned by this run. verilex never attaches to an instance it did not launch; `--continue` only takes over one that an earlier `--keep` run launched.
2. **Doctor** confirms the instance is this run's and worth driving, before the first word and again after any failure. A refusal stops the run as `inconclusive`.
3. **Evidence** for every step goes to `~/.local/state/verilex/<project>/runs/<run>/` (override with `VERILEX_HOME`), outside the product and every repository.
4. **Honesty rules** turn a word's report into `inconclusive` when it is not backed, so it is never green or red:
   - `pass` without a second observation;
   - `fail` without stating that its preconditions held;
   - an exit code that disagrees with the verdict, or no result JSON;
   - a secret pattern (private key block, GitHub, AWS, Slack or Anthropic token, or a project pattern) anywhere in its evidence.
   Workstation and environment trouble is `inconclusive`, never `red`.
5. **Cleanup** always runs, tears down only what the run started, and verilex then confirms the evidence survived.

### Parallel runs

Many runs may target one product at once, from one machine or from many stateless instances, and no two of them share a mutable instance. Each run gets an id no other run holds (`<epoch>-<12 hex digits>`) and passes it to every frame step and word as `VERILEX_RUN`. Launch labels the instance it creates with that id, the doctor refuses an instance that does not carry it before any word runs, and cleanup removes only what carries it. A run holds its instance for as long as its process lives:

- `verilex cleanup <run>` and `verilex run --continue <run>` refuse a run that is still going, before anything starts: `verilex: refused: <run> is still running and owns its instance; wait until it finishes`.
- A kept instance goes to exactly one continuing run. Every other attempt to take it over is refused and records no run.
- A run whose process died holds nothing: `verilex runs` shows it `died`, and `verilex cleanup <run>` tears down the instance it left behind.
- Reading a run never blocks another: concurrent `verilex plan --continue <run>` calls all answer.

## Proof stamps and skipping

Every word result carries a proof stamp: a fingerprint of everything the result depended on.

- the word's own directory (`word.md`, `run`, its `admission.json` and anything beside them, with permissions);
- for a word that proves a claim, the seal of its onboarding decision (`word admission`), so onboarding runs its chains live once more;
- the fingerprint of the claim version the word proves, so a pass is evidence only for that version, and the claim's sources, so the first run after a re-map is live;
- everything in `.verilex/words/` outside word directories (helpers words share), `.verilex/config.yaml` and `.verilex/frame/`;
- the paths in the word's `inputs` and the values of the variables in its `env`;
- the verilex executable;
- the stamp of the step before it, so a change anywhere upstream reaches every later word.

After a run that is not inconclusive, verilex records each green word as a pass in [the ledger](#the-shared-ledger), keyed by the chain prefix that ends in that word. A later `verilex run` skips the chain only when every word is admitted (see [the word lifecycle](#the-word-lifecycle)) and a pass stands for every word's stamp: it proved the claim version the word pins, its evidence still exists and meets the evidence contract, and it was recorded less than 7 days ago. A skipped run launches nothing and names the run it relies on:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
green: 3 green, skipped: stamps match run 1767225600-a1b2c3d4e5f6; run 1767225900-d4e5f6a7b8c9
```

Anything missing or unclear runs the chain live: a word without `inputs` or with an empty `inputs` list, an input that is missing, an unreadable ledger, a pass that does not stand (see [the shared ledger](#the-shared-ledger)), evidence that is gone, a green result 7 days old or older, a stamp that changed while the run was going. A **provisional** or **drift-suspect** word, or one whose admission record or grouping file cannot be read, also runs the chain live, however well its stamp matches: its results are still recorded, but none is trusted until the word is admitted and unchanged. Skipping is all or nothing, because each run starts from a fresh instance: a word that has to run needs the effects of every word before it, and every word after it depends on its new result. `--keep` and `--fresh` always run live. `--json` names the first reason a chain ran live in `rerun`, for example `item-listed apple: provisional; only admitted words are skipped`.

A stamp covers only what it lists. A word that reads anything else (another file, a service, a tool's version) must declare it in `inputs` or `env`, or leave `inputs` out (or empty) so it is never skipped.

### The shared ledger

The ledger is a directory of passes that every verilex instance pointed at it reads and writes. By default it is `~/.local/state/verilex/<project>/ledger/` (under `VERILEX_HOME`). With `VERILEX_LEDGER` set, it is `$VERILEX_LEDGER/<project>/`. Point stateless instances, each with a state home of its own, at one shared directory, and they keep the skip savings: a pass one instance records skips the same chain on every other. [docs/ledger-placement.md](docs/ledger-placement.md) records why the ledger is a directory and not a single in-repo file or a service, with measurements.

Each pass is one file, `passes/<slot>/<stamp>.<digest>.json`, beside its own copy of the step's evidence under `evidence/`. It records the claim version the word proved, the stamp and every fingerprint behind it, the observation, the sha256 of the word's result object, when it was recorded, the run that recorded it and the run that launched the instance it drove. verilex only adds pass files and never edits one. So concurrent runs that prove one step each add a pass of their own: no pass is lost, and no two are mixed into one. Recording a step drops that step's passes that are 7 days old or older, with their evidence.

Only a green step of a run that is not inconclusive and that launched its own instance becomes a pass. Its evidence must also meet the evidence contract: exit code 0 and a result that reports `pass` with a second observation. A reader trusts no file, so a pass stands for a step only when:

- its content matches the digest in its name;
- it proved the claim version the step's word pins now, never another version;
- the run that recorded it launched the instance it drove;
- its copy of the evidence still exists, its result object is unchanged, and it still meets the evidence contract;
- it was recorded less than 7 days ago.

Otherwise the chain runs live and names the first reason, for example:

```
$ verilex plan 'store-open | item-stored apple | item-listed apple'
plan: skip 0, run 3; item-stored apple: the pass from run 1767225600-a1b2c3d4e5f6 misses the evidence contract: pass without a second observation
```

The digest catches a damaged or hand-edited pass, not a deliberate forgery: anyone who can write the ledger directory can write a pass that stands. Give write access only to the instances that run verilex. The first run after upgrading from a version that kept `runs/ledger.json` runs live, because that file is no longer read.

### Plan

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

### Claim plan

`verilex plan` without a chain plans claims and a diff. The caller passes claims it derived from its intent (`--claim`, repeatable), named claims (`--named`, repeatable) and the diff (`--changed`, repeatable). Named claims are always included and never limit the derived claims. With no `--claim`, the intent is "prove nothing this change touched broke" and the selected claims are exactly the claims the diff touches, plus any named claims. `verilex run` with the same flags executes `chain` and carries a missed-claim warning in the verdict. Pass a chain, or claims and a diff, not both. A chain `plan` or `run` may still take `--changed`: a config key or image pin hit runs that chain live, and on a run, touched claims the chain did not prove are uncovered.

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

A claim-plan run record, and a chain run given `--changed`, adds `format` (`verilex-claim-run-1`), `requested` (`claims`, `named`, `changed`) and `touched`, so a launcher reads what verilex was asked from verilex, not from the agent. `claims` lists every selected claim, including one that did not run (`verdict` `inconclusive`, `got` `not run`). A claim that ran, even red, is not also `uncovered`. `verilex runs --json` lists each run's `run`, `verdict`, `warning`, `cleanup` and `chain`, and the human `runs` line ends with `warning: <phrase>` when one was recorded.

### Continuing a kept instance

`verilex run --keep` leaves its instance alive and records its **history**: every word that drove it, in order, with its stamp and verdict. `verilex run --continue <run> '<chain>'` takes that instance over instead of launching one:

1. It decides, before touching anything, which steps the instance already proves. It skips the longest prefix of the chain whose steps each match the instance's next history entry with the same stamp, under the same rules as above (green, evidence present, younger than 7 days, every word admitted). Every step after that prefix runs live on the instance.
2. It runs the project's `refresh` frame step, which brings the instance up to the current checkout while keeping its states, then the doctor. A failing `refresh` or doctor makes the run `inconclusive`.
3. It runs the remaining words and cleans the instance up, unless `--keep` is given again.

```
$ verilex run --continue 1767225600-a1b2c3d4e5f6 --keep 'store-open | item-stored apple | item-listed apple'
green: 3 green, continued 1767225600-a1b2c3d4e5f6, 2 skipped: proven on its instance by run 1767225600-a1b2c3d4e5f6; run 1767225900-d4e5f6a7b8c9
kept: tear down with `verilex cleanup 1767225900-d4e5f6a7b8c9`
```

A word that changes the instance is not safe to run twice, so `--continue` never runs one on top of effects the chain before it would not have built. When the first word that must run live would follow history that still holds a word that changes state (because that word itself changed, is held, or its result expired), `--continue` refuses before anything starts and asks for a fresh run:

```
$ verilex run --continue 1767225600-a1b2c3d4e5f6 'store-open | item-stored apple | item-listed apple'
verilex: refused: item-stored apple: word changed; the kept instance already holds the effects of item-stored apple from run 1767225600-a1b2c3d4e5f6, and a word that changes state never runs twice or out of order on one instance: run without --continue
```

Only words whose contract declares `read_only: true` are passed over in that history: they observe the instance and never change it, so a changed read-only word runs again on the kept instance, as above. A change to the frame (including `refresh` itself), to shared word files, to verilex or to an input changes every stamp, so it always ends in that refusal. `--fresh` cannot be combined with `--continue`.

The continuing run owns the instance from then on: the kept run shows `cleanup=continued` in `verilex runs`, and `verilex cleanup` and `--continue` on it point at the run that took over. Results proven on a continued instance stand only in that instance's history, never in the ledger, so they never skip a chain on a fresh instance.

## Claims

A **claim** is what is true once a word passes, plus the evidence that proves it. Several words (variants) can prove one claim, so naming words stays free while their meaning stays pinned. The verify skill wins on rules (what must happen, what counts as evidence), and a claim pins those rules for verilex. verilex wins on recorded facts (what already passed, at which stamps). Each claim lives in `.verilex/claims/<claim>.yaml`:

```yaml
claim: item-added
sentence: A named item is in the store.
args: [name]                 # placeholders its states use; a word that proves it takes them all
entry: [cli]                 # the user entry points that can prove it
preconditions: []            # configuration, build and environment postures it holds under, optional
requires: [store]            # the order of reality, pinned for every word that proves it
provides: ["item:{name}"]
evidence:
  action: "`tally add NAME` exits 0 and prints `added NAME`."
  observation: store.json lists NAME.         # an independent, read-only second look
  non_proofs:                                  # looks like success, proves nothing; optional
    - "`added NAME` alone does not prove the item was stored."
defects:                     # planted defects: states in which the claim is false (see Onboarding)
  dropped-add: {TALLY_DEFECT: drop-adds}
sources:
  - ref: verify-tally/features/items.md#item-add    # <skill>/<file>#<sub-feature id>
    prose: 2948a95bd310                             # the sub-feature's prose, as its reviewer accepted it
    requirements:                                   # the requirement sentences the claim maps to
      - "Expect exit 0 and `added NAME`; `store.json` lists NAME."
```

**Identity and versions.** A claim's fingerprint covers its sentence, args, entry points, preconditions, `requires`, `provides` and evidence contract, not its planted defects or sources. Whitespace, key order and the order of list entries never change it. Any other change to them is a new version. `verilex claims` prints each claim as `<claim>@<version>`, the version being the first 12 hex digits of the fingerprint.

**Words pin a version.** A word proves a claim by pinning a version in its contract (`claim: item-added@18e2db0cee8f`) and naming the entry point it exercises (`entry: cli`). The claim's fingerprint is part of the word's proof stamp, so a pass is evidence only for the version it ran against, and a pass through one entry point never stands for another. When a claim changes, a chain that holds a word pinned to the old version is refused before anything starts:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
verilex: refused: item-stored pins claim item-added@18e2db0cee8f, which is now item-added@27d4aafc09fd; a pass proves only the version it ran against: check that item-stored still proves the claim, then pin item-added@27d4aafc09fd
```

Re-pinning changes the word, so it runs live (`item-stored apple: claim changed`) and is onboarded again; only uses that proved the new version count.

**Pinned rules.** The claim's `requires` and `provides` join every word's own, so a variant cannot drop the order of reality, and a chain that breaks it is refused before it starts:

```
$ verilex run 'item-put apple | store-open'
verilex: refused: item-put apple requires store, pinned by claim item-added; nothing earlier provides it
```

A word that proves a claim must also take every arg the claim uses, name one of its entry points, leave out `implements` (its sources come from the claim), and not be `read_only` when the claim provides states. Otherwise the dictionary refuses to load.

**Sources and review.** A source anchors the claim on a stable sub-feature id and the requirement sentences the claim maps to, not on a whole file or section. A sub-feature is the text its id names in the feature file: every list item, paragraph or table row that opens with the id as inline code, with the lines nested under it and any fenced block that follows it with only blank lines between, and the section under a heading whose anchor is the id. A mention of the id anywhere else names nothing. So a feature file labels each step with the sub-feature it drives:

```markdown
## Sub-features

- `item-add` stores a named item.

## Driving it with the tally CLI

- `item-add`: Run `bin/tally --store "$STORE" add NAME`. Expect exit 0 and `added NAME`; `store.json` lists NAME.
```

A requirement sentence says `Expect`, `must`, `require`, `exits`, `returns` or `Success is` outside its code spans. A sentence that starts with `Run `, after an optional sub-feature label, is an action, so its command never counts. A sentence with a date, run history included, is read like the rest. Headings and fenced blocks are not sentences. A fence opens with three or more backticks or tildes. Sentences are compared without list markers, emphasis or line breaks, and literal values in code spans count. Each sentence a source maps must be one such requirement sentence, or the claim is refused when it loads.

Everything else in the sub-feature is its **prose**: headings and the other sentences, the words of `Run` sentences included, with each inline code span blanked, plus every line of its fenced blocks as written. A source pins the prose by its 12-digit fingerprint, which `verilex claims` prints.

The claim **needs review** when its sub-feature is gone, when a sentence it maps is gone or now sits outside the sub-feature, when the sub-feature states a requirement sentence that no claim maps, or when the sub-feature's prose is not what the source pins. `verilex claims` and `verilex check` name the sentence (or the prose fingerprint to pin after review), and every admitted word that proves the claim is drift-suspect, so no chain that holds one is skipped:

```
$ verilex claims
item-added@18e2db0cee8f  A named item is in the store.
  entry: cli  words: item-stored
  review: verify-tally/features/items.md#item-add: requirement changed or gone: Expect exit 0 and `added NAME`; `store.json` lists NAME.
  review: verify-tally/features/items.md#item-add: requirement no claim maps: Expect exit 0 and `added NAME`; `store.json` must list NAME.
```

Inline commands, layout and another sub-feature's text ask for nothing. An edit inside a fenced block of the sub-feature does, and so does a run-history line: verilex cannot tell a dated rule from history, so it asks. A requirement sentence that another claim maps in the same sub-feature is covered there, but only while one of that claim's words is onboarded for its current version and sources. A claim without a word, a word that never ran or only ran, and a source added after the word was onboarded cover nothing: only onboarding, which checks the claim's step-to-claim mapping, says that the word exercises the sentence. Sources are not part of the claim's identity. When the reviewer judges that the claim still says the same, they map it to the new sentences or pin the new prose: the version stays, and the uses recorded for it still count toward onboarding. Onboarding checked the claim's words against the old sources, though, so each becomes drift-suspect (`onboarded before claim item-added's sources changed; onboard it again`) and runs live until it is onboarded again. The sources are also part of the proof stamp, so even a restored mapping runs live once. A sentence outside the sub-feature never clears a review. When the claim's meaning changed, the reviewer changes its identity instead, which makes a new version.

**Sources in another repository.** When the verify skill lives in another repository, a source names a checkout of it with `repo:` (a path relative to the project root, or absolute), and its `ref` is looked up under that checkout's default skill directories. Onboarding records the repository (its `origin` URL, or the path when it has none) and the commit it checked the mapping against, and refuses while the source's feature file has uncommitted changes there, so the recorded commit holds what was checked. A source in another repository never covers a local sub-feature with the same reference.

## Run tickets

A run ticket fixes everything about one verification run: what it is meant to prove and which brain (harness, model and effort) drives it. Many independent runs can each choose their own brain without editing a shared file. verilex carries and validates the ticket and never acts on its brain fields: it launches no harness and calls no model. A companion launcher reads the resolved ticket and starts the harness.

```yaml
intent: prove a renamed item keeps its count   # what the run is meant to prove
diff: main...HEAD          # the change to verify, as a git revision or range; a ticket names intent, diff or both
profile: deep-verify       # a named profile, optional
harness: claude-code       # optional when a lower level sets it
model: claude-opus-5-5
effort: high               # minimal, low, medium, high, xhigh or max
token_budget: 400000       # optional, a positive whole number
time_budget: 45m           # optional, a positive duration such as 30m or 1h30m
```

Each of `harness`, `model`, `effort`, `token_budget` and `time_budget` comes from the first level that sets it:

1. the ticket;
2. its named profile;
3. the project default, in `.verilex/profiles.yaml`;
4. the user default, in `$XDG_CONFIG_HOME/verilex/profiles.yaml` (by default `~/.config/verilex/profiles.yaml`);
5. the built-in, which sets only `effort: medium`.

A profiles file holds `defaults` (any of those fields, plus a default `profile`) and named `profiles`:

```yaml
defaults:
  profile: quick-verify
  harness: claude-code
profiles:
  quick-verify: {model: claude-haiku-4-5, effort: low, time_budget: 10m}
  deep-verify: {model: claude-opus-5-5, effort: xhigh, token_budget: 400000}
```

The profile is the ticket's `profile`, else the project default's, else the user default's. A project profile replaces a user profile of the same name as a whole. A resolved ticket must name a harness and a model.

`verilex ticket <file>` prints the resolved ticket, each field with the level it came from; `--json` prints it whole, with `from` naming each level:

```
$ verilex ticket deep.yaml
intent: prove a renamed item keeps its count
profile: deep-verify  from ticket
harness: claude-code  from user default
model: claude-opus-5-5  from project profile deep-verify
effort: xhigh  from project profile deep-verify
token_budget: 400000  from project profile deep-verify
```

`verilex run --ticket <file>` and `verilex plan --ticket <file>` resolve the ticket before anything starts, and refuse an invalid one with exit code `2`, naming the file and the field:

```
$ verilex run --ticket deep.yaml 'store-open | item-stored apple'
verilex: refused: ticket deep.yaml: effort: "turbo" is not one of minimal, low, medium, high, xhigh, max
```

A run records its resolved ticket in its own run record (`ticket` in `--json` and `run.json`), and a plan prints it in `--json`. verilex only reads the ticket and profiles files and writes nothing shared for a ticket, so concurrent runs with different tickets never conflict. The ticket never changes what runs or what is skipped: it is not part of any proof stamp, and `.verilex/profiles.yaml` is not either.

## Companion launcher

`verilex-agent` is a separate module in `agent/`. The core does not import it and does not start a harness. The launcher reads the run spec through `verilex ticket`, starts the brain that spec names with the verilex skill and the intent, and prints the JSON of one `verilex run` the brain made, chosen by the [verdict rule](#verdict-rule). That JSON is verilex's own verdict, byte for byte, including its missed-claim warning (`warning`, `uncovered`, `unmapped`, `unclaimed`, `unrun`). The brain's message is not the verdict. A real harness starts only with `--allow-harness`. A named executable (`--brain`) is how tests and bots supply a brain without one.

```
go build -o verilex-agent ./agent/cmd/verilex-agent
verilex-agent --ticket deep.yaml --project . --skill skills/verilex/SKILL.md
verilex-agent --diff main...HEAD --claim item-listed --harness claude-code --model <model> --allow-harness
```

The project must be in a git work tree. The brain's `verilex` command is the launcher's, and the only one its sandbox shows: it may run `index`, `plan`, `run`, `words`, `claims`, `runs`, `ticket` and `check`, never with `--keep` or `--continue`; every `run` and `plan` carries the run spec as `--ticket`, so the run record names the brain.

With `--suggest`, the brain is first asked, with the launcher's `verilex` blocked, for extra claims; they reach the run prompt as suggestions, never as a verdict or a ceiling. Each run gets its own state home, so two runs do not share a mutable instance. Pass the same `--ledger` to keep skip savings. A second run that reuses a home still in use is refused.

### Verdict rule

The launcher keeps the JSON of every `verilex run` the brain makes, in the order the runs finish. The table below turns those runs into one result, and the first row that matches decides. The launcher checks rows 1-8 before it judges any run (`run.Run` in `agent/internal/run`). `verdict.Decide` in `agent/internal/verdict` owns rows 9-34, and `prompt.Rule` states the same rule to the brain. Row NN is the subtest `NN ...` of `TestVerdictRule` in `agent/rule_test.go`, so change a row and its subtest together.

The rule uses these terms:

- A **planned run** is a `verilex run` with `--claim`, `--named` or `--changed` and no chain argument, so verilex plans its chain. A red from a planned run is a failure that verilex found in the code under test. The project is read-only for the brain, so no later run can undo that red.
- A **brain chain** is a `verilex run` with a chain argument that the brain wrote, with or without `--changed`. The launcher reads the arguments of each run to tell a brain chain from a planned run. It does not use the JSON format, because a chain with `--changed` also prints `verilex-claim-run-1`. A brain chain can be red on correct code: `store-open | store-open` opens one store twice. So a red brain chain decides only when it is the last run.
- The **floor** is every `--claim` given to the launcher, plus each claim that `verilex index --intent <intent> --json` finds for the intent. The launcher runs this lookup itself before the brain starts, and the prompt lists the floor. A spec without an intent has only the `--claim` floor.
- A run **proves** a claim when the run is green and its `requested` names the claim (`claims` or `named`), or its `claims` or `words` show the claim green.
- The **open claims** of an earlier run are the claims that the last run must prove. An inconclusive run leaves open every claim that it selected: its `requested` claims, its `claims` and the claim of each of its words. A red brain chain leaves open the claim of each red word.
- A green **covers the spec** when it is a claim run (format `verilex-claim-run-1`, with `requested`), it proves every floor claim, and its `requested.changed` holds every path that the diff changes. The diff's paths are `git diff --name-only -z --no-renames --relative <diff>` in the project, so both sides of a rename count.

The table uses the tally fixture (`tests/fixtures/tally`) and these short forms:

- `apple` is `--intent 'prove a stored apple is listed'`, with the floor `item-listed` and `item-added`. `store` is `--intent 'prove the store opens'`, with the floor `store-opened`. `nothing` is `--intent 'make the export faster'`, which names no claim. `diff` is `--diff HEAD`, with `bin/tally` changed.
- `claim X` is `verilex run --claim X`. `+ changed` adds `--changed bin/tally`. `chain A | B` is `verilex run 'A | B'`.
- The defect is `TALLY_DEFECT=hide-lists`, which makes `item-listed` red. A locked store makes verilex inconclusive.
- `run N` means that the launcher prints the JSON of the brain's run N, as verilex printed it. `no JSON` means that the launcher prints `verilex-agent: inconclusive: <reason>` on stderr and nothing on stdout.

| # | Spec | The brain's runs, in order | Verdict | Exit | Why |
|---|---|---|---|---|---|
| 1 | `nothing` | none: the brain does not start | inconclusive, no JSON | 2 | The intent names no claim, and no `--claim` stands for it. So no verilex run can prove the intent. |
| 2 | `--diff HEAD..HEAD`, no intent and no `--claim` | none: the brain does not start | inconclusive, no JSON | 2 | The diff changes no file, and no intent or `--claim` names a claim. So there is nothing to prove. |
| 3 | `apple`, `verilex index` fails | none: the brain does not start | inconclusive, no JSON | 2 | The floor is unknown. The launcher ran the lookup itself, so the failure is the environment's, not a refusal. |
| 4 | `apple`, `time_budget: 1s`, `verilex index` hangs | none: the brain does not start | inconclusive, no JSON | 2 | The floor is unknown. The lookup stops when the time budget ends or after 30 seconds, whichever comes first. |
| 5 | `store`, the sandbox tool fails | none: the brain does not start | inconclusive, no JSON | 2 | The brain runs only in a sandbox. See [Sandbox](#sandbox). |
| 6 | `store`, the sandbox tool starts the brain without a sandbox | none: the brain does not start | inconclusive, no JSON | 2 | The check inside the sandbox finds that the brain could write the project. |
| 7 | `store`, a file in the project changes on the host during the run | `claim store-opened` green | inconclusive, no JSON | 2 | The verdict can be about code that is no longer the code under test (F1). |
| 8 | `store`, a `verilex run --keep` in the launcher's home, outside the launcher, during the run | `claim store-opened` green | inconclusive, no JSON | 2 | A run in the home did not come through the launcher, and it kept its instance (F5). |
| 9 | `store`, a fake verilex | run 1 prints red and exits 0, run 2 green | inconclusive, no JSON | 2 | An exit that disagrees with the JSON is an environment failure, never a verdict. |
| 10 | `apple`, the defect | `claim item-listed` red | red, run 1 | 1 | A planned red is a failure that verilex found in the code under test. |
| 11 | `apple`, the defect | `claim item-listed` red, `claim store-opened` green | red, run 1 | 1 | A planned red is final. A later green on other claims cannot undo it (F6). |
| 12 | `diff`, the defect | `verilex run --changed bin/tally` red, `claim store-opened + changed` green | red, run 1 | 1 | The same as row 11, on the diff (F6). |
| 13 | `apple`, the defect | `claim item-listed` red, a forged pass written to the ledger's path, `claim item-listed` red | red, run 2 | 1 | The brain cannot reach the ledger, so the second run is not skipped. Of two planned reds, the launcher returns the last (F2). |
| 14 | `apple`, `time_budget: 2s`, the defect | `claim item-listed` red, then the budget ends | red, run 1 | 1 | A planned red stands when the budget ends after it (F4). |
| 15 | `apple`, `time_budget: 2s` | `claim item-listed` green, then the budget ends | inconclusive, no JSON | 2 | The brain did not finish, so only a planned red can decide. The same applies when the budget ends before any run (F4). |
| 16 | `store`, `time_budget: 2s` | `chain store-open \| store-open` red, then the budget ends | inconclusive, no JSON | 2 | A red brain chain decides only as the last run of a brain that finished. |
| 17 | `store` | a refused `verilex run --keep` and a `verilex plan` | inconclusive, no JSON | 2 | No verilex run printed a verdict. The brain's own message is not a verdict. |
| 18 | `store` | `chain store-open \| store-open` red | red, run 1 | 1 | A red brain chain that is the last run is the result. |
| 19 | `apple`, the store locked for run 2 | `claim item-listed` green, `claim item-listed` inconclusive | inconclusive, run 2 | 2 | The last run is the result when it is not green. |
| 20 | `store` | `chain store-open \| store-open` red, `claim store-opened` green | green, run 2 | 0 | The green proves `store-opened`, the claim of the chain's red word, and covers the spec (F7). |
| 21 | `apple`, the defect | `chain store-open \| item-stored apple \| item-listed apple` red, `claim store-opened` green | inconclusive, no JSON | 2 | The green does not prove `item-listed`, the claim of the chain's red word (F6 as a chain). |
| 22 | `diff`, the defect | the chain of row 21 `+ changed` red, `claim store-opened + changed` green | inconclusive, no JSON | 2 | The same as row 21, on the diff. The chain prints `verilex-claim-run-1`, and it is still a brain chain (F6 as a chain). |
| 23 | `apple`, the store locked for run 1 | `claim item-listed` inconclusive, `claim item-listed` green | green, run 2 | 0 | The green proves every claim that the inconclusive run selected, and covers the spec. |
| 24 | `store`, a fake verilex | run 1 inconclusive, asked for `item-listed` and selecting `store-opened`, `item-added` and `item-listed`, run 2 green on `store-opened` | inconclusive, no JSON | 2 | The green does not prove `item-listed` or `item-added`, which the inconclusive run selected. |
| 25 | `apple` | `claim item-listed` green | green, run 1 | 0 | The green covers the spec. Its words prove both floor claims. |
| 26 | `apple` | `claim item-listed` green, `claim store-opened` green | inconclusive, no JSON | 2 | The last green proves neither floor claim. An earlier green does not count, because the last run is the result. |
| 27 | `store`, `--claim item-listed` | `claim store-opened` green | inconclusive, no JSON | 2 | The green does not prove `item-listed`, a floor claim from `--claim`. |
| 28 | `nothing`, `--claim store-opened` | `claim store-opened` green | green, run 1 | 0 | `--claim` stands for an intent that names no claim. |
| 29 | `diff` | `claim store-opened` green | inconclusive, no JSON | 2 | The green was not asked about `bin/tally`. |
| 30 | `diff` | `verilex run --changed bin/tally` green | green, run 1 | 0 | The green covers the spec. It proves every claim that the diff touches, so verilex prints no warning. |
| 31 | `diff` | `claim store-opened + changed` green | green, run 1 | 0 | The green covers the spec. verilex's own warning, `2 touched claims not covered`, stays in the JSON. |
| 32 | `store`, `--diff HEAD` with a rename and a non-ASCII file name | `claim store-opened`, with `--changed` for each changed path in the prompt | green, run 1 | 0 | Both sides of the rename and the non-ASCII name reach the prompt and `requested.changed` (F3). |
| 33 | `store`, `diff` | `chain store-open + changed` green | green, run 1 | 0 | A brain chain with `--changed` is a claim run, and this one covers the spec. |
| 34 | `store` | `chain store-open` green | inconclusive, no JSON | 2 | A chain without `--changed` is not a claim run, so its JSON does not show what it was asked. |

Row 7 checks every file that git tracks or would track, and ignores the files that git ignores. A file counts as changed when it was added, removed, written or touched between the start of the brain and its end, even if its bytes were put back. Row 8 also matches a kept instance in the home. The launcher tears down every instance that it finds in the home.

A red or inconclusive verdict is returned as verilex printed it. Exit codes are verilex's: `0` green, `1` red, `2` inconclusive or refused. `--text` prints the verdict line and each gap instead of the JSON.

One deadline, the run spec's `time_budget`, covers the intent lookup, `--suggest` and the run. No child that the brain started holds the launcher past it. Each `verilex ticket` and `verilex index` command that the launcher runs for itself also stops after 30 seconds, and the launcher is then inconclusive.

The rule has these limits:

- The floor is only as good as the index's intent lookup. `verilex index --intent` returns up to five claims, best first, weak matches included, and the floor holds all of them. So on a large product, a brain that proves only the claims that it judged relevant can get inconclusive and must run more claims. This costs time, but it never gives a false green or a false red.
- The last run is the run that finished last. When the brain runs verilex in parallel, timing decides which run is last, so the same runs can give green or inconclusive. They cannot give a false green, because the last run must cover the spec alone.
- A red brain chain is not final. When a later green proves the claim of each red word, the result is green, even if only the chain's own inputs reach a defect. verilex proves a claim through its admitted words, so a defect that those words do not reach is outside the claim.

### Sandbox

The brain runs untrusted, in a sandbox: bubblewrap (`bwrap`) on Linux, `sandbox-exec` on macOS. In the sandbox, the brain:

- reads, and cannot write, the project, the system directories, the directories on its `PATH`, its own executable or install directory, the launcher's executable, the project's git directory when it lives elsewhere (a worktree), and its harness's credential files. On macOS it may also read the rest of the system outside `/Users`, `/Volumes`, the temporary directories and the user's home;
- writes only `<run>/brain`, which holds its `HOME` and its `TMPDIR` (`/tmp` on Linux);
- cannot reach the verilex home, the ledger, the real `verilex` binary (`--verilex`, and any other `verilex` on `PATH`), `VERILEX_HOME`, `VERILEX_LEDGER` or the run's logs. Each one is missing, or shows as an empty directory or as `/dev/null`;
- runs verilex only through `<run>/share/bin/verilex`, which sends each command to the launcher. The launcher runs it with its own home and ledger;
- reaches the network only through an egress proxy (`HTTPS_PROXY`, `HTTP_PROXY` and `ALL_PROXY`). The proxy serves `CONNECT` to public addresses only. It refuses loopback, private, link-local, CGNAT and reserved addresses, and the host's own addresses;
- on Linux, sees only its own processes and has no host network, IPC or abstract sockets. Every process it starts ends with the run;
- has no terminal. It runs in a session of its own with no controlling terminal, so it cannot type into the user's shell. On macOS the profile also denies every terminal device (`/dev/tty*`, `/dev/pty*` and `/dev/ptmx`), so the brain cannot read or write a terminal by its path. On Linux, `/dev` is the sandbox's own, with its own pseudo-terminals.

Before the brain starts, a helper inside the sandbox checks that the project refuses a new file, that each hidden path shows nothing, and that a port on the host's loopback is out of reach. When the sandbox tool is missing or fails, or a check fails, the brain does not run, and the launcher prints `verilex-agent: inconclusive: the brain runs only in a sandbox, and the sandbox is not available here: <reason>`. The launcher never runs a brain without the sandbox.

Linux needs `bwrap` and unprivileged user namespaces. Ubuntu 23.10 and later allow user namespaces only to a program that an AppArmor profile names. CI adds this profile:

```
printf 'abi <abi/4.0>,\ninclude <tunables/global>\nprofile bwrap /usr/bin/bwrap flags=(unconfined) {\n  userns,\n}\n' | sudo tee /etc/apparmor.d/bwrap
sudo apparmor_parser -r /etc/apparmor.d/bwrap
```

Containers often block user namespaces (Docker's default seccomp profile does), so run the launcher on the host or in a VM. macOS uses `/usr/bin/sandbox-exec`, which ships with the system. Other systems have no sandbox, so each run there is inconclusive.

Each run keeps its files in one directory, `$TMPDIR/verilex-agent-*`, or under `/tmp` when `$TMPDIR` is longer than 40 characters (unix socket paths are short):

```
verilex-agent-*/
  share/       prompt.txt, suggest.txt, bin/verilex   brain reads
  brain/       home/, tmp/                            brain writes
  log/         verify.out, verify.err, suggest.out, suggest.err,
               verilex.log, egress.log
  sock/        the socket behind bin/verilex
  sandbox/     per phase: egress.sock (Linux) or sandbox.sb (macOS)
  home/        the verilex home, when --home is not given
  ticket.yaml  the run spec
```

The launcher removes the directory at the end. With `--keep-work`, it keeps the directory and prints `verilex-agent: run files kept in <dir>`. `log/verilex.log` lists each verilex command the brain sent to the launcher, with its exit code or the refusal. `log/egress.log` lists each network request and whether the proxy refused it.

A built-in harness gets only its credential file, read-only, at the same place in its own home: `~/.claude/.credentials.json` for `claude` and `claude-code`, `~/.codex/auth.json` for `codex`, and `~/.pi/agent/auth.json` for `pi`. The user's other harness settings are not there. The built-in harnesses run with their own permission prompts and sandbox off (`--permission-mode bypassPermissions` for claude, `--dangerously-bypass-approvals-and-sandbox` for codex), because the launcher's sandbox is the boundary and a headless brain cannot answer a prompt. In a `--harnesses` file, a harness is an argv list or `{"argv": [...], "files": [...]}`. A `~/` file shows at the same place in the brain's home, and any other file at its own path.

The sandbox does not cover these cases:

- The brain gets the launcher's environment, except `HOME`, `TMPDIR`, the `XDG_*` directories, `NO_PROXY`, `VERILEX_HOME` and `VERILEX_LEDGER`. A secret in that environment reaches the brain, so start the launcher with only the variables the harness needs.
- The brain can reach any public address, so a service that the project publishes on a public address is reachable.
- On Linux, a unix socket inside a readable directory, such as the project, stays reachable.
- On macOS, a process that the brain detaches from its process group can outlive the run. The process keeps the sandbox's limits.
- On macOS, the brain cannot open a pseudo-terminal, so a harness that runs its commands in one fails there. No harness was run on macOS.
- The egress proxy serves only `CONNECT`. A client that sends plain HTTP requests to the proxy gets `405`.
- A harness cannot save a token that it refreshes during a run. If the provider rotates refresh tokens, log in again on the host when the harness reports an expired login.

The project check and the home check, rows 7 and 8 of the [verdict rule](#verdict-rule), stay as defense in depth. The brain can no longer cause either one, but they still catch a change that something outside the sandbox makes during the run, and a run that something starts around the launcher in its home.

## The word lifecycle

A word is **provisional** until it is admitted. A word that proves a claim is admitted by [onboarding](#onboarding); a word without a claim, by an outside curator (`propose` and `admit`). verilex never edits a verify skill and never calls a model: what only a reader can decide goes back to the calling agent as a decision request.

1. **Invent.** `verilex new item-renamed --implements verify-tally/features/items.md#item-add` writes `.verilex/words/item-renamed/` with a contract stub and a `run` that reports `blocked` until written, so its steps are `inconclusive`. Every `--implements` must resolve to a feature-map section, or `new` refuses. To make the word prove a claim instead, replace `implements` with the claim's `claim` pin and an `entry`. In a project without `.verilex/`, `new` also creates `config.yaml` and frame stubs (`launch`, `doctor`, `refresh`, `cleanup`) that exit 2, so every run is `inconclusive` until they are written. `refresh` brings a kept instance up to the current checkout, keeping its states; only `--continue` calls it.
2. **Use.** Provisional words run in chains like any other, always live. A use counts when the word's step was `green` or `red` in a live run whose verdict was `green` or `red`. Inconclusive steps, inconclusive runs and skipped runs do not count, and a run counts once however often the word appears in it.
3. **Onboard or propose.** A word that proves a claim joins through `verilex onboard <word>` (see [onboarding](#onboarding)); `propose` and `admit` refuse it. For a word without a claim, `verilex propose <word>` refuses a word with counted uses in fewer than two different runs. Otherwise it writes one self-contained JSON packet to `~/.local/state/verilex/<project>/proposals/<word>/<id>.json`: the word's files, its uses with verdicts and evidence paths, the text and hash of each feature-map section it implements, the whole dictionary with each word's status, and instructions for the curator.
4. **Admit.** For a proposed word, the curator writes a verdict file:

   ```json
   {"word": "item-renamed", "packet": "<id>", "verdict": "admit", "curator": "<model that judged>", "reason": "..."}
   ```

   `verilex admit <word> --verdict <file>` refuses a verdict whose packet is unknown, or whose word files or sections changed since the packet was built. Both verdicts are kept beside the packet. Only `admit` writes `.verilex/words/<word>/admission.json` (date, curator, packet, counted runs, word digest, and the hash of each implemented section); a rejected word stays as it was. The admission record is part of the word's stamp, so admitting a word runs its chains live once more before they can be skipped.
5. **Drift.** `verilex check` compares each admitted word with the project now. For a word that proves a claim, it checks the onboarding decision: its seal, the word digest, the fingerprints of the frame and the shared word files, the claim version it was onboarded for (and, through an alias, the grouped claim's version), the planted defects of its claim (and of the grouped claim) and the claim's review state. For a word without a claim, it checks the admission record's word digest, the fingerprints of the frame and the shared word files, and stored section hashes. A word runs with `.verilex/frame/` and with everything in `.verilex/words/` outside word directories (helpers words share) as well as its own files, so a change to either makes every admitted word drift-suspect: `the shared word files changed since onboarding (.verilex/words/tally_word.py)`. `config.yaml` is not part of it, because it names the product and one vocabulary serves several products. A decision or admission record written before verilex kept these fingerprints is drift-suspect too (`the frame and the shared word files were not recorded at onboarding; onboard it again`). Changed word files, a changed frame or shared word file, another claim version, a planted defect the word was never gated against, a decision edited by hand, a claim that needs review, or a changed or missing section makes the word **drift-suspect**: it always runs, and no earlier result is trusted for it, even though the verify skill is not part of its stamp. Drift is computed on every call, never cached. Onboard (or propose) a drift-suspect word again to re-admit it; a word that is drift-suspect only because its claim needs review stops being drift-suspect as soon as the claim is reviewed, and a re-map of the claim's sources makes the word drift-suspect until it is onboarded (or proposed and admitted) again.

`verilex gap 'A user renames an item'` writes a note to `.verilex/gaps/` for the verify skill's owner when the feature map has no section for a product moment.

A word without a claim names feature-map sections in `implements`. Such a reference is `<skill>/<file>#<section>`, looked up under the project's skill directories. The section is the heading whose slug matches, up to the next heading of the same or a higher level. A sub-feature id written as inline code in the file (`` `item-add` ``) selects the whole feature file. A claim's sources use the finer anchor described under [claims](#claims).

## Onboarding

`verilex onboard <word>` admits a word that proves a claim. It runs once per proposal, and it is the only writer of `.verilex/grouping.yaml`. Every check is mechanical, and verilex calls no model. A proposal those checks cannot settle goes back to the calling agent as a decision request:

```
$ verilex onboard item-put
onboarded item-put: variant of item-added@18e2db0cee8f through claim item-put-done, matched mechanically; caught dropped-add
  record: ~/.local/state/verilex/tally/onboarding/item-put/1791241493-2e3cc9
```

```
 two counted uses ─> mapping ─> non-proofs ─> mechanical match
                                                    │
        same / match / ambiguous / new <────────────┘
                     │
 trials: healthy + every planted defect, 4 at a time
                     │
 correctness gate ─> behavioral check ─> undecided (ambiguous only)
                     │                         │ decision request
                     │                    calling agent:
                     │                    --same-as or --distinct
                     │                         │
          .verilex/grouping.yaml (sealed decision)
```

1. **Usefulness.** The word needs counted uses (see [the word lifecycle](#the-word-lifecycle)) in at least two different runs of this product that proved the claim version it pins. Its claim must not need review, and must declare at least one planted defect.
2. **Mapping.** Each source must map the step that proves the claim. Most of the literal values a mapped sentence names (code spans, numbers, file names) must appear in the claim's evidence contract; a sentence without literals must share a term with the claim's sentence or evidence contract.
3. **Non-proofs.** A green use whose observation shows only one of the claim's declared non-proofs is rejected, and so is a claim whose own observation is only a non-proof of itself or of a claim it is matched against.
4. **Mechanical match.** The claim is compared with every claim the vocabulary groups. Their structure must be equal: preconditions, the word's entry point among the other claim's, the number of args, and `requires` and `provides` with placeholders compared by position. Then the literal values of the evidence contract and the sentence decide: both alike (the sentences share at least 80% of their terms) is a **match**, one of them is **ambiguous**, neither is **new**. A word that pins a grouped claim itself, or an alias of one, is the **same** claim. A decision places claims only while it holds: its word still exists and pins the claim version the decision judged, and every claim version it names is current. A new claim version is a new meaning, so an answer or match for the old version never carries over; the claim is matched, and if need be the agent is asked, again. When several holding decisions place one claim differently, the first in word order counts, so the outcome depends only on the records.
5. **Correctness gate.** Trials replay the chain of the word's latest counted use on fresh instances, four at a time, outside the run history and the ledger: on the healthy product and under each planted defect of the claim and of every claim it is matched against. The word must be green on the healthy product and red under each planted defect of its own claim. A false pass, a red healthy product, or a healthy observation that shows only a non-proof rejects it. A planted defect that stops the chain before the word runs, or any other inconclusive trial, makes the outcome inconclusive, never a rejection.
6. **Behavioral check.** For each claim it is matched against, up to three admitted words grouped under that claim replace the word in the same chain, on the same states. Each state where their verdicts differ is a **finding**. Any finding rejects a match: one of the words is wrong, or the claims differ. When no grouped word can be compared, the word is held to that claim's gate instead: green on the healthy product, red under its planted defects.
7. **Decision request.** An ambiguous proposal whose word behaved like the other claim's words on every state is **undecided**: only a reader can tell whether the two claims say the same. The word is onboarded with its claim as a claim of its own (`match: new`), the candidates stay open under `pending`, and onboarding prints the question and the two commands that answer it. Until the agent answers, the claims stay separate. Every later word that joins the undecided claim (the same claim, or a match of it) is also compared with the pending claim's words, and carries the same question if it behaved like them.

`onboard` exits `0` when the word is onboarded or undecided, `1` when it is rejected, and `2` when the outcome is inconclusive or the proposal is refused (`verilex: refused: ...` on stderr). It prints the outcome first, then only the trials that broke the gate and the findings, each with its evidence path, then the onboarding record: `~/.local/state/verilex/<project>/onboarding/<word>/<id>/`, which holds `record.json` (what `--json` prints) and each trial's run directory under `trials/`.

```
$ verilex onboard item-trusted
rejected item-trusted: item-trusted gives a false pass under planted defect dropped-add: store-open | item-trusted pear is green
  trial  dropped-add  store-open | item-trusted pear: green, expected red
    evidence: ~/.local/state/verilex/tally/onboarding/item-trusted/1791241042-c1aa48/trials/02-dropped-add-item-trusted/02-item-trusted
  record: ~/.local/state/verilex/tally/onboarding/item-trusted/1791241042-c1aa48
```

**Planted defects.** A claim's `defects` name product states in which the claim is false. Each sets environment variables that every frame step and word of a trial sees; the healthy trial unsets them all. They are not part of the claim's identity. A planted defect added or changed later makes every word gated against the claim drift-suspect until it is onboarded again, and the same holds for a word grouped under the claim through an alias.

**The grouping file.** `.verilex/grouping.yaml` holds one decision per onboarded word: the claim version it is grouped under (and the alias it pins, as `proves`), its entry point, the digest of its files, how it was grouped (`same`, `mechanical`, `agent` or `new`), the grouped claims still open for an undecided word (`pending`), the digest of each planted defect it caught, its counted runs per product, the chain it was proven on, the digest of its claim's sources as onboarding checked them, the commit of each source in another repository, the date and the onboarding record:

```yaml
words:
  item-put:
    claim: item-added@18e2db0cee8f
    proves: item-put-done@da5d25fdde24
    entry: cli
    digest: b17fbc7e0a211a5d77b71c10131b2508f56fe6a3811b5df4612e813eb4a0f14f
    match: mechanical
    defects:
      dropped-add: c71c4bbad861
    group_defects:
      dropped-add: c71c4bbad861
    uses:
      tally:
        - 1791241493-237424
        - 1791241493-d9fb88
    chain: store-open | item-put pear
    claim_sources: c030b0d0789a0f18af00ca5419ea262eea912535892b2e9ff45cc15d1cab06fc
    date: "2026-10-05T23:04:53Z"
    record: 1791241493-2e3cc9
    seal: 2dcaabd77c2bca3d949285f0679577dab17fb58e53508b30aec729f0364d62a7
```

Each decision carries a seal over its own fields. The seal is an unkeyed checksum, not a signature: it catches accidental and naive edits, not a deliberate forgery by someone who recomputes it. That is the same limit as a curator's `admission.json`. A decision edited by hand no longer matches it, so the word is drift-suspect (`its grouping decision does not match its seal: it was edited outside verilex onboard; onboard it again`) and nothing it was admitted for is trusted. Each decision is sealed on its own, so a merge that keeps the decisions of both branches keeps them valid. The seal is part of the word's stamp (`word admission`), so the first run after onboarding is live.

**Products.** Uses are counted per product, the `project` in `config.yaml`. The correctness gate holds for every product, so onboarding an admitted word in another product only checks its two counted uses there and records the product: `onboarded item-stored for tally-b: it proves item-added@18e2db0cee8f, and its correctness gate already holds`. Onboarding it again in a product that already has it changes nothing.

**Deciding an undecided word.** The decision request names the claim the word is grouped under, each candidate claim with its text and the words the behavioral check compared, and the commands that answer it; `--json` prints it as `request` (`question`, `proposal`, `candidates`, `commands`). The calling agent reads both claims and answers once:

```
$ verilex onboard item-keep
undecided item-keep: claim item-kept@836c8b50590b joins the vocabulary as a claim of its own for now; caught dropped-add
  decide: does claim item-kept say the same as item-added@18e2db0cee8f (compared: item-filed, item-stored)? item-keep behaved like them on every trial state.
    verilex onboard item-keep --same-as item-added
    verilex onboard item-keep --distinct
  record: ~/.local/state/verilex/tally/onboarding/item-keep/1791289822-ac1b2c

$ verilex onboard item-keep --same-as item-added
onboarded item-keep: variant of item-added@18e2db0cee8f through claim item-kept, as the agent decided
```

The answer is about the claim, not one word, so it regroups every word grouped under that claim. `--same-as <claim>` moves them all under that candidate (`match: agent`) and records the candidate's planted defects, which each word already behaved like its words under, as `group_defects`; it is refused while one of them was never compared with the candidate or behaved differently from its words. A word moved under a claim whose own question is still open keeps the pending claims it was compared with, so that claim's later answer moves it too. The answer is checked and applied under the grouping file's lock, so of two answers at once only one applies, and the other is refused. `--distinct` keeps the claim a claim of its own and closes the question for all of them. Either answer runs no trial: it seals the decision onboarding already gated. It is refused when the word has no open question, names a claim the question does not offer, or is no longer admitted because its files or claim changed; a candidate that moved to a new version can no longer be named, so the agent keeps the word apart or onboards it again once its files change.

## The index

`verilex index` is generated on every call from the claims, the words and the grouping file. It calls no model. Nothing is cached or stored, so the same records always give the same index. It has three tiers, so an agent reads only what it needs:

```
$ verilex index
index tally: 3 active claim(s); `verilex index <claim>` lists a claim's words
item-added  A named item is in the store.
item-listed  A stored item shows up when a user lists the store.
store-opened  A new, empty store is open and ready for items.

$ verilex index item-added
item-added@18e2db0cee8f  A named item is in the store.
  entry: cli  requires: store  provides: item:{name}
  item-filed <name>
  item-put <name>  (via item-put-done)
  item-stored <name>

$ verilex index item-added item-stored
item-stored <name>  proves item-added@18e2db0cee8f through cli; admitted
  requires: store  provides: item:{name}
  inputs: bin/tally  env: TALLY_DEFECT, TALLY_SIMULATE_LOCK  timeout: 1800s
  proven on: store-open | item-stored apple
```

1. **Active claims.** A claim is active for a product when a word grouped under it is onboarded for that product, at the claim versions it pins now. A word whose claim moved to a new version, or that is grouped under an old version, is provisional until it is onboarded again. Variants, aliases and words other products use never grow this tier.
2. **A claim's words.** The words grouped under the claim, with the alias each pins. When the claim has words onboarded for this product, only those are listed; otherwise every word is, each marked `dormant` (onboarded only for other products) or `provisional`. An alias claim name finds its grouped claim.
3. **One word.** What a run needs: its args, entry point, states, inputs, environment, timeout, lifecycle status and the chain onboarding proved it on.

`--intent '<text>'` finds up to five claims an intent names, best first. It compares the intent's terms with each claim's name, sentence, evidence, aliases, sources and words; a term counts double in the name or sentence, and a rarer term counts more. `--changed <file>` (repeatable) lists the claims whose dependencies cover the touched files, and every known chain that holds one of their words: the chains onboarding proved them on and those this product ran. For each chain it gives the decision `verilex plan` makes, through the same code: run (with the first reason), skip, or refused.

```
$ verilex index --changed .verilex/words/item-listed/run
changed: 1 claim(s); 1 of 1 known chain(s) must re-run
item-listed  A stored item shows up when a user lists the store.
  run  store-open | item-stored apple | item-listed apple: item-listed apple: word changed
```

A word depends on its own directory, its claim file, the feature files its claim's sources (or its `implements`) point at, its `inputs`, declared `depends` (paths, config keys, image pins, runbook refs), and what every word shares: `config.yaml`, the frame, the files beside the word directories and the grouping file. `--changed` accepts a path or `config:<key>`, `image:<pin>` or `runbook:<ref>`.

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
  words/<word>/admission.json  written by `verilex admit` for a word without a claim
  claims/<claim>.yaml  a claim (see Claims)
  profiles.yaml        run ticket defaults and named profiles, optional (see Run tickets)
  grouping.yaml        written only by `verilex onboard` (see Onboarding)
  gaps/                notes from `verilex gap` for the verify skill's owner
```

Frame steps and words receive these environment variables: `VERILEX_RUN` (the id of the run that launched the instance, which a continuing run keeps; label everything you launch with it), `VERILEX_INSTANCE` (the launch's `instance`, as JSON; `null` if launch failed), `VERILEX_PROJECT_ROOT` and `VERILEX_EVIDENCE` (this step's evidence directory).

A word's contract:

```yaml
---
word: item-stored
promise: A named item is in the store.
args: [name]
claim: item-added@18e2db0cee8f   # the claim version it proves, as `verilex claims` prints it
entry: cli               # the entry point it exercises, one of its claim's
requires: []             # states it needs beyond its claim's, optional
provides: []             # states it makes true beyond its claim's, optional
inputs: [bin/tally]      # product paths the result depends on; omit or leave empty to never skip
env: [TALLY_DEFECT]      # environment variables the result depends on, optional
depends:                 # optional diff footprint beyond inputs and claim sources
  paths: []              # more product paths; fingerprinted, so a change cannot skip
  config_keys: []        # a diff entry config:<key> touches this word; not in the proof stamp, so a hit runs live
  images: []             # image pins; image:<pin> likewise runs live
  runbook: []            # extra runbook refs; claim sources already count, and their hashes are in the stamp
timeout: 1800            # seconds, optional
read_only: false         # optional; true when the word only observes the instance (it then provides no states)
---
```

A word without a claim lists the feature-map sections it implements instead (`implements: [verify-tally/features/items.md#item-add]`) and declares all its states itself.

`run` receives its arguments on the command line and a state document on stdin (`run`, the same id as `VERILEX_RUN`; `instance`, `states`, `args`, `evidence`). It writes its action and observation files into its evidence directory and prints one result object on stdout, exiting `0`, `1` or `2` to match:

```json
{"verdict": "pass", "observation": "store.json lists apple"}
{"verdict": "fail", "preconditions_held": true, "detail": "tally said 'added apple' but store.json lacks apple"}
{"verdict": "blocked", "detail": "the store is locked by another process"}
```

`tests/fixtures/tally/` is a complete example: a made-up inventory CLI, its verification skill, and its `.verilex/` directory with claims and the words that prove them.

## Development

```
go test ./...
go vet ./...
go build -o verilex ./cmd/verilex
```

The Go behavior tests drive the compiled CLI against tally. The sample product and its words
use Python 3, so tests require `python3` and a POSIX shell. The verilex core does not require Python.
The launcher's tests (`cd agent && go test ./...`) run each brain in the [sandbox](#sandbox), so they
also need `bwrap` on Linux or `sandbox-exec` on macOS.

`scripts/ci` runs the whole blocking set that CI runs. Its `canary` step has verilex prove its own
claims: the repository's `.verilex/` holds words that build the verilex under test from the checkout and drive it
on a scratch tally, each claim anchored on a `verify-verilex` sub-feature. A known-good build pinned in
`scripts/ci` (`canary_driver`) runs them live in throwaway state, never the build under test, and the step fails
on a red or inconclusive run and on drift. Onboard a changed canary word with that pinned build.
Its `canary-guards` step (`scripts/canary-guards`) replays attacks that once passed the canary step on scratch
copies of the checkout, and each must now fail it.
