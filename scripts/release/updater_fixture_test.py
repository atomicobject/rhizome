from __future__ import annotations

import copy
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from desktop_candidates import CandidateError
from updater_fixture import prepare, read_overlay
from windows_sign import configuration, sign


class FixtureTests(unittest.TestCase):
    def test_generated_overlay_changes_only_endpoint_key_and_fixture_transport(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            production = root / "desktop/src-tauri/tauri.conf.json"
            production.parent.mkdir(parents=True)
            production.write_text('{"plugins":{"updater":{"pubkey":"production","endpoints":["https://production.invalid"]}}}')
            before = production.read_bytes()
            calls = []
            def generate(command, **kwargs):
                calls.append((command, kwargs))
                private = Path(command[-1])
                private.write_text("synthetic private test key")
                Path(str(private) + ".pub").write_text("c3ludGhldGljLXB1YmxpYy1rZXk=")
                return subprocess.CompletedProcess(command, 0, "not forwarded", "")
            overlay = prepare(root / "fixture", "http://127.0.0.1:9444/latest.json", root=root, runner=generate,
                              source={"TAURI_SIGNING_PRIVATE_KEY": "synthetic-unrelated", "GITHUB_TOKEN": "synthetic"})
            self.assertEqual(before, production.read_bytes())
            self.assertEqual({"plugins"}, set(read_overlay(overlay)))
            self.assertNotIn("TAURI_SIGNING_PRIVATE_KEY", calls[0][1]["env"])
            self.assertNotIn("GITHUB_TOKEN", calls[0][1]["env"])
            self.assertTrue(calls[0][1]["capture_output"])
            original = json.loads(overlay.read_text())
            for extra in ({"bundle": {"externalBin": ["binaries/team-api"]}}, {"version": "99.0.0"}):
                overlay.write_text(json.dumps({**original, **extra}))
                with self.assertRaises(CandidateError):
                    read_overlay(overlay)
            altered = copy.deepcopy(original)
            altered["plugins"]["updater"]["endpoints"] = ["http://127.0.0.1.attacker.invalid/latest.json"]
            overlay.write_text(json.dumps(altered))
            with self.assertRaises(CandidateError):
                read_overlay(overlay)

    def test_fixture_refuses_existing_key_directory_and_non_loopback_endpoint(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with self.assertRaisesRegex(CandidateError, "already exists"):
                prepare(root, "http://127.0.0.1:9444/latest.json")
            with self.assertRaisesRegex(CandidateError, "loopback"):
                prepare(root / "new", "https://production.invalid/latest.json")

    def test_windows_wrapper_keeps_canonical_bytes_and_scopes_native_signer(self):
        from desktop_candidates import sha256
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            cli = root / "rzm.exe"
            gui = root / "gui.exe"
            cli.write_bytes(b"canonical signed CLI")
            gui.write_bytes(b"GUI")
            config_path = root / "signer.json"
            config_path.write_text(json.dumps({"command": ["signer", "%1"], "environment": ["AZURE_CLIENT_SECRET"]}))
            config = configuration(config_path)
            calls = []
            def signer(command, **kwargs):
                calls.append((command, kwargs))
            env = {"PATH": "/usr/bin", "AZURE_CLIENT_SECRET": "synthetic-native", "TAURI_SIGNING_PRIVATE_KEY": "synthetic-updater"}
            sign(cli, config, sha256(cli), env, runner=signer)
            self.assertEqual([], calls)
            sign(gui, config, sha256(cli), env, runner=signer)
            self.assertEqual(["signer", str(gui)], calls[0][0])
            self.assertEqual("synthetic-native", calls[0][1]["env"]["AZURE_CLIENT_SECRET"])
            self.assertNotIn("TAURI_SIGNING_PRIVATE_KEY", calls[0][1]["env"])


if __name__ == "__main__":
    unittest.main()
