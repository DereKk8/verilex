# Claims

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

## Identity and versions

A claim's fingerprint covers its sentence, args, entry points, preconditions, `requires`, `provides` and evidence contract, not its planted defects or sources. Whitespace, key order and the order of list entries never change it. Any other change to them is a new version. `verilex claims` prints each claim as `<claim>@<version>`, the version being the first 12 hex digits of the fingerprint.

## Words pin a version

A word proves a claim by pinning a version in its contract (`claim: item-added@18e2db0cee8f`) and naming the entry point it exercises (`entry: cli`). The claim's fingerprint is part of the word's proof stamp, so a pass is evidence only for the version it ran against, and a pass through one entry point never stands for another. When a claim changes, a chain that holds a word pinned to the old version is refused before anything starts:

```
$ verilex run 'store-open | item-stored apple | item-listed apple'
verilex: refused: item-stored pins claim item-added@18e2db0cee8f, which is now item-added@27d4aafc09fd; a pass proves only the version it ran against: check that item-stored still proves the claim, then pin item-added@27d4aafc09fd
```

Re-pinning changes the word, so it runs live (`item-stored apple: claim changed`) and is onboarded again; only uses that proved the new version count.

## Pinned rules

The claim's `requires` and `provides` join every word's own, so a variant cannot drop the order of reality, and a chain that breaks it is refused before it starts:

```
$ verilex run 'item-put apple | store-open'
verilex: refused: item-put apple requires store, pinned by claim item-added; nothing earlier provides it
```

A word that proves a claim must also take every arg the claim uses, name one of its entry points, leave out `implements` (its sources come from the claim), and not be `read_only` when the claim provides states. Otherwise the dictionary refuses to load.

## Sources and review

A source anchors the claim on a stable sub-feature id and the requirement sentences the claim maps to, not on a whole file or section. A sub-feature is the text its id names in the feature file: every list item, paragraph or table row that opens with the id as inline code, with the lines nested under it and any fenced block that follows it with only blank lines between, and the section under a heading whose anchor is the id. A mention of the id anywhere else names nothing. So a feature file labels each step with the sub-feature it drives:

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

## Sources in another repository

When the verify skill lives in another repository, a source names a checkout of it with `repo:` (a path relative to the project root, or absolute), and its `ref` is looked up under that checkout's default skill directories. Onboarding records the repository (its `origin` URL, or the path when it has none) and the commit it checked the mapping against, and refuses while the source's feature file has uncommitted changes there, so the recorded commit holds what was checked. A source in another repository never covers a local sub-feature with the same reference.
