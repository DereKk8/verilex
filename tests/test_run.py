"""Behavior tests: drive the verilex CLI against the made-up tally product."""

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

FIXTURE = Path(__file__).parent / "fixtures" / "tally"
CHAIN = "store-open | item-stored apple | item-listed apple"


@pytest.fixture
def product(tmp_path):
    root = tmp_path / "tally"
    shutil.copytree(FIXTURE, root, symlinks=True)
    (tmp_path / "stores").mkdir()
    return root


def verilex(product: Path, *args: str, **env: str) -> subprocess.CompletedProcess:
    base = {k: v for k, v in os.environ.items() if not k.startswith(("TALLY_", "VERILEX_"))}
    base.update(VERILEX_HOME=str(product.parent / "state"), TALLY_STORES=str(product.parent / "stores"), **env)
    return subprocess.run(
        [sys.executable, "-m", "verilex", *args], cwd=product, env=base, capture_output=True, text=True, check=False
    )


def last_run(product: Path) -> dict:
    (path,) = (product.parent / "state" / "tally" / "runs").glob("*/run.json")
    return json.loads(path.read_text())


def stores(product: Path) -> list[str]:
    return sorted(p.name for p in (product.parent / "stores").iterdir())


def add_word(product: Path, name: str, script: str) -> None:
    word = product / ".verilex" / "words" / name
    word.mkdir()
    (word / "word.md").write_text(
        f"---\nword: {name}\npromise: A test word.\nrequires: [store]\n"
        "implements: [verify-tally/features/store.md#store-open]\n---\n"
    )
    (word / "run").write_text("#!/bin/sh\ncat >/dev/null\n" + script)
    (word / "run").chmod(0o755)


def test_passing_chain_cleans_up_the_instance_and_keeps_evidence(product):
    done = verilex(product, "run", CHAIN)

    assert done.returncode == 0, done.stdout + done.stderr
    record = last_run(product)
    assert [(w["word"], w["verdict"]) for w in record["words"]] == [
        ("store-open", "pass"),
        ("item-stored", "pass"),
        ("item-listed", "pass"),
    ]
    assert record["cleanup"] == "done"
    assert stores(product) == []
    actions = Path(record["words"][1]["evidence"], "actions.log").read_text()
    assert actions == "$ tally add apple\nexit 0\nadded apple\n\n"
    assert "result: pass\n" in done.stdout


def test_planted_defect_fails_the_word_that_proves_it(product):
    done = verilex(product, "run", CHAIN, TALLY_DEFECT="drop-adds")

    assert done.returncode == 1
    record = last_run(product)
    assert [w["verdict"] for w in record["words"]] == ["pass", "fail"]
    assert record["reason"] == "item-stored apple: tally said 'added apple' but store.json lacks apple"
    assert "fall back to the verify skill: verify-tally/features/items.md#item-add" in done.stdout
    assert [f["step"] for f in record["frame"]] == ["launch", "doctor", "doctor-after-failure", "cleanup"]
    assert stores(product) == []


def test_environment_trouble_is_blocked_not_failed(product):
    done = verilex(product, "run", CHAIN, TALLY_SIMULATE_LOCK="1")

    assert done.returncode == 2
    record = last_run(product)
    assert record["verdict"] == "blocked"
    assert record["words"][0]["reason"].endswith("is locked by another process")
    assert stores(product) == []


def test_chain_against_the_order_of_reality_is_refused_before_launch(product):
    done = verilex(product, "run", "item-stored apple | store-open")

    assert done.returncode == 2
    assert done.stderr == "verilex: refused: item-stored apple requires store; nothing earlier provides it\n"
    assert not (product.parent / "state").exists()
    assert stores(product) == []


def test_unknown_word_and_wrong_arity_are_refused(product):
    assert verilex(product, "run", "store-open | item-sold apple").stderr == (
        "verilex: refused: unknown word 'item-sold'; `verilex words` lists the dictionary\n"
    )
    assert verilex(product, "run", "store-open | item-stored").stderr == (
        "verilex: refused: item-stored takes 1 argument(s) ['name'], got []\n"
    )


def test_instance_the_run_did_not_launch_is_refused_and_left_intact(product, tmp_path):
    someone_elses = tmp_path / "dev-store"
    someone_elses.mkdir()
    (someone_elses / "owner").write_text("a-developer\n")
    (someone_elses / "store.json").write_text('{"items": ["keep-me"]}')

    done = verilex(product, "run", CHAIN, TALLY_ADOPT_STORE=str(someone_elses))

    assert done.returncode == 2
    record = last_run(product)
    assert record["verdict"] == "blocked"
    assert record["reason"] == "doctor refused the instance (exit 1)"
    assert record["words"] == []
    assert (someone_elses / "store.json").read_text() == '{"items": ["keep-me"]}'


