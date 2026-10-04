"""The project's dictionary of words and the chain grammar over it."""

from __future__ import annotations

import os
import re
import shlex
from dataclasses import dataclass, field
from pathlib import Path

import yaml

PROJECT_DIR = ".verilex"
FRAME_STEPS = ("launch", "doctor", "cleanup")
_PLACEHOLDER = re.compile(r"\{(\w+)\}")


class VerilexError(Exception):
    """A usage or contract error that stops verilex before it touches the product."""


@dataclass(frozen=True)
class Project:
    root: Path
    name: str
    secret_patterns: tuple[str, ...]

    @property
    def dir(self) -> Path:
        return self.root / PROJECT_DIR

    def frame(self, step: str) -> Path:
        return self.dir / "frame" / step


@dataclass(frozen=True)
class Word:
    name: str
    path: Path
    promise: str
    args: tuple[str, ...]
    requires: tuple[str, ...]
    provides: tuple[str, ...]
    implements: tuple[str, ...]
    timeout: int

    @property
    def run(self) -> Path:
        return self.path / "run"


@dataclass(frozen=True)
class Step:
    """One word in a chain, with its arguments bound."""

    word: Word
    argv: tuple[str, ...]
    requires: tuple[str, ...] = field(default=())
    provides: tuple[str, ...] = field(default=())

    @property
    def label(self) -> str:
        return " ".join((self.word.name, *self.argv))


def find_project(start: Path) -> Project:
    for root in (start, *start.parents):
        config = root / PROJECT_DIR / "config.yaml"
        if config.is_file():
            data = _yaml(config)
            name = data.get("project")
            if not isinstance(name, str) or not re.fullmatch(r"[A-Za-z0-9._-]+", name):
                raise VerilexError(f"{config}: 'project' must be a plain name")
            patterns = tuple(str(p) for p in data.get("secret_patterns") or ())
            project = Project(root=root, name=name, secret_patterns=patterns)
            for step in FRAME_STEPS:
                if not os.access(project.frame(step), os.X_OK):
                    raise VerilexError(f"missing executable frame step {project.frame(step)}")
            return project
    raise VerilexError(f"no {PROJECT_DIR}/config.yaml in {start} or its parents")


def load_words(project: Project) -> dict[str, Word]:
    words: dict[str, Word] = {}
    for path in sorted((project.dir / "words").glob("*/word.md")):
        word = _load_word(path.parent)
        words[word.name] = word
    return words


def _load_word(path: Path) -> Word:
    text = (path / "word.md").read_text(encoding="utf-8")
    parts = text.split("---", 2)
    if not text.startswith("---") or len(parts) < 3:
        raise VerilexError(f"{path}/word.md: missing YAML frontmatter")
    meta = yaml.safe_load(parts[1]) or {}
    name = meta.get("word")
    if name != path.name:
        raise VerilexError(f"{path}/word.md: 'word' must equal the directory name {path.name!r}")
    if not os.access(path / "run", os.X_OK):
        raise VerilexError(f"{path}: missing executable 'run'")
    implements = _strings(meta, "implements", path)
    if not implements:
        raise VerilexError(f"{path}/word.md: 'implements' must point at the verify skill's feature map")
    if not str(meta.get("promise") or "").strip():
        raise VerilexError(f"{path}/word.md: 'promise' is required")
    args = _strings(meta, "args", path)
    for state in (*_strings(meta, "requires", path), *_strings(meta, "provides", path)):
        unknown = set(_PLACEHOLDER.findall(state)) - set(args)
        if unknown:
            raise VerilexError(f"{path}/word.md: state {state!r} uses undeclared args {sorted(unknown)}")
    return Word(
        name=name,
        path=path,
        promise=" ".join(str(meta["promise"]).split()),
        args=args,
        requires=_strings(meta, "requires", path),
        provides=_strings(meta, "provides", path),
        implements=implements,
        timeout=int(meta.get("timeout", 1800)),
    )


def parse_chain(chain: str, words: dict[str, Word]) -> list[Step]:
    steps = []
    for segment in chain.split("|"):
        tokens = shlex.split(segment)
        if not tokens:
            raise VerilexError(f"empty word in chain {chain!r}")
        name, *argv = tokens
        if name not in words:
            raise VerilexError(f"unknown word {name!r}; `verilex words` lists the dictionary")
        word = words[name]
        if len(argv) != len(word.args):
            raise VerilexError(f"{name} takes {len(word.args)} argument(s) {list(word.args)}, got {argv}")
        bound = dict(zip(word.args, argv))
        steps.append(
            Step(
                word=word,
                argv=tuple(argv),
                requires=tuple(_bind(s, bound) for s in word.requires),
                provides=tuple(_bind(s, bound) for s in word.provides),
            )
        )
    return steps


def check_order(steps: list[Step]) -> None:
    """Refuse a chain in which a word requires a state no earlier word provides."""
    available: set[str] = set()
    for step in steps:
        for state in step.requires:
            if state not in available:
                raise VerilexError(f"{step.label} requires {state}; nothing earlier provides it")
        available.update(step.provides)


def _bind(state: str, bound: dict[str, str]) -> str:
    return _PLACEHOLDER.sub(lambda m: bound[m.group(1)], state)


def _strings(meta: dict, key: str, path: Path) -> tuple[str, ...]:
    value = meta.get(key) or []
    if not isinstance(value, list) or not all(isinstance(v, str) for v in value):
        raise VerilexError(f"{path}/word.md: '{key}' must be a list of strings")
    return tuple(value)


def _yaml(path: Path) -> dict:
    data = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
    if not isinstance(data, dict):
        raise VerilexError(f"{path}: expected a mapping")
    return data
