from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

import evidence


class EvidenceTests(unittest.TestCase):
    def test_strict_jsonl_retains_malformed_and_unknown_usage(self):
        with tempfile.TemporaryDirectory() as directory:
            events = Path(directory) / "events.jsonl"
            events.write_bytes(
                b'{"type":"item.started","item":{"type":"mcp_tool_call","name":"rhizome.search"}}\n'
                b'{"type":"item.completed","item":{"type":"mcp_tool_call","name":"rhizome.search"}}\n'
                b'{broken\n'
                b'{"type":"message","usage":{"input_tokens":7}}\n'
            )
            result = evidence.parse_events(events)
        self.assertTrue(result["incomplete"])
        self.assertEqual(result["records"][2]["raw"], "{broken")
        self.assertEqual(result["exposed_tool_invocations"], 1)
        self.assertEqual(result["exposed_rhizome_labeled_tool_events"], 1)
        self.assertEqual(result["usage"]["input_tokens"], 7)
        self.assertIsNone(result["usage"]["output_tokens"])
        self.assertEqual(result["usage"]["source"], ["event.usage"])

    def test_real_turn_usage_aliases_are_preserved_as_measured(self):
        event = {
            "type": "turn.completed",
            "usage": {
                "input_tokens": 115914,
                "cached_input_tokens": 97536,
                "cache_write_input_tokens": 0,
                "output_tokens": 2326,
                "reasoning_output_tokens": 1404,
            },
        }
        with tempfile.TemporaryDirectory() as directory:
            events = Path(directory) / "events.jsonl"
            events.write_text(json.dumps(event) + "\n")
            result = evidence.parse_events(events)
        self.assertFalse(result["incomplete"])
        self.assertEqual(result["usage"], {
            "source": ["event.usage"], "input_tokens": 115914, "output_tokens": 2326,
            "cached_tokens": 97536, "reasoning_tokens": 1404,
        })
        self.assertEqual(result["exposed_tool_invocations"], 0)

    def test_rhizome_metric_counts_lexical_tool_events_not_successful_operations(self):
        events_data = [
            {"type": "item.completed", "item": {"type": "command_execution",
             "command": "command -v rzm", "exit_code": 1}},
            {"type": "turn.completed", "usage": {}},
        ]
        with tempfile.TemporaryDirectory() as directory:
            events = Path(directory) / "events.jsonl"
            events.write_text("".join(json.dumps(event) + "\n" for event in events_data))
            result = evidence.parse_events(events)
        self.assertEqual(result["exposed_rhizome_labeled_tool_events"], 1)
        self.assertNotIn("exposed_rhizome_operations", result)

    def test_missing_event_file_reports_unavailable_not_zero_usage(self):
        with tempfile.TemporaryDirectory() as directory:
            result = evidence.parse_events(Path(directory) / "absent.jsonl")
        self.assertTrue(result["incomplete"])
        self.assertIsNone(result["usage"]["source"])
        self.assertIsNone(result["usage"]["input_tokens"])

    def test_valid_jsonl_without_completed_turn_is_incomplete(self):
        with tempfile.TemporaryDirectory() as directory:
            events = Path(directory) / "events.jsonl"
            events.write_text('{"type":"item.completed","item":{"type":"agent_message"}}\n')
            result = evidence.parse_events(events)
        self.assertTrue(result["incomplete"])
        self.assertIsNone(result["terminal_event"])

    def test_snapshot_includes_untracked_and_classifies_transient_separately(self):
        with tempfile.TemporaryDirectory() as directory:
            repo = Path(directory)
            subprocess.run(["git", "init", "-q", str(repo)], check=True)
            (repo / "tracked.txt").write_text("old")
            subprocess.run(["git", "-C", str(repo), "add", "tracked.txt"], check=True)
            (repo / "extra.txt").write_text("extra")
            (repo / ".rhizome" / "cache").mkdir(parents=True)
            (repo / ".rhizome" / "cache" / "state.json").write_text("{}")
            before = evidence.snapshot(repo)
            (repo / "tracked.txt").write_text("new")
            after = evidence.snapshot(repo)
        self.assertEqual(before["files"]["extra.txt"]["classification"], "untracked")
        self.assertIn(".rhizome/cache/state.json", before["transient"])
        self.assertEqual(evidence.changes(before, after)["modified"], ["tracked.txt"])

    def test_capture_provenance_copies_guidance_and_build_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo = root / "repo"
            result = root / "result"
            repo.mkdir()
            result.mkdir()
            guidance = repo / "AGENTS.md"
            receipt = root / "build.json"
            guidance.write_text("fixture guidance")
            receipt.write_text("{}")
            run = {
                "case_id": "A01", "guidance": {"AGENTS.md": evidence.sha256_file(guidance)},
                "build_receipt": str(receipt), "build_receipt_sha256": evidence.sha256_file(receipt),
            }
            provenance = evidence.capture_provenance(repo, run, result)
            self.assertEqual((result / "guidance" / "AGENTS.md").read_text(), "fixture guidance")
            self.assertEqual((result / "build-receipt.json").read_text(), "{}")
            self.assertEqual(provenance["oracle"]["case_id"], "A01")


if __name__ == "__main__":
    unittest.main()
