# Verdicts

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
