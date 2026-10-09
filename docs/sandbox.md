# Sandbox

## What the brain can reach

The brain runs untrusted, in a sandbox: bubblewrap (`bwrap`) on Linux, `sandbox-exec` on macOS. In the sandbox, the brain:

- reads, and cannot write, the project, the system directories, the directories on its `PATH`, its own executable or install directory, the launcher's executable, the project's git directory when it lives elsewhere (a worktree), and its harness's credential files. On macOS it may also read the rest of the system outside `/Users`, `/Volumes`, the temporary directories and the user's home;
- writes only `<run>/brain`, which holds its `HOME` and its `TMPDIR` (`/tmp` on Linux);
- cannot reach the verilex home, the ledger, the real `verilex` binary (`--verilex`, and any other `verilex` on `PATH`), `VERILEX_HOME`, `VERILEX_LEDGER` or the run's logs. Each one is missing, or shows as an empty directory or as `/dev/null`;
- runs verilex only through `<run>/share/bin/verilex`, which sends each command to the launcher. The launcher runs it with its own home and ledger;
- reaches the network only through an egress proxy (`HTTPS_PROXY`, `HTTP_PROXY` and `ALL_PROXY`). The proxy serves `CONNECT` to public addresses only. It refuses loopback, private, link-local, CGNAT and reserved addresses, and the host's own addresses;
- on Linux, sees only its own processes and has no host network, IPC or abstract sockets. Every process it starts ends with the run;
- has no terminal. It runs in a session of its own with no controlling terminal, so it cannot type into the user's shell. On macOS the profile also denies every terminal device (`/dev/tty*`, `/dev/pty*` and `/dev/ptmx`), so the brain cannot read or write a terminal by its path. On Linux, `/dev` is the sandbox's own, with its own pseudo-terminals.

## Preflight checks

Before the brain starts, a helper inside the sandbox checks that the project refuses a new file, that each hidden path shows nothing, and that a port on the host's loopback is out of reach. When the sandbox tool is missing or fails, or a check fails, the brain does not run, and the launcher prints `verilex-agent: inconclusive: the brain runs only in a sandbox, and the sandbox is not available here: <reason>`. The launcher never runs a brain without the sandbox.

## Platforms

Linux needs `bwrap` and unprivileged user namespaces. Ubuntu 23.10 and later allow user namespaces only to a program that an AppArmor profile names. CI adds this profile:

```
printf 'abi <abi/4.0>,\ninclude <tunables/global>\nprofile bwrap /usr/bin/bwrap flags=(unconfined) {\n  userns,\n}\n' | sudo tee /etc/apparmor.d/bwrap
sudo apparmor_parser -r /etc/apparmor.d/bwrap
```

Containers often block user namespaces (Docker's default seccomp profile does), so run the launcher on the host or in a VM. macOS uses `/usr/bin/sandbox-exec`, which ships with the system. Other systems have no sandbox, so each run there is inconclusive.

## Run files

Each run keeps its files in one directory, `$TMPDIR/verilex-agent-*`, or under `/tmp` when `$TMPDIR` is longer than 40 characters (unix socket paths are short):

```
verilex-agent-*/
  share/       prompt.txt, suggest.txt, bin/verilex   brain reads
  brain/       home/, tmp/                            brain writes
  log/         verify.out, verify.err, suggest.out, suggest.err,
               verilex.log, egress.log
  sock/        the socket behind bin/verilex
  sandbox/     per phase: egress.sock (Linux) or sandbox.sb (macOS)
  home/        the verilex home, when --home is not given
  ticket.yaml  the run spec
```

The launcher removes the directory at the end. With `--keep-work`, it keeps the directory and prints `verilex-agent: run files kept in <dir>`. `log/verilex.log` lists each verilex command the brain sent to the launcher, with its exit code or the refusal. `log/egress.log` lists each network request and whether the proxy refused it.

## Harness credentials

A built-in harness gets only its credential file, read-only, at the same place in its own home: `~/.claude/.credentials.json` for `claude` and `claude-code`, `~/.codex/auth.json` for `codex`, and `~/.pi/agent/auth.json` for `pi`. The user's other harness settings are not there. The built-in harnesses run with their own permission prompts and sandbox off (`--permission-mode bypassPermissions` for claude, `--dangerously-bypass-approvals-and-sandbox` for codex), because the launcher's sandbox is the boundary and a headless brain cannot answer a prompt. In a `--harnesses` file, a harness is an argv list or `{"argv": [...], "files": [...]}`. A `~/` file shows at the same place in the brain's home, and any other file at its own path.

## Limits

The sandbox does not cover these cases:

- The brain gets the launcher's environment, except `HOME`, `TMPDIR`, the `XDG_*` directories, `NO_PROXY`, `VERILEX_HOME` and `VERILEX_LEDGER`. A secret in that environment reaches the brain, so start the launcher with only the variables the harness needs.
- The brain can reach any public address, so a service that the project publishes on a public address is reachable.
- On Linux, a unix socket inside a readable directory, such as the project, stays reachable.
- On macOS, a process that the brain detaches from its process group can outlive the run. The process keeps the sandbox's limits.
- On macOS, the brain cannot open a pseudo-terminal, so a harness that runs its commands in one fails there. No harness was run on macOS.
- The egress proxy serves only `CONNECT`. A client that sends plain HTTP requests to the proxy gets `405`.
- A harness cannot save a token that it refreshes during a run. If the provider rotates refresh tokens, log in again on the host when the harness reports an expired login.

The project check and the home check, rows 7 and 8 of the [verdict rule](verdict-rule.md), stay as defense in depth. The brain can no longer cause either one, but they still catch a change that something outside the sandbox makes during the run, and a run that something starts around the launcher in its home.
