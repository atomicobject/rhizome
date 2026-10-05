#!/usr/bin/env python3
"""Behavior tests for the resumable release orchestrator."""

from __future__ import annotations

from contextlib import redirect_stderr
import io
from pathlib import Path
import runpy
import subprocess
import tempfile
import sys
import unittest
from unittest.mock import patch

from notes import GenerationResult
from release_theme import ReleaseTheme
from release_cli import OrchestrationError, Orchestrator
from release_types import Diagnostic, ReleaseEvidence


BASE = "v0.49.0"
BASE_SHA = "a" * 40
HEAD_SHA = "b" * 40
RELEASE_SHA = "c" * 40
MERGE_SHA = "e" * 40
VERSION = "v0.50.0"


def generation_result() -> GenerationResult:
    return GenerationResult(
        recommended_bump="minor",
        rationale="The range adds user-facing release orchestration.",
        themes=(ReleaseTheme("- **Review releases safely.** Plan from bounded evidence before mutation.", "Added bounded release planning.", ("unreleased:1",)),),
    )


class ParserFailureCLITest(unittest.TestCase):
    def test_parser_failure_reports_underlying_error_and_exits(self):
        from effort_evidence import CanonicalContractError

        stderr = io.StringIO()
        with patch("release_command.run_cli", side_effect=CanonicalContractError("go toolchain unavailable")):
            with redirect_stderr(stderr), self.assertRaises(SystemExit) as raised:
                runpy.run_path(str(Path(__file__).with_name("release_cli.py")), run_name="__main__")
        self.assertEqual(raised.exception.code, 1)
        self.assertEqual(stderr.getvalue(), "error: go toolchain unavailable\n")


class FakeCommands:
    def __init__(self) -> None:
        self.calls: list[tuple[str, ...]] = []
        self.head = HEAD_SHA
        self.base_sha = BASE_SHA
        self.status = ""
        self.tags: dict[str, str] = {}
        self.remote_tags: dict[str, str] = {}
        self.branch = "main"
        self.remote_main = HEAD_SHA
        self.remote_release = BASE_SHA
        self.planned_files: dict[str, str] = {}
        self.fail: dict[tuple[str, ...], str] = {}
        self.reject_atomic_push = False
        self.fail_release_ref_after_push = False

    def __call__(self, command, cwd):
        command = tuple(str(item) for item in command)
        self.calls.append(command)
        for prefix, message in self.fail.items():
            if command[: len(prefix)] == prefix:
                return subprocess.CompletedProcess(command, 1, "", message)
        output = ""
        if command[:2] == ("git", "describe"):
            output = BASE + "\n"
        elif command[:3] == ("git", "rev-parse", "HEAD"):
            output = self.head + "\n"
        elif command[:4] == ("git", "symbolic-ref", "--quiet", "--short"):
            output = self.branch + "\n"
        elif command[:3] == ("git", "rev-parse", "refs/remotes/origin/main"):
            output = self.remote_main + "\n"
        elif command[:3] == ("git", "rev-parse", "refs/remotes/origin/release"):
            output = self.remote_release + "\n"
        elif command[:3] == ("git", "rev-parse", BASE):
            output = self.base_sha + "\n"
        elif command[:3] == ("git", "status", "--porcelain"):
            output = self.status
        elif command[:2] == ("git", "show"):
            path = command[2].split(":", 1)[1]
            output = self.planned_files[path]
        elif command[:3] == ("git", "var", "GIT_AUTHOR_IDENT"):
            output = "Drew Colthorp <colthorp@atomicobject.com> 0 +0000\n"
        elif command[:4] == ("git", "log", "-1", "--pretty=%s"):
            output = f"Release {VERSION}\n" if self.head == RELEASE_SHA else "Feature work\n"
        elif command[:3] == ("git", "rev-parse", "HEAD^"):
            output = HEAD_SHA + "\n"
        elif command[:3] == ("git", "rev-parse", f"refs/tags/{VERSION}"):
            if VERSION not in self.tags:
                return subprocess.CompletedProcess(command, 1, "", "unknown revision")
            output = self.tags[VERSION] + "\n"
        elif command[:3] == ("git", "tag", "--list"):
            output = command[3] + "\n" if command[3] in self.tags else ""
        elif command[:2] == ("git", "commit"):
            self.head = RELEASE_SHA
        elif command[:2] == ("git", "tag"):
            self.tags[command[2]] = self.head
        elif command[:2] == ("git", "ls-remote"):
            requested_refs = command[3:]
            if (
                requested_refs == ("refs/heads/release",)
                and self.fail_release_ref_after_push
                and self.remote_release == RELEASE_SHA
            ):
                return subprocess.CompletedProcess(
                    command, 1, "", "network unavailable"
                )
            refs = {
                "refs/heads/main": self.remote_main,
                "refs/heads/release": self.remote_release,
                f"refs/tags/{VERSION}": self.remote_tags.get(VERSION, ""),
                f"refs/tags/{VERSION}^{{}}": "",
            }
            output = "".join(
                f"{refs[ref]}\t{ref}\n"
                for ref in requested_refs
                if refs.get(ref)
            )
        elif command[:3] == ("git", "merge-base", "--is-ancestor"):
            ancestor, descendant = command[3], command[4]
            known = ancestor == descendant or descendant == MERGE_SHA
            return subprocess.CompletedProcess(command, 0 if known else 1, "", "")
        elif command[:3] == ("git", "rev-list", "--first-parent"):
            if self.remote_main == MERGE_SHA:
                output = f"{MERGE_SHA} {'d' * 40} {RELEASE_SHA}\n"
        elif command[:3] == ("git", "merge-tree", "--write-tree"):
            output = "f" * 40 + "\n"
        elif command[:2] == ("git", "commit-tree"):
            output = MERGE_SHA + "\n"
        elif command[:3] == ("git", "push", "--atomic"):
            if self.reject_atomic_push:
                return subprocess.CompletedProcess(command, 1, "", "non-fast-forward")
            self.remote_main = command[-2].split(":", 1)[0]
            self.remote_release = command[-1].split(":", 1)[0]
        elif len(command) > 1 and command[1].endswith("release_publish.py"):
            self.remote_tags[VERSION] = self.head
        elif command[:1] == ("trash",):
            path = Path(command[1])
            path.rename(path.with_name(f"trashed-{path.name}"))
        return subprocess.CompletedProcess(command, 0, output, "")


