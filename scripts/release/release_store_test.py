#!/usr/bin/env python3
"""Behavior tests for persisted, resumable release runs."""

from __future__ import annotations

from pathlib import Path
import tempfile
import unittest

from release_store import ReleaseConflictError, ReleaseStore
from release_theme import ReleaseTheme
from release_types import ReleaseEvidence, ReleasePlan, ReleaseRunState


def sample_evidence(*, head: str = "abcdef1234567890") -> ReleaseEvidence:
    return ReleaseEvidence(
        base_tag="release/v0.49.0",
        base_commit="base123",
        head_commit=head,
        release_branch="main",
        unreleased_entries=("Bound release evidence.",),
    )


def sample_plan(
    evidence: ReleaseEvidence,
    *,
    model: str = "gpt-5.6-luna",
) -> ReleasePlan:
    return ReleasePlan(
        base_tag=evidence.base_tag,
        base_commit=evidence.base_commit,
        head_commit=evidence.head_commit,
        evidence_fingerprint=evidence.fingerprint,
        model=model,
        reasoning_effort="high",
        recommended_bump="minor",
        themes=(ReleaseTheme("Bound release evidence.", "Bound release evidence.", ("unreleased:1",)),),
    )


def sample_state(plan: ReleasePlan, *, phase: str = "planned") -> ReleaseRunState:
    checkpoints = () if phase == "planned" else (phase,)
    return ReleaseRunState(
        plan_fingerprint=plan.plan_fingerprint,
        phase=phase,
        completed_checkpoints=checkpoints,
    )


class ReleaseStoreTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp_dir.cleanup)
        self.repo = Path(self.temp_dir.name).resolve()
        self.commands: list[tuple[str, ...]] = []
        self.store = ReleaseStore(
            self.repo,
            command_runner=lambda command: self.commands.append(tuple(command)),
        )

    def test_run_directory_sanitizes_base_and_shortens_head(self) -> None:
        run_dir = self.store.run_dir("refs/tags/v0.49.0 candidate", "abcdef1234567890")

        self.assertEqual(
            run_dir,
            self.repo / ".release" / "refs-tags-v0.49.0-candidate-abcdef123456",
        )

    def test_write_and_load_round_trip_human_readable_artifacts(self) -> None:
        evidence = sample_evidence()
        plan = sample_plan(evidence)
        state = sample_state(plan)

        stored = self.store.write_run(evidence, plan, state)
        loaded = self.store.load_run(evidence.base_tag, evidence.head_commit)

        self.assertEqual(loaded.evidence, evidence)
        self.assertEqual(loaded.plan, plan)
        self.assertEqual(loaded.state, state)
        self.assertEqual(stored.run_dir, loaded.run_dir)
        for filename in ("evidence.json", "plan.json", "state.json"):
            content = (stored.run_dir / filename).read_text(encoding="utf-8")
            self.assertTrue(content.startswith("{\n"))
            self.assertTrue(content.endswith("\n"))

    def test_identical_reviewed_plan_resumes_without_resetting_state(self) -> None:
        evidence = sample_evidence()
        plan = sample_plan(evidence)
        built = sample_state(plan, phase="built")
        self.store.write_run(evidence, plan, built)
        state_path = self.store.run_dir(evidence.base_tag, evidence.head_commit) / "state.json"
        original_state = state_path.read_text(encoding="utf-8")

        resumed = self.store.write_run(evidence, plan, sample_state(plan))

        self.assertEqual(resumed.state, built)
        self.assertEqual(state_path.read_text(encoding="utf-8"), original_state)

    def test_different_evidence_never_overwrites_reviewed_run_by_default(self) -> None:
        evidence = sample_evidence()
        plan = sample_plan(evidence)
        stored = self.store.write_run(evidence, plan, sample_state(plan))
        original = (stored.run_dir / "evidence.json").read_text(encoding="utf-8")
        changed = ReleaseEvidence.from_dict(
            {**evidence.to_dict(), "unreleased_entries": ["Different evidence."]}
        )

        with self.assertRaisesRegex(ReleaseConflictError, "evidence fingerprint"):
            self.store.write_run(
                changed,
                sample_plan(changed),
                sample_state(sample_plan(changed)),
            )

        self.assertEqual(
            (stored.run_dir / "evidence.json").read_text(encoding="utf-8"),
            original,
        )

    def test_different_plan_never_overwrites_reviewed_run_by_default(self) -> None:
        evidence = sample_evidence()
        original_plan = sample_plan(evidence)
        stored = self.store.write_run(
            evidence,
            original_plan,
            sample_state(original_plan),
        )
        changed_plan = sample_plan(evidence, model="operator-override")

        with self.assertRaisesRegex(ReleaseConflictError, "plan fingerprint"):
            self.store.write_run(
                evidence,
                changed_plan,
                sample_state(changed_plan),
            )

        self.assertEqual(self.store.load_run(evidence.base_tag, evidence.head_commit).plan, original_plan)
        self.assertEqual(stored.run_dir.parent, self.repo / ".release")

    def test_new_plan_explicitly_replaces_artifacts_and_resets_state(self) -> None:
        evidence = sample_evidence()
        original_plan = sample_plan(evidence)
        self.store.write_run(evidence, original_plan, sample_state(original_plan, phase="built"))
        changed_plan = sample_plan(evidence, model="operator-override")
        planned = sample_state(changed_plan)

        replaced = self.store.write_run(
            evidence,
            changed_plan,
            planned,
            new_plan=True,
        )

        self.assertEqual(replaced.plan, changed_plan)
        self.assertEqual(replaced.state, planned)

    def test_state_must_reference_the_supplied_immutable_plan(self) -> None:
        evidence = sample_evidence()
        plan = sample_plan(evidence)
        wrong_state = ReleaseRunState(plan_fingerprint="sha256:" + "0" * 64)

        with self.assertRaisesRegex(ValueError, "state plan fingerprint"):
            self.store.write_run(evidence, plan, wrong_state)

        self.assertFalse(self.store.run_dir(evidence.base_tag, evidence.head_commit).exists())

    def test_update_state_requires_the_persisted_plan_and_writes_checkpoint(self) -> None:
        evidence = sample_evidence()
        plan = sample_plan(evidence)
        self.store.write_run(evidence, plan, sample_state(plan))
        published = sample_state(plan, phase="published")

        result = self.store.update_state(plan, published)

        self.assertEqual(result, published)
        self.assertEqual(
            self.store.load_run(evidence.base_tag, evidence.head_commit).state,
            published,
        )

    def test_failed_run_is_preserved_and_success_uses_trash_for_exact_run(self) -> None:
        evidence = sample_evidence()
        plan = sample_plan(evidence)
        stored = self.store.write_run(evidence, plan, sample_state(plan))

        self.assertFalse(
            self.store.cleanup(evidence.base_tag, evidence.head_commit, succeeded=False)
        )
        self.assertEqual(self.commands, [])
        self.assertTrue(stored.run_dir.exists())

        self.assertTrue(
            self.store.cleanup(evidence.base_tag, evidence.head_commit, succeeded=True)
        )
        self.assertEqual(self.commands, [("trash", str(stored.run_dir))])
        self.assertNotEqual(self.commands[0][1], str(self.repo / ".release"))

    def test_cleanup_of_missing_run_is_a_no_op(self) -> None:
        self.assertFalse(self.store.cleanup("v1.0.0", "abc123", succeeded=True))
        self.assertEqual(self.commands, [])


if __name__ == "__main__":
    unittest.main()
