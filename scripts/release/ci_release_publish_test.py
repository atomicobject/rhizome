import base64
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock

from ci_release_artifacts import BUNDLE_FILES, digest
from ci_release_artifacts_test import SOURCE, VERSION, fixture
from ci_release_publish import Commands, PublishError, formula, publish, restore_gateway_receipt

ENV = {"GITHUB_ACTIONS": "true", "GITHUB_REF": "refs/heads/main", "GITHUB_REPOSITORY": "atomicobject/rhizome",
       "GITHUB_TOKEN": "synthetic-github-token", "BREW_GITHUB_TOKEN": "synthetic-brew-token",
       "APPLE_API_PRIVATE_KEY": "synthetic-apple-key", "OP_SERVICE_ACCOUNT_TOKEN": "synthetic-op-token"}


class FakeHost:
    def __init__(self, assets, notes):
        self.assets, self.notes = assets, notes
        self.release = None
        self.remote_release = self.remote_tag = None
        self.local_tag = False
        self.uploaded = {}
        self.events, self.calls = [], []
        self.formula = "old formula"
        self.fail_upload = None
        self.fail_brew = False
        self.move_after_gate = False
        self.download_override = {}

    def run(self, argv, **kwargs):
        self.calls.append((argv, kwargs))
        for value in ENV.values():
            if value.startswith("synthetic-"):
                assert value not in argv
        env = kwargs["env"]
        assert not any(key.startswith("APPLE_") or key.startswith("OP_") for key in env)
        assert "GITHUB_TOKEN" not in env and "BREW_GITHUB_TOKEN" not in env
        code, value = 0, ""
        if argv[0] == "git":
            if "push" in argv:
                self.events.append("refs")
                self.remote_release = self.remote_tag = SOURCE
                assert "--atomic" in argv and not any("force" in part for part in argv)
            elif argv[1] == "ls-remote":
                refs = {"refs/heads/release": self.remote_release, f"refs/tags/{VERSION}": self.remote_tag}
                value = "".join(f"{sha}\t{ref}\n" for ref, sha in refs.items() if sha and ref in argv[3:])
            elif argv[1] == "rev-parse":
                if "--verify" in argv:
                    code, value = (0, SOURCE) if self.local_tag else (1, "")
                else: value = SOURCE
            elif argv[1] == "tag": self.local_tag = True
            elif argv[1] == "describe": value = VERSION
            elif argv[1] == "merge-base" and self.move_after_gate: code = 1
        elif argv[:3] == ("gh", "release", "upload"):
            self.events.append("upload")
            name = Path(argv[4]).name
            if name == self.fail_upload:
                code = 1
            else:
                self.uploaded[name] = {"id": len(self.uploaded) + 1, "name": name,
                    "size": (self.assets / name).stat().st_size, "digest": "sha256:" + digest(self.assets / name)}
        elif argv[:3] == ("gh", "release", "download"):
            name = argv[argv.index("--pattern") + 1]
            destination = Path(argv[argv.index("--output") + 1])
            destination.write_bytes(self.download_override.get(name, (self.assets / name).read_bytes()))
        elif argv[:2] == ("gh", "api"):
            endpoint = argv[2]
            payload = json.loads(kwargs["input"]) if kwargs["input"] is not None else None
            method = argv[argv.index("--method") + 1]
            is_brew = "homebrew-tap" in endpoint
            assert env["GH_TOKEN"] == (ENV["BREW_GITHUB_TOKEN"] if is_brew else ENV["GITHUB_TOKEN"])
            if is_brew:
                if method == "PUT":
                    self.events.append("brew")
                    if self.fail_brew: code = 1
                    else: self.formula = base64.b64decode(payload["content"]).decode()
                    value = {}
                else: value = {"content": base64.b64encode(self.formula.encode()).decode(), "sha": "c" * 40}
            elif endpoint.endswith("/releases") and method == "POST":
                self.events.append("create")
                self.release = {**payload, "id": 1}
                value = self.release
            elif "/assets?" in endpoint:
                value = list(self.uploaded.values())
            elif method == "PATCH":
                self.events.append("publish")
                self.release.update(payload)
                value = self.release
            elif "/releases?" in endpoint:
                value = [self.release] if self.release else []
            else: raise AssertionError(argv)
        else: raise AssertionError(argv)
        output = value if isinstance(value, str) else json.dumps(value)
        return subprocess.CompletedProcess(argv, code, output, "synthetic command error" if code else "")

    def gate(self, root, source, receipt, commands):
        self.events.append("gate")


class PublisherTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        _, self.assets = fixture(self.root)
        notes = self.root / f"docs/releases/{VERSION}.md"
        notes.parent.mkdir(parents=True)
        self.notes = "A completely new desktop experience.\r\n\r\n### Workbench\r\nReviewed human prose.\r\n"
        notes.write_bytes(self.notes.encode())
        self.intent = {"schema_version": 1, "version": VERSION, "source_commit": "d" * 40, "base_tag": "", "release_date": "2026-11-05"}
        self.host = FakeHost(self.assets, self.notes)

    def publish(self, **kwargs):
        return publish(self.root, SOURCE, self.assets, self.root / "receipt.json",
            environment=ENV, runner=self.host.run, validator=lambda root: self.intent,
            gate=kwargs.pop("gate", self.host.gate), **kwargs)

    def test_build_validation_precedes_refs_and_gateway_precedes_all_uploads(self):
        self.publish()
        self.assertLess(self.host.events.index("refs"), self.host.events.index("gate"))
        self.assertLess(self.host.events.index("gate"), self.host.events.index("create"))
        self.assertLess(self.host.events.index("gate"), self.host.events.index("upload"))
        self.assertLess(self.host.events.index("publish"), self.host.events.index("brew"))
        self.assertEqual(self.host.release["body"], self.notes)
        self.assertFalse(self.host.release["draft"])
        self.assertEqual(set(self.host.uploaded), BUNDLE_FILES)
        self.assertIn(f"/download/{VERSION}/rhizome-darwin-arm64.tar.gz", self.host.formula)
        self.assertIn('bin.install "rzm"', self.host.formula)
        for argv, kwargs in self.host.calls:
            if argv[:2] == ("gh", "api") and "homebrew-tap" in argv[2]:
                self.assertEqual(kwargs["env"]["GH_TOKEN"], ENV["BREW_GITHUB_TOKEN"])
            else:
                self.assertEqual(kwargs["env"]["GH_TOKEN"], ENV["GITHUB_TOKEN"])

    def test_tampered_bundle_prevents_every_external_command(self):
        (self.assets / "Rhizome-macos-arm64.dmg").write_text("tampered")
        with self.assertRaises(ValueError): self.publish()
        self.assertEqual(self.host.events, [])
        self.assertEqual(self.host.calls, [])

    def test_gateway_failure_prevents_release_creation_and_upload(self):
        def fail(*args): raise PublishError("synthetic gateway failure")
        with self.assertRaisesRegex(PublishError, "gateway failure"): self.publish(gate=fail)
        self.assertEqual(self.host.events, ["refs"])

    def test_conflicting_refs_prevent_gateway_and_upload(self):
        self.host.remote_release = "b" * 40
        with self.assertRaisesRegex(PublishError, "conflicting"): self.publish()
        self.assertEqual(self.host.events, [])

    def test_partial_draft_resumes_only_missing_assets_and_preserves_human_notes(self):
        self.host.fail_upload = sorted(BUNDLE_FILES)[3]
        with self.assertRaises(PublishError): self.publish()
        retained = set(self.host.uploaded)
        self.assertTrue(retained)
        self.assertTrue(self.host.release["draft"])
        self.host.fail_upload = None
        self.host.events.clear()
        self.publish()
        self.assertEqual(self.host.events.count("upload"), len(BUNDLE_FILES - retained))
        self.assertEqual(self.host.release["body"], self.notes)
        self.assertFalse(self.host.release["draft"])

    def test_completed_release_is_verified_and_only_failed_brew_step_resumes(self):
        self.host.fail_brew = True
        with self.assertRaises(PublishError): self.publish()
        self.assertFalse(self.host.release["draft"])
        self.host.fail_brew = False
        self.host.events.clear()
        self.publish()
        self.assertEqual(self.host.events, ["gate", "brew"])
        self.host.events.clear()
        self.publish()
        self.assertEqual(self.host.events, [])

    def test_completed_release_brew_recovery_requires_fresh_gateway_verification(self):
        self.publish()
        self.host.formula = "old formula"
        self.host.events.clear()
        def fail(*args): raise PublishError("synthetic recovery gateway failure")
        with self.assertRaisesRegex(PublishError, "recovery gateway failure"):
            self.publish(gate=fail)
        self.assertEqual(self.host.events, [])
        self.assertEqual(self.host.formula, "old formula")

    def test_conflicting_existing_release_and_changed_uploaded_bytes_are_not_overwritten(self):
        self.publish()
        self.host.events.clear()
        self.host.release["body"] = "different human draft"
        with self.assertRaisesRegex(PublishError, "notes/source"): self.publish()
        self.assertEqual(self.host.events, [])
        self.host.release["body"] = self.notes
        self.host.uploaded[next(iter(BUNDLE_FILES))]["digest"] = "sha256:" + "0" * 64
        with self.assertRaisesRegex(PublishError, "differs"): self.publish()
        self.assertEqual(self.host.events, [])

    def test_missing_api_digest_downloads_and_hashes_existing_assets(self):
        self.publish()
        for item in self.host.uploaded.values(): item.pop("digest")
        self.host.events.clear()
        self.publish()
        self.assertEqual(self.host.events, [])
        name = next(iter(BUNDLE_FILES))
        self.host.download_override[name] = b"different downloaded bytes"
        with self.assertRaisesRegex(PublishError, "differs"): self.publish()
        self.assertEqual(self.host.events, [])

    def test_post_gateway_source_movement_blocks_upload(self):
        def gate(*args): self.host.move_after_gate = True
        with self.assertRaises(Exception): self.publish(gate=gate)
        self.assertFalse(self.host.release)
        self.assertNotIn("upload", self.host.events)

    def test_publication_requires_canonical_ci_environment(self):
        with self.assertRaisesRegex(PublishError, "only in"):
            publish(self.root, SOURCE, self.assets, self.root / "receipt", environment={}, runner=self.host.run)
        self.assertEqual(self.host.calls, [])


class ReceiptRecoveryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.receipt = Path(self.temporary.name) / "release-gateway.json"
        self.environment = {"GITHUB_RUN_ATTEMPT": "2", "GITHUB_RUN_ID": "123"}
        self.commands = Mock()

    def test_retry_restores_original_gateway_correlation(self):
        self.commands.api.return_value = {"artifacts": [{"name": "release-gateway-receipt", "expired": False}]}
        saved = '{"attempt":{"state":"dispatch_uncertain","request_id":"saved"}}'
        self.commands.run.side_effect = lambda argv: self.receipt.write_text(saved)
        restore_gateway_receipt(self.commands, self.receipt, self.environment)
        self.assertEqual(self.receipt.read_text(), saved)
        self.assertIn("download", self.commands.run.call_args.args[0])

    def test_lost_receipt_after_started_publisher_blocks_retry(self):
        self.commands.api.side_effect = [{"artifacts": []}, {"jobs": [{
            "name": "Publish approved release and Homebrew update", "steps": [{
                "name": "Publish approved release and update Homebrew", "started_at": "2026-11-05T12:00:00Z", "conclusion": "failure"}]}]}]
        with self.assertRaisesRegex(PublishError, "reconcile its deployment"):
            restore_gateway_receipt(self.commands, self.receipt, self.environment)
        self.commands.run.assert_not_called()

    def test_first_publisher_after_prior_build_failure_needs_no_receipt(self):
        self.commands.api.side_effect = [{"artifacts": []}, {"jobs": [{
            "name": "Publish approved release and Homebrew update", "steps": [{
                "name": "Publish approved release and update Homebrew", "started_at": None, "conclusion": "skipped"}]}]}]
        restore_gateway_receipt(self.commands, self.receipt, self.environment)
        self.commands.run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
