#!/usr/bin/env python3

import json
import os
import stat
import tempfile
import textwrap
import unittest
from dataclasses import replace
from pathlib import Path

import notes
from release_types import (
    DeliveryUnit,
    EffortEvidence,
    GitCommit,
    PullRequestEvidence,
    ReleaseEvidence,
)


def unit(index: int, detail_size: int = 80) -> DeliveryUnit:
    sha = f"sha-{index}"
    detail = f"detail-{index}-" + ("x" * detail_size)
    commit = GitCommit(
        sha=sha,
        parents=(f"sha-{index - 1}",),
        subject=f"User-facing delivery {index}",
        body=f"commit {detail}",
        mainline_index=index,
    )
    return DeliveryUnit(
        mainline_commit=commit,
        pull_request=PullRequestEvidence(
            number=100 + index,
            title=f"Feature {index}",
            body=f"PR {detail}",
            role="primary",
            merge_sha=sha,
            commit_shas=(sha,),
        ),
        efforts=(
            EffortEvidence(
                effort_id=f"EFF-{index:04d}",
                path=f"docs/efforts/{index}.md",
                name=f"Effort {index}",
                actual_delivered=f"Effort {detail}",
                role="primary",
            ),
        ),
        commits=(commit,),
    )


def evidence(count: int = 2, detail_size: int = 80) -> ReleaseEvidence:
    return ReleaseEvidence(
        base_tag="v1.0.0",
        base_commit="base",
        head_commit="head",
        release_branch="main",
        unreleased_entries=("Curated shipped behavior " + ("c" * detail_size),),
        delivery_units=tuple(unit(i, detail_size) for i in range(count)),
    )


VALID_RESULT = {
    "recommended_bump": "minor",
    "rationale": "The release adds user-facing capabilities.",
    "themes": [{
        "release_note": "- Added useful behavior.",
        "changelog_entry": "Added useful behavior.",
        "source_ids": ["pr:100"],
    }],
}


class PacketTest(unittest.TestCase):
    def test_packet_keeps_every_unit_identity_and_summary(self):
        packets = notes.build_generation_packets(evidence(3), budget_chars=10_000)

        self.assertEqual(len(packets), 1)
        units = packets[0].payload["delivery_units"]
        self.assertEqual([item["unit_id"] for item in units], ["pr:100", "pr:101", "pr:102"])
        self.assertEqual([item["summary"] for item in units], ["Feature 0", "Feature 1", "Feature 2"])
        self.assertLessEqual(len(packets[0].prompt_payload()), 10_000)

    def test_detail_budget_uses_trust_priority_and_records_omissions(self):
        release = evidence(1, detail_size=300)
        minimal_size = len(notes.build_minimal_packet(release).prompt_payload())
        budget = minimal_size + 160

        packet = notes.build_generation_packets(release, budget_chars=budget)[0]
        payload = packet.payload

        self.assertTrue(payload["unreleased_entries"])
        self.assertFalse(payload["delivery_units"][0].get("efforts"))
        self.assertIn("effort:EFF-0000", packet.omitted_sources)
        self.assertTrue(packet.truncated_sources or packet.omitted_sources)
        self.assertLessEqual(len(packet.prompt_payload()), budget)

    def test_minimal_overflow_chunks_without_dropping_units(self):
        release = evidence(8, detail_size=20)
        two_units = evidence(2, detail_size=20)
        budget = max(512, len(notes.build_minimal_packet(two_units).prompt_payload()) + 20)

        packets = notes.build_generation_packets(release, budget_chars=budget)

        self.assertGreater(len(packets), 1)
        unit_ids = [
            item["unit_id"]
            for packet in packets
            for item in packet.payload["delivery_units"]
        ]
        self.assertEqual(unit_ids, [f"pr:{100 + index}" for index in range(8)])
        self.assertTrue(all(len(packet.prompt_payload()) <= budget for packet in packets))

    def test_long_minimal_summary_records_truncation(self):
        release = evidence(1)
        current = release.delivery_units[0]
        long_pr = replace(current.pull_request, title="z" * 800)
        release = replace(release, delivery_units=(replace(current, pull_request=long_pr),))

        packet = notes.build_minimal_packet(release)

        self.assertIn("summary:pr:100", packet.truncated_sources)
        self.assertLessEqual(len(packet.payload["delivery_units"][0]["summary"]), 500)


