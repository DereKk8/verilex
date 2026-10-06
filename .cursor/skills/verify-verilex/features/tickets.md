# Run tickets

A run ticket fixes what one verification run is meant to prove (`intent`, `diff` or both) and which brain drives it (`harness`, `model`, `effort`, optional `token_budget` and `time_budget`). Each brain field comes from the first level that sets it: the ticket, its named profile, the project default (`.verilex/profiles.yaml`), the user default (`$XDG_CONFIG_HOME/verilex/profiles.yaml`), then the built-in (`effort: medium`). `verilex ticket <file>` prints the resolved ticket with the level behind each field. `run --ticket` and `plan --ticket` refuse an invalid ticket before anything starts and otherwise carry the resolved ticket in the run record and the JSON plan. A ticket never changes what runs or what is skipped. verilex launches no harness and calls no model.

## Sub-features

- `ticket-resolve` prints each field with the level that set it; `--json` prints the ticket whole, with `from`.
- `ticket-profiles` resolves a named profile to a concrete harness, model and effort; a project profile replaces a user profile of the same name, and a user profile serves a name the project lacks.
- `ticket-default-profile` picks the project default's profile when the ticket names none.
- `ticket-precedence` lets each level win over every level below it: ticket, named profile, project default, user default, built-in.
- `ticket-refused` refuses an invalid ticket or profiles file in `ticket`, `plan` and `run`, naming the file and the field, exit `2`, with no run and no store.
- `ticket-carried` records the resolved ticket in the run record and the JSON plan, and leaves human output unchanged.
- `ticket-no-skip-change` keeps the skip decision unchanged by the ticket and by `.verilex/profiles.yaml`.
- `ticket-concurrent` runs two chains with different tickets at the same time; each run records its own ticket, and no ticket, profiles or product file changes.

## How to get to it (user POV)

