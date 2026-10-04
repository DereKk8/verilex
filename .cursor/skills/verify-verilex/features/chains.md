# Chains

## Sub-features

Dictionary listing, quoted argument binding, requires/provides ordering, passing chains, and product failures.

## How to get to it (user POV)

Use `verilex words` and `verilex run '<chain>'` inside a project or select it with `--project`.

## Driving it with the CLI

Follow the skill's launch and doctor commands. Run the three-word apple chain and expect exit 0.
Read all three passing word records and the stored word's action and observation files.
Reverse the first two words and expect exit 2 before any new run directory appears.
Set `TALLY_DEFECT=drop-adds`, repeat the valid chain, and expect exit 1 after the second word.
Read the reason stating that tally claimed an addition absent from the store.

## Gotchas

Quote the entire chain so the shell does not interpret `|`. Tally's listing word splits on whitespace.
Refused chains create no run. Word stdout must contain one result JSON object.
