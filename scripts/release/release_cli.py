#!/usr/bin/env python3
"""Reviewable, resumable release orchestration for Rhizome maintainers."""

from __future__ import annotations

from dataclasses import replace
from datetime import date
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
from typing import Callable, Mapping, Sequence

from collect_evidence import collect_release_evidence
from effort_evidence import CanonicalContractError
from evidence import GhGitHubClient
from notes import (
    DEFAULT_MODEL,
    DEFAULT_REASONING_EFFORT,
    GenerationError,
    GenerationResult,
    generate_release_notes,
)
from release_actions import RELEASE_FILE_PATHS, apply_release_files
from release_command import run_cli
from release_git import ReleaseGit, ReleaseGitError
from release_preconditions import ReleasePreconditionError, validate_stored_apply_boundary, verify_base_tag
from release_publish import PUBLISH_TOKEN_ENV, publish_command
from release_store import ReleaseStore, ReleaseStoreError, StoredReleaseRun
from release_theme import merge_curated_themes
from release_types import ReleaseEvidence, ReleasePlan, ReleaseRunState
from release_version import choose_release_version, present_release_preview


CommandRunner = Callable[[Sequence[str], Path], subprocess.CompletedProcess[str]]
EvidenceCollector = Callable[..., ReleaseEvidence]
NoteGenerator = Callable[..., GenerationResult]


class OrchestrationError(RuntimeError):
    """A release phase failed without discarding its resumable state."""

    def __init__(self, message: str, *, next_command: str = "") -> None:
        super().__init__(message)
        self.next_command = next_command


