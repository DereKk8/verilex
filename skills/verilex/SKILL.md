---
name: verilex
description: Drive verilex to prove claims from an intent and a diff. Use when a product has .verilex and a change should be verified by recorded claims. Fall back to the product verify skill when verilex has no claim for the moment.
---

# verilex

You are the brain. verilex is the hands. It does not pick claims and it does not judge meaning.

Reach for verilex when the product has `.verilex/` and the change should be proved by recorded claims. Fall back to the product's verify skill when a lookup returns no claim, a word is missing, or you must drive the product by hand.

No intent means prove nothing this change touched broke. If verilex says no claim covers the change, that is inconclusive: fall back to the product verify skill.

1. `verilex index --intent '<intent>'` and `verilex index --changed <change>`. A change is a path, `config:<key>`, `image:<pin>` or `runbook:<ref>`.
2. Derive claims from both lookups. Named claims are a floor, never a ceiling. Pass them with `--named`. Do not treat a named list as the whole job.
3. `verilex plan --claim <derived> --named <named> --changed <change>`. With no intent, omit `--claim`. Do not pass `--continue`.
4. Read skip, run, order, chain and unpicked. Then run `verilex run --claim <derived> --named <named> --changed <change>`: the same flags, no chain argument. If the plan says `whole chain runs live`, every step runs, including `proven` ones; report it that way.
5. If the plan lists `unpicked`, or the verdict says `touched claim not covered`, add those claims with `--claim` and run again before you accept the result. If it lists `unmapped`, `unclaimed` or `unrun`, fix a mistyped change, or verify that part with the product verify skill or a chain that uses the word.

The command output is the rest of the contract.
