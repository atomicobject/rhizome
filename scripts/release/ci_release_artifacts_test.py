import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

from run_clean import scoped_environment
from assemble_desktop import assemble as assemble_desktop
from desktop_candidates_test import make_candidate
from desktop_candidates import TARGETS as DESKTOP_TARGETS
import os

from ci_release_artifacts import (ArtifactError, BUNDLE_FILES, DESKTOP, GROUPS, PAYLOADS,
    assemble, binary_name, build_info, collect, digest, prepare_native, validate_native_inputs,
    verify_bundle, write_manifest)

SOURCE = "a" * 40
VERSION = "v1.2.3"


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
    candidates, hashes = {}, {}
    parent = root / 'candidates'
    parent.mkdir()
    for target in DESKTOP_TARGETS:
        candidate, checksum = make_candidate(parent, target, production=not candidate_only or target == "darwin-aarch64")
        candidates[target], hashes[target] = candidate, checksum
    def signer(command, **kwargs):
        Path(command[-1] + '.sig').write_text('synthetic signature\n')
        return subprocess.CompletedProcess(command, 0, '', '')
    desktop = root / 'desktop-joined'
    assemble_desktop(list(candidates.values()), VERSION, SOURCE, hashes, desktop, root=root,
                     source={'TAURI_SIGNING_PRIVATE_KEY':'synthetic'}, runner=signer, verifier=ignore_signature,
                     production=not candidate_only, candidate_only=candidate_only)
    (desktop / DESKTOP[0]).write_text('synthetic DMG')
    inputs = []
    for platform, targets in GROUPS.items():
        directory = root / platform
        directory.mkdir()
        files = []
        for target in targets:
            name = f"bin/{target}/{binary_name(target)}"
            path = directory / name
            path.parent.mkdir(parents=True)
            desktop_target = {'darwin-arm64':'darwin-aarch64', 'linux-amd64':'linux-x86_64',
                              'windows-amd64':'windows-x86_64'}.get(target)
            path.write_bytes((candidates[desktop_target] / binary_name(target)).read_bytes() if desktop_target
                             else ("synthetic binary " + target).encode())
            path.chmod(0o755)
            files.append(name)
        if platform == "macos":
            (directory / "desktop").mkdir()
            for name in DESKTOP:
                (directory / "desktop" / name).write_bytes((desktop / name).read_bytes())
                files.append("desktop/" + name)
        else:
            import shutil
            target = 'linux-x86_64' if platform == 'linux' else 'windows-x86_64'
            shutil.copytree(candidates[target], directory / 'candidate')
            files.extend('candidate/' + path.name for path in candidates[target].iterdir())
        write_manifest(directory, SOURCE, VERSION, files, platform, candidate_only=candidate_only)
        inputs.append(directory)
    output = root / "publish"
    assemble(SOURCE, VERSION, inputs, output, root=root, scanner=lambda path: None, verifier=ignore_signature, candidate_only=candidate_only)
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
            self.assertEqual((self.root / "windows/bin/windows-amd64/rzm.exe").read_bytes(),
                             archive.extractfile("rzm.exe").read())
        self.assertFalse((self.root / "linux/bin/windows-amd64/rzm.exe").exists())
        checksums = (output / "checksums.txt").read_text()
        self.assertIn(digest(output / DESKTOP[0]) + "  " + DESKTOP[0], checksums)
        # Host ownership and modification times must not alter public release bytes.
        os.utime(inputs[0] / "bin/linux-arm64/rzm", (1, 1))
        os.utime(self.root / "LICENSE", (2, 2))
        repeat = self.root / "repeat"
        assemble(SOURCE, VERSION, inputs, repeat, root=self.root, scanner=lambda path: None, verifier=ignore_signature)
        self.assertEqual({name: digest(output / name) for name in BUNDLE_FILES},
                         {name: digest(repeat / name) for name in BUNDLE_FILES})

    def test_collection_and_public_content_scanning_use_only_selected_targets(self):
        dist, extras = self.root / "dist", self.root / "extras"
        extras.mkdir()
        for target in GROUPS["linux"]:
            goos, goarch = target.split("-")
            build = "rzm-linux" if goos == "linux" else "rzm-windows-amd64"
            path = dist / f"{build}_{goos}_{goarch}_v1" / binary_name(target)
            path.parent.mkdir(parents=True)
            path.write_text(target)
        (dist / "metadata.json").write_text(json.dumps({"version": VERSION[1:], "commit": SOURCE}))
        scanned, inspected = [], []
        candidate, _ = make_candidate(self.root, 'linux-x86_64')
        native = next(dist.glob('rzm-linux_linux_amd64*/rzm'))
        native.write_bytes((candidate / 'rzm').read_bytes())
        collect("linux", SOURCE, VERSION, dist, extras, self.root / "collected", candidate=candidate,
                scanner=lambda path: scanned.append(path.name), inspector=lambda *args: inspected.append(args[1:]))
        self.assertEqual(len(scanned), 2)
        self.assertEqual({row[0] for row in inspected}, set(GROUPS["linux"]))
        self.assertTrue(all(row[1:] == (SOURCE, VERSION) for row in inspected))
        (dist / "metadata.json").write_text(json.dumps({"version": "1.2.4", "commit": SOURCE}))
        with self.assertRaisesRegex(ArtifactError, "metadata version"):
            collect("linux", SOURCE, VERSION, dist, extras, self.root / "wrong-version",
                    scanner=lambda path: None, inspector=lambda *args: None)
        self.assertFalse((self.root / "wrong-version").exists())

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
            target = directory / "bin/linux-arm64/rzm"
            before = target.read_bytes()
            if modification == "tamper": target.write_text("changed")
            if modification == "extra": (directory / "unexpected.txt").write_text("extra")
            if modification == "missing": target.rename(target.with_name("moved"))
            with self.assertRaises((ArtifactError, FileNotFoundError)):
                assemble("b" * 40 if modification == "source" else SOURCE, VERSION, inputs, self.root / modification, root=self.root, scanner=lambda path: None, verifier=ignore_signature)
            if modification == "tamper": target.write_bytes(before)
            if modification == "extra": (directory / "unexpected.txt").unlink()
            if modification == "missing": target.with_name("moved").rename(target)
        self.assertTrue(output.exists())

    def test_symlink_and_bad_feed_are_rejected(self):
        inputs, _ = fixture(self.root)
        binary = inputs[0] / "bin/linux-arm64/rzm"
        binary.rename(binary.with_name("saved"))
        binary.symlink_to("saved")
        with self.assertRaisesRegex(ArtifactError, "symlink"):
            assemble(SOURCE, VERSION, inputs, self.root / "bad", root=self.root, scanner=lambda path: None, verifier=ignore_signature)
        binary.unlink()
        binary.with_name("saved").rename(binary)
        mac = next(path for path in inputs if path.name == 'macos')
        feed = mac / "desktop/latest.json"
        value = json.loads(feed.read_text())
        value["platforms"]["darwin-aarch64"]["url"] = "https://example.test/other"
        feed.write_text(json.dumps(value))
        write_manifest(mac, SOURCE, VERSION, ["bin/darwin-arm64/rzm", "bin/darwin-amd64/rzm", *("desktop/" + name for name in DESKTOP)], "macos")
        with self.assertRaisesRegex(ArtifactError, "updater feed"):
            assemble(SOURCE, VERSION, inputs, self.root / "bad-feed", root=self.root, scanner=lambda path: None, verifier=ignore_signature)

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

    def test_public_scan_failure_stops_assembly(self):
        inputs, _ = fixture(self.root)
        def reject(path): raise ArtifactError("synthetic scanner rejection")
        with self.assertRaisesRegex(ArtifactError, "scanner rejection"):
            assemble(SOURCE, VERSION, inputs, self.root / "rejected", root=self.root, scanner=reject, verifier=ignore_signature)
        self.assertFalse((self.root / "rejected/release-manifest.json").exists())

    def test_same_run_native_staging_binds_both_platforms_before_coordinator_signing(self):
        inputs, _ = fixture(self.root)
        native = [path for path in inputs if path.name in {"linux", "windows"}]
        output = self.root / "native-inputs"
        prepare_native(SOURCE, VERSION, native, output)
        validate_native_inputs(output, SOURCE, VERSION)
        self.assertTrue((output / "linux-x86_64/desktop-candidate.json").is_file())
        self.assertTrue((output / "windows-x86_64/desktop-candidate.json").is_file())
        with self.assertRaisesRegex(ArtifactError, "missing"):
            prepare_native(SOURCE, VERSION, native[:1], self.root / "missing-native")
        (output / "windows-x86_64/rzm.exe").write_bytes(b"changed")
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
        native = [path for path in inputs if path.name != "macos"]
        with self.assertRaisesRegex(ArtifactError, "manifest identity"):
            prepare_native(SOURCE, VERSION, native, self.root / "not-production")
        prepare_native(SOURCE, VERSION, native, self.root / "candidate-input", candidate_only=True)


if __name__ == "__main__":
    unittest.main()
