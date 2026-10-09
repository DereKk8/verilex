# Adding verilex to a project

## The .verilex directory

```tree
.verilex/
  config.yaml        `project: <name>`; optional `secret_patterns: [<regex>, ...]`; optional `skills: [<dir>, ...]` (default `.cursor/skills`, `.claude/skills`, `.agents/skills`)
  frame/
    launch           prints `{"instance": <anything>}` on stdout
    doctor           exits 0 when the instance is this run's and healthy
    refresh          brings a kept instance up to the current checkout, keeping its states; needed only by `verilex run --continue`
    cleanup          removes what this run launched
  words/
    <word>/
      word.md        contract (YAML frontmatter) and a short description
      run            the executable word
      admission.json  written by `verilex admit` for a word without a claim
  claims/
    <claim>.yaml     a claim (see Claims)
  profiles.yaml      run ticket defaults and named profiles, optional (see Run tickets)
  grouping.yaml      written only by `verilex onboard` (see Onboarding)
  gaps/              notes from `verilex gap` for the verify skill's owner
```

## Environment

Frame steps and words receive these environment variables: `VERILEX_RUN` (the id of the run that launched the instance, which a continuing run keeps; label everything you launch with it), `VERILEX_INSTANCE` (the launch's `instance`, as JSON; `null` if launch failed), `VERILEX_PROJECT_ROOT` and `VERILEX_EVIDENCE` (this step's evidence directory).

## The word contract

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

## The run program

`run` receives its arguments on the command line and a state document on stdin (`run`, the same id as `VERILEX_RUN`; `instance`, `states`, `args`, `evidence`). It writes its action and observation files into its evidence directory and prints one result object on stdout, exiting `0`, `1` or `2` to match:

```json
{"verdict": "pass", "observation": "store.json lists apple"}
{"verdict": "fail", "preconditions_held": true, "detail": "tally said 'added apple' but store.json lacks apple"}
{"verdict": "blocked", "detail": "the store is locked by another process"}
```

## A complete example

`tests/fixtures/tally/` is a complete example: a made-up inventory CLI, its verification skill, and its `.verilex/` directory with claims and the words that prove them.
