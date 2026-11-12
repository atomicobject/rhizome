import base64
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import Mock

from ci_release_artifacts import BUNDLE_FILES, digest, verify_bundle
from ci_release_artifacts_test import SOURCE, VERSION, fixture
from ci_release_publish import Commands, PublishError, publish, restore_gateway_receipt
from release_advertise import API as PUBLIC_API, DOWNLOAD, AdvertisementError
from release_homebrew import recipe_contents

ENV = {"GITHUB_ACTIONS": "true", "GITHUB_REF": "refs/heads/main", "GITHUB_REPOSITORY": "atomicobject/rhizome",
       "GITHUB_SHA": SOURCE, "GITHUB_EVENT_NAME": "push",
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
        inventory = {name: {"sha256": digest(assets / name)} for name in BUNDLE_FILES}
        self.old_recipes = {name: content.decode() for name, content in recipe_contents("0.0.1", inventory).items()}
        self.tap_files = {"rhizome.rb": self.old_recipes["rhizome.rb"]}
        self.latest_id = None
        self.fail_anonymous = False
        self.fail_upload = None
        self.fail_brew = False
        self.fail_brew_path = None
        self.fail_tap_read = False
        self.truncated_tap = False
        self.change_tap_during_read = False
        self.move_after_gate = False
        self.download_override = {}

    @property
    def formula(self):
        return self.tap_files["rhizome.rb"]

    @formula.setter
    def formula(self, content):
        self.tap_files["rhizome.rb"] = content

    def tap_sha(self, path):
        return hashlib.sha1(self.tap_files[path].encode()).hexdigest()

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
                    "size": (self.assets / name).stat().st_size, "digest": "sha256:" + digest(self.assets / name), "state": "uploaded"}
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
                if self.fail_tap_read and method == "GET":
                    code, value = 1, {"message": "synthetic forbidden"}
                elif "/git/trees/" in endpoint:
                    value = {"truncated": self.truncated_tap, "tree": [
                        {"path": path, "type": "blob", "mode": "100644", "sha": self.tap_sha(path)}
                        for path in self.tap_files]}
                elif method == "PUT":
                    path = endpoint.split("/contents/", 1)[1]
                    self.events.append("brew")
                    if path in self.tap_files:
                        assert payload["sha"] == self.tap_sha(path)
                    else:
                        assert "sha" not in payload
                    if self.fail_brew or self.fail_brew_path == path: code = 1
                    else: self.tap_files[path] = base64.b64decode(payload["content"]).decode()
                    value = {}
                else:
                    path = endpoint.split("/contents/", 1)[1].split("?", 1)[0]
                    value = {"content": base64.b64encode(self.tap_files[path].encode()).decode(), "sha": self.tap_sha(path),
                             "type": "file", "path": path, "encoding": "base64"}
                    if self.change_tap_during_read:
                        value["sha"] = "0" * 40
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

    def api(self, endpoint, *, method="GET", data=None):
        if endpoint.startswith("releases?"):
            return [dict(self.release)] if self.release else []
        if endpoint == "releases/1/assets?per_page=100":
            return [dict(asset) for asset in self.uploaded.values()]
        if endpoint == "releases/1" and method == "GET":
            return dict(self.release)
        if endpoint == "git/ref/tags/" + VERSION:
            return {"object": {"type": "commit", "sha": SOURCE}}
        if endpoint == "releases/1" and method == "PATCH":
            self.release.update(data)
            if data.get("draft") is False:
                self.events.append("publish")
            elif data.get("draft") is True:
                self.events.append("recover")
                self.latest_id = None
            if data.get("make_latest") == "true":
                self.latest_id = 1
                self.events.append("promote")
            return dict(self.release)
        raise AssertionError((method, endpoint))

    def latest(self):
        return dict(self.release) if self.latest_id else None

    def download(self, url, path, *, size=None, authenticated=False, limit=None):
        if authenticated:
            identifier = int(url.rsplit("/", 1)[1])
            name = next(name for name, value in self.uploaded.items() if value["id"] == identifier)
        elif "/latest/download/" in url:
            if not self.latest_id:
                raise AdvertisementError("no latest release")
            name = "latest.json"
        else:
            if self.fail_anonymous:
                raise AdvertisementError("synthetic anonymous download failure")
            assert not self.release["draft"]
            assert url.startswith(f"{DOWNLOAD}/download/{VERSION}/")
            name = url.rsplit("/", 1)[1]
        content = self.download_override.get(name, (self.assets / name).read_bytes())
        if size is not None and len(content) != size:
            raise AdvertisementError("unexpected downloaded size")
        path.write_bytes(content)

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
            environment=kwargs.pop("environment", ENV), runner=self.host.run, validator=lambda root: self.intent,
            gate=kwargs.pop("gate", self.host.gate), api_factory=lambda commands: self.host,
            signature_verifier=lambda *a, **k: None,
            bundle_verifier=kwargs.pop("bundle_verifier", lambda source, version, assets: verify_bundle(source, version, assets, root=self.root, verifier=lambda *a, **k: None)), **kwargs)

    def test_build_validation_precedes_refs_and_gateway_precedes_all_uploads(self):
        self.publish()
        self.assertLess(self.host.events.index("refs"), self.host.events.index("gate"))
        self.assertLess(self.host.events.index("gate"), self.host.events.index("create"))
        self.assertLess(self.host.events.index("gate"), self.host.events.index("upload"))
        self.assertLess(self.host.events.index("publish"), self.host.events.index("brew"))
        self.assertLess(self.host.events.index("promote"), self.host.events.index("brew"))
        self.assertEqual(self.host.release["body"], self.notes)
        self.assertFalse(self.host.release["draft"])
        self.assertEqual(set(self.host.uploaded), BUNDLE_FILES)
        self.assertIn(f"/download/{VERSION}/rhizome-darwin-arm64.tar.gz", self.host.formula)
        self.assertIn('bin.install "rzm"', self.host.formula)
        cask = self.host.tap_files["Casks/rhizome-desktop.rb"]
        self.assertIn(f'  version "{VERSION.removeprefix("v")}"', cask)
        self.assertIn(f'  sha256 "{digest(self.assets / "Rhizome-macos-arm64.dmg")}"', cask)
        self.assertIn('/releases/download/v#{version}/Rhizome-macos-arm64.dmg"', cask)
        self.assertIn('auto_updates true', cask)
        self.assertIn('depends_on arch: :arm64', cask)
        self.assertIn('depends_on macos: :monterey', cask)
        self.assertIn('app "Rhizome.app"', cask)
        self.assertIn('binary "#{appdir}/Rhizome.app/Contents/MacOS/rzm"', cask)
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

    def test_anonymous_asset_failure_defers_both_recipes_and_retry_does_not_reupload(self):
        self.host.fail_anonymous = True
        with self.assertRaisesRegex(PublishError, "advertisement failed"):
            self.publish()
        self.assertFalse(self.host.release["draft"])
        self.assertIsNone(self.host.latest_id)
        self.assertNotIn("brew", self.host.events)
        self.host.fail_anonymous = False
        self.host.events.clear()
        self.publish()
        self.assertNotIn("upload", self.host.events)
        self.assertLess(self.host.events.index("promote"), self.host.events.index("brew"))
        self.assertIn("Casks/rhizome-desktop.rb", self.host.tap_files)

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
        self.assertEqual(self.host.events, ["gate", "brew", "brew"])
        self.host.events.clear()
        self.publish()
        self.assertEqual(self.host.events, [])

    def test_cask_creation_and_existing_recipe_updates_use_conditional_writes(self):
        self.publish()
        puts = [(argv[2], json.loads(kwargs["input"])) for argv, kwargs in self.host.calls
                if argv[:2] == ("gh", "api") and "homebrew-tap" in argv[2] and "PUT" in argv]
        self.assertEqual(len(puts), 2)
        self.assertIn("sha", puts[0][1])
        self.assertTrue(puts[1][0].endswith("/Casks/rhizome-desktop.rb"))
        self.assertNotIn("sha", puts[1][1])
        self.host.calls.clear()
        self.host.tap_files["Casks/rhizome-desktop.rb"] = self.host.old_recipes["Casks/rhizome-desktop.rb"]
        self.publish()
        puts = [(argv[2], json.loads(kwargs["input"])) for argv, kwargs in self.host.calls
                if argv[:2] == ("gh", "api") and "homebrew-tap" in argv[2] and "PUT" in argv]
        self.assertEqual(len(puts), 1)
        self.assertIn("sha", puts[0][1])

    def test_partial_formula_success_retries_only_cask_after_fresh_gateway(self):
        self.host.fail_brew_path = "Casks/rhizome-desktop.rb"
        with self.assertRaises(PublishError): self.publish()
        saved_formula = self.host.formula
        self.assertNotEqual(saved_formula, "old formula")
        self.assertNotIn("Casks/rhizome-desktop.rb", self.host.tap_files)
        self.assertFalse(self.host.release["draft"])
        self.host.fail_brew_path = None
        self.host.events.clear()
        self.host.calls.clear()
        self.publish()
        self.assertEqual(self.host.events, ["gate", "brew"])
        self.assertEqual(self.host.formula, saved_formula)
        self.assertIn("Casks/rhizome-desktop.rb", self.host.tap_files)
        self.host.events.clear()
        self.publish()
        self.assertEqual(self.host.events, [])

    def test_missing_cask_recovery_cannot_write_before_fresh_gateway(self):
        self.publish()
        del self.host.tap_files["Casks/rhizome-desktop.rb"]
        self.host.events.clear()
        def fail(*args): raise PublishError("synthetic recovery gateway failure")
        with self.assertRaisesRegex(PublishError, "recovery gateway failure"):
            self.publish(gate=fail)
        self.assertNotIn("Casks/rhizome-desktop.rb", self.host.tap_files)
        self.assertEqual(self.host.events, [])

    def test_tap_auth_failure_or_incomplete_tree_never_becomes_recipe_creation(self):
        self.publish()
        del self.host.tap_files["Casks/rhizome-desktop.rb"]
        for failure in ("auth", "truncated"):
            with self.subTest(failure=failure):
                self.host.fail_tap_read = failure == "auth"
                self.host.truncated_tap = failure == "truncated"
                self.host.events.clear()
                with self.assertRaises(PublishError): self.publish()
                self.assertEqual(self.host.events, [])
                self.assertNotIn("Casks/rhizome-desktop.rb", self.host.tap_files)

    def test_changed_tap_recipe_metadata_blocks_every_homebrew_write(self):
        self.publish()
        self.host.formula = "previous formula"
        self.host.change_tap_during_read = True
        self.host.events.clear()
        with self.assertRaisesRegex(PublishError, "changed Homebrew recipe"):
            self.publish()
        self.assertEqual(self.host.events, [])
        self.assertEqual(self.host.formula, "previous formula")

    def test_completed_release_brew_recovery_requires_fresh_gateway_verification(self):
        self.publish()
        self.host.formula = self.host.old_recipes["rhizome.rb"]
        self.host.events.clear()
        def fail(*args): raise PublishError("synthetic recovery gateway failure")
        with self.assertRaisesRegex(PublishError, "recovery gateway failure"):
            self.publish(gate=fail)
        self.assertEqual(self.host.events, [])
        self.assertEqual(self.host.formula, self.host.old_recipes["rhizome.rb"])

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

    def test_other_events_or_a_different_push_sha_cannot_publish(self):
        for changes in ({"GITHUB_EVENT_NAME": "pull_request"}, {"GITHUB_EVENT_NAME": "schedule"},
                        {"GITHUB_SHA": "b" * 40}, {"GITHUB_REF": "refs/pull/131/merge"}):
            with self.subTest(changes=changes), self.assertRaisesRegex(PublishError, "only in"):
                publish(self.root, SOURCE, self.assets, self.root / "receipt", environment={**ENV, **changes}, runner=self.host.run)
        self.assertEqual(self.host.calls, [])

    def test_dispatch_for_a_merged_release_pr_may_run_at_a_later_main_sha(self):
        self.publish(environment={**ENV, "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_SHA": "b" * 40})
        self.assertIn("upload", self.host.events)

    def test_candidate_bundle_is_refused_before_refs_gateway_or_uploads(self):
        path = self.assets / "release-manifest.json"
        manifest = json.loads(path.read_text())
        manifest["candidate_only"] = True
        path.write_text(json.dumps(manifest))
        with self.assertRaisesRegex(PublishError, "candidate-only"):
            self.publish(bundle_verifier=lambda *args: None)
        self.assertEqual(self.host.calls, [])
        self.assertEqual(self.host.events, [])


class ReceiptRecoveryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.receipt = Path(self.temporary.name) / "release-gateway.json"
        self.environment = {"GITHUB_RUN_ATTEMPT": "2", "GITHUB_RUN_ID": "123"}
        self.commands = Mock()

    def test_gateway_requests_remain_bounded_and_timeout_diagnostics_are_private(self):
        def runner(argv, **kwargs):
            self.assertEqual(kwargs["timeout"], 120)
            raise subprocess.TimeoutExpired(argv, 120, stderr="synthetic secret response")
        commands = Commands(self.receipt.parent, ENV, runner)
        with self.assertRaises(PublishError) as result:
            commands.text(("gh", "api", "synthetic-endpoint"))
        self.assertNotIn("synthetic secret response", str(result.exception))

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
