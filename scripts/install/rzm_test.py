#!/usr/bin/env python3
"""Smoke tests for the hosted install-rzm.sh bootstrap script."""

from __future__ import annotations

import hashlib
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import tarfile
import tempfile
import threading
import unittest


def write_legacy_managed_launcher(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(
        Path(__file__).with_name("testdata") / "legacy-launcher-pre-explicit.sh",
        path,
    )
    path.chmod(0o755)


def installer_accepting_sanitized_legacy_launcher(directory: Path) -> Path:
    """Copy the installer so it also recognizes the sanitized legacy launcher fixture.

    The fixture replaces the private mirror URL of the real launcher whose
    fingerprint is 1f85ac28..., so its whole-file hash differs. Production
    recognition stays unchanged; only this test copy adds the fixture's hash.
    """
    source = Path(__file__).with_name("install-rzm.sh").read_text()
    fixture = Path(__file__).with_name("testdata") / "legacy-launcher-pre-explicit.sh"
    marker = "    1f85ac28d8d853d36e8432305011e9e21dfae6f30529161897ef59131eabe665|\\\n"
    if marker not in source:
        raise AssertionError("installer no longer lists the real legacy launcher fingerprint")
    fixture_sha = hashlib.sha256(fixture.read_bytes()).hexdigest()
    path = directory / "install-rzm-under-test.sh"
    path.write_text(source.replace(marker, f"    {fixture_sha}|\\\n" + marker, 1))
    path.chmod(0o755)
    return path


class InstallScriptContractTest(unittest.TestCase):
    def setUp(self) -> None:
        self.script = Path(__file__).with_name("install-rzm.sh").read_text()

    def test_onboarding_verification_matches_agent_surface_contract(self) -> None:
        onboarding = Path(__file__).resolve().parents[2].joinpath(
            "docs/reference/guides/getting-started.md"
        ).read_text()
        self.assertIn(
            "./bin/rzm init --workflow agentic-engineering",
            onboarding,
        )
        self.assertIn("test -s .rhizome/config.yml", onboarding)
        self.assertIn("BEGIN RZM INIT RHIZOME BLOCK", onboarding)
        self.assertIn("test -f .agents/skills/rhizome/SKILL.md ||", onboarding)
        self.assertIn("test -f .claude/skills/rhizome/SKILL.md", onboarding)
        self.assertIn(
            './bin/rzm agent start --intent "verify Rhizome integration"',
            onboarding,
        )

    def test_init_candidate_keeps_removed_flags_only_for_older_pins(self) -> None:
        helper = re.search(r"^init_candidate_args\(\) \{\n.*?^\}\n", self.script, re.M | re.S)
        self.assertIsNotNone(helper)
        legacy = "init --agentsmd on --agent-skills on --yes"
        for version, want in {
            "v0.49.9": legacy, "v0.50.5": legacy, "v0.50.6": "init",
            "v0.51.0": "init", "v1.0.0": "init", "dev": "init",
        }.items():
            result = subprocess.run(
                ["bash", "-c", helper.group(0) + 'init_candidate_args "$1"', "bash", version],
                text=True, capture_output=True, check=True,
            )
            self.assertEqual(want, result.stdout.strip(), version)

    def test_supports_repo_launcher_install_contract(self) -> None:
        self.assertIn("Usage:", self.script)
        self.assertIn("install-rzm.sh --project <root> [--launcher <relative-path>]", self.script)
        self.assertIn("--user-binary auto|install|skip", self.script)
        self.assertIn("[--yes] [--json]", self.script)
        self.assertIn("install-rzm.sh --user", self.script)
        self.assertIn(".rhizome/bin/${os_name}-${arch_name}/${exe}", self.script)
        self.assertIn("bin/${os_name}/${exe}", self.script)
        self.assertIn("Project root", self.script)
        self.assertIn('"bin/rzm"', self.script)
        self.assertNotIn('"scripts/' + 'rzm"', self.script)
        self.assertIn(".rhizome/.gitignore", self.script)
        self.assertNotIn("ATOMIC_RHIZOME_KEY", self.script)

    def test_verifies_sha256_before_installing(self) -> None:
        self.assertIn("shasum -a 256", self.script)
        self.assertIn("sha256sum", self.script)
        self.assertIn("sha256 mismatch", self.script)
        self.assertLess(self.script.index("sha256 mismatch"), self.script.index("install -m 0755"))

    def test_extracts_only_the_platform_binary_from_release_archives(self) -> None:
        self.assertIn('tar -xzf "$archive" -C "$extract" "$exe"', self.script)
        self.assertNotIn('tar -xzf "$archive" -C "$extract"\n', self.script)

    def test_legacy_launcher_fingerprint_allowlist_remains_complete(self) -> None:
        expected = {
            "1f85ac28d8d853d36e8432305011e9e21dfae6f30529161897ef59131eabe665",
            "97ed150b2437bc65b0d8497a8795773c76c01ae746bafcd641610a47300008c8",
            "72a5bffd1bc3a909efce11269e554af0e11b40bf04774c1f1de5b9f3974bce02",
            "2fbde795e4d82a2703c75755f8d239b9e51c759fccfc6e71663eec44036a08a2",
            "95d0da1eef6b338d773c1b1c7d78cd77a5c908ae7f3ac761b16449f6e2eb6a66",
        }
        match = re.search(
            r"is_legacy_managed_launcher\(\) \{(?P<body>.*?)\n\}",
            self.script,
            re.DOTALL,
        )
        self.assertIsNotNone(match)
        actual = set(re.findall(r"\b[0-9a-f]{64}\b", match.group("body")))
        self.assertEqual(expected, actual)

    def test_binary_version_probes_disable_repo_delegation(self) -> None:
        self.assertIn(
            'RZM_SKIP_REPO_DELEGATE=1 "$binary" --version',
            self.script,
        )
        self.assertIn(
            'RZM_SKIP_REPO_DELEGATE=1 "$target" --version',
            self.script,
        )

    @unittest.skipUnless(os.name == "posix", "launcher integration fixture is POSIX-only")
    def test_repo_launcher_bootstraps_pinned_platform_binary(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            script_path = installer_accepting_sanitized_legacy_launcher(root)
            server_root = root / "server"
            repo = root / "repo"
            server_root.mkdir()
            repo.mkdir()
            version = "v9.8.7"
            archive = self._fake_archive(server_root, version)
            sha = hashlib.sha256(archive.read_bytes()).hexdigest()
            installer = server_root / "install-rzm.sh"
            installer.write_text(script_path.read_text())
            installer.chmod(0o755)
            installer_sha = hashlib.sha256(installer.read_bytes()).hexdigest()
            (server_root / "install-rzm.sh.sha256").write_text(
                f"{installer_sha}  install-rzm.sh\n"
            )
            pinned_version = "v8.7.6"
            pinned_dir = server_root / pinned_version
            pinned_dir.mkdir()
            pinned_archive = self._fake_archive(pinned_dir, pinned_version)
            pinned_sha = hashlib.sha256(pinned_archive.read_bytes()).hexdigest()

            class QuietHandler(SimpleHTTPRequestHandler):
                def __init__(self, *args, **kwargs):
                    super().__init__(*args, directory=server_root, **kwargs)

                def log_message(self, format: str, *args: object) -> None:
                    return

            httpd = ThreadingHTTPServer(("127.0.0.1", 0), QuietHandler)
            thread = threading.Thread(target=httpd.serve_forever, daemon=True)
            thread.start()
            base_url = f"http://127.0.0.1:{httpd.server_port}"
            (server_root / "checksums-latest.txt").write_text(
                f"{sha}  {archive.name}\n"
            )
            (server_root / "checksums-pinned.txt").write_text(
                f"{pinned_sha}  {pinned_archive.name}\n"
            )
            common_assets = [
                {"name": "install-rzm.sh", "browser_download_url": f"{base_url}/install-rzm.sh"},
                {"name": "install-rzm.sh.sha256", "browser_download_url": f"{base_url}/install-rzm.sh.sha256"},
            ]
            latest_release = {
                "tag_name": version,
                "draft": False,
                "prerelease": False,
                "assets": [
                    {"name": archive.name, "browser_download_url": f"{base_url}/{archive.name}"},
                    {"name": "checksums.txt", "browser_download_url": f"{base_url}/checksums-latest.txt"},
                    *common_assets,
                ],
            }
            pinned_release = {
                "tag_name": pinned_version,
                "draft": False,
                "prerelease": False,
                "assets": [
                    {"name": pinned_archive.name, "browser_download_url": f"{base_url}/{pinned_version}/{pinned_archive.name}"},
                    {"name": "checksums.txt", "browser_download_url": f"{base_url}/checksums-pinned.txt"},
                    *common_assets,
                ],
            }
            latest_path = server_root / "releases" / "latest"
            latest_path.parent.mkdir()
            latest_path.write_text(json.dumps(latest_release))
            tag_dir = server_root / "releases" / "tags"
            tag_dir.mkdir()
            (tag_dir / version).write_text(json.dumps(latest_release))
            (tag_dir / pinned_version).write_text(json.dumps(pinned_release))
            (repo / ".rhizome").mkdir()
            (repo / ".rhizome" / "config.yml").write_text(
                "rhizome:\n    binaryPath: legacy-rzm\ncode:\n    enabled: true\n    python:\n        roots:\n            - src\n"
            )
            env = os.environ.copy()
            home = root / "home"
            home.mkdir()
            env["HOME"] = str(home)
            env["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin:" + env.get("PATH", "")
            env["RZM_GITHUB_RELEASES_URL"] = f"{base_url}/releases"
            env["RZM_UPDATE_MANIFEST_URL"] = f"{base_url}/releases/latest"
            env["RZM_TEST_OS"] = "linux"
            invocation_log = root / "project-install-invocations.log"
            env["RZM_INVOCATION_LOG"] = str(invocation_log)
            try:
                install = subprocess.run(
                    [str(script_path), "--project", str(repo), "--user-binary", "skip"],
                    cwd=repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, install.returncode, install.stdout + install.stderr)
                self.assertFalse(
                    invocation_log.exists(),
                    "project installation must not execute the launcher or run init",
                )
                config = (repo / ".rhizome" / "config.yml").read_text()
                self.assertIn("rhizome:\n    version: v9.8.7\n", config)
                self.assertIn("code:\n    enabled: true\n    python:\n        roots:\n            - src\n", config)
                self.assertIn("version: v9.8.7", config)
                self.assertNotIn("binaryPath", config)

                (repo / ".rhizome" / "config.yml").write_text(
                    "rhizome:\n  version: v9.8.7\ncode:\n  enabled: true\n  python:\n    roots:\n      - src\n"
                )
                reinstall = subprocess.run(
                    [str(script_path), "--project", str(repo), "--user-binary", "skip", "--yes"],
                    cwd=repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, reinstall.returncode, reinstall.stdout + reinstall.stderr)
                config = (repo / ".rhizome" / "config.yml").read_text()
                self.assertIn("rhizome:\n  version: v9.8.7\n", config)
                self.assertIn("code:\n  enabled: true\n  python:\n    roots:\n      - src\n", config)
                self.assertNotIn("code:\n  enabled: true\n  python:\n  roots:", config)

                json_reinstall = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=root,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, json_reinstall.returncode, json_reinstall.stdout + json_reinstall.stderr)
                self.assertEqual(1, len(json_reinstall.stdout.splitlines()))
                payload = json.loads(json_reinstall.stdout)
                self.assertEqual(
                    {
                        "projectRoot", "launcher", "pin", "userScopeTouched",
                        "pinSelectionSource", "latestRelease", "pinRelationToLatest",
                        "agentSurfaceStructureStatus", "agentSurfaceStructureReason",
                        "agentSurfaceRepair", "next",
                    },
                    set(payload),
                )
                self.assertEqual(str(repo.resolve()), payload["projectRoot"])
                self.assertEqual(str(repo.resolve() / "bin" / "rzm"), payload["launcher"])
                self.assertEqual(version, payload["pin"])
                self.assertEqual("preserved", payload["pinSelectionSource"])
                self.assertEqual(version, payload["latestRelease"])
                self.assertEqual("same", payload["pinRelationToLatest"])
                self.assertFalse(payload["userScopeTouched"])
                self.assertEqual("init-required", payload["agentSurfaceStructureStatus"])
                self.assertEqual(
                    "managed guidance and the consolidated rhizome skill are absent",
                    payload["agentSurfaceStructureReason"],
                )
                self.assertEqual([], payload["next"])
                self.assertEqual(
                    [payload["launcher"], "init"],
                    payload["agentSurfaceRepair"]["initCandidate"],
                )
                self.assertEqual(
                    "unverified",
                    payload["agentSurfaceRepair"]["capabilityStatus"],
                )
                self.assertTrue(payload["agentSurfaceRepair"]["requiresConfirmation"])
                self.assertIsNone(
                    payload["agentSurfaceRepair"]["advertisedLatestPinChangeCandidate"],
                )
                self.assertIn("Installed Rhizome project launcher", json_reinstall.stderr)

                (repo / "AGENTS.md").write_text("<!-- BEGIN RZM INIT RHIZOME BLOCK -->\n")
                incomplete_result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertEqual(0, incomplete_result.returncode, incomplete_result.stdout + incomplete_result.stderr)
                self.assertEqual(
                    "incomplete",
                    json.loads(incomplete_result.stdout)["agentSurfaceStructureStatus"],
                )

                rhizome_skill = repo / ".agents" / "skills" / "rhizome" / "SKILL.md"
                rhizome_skill.parent.mkdir(parents=True)
                rhizome_skill.write_text("---\nname: rhizome\n---\n")
                trivial_skill_result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    trivial_skill_result.returncode,
                    trivial_skill_result.stdout + trivial_skill_result.stderr,
                )
                self.assertEqual(
                    "incomplete",
                    json.loads(trivial_skill_result.stdout)["agentSurfaceStructureStatus"],
                )

                repo_root = Path(__file__).resolve().parents[2]
                canonical_guidance = (
                    repo_root / "docs" / "rhizome-md-templates" / "RHIZOME.md"
                ).read_text()
                (repo / "AGENTS.md").write_text(
                    "<!-- BEGIN RZM INIT RHIZOME BLOCK -->\n"
                    + canonical_guidance
                    + "\n<!-- END RZM INIT RHIZOME BLOCK -->\n"
                )
                canonical_skill = (
                    repo_root / "pkg" / "app" / "cli" / "init" / "templates"
                    / "skills" / "markdown" / "rhizome"
                )
                shutil.copytree(canonical_skill, rhizome_skill.parent, dirs_exist_ok=True)
                ready_result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertEqual(0, ready_result.returncode, ready_result.stdout + ready_result.stderr)
                ready_payload = json.loads(ready_result.stdout)
                self.assertEqual("present", ready_payload["agentSurfaceStructureStatus"])
                self.assertIsNone(ready_payload["agentSurfaceRepair"])
                self.assertEqual(
                    [
                        payload["launcher"], "agent", "start", "--intent",
                        "verify Rhizome integration",
                    ],
                    ready_payload["next"],
                )

                rhizome_skill.write_text(
                    rhizome_skill.read_text().replace(
                        "Use this skill as the expert entrypoint for operating Rhizome.",
                        "Operate Rhizome through this routed skill.",
                    )
                )
                prose_change_result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    prose_change_result.returncode,
                    prose_change_result.stdout + prose_change_result.stderr,
                )
                self.assertEqual(
                    "present",
                    json.loads(prose_change_result.stdout)["agentSurfaceStructureStatus"],
                    "ordinary skill prose must not be part of the installer structure contract",
                )

                shared_skill = root / "shared-rhizome-skill"
                rhizome_skill.parent.rename(shared_skill)
                codex_skill = repo / ".codex" / "skills" / "rhizome"
                shutil.copytree(canonical_skill, codex_skill)
                codex_only_result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    codex_only_result.returncode,
                    codex_only_result.stdout + codex_only_result.stderr,
                )
                self.assertEqual(
                    "incomplete",
                    json.loads(codex_only_result.stdout)["agentSurfaceStructureStatus"],
                    "init does not install the shared rhizome skill under .codex/skills",
                )

                claude_skill = repo / ".claude" / "skills" / "rhizome"
                shutil.copytree(canonical_skill, claude_skill)
                claude_only_result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    claude_only_result.returncode,
                    claude_only_result.stdout + claude_only_result.stderr,
                )
                self.assertEqual(
                    "present",
                    json.loads(claude_only_result.stdout)["agentSurfaceStructureStatus"],
                    "init-owned Claude skills remain a supported standalone surface",
                )
                claude_skill.rename(root / "shared-claude-rhizome-skill")
                shared_skill.rename(rhizome_skill.parent)

                rhizome_skill.write_text(
                    re.sub(
                        r"`(references/[^`]+)`",
                        r"[reference](\1)",
                        rhizome_skill.read_text(),
                    )
                )
                reformatted_routes_result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    reformatted_routes_result.returncode,
                    reformatted_routes_result.stdout + reformatted_routes_result.stderr,
                )
                self.assertEqual(
                    "present",
                    json.loads(reformatted_routes_result.stdout)["agentSurfaceStructureStatus"],
                    "ordinary Markdown punctuation must not be part of the route contract",
                )
                shutil.copytree(canonical_skill, rhizome_skill.parent, dirs_exist_ok=True)

                missing_reference = (
                    rhizome_skill.parent / "references" / "sessions.md"
                )
                missing_reference.unlink()
                missing_reference_result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    missing_reference_result.returncode,
                    missing_reference_result.stdout + missing_reference_result.stderr,
                )
                self.assertEqual(
                    "incomplete",
                    json.loads(missing_reference_result.stdout)["agentSurfaceStructureStatus"],
                )
                shutil.copytree(canonical_skill, rhizome_skill.parent, dirs_exist_ok=True)

                skill_lines = rhizome_skill.read_text().splitlines()
                rhizome_skill.write_text(
                    "\n".join(
                        line for line in skill_lines
                        if "references/sessions.md" not in line
                    )
                    + "\n"
                )
                missing_route_result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    missing_route_result.returncode,
                    missing_route_result.stdout + missing_route_result.stderr,
                )
                self.assertEqual(
                    "incomplete",
                    json.loads(missing_route_result.stdout)["agentSurfaceStructureStatus"],
                    "removing a canonical route while leaving its file must not look complete",
                )
                shutil.copytree(canonical_skill, rhizome_skill.parent, dirs_exist_ok=True)

                legacy_repo = root / "repo-legacy-launcher"
                legacy_repo.mkdir()
                (legacy_repo / ".rhizome").mkdir()
                legacy_config = "rhizome:\n  version: v9.8.7\n"
                (legacy_repo / ".rhizome" / "config.yml").write_text(legacy_config)
                legacy_launcher = legacy_repo / "bin" / "rzm"
                write_legacy_managed_launcher(legacy_launcher)
                invocations_before_legacy_refresh = (
                    invocation_log.read_text() if invocation_log.exists() else ""
                )
                legacy_refresh = subprocess.run(
                    [str(script_path), "--yes", str(legacy_launcher.resolve())],
                    cwd=legacy_repo, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    legacy_refresh.returncode,
                    legacy_refresh.stdout + legacy_refresh.stderr,
                )
                self.assertIn(
                    "# Generated by install-rzm.sh; local edits will be replaced.",
                    legacy_launcher.read_text(),
                )
                self.assertEqual(
                    legacy_config,
                    (legacy_repo / ".rhizome" / "config.yml").read_text(),
                )
                self.assertEqual(
                    invocations_before_legacy_refresh,
                    invocation_log.read_text() if invocation_log.exists() else "",
                    "legacy migration must not execute the launcher or touch user scope",
                )

                write_legacy_managed_launcher(legacy_launcher)
                explicit_legacy_refresh = subprocess.run(
                    [
                        str(script_path), "--project", str(legacy_repo),
                        "--user-binary", "skip",
                    ],
                    cwd=legacy_repo, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    explicit_legacy_refresh.returncode,
                    explicit_legacy_refresh.stdout + explicit_legacy_refresh.stderr,
                )
                self.assertIn(
                    "# Generated by install-rzm.sh; local edits will be replaced.",
                    legacy_launcher.read_text(),
                )

                custom_repo = root / "repo-custom-launcher"
                custom_repo.mkdir()
                custom_launcher = custom_repo / "tools" / "rzm-project"
                custom_launcher.parent.mkdir()
                custom_launcher.write_text("#!/bin/sh\necho unmanaged\n")
                custom_launcher.chmod(0o755)
                overwrite = subprocess.run(
                    [
                        str(script_path), "--project", str(custom_repo),
                        "--launcher", "tools/rzm-project",
                        "--user-binary", "skip", "--yes",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertEqual(0, overwrite.returncode, overwrite.stdout + overwrite.stderr)
                self.assertIn("RZM MANAGED LAUNCHER", custom_launcher.read_text())
                custom_launcher.write_text(custom_launcher.read_text() + "\n# stale custom launcher\n")
                custom_refresh = subprocess.run(
                    [str(custom_launcher), "update", "--pinned"],
                    cwd=custom_repo, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    custom_refresh.returncode,
                    custom_refresh.stdout + custom_refresh.stderr,
                )
                self.assertNotIn("stale custom launcher", custom_launcher.read_text())

                nested_config = (
                    "rhizome: # repo settings\n"
                    f"  version: {pinned_version} # intentionally pinned\n"
                    "  metadata:\n"
                    "    version: schema-v2\n"
                    "    binaryPath: tools/rzm\n"
                    "  description: |\n"
                    "    first line\n"
                    "      indented line\n"
                    "code:\n"
                    "  enabled: true\n"
                )
                commented_pin_repo = root / "repo-commented-pin"
                commented_pin_repo.mkdir()
                (commented_pin_repo / ".rhizome").mkdir()
                (commented_pin_repo / ".rhizome" / "config.yml").write_text(nested_config)
                invocations_before_preserved_install = (
                    invocation_log.read_text() if invocation_log.exists() else ""
                )
                preserve_pin = subprocess.run(
                    [
                        str(script_path), "--project", str(commented_pin_repo),
                        "--user-binary", "skip", "--yes",
                    ],
                    cwd=commented_pin_repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, preserve_pin.returncode, preserve_pin.stdout + preserve_pin.stderr)
                self.assertEqual(
                    nested_config,
                    (commented_pin_repo / ".rhizome" / "config.yml").read_text(),
                )
                preserved_json = subprocess.run(
                    [
                        str(script_path), "--project", str(commented_pin_repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=commented_pin_repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(
                    0,
                    preserved_json.returncode,
                    preserved_json.stdout + preserved_json.stderr,
                )
                self.assertEqual(
                    nested_config,
                    (commented_pin_repo / ".rhizome" / "config.yml").read_text(),
                    "machine-readable repair guidance must not silently change the pin",
                )
                preserved_payload = json.loads(preserved_json.stdout)
                self.assertEqual(pinned_version, preserved_payload["pin"])
                self.assertEqual("preserved", preserved_payload["pinSelectionSource"])
                self.assertEqual(version, preserved_payload["latestRelease"])
                self.assertEqual("different", preserved_payload["pinRelationToLatest"])
                self.assertEqual(
                    "init-required",
                    preserved_payload["agentSurfaceStructureStatus"],
                )
                self.assertEqual([], preserved_payload["next"])
                self.assertEqual(
                    [
                        "bash", str(script_path.resolve()),
                        "--project", str(commented_pin_repo.resolve()),
                        "--launcher", "bin/rzm",
                        "--user-binary", "skip",
                        "--version", version,
                        "--yes", "--json",
                    ],
                    preserved_payload["agentSurfaceRepair"][
                        "advertisedLatestPinChangeCandidate"
                    ],
                )
                self.assertIn(
                    "may not support the consolidated rhizome skill",
                    preserve_pin.stdout,
                )
                self.assertEqual(
                    invocations_before_preserved_install,
                    invocation_log.read_text() if invocation_log.exists() else "",
                    "older-pin guidance must not execute init or the launcher",
                )

                unprefixed_pin_repo = root / "repo-unprefixed-pin"
                unprefixed_pin_repo.mkdir()
                (unprefixed_pin_repo / ".rhizome").mkdir()
                (unprefixed_pin_repo / ".rhizome" / "config.yml").write_text(
                    "rhizome:\n  version: 8.7.6\n"
                )
                unprefixed_pin = subprocess.run(
                    [
                        str(script_path), "--project", str(unprefixed_pin_repo),
                        "--user-binary", "skip", "--yes", "--json",
                    ],
                    cwd=unprefixed_pin_repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(
                    0,
                    unprefixed_pin.returncode,
                    unprefixed_pin.stdout + unprefixed_pin.stderr,
                )
                unprefixed_payload = json.loads(unprefixed_pin.stdout)
                self.assertEqual(pinned_version, unprefixed_payload["pin"])
                self.assertEqual("preserved", unprefixed_payload["pinSelectionSource"])
                self.assertIn(
                    f"version: {pinned_version}",
                    (unprefixed_pin_repo / ".rhizome" / "config.yml").read_text(),
                )
                nested_reinstall = subprocess.run(
                    [
                        str(script_path), "--project", str(commented_pin_repo),
                        "--user-binary", "skip", "--version", version, "--yes",
                    ],
                    cwd=commented_pin_repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, nested_reinstall.returncode, nested_reinstall.stdout + nested_reinstall.stderr)
                updated_nested_config = (commented_pin_repo / ".rhizome" / "config.yml").read_text()
                self.assertIn(f"  version: {version} # intentionally pinned", updated_nested_config)
                self.assertIn("    version: schema-v2", updated_nested_config)
                self.assertIn("    binaryPath: tools/rzm", updated_nested_config)
                commented_pin_binary = subprocess.run(
                    [str(commented_pin_repo / "bin" / "rzm"), "--version"],
                    cwd=commented_pin_repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, commented_pin_binary.returncode, commented_pin_binary.stdout + commented_pin_binary.stderr)
                self.assertIn(f"rhizome {version}", commented_pin_binary.stdout)

                nested_pin_repo = root / "repo-nested-pin"
                nested_pin_repo.mkdir()
                (nested_pin_repo / ".rhizome").mkdir()
                (nested_pin_repo / ".rhizome" / "config.yml").write_text(
                    "rhizome: # repo settings\n  metadata:\n    version: schema-v2\n"
                )
                nested_pin = subprocess.run(
                    [
                        str(script_path), "--project", str(nested_pin_repo),
                        "--user-binary", "skip", "--yes",
                    ],
                    cwd=nested_pin_repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, nested_pin.returncode, nested_pin.stdout + nested_pin.stderr)
                self.assertEqual(
                    "rhizome: # repo settings\n  metadata:\n    version: schema-v2\n  version: v9.8.7\n",
                    (nested_pin_repo / ".rhizome" / "config.yml").read_text(),
                )

                missing_config_repo = root / "repo missing config"
                missing_config_repo.mkdir()
                missing_config = subprocess.run(
                    [
                        str(script_path), "--project", str(missing_config_repo),
                        "--user-binary", "skip", "--yes",
                    ],
                    cwd=missing_config_repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, missing_config.returncode, missing_config.stdout + missing_config.stderr)
                config = (missing_config_repo / ".rhizome" / "config.yml").read_text()
                self.assertIn("rhizome:\n  version: v9.8.7\n", config)

                malformed_repo = root / "repo-malformed-manifest"
                malformed_repo.mkdir()
                latest_manifest = server_root / "releases" / "latest"
                valid_latest = latest_manifest.read_text()
                latest_manifest.write_text('{"tag_name":"v9.8.7","draft":false,"prerelease":false,"assets":[]}')
                malformed = subprocess.run(
                    [
                        str(script_path), "--project", str(malformed_repo),
                        "--user-binary", "skip", "--yes",
                    ],
                    cwd=root,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                latest_manifest.write_text(valid_latest)
                self.assertNotEqual(0, malformed.returncode)
                self.assertIn("is missing or duplicated", malformed.stderr)
                self.assertFalse((malformed_repo / ".rhizome").exists())
                self.assertFalse((malformed_repo / "bin").exists())

                result = subprocess.run([str(repo / "bin" / "rzm"), "--version"], cwd=repo, env=env, text=True, capture_output=True)
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                self.assertIn("rhizome v9.8.7", result.stdout)
                pinned_binary = repo / ".rhizome" / "bin" / self._platform_dir() / "rzm"
                self.assertTrue(pinned_binary.is_file())

                (repo / ".rhizome" / "config.yml").write_text(
                    "rhizome: # repo settings\n  metadata:\n    version: schema-v2\n"
                )
                nested_launcher_pin = subprocess.run(
                    [str(repo / "bin" / "rzm"), "--version"],
                    cwd=repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertNotEqual(0, nested_launcher_pin.returncode)
                self.assertIn("rhizome.version missing", nested_launcher_pin.stderr)

                (repo / ".rhizome" / "config.yml").write_text(
                    "rhizome: # repo settings\n"
                    "  version: v9.8.7 # intentionally pinned\n"
                    "  metadata:\n"
                    "    devBinaryDir: missing-dev-bin\n"
                )
                nested_dev_dir = subprocess.run(
                    [str(repo / "bin" / "rzm"), "--version"],
                    cwd=repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, nested_dev_dir.returncode, nested_dev_dir.stdout + nested_dev_dir.stderr)
                self.assertIn("rhizome v9.8.7", nested_dev_dir.stdout)

                pinned_binary.write_text("#!/bin/sh\necho rhizome v0.1.0\n")
                pinned_binary.chmod(0o755)
                repaired = subprocess.run([str(repo / "bin" / "rzm"), "--version"], cwd=repo, env=env, text=True, capture_output=True)
                self.assertEqual(0, repaired.returncode, repaired.stdout + repaired.stderr)
                self.assertIn("rhizome v9.8.7", repaired.stdout)
                self.assertNotIn("v0.1.0", pinned_binary.read_text())

                pinned_binary.write_text("#!/bin/sh\necho rhizome v9.8.70\n")
                pinned_binary.chmod(0o755)
                prefix_mismatch = subprocess.run([str(repo / "bin" / "rzm"), "--version"], cwd=repo, env=env, text=True, capture_output=True)
                self.assertEqual(0, prefix_mismatch.returncode, prefix_mismatch.stdout + prefix_mismatch.stderr)
                self.assertIn("rhizome v9.8.7", prefix_mismatch.stdout)
                self.assertNotIn("v9.8.70", pinned_binary.read_text())

                (repo / "go.mod").write_text("module github.com/atomicobject/rhizome\n")
                dev_binary = repo / "bin" / self._dev_os_dir() / "rzm"
                dev_binary.parent.mkdir(parents=True)
                dev_binary.write_text("#!/bin/sh\necho rhizome dev-build\n")
                dev_binary.chmod(0o755)
                dev = subprocess.run([str(repo / "bin" / "rzm"), "--version"], cwd=repo, env=env, text=True, capture_output=True)
                self.assertEqual(0, dev.returncode, dev.stdout + dev.stderr)
                self.assertIn("rhizome dev-build", dev.stdout)

                launcher = repo / "bin" / "rzm"
                launcher.write_text(launcher.read_text() + "\n# stale launcher content\n")
                update = subprocess.run([str(launcher), "update", "--pinned"], cwd=repo, env=env, text=True, capture_output=True)
                self.assertEqual(0, update.returncode, update.stdout + update.stderr)
                self.assertIn("Updated", update.stdout)
                self.assertNotIn("stale launcher content", launcher.read_text())

                # Plain `update` (no --pinned) also refreshes the managed
                # launcher, then forwards the args to the binary so the binary
                # owns the actual update decision (which version, prompts, pin
                # bumping). Regression for "shim only auto-updates on --pinned".
                # We remove the dev binary fixture first so the launcher takes
                # the regular pinned path; otherwise the dev shortcut would
                # short-circuit on subsequent invocations.
                shutil.rmtree(repo / "bin" / self._dev_os_dir())
                (repo / "go.mod").unlink()
                launcher.write_text(launcher.read_text() + "\n# second stale marker\n")
                plain_update = subprocess.run([str(launcher), "update"], cwd=repo, env=env, text=True, capture_output=True)
                self.assertEqual(0, plain_update.returncode, plain_update.stdout + plain_update.stderr)
                self.assertNotIn("second stale marker", launcher.read_text())
                # Launcher exec'd the (fake) pinned binary, which echoes its version.
                self.assertIn("rhizome v9.8.7", plain_update.stdout)

                invocation_log = root / "invocations.log"
                env["RZM_INVOCATION_LOG"] = str(invocation_log)
                launcher.write_text(launcher.read_text() + "\n# persistent-order stale marker\n")
                persistent_order_update = subprocess.run(
                    [str(launcher), "--no-pager", "update", "--latest"],
                    cwd=repo, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    persistent_order_update.returncode,
                    persistent_order_update.stdout + persistent_order_update.stderr,
                )
                self.assertNotIn("persistent-order stale marker", launcher.read_text())
                self.assertIn("--no-pager update --latest", invocation_log.read_text())

                launcher.write_text(launcher.read_text() + "\n# manifest-order stale marker\n")
                manifest_order_update = subprocess.run(
                    [
                        str(launcher), "--manifest-url",
                        f"{base_url}/releases/latest", "update", "--pinned",
                    ],
                    cwd=repo, env=env, text=True, capture_output=True,
                )
                self.assertEqual(
                    0,
                    manifest_order_update.returncode,
                    manifest_order_update.stdout + manifest_order_update.stderr,
                )
                self.assertNotIn("manifest-order stale marker", launcher.read_text())
                self.assertNotIn("Updated", manifest_order_update.stdout)
                self.assertIn(
                    f"--manifest-url {base_url}/releases/latest update --pinned",
                    invocation_log.read_text(),
                )

                (repo / ".rhizome" / "config.yml").write_text("rhizome:\n  devBinaryDir: bin\n")
                dev_binary = repo / "bin" / self._dev_os_dir() / "rzm"
                dev_binary.parent.mkdir(parents=True)
                dev_binary.write_text("#!/bin/sh\necho rhizome dev-only\n")
                dev_binary.chmod(0o755)
                dev_only = subprocess.run([str(launcher), "--version"], cwd=repo, env=env, text=True, capture_output=True)
                self.assertEqual(0, dev_only.returncode, dev_only.stdout + dev_only.stderr)
                self.assertIn("rhizome dev-only", dev_only.stdout)

                launcher.write_text(launcher.read_text() + "\n# dev-only stale marker\n")
                dev_update = subprocess.run([str(launcher), "update"], cwd=repo, env=env, text=True, capture_output=True)
                self.assertEqual(0, dev_update.returncode, dev_update.stdout + dev_update.stderr)
                self.assertIn("rhizome dev-only", dev_update.stdout)
                self.assertIn("dev-only stale marker", launcher.read_text())
                self.assertNotIn("version:", (repo / ".rhizome" / "config.yml").read_text())

                dev_binary.chmod(0o644)
                non_executable = subprocess.run([str(launcher), "--version"], cwd=repo, env=env, text=True, capture_output=True)
                self.assertNotEqual(0, non_executable.returncode)
                self.assertIn("configured rhizome.devBinaryDir target is not executable", non_executable.stderr)
                dev_binary.chmod(0o755)

                shutil.rmtree(dev_binary.parent)
                missing_dev = subprocess.run([str(launcher), "--version"], cwd=repo, env=env, text=True, capture_output=True)
                self.assertNotEqual(0, missing_dev.returncode)
                self.assertIn("configured rhizome.devBinaryDir target missing", missing_dev.stderr)
            finally:
                httpd.shutdown()
                httpd.server_close()

    def test_refuses_to_overwrite_unmanaged_launcher_without_yes(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            launcher = repo / "bin" / "rzm"
            launcher.parent.mkdir()
            launcher.write_text("#!/bin/sh\necho not rhizome\n")
            launcher.chmod(0o755)

            env = os.environ.copy()
            env["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin:" + env.get("PATH", "")
            result = subprocess.run(
                [str(script_path), "--project", str(repo), "--user-binary", "skip"],
                cwd=repo, env=env, text=True, capture_output=True,
            )

            self.assertNotEqual(0, result.returncode)
            self.assertIn("not a Rhizome-managed launcher", result.stderr)
            self.assertFalse((repo / ".rhizome" / "config.yml").exists())

    def test_externally_managed_project_refuses_before_mutation(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        configs = {
            "bare": "rhizome:\n  binaryManager: external\n",
            "quoted key": 'rhizome:\n  "binaryManager": external\n',
            "single-quoted key": "rhizome:\n  'binaryManager' : external\n",
            "spaced root key and value": 'rhizome :\n  binaryManager: " external "\n',
            "literal scalar": "rhizome:\n  binaryManager: |-\n    external\n",
            "folded scalar": "rhizome:\n  binaryManager: >-\n    external\n",
        }
        for name, rhizome_config in configs.items():
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                repo = root / "repo"
                config = repo / ".rhizome" / "config.yml"
                config.parent.mkdir(parents=True)
                original = rhizome_config + "notes:\n  includes: [docs/**/*.md]\n"
                config.write_text(original)
                home = root / "home"
                home.mkdir()
                env = os.environ.copy()
                env["HOME"] = str(home)
                env["RZM_UPDATE_MANIFEST_URL"] = "file:///manifest-must-not-be-read"

                result = subprocess.run(
                    [
                        str(script_path),
                        "--project", str(repo),
                        "--user-binary", "skip",
                        "--yes",
                    ],
                    cwd=repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )

                self.assertNotEqual(0, result.returncode)
                self.assertIn("externally managed", result.stderr)
                self.assertIn("external binary manager", result.stderr)
                self.assertEqual(original, config.read_text())
                self.assertFalse((repo / "bin" / "rzm").exists())
                self.assertFalse((home / ".local" / "bin" / "rzm").exists())

    def test_flow_config_refuses_before_mutation(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        configs = {
            "root": "{rhizome: {binaryManager: external}}\n",
            "nested": "rhizome:\n  {binaryManager: external}\n",
            "multiline nested": "rhizome:\n  {\n    binaryManager: external\n  }\n",
            "comments before nested": (
                "rhizome : # executable ownership\n\n"
                "  # selected by mise\n  {binaryManager: external}\n"
            ),
        }
        for name, original in configs.items():
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                repo = root / "repo"
                config = repo / ".rhizome" / "config.yml"
                config.parent.mkdir(parents=True)
                config.write_text(original)
                home = root / "home"
                home.mkdir()
                env = os.environ.copy()
                env["HOME"] = str(home)
                env["RZM_UPDATE_MANIFEST_URL"] = "file:///manifest-must-not-be-read"

                result = subprocess.run(
                    [
                        str(script_path),
                        "--project", str(repo),
                        "--user-binary", "skip",
                        "--yes",
                    ],
                    cwd=repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )

                self.assertNotEqual(0, result.returncode)
                self.assertIn("unsupported Rhizome config shape", result.stderr)
                self.assertIn("not a flow mapping", result.stderr)
                self.assertNotIn("manifest-must-not-be-read", result.stderr)
                self.assertEqual(original, config.read_text())
                self.assertFalse((repo / "bin" / "rzm").exists())
                self.assertFalse((home / ".local" / "bin" / "rzm").exists())

    @unittest.skipUnless(os.name == "posix", "binary fixture is POSIX-only")
    def test_user_only_install_ignores_project_binary_manager(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            repo = root / "repo"
            config = repo / ".rhizome" / "config.yml"
            config.parent.mkdir(parents=True)
            config.write_text("rhizome:\n  binaryManager: external\n")
            version = "v9.8.7"
            httpd, base_url = self._start_fake_release(root, version, version)
            home = root / "home"
            home.mkdir()
            env = os.environ.copy()
            env["HOME"] = str(home)
            env["RZM_TEST_OS"] = "linux"
            env["RZM_GITHUB_RELEASES_URL"] = f"{base_url}/releases"
            env["RZM_UPDATE_MANIFEST_URL"] = f"{base_url}/releases/latest"
            try:
                result = subprocess.run(
                    [str(script_path), "--user", "--yes", "--version", version],
                    cwd=repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
            finally:
                httpd.shutdown()
                httpd.server_close()

            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            installed = home / ".local" / "bin" / "rzm"
            self.assertTrue(installed.is_file())
            self.assertIn(version, subprocess.check_output([str(installed), "--version"], text=True))
            self.assertEqual("rhizome:\n  binaryManager: external\n", config.read_text())

    def test_marker_spoof_does_not_claim_managed_launcher_ownership(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp)
            launcher = repo / "bin" / "rzm"
            launcher.parent.mkdir()
            launcher.write_text(
                "#!/bin/sh\n"
                "# copied text: RZM MANAGED LAUNCHER\n"
                "managed_launcher_path() { :; }\n"
                "install_pinned() { :; }\n"
                "update_launcher() { :; }\n"
                "(cd \"$root\" && \"$installer\" --yes \"$self\")\n"
                "exec \"$target\" \"$@\"\n"
            )
            launcher.chmod(0o755)

            result = subprocess.run(
                [str(script_path), "--project", str(repo), "--user-binary", "skip"],
                cwd=repo,
                text=True,
                capture_output=True,
            )

            self.assertNotEqual(0, result.returncode)
            self.assertIn("not a Rhizome-managed launcher", result.stderr)
            self.assertEqual(
                "#!/bin/sh\n"
                "# copied text: RZM MANAGED LAUNCHER\n"
                "managed_launcher_path() { :; }\n"
                "install_pinned() { :; }\n"
                "update_launcher() { :; }\n"
                "(cd \"$root\" && \"$installer\" --yes \"$self\")\n"
                "exec \"$target\" \"$@\"\n",
                launcher.read_text(),
            )
            self.assertFalse((repo / ".rhizome").exists())

    def test_rejects_general_positional_launcher_with_migration_guidance(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        result = subprocess.run(
            [str(script_path), "bin/rzm"], text=True, capture_output=True,
        )
        self.assertNotEqual(0, result.returncode)
        self.assertIn("positional launcher paths are no longer supported", result.stderr)
        self.assertIn("--project <root> --launcher <relative-path>", result.stderr)

    def test_legacy_bridge_rejects_unverified_or_unsafe_targets(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            repo = root / "repo"
            outside = root / "outside"
            repo.mkdir()
            outside.mkdir()
            (repo / ".rhizome").mkdir()
            (repo / ".rhizome" / "config.yml").write_text(
                "rhizome:\n  version: v1.2.3\n"
            )

            unmanaged = repo / "bin" / "rzm"
            unmanaged.parent.mkdir()
            unmanaged.write_text(
                "#!/usr/bin/env bash\n# RZM MANAGED LAUNCHER\necho user-owned\n"
            )
            unmanaged.chmod(0o755)
            unmanaged_before = unmanaged.read_text()
            result = subprocess.run(
                [str(script_path), "--yes", str(unmanaged.resolve())],
                cwd=repo, text=True, capture_output=True,
            )
            self.assertNotEqual(0, result.returncode)
            self.assertIn("not a verified legacy Rhizome-managed launcher", result.stderr)
            self.assertEqual(unmanaged_before, unmanaged.read_text())

            outside_launcher = outside / "rzm"
            write_legacy_managed_launcher(outside_launcher)
            result = subprocess.run(
                [str(script_path), "--yes", str(outside_launcher.resolve())],
                cwd=repo, text=True, capture_output=True,
            )
            self.assertNotEqual(0, result.returncode)
            self.assertIn("must stay inside the current project", result.stderr)

            link = repo / "bin" / "rzm-link"
            link.symlink_to(outside_launcher)
            result = subprocess.run(
                [str(script_path), "--yes", str(link)],
                cwd=repo, text=True, capture_output=True,
            )
            self.assertNotEqual(0, result.returncode)
            self.assertIn("regular, non-symlink launcher", result.stderr)

            write_legacy_managed_launcher(unmanaged)
            result = subprocess.run(
                [str(script_path), "--yes", str(unmanaged.resolve()), "--json"],
                cwd=repo, text=True, capture_output=True,
            )
            self.assertNotEqual(0, result.returncode)
            self.assertIn("positional launcher paths are no longer supported", result.stderr)

    def test_yes_requires_explicit_project_and_user_policy_before_mutation(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp) / "repo"
            repo.mkdir()
            missing_project = subprocess.run(
                [str(script_path), "--yes", "--user-binary", "skip"],
                text=True, capture_output=True,
            )
            self.assertNotEqual(0, missing_project.returncode)
            self.assertIn("explicit --project", missing_project.stderr)

            missing_policy = subprocess.run(
                [str(script_path), "--project", str(repo), "--yes"],
                text=True, capture_output=True,
            )
            self.assertNotEqual(0, missing_policy.returncode)
            self.assertIn("explicit --user-binary", missing_policy.stderr)
            self.assertFalse((repo / ".rhizome").exists())

    def test_json_requires_yes_before_project_or_network_activity(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp) / "repo"
            repo.mkdir()
            env = os.environ.copy()
            env["RZM_UPDATE_MANIFEST_URL"] = "http://127.0.0.1:1/should-not-be-requested"

            result = subprocess.run(
                [
                    str(script_path), "--project", str(repo),
                    "--user-binary", "skip", "--json",
                ],
                cwd=repo,
                env=env,
                text=True,
                capture_output=True,
            )

            self.assertNotEqual(0, result.returncode)
            self.assertEqual("", result.stdout)
            self.assertIn("--json requires --yes", result.stderr)
            self.assertFalse((repo / ".rhizome").exists())

    def test_interactive_project_policy_is_validated_after_prompt(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp) / "repo"
            repo.mkdir()
            result = subprocess.run(
                [str(script_path)],
                input=f"project\n{repo}\ninvalid-policy\n",
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(0, result.returncode)
            self.assertIn("--user-binary must be auto, install, or skip", result.stderr)
            self.assertFalse((repo / ".rhizome").exists())

    def test_project_target_preflight_rejects_invalid_shapes_without_mutation(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            env = os.environ.copy()
            env["RZM_UPDATE_MANIFEST_URL"] = "http://127.0.0.1:1/should-not-be-requested"

            cases = []

            bin_file_repo = root / "bin-file"
            bin_file_repo.mkdir()
            (bin_file_repo / "bin").write_text("collision")
            cases.append((bin_file_repo, "launcher path ancestor is not a directory"))

            rhizome_file_repo = root / "rhizome-file"
            rhizome_file_repo.mkdir()
            (rhizome_file_repo / ".rhizome").write_text("collision")
            cases.append((rhizome_file_repo, ".rhizome must be a directory"))

            config_dir_repo = root / "config-dir"
            (config_dir_repo / ".rhizome" / "config.yml").mkdir(parents=True)
            cases.append((config_dir_repo, "config.yml must be a regular file"))

            gitignore_dir_repo = root / "gitignore-dir"
            (gitignore_dir_repo / ".rhizome" / ".gitignore").mkdir(parents=True)
            cases.append((gitignore_dir_repo, ".gitignore must be a regular file"))

            for repo, expected in cases:
                before = sorted(
                    (path.relative_to(repo), path.read_bytes() if path.is_file() else None)
                    for path in repo.rglob("*")
                )
                result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes",
                    ],
                    cwd=root, env=env, text=True, capture_output=True,
                )
                self.assertNotEqual(0, result.returncode, repo.name)
                self.assertIn(expected, result.stderr, repo.name)
                after = sorted(
                    (path.relative_to(repo), path.read_bytes() if path.is_file() else None)
                    for path in repo.rglob("*")
                )
                self.assertEqual(before, after, repo.name)

    def test_unsupported_valid_yaml_shapes_are_refused_without_mutation(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            env = os.environ.copy()
            env["RZM_UPDATE_MANIFEST_URL"] = "http://127.0.0.1:1/should-not-be-requested"
            cases = {
                "flow-map": "rhizome: {version: v9.8.7}\ncode:\n  enabled: true\n",
                "quoted-key": '"rhizome":\n  version: v9.8.7\ncode:\n  enabled: true\n',
                "merged-manager": "rhizome:\n  <<: {binaryManager: external}\n",
                "merged-manager-after-child": "rhizome:\n  version: ''\n  <<: {binaryManager: external}\n",
                "merged-root": "<<: {rhizome: {binaryManager: external}}\n",
                "merged-root-after-child": "code:\n  enabled: true\n<<: {rhizome: {binaryManager: external}}\n",
                "indented-root": "  rhizome:\n    binaryManager: external\n",
                "escaped-root": '"\\u0072hizome":\n  binaryManager: external\n',
                "escaped-manager": 'rhizome:\n  "\\u0062inaryManager": external\n',
                "explicit-manager-key": "rhizome:\n  ? binaryManager\n  : external\n",
                "tagged-nested-map": "rhizome:\n  !!map {binaryManager: external}\n",
            }
            for name, config_text in cases.items():
                repo = root / name
                (repo / ".rhizome").mkdir(parents=True)
                config = repo / ".rhizome" / "config.yml"
                config.write_text(config_text)

                result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes",
                    ],
                    cwd=root,
                    env=env,
                    text=True,
                    capture_output=True,
                )

                self.assertNotEqual(0, result.returncode, name)
                self.assertIn("unsupported Rhizome config shape", result.stderr, name)
                self.assertNotIn("should-not-be-requested", result.stderr, name)
                self.assertEqual(config_text, config.read_text(), name)
                self.assertFalse((repo / "bin").exists(), name)

    def test_rejects_windows_launcher_path_forms_before_mutation(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            repo = root / "repo"
            repo.mkdir()
            for launcher in [
                "C:/tools/rzm",
                "C:\\tools\\rzm",
                "\\\\server\\share\\rzm",
                "dir\\..\\outside\\rzm",
                "//server/share/rzm",
            ]:
                result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--launcher", launcher, "--user-binary", "skip", "--yes",
                    ],
                    text=True, capture_output=True,
                )
                self.assertNotEqual(0, result.returncode, launcher)
                self.assertIn("Windows-style", result.stderr, launcher)
                self.assertFalse((repo / ".rhizome").exists(), launcher)

            windows_project_paths = ["C:\\repo", "C:/repo", "//server/share"]
            (root / "C:\\repo").mkdir()
            (root / "C:" / "repo").mkdir(parents=True)
            for project in windows_project_paths:
                result = subprocess.run(
                    [
                        str(script_path), "--project", project,
                        "--user-binary", "skip", "--yes",
                    ],
                    cwd=root, text=True, capture_output=True,
                )
                self.assertNotEqual(0, result.returncode, project)
                self.assertIn("Windows-style", result.stderr, project)
            self.assertFalse((root / "C:\\repo" / ".rhizome").exists())
            self.assertFalse((root / "C:" / "repo" / ".rhizome").exists())

    @unittest.skipUnless(os.name == "posix", "symlink containment fixture is POSIX-only")
    def test_launcher_must_stay_in_project_including_symlink_escapes(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            repo = root / "repo"
            outside = root / "outside"
            repo.mkdir()
            outside.mkdir()
            (repo / "escape").symlink_to(outside, target_is_directory=True)
            inside_target = repo / "inside-target"
            inside_target.write_text("existing target")
            (repo / "launcher-link").symlink_to(inside_target)

            for launcher, message in [
                ("../outside/rzm", "stay inside"),
                (str(outside / "rzm"), "must be relative"),
                ("escape/rzm", "escapes the project root through a symlink"),
                ("launcher-link", "must not be a symlink"),
            ]:
                result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--launcher", launcher, "--user-binary", "skip",
                    ],
                    text=True,
                    capture_output=True,
                )
                self.assertNotEqual(0, result.returncode)
                self.assertIn(message, result.stderr)
                self.assertFalse((repo / ".rhizome").exists())

    @unittest.skipUnless(os.name == "posix", "cache symlink fixture is POSIX-only")
    def test_generated_launcher_rejects_symlinked_cache_paths_before_writes(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            version = "v9.8.7"
            httpd, base_url = self._start_fake_release(root, version, version)
            env = os.environ.copy()
            env["RZM_GITHUB_RELEASES_URL"] = f"{base_url}/releases"
            env["RZM_UPDATE_MANIFEST_URL"] = f"{base_url}/releases/latest"
            env["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin:" + env.get("PATH", "")

            def install_repo(name: str) -> Path:
                repo = root / name
                repo.mkdir()
                result = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes",
                    ],
                    cwd=repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                return repo

            try:
                bin_repo = install_repo("bin-symlink")
                outside_bin = root / "outside-bin"
                outside_bin.mkdir()
                (bin_repo / ".rhizome" / "bin").symlink_to(outside_bin, target_is_directory=True)
                bin_result = subprocess.run(
                    [str(bin_repo / "bin" / "rzm"), "--version"],
                    cwd=bin_repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertNotEqual(0, bin_result.returncode)
                self.assertIn("cache path must not contain symlinks", bin_result.stderr)
                self.assertEqual([], list(outside_bin.iterdir()))

                platform_repo = install_repo("platform-symlink")
                cache_root = platform_repo / ".rhizome" / "bin"
                cache_root.mkdir()
                outside_platform = root / "outside-platform"
                outside_platform.mkdir()
                (cache_root / self._platform_dir()).symlink_to(
                    outside_platform,
                    target_is_directory=True,
                )
                platform_result = subprocess.run(
                    [str(platform_repo / "bin" / "rzm"), "--version"],
                    cwd=platform_repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertNotEqual(0, platform_result.returncode)
                self.assertIn("cache path must not contain symlinks", platform_result.stderr)
                self.assertEqual([], list(outside_platform.iterdir()))

                target_repo = install_repo("target-symlink")
                platform_cache = (
                    target_repo / ".rhizome" / "bin" / self._platform_dir()
                )
                platform_cache.mkdir(parents=True)
                outside_target = root / "outside-target"
                outside_target.write_text("user-owned\n")
                (platform_cache / "rzm").symlink_to(outside_target)
                target_result = subprocess.run(
                    [str(target_repo / "bin" / "rzm"), "--version"],
                    cwd=target_repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertNotEqual(0, target_result.returncode)
                self.assertIn("cache target must be a regular file", target_result.stderr)
                self.assertEqual("user-owned\n", outside_target.read_text())
            finally:
                httpd.shutdown()
                httpd.server_close()

    @unittest.skipUnless(os.name == "posix", "binary version fixture is POSIX-only")
    def test_downloaded_binary_must_report_selected_pin_before_installation(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            selected_version = "v9.8.7"
            httpd, base_url = self._start_fake_release(
                root,
                selected_version,
                "v0.0.1",
            )
            env = os.environ.copy()
            home = root / "home"
            home.mkdir()
            env["HOME"] = str(home)
            env["RZM_GITHUB_RELEASES_URL"] = f"{base_url}/releases"
            env["RZM_UPDATE_MANIFEST_URL"] = f"{base_url}/releases/latest"
            env["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin:" + env.get("PATH", "")
            env["RZM_TEST_OS"] = "linux"
            try:
                repo = root / "repo"
                repo.mkdir()
                project_install = subprocess.run(
                    [
                        str(script_path), "--project", str(repo),
                        "--user-binary", "skip", "--yes",
                    ],
                    cwd=repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(
                    0,
                    project_install.returncode,
                    project_install.stdout + project_install.stderr,
                )
                launcher_result = subprocess.run(
                    [str(repo / "bin" / "rzm"), "--version"],
                    cwd=repo,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertNotEqual(0, launcher_result.returncode)
                self.assertIn("does not report selected pin", launcher_result.stderr)
                self.assertFalse(
                    (repo / ".rhizome" / "bin" / self._platform_dir() / "rzm").exists()
                )

                user_result = subprocess.run(
                    [
                        str(script_path), "--user", "--yes",
                        "--version", selected_version,
                    ],
                    cwd=root,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertNotEqual(0, user_result.returncode)
                self.assertIn(f"expected {selected_version}", user_result.stderr)
                self.assertFalse((home / ".local" / "bin" / "rzm").exists())
            finally:
                httpd.shutdown()
                httpd.server_close()

    @unittest.skipUnless(os.name == "posix", "user install fixture is POSIX-only")
    def test_user_install_registers_macos_paths_d(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            etc = root / "etc"
            paths_d = root / "etc" / "paths.d"
            home.mkdir()
            etc.mkdir()
            version = "v9.8.7"
            httpd, base_url = self._start_fake_release(root, version, version)

            env = os.environ.copy()
            env["HOME"] = str(home)
            env["PATH"] = "/usr/bin:/bin:/usr/sbin:/sbin:" + env.get("PATH", "")
            env["RZM_GITHUB_RELEASES_URL"] = f"{base_url}/releases"
            env["RZM_UPDATE_MANIFEST_URL"] = f"{base_url}/releases/latest"
            env["RZM_TEST_OS"] = "darwin"
            env["RZM_PATHS_D_DIR"] = str(paths_d)
            env["RZM_PATHS_FILE"] = str(root / "etc" / "paths")
            try:
                result = subprocess.run(
                    [str(script_path), "--user", "--yes"],
                    cwd=root,
                    env=env,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                self.assertTrue((home / ".local" / "bin" / "rzm").is_file())
                self.assertEqual(str(home / ".local" / "bin") + "\n", (paths_d / "rhizome").read_text())
                self.assertIn("Registered", result.stdout)

                try:
                    paths_d.chmod(0o555)
                    registered_result = subprocess.run(
                        [str(script_path), "--user", "--yes"],
                        cwd=root,
                        env=env,
                        text=True,
                        capture_output=True,
                    )
                finally:
                    paths_d.chmod(0o755)
                self.assertEqual(
                    0,
                    registered_result.returncode,
                    registered_result.stdout + registered_result.stderr,
                )
                self.assertIn("already registered", registered_result.stdout)
            finally:
                httpd.shutdown()
                httpd.server_close()

    @unittest.skipUnless(
        os.name == "posix" and hasattr(os, "geteuid") and os.geteuid() != 0,
        "permission fixture needs non-root POSIX",
    )
    def test_json_user_binary_policies_preflight_privileged_macos_paths(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            version = "v9.8.7"
            httpd, base_url = self._start_fake_release(root, version, version)
            fake_bin = root / "fake-bin"
            fake_bin.mkdir()
            fake_sudo = fake_bin / "sudo"
            fake_sudo.write_text(
                "#!/bin/sh\n"
                'printf "invoked\\n" >> "$RZM_SUDO_LOG"\n'
                "exit 99\n"
            )
            fake_sudo.chmod(0o755)
            try:
                for policy in ["install", "auto"]:
                    case_root = root / policy
                    home = case_root / "home"
                    repo = case_root / "repo"
                    etc = case_root / "etc"
                    home.mkdir(parents=True)
                    repo.mkdir()
                    etc.mkdir()
                    sudo_log = case_root / "sudo.log"
                    env = os.environ.copy()
                    env["HOME"] = str(home)
                    env["PATH"] = (
                        f"{fake_bin}:/usr/bin:/bin:/usr/sbin:/sbin"
                    )
                    env["RZM_GITHUB_RELEASES_URL"] = f"{base_url}/releases"
                    env["RZM_UPDATE_MANIFEST_URL"] = f"{base_url}/releases/latest"
                    env["RZM_TEST_OS"] = "darwin"
                    env["RZM_PATHS_D_DIR"] = str(etc / "paths.d")
                    env["RZM_PATHS_FILE"] = str(etc / "paths")
                    env["RZM_SUDO_LOG"] = str(sudo_log)
                    try:
                        etc.chmod(0o555)
                        result = subprocess.run(
                            [
                                str(script_path), "--project", str(repo),
                                "--user-binary", policy, "--yes", "--json",
                            ],
                            cwd=repo,
                            env=env,
                            text=True,
                            capture_output=True,
                        )
                    finally:
                        etc.chmod(0o755)

                    self.assertNotEqual(0, result.returncode, policy)
                    self.assertEqual("", result.stdout, policy)
                    self.assertIn(
                        "non-interactive install cannot use sudo",
                        result.stderr,
                        policy,
                    )
                    self.assertFalse(sudo_log.exists(), policy)
                    self.assertEqual([], list(home.iterdir()), policy)
                    self.assertEqual([], list(repo.iterdir()), policy)
            finally:
                httpd.shutdown()
                httpd.server_close()

    @unittest.skipUnless(os.name == "posix" and hasattr(os, "geteuid") and os.geteuid() != 0, "permission fixture needs non-root POSIX")
    def test_yes_user_install_preflights_privileged_macos_paths(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            home = root / "home"
            etc = root / "etc"
            fake_bin = root / "fake-bin"
            paths_d = etc / "paths.d"
            home.mkdir()
            etc.mkdir()
            fake_bin.mkdir()
            fake_sudo = fake_bin / "sudo"
            sudo_log = root / "sudo.log"
            fake_sudo.write_text(
                "#!/bin/sh\n"
                'printf "invoked\\n" >> "$RZM_SUDO_LOG"\n'
                "exit 1\n"
            )
            fake_sudo.chmod(0o755)
            version = "v9.8.7"
            httpd, base_url = self._start_fake_release(root, version, version)

            env = os.environ.copy()
            env["HOME"] = str(home)
            env["PATH"] = f"{fake_bin}:/usr/bin:/bin:/usr/sbin:/sbin:" + env.get("PATH", "")
            env["RZM_GITHUB_RELEASES_URL"] = f"{base_url}/releases"
            env["RZM_UPDATE_MANIFEST_URL"] = f"{base_url}/releases/latest"
            env["RZM_TEST_OS"] = "darwin"
            env["RZM_PATHS_D_DIR"] = str(paths_d)
            env["RZM_PATHS_FILE"] = str(root / "etc" / "paths")
            env["RZM_SUDO_LOG"] = str(sudo_log)
            try:
                etc.chmod(0o555)
                result = subprocess.run(
                    [str(script_path), "--user", "--yes"],
                    cwd=root,
                    env=env,
                    text=True,
                    capture_output=True,
                )
            finally:
                etc.chmod(0o755)
                httpd.shutdown()
                httpd.server_close()

            self.assertNotEqual(0, result.returncode)
            self.assertFalse((home / ".local" / "bin" / "rzm").exists())
            self.assertFalse((paths_d / "rhizome").exists())
            self.assertFalse(sudo_log.exists())
            self.assertIn("non-interactive install cannot use sudo", result.stderr)

    @unittest.skipUnless(os.name == "posix", "repo install fixture is POSIX-only")
    def test_repo_install_ensures_user_binary_when_rzm_missing(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            version = "v9.8.7"
            httpd, base_url = self._start_fake_release(root, version, version)

            def run_repo_install(
                case: str,
                user_policy: str,
                *,
                with_path_rzm: bool = False,
                repo_bin_on_path: bool = False,
                json_output: bool = False,
            ):
                home = root / f"home-{case}"
                repo = root / f"repo-{case}"
                home.mkdir()
                repo.mkdir()
                env = os.environ.copy()
                env["HOME"] = str(home)
                path = "/usr/bin:/bin:/usr/sbin:/sbin"
                if with_path_rzm:
                    fake_bin = root / f"fake-bin-{case}"
                    fake_bin.mkdir()
                    fake_rzm = fake_bin / "rzm"
                    fake_rzm.write_text("#!/bin/sh\necho rhizome v0.0.1\n")
                    fake_rzm.chmod(0o755)
                    path = f"{fake_bin}:{path}"
                if repo_bin_on_path:
                    path = f"{repo / 'bin'}:{path}"
                env["PATH"] = path
                env["RZM_GITHUB_RELEASES_URL"] = f"{base_url}/releases"
                env["RZM_UPDATE_MANIFEST_URL"] = f"{base_url}/releases/latest"
                env["RZM_TEST_OS"] = "linux"  # skip macOS paths.d in this fixture
                args = [
                    str(script_path), "--project", str(repo),
                    "--user-binary", user_policy,
                ]
                if json_output:
                    args.extend(["--yes", "--json"])
                result = subprocess.run(
                    args,
                    cwd=repo, env=env, text=True, capture_output=True,
                )
                return result, home, repo

            try:
                # (a) no rzm on PATH -> user binary installed too
                result, home, repo = run_repo_install("missing", "auto", json_output=True)
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                self.assertTrue((repo / "bin" / "rzm").is_file())
                self.assertEqual(1, len(result.stdout.splitlines()))
                payload = json.loads(result.stdout)
                self.assertTrue(payload["userScopeTouched"])
                self.assertEqual(str(repo.resolve()), payload["projectRoot"])
                self.assertIn("No rzm on PATH", result.stderr)
                self.assertIn("automatic user binary", result.stderr)
                user_bin = home / ".local" / "bin" / "rzm"
                self.assertTrue(user_bin.is_file())
                check = subprocess.run([str(user_bin), "--version"], text=True, capture_output=True)
                self.assertIn("rhizome v9.8.7", check.stdout)

                # (b) explicit skip leaves HOME untouched and prints the project launcher
                result, home, repo = run_repo_install("nouser", "skip")
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                self.assertTrue((repo / "bin" / "rzm").is_file())
                self.assertFalse((home / ".local" / "bin" / "rzm").exists())
                self.assertIn("Skipped user binary installation", result.stdout)
                self.assertIn(str(repo / "bin" / "rzm"), result.stdout)
                self.assertEqual([], list(home.iterdir()))
                self.assertFalse((repo / ".rhizome" / "bin").exists())
                self.assertFalse((repo / "AGENTS.md").exists())

                # (c) rzm already on PATH -> no user install, no opt-out note
                result, home, repo = run_repo_install("present", "auto", with_path_rzm=True)
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                self.assertTrue((repo / "bin" / "rzm").is_file())
                self.assertFalse((home / ".local" / "bin" / "rzm").exists())
                self.assertNotIn("No rzm on PATH", result.stdout)

                # (d) install always writes the standalone user binary.
                result, home, repo = run_repo_install("install", "install", with_path_rzm=True)
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                self.assertTrue((repo / "bin" / "rzm").is_file())
                self.assertTrue((home / ".local" / "bin" / "rzm").is_file())

                # (e) launcher creation must not change auto's invocation-time decision.
                result, home, repo = run_repo_install(
                    "future-launcher-path",
                    "auto",
                    repo_bin_on_path=True,
                    json_output=True,
                )
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                self.assertTrue((repo / "bin" / "rzm").is_file())
                self.assertTrue((home / ".local" / "bin" / "rzm").is_file())
                self.assertIn("No rzm on PATH", result.stderr)
                self.assertTrue(json.loads(result.stdout)["userScopeTouched"])
            finally:
                httpd.shutdown()
                httpd.server_close()

    def test_rejects_legacy_no_user_with_migration(self) -> None:
        script_path = Path(__file__).with_name("install-rzm.sh")
        result = subprocess.run(
            [str(script_path), "--user", "--no-user", "--yes"],
            text=True, capture_output=True,
        )
        self.assertNotEqual(0, result.returncode)
        self.assertIn("--no-user has been removed", result.stderr)
        self.assertIn("--user-binary skip", result.stderr)

    def _fake_archive(self, root: Path, version: str) -> Path:
        src = root / "src"
        src.mkdir()
        binary = src / "rzm"
        binary.write_text(
            "#!/bin/sh\n"
            'if [ -n "${RZM_INVOCATION_LOG:-}" ]; then '
            'printf "%s\\n" "$*" >> "$RZM_INVOCATION_LOG"; fi\n'
            f"echo rhizome {version}\n"
        )
        binary.chmod(0o755)
        archive = root / f"rhizome-{self._platform_dir()}.tar.gz"
        with tarfile.open(archive, "w:gz") as tar:
            tar.add(binary, arcname="rzm")
        return archive

    def _start_fake_release(
        self,
        root: Path,
        selected_version: str,
        binary_version: str,
    ) -> tuple[ThreadingHTTPServer, str]:
        server_root = root / "server"
        server_root.mkdir()
        archive = self._fake_archive(server_root, binary_version)
        sha = hashlib.sha256(archive.read_bytes()).hexdigest()

        class QuietHandler(SimpleHTTPRequestHandler):
            def __init__(self, *args, **kwargs):
                super().__init__(*args, directory=server_root, **kwargs)

            def log_message(self, format: str, *args: object) -> None:
                return

        httpd = ThreadingHTTPServer(("127.0.0.1", 0), QuietHandler)
        thread = threading.Thread(target=httpd.serve_forever, daemon=True)
        thread.start()
        base_url = f"http://127.0.0.1:{httpd.server_port}"
        archive_name = archive.name
        installer = server_root / "install-rzm.sh"
        shutil.copyfile(Path(__file__).with_name("install-rzm.sh"), installer)
        installer.chmod(0o755)
        installer_sha = hashlib.sha256(installer.read_bytes()).hexdigest()
        (server_root / "install-rzm.sh.sha256").write_text(
            f"{installer_sha}  install-rzm.sh\n"
        )
        (server_root / "checksums.txt").write_text(f"{sha}  {archive_name}\n")
        release = json.dumps(
            {
                "tag_name": selected_version,
                "draft": False,
                "prerelease": "-" in selected_version,
                "assets": [
                    {"name": archive_name, "browser_download_url": f"{base_url}/{archive_name}"},
                    {"name": "checksums.txt", "browser_download_url": f"{base_url}/checksums.txt"},
                    {"name": "install-rzm.sh", "browser_download_url": f"{base_url}/install-rzm.sh"},
                    {"name": "install-rzm.sh.sha256", "browser_download_url": f"{base_url}/install-rzm.sh.sha256"},
                ],
            }
        )
        latest_path = server_root / "releases" / "latest"
        latest_path.parent.mkdir()
        latest_path.write_text(release)
        tag_path = server_root / "releases" / "tags" / selected_version
        tag_path.parent.mkdir()
        tag_path.write_text(release)
        return httpd, base_url

    def _platform_key(self) -> str:
        return self._platform_dir().replace("-", "/")

    def _platform_dir(self) -> str:
        system = platform.system()
        machine = platform.machine().lower()
        os_name = {"Darwin": "darwin", "Linux": "linux"}.get(system, system.lower())
        arch = "arm64" if machine in {"arm64", "aarch64"} else "amd64"
        return f"{os_name}-{arch}"

    def _dev_os_dir(self) -> str:
        return {"Darwin": "darwin", "Linux": "linux"}.get(platform.system(), platform.system().lower())


if __name__ == "__main__":
    unittest.main()
