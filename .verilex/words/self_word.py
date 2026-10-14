"""CLI scenarios for Verilex's self claims. Each word owns a fresh scratch product."""

import contextlib
import json
import os
import shutil
import signal
import subprocess
from pathlib import Path

import canary_word as c

source = Path(json.loads(os.environ["VERILEX_INSTANCE"])["source"])
root = c.instance


def start(name):
    c.instance = root / name
    c.instance.mkdir()
    shutil.copytree(root / "bin", c.instance / "bin")
    shutil.copytree(source / "tests/fixtures/tally", c.instance / "tally")
    for directory in ("home", "stores", "config"):
        (c.instance / directory).mkdir()


def expect(condition, detail):
    if not condition:
        c.fail(detail)


def doc(label, *args, **env):
    done = c.verilex(label, *args, "--json", **env)
    try:
        value = json.loads(done.stdout)
    except ValueError:
        c.fail(f"{label}: exit {done.returncode}, invalid JSON: {c.said(done)}")
    if done.returncode == 2 and isinstance(value, dict) and value.get("verdict") == "inconclusive":
        c.result("blocked", detail=f"{label}: {value.get('reason') or c.said(done)}")
    expect(done.returncode == 0, f"{label}: exit {done.returncode}: {c.said(done)}")
    return value


def baseline():
    c.must("use-1", "run", c.CHAIN)
    c.must("use-2", "run", c.CHAIN)
    for word in ("store-open", "item-stored", "item-listed"):
        c.must(f"onboard-{word}", "onboard", word)
    return doc("live", "run", c.CHAIN)


def external(label, argv, cwd=None, clean_git=False, **extra):
    # No inner command inherits the outer driver's home, ledger or product variables.
    env = {k: v for k, v in os.environ.items() if not k.startswith(("VERILEX_", "TALLY_"))}
    if clean_git:
        for key in ("GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_NAMESPACE"):
            env.pop(key, None)
    env.update(VERILEX_HOME=str(c.instance / "home"), TALLY_STORES=str(c.instance / "stores"),
               XDG_CONFIG_HOME=str(c.instance / "config"), **extra)
    # Its own process group, so a timeout stops everything it started, not just the first process.
    proc = subprocess.Popen([str(a) for a in argv], cwd=cwd or c.instance, env=env, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, text=True, start_new_session=True)
    try:
        stdout, stderr = proc.communicate(timeout=120)
    except subprocess.TimeoutExpired:
        with contextlib.suppress(ProcessLookupError):
            os.killpg(proc.pid, signal.SIGKILL)
        proc.communicate()
        raise
    done = subprocess.CompletedProcess(argv, proc.returncode, stdout, stderr)
    (c.evidence / f"{label}.txt").write_text(
        f"$ {' '.join(str(a) for a in argv)}\nexit {done.returncode}\n"
        f"--- stdout\n{done.stdout}--- stderr\n{done.stderr}")
    return done


def setup(label, argv, cwd=None, **env):
    done = external(label, argv, cwd, **env)
    if done.returncode:
        c.result("blocked", detail=f"{label}: exit {done.returncode}: {c.said(done)}")
    return done


def plan(label, chain, **env):
    """`plan --json` for chain, holding exactly one step per chain step, in order."""
    value = doc(label, "plan", chain, **env)
    want = [step.strip() for step in chain.split("|")]
    got = [step.get("step") for step in value.get("steps") or []]
    expect(got == want, f"{label}: plan steps {got}, not {want}")
    return value


def edit(path, old, new):
    """Replace old with new in a fixture file, returning its previous text. A fixture that no longer holds
    old exactly once cannot drive the scenario, so the word is blocked, never red."""
    text = path.read_text()
    if text.count(old) != 1:
        c.result("blocked", detail=f"fixture {path.relative_to(c.instance)} no longer holds {old!r} exactly once")
    path.write_text(text.replace(old, new))
    return text
