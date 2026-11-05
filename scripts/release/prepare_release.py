#!/usr/bin/env python3
"""Collect local editorial evidence and prepare reviewed release files, without publishing."""

from __future__ import annotations

import argparse
from dataclasses import asdict, replace
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys

from collect_evidence import collect_release_evidence
from evidence import GhGitHubClient
from notes import GenerationError, build_generation_packets
from release_types import canonical_json
from release_actions import (
    CHANGELOG_PATH, EMBEDDED_CHANGELOG_PATH, VERSION_PATH, apply_release_files,
)
from release_intent import (
    MANIFEST_PATH, notes_path, parse_manifest, read_release_file, release_files, validate_prepared,
)
from release_version import source_release_version, validate_next_version


class PreparationError(ValueError):
    """Release preparation needs fresh source or corrected reviewed input."""


EDITORIAL_BUDGET_CHARS = 24_000


def _summary(text: str, limit: int = 240) -> str:
    text = " ".join(text.split())
    return text if len(text) <= limit else text[:limit - 1].rstrip() + "…"


def _coverage_pages(evidence, packet_names: dict[str, list[str]]) -> list[str]:
    """Index every delivery and curated entry without filling the editor with bodies."""
    records = []
    for index, unit in enumerate(evidence.delivery_units):
        pointer = f"/delivery_units/{index}"
        areas = sorted({"/".join(path.split("/")[:2]) for path in unit.changed_paths})
        records.append({
            "kind": "delivery", "unit_id": unit.unit_id,
            "summary": _summary((unit.pull_request.title if unit.pull_request else unit.mainline_commit.subject)
                                or unit.mainline_commit.sha),
            "evidence_pointer": pointer, "packets": packet_names.get(unit.unit_id, []),
            "changed_path_count": len(unit.changed_paths), "changed_areas": areas[:12],
            "all_changed_paths": pointer + "/changed_paths",
        })
        sources = [{"source_id": f"commit:{commit.sha}"} for commit in unit.commits]
        prs = (() if unit.pull_request is None else (unit.pull_request,)) + unit.supporting_pull_requests
        sources.extend({"source_id": f"pr:{pr.number}"} for pr in prs)
        sources.extend({"source_id": f"effort:{effort.effort_id}", "path": effort.path} for effort in unit.efforts)
        # Separate reference rows also fit deliveries with thousands of source identities.
        for source in sources:
            records.append({"kind": "source", "unit_id": unit.unit_id, **source})
    for index, entry in enumerate(evidence.unreleased_entries):
        records.append({
            "kind": "curated", "source_id": f"unreleased:{index + 1}",
            "summary": _summary(entry), "evidence_pointer": f"/unreleased_entries/{index}",
        })

    pages, current = [], []
    def render(items):
        return canonical_json({"evidence_file": "evidence.json", "records": items}) + "\n"
    for record in records:
        if len(render([record])) > EDITORIAL_BUDGET_CHARS:
            raise PreparationError("one coverage reference exceeds the local editorial packet budget")
        if current and len(render([*current, record])) > EDITORIAL_BUDGET_CHARS:
            pages.append(render(current))
            current = []
        current.append(record)
    if current or not pages:
        pages.append(render(current))
    return pages


def write_editorial_evidence(evidence, destination: Path) -> dict:
    """Keep the audit intact, with small optional detail packets and complete coverage."""
    destination.mkdir(parents=True, exist_ok=True)
    (destination / "evidence.json").write_text(evidence.to_json(), encoding="utf-8")
    packet_names, by_unit = [], {}
    fallback_units = []
    for unit in evidence.delivery_units:
        try:
            # The editor follows selected delivery references. Per-delivery
            # packets avoid repeatedly fitting the entire history into context.
            selection = replace(evidence, delivery_units=(unit,), unreleased_entries=())
            packets = build_generation_packets(selection, budget_chars=EDITORIAL_BUDGET_CHARS - 1)
        except GenerationError:
            fallback_units.append(unit.unit_id)
            continue
        for packet in packets:
            name = f"packet-{len(packet_names) + 1}.json"
            (destination / name).write_text(packet.prompt_payload() + "\n", encoding="utf-8")
            packet_names.append(name)
            by_unit.setdefault(unit.unit_id, []).append(name)
    coverage = []
    for index, page in enumerate(_coverage_pages(evidence, by_unit), 1):
        name = f"coverage-{index}.json"
        (destination / name).write_text(page, encoding="utf-8")
        coverage.append(name)
    return {"coverage": coverage, "packets": packet_names,
            "packet_budget_chars": EDITORIAL_BUDGET_CHARS,
            "delivery_unit_count": len(evidence.delivery_units),
            "curated_entry_count": len(evidence.unreleased_entries),
            "detail_fallback_units": fallback_units}


