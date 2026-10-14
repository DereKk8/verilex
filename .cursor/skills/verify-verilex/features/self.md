# Self-verification scenarios

These checks drive the built CLI and the shipped verification helpers on isolated scratch products. Each has a healthy trial and a literal source mutation during onboarding.

## Sub-features

- `self-index-tiers` checks index tiers list this product's admitted words and expose word drift consistently.
- `self-index-changed` checks the changed-word lookup selects its claim and agrees with the chain plan.
- `self-ledger-shared` checks a fresh stateless instance reuses intact shared passes without launching a store.
- `self-ledger-integrity` checks changed shared-ledger evidence forces a live chain until a fresh pass stands.
- `self-drift` checks changed word files and added or removed planted defects force drift and recover when restored.
- `self-plan-binding` checks a fresh state home's claim plan binds arguments and provider order from admission.
- `self-agent-diff-request` checks the launcher refuses a green run whose request omits a changed path.
- `self-skill-session` checks the verification session freezes its binary, detects tampering and refuses another worker's cleanup.
- `self-ci-main-ref` checks the canary scripts refuse a tag in place of the origin/main branch ref.

## How to get to it (user POV)

Run `scripts/ci canary`. To replay one scenario, use the pinned driver from `scripts/ci` to run `verilex run --fresh 'self-<scenario>'` in this checkout.

## Driving it with vx

- `self-index-tiers`: Expect tier 1 lists item-added, item-listed, store-opened; tier 2 shows item-stored active; tier 3 shows admitted apple; tier 2 and tier 3 agree on drift-suspect after editing item-stored and clear drift after restoration; tally-b lists no active claims and item-stored dormant.
- `self-index-changed`: Expect index --changed selects only item-listed; every affected chain runs and its reason equals plan rerun after the word edit; unrelated.txt selects no claims.
- `self-ledger-shared`: Expect shared stateless reader skipped with instance null and cleanup none; ledger-audit shows three intact matched passes and zero lost, forged, damaged, unowned; an isolated home runs all steps.
- `self-ledger-integrity`: Expect changed mango evidence makes plan run all three steps with changed-after-recording reason; run has no relies_on; the fresh pass restores skip.
- `self-drift`: Expect check names changed item-listed, new another-dropped-add and removed dropped-add defects as drift; plan runs each drifted chain live; restoring files clears drift and restores skip.
- `self-plan-binding`: Expect fresh-home claim plan binds store-open | item-stored apple | item-listed apple with provider order and writes no run; execution is green and its run record holds that chain.
- `self-agent-diff-request`: Expect launcher refuses omitted bin/tally with exit 2 and empty stdout although its run record is green; a run requested with bin/tally returns green and leaves no store.
- `self-skill-session`: Expect launch freezes --bin across a source rebuild; doctor accepts the frozen session and refuses tampering with exit 1; cleanup refuses a different worker with exit 2 and unchanged session files, then removes the exact session under its launching identity.
- `self-ci-main-ref`: Expect with only a tag named refs/remotes/origin/main, scripts/ci canary exits 1 and canary-guards exits 2 naming the absent branch ref; git show-ref independently confirms the branch is absent.

## Gotchas

- The outer driver must be the reviewed commit pinned in `scripts/ci`, not the binary under test.
- Each scenario creates a fresh tally copy, state home and ledger. Evidence holds command transcripts and independent read-only observations.
- The launcher scenario uses a deterministic shell brain, not a paid model, and needs the real platform sandbox. An unavailable sandbox is inconclusive.
- These claims cover current main behavior. They do not assert admission-first binding over recent history, a touched-claim diff floor, storage-clock pruning until their fixes land.
