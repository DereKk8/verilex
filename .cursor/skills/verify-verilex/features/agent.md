# Launcher

`verilex-agent` runs one verification by starting the brain named in the run spec and printing the JSON of the brain's last `verilex run`, byte for byte. The brain's own message is not the verdict. A green is returned only from a claim run (`verilex-claim-run-1`) whose `requested` holds every `--claim` given to the launcher and every path the diff changes; otherwise the launcher is inconclusive, exit `2`, with nothing on stdout. The brain runs in a sandbox: it reads the project read-only, writes only its own run directory, and reaches neither the verilex home, the ledger, the real `verilex` binary nor the host's network. Where the sandbox cannot start or does not hold, the brain does not run and the launcher is inconclusive. Each run has its own state home. Runs that share `--ledger` keep skip savings. A second run that reuses a home still in use is refused.

## Sub-features

- `agent-verdict` prints verilex's JSON verdict, not the text the brain wrote, and the run record in that run's home matches the run id and verdict and carries the run spec's brain as `ticket`.
- `agent-skill` hands the brain the skill file, the intent and each changed path, on stdin and in the file `VERILEX_AGENT_PROMPT` names. With a diff and no intent, the intent line is `prove nothing this change touched broke`.
- `agent-missed-claim` returns verilex's own missed-claim warning: a brain that derives one claim from a diff touching three gets verilex's green with `warning` `2 touched claims not covered` and both `uncovered` claims, and `--text` lists them.
- `agent-uncovered-spec` is inconclusive, exit `2` and no stdout, when the green run left out a changed path, left out a launcher `--claim`, or was a chain run that was given no diff.
- `agent-read-only-project` keeps the project as it is: a brain's edit fails, the launcher returns verilex's red for the code under test, and the project is unchanged.
- `agent-changed-project` is inconclusive, exit `2`, when something outside the sandbox changes the project while the brain runs.
- `agent-bypass` keeps the real `verilex` out of the brain's reach: its call fails, its `verilex` is the launcher's relay, and the home holds only the run that came through the launcher.
- `agent-around` is inconclusive, exit `2`, when a run that did not come through the launcher appears in its home during the run, and tears down the instance that run kept.
- `agent-sandbox` stops the tester's F2 forger (P11): the brain finds no ledger path in `/proc` or `ps`, cannot read the ledger or the home, cannot run the real `verilex`, gets a refusal from the egress proxy for loopback, and writes nothing to the ledger, the home or the project. Its rerun stays verilex's red, and `ledger-audit` finds no forged pass.
- `agent-no-sandbox` is inconclusive, exit `2`, and never starts the brain, when the sandbox tool fails or starts the brain without a sandbox.
- `agent-budget` ends the run at `time_budget` even when the brain started a child in its own session.
- `agent-brain-limits` refuses, exit `2`, a brain's `onboard`, `run --keep` and `run --continue`, and nothing they would change changes.
- `agent-ticket` accepts a ticket file instead of intent flags, and still returns verilex's JSON.
- `agent-same-home` refuses a second run whose `--home` is still in use, exit `2`, with no second verdict.
- `agent-parallel` runs two verilex-agent processes at once, each with its own home and the same ledger. Each home holds only its own run. A later run on a new home skips and launches no store.
- `agent-no-harness` does not start a real harness unless `--allow-harness` is set.
- `agent-real-harness` drives a real claude, codex or pi brain in the sandbox with only its credential file, and returns the verdict of the verilex run that brain made.

## How to get to it (user POV)

