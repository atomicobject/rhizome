#!/usr/bin/env python3
"""Versioned persistence contracts for evidence-grounded releases.

Fingerprints use canonical JSON; persisted files use human-readable JSON.
"""

from __future__ import annotations

from dataclasses import dataclass, fields, is_dataclass
import hashlib
import json
import posixpath
from typing import Any, Mapping
from release_theme import ReleaseTheme


SCHEMA_VERSION = 1
RUN_STATE_SCHEMA_VERSION = 3
RELEASE_CHECKPOINT_ORDER = (
    "evidence_collected", "notes_generated", "plan_reviewed", "version_selected",
    "built", "internal_release", "applied", "canonical_branches_published", "github_published",
    "s3_published", "published",
)


def _json_value(value: Any) -> Any:
    if is_dataclass(value) and not isinstance(value, type):
        return {field.name: _json_value(getattr(value, field.name)) for field in fields(value)}
    if isinstance(value, Mapping):
        converted: dict[str, Any] = {}
        for key, item in value.items():
            if not isinstance(key, str):
                raise TypeError(f"canonical JSON requires string mapping keys, got {type(key).__name__}")
            converted[key] = _json_value(item)
        return converted
    if isinstance(value, (list, tuple)):
        return [_json_value(item) for item in value]
    if isinstance(value, (set, frozenset)):
        raise TypeError(f"canonical JSON does not support unordered {type(value).__name__} values")
    if value is None or isinstance(value, (str, int, float, bool)):
        return value
    raise TypeError(f"canonical JSON does not support {type(value).__name__}")


def canonical_json(value: Any) -> str:
    """Serialize supported values identically regardless of mapping insertion order."""

    return json.dumps(
        _json_value(value),
        ensure_ascii=False,
        allow_nan=False,
        separators=(",", ":"),
        sort_keys=True,
    )


def fingerprint_value(value: Any) -> str:
    """Return a self-describing SHA-256 fingerprint for a persisted contract value."""

    digest = hashlib.sha256(canonical_json(value).encode("utf-8")).hexdigest()
    return f"sha256:{digest}"


def _read_schema(value: Mapping[str, Any], contract: str) -> None:
    version = value.get("schema_version")
    if version != SCHEMA_VERSION:
        raise ValueError(
            f"unsupported {contract} schema version {version}; expected {SCHEMA_VERSION}"
        )


def _pretty_json(value: Any) -> str:
    return json.dumps(
        _json_value(value),
        ensure_ascii=False,
        allow_nan=False,
        indent=2,
        sort_keys=True,
    ) + "\n"


def _tuple_strings(value: Any) -> tuple[str, ...]:
    return tuple(str(item) for item in value or ())


def _freeze_tuple(instance: Any, field_name: str) -> None:
    object.__setattr__(instance, field_name, tuple(getattr(instance, field_name)))


@dataclass(frozen=True, slots=True)
class Diagnostic:
    code: str
    message: str
    severity: str = "warning"
    source: str | None = None
    details: tuple[tuple[str, str], ...] = ()

    def __post_init__(self) -> None:
        details = tuple((str(key), str(value)) for key, value in self.details)
        object.__setattr__(self, "details", details)

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> Diagnostic:
        return cls(
            code=str(value["code"]),
            message=str(value["message"]),
            severity=str(value.get("severity", "warning")),
            source=None if value.get("source") is None else str(value["source"]),
            details=tuple((str(key), str(item)) for key, item in value.get("details", ())),
        )


@dataclass(frozen=True, slots=True)
class GitCommit:
    sha: str
    parents: tuple[str, ...] = ()
    subject: str = ""
    body: str = ""
    mainline_index: int = -1

    def __post_init__(self) -> None:
        _freeze_tuple(self, "parents")

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> GitCommit:
        return cls(
            sha=str(value["sha"]),
            parents=_tuple_strings(value.get("parents")),
            subject=str(value.get("subject", "")),
            body=str(value.get("body", "")),
            mainline_index=int(value.get("mainline_index", -1)),
        )


