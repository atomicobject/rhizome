from __future__ import annotations

import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import campaign
import persistent_campaign
import persistent_environment
import runner


def valid_manifest(root: Path) -> dict:
    source = "a" * 40
    binary_hash = "b" * 64
    methods = persistent_campaign.METHOD_INSTRUCTIONS
    cases = persistent_campaign.CASES
    runs = []
    for run_id, case_id, arm in persistent_campaign.RUN_ORDER:
        pair_id, task = cases[case_id]
        instruction = methods[arm]
        runs.append({
            "id": run_id, "case_id": case_id, "pair_id": pair_id, "arm": arm, "method": arm,
            "repo": str(root / "prepared" / run_id), "binary": str(root / "rzm"),
            "binary_sha256": binary_hash, "source_sha": source, "task_prompt": task,
            "method_instruction": instruction,
            "prompt": task + "\n\nEvaluation method constraint: " + instruction,
            "fixture": {"README.md": "c" * 64}, "fixture_commit": "d" * 40,
            "fixture_tree": {"A01": "1", "A03": "2", "A05": "3"}[case_id] * 40,
            "guidance": {"AGENTS.md": "e" * 64}, "support": str(root / "support" / run_id),
            "setup_sha256": "f" * 64, "isolation": {"codex": "/opt/homebrew/bin/codex"},
            "build_receipt": str(root / "build.json"), "build_receipt_sha256": "9" * 64,
        })
    return {
        "schema_version": 2, "kind": "persistent-code-mode-subscription-evaluation",
        "campaign_id": persistent_campaign.CAMPAIGN_ID, "model": persistent_campaign.MODEL,
        "reasoning": persistent_campaign.REASONING, "max_launches": 6,
        "profile_sha256": persistent_campaign.APPROVED_PROFILE_SHA256,
        "comparison_design": "forced-arm", "automatic_retries": 0, "api_fallback": False,
        "per_run_seconds": 600, "total_seconds": 3600, "runs": runs,
    }


class PersistentCampaignTests(unittest.TestCase):
    def test_exact_six_paired_runs_use_one_binary_and_equivalent_trees(self):
        with tempfile.TemporaryDirectory() as directory:
            persistent_campaign.validate_manifest(valid_manifest(Path(directory)))

    def test_wrong_model_prompt_or_pair_tree_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = valid_manifest(Path(directory))
            for mutate in (
                lambda value: value.update(model="gpt-6-astra"),
                lambda value: value["runs"][0].update(prompt="changed"),
                lambda value: value["runs"][1].update(fixture_tree="0" * 40),
            ):
                changed = json.loads(json.dumps(manifest))
                mutate(changed)
                with self.assertRaises(persistent_campaign.RunnerError):
                    persistent_campaign.validate_manifest(changed)

    def test_seventh_reservation_is_impossible_and_old_campaign_stays_four(self):
        self.assertEqual(campaign.MAX_LAUNCHES, 4)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = valid_manifest(root)
            ledger = runner.load_ledger(root / "results", manifest)
            for run in manifest["runs"]:
                runner.reserve(ledger, manifest, run, root / "results")
            extra = dict(manifest["runs"][-1], id="p04-direct")
            with self.assertRaises(runner.RunnerError):
                runner.reserve(ledger, manifest, extra, root / "results")

    def test_only_one_run_can_be_selected_per_invocation(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = valid_manifest(Path(directory))
            with self.assertRaises(persistent_campaign.RunnerError):
                persistent_campaign.selected_runs(manifest, ["p01-direct", "p01-persistent"])

    def test_environment_binds_fresh_ledger_and_all_six_distinct_homes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = valid_manifest(root)
            manifest_hash = persistent_campaign.canonical_hash(manifest)
            results = root / "results"
            ledger = runner.load_ledger(results, manifest)
            runs = {}
            for run_id, _, _ in persistent_campaign.RUN_ORDER:
                home = root / "homes" / run_id
                runs[run_id] = {
                    "clean_home": str(home), "codex_home": str(home / ".codex"),
                    "config_sha256": "a" * 64, "execution_repo": str(root / "execution" / run_id),
                    "protected_paths": [],
                }
            overlay = {
                "schema_version": 1, "kind": "clean-codex-environment-persistent-campaign",
                "campaign_id": persistent_campaign.CAMPAIGN_ID,
                "campaign_hash": runner.campaign_hash(manifest),
                "manifest_sha256": manifest_hash,
                "expected_run_ids": [item[0] for item in persistent_campaign.RUN_ORDER],
                "runs": runs, "support_root": str(root / "runtime-support"),
                "output_root": str(results), "protected_paths": [],
            }
            path = root / "environment.json"
            path.write_text(json.dumps(overlay))
            loaded, _ = persistent_environment.load(path, manifest, manifest_hash)
            self.assertEqual(persistent_environment.validate_ledger(loaded, results), ledger)
            self.assertEqual(ledger["launches_reserved"], 0)


if __name__ == "__main__":
    unittest.main()
