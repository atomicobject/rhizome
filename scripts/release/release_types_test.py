#!/usr/bin/env python3
"""Contract tests for persisted release evidence and plans."""

from __future__ import annotations

from dataclasses import FrozenInstanceError, replace
import json
import unittest

from release_types import (
    DeliveryUnit,
    Diagnostic,
    EffortEvidence,
    GitCommit,
    PullRequestEvidence,
    ReleaseEvidence,
    ReleasePlan,
    ReleaseRunState,
    canonical_json,
    fingerprint_value,
)
from release_theme import ReleaseTheme


def sample_evidence() -> ReleaseEvidence:
    commit = GitCommit(
        sha="abc123",
        parents=("base123",),
        subject="Ship evidence pipeline (#42)",
        body="Bound release inputs.",
        mainline_index=0,
    )
    pull_request = PullRequestEvidence(
        number=42,
        title="Ship evidence pipeline",
        body="## Summary\nBound release inputs.",
        url="https://github.com/example/rhizome/pull/42",
        base_ref="main",
        head_ref="release-evidence",
        merge_sha="abc123",
        merged_at="2026-07-17T12:00:00Z",
        commit_shas=("abc123",),
    )
    effort = EffortEvidence(
        effort_id="EFF-0057",
        path="docs/efforts/release.md",
        name="Evidence-grounded release orchestration",
        actual_delivered="Bounded release evidence.",
        deviations=("GitHub enrichment is optional.",),
        inclusion_reason="included_new_complete",
    )
    unit = DeliveryUnit(
        mainline_commit=commit,
        changed_paths=("scripts/release/evidence.py",),
        insertions=80,
        deletions=5,
        pull_request=pull_request,
        efforts=(effort,),
        commits=(commit,),
    )
    return ReleaseEvidence(
        base_tag="v0.49.0",
        base_commit="base123",
        head_commit="abc123",
        release_branch="main",
        unreleased_entries=("Bound release-note evidence.",),
        delivery_units=(unit,),
        diagnostics=(
            Diagnostic(
                code="github_partial",
                message="Child pull requests were unavailable.",
                details=(("primary_pr", "42"),),
            ),
        ),
        github_status="partial",
    )


class CanonicalSerializationTest(unittest.TestCase):
    def test_canonical_json_is_independent_of_mapping_insertion_order(self) -> None:
        left = {"z": [2, 1], "a": {"accent": "Rhizomé", "ok": True}}
        right = {"a": {"ok": True, "accent": "Rhizomé"}, "z": [2, 1]}

        self.assertEqual(canonical_json(left), canonical_json(right))
        self.assertEqual(
            canonical_json(left),
            '{"a":{"accent":"Rhizomé","ok":true},"z":[2,1]}',
        )

    def test_fingerprint_changes_when_nested_evidence_changes(self) -> None:
        original = sample_evidence()
        changed = ReleaseEvidence.from_dict(
            {**original.to_dict(), "head_commit": "different"}
        )

        self.assertTrue(original.fingerprint.startswith("sha256:"))
        self.assertNotEqual(original.fingerprint, changed.fingerprint)

    def test_unordered_values_are_rejected_instead_of_fingerprinted(self) -> None:
        with self.assertRaisesRegex(TypeError, "set"):
            canonical_json({"paths": {"a", "b"}})


