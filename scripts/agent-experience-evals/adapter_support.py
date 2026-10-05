"""Shared immutable fixture validation for evaluation adapters."""

from __future__ import annotations

import hashlib
import json
import subprocess
from pathlib import Path


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def inventory(root: Path) -> dict[str, str]:
    result = {}
    for rel in ("AGENTS.md", "CLAUDE.md", "CONTEXT.md", ".agents", ".codex",
                ".rhizome/config.yml", ".rhizome/workflows.yml"):
        path = root / rel
        files = [path] if path.is_file() else sorted(path.rglob("*")) if path.is_dir() else []
        for file in files:
            if file.is_file():
                if file.is_symlink() or not file.resolve().is_relative_to(root.resolve()):
                    raise ValueError(f"guidance escapes fixture: {file}")
                result[file.relative_to(root).as_posix()] = sha256(file)
    return result


def validate_run_sources(run: dict) -> Path:
    repo = Path(run["repo"]).resolve()
    if not (repo / ".git").is_dir():
        raise ValueError("fixture is not an isolated Git repository")
    if sha256(Path(run["binary"])) != run["binary_sha256"]:
        raise ValueError("selected binary changed")
    receipt_path = Path(run["build_receipt"])
    receipt = json.loads(receipt_path.read_text())
    if (sha256(receipt_path) != run["build_receipt_sha256"]
            or receipt["source_sha"] != run["source_sha"]
            or receipt["binary_sha256"] != run["binary_sha256"]):
        raise ValueError("source build receipt changed or disagrees")
    if inventory(repo) != run["guidance"]:
        raise ValueError("installed guidance/config changed")
    for rel, digest in run["fixture"].items():
        path = repo / rel
        if path.is_symlink() or not path.resolve().is_relative_to(repo) or sha256(path) != digest:
            raise ValueError(f"fixture source changed: {rel}")
    def git(*args: str) -> str:
        return subprocess.check_output(["git", *args], cwd=repo, text=True).strip()
    if git("rev-parse", "HEAD") != run["fixture_commit"] or git("rev-parse", "HEAD^{tree}") != run["fixture_tree"]:
        raise ValueError("fixture Git baseline changed")
    if git("status", "--porcelain", "--untracked-files=all"):
        raise ValueError("fixture has changes before launch")
    if sha256(Path(run["support"]) / "setup.json") != run["setup_sha256"]:
        raise ValueError("setup evidence changed")
    return repo
