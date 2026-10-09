# Onboarding

`verilex onboard <word>` admits a word that proves a claim. It runs once per proposal, and it is the only writer of `.verilex/grouping.yaml`. Every check is mechanical, and verilex calls no model. A proposal those checks cannot settle goes back to the calling agent as a decision request:

```
$ verilex onboard item-put
onboarded item-put: variant of item-added@18e2db0cee8f through claim item-put-done, matched mechanically; caught dropped-add
  record: ~/.local/state/verilex/tally/onboarding/item-put/1791241493-2e3cc9
```

## The checks

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

1. **Usefulness.** The word needs counted uses (see [the word lifecycle](word-lifecycle.md)) in at least two different runs of this product that proved the claim version it pins. Its claim must not need review, and must declare at least one planted defect.
2. **Mapping.** Each source must map the step that proves the claim. Most of the literal values a mapped sentence names (code spans, numbers, file names) must appear in the claim's evidence contract; a sentence without literals must share a term with the claim's sentence or evidence contract.
3. **Non-proofs.** A green use whose observation shows only one of the claim's declared non-proofs is rejected, and so is a claim whose own observation is only a non-proof of itself or of a claim it is matched against.
4. **Mechanical match.** The claim is compared with every claim the vocabulary groups. Their structure must be equal: preconditions, the word's entry point among the other claim's, the number of args, and `requires` and `provides` with placeholders compared by position. Then the literal values of the evidence contract and the sentence decide: both alike (the sentences share at least 80% of their terms) is a **match**, one of them is **ambiguous**, neither is **new**. A word that pins a grouped claim itself, or an alias of one, is the **same** claim. A decision places claims only while it holds: its word still exists and pins the claim version the decision judged, and every claim version it names is current. A new claim version is a new meaning, so an answer or match for the old version never carries over; the claim is matched, and if need be the agent is asked, again. When several holding decisions place one claim differently, the first in word order counts, so the outcome depends only on the records.
5. **Correctness gate.** Trials replay the chain of the word's latest counted use on fresh instances, four at a time, outside the run history and the ledger: on the healthy product and under each planted defect of the claim and of every claim it is matched against. The word must be green on the healthy product and red under each planted defect of its own claim. A false pass, a red healthy product, or a healthy observation that shows only a non-proof rejects it. A planted defect that stops the chain before the word runs, or any other inconclusive trial, makes the outcome inconclusive, never a rejection.
6. **Behavioral check.** For each claim it is matched against, up to three admitted words grouped under that claim replace the word in the same chain, on the same states. Each state where their verdicts differ is a **finding**. Any finding rejects a match: one of the words is wrong, or the claims differ. When no grouped word can be compared, the word is held to that claim's gate instead: green on the healthy product, red under its planted defects.
7. **Decision request.** An ambiguous proposal whose word behaved like the other claim's words on every state is **undecided**: only a reader can tell whether the two claims say the same. The word is onboarded with its claim as a claim of its own (`match: new`), the candidates stay open under `pending`, and onboarding prints the question and the two commands that answer it. Until the agent answers, the claims stay separate. Every later word that joins the undecided claim (the same claim, or a match of it) is also compared with the pending claim's words, and carries the same question if it behaved like them.

## Outcome and output

`onboard` exits `0` when the word is onboarded or undecided, `1` when it is rejected, and `2` when the outcome is inconclusive or the proposal is refused (`verilex: refused: ...` on stderr). It prints the outcome first, then only the trials that broke the gate and the findings, each with its evidence path, then the onboarding record: `~/.local/state/verilex/<project>/onboarding/<word>/<id>/`, which holds `record.json` (what `--json` prints) and each trial's run directory under `trials/`.

```
$ verilex onboard item-trusted
rejected item-trusted: item-trusted gives a false pass under planted defect dropped-add: store-open | item-trusted pear is green
  trial  dropped-add  store-open | item-trusted pear: green, expected red
    evidence: ~/.local/state/verilex/tally/onboarding/item-trusted/1791241042-c1aa48/trials/02-dropped-add-item-trusted/02-item-trusted
  record: ~/.local/state/verilex/tally/onboarding/item-trusted/1791241042-c1aa48
```

## Planted defects

A claim's `defects` name product states in which the claim is false. Each sets environment variables that every frame step and word of a trial sees; the healthy trial unsets them all. They are not part of the claim's identity. A planted defect added or changed later makes every word gated against the claim drift-suspect until it is onboarded again, and the same holds for a word grouped under the claim through an alias.

## The grouping file

`.verilex/grouping.yaml` holds one decision per onboarded word: the claim version it is grouped under (and the alias it pins, as `proves`), its entry point, the digest of its files, how it was grouped (`same`, `mechanical`, `agent` or `new`), the grouped claims still open for an undecided word (`pending`), the digest of each planted defect it caught, its counted runs per product, the chain it was proven on, the digest of its claim's sources as onboarding checked them, the commit of each source in another repository, the date and the onboarding record:

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

## Products

Uses are counted per product, the `project` in `config.yaml`. The correctness gate holds for every product, so onboarding an admitted word in another product only checks its two counted uses there and records the product: `onboarded item-stored for tally-b: it proves item-added@18e2db0cee8f, and its correctness gate already holds`. Onboarding it again in a product that already has it changes nothing.

## Deciding an undecided word

The decision request names the claim the word is grouped under, each candidate claim with its text and the words the behavioral check compared, and the commands that answer it; `--json` prints it as `request` (`question`, `proposal`, `candidates`, `commands`). The calling agent reads both claims and answers once:

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