def _git(root: Path, *args: str, check: bool = True) -> str:
    result = subprocess.run(("git", *args), cwd=root, capture_output=True, text=True)
    if check and result.returncode:
        raise PreparationError(f"git {args[0]} failed; resolve the Git error before preparing a release")
    return result.stdout


def _base_tag(root: Path, source: str) -> str:
    tag = _git(root, "describe", "--tags", "--match", "v[0-9]*", "--abbrev=0", source, check=False).strip()
    if tag:
        return tag
    if _git(root, "tag", "--list", "v[0-9]*").strip():
        raise PreparationError("no version tag is reachable from the release source")
    return ""


def _fetch_main(root: Path) -> str:
    _git(root, "fetch", "--tags", "origin", "+refs/heads/main:refs/remotes/origin/main")
    return _git(root, "rev-parse", "refs/remotes/origin/main").strip()


def canonical_source(root: Path, source: str | None = None) -> tuple[str, str]:
    """Pin a clean working tree to freshly fetched main before editorial capture/apply."""
    if _git(root, "status", "--porcelain").strip():
        raise PreparationError("worktree must be clean; preserve unrelated work in its own checkout")
    if not _git(root, "symbolic-ref", "--quiet", "--short", "HEAD", check=False).strip():
        raise PreparationError("release preparation requires an attached branch")
    head = _git(root, "rev-parse", "HEAD").strip()
    if source is not None and head != source:
        raise PreparationError("HEAD differs from the reviewed evidence source")
    if head != _fetch_main(root):
        raise PreparationError("release source must equal freshly fetched origin/main; refresh the notes and source")
    return head, _base_tag(root, head)


def _github_client(root: Path):
    if shutil.which("gh") is None:
        return None
    remote = _git(root, "remote", "get-url", "origin").strip()
    match = re.search(r"(?:github\.com[:/])([^/]+/[^/]+?)(?:\.git)?$", remote)
    return GhGitHubClient(match.group(1)) if match else None


def collect_evidence(root: Path, output: Path | None = None) -> dict:
    source, base = canonical_source(root)
    destination = output or root / ".release" / "preparation" / source
    evidence = collect_release_evidence(root, base, source, release_branch="main", github_client=_github_client(root))
    projection = write_editorial_evidence(evidence, destination)
    result = {
        "source_commit": source,
        "base_tag": base,
        "source_version": source_release_version(_git(root, "show", f"{source}:{VERSION_PATH.as_posix()}")),
        "evidence_fingerprint": evidence.fingerprint,
        "github_status": evidence.github_status,
        **projection,
        "output_dir": str(destination),
        "diagnostics": [asdict(diagnostic) for diagnostic in evidence.diagnostics],
    }
    (destination / "index.json").write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    return result


def _draft(path: Path) -> bytes:
    if path.is_symlink() or not path.is_file():
        raise PreparationError("reviewed draft must be an ordinary file")
    content = path.read_bytes()
    try:
        text = content.decode("utf-8")
    except UnicodeDecodeError as error:
        raise PreparationError("reviewed draft must be UTF-8") from error
    if not text.strip() or "\x00" in text:
        raise PreparationError("reviewed draft must be nonempty Markdown text")
    return content


def changelog_entries(content: str) -> tuple[str, ...]:
    entries: list[str] = []
    for line in content.strip().splitlines():
        if line.startswith("- "):
            if not line[2:].strip():
                raise PreparationError("changelog entries must be nonempty")
            entries.append(line)
        elif entries and (not line.strip() or line.startswith(("  ", "\t"))):
            entries[-1] += "\n" + line
        else:
            raise PreparationError("changelog draft must contain '- ' entries with indented continuation lines")
    if not entries:
        raise PreparationError("changelog draft must contain reviewed entries")
    return tuple(entries)


def _writable_destination(root: Path, relative: Path) -> None:
    path = root / relative
    if path.is_symlink() or not path.resolve().is_relative_to(root.resolve()) or (path.exists() and not path.is_file()):
        raise PreparationError(f"release destination must be an ordinary checkout file: {relative}")


