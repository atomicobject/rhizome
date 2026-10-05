"""Synthetic repositories and deterministic checks for agent-experience evaluations.

The files under ``testdata/agent-experience`` are reviewer-owned source material.
``materialize`` copies one visible case into a scratch repository and deliberately
does not copy ``oracles``.  ``check`` only observes the scratch repository; it does
not invoke a model or consult the model's response.

The module has no dependencies outside the Python standard library so the runner
can use it before a scratch repository has any project dependencies installed.
"""

from __future__ import annotations

import hashlib
import json
import shutil
from pathlib import Path
from typing import Any, Iterable, Mapping

from fixture_support import (
    _before_content,
    _before_sha256,
    _check,
    _read,
    _required_paths,
    _result,
    _run_behavior_probe,
    _run_visible_tests,
    _unchanged_seeded_files,
    _unexpected_added_paths,
    snapshot,
)


_REPO_ROOT = Path(__file__).resolve().parents[2]
_FIXTURE_ROOT = _REPO_ROOT / "testdata" / "agent-experience"


_REGISTRY_PATH = _FIXTURE_ROOT / "registry.json"
_REGISTRY = json.loads(_REGISTRY_PATH.read_text())
if _REGISTRY.get("version") != 1:
    raise ValueError("unsupported fixture registry version")
CASES: dict[str, dict[str, Any]] = {case["case_id"]: case for case in _REGISTRY["cases"]}


def _case_definition(case_id: str) -> dict[str, Any]:
    try:
        return CASES[case_id.upper()]
    except KeyError as exc:
        valid = ", ".join(sorted(CASES))
        raise ValueError(f"unknown fixture case {case_id!r}; expected one of {valid}") from exc


def _source_directory(case: Mapping[str, Any]) -> Path:
    return _FIXTURE_ROOT / str(case["relative_path"])


def _safe_relative(path: Path, root: Path) -> str:
    try:
        return path.resolve().relative_to(root.resolve()).as_posix()
    except ValueError as exc:
        raise ValueError(f"path escapes fixture destination: {path}") from exc


def _iter_files(root: Path) -> Iterable[Path]:
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"symlinks are not allowed in synthetic fixtures: {path}")
        if path.is_file():
            yield path


def _copy_visible_tree(source: Path, destination: Path) -> list[str]:
    if not source.is_dir():
        raise FileNotFoundError(f"fixture source does not exist: {source}")
    if destination.exists() and destination.is_symlink():
        raise ValueError(f"fixture destination cannot be a symlink: {destination}")
    if destination.exists() and any(destination.iterdir()):
        raise FileExistsError(f"fixture destination must be empty: {destination}")
    destination.mkdir(parents=True, exist_ok=True)
    copied: list[str] = []
    for source_path in _iter_files(source):
        relative = source_path.relative_to(source)
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source_path, target)
        copied.append(relative.as_posix())
    return copied


def materialize(case_id: str, destination: Path) -> dict[str, Any]:
    """Copy a model-visible case into ``destination`` and return its task metadata.

    The oracle directory is a sibling of every case source under the repository's
    testdata tree and is never traversed by this function.  ``destination`` must
    be empty when it already exists so a stale worktree cannot silently contaminate
    a run.
    """

    case = _case_definition(case_id)
    destination = Path(destination)
    copied = _copy_visible_tree(_source_directory(case), destination)
    expected = sorted(copied)
    required = sorted(str(path) for path in case["expected_paths"])
    if expected != required:
        raise RuntimeError(
            f"fixture registry drift for {case_id}: copied={expected!r}, expected={required!r}"
        )
    result = {
        "case_id": case["case_id"],
        "installation": case["installation"],
        "prompt": case["prompt"],
        "expected_paths": expected,
        "allowed_changes": list(case["allowed_changes"]),
        "notes": case["notes"],
        "fixture_root": str(destination),
    }
    if "follow_up_prompt" in case:
        result["follow_up_prompt"] = case["follow_up_prompt"]
    return result


