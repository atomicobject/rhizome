"""Evidence capture for manual agent-experience evaluations."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import stat
import subprocess
from pathlib import Path
from typing import Any


TRANSIENT_PREFIXES = (
    ".rhizome/cache",
    ".rhizome/index",
    ".rhizome/tmp/",
)


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _git_paths(repo: Path) -> tuple[set[str], set[str]]:
    def collect(*args: str) -> set[str]:
        result = subprocess.run(
            ["git", "-C", str(repo), "ls-files", "-z", *args],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        return {os.fsdecode(item) for item in result.stdout.split(b"\0") if item}

    return collect("--cached"), collect("--others")


def snapshot(repo: Path) -> dict[str, Any]:
    """Hash every tracked and non-ignored untracked file in a Git worktree."""
    repo = repo.resolve()
    tracked, untracked = _git_paths(repo)
    files: dict[str, dict[str, Any]] = {}
    transient: dict[str, dict[str, Any]] = {}
    for relative in sorted(tracked | untracked):
        path = repo / relative
        if not path.exists() and not path.is_symlink():
            continue
        item: dict[str, Any] = {
            "classification": "tracked" if relative in tracked else "untracked"
        }
        mode = path.lstat().st_mode
        if stat.S_ISLNK(mode):
            target = os.readlink(path)
            resolved = path.resolve(strict=False)
            if not resolved.is_relative_to(repo):
                raise ValueError(f"symlink escapes fixture repository: {relative} -> {target}")
            item.update(kind="symlink", target=target, sha256=sha256_bytes(os.fsencode(target)))
        elif stat.S_ISREG(mode):
            item.update(kind="file", size=path.stat().st_size, sha256=sha256_file(path))
        else:
            item.update(kind="other")
        destination = transient if relative.startswith(TRANSIENT_PREFIXES) else files
        destination[relative] = item
    return {"files": files, "transient": transient}


def changes(before: dict[str, Any], after: dict[str, Any]) -> dict[str, list[str]]:
    old = before.get("files", {})
    new = after.get("files", {})
    return {
        "added": sorted(new.keys() - old.keys()),
        "removed": sorted(old.keys() - new.keys()),
        "modified": sorted(path for path in old.keys() & new.keys() if old[path] != new[path]),
    }


def copy_added_files(repo: Path, after: dict[str, Any], delta: dict[str, list[str]], target: Path) -> None:
    for relative in delta["added"]:
        item = after["files"][relative]
        if item.get("kind") != "file":
            continue
        destination = target / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(repo / relative, destination)


def write_changes_patch(repo: Path, target: Path) -> None:
    command = ["git", "-C", str(repo), "diff", "--binary", "--no-ext-diff", "HEAD", "--"]
    result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        result = subprocess.run(command[:-2] + ["--"], check=True, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE)
    target.write_bytes(result.stdout)


def capture_provenance(repo: Path, run: dict[str, Any], result_dir: Path,
                       adapter_files: tuple[str, ...] = ("codex_adapter.py",)) -> dict[str, Any]:
    """Copy model-visible guidance and record immutable evaluator inputs."""
    guidance_dir = result_dir / "guidance"
    for relative, expected in run["guidance"].items():
        source = repo / relative
        if source.is_symlink() or not source.resolve().is_relative_to(repo.resolve()):
            raise ValueError(f"guidance escapes fixture: {relative}")
        if sha256_file(source) != expected:
            raise ValueError(f"guidance changed before evidence capture: {relative}")
        destination = guidance_dir / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(source, destination)
    scripts = Path(__file__).resolve().parent
    oracle = scripts.parents[1] / "testdata" / "agent-experience" / "oracles" / f"{run['case_id']}.json"
    receipt = Path(run["build_receipt"])
    if sha256_file(receipt) != run["build_receipt_sha256"]:
        raise ValueError("build receipt changed before evidence capture")
    shutil.copyfile(receipt, result_dir / "build-receipt.json")
    checker_files = ("fixtures.py", "fixture_support.py")
    return {
        "adapter": {name: sha256_file(scripts / name) for name in adapter_files},
        "checker": {name: sha256_file(scripts / name) for name in checker_files},
        "registry": {"path": "testdata/agent-experience/registry.json",
                     "sha256": sha256_file(scripts.parents[1] / "testdata" / "agent-experience" / "registry.json")},
        "oracle": {"case_id": run["case_id"], "sha256": sha256_file(oracle)},
        "build_receipt": {"sha256": run["build_receipt_sha256"]},
        "guidance": dict(run["guidance"]),
    }


def parse_events(path: Path) -> dict[str, Any]:
    """Strictly parse JSONL while retaining every malformed record verbatim."""
    records: list[dict[str, Any]] = []
    tool_count = 0
    rhizome_labeled_events = 0
    usage_values: dict[str, int] = {}
    usage_sources: set[str] = set()
    malformed = 0
    completed = False
    if not path.exists():
        return {
            "records": [], "incomplete": True, "malformed_count": 0,
            "terminal_event": None,
            "exposed_tool_invocations": 0, "exposed_rhizome_labeled_tool_events": 0,
            "usage": {"source": None, "input_tokens": None, "output_tokens": None,
                      "cached_tokens": None, "reasoning_tokens": None},
        }
    with path.open("rb") as source:
        for number, raw in enumerate(source, 1):
            text = raw.decode("utf-8", errors="replace").removesuffix("\n")
            try:
                value = json.loads(text)
                if not isinstance(value, dict):
                    raise ValueError("event is not an object")
                records.append({"line": number, "valid": True, "event": value})
                completed = completed or value.get("type") == "turn.completed"
                calls = list(_tool_calls(value))
                tool_count += len(calls)
                rhizome_labeled_events += sum(1 for call in calls if _mentions_rhizome(call))
                for source_name, usage in _usage_objects(value):
                    usage_sources.add(source_name)
                    aliases = {
                        "input_tokens": "input_tokens", "output_tokens": "output_tokens",
                        "cached_tokens": "cached_input_tokens", "reasoning_tokens": "reasoning_output_tokens",
                    }
                    for key, provider_key in aliases.items():
                        amount = usage.get(key)
                        if not isinstance(amount, int) or isinstance(amount, bool):
                            amount = usage.get(provider_key)
                        if isinstance(amount, int) and not isinstance(amount, bool):
                            usage_values[key] = usage_values.get(key, 0) + amount
            except (json.JSONDecodeError, ValueError) as error:
                malformed += 1
                records.append({"line": number, "valid": False, "raw": text, "error": str(error)})
    return {
        "records": records,
        "incomplete": malformed > 0 or not completed,
        "terminal_event": "turn.completed" if completed else None,
        "malformed_count": malformed,
        "exposed_tool_invocations": tool_count,
        "exposed_rhizome_labeled_tool_events": rhizome_labeled_events,
        "usage": {
            "source": sorted(usage_sources) or None,
            **{key: usage_values.get(key) for key in
               ("input_tokens", "output_tokens", "cached_tokens", "reasoning_tokens")},
        },
    }


def _tool_calls(value: Any):
    if isinstance(value, dict):
        event_type = value.get("type")
        if event_type in {"item.started", "item.updated", "response.output_item.added"}:
            return
        if event_type in {"item.completed", "response.output_item.done"}:
            item = value.get("item") or value.get("output_item")
            if isinstance(item, dict) and item.get("type") in {
                "command_execution", "function_call", "tool_call", "mcp_tool_call", "web_search"
            }:
                yield item
            return
        if isinstance(event_type, str) and (
            event_type in {"command_execution", "function_call", "tool_call", "mcp_tool_call", "web_search"}
            or (event_type.endswith("_call") and not event_type.endswith("_call_output"))
        ):
            yield value
            return
        for child in value.values():
            yield from _tool_calls(child)
    elif isinstance(value, list):
        for child in value:
            yield from _tool_calls(child)


def _mentions_rhizome(call: dict[str, Any]) -> bool:
    """Identify only lexical Rhizome references in exposed completed tool events."""
    label = " ".join(str(call.get(key, "")) for key in ("name", "tool", "server", "command"))
    return "rhizome" in label.lower() or "rzm" in label.lower().split()


def _usage_objects(value: Any, trail: str = "event"):
    if isinstance(value, dict):
        usage = value.get("usage")
        if isinstance(usage, dict):
            yield trail + ".usage", usage
        for key, child in value.items():
            if key != "usage":
                yield from _usage_objects(child, trail + "." + str(key))
    elif isinstance(value, list):
        for index, child in enumerate(value):
            yield from _usage_objects(child, f"{trail}[{index}]")
