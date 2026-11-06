import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest

from run_clean import scoped_environment
import os

from ci_release_artifacts import (ArtifactError, BUNDLE_FILES, DESKTOP, GROUPS, PAYLOADS,
    assemble, binary_name, build_info, collect, digest, verify_bundle, write_manifest)

SOURCE = "a" * 40
VERSION = "v1.2.3"


def fixture(root):
    (root / "licenses").mkdir()
    (root / "licenses/dependency.txt").write_text("Synthetic third-party license")
    for name in ("LICENSE", "THIRD_PARTY_LICENSES.md"):
        (root / name).write_text("Synthetic license")
    installer = root / "scripts/install/install-rzm.sh"
    installer.parent.mkdir(parents=True)
    installer.write_text("#!/bin/sh\necho installer\n")
    inputs = []
    for platform, targets in GROUPS.items():
        directory = root / platform
        directory.mkdir()
        files = []
        for target in targets:
            name = f"bin/{target}/{binary_name(target)}"
            path = directory / name
            path.parent.mkdir(parents=True)
            path.write_bytes(("synthetic binary " + target).encode())
            path.chmod(0o755)
            files.append(name)
        if platform == "macos":
            (directory / "desktop").mkdir()
            for name in DESKTOP:
                (directory / "desktop" / name).write_text("synthetic package")
                files.append("desktop/" + name)
            (directory / "desktop" / DESKTOP[2]).write_text("synthetic signature\n")
            (directory / "desktop/latest.json").write_text(json.dumps({"version": VERSION[1:], "platforms": {
                "darwin-aarch64": {"signature": "synthetic signature", "url": f"https://github.com/atomicobject/rhizome/releases/download/{VERSION}/{DESKTOP[1]}"}}}))
        write_manifest(directory, SOURCE, VERSION, files, platform)
        inputs.append(directory)
    output = root / "publish"
    assemble(SOURCE, VERSION, inputs, output, root=root, scanner=lambda path: None)
    return inputs, output


class ArtifactTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)

    def test_assemble_preserves_names_licenses_and_complete_checksums(self):
        inputs, output = fixture(self.root)
        self.assertEqual({p.name for p in output.iterdir()}, BUNDLE_FILES)
        verify_bundle(SOURCE, VERSION, output)
        with tarfile.open(output / "rhizome-windows-amd64.tar.gz") as archive:
            self.assertEqual(set(archive.getnames()), {"rzm.exe", "LICENSE", "THIRD_PARTY_LICENSES.md", "licenses/dependency.txt"})
        checksums = (output / "checksums.txt").read_text()
        self.assertIn(digest(output / DESKTOP[0]) + "  " + DESKTOP[0], checksums)
        # Host ownership and modification times must not alter public release bytes.
        os.utime(inputs[0] / "bin/linux-arm64/rzm", (1, 1))
        os.utime(self.root / "LICENSE", (2, 2))
        repeat = self.root / "repeat"
        assemble(SOURCE, VERSION, inputs, repeat, root=self.root, scanner=lambda path: None)
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
        collect("linux", SOURCE, VERSION, dist, extras, self.root / "collected",
                scanner=lambda path: scanned.append(path.name), inspector=lambda *args: inspected.append(args[1:]))
        self.assertEqual(len(scanned), 3)
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
                assemble("b" * 40 if modification == "source" else SOURCE, VERSION, inputs, self.root / modification, root=self.root, scanner=lambda path: None)
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
            assemble(SOURCE, VERSION, inputs, self.root / "bad", root=self.root, scanner=lambda path: None)
        binary.unlink()
        binary.with_name("saved").rename(binary)
        feed = inputs[1] / "desktop/latest.json"
        value = json.loads(feed.read_text())
        value["platforms"]["darwin-aarch64"]["url"] = "https://example.test/other"
        feed.write_text(json.dumps(value))
        write_manifest(inputs[1], SOURCE, VERSION, ["bin/darwin-arm64/rzm", "bin/darwin-amd64/rzm", *("desktop/" + name for name in DESKTOP)], "macos")
        with self.assertRaisesRegex(ArtifactError, "updater feed"):
            assemble(SOURCE, VERSION, inputs, self.root / "bad-feed", root=self.root, scanner=lambda path: None)

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
                    verify_bundle(SOURCE, VERSION, output)

    def test_public_scan_failure_stops_assembly(self):
        inputs, _ = fixture(self.root)
        def reject(path): raise ArtifactError("synthetic scanner rejection")
        with self.assertRaisesRegex(ArtifactError, "scanner rejection"):
            assemble(SOURCE, VERSION, inputs, self.root / "rejected", root=self.root, scanner=reject)
        self.assertFalse((self.root / "rejected/release-manifest.json").exists())


if __name__ == "__main__":
    unittest.main()