- Write a ticket file, and optionally `.verilex/profiles.yaml` and `~/.config/verilex/profiles.yaml`.
- Run `verilex ticket <file> [--json]`, `verilex plan --ticket <file> '<chain>'` or `verilex run --ticket <file> '<chain>'` in a product checkout.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor. Everything from **Carried** on needs the admitted baseline (`admit-all`).
- `T` is `$S/tickets`, `U` is `$S/config/verilex/profiles.yaml` (the session's user profiles, through `XDG_CONFIG_HOME`), `P` is `$S/tally/.verilex/profiles.yaml` and `C` is `store-open | item-stored apple | item-listed apple`.
- **Setup.** Run `mkdir -p "$T" "$S/config/verilex"`. Write the baseline profiles, and write them again whenever a step says to restore `U` and `P`:
  - `printf 'defaults:\n  harness: claude-code\n  model: claude-sonnet-5-5\n  effort: low\nprofiles:\n  deep-verify: {harness: codex, model: gpt-5.1-codex}\n  nightly: {model: gpt-5.1-codex, effort: max}\n' > "$U"`
  - `printf 'defaults:\n  profile: quick-verify\n  effort: high\nprofiles:\n  quick-verify: {model: claude-haiku-4-5, effort: low, time_budget: 10m}\n  deep-verify: {model: claude-opus-5-5, effort: xhigh, token_budget: 400000}\n' > "$P"`
  - `printf 'intent: prove a renamed item keeps its count\nprofile: deep-verify\n' > "$T/deep.yaml"` and `printf 'diff: main...HEAD\nprofile: quick-verify\n' > "$T/quick.yaml"`

- **Resolve.** Run `"$S/vx" tk-deep verilex ticket "$T/deep.yaml"`. Exit `0`, and exactly:

  ```
  intent: prove a renamed item keeps its count
  profile: deep-verify  from ticket
  harness: claude-code  from user default
  model: claude-opus-5-5  from project profile deep-verify
  effort: xhigh  from project profile deep-verify
  token_budget: 400000  from project profile deep-verify
  ```

  The project's `deep-verify` replaces the user's as a whole, so the harness is not `codex`.
- **JSON.** Run `"$S/vx" tk-deep-json verilex ticket --json "$T/deep.yaml"`. The object has `"harness": "claude-code"`, `"token_budget": 400000` (a number) and `from` with `"harness": "user default"`, `"model": "project profile deep-verify"` and `"profile": "ticket"`.
- **Named profile.** Run `"$S/vx" tk-quick verilex ticket "$T/quick.yaml"`: `diff: main...HEAD`, `profile: quick-verify  from ticket`, `harness: claude-code  from user default`, `model: claude-haiku-4-5  from project profile quick-verify`, `effort: low  from project profile quick-verify`, `time_budget: 10m  from project profile quick-verify`.
- **User profile.** Run `printf 'intent: prove listing survives a restart\nprofile: nightly\n' > "$T/nightly.yaml"`, then `"$S/vx" tk-nightly verilex ticket "$T/nightly.yaml"`: `model: gpt-5.1-codex  from user profile nightly` and `effort: max  from user profile nightly`.
- **Default profile.** Run `printf 'intent: prove the store opens empty\n' > "$T/plain.yaml"`, then `"$S/vx" tk-default-profile verilex ticket "$T/plain.yaml"`: `profile: quick-verify  from project default`.
- **Precedence.** Run `printf 'intent: prove the store opens empty\nprofile: deep-verify\neffort: max\n' > "$T/ladder.yaml"`. Remove one level at a time, and run `"$S/vx" <label> verilex ticket "$T/ladder.yaml"` after each change. Read its `effort:` line:
  1. As written (`tk-ladder-ticket`): `effort: max  from ticket`.
  2. After `sed -i '/^effort: max$/d' "$T/ladder.yaml"` (`tk-ladder-profile`): `effort: xhigh  from project profile deep-verify`.
  3. After `sed -i 's/, effort: xhigh//' "$P"` (`tk-ladder-project`): `effort: high  from project default`.
  4. After `sed -i '/^  effort: high$/d' "$P"` (`tk-ladder-user`): `effort: low  from user default`.
  5. After `sed -i '/^  effort: low$/d' "$U"` (`tk-ladder-builtin`): `effort: medium  from built-in`.

  Restore `U` and `P`.
- **Refused.** Run `"$S/vx" tk-refused-runs-before verilex runs`. Run `printf 'intent: prove the store opens empty\neffort: turbo\n' > "$T/bad.yaml"`. Then run `"$S/vx" tk-bad-ticket verilex ticket "$T/bad.yaml"`, `"$S/vx" tk-bad-plan verilex plan --ticket "$T/bad.yaml" "$C"` and `"$S/vx" tk-bad-run verilex run --ticket "$T/bad.yaml" "$C"`. Each prints stderr `verilex: refused: ticket $T/bad.yaml: effort: "turbo" is not one of minimal, low, medium, high, xhigh, max` and exits `2`.
- **Refused, other fields.** Each exits `2` with the stderr shown:
  - `printf 'intent: prove the store opens empty\nefort: max\n' > "$T/typo.yaml"`, then `"$S/vx" tk-typo verilex run --ticket "$T/typo.yaml" 'store-open'`: `verilex: refused: ticket $T/typo.yaml: efort: unknown field; expected one of intent, diff, profile, harness, model, effort, token_budget, time_budget`.
  - `printf 'model: claude-opus-5-5\n' > "$T/empty.yaml"`, then `"$S/vx" tk-empty verilex plan --ticket "$T/empty.yaml" 'store-open'`: `verilex: refused: ticket $T/empty.yaml: intent or diff: a ticket names at least one`.
  - `printf 'intent: x\nprofile: weekly\n' > "$T/weekly.yaml"`, then `"$S/vx" tk-unknown-profile verilex ticket "$T/weekly.yaml"`: `verilex: refused: ticket $T/weekly.yaml: profile: no profile named weekly in $P or $U`.
  - `sed -i 's/model: claude-haiku-4-5/model: claude haiku/' "$P"`, then `"$S/vx" tk-bad-profiles verilex run --ticket "$T/quick.yaml" 'store-open'`: `verilex: refused: $P: profiles.quick-verify.model: must be a plain name without spaces, not "claude haiku"`. Restore `P`.
- **Refused, second view.** Run `"$S/vx" tk-refused-runs-after verilex runs` and `"$S/vx" tk-refused-stores ls -A "$S/stores"`. The run list equals `tk-refused-runs-before` and no store exists.
- **Carried.** Run `admit-all`, then `"$S/vx" tk-run verilex run --ticket "$T/deep.yaml" "$C"`: `green: 3 green; run <A>`, live, with no ticket line in the output. Run `"$S/vx" tk-run-record python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); t=r["ticket"]; print(r.get("skipped", False), t["profile"], t["harness"], t["model"], t["effort"], t["from"]["model"])' "$S/home/tally/runs/<A>/run.json"`: `False deep-verify claude-code claude-opus-5-5 xhigh project profile deep-verify`.
- **No skip change.** Run `"$S/vx" tk-plan-plain verilex plan "$C"` and `"$S/vx" tk-plan-quick verilex plan --ticket "$T/quick.yaml" "$C"`. Both print `plan: skip 3, run 0` and three `relies on run <A>` lines; `diff` of their `stdout` files is empty. Run `"$S/vx" tk-plan-json verilex plan --json --ticket "$T/quick.yaml" "$C"`, then `"$S/vx" tk-plan-json-read python3 -c 'import json,sys; p=json.load(open(sys.argv[1])); print(repr(p.get("rerun", "")), [s["skipped"] for s in p["steps"]], p["ticket"]["profile"], p["ticket"]["model"])' <evidence>/stdout` with the `tk-plan-json` evidence directory: `'' [True, True, True] quick-verify claude-haiku-4-5`.
- **Profiles outside the stamp.** Run `sed -i 's/model: claude-opus-5-5/model: claude-opus-5-1/' "$P"`, then `"$S/vx" tk-plan-edited verilex plan --ticket "$T/deep.yaml" "$C"`: still `plan: skip 3, run 0`. Restore `P`.
- **Skipped run carries its ticket.** Run `"$S/vx" tk-run-skip verilex run --ticket "$T/quick.yaml" "$C"`: `green: 3 green, skipped: stamps match run <A>; run <B>`. Run `"$S/vx" tk-run-skip-record python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); t=r["ticket"]; print(r.get("skipped", False), t["profile"], t["model"], t["effort"])' "$S/home/tally/runs/<B>/run.json"`: `True quick-verify claude-haiku-4-5 low`.
- **Concurrent.** Add a word that waits until both runs reach it, so the two runs are live at the same time. Set `W` to `$S/tally/.verilex/words/store-met` and run `mkdir "$S/met" "$W"`. Write its contract with `printf -- '---\nword: store-met\npromise: Two runs are live at once.\nrequires: [store]\nimplements: [verify-tally/features/store.md#store-open]\n---\n' > "$W/word.md"`. Write its `run` with the lines below, then `chmod +x "$W/run"`:

  ```sh
  #!/bin/sh
  cat >/dev/null
  touch "$MET/$VERILEX_RUN"
  i=0
  while [ "$(ls -A "$MET" | wc -l)" -lt 2 ]; do
    i=$((i+1)); if [ $i -gt 400 ]; then echo '{"verdict": "blocked", "detail": "the other run never started"}'; exit 2; fi
    sleep 0.05
  done
  echo '{"verdict": "pass", "observation": "both runs were live at once"}'
  ```

  Run `"$S/vx" tk-snapshot-before "$PWD/.cursor/skills/verify-verilex/scripts/snapshot" "$S"`. Start both runs together and wait for both: `MET="$S/met" "$S/vx" tk-concurrent-quick verilex run --ticket "$T/quick.yaml" 'store-open | store-met' & MET="$S/met" "$S/vx" tk-concurrent-deep verilex run --ticket "$T/deep.yaml" 'store-open | store-met' & wait`. Each prints `green: 2 green; run <RUN>` (`<Q>` and `<D>`) and exits `0`.
- **Concurrent, second view.** Run `"$S/vx" tk-met ls -A "$S/met"`: exactly `<Q>` and `<D>`, so each run's word saw the other live. Run `"$S/vx" tk-concurrent-records python3 -c 'import json,sys; [print(json.load(open(p))["ticket"]["profile"], json.load(open(p))["ticket"]["model"]) for p in sys.argv[1:]]' "$S/home/tally/runs/<Q>/run.json" "$S/home/tally/runs/<D>/run.json"`: `quick-verify claude-haiku-4-5` then `deep-verify claude-opus-5-5`. Run `"$S/vx" tk-snapshot-after "$PWD/.cursor/skills/verify-verilex/scripts/snapshot" "$S"`; `diff` of the two `stdout` files is empty. Remove the word with `command rm -rf "$W" "$S/met"`.

## Gotchas

- `verilex ticket` resolves inside a product checkout, because the project default comes from the product's `.verilex/profiles.yaml`.
- Ticket paths are relative to the working directory, which is `$S/tally` under `vx`. Pass absolute paths.
- `vx` sets `XDG_CONFIG_HOME=$S/config`, so the operator's own `~/.config/verilex/profiles.yaml` never reaches a session. A session launched before that change has no `$S/config`; the doctor fails it, so relaunch.
- A ticket names `intent`, `diff` or both. `diff` is carried, not read: `plan` takes no diff input yet.
- An invalid profiles file refuses every `--ticket` call, even for a profile the ticket does not use. Calls without `--ticket` never read profiles files.
- Two `vx` calls started together can get the same `NNN-` number; their labels keep the evidence directories apart.
