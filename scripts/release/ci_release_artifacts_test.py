import io
import json
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest

from run_clean import scoped_environment
from desktop_candidates_test import make_candidate
from desktop_candidates import CandidateError, REQUIRED_TARGETS, TARGETS
from join_desktop import join
import os

from ci_release_artifacts import (ArtifactError, BUNDLE_FILES, DESKTOP, DESKTOP_CLI, GROUPS, MAC_DMG, NATIVE_GROUPS,
    PAYLOADS, assemble, binary_name, build_info, collect, digest, prepare_native, validate_native_inputs,
    verify_bundle, write_manifest)

SOURCE = "a" * 40
VERSION = "v1.2.3"
JOINED = "desktop-joined"


def ignore_signature(*args, **kwargs):
    pass


def fixture(root, *, candidate_only=False):
    (root / "licenses").mkdir()
    (root / "licenses/dependency.txt").write_text("Synthetic third-party license")
    for name in ("LICENSE", "THIRD_PARTY_LICENSES.md"):
        (root / name).write_text("Synthetic license")
    installer = root / "scripts/install/install-rzm.sh"
    installer.parent.mkdir(parents=True)
    installer.write_text("#!/bin/sh\necho installer\n")
    config = root / "desktop/src-tauri/tauri.conf.json"
    config.parent.mkdir(parents=True, exist_ok=True)
    if not config.exists():
        config.write_text('{"plugins":{"updater":{"pubkey":"c3ludGhldGlj"}}}')
    parent = root / 'candidates'
    parent.mkdir()
    candidates = {target: make_candidate(parent, target, production=not candidate_only or target == "darwin-aarch64")[0]
                  for target in REQUIRED_TARGETS}
    inputs = []
    for platform, targets in GROUPS.items():
        directory = root / platform
        directory.mkdir()
        files = []
        for target in targets:
            name = f"bin/{target}/{binary_name(target)}"
            path = directory / name
            path.parent.mkdir(parents=True)
            desktop_target = {cli: desktop for desktop, cli in DESKTOP_CLI.items()}.get(target)
            path.write_bytes((candidates[desktop_target] / binary_name(target)).read_bytes() if desktop_target
                             else ("synthetic binary " + target).encode())
            path.chmod(0o755)
            files.append(name)
        if platform == "macos":
            (directory / "desktop").mkdir()
            (directory / "desktop" / MAC_DMG).write_text("synthetic DMG")
            files.append("desktop/" + MAC_DMG)
        target = NATIVE_GROUPS[platform]
        shutil.copytree(candidates[target], directory / 'candidate')
        files.extend('candidate/' + path.name for path in candidates[target].iterdir())
        write_manifest(directory, SOURCE, VERSION, files, platform, candidate_only=candidate_only)
        inputs.append(directory)
    # The protected coordinator: verify every group, then sign each package once.
    signed = []
    def signer(command, **kwargs):
        signed.append(Path(command[-1]).name)
        Path(command[-1] + '.sig').write_text('synthetic signature\n')
        return subprocess.CompletedProcess(command, 0, '', '')
    native = root / 'native'
    prepare_native(SOURCE, VERSION, inputs, native, candidate_only=candidate_only)
    join(native, SOURCE, VERSION, root / JOINED, environ={'TAURI_SIGNING_PRIVATE_KEY': 'synthetic'}, root=root,
         runner=signer, verifier=ignore_signature, candidate_only=candidate_only)
    assert sorted(signed) == sorted(TARGETS[target]["asset"] for target in REQUIRED_TARGETS), signed
    output = root / "publish"
    assemble(SOURCE, VERSION, inputs, root / JOINED, output, root=root, scanner=lambda path: None,
             verifier=ignore_signature, candidate_only=candidate_only)
    return inputs, output


class ArtifactTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def test_assemble_preserves_names_licenses_and_complete_checksums(self):
        inputs, output = fixture(self.root)
        self.assertEqual({p.name for p in output.iterdir()}, BUNDLE_FILES)
        verify_bundle(SOURCE, VERSION, output, root=self.root, verifier=ignore_signature)
        with tarfile.open(output / "rhizome-windows-amd64.tar.gz") as archive:
            self.assertEqual(set(archive.getnames()), {"rzm.exe", "LICENSE", "THIRD_PARTY_LICENSES.md", "licenses/dependency.txt"})
            self.assertEqual((self.root / "linux/bin/windows-amd64/rzm.exe").read_bytes(),
                             archive.extractfile("rzm.exe").read())
        self.assertNotIn("Rhizome-windows-x64-setup.exe", BUNDLE_FILES)
        checksums = (output / "checksums.txt").read_text()
        self.assertIn(digest(output / DESKTOP[0]) + "  " + DESKTOP[0], checksums)
        # Host ownership and modification times must not alter public release bytes.
        os.utime(inputs[0] / "bin/linux-amd64/rzm", (1, 1))
        os.utime(self.root / "LICENSE", (2, 2))
        repeat = self.root / "repeat"
        assemble(SOURCE, VERSION, inputs, self.root / JOINED, repeat, root=self.root, scanner=lambda path: None, verifier=ignore_signature)
        self.assertEqual({name: digest(output / name) for name in BUNDLE_FILES},
                         {name: digest(repeat / name) for name in BUNDLE_FILES})

    def test_collection_and_public_content_scanning_use_only_selected_targets(self):
        dist, extras = self.root / "dist", self.root / "extras"
        extras.mkdir()
        for target in GROUPS["linux"]:
            goos, goarch = target.split("-")
            path = dist / f"rzm-{target}_{goos}_{goarch}_v1" / binary_name(target)
            path.parent.mkdir(parents=True)
            path.write_text(target)
        (dist / "metadata.json").write_text(json.dumps({"version": VERSION[1:], "commit": SOURCE}))
        scanned, inspected = [], []
        candidate, _ = make_candidate(self.root, 'linux-x86_64')
        native = next(dist.glob('rzm-linux-amd64_linux_amd64*/rzm'))
        native.write_bytes((candidate / 'rzm').read_bytes())
        collect("linux", SOURCE, VERSION, dist, extras, self.root / "collected", candidate=candidate,
                scanner=lambda path: scanned.append(path.name), inspector=lambda *args: inspected.append(args[1:]))
        self.assertEqual(len(scanned), len(GROUPS["linux"]))
        self.assertEqual({row[0] for row in inspected}, set(GROUPS["linux"]))
        self.assertTrue(all(row[1:] == (SOURCE, VERSION) for row in inspected))
        (dist / "metadata.json").write_text(json.dumps({"version": "1.2.4", "commit": SOURCE}))
        with self.assertRaisesRegex(ArtifactError, "metadata version"):
            collect("linux", SOURCE, VERSION, dist, extras, self.root / "wrong-version",
                    scanner=lambda path: None, inspector=lambda *args: None)
        self.assertFalse((self.root / "wrong-version").exists())

    def test_mac_collection_carries_its_dmg_and_unsigned_updater_candidate(self):
        dist, extras = self.root / "dist", self.root / "extras"
        candidate, _ = make_candidate(self.root, "darwin-aarch64")
        for target in GROUPS["macos"]:
            path = dist / f"rzm-darwin_{target.replace('-', '_')}_v1" / "rzm"
            path.parent.mkdir(parents=True)
            path.write_bytes((candidate / "rzm").read_bytes() if target == "darwin-arm64" else b"synthetic amd64")
        (dist / "metadata.json").write_text(json.dumps({"version": VERSION[1:], "commit": SOURCE}))
        extras.mkdir()
        (extras / MAC_DMG).write_text("synthetic DMG")
        collected = collect("macos", SOURCE, VERSION, dist, extras, self.root / "collected", candidate=candidate,
                            scanner=lambda path: None, inspector=lambda *args: None)
        self.assertIn("candidate/Rhizome-macos-arm64.app.tar.gz", collected["files"])
        self.assertNotIn("desktop/latest.json", collected["files"])
        (candidate / "Rhizome-macos-arm64.app.tar.gz.sig").write_text("a producer must not sign")
        with self.assertRaisesRegex(ArtifactError, "unexpected producer files"):
            collect("macos", SOURCE, VERSION, dist, extras, self.root / "signed", candidate=candidate,
                    scanner=lambda path: None, inspector=lambda *args: None)

    def test_go_build_info_checks_actual_target_source_cleanliness_and_version(self):
        text = 'binary: go1.24.2\n\tbuild\tGOOS=linux\n\tbuild\tGOARCH=arm64\n\tbuild\tvcs.revision=' + SOURCE + '\n\tbuild\tvcs.modified=false\n\tbuild\t-ldflags="-s -w -X github.com/atomicobject/rhizome/pkg/vault/version.Version=1.2.3"\n'
        def runner(argv, **kwargs):
            self.assertEqual(argv[:3], ["go", "version", "-m"])
            self.assertNotIn("GH_TOKEN", kwargs["env"])
            return subprocess.CompletedProcess(argv, 0, text, "")
        build_info(self.root / "rzm", "linux-arm64", SOURCE, VERSION, runner)
        for target, source, version in (("linux-amd64", SOURCE, VERSION), ("linux-arm64", "b" * 40, VERSION), ("linux-arm64", SOURCE, "v1.2.4")):
            with self.assertRaises(ArtifactError):
                build_info(self.root / "rzm", target, source, version, runner)
        text = text.replace("vcs.modified=false", "vcs.modified=true")
        def dirty_runner(argv, **kwargs):
            if argv[0] == "go": return runner(argv, **kwargs)
            return subprocess.CompletedProcess(argv, 0, "pkg/app/web/assets/dist/index.html\0", "")
        build_info(self.root / "rzm", "linux-arm64", SOURCE, VERSION, dirty_runner)
        def changed_source(argv, **kwargs):
            if argv[0] == "go": return runner(argv, **kwargs)
            return subprocess.CompletedProcess(argv, 0, "pkg/app/desktop/controller.go\0", "")
        with self.assertRaisesRegex(ArtifactError, "non-derived"):
            build_info(self.root / "rzm", "linux-arm64", SOURCE, VERSION, changed_source)

    def test_real_go_binary_build_information_matches_the_collector_contract(self):
        root = self.root
        (root / "go.mod").write_text("module github.com/atomicobject/rhizome\n\ngo 1.24\n")
        (root / ".gitignore").write_text("bin/\n")
        (root / "pkg/vault/version").mkdir(parents=True)
        (root / "pkg/vault/version/version.go").write_text('package version\nvar Version = "dev"\n')
        (root / "main.go").write_text('package main\nimport ("fmt"; "github.com/atomicobject/rhizome/pkg/vault/version")\nfunc main() { fmt.Println(version.Version) }\n')
        env = scoped_environment(os.environ, "build")
        env.update(CGO_ENABLED="0", GOTOOLCHAIN="local")
        for argv in (("git", "init", "-q"), ("git", "config", "user.name", "Synthetic Test"),
                     ("git", "config", "user.email", "synthetic@example.test"), ("git", "add", "."),
                     ("git", "commit", "-qm", "fixture")):
            subprocess.run(argv, cwd=root, env=env, check=True, capture_output=True)
        source = subprocess.run(("git", "rev-parse", "HEAD"), cwd=root, env=env, check=True, capture_output=True, text=True).stdout.strip()
        binary = root / "bin/rzm"
        subprocess.run(("go", "build", "-trimpath", "-ldflags", "-X github.com/atomicobject/rhizome/pkg/vault/version.Version=1.2.3", "-o", str(binary), "."), cwd=root, env=env, check=True, capture_output=True, timeout=60)
        target = subprocess.run(("go", "env", "GOOS", "GOARCH"), cwd=root, env=env, check=True, capture_output=True, text=True).stdout.split()
        build_info(binary, "-".join(target), source, VERSION)

    def test_missing_extra_tampered_and_cross_source_inputs_fail(self):
        inputs, output = fixture(self.root)
        for modification in ("tamper", "extra", "missing", "source"):
            directory = inputs[0]
            target = directory / "bin/linux-amd64/rzm"
            before = target.read_bytes()
            if modification == "tamper": target.write_text("changed")
            if modification == "extra": (directory / "unexpected.txt").write_text("extra")
            if modification == "missing": target.rename(target.with_name("moved"))
            with self.assertRaises((ArtifactError, FileNotFoundError)):
                assemble("b" * 40 if modification == "source" else SOURCE, VERSION, inputs, self.root / JOINED, self.root / modification, root=self.root, scanner=lambda path: None, verifier=ignore_signature)
            if modification == "tamper": target.write_bytes(before)
            if modification == "extra": (directory / "unexpected.txt").unlink()
            if modification == "missing": target.with_name("moved").rename(target)
        self.assertTrue(output.exists())

    def test_symlink_and_bad_feed_are_rejected(self):
        inputs, _ = fixture(self.root)
        binary = inputs[0] / "bin/linux-amd64/rzm"
        binary.rename(binary.with_name("saved"))
        binary.symlink_to("saved")
        with self.assertRaisesRegex(ArtifactError, "symlink"):
            assemble(SOURCE, VERSION, inputs, self.root / JOINED, self.root / "bad", root=self.root, scanner=lambda path: None, verifier=ignore_signature)
        binary.unlink()
        binary.with_name("saved").rename(binary)
        feed = self.root / JOINED / "latest.json"
        value = json.loads(feed.read_text())
        value["platforms"]["darwin-aarch64"]["url"] = "https://example.test/other"
        feed.write_text(json.dumps(value))
        with self.assertRaisesRegex(ArtifactError, "updater feed"):
            assemble(SOURCE, VERSION, inputs, self.root / JOINED, self.root / "bad-feed", root=self.root, scanner=lambda path: None, verifier=ignore_signature)

    def test_rehashed_archive_with_traversal_is_rejected(self):
        _, output = fixture(self.root)
        path = output / "rhizome-linux-amd64.tar.gz"
        for unsafe in ("licenses/../outside", "licenses/\\outside", "licenses/C:outside", "licenses/./outside"):
            with self.subTest(name=unsafe):
                with tarfile.open(path, "w:gz") as archive:
                    for name in ("rzm", "LICENSE", "THIRD_PARTY_LICENSES.md", "licenses/dependency.txt", unsafe):
                        item = tarfile.TarInfo(name)
                        item.size = 1
                        archive.addfile(item, io.BytesIO(b"x"))
                write_manifest(output, SOURCE, VERSION, PAYLOADS)
                checksums = {name: digest(output / name) for name in (*PAYLOADS, "release-manifest.json")}
                (output / "checksums.txt").write_text("".join(f"{checksums[name]}  {name}\n" for name in sorted(checksums)))
                with self.assertRaisesRegex(ArtifactError, "archive contents"):
                    verify_bundle(SOURCE, VERSION, output, root=self.root, verifier=ignore_signature)

    def test_each_desktop_candidate_must_carry_its_groups_archived_cli(self):
        inputs, _ = fixture(self.root)
        for group, cli in (("linux-arm64", "bin/linux-arm64/rzm"), ("macos", "bin/darwin-arm64/rzm")):
            with self.subTest(group=group):
                directory = next(path for path in inputs if path.name == group)
                (directory / cli).write_bytes(b"a different build")
                files = json.loads((directory / "manifest.json").read_text())["files"]
                write_manifest(directory, SOURCE, VERSION, files, group)
                with self.assertRaisesRegex(CandidateError, "canonical CLI"):
                    assemble(SOURCE, VERSION, inputs, self.root / JOINED, self.root / ("mismatched-" + group),
                             root=self.root, scanner=lambda path: None, verifier=ignore_signature)
                with self.assertRaisesRegex(CandidateError, "canonical CLI"):
                    prepare_native(SOURCE, VERSION, inputs, self.root / ("native-" + group))

    def test_public_scan_failure_stops_assembly(self):
        inputs, _ = fixture(self.root)
        def reject(path): raise ArtifactError("synthetic scanner rejection")
        with self.assertRaisesRegex(ArtifactError, "scanner rejection"):
            assemble(SOURCE, VERSION, inputs, self.root / JOINED, self.root / "rejected", root=self.root, scanner=reject, verifier=ignore_signature)
        self.assertFalse((self.root / "rejected/release-manifest.json").exists())

    def test_same_run_native_staging_binds_every_native_platform_before_coordinator_signing(self):
        inputs, _ = fixture(self.root)
        output = self.root / "native-inputs"
        prepare_native(SOURCE, VERSION, inputs, output)
        validate_native_inputs(output, SOURCE, VERSION)
        for target in REQUIRED_TARGETS:
            self.assertTrue((output / target / "desktop-candidate.json").is_file())
        without_mac = [path for path in inputs if path.name != "macos"]
        with self.assertRaisesRegex(ArtifactError, "missing"):
            prepare_native(SOURCE, VERSION, without_mac, self.root / "missing-native")
        (output / "linux-aarch64/rzm").write_bytes(b"changed")
        with self.assertRaisesRegex(ArtifactError, "changed"):
            validate_native_inputs(output, SOURCE, VERSION)

    def test_signature_verification_failure_blocks_the_complete_bundle(self):
        _, output = fixture(self.root)
        def reject(*args, **kwargs):
            raise ArtifactError("invalid updater signature")
        with self.assertRaisesRegex(ArtifactError, "updater signature"):
            verify_bundle(SOURCE, VERSION, output, root=self.root, verifier=reject)

    def test_candidate_bundle_requires_explicit_mode_and_production_rejects_unsigned_group(self):
        inputs, output = fixture(self.root, candidate_only=True)
        with self.assertRaisesRegex(ArtifactError, "manifest identity"):
            verify_bundle(SOURCE, VERSION, output, root=self.root, verifier=ignore_signature)
        candidate = verify_bundle(SOURCE, VERSION, output, root=self.root, verifier=ignore_signature, candidate_only=True)
        self.assertIs(candidate["candidate_only"], True)
        desktop = json.loads((output / "desktop-assembly.json").read_text())
        self.assertIs(desktop["production"], False)
        self.assertIs(desktop["candidateOnly"], True)
        with self.assertRaisesRegex(ArtifactError, "manifest identity"):
            prepare_native(SOURCE, VERSION, inputs, self.root / "not-production")
        prepare_native(SOURCE, VERSION, inputs, self.root / "candidate-input", candidate_only=True)


if __name__ == "__main__":
    unittest.main()