@dataclass(frozen=True, slots=True)
class PullRequestEvidence:
    number: int
    title: str = ""
    body: str = ""
    url: str = ""
    base_ref: str = ""
    head_ref: str = ""
    merge_sha: str = ""
    merged_at: str = ""
    role: str = "primary"
    commit_shas: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        _freeze_tuple(self, "commit_shas")
        if self.number <= 0:
            raise ValueError("pull request number must be positive")
        if self.role not in {"primary", "supporting", "stub"}:
            raise ValueError("pull request role must be primary, supporting, or stub")
        if len(set(self.commit_shas)) != len(self.commit_shas):
            raise ValueError("pull request commit SHAs must be unique")

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> PullRequestEvidence:
        return cls(
            number=int(value["number"]),
            title=str(value.get("title", "")),
            body=str(value.get("body", "")),
            url=str(value.get("url", "")),
            base_ref=str(value.get("base_ref", "")),
            head_ref=str(value.get("head_ref", "")),
            merge_sha=str(value.get("merge_sha", "")),
            merged_at=str(value.get("merged_at", "")),
            role=str(value.get("role", "primary")),
            commit_shas=_tuple_strings(value.get("commit_shas")),
        )


@dataclass(frozen=True, slots=True)
class EffortEvidence:
    effort_id: str
    path: str
    name: str
    actual_delivered: str
    deviations: tuple[str, ...] = ()
    role: str = "peer"
    inclusion_reason: str = "included_complete_transition"

    def __post_init__(self) -> None:
        _freeze_tuple(self, "deviations")

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> EffortEvidence:
        return cls(
            effort_id=str(value["effort_id"]),
            path=str(value["path"]),
            name=str(value["name"]),
            actual_delivered=str(value["actual_delivered"]),
            deviations=_tuple_strings(value.get("deviations")),
            role=str(value.get("role", "peer")),
            inclusion_reason=str(value.get("inclusion_reason", "included_complete_transition")),
        )


@dataclass(frozen=True, slots=True)
class DeliveryUnit:
    mainline_commit: GitCommit
    changed_paths: tuple[str, ...] = ()
    insertions: int = 0
    deletions: int = 0
    pull_request: PullRequestEvidence | None = None
    supporting_pull_requests: tuple[PullRequestEvidence, ...] = ()
    efforts: tuple[EffortEvidence, ...] = ()
    commits: tuple[GitCommit, ...] = ()

    def __post_init__(self) -> None:
        tuple_fields = "changed_paths", "supporting_pull_requests", "efforts", "commits"
        for field_name in tuple_fields:
            _freeze_tuple(self, field_name)

        if not self.commits:
            raise ValueError("delivery unit commits must be non-empty")
        commit_shas = tuple(commit.sha for commit in self.commits)
        if len(set(commit_shas)) != len(commit_shas):
            raise ValueError("delivery unit commits must be unique and ordered")
        if self.mainline_commit.sha not in commit_shas:
            raise ValueError("delivery unit mainline commit must belong to commits")
        indexed = [commit.mainline_index for commit in self.commits]
        if all(index >= 0 for index in indexed) and indexed != sorted(indexed):
            raise ValueError("delivery unit commits must be in mainline order")

        normalized_paths = tuple(_normalize_changed_path(path) for path in self.changed_paths)
        if normalized_paths != self.changed_paths:
            raise ValueError("delivery unit changed paths must be normalized")
        if len(set(self.changed_paths)) != len(self.changed_paths):
            raise ValueError("delivery unit changed paths must be unique")

        if self.pull_request is not None:
            if self.pull_request.role not in {"primary", "stub"}:
                raise ValueError("delivery unit pull request must have primary or stub role")
            if self.pull_request.merge_sha and self.pull_request.merge_sha != self.mainline_commit.sha:
                raise ValueError("primary pull request merge SHA must match mainline commit")
            if self.pull_request.commit_shas and self.pull_request.commit_shas != commit_shas:
                raise ValueError("primary pull request commits must match delivery commits")

        supporting_numbers: list[int] = []
        for pull_request in self.supporting_pull_requests:
            if pull_request.role != "supporting":
                raise ValueError("supporting pull requests must have supporting role")
            supporting_numbers.append(pull_request.number)
        if len(set(supporting_numbers)) != len(supporting_numbers):
            raise ValueError("supporting pull request identities must be distinct")
        if self.pull_request is not None and self.pull_request.number in supporting_numbers:
            raise ValueError("primary and supporting pull request identities must be distinct")

    @property
    def unit_id(self) -> str:
        if self.pull_request is not None:
            return f"pr:{self.pull_request.number}"
        return f"commit:{self.mainline_commit.sha}"

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> DeliveryUnit:
        pull_request = value.get("pull_request")
        if "unit_id" in value:
            raise ValueError("delivery unit unit_id is derived and must not be persisted")
        return cls(
            mainline_commit=GitCommit.from_dict(value["mainline_commit"]),
            changed_paths=_tuple_strings(value.get("changed_paths")),
            insertions=int(value.get("insertions", 0)),
            deletions=int(value.get("deletions", 0)),
            pull_request=None if pull_request is None else PullRequestEvidence.from_dict(pull_request),
            supporting_pull_requests=tuple(
                PullRequestEvidence.from_dict(item)
                for item in value.get("supporting_pull_requests", ())
            ),
            efforts=tuple(
                EffortEvidence.from_dict(item) for item in value.get("efforts", ())
            ),
            commits=tuple(
                GitCommit.from_dict(item) for item in value.get("commits", ())
            ),
        )