class EvidenceContractTest(unittest.TestCase):
    def test_diagnostic_wording_does_not_invalidate_reviewed_evidence(self) -> None:
        evidence = sample_evidence()
        changed_diagnostic = ReleaseEvidence.from_dict(
            {
                **evidence.to_dict(),
                "diagnostics": [
                    {
                        "code": "different_code",
                        "message": "Different transient GitHub error wording.",
                        "severity": "error",
                        "source": "gh",
                        "details": [["request_id", "new-value"]],
                    }
                ],
            }
        )

        self.assertNotEqual(evidence.to_dict(), changed_diagnostic.to_dict())
        self.assertEqual(evidence.fingerprint, changed_diagnostic.fingerprint)

    def test_normalized_github_status_participates_in_fingerprint(self) -> None:
        evidence = sample_evidence()
        same_status = ReleaseEvidence.from_dict(
            {**evidence.to_dict(), "github_status": " PARTIAL "}
        )
        different_status = ReleaseEvidence.from_dict(
            {**evidence.to_dict(), "github_status": "unavailable"}
        )

        self.assertEqual(evidence.fingerprint, same_status.fingerprint)
        self.assertNotEqual(evidence.fingerprint, different_status.fingerprint)

    def test_release_branch_participates_in_fingerprint(self) -> None:
        main_evidence = sample_evidence()
        release_evidence = replace(main_evidence, release_branch="release")

        self.assertNotEqual(main_evidence.fingerprint, release_evidence.fingerprint)

    def test_evidence_round_trips_through_human_readable_json(self) -> None:
        evidence = sample_evidence()

        restored = ReleaseEvidence.from_json(evidence.to_json())

        self.assertEqual(restored, evidence)
        self.assertEqual(restored.fingerprint, evidence.fingerprint)
        self.assertEqual(json.loads(evidence.to_json()), evidence.to_dict())
        self.assertTrue(evidence.to_json().endswith("\n"))

    def test_evidence_is_immutable_at_every_collection_boundary(self) -> None:
        evidence = sample_evidence()

        with self.assertRaises(FrozenInstanceError):
            evidence.head_commit = "changed"  # type: ignore[misc]
        with self.assertRaises(AttributeError):
            evidence.delivery_units.append("changed")  # type: ignore[attr-defined]
        with self.assertRaises(FrozenInstanceError):
            evidence.delivery_units[0].insertions = 0  # type: ignore[misc]

    def test_mutable_constructor_inputs_are_frozen_on_entry(self) -> None:
        parents = ["base123"]
        commit = GitCommit(sha="abc123", parents=parents)  # type: ignore[arg-type]

        parents.append("later")

        self.assertEqual(commit.parents, ("base123",))
        with self.assertRaises(AttributeError):
            commit.parents.append("changed")  # type: ignore[attr-defined]

    def test_unknown_schema_version_fails_loudly(self) -> None:
        value = sample_evidence().to_dict()
        value["schema_version"] = 999

        with self.assertRaisesRegex(ValueError, "schema version 999"):
            ReleaseEvidence.from_dict(value)


class ReleasePlanContractTest(unittest.TestCase):
    def test_plan_records_review_preconditions_and_generated_content(self) -> None:
        evidence = sample_evidence()
        plan = ReleasePlan(
            base_tag=evidence.base_tag,
            base_commit=evidence.base_commit,
            head_commit=evidence.head_commit,
            evidence_fingerprint=evidence.fingerprint,
            model="gpt-5.6-luna",
            reasoning_effort="high",
            recommended_bump="minor",
            rationale="The release adds a maintainer capability.",
            themes=(ReleaseTheme("Bounded release evidence.", "Bound release-note evidence.", ("pr:42", "effort:EFF-0057")),),
        )

        restored = ReleasePlan.from_json(plan.to_json())

        self.assertEqual(restored, plan)
        self.assertEqual(restored.evidence_fingerprint, evidence.fingerprint)
        self.assertEqual(restored.plan_fingerprint, fingerprint_value(plan))

    def test_plan_fingerprint_covers_generation_inputs(self) -> None:
        evidence = sample_evidence()
        common = {
            "base_tag": evidence.base_tag,
            "base_commit": evidence.base_commit,
            "head_commit": evidence.head_commit,
            "evidence_fingerprint": evidence.fingerprint,
            "model": "gpt-5.6-luna",
            "reasoning_effort": "high",
            "themes": (ReleaseTheme("Release note.", "Changelog entry.", ("pr:42",)),),
        }

        luna = ReleasePlan(**common)
        overridden = ReleasePlan(**{**common, "model": "other-model"})

        self.assertNotEqual(luna.plan_fingerprint, overridden.plan_fingerprint)

    def test_plan_has_no_execution_phase(self) -> None:
        plan = ReleasePlan(
            base_tag="v0.49.0",
            base_commit="base123",
            head_commit="abc123",
            evidence_fingerprint="sha256:evidence",
            model="gpt-5.6-luna",
            reasoning_effort="high",
            themes=(ReleaseTheme("Release note.", "Changelog entry.", ("pr:42",)),),
        )

        self.assertNotIn("phase", plan.to_dict())
        with self.assertRaisesRegex(ValueError, "phase"):
            ReleasePlan.from_dict({**plan.to_dict(), "phase": "published"})


