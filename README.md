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
| `verilex plan '<chain>' [--continue <run>] [--ticket <file>] [--json]` | Runs nothing; prints whether the chain would be skipped or run live, the run each skipped step relies on, and the first reason it must run live |
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
| `verilex index --changed <file>... [--json]` | Lists the claims touched files affect, and for each known chain whether `plan` would run it |
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
5. **Drift.** `verilex check` compares each admitted word with the project now. For a word that proves a claim, it checks the onboarding decision: its seal, the word digest, the claim version it was onboarded for (and, through an alias, the grouped claim's version), the planted defects of its claim (and of the grouped claim) and the claim's review state. For a word without a claim, it checks the admission record's word digest and stored section hashes. Changed word files, another claim version, a planted defect the word was never gated against, a decision edited by hand, a claim that needs review, or a changed or missing section makes the word **drift-suspect**: it always runs, and no earlier result is trusted for it, even though the verify skill is not part of its stamp. Drift is computed on every call, never cached. Onboard (or propose) a drift-suspect word again to re-admit it; a word that is drift-suspect only because its claim needs review stops being drift-suspect as soon as the claim is reviewed, and a re-map of the claim's sources makes the word drift-suspect until it is onboarded (or proposed and admitted) again.

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
4. **Mechanical match.** The claim is compared with every claim the vocabulary groups. Their structure must be equal: preconditions, the word's entry point among the other claim's, the number of args, and `requires` and `provides` with placeholders compared by position. Then the literal values of the evidence contract and the sentence decide: both alike (the sentences share at least 80% of their terms) is a **match**, one of them is **ambiguous**, neither is **new**. A word that pins a grouped claim itself, or an alias of one, is the **same** claim.
5. **Correctness gate.** Trials replay the chain of the word's latest counted use on fresh instances, four at a time, outside the run history and the ledger: on the healthy product and under each planted defect of the claim and of every claim it is matched against. The word must be green on the healthy product and red under each planted defect of its own claim. A false pass, a red healthy product, or a healthy observation that shows only a non-proof rejects it. A planted defect that stops the chain before the word runs, or any other inconclusive trial, makes the outcome inconclusive, never a rejection.
6. **Behavioral check.** For each claim it is matched against, up to three admitted words grouped under that claim replace the word in the same chain, on the same states. Each state where their verdicts differ is a **finding**. Any finding rejects a match: one of the words is wrong, or the claims differ. When no grouped word can be compared, the word is held to that claim's gate instead: green on the healthy product, red under its planted defects.
7. **Decision request.** An ambiguous proposal whose word behaved like the other claim's words on every state is **undecided**: only a reader can tell whether the two claims say the same. The word is onboarded with its claim as a claim of its own (`match: new`), the candidates stay open under `pending`, and onboarding prints the question and the two commands that answer it. Until the agent answers, the claims stay separate.

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

Each decision carries a seal over its own fields. A decision edited by hand no longer matches it, so the word is drift-suspect (`its grouping decision does not match its seal: it was edited outside verilex onboard; onboard it again`) and nothing it was admitted for is trusted. Each decision is sealed on its own, so a merge that keeps the decisions of both branches keeps them valid. The seal is part of the word's stamp (`word admission`), so the first run after onboarding is live.

**Products.** Uses are counted per product, the `project` in `config.yaml`. The correctness gate holds for every product, so onboarding an admitted word in another product only checks its two counted uses there and records the product: `onboarded item-stored for tally-b: it proves item-added@18e2db0cee8f, and its correctness gate already holds`. Onboarding it again in a product that already has it changes nothing.

**Deciding an undecided word.** The decision request names the word's claim, each candidate claim with its text and words, and the commands that answer it; `--json` prints it as `request` (`question`, `proposal`, `candidates`, `commands`). The calling agent reads both claims and answers once:

```
$ verilex onboard item-keep
undecided item-keep: claim item-kept@836c8b50590b joins the vocabulary as a claim of its own for now; caught dropped-add
  decide: does claim item-kept say the same as item-added@18e2db0cee8f (words: item-filed, item-stored)? It behaved like their words on every trial state.
    verilex onboard item-keep --same-as item-added
    verilex onboard item-keep --distinct
  record: ~/.local/state/verilex/tally/onboarding/item-keep/1791289822-ac1b2c

$ verilex onboard item-keep --same-as item-added
onboarded item-keep: variant of item-added@18e2db0cee8f through claim item-kept, as the agent decided
```

`--same-as <claim>` groups the word under that candidate (`match: agent`) and records the candidate's planted defects, which the word already behaved like its words under, as `group_defects`. `--distinct` keeps its claim a claim of its own and closes the question. Either answer runs no trial: it seals the decision onboarding already gated. It is refused when the word has no open question, names a claim the question does not offer, or is no longer admitted because its files or claim changed; a candidate that moved to a new version can no longer be named, so the agent keeps the word apart or onboards it again once its files change.

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

1. **Active claims.** A claim is active for a product when a word grouped under it is onboarded for that product. Variants, aliases and words other products use never grow this tier.
2. **A claim's words.** The words grouped under the claim, with the alias each pins. When the claim has words onboarded for this product, only those are listed; otherwise every word is, each marked `dormant` (onboarded only for other products) or `provisional`. An alias claim name finds its grouped claim.
3. **One word.** What a run needs: its args, entry point, states, inputs, environment, timeout, lifecycle status and the chain onboarding proved it on.

`--intent '<text>'` finds up to five claims an intent names, best first. It compares the intent's terms with each claim's name, sentence, evidence, aliases, sources and words; a term counts double in the name or sentence, and a rarer term counts more. `--changed <file>` (repeatable) lists the claims whose dependencies cover the touched files, and every known chain that holds one of their words: the chains onboarding proved them on and those this product ran. For each chain it gives the decision `verilex plan` makes, through the same code: run (with the first reason), skip, or refused.

```
$ verilex index --changed .verilex/words/item-listed/run
changed: 1 claim(s); 1 of 1 known chain(s) must re-run
item-listed  A stored item shows up when a user lists the store.
  run  store-open | item-stored apple | item-listed apple: item-listed apple: word changed
```

A word depends on its own directory, its claim file, the feature files its claim's sources (or its `implements`) point at, its `inputs`, and what every word shares: `config.yaml`, the frame, the files beside the word directories and the grouping file.

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