def _normalize_changed_path(path: str) -> str:
    normalized = posixpath.normpath(path.replace("\\", "/"))
    if not path or normalized in {"", "."}:
        raise ValueError("delivery unit changed paths must be non-empty")
    if normalized.startswith("/") or normalized == ".." or normalized.startswith("../"):
        raise ValueError("delivery unit changed paths must be relative")
    return normalized


@dataclass(frozen=True, slots=True)
class ReleaseEvidence:
    base_tag: str
    base_commit: str
    head_commit: str
    release_branch: str
    unreleased_entries: tuple[str, ...] = ()
    delivery_units: tuple[DeliveryUnit, ...] = ()
    diagnostics: tuple[Diagnostic, ...] = ()
    github_status: str = "available"
    schema_version: int = SCHEMA_VERSION

    def __post_init__(self) -> None:
        if self.schema_version != SCHEMA_VERSION:
            raise ValueError(
                f"unsupported release evidence schema version {self.schema_version}; "
                f"expected {SCHEMA_VERSION}"
            )
        for field_name in ("unreleased_entries", "delivery_units", "diagnostics"):
            _freeze_tuple(self, field_name)
        normalized_status = self.github_status.strip().lower()
        if normalized_status not in {"available", "partial", "unavailable"}:
            raise ValueError(
                "github status must be available, partial, or unavailable"
            )
        object.__setattr__(self, "github_status", normalized_status)

    def to_dict(self) -> dict[str, Any]:
        return _json_value(self)
    def to_json(self) -> str:
        return _pretty_json(self)
    @property
    def fingerprint(self) -> str:
        # Diagnostic prose is operational context, not reviewed release evidence.
        # Excluding it keeps transient provider errors from invalidating a plan.
        return fingerprint_value(
            {
                "schema_version": self.schema_version,
                "base_tag": self.base_tag,
                "base_commit": self.base_commit,
                "head_commit": self.head_commit,
                "release_branch": self.release_branch,
                "github_status": self.github_status,
                "unreleased_entries": self.unreleased_entries,
                "delivery_units": self.delivery_units,
            }
        )

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> ReleaseEvidence:
        _read_schema(value, "release evidence")
        return cls(
            base_tag=str(value["base_tag"]),
            base_commit=str(value["base_commit"]),
            head_commit=str(value["head_commit"]),
            release_branch=str(value["release_branch"]),
            unreleased_entries=_tuple_strings(value.get("unreleased_entries")),
            delivery_units=tuple(
                DeliveryUnit.from_dict(item) for item in value.get("delivery_units", ())
            ),
            diagnostics=tuple(
                Diagnostic.from_dict(item) for item in value.get("diagnostics", ())
            ),
            github_status=str(value.get("github_status", "available")),
            schema_version=int(value["schema_version"]),
        )

    @classmethod
    def from_json(cls, content: str) -> ReleaseEvidence:
        value = json.loads(content)
        if not isinstance(value, dict):
            raise ValueError("release evidence JSON must contain an object")
        return cls.from_dict(value)


