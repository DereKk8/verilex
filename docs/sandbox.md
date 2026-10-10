# Sandbox

## What the brain can reach

The brain runs untrusted, in a sandbox: bubblewrap (`bwrap`) on Linux, `sandbox-exec` on macOS. In the sandbox, the brain:

- reads, and cannot write, the project, the system directories, the directories on its `PATH`, its own executable or install directory, the launcher's executable, the project's git directory when it lives elsewhere (a worktree), and its harness's credential files. On macOS it may also read the rest of the system outside `/Users`, `/Volumes`, the temporary directories and the user's home;
- writes only `<run>/brain`, which holds its `HOME` and its `TMPDIR` (`/tmp` on Linux);
- cannot reach the verilex home, the ledger, the real `verilex` binary (`--verilex`, and any other `verilex` on `PATH`), `VERILEX_HOME`, `VERILEX_LEDGER` or the run's logs. Each one is missing, or shows as an empty directory or as `/dev/null`;
- runs verilex only through `<run>/share/bin/verilex`, which sends each command to the launcher. The launcher runs it with its own home and ledger;
- reaches the network only through an egress proxy (`HTTPS_PROXY`, `HTTP_PROXY` and `ALL_PROXY`). The proxy serves `CONNECT` to public addresses only. It refuses loopback, private, link-local, CGNAT and reserved addresses, and the host's own addresses;
- on Linux, sees only its own processes and has no host network, IPC or abstract sockets. Every process it starts ends with the run;
- on macOS, can signal only processes in its own sandbox. When the brain exits or the time budget ends, the sandbox's first process ends every process in the sandbox, so a process that the brain moved to a session of its own also ends with the run;
- has no terminal. It runs in a session of its own with no controlling terminal, so it cannot type into the user's shell. On macOS the profile also denies every terminal device (`/dev/tty*`, `/dev/pty*` and `/dev/ptmx`), so the brain cannot read or write a terminal by its path. On Linux, `/dev` is the sandbox's own, with its own pseudo-terminals.

## Preflight checks

Before the brain starts, a helper inside the sandbox checks that the project refuses a new file, that each hidden path shows nothing, that a port on the host's loopback is out of reach, and, on macOS, that a signal to the launcher is refused. When the sandbox tool is missing or fails, or a check fails, the brain does not run, and the launcher prints `verilex-agent: inconclusive: the brain runs only in a sandbox, and the sandbox is not available here: <reason>`. The launcher never runs a brain without the sandbox.

## Platforms

Linux needs `bwrap` and unprivileged user namespaces. Ubuntu 23.10 and later allow user namespaces only to a program that an AppArmor profile names. CI adds this profile:

```
printf 'abi <abi/4.0>,\ninclude <tunables/global>\nprofile bwrap /usr/bin/bwrap flags=(unconfined) {\n  userns,\n}\n' | sudo tee /etc/apparmor.d/bwrap
sudo apparmor_parser -r /etc/apparmor.d/bwrap
```

