# The word index

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

## The three tiers

1. **Active claims.** A claim is active for a product when a word grouped under it is onboarded for that product, at the claim versions it pins now. A word whose claim moved to a new version, or that is grouped under an old version, is provisional until it is onboarded again. Variants, aliases and words other products use never grow this tier.
2. **A claim's words.** The words grouped under the claim, with the alias each pins. When the claim has words onboarded for this product, only those are listed; otherwise every word is, each marked `dormant` (onboarded only for other products) or `provisional`. An alias claim name finds its grouped claim.
3. **One word.** What a run needs: its args, entry point, states, inputs, environment, timeout, lifecycle status and the chain onboarding proved it on.

## Intent and diff lookups

`--intent '<text>'` finds up to five claims an intent names, best first. It compares the intent's terms with each claim's name, sentence, evidence, aliases, sources and words; a term counts double in the name or sentence, and a rarer term counts more. `--changed <file>` (repeatable) lists the claims whose dependencies cover the touched files, and every known chain that holds one of their words: the chains onboarding proved them on and those this product ran. For each chain it gives the decision `verilex plan` makes, through the same code: run (with the first reason), skip, or refused.

```
$ verilex index --changed .verilex/words/item-listed/run
changed: 1 claim(s); 1 of 1 known chain(s) must re-run
item-listed  A stored item shows up when a user lists the store.
  run  store-open | item-stored apple | item-listed apple: item-listed apple: word changed
```

## What a word depends on

A word depends on its own directory, its claim file, the feature files its claim's sources (or its `implements`) point at, its `inputs`, declared `depends` (paths, config keys, image pins, runbook refs), and what every word shares: `config.yaml`, the frame, the files beside the word directories and the grouping file. `--changed` accepts a path or `config:<key>`, `image:<pin>` or `runbook:<ref>`.