def _check_a01(destination: Path, before: Mapping[str, Any] | None) -> dict[str, Any]:
    case = CASES["A01"]
    policy = _read(destination, "docs/reference/expiry-policy.md") if (destination / "docs/reference/expiry-policy.md").is_file() else ""
    historical = _read(destination, "docs/history/expiry-policy-local-time.md") if (destination / "docs/history/expiry-policy-local-time.md").is_file() else ""
    source = _read(destination, "src/expiry.py") if (destination / "src/expiry.py").is_file() else ""
    checks: list[Mapping[str, Any]] = [
        _required_paths(destination, case),
        _run_visible_tests(destination),
        _run_behavior_probe(
            destination,
            (
                "from datetime import datetime, timezone; "
                "from src.expiry import is_valid, parse_persisted_expiry; "
                "expiry = parse_persisted_expiry('2026-09-07T16:00:00Z'); "
                "assert not is_valid(expiry, datetime(2026, 9, 7, 16, 0, 0, tzinfo=timezone.utc)); "
                "assert expiry.tzinfo == timezone.utc"
            ),
            "boundary_behavior",
            "exact boundary is expired and persisted values are UTC-aware",
        ),
        _check(
            "current_policy_is_explicit",
            "active" in policy and "UTC" in policy and "exactly at the expiry instant" in policy,
            "current policy states the UTC and exact-boundary invariants",
        ),
        _check(
            "superseded_rule_is_marked",
            "superseded" in historical.lower() and "local time" in historical.lower(),
            "older local-time guidance is visibly superseded",
        ),
        _check(
            "public_surface_preserved",
            "def is_valid(expiry: datetime, now: datetime | None = None)" in source
            and "def parse_persisted_expiry(value: str)" in source,
            "expiry public function signatures remain stable",
        ),
        _unchanged_seeded_files(
            destination,
            case,
            allowed_changes={"src/expiry.py", "tests/test_expiry.py"},
        ),
        _unexpected_added_paths(destination, case, before),
    ]
    return _result("A01", checks)


def _check_exact_replacement(destination: Path, case_id: str, before: Mapping[str, Any] | None) -> dict[str, Any]:
    case = CASES[case_id]
    readme_path = destination / "README.md"
    if not readme_path.is_file():
        return _result(case_id, [_required_paths(destination, case)])
    current = readme_path.read_text(encoding="utf-8")
    initial = (_source_directory(case) / "README.md").read_text(encoding="utf-8")
    expected = initial.replace("recieve", "receive")
    checks: list[Mapping[str, Any]] = [
        _required_paths(destination, case),
        _check(
            "exact_replacement",
            current == expected and initial.count("recieve") == 1 and current.count("recieve") == 0,
            "README contains only the requested one-word replacement",
            initial_occurrences=initial.count("recieve"),
            remaining_occurrences=current.count("recieve"),
        ),
        _unchanged_seeded_files(destination, case, allowed_changes={"README.md"}),
        _unexpected_added_paths(destination, case, before),
    ]
    before_readme = _before_content(before, "README.md")
    if before_readme is not None:
        checks.append(
            _check(
                "before_snapshot_matches_seed",
                before_readme == initial.encode("utf-8"),
                "before snapshot contains the seeded README",
            )
        )
    return _result(case_id, checks)


def _check_a03(destination: Path, before: Mapping[str, Any] | None) -> dict[str, Any]:
    case = CASES["A03"]
    effort = _read(destination, "docs/efforts/2026-09-07-12-00-retry-limit.md") if (destination / "docs/efforts/2026-09-07-12-00-retry-limit.md").is_file() else ""
    spec = _read(destination, "docs/specs/retry-limit.md") if (destination / "docs/specs/retry-limit.md").is_file() else ""
    backlog = _read(destination, "docs/backlog/retry-jitter.md") if (destination / "docs/backlog/retry-jitter.md").is_file() else ""
    checks: list[Mapping[str, Any]] = [
        _required_paths(destination, case),
        _run_visible_tests(destination),
        _check(
            "approved_limit_preserved",
            "three" in spec.lower() and "no retry after success" in spec.lower(),
            "approved maximum and success rule remain in the spec",
        ),
        _check(
            "approved_identity_and_handoff",
            "[[Synthetic Approver]]" in effort and "python3 -m unittest" in effort,
            "synthetic approver and verification evidence are durable",
        ),
        _check(
            "excluded_jitter_preserved",
            "out of scope" in backlog.lower() and "jitter" in backlog.lower(),
            "neighboring jitter request remains excluded",
        ),
        _check(
            "no_jitter_implementation",
            "jitter" not in _read(destination, "src/retry.py").lower() if (destination / "src/retry.py").is_file() else False,
            "implementation does not add the excluded jitter feature",
        ),
        _unexpected_added_paths(destination, case, before),
    ]
    return _result("A03", checks)


