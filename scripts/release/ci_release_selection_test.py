#!/usr/bin/env python3
"""Exercise release approval boundaries against real Git histories."""
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from release_actions import apply_release_files, VERSION_PATH, render_version_go
from release_intent import MANIFEST_PATH, notes_path
from ci_release_selection import REPOSITORY, select, validate_pull_request


class SelectionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Release test")
        self.git("config", "user.email", "release@example.invalid")
        (self.root / VERSION_PATH).parent.mkdir(parents=True)
        (self.root / VERSION_PATH).write_text(render_version_go("v0.50.5"))
        (self.root / "CHANGELOG.md").write_text("# Changelog\n\n## [Unreleased]\n\n- Desktop\n\n## [v0.50.5] - 2026-10-01\n\n- Prior release\n")
        self.commit("Source")
        self.source = self.git("rev-parse", "HEAD")
        self.git("switch", "-c", "release-prep/v0.51.0")
        apply_release_files(self.root, version="v0.51.0", release_date="2026-11-05", changelog_entries=["A desktop app"])
        self.notes = notes_path("v0.51.0")
        (self.root / self.notes).parent.mkdir(parents=True)
        (self.root / self.notes).write_text("# A new desktop experience\n\nHuman-edited narrative.\n")
        self.manifest = {"schema_version": 1, "version": "v0.51.0", "source_commit": self.source, "base_tag": "", "release_date": "2026-11-05"}
        (self.root / MANIFEST_PATH).parent.mkdir(parents=True)
        self.write_manifest()
        self.commit("Prepare release")
        self.head = self.git("rev-parse", "HEAD")

    def git(self, *args):
        r = subprocess.run(["git", *args], cwd=self.root, capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        return r.stdout.strip()

    def commit(self, subject):
        self.git("add", ".")
        self.git("commit", "-m", subject)

    def write_manifest(self):
        (self.root / MANIFEST_PATH).write_text(json.dumps(self.manifest))

    def merge(self, squash=False):
        self.head = self.git("rev-parse", "HEAD")
        self.git("switch", "main")
        if squash:
            self.git("merge", "--squash", "release-prep/v0.51.0")
            self.commit("Release v0.51.0")
        else:
            self.git("merge", "--no-ff", "release-prep/v0.51.0", "-m", "Merge reviewed release")
        commit = self.git("rev-parse", "HEAD")
        return {"number": 123, "merged": True, "commits": 1, "merge_commit_sha": commit,
                "base": {"ref": "main", "repo": {"full_name": REPOSITORY}},
                "head": {"ref": "release-prep/v0.51.0", "sha": self.head, "repo": {"full_name": REPOSITORY}}}

    def test_merge_preserves_exact_edited_notes_and_source(self):
        pr = self.merge()
        chosen = validate_pull_request(self.root, pr, event_sha=pr["merge_commit_sha"])
        self.assertEqual(chosen["source"], pr["merge_commit_sha"])
        self.assertEqual(chosen["version"], "v0.51.0")
        self.assertEqual((self.root / self.notes).read_text(), "# A new desktop experience\n\nHuman-edited narrative.\n")

    def test_squash_merge_and_matching_completed_tag_are_valid_recovery_inputs(self):
        pr = self.merge(squash=True)
        self.git("tag", "v0.51.0")
        self.assertEqual(validate_pull_request(self.root, pr)["version"], "v0.51.0")

    def test_rebased_multiple_commits_keep_original_approved_source(self):
        (self.root / self.notes).write_text("Edited a second time.\n")
        self.commit("Review narrative")
        self.head = self.git("rev-parse", "HEAD")
        self.git("switch", "main")
        self.git("merge", "--ff-only", "release-prep/v0.51.0")
        pr = {"number": 123, "merged": True, "commits": 2, "merge_commit_sha": self.head,
              "base": {"ref": "main", "repo": {"full_name": REPOSITORY}},
              "head": {"ref": "release-prep/v0.51.0", "sha": self.head, "repo": {"full_name": REPOSITORY}}}
        self.assertEqual(validate_pull_request(self.root, pr)["source"], self.head)

    def test_ordinary_merge_selects_no_release(self):
        pr = self.merge()
        pr["head"]["ref"] = "feature/ordinary"
        self.assertIsNone(select(self.root, {"repository": {"full_name": REPOSITORY}, "pull_request": pr}, "pull_request", pr["merge_commit_sha"]))

    def test_manual_dispatch_cannot_rebuild_a_partially_published_release(self):
        with self.assertRaisesRegex(ValueError, "retrying the original failed publisher"):
            select(self.root, {"repository": {"full_name": REPOSITORY}}, "workflow_dispatch", self.source)

    def test_unmerged_fork_and_wrong_merge_event_are_rejected(self):
        pr = self.merge()
        for field in ("unmerged", "fork", "event"):
            case = json.loads(json.dumps(pr))
            if field == "unmerged": case["merged"] = False
            if field == "fork": case["head"]["repo"]["full_name"] = "someone/fork"
            with self.subTest(field=field), self.assertRaises(ValueError):
                validate_pull_request(self.root, case, event_sha="0" * 40 if field == "event" else None)

    def test_source_code_hidden_in_release_pr_is_rejected(self):
        (self.root / "surprise.go").write_text("package surprise\n")
        self.commit("Unapproved source change")
        with self.assertRaisesRegex(ValueError, "only the approved release files"):
            validate_pull_request(self.root, self.merge())

    def test_main_movement_before_merge_rejects_stale_preparation(self):
        self.git("switch", "main")
        (self.root / "new-feature.txt").write_text("Another delivery\n")
        self.commit("Main advances")
        self.git("switch", "release-prep/v0.51.0")
        with self.assertRaisesRegex(ValueError, "reviewed PR head|main changed"):
            validate_pull_request(self.root, self.merge())

    def test_changed_base_tag_and_non_increasing_version_are_rejected(self):
        self.git("tag", "v0.50.5", self.source)
        with self.assertRaisesRegex(ValueError, "previous version tag"):
            validate_pull_request(self.root, self.merge())

    def test_modified_notes_after_commit_are_not_approved(self):
        pr = self.merge()
        (self.root / self.notes).write_text("An unreviewed edit\n")
        with self.assertRaisesRegex(ValueError, "clean"):
            validate_pull_request(self.root, pr)

    def test_candidate_is_read_only_and_needs_no_manifest_or_model(self):
        self.git("checkout", "--detach", self.source)
        selected = select(self.root, {"repository": {"full_name": REPOSITORY}}, "workflow_dispatch", self.source, candidate=True)
        self.assertEqual(selected, {"source": self.source, "version": "v0.50.5", "release_pr": "", "candidate_only": "true"})
        self.assertFalse((self.root / MANIFEST_PATH).exists())
        self.assertEqual(self.git("status", "--porcelain"), "")
        self.assertEqual(self.git("tag", "--list"), "")


if __name__ == "__main__":
    unittest.main()
