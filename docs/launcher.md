# Companion launcher

`verilex-agent` is a separate module in `agent/`. The core does not import it and does not start a harness. The launcher reads the run spec through `verilex ticket`, starts the brain that spec names with the verilex skill and the intent, and prints the JSON of one `verilex run` the brain made, chosen by the [verdict rule](verdict-rule.md). That JSON is verilex's own verdict, byte for byte, including its missed-claim warning (`warning`, `uncovered`, `unmapped`, `unclaimed`, `unrun`). The brain's message is not the verdict. A real harness starts only with `--allow-harness`. A named executable (`--brain`) is how tests and bots supply a brain without one.

## Usage

```
go build -o verilex-agent ./agent/cmd/verilex-agent
verilex-agent --ticket deep.yaml --project . --skill skills/verilex/SKILL.md
verilex-agent --diff main...HEAD --claim item-listed --harness claude-code --model <model> --allow-harness
```

## What the brain may run

The project must be in a git work tree. The brain's `verilex` command is the launcher's, and the only one its sandbox shows: it may run `index`, `plan`, `run`, `words`, `claims`, `runs`, `ticket` and `check`, never with `--keep` or `--continue`. Every `run` and `plan` carries the run spec as `--ticket`, so the run record names the brain, and `--no-chain`, so verilex refuses a chain that the brain writes.

With `--suggest`, the brain is first asked, with the launcher's `verilex` blocked, for extra claims; they reach the run prompt as suggestions, never as a verdict or a ceiling. Each run gets its own state home, so two runs do not share a mutable instance. Pass the same `--ledger` to keep skip savings. A second run that reuses a home still in use is refused.
