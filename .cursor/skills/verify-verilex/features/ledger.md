# Shared ledger

The ledger is a directory of passes that every verilex instance pointed at it reads and writes. Stateless instances, each with a state home of its own, set `VERILEX_LEDGER` to one shared directory and keep the skip savings. Concurrent runs each add a pass file of their own, so no pass is lost or mixed. A reader refuses a pass that does not match its digest, proved another claim version, was recorded on an instance its run did not launch, or whose evidence is gone, changed or misses the evidence contract. A pass 7 days old or older ages out.

## Sub-features

- `ledger-default` keeps one ledger per state home, `$VERILEX_HOME/tally/ledger`, when `VERILEX_LEDGER` is unset.
- `ledger-concurrent` records every pass of concurrent stateless instances, with none lost and none forged.
- `ledger-shared` lets an instance reuse passes other instances recorded in `VERILEX_LEDGER`, launching nothing; an instance outside it reuses none.
- `ledger-pass-record` records the claim version, fingerprints, a copy of the evidence, the result digest, the time, the recording run and the run that launched its instance.
- `ledger-refuse-unowned` refuses a pass recorded on an instance its run did not launch.
- `ledger-refuse-contract` refuses a pass whose evidence misses the evidence contract.
- `ledger-refuse-claim-version` refuses a pass that proved another claim version.
- `ledger-refuse-damaged` refuses a pass whose content does not match its digest, and a pass whose evidence changed or is gone.
- `ledger-age-out` refuses a pass 7 days old or older, and drops it with its evidence when the step records again.

## How to get to it (user POV)

- Set `VERILEX_LEDGER=<dir>` for every instance that should share passes, then use `verilex run` and `verilex plan` as usual.
- Read a pass at `<dir>/tally/passes/<slot>/<stamp>.<digest>.json` and its evidence at `<dir>/tally/evidence/<id>/`.

## Driving it with vx

Preconditions:

- A fresh session with the admitted baseline (`admit-all`). Onboarding decisions (`.verilex/grouping.yaml`) live in `$S/tally`, so every instance sees the words as admitted.
- A stateless instance is `VX_INSTANCE=<name>`: its state home is `$S/instances/<name>`. Add `VERILEX_LEDGER="$S/ledger"` to share the ledger.
- `AUDIT="$PWD/.cursor/skills/verify-verilex/scripts/ledger-audit"` and `RESEAL="$PWD/.cursor/skills/verify-verilex/scripts/reseal"`.
- The refusal steps play another writer of the shared ledger, a tool or a person that bypasses verilex. They are the only steps that edit verilex state by hand.

- **Default ledger.** After `admit-all`, run `"$S/vx" led-default verilex run 'store-open | item-stored apple | item-listed apple'` (live: onboarding changed every stamp). Run `"$S/vx" led-default-audit "$AUDIT" "$S/home/tally/ledger" "$S/home"`: `lost: 0  forged: 0  damaged: 0  unowned: 0`, with as many passes as proven steps.
- **Concurrent.** Run `for i in 1 2 3 4 5 6; do f=$(echo apple pear plum | cut -d' ' -f$(( (i - 1) % 3 + 1 ))); VX_INSTANCE=c$i VERILEX_LEDGER="$S/ledger" "$S/vx" led-conc-$i verilex run "store-open | item-stored $f | item-listed $f" > "$S/led-$i.out" 2>&1 & done; wait`. Each `led-$i.out` holds `green: 3 green; run <id>` and `exit 0`.
- **Concurrent, second view.** Run `"$S/vx" led-conc-audit "$AUDIT" "$S/ledger/tally" "$S"/instances/c*`. It prints `proven: 18  passes: 18  intact: 18  matched: 18  lost: 0  forged: 0  damaged: 0  unowned: 0` and `passes per slot: 2 2 2 2 2 2 6`: `store-open` holds a pass from each of the six runs.
- **Shared.** Run `VX_INSTANCE=n1 VERILEX_LEDGER="$S/ledger" "$S/vx" led-shared verilex run 'store-open | item-stored pear | item-listed pear'`. Output `green: 3 green, skipped: stamps match ...; run <N>`. Run `"$S/vx" led-shared-stores ls "$S/stores"`: empty, nothing was launched.
- **Not shared.** Run `VX_INSTANCE=alone "$S/vx" led-alone verilex plan 'store-open | item-stored pear | item-listed pear'`: `plan: skip 0, run 3; store-open: no green result on record`.
- **Pass record.** Run `"$S/vx" led-pass python3 -c 'import glob,json,sys; p=[json.load(open(f)) for f in glob.glob(sys.argv[1]+"/passes/*/*.json") if json.load(open(f))["label"]=="item-stored plum"][0]; print(p["claim"], p["run"]==p["owner"], p["recorded"], p["evidence"], p["result"], sorted(p["components"]))' "$S/ledger/tally"`. It prints `item-added@<version> True <time> evidence/<id> <sha256> [...]`, and the components include `claim`, `claim sources`, `word`, `input bin/tally` and `upstream`.