@dataclass(frozen=True, slots=True)
class ReleasePlan:
    base_tag: str
    base_commit: str
    head_commit: str
    evidence_fingerprint: str
    model: str
    reasoning_effort: str
    generation_guidance: str = ""
    recommended_bump: str = ""
    rationale: str = ""
    themes: tuple[ReleaseTheme, ...] = ()
    selected_version: str | None = None
    diagnostics: tuple[Diagnostic, ...] = ()
    schema_version: int = SCHEMA_VERSION

    def __post_init__(self) -> None:
        if self.schema_version != SCHEMA_VERSION:
            raise ValueError(
                f"unsupported release plan schema version {self.schema_version}; "
                f"expected {SCHEMA_VERSION}"
            )
        for field_name in ("themes", "diagnostics"):
            _freeze_tuple(self, field_name)
        if not self.themes:
            raise ValueError("release plan requires at least one attributed theme")

    @property
    def release_notes(self) -> str:
        return "\n\n".join(theme.release_note for theme in self.themes)

    @property
    def changelog_entries(self) -> tuple[str, ...]:
        return tuple(theme.changelog_entry for theme in self.themes)

    @property
    def source_attributions(self) -> tuple[str, ...]:
        return tuple(dict.fromkeys(source for theme in self.themes for source in theme.source_ids))

    def to_dict(self) -> dict[str, Any]:
        return _json_value(self)

    def to_json(self) -> str:
        return _pretty_json(self)

    @property
    def plan_fingerprint(self) -> str:
        return fingerprint_value(self)

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> ReleasePlan:
        _read_schema(value, "release plan")
        if "phase" in value:
            raise ValueError("release plan phase belongs in release run state")
        selected_version = value.get("selected_version")
        return cls(
            base_tag=str(value["base_tag"]),
            base_commit=str(value["base_commit"]),
            head_commit=str(value["head_commit"]),
            evidence_fingerprint=str(value["evidence_fingerprint"]),
            model=str(value["model"]),
            reasoning_effort=str(value["reasoning_effort"]),
            generation_guidance=str(value.get("generation_guidance", "")),
            recommended_bump=str(value.get("recommended_bump", "")),
            rationale=str(value.get("rationale", "")),
            themes=tuple(ReleaseTheme.from_dict(item) for item in value.get("themes", ())),
            selected_version=(
                None if selected_version is None else str(selected_version)
            ),
            diagnostics=tuple(
                Diagnostic.from_dict(item) for item in value.get("diagnostics", ())
            ),
            schema_version=int(value["schema_version"]),
        )

    @classmethod
    def from_json(cls, content: str) -> ReleasePlan:
        value = json.loads(content)
        if not isinstance(value, dict):
            raise ValueError("release plan JSON must contain an object")
        return cls.from_dict(value)


@dataclass(frozen=True, slots=True)
class ReleaseRunState:
    """Mutable release progress persisted separately from the reviewed plan."""

    plan_fingerprint: str
    phase: str = "planned"
    completed_checkpoints: tuple[str, ...] = ()
    release_date: str | None = None
    schema_version: int = RUN_STATE_SCHEMA_VERSION

    def __post_init__(self) -> None:
        if self.schema_version != RUN_STATE_SCHEMA_VERSION:
            raise ValueError(
                f"unsupported release run state schema version {self.schema_version}; "
                f"expected {RUN_STATE_SCHEMA_VERSION}; restart the release plan"
            )
        _freeze_tuple(self, "completed_checkpoints")
        if not self.plan_fingerprint.strip():
            raise ValueError("release run state plan fingerprint must be non-empty")
        if not self.phase.strip():
            raise ValueError("release run state phase must be non-empty")
        if any(not checkpoint.strip() for checkpoint in self.completed_checkpoints):
            raise ValueError("release run state checkpoints must be non-empty")
        if len(set(self.completed_checkpoints)) != len(self.completed_checkpoints):
            raise ValueError("release run state checkpoints must be unique and ordered")
        unknown = set(self.completed_checkpoints).difference(RELEASE_CHECKPOINT_ORDER)
        if unknown:
            raise ValueError("release run state checkpoints must use known names")
        checkpoint_indexes = [RELEASE_CHECKPOINT_ORDER.index(item) for item in self.completed_checkpoints]
        if checkpoint_indexes != sorted(checkpoint_indexes):
            raise ValueError("release run state checkpoints must follow release order")
        if self.release_date is not None and not self.release_date.strip():
            raise ValueError("release run state date must be non-empty when set")

    def to_dict(self) -> dict[str, Any]:
        return _json_value(self)

    def to_json(self) -> str:
        return _pretty_json(self)

    @classmethod
    def from_dict(cls, value: Mapping[str, Any]) -> ReleaseRunState:
        return cls(
            plan_fingerprint=str(value["plan_fingerprint"]),
            phase=str(value.get("phase", "planned")),
            completed_checkpoints=_tuple_strings(value.get("completed_checkpoints")),
            release_date=None if value.get("release_date") is None else str(value["release_date"]),
            schema_version=int(value["schema_version"]),
        )

    @classmethod
    def from_json(cls, content: str) -> ReleaseRunState:
        value = json.loads(content)
        if not isinstance(value, dict):
            raise ValueError("release run state JSON must contain an object")
        return cls.from_dict(value)
