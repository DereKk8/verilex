# Continuing a kept instance

`verilex run --keep` leaves its instance alive and records its **history**: every word that drove it, in order, with its stamp and verdict. `verilex run --continue <run> '<chain>'` takes that instance over instead of launching one:

1. It decides, before touching anything, which steps the instance already proves. It skips the longest prefix of the chain whose steps each match the instance's next history entry with the same stamp, under the same rules as above (green, evidence present, younger than 7 days, every word admitted). Every step after that prefix runs live on the instance.
2. It runs the project's `refresh` frame step, which brings the instance up to the current checkout while keeping its states, then the doctor. A failing `refresh` or doctor makes the run `inconclusive`.
3. It runs the remaining words and cleans the instance up, unless `--keep` is given again.

```
$ verilex run --continue 1767225600-a1b2c3d4e5f6 --keep 'store-open | item-stored apple | item-listed apple'
green: 3 green, continued 1767225600-a1b2c3d4e5f6, 2 skipped: proven on its instance by run 1767225600-a1b2c3d4e5f6; run 1767225900-d4e5f6a7b8c9
kept: tear down with `verilex cleanup 1767225900-d4e5f6a7b8c9`
```

## Words that change state

A word that changes the instance is not safe to run twice, so `--continue` never runs one on top of effects the chain before it would not have built. When the first word that must run live would follow history that still holds a word that changes state (because that word itself changed, is held, or its result expired), `--continue` refuses before anything starts and asks for a fresh run:

```
$ verilex run --continue 1767225600-a1b2c3d4e5f6 'store-open | item-stored apple | item-listed apple'
verilex: refused: item-stored apple: word changed; the kept instance already holds the effects of item-stored apple from run 1767225600-a1b2c3d4e5f6, and a word that changes state never runs twice or out of order on one instance: run without --continue
```

Only words whose contract declares `read_only: true` are passed over in that history: they observe the instance and never change it, so a changed read-only word runs again on the kept instance, as above. A change to the frame (including `refresh` itself), to shared word files, to verilex or to an input changes every stamp, so it always ends in that refusal. `--fresh` cannot be combined with `--continue`.

## Ownership

The continuing run owns the instance from then on: the kept run shows `cleanup=continued` in `verilex runs`, and `verilex cleanup` and `--continue` on it point at the run that took over. Results proven on a continued instance stand only in that instance's history, never in the ledger, so they never skip a chain on a fresh instance.
