# Where the shared ledger lives

The shared ledger is a directory of immutable pass files, and `VERILEX_LEDGER` places it. Stateless instances point it at storage they all mount. Instances that share only git can commit the same directory into the repository instead: it merges without conflicts, at a measured cost in pushes and repository growth. verilex does not keep the ledger in one shared file, because one file loses passes under concurrency. verilex has no ledger service: a service is fast, but it adds hosting, credentials and an outage mode to a model-free CLI, and it buys no trust that the directory lacks.

The obvious alternatives are a ledger file in the repository and a small service. This page gives the measurements behind the design. They show that the deciding property is the format, not the transport: one immutable file per pass loses nothing under any of the three placements, and one shared file loses passes under every one of them.

## How it was measured

`docs/ledger-placement/measure` (run from the repository root) drives the real `verilex` CLI on copies of the tally sample product. Each stateless instance is a `verilex` process with a state home of its own. The figures compare the directory ledger with the single-file format that verilex used before it, one `ledger.json` per state home (commit `e4af42f`). `service.go` is a minimal in-memory HTTP ledger service, and `store.go` drives the directory ledger's own record and read calls under the same load. The figures below come from one run on Linux 7.2, a local btrfs disk and 24 cores. Nothing crossed a network: a real service and a real git remote add a round trip to every call.

## Write contention and integrity

Concurrent runs of `store-open | item-stored fN | item-listed fN`, one instance each, all recording into one shared directory. `store-open` is one slot that every run writes. `ledger-audit`, a script in the repository's own verify skill that shares no code with verilex, matches every pass against the run records.

| Instances | Green runs | Steps proven | Passes recorded | Lost | Forged | Batch time | New instance plans |
|---|---|---|---|---|---|---|---|
| 1 | 1 | 3 | 3 | 0 | 0 | 227 ms | skip 3, run 0 (8 ms) |
| 4 | 4 | 12 | 12 | 0 | 0 | 231 ms | skip 3, run 0 (8 ms) |
| 16 | 16 | 48 | 48 | 0 | 0 | 322 ms | skip 3, run 0 (8 ms) |
| 32 | 32 | 96 | 96 | 0 | 0 | 586 ms | skip 3, run 0 (9 ms) |

Two branches of the product that prove the same chain under different stamps, four concurrent runs each:

| Ledger | Branch a plans | Branch b plans |
|---|---|---|
| one `ledger.json`, shared state home | skip 3, run 0 | skip 0, run 3 (`store-open: env TALLY_DEFECT changed`) |
| directory, one state home per instance | skip 3, run 0 | skip 3, run 0 |

One file keeps one entry per step, so concurrent branches overwrite each other's passes even under its lock. The directory keeps every pass side by side.

## In the repository

Each instance is a git clone. It runs its chain, commits its ledger and pushes, pulling with rebase until the push lands. A rebase conflict drops the pass, because merging two ledger files needs a person. The instances are then gone, and a fresh clone of main plans the first chain.

| Format | Instances | Pushes | Rebase conflicts | Entries on main | Time | Growth per entry (packed) | Fresh clone plans |
|---|---|---|---|---|---|---|---|
| `ledger.json` | 4 | 4 | 3 | 3 of 9 | 317 ms | 2,060 B | run 3: `evidence from run ... is gone` |
| `ledger.json` | 16 | 16 | 15 | 3 of 33 | 496 ms | 4,080 B | run 3: `evidence from run ... is gone` |
| directory | 4 | 10 | 0 | 12 of 12 | 542 ms | 2,009 B | skip 3, run 0 |
| directory | 16 | 136 | 0 | 48 of 48 | 2,151 ms | 3,343 B | skip 3, run 0 |

A single in-repo file conflicts on every concurrent push, and its passes cannot be reused anyway, because their evidence stayed on instances that are gone. The directory merges cleanly and carries its evidence, but sixteen instances pushing to one branch needed 136 pushes, 8.5 per instance. On a hosted remote each push is a network round trip. Every pass also stays in the repository's history and shows up in the change that records it.

## A small service

The same load, a write of three passes then a read of three steps per client, on loopback:

| Clients | Service write p50 / p95 | Directory write p50 / p95 | Service read p50 / p95 | Directory read p50 / p95 | Lost (both) |
|---|---|---|---|---|---|
| 1 | 0.04 / 0.04 ms | 0.20 / 0.20 ms | 0.03 / 0.03 ms | 0.05 / 0.05 ms | 0 |
| 4 | 0.07 / 0.43 ms | 0.45 / 0.61 ms | 0.05 / 0.07 ms | 0.07 / 0.14 ms | 0 |
| 16 | 0.83 / 2.10 ms | 1.98 / 4.34 ms | 0.46 / 0.86 ms | 0.38 / 1.68 ms | 0 |
| 32 | 1.69 / 13.28 ms | 4.06 / 9.67 ms | 0.86 / 3.33 ms | 6.21 / 10.46 ms | 0 |

Both are lossless and both cost well under the 8 ms that one `verilex plan` takes. A hosted service adds a network round trip, typically 10 to 100 ms, to every read and write, and verilex would need an HTTP client, credentials and an answer for an outage. An outage would only cost re-runs, because a missing pass means "run it live". Still, every outage would cost every instance its skip savings at once.

## Trust

`measure` changes one recorded pass at a time, as another writer of the ledger would, then plans its chain:

| Change to a recorded pass | Reader |
|---|---|
| edited in place | refuses: `a pass on record is damaged: its content does not match its digest` |
| resealed with another owner | refuses: `... was recorded on an instance run ... launched; only a run that launched its own instance records a pass` |
| resealed with another claim version | refuses: `... proved item-added@000000000000, not item-added@18e2db0cee8f; a pass proves only the claim version it ran against` |
| resealed with evidence that misses the contract | refuses: `... misses the evidence contract: pass without a second observation` |
| resealed younger, everything else intact | skips |
| resealed with invented evidence that meets the contract | skips |

The reader catches damage, merge accidents, passes from other claim versions or unowned instances, and evidence that misses the contract. It cannot tell a pass that was carefully forged by someone who can write the ledger. No placement changes that: a service accepts whatever an instance with its credentials sends, and it cannot see whether a run happened either. So the trust boundary is write access to the ledger in all three placements. In the repository, git adds who wrote each pass and review of the change. A service would add per-instance tokens. A shared directory relies on file permissions.

## Cost

- **Directory:** 1,429 bytes per pass file, plus the step's evidence (131 bytes for tally; a real product's screenshots or logs cost more). The default ledger hard-links its evidence to the run's own files, so on one machine it costs no extra space. Recording a step drops that step's passes that are 7 days old or older.
- **Skip decision at scale:** a pass file names its stamp, so the reader opens only the passes for the current stamp. `verilex plan` took 8 ms with no other pass in the slot, 10 ms with 1,000 and 27 ms with 10,000.
- **In the repository:** 2.0 to 3.3 KB packed per pass, kept in history after it expires, plus every push round trip and its retries.
- **Service:** hosting, credentials and availability, plus a network round trip per call.

## Choosing a placement

- One machine: nothing to set. The ledger lives under the state home. verilex does not read the older `runs/ledger.json`, so the first run after an upgrade from a version that kept it is live.
- Stateless instances: set `VERILEX_LEDGER` to a directory every instance mounts.
- Instances that share only git: point `VERILEX_LEDGER` into the checkout and commit the directory. Keep it outside every path a word declares in `inputs`, or recording a pass would change that word's stamp.
- A service stays an option for deployments that can mount nothing and outgrow git pushes. It would store these same pass files, and readers would keep re-checking every one.
