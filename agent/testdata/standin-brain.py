#!/usr/bin/env python3
"""Scripted Linux brain: fixed probes, no model and no external network dependency."""

import json
import os
from pathlib import Path
import socket
import subprocess
from urllib.parse import urlsplit

home = Path(os.environ["HOME"])
project = Path.cwd()
report = {"probes": {}, "commands": []}


def probe(label, action):
    # Only OS refusals count as denials. A missing tool, Python error or timeout
    # must stop the script, so the host test fails on an incomplete report.
    try:
        report["probes"][label] = action()
    except OSError:
        report["probes"][label] = False


def read_marker(path):
    return Path(path).read_text() == "standin-marker\n"


def write_marker(path):
    Path(path).write_text("standin-marker\n")
    return read_marker(path)


def mutate(action):
    action()
    return True


probe("read project", lambda: read_marker(project / "standin-marker"))
probe("read system", lambda: bool(Path("/etc/os-release").read_text()))
probe("read brain", lambda: bool(Path(__file__).read_text()))
probe("read launcher", lambda: bool(Path(os.environ["STANDIN_LAUNCHER"]).read_bytes()))
probe("read credential", lambda: read_marker(home / ".standin/auth"))
probe("write home", lambda: write_marker(home / "writable"))
probe("write tmp", lambda: write_marker(Path(os.environ["TMPDIR"]) / "writable"))
probe("write project", lambda: write_marker(project / "standin-marker"))
probe("create project file", lambda: write_marker(project / "escaped"))
probe("mkdir project", lambda: mutate(lambda: (project / "escaped-dir").mkdir()))
probe("rename project file", lambda: mutate(lambda: (project / "standin-marker").rename(project / "escaped")))
probe("unlink project file", lambda: mutate(lambda: (project / "standin-marker").unlink()))
probe("chmod project file", lambda: mutate(lambda: (project / "standin-marker").chmod(0o777)))
probe("symlink in project", lambda: mutate(lambda: (project / "escaped-link").symlink_to(home / "writable")))
probe("hardlink from project", lambda: mutate(lambda: os.link(project / "standin-marker", home / "hardlink")))
probe("write credential", lambda: write_marker(home / ".standin/auth"))
probe("read other home file", lambda: read_marker(os.environ["STANDIN_OTHER_HOME"]))
probe("write system", lambda: write_marker("/etc/standin-escaped"))
# /tmp is private scratch space. Host-looking paths here may be writable, but
# the host test checks that these writes never reach the corresponding host paths.
probe("write tmp lookalike outside", lambda: write_marker(os.environ["STANDIN_OUTSIDE"]))
probe("write brain executable", lambda: write_marker(__file__))

for label, variable in [("ledger", "STANDIN_LEDGER"), ("verilex home", "STANDIN_STATE"),
                        ("caller home", "VERILEX_HOME"), ("caller ledger", "VERILEX_LEDGER")]:
    # The last two paths were supplied under separate names: the sandbox drops
    # VERILEX_HOME and VERILEX_LEDGER themselves.
    target = Path(os.environ["STANDIN_" + variable] if variable.startswith("VERILEX_") else os.environ[variable])
    probe("read " + label, lambda target=target: read_marker(target / "standin-marker"))
    probe("write " + label, lambda target=target: write_marker(target / "escaped"))
    link = home / (label.replace(" ", "-") + "-link")
    link.symlink_to(target)
    probe("symlink read " + label, lambda link=link: read_marker(link / "standin-marker"))

root = Path(os.environ["VERILEX_AGENT_PROMPT"]).parent.parent
probe("read prompt", lambda: "# Task" in Path(os.environ["VERILEX_AGENT_PROMPT"]).read_text())
probe("read logs", lambda: mutate(lambda: (root / "log/verify.out").read_bytes()))
probe("write logs", lambda: write_marker(root / "log/escaped"))
probe("write tmp lookalike state", lambda: write_marker(root / "sandbox/verify/escaped"))
report["probes"]["state env removed"] = "VERILEX_HOME" not in os.environ and "VERILEX_LEDGER" not in os.environ
report["probes"]["proxy env set"] = all(os.environ[name] == os.environ["HTTPS_PROXY"] for name in ["HTTP_PROXY", "ALL_PROXY", "https_proxy", "http_proxy", "all_proxy"])
report["probes"]["no controlling terminal"] = not os.isatty(0) and not os.isatty(1) and not os.isatty(2)
report["probes"]["private namespaces"] = all(
    os.readlink("/proc/self/ns/" + name) != host
    for name, host in json.loads(os.environ["STANDIN_NAMESPACES"]).items()
)
probe("open terminal", lambda: mutate(lambda: os.open("/dev/tty", os.O_RDWR)))
probe("see host process", lambda: mutate(lambda: os.kill(int(os.environ["STANDIN_HOST_PID"]), 0)))

