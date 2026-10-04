"""Run a chain of words inside the trust frame: launch, doctor, evidence, cleanup."""

from __future__ import annotations

import json
import os
import re
import secrets
import signal
import subprocess
import time
from datetime import UTC, datetime
from pathlib import Path

from .dictionary import Project, Step

EXIT_CODES = {"pass": 0, "fail": 1, "blocked": 2, "unverified": 2}
_WORD_VERDICTS = ("pass", "fail", "blocked")
FRAME_TIMEOUT = 1800
SECRET_PATTERNS = (
    r"-----BEGIN [A-Z ]*PRIVATE KEY-----",
    r"\bgh[pousr]_[A-Za-z0-9]{36,}",
    r"\bgithub_pat_[A-Za-z0-9_]{22,}",
    r"\bAKIA[0-9A-Z]{16}\b",
    r"\bxox[abprs]-[A-Za-z0-9-]{10,}",
    r"\bsk-ant-[A-Za-z0-9_-]{20,}",
)


def state_home() -> Path:
    return Path(os.environ.get("VERILEX_HOME") or Path.home() / ".local/state/verilex")


def runs_dir(project: Project) -> Path:
    return state_home() / project.name / "runs"


class Run:
    def __init__(self, project: Project, chain: str, steps: list[Step]):
        self.project = project
        self.steps = steps
        self.id = f"{int(time.time())}-{secrets.token_hex(3)}"
        self.dir = runs_dir(project) / self.id
        self.dir.mkdir(mode=0o700, parents=True)
        self.patterns = [re.compile(p) for p in (*SECRET_PATTERNS, *project.secret_patterns)]
        self.record: dict = {
            "run": self.id,
            "project": project.name,
            "root": str(project.root),
            "chain": chain,
            "dir": str(self.dir),
            "started": _now(),
            "instance": None,
            "frame": [],
            "words": [],
            "verdict": None,
            "reason": None,
            "cleanup": "pending",
        }
        self._save()

    def execute(self, keep: bool = False) -> dict:
        try:
            if self._launch() and self._doctor("doctor"):
                self._words()
        finally:
            if keep:
                self.record["cleanup"] = "kept"
            else:
                cleanup(self.project, self.record, self.dir)
            self._save()
        return self.record

    def _launch(self) -> bool:
        result = self._frame("launch")
        if result["exit"] != 0:
            return self._stop("blocked", f"launch exited {result['exit']}")
        try:
            launched = json.loads(Path(result["evidence"], "stdout").read_text(encoding="utf-8"))
            self.record["instance"] = launched["instance"]
        except (ValueError, KeyError, TypeError):
            return self._stop("blocked", "launch printed no JSON object with an 'instance'")
        self._save()
        return True

    def _doctor(self, label: str) -> bool:
        result = self._frame("doctor", label)
        if result["exit"] != 0:
            return self._stop("blocked", f"{label} refused the instance (exit {result['exit']})")
        return True

    def _words(self) -> None:
        available: set[str] = set()
        for index, step in enumerate(self.steps, start=1):
            entry = self._word(index, step, sorted(available))
            self.record["words"].append(entry)
            self._save()
            if entry["verdict"] != "pass":
                reason = f"{step.label}: {entry['reason']}" if entry.get("reason") else step.label
                self._stop(entry["verdict"], reason)
                self._doctor("doctor-after-failure")
                return
            available.update(step.provides)
        self.record["verdict"] = "pass"

    def _word(self, index: int, step: Step, states: list[str]) -> dict:
        evidence = self.dir / f"{index:02d}-{step.word.name}"
        state = {
            "run": self.id,
            "instance": self.record["instance"],
            "states": states,
            "args": dict(zip(step.word.args, step.argv)),
            "evidence": str(evidence),
        }
        started = time.monotonic()
        code = _execute(
            [str(step.word.run), *step.argv], evidence, self._env(evidence), step.word.timeout, json.dumps(state)
        )
        verdict, reason, result = _judge(code, evidence, step.word.timeout)
        leak = self._scan(evidence)
        if leak:
            verdict, reason = "unverified", f"secret pattern in evidence {leak}"
        return {
            "word": step.word.name,
            "args": list(step.argv),
            "provides": list(step.provides),
            "implements": list(step.word.implements),
            "verdict": verdict,
            "reason": reason,
            "observation": result.get("observation"),
            "detail": result.get("detail"),
            "exit": code,
            "seconds": round(time.monotonic() - started, 2),
            "evidence": str(evidence),
        }

    def _frame(self, step: str, label: str | None = None) -> dict:
        label = label or step
        evidence = self.dir / f"frame-{label}"
        code = _execute([str(self.project.frame(step))], evidence, self._env(evidence), FRAME_TIMEOUT)
        result = {"step": label, "exit": code, "evidence": str(evidence)}
        leak = self._scan(evidence)
        if leak:
            result["exit"] = 2 if code == 0 else code
            result["leak"] = leak
        self.record["frame"].append(result)
        self._save()
        return result

    def _env(self, evidence: Path) -> dict[str, str]:
        return {
            **os.environ,
            "VERILEX_RUN": self.id,
            "VERILEX_PROJECT_ROOT": str(self.project.root),
            "VERILEX_INSTANCE": json.dumps(self.record["instance"]),
            "VERILEX_EVIDENCE": str(evidence),
        }

    def _scan(self, evidence: Path) -> str | None:
        return scan(evidence, self.patterns)

    def _stop(self, verdict: str, reason: str) -> bool:
        if self.record["verdict"] is None or (self.record["verdict"] == "fail" and verdict == "blocked"):
            self.record["verdict"], self.record["reason"] = verdict, reason
        return False

    def _save(self) -> None:
        save(self.dir, self.record)


