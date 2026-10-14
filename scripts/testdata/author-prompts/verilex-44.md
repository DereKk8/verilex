## Author prompts

**Why this PR exists:** The author wants Verilex to keep an index that ties documentation passages to claims, and claims to code, so that at review time a diff names which doc passages it could have made false. The author chose claim identity over line numbers so anchors survive refactors, wants both directions (a code diff flags tied doc passages, and a doc-only diff flags the claims it asserts about), and settled that the code wins when a doc and the code disagree. The author asked for the first unit of that work to be built as its own PR: the anchor format, the `index --changed` lookups in both directions, anchors on Verilex's own docs, and a refusal for an anchor that names a missing claim. The author gave the health number, freshness stamp and review step to later units.

> send claude 'cloud agents' to build the first units of work of those abstracted tasks
