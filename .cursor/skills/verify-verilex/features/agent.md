# Launcher

`verilex-agent` runs one verification by starting the brain named in the run spec and printing the JSON of one of the brain's `verilex run`s, byte for byte. [The verdict rule](../../../../docs/verdict-rule.md) chooses that run. The launcher adds `--no-chain` to every `run` and `plan` the brain asks for, so verilex refuses any chain argument, an empty one too, before it runs anything: the brain runs only claim plans, and a chain leaves no run record. A red is final; otherwise the last run is the result. The brain's own message is not the verdict. A green is returned only from a run that proved every floor claim (each launcher `--claim` and each claim `verilex index --intent` finds for the intent), was asked about every path the diff changes, and proved every claim that an earlier inconclusive run selected; otherwise the launcher is inconclusive, exit `2`, with nothing on stdout. An intent that names no claim is inconclusive before the brain starts, unless `--claim` is given. The brain runs in a sandbox: it reads the project read-only, writes only its own run directory, and reaches neither the verilex home, the ledger, the real `verilex` binary nor the host's network. Where the sandbox cannot start or does not hold, the brain does not run and the launcher is inconclusive. Each run has its own state home. Runs that share `--ledger` keep skip savings. A second run that reuses a home still in use is refused.

## Sub-features

- `agent-verdict` prints verilex's JSON verdict, not the text the brain wrote, and the run record in that run's home matches the run id and verdict and carries the run spec's brain as `ticket`.
- `agent-skill` hands the brain the skill file, the intent and each changed path, on stdin and in the file `VERILEX_AGENT_PROMPT` names. With a diff and no intent, the intent line is `prove nothing this change touched broke`.
- `agent-missed-claim` returns verilex's own missed-claim warning: a brain that derives one claim from a diff touching three gets verilex's green with `warning` `2 touched claims not covered` and both `uncovered` claims, and `--text` lists them.
- `agent-uncovered-spec` is inconclusive, exit `2` and no stdout, when the green run left out a changed path or a launcher `--claim`.
- `agent-every-run` keeps a red: the tester's F6 brain gets a red, then runs a narrower claim that is green, and the launcher exits `1` with the red, for an intent and for a diff.
- `agent-chain-refused` refuses every chain the brain writes: verilex rejects it under `--no-chain`, an empty chain argument too (F8), and the chain leaves no run record. A claim run before or after a refused chain decides as usual (F7, F10), and a chain alone is inconclusive (F10). After a refused chain on `pear`, the claim plan still lists `apple`, so `hide-apple` gives verilex's red, also with `--fresh` (F9, F11).
- `agent-intent-floor` makes the claims `verilex index --intent` finds a floor: the prompt lists them, a brain whose last run proves a narrower claim is inconclusive, and an intent that names no claim is inconclusive before the brain starts unless `--claim` is given. A `verilex index` that fails, or does not answer within the time budget or 30 seconds, is inconclusive before the brain starts, not refused.
- `agent-read-only-project` keeps the project as it is: a brain's edit fails, the launcher returns verilex's red for the code under test, and the project is unchanged.
- `agent-changed-project` is inconclusive, exit `2`, when something outside the sandbox changes the project while the brain runs.
- `agent-bypass` keeps the real `verilex` out of the brain's reach: its call fails, its `verilex` is the launcher's relay, and the home holds only the run that came through the launcher.
- `agent-around` is inconclusive, exit `2`, when a run that did not come through the launcher appears in its home during the run, and tears down the instance that run kept.
- `agent-sandbox` stops the tester's F2 forger (P11): the brain finds no ledger path in `/proc` or `ps`, cannot read the ledger or the home, cannot run the real `verilex`, gets a refusal from the egress proxy for loopback, and writes nothing to the ledger, the home or the project. Its rerun stays verilex's red, and `ledger-audit` finds no forged pass.
- `agent-no-sandbox` is inconclusive, exit `2`, and never starts the brain, when the sandbox tool fails or starts the brain without a sandbox.
- `agent-terminal` keeps the brain away from the terminal the launcher runs in: it cannot write `/dev/tty` or the terminal's device, and nothing it writes shows in the terminal.
- `agent-budget` ends the run at `time_budget` even when the brain started a child in its own session.
- `agent-brain-limits` refuses, exit `2`, a brain's `onboard`, `run --keep` and `run --continue`, and nothing they would change changes.
- `agent-ticket` accepts a ticket file instead of intent flags, and still returns verilex's JSON.
- `agent-same-home` refuses a second run whose `--home` is still in use, exit `2`, with no second verdict.
- `agent-parallel` runs two verilex-agent processes at once, each with its own home and the same ledger. Each home holds only its own run. A later run on a new home skips and launches no store.
- `agent-no-harness` does not start a real harness unless `--allow-harness` is set.
- `agent-real-harness` drives a real claude, codex or pi brain in the sandbox with only its credential file, and returns the verdict of the verilex run that brain made.

