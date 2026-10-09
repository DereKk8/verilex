# Proof stamps and skipping

## What a stamp covers

Every word result carries a proof stamp: a fingerprint of everything the result depended on.

- the word's own directory (`word.md`, `run`, its `admission.json` and anything beside them, with permissions);
- for a word that proves a claim, the seal of its onboarding decision (`word admission`), so onboarding runs its chains live once more;
- the fingerprint of the claim version the word proves, so a pass is evidence only for that version, and the claim's sources, so the first run after a re-map is live;
- everything in `.verilex/words/` outside word directories (helpers words share), `.verilex/config.yaml` and `.verilex/frame/`;
- the paths in the word's `inputs` and the values of the variables in its `env`;
- the verilex executable;
- the stamp of the step before it, so a change anywhere upstream reaches every later word.

## When a chain is skipped

After a run that is not inconclusive, verilex records each green word as a pass in [the ledger](#the-shared-ledger), keyed by the chain prefix that ends in that word. A later `verilex run` skips the chain only when every word is admitted (see [the word lifecycle](word-lifecycle.md)) and a pass stands for every word's stamp: it proved the claim version the word pins, its evidence still exists and meets the evidence contract, and it was recorded less than 7 days ago. A skipped run launches nothing and names the run it relies on:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
green: 3 green, skipped: stamps match run 1767225600-a1b2c3d4e5f6; run 1767225900-d4e5f6a7b8c9
```

Anything missing or unclear runs the chain live: a word without `inputs` or with an empty `inputs` list, an input that is missing, an unreadable ledger, a pass that does not stand (see [the shared ledger](#the-shared-ledger)), evidence that is gone, a green result 7 days old or older, a stamp that changed while the run was going. A **provisional** or **drift-suspect** word, or one whose admission record or grouping file cannot be read, also runs the chain live, however well its stamp matches: its results are still recorded, but none is trusted until the word is admitted and unchanged. Skipping is all or nothing, because each run starts from a fresh instance: a word that has to run needs the effects of every word before it, and every word after it depends on its new result. `--keep` and `--fresh` always run live. `--json` names the first reason a chain ran live in `rerun`, for example `item-listed apple: provisional; only admitted words are skipped`.

A stamp covers only what it lists. A word that reads anything else (another file, a service, a tool's version) must declare it in `inputs` or `env`, or leave `inputs` out (or empty) so it is never skipped.

## The shared ledger

The ledger is a directory of passes that every verilex instance pointed at it reads and writes. By default it is `~/.local/state/verilex/<project>/ledger/` (under `VERILEX_HOME`). With `VERILEX_LEDGER` set, it is `$VERILEX_LEDGER/<project>/`. Point stateless instances, each with a state home of its own, at one shared directory, and they keep the skip savings: a pass one instance records skips the same chain on every other. [Where the ledger lives](ledger-placement.md) records why the ledger is a directory and not a single in-repo file or a service, with measurements.

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
