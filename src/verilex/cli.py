"""verilex command line."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from .dictionary import VerilexError, check_order, find_project, load_words, parse_chain
from .runner import EXIT_CODES, Run, cleanup, load_runs, runs_dir

USAGE_EXIT = 2


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="verilex", description=__doc__)
    parser.add_argument("--project", type=Path, default=Path.cwd(), help="product checkout (default: cwd)")
    sub = parser.add_subparsers(dest="command", required=True)
    run = sub.add_parser("run", help="run a chain of words, e.g. 'a | b X | c'")
    run.add_argument("chain")
    run.add_argument("--keep", action="store_true", help="skip cleanup; tear down later with `verilex cleanup`")
    run.add_argument("--json", action="store_true", help="print the run record as JSON")
    sub.add_parser("words", help="list the dictionary")
    sub.add_parser("runs", help="list this project's runs and any instance still alive")
    clean = sub.add_parser("cleanup", help="tear down a kept run's instance")
    clean.add_argument("run")
    args = parser.parse_args(argv)

    try:
        project = find_project(args.project.resolve())
        if args.command == "runs":
            for record in load_runs(project):
                print(
                    f"{record['run']}  {record['verdict'] or 'running'}  cleanup={record['cleanup']}  {record['chain']}"
                )
            return 0
        if args.command == "cleanup":
            return _cleanup(project, args.run)
        words = load_words(project)
        if args.command == "words":
            for word in words.values():
                print(f"{word.name} {' '.join(word.args)}".strip())
                print(f"  promise:  {word.promise}")
                print(f"  requires: {', '.join(word.requires) or '-'}  provides: {', '.join(word.provides) or '-'}")
            return 0
        steps = parse_chain(args.chain, words)
        check_order(steps)
    except VerilexError as exc:
        print(f"verilex: refused: {exc}", file=sys.stderr)
        return USAGE_EXIT

    record = Run(project, args.chain, steps).execute(keep=args.keep)
    if args.json:
        print(json.dumps(record, indent=2))
    else:
        _report(record)
    return EXIT_CODES[record["verdict"] or "blocked"]


def _report(record: dict) -> None:
    print(f"verilex run {record['run']} ({record['project']})")
    frames = {frame["step"]: frame for frame in record["frame"]}
    for step in ("launch", "doctor"):
        if step in frames:
            print(f"  [{'ok' if frames[step]['exit'] == 0 else 'refused'}] {step}")
    for word in record["words"]:
        label = " ".join([word["word"], *word["args"]])
        print(f"  [{word['verdict']}] {label}  ({word['seconds']}s)")
        for key in ("observation", "reason"):
            if word.get(key):
                print(f"      {key}: {word[key]}")
        if word["verdict"] != "pass":
            print(f"      fall back to the verify skill: {', '.join(word['implements'])}")
    if "doctor-after-failure" in frames:
        print(f"  [{'ok' if frames['doctor-after-failure']['exit'] == 0 else 'refused'}] doctor after failure")
    print(f"  cleanup: {record['cleanup']}")
    print(f"result: {record['verdict']}" + (f" - {record['reason']}" if record["reason"] else ""))
    print(f"evidence: {record['dir']}")


def _cleanup(project, run_id: str) -> int:
    run_dir = runs_dir(project) / run_id
    path = run_dir / "run.json"
    if not path.is_file():
        print(f"verilex: refused: {run_id} is not a run of {project.name}", file=sys.stderr)
        return USAGE_EXIT
    record = json.loads(path.read_text(encoding="utf-8"))
    if record["cleanup"] == "done":
        print(f"verilex: {run_id} was already cleaned up")
        return 0
    cleanup(project, record, run_dir)
    print(f"cleanup: {record['cleanup']}")
    return 0 if record["cleanup"] == "done" else USAGE_EXIT


if __name__ == "__main__":
    sys.exit(main())