## How to get to it (user POV)

- Build `verilex-agent` from `./agent/cmd/verilex-agent` and put it on `PATH`. On Linux, install `bwrap` and allow it user namespaces ([Sandbox](../../../../docs/sandbox.md)). macOS uses the built-in `sandbox-exec`.
- Pass `--ticket`, or `--intent` or `--diff`, plus `--brain` for a stub executable or `--allow-harness` for a configured harness.
- Read the JSON verdict on stdout. Exit `0` is green, `1` is red, `2` is inconclusive or refused; stderr says `verilex-agent: inconclusive:` or `verilex-agent: refused:`.
- Pass `--keep-work` to keep the run directory. stderr then ends with `verilex-agent: run files kept in <R>`. `<R>/brain/home` is the brain's `HOME`, `<R>/log/verilex.log` lists each verilex command the brain sent, and `<R>/log/egress.log` lists each network request.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor, then `admit-all`. The launcher needs the project in a git work tree, so commit the product: `"$S/vx" agent-git-init git init -q`, `"$S/vx" agent-git-add git add -A`, `"$S/vx" agent-git-commit git -c user.name=v -c user.email=v@example.com commit -qm base`. Build the launcher into the session bin: `go build -o "$S/bin/verilex-agent" ./agent/cmd/verilex-agent` from the repository root.
- `A` is the common launcher flags: `--project "$S/tally" --skill "$S/skill.md" --harness stub --model stub`.
- The brain can write only its own home, so a brain that leaves a file for the second view writes it in `$HOME`, and its run passes `--keep-work`. `<R>` is the directory that run's stderr names. Read the file at `<R>/brain/home/<file>`.
- Write the skill and the brains, then `chmod +x "$S"/brain*`. `brain` proves the item-listed claim; `brain-one` derives only `store-opened` and passes each changed path the prompt lists.

```sh
printf '%s\n' 'SKILL-MARKER' > "$S/skill.md"
cat > "$S/brain" << 'EOF'
#!/bin/sh
printf '%s\n' 'BRAIN SAYS GREEN'
cp "$VERILEX_AGENT_PROMPT" "$HOME/prompt"
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

- **Verdict.** Run `"$S/vx" agent-verdict verilex-agent $A --home "$S/agent-home" --brain "$S/brain" --intent 'prove a stored apple is listed' --effort low --keep-work`. Exit `0`. Stdout is one JSON object whose `format` is `verilex-claim-run-1`, `verdict` is `green` and `run` is `<A>`. It does not contain `BRAIN SAYS`. stderr names `<R>`.
- **Verdict, second view.** Run `"$S/vx" agent-verdict-record python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); print(r["verdict"], r["run"], r["ticket"]["harness"], r["ticket"]["model"])' "$S/agent-home/tally/runs/<A>/run.json"`. It prints `green <A> stub stub`. `"$S/vx" agent-verilex-log cat "<R>/log/verilex.log"` holds one line, `verilex --project <S>/tally run --ticket <R>/ticket.yaml --json --claim item-listed -> exit 0`.
- **Skill.** Run `"$S/vx" agent-skill cat "<R>/brain/home/prompt"`. The file contains `SKILL-MARKER`, `intent: prove a stored apple is listed`, the floor lines `floor: item-listed` and `floor: item-added`, `It adds --no-chain to every verilex run and plan` and `The first case that applies decides:` with the six numbered cases of the verdict rule (`docs/verdict-rule.md`) and, last, a `# Task` section that tells the brain to prove the intent now.
- **A diff.** Change one file every claim depends on: `printf '# changed\n' >> "$S/tally/bin/tally"`. Then `"$S/vx" agent-git-diff git diff --name-only` prints `bin/tally`.
- **Default intent.** Write a brain that only copies the prompt, `cat > "$S/brain-intent" << 'EOF'` then the two lines `#!/bin/sh` and `cp "$VERILEX_AGENT_PROMPT" "$HOME/prompt"`, and `chmod +x "$S/brain-intent"`. Run `"$S/vx" agent-default verilex-agent $A --home "$S/agent-default-home" --brain "$S/brain-intent" --diff HEAD --keep-work`. Exit `2`, stderr `verilex-agent: inconclusive: the brain made no verilex run, so there is no verdict`, empty stdout. Run `"$S/vx" agent-default-prompt cat "<R>/brain/home/prompt"`. It contains `intent: prove nothing this change touched broke`, `diff: HEAD` and `changed: bin/tally`.
- **Missed claim.** Run `"$S/vx" agent-missed verilex-agent $A --home "$S/agent-missed-home" --ledger "$S/ledger" --brain "$S/brain-one" --intent 'prove the store opens' --diff HEAD`. Exit `0`. JSON `verdict` `green`, `warning` `2 touched claims not covered`, `uncovered` claims `item-added` and `item-listed`, each with a `next` command; `requested.changed` is `["bin/tally"]`. No `BRAIN SAYS`.
- **Missed claim, second view.** `"$S/vx" agent-missed-record python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); print(r["warning"]); print(*[u["claim"] for u in r["uncovered"]])' "$S/agent-missed-home/tally/runs/<M>/run.json"` prints the same warning and `item-added item-listed`. `"$S/vx" agent-missed-text verilex-agent $A --ledger "$S/ledger" --brain "$S/brain-one" --intent 'prove the store opens' --diff HEAD --text` prints `green; run <id>...; 2 touched claims not covered` and one `uncovered` line per claim.
- **Uncovered spec.** Each of these exits `2`, prints nothing on stdout, and says `verilex-agent: inconclusive: run <id> is green but` with the reason. Write `$S/brain-<case>` as `#!/bin/sh` plus the one `verilex run` line, `chmod +x`, and run `"$S/vx" agent-<case> verilex-agent $A --home "$S/agent-<case>-home" --ledger "$S/ledger" --brain "$S/brain-<case>" --intent 'prove the store opens' --diff HEAD`:
  - `nodiff`: `verilex run --claim store-opened` → `was not asked about change bin/tally`.
  - `nofloor`: `verilex run --claim store-opened --changed bin/tally`, with `--claim item-listed` added to the launcher → `was not asked about claim item-listed`.
  - Second view: `"$S/vx" agent-uncovered-runs python3 -c 'import json,sys,glob; [print(p.split("/")[-5], json.load(open(p))["verdict"]) for a in sys.argv[1:] for p in glob.glob(a+"/tally/runs/*/run.json")]' "$S/agent-nodiff-home" "$S/agent-nofloor-home"` prints each home with `green`. verilex proved what it was asked; the launcher refused to call that the spec's verdict.
