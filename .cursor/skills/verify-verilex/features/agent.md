# Launcher

`verilex-agent` runs one verification by starting the brain named in the run spec and printing the JSON `verilex run` produced. The brain's own message is not the verdict. Each run has its own state home. Runs that share `--ledger` keep skip savings. A second run that reuses a home still in use is refused.

## Sub-features

- `agent-verdict` prints verilex's JSON verdict, not the text the brain wrote, and the run record in that run's home matches the run id and verdict.
- `agent-skill` hands the brain the skill file and the intent. With a diff and no intent, the intent line is `prove nothing this change touched broke`.
- `agent-ticket` accepts a ticket file instead of intent flags, and still returns verilex's JSON.
- `agent-same-home` refuses a second run whose `--home` is still in use, exit `2`, with no second verdict.
- `agent-parallel` runs two verilex-agent processes at once, each with its own home and the same ledger. Each home holds only its own run. A later run on a new home skips and launches no store.
- `agent-no-harness` does not start a real harness unless `--allow-harness` is set.

## How to get to it (user POV)

- Build `verilex-agent` from `./agent/cmd/verilex-agent` and put it on `PATH`.
- Pass `--ticket`, or `--intent` or `--diff`, plus `--brain` for a stub executable or `--allow-harness` for a configured harness.
- Read the JSON verdict on stdout. Exit `0` is green, `1` is red, `2` is inconclusive or refused.

## Driving it with vx

Preconditions:

- A fresh session that passes the doctor. Build the launcher into the session bin: `go build -o "$S/bin/verilex-agent" ./agent/cmd/verilex-agent` from the repository root.
- `C` is `store-open | item-stored apple | item-listed apple`.
- Write the skill and the brain, then `chmod +x "$S/brain"`:

```sh
printf '%s\n' 'SKILL-MARKER' > "$S/skill.md"
cat > "$S/brain" << 'EOF'
#!/bin/sh
printf '%s\n' 'BRAIN SAYS GREEN'
if [ -n "${PROMPT_COPY:-}" ]; then cp "$VERILEX_AGENT_PROMPT" "$PROMPT_COPY"; fi
verilex run --json 'store-open | item-stored apple | item-listed apple'
EOF
```

- **Verdict.** Run `PROMPT_COPY="$S/seen-prompt" "$S/vx" agent-verdict verilex-agent --project "$S/tally" --home "$S/agent-home" --skill "$S/skill.md" --brain "$S/brain" --intent 'prove a stored apple is listed' --harness stub --model stub --effort low`. Exit `0`. Stdout is one JSON object whose `verdict` is `green` and whose `run` is `<A>`. It does not contain `BRAIN SAYS`.
- **Verdict, second view.** Run `"$S/vx" agent-verdict-record python3 -c 'import json,sys; r=json.load(open(sys.argv[1])); print(r["verdict"], r["run"])' "$S/agent-home/tally/runs/<A>/run.json"`. It prints `green <A>`.
- **Skill.** Run `"$S/vx" agent-skill cat "$S/seen-prompt"`. The file contains `SKILL-MARKER`, `intent: prove a stored apple is listed` and `The launcher ignores your message and returns verilex's own JSON verdict.`
- **Default intent.** Write a brain that only copies the prompt, `cat > "$S/brain-intent" << 'EOF'` then the two lines `#!/bin/sh` and `cp "$VERILEX_AGENT_PROMPT" "$PROMPT_COPY"`, and `chmod +x "$S/brain-intent"`. Run `PROMPT_COPY="$S/seen-default" "$S/vx" agent-default verilex-agent --project "$S/tally" --home "$S/agent-default-home" --skill "$S/skill.md" --brain "$S/brain-intent" --diff HEAD --harness stub --model stub`. Exit `2` (no verilex run). Run `"$S/vx" agent-default-prompt cat "$S/seen-default"`. It contains `intent: prove nothing this change touched broke` and `diff: HEAD`.
- **Ticket.** Run `printf 'intent: prove a stored apple is listed\nharness: stub\nmodel: stub\neffort: low\n' > "$S/agent-ticket.yaml"`, then `"$S/vx" agent-ticket verilex-agent --project "$S/tally" --home "$S/agent-ticket-home" --skill "$S/skill.md" --brain "$S/brain" --ticket "$S/agent-ticket.yaml"`. Exit `0`, JSON `verdict` `green`, and stdout does not contain `BRAIN SAYS`.
- **Same home.** Write `$S/brain-hold` as below and `chmod +x` it. `HOLD` is `$S/hold-flag` and `RELEASE` is `$S/release-flag`.

