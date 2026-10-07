"""Shared plumbing for the canary words: drive the verilex under test on its scratch tally product."""

import json
import os
import subprocess
import sys
from pathlib import Path

CHAIN = "store-open | item-stored apple | item-listed apple"
instance = Path(json.loads(os.environ["VERILEX_INSTANCE"])["dir"])
evidence = Path(os.environ["VERILEX_EVIDENCE"])


def verilex(label: str, *args: str, **env: str) -> subprocess.CompletedProcess:
    """Run the verilex under test in the scratch product. The outer run's VERILEX_* and TALLY_* never leak in,
    so the inner build reads and writes only its own home and ledger."""
    clean = {k: v for k, v in os.environ.items() if not k.startswith(("VERILEX_", "TALLY_"))}
    clean.update(VERILEX_HOME=str(instance / "home"), TALLY_STORES=str(instance / "stores"),
                 XDG_CONFIG_HOME=str(instance / "config"), **env)
    done = subprocess.run([instance / "bin" / "verilex", *args], cwd=instance / "tally", env=clean,
                          capture_output=True, text=True, timeout=600, check=False)
    shown = " ".join(f"{k}={v}" for k, v in env.items())
    (evidence / f"{label}.txt").write_text(
        f"$ {shown + ' ' if shown else ''}verilex {' '.join(args)}\nexit {done.returncode}\n"
        f"--- stdout\n{done.stdout}--- stderr\n{done.stderr}")
    return done


def must(label: str, *args: str) -> subprocess.CompletedProcess:
    """A setup step: when it does not exit 0, the word's preconditions did not hold."""
    done = verilex(label, *args)
    if done.returncode != 0:
        result("blocked", detail=f"setup `verilex {' '.join(args)}` exited {done.returncode}: {said(done)}")
    return done


def said(done: subprocess.CompletedProcess) -> str:
    return (done.stdout + done.stderr).strip()[:300]


def stores() -> list:
    return sorted(os.listdir(instance / "stores"))


def runs() -> list:
    return [line for line in must("runs", "runs").stdout.splitlines() if line.strip()]


def run_line(run: str) -> str:
    return next((line for line in runs() if line.split()[0] == run), "")


def run_id(done: subprocess.CompletedProcess) -> str:
    return done.stdout.split("; run ")[1].split()[0] if "; run " in done.stdout else ""


def result(verdict: str, **fields) -> None:
    print(json.dumps({"verdict": verdict, **fields}))
    sys.exit({"pass": 0, "fail": 1, "blocked": 2}[verdict])


def fail(detail: str) -> None:
    result("fail", preconditions_held=True, detail=detail)