class ReleaseRunStateContractTest(unittest.TestCase):
    def test_run_state_rejects_legacy_schema_without_changing_plan_schema(self) -> None:
        with self.assertRaisesRegex(ValueError, "release run state schema version 1"):
            ReleaseRunState.from_dict(
                {
                    "schema_version": 1,
                    "plan_fingerprint": "sha256:plan",
                    "phase": "publishing",
                    "completed_checkpoints": ["github_published"],
                }
            )

    def test_run_state_round_trips_separately_from_reviewed_plan(self) -> None:
        state = ReleaseRunState(
            plan_fingerprint="sha256:reviewed-plan",
            phase="building",
            completed_checkpoints=("evidence_collected", "notes_generated"),
            release_date="2026-07-17",
        )

        restored = ReleaseRunState.from_json(state.to_json())

        self.assertEqual(restored, state)
        self.assertEqual(restored.plan_fingerprint, "sha256:reviewed-plan")
        self.assertEqual(restored.phase, "building")
        self.assertEqual(restored.release_date, "2026-07-17")

    def test_run_state_rejects_duplicate_or_empty_checkpoints(self) -> None:
        with self.assertRaisesRegex(ValueError, "unique"):
            ReleaseRunState(
                plan_fingerprint="sha256:plan",
                completed_checkpoints=("planned", "planned"),
            )
        with self.assertRaisesRegex(ValueError, "non-empty"):
            ReleaseRunState(
                plan_fingerprint="sha256:plan",
                completed_checkpoints=("",),
            )

    def test_run_state_rejects_unknown_or_out_of_order_checkpoints(self) -> None:
        with self.assertRaisesRegex(ValueError, "known"):
            ReleaseRunState(
                plan_fingerprint="sha256:plan",
                completed_checkpoints=("invented",),
            )
        with self.assertRaisesRegex(ValueError, "order"):
            ReleaseRunState(
                plan_fingerprint="sha256:plan",
                completed_checkpoints=("published", "built"),
            )

    def test_publish_checkpoints_end_with_github_and_completion(self) -> None:
        state = ReleaseRunState(
            plan_fingerprint="sha256:plan",
            phase="published",
            completed_checkpoints=(
                "evidence_collected",
                "notes_generated",
                "plan_reviewed",
                "version_selected",
                "built",
                "applied",
                "canonical_branches_published",
                "github_published",
                "published",
            ),
        )

        self.assertEqual(
            state.completed_checkpoints[-3:],
            ("canonical_branches_published", "github_published", "published"),
        )


class DeliveryUnitContractTest(unittest.TestCase):
    def test_unit_id_is_derived_from_primary_pr_or_mainline_commit(self) -> None:
        evidence = sample_evidence()
        pr_unit = evidence.delivery_units[0]
        direct_unit = DeliveryUnit(
            mainline_commit=pr_unit.mainline_commit,
            commits=(pr_unit.mainline_commit,),
        )

        self.assertEqual(pr_unit.unit_id, "pr:42")
        self.assertEqual(direct_unit.unit_id, "commit:abc123")
        self.assertNotIn("unit_id", evidence.to_dict()["delivery_units"][0])

    def test_unit_rejects_empty_duplicate_or_unanchored_commits(self) -> None:
        anchor = GitCommit(sha="anchor")
        other = GitCommit(sha="other")

        with self.assertRaisesRegex(ValueError, "non-empty"):
            DeliveryUnit(mainline_commit=anchor)
        with self.assertRaisesRegex(ValueError, "unique"):
            DeliveryUnit(mainline_commit=anchor, commits=(anchor, anchor))
        with self.assertRaisesRegex(ValueError, "mainline commit"):
            DeliveryUnit(mainline_commit=anchor, commits=(other,))

    def test_unit_rejects_inconsistent_pr_roles_and_identities(self) -> None:
        anchor = GitCommit(sha="anchor")
        primary = PullRequestEvidence(number=42, role="primary")
        supporting = PullRequestEvidence(number=43, role="supporting")

        with self.assertRaisesRegex(ValueError, "primary or stub"):
            DeliveryUnit(
                mainline_commit=anchor,
                pull_request=supporting,
                commits=(anchor,),
            )
        with self.assertRaisesRegex(ValueError, "supporting role"):
            DeliveryUnit(
                mainline_commit=anchor,
                pull_request=primary,
                supporting_pull_requests=(primary,),
                commits=(anchor,),
            )
        with self.assertRaisesRegex(ValueError, "distinct"):
            DeliveryUnit(
                mainline_commit=anchor,
                pull_request=primary,
                supporting_pull_requests=(
                    PullRequestEvidence(number=42, role="supporting"),
                ),
                commits=(anchor,),
            )

    def test_unit_requires_normalized_unique_changed_paths(self) -> None:
        anchor = GitCommit(sha="anchor")

        with self.assertRaisesRegex(ValueError, "normalized"):
            DeliveryUnit(
                mainline_commit=anchor,
                changed_paths=("scripts/./release.py",),
                commits=(anchor,),
            )
        with self.assertRaisesRegex(ValueError, "unique"):
            DeliveryUnit(
                mainline_commit=anchor,
                changed_paths=("a.py", "a.py"),
                commits=(anchor,),
            )


if __name__ == "__main__":
    unittest.main()