def prepare(root: Path, *, source: str, version: str, notes: Path, changelog: Path, release_date: str) -> dict:
    """Write exactly the reviewed release input set on its temporary branch."""
    narrative = _draft(notes)
    entries = changelog_entries(_draft(changelog).decode("utf-8"))
    head, base = canonical_source(root, source)
    manifest = parse_manifest(json.dumps({
        "schema_version": 1, "source_commit": head, "base_tag": base,
        "version": version, "release_date": release_date,
    }))
    source_version = source_release_version(_git(root, "show", f"{head}:{VERSION_PATH.as_posix()}"))
    validate_next_version(source_version, version)
    if _git(root, "tag", "--list", version).strip():
        raise PreparationError("selected release tag already exists")
    for relative in release_files(version):
        _writable_destination(root, relative)
    if (root / notes_path(version)).exists():
        raise PreparationError("selected release notes already exist; preserve them and choose a new version")
    branch = f"release-prep/{version}"
    current = _git(root, "branch", "--show-current").strip()
    if current != branch:
        _git(root, "switch", "-c", branch)
    apply_release_files(root, version=version, release_date=release_date, changelog_entries=entries)
    output_notes = root / notes_path(version)
    output_notes.parent.mkdir(parents=True, exist_ok=True)
    output_notes.write_bytes(narrative)
    output_manifest = root / MANIFEST_PATH
    output_manifest.parent.mkdir(parents=True, exist_ok=True)
    output_manifest.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    validate_prepared(root)
    return manifest


def _verify_preparation(root: Path) -> dict:
    manifest = parse_manifest(read_release_file(root, MANIFEST_PATH))
    if _git(root, "branch", "--show-current").strip() != f"release-prep/{manifest['version']}":
        raise PreparationError("use the matching release-prep branch to edit or check this preparation")
    if _fetch_main(root) != manifest["source_commit"]:
        raise PreparationError("main changed before approval; refresh preparation and reconcile the reviewed notes")
    if _base_tag(root, manifest["source_commit"]) != manifest["base_tag"]:
        raise PreparationError("previous release tag changed; refresh preparation")
    baseline = source_release_version(_git(root, "show", f"{manifest['source_commit']}:{VERSION_PATH.as_posix()}"))
    validate_next_version(baseline, manifest["version"])
    if _git(root, "tag", "--list", manifest["version"]).strip():
        raise PreparationError("selected release tag already exists")
    changed = set(_git(root, "diff", "--name-only", "-z", manifest["source_commit"]).split("\0"))
    changed.update(_git(root, "ls-files", "--others", "--exclude-standard", "-z").split("\0"))
    changed.discard("")
    owned = {path.as_posix() for path in release_files(manifest["version"])}
    if changed != owned:
        raise PreparationError("preparation must change exactly the five release-owned files; preserve unrelated work separately")
    return manifest


def sync(root: Path) -> dict:
    manifest = _verify_preparation(root)
    changelog = read_release_file(root, CHANGELOG_PATH)
    _writable_destination(root, EMBEDDED_CHANGELOG_PATH)
    (root / EMBEDDED_CHANGELOG_PATH).write_bytes(changelog.encode("utf-8"))
    validate_prepared(root)
    return manifest


def check(root: Path) -> dict:
    manifest = _verify_preparation(root)
    validate_prepared(root)
    return manifest


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=Path(__file__).resolve().parents[2])
    commands = parser.add_subparsers(dest="command", required=True)
    evidence = commands.add_parser("evidence", help="write bounded source evidence for the active local agent")
    evidence.add_argument("--output-dir", type=Path)
    apply = commands.add_parser("prepare", help="write reviewed notes/version/changelog and manifest on a release-prep branch")
    apply.add_argument("--source", required=True, help="full source SHA from the reviewed evidence index")
    apply.add_argument("--version", required=True)
    apply.add_argument("--notes-file", type=Path, required=True)
    apply.add_argument("--changelog-file", type=Path, required=True)
    apply.add_argument("--date", default=datetime.now(timezone.utc).date().isoformat())
    commands.add_parser("sync", help="mirror an edited root changelog into the embedded copy")
    commands.add_parser("check", help="check current preparation/source without changing tracked files")
    args = parser.parse_args(argv)
    root = args.repo_root.resolve()
    try:
        if args.command == "evidence":
            result = collect_evidence(root, args.output_dir)
        elif args.command == "prepare":
            result = prepare(root, source=args.source, version=args.version, notes=args.notes_file,
                             changelog=args.changelog_file, release_date=args.date)
        else:
            result = sync(root) if args.command == "sync" else check(root)
        print(json.dumps(result, indent=2))
        return 0
    except (ValueError, OSError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
