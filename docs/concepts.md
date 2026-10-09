# Core concepts

verilex lets verifier and QA agents run product verification as a chain of words, Unix-pipe style. This page names the parts and links to the page that specifies each one.

```
verilex run 'store-open | item-stored apple | item-listed apple'
```

## Words

A **word** is a reusable, executable piece that drives the product through a surface its users have and promises one product state. Someone who knows the product, but not how it is built, would recognize each word as a real moment in the product's life.

A word lives in `.verilex/words/<word>/`: a contract in `word.md` and an executable `run`, in any language. It reports `pass`, `fail` or `blocked`, and verilex judges that report. See [Adding verilex to a project](adding-verilex.md#the-word-contract).

## Chains

A chain is words joined by `|`, with arguments after the word. Each word declares the states it `requires` and `provides`, and verilex refuses a chain in which a word requires a state that no earlier word provides, before it starts anything. Agents are creative by composing existing words into new chains within those rules. See [The chain grammar](chains.md).

## Claims and the verify skill

verilex holds execution only. The project's verification skill keeps the meaning: what a feature is, how a user reaches it, what proves it and its traps. A **claim** pins one piece of that meaning for verilex: what is true once a word passes, and the evidence that proves it. Several words can prove one claim, and each pins the claim version it proves. When a word is not green, verilex prints the verify-skill entries behind it so the agent can continue by hand. See [Claims](claims.md).

## Three verdicts

| Verdict | Means | Exit |
|---|---|---|
| `green` | The product works, backed by evidence. | `0` |
| `red` | The product is broken, backed by evidence. | `1` |
| `inconclusive` | Environment or harness trouble, or a word's report that is not backed. Never reported as a product failure. | `2` |

A refused command also exits `2`. See [Verdicts](verdicts.md).

## The trust frame

Every run goes through the project's frame, and no word can opt out of it: **launch** creates an instance owned by this run, the **doctor** vouches for it, every step writes **evidence** outside the product, **honesty rules** turn an unbacked report into `inconclusive`, and **cleanup** tears down only what the run started. See [The trust frame](trust-frame.md).

## Stamps, the ledger and skipping

Every word result carries a **proof stamp**: a fingerprint of everything the result depended on. Green steps become passes in the **ledger**. A later run skips the chain only when every word is admitted and a pass younger than 7 days stands for every word's stamp; anything missing or unclear runs the chain live. `verilex plan` makes the same decision through the same code and runs nothing. See [Proof stamps and skipping](skipping.md) and [Plan](plan.md).

## The word lifecycle

A word is **provisional** until it is admitted. A word that proves a claim is admitted by [onboarding](onboarding.md), which gates it on the claim's planted defects; a word without a claim is admitted by an outside curator. An admitted word whose files, frame or claim changed is **drift-suspect** and always runs live. See [The word lifecycle](word-lifecycle.md).

## The launcher

`verilex-agent` starts the brain a run ticket names in a sandbox, lets it run only claim plans, and prints the JSON of one of its `verilex run`s, chosen by a fixed rule. See [Companion launcher](launcher.md).

## Glossary

| Term | Meaning |
|---|---|
| admitted | A word that onboarding (or, for a word without a claim, an outside curator) accepted. Only chains whose words are all admitted are skipped. |
| brain | The harness, model and effort that a run ticket names. `verilex-agent` starts it. |
| chain | Words joined by `\|`, run in order on one instance. |
| claim | What is true once a word passes, plus the evidence that proves it, in `.verilex/claims/<claim>.yaml`. |
| doctor | The frame step that confirms the instance is this run's and worth driving, before the first word and again after any failure. |
| drift-suspect | An admitted word whose files, frame, shared word files, claim version, planted defects or sources changed since it was admitted. It always runs live. |
| entry point | The user entry a word exercises, one of its claim's `entry` values, such as `cli`. |
| evidence | What each step leaves under `~/.local/state/verilex/<project>/runs/<run>/`, outside the product and every repository. |
| frame | The project's `launch`, `doctor`, `refresh` and `cleanup` steps in `.verilex/frame/`. |
| instance | What `launch` creates for one run. No two runs share a mutable instance. |
| ledger | The directory of passes that every verilex instance pointed at it reads and writes. |
| pass | A green step of a run that is not inconclusive and launched its own instance, recorded in the ledger with its stamp, its claim version and a copy of its evidence. |
| planted defect | Environment variables under which a claim is false. Onboarding requires its word to go red under each. |
| provisional | A word that is not admitted yet. It runs live in every chain. |
| run id | `<epoch>-<12 hex digits>`, passed to every frame step and word as `VERILEX_RUN`. |
| run ticket | A file that fixes what one run is meant to prove and which brain drives it. |
| state | A name a word `requires` or `provides`, such as `store` or `item:{name}`. |
| stamp | A fingerprint of everything a word result depended on. |
| verify skill | The project's `verify-*` skill and its feature map, which keep the meaning that claims pin. |
| word | A reusable, executable piece that drives the product and promises one product state. |
