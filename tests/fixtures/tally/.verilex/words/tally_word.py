"""Shared plumbing for the tally words: drive the CLI, keep evidence, print one result."""

import json
import os
import subprocess
import sys
from pathlib import Path

LOCKED = 75
state = json.load(sys.stdin)
store = Path(state["instance"]["store"])
evidence = Path(state["evidence"])


def tally(*args: str) -> subprocess.CompletedProcess:
    cli = Path(os.environ["VERILEX_PROJECT_ROOT"], "bin", "tally")
    done = subprocess.run([cli, "--store", store, *args], capture_output=True, text=True, check=False)
    with open(evidence / "actions.log", "a") as log:
        log.write(f"$ tally {' '.join(args)}\nexit {done.returncode}\n{done.stdout}{done.stderr}\n")
    if done.returncode == LOCKED:
        result("blocked", detail=done.stderr.strip())
    return done


def stored_items() -> list:
    items = json.loads((store / "store.json").read_text())["items"]
    (evidence / "store.json").write_text(json.dumps(items))
    return items


def result(verdict: str, **fields) -> None:
    print(json.dumps({"verdict": verdict, **fields}))
    sys.exit({"pass": 0, "fail": 1, "blocked": 2}[verdict])
