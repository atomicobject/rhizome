#!/usr/bin/env python3

import subprocess
import tempfile
import unittest
from pathlib import Path

import evidence


def git(repo: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", *args], cwd=repo, capture_output=True, text=True, check=True
    )
    return result.stdout.strip()


def commit(repo: Path, subject: str, filename: str) -> str:
    path = repo / filename
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(subject, encoding="utf-8")
    git(repo, "add", filename)
    git(repo, "commit", "-m", subject)
    return git(repo, "rev-parse", "HEAD")


class MainlineEvidenceTest(unittest.TestCase):
    def make_repo(self) -> Path:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp_dir.cleanup)
        repo = Path(self.temp_dir.name)
        git(repo, "init", "-b", "main")
        git(repo, "config", "user.email", "release@example.com")
        git(repo, "config", "user.name", "Release Test")
        return repo

    def test_collect_mainline_keeps_first_parent_order_and_changed_paths(self):
        repo = self.make_repo()
        base = commit(repo, "base", "README.md")
        first = commit(repo, "feat: first", "src/first.txt")
        second = commit(repo, "fix: second", "src/second.txt")

        commits = evidence.collect_mainline(repo, base, second)

        self.assertEqual([item.sha for item in commits], [first, second])
        self.assertEqual(commits[0].mainline_index, 0)
        self.assertEqual(commits[0].parents, (base,))
        self.assertEqual(
            evidence.changed_paths(repo, base, first), ("src/first.txt",)
        )

    def test_collect_mainline_rejects_base_outside_first_parent_chain(self):
        repo = self.make_repo()
        base = commit(repo, "base", "README.md")
        git(repo, "switch", "-c", "side")
        side = commit(repo, "side", "side.txt")
        git(repo, "switch", "main")
        commit(repo, "main", "main.txt")

        with self.assertRaisesRegex(ValueError, "first-parent"):
            evidence.collect_mainline(repo, side, "HEAD")

        self.assertNotEqual(base, side)

    def test_git_collection_never_requests_a_raw_diff(self):
        commands = []

        def runner(args):
            commands.append(tuple(args))
            if args[:2] == ["rev-parse", "HEAD"]:
                return "head"
            if args[:2] == ["rev-parse", "base"]:
                return "base"
            if args[:2] == ["rev-list", "--first-parent"]:
                return "head\nbase"
            if args and args[0] == "log":
                return "head\x1fbase\x1fsubject\x1fbody\x1e"
            raise AssertionError(f"unexpected git command: {args}")

        result = evidence.collect_mainline_with_runner(runner, "base", "HEAD")

        self.assertEqual(len(result), 1)
        self.assertFalse(any(command and command[0] == "diff" for command in commands))


class CuratedEvidenceTest(unittest.TestCase):
    def test_extract_unreleased_stops_at_the_first_version_heading(self):
        changelog = """# Changelog

## [Unreleased]

- Added bounded release planning.
- Fixed release retries.

## [0.49.0] - 2026-07-01

- Older item.
"""

        self.assertEqual(
            evidence.extract_unreleased(changelog),
            ("Added bounded release planning.", "Fixed release retries."),
        )

    def test_project_pr_body_keeps_product_sections_and_drops_checklists(self):
        body = """## Summary
Adds reliable release planning.

## Testing
- [x] Unit tests

## Root Cause
The raw diff exceeded the model context.

## Checklist
- [x] Docs
"""

        projected = evidence.project_pr_body(body)

        self.assertIn("## Summary", projected)
        self.assertIn("## Root Cause", projected)
        self.assertNotIn("## Testing", projected)
        self.assertNotIn("## Checklist", projected)


class PullRequestEvidenceTest(unittest.TestCase):
    def pr(
        self,
        number: int,
        *,
        base: str = "main",
        head: str = "feature",
        merge_sha: str = "sha-1",
    ) -> evidence.PullRequestRecord:
        return evidence.PullRequestRecord(
            number=number,
            title=f"PR {number}",
            body="## Summary\nUser-facing change.",
            url=f"https://example.test/pull/{number}",
            base_ref=base,
            head_ref=head,
            merge_sha=merge_sha,
            merged_at="2026-07-17T00:00:00Z",
        )

    def mainline(self):
        return (
            evidence.GitCommit(
                sha="sha-1",
                parents=("base",),
                subject="change",
                body="",
                mainline_index=0,
            ),
            evidence.GitCommit(
                sha="sha-2",
                parents=("sha-1",),
                subject="direct",
                body="",
                mainline_index=1,
            ),
        )

    def test_primary_pr_requires_release_branch_and_mainline_merge_sha(self):
        valid = self.pr(10)
        wrong_base = self.pr(11, base="develop")
        old = self.pr(12, merge_sha="old-sha")

        selection = evidence.select_primary_prs(
            self.mainline(),
            {"sha-1": (valid, wrong_base, old), "sha-2": ()},
            "main",
        )

        self.assertEqual([item.number for item in selection.primary], [10])
        self.assertEqual(selection.covered_shas, ("sha-1",))
        self.assertEqual(selection.direct_shas, ("sha-2",))

    def test_multiple_exact_primary_candidates_are_ambiguous(self):
        selection = evidence.select_primary_prs(
            self.mainline(),
            {"sha-1": (self.pr(10), self.pr(11)), "sha-2": ()},
            "main",
        )

        self.assertEqual(selection.primary, ())
        self.assertEqual(selection.direct_shas, ("sha-1", "sha-2"))
        self.assertEqual(selection.diagnostics[0].code, "ambiguous_primary_pr")

    def test_supporting_pr_requires_matching_base_and_parent_inventory(self):
        parent = self.pr(160, head="integration", merge_sha="sha-1")
        child = self.pr(
            161, base="integration", head="child", merge_sha="child-merge"
        )
        merely_related = self.pr(
            162, base="integration", head="other", merge_sha="missing"
        )
        client = evidence.StaticGitHubClient(
            associations={"child-merge": (child,), "missing": (merely_related,)},
            inventories={160: ("child-merge",)},
        )

        supporting, diagnostics = evidence.expand_supporting_prs(
            (parent,), client, primary_numbers={160}
        )

        self.assertEqual([item.number for item in supporting[160]], [161])
        self.assertEqual(diagnostics, ())

    def test_local_markers_are_only_stubs_when_github_is_unavailable(self):
        commits = (
            evidence.GitCommit(
                sha="one",
                parents=("base",),
                subject="feat: shipped (#42)",
                body="",
                mainline_index=0,
            ),
            evidence.GitCommit(
                sha="two",
                parents=("one", "side"),
                subject="Merge pull request #43 from owner/topic",
                body="",
                mainline_index=1,
            ),
        )

        stubs = evidence.local_pr_stubs(commits)

        self.assertEqual([item.number for item in stubs], [42, 43])
        self.assertTrue(all(item.confidence == "local_marker" for item in stubs))


if __name__ == "__main__":
    unittest.main()