@pytest.mark.parametrize(
    ("script", "reason"),
    [
        ('echo \'{"verdict": "pass"}\'\n', "pass without a second observation"),
        (
            'echo \'{"verdict": "fail", "detail": "broken"}\'\nexit 1\n',
            "fail without stating that its preconditions held",
        ),
        ('echo \'{"verdict": "pass", "observation": "fine"}\'\nexit 1\n', "exit 1 disagrees with verdict pass"),
        ("echo all good\n", "stdout is not one result JSON object"),
        (
            (
                'printf -- "-----BEGIN PRIVATE KEY-----\\n" > "$VERILEX_EVIDENCE/key.pem"\n'
                'echo \'{"verdict": "pass", "observation": "fine"}\'\n'
            ),
            "secret pattern in evidence key.pem",
        ),
    ],
)
def test_dishonest_word_results_are_unverified(product, script, reason):
    add_word(product, "store-glanced", script)

    done = verilex(product, "run", "store-open | store-glanced")

    assert done.returncode == 2
    record = last_run(product)
    assert record["words"][-1]["verdict"] == "unverified"
    assert record["words"][-1]["reason"] == reason
    assert stores(product) == []


def test_kept_instance_is_listed_until_cleaned_up(product):
    done = verilex(product, "run", "store-open", "--keep")
    assert done.returncode == 0
    run_id = last_run(product)["run"]
    assert len(stores(product)) == 1

    assert verilex(product, "runs").stdout == f"{run_id}  pass  cleanup=kept  store-open\n"
    assert verilex(product, "cleanup", run_id).stdout == "cleanup: done\n"
    assert stores(product) == []
    assert verilex(product, "runs").stdout == f"{run_id}  pass  cleanup=done  store-open\n"


def test_doctor_refusal_after_word_failure_blocks_the_run(product):
    script = (
        'python3 -c "import json, os, pathlib; '
        "p = pathlib.Path(json.loads(os.environ['VERILEX_INSTANCE'])['store']) / 'owner'; "
        'p.unlink()"\n'
        'echo \'{"verdict": "fail", "preconditions_held": true, "detail": "failed and corrupted"}\'\n'
        "exit 1\n"
    )
    add_word(product, "store-corrupted", script)
    done = verilex(product, "run", "store-open | store-corrupted")

    assert done.returncode == 2
    record = last_run(product)
    assert record["verdict"] == "blocked"
    assert "doctor-after-failure refused the instance" in record["reason"]
    assert "[refused] doctor after failure" in done.stdout


def test_cleanup_evidence_secret_leak_marks_run_unverified(product):
    cleanup_script = product / ".verilex" / "frame" / "cleanup"
    with open(cleanup_script, "a", encoding="utf-8") as f:
        f.write(
            "\nfrom pathlib import Path\n"
            "import os\n"
            '(Path(os.environ["VERILEX_EVIDENCE"]) / "key.pem").write_text("-----BEGIN PRIVATE KEY-----\\n")\n'
        )

    done = verilex(product, "run", "store-open")

    assert done.returncode == 2
    record = last_run(product)
    assert record["verdict"] == "unverified"
    assert record["reason"] == "secret pattern in evidence key.pem"
    assert record["cleanup"] == "done"


def test_word_failure_without_detail_does_not_format_none(product):
    script = 'echo \'{"verdict": "fail", "preconditions_held": true}\'\nexit 1\n'
    add_word(product, "store-flaked", script)
    done = verilex(product, "run", "store-open | store-flaked")

    assert done.returncode == 1
    record = last_run(product)
    assert record["verdict"] == "fail"
    assert record["reason"] == "store-flaked"
    assert "result: fail - store-flaked\n" in done.stdout
    assert ": None" not in done.stdout


def test_management_commands_work_with_malformed_word_dictionary(product):
    broken = product / ".verilex" / "words" / "broken"
    broken.mkdir()
    (broken / "word.md").write_text("not yaml frontmatter at all\n")

    done_runs = verilex(product, "runs")
    assert done_runs.returncode == 0
    assert done_runs.stdout == ""

    done_clean = verilex(product, "cleanup", "no-such-run")
    assert done_clean.returncode == 2
    assert "no-such-run is not a run of tally" in done_clean.stderr


def test_syntax_and_metadata_validation_errors_are_refused_as_verilex_error(product):
    done_quote = verilex(product, "run", 'store-open | "item-stored')
    assert done_quote.returncode == 2
    assert "verilex: refused: invalid word syntax in chain" in done_quote.stderr
    assert "No closing quotation" in done_quote.stderr

    (product / ".verilex" / "config.yaml").write_text("project: tally\nsecret_patterns: ['[']\n")
    done_config = verilex(product, "words")
    assert done_config.returncode == 2
    assert "verilex: refused:" in done_config.stderr
    assert "invalid regex in 'secret_patterns'" in done_config.stderr

    (product / ".verilex" / "config.yaml").write_text("project: tally\n")
    (product / ".verilex" / "words" / "store-open" / "word.md").write_text(
        "---\nword: store-open\npromise: A store is open.\ntimeout: not-a-number\n"
        "implements: [verify-tally/features/store.md#store-open]\n---\n"
    )
    done_timeout = verilex(product, "words")
    assert done_timeout.returncode == 2
    assert "verilex: refused:" in done_timeout.stderr
    assert "'timeout' must be a positive integer" in done_timeout.stderr