```sh
#!/bin/sh
touch "$HOLD"
while [ ! -f "$RELEASE" ]; do sleep 0.05; done
verilex run --json 'store-open'
```

  Run `HOLD="$S/hold-flag" RELEASE="$S/release-flag" "$S/vx" agent-hold verilex-agent --project "$S/tally" --home "$S/agent-hold-home" --skill "$S/skill.md" --brain "$S/brain-hold" --intent 'prove the store opens' --harness stub --model stub > "$S/agent-hold.out" 2>&1 &`. When `$S/hold-flag` exists, run `"$S/vx" agent-same-home verilex-agent --project "$S/tally" --home "$S/agent-hold-home" --skill "$S/skill.md" --brain "$S/brain" --intent 'prove the store opens' --harness stub --model stub`. Exit `2`, stderr contains `in use`, and stdout has no `verdict`. Then `touch "$S/release-flag"` and `wait`. The held run exits `0`.
- **Parallel.** Run `admit-all` first. Then `for i in a b; do "$S/vx" agent-par-$i verilex-agent --project "$S/tally" --home "$S/agent-par-$i" --ledger "$S/ledger" --skill "$S/skill.md" --brain "$S/brain" --intent 'prove a stored apple is listed' --harness stub --model stub > "$S/agent-par-$i.out" 2>&1 & done; wait`. Each out file contains `"verdict": "green"` and `exit 0`, and does not contain `BRAIN SAYS`.
- **Parallel, second view.** Read each JSON `run` id. `"$S/vx" agent-par-homes python3 -c 'import os,sys; a,b=sys.argv[1:3]; print(any(os.path.basename(p)==b for p in os.listdir(a)) if os.path.isdir(a) else False)' "$S/agent-par-a/tally/runs" "<B>"` prints `False`, and the same with the homes swapped prints `False`.
- **Shared skip.** Run `"$S/vx" agent-skip verilex-agent --project "$S/tally" --home "$S/agent-skip-home" --ledger "$S/ledger" --skill "$S/skill.md" --brain "$S/brain" --intent 'prove a stored apple is listed' --harness stub --model stub`. Exit `0`. JSON has `"skipped": true` and `"verdict": "green"`. Run `"$S/vx" agent-skip-stores ls -A "$S/agent-skip-home/stores"`. The directory is empty.
- **No real harness.** Run `"$S/vx" agent-no-harness verilex-agent --project "$S/tally" --skill "$S/skill.md" --intent 'prove the store opens' --harness claude-code --model claude-opus`. Exit `2`, stderr contains `--allow-harness`, and `"$S/vx" agent-no-harness-runs verilex runs` shows no new run from that command.

## Gotchas

- The launcher's stdout is verilex's JSON. A missed-claim warning is the `warning` and `uncovered` fields verilex prints. This checkout's core does not print them yet; do not treat a green without them as proof that coverage was checked.
- `vx` adds a `$ command` line before stdout. Parse the JSON from the evidence `stdout` file, not from the `vx` transcript.
- Cleanup removes a live run's store. Isolation is the store path recorded under that run's home, and the other home lacking that run id.
- `--ticket` refuses `--intent`, `--diff`, `--harness`, `--model`, `--effort` and `--profile` on the same command.
- The held run must be released with `touch "$S/release-flag"` or it sleeps until the shell ends.