class CodexGenerationTest(unittest.TestCase):
    def make_fake_codex(
        self,
        *,
        result: dict | str = VALID_RESULT,
        login_error: str = "",
        exec_error: str = "",
    ) -> tuple[Path, Path, Path]:
        temp_dir = Path(self.temp_dir.name)
        codex_path = temp_dir / "codex"
        log_path = temp_dir / "calls.jsonl"
        prompt_path = temp_dir / "prompts.txt"
        encoded_result = result if isinstance(result, str) else json.dumps(result)
        codex_path.write_text(
            textwrap.dedent(
                f"""\
                #!/usr/bin/env python3
                import json
                import pathlib
                import sys

                log_path = pathlib.Path({str(log_path)!r})
                prompt_path = pathlib.Path({str(prompt_path)!r})
                with log_path.open("a", encoding="utf-8") as handle:
                    handle.write(json.dumps(sys.argv[1:]) + "\\n")
                if sys.argv[1:3] == ["login", "status"]:
                    if {login_error!r}:
                        print({login_error!r}, file=sys.stderr)
                        raise SystemExit(1)
                    print("Logged in")
                    raise SystemExit(0)
                prompt = sys.stdin.read()
                with prompt_path.open("a", encoding="utf-8") as handle:
                    handle.write(prompt + "\\n---CALL---\\n")
                if {exec_error!r}:
                    print({exec_error!r}, file=sys.stderr)
                    raise SystemExit(1)
                schema_path = pathlib.Path(sys.argv[sys.argv.index("--output-schema") + 1])
                schema = json.loads(schema_path.read_text(encoding="utf-8"))
                assert schema["additionalProperties"] is False
                assert schema["properties"]["recommended_bump"]["enum"] == ["minor", "patch"]
                assert schema["properties"]["themes"]["maxItems"] == 7
                def assert_supported(value):
                    if isinstance(value, dict):
                        unsupported = {{"minLength", "uniqueItems"}}.intersection(value)
                        assert not unsupported, f"unsupported structured-output keyword: {{unsupported}}"
                        for item in value.values():
                            assert_supported(item)
                    elif isinstance(value, list):
                        for item in value:
                            assert_supported(item)
                assert_supported(schema)
                output_path = pathlib.Path(sys.argv[sys.argv.index("--output-last-message") + 1])
                output_path.write_text({encoded_result!r}, encoding="utf-8")
                """
            ),
            encoding="utf-8",
        )
        codex_path.chmod(codex_path.stat().st_mode | stat.S_IXUSR)
        return codex_path, log_path, prompt_path

    def setUp(self):
        self.temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp_dir.cleanup)

    def calls(self, path: Path) -> list[list[str]]:
        return [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines()]

    def test_default_invocation_is_luna_medium_read_only_with_schema(self):
        codex, log_path, _ = self.make_fake_codex()

        result = notes.generate_release_notes(
            evidence(1), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
        )

        self.assertEqual(result.recommended_bump, "minor")
        self.assertEqual(result.model, "gpt-5.6-luna")
        self.assertEqual(result.reasoning_effort, "medium")
        calls = self.calls(log_path)
        self.assertEqual(calls[0], ["login", "status"])
        argv = calls[1]
        self.assertEqual(argv[:3], ["exec", "--model", "gpt-5.6-luna"])
        self.assertIn('model_reasoning_effort="medium"', argv)
        self.assertEqual(argv[argv.index("--sandbox") + 1], "read-only")
        schema_path = Path(argv[argv.index("--output-schema") + 1])
        self.assertFalse(schema_path.exists())
        self.assertIn("--output-last-message", argv)

    def test_environment_overrides_are_explicit(self):
        codex, log_path, _ = self.make_fake_codex()
        env = {"RZM_RELEASE_MODEL": "custom-model", "RZM_RELEASE_REASONING_EFFORT": "medium"}

        notes.generate_release_notes(
            evidence(1),
            codex_bin=str(codex),
            repo_root=Path(self.temp_dir.name),
            environ=env,
        )

        argv = self.calls(log_path)[1]
        self.assertEqual(argv[:3], ["exec", "--model", "custom-model"])
        self.assertIn('model_reasoning_effort="medium"', argv)

    def test_prompt_enforces_a_global_user_facing_editorial_pass(self):
        codex, _, prompt_path = self.make_fake_codex()

        notes.generate_release_notes(
            evidence(3), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
        )

        prompt = prompt_path.read_text(encoding="utf-8")
        self.assertIn("at most 7 themes for the entire release", prompt)
        self.assertIn("Cluster related delivery units", prompt)
        self.assertIn("Lead with the user benefit", prompt)
        self.assertIn("Omit internal-only", prompt)
        self.assertIn("breaking changes, required migrations, and default changes", prompt)
        self.assertIn("begin with `- `", prompt)
        self.assertIn("Do not create a second theme", prompt)

    def test_chunked_generation_calls_each_chunk_then_synthesizes(self):
        codex, log_path, prompt_path = self.make_fake_codex()
        release = evidence(7, detail_size=20)
        budget = max(512, len(notes.build_minimal_packet(evidence(2, 20)).prompt_payload()) + 20)

        notes.generate_release_notes(
            release,
            budget_chars=budget,
            codex_bin=str(codex),
            repo_root=Path(self.temp_dir.name),
        )

        calls = self.calls(log_path)
        exec_calls = [call for call in calls if call and call[0] == "exec"]
        self.assertGreater(len(exec_calls), 2)
        prompts = prompt_path.read_text(encoding="utf-8")
        self.assertIn("Synthesize the chunk results", prompts)
        for index in range(7):
            self.assertIn(f"pr:{100 + index}", prompts)

    def test_malformed_json_is_rejected(self):
        codex, _, _ = self.make_fake_codex(result="not json")

        with self.assertRaisesRegex(notes.GenerationError, "valid JSON"):
            notes.generate_release_notes(
                evidence(1), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
            )

    def test_invalid_or_extra_fields_are_rejected(self):
        invalid = dict(VALID_RESULT, recommended_bump="major", surprise=True)
        codex, _, _ = self.make_fake_codex(result=invalid)

        with self.assertRaisesRegex(notes.GenerationError, "unexpected fields|recommended_bump"):
            notes.generate_release_notes(
                evidence(1), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
            )

    def test_unknown_source_attribution_is_rejected(self):
        invalid = dict(VALID_RESULT, themes=[{
            "release_note": "Unknown.",
            "changelog_entry": "Unknown.",
            "source_ids": ["pr:999"],
        }])
        codex, _, _ = self.make_fake_codex(result=invalid)

        with self.assertRaisesRegex(notes.GenerationError, "unknown source attribution"):
            notes.generate_release_notes(
                evidence(1), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
            )

    def test_theme_without_source_attribution_is_rejected(self):
        invalid = dict(VALID_RESULT, themes=[{
            "release_note": "Unsupported.",
            "changelog_entry": "Unsupported.",
            "source_ids": [],
        }])
        codex, _, _ = self.make_fake_codex(result=invalid)

        with self.assertRaisesRegex(notes.GenerationError, "at least one source"):
            notes.generate_release_notes(
                evidence(1), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
            )

    def test_duplicate_theme_source_attribution_is_rejected_in_python(self):
        invalid = dict(VALID_RESULT, themes=[{
            "release_note": "Duplicated source.",
            "changelog_entry": "Duplicated source.",
            "source_ids": ["pr:100", "pr:100"],
        }])
        codex, _, _ = self.make_fake_codex(result=invalid)

        with self.assertRaisesRegex(notes.GenerationError, "must be unique"):
            notes.generate_release_notes(
                evidence(1), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
            )

    def test_more_than_seven_themes_is_rejected_in_python(self):
        invalid = dict(
            VALID_RESULT,
            themes=[
                {
                    "release_note": f"- Theme {index}.",
                    "changelog_entry": f"Theme {index}.",
                    "source_ids": ["pr:100"],
                }
                for index in range(8)
            ],
        )
        codex, _, _ = self.make_fake_codex(result=invalid)

        with self.assertRaisesRegex(notes.GenerationError, "at most 7 themes"):
            notes.generate_release_notes(
                evidence(1), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
            )

    def test_editorial_field_shapes_are_rejected_in_python(self):
        invalid = dict(
            VALID_RESULT,
            themes=[{
                "release_note": "Missing Markdown bullet.",
                "changelog_entry": "- Includes an unwanted marker.",
                "source_ids": ["pr:100"],
            }],
        )
        codex, _, _ = self.make_fake_codex(result=invalid)

        with self.assertRaisesRegex(notes.GenerationError, "release_note must begin"):
            notes.generate_release_notes(
                evidence(1), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
            )

    def test_auth_failure_stops_before_exec(self):
        codex, log_path, _ = self.make_fake_codex(login_error="not logged in")

        with self.assertRaisesRegex(notes.GenerationError, "authentication"):
            notes.generate_release_notes(
                evidence(1), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
            )

        self.assertEqual(self.calls(log_path), [["login", "status"]])

    def test_model_failure_does_not_retry_or_fallback(self):
        codex, log_path, _ = self.make_fake_codex(exec_error="model unavailable")

        with self.assertRaisesRegex(notes.GenerationError, "model unavailable"):
            notes.generate_release_notes(
                evidence(1), codex_bin=str(codex), repo_root=Path(self.temp_dir.name)
            )

        exec_calls = [call for call in self.calls(log_path) if call and call[0] == "exec"]
        self.assertEqual(len(exec_calls), 1)
        self.assertEqual(exec_calls[0][2], "gpt-5.6-luna")


if __name__ == "__main__":
    unittest.main()