def _check_a04(destination: Path, before: Mapping[str, Any] | None) -> dict[str, Any]:
    case = CASES["A04"]
    effort_path = destination / "docs/efforts/2026-09-07-12-30-two-phase-retry.md"
    effort = effort_path.read_text(encoding="utf-8") if effort_path.is_file() else ""
    decision_path = destination / "docs/decisions/retry-zero-attempts.md"
    decision = decision_path.read_text(encoding="utf-8") if decision_path.is_file() else ""
    checks: list[Mapping[str, Any]] = [
        _required_paths(destination, case),
        _run_visible_tests(destination),
        _check(
            "zero_attempt_edge_documented",
            "zero" in decision.lower()
            and ("max_attempts=0" in decision or "max attempts = 0" in decision.lower())
            and "phase 1" in decision.lower(),
            "phase 1 durable context records the zero-attempt behavior",
        ),
        _check(
            "fresh_handoff_completed",
            "phase 1" in effort.lower()
            and "phase 2" in effort.lower()
            and ("complete" in effort.lower() or "delivered" in effort.lower()),
            "effort records both phases and a completed handoff",
        ),
        _check(
            "approved_scope_preserved",
            "[[Synthetic Approver]]" in effort and "approved" in effort.lower(),
            "approved effort identity and scope remain visible",
        ),
        _unexpected_added_paths(destination, case, before),
    ]
    return _result("A04", checks)


def _check_a05(destination: Path, before: Mapping[str, Any] | None) -> dict[str, Any]:
    case = CASES["A05"]
    v1 = _read(destination, "docs/reference/requirements/sources/policy-v1.md") if (destination / "docs/reference/requirements/sources/policy-v1.md").is_file() else ""
    v2 = _read(destination, "docs/reference/requirements/sources/policy-v2.md") if (destination / "docs/reference/requirements/sources/policy-v2.md").is_file() else ""
    requirement = _read(destination, "docs/reference/requirements/requirements/retention-limit.md") if (destination / "docs/reference/requirements/requirements/retention-limit.md").is_file() else ""
    process = _read(destination, "docs/reference/domain/processes/retention-review.md") if (destination / "docs/reference/domain/processes/retention-review.md").is_file() else ""
    workflow = _read(destination, "docs/reference/domain/workflows/review-retention-change.md") if (destination / "docs/reference/domain/workflows/review-retention-change.md").is_file() else ""
    spec = _read(destination, "docs/specs/retention-controls.md") if (destination / "docs/specs/retention-controls.md").is_file() else ""
    effort = _read(destination, "docs/efforts/2026-09-07-13-00-retention-source-review.md") if (destination / "docs/efforts/2026-09-07-13-00-retention-source-review.md").is_file() else ""
    checks: list[Mapping[str, Any]] = [
        _required_paths(destination, case),
        _check(
            "source_versions_and_locations_preserved",
            all(value in v1 for value in ("source-version: v1", "signed-agreement.pdf", "2026-08-15", "30 days"))
            and all(value in v2 for value in ("source-version: v2", "change-request-2026-09-01.pdf", "2026-09-01", "14 days")),
            "both source versions retain dates, locations, and distinct facts",
        ),
        _check(
            "accepted_requirement_remains_unimplemented",
            "status: accepted" in requirement
            and "status: implemented" not in requirement.lower(),
            "accepted requirement is not promoted to implemented",
        ),
        _check(
            "traceability_is_explicit",
            all(value in requirement for value in ("SRC-A05-V1", "PROC-A05", "WF-A05", "SPEC-A05"))
            and "REQ-A05-RETENTION" in spec
            and "requirement" in process.lower()
            and "process" in workflow.lower(),
            "requirement, process, workflow, source, and spec links remain explicit",
        ),
        _check(
            "conflict_and_partial_backport_recorded",
            ("conflict" in effort.lower() or "candidate" in effort.lower())
            and "30 days" in effort
            and "14 days" in effort
            and ("partial" in effort.lower() or "one of two" in effort.lower()),
            "review record preserves conflict and partial delivery evidence",
        ),
        _check(
            "frozen_spec_not_rewritten",
            "30 days" in spec and "14 days" not in spec,
            "the conflicting proposal does not rewrite the frozen contract",
        ),
        _check(
            "domain_layers_remain_distinct",
            "actor" in workflow.lower() and "state" in process.lower(),
            "process and actor workflow retain distinct meanings",
        ),
        _check(
            "review_record_updated",
            _before_sha256(before, "docs/efforts/2026-09-07-13-00-retention-source-review.md")
            is None
            or _before_sha256(
                before,
                "docs/efforts/2026-09-07-13-00-retention-source-review.md",
            )
            != hashlib.sha256(effort.encode("utf-8")).hexdigest(),
            "source review leaves an updated durable record when a before snapshot is available",
        ),
        _unchanged_seeded_files(
            destination,
            case,
            allowed_changes=set(str(path) for path in case["allowed_changes"]),
        ),
        _unexpected_added_paths(destination, case, before),
    ]
    return _result("A05", checks)