- **Every run.** Write `$S/brain-narrow` as `#!/bin/sh`, `verilex run --claim item-listed > /dev/null` and `verilex run --claim store-opened > /dev/null`, and `$S/brain-narrow-diff` as `#!/bin/sh`, `verilex run --changed bin/tally > /dev/null` and `verilex run --claim store-opened --changed bin/tally > /dev/null`, and `chmod +x` both. Then:
  - `TALLY_DEFECT=hide-lists "$S/vx" agent-narrow verilex-agent $A --home "$S/agent-narrow-home" --brain "$S/brain-narrow" --intent 'prove a stored apple is listed' --keep-work`: exit `1`, JSON `verdict` `red` and `requested.claims` `["item-listed"]`. `"$S/vx" agent-narrow-log cat "<R>/log/verilex.log"` shows `--claim item-listed -> exit 1`, then `--claim store-opened -> exit 0`: the launcher held the later green and returned the red.
  - `TALLY_DEFECT=hide-lists "$S/vx" agent-narrow-diff verilex-agent $A --home "$S/agent-narrow-diff-home" --brain "$S/brain-narrow-diff" --diff HEAD --keep-work`: exit `1`, JSON `verdict` `red`, `requested.claims` `[]` and `requested.changed` `["bin/tally"]`. Its `verilex.log` (`agent-narrow-diff-log`) shows `--changed bin/tally -> exit 1`, then `--claim store-opened --changed bin/tally -> exit 0`.
