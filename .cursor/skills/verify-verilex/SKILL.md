---
name: verify-verilex
description: "Drive the verilex CLI as its users do, against a disposable copy of the tally sample product with an isolated VERILEX_HOME; use when proving verilex behavior (run, plan, --continue, words, claims, runs, cleanup, new, propose, admit, gap, check, the three verdicts and quiet output) on a fresh or a given verilex build."
---

# Verify verilex

verilex's user surface is its CLI. A user runs `verilex <command>` inside a product checkout that has a `.verilex/` directory. This skill gives that user a throwaway product: a copy of `tests/fixtures/tally/` (a made-up inventory CLI with its own words, frame and verify skill). Every proof drives the CLI on that copy, never Go internals, and depends on no verilex words, claims or harness. Read [the feature map](features/README.md) before choosing a path.

The binary under test is pinned at launch. A frozen known-good build can verify a development build by passing `--bin`. One build never verifies itself in that setup: whatever drives this skill is not the binary given to `launch`.

All commands below run from the repository root. `S` is the session directory that `launch` prints.

## Launch

```bash
.cursor/skills/verify-verilex/scripts/launch                     # build ./cmd/verilex from this checkout
.cursor/skills/verify-verilex/scripts/launch --bin /path/to/verilex   # pin a given binary instead
```

With `--bin`, a path that is not an executable file exits `2` and creates no session. Ready when it prints three lines: `session: <S>`, `binary: <source> (<sha256>)` and `evidence: <dir>`. Launch creates, under `${TMPDIR:-/tmp}`:

| Path | Holds |
|---|---|
| `$S/bin/verilex` | a copy of the binary under test (a later rebuild cannot change the session) |
| `$S/tally/` | the scratch product; every command runs from here |
| `$S/home/` | `VERILEX_HOME` for the session: runs, ledger, proposals |
| `$S/stores/` | tally stores (instances); `TALLY_STORES` points here |
| `$S/vx` | the session's capture helper (see Drive) |
| `$S/session`, `$S/owner` | session identity, binary source and sha256 |

Launch builds no server: verilex is a short-lived CLI, so each command is its own process. The scratch product needs `python3`; building needs `go`. Teardown is [Cleanup](#cleanup).

## Doctor

```bash
.cursor/skills/verify-verilex/scripts/doctor "$S"
```

Read-only. It prints `doctor: ok` and the binary source, sha256, `vcs.revision` and `vcs.modified`, kept store count and evidence path. It prints `doctor: FAIL <reason>` and exits 1 when the session is not ours, the binary changed since launch or does not answer `--help`, `VERILEX_HOME` is not inside the session, the product is not tally, `python3` is missing, or files under `~/.local/state/verilex/tally` changed since launch (something escaped `VERILEX_HOME`). Run it first, and again whenever a result looks off. Never drive a session the doctor fails or that this run did not launch.

## Drive

```bash
"$S/vx" <label> verilex <command> [args...]
"$S/vx" <label> <any read-only command>      # a second observation
```

`vx` runs the command from `$S/tally` with `VERILEX_HOME=$S/home`, `TALLY_STORES=$S/stores` and `$S/bin` first on `PATH`, so `verilex` is the pinned binary. Extra environment passes through: `TALLY_DEFECT=drop-adds "$S/vx" ...`. It prints the command, stdout, stderr (prefixed `stderr| `) and `exit N  evidence: <dir>`, and exits with the command's code. Labels are short kebab-case names for the proof step. Relative paths resolve in `$S/tally`, so pass absolute paths for anything else: verdict files, `--project`, and repository files as `"$PWD/tests/..."`. `vx` adds `$ command` and `exit N` lines to its stdout, so read `<evidence>/NNN-<label>/stdout` when a step needs to parse JSON. Write file commands in recipes as `command rm`, `command cp` and `command mv`: some interactive shells alias them to prompting forms (`rm -I`), which silently skip the change in a non-interactive call. The feature files hold the exact commands per feature.

## Evidence

Each `vx` call writes `${TMPDIR:-/tmp}/verify-verilex-evidence/<session id>/NN-<label>/` with `command`, `env` (any `TALLY_*` or `VERILEX_*` it saw), `stdout`, `stderr` and `exit`. `session` beside them names the binary and its sha256.

Proof standards:

- Drive the real user path: the `verilex` CLI in a product checkout. Never call Go packages or edit `$S/home` by hand to reach a state.
- Every proof is the action plus a second observation from another `vx` call: `verilex runs`, `verilex words`, `verilex plan`, a file under `$S/home` or `$S/tally`, or `ls "$TALLY_STORES"`. A verdict line alone is not proof.
- Check side effects alongside the printed line: run records, ledger skips, packets, `admission.json`, gap notes, stores created and removed.
- A refusal is proved by its stderr, exit `2`, and an unchanged second view (no new run in `verilex runs`, an unchanged store).
- verilex's own evidence paths (`$S/home/tally/runs/...`) vanish at cleanup. Copy what the proof needs first: `"$S/vx" <label> cat <path>`.
- Record the feature file and sub-feature id with each proof. Report a path you could not reach with its attempted command and unmet precondition; never report it as verified through another path.

## Cleanup

```bash
.cursor/skills/verify-verilex/scripts/cleanup "$S"
```

It tears down every run that `verilex runs` shows as `cleanup=kept` with `verilex cleanup <run>` (captured as evidence), then removes `$S` only when `$S/owner` matches the session. It kills no process: verilex leaves none running. It keeps the evidence directory and fails if that directory is gone. Delete `${TMPDIR:-/tmp}/verify-verilex-evidence/<session id>` yourself once the proof is reported.

## Traps

Each feature file lists its own gotchas. These traps cross features:

- Provisional words never skip, and `--continue` refuses a kept instance whose history holds one. Run `admit-all` first.
- The first run after an admission is live (`word changed`), because `admission.json` is part of every stamp.
- Only variables in a word's `env` enter its stamp. Probe frame-only variables such as `TALLY_ADOPT_STORE` with `--fresh`.
- `verilex plan` and `verilex check` exit `0` whatever they report. Read stdout.
- `verilex runs` sorts by run id, so two runs in the same second are out of time order. Use the id a command printed.
- Edits to `$S/tally` (words, claims, `config.yaml`, the tally feature map) change stamps, versions or cause drift. Restore them before the next recipe.
- The tally words prove claims, so a feature-map edit causes drift when it changes anything outside code spans in a sub-feature's text (the `Sub-features` entry and the step that opens with its id): an id, a requirement sentence, other prose (dated sentences included) or anything inside a fenced block. Inline command edits, run history in the form `Verified 2026-09-12: ...` and text outside every sub-feature flag nothing. Re-mapping a claim's sources or pinning new prose makes its admitted words drift-suspect until they are admitted again, and another claim covers a sentence only through a word a curator admitted (see [claims.md](features/claims.md)).

## Helpers

All live in `.cursor/skills/verify-verilex/scripts/` and are executable.

| Helper | Use |
|---|---|
| `launch [--bin PATH]` | create a session; prints `session:`, `binary:`, `evidence:` |
| `doctor SESSION` | read-only health check |
| `"$S/vx" LABEL CMD...` | run one command in the session and keep its transcript (wraps `capture SESSION LABEL CMD...`) |
| `admit-all SESSION` | baseline for skip, plan and `--continue`: two green runs of `store-open \| item-stored apple \| item-listed apple`, then `propose` and `admit` for all three words; prints `admitted words: 3`. Run it once per session. |
| `cleanup SESSION` | tear down kept instances and the session, keep evidence |