class Fixture:
    def __init__(
        self, test: unittest.TestCase, *, degraded: bool = False, branch: str = "main"
    ) -> None:
        temp = tempfile.TemporaryDirectory()
        test.addCleanup(temp.cleanup)
        self.repo = Path(temp.name)
        self.commands = FakeCommands()
        self.commands.branch = branch
        if branch == "release":
            self.commands.remote_release = HEAD_SHA
        self.generated = 0
        diagnostics = (
            (Diagnostic("github_unavailable", "offline", severity="warning"),)
            if degraded
            else ()
        )
        self.evidence = ReleaseEvidence(
            base_tag=BASE,
            base_commit=BASE_SHA,
            head_commit=HEAD_SHA,
            release_branch=branch,
            unreleased_entries=("Bound release evidence.",),
            diagnostics=diagnostics,
            github_status="unavailable" if degraded else "available",
        )
        (self.repo / "CHANGELOG.md").write_text(
            "# Changelog\n\n## [Unreleased]\n\n- Old draft.\n", encoding="utf-8"
        )
        version = self.repo / "pkg/vault/version/version.go"
        embedded = self.repo / "pkg/vault/changelog/CHANGELOG.md"
        version.parent.mkdir(parents=True)
        embedded.parent.mkdir(parents=True)
        version.write_text("old\n", encoding="utf-8")
        embedded.write_text("old\n", encoding="utf-8")
        self.commands.planned_files = {
            path: (self.repo / path).read_text(encoding="utf-8")
            for path in (
                "CHANGELOG.md",
                "pkg/vault/version/version.go",
                "pkg/vault/changelog/CHANGELOG.md",
            )
        }
        self.inputs: list[str] = []
        self.output: list[str] = []
        self.orchestrator = Orchestrator(
            self.repo,
            command_runner=self.commands,
            evidence_collector=self.collect,
            note_generator=self.generate,
            input_fn=self.ask,
            output_fn=self.output.append,
            today_fn=lambda: "2026-07-17",
            environ={"RZM_RELEASE_GITHUB_TOKEN": "publish-secret"},
        )

    def collect(self, repo, base, head, **kwargs):
        return self.evidence

    def generate(self, evidence, **kwargs):
        self.generated += 1
        return generation_result()

    def ask(self, prompt: str) -> str:
        if not self.inputs:
            raise AssertionError(f"unexpected prompt: {prompt}")
        return self.inputs.pop(0)

    def plan(self, **kwargs):
        return self.orchestrator.plan(base=BASE, version=VERSION, **kwargs)