def _check_a06(destination: Path, before: Mapping[str, Any] | None) -> dict[str, Any]:
    case = CASES["A06"]
    readme = _read(destination, "README.md") if (destination / "README.md").is_file() else ""
    policy = _read(destination, "docs/reference/expiry-policy.md") if (destination / "docs/reference/expiry-policy.md").is_file() else ""
    checks: list[Mapping[str, Any]] = [
        _required_paths(destination, case),
        _check(
            "independent_typo_fix",
            readme.count("recieve") == 0 and readme.count("receive") == 1,
            "README typo is fixed exactly once",
        ),
        _check(
            "local_rule_remains_observable",
            "UTC" in policy and "exactly at the expiry instant" in policy,
            "local current policy remains available for direct inspection",
        ),
        _check(
            "no_fake_provider_or_index_artifacts",
            not any(
                path.name in {"embeddings.sqlite", "semantic-results.json"}
                or path.suffix in {".env", ".key"}
                for path in _iter_files(destination)
            ),
            "fixture contains no fabricated retrieval or secret artifacts",
        ),
        _unchanged_seeded_files(destination, case, allowed_changes={"README.md"}),
        _unexpected_added_paths(destination, case, before),
    ]
    return _result("A06", checks)


def check(case_id: str, destination: Path, before: Mapping[str, Any] | None = None) -> dict[str, Any]:
    """Run deterministic hidden checks for a materialized case.

    ``check`` does not inspect model events or final prose.  Those remain human
    review evidence in the runner.  A result's top-level ``passed`` field is the
    supporting hidden-check outcome consumed by the runner; human review is separate.
    """

    case = _case_definition(case_id)
    destination = Path(destination)
    if not destination.is_dir() or destination.is_symlink():
        return _result(case["case_id"], [_check("destination", False, "destination is not a directory")])
    normalized_before = before if isinstance(before, Mapping) else None
    if case["case_id"] == "A01":
        return _check_a01(destination, normalized_before)
    if case["case_id"] in {"A02", "A06"}:
        return _check_exact_replacement(destination, case["case_id"], normalized_before) if case["case_id"] == "A02" else _check_a06(destination, normalized_before)
    if case["case_id"] == "A03":
        return _check_a03(destination, normalized_before)
    if case["case_id"] == "A04":
        return _check_a04(destination, normalized_before)
    if case["case_id"] == "A05":
        return _check_a05(destination, normalized_before)
    raise AssertionError(f"unhandled fixture case {case['case_id']}")


def registry() -> dict[str, Any]:
    """Return JSON-serializable case metadata for runner discovery."""

    return {"version": 1, "cases": [CASES[key] for key in sorted(CASES)]}


__all__ = ["CASES", "check", "materialize", "registry", "snapshot"]
