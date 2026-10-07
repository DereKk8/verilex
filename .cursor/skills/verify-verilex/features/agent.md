# Launcher

`verilex-agent` runs one verification by starting the brain named in the run spec and printing the JSON of the brain's last `verilex run`, byte for byte. The brain's own message is not the verdict. A green is returned only from a claim run (`verilex-claim-run-1`) whose `requested` holds every `--claim` given to the launcher and every path the diff changes; otherwise the launcher is inconclusive, exit `2`, with nothing on stdout. Each run has its own state home. Runs that share `--ledger` keep skip savings. A second run that reuses a home still in use is refused.

## Sub-features

- `agent-verdict` prints verilex's JSON verdict, not the text the brain wrote, and the run record in that run's home matches the run id and verdict and carries the run spec's brain as `ticket`.
- `agent-skill` hands the brain the skill file, the intent and each changed path. With a diff and no intent, the intent line is `prove nothing this change touched broke`.
- `agent-missed-claim` returns verilex's own missed-claim warning: a brain that derives one claim from a diff touching three gets verilex's green with `warning` `2 touched claims not covered` and both `uncovered` claims, and `--text` lists them.
- `agent-uncovered-spec` is inconclusive, exit `2` and no stdout, when the green run left out a changed path, left out a launcher `--claim`, or was a chain run that was given no diff.
- `agent-edited-project` is inconclusive, exit `2`, when the brain changes a file in the project during the run, even when the same defect gives the honest brain verilex's red.
- `agent-bypass` is inconclusive, exit `2`, when the brain runs the real `verilex` in the launcher's home, and tears down the instance that run kept.
- `agent-budget` ends the run at `time_budget` even when the brain started a child in its own session.
- `agent-brain-limits` refuses, exit `2`, a brain's `onboard`, `run --keep` and `run --continue`, and nothing they would change changes.
- `agent-ticket` accepts a ticket file instead of intent flags, and still returns verilex's JSON.
- `agent-same-home` refuses a second run whose `--home` is still in use, exit `2`, with no second verdict.
- `agent-parallel` runs two verilex-agent processes at once, each with its own home and the same ledger. Each home holds only its own run. A later run on a new home skips and launches no store.
- `agent-no-harness` does not start a real harness unless `--allow-harness` is set.

## How to get to it (user POV)

- Build `verilex-agent` from `./agent/cmd/verilex-agent` and put it on `PATH`.
- Pass `--ticket`, or `--intent` or `--diff`, plus `--brain` for a stub executable or `--allow-harness` for a configured harness.
- Read the JSON verdict on stdout. Exit `0` is green, `1` is red, `2` is inconclusive or refused; stderr says `verilex-agent: inconclusive:` or `verilex-agent: refused:`.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor, then `admit-all`. The launcher needs the project in a git work tree, so commit the product: `"$S/vx" agent-git-init git init -q`, `"$S/vx" agent-git-add git add -A`, `"$S/vx" agent-git-commit git -c user.name=v -c user.email=v@example.com commit -qm base`. Build the launcher into the session bin: `go build -o "$S/bin/verilex-agent" ./agent/cmd/verilex-agent` from the repository root.
- `A` is the common launcher flags: `--project "$S/tally" --skill "$S/skill.md" --harness stub --model stub`.
- Write the skill and the brains, then `chmod +x "$S"/brain*`. `brain` proves the item-listed claim; `brain-one` derives only `store-opened` and passes each changed path the prompt lists.

```sh
printf '%s\n' 'SKILL-MARKER' > "$S/skill.md"
cat > "$S/brain" << 'EOF'
#!/bin/sh
printf '%s\n' 'BRAIN SAYS GREEN'
if [ -n "${PROMPT_COPY:-}" ]; then cp "$VERILEX_AGENT_PROMPT" "$PROMPT_COPY"; fi
verilex run --claim item-listed
EOF
cat > "$S/brain-one" << 'EOF'
#!/bin/sh
printf '%s\n' 'BRAIN SAYS EVERYTHING IS COVERED'
set --
for path in $(sed -n 's/^changed: //p' "$VERILEX_AGENT_PROMPT"); do set -- "$@" --changed "$path"; done
verilex run --claim store-opened "$@"
EOF
```