- **Chains refused.** Write each brain as `#!/bin/sh` plus these runs, each with `> /dev/null`, and each chain run with `2>> "$HOME/refused"` too, and `chmod +x` them: `$S/brain-dup` runs `verilex run 'store-open | store-open'`, then `verilex run --claim store-opened`. `$S/brain-dup-after` runs the same two in the other order. `$S/brain-dup-alone` runs only the chain. `$S/brain-blank` runs `verilex run --changed bin/tally ''`, then `verilex run --changed bin/tally`. `$S/brain-f11` runs `verilex run --changed bin/tally 'store-open | item-stored pear | item-listed pear'`, then `verilex run --claim item-listed`. `$S/brain-f11-fresh` runs `verilex run 'store-open | item-stored pear | item-listed pear'`, then `verilex run --fresh --claim item-listed`. `$S/brain-f6chain` runs `verilex run 'store-open | item-stored apple | item-listed apple'`, then `verilex run --claim store-opened`, and `$S/brain-f6chain-diff` runs the same two with `--changed bin/tally` added to each. Then:
  - `"$S/vx" agent-dup-chain verilex-agent $A --home "$S/agent-dup-chain-home" --brain "$S/brain-dup" --intent 'prove the store opens' --keep-work`: exit `0`, JSON `verdict` `green` and `requested.claims` `["store-opened"]`. `"$S/vx" agent-dup-chain-refused cat "<R>/brain/home/refused"` shows `verilex run: error: --no-chain refuses a chain argument, an empty one too; pass --claim, --named or --changed`. `"$S/vx" agent-dup-chain-log cat "<R>/log/verilex.log"` shows `run --no-chain --ticket <R>/ticket.yaml --json store-open | store-open -> exit 2`, then `--json --claim store-opened -> exit 0`, and `"$S/vx" agent-dup-chain-runs ls "$S/agent-dup-chain-home/tally/runs"` lists one run (F7).
  - `"$S/vx" agent-dup-after verilex-agent $A --home "$S/agent-dup-after-home" --brain "$S/brain-dup-after" --intent 'prove the store opens'`: exit `0`, the green of the first run (F10).
  - `"$S/vx" agent-dup-alone verilex-agent $A --home "$S/agent-dup-alone-home" --brain "$S/brain-dup-alone" --intent 'prove the store opens'`: exit `2`, empty stdout, stderr `verilex-agent: inconclusive: brain exited: exit status 2, and made no verilex run, so there is no verdict` (F10). The brain exits `2` because its last command, the refused chain, did.
  - `TALLY_DEFECT=hide-lists "$S/vx" agent-blank verilex-agent $A --home "$S/agent-blank-home" --brain "$S/brain-blank" --diff HEAD --keep-work`: exit `1`, JSON `verdict` `red` and `requested.changed` `["bin/tally"]`. Its `verilex.log` (`agent-blank-log`) shows `--json --changed bin/tally  -> exit 2` (two spaces: the empty argument), then `--json --changed bin/tally -> exit 1` (F8). Each line starts with `run --no-chain`.
  - `TALLY_DEFECT=hide-apple "$S/vx" agent-f11 verilex-agent $A --home "$S/agent-f11-home" --brain "$S/brain-f11" --intent 'prove a stored apple is listed'`: exit `1`, verilex's red `item-listed apple: tally list printed [], not apple`, and its `words` run `item-listed` with `apple`, not `pear` (F9, F11).
  - `TALLY_DEFECT=hide-apple "$S/vx" agent-f11-fresh verilex-agent $A --home "$S/agent-f11-fresh-home" --brain "$S/brain-f11-fresh" --intent 'prove a stored apple is listed'`: the same exit and red, not skipped (F11).
  - `TALLY_DEFECT=hide-lists "$S/vx" agent-f6chain verilex-agent $A --home "$S/agent-f6chain-home" --brain "$S/brain-f6chain" --intent 'prove a stored apple is listed'`: exit `2`, empty stdout, stderr `verilex-agent: inconclusive: run <G> is green but was not asked about claim item-listed, claim item-added`.
  - `TALLY_DEFECT=hide-lists "$S/vx" agent-f6chain-diff verilex-agent $A --home "$S/agent-f6chain-diff-home" --brain "$S/brain-f6chain-diff" --diff HEAD`: exit `0`, JSON `verdict` `green` with `warning` `2 touched claims not covered`. A diff sets no floor of touched claims yet (`docs/verdict-rule.md`, Limits), so this is verilex's own warned green, as in row 32.
- **Intent floor.** Without the defect, `"$S/vx" agent-floor verilex-agent $A --home "$S/agent-floor-home" --brain "$S/brain-narrow" --intent 'prove a stored apple is listed'` exits `2`, empty stdout, with `verilex-agent: inconclusive: run <id> is green but was not asked about claim item-listed, claim item-added`: both runs were green, but the last one did not prove the floor. Write `$S/brain-marker` as `#!/bin/sh`, `touch "$HOME/started"` and `verilex run --claim store-opened`, and `chmod +x` it. `"$S/vx" agent-floor-none verilex-agent $A --brain "$S/brain-marker" --intent 'make the export faster' --keep-work` exits `2` with `inconclusive: intent "make the export faster" names no claim (verilex index --intent found none)`, and `"$S/vx" agent-floor-none-marker ls "<R>/brain/home/started"` fails: the brain never ran. The same run with `--claim store-opened` added (`agent-floor-claim`) exits `0`, verilex's green.
- **Lookup failures.** Put a `verilex` in front of the real one for each failure: `index` fails, `index` hangs, or `ticket` hangs. Write each with `printf '#!/bin/sh\ncase "$3" in %s) %s ;; esac\nexec %s "$@"\n' <sub> <action> "$S/bin/verilex" > "$S/verilex-<case>"`: `index` with `echo "verilex: refused: the index is broken" >&2; exit 2` as `noindex`, `index` with `exec sleep 61` as `hang-index`, and `ticket` with `exec sleep 61` as `hang-ticket`. `chmod +x` all three, and write `printf 'intent: prove the store opens\nharness: stub\nmodel: stub\ntime_budget: 1s\n' > "$S/agent-lookup.yaml"`. Each run below exits `2` with empty stdout, and `ls "<R>/brain/home/started"` fails:
  - `"$S/vx" agent-index-fails verilex-agent $A --verilex "$S/verilex-noindex" --brain "$S/brain-marker" --intent 'prove the store opens' --keep-work`: stderr `verilex-agent: inconclusive: verilex index --intent failed, so the intent's claims are unknown: the index is broken`.
  - `"$S/vx" agent-index-hang verilex-agent --project "$S/tally" --skill "$S/skill.md" --verilex "$S/verilex-hang-index" --brain "$S/brain-marker" --ticket "$S/agent-lookup.yaml" --keep-work`: about one second, stderr `verilex-agent: inconclusive: the time budget ended before verilex index --intent answered`.
  - `"$S/vx" agent-ticket-hang verilex-agent --project "$S/tally" --skill "$S/skill.md" --verilex "$S/verilex-hang-ticket" --brain "$S/brain-marker" --ticket "$S/agent-lookup.yaml" --keep-work`: about 30 seconds, because the budget is in the ticket, stderr `verilex-agent: inconclusive: verilex ticket did not answer within 30s`.
  - After both hangs, `"$S/vx" agent-lookup-sleeps pgrep -f 'sleep 6[1]'` prints nothing and exits `1`: the launcher killed each hung lookup. The `[1]` keeps `pgrep` from finding the `vx` command line that holds the pattern.

