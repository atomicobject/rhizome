#!/usr/bin/env python3
"""Behavior tests for idempotent release file mutations."""

from __future__ import annotations

from pathlib import Path
import os
import re
import subprocess
import tempfile
import unittest

import release_actions
from run_clean import scoped_environment


UNRELEASED_CHANGELOG = """# Changelog

## [Unreleased]

- Existing unreleased detail.

## [v1.2.2] - 2026-07-01

- Previous release.
"""


class ReleaseActionsTest(unittest.TestCase):
    def test_release_version_requires_canonical_v_semver(self) -> None:
        for version in ("v0.0.0", "v1.2.3", "v100.20.300"):
            with self.subTest(version=version):
                self.assertEqual(release_actions.validate_release_version(version), version)

        for version in (
            "1.2.3",
            "v1.2",
            "v01.2.3",
            "v1.02.3",
            "v1.2.03",
            "v1.2.3-rc.1",
            "v1.2.3+build",
            " v1.2.3",
            "v1.2.3\n",
        ):
            with self.subTest(version=version):
                with self.assertRaisesRegex(ValueError, "vMAJOR.MINOR.PATCH"):
                    release_actions.validate_release_version(version)

    def test_changelog_stamps_plan_entries_and_clears_unreleased_body(self) -> None:
        rendered = release_actions.render_changelog(
            UNRELEASED_CHANGELOG,
            version="v1.3.0",
            release_date="2026-07-17",
            changelog_entries=("Added bounded release planning.", "- Fixed retries."),
        )

        self.assertEqual(
            rendered,
            """# Changelog

## [Unreleased]

## [v1.3.0] - 2026-07-17

- Added bounded release planning.
- Fixed retries.

## [v1.2.2] - 2026-07-01

- Previous release.
""",
        )
        self.assertNotIn("Existing unreleased detail", rendered)

    def test_changelog_retry_is_identical_and_has_one_version_section(self) -> None:
        first = release_actions.render_changelog(
            UNRELEASED_CHANGELOG,
            version="v1.3.0",
            release_date="2026-07-17",
            changelog_entries=("Added bounded release planning.",),
        )
        second = release_actions.render_changelog(
            first,
            version="v1.3.0",
            release_date="2026-07-17",
            changelog_entries=("Added bounded release planning.",),
        )

        self.assertEqual(second, first)
        self.assertEqual(second.count("## [v1.3.0] -"), 1)

    def test_changelog_collapses_preexisting_duplicate_target_sections(self) -> None:
        duplicated = UNRELEASED_CHANGELOG + """
## [v1.3.0] - 2026-07-16

- Stale duplicate.
"""

        rendered = release_actions.render_changelog(
            duplicated,
            version="v1.3.0",
            release_date="2026-07-17",
            changelog_entries=("Final entry.",),
        )

        self.assertEqual(rendered.count("## [v1.3.0] -"), 1)
        self.assertNotIn("Stale duplicate", rendered)
        self.assertIn("## [v1.3.0] - 2026-07-17\n\n- Final entry.", rendered)

    def test_changelog_without_unreleased_is_stamped_after_title(self) -> None:
        rendered = release_actions.render_changelog(
            "# Changelog\n\n## [v1.2.2] - 2026-07-01\n\n- Previous.\n",
            version="v1.3.0",
            release_date="2026-07-17",
            changelog_entries=("Added release planning.",),
        )

        self.assertTrue(
            rendered.startswith(
                "# Changelog\n\n## [v1.3.0] - 2026-07-17\n\n"
                "- Added release planning.\n\n## [v1.2.2]"
            )
        )

    def test_version_source_is_deterministic(self) -> None:
        current = (Path(__file__).resolve().parents[2] / release_actions.VERSION_PATH).read_text()
        expected, replacements = re.subn(r'(?m)^var Version = "[^"]*"$', 'var Version = "v1.3.0"', current)
        self.assertEqual(replacements, 1)
        self.assertEqual(
            release_actions.render_version_go("v1.3.0"),
            expected,
        )

    def test_prepared_version_compiles_and_preserves_release_channel_behavior(self) -> None:
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "go.mod").write_text("module github.com/atomicobject/rhizome\n\ngo 1.24\n")
            package = root / release_actions.VERSION_PATH
            package.parent.mkdir(parents=True)
            package.write_text(release_actions.render_version_go("v1.3.0"))
            (root / "main.go").write_text('package main\nimport ("fmt"; "github.com/atomicobject/rhizome/pkg/vault/version")\nfunc main() { fmt.Printf("%s %s %t", version.Version, version.Channel, version.Development()) }\n')
            environment = scoped_environment(os.environ, "build")
            environment.update(GOTOOLCHAIN="local", CGO_ENABLED="0")
            for flags, expected in (([], "v1.3.0 dev true"),
                    (["-ldflags", "-X github.com/atomicobject/rhizome/pkg/vault/version.Channel=release"], "v1.3.0 release false")):
                with self.subTest(expected=expected):
                    result = subprocess.run(["go", "run", *flags, "."], cwd=root,
                        env=environment, capture_output=True, text=True, timeout=60)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(result.stdout, expected)

    def test_apply_changes_exactly_three_files_and_is_noop_on_retry(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo = Path(temp_dir)
            version_path = repo / "pkg/vault/version/version.go"
            embedded_path = repo / "pkg/vault/changelog/CHANGELOG.md"
            version_path.parent.mkdir(parents=True)
            embedded_path.parent.mkdir(parents=True)
            (repo / "CHANGELOG.md").write_text(UNRELEASED_CHANGELOG, encoding="utf-8")
            version_path.write_text("old version\n", encoding="utf-8")
            embedded_path.write_text("old changelog\n", encoding="utf-8")
            sentinel = repo / "sentinel.txt"
            sentinel.write_text("unchanged\n", encoding="utf-8")

            changed = release_actions.apply_release_files(
                repo,
                version="v1.3.0",
                release_date="2026-07-17",
                changelog_entries=("Added bounded release planning.",),
            )

            self.assertEqual(
                changed,
                (
                    Path("CHANGELOG.md"),
                    Path("pkg/vault/version/version.go"),
                    Path("pkg/vault/changelog/CHANGELOG.md"),
                ),
            )
            changelog = (repo / "CHANGELOG.md").read_text(encoding="utf-8")
            self.assertEqual(embedded_path.read_text(encoding="utf-8"), changelog)
            self.assertEqual(version_path.read_text(encoding="utf-8"), release_actions.render_version_go("v1.3.0"))
            self.assertEqual(sentinel.read_text(encoding="utf-8"), "unchanged\n")

            retry = release_actions.apply_release_files(
                repo,
                version="v1.3.0",
                release_date="2026-07-17",
                changelog_entries=("Added bounded release planning.",),
            )

            self.assertEqual(retry, ())
            self.assertEqual(changelog.count("## [v1.3.0] -"), 1)

    def test_partial_apply_converges_on_retry(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            repo = Path(temp_dir)
            version_path = repo / "pkg/vault/version/version.go"
            embedded_path = repo / "pkg/vault/changelog/CHANGELOG.md"
            version_path.parent.mkdir(parents=True)
            embedded_path.parent.mkdir(parents=True)
            desired_changelog = release_actions.render_changelog(
                UNRELEASED_CHANGELOG,
                version="v1.3.0",
                release_date="2026-07-17",
                changelog_entries=("Added bounded release planning.",),
            )
            (repo / "CHANGELOG.md").write_text(desired_changelog, encoding="utf-8")
            version_path.write_text("old version\n", encoding="utf-8")
            embedded_path.write_text("old changelog\n", encoding="utf-8")

            changed = release_actions.apply_release_files(
                repo,
                version="v1.3.0",
                release_date="2026-07-17",
                changelog_entries=("Added bounded release planning.",),
            )

            self.assertEqual(
                changed,
                (
                    Path("pkg/vault/version/version.go"),
                    Path("pkg/vault/changelog/CHANGELOG.md"),
                ),
            )
            self.assertEqual((repo / "CHANGELOG.md").read_text(encoding="utf-8"), desired_changelog)
            self.assertEqual(embedded_path.read_text(encoding="utf-8"), desired_changelog)


if __name__ == "__main__":
    unittest.main()