- **Verdict.** Run `PROMPT_COPY="$S/seen-prompt" "$S/vx" agent-verdict verilex-agent $A --home "$S/agent-home" --brain "$S/brain" --intent 'prove a stored apple is listed' --effort low`. Exit `0`. Stdout is one JSON object whose `format` is `verilex-claim-run-1`, `verdict` is `green` and `run` is `<A>`. It does not contain `BRAIN SAYS`.
- **Verdict, second view.** Run `"$S/vx" agent-verdict-record python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); print(r["verdict"], r["run"], r["ticket"]["harness"], r["ticket"]["model"])' "$S/agent-home/tally/runs/<A>/run.json"`. It prints `green <A> stub stub`.
- **Skill.** Run `"$S/vx" agent-skill cat "$S/seen-prompt"`. The file contains `SKILL-MARKER`, `intent: prove a stored apple is listed` and `The launcher returns verilex's own JSON verdict from your last verilex run and ignores your message.`
- **A diff.** Change one file every claim depends on: `printf '# changed\n' >> "$S/tally/bin/tally"`. Then `"$S/vx" agent-git-diff git diff --name-only` prints `bin/tally`.
- **Default intent.** Write a brain that only copies the prompt, `cat > "$S/brain-intent" << 'EOF'` then the two lines `#!/bin/sh` and `cp "$VERILEX_AGENT_PROMPT" "$PROMPT_COPY"`, and `chmod +x "$S/brain-intent"`. Run `PROMPT_COPY="$S/seen-default" "$S/vx" agent-default verilex-agent $A --home "$S/agent-default-home" --brain "$S/brain-intent" --diff HEAD`. Exit `2`, stderr `verilex-agent: inconclusive: the brain exited without a verilex run`, empty stdout. Run `"$S/vx" agent-default-prompt cat "$S/seen-default"`. It contains `intent: prove nothing this change touched broke`, `diff: HEAD` and `changed: bin/tally`.
- **Missed claim.** Run `"$S/vx" agent-missed verilex-agent $A --home "$S/agent-missed-home" --ledger "$S/ledger" --brain "$S/brain-one" --intent 'prove the store opens' --diff HEAD`. Exit `0`. JSON `verdict` `green`, `warning` `2 touched claims not covered`, `uncovered` claims `item-added` and `item-listed`, each with a `next` command; `requested.changed` is `["bin/tally"]`. No `BRAIN SAYS`.
- **Missed claim, second view.** `"$S/vx" agent-missed-record python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); print(r["warning"]); print(*[u["claim"] for u in r["uncovered"]])' "$S/agent-missed-home/tally/runs/<M>/run.json"` prints the same warning and `item-added item-listed`. `"$S/vx" agent-missed-text verilex-agent $A --ledger "$S/ledger" --brain "$S/brain-one" --intent 'prove the store opens' --diff HEAD --text` prints `green; run <id>...; 2 touched claims not covered` and one `uncovered` line per claim.
- **Uncovered spec.** Each of these exits `2`, prints nothing on stdout, and says `verilex-agent: inconclusive: run <id> is green but` with the reason. Write `$S/brain-<case>` as `#!/bin/sh` plus the one `verilex run` line, `chmod +x`, and run `"$S/vx" agent-<case> verilex-agent $A --home "$S/agent-<case>-home" --ledger "$S/ledger" --brain "$S/brain-<case>" --intent 'prove the store opens' --diff HEAD`:
  - `nodiff`: `verilex run --claim store-opened` → `was not asked about change bin/tally`.
  - `nofloor`: `verilex run --claim store-opened --changed bin/tally`, with `--claim item-listed` added to the launcher → `was not asked about claim item-listed`.
  - `chain`: `verilex run 'store-open'` → `is not a claim run, so it does not show what it was asked`.
  - Second view: `"$S/vx" agent-uncovered-runs python3 -c 'import json,sys,glob; [print(p.split("/")[-5], json.load(open(p))["verdict"]) for a in sys.argv[1:] for p in glob.glob(a+"/tally/runs/*/run.json")]' "$S/agent-nodiff-home" "$S/agent-nofloor-home" "$S/agent-chain-home"` prints each home with `green`. verilex proved what it was asked; the launcher refused to call that the spec's verdict.