- Build `verilex-agent` from `./agent/cmd/verilex-agent` and put it on `PATH`. On Linux, install `bwrap` and allow it user namespaces (README, Sandbox). macOS uses the built-in `sandbox-exec`.
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
- **Skill.** Run `"$S/vx" agent-skill cat "<R>/brain/home/prompt"`. The file contains `SKILL-MARKER`, `intent: prove a stored apple is listed`, `The launcher returns verilex's own JSON verdict from your last verilex run and ignores your message.` and, last, a `# Task` section that tells the brain to prove the intent now.
- **A diff.** Change one file every claim depends on: `printf '# changed\n' >> "$S/tally/bin/tally"`. Then `"$S/vx" agent-git-diff git diff --name-only` prints `bin/tally`.
- **Default intent.** Write a brain that only copies the prompt, `cat > "$S/brain-intent" << 'EOF'` then the two lines `#!/bin/sh` and `cp "$VERILEX_AGENT_PROMPT" "$HOME/prompt"`, and `chmod +x "$S/brain-intent"`. Run `"$S/vx" agent-default verilex-agent $A --home "$S/agent-default-home" --brain "$S/brain-intent" --diff HEAD --keep-work`. Exit `2`, stderr `verilex-agent: inconclusive: the brain exited without a verilex run`, empty stdout. Run `"$S/vx" agent-default-prompt cat "<R>/brain/home/prompt"`. It contains `intent: prove nothing this change touched broke`, `diff: HEAD` and `changed: bin/tally`.
- **Missed claim.** Run `"$S/vx" agent-missed verilex-agent $A --home "$S/agent-missed-home" --ledger "$S/ledger" --brain "$S/brain-one" --intent 'prove the store opens' --diff HEAD`. Exit `0`. JSON `verdict` `green`, `warning` `2 touched claims not covered`, `uncovered` claims `item-added` and `item-listed`, each with a `next` command; `requested.changed` is `["bin/tally"]`. No `BRAIN SAYS`.
- **Missed claim, second view.** `"$S/vx" agent-missed-record python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); print(r["warning"]); print(*[u["claim"] for u in r["uncovered"]])' "$S/agent-missed-home/tally/runs/<M>/run.json"` prints the same warning and `item-added item-listed`. `"$S/vx" agent-missed-text verilex-agent $A --ledger "$S/ledger" --brain "$S/brain-one" --intent 'prove the store opens' --diff HEAD --text` prints `green; run <id>...; 2 touched claims not covered` and one `uncovered` line per claim.
- **Uncovered spec.** Each of these exits `2`, prints nothing on stdout, and says `verilex-agent: inconclusive: run <id> is green but` with the reason. Write `$S/brain-<case>` as `#!/bin/sh` plus the one `verilex run` line, `chmod +x`, and run `"$S/vx" agent-<case> verilex-agent $A --home "$S/agent-<case>-home" --ledger "$S/ledger" --brain "$S/brain-<case>" --intent 'prove the store opens' --diff HEAD`:
  - `nodiff`: `verilex run --claim store-opened` → `was not asked about change bin/tally`.
  - `nofloor`: `verilex run --claim store-opened --changed bin/tally`, with `--claim item-listed` added to the launcher → `was not asked about claim item-listed`.
  - `chain`: `verilex run 'store-open'` → `is not a claim run, so it does not show what it was asked`.
  - Second view: `"$S/vx" agent-uncovered-runs python3 -c 'import json,sys,glob; [print(p.split("/")[-5], json.load(open(p))["verdict"]) for a in sys.argv[1:] for p in glob.glob(a+"/tally/runs/*/run.json")]' "$S/agent-nodiff-home" "$S/agent-nofloor-home" "$S/agent-chain-home"` prints each home with `green`. verilex proved what it was asked; the launcher refused to call that the spec's verdict.
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

