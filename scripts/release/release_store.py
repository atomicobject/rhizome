#!/usr/bin/env python3
"""Persist reviewed release inputs and resumable execution state.

Reviewed evidence and plans are immutable within a run directory. Mutable phase
checkpoints live in ``state.json`` and reference the immutable plan fingerprint.
Successful orchestration may trash the exact run directory; failures retain it.
"""

from __future__ import annotations

from dataclasses import dataclass
import os
from pathlib import Path
import re
import subprocess
import tempfile
from typing import Callable, Sequence

from release_types import ReleaseEvidence, ReleasePlan, ReleaseRunState


_UNSAFE_COMPONENT = re.compile(r"[^A-Za-z0-9._-]+")


class ReleaseStoreError(RuntimeError):
    """Base error for invalid or unreadable persisted release artifacts."""


class ReleaseConflictError(ReleaseStoreError):
    """A reviewed artifact would be replaced without explicit authorization."""


@dataclass(frozen=True, slots=True)
class StoredReleaseRun:
    run_dir: Path
    evidence: ReleaseEvidence
    plan: ReleasePlan
    state: ReleaseRunState


def _default_command_runner(command: Sequence[str]) -> None:
    subprocess.run(command, check=True)


def _safe_component(value: str, *, label: str) -> str:
    sanitized = _UNSAFE_COMPONENT.sub("-", value).strip(".-_")
    if not sanitized:
        raise ValueError(f"{label} must contain at least one safe character")
    return sanitized