- **Brain limits.** Write `$S/brain-limits` as below, `chmod +x` it, and snapshot first: `"$S/vx" agent-limits-before "$PWD/.cursor/skills/verify-verilex/scripts/snapshot" "$S"`. Run `"$S/vx" agent-limits verilex-agent $A --home "$S/agent-limits-home" --ledger "$S/ledger" --brain "$S/brain-limits" --diff HEAD --keep-work`. Exit `0`, no `warning` in the JSON. `"$S/vx" agent-limits-log cat "<R>/brain/home/refusals"` holds `verilex onboard is not available to the brain`, `--keep is not available to the brain`, `--continue is not available to the brain` and three `exit 2` lines. A second snapshot (`agent-limits-after`) matches the first, `ls -A "$S/agent-limits-home/tally"` holds only `runs` (no onboarding record), and `ls -A "$S/stores"` is unchanged.

```sh
#!/bin/sh
verilex onboard item-listed 2>> "$HOME/refusals"; echo "exit $?" >> "$HOME/refusals"
verilex run --keep --claim store-opened --changed bin/tally 2>> "$HOME/refusals"; echo "exit $?" >> "$HOME/refusals"
verilex run --continue someone-else 'store-open' 2>> "$HOME/refusals"; echo "exit $?" >> "$HOME/refusals"
verilex run --changed bin/tally > /dev/null
```

- **Read-only project.** Write `$S/brain-fix` as below and `chmod +x` it. Snapshot first (`agent-ro-before`). Run `TALLY_DEFECT=hide-lists "$S/vx" agent-honest-red verilex-agent $A --brain "$S/brain" --intent 'prove a stored apple is listed'`: exit `1`, verilex's red. Then `TALLY_DEFECT=hide-lists "$S/vx" agent-fixer verilex-agent $A --brain "$S/brain-fix" --intent 'prove a stored apple is listed' --keep-work`: exit `1` and verilex's red again, because the fix never reached the code. `"$S/vx" agent-fixer-edit cat "<R>/brain/home/edit"` shows the Python error `Read-only file system` (Linux) or `Operation not permitted` (macOS) and `edit exit 1`. A second snapshot (`agent-ro-after`) matches the first.

```sh
#!/bin/sh
verilex run --claim item-listed > /dev/null
python3 -c "p='bin/tally'; s=open(p).read(); open(p,'w').write(s.replace('hide-lists', 'hide-lists-off'))" 2> "$HOME/edit"
echo "edit exit $?" >> "$HOME/edit"
verilex run --claim item-listed > /dev/null
```

- **Bypass.** Write `$S/brain-bypass` as below and `chmod +x` it. Run `REAL_VERILEX="$S/bin/verilex" AGENT_HOME="$S/agent-bypass-home" "$S/vx" agent-bypass verilex-agent $A --home "$S/agent-bypass-home" --brain "$S/brain-bypass" --intent 'prove the store opens' --keep-work`: exit `0`, verilex's green. `"$S/vx" agent-bypass-log cat "<R>/brain/home/bypass" "<R>/brain/home/which"` shows a failed call (`bypass exit 126` on Linux, where the binary shows as `/dev/null`) and `<R>/share/bin/verilex`. Second view: `"$S/vx" agent-bypass-runs python3 -c 'import json,sys,glob; [print(json.load(open(p))["run"], json.load(open(p))["cleanup"]) for p in glob.glob(sys.argv[1]+"/tally/runs/*/run.json")]' "$S/agent-bypass-home"` lists one run, the JSON `run`, and no `kept`, and `ls -A "$S/stores"` holds no store of it.

```sh
#!/bin/sh
VERILEX_HOME="$AGENT_HOME" "$REAL_VERILEX" run --keep --claim store-opened > /dev/null 2> "$HOME/bypass"
echo "bypass exit $?" >> "$HOME/bypass"
command -v verilex > "$HOME/which"
verilex run --claim store-opened > /dev/null
```