- **Brain limits.** Write `$S/brain-limits` as below, `chmod +x` it, and snapshot first: `"$S/vx" agent-limits-before .cursor/skills/verify-verilex/scripts/snapshot "$S"` from the repository root. Run `"$S/vx" agent-limits verilex-agent $A --home "$S/agent-limits-home" --ledger "$S/ledger" --brain "$S/brain-limits" --diff HEAD`. Exit `0`, no `warning` in the JSON. `"$S/vx" agent-limits-log cat "$S/refusals"` holds `verilex onboard is not available to the brain`, `--keep is not available to the brain`, `--continue is not available to the brain` and three `exit 2` lines. A second snapshot matches the first, `ls -A "$S/agent-limits-home/tally"` holds only `runs` (no onboarding record), and `ls -A "$S/stores"` is unchanged.

```sh
#!/bin/sh
verilex onboard item-listed 2>> "$REFUSALS"; echo "exit $?" >> "$REFUSALS"
verilex run --keep --claim store-opened --changed bin/tally 2>> "$REFUSALS"; echo "exit $?" >> "$REFUSALS"
verilex run --continue someone-else 'store-open' 2>> "$REFUSALS"; echo "exit $?" >> "$REFUSALS"
verilex run --changed bin/tally > /dev/null
```

  Pass `REFUSALS="$S/refusals"` in front of `"$S/vx"`.
- **Edited project.** Write `$S/brain-fix` as below and `chmod +x` it. Run `TALLY_DEFECT=hide-lists "$S/vx" agent-honest-red verilex-agent $A --brain "$S/brain" --intent 'prove a stored apple is listed'`: exit `1`, verilex's red. Then `TALLY_DEFECT=hide-lists "$S/vx" agent-fixer verilex-agent $A --brain "$S/brain-fix" --intent 'prove a stored apple is listed'`: exit `2`, empty stdout, stderr `verilex-agent: inconclusive: the brain changed the project during the run, so no verdict covers the code under test: bin/tally`. Second view: `"$S/vx" agent-fixer-status git status --porcelain` shows ` M bin/tally`. Restore it with `"$S/vx" agent-fixer-undo git checkout -q -- bin/tally` and append the diff line again.

```sh
#!/bin/sh
verilex run --claim item-listed > /dev/null
python3 -c "p='bin/tally'; s=open(p).read(); open(p,'w').write(s.replace('hide-lists', 'hide-lists-off'))"
verilex run --claim item-listed > /dev/null
```

- **Bypass.** Write `$S/brain-bypass` as below and `chmod +x` it. Run `REAL_VERILEX="$S/bin/verilex" AGENT_HOME="$S/agent-bypass-home" "$S/vx" agent-bypass verilex-agent $A --home "$S/agent-bypass-home" --brain "$S/brain-bypass" --intent 'prove the store opens'`: exit `2`, empty stdout, stderr names the bypass run with `did not come through the launcher` and `kept its instance; the launcher tore it down`. Second view: `"$S/vx" agent-bypass-runs python3 -c 'import json,sys,glob; [print(json.load(open(p))["run"], json.load(open(p))["cleanup"]) for p in glob.glob(sys.argv[1]+"/tally/runs/*/run.json")]' "$S/agent-bypass-home"` shows no run with `cleanup` `kept`, and `ls -A "$S/stores"` holds no store of the bypass run.

```sh
#!/bin/sh
VERILEX_HOME="$AGENT_HOME" "$REAL_VERILEX" run --keep --claim store-opened > /dev/null
verilex run --claim store-opened > /dev/null
```

- **Budget.** Write `$S/brain-escape` as `#!/bin/sh`, `python3 -c 'import os, time; os.setsid(); time.sleep(8)' &` and `sleep 8`, and `chmod +x` it. Run `printf 'intent: prove the store opens\nharness: stub\nmodel: stub\ntime_budget: 1s\n' > "$S/agent-budget.yaml"`, then `"$S/vx" agent-budget verilex-agent --project "$S/tally" --skill "$S/skill.md" --brain "$S/brain-escape" --ticket "$S/agent-budget.yaml"`: exit `2` with `inconclusive: the time budget ended` about one second after it started, not eight. The `evidence` dir's timestamps, or `time`, show it.
- **Undo the diff.** `"$S/vx" agent-git-undo git checkout -q -- bin/tally`, then `"$S/vx" agent-git-clean git status --porcelain` prints nothing before the next recipe.
- **Ticket.** Run `printf 'intent: prove a stored apple is listed\nharness: stub\nmodel: stub\neffort: low\n' > "$S/agent-ticket.yaml"`, then `"$S/vx" agent-ticket verilex-agent --project "$S/tally" --home "$S/agent-ticket-home" --skill "$S/skill.md" --brain "$S/brain" --ticket "$S/agent-ticket.yaml"`. Exit `0`, JSON `verdict` `green`, and stdout does not contain `BRAIN SAYS`.
- **Same home.** Write `$S/brain-hold` as below and `chmod +x` it. `HOLD` is `$S/hold-flag` and `RELEASE` is `$S/release-flag`.