class ReleaseStore:
    """Own the ignored ``.release`` persistence boundary for release runs."""

    def __init__(
        self,
        repo_root: str | Path,
        *,
        command_runner: Callable[[Sequence[str]], None] = _default_command_runner,
    ) -> None:
        self.repo_root = Path(repo_root).resolve()
        self.root = self.repo_root / ".release"
        self._command_runner = command_runner

    def run_dir(self, base_tag: str, head_commit: str) -> Path:
        safe_base = _safe_component(base_tag, label="base tag")
        safe_head = _safe_component(head_commit, label="head commit")[:12]
        return self.root / f"{safe_base}-{safe_head}"

    def write_run(
        self,
        evidence: ReleaseEvidence,
        plan: ReleasePlan,
        state: ReleaseRunState,
        *,
        new_plan: bool = False,
    ) -> StoredReleaseRun:
        """Persist a new run or idempotently resume an identical reviewed plan."""

        self._validate_contracts(evidence, plan, state)
        run_dir = self.run_dir(evidence.base_tag, evidence.head_commit)

        existing_evidence = self._read_optional(
            run_dir / "evidence.json", ReleaseEvidence.from_json
        )
        existing_plan = self._read_optional(run_dir / "plan.json", ReleasePlan.from_json)
        existing_state = self._read_optional(
            run_dir / "state.json", ReleaseRunState.from_json
        )

        if not new_plan:
            self._reject_conflicts(
                evidence,
                plan,
                existing_evidence,
                existing_plan,
                existing_state,
            )
            if existing_evidence is not None:
                evidence = existing_evidence
            if existing_plan is not None:
                plan = existing_plan
            if existing_state is not None:
                state = existing_state

        run_dir.mkdir(parents=True, exist_ok=True)
        if new_plan or existing_evidence is None:
            self._atomic_write(run_dir / "evidence.json", evidence.to_json())
        if new_plan or existing_plan is None:
            self._atomic_write(run_dir / "plan.json", plan.to_json())
        if new_plan or existing_state is None:
            self._atomic_write(run_dir / "state.json", state.to_json())

        return StoredReleaseRun(run_dir, evidence, plan, state)

    def load_run(self, base_tag: str, head_commit: str) -> StoredReleaseRun:
        """Load and cross-check all artifacts for one release run."""

        run_dir = self.run_dir(base_tag, head_commit)
        evidence = self._read_required(
            run_dir / "evidence.json", ReleaseEvidence.from_json
        )
        plan = self._read_required(run_dir / "plan.json", ReleasePlan.from_json)
        state = self._read_required(run_dir / "state.json", ReleaseRunState.from_json)
        self._validate_contracts(evidence, plan, state)
        if evidence.base_tag != base_tag or evidence.head_commit != head_commit:
            raise ReleaseConflictError(
                "release directory collides with artifacts for a different base or head"
            )
        return StoredReleaseRun(run_dir, evidence, plan, state)

    def update_state(
        self,
        plan: ReleasePlan,
        state: ReleaseRunState,
    ) -> ReleaseRunState:
        """Atomically checkpoint mutable state for the already reviewed plan."""

        if state.plan_fingerprint != plan.plan_fingerprint:
            raise ValueError("state plan fingerprint does not match supplied plan")
        run_dir = self.run_dir(plan.base_tag, plan.head_commit)
        persisted_plan = self._read_required(run_dir / "plan.json", ReleasePlan.from_json)
        if persisted_plan.plan_fingerprint != plan.plan_fingerprint:
            raise ReleaseConflictError(
                "plan fingerprint differs from the reviewed persisted plan"
            )
        self._atomic_write(run_dir / "state.json", state.to_json())
        return state

    def cleanup(self, base_tag: str, head_commit: str, *, succeeded: bool) -> bool:
        """Trash exactly one successful run, retaining failed or missing runs."""

        if not succeeded:
            return False
        run_dir = self.run_dir(base_tag, head_commit)
        if not run_dir.exists():
            return False
        self._command_runner(("trash", str(run_dir)))
        return True

    @staticmethod
    def _validate_contracts(
        evidence: ReleaseEvidence,
        plan: ReleasePlan,
        state: ReleaseRunState,
    ) -> None:
        evidence_range = (
            evidence.base_tag,
            evidence.base_commit,
            evidence.head_commit,
        )
        plan_range = (plan.base_tag, plan.base_commit, plan.head_commit)
        if plan_range != evidence_range:
            raise ValueError("plan release range does not match evidence")
        if plan.evidence_fingerprint != evidence.fingerprint:
            raise ValueError("plan evidence fingerprint does not match evidence")
        if state.plan_fingerprint != plan.plan_fingerprint:
            raise ValueError("state plan fingerprint does not match plan")

    @staticmethod
    def _reject_conflicts(
        evidence: ReleaseEvidence,
        plan: ReleasePlan,
        existing_evidence: ReleaseEvidence | None,
        existing_plan: ReleasePlan | None,
        existing_state: ReleaseRunState | None,
    ) -> None:
        if (
            existing_evidence is not None
            and existing_evidence.fingerprint != evidence.fingerprint
        ):
            raise ReleaseConflictError(
                "evidence fingerprint differs from reviewed evidence; use new_plan=True"
            )
        if existing_plan is not None and existing_plan.plan_fingerprint != plan.plan_fingerprint:
            raise ReleaseConflictError(
                "plan fingerprint differs from reviewed plan; use new_plan=True"
            )
        if (
            existing_state is not None
            and existing_state.plan_fingerprint != plan.plan_fingerprint
        ):
            raise ReleaseConflictError(
                "state references a different reviewed plan; use new_plan=True"
            )

    @staticmethod
    def _read_optional(path: Path, loader: Callable[[str], object]) -> object | None:
        if not path.exists():
            return None
        return ReleaseStore._read(path, loader)

    @staticmethod
    def _read_required(path: Path, loader: Callable[[str], object]) -> object:
        if not path.exists():
            raise ReleaseStoreError(f"missing release artifact: {path}")
        return ReleaseStore._read(path, loader)

    @staticmethod
    def _read(path: Path, loader: Callable[[str], object]) -> object:
        try:
            return loader(path.read_text(encoding="utf-8"))
        except (OSError, TypeError, ValueError) as error:
            raise ReleaseStoreError(f"invalid release artifact {path}: {error}") from error

    @staticmethod
    def _atomic_write(path: Path, content: str) -> None:
        """Replace one artifact atomically; failed temporary output remains inspectable."""

        file_descriptor, temporary_name = tempfile.mkstemp(
            dir=path.parent,
            prefix=f".{path.name}.",
            text=True,
        )
        temporary_path = Path(temporary_name)
        try:
            with os.fdopen(file_descriptor, "w", encoding="utf-8") as handle:
                handle.write(content)
                handle.flush()
                os.fsync(handle.fileno())
            os.replace(temporary_path, path)
        except BaseException:
            # IMPORTANT: preserve failed output for diagnosis; successful run cleanup
            # trashes the containing directory through ``cleanup``.
            raise