- **Budget.** Write `$S/brain-escape` as `#!/bin/sh`, `python3 -c 'import os, time; os.setsid(); time.sleep(8)' &` and `sleep 8`, and `chmod +x` it. Run `printf 'intent: prove the store opens\nharness: stub\nmodel: stub\ntime_budget: 1s\n' > "$S/agent-budget.yaml"`, then `"$S/vx" agent-budget verilex-agent --project "$S/tally" --skill "$S/skill.md" --brain "$S/brain-escape" --ticket "$S/agent-budget.yaml"`: exit `2` with `inconclusive: the time budget ended` about one second after it started, not eight. The `evidence` dir's timestamps, or `time`, show it.
- **Undo the diff.** `"$S/vx" agent-git-undo git checkout -q -- bin/tally`, then `"$S/vx" agent-git-clean git status --porcelain` prints nothing before the next recipe.
- **Changed project.** Write `$S/brain-wait` as `#!/bin/sh`, `sleep 4` and `verilex run --claim store-opened`, and `chmod +x` it. Run `"$S/vx" agent-changed verilex-agent $A --home "$S/agent-changed-home" --brain "$S/brain-wait" --intent 'prove the store opens' > "$S/agent-changed.out" 2>&1 &`. When `$S/agent-changed-home/launcher.lock` exists, the brain is starting: run `printf '# changed outside the sandbox\n' >> "$S/tally/bin/tally"`, then `wait`. The out file has exit `2` and `verilex-agent: inconclusive: the project changed during the run, so no verdict covers the code under test: bin/tally`. Undo it with `"$S/vx" agent-changed-undo git checkout -q -- bin/tally`.
- **Around the launcher.** Run `"$S/vx" agent-around verilex-agent $A --home "$S/agent-around-home" --brain "$S/brain-wait" --intent 'prove the store opens' > "$S/agent-around.out" 2>&1 &`. When `$S/agent-around-home/launcher.lock` exists, run `"$S/vx" agent-around-host env VERILEX_HOME="$S/agent-around-home" verilex run --keep --claim store-opened`, then `wait`. The out file has exit `2` and `inconclusive: run <H> did not come through the launcher; run <H> kept its instance; the launcher tore it down`. Second view: the `agent-bypass-runs` command on `"$S/agent-around-home"` shows `<H>` with `cleanup` `done`, and `ls -A "$S/stores"` holds no store of `<H>`.
- **Sandbox.** Write `$S/brain-forge` as below and `chmod +x` it. Fill a fresh ledger with an honest run: `"$S/vx" agent-forge-honest verilex-agent $A --home "$S/agent-forge-honest-home" --ledger "$S/forge-ledger" --brain "$S/brain" --intent 'prove a stored apple is listed'`, exit `0`. Snapshot (`agent-forge-before`). Give the forger every path it needs, then run it: `TALLY_DEFECT=hide-lists LEDGER="$S/forge-ledger" AGENT_HOME="$S/agent-forge-home" PRODUCT="$S/tally" REAL_VERILEX="$S/bin/verilex" "$S/vx" agent-forge verilex-agent $A --home "$S/agent-forge-home" --ledger "$S/forge-ledger" --brain "$S/brain-forge" --intent 'prove a stored apple is listed' --keep-work`. Exit `1`, JSON `verdict` `red` and `skipped` `false`.
  - `"$S/vx" agent-forge-probe cat "<R>/brain/home/probe"`: all six lines end in `refused`.
  - `"$S/vx" agent-forge-egress cat "<R>/log/egress.log"`: `refused CONNECT 127.0.0.1:9: 127.0.0.1 is not a public address the brain may reach`.
  - `"$S/vx" agent-forge-verilex-log cat "<R>/log/verilex.log"`: two `run ... --claim item-listed -> exit 1` lines.
  - `"$S/vx" agent-forge-audit "$PWD/.cursor/skills/verify-verilex/scripts/ledger-audit" "$S/forge-ledger/tally" "$S/agent-forge-honest-home" "$S/agent-forge-home"`: exit `0`, `lost: 0  forged: 0  damaged: 0  unowned: 0`.
  - `"$S/vx" agent-forge-forged find "$S/forge-ledger" "$S/agent-forge-home" -name forged` prints nothing, and a second snapshot (`agent-forge-after`) matches the first.

