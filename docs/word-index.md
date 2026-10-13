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

`--intent '<text>'` finds up to five claims an intent names, best first. It compares the intent's terms with each claim's name, sentence, evidence, aliases, sources and words; a term counts double in the name or sentence, or in an alias claim's name or sentence, and a rarer term counts more. A claim is returned only when it covers at least half of the intent's terms, a term in its name or sentence counting whole and one elsewhere in the claim half. A two-term intent that shares only one term with a claim must name the claim with it: the term must be all of what sets one of the claim's names apart, its name terms that are in fewer than all claims. So `list works` finds `item-listed`, but `store refunds` finds nothing, because every claim mentions the store, and `refused payment` does not find `chain-order-refused`, which `order` and `refused` together set apart. This is a lexical filter, not a meaning check: a legitimate paraphrase with too few shared terms can still find no claim, and `list refunds` finds `item-listed` just as `list works` does. `--changed <file>` (repeatable) lists the claims whose dependencies cover the touched files, and every known chain that holds one of their words: the chains onboarding proved them on and those this product ran. For each chain it gives the decision `verilex plan` makes, through the same code: run (with the first reason), skip, or refused.

```
$ verilex index --changed .verilex/words/item-listed/run
changed: 1 claim(s); 1 of 1 known chain(s) must re-run
item-listed  A stored item shows up when a user lists the store.
  run  store-open | item-stored apple | item-listed apple: item-listed apple: word changed
```

## Doc anchors

A doc passage can say which claims it describes, so a diff names the passages it could have made false. An anchor is a line of its own in a markdown file under `docs/` (a regular file, not a symlink), outside fenced and indented code blocks, where it is an example:

```markdown
## When a chain is skipped
<!-- claims: chain-skipped -->
```

It ties the passage it sits in (the file's section under the nearest heading above it, up to the next heading) to the claims it names, separated by spaces. It names claims, not line numbers, so it survives a refactor, and as an HTML comment it renders as nothing. `--changed` reads every anchor on each call: a code change also lists, as `doc`, the passages tied to the claims it reports, and a doc change lists, as `names`, each passage of that doc with the claims it names. Anchors add no dependency, so a doc change re-runs nothing it did not before. When a doc and the code disagree, the code wins: fix the passage, or prove the claim again.

```
$ verilex index --changed docs/skipping.md
changed: 0 claim(s); 0 of 0 known chain(s) must re-run; the changed docs name 1 claim(s)
  names  docs/skipping.md: When a chain is skipped  (chain-skipped)
```

An anchor that names a claim that does not exist, that sits above the file's first heading, or that names no claim is refused, with the file and line: `verilex: refused: docs/guide.md:5: the anchor names claim item-addded, which does not exist; ...`.

## What a word depends on

A word depends on its own directory, its claim file, the feature files its claim's sources (or its `implements`) point at, its `inputs`, declared `depends` (paths, config keys, image pins, runbook refs), and what every word shares: `config.yaml`, the frame, the files beside the word directories and the grouping file. `--changed` accepts a path or `config:<key>`, `image:<pin>` or `runbook:<ref>`.
