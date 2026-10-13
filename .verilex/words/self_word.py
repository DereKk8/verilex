"""CLI scenarios for Verilex's self claims. Each word owns a fresh scratch product."""

import json
import os
import shutil
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
    expect(done.returncode == 0, f"{label}: exit {done.returncode}: {c.said(done)}")
    try:
        return json.loads(done.stdout)
    except ValueError:
        c.fail(f"{label}: invalid JSON: {c.said(done)}")


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
    done = subprocess.run([str(a) for a in argv], cwd=cwd or c.instance, env=env,
                          capture_output=True, text=True, timeout=120, check=False)
    (c.evidence / f"{label}.txt").write_text(
        f"$ {' '.join(str(a) for a in argv)}\nexit {done.returncode}\n"
        f"--- stdout\n{done.stdout}--- stderr\n{done.stderr}")
    return done


def setup(label, argv, cwd=None, **env):
    done = external(label, argv, cwd, **env)
    if done.returncode:
        c.result("blocked", detail=f"{label}: exit {done.returncode}: {c.said(done)}")
    return done