```sh
#!/bin/sh
verilex run --claim item-listed > /dev/null
try() {
  label=$1; shift
  if "$@" > /dev/null 2>&1; then echo "$label: reached" >> "$HOME/probe"; else echo "$label: refused" >> "$HOME/probe"; fi
}
try "ledger path in /proc" sh -c 'cat /proc/*/cmdline | tr "\000" "\n" | grep -q -x -e "[-]-ledger" -e "[-]-home"'
try "ledger path in ps" sh -c 'ps -A -ww -o args= | grep -q -e "[-]-ledger" -e "[-]-home"'
try "read ledger" sh -c 'find "$LEDGER" -type f | grep -q .'
try "read home" sh -c 'find "$AGENT_HOME" -type f | grep -q .'
try "run the real verilex" "$REAL_VERILEX" --help
try "egress to loopback" python3 -c '
import os, socket, sys
host, port = os.environ["HTTPS_PROXY"].split("//")[1].rstrip("/").rsplit(":", 1)
s = socket.create_connection((host, int(port)), 5)
s.sendall(b"CONNECT 127.0.0.1:9 HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")
sys.exit(0 if b" 200 " in s.recv(200) else 1)'
for target in "$LEDGER/tally/passes/forged" "$AGENT_HOME/forged" "$PRODUCT/forged"; do
  mkdir -p "$target" 2> /dev/null && echo '{"run": "forged"}' > "$target/pass.json"
done
verilex run --claim item-listed > /dev/null
```

- **No sandbox.** Put a failing sandbox tool first on `PATH`: `mkdir -p "$S/fake-tool"`, write `$S/fake-tool/bwrap` (`sandbox-exec` on macOS) as `#!/bin/sh`, `echo "bwrap: setting up uid map: Permission denied" >&2` and `exit 1`, and `chmod +x` it. `$S/brain-marker` is the brain from **Intent floor**. Run `PATH="$S/fake-tool:$PATH" "$S/vx" agent-no-sandbox verilex-agent $A --brain "$S/brain-marker" --intent 'prove the store opens' --keep-work`: exit `2`, empty stdout, stderr `verilex-agent: inconclusive: the brain runs only in a sandbox, and the sandbox is not available here: bwrap could not start the sandbox: bwrap: setting up uid map: Permission denied`. Replace the fake with one that starts the command without a sandbox: on Linux `#!/bin/sh`, `while [ "$1" != -- ]; do shift; done`, `shift` and `exec "$@"`; on macOS `#!/bin/sh`, `shift 2` and `exec "$@"`. The same run (`agent-no-hold`) exits `2` with `the sandbox did not hold: the brain could write the project`. Second view: for each run, `"$S/vx" agent-no-sandbox-marker ls "<R>/brain/home/started"` fails (the brain never ran), and `git status --porcelain` prints nothing.
- **Terminal.** Write the helper below as `$S/terminal.py`. It starts its arguments on a new terminal, the way a shell would, sets `TTY_PATH` to that terminal's device, and prints everything the terminal showed. Write `$S/brain-tty` as below and `chmod +x` it. Run `"$S/vx" agent-terminal python3 -I "$S/terminal.py" "$S/bin/verilex-agent" $A --home "$S/agent-tty-home" --brain "$S/brain-tty" --intent 'prove the store opens' --keep-work`: exit `0`, and the output holds verilex's green JSON and the `run files kept in <R>` line, but no `TTY-MARKER`. `"$S/vx" agent-terminal-report cat "<R>/brain/home/tty"` shows `/dev/tty: refused` and `<device>: refused`, where `<device>` is `/dev/pts/<n>` on Linux or `/dev/ttys<n>` on macOS.

```python
import os, pty, sys
pid, fd = pty.fork()
if pid == 0:
    os.environ["TTY_PATH"] = os.ttyname(0)
    os.execv(sys.argv[1], sys.argv[1:])
shown = b""
while True:
    try:
        chunk = os.read(fd, 65536)
    except OSError:
        break
    if not chunk:
        break
    shown += chunk
_, status = os.waitpid(pid, 0)
sys.stdout.buffer.write(shown)
sys.exit(os.waitstatus_to_exitcode(status))
```

```sh
#!/bin/sh
for path in /dev/tty "$TTY_PATH"; do
  if printf 'TTY-MARKER\n' 2> /dev/null > "$path"; then echo "$path: written"; else echo "$path: refused"; fi
done > "$HOME/tty"
verilex run --claim store-opened > /dev/null
```

