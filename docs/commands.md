# Commands

| Command | Does |
|---|---|
| `verilex run '<chain>' [--keep] [--fresh] [--ticket <file>] [--json]` | Plans the chain, skips it when every proof stamp matches and every word is admitted, else launches an owned instance, runs each word, cleans up |
| `verilex run [--claim <claim> ...] [--named <claim> ...] [--changed <change> ...] [--fresh] [--no-chain] [--json]` | Runs the chain from the claim plan. The verdict names any touched claim the run did not prove |
| `verilex plan '<chain>' [--continue <run>] [--ticket <file>] [--json]` | Runs nothing; prints whether the chain would be skipped or run live, the run each skipped step relies on, and the first reason it must run live |
| `verilex plan [--claim <claim> ...] [--named <claim> ...] [--changed <change> ...] [--no-chain] [--json]` | Plans claims and a diff: skip (with the fingerprints that prove it), run, chain order, and touched claims not picked. No `--claim` means prove nothing the diff touched broke |
| `verilex run --continue <run> '<chain>' [--keep] [--json]` | Takes over the instance `<run>` kept: `refresh`, doctor, then runs only the words that instance does not already prove |
| `verilex ticket <file> [--json]` | Validates a run ticket and prints each field with the level it came from |
| `verilex words` | Lists the dictionary with each word's promise, claim, `requires`, `provides` and lifecycle status |
| `verilex claims [--json]` | Lists each claim's current version, the words that prove it, and why it needs review |
| `verilex runs` | Lists this project's runs and whether each instance was cleaned up; a run without a verdict shows `running` while its process lives and `died` once it is gone |
| `verilex cleanup <run>` | Tears down an instance kept with `--keep`, or left behind by a run whose process died; refuses a run that is still going |
| `verilex new <word> --implements <ref>` | Scaffolds a provisional word that implements a feature-map section |
| `verilex onboard <word> [--json]` | Admits a word that proves a claim: checks its uses, mapping and evidence, matches its claim against the vocabulary, gates it on planted defects, and records the decision in `.verilex/grouping.yaml` |
| `verilex onboard <word> --same-as <claim> \| --distinct` | Answers the decision request an undecided onboarding left: the word's claim says the same as that grouped claim, or stays a claim of its own |
| `verilex index [<claim> [<word>]] [--json]` | Prints the word index for this product: active claims, a claim's words, or one word's details |
| `verilex index --intent '<text>' [--json]` | Finds the claims an intent names |
| `verilex index --changed <change>... [--json]` | Lists the claims a diff affects, and for each known chain whether `plan` would run it. A change is a file path, `config:<key>`, `image:<pin>` or `runbook:<ref>` |
| `verilex propose <word>` | Builds a curator packet for a word without a claim, used in at least two runs |
| `verilex admit <word> --verdict <file>` | Records an outside curator's `admit` or `reject` verdict on such a packet |
| `verilex gap '<description>'` | Notes a product moment the feature map has no section for |
| `verilex check` | Reports admitted words that are drift-suspect, including those whose claim needs review |

## Exit codes

Exit codes: `0` green, `1` red, `2` inconclusive or refused.
