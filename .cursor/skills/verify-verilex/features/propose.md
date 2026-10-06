# Propose

`verilex propose <word>` builds one self-contained curator packet for a word that has counted uses in at least two different runs. A use counts when the word's step was green or red in a live run whose verdict was green or red. Only uses that proved the claim version the word pins count. The packet holds the word's files, its uses, the claim it proves with each source's anchor (or, for a word without a claim, the feature-map sections it implements), the whole dictionary and instructions for the curator.

## Sub-features

- `propose-refused` refuses a word with counted uses in fewer than two runs.
- `propose-counting` counts green and red runs, and ignores inconclusive and skipped runs.
- `propose-packet` writes `$VERILEX_HOME/<project>/proposals/<word>/<id>.json` and prints its id and path.
- `propose-claim` puts the claim the word proves in the packet: its pin, the word's entry point, the claim file and each source's anchor.

## How to get to it (user POV)

- Run `verilex propose <word>` after the word has been used in chains.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor.

- **Refused.** Run `"$S/vx" prop-use-1 verilex run 'store-open | item-stored apple | item-listed apple'`, then `"$S/vx" prop-short verilex propose item-listed`. Stderr `verilex: refused: item-listed has counted uses in 1 run(s); propose needs at least 2 different runs in which it was green or red`, exit `2`.
- **Inconclusive does not count.** Run `TALLY_SIMULATE_LOCK=1 "$S/vx" prop-blocked verilex run 'store-open | item-stored apple | item-listed apple'` (inconclusive), then `"$S/vx" prop-still-short verilex propose item-listed`. It still says `1 run(s)`.
- **Red run counts.** Run `TALLY_DEFECT=drop-adds "$S/vx" prop-red verilex run 'store-open | item-stored apple | item-listed apple'` (red, `store-open` green), then `"$S/vx" prop-store verilex propose store-open`. Stdout `proposed store-open: packet <PACKET> (2 runs)`, the packet path, and the `verilex admit` hint. Exit `0`.
- **Packet, second view.** Run `"$S/vx" prop-packet python3 -c 'import json,sys,glob; p=json.load(open(sorted(glob.glob(sys.argv[1] + "/*.json"))[0])); print(sorted(p)); print([(u["run"], u["verdict"]) for u in p["uses"]])' "$S/home/tally/proposals/store-open"`. Keys include `claim`, `curator_instructions`, `dictionary`, `files`, `sections`, `uses`, `word_digest`, and `uses` lists the green and the red run, each with `green` for `store-open`.
- **Packet claim.** Run `"$S/vx" prop-claim python3 -c 'import json,sys,glob; c=json.load(open(sorted(glob.glob(sys.argv[1] + "/*.json"))[0]))["claim"]; print(c["pin"], c["entry"], [(s["ref"], s["requirements"], s.get("review")) for s in c["sources"]]); print(c["text"].splitlines()[1])' "$S/home/tally/proposals/store-open"`. It prints ``store-opened@<version> cli [('verify-tally/features/store.md#store-open', ['Expect exit 0 and `opened`; `store.json` holds `{"items": []}`.'], None)]`` and `sentence: A new, empty store is open and ready for items.`

## Gotchas

- The use count is per run: a word that appears twice in one chain still counts once.
- `item-listed` gets no use from a red chain where `item-stored` failed first, because it never ran (`not run`).
- Each `propose` writes a new packet id. A later `admit` must name the packet the curator actually judged.