- **Ticket.** Run `printf 'intent: prove a stored apple is listed\nharness: stub\nmodel: stub\neffort: low\n' > "$S/agent-ticket.yaml"`, then `"$S/vx" agent-ticket verilex-agent --project "$S/tally" --home "$S/agent-ticket-home" --skill "$S/skill.md" --brain "$S/brain" --ticket "$S/agent-ticket.yaml"`. Exit `0`, JSON `verdict` `green`, and stdout does not contain `BRAIN SAYS`.
- **Same home.** Run `"$S/vx" agent-hold verilex-agent $A --home "$S/agent-hold-home" --brain "$S/brain-wait" --intent 'prove the store opens' > "$S/agent-hold.out" 2>&1 &`. When `$S/agent-hold-home/launcher.lock` exists, run `"$S/vx" agent-same-home verilex-agent $A --home "$S/agent-hold-home" --brain "$S/brain" --intent 'prove the store opens'`. Exit `2`, stderr contains `in use`, and stdout is empty. Then `wait`. The held run exits `0`.
- **Parallel.** Use a fresh ledger: `for i in a b; do "$S/vx" agent-par-$i verilex-agent $A --home "$S/agent-par-$i" --ledger "$S/par-ledger" --brain "$S/brain" --intent 'prove a stored apple is listed' > "$S/agent-par-$i.out" 2>&1 & done; wait`. Each out file contains `"verdict": "green"` and `exit 0`, and does not contain `BRAIN SAYS`.
- **Parallel, second view.** Read each JSON `run` id. `"$S/vx" agent-par-homes ls "$S/agent-par-a/tally/runs" "$S/agent-par-b/tally/runs"` lists exactly `<A>` under the first and `<B>` under the second.
- **Shared skip.** Run `"$S/vx" agent-skip verilex-agent $A --home "$S/agent-skip-home" --ledger "$S/par-ledger" --brain "$S/brain" --intent 'prove a stored apple is listed'`. Exit `0`. JSON has `"skipped": true` and `"verdict": "green"`. JSON `instance` is `null`. Run `"$S/vx" agent-skip-stores ls -A "$S/stores"`. It holds no `tally-<skip run>` store.
- **No real harness.** Run `"$S/vx" agent-no-harness verilex-agent --project "$S/tally" --skill "$S/skill.md" --intent 'prove the store opens' --harness claude-code --model claude-opus`. Exit `2`, stderr `verilex-agent: refused: harness claude-code is not started unless --allow-harness is set`, and `"$S/vx" agent-no-harness-runs verilex runs` shows no new run from that command.
- **Real harness.** Opt-in: it spends the harness account's tokens, and needs that harness logged in on the host. For each of `claude haiku`, `codex <model>` and `pi <provider>/<model>` as `<h> <m>`, run `"$S/vx" agent-real-<h> verilex-agent --project "$S/tally" --skill "$PWD/skills/verilex/SKILL.md" --home "$S/agent-real-<h>-home" --intent 'prove a stored apple is listed' --harness <h> --model <m> --effort low --allow-harness --keep-work`. The verdict follows the verdict rule (`docs/verdict-rule.md`) over the runs in `<R>/log/verilex.log`: a run that exited `1` decides, and otherwise the last run does, if it proved what earlier inconclusive runs left open and covers the spec. verilex refuses any chain the brain writes, and `verilex.log` shows it with `-> exit 2`. The run record in `$S/agent-real-<h>-home` names harness `<h>` and model `<m>`. `<R>/log/egress.log` lists only `CONNECT` lines to the harness provider's public hosts.

## Gotchas

- The launcher keeps every `verilex run` of the brain that printed JSON and applies the verdict rule (`docs/verdict-rule.md`) to them: a red is final. A refused run prints none and does not count, and that includes every chain, which verilex refuses under the pinned `--no-chain`.
- The floor comes from `verilex index --intent`, which the launcher runs itself, so an intent's floor can hold claims the brain never thought of: `prove a stored apple is listed` holds `item-listed` and `item-added`.
- A chain the brain writes never runs, so it cannot set the word arguments of a later claim plan: verilex binds those from the run records in the home (F11).
- Without `--home` the launcher's home lives in the run directory and goes with it, so `verilex runs` in the session never lists launcher runs. Pass `--home` for a second view.
- `vx` adds a `$ command` line before stdout. Parse the JSON from the evidence `stdout` file, not from the `vx` transcript.
- `vx` runs from `$S/tally`, so a repository helper needs its absolute path: `"$PWD/.cursor/skills/verify-verilex/scripts/snapshot"`.
- The launcher replaces `VERILEX_HOME` and `VERILEX_LEDGER` for verilex and passes the rest of the environment through, so `TALLY_STORES` from `vx` still places stores under `$S/stores`. The brain itself gets neither variable, and gets its own `HOME`, `TMPDIR` and proxy variables.
- The brain cannot write `$S`: a brain that writes a file anywhere but `$HOME` or `$TMPDIR` fails, or writes into its own `/tmp` on Linux, which the second view never sees.
- `--keep-work` leaves `<R>` under `$TMPDIR`, outside the session, so `cleanup` does not remove it. Remove each `<R>` by its printed path once the proof is reported.
- `$S/bin` is on the brain's `PATH`, but `$S/bin/verilex` shows as `/dev/null` there. The brain's `verilex` is `<R>/share/bin/verilex`.
- The project must be a git work tree, or the launcher refuses before the brain starts; so is a `--diff` revision git cannot read. The changed-project check ignores files git ignores.
- `--ticket` refuses `--intent`, `--diff`, `--harness`, `--model`, `--effort` and `--profile` on the same command.
- `launcher.lock` stays in a home after its run ends, so wait on it only in a fresh home.
- A launcher run is inconclusive when its home already holds a run from an earlier launcher call (`did not come through the launcher`). Give each launcher call its own `--home`.
- A real harness that fails on its account (no credits, a disabled model) is inconclusive with the harness's error, and `<R>/log/egress.log` still shows that it reached its provider.