Containers often block user namespaces (Docker's default seccomp profile does), so run the launcher on the host or in a VM. macOS uses `/usr/bin/sandbox-exec`, which ships with the system. Other systems have no sandbox, so each run there is inconclusive.

## Run files

Each run keeps its files in one directory, `$TMPDIR/verilex-agent-*`, or under `/tmp` when `$TMPDIR` is longer than 40 characters (unix socket paths are short):

```tree
verilex-agent-*/
  share/             the brain reads it
    prompt.txt
    suggest.txt
    bin/verilex
  brain/             the brain writes it
    home/
    tmp/
  log/
    verify.out
    verify.err
    suggest.out
    suggest.err
    verilex.log
    egress.log
  sock/              the socket behind `bin/verilex`
  sandbox/           per phase: `egress.sock` (Linux) or `sandbox.sb` (macOS)
  home/              the verilex home, when `--home` is not given
  ticket.yaml        the run spec
```

The launcher removes the directory at the end. With `--keep-work`, it keeps the directory and prints `verilex-agent: run files kept in <dir>`. `log/verilex.log` lists each verilex command the brain sent to the launcher, with its exit code or the refusal. `log/egress.log` lists each network request and whether the proxy refused it.

## Harness credentials

A built-in harness gets only its credential file, read-only, at the same place in its own home: `~/.claude/.credentials.json` for `claude` and `claude-code`, `~/.codex/auth.json` for `codex`, and `~/.pi/agent/auth.json` for `pi`. The user's other harness settings are not there. The built-in harnesses run with their own permission prompts and sandbox off (`--permission-mode bypassPermissions` for claude, `--dangerously-bypass-approvals-and-sandbox` for codex), because the launcher's sandbox is the boundary and a headless brain cannot answer a prompt. In a `--harnesses` file, a harness is an argv list or `{"argv": [...], "files": [...]}`. A `~/` file shows at the same place in the brain's home, and any other file at its own path.

## Limits

The sandbox does not cover these cases:

- The brain gets the launcher's environment, except `HOME`, `TMPDIR`, the `XDG_*` directories, `NO_PROXY`, `VERILEX_HOME` and `VERILEX_LEDGER`. A secret in that environment reaches the brain, so start the launcher with only the variables the harness needs.
- The brain can reach any public address, so a service that the project publishes on a public address is reachable.
- On Linux, a unix socket inside a readable directory, such as the project, stays reachable.
- On macOS, a brain that kills or stops the sandbox's first process (`verilex-agent sandbox-init`) before the run ends can leave a process running after the run. [Leftover processes on macOS](#leftover-processes-on-macos) gives the workarounds.
- On macOS, the brain cannot open a pseudo-terminal, so a harness that runs its commands in one fails there. The built-in harnesses are not tested on macOS.
- The egress proxy serves only `CONNECT`. A client that sends plain HTTP requests to the proxy gets `405`.
- A harness cannot save a token that it refreshes during a run. If the provider rotates refresh tokens, log in again on the host when the harness reports an expired login.

The project check and the home check, rows 7 and 8 of the [verdict rule](verdict-rule.md), stay as defense in depth. The brain cannot cause either one, but they still catch a change that something outside the sandbox makes during the run, and a run that something starts around the launcher in its home.

## Leftover processes on macOS

When the brain exits or the time budget ends, the sandbox's first process, `verilex-agent sandbox-init`, ends every process in the sandbox. A brain can stop that clean-up only on purpose: it kills or stops `sandbox-init`, for example with `kill -9 $PPID`. A process that the brain moved to a session of its own (`setsid`) then keeps running after the launcher returns. That process keeps the sandbox's limits: it can write only the run's `brain` directory, and it can connect only to the egress proxy's port on localhost, which closes with the run. It still uses CPU and memory until something ends it.

When you run a brain that you do not trust on macOS, use one of these workarounds:

- **Run the launcher on Linux.** Every process of the sandbox, in any session, ends with bwrap's pid namespace, also when the brain kills `sandbox-init`. `TestNoChildOfTheBrainOutlivesTheRun` in `agent/stop_test.go` proves this on each Linux CI run.
  - Protects against: every leftover process.
  - Costs: a Linux host with `bwrap` and user namespaces (see [Platforms](#platforms)).
- **Run the launcher in a Linux VM on the Mac.** Inside the VM, the Linux guarantee applies. The Linux CI runner that proves it is itself a VM.
  - Protects against: every leftover process.
  - Costs: a VM with `bwrap` and user namespaces, and the project and the harness's credential file inside the VM. A container is not enough when its runtime blocks user namespaces, as Docker's default seccomp profile does.
- **Run the launcher as a dedicated macOS user, then end every process of that user.** A process cannot leave its user, so after the run, `pkill` ends what the brain left, a `setsid` child too. macOS CI showed it: a child that outlived the run was the only process `pgrep -l -u` listed for the user, and it was gone after `pkill`.
  1. Create the user once, with admin rights: `sudo dscl . -create /Users/vxbrain`, then set its `UniqueID` (an unused number), `PrimaryGroupID 20`, `UserShell /bin/sh` and `NFSHomeDirectory /var/empty` with the same command.
  2. Make `verilex`, `verilex-agent`, the brain and its tools readable by that user. Give it the project too: git refuses a repository that another user owns.
  3. Run the launcher as that user: `sudo -u vxbrain env HOME=<dir> TMPDIR=<dir> PATH="$PATH" verilex-agent ...`, with both directories owned by `vxbrain`.
  4. After the run, list what is left with `pgrep -l -u vxbrain`, and end it with `sudo pkill -KILL -u vxbrain`.
  - Protects against: every leftover process, also one whose brain killed `sandbox-init`.
  - Costs: admin rights once and `sudo` after each run. Run one launcher at a time as that user, because `pkill` ends all of its runs. macOS can start its own services for the user, such as `distnoted`; `pkill` ends those too, and macOS starts them again. The user needs its own copy of the harness's credential file. Tools that go through Xcode's `xcrun` shims, such as `/usr/bin/python3`, failed in the sandbox for that user on CI, so put real binaries, such as Homebrew's, first on its `PATH`.

To check for a leftover under your own user, look for the processes you expect the brain to start, with `pgrep -lf <name>`. That finds a process only by its name or arguments, which the brain controls, so an empty answer does not prove that nothing is left. Only a dedicated user gives a complete list: `pgrep -l -u <user>`.