```sh
#!/bin/sh
touch "$HOLD"
while [ ! -f "$RELEASE" ]; do sleep 0.05; done
verilex run --claim store-opened
```

  Run `HOLD="$S/hold-flag" RELEASE="$S/release-flag" "$S/vx" agent-hold verilex-agent $A --home "$S/agent-hold-home" --brain "$S/brain-hold" --intent 'prove the store opens' > "$S/agent-hold.out" 2>&1 &`. When `$S/hold-flag` exists, run `"$S/vx" agent-same-home verilex-agent $A --home "$S/agent-hold-home" --brain "$S/brain" --intent 'prove the store opens'`. Exit `2`, stderr contains `in use`, and stdout is empty. Then `touch "$S/release-flag"` and `wait`. The held run exits `0`.
- **Parallel.** Use a fresh ledger: `for i in a b; do "$S/vx" agent-par-$i verilex-agent $A --home "$S/agent-par-$i" --ledger "$S/par-ledger" --brain "$S/brain" --intent 'prove a stored apple is listed' > "$S/agent-par-$i.out" 2>&1 & done; wait`. Each out file contains `"verdict": "green"` and `exit 0`, and does not contain `BRAIN SAYS`.
- **Parallel, second view.** Read each JSON `run` id. `"$S/vx" agent-par-homes ls "$S/agent-par-a/tally/runs" "$S/agent-par-b/tally/runs"` lists exactly `<A>` under the first and `<B>` under the second.
- **Shared skip.** Run `"$S/vx" agent-skip verilex-agent $A --home "$S/agent-skip-home" --ledger "$S/par-ledger" --brain "$S/brain" --intent 'prove a stored apple is listed'`. Exit `0`. JSON has `"skipped": true` and `"verdict": "green"`. JSON `instance` is `null`. Run `"$S/vx" agent-skip-stores ls -A "$S/stores"`. It holds no `tally-<skip run>` store.
- **No real harness.** Run `"$S/vx" agent-no-harness verilex-agent --project "$S/tally" --skill "$S/skill.md" --intent 'prove the store opens' --harness claude-code --model claude-opus`. Exit `2`, stderr `verilex-agent: refused: harness claude-code is not started unless --allow-harness is set`, and `"$S/vx" agent-no-harness-runs verilex runs` shows no new run from that command.

## Gotchas

- The launcher's stdout is verilex's JSON from the brain's last `verilex run` that printed JSON. A refused run prints none and does not hide an earlier verdict.
- A chain run given `--changed` is a claim run too: verilex warns about the touched claims the chain did not prove, and the launcher returns that green with its warning.
- Without `--home` the launcher's home is temporary and removed after the run, so `verilex runs` in the session never lists launcher runs. Pass `--home` for a second view.
- `vx` adds a `$ command` line before stdout. Parse the JSON from the evidence `stdout` file, not from the `vx` transcript.
- The launcher replaces `VERILEX_HOME` and `VERILEX_LEDGER` for verilex and passes the rest of the environment through, so `TALLY_STORES` from `vx` still places stores under `$S/stores`.
- The project must be a git work tree, or the launcher refuses before the brain starts; so is a `--diff` revision git cannot read. Files git ignores are not checked for brain edits.
- The edited-project and bypass checks are detection, not a sandbox: a brain with a shell can still forge a shared-ledger pass (README, Companion launcher).
- `--ticket` refuses `--intent`, `--diff`, `--harness`, `--model`, `--effort` and `--profile` on the same command.
- The held run must be released with `touch "$S/release-flag"` or it sleeps until the shell ends.
