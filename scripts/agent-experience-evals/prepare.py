#!/usr/bin/env python3
"""Materialize one disposable treatment without launching an evaluation model."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import shlex
import subprocess
import time

import codex_adapter
import fixtures


def prepare(case_id: str, arm: str, repo: Path, binary: Path, source_sha: str,
            support: Path, output: Path) -> dict:
    started = time.monotonic()
    repo, binary, support, output = (p.resolve() for p in (repo, binary, support, output))
    if any(p.is_relative_to(repo) for p in (support, output, binary)):
        raise ValueError("support, output and selected binary must be outside the model workspace")
    if len(source_sha) != 40 or any(c not in "0123456789abcdef" for c in source_sha):
        raise ValueError("source_sha must be the full lowercase source revision")
    if support.exists():
        raise ValueError("support directory already exists; prepare never overwrites evidence")
    case = fixtures.materialize(case_id, repo)
    support.mkdir(parents=True)
    (support / "fixture.json").write_text(json.dumps(case, indent=2) + "\n")
    source_inventory = {p.relative_to(repo).as_posix(): codex_adapter.sha256(p)
                        for p in repo.rglob("*") if p.is_file()}
    shell_dir = repo / "scripts"
    shell_dir.mkdir()
    launcher = shell_dir / "rzm"
    launcher.write_text("#!/bin/sh\nexec " + shlex.quote(str(binary)) + ' "$@"\n')
    launcher.chmod(0o755)
    config_dir = repo / ".rhizome"
    config_dir.mkdir()
    code = "true" if (repo / "src").is_dir() else "false"
    (config_dir / "config.yml").write_text(
        "rhizome:\n  devBinaryDir: bin\nnoteEmbeddings:\n  enabled: false\ncodeEmbeddings:\n  enabled: false\n"
        f"code:\n  enabled: {code}\n  python:\n    roots: [src]\n"
        "agents:\n  codex: on\n  claude: off\n  cursor: off\n  agentSkills: on\n  agentsmd: on\n"
    )
    run = {"id": f"{case_id.lower()}-{arm}", "case_id": case_id, "arm": arm,
           "repo": str(repo), "binary": str(binary), "binary_sha256": codex_adapter.sha256(binary),
           "source_sha": source_sha, "prompt": case["prompt"],
           "fixture": source_inventory, "support": str(support)}
    receipt = binary.parent / "build.json"
    run["build_receipt"] = str(receipt)
    run["build_receipt_sha256"] = codex_adapter.sha256(receipt)
    env = codex_adapter.environment({}, run)
    steps = []
    def step(argv: list[str], *, expected: int = 0) -> str:
        t0 = time.monotonic()
        p = subprocess.run(argv, cwd=repo, env=env, capture_output=True, text=True, timeout=120)
        steps.append({"argv": argv, "seconds": time.monotonic() - t0,
                      "exit_code": p.returncode, "stdout": p.stdout, "stderr": p.stderr})
        (support / "setup.json").write_text(json.dumps(steps, indent=2) + "\n")
        if p.returncode != expected:
            raise ValueError(f"setup command failed: {argv[0:3]}: {p.stderr[-500:]}")
        return p.stdout
    step(["git", "init", "-q"])
    step(["git", "config", "user.name", "Synthetic Evaluation"])
    step(["git", "config", "user.email", "synthetic-evaluation@example.invalid"])
    step([str(binary), "--version"])
    init = [str(binary), "init", "--path", str(repo), "--workflow", case["installation"],
            "--agents", "codex"]
    step(init)
    first = codex_adapter.inventory(repo)
    step(init)
    if first != codex_adapter.inventory(repo):
        raise ValueError("second init changed installed guidance")
    # Existing index/projection writes are explicit setup, not source mutation.
    if case_id != "A06":
        step([str(binary), "index"])
    step([str(binary), "validate"])
    if case["installation"] != "none":
        step([str(binary), "agent", "current-user", "set", "Synthetic Approver"])
        step([str(binary), "agent", "current-user", "validate"])
    step(["git", "add", "."])
    step(["git", "commit", "-qm", "Seed synthetic evaluation treatment"])
    run["fixture_commit"] = step(["git", "rev-parse", "HEAD"]).strip()
    run["fixture_tree"] = step(["git", "rev-parse", "HEAD^{tree}"]).strip()
    run["guidance"] = codex_adapter.inventory(repo)
    run["isolation"] = codex_adapter.prepare_isolation(
        repo, support / "isolation", [Path(__file__).resolve().parents[2], support, output])
    run["setup_seconds"] = time.monotonic() - started
    run["setup_sha256"] = codex_adapter.sha256(support / "setup.json")
    (support / "run.json").write_text(json.dumps(run, indent=2) + "\n")
    return run


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--case-id", required=True, choices=sorted(fixtures.CASES))
    p.add_argument("--arm", required=True, choices=["baseline", "candidate"])
    p.add_argument("--repo", required=True, type=Path)
    p.add_argument("--binary", required=True, type=Path)
    p.add_argument("--source-sha", required=True)
    p.add_argument("--support", required=True, type=Path)
    p.add_argument("--output", required=True, type=Path)
    args = p.parse_args()
    print(json.dumps(prepare(args.case_id, args.arm, args.repo, args.binary,
                             args.source_sha, args.support, args.output), indent=2))


if __name__ == "__main__":
    main()
