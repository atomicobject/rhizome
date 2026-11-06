#!/usr/bin/env python3
"""Trust-boundary tests for reviewed release intent and its committed content."""

from __future__ import annotations

import json
from pathlib import Path
import tempfile
import unittest

from release_actions import apply_release_files
from release_intent import MANIFEST_PATH, notes_path, parse_manifest, release_files, validate_prepared


MANIFEST = {"schema_version": 1, "source_commit": "a" * 40, "base_tag": "v1.2.3",
            "version": "v1.3.0", "release_date": "2026-11-05"}


def prepared(root: Path) -> None:
    (root / "CHANGELOG.md").write_text("# Changelog\n\n## [Unreleased]\n\n- Desktop app.\n", encoding="utf-8")
    apply_release_files(root, version=MANIFEST["version"], release_date=MANIFEST["release_date"],
                        changelog_entries=("Added the desktop app.",))
    (root / MANIFEST_PATH).parent.mkdir(parents=True, exist_ok=True)
    (root / MANIFEST_PATH).write_text(json.dumps(MANIFEST), encoding="utf-8")
    path = root / notes_path(MANIFEST["version"])
    path.parent.mkdir(parents=True)
    path.write_bytes(b"# A new desktop experience\r\n\r\nThe reviewed narrative.\r\n")


class ReleaseIntentTest(unittest.TestCase):
    def test_manifest_rejects_ambiguous_or_path_bearing_intent(self):
        invalid = [
            {**MANIFEST, "schema_version": True},
            {**MANIFEST, "schema_version": 2},
            {**MANIFEST, "source_commit": "HEAD"},
            {**MANIFEST, "source_commit": "A" * 40},
            {**MANIFEST, "version": "../../outside"},
            {**MANIFEST, "version": "v1.2.3"},
            {**MANIFEST, "base_tag": "v2.0.0"},
            {**MANIFEST, "release_date": "2026-2-1"},
            {**MANIFEST, "notes_path": "outside.md"},
            {**MANIFEST, "api_key": "synthetic-fixture"},
        ]
        for value in invalid:
            with self.subTest(value=value), self.assertRaises(ValueError):
                parse_manifest(json.dumps(value))
        with self.assertRaisesRegex(ValueError, "repeats"):
            parse_manifest(json.dumps(MANIFEST)[:-1] + ', "version": "v1.4.0"}')

    def test_first_release_retains_empty_prior_tag_and_derived_owned_paths(self):
        value = parse_manifest(json.dumps({**MANIFEST, "base_tag": ""}))
        self.assertEqual(value["base_tag"], "")
        self.assertEqual(notes_path(value["version"]), Path("docs/releases/v1.3.0.md"))
        self.assertEqual(len(release_files(value["version"])), 5)

    def test_validate_preserves_exact_narrative_bytes_and_detects_inconsistent_edits(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            prepared(root)
            narrative = root / notes_path(MANIFEST["version"])
            original = narrative.read_bytes()
            self.assertEqual(validate_prepared(root), MANIFEST)
            self.assertEqual(narrative.read_bytes(), original)
            embedded = root / "pkg/vault/changelog/CHANGELOG.md"
            embedded.write_text("human edit needs synchronization\n", encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "changelogs differ"):
                validate_prepared(root)

    def test_wrong_version_date_empty_notes_and_duplicate_sections_are_rejected(self):
        mutations = (
            ("pkg/vault/version/version.go", 'package version\nvar Version = "v9.0.0"\n'),
            ("pkg/vault/version/version.go", 'package version\nvar Version = "v1.3.0"\nfunc init() {}\n'),
            ("docs/releases/v1.3.0.md", " \n"),
        )
        for relative, content in mutations:
            with self.subTest(relative=relative), tempfile.TemporaryDirectory() as folder:
                root = Path(folder)
                prepared(root)
                (root / relative).write_text(content, encoding="utf-8")
                with self.assertRaises(ValueError):
                    validate_prepared(root)
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            prepared(root)
            changelog = root / "CHANGELOG.md"
            text = changelog.read_text() + "\n## [v1.3.0] - 2026-11-05\n\n- Duplicate.\n"
            changelog.write_text(text)
            (root / "pkg/vault/changelog/CHANGELOG.md").write_text(text)
            with self.assertRaisesRegex(ValueError, "exactly one"):
                validate_prepared(root)

    def test_release_content_cannot_follow_an_outside_symlink(self):
        with tempfile.TemporaryDirectory() as folder, tempfile.TemporaryDirectory() as outside:
            root = Path(folder)
            prepared(root)
            narrative = root / notes_path(MANIFEST["version"])
            narrative.unlink()
            external = Path(outside) / "fixture.md"
            external.write_text("synthetic outside content\n")
            narrative.symlink_to(external)
            with self.assertRaisesRegex(ValueError, "ordinary checkout file"):
                validate_prepared(root)


if __name__ == "__main__":
    unittest.main()
