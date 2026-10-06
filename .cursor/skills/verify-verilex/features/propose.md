# Propose

`verilex propose <word>` builds one self-contained curator packet for a word without a claim (one that names feature-map sections in `implements`) that has counted uses in at least two different runs. A use counts when the word's step was green or red in a live run whose verdict was green or red. The packet holds the word's files, its uses, the text and hash of each feature-map section it implements, the whole dictionary and instructions for the curator. A word that proves a claim joins through [onboarding](onboard.md) instead, so `propose` refuses it.

## Sub-features

- `propose-refused` refuses a word with counted uses in fewer than two runs.
- `propose-counting` counts green and red runs, and ignores inconclusive and skipped runs.
- `propose-packet` writes `$VERILEX_HOME/<project>/proposals/<word>/<id>.json` and prints its id and path.
- `propose-claim-refused` refuses a word that proves a claim and names `verilex onboard`.

## How to get to it (user POV)

- Run `verilex propose <word>` after the word has been used in chains.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor.
- A probe word without a claim. Run `"$S/vx" prop-new verilex new probe-item --implements verify-tally/features/items.md#item-add`, then `command cp "$S/tally/.verilex/words/item-stored/run" "$S/tally/.verilex/words/probe-item/run"` and `printf -- '---\nword: probe-item\npromise: A named item is in the store.\nargs: [name]\nrequires: [store]\nprovides: ["item:{name}"]\nimplements:\n  - verify-tally/features/items.md#item-add\ninputs: [bin/tally]\nenv: [TALLY_DEFECT]\n---\n' > "$S/tally/.verilex/words/probe-item/word.md"`.

- **Refused.** Run `"$S/vx" prop-use-1 verilex run 'store-open | probe-item apple | item-listed apple'`, then `"$S/vx" prop-short verilex propose probe-item`. Stderr `verilex: refused: probe-item has counted uses in 1 run(s); propose needs at least 2 different runs in which it was green or red`, exit `2`.
- **Inconclusive does not count.** Run `TALLY_SIMULATE_LOCK=1 "$S/vx" prop-blocked verilex run 'store-open | probe-item apple | item-listed apple'` (inconclusive), then `"$S/vx" prop-still-short verilex propose probe-item`. It still says `1 run(s)`.
- **Red run counts.** Run `TALLY_DEFECT=drop-adds "$S/vx" prop-red verilex run 'store-open | probe-item apple | item-listed apple'` (red at `probe-item apple`), then `"$S/vx" prop-probe verilex propose probe-item`. Stdout `proposed probe-item: packet <PACKET> (2 runs)`, the packet path, and the `verilex admit` hint. Exit `0`.
- **Packet, second view.** Run `"$S/vx" prop-packet python3 -c 'import json,sys,glob; p=json.load(open(sorted(glob.glob(sys.argv[1] + "/*.json"))[0])); print(sorted(p)); print([(u["run"], u["verdict"]) for u in p["uses"]]); print([s["ref"] for s in p["sections"]])' "$S/home/tally/proposals/probe-item"`. Keys include `curator_instructions`, `dictionary`, `files`, `sections`, `uses`, `word_digest` (no `claim`); `uses` lists the green and the red run, `green` and `red` for `probe-item`; `sections` is `['verify-tally/features/items.md#item-add']`.
- **Claim refused.** Run `"$S/vx" prop-claim verilex propose item-stored`. Stderr ``verilex: refused: item-stored proves claim item-added, so it joins the vocabulary through `verilex onboard item-stored`, not propose and admit``, exit `2`. Run `"$S/vx" prop-claim-dir ls "$S/home/tally/proposals"`: only `probe-item`.

## Gotchas

- The use count is per run: a word that appears twice in one chain still counts once.
- A word gets no use from a red chain where an earlier word failed first, because it never ran (`not run`).
- Each `propose` writes a new packet id. A later `admit` must name the packet the curator actually judged.
- Remove the probe word with `command rm -rf "$S/tally/.verilex/words/probe-item"` before a recipe that expects only tally's three words.