def _run_command(command: Sequence[str], cwd: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(command, cwd=cwd, capture_output=True, text=True)


def _repo_slug(remote: str) -> str:
    match = re.search(r"(?:github\.com[:/])([^/]+/[^/]+?)(?:\.git)?$", remote.strip())
    return match.group(1) if match else ""


class Orchestrator:
    """Own phase ordering while delegating collection, generation, and storage."""

    def __init__(
        self,
        repo_root: Path,
        *,
        command_runner: CommandRunner = _run_command,
        evidence_collector: EvidenceCollector = collect_release_evidence,
        note_generator: NoteGenerator = generate_release_notes,
        input_fn: Callable[[str], str] = input,
        output_fn: Callable[[str], None] = print,
        today_fn: Callable[[], str] = lambda: date.today().isoformat(),
        environ: Mapping[str, str] | None = None,
    ) -> None:
        self.repo_root = Path(repo_root).resolve()
        self.command_runner = command_runner
        self.evidence_collector = evidence_collector
        self.note_generator = note_generator
        self.input_fn = input_fn
        self.output_fn = output_fn
        self.today_fn = today_fn
        self.environ = os.environ if environ is None else environ
        self.store = ReleaseStore(
            self.repo_root,
            command_runner=lambda command: self._run(command),
        )
        self.release_git = ReleaseGit(self.repo_root, command_runner=self.command_runner)

    def _preflight_source(self) -> tuple[str, str]:
        try:
            return self.release_git.preflight_source()
        except ReleaseGitError as error:
            raise OrchestrationError(str(error)) from error

    def _run(
        self,
        command: Sequence[str],
        *,
        next_command: str = "",
    ) -> subprocess.CompletedProcess[str]:
        result = self.command_runner(tuple(command), self.repo_root)
        if result.returncode:
            detail = result.stderr.strip() or result.stdout.strip() or "command failed"
            raise OrchestrationError(detail, next_command=next_command)
        return result

    def _git(self, *args: str) -> str:
        return self._run(("git", *args)).stdout.strip()

    def _git_file(self, commit: str, path: str) -> str:
        return self._run(("git", "show", f"{commit}:{path}")).stdout

    def _head(self) -> str:
        return self._git("rev-parse", "HEAD")

    def _status(self) -> str:
        return self._run(("git", "status", "--porcelain")).stdout.rstrip("\n")

    def _base(self) -> str:
        base = self.environ.get("PREV_TAG", "").strip()
        return base or self._git("describe", "--tags", "--abbrev=0")

    def _github_client(self) -> GhGitHubClient | None:
        if shutil.which("gh") is None:
            return None
        try:
            remote = self._git("remote", "get-url", "origin")
        except OrchestrationError:
            return None
        slug = _repo_slug(remote)
        return GhGitHubClient(slug) if slug else None

    def _collect(self, base: str, head: str) -> ReleaseEvidence:
        return self.evidence_collector(
            self.repo_root,
            base,
            head,
            github_client=self._github_client(),
        )

    @staticmethod
    def _state(
        plan: ReleasePlan,
        checkpoints: Sequence[str],
        phase: str,
        release_date: str | None = None,
    ) -> ReleaseRunState:
        return ReleaseRunState(
            plan_fingerprint=plan.plan_fingerprint,
            phase=phase,
            completed_checkpoints=tuple(checkpoints),
            release_date=release_date,
        )

    def _checkpoint(
        self, run: StoredReleaseRun, checkpoint: str, phase: str
    ) -> StoredReleaseRun:
        checkpoints = run.state.completed_checkpoints
        if checkpoint not in checkpoints:
            checkpoints = (*checkpoints, checkpoint)
        state = self._state(run.plan, checkpoints, phase, run.state.release_date)
        self.store.update_state(run.plan, state)
        return replace(run, state=state)

    def _write_notes(self, run: StoredReleaseRun) -> Path:
        path = run.run_dir / "release-notes.md"
        path.write_text(run.plan.release_notes.rstrip() + "\n", encoding="utf-8")
        return path

    def _revalidate_unmutated(self, run: StoredReleaseRun) -> None:
        verify_base_tag(run.plan.base_tag, run.plan.base_commit, lambda ref: self._git("rev-parse", ref))
        if self._head() != run.plan.head_commit:
            raise OrchestrationError("HEAD changed since the release plan was reviewed")
        if self._status():
            raise OrchestrationError("worktree must be clean before release mutation")
        if run.plan.evidence_fingerprint != run.evidence.fingerprint:
            raise OrchestrationError("release evidence fingerprint no longer matches the plan")

    def _revalidate_apply(
        self, run: StoredReleaseRun, version: str, release_date: str
    ) -> None:
        validate_stored_apply_boundary(
            self.repo_root, run=run, version=version, release_date=release_date,
            current_head=self._head(), status=self._status(),
            resolve=lambda ref: self._git("rev-parse", ref),
            read_planned=self._git_file,
        )

    def _ensure_tag(self, version: str, retry: str) -> None:
        current_head = self._head()
        if self._git("tag", "--list", version):
            target = self._git("rev-parse", f"refs/tags/{version}")
            if target != current_head:
                raise OrchestrationError(
                    f"release tag {version} points at a different commit",
                    next_command=retry,
                )
            return
        self._run(("git", "tag", version), next_command=retry)

    def _recover_applied(
        self, run: StoredReleaseRun, version: str, retry: str
    ) -> StoredReleaseRun | None:
        """Recognize a release commit made just before its checkpoint was persisted."""

        if self._head() == run.plan.head_commit:
            return None
        subject = self._git("log", "-1", "--pretty=%s")
        parent = self._git("rev-parse", "HEAD^")
        if (
            subject != f"Release {version}"
            or parent != run.plan.head_commit
            or self._status()
        ):
            raise OrchestrationError(
                "HEAD changed since the release plan was reviewed",
                next_command=retry,
            )
        self._ensure_tag(version, retry)
        return self._checkpoint(run, "applied", "applied")

    def load(self, base: str, head: str) -> StoredReleaseRun:
        return self.store.load_run(base, head)

    def plan(
        self,
        *,
        base: str | None = None,
        head: str = "HEAD",
        version: str | None = None,
        guidance: str = "",
        new_plan: bool = False,
    ) -> StoredReleaseRun:
        source_branch, source_head = self._preflight_source()
        base = base or self._base()
        guidance = guidance or self.environ.get("FEEDBACK", "").strip()
        head_sha = self._git("rev-parse", head)
        if head_sha != source_head:
            raise OrchestrationError("release plan head must equal the canonical source HEAD")
        run_dir = self.store.run_dir(base, head_sha)
        if run_dir.exists() and not new_plan:
            run = self.store.load_run(base, head_sha)
            if run.evidence.release_branch != source_branch:
                raise OrchestrationError(
                    "reviewed release plan belongs to a different canonical branch; "
                    "use --new-plan"
                )
            present_release_preview(
                run.plan.release_notes,
                run.plan.rationale,
                run.plan.recommended_bump,
                output_fn=self.output_fn,
            )
            return run

        evidence = self._collect(base, head_sha)
        if evidence.release_branch != source_branch:
            raise OrchestrationError("release evidence source branch changed during planning")
        generated = self.note_generator(
            evidence,
            repo_root=self.repo_root,
            extra_guidance=guidance,
            environ=self.environ,
        )
        themes = merge_curated_themes(evidence.unreleased_entries, generated.themes)
        release_notes = "\n\n".join(theme.release_note for theme in themes)
        present_release_preview(
            release_notes,
            generated.rationale,
            generated.recommended_bump,
            output_fn=self.output_fn,
        )
        selected = choose_release_version(
            base,
            generated.recommended_bump,
            supplied=version,
            environ=self.environ,
            input_fn=self.input_fn,
            output_fn=self.output_fn,
        )
        plan = ReleasePlan(
            base_tag=evidence.base_tag,
            base_commit=evidence.base_commit,
            head_commit=evidence.head_commit,
            evidence_fingerprint=evidence.fingerprint,
            model=generated.model or DEFAULT_MODEL,
            reasoning_effort=generated.reasoning_effort or DEFAULT_REASONING_EFFORT,
            generation_guidance=guidance,
            recommended_bump=generated.recommended_bump,
            rationale=generated.rationale,
            themes=themes,
            selected_version=selected,
            diagnostics=generated.diagnostics,
        )
        state = self._state(
            plan,
            ("evidence_collected", "notes_generated", "plan_reviewed", "version_selected"),
            "planned",
        )
        run = self.store.write_run(evidence, plan, state, new_plan=new_plan)
        self._write_notes(run)
        return run

    def build(
        self, base: str, head: str, *, retry_command: str | None = None
    ) -> StoredReleaseRun:
        run = self.load(base, head)
        if "built" in run.state.completed_checkpoints:
            return run
        self._revalidate_unmutated(run)
        notes_path = self._write_notes(run)
        retry = self._resume_command(base, head) if retry_command is None else retry_command
        self._run(
            (sys.executable, "scripts/release/run_clean.py", "build", "--", "goreleaser", "release", "--snapshot", "--clean", "--release-notes", str(notes_path)),
            next_command=retry,
        )
        return self._checkpoint(run, "built", "built")

    def apply(
        self,
        base: str,
        head: str,
        *,
        accept_degraded_evidence: bool = False,
        interactive: bool = True,
    ) -> StoredReleaseRun:
        run = self.load(base, head)
        if "applied" in run.state.completed_checkpoints:
            return run
        if "built" not in run.state.completed_checkpoints:
            raise OrchestrationError("candidate build must succeed before apply")
        if not self.environ.get(PUBLISH_TOKEN_ENV, "").strip():
            raise OrchestrationError(f"set {PUBLISH_TOKEN_ENV} before applying a release")
        if run.evidence.github_status != "available" and not accept_degraded_evidence:
            accepted = interactive and self.input_fn(
                "GitHub evidence is degraded. Apply anyway? [y/N]: "
            ).strip().lower() in {"y", "yes"}
            if not accepted:
                raise OrchestrationError(
                    "degraded evidence requires --accept-degraded-evidence"
                )
        retry = self._resume_command(base, head)
        version = run.plan.selected_version
        if version is None:
            raise OrchestrationError("reviewed release plan has no selected version")
        verify_base_tag(run.plan.base_tag, run.plan.base_commit, lambda ref: self._git("rev-parse", ref))
        recovered = self._recover_applied(run, version, retry)
        if recovered is not None:
            return recovered
        source_branch, source_head = self._preflight_source()
        if source_branch != run.evidence.release_branch or source_head != run.plan.head_commit:
            raise OrchestrationError(
                "release source no longer matches the reviewed canonical branch",
                next_command=retry,
            )
        release_date = run.state.release_date or self.today_fn()
        self._revalidate_apply(run, version, release_date)
        self._run(("git", "var", "GIT_AUTHOR_IDENT"), next_command=retry)
        self._verify_release_mode(run, retry)
        self._run((sys.executable, "scripts/teamkeys/release.py", "--check"), next_command=retry)
        if self._internal_release():
            self._run(("make", "release-s3-check"), next_command=retry)
            self._mirror_marker(run).write_text(self._mirror_fingerprint())
            run = self._checkpoint(run, "internal_release", "applying")
        self._revalidate_apply(run, version, release_date)
        run = replace(run, state=self._state(
            run.plan, run.state.completed_checkpoints, "applying", release_date
        ))
        self.store.update_state(run.plan, run.state)
        apply_release_files(
            self.repo_root,
            version=version,
            release_date=release_date,
            changelog_entries=run.plan.changelog_entries,
        )
        self._run(("git", "add", *(path.as_posix() for path in RELEASE_FILE_PATHS)), next_command=retry)
        self._run(("git", "commit", "-m", f"Release {version}"), next_command=retry)
        self._ensure_tag(version, retry)
        return self._checkpoint(run, "applied", "applied")

    def _internal_release(self) -> bool:
        """Internal releases also publish to the private S3 mirror named in the release Environment."""
        return bool(self.environ.get("RZM_INTERNAL_S3_BUCKET", "").strip())

    @staticmethod
    def _mirror_marker(run: StoredReleaseRun) -> Path:
        return run.run_dir / "internal-mirror.sha256"

    def _mirror_fingerprint(self) -> str:
        """Identify the mirror bucket without storing its name in release state."""
        bucket = self.environ.get("RZM_INTERNAL_S3_BUCKET", "").strip()
        return hashlib.sha256(bucket.encode()).hexdigest()

    def _verify_release_mode(self, run: StoredReleaseRun, retry: str) -> None:
        """Keep a release internal or public, and on one mirror, across retries."""
        recorded = "internal_release" in run.state.completed_checkpoints
        if recorded and not self._internal_release():
            raise OrchestrationError(
                "this release was applied as internal; resume through ./scripts/with-secrets release",
                next_command=retry,
            )
        marker = self._mirror_marker(run)
        if recorded and (not marker.is_file() or marker.read_text() != self._mirror_fingerprint()):
            raise OrchestrationError(
                "RZM_INTERNAL_S3_BUCKET differs from the bucket this release was built for; "
                "resume with the original release Environment",
                next_command=retry,
            )
        if not recorded and self._internal_release() and "applied" in run.state.completed_checkpoints:
            raise OrchestrationError(
                "this release was applied as public; unset RZM_INTERNAL_S3_BUCKET before resuming",
                next_command=retry,
            )

    def publish(self, base: str, head: str) -> StoredReleaseRun:
        run = self.load(base, head)
        if "applied" not in run.state.completed_checkpoints:
            raise OrchestrationError("release must be applied before publish")
        retry = self._resume_command(base, head)
        self._verify_release_mode(run, retry)
        notes_path = self._write_notes(run)
        version = run.plan.selected_version
        if version is None:
            raise OrchestrationError("reviewed release plan has no selected version")
        try:
            release_commit = self.release_git.validate_local_release_commit(
                version, run.plan.head_commit
            )
        except ReleaseGitError as error:
            raise OrchestrationError(str(error), next_command=retry) from error
        if "github_published" not in run.state.completed_checkpoints:
            try:
                if self.release_git.remote_tag_target(version) is not None:
                    self.release_git.verify_remote_tag(version, release_commit)
                    raise OrchestrationError(
                        "remote tag exists without a GitHub publication checkpoint; "
                        "inspect the GitHub release before retrying",
                        next_command=retry,
                    )
            except ReleaseGitError as error:
                raise OrchestrationError(str(error), next_command=retry) from error
        if "canonical_branches_published" not in run.state.completed_checkpoints:
            try:
                targets = self.release_git.branch_targets(
                    run.evidence.release_branch, release_commit, version
                )
                self.release_git.publish_canonical_branches(targets)
            except ReleaseGitError as error:
                raise OrchestrationError(str(error), next_command=retry) from error
            run = self._checkpoint(
                run, "canonical_branches_published", "publishing"
            )
        try:
            publication_complete = self.release_git.publication_complete(
                release_commit, run.evidence.release_branch
            )
        except ReleaseGitError as error:
            raise OrchestrationError(str(error), next_command=retry) from error
        if not publication_complete:
            raise OrchestrationError(
                "canonical release branches no longer match the release commit",
                next_command=retry,
            )
        if "github_published" not in run.state.completed_checkpoints:
            self._run(publish_command(self.repo_root, notes_path), next_command=retry)
            try:
                self.release_git.verify_remote_tag(version, release_commit)
            except ReleaseGitError as error:
                raise OrchestrationError(str(error), next_command=retry) from error
            run = self._checkpoint(run, "github_published", "publishing")
        else:
            try:
                self.release_git.verify_remote_tag(version, release_commit)
            except ReleaseGitError as error:
                raise OrchestrationError(str(error), next_command=retry) from error
        if self._internal_release() and "s3_published" not in run.state.completed_checkpoints:
            self._run(("make", "release-s3"), next_command=retry)
            run = self._checkpoint(run, "s3_published", "publishing")
        run = self._checkpoint(run, "published", "complete")
        self.store.cleanup(base, head, succeeded=True)
        return run

    def resume(
        self,
        base: str,
        head: str,
        *,
        accept_degraded_evidence: bool = False,
    ) -> StoredReleaseRun:
        run = self.load(base, head)
        if "built" not in run.state.completed_checkpoints:
            run = self.build(base, head)
        if "applied" not in run.state.completed_checkpoints:
            run = self.apply(
                base,
                head,
                accept_degraded_evidence=accept_degraded_evidence,
                interactive=False,
            )
        return self.publish(base, head)

    def dry_run(
        self,
        *,
        base: str | None = None,
        head: str = "HEAD",
        version: str | None = None,
        guidance: str = "",
        new_plan: bool = False,
    ) -> Path:
        run = self.plan(
            base=base,
            head=head,
            version=version,
            guidance=guidance,
            new_plan=new_plan,
        )
        retry = self._dry_run_command(
            run.plan.base_tag, run.plan.head_commit, run.plan.selected_version
        )
        self.build(run.plan.base_tag, run.plan.head_commit, retry_command=retry)
        self.store.cleanup(run.plan.base_tag, run.plan.head_commit, succeeded=True)
        return run.run_dir

    @staticmethod
    def _resume_command(base: str, head: str) -> str:
        return f"python3 scripts/release/release_cli.py resume --base {base} --head {head}"

    @staticmethod
    def _dry_run_command(base: str, head: str, version: str | None) -> str:
        suffix = f" --version {version}" if version else ""
        return f"python3 scripts/release/release_cli.py dry-run --base {base} --head {head}{suffix}"


def main(argv: Sequence[str] | None = None) -> int:
    root = Path(__file__).resolve().parents[2]
    return run_cli(Orchestrator(root), argv)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (
        CanonicalContractError,
        GenerationError,
        OrchestrationError,
        ReleasePreconditionError,
        ReleaseStoreError,
        ValueError,
    ) as error:
        print(f"error: {error}", file=sys.stderr)
        next_command = getattr(error, "next_command", "")
        if next_command:
            print(f"retry: {next_command}", file=sys.stderr)
        raise SystemExit(1)