The refusal steps each record one new step, so its slot holds a single pass, then change that pass as another writer would. Run every command in them in one instance that shares the ledger: prefix it with `VX_INSTANCE=r1 VERILEX_LEDGER="$S/ledger"`. For a fruit `<F>`:

1. Record it: `"$S/vx" led-<F> verilex run 'store-open | item-stored <F> | item-listed <F>'` (run `<R>`).
2. Find its pass: `"$S/vx" led-<F>-pass grep -rl '"label": "item-stored <F>"' "$S/ledger/tally/passes"` prints `<PASS>`.
3. Find its evidence: `EVIDENCE="$S/ledger/tally/$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["evidence"])' <PASS>)"`.
4. After the change, `"$S/vx" led-<F>-plan verilex plan 'store-open | item-stored <F> | item-listed <F>'` prints `plan: skip 0, run 3; item-stored <F>: <reason>`.

- **Refuse unowned.** With `fig`: `"$S/vx" led-fig-reseal "$RESEAL" <PASS> owner=1767225600-a1b2c3d4e5f6`. Reason: `the pass from run <R> was recorded on an instance run 1767225600-a1b2c3d4e5f6 launched; only a run that launched its own instance records a pass`.
- **Refuse unowned, second view.** Run `"$S/vx" led-fig-rerun verilex run --json 'store-open | item-stored fig | item-listed fig'`: `"rerun"` holds the same reason and every word is green with no `relies_on`. Then `"$S/vx" led-fig-again verilex plan 'store-open | item-stored fig | item-listed fig'` prints `plan: skip 3, run 0`, relying on the re-run: the fresh pass stands beside the refused one.
- **Refuse contract.** With `kiwi`: `command rm "$EVIDENCE/stdout"; echo '{"verdict": "pass"}' > "$EVIDENCE/stdout"`, then `"$S/vx" led-kiwi-reseal "$RESEAL" <PASS> result=$(sha256sum < "$EVIDENCE/stdout" | cut -d' ' -f1)`. Reason: `the pass from run <R> misses the evidence contract: pass without a second observation`.
- **Refuse claim version.** With `lime`: `"$S/vx" led-lime-reseal "$RESEAL" <PASS> claim=item-added@000000000000`. Reason: `the pass from run <R> proved item-added@000000000000, not item-added@<version>; a pass proves only the claim version it ran against`.
- **Refuse damaged.** With `date`: `sed -i 's/"recorded": "/"recorded": "2/' <PASS>`. Reason: `a pass on record is damaged: its content does not match its digest`.
- **Refuse changed evidence.** With `mango`: `command rm "$EVIDENCE/stdout"; echo '{"verdict": "pass", "observation": "trust me"}' > "$EVIDENCE/stdout"`. Reason: `the evidence of the pass from run <R> changed after it was recorded`. Then `command rm -r "$EVIDENCE"`: the reason becomes `evidence from run <R> is gone`.
- **Age out.** With `grape`: reseal both its `item-stored grape` and `item-listed grape` passes with `recorded=$(date -u -d '8 days ago' +%Y-%m-%dT%H:%M:%SZ)`, noting each pass's slot directory `<SLOT>` and evidence id `<OLD>`. Reason: `the green result from run <R> expired (8d old; results stand 7d)`.
- **Age out, second view.** Run the chain again (live), then `"$S/vx" led-grape-after ls "$S/ledger/tally/passes/<SLOT>"` and `"$S/vx" led-grape-evidence ls "$S/ledger/tally/evidence"`. Each slot holds one pass, recorded by the re-run, and neither `<OLD>` id is listed: recording dropped the expired passes with their evidence.

## Gotchas

- A slot keeps every pass that stands, so a refusal shows only while the tampered pass is the slot's only pass for that stamp. Tamper with a step that one run recorded, as the steps above do with a new fruit each.
- Replace an evidence file with `rm` and a new write, never in place: the default ledger links its evidence to the run's own files, so an in-place edit changes both.
- `ledger-audit` counts a pruned pass as lost. Run it before **Age out**, or leave the aged runs' homes out.
- `admit-all` runs in `$S/home`, outside `$S/ledger`. Its passes stay in the default ledger, so the shared ledger starts empty.
- The 7-day expiry itself cannot be waited out in a session. **Age out** rewrites `recorded` as another writer would, and verilex then refuses and prunes through the CLI.
