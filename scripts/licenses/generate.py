#!/usr/bin/env python3
"""Generate third-party notices from the locked dependency inputs.

The default command refreshes generated notices. ``--check`` only compares the
expected output with the checked-in files; it never writes them. Source
fallbacks are checked in under ``licenses/sources`` so ordinary runs are
offline.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
import urllib.request
from dataclasses import dataclass
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
LICENSES = ROOT / "licenses"
SOURCES = LICENSES / "sources"
VENDOR = ROOT / "vendor"
WEB = ROOT / "web"
FETCH_TIMEOUT_SECONDS = 15


@dataclass(frozen=True)
class Source:
    path: str
    url: str


@dataclass(frozen=True)
class NoticeEntry:
    group: str
    title: str
    version: str
    declared: str
    source: str
    directory: Path
    texts: tuple[tuple[str, bytes], ...]


# These source companions cover dependencies which do not ship a standalone
# license file. URLs are immutable tags or commits. Fetch them only explicitly.
FETCHED_SOURCES = (
    Source("go/sqlite-vec-v0.1.6/LICENSE-APACHE", "https://raw.githubusercontent.com/asg017/sqlite-vec/v0.1.6/LICENSE-APACHE"),
    Source("go/sqlite-vec-v0.1.6/LICENSE-MIT", "https://raw.githubusercontent.com/asg017/sqlite-vec/v0.1.6/LICENSE-MIT"),
    Source("web/react-compiler-runtime-19.1.0-rc.1/LICENSE", "https://raw.githubusercontent.com/facebook/react/v19.1.0/LICENSE"),
    Source("web/react-remove-scroll-bar-2.3.8/LICENSE", "https://raw.githubusercontent.com/theKashey/react-remove-scroll-bar/8ca9ba5ea52de03308fe8ced94f7b159a44d28ff/LICENSE"),
    Source("skills/impeccable-4.0.2/LICENSE", "https://raw.githubusercontent.com/pbakaus/impeccable/skill-v4.0.2/LICENSE"),
    Source("skills/impeccable-4.0.2/NOTICE.md", "https://raw.githubusercontent.com/pbakaus/impeccable/skill-v4.0.2/NOTICE.md"),
    Source("skills/greptileai/LICENSE", "https://raw.githubusercontent.com/greptileai/skills/646e2dfad81e5157e97daecc802b68d3d2c4d1e4/LICENSE"),
)


def add_output(outputs: dict[Path, bytes], path: Path, content: bytes) -> None:
    existing = outputs.get(path)
    if existing is not None and existing != content:
        raise RuntimeError(f"conflicting generated content for {path.relative_to(ROOT)}")
    outputs[path] = content


def apply_outputs(outputs: dict[Path, bytes]) -> None:
    for path, content in sorted(outputs.items()):
        path.parent.mkdir(parents=True, exist_ok=True)
        if not path.is_file() or path.read_bytes() != content:
            path.write_bytes(content)


def fetch_sources() -> None:
    for source in FETCHED_SOURCES:
        with urllib.request.urlopen(source.url, timeout=FETCH_TIMEOUT_SECONDS) as response:
            content = response.read()
        if not content:
            raise RuntimeError(f"empty source: {source.url}")
        path = SOURCES / source.path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)


def is_notice(path: Path) -> bool:
    if path.suffix.lower() in {".js", ".mjs", ".ts", ".go", ".py"}:
        return False
    name = path.name.lower().replace("-", "").replace("_", "")
    return name.startswith(("license", "copying", "notice", "thirdparty"))


def safe_name(name: str) -> str:
    return re.sub(r"[^A-Za-z0-9._-]+", "_", name).strip("_")


def normalize_notice(content: bytes) -> bytes:
    """Keep copied notice text readable and free of repository whitespace noise."""
    return b"\n".join(line.rstrip(b" \t\r") for line in content.splitlines()).rstrip() + b"\n"


def go_modules() -> list[tuple[str, str]]:
    modules: list[tuple[str, str]] = []
    for line in (VENDOR / "modules.txt").read_text().splitlines():
        if not line.startswith("# "):
            continue
        fields = line[2:].split()
        if len(fields) >= 2 and fields[1].startswith("v") and "=>" not in fields:
            modules.append((fields[0], fields[1]))
    return modules


def tree_sitter_aliases() -> list[tuple[str, str, str]]:
    aliases: list[tuple[str, str, str]] = []
    for line in (VENDOR / "modules.txt").read_text().splitlines():
        if not line.startswith("# ") or " => " not in line:
            continue
        alias, replacement = line[2:].split(" => ", 1)
        fields = replacement.split()
        if alias.endswith("/bindings/go") and len(fields) >= 2 and fields[1].startswith("v"):
            aliases.append((alias, fields[0], fields[1]))
    return aliases


def dependency_files(directory: Path) -> list[Path]:
    return sorted(path for path in directory.rglob("*") if path.is_file() and is_notice(path))


def license_section(readme: Path) -> bytes:
    text = readme.read_text(encoding="utf-8")
    match = re.search(r"^#{1,6}\s+License\s*$\n?(.*)$", text, re.MULTILINE | re.DOTALL)
    if not match:
        raise RuntimeError(f"no License section in {readme.relative_to(ROOT)}")
    return match.group(1).strip().encode() + b"\n"


def npm_source(package: dict, name: str, version: str, installed: dict) -> str:
    if name == "elkjs":
        return f"https://github.com/eclipse-elk/elk/tree/v{version}"
    repository = package.get("repository", "") or installed.get("repository", "")
    if isinstance(repository, dict):
        repository = repository.get("url", "")
    repository = str(repository).removeprefix("git+").removeprefix("git://").removesuffix(".git")
    if repository.startswith("github:"):
        repository = "https://github.com/" + repository.removeprefix("github:")
    return repository or f"https://www.npmjs.com/package/{name}/v/{version}"


def web_fallback(name: str) -> tuple[tuple[tuple[str, bytes], ...], str] | None:
    if name in {"@graphiql/plugin-doc-explorer", "@graphiql/plugin-history"}:
        return (("LICENSE", (WEB / "node_modules/@graphiql/react/LICENSE").read_bytes()),), "https://github.com/graphql/graphiql (same GraphiQL upstream license)"
    if name in {"fastdom", "strictdom"}:
        return (("LICENSE", license_section(WEB / "node_modules" / name / "README.md")),), f"https://github.com/wilsonpage/{name} (license section in installed README)"
    if name == "react-compiler-runtime":
        return (("LICENSE", (SOURCES / "web/react-compiler-runtime-19.1.0-rc.1/LICENSE").read_bytes()),), "https://github.com/facebook/react/tree/v19.1.0 (v19.1.0 source-family text for locked 19.1.0-rc.1 package)"
    if name == "react-remove-scroll-bar":
        return (("LICENSE", (SOURCES / "web/react-remove-scroll-bar-2.3.8/LICENSE").read_bytes()),), "https://github.com/theKashey/react-remove-scroll-bar/tree/8ca9ba5ea52de03308fe8ced94f7b159a44d28ff (pinned source-family text for locked 2.3.8 package)"
    return None


def plan_entry(outputs: dict[Path, bytes], entry: NoticeEntry) -> None:
    for filename, content in entry.texts:
        add_output(outputs, entry.directory / safe_name(filename), normalize_notice(content))


def generate_go(outputs: dict[Path, bytes]) -> list[NoticeEntry]:
    entries: list[NoticeEntry] = []
    for module, version in go_modules():
        module_dir = VENDOR / module
        if module == "github.com/asg017/sqlite-vec-go-bindings":
            texts = (
                ("LICENSE-APACHE", (SOURCES / "go/sqlite-vec-v0.1.6/LICENSE-APACHE").read_bytes()),
                ("LICENSE-MIT", (SOURCES / "go/sqlite-vec-v0.1.6/LICENSE-MIT").read_bytes()),
            )
            source = "https://github.com/asg017/sqlite-vec/tree/v0.1.6"
        else:
            texts = tuple((str(path.relative_to(module_dir)), path.read_bytes()) for path in dependency_files(module_dir))
            source = f"vendor/{module}"
        if not texts:
            raise RuntimeError(f"missing vendored license text: {module} {version}")
        entry = NoticeEntry("Go", module, version, "vendored source", source, LICENSES / "go" / f"{safe_name(module)}@{version}", texts)
        plan_entry(outputs, entry)
        entries.append(entry)

    for alias, parent, version in tree_sitter_aliases():
        content = (VENDOR / parent / "LICENSE").read_bytes()
        entry = NoticeEntry("Go", alias, version, "parent language module license", f"vendor/{parent}/LICENSE", LICENSES / "go" / f"{safe_name(alias)}@{version}", (("LICENSE", content),))
        plan_entry(outputs, entry)
        entries.append(entry)
    return entries


def generate_web(outputs: dict[Path, bytes]) -> list[NoticeEntry]:
    lock = json.loads((WEB / "package-lock.json").read_text())
    packages = sorted(
        (path.removeprefix("node_modules/"), metadata)
        for path, metadata in lock["packages"].items()
        if path.startswith("node_modules/") and not metadata.get("dev", False)
    )
    entries: list[NoticeEntry] = []
    for install_path, metadata in packages:
        name = install_path.rsplit("node_modules/", 1)[-1]
        version = metadata["version"]
        directory = WEB / "node_modules" / install_path
        installed_metadata = json.loads((directory / "package.json").read_text())
        if installed_metadata.get("version") != version:
            raise RuntimeError(f"stale node_modules: {install_path} has {installed_metadata.get('version')!r}, lock requires {version!r}")
        files = [path for path in directory.iterdir() if path.is_file() and is_notice(path)]
        if name == "dompurify":
            files = [directory / "LICENSE"]
        fallback = web_fallback(name) if not files else None
        if fallback:
            texts, source = fallback
        else:
            if not files:
                raise RuntimeError(f"missing web license text: {name} {version}")
            texts = tuple((path.name, path.read_bytes()) for path in sorted(files))
            source = npm_source(metadata, name, version, installed_metadata)
        entry = NoticeEntry("Web", f"{name} ({install_path})", version, metadata.get("license") or installed_metadata.get("license") or "not declared in package metadata; see included license text", source, LICENSES / "web" / f"{safe_name(install_path)}@{version}", texts)
        plan_entry(outputs, entry)
        entries.append(entry)
    return entries


def generate_skills(outputs: dict[Path, bytes]) -> list[NoticeEntry]:
    entries: list[NoticeEntry] = []
    impeccable = (
        ("LICENSE", (SOURCES / "skills/impeccable-4.0.2/LICENSE").read_bytes()),
        ("NOTICE", (SOURCES / "skills/impeccable-4.0.2/NOTICE.md").read_bytes()),
    )
    for path in [ROOT / ".agents/skills/impeccable", ROOT / ".claude/skills/impeccable", ROOT / ".github/skills/impeccable"]:
        for filename, content in impeccable:
            add_output(outputs, path / filename, content)
    entry = NoticeEntry("Skills", "Impeccable copied skills", "4.0.2", "Apache-2.0", "https://github.com/pbakaus/impeccable/tree/skill-v4.0.2", LICENSES / "skills" / "impeccable@4.0.2", impeccable)
    plan_entry(outputs, entry)
    entries.append(entry)

    greptile_license = (("LICENSE", (SOURCES / "skills/greptileai/LICENSE").read_bytes()),)
    for path in [ROOT / ".agents/skills/check-pr", ROOT / ".claude/skills/check-pr", ROOT / ".agents/skills/greploop", ROOT / ".claude/skills/greploop"]:
        add_output(outputs, path / "LICENSE", greptile_license[0][1])
    entry = NoticeEntry("Skills", "Greptile copied skills (check-pr, greploop)", "1.3", "MIT", "https://github.com/greptileai/skills/tree/646e2dfad81e5157e97daecc802b68d3d2c4d1e4", LICENSES / "skills" / "greptile@1.3", greptile_license)
    plan_entry(outputs, entry)
    entries.append(entry)

    return entries


def render_full_entry(entry: NoticeEntry) -> str:
    output = [f"## {entry.title} {entry.version}", "", f"Declared license: {entry.declared}", f"Source: {entry.source}", ""]
    for filename, content in entry.texts:
        output.extend([f"### {filename}", "", normalize_notice(content).decode("utf-8", errors="replace").rstrip(), ""])
    return "\n".join(output).rstrip() + "\n"


def render_index(entries: list[NoticeEntry]) -> bytes:
    lines = [
        "# Third-party license index",
        "",
        "Generated by `python3 scripts/licenses/generate.py`. Do not edit by hand.",
        "Full license and notice text is stored in `licenses/**`. The browser notice is `web/public/third-party-licenses.txt`.",
        "",
    ]
    for group in ("Go", "Web", "Skills"):
        lines.extend([f"## {group}", "", "| Package | Version | Declared license | Full text directory | Source |", "| --- | --- | --- | --- | --- |"])
        for entry in (item for item in entries if item.group == group):
            lines.append(f"| {entry.title} | {entry.version} | {entry.declared} | `{entry.directory.relative_to(ROOT)}` | {entry.source} |")
        lines.append("")
    return ("\n".join(lines).rstrip() + "\n").encode()


def stale_outputs(outputs: dict[Path, bytes]) -> list[Path]:
    generated_roots = (LICENSES / "go", LICENSES / "web", LICENSES / "skills")
    files: set[Path] = {ROOT / "THIRD_PARTY_LICENSES.md", WEB / "public/third-party-licenses.txt"}
    for root in generated_roots:
        if root.exists():
            files.update(path for path in root.rglob("*") if path.is_file())
    return sorted(path for path in files if path not in outputs)


def check_outputs(outputs: dict[Path, bytes]) -> None:
    missing = [path for path in sorted(outputs) if not path.is_file()]
    changed = [path for path in sorted(outputs) if path.is_file() and path.read_bytes() != outputs[path]]
    extra = stale_outputs(outputs)
    if missing or changed or extra:
        details = [
            *(f"missing: {path.relative_to(ROOT)}" for path in missing),
            *(f"stale: {path.relative_to(ROOT)}" for path in changed),
            *(f"stale generated file: {path.relative_to(ROOT)}" for path in extra),
        ]
        shown = "\n".join(details[:20])
        suffix = "\n..." if len(details) > 20 else ""
        raise RuntimeError(f"generated notices are out of date ({len(details)} differences):\n{shown}{suffix}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--fetch-sources", action="store_true", help="download pinned primary-source fallbacks")
    parser.add_argument("--check", action="store_true", help="verify generated outputs without writing them")
    args = parser.parse_args()
    if args.check and args.fetch_sources:
        parser.error("--check cannot be combined with --fetch-sources")
    if args.fetch_sources:
        fetch_sources()
    missing_sources = [source.path for source in FETCHED_SOURCES if not (SOURCES / source.path).is_file()]
    if missing_sources:
        raise RuntimeError("missing fallback source files; run with --fetch-sources: " + ", ".join(missing_sources))

    outputs: dict[Path, bytes] = {}
    go_entries = generate_go(outputs)
    web_entries = generate_web(outputs)
    skill_entries = generate_skills(outputs)
    go_module_count = len(go_modules())
    go_alias_count = len(tree_sitter_aliases())
    entries = go_entries + web_entries + skill_entries
    add_output(outputs, ROOT / "THIRD_PARTY_LICENSES.md", render_index(entries))
    web_notice = ("Third-party web dependency license notices\nGenerated by `python3 scripts/licenses/generate.py`.\n\n" + "\n".join(render_full_entry(entry) for entry in web_entries)).rstrip() + "\n"
    add_output(outputs, WEB / "public/third-party-licenses.txt", web_notice.encode())

    if args.check:
        check_outputs(outputs)
        print(f"license notices are current for {go_module_count} vendored Go modules, {go_alias_count} replacement aliases, {len(web_entries)} non-development web packages, and {len(skill_entries)} copied skills")
    else:
        apply_outputs(outputs)
        print(f"generated notices for {go_module_count} vendored Go modules, {go_alias_count} replacement aliases, {len(web_entries)} non-development web packages, and {len(skill_entries)} copied skills")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except RuntimeError as error:
        print(f"license generation failed: {error}", file=sys.stderr)
        raise SystemExit(1)