class OrchestratorTest(unittest.TestCase):
    def test_plan_requires_fresh_exact_canonical_source(self) -> None:
        fixture = Fixture(self)
        fixture.commands.branch = "feature"
        with self.assertRaisesRegex(OrchestrationError, "main or release"):
            fixture.plan()

        fixture.commands.branch = "main"
        fixture.commands.remote_main = "d" * 40
        with self.assertRaisesRegex(OrchestrationError, "latest origin/main"):
            fixture.plan()

    def test_plan_previews_release_notes_and_rationale_before_version_menu(self) -> None:
        fixture = Fixture(self)
        fixture.inputs = [""]

        run = fixture.orchestrator.plan(base=BASE)

        preview = fixture.output.index("=== Release Notes Preview ===")
        notes = fixture.output.index(run.plan.release_notes)
        rationale = fixture.output.index(
            "Recommendation rationale (minor): " + generation_result().rationale
        )
        menu = fixture.output.index("Version options:")
        self.assertLess(preview, notes)
        self.assertLess(notes, rationale)
        self.assertLess(rationale, menu)
        self.assertEqual(run.plan.selected_version, VERSION)

    def test_plan_persists_reviewed_luna_medium_plan_and_initial_state(self) -> None:
        fixture = Fixture(self)

        run = fixture.plan(guidance="Prefer user-visible outcomes.")

        self.assertEqual(run.plan.model, "gpt-5.6-luna")
        self.assertEqual(run.plan.reasoning_effort, "medium")
        self.assertEqual(run.plan.selected_version, VERSION)
        self.assertEqual(run.plan.source_attributions, ("unreleased:1",))
        self.assertEqual(len(run.plan.themes), 1)
        self.assertEqual(run.plan.themes[0].changelog_entry, "Added bounded release planning.")
        self.assertEqual(run.plan.themes[0].source_ids, ("unreleased:1",))
        self.assertIn("Review releases safely", run.plan.release_notes)
        self.assertEqual(
            run.state.completed_checkpoints,
            ("evidence_collected", "notes_generated", "plan_reviewed", "version_selected"),
        )
        self.assertEqual(fixture.generated, 1)
        self.assertTrue((run.run_dir / "evidence.json").exists())
        self.assertTrue((run.run_dir / "plan.json").exists())
        self.assertTrue((run.run_dir / "state.json").exists())

    def test_plan_reuses_identical_reviewed_artifacts_without_regeneration(self) -> None:
        fixture = Fixture(self)
        first = fixture.plan()

        second = fixture.plan()

        self.assertEqual(second.plan, first.plan)
        self.assertEqual(fixture.generated, 1)

    def test_plan_does_not_reuse_same_commit_plan_from_other_canonical_branch(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.commands.branch = "release"
        fixture.commands.remote_release = HEAD_SHA

        with self.assertRaisesRegex(OrchestrationError, "different canonical branch"):
            fixture.plan()

    def test_plan_degrades_when_an_optional_github_remote_is_unavailable(self) -> None:
        fixture = Fixture(self)
        fixture.commands.fail[("git", "remote", "get-url", "origin")] = "no remote"

        with patch("release_cli.shutil.which", return_value="/usr/local/bin/gh"):
            run = fixture.plan()

        self.assertEqual(run.plan.selected_version, VERSION)

    def test_new_plan_explicitly_regenerates(self) -> None:
        fixture = Fixture(self)
        fixture.plan()

        fixture.plan(new_plan=True)

        self.assertEqual(fixture.generated, 2)

    def test_build_revalidates_and_checkpoints_candidate(self) -> None:
        fixture = Fixture(self)
        run = fixture.plan()

        built = fixture.orchestrator.build(BASE, HEAD_SHA)

        self.assertIn(
            (
                sys.executable, "scripts/release/run_clean.py", "build", "--", "goreleaser", "release", "--snapshot", "--clean", "--release-notes",
                str(run.run_dir / "release-notes.md"),
            ),
            fixture.commands.calls,
        )
        self.assertIn("built", built.state.completed_checkpoints)

    def test_build_rejects_stale_head_before_goreleaser_mutation(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.commands.head = "d" * 40

        with self.assertRaisesRegex(OrchestrationError, "HEAD changed"):
            fixture.orchestrator.build(BASE, HEAD_SHA)

        self.assertFalse(any("goreleaser" in call for call in fixture.commands.calls))

    def test_build_rejects_dirty_worktree(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.commands.status = " M unrelated.txt\n"

        with self.assertRaisesRegex(OrchestrationError, "worktree"):
            fixture.orchestrator.build(BASE, HEAD_SHA)

    def test_apply_requires_built_and_explicit_degraded_acceptance(self) -> None:
        fixture = Fixture(self, degraded=True)
        fixture.plan()

        with self.assertRaisesRegex(OrchestrationError, "candidate build"):
            fixture.orchestrator.apply(BASE, HEAD_SHA, accept_degraded_evidence=True)

        fixture.orchestrator.build(BASE, HEAD_SHA)
        with self.assertRaisesRegex(OrchestrationError, "degraded evidence"):
            fixture.orchestrator.apply(BASE, HEAD_SHA, interactive=False)
        self.assertEqual(fixture.commands.head, HEAD_SHA)

    def test_apply_changes_three_files_commits_tags_and_checkpoints(self) -> None:
        fixture = Fixture(self, degraded=True)
        run = fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)

        applied = fixture.orchestrator.apply(
            BASE, HEAD_SHA, accept_degraded_evidence=True
        )

        self.assertEqual(fixture.commands.head, RELEASE_SHA)
        self.assertEqual(fixture.commands.tags, {VERSION: RELEASE_SHA})
        self.assertIn(("git", "commit", "-m", f"Release {VERSION}"), fixture.commands.calls)
        self.assertIn("applied", applied.state.completed_checkpoints)
        changelog = (fixture.repo / "CHANGELOG.md").read_text(encoding="utf-8")
        self.assertIn(f"## [{VERSION}] - 2026-07-17", changelog)
        self.assertEqual(
            (fixture.repo / "pkg/vault/changelog/CHANGELOG.md").read_text(encoding="utf-8"),
            changelog,
        )
        self.assertEqual(
            (run.run_dir / "release-notes.md").read_text(encoding="utf-8"),
            run.plan.release_notes + "\n",
        )

    def test_apply_requires_release_scoped_github_token_before_mutation(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.orchestrator.environ = {}

        with self.assertRaisesRegex(OrchestrationError, "RZM_RELEASE_GITHUB_TOKEN"):
            fixture.orchestrator.apply(BASE, HEAD_SHA)

        self.assertEqual(fixture.commands.head, HEAD_SHA)
        self.assertFalse(any(call[:2] == ("git", "commit") for call in fixture.commands.calls))

    def test_first_apply_rejects_origin_advance_after_candidate_build(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.commands.remote_main = "d" * 40

        with self.assertRaisesRegex(OrchestrationError, "latest origin/main"):
            fixture.orchestrator.apply(BASE, HEAD_SHA)

        self.assertEqual(fixture.commands.head, HEAD_SHA)
        self.assertFalse(any(call[:2] == ("git", "commit") for call in fixture.commands.calls))

    def test_apply_resume_recovers_commit_and_tag_before_lagging_checkpoint(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.commands.head = RELEASE_SHA
        fixture.commands.tags[VERSION] = RELEASE_SHA

        resumed = fixture.orchestrator.apply(BASE, HEAD_SHA)

        self.assertIn("applied", resumed.state.completed_checkpoints)
        self.assertFalse(any(call[:2] == ("git", "commit") for call in fixture.commands.calls))

    def test_apply_resume_allows_only_owned_partial_file_changes(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        run = fixture.orchestrator.load(BASE, HEAD_SHA)
        state = fixture.orchestrator._state(
            run.plan, run.state.completed_checkpoints, "applying"
        )
        fixture.orchestrator.store.update_state(run.plan, state)
        fixture.commands.status = " M CHANGELOG.md\n"

        resumed = fixture.orchestrator.apply(BASE, HEAD_SHA)

        self.assertIn("applied", resumed.state.completed_checkpoints)
        self.assertEqual(fixture.commands.head, RELEASE_SHA)

    def test_apply_rejects_moved_base_tag_before_mutation(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.commands.base_sha = "d" * 40

        with self.assertRaisesRegex(ValueError, "base tag.*moved"):
            fixture.orchestrator.apply(BASE, HEAD_SHA)

        self.assertFalse(any(call[:2] == ("git", "commit") for call in fixture.commands.calls))

    def test_first_apply_rejects_dirty_release_owned_file(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.commands.status = " M CHANGELOG.md\n"

        with self.assertRaisesRegex(ValueError, "clean before first"):
            fixture.orchestrator.apply(BASE, HEAD_SHA)

    def test_apply_retry_rejects_arbitrary_release_owned_content(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        run = fixture.orchestrator.load(BASE, HEAD_SHA)
        state = fixture.orchestrator._state(
            run.plan, run.state.completed_checkpoints, "applying"
        )
        fixture.orchestrator.store.update_state(run.plan, state)
        fixture.commands.status = " M CHANGELOG.md\n"
        (fixture.repo / "CHANGELOG.md").write_text("arbitrary\n", encoding="utf-8")

        with self.assertRaisesRegex(ValueError, "arbitrary retry content"):
            fixture.orchestrator.apply(BASE, HEAD_SHA)

    def test_apply_retry_reuses_persisted_release_date_across_midnight(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        run = fixture.orchestrator.load(BASE, HEAD_SHA)
        state = fixture.orchestrator._state(
            run.plan, run.state.completed_checkpoints, "applying", "2026-07-16"
        )
        fixture.orchestrator.store.update_state(run.plan, state)
        fixture.orchestrator.today_fn = lambda: "2026-07-17"

        resumed = fixture.orchestrator.apply(BASE, HEAD_SHA)

        self.assertEqual(resumed.state.release_date, "2026-07-16")
        self.assertIn("2026-07-16", (fixture.repo / "CHANGELOG.md").read_text(encoding="utf-8"))

    def test_publish_completes_after_github_release(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.orchestrator.apply(BASE, HEAD_SHA)
        published = fixture.orchestrator.publish(BASE, HEAD_SHA)

        github_calls = [call for call in fixture.commands.calls if len(call) > 1 and call[1].endswith("release_publish.py")]
        self.assertEqual(len(github_calls), 1)
        self.assertIn("github_published", published.state.completed_checkpoints)
        self.assertIn("published", published.state.completed_checkpoints)
        self.assertFalse(fixture.orchestrator.store.run_dir(BASE, HEAD_SHA).exists())
        self.assertFalse(any(call[:2] == ("make", "release-s3") or call[:2] == ("make", "release-s3-check") for call in fixture.commands.calls))

    def test_internal_release_checks_s3_before_apply_and_resumes_only_s3(self) -> None:
        fixture = Fixture(self)
        fixture.orchestrator.environ = {**fixture.orchestrator.environ, "RZM_INTERNAL_S3_BUCKET": "example-bucket"}
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.orchestrator.apply(BASE, HEAD_SHA)
        check = fixture.commands.calls.index(("make", "release-s3-check"))
        self.assertLess(fixture.commands.calls.index((sys.executable, "scripts/teamkeys/release.py", "--check")), check + 2)
        commit = next(i for i, call in enumerate(fixture.commands.calls) if call[:2] == ("git", "commit"))
        self.assertLess(check, commit)
        fixture.commands.fail[("make", "release-s3")] = "S3 unavailable"

        with self.assertRaisesRegex(OrchestrationError, "S3 unavailable") as raised:
            fixture.orchestrator.publish(BASE, HEAD_SHA)

        self.assertIn("resume", raised.exception.next_command)
        self.assertIn("github_published", fixture.orchestrator.load(BASE, HEAD_SHA).state.completed_checkpoints)
        fixture.commands.fail.clear()
        resumed = fixture.orchestrator.resume(BASE, HEAD_SHA)

        github_calls = [call for call in fixture.commands.calls if len(call) > 1 and call[1].endswith("release_publish.py")]
        self.assertEqual(len(github_calls), 1)
        self.assertIn("s3_published", resumed.state.completed_checkpoints)

    def test_resume_refuses_to_switch_release_mode(self) -> None:
        for applied_internal in (True, False):
            with self.subTest(applied_internal=applied_internal):
                fixture = Fixture(self)
                internal = {**fixture.orchestrator.environ, "RZM_INTERNAL_S3_BUCKET": "example-bucket"}
                public = dict(fixture.orchestrator.environ)
                fixture.orchestrator.environ = internal if applied_internal else public
                fixture.plan()
                fixture.orchestrator.build(BASE, HEAD_SHA)
                fixture.orchestrator.apply(BASE, HEAD_SHA)
                fixture.orchestrator.environ = public if applied_internal else internal

                with self.assertRaisesRegex(OrchestrationError, "applied as"):
                    fixture.orchestrator.publish(BASE, HEAD_SHA)

                self.assertFalse(any(len(call) > 1 and call[1].endswith("release_publish.py") for call in fixture.commands.calls))

    def test_resume_refuses_a_different_mirror_bucket(self) -> None:
        fixture = Fixture(self)
        fixture.orchestrator.environ = {**fixture.orchestrator.environ, "RZM_INTERNAL_S3_BUCKET": "example-bucket"}
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.orchestrator.apply(BASE, HEAD_SHA)
        fixture.orchestrator.environ = {**fixture.orchestrator.environ, "RZM_INTERNAL_S3_BUCKET": "other-bucket"}

        with self.assertRaisesRegex(OrchestrationError, "differs from the bucket"):
            fixture.orchestrator.publish(BASE, HEAD_SHA)

        self.assertFalse(any(len(call) > 1 and call[1].endswith("release_publish.py") for call in fixture.commands.calls))

    def test_release_hotfix_publishes_main_first_merge_before_github(self) -> None:
        fixture = Fixture(self, branch="release")
        fixture.commands.remote_main = "d" * 40
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.orchestrator.apply(BASE, HEAD_SHA)

        published = fixture.orchestrator.publish(BASE, HEAD_SHA)

        push = next(call for call in fixture.commands.calls if call[:3] == ("git", "push", "--atomic"))
        github = next(call for call in fixture.commands.calls if len(call) > 1 and call[1].endswith("release_publish.py"))
        self.assertLess(fixture.commands.calls.index(push), fixture.commands.calls.index(github))
        self.assertEqual(fixture.commands.remote_main, MERGE_SHA)
        self.assertEqual(fixture.commands.remote_release, RELEASE_SHA)
        self.assertIn("published", published.state.completed_checkpoints)

    def test_publish_rejects_moved_local_tag(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.orchestrator.apply(BASE, HEAD_SHA)
        fixture.commands.tags[VERSION] = "d" * 40

        with self.assertRaisesRegex(OrchestrationError, "local release commit or tag"):
            fixture.orchestrator.publish(BASE, HEAD_SHA)

        self.assertFalse(any(call[:3] == ("git", "push", "--atomic") for call in fixture.commands.calls))

    def test_publish_fails_closed_when_github_tag_exists_without_checkpoint(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.orchestrator.apply(BASE, HEAD_SHA)
        fixture.commands.remote_main = RELEASE_SHA
        fixture.commands.remote_release = RELEASE_SHA
        fixture.commands.remote_tags[VERSION] = RELEASE_SHA

        with self.assertRaisesRegex(OrchestrationError, "without a GitHub publication checkpoint"):
            fixture.orchestrator.publish(BASE, HEAD_SHA)

        self.assertFalse(
            any(call for call in fixture.commands.calls if len(call) > 1 and call[1].endswith("release_publish.py"))
        )

    def test_conflicting_remote_tag_is_rejected_before_canonical_branches_move(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.orchestrator.apply(BASE, HEAD_SHA)
        original_main = fixture.commands.remote_main
        original_release = fixture.commands.remote_release
        fixture.commands.remote_tags[VERSION] = "d" * 40

        with self.assertRaisesRegex(OrchestrationError, "does not resolve"):
            fixture.orchestrator.publish(BASE, HEAD_SHA)

        self.assertEqual(fixture.commands.remote_main, original_main)
        self.assertEqual(fixture.commands.remote_release, original_release)
        self.assertFalse(any(call[:3] == ("git", "push", "--atomic") for call in fixture.commands.calls))

    def test_atomic_push_rejection_reports_safe_resume_command(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.orchestrator.apply(BASE, HEAD_SHA)
        fixture.commands.reject_atomic_push = True

        with self.assertRaisesRegex(OrchestrationError, "atomically publish") as raised:
            fixture.orchestrator.publish(BASE, HEAD_SHA)

        self.assertIn(" resume ", raised.exception.next_command)

    def test_post_publication_verification_failure_reports_safe_resume_command(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.orchestrator.build(BASE, HEAD_SHA)
        fixture.orchestrator.apply(BASE, HEAD_SHA)
        fixture.commands.fail_release_ref_after_push = True

        with self.assertRaisesRegex(OrchestrationError, "network unavailable") as raised:
            fixture.orchestrator.publish(BASE, HEAD_SHA)

        self.assertIn(" resume ", raised.exception.next_command)
        self.assertEqual(fixture.commands.remote_main, RELEASE_SHA)
        self.assertEqual(fixture.commands.remote_release, RELEASE_SHA)

    def test_dry_run_builds_candidate_then_trashes_exact_run_without_apply(self) -> None:
        fixture = Fixture(self)

        run_dir = fixture.orchestrator.dry_run(base=BASE, version=VERSION)

        self.assertFalse(run_dir.exists())
        self.assertEqual(fixture.commands.head, HEAD_SHA)
        self.assertEqual(fixture.commands.tags, {})
        self.assertFalse(any(call[:2] == ("git", "commit") for call in fixture.commands.calls))
        self.assertIn(("trash", str(run_dir)), fixture.commands.calls)

    def test_failure_reports_exact_safe_resume_command(self) -> None:
        fixture = Fixture(self)
        fixture.plan()
        fixture.commands.fail[(sys.executable, "scripts/release/run_clean.py", "build", "--", "goreleaser", "release", "--snapshot")] = "build failed"

        with self.assertRaisesRegex(OrchestrationError, "build failed") as raised:
            fixture.orchestrator.build(BASE, HEAD_SHA)

        self.assertEqual(
            raised.exception.next_command,
            f"python3 scripts/release/release_cli.py resume --base {BASE} --head {HEAD_SHA}",
        )

    def test_dry_run_failure_never_recommends_publish_resume(self) -> None:
        fixture = Fixture(self)
        fixture.commands.fail[(sys.executable, "scripts/release/run_clean.py", "build", "--", "goreleaser", "release", "--snapshot")] = "build failed"

        with self.assertRaisesRegex(OrchestrationError, "build failed") as raised:
            fixture.orchestrator.dry_run(base=BASE, version=VERSION)

        self.assertIn("dry-run", raised.exception.next_command)
        self.assertNotIn(" resume ", raised.exception.next_command)


if __name__ == "__main__":
    unittest.main()