subprocess.run(["sh", "-c", 'printf "child-marker\\n" > "$HOME/child"'], check=True)
report["probes"]["start child"] = (home / "child").read_text() == "child-marker\n"


def local_socket(network, address):
    with socket.socket(network) as listener:
        listener.bind(address)
        listener.listen(1)
        with socket.socket(network) as client:
            client.connect(listener.getsockname())
            conn, _ = listener.accept()
            with conn:
                client.sendall(b"marker")
                return conn.recv(6) == b"marker"


probe("own unix socket", lambda: local_socket(socket.AF_UNIX, str(home / "own.sock")))
probe("own tcp socket", lambda: local_socket(socket.AF_INET, ("127.0.0.1", 0)))


def connect(network, address):
    with socket.socket(network) as conn:
        conn.settimeout(2)
        conn.connect(address)
        return True


probe("host loopback", lambda: connect(socket.AF_INET, ("127.0.0.1", int(os.environ["STANDIN_HOST_PORT"]))))
probe("host unix socket", lambda: connect(socket.AF_UNIX, os.environ["STANDIN_HOST_SOCKET"]))
probe("host abstract socket", lambda: connect(socket.AF_UNIX, "\0" + os.environ["STANDIN_HOST_ABSTRACT"]))

proxy = urlsplit(os.environ["HTTPS_PROXY"])


def proxy_status(request):
    with socket.create_connection((proxy.hostname, proxy.port), timeout=3) as conn:
        conn.sendall(request.encode())
        response = b""
        while b"\r\n" not in response:
            chunk = conn.recv(1024)
            if not chunk:
                raise RuntimeError("proxy returned no status line")
            response += chunk
        return int(response.split(b" ")[1])


# Fixed boundary cases, not fuzzing. No public service or DNS is needed.
for target in ["127.0.0.1", "[::1]", "[::ffff:127.0.0.1]", "10.0.0.1", "172.16.0.1", "192.168.1.1",
               "169.254.169.254", "100.64.0.1", "192.0.2.1", "198.18.0.1", "224.0.0.1", "240.0.0.1",
               "[fc00::1]", "[fe80::1]", "[2001:db8::1]", "[64:ff9b::7f00:1]"]:
    report["probes"]["proxy " + target] = proxy_status(f"CONNECT {target}:9 HTTP/1.1\r\nHost: {target}:9\r\n\r\n")
# An invalid port makes dialing fail locally, after the public-address policy
# accepts the target. This proves admission without contacting a public service.
for target in ["8.8.8.8", "[::ffff:8.8.8.8]", "[2606:4700:4700::1111]"]:
    report["probes"]["proxy public " + target] = proxy_status(f"CONNECT {target}:65536 HTTP/1.1\r\nHost: {target}:65536\r\n\r\n")
report["probes"]["proxy plain http"] = proxy_status("GET http://127.0.0.1/ HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")
report["probes"]["proxy malformed connect"] = proxy_status("CONNECT missing-port HTTP/1.1\r\nHost: missing-port\r\n\r\n")

(home / "ticket.yaml").write_text("intent: prove the store opens\nharness: forged\nmodel: forged\n")
for case in json.loads(Path(os.environ["STANDIN_COMMANDS"]).read_text()):
    args = [arg.replace("$HOME", str(home)) for arg in case["args"]]
    result = subprocess.run(args, input="x" * case.get("stdin_size", 0), text=True, capture_output=True, timeout=20)
    report["commands"].append({"label": case["label"], "exit": result.returncode,
                               "stdout": result.stdout, "stderr": result.stderr})

(home / "report.json").write_text(json.dumps(report, sort_keys=True))
print("BRAIN SAYS GREEN")
