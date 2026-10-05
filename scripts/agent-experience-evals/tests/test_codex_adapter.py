import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import codex_adapter as adapter


class AdapterTests(unittest.TestCase):
    def test_prompt_requires_exact_fixture_guidance_and_local_skill(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            (root / "AGENTS.md").write_text("# Fixture\nDo the bounded task.\n")
            skill = root / ".agents/skills/rhizome/SKILL.md"
            skill.parent.mkdir(parents=True)
            skill.write_text("fixture")
            text = (f"- `r0` = `{root / '.agents/skills'}`\n"
                    "- rhizome: Fixture. (file: r0/rhizome/SKILL.md)")
            def item(value):
                return {"content": [{"text": value}]}
            items = [item(text), item("# AGENTS.md instructions\n<INSTRUCTIONS>\n" +
                     (root / "AGENTS.md").read_text() + "</INSTRUCTIONS>"), item("task")]
            self.assertEqual(adapter.validate_prompt(items, root, "task")["skills"],
                             [".agents/skills/rhizome/SKILL.md"])
            items.insert(1, item("# AGENTS.md instructions\n<INSTRUCTIONS>personal</INSTRUCTIONS>"))
            with self.assertRaisesRegex(ValueError, "additional global"):
                adapter.validate_prompt(items, root, "task")

    def test_ambient_skill_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            items = [{"content": [{"text": "- `r0` = `/outside`\n"
                       "- other: ambient. (file: r0/other/SKILL.md)"}]}]
            with self.assertRaisesRegex(ValueError, "ambient skill"):
                adapter.validate_prompt(items, Path(temporary), "task")

    def test_environment_discards_provider_secrets_and_app_session(self):
        with patch.dict(os.environ, {"OPENAI_API_KEY": "secret", "CODEX_THREAD_ID": "parent"}):
            env = adapter.environment({}, {"repo": "/tmp/fixture"})
            self.assertNotIn("OPENAI_API_KEY", env)
            self.assertNotIn("CODEX_THREAD_ID", env)
            self.assertEqual(env.get("HOME"), os.environ.get("HOME"))

    def test_modified_isolation_policy_cannot_launch(self):
        with tempfile.TemporaryDirectory() as temporary:
            profile = Path(temporary) / "isolation.sb"
            profile.write_text("changed")
            run = {"isolation": {"profile": str(profile), "profile_sha256": "old"}}
            with self.assertRaisesRegex(ValueError, "profile changed"):
                adapter.command({}, run, Path(temporary))

    def test_preflight_wrong_model_never_starts_a_process(self):
        with patch.object(adapter.subprocess, "run") as process:
            result = adapter.preflight({"model": "gpt-6-astra", "reasoning": "xhigh"}, {})
            self.assertFalse(result["ready"])
            process.assert_not_called()


if __name__ == "__main__":
    unittest.main()