def cleanup(project: Project, record: dict, run_dir: Path) -> None:
    """Tear down only what this run launched, then confirm the evidence survived."""
    evidence = run_dir / "frame-cleanup"
    env = {
        **os.environ,
        "VERILEX_RUN": record["run"],
        "VERILEX_PROJECT_ROOT": str(project.root),
        "VERILEX_INSTANCE": json.dumps(record["instance"]),
        "VERILEX_EVIDENCE": str(evidence),
    }
    code = _execute([str(project.frame("cleanup"))], evidence, env, FRAME_TIMEOUT)
    frame_step = {"step": "cleanup", "exit": code, "evidence": str(evidence)}
    leak = scan(evidence, [re.compile(p) for p in (*SECRET_PATTERNS, *project.secret_patterns)])
    if leak:
        frame_step["exit"] = 2 if code == 0 else code
        frame_step["leak"] = leak
    record["frame"].append(frame_step)
    record["cleanup"] = "done" if code == 0 else "failed"
    record["evidence_kept"] = all(Path(w["evidence"], "stdout").is_file() for w in record["words"])
    if code != 0 and record["verdict"] in (None, "pass"):
        record["verdict"], record["reason"] = "blocked", f"cleanup exited {code}; the instance may outlive the run"
    if not record["evidence_kept"]:
        record["verdict"], record["reason"] = "unverified", "evidence did not survive cleanup"
    if leak:
        record["verdict"], record["reason"] = "unverified", f"secret pattern in evidence {leak}"
    record["finished"] = _now()
    save(run_dir, record)


def scan(evidence: Path, patterns: list[re.Pattern]) -> str | None:
    for path in sorted(evidence.rglob("*")):
        if path.is_file():
            text = path.read_bytes().decode("utf-8", "replace")
            if any(p.search(text) for p in patterns):
                return path.name
    return None


def save(run_dir: Path, record: dict) -> None:
    tmp = run_dir / "run.json.tmp"
    tmp.write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    tmp.replace(run_dir / "run.json")


def load_runs(project: Project) -> list[dict]:
    records = []
    for path in sorted(runs_dir(project).glob("*/run.json")):
        records.append(json.loads(path.read_text(encoding="utf-8")))
    return records


def _judge(code: int | None, evidence: Path, timeout: int) -> tuple[str, str | None, dict]:
    """Apply the structural honesty rules to one word's result."""
    if code is None:
        return "unverified", f"timed out after {timeout}s", {}
    try:
        result = json.loads(Path(evidence, "stdout").read_text(encoding="utf-8"))
    except ValueError:
        return "unverified", "stdout is not one result JSON object", {}
    if not isinstance(result, dict) or result.get("verdict") not in _WORD_VERDICTS:
        return "unverified", "result has no verdict of pass, fail or blocked", {}
    verdict = result["verdict"]
    if EXIT_CODES[verdict] != code:
        return "unverified", f"exit {code} disagrees with verdict {verdict}", result
    if verdict == "pass" and not str(result.get("observation") or "").strip():
        return "unverified", "pass without a second observation", result
    if verdict == "fail" and result.get("preconditions_held") is not True:
        return "unverified", "fail without stating that its preconditions held", result
    return verdict, result.get("detail"), result


def _execute(argv: list[str], evidence: Path, env: dict, timeout: int, stdin: str = "") -> int | None:
    """Run one step with its own evidence directory; None means it timed out."""
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    with open(evidence / "stdout", "wb") as out, open(evidence / "stderr", "wb") as err:
        proc = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=out, stderr=err, env=env, start_new_session=True)
        try:
            proc.communicate(stdin.encode(), timeout=timeout)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            proc.wait()
            code = None
        else:
            code = proc.returncode
    (evidence / "exit").write_text(f"{code if code is not None else 'timeout'}\n", encoding="utf-8")
    return code


def _now() -> str:
    return datetime.now(UTC).isoformat(timespec="seconds")
