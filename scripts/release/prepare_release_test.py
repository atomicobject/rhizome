#!/usr/bin/env python3
"""Exercise local editorial preparation against disposable Git histories."""

from __future__ import annotations

from contextlib import redirect_stdout
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import prepare_release as prep
from release_actions import VERSION_PATH, render_version_go
from release_intent import MANIFEST_PATH, notes_path, release_files
from release_types import DeliveryUnit, EffortEvidence, GitCommit, ReleaseEvidence


class PreparationTest(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.home = Path(self.folder.name)
        self.remote = self.home / "origin.git"
        self.root = self.home / "work"
        subprocess.run(("git", "init", "--bare", str(self.remote)), check=True, capture_output=True)
        subprocess.run(("git", "init", "-b", "main", str(self.root)), check=True, capture_output=True)
        self.git("config", "user.name", "Fixture Maintainer")
        self.git("config", "user.email", "fixture@example.test")
        self.git("config", "core.hooksPath", str(self.home / "no-hooks"))
        (self.root / ".gitignore").write_text(".release/\n")
        (self.root / "CHANGELOG.md").write_text("# Changelog\n\n## [Unreleased]\n\n- Desktop app.\n")
        version = self.root / VERSION_PATH
        version.parent.mkdir(parents=True)
        version.write_text(render_version_go("v0.50.5"))
        embedded = self.root / "pkg/vault/changelog/CHANGELOG.md"
        embedded.parent.mkdir(parents=True)
        embedded.write_bytes((self.root / "CHANGELOG.md").read_bytes())
        self.git("add", ".")
        self.git("commit", "-m", "Initial product")
        self.git("remote", "add", "origin", str(self.remote))
        self.git("push", "origin", "main")
        self.source = self.git("rev-parse", "HEAD").strip()
        self.notes = self.home / "notes.md"
        self.notes.write_bytes(b"# Rhizome on your desktop\r\n\r\nReviewed **narrative**, not a PR inventory.\r\n")
        self.changelog = self.home / "changelog.md"
        self.changelog.write_text("- Added the desktop app.\n- Redesigned the workbench.\n")

    def git(self, *args):
        return subprocess.run(("git", *args), cwd=self.root, check=True, capture_output=True, text=True).stdout

    def prepare(self, **overrides):
        args = dict(source=self.source, version="v0.51.0", notes=self.notes,
                    changelog=self.changelog, release_date="2026-11-05")
        args.update(overrides)
        return prep.prepare(self.root, **args)

    def test_first_release_writes_only_reviewed_files_without_committing_or_tagging(self):
        notes = self.notes.read_bytes()
        manifest = self.prepare()
        self.assertEqual(manifest["base_tag"], "")
        self.assertEqual(self.git("branch", "--show-current").strip(), "release-prep/v0.51.0")
        self.assertEqual(self.git("rev-parse", "HEAD").strip(), self.source)
        self.assertEqual(self.git("tag", "--list"), "")
        self.assertEqual((self.root / notes_path(manifest["version"])).read_bytes(), notes)
        self.assertEqual(prep.check(self.root), manifest)
        changed = set(self.git("diff", "--name-only").splitlines())
        changed.update(self.git("ls-files", "--others", "--exclude-standard").splitlines())
        self.assertEqual(changed, {p.as_posix() for p in release_files(manifest["version"])})
        self.assertEqual(self.git("ls-remote", "origin", "refs/heads/main").split()[0], self.source)

    def test_existing_tag_and_source_version_are_both_version_floors(self):
        self.git("tag", "v0.50.4")
        self.git("push", "origin", "v0.50.4")
        for version in ("v0.50.4", "v0.50.5"):
            with self.subTest(version=version), self.assertRaisesRegex(ValueError, "greater"):
                self.prepare(version=version)
        self.assertEqual(self.git("branch", "--show-current").strip(), "main")
        self.assertFalse((self.root / MANIFEST_PATH).exists())

    def test_dirty_unrelated_work_is_preserved_and_refused(self):
        sentinel = self.root / "unrelated.txt"
        sentinel.write_text("work in progress\n")
        with self.assertRaisesRegex(prep.PreparationError, "clean"):
            self.prepare()
        self.assertEqual(sentinel.read_text(), "work in progress\n")
        self.assertFalse((self.root / MANIFEST_PATH).exists())

    def test_invalid_editorial_input_is_rejected_before_branch_or_file_changes(self):
        for content in (b" \n", b"\xff", b"text\x00text"):
            with self.subTest(content=content):
                self.notes.write_bytes(content)
                with self.assertRaises(prep.PreparationError):
                    self.prepare()
                self.assertEqual(self.git("branch", "--show-current").strip(), "main")
                self.assertFalse((self.root / MANIFEST_PATH).exists())

    def test_remote_main_movement_is_refused_before_preparation(self):
        self.git("switch", "-c", "other-work")
        (self.root / "new.txt").write_text("a newer source\n")
        self.git("add", "new.txt")
        self.git("commit", "-m", "Advance main elsewhere")
        self.git("push", "origin", "HEAD:main")
        self.git("switch", "main")
        with self.assertRaisesRegex(prep.PreparationError, "freshly fetched origin/main"):
            self.prepare()
        self.assertEqual(self.git("rev-parse", "HEAD").strip(), self.source)
        self.assertFalse((self.root / MANIFEST_PATH).exists())

    def test_source_cannot_change_since_the_editorial_evidence(self):
        (self.root / "new.txt").write_text("new product change\n")
        self.git("add", "new.txt")
        self.git("commit", "-m", "New product change")
        self.git("push", "origin", "main")
        with self.assertRaisesRegex(prep.PreparationError, "reviewed evidence source"):
            self.prepare()
        self.assertFalse((self.root / MANIFEST_PATH).exists())

    def test_remote_main_movement_is_refused_after_review(self):
        other = self.home / "other"
        subprocess.run(("git", "clone", "-b", "main", str(self.remote), str(other)), check=True, capture_output=True)
        for key, value in (("user.name", "Fixture"), ("user.email", "fixture@example.test")):
            subprocess.run(("git", "config", key, value), cwd=other, check=True)
        self.prepare()
        (other / "new.txt").write_text("main moved\n")
        for args in (("add", "."), ("commit", "-m", "New source"), ("push", "origin", "main")):
            subprocess.run(("git", *args), cwd=other, check=True, capture_output=True)
        with self.assertRaisesRegex(prep.PreparationError, "main changed"):
            prep.check(self.root)
        self.assertEqual((self.root / notes_path("v0.51.0")).read_bytes(), self.notes.read_bytes())

    def test_human_note_edits_survive_sync_and_unrelated_edits_are_rejected(self):
        self.prepare()
        narrative = self.root / notes_path("v0.51.0")
        revised = b"# A better headline\n\nThe maintainer's exact revised story.\n"
        narrative.write_bytes(revised)
        root_changelog = self.root / "CHANGELOG.md"
        root_changelog.write_text(root_changelog.read_text().replace("Added the desktop app.", "Made the desktop app the primary experience."))
        with self.assertRaisesRegex(ValueError, "changelogs differ"):
            prep.check(self.root)
        prep.sync(self.root)
        self.assertEqual(narrative.read_bytes(), revised)
        self.assertEqual(root_changelog.read_bytes(), (self.root / "pkg/vault/changelog/CHANGELOG.md").read_bytes())
        (self.root / "unrelated.txt").write_text("keep this\n")
        with self.assertRaisesRegex(prep.PreparationError, "five release-owned"):
            prep.sync(self.root)
        self.assertEqual(narrative.read_bytes(), revised)
        self.git("add", ".")
        self.git("commit", "-m", "Unrelated code hidden in release branch")
        with self.assertRaisesRegex(prep.PreparationError, "five release-owned"):
            prep.check(self.root)

    def test_committed_preparation_still_checks_and_new_base_tag_invalidates_it(self):
        self.prepare()
        self.git("add", ".")
        self.git("commit", "-m", "Prepare reviewed release")
        self.assertEqual(prep.check(self.root)["version"], "v0.51.0")
        self.git("tag", "v0.50.5", self.source)
        self.git("push", "origin", "v0.50.5")
        with self.assertRaisesRegex(prep.PreparationError, "previous release tag changed"):
            prep.check(self.root)

    def test_evidence_emits_bounded_packets_without_calling_a_model(self):
        with patch("notes.generate_release_notes", side_effect=AssertionError("model must not run")):
            result = prep.collect_evidence(self.root)
        self.assertEqual(result["source_commit"], self.source)
        self.assertEqual(result["source_version"], "v0.50.5")
        self.assertEqual(result["base_tag"], "")
        self.assertEqual(result["github_status"], "unavailable")
        output = Path(result["output_dir"])
        for name in result["coverage"] + result["packets"]:
            self.assertLessEqual(len((output / name).read_text()), prep.EDITORIAL_BUDGET_CHARS)
        for name in result["packets"]:
            packet = json.loads((output / name).read_text())
            self.assertEqual(packet["packet"]["release_range"]["head_commit"], self.source)
        self.assertEqual(self.git("status", "--porcelain"), "")

    def test_large_editorial_projection_covers_every_summary_and_source_in_small_pages(self):
        units = []
        for index in range(40):
            commit = GitCommit(sha=f"{index:040x}", subject=f"Delivery {index}", body="details " * 5000)
            units.append(DeliveryUnit(mainline_commit=commit, commits=(commit,),
                changed_paths=(f"pkg/feature{index}/implementation.go",),
                efforts=(EffortEvidence(f"EFF-{index}", f"docs/efforts/{index}.md", "Delivered feature", "outcome"),)))
        entries = tuple(f"Curated {index}: " + "reviewed context " * 40 for index in range(400))
        evidence = ReleaseEvidence("", "", self.source, "main", entries, tuple(units))
        output = self.home / "editorial"
        with patch("notes.generate_release_notes", side_effect=AssertionError("model must not run")):
            result = prep.write_editorial_evidence(evidence, output)
        records = []
        for name in result["coverage"] + result["packets"]:
            self.assertLessEqual(len((output / name).read_text()), 24_000)
        for name in result["coverage"]:
            records.extend(json.loads((output / name).read_text())["records"])
        deliveries = [row for row in records if row["kind"] == "delivery"]
        curated = [row for row in records if row["kind"] == "curated"]
        references = [row for row in records if row["kind"] == "source"]
        self.assertEqual([row["unit_id"] for row in deliveries], [unit.unit_id for unit in units])
        self.assertEqual([row["source_id"] for row in curated], [f"unreleased:{n}" for n in range(1, 401)])
        self.assertEqual({row["path"] for row in references if "path" in row}, {f"docs/efforts/{n}.md" for n in range(40)})
        for row in deliveries:
            packet = json.loads((output / row["packets"][0]).read_text())
            self.assertEqual(packet["packet"]["delivery_units"][0]["unit_id"], row["unit_id"])
        self.assertEqual((output / "evidence.json").read_text(), evidence.to_json())
        self.assertGreater(len(result["coverage"]), 1)

    def test_oversized_delivery_reference_list_retains_complete_coverage_and_targeted_audit(self):
        commits = tuple(GitCommit(sha=f"{index:040x}", subject="Change") for index in range(600))
        unit = DeliveryUnit(mainline_commit=commits[0], commits=commits)
        evidence = ReleaseEvidence("", "", self.source, "main", delivery_units=(unit,))
        output = self.home / "oversized-unit"
        result = prep.write_editorial_evidence(evidence, output)
        self.assertEqual(result["detail_fallback_units"], [unit.unit_id])
        records = []
        for name in result["coverage"]:
            content = (output / name).read_text()
            self.assertLessEqual(len(content), 24_000)
            records.extend(json.loads(content)["records"])
        self.assertEqual([row["source_id"] for row in records if row["kind"] == "source"],
                         [f"commit:{commit.sha}" for commit in commits])
        self.assertEqual((output / "evidence.json").read_text(), evidence.to_json())

    def test_cli_requires_explicit_source_and_prints_only_release_intent(self):
        with redirect_stdout(io.StringIO()) as stdout:
            status = prep.main(("--repo-root", str(self.root), "prepare", "--source", self.source,
                                "--version", "v0.51.0", "--notes-file", str(self.notes),
                                "--changelog-file", str(self.changelog), "--date", "2026-11-05"))
        self.assertEqual(status, 0)
        self.assertEqual(json.loads(stdout.getvalue())["source_commit"], self.source)


if __name__ == "__main__":
    unittest.main()