- **No sandbox.** Put a failing sandbox tool first on `PATH`: `mkdir -p "$S/fake-tool"`, write `$S/fake-tool/bwrap` (`sandbox-exec` on macOS) as `#!/bin/sh`, `echo "bwrap: setting up uid map: Permission denied" >&2` and `exit 1`, and `chmod +x` it. Write `$S/brain-marker` as `#!/bin/sh`, `touch "$MARKER"` and `verilex run --claim store-opened`. Run `MARKER="$S/marker" PATH="$S/fake-tool:$PATH" "$S/vx" agent-no-sandbox verilex-agent $A --brain "$S/brain-marker" --intent 'prove the store opens'`: exit `2`, empty stdout, stderr `verilex-agent: inconclusive: the brain runs only in a sandbox, and the sandbox is not available here: bwrap could not start the sandbox: bwrap: setting up uid map: Permission denied`. Replace the fake with one that starts the command without a sandbox: on Linux `#!/bin/sh`, `while [ "$1" != -- ]; do shift; done`, `shift` and `exec "$@"`; on macOS `#!/bin/sh`, `shift 2` and `exec "$@"`. The same run (`agent-no-hold`) exits `2` with `the sandbox did not hold: the brain could write the project`. Second view: `"$S/vx" agent-no-sandbox-marker ls "$S/marker"` fails (the brain never ran), and `git status --porcelain` prints nothing.
- **Ticket.** Run `printf 'intent: prove a stored apple is listed\nharness: stub\nmodel: stub\neffort: low\n' > "$S/agent-ticket.yaml"`, then `"$S/vx" agent-ticket verilex-agent --project "$S/tally" --home "$S/agent-ticket-home" --skill "$S/skill.md" --brain "$S/brain" --ticket "$S/agent-ticket.yaml"`. Exit `0`, JSON `verdict` `green`, and stdout does not contain `BRAIN SAYS`.
- **Same home.** Run `"$S/vx" agent-hold verilex-agent $A --home "$S/agent-hold-home" --brain "$S/brain-wait" --intent 'prove the store opens' > "$S/agent-hold.out" 2>&1 &`. When `$S/agent-hold-home/launcher.lock` exists, run `"$S/vx" agent-same-home verilex-agent $A --home "$S/agent-hold-home" --brain "$S/brain" --intent 'prove the store opens'`. Exit `2`, stderr contains `in use`, and stdout is empty. Then `wait`. The held run exits `0`.
- **Parallel.** Use a fresh ledger: `for i in a b; do "$S/vx" agent-par-$i verilex-agent $A --home "$S/agent-par-$i" --ledger "$S/par-ledger" --brain "$S/brain" --intent 'prove a stored apple is listed' > "$S/agent-par-$i.out" 2>&1 & done; wait`. Each out file contains `"verdict": "green"` and `exit 0`, and does not contain `BRAIN SAYS`.
- **Parallel, second view.** Read each JSON `run` id. `"$S/vx" agent-par-homes ls "$S/agent-par-a/tally/runs" "$S/agent-par-b/tally/runs"` lists exactly `<A>` under the first and `<B>` under the second.
- **Shared skip.** Run `"$S/vx" agent-skip verilex-agent $A --home "$S/agent-skip-home" --ledger "$S/par-ledger" --brain "$S/brain" --intent 'prove a stored apple is listed'`. Exit `0`. JSON has `"skipped": true` and `"verdict": "green"`. JSON `instance` is `null`. Run `"$S/vx" agent-skip-stores ls -A "$S/stores"`. It holds no `tally-<skip run>` store.
- **No real harness.** Run `"$S/vx" agent-no-harness verilex-agent --project "$S/tally" --skill "$S/skill.md" --intent 'prove the store opens' --harness claude-code --model claude-opus`. Exit `2`, stderr `verilex-agent: refused: harness claude-code is not started unless --allow-harness is set`, and `"$S/vx" agent-no-harness-runs verilex runs` shows no new run from that command.
- **Real harness.** Opt-in: it spends the harness account's tokens, and needs that harness logged in on the host. For each of `claude haiku`, `codex <model>` and `pi <provider>/<model>` as `<h> <m>`, run `"$S/vx" agent-real-<h> verilex-agent --project "$S/tally" --skill "$PWD/skills/verilex/SKILL.md" --home "$S/agent-real-<h>-home" --intent 'prove a stored apple is listed' --harness <h> --model <m> --effort low --allow-harness --keep-work`. The verdict is the JSON of the last verilex run in `<R>/log/verilex.log`, and the run record in `$S/agent-real-<h>-home` names harness `<h>` and model `<m>`. `<R>/log/egress.log` lists only `CONNECT` lines to the harness provider's public hosts.

## Gotchas

- The launcher's stdout is verilex's JSON from the brain's last `verilex run` that printed JSON. A refused run prints none and does not hide an earlier verdict.
- A chain run given `--changed` is a claim run too: verilex warns about the touched claims the chain did not prove, and the launcher returns that green with its warning.
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
