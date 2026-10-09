# Getting started

This page installs verilex and walks through one session on tally, the sample product in this repository: a made-up inventory CLI with its own words, frame and verify skill. Every output below is what verilex printed for these commands. Your run ids and paths will differ.

## Install

```
go install github.com/DereKk8/verilex/cmd/verilex@latest
```

Use Go 1.27.1 or newer on a Unix system. Add `$(go env GOPATH)/bin` to your `PATH`.
The core is Go; frame steps and words can use any language installed on the product's workstation.

## Get a product to verify

verilex runs inside a product checkout that has a `.verilex/` directory. Copy tally somewhere disposable. Its frame and words use Python 3.

```
git clone https://github.com/DereKk8/verilex
cp -r verilex/tests/fixtures/tally /tmp/tally
cd /tmp/tally
```

## Read the dictionary

`verilex words` lists every word with its promise, the claim it proves, its states and its lifecycle status:

```
$ verilex words
item-listed name
  promise:  A stored item shows up when a user lists the store.
  claim:    item-listed@b8fa4e44bc73 via cli
  requires: item:{name}  provides: -
  status:   provisional
item-stored name
  promise:  A named item is in the store.
  claim:    item-added@18e2db0cee8f via cli
  requires: store  provides: item:{name}
  status:   provisional
store-open
  promise:  A new, empty store is open and ready for items.
  claim:    store-opened@828f801851c1 via cli
  requires: -  provides: store
  status:   provisional
```

`requires` and `provides` encode the product's order of reality: `item-stored` needs a `store`, which `store-open` provides. Each word proves a claim, pinned at a version. See [Core concepts](concepts.md) for the vocabulary.

## Plan, then run

`verilex plan` makes the same skip decision `verilex run` would, and runs nothing. Nothing has run yet, so the whole chain must run live:

```
$ verilex plan 'store-open | item-stored apple | item-listed apple'
plan: skip 0, run 3; store-open: no green result on record
```

Run it. verilex launches an instance owned by this run, asks the doctor to vouch for it, runs each word, records evidence and cleans up:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
green: 3 green; run 1791503104-22eb1c4bc654
```

The verdict comes first and green steps are only counted. Exit code `0`.

## See a red

tally takes planted defects through `TALLY_DEFECT`. Under `drop-adds`, `tally add` reports success without storing the item:

```
$ TALLY_DEFECT=drop-adds verilex run 'store-open | item-stored apple | item-listed apple'
red: 1 green, 1 red, 1 not run; run 1791503105-d713d0012bbc
  red  item-stored apple: tally said 'added apple' but store.json lacks apple
    evidence: /home/you/.local/state/verilex/tally/runs/1791503105-d713d0012bbc/02-item-stored
    verify skill: verify-tally/features/items.md#item-add
```

Exit code `1`. Only the step that is not green is printed, with a one-line cause, the path to its evidence and the verify-skill entry behind it, so an agent can continue by hand. See [Verdicts](verdicts.md).

## See a refusal

A chain that breaks the order of reality is refused before anything starts:

```
$ verilex run 'item-stored apple | store-open'
verilex: refused: item-stored apple requires store, pinned by claim item-added; nothing earlier provides it
```

Exit code `2`. Nothing was launched and no run was recorded. See [The chain grammar](chains.md).

## Onboard the words

The plan after the first green run names why nothing is skipped yet:

```
$ verilex plan 'store-open | item-stored apple | item-listed apple'
plan: skip 0, run 3; store-open: provisional; only admitted words are skipped
```

A word is provisional until it is admitted. Run the chain once more, so each word has counted uses in two different runs, then onboard each word. Onboarding replays the chain on fresh instances and gates the word on its claim's planted defects:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
green: 3 green; run 1791503105-6feb72b2ae70

$ verilex onboard store-open
onboarded store-open: claim store-opened@828f801851c1 joins the vocabulary; caught dirty-open
  record: /home/you/.local/state/verilex/tally/onboarding/store-open/1791503106-d4399c

$ verilex onboard item-stored
onboarded item-stored: claim item-added@18e2db0cee8f joins the vocabulary; caught dropped-add
  record: /home/you/.local/state/verilex/tally/onboarding/item-stored/1791503106-1c7775

$ verilex onboard item-listed
onboarded item-listed: claim item-listed@b8fa4e44bc73 joins the vocabulary; caught hidden-list
  record: /home/you/.local/state/verilex/tally/onboarding/item-listed/1791503106-6f1d7c
```

See [Onboarding](onboarding.md) for every check it runs.

## Skip what is already proven

The onboarding decision is part of each word's proof stamp, so the first run after onboarding runs live. After that, the plan cites the run each step relies on, and the run launches nothing:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
green: 3 green; run 1791503107-17d0dc90a0a2

$ verilex plan 'store-open | item-stored apple | item-listed apple'
plan: skip 3, run 0
  skip  store-open  relies on run 1791503107-17d0dc90a0a2
  skip  item-stored apple  relies on run 1791503107-17d0dc90a0a2
  skip  item-listed apple  relies on run 1791503107-17d0dc90a0a2

$ verilex run 'store-open | item-stored apple | item-listed apple'
green: 3 green, skipped: stamps match run 1791503107-17d0dc90a0a2; run 1791503107-87c51fb6e590
```

Change a word, its claim, the frame, an input or verilex itself, and the stamp no longer matches: the chain runs live again. See [Proof stamps and skipping](skipping.md).

## Look back

```
$ verilex runs
1791503104-22eb1c4bc654  green  cleanup=done  store-open | item-stored apple | item-listed apple
1791503105-6feb72b2ae70  green  cleanup=done  store-open | item-stored apple | item-listed apple
1791503105-d713d0012bbc  red  cleanup=done  store-open | item-stored apple | item-listed apple
1791503107-17d0dc90a0a2  green  cleanup=done  store-open | item-stored apple | item-listed apple
1791503107-87c51fb6e590  green  cleanup=none  store-open | item-stored apple | item-listed apple

$ verilex check
check: no drift (3 admitted)
```

The skipped run launched no instance, so it had nothing to clean up.

## Next

- [Core concepts](concepts.md): the vocabulary in one page.
- [Commands](commands.md): every command and flag.
- [Adding verilex to a project](adding-verilex.md): write a frame, words and claims for your own product.
