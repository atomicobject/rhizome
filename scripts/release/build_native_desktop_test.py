from __future__ import annotations

import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

from build_native_desktop import build
from desktop_candidates import CandidateError, MANIFEST, ROOT, TARGETS, sha256
from desktop_candidates_test import COMMIT, VERSION


class NativeBuilderTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        for platform in ("windows", "linux"):
            destination = self.root / f"desktop/src-tauri/tauri.release.{platform}.conf.json"
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / destination.relative_to(self.root), destination)
        self.rzm = self.root / "canonical/rzm.exe"
        self.rzm.parent.mkdir()
        self.rzm.write_bytes(b"canonical executable")
        self.digest = sha256(self.rzm)
        self.output = self.root / "candidate"
        self.calls = []
        self.extracted_gateway = False
        self.prebuilt_frontend = False

    def tearDown(self):
        self.temporary.cleanup()

    def runner(self, command, **kwargs):
        self.calls.append((command, kwargs))
        stdout = "synthetic tool 1"
        if command[:2] == ["git", "rev-parse"]:
            stdout = COMMIT
        elif command[-1] == "--version" and Path(command[0]).resolve() == self.rzm.resolve():
            stdout = "rzm version v" + VERSION
        elif command[0] == "powershell.exe":
            if "-Imports" in command:
                stdout = json.dumps({"host": {"os": "Windows Server 2025", "version": "10.0.26100",
                                               "systemDirectory": "C:\\Windows\\System32"},
                    "imports": [{"name": "kernel32.dll", "path": "C:\\Windows\\System32\\kernel32.dll",
                                 "origin": "system", "signature": "microsoft-valid"}], "probeVersion": "rzm version v" + VERSION})
            else:
                stdout = '{"status":"authenticode","thumbprint":"synthetic-thumbprint"}'
        elif command[0] == "objdump":
            stdout = "DLL Name: KERNEL32.dll\n"
        elif command[0] == "cat":
            stdout = 'ID=ubuntu\nVERSION_ID="24.04"\n'
        elif command[0] == "getconf":
            stdout = "glibc 2.39"
        elif command[:2] == ["readelf", "-d"]:
            stdout = "0x0000000000000001 (NEEDED) Shared library: [libc.so.6]"
        elif command[:2] == ["readelf", "-l"]:
            stdout = "[Requesting program interpreter: /lib64/ld-linux-x86-64.so.2]"
        elif command[:2] == ["readelf", "--version-info"]:
            stdout = "Name: GLIBC_2.17"
        elif command[0] == "ldd":
            stdout = "libc.so.6 => /usr/lib/x86_64-linux-gnu/libc.so.6 (0x1234)"
        elif command[-1] == "--version" and Path(command[0]).name == "rzm":
            stdout = "rzm version v" + VERSION
        elif "bundle" in command and command[0] in {"npm", "node"}:
            target = command[command.index("--bundles") + 1]
            directory = self.root / "desktop/src-tauri/target/release/bundle" / target
            directory.mkdir(parents=True)
            file = "Rhizome_1.2.3_x64-setup.exe" if target == "nsis" else "Rhizome_1.2.3_amd64.AppImage"
            (directory / file).write_bytes(b"native installer")
        elif command[0] == "7z":
            directory = Path(next(v[2:] for v in command if v.startswith("-o")))
            (directory / "rzm.exe").write_bytes(self.rzm.read_bytes())
            if self.extracted_gateway:
                (directory / "team-api.exe").write_bytes(b"unexpected")
        elif command[-1] == "--appimage-extract":
            directory = kwargs["cwd"] / "squashfs-root/usr/bin"
            directory.mkdir(parents=True)
            (directory / "rzm").write_bytes(self.rzm.read_bytes())
        return subprocess.CompletedProcess(command, 0, stdout, "")

    def build(self, target="windows-x86_64", **kwargs):
        return build(self.rzm, VERSION, COMMIT, target, self.output, canonical_hash=self.digest,
                     root=self.root, runner=self.runner, host=TARGETS[target]["host"], machine="x86_64",
                     source={"PATH": "/usr/bin", "TAURI_SIGNING_PRIVATE_KEY": "synthetic-updater",
                             "VOYAGE_API_KEY": "synthetic-provider", "GITHUB_TOKEN": "synthetic-publisher",
                             "AZURE_CLIENT_SECRET": "synthetic-native",
                             **({"RZM_DESKTOP_PREBUILT_FRONTEND": "1"} if self.prebuilt_frontend else {})}, **kwargs)

    def test_unsigned_windows_package_is_extracted_and_bound_to_cli_without_any_secrets(self):
        manifest = json.loads(self.build().read_text())
        self.assertEqual("windows-x86_64", manifest["target"])
        self.assertFalse(manifest["production"])
        self.assertEqual({"status": "unsigned"}, manifest["nativeSignature"])
        self.assertEqual(self.digest, manifest["sidecars"][0]["sha256"])
        self.assertEqual("windows-11-x86_64", manifest["nativeDependencies"]["baseline"])
        self.assertEqual(b"native installer", (self.output / "Rhizome-windows-x64-setup.exe").read_bytes())
        _, probe = next((command, call) for command, call in self.calls if Path(command[0]).resolve() == self.rzm.resolve())
        self.assertEqual("1", probe["env"]["RZM_SKIP_REPO_DELEGATE"])
        self.assertFalse(probe["cwd"].is_relative_to(self.root.resolve()))
        for _, call in self.calls:
            self.assertNotIn("TAURI_SIGNING_PRIVATE_KEY", call["env"])
            self.assertNotIn("VOYAGE_API_KEY", call["env"])
            self.assertNotIn("GITHUB_TOKEN", call["env"])
            self.assertNotIn("AZURE_CLIENT_SECRET", call["env"])

    def test_linux_appimage_uses_final_extracted_sidecar(self):
        manifest = json.loads(self.build("linux-x86_64").read_text())
        self.assertEqual([{"name": "rzm", "sha256": self.digest}], manifest["sidecars"])
        self.assertEqual("Rhizome-linux-x64.AppImage", manifest["artifact"]["file"])
        self.assertTrue(any(command[-1] == "--appimage-extract" for command, _ in self.calls))
        self.assertEqual("2.17", manifest["nativeDependencies"]["minimumGlibc"])

    def test_native_production_signing_is_required_and_only_passed_to_bundle(self):
        with self.assertRaisesRegex(CandidateError, "signing is not configured"):
            self.build(production=True)
        self.assertFalse(any("build" in command for command, _ in self.calls))
        self.calls.clear()
        config = self.root / "signer.json"
        config.write_text(json.dumps({"command": ["signer", "%1"], "environment": ["AZURE_CLIENT_SECRET"]}))
        manifest = json.loads(self.build(production=True, sign_config=config).read_text())
        self.assertTrue(manifest["production"])
        for command, call in self.calls:
            if command[0] in {"npm", "node"} and "bundle" in command:
                self.assertEqual("synthetic-native", call["env"]["AZURE_CLIENT_SECRET"])
                self.assertIn("windows_sign.py", " ".join(command))
            else:
                self.assertNotIn("AZURE_CLIENT_SECRET", call["env"])
            self.assertNotIn("TAURI_SIGNING_PRIVATE_KEY", call["env"])

    def test_gateway_or_canonical_mismatch_never_stages_a_candidate(self):
        self.extracted_gateway = True
        with self.assertRaisesRegex(CandidateError, "local gateway"):
            self.build()
        self.assertFalse(self.output.exists())
        self.rzm.write_bytes(b"changed")
        with self.assertRaisesRegex(CandidateError, "canonical CLI"):
            self.build()
        self.assertFalse((self.output / MANIFEST).exists())

    def test_shared_frontend_is_preserved_and_missing_transfer_fails_before_compiling(self):
        self.prebuilt_frontend = True
        with self.assertRaisesRegex(CandidateError, "prepared desktop frontend is missing"):
            self.build()
        self.assertFalse(any("build" in command for command, _ in self.calls))
        index = self.root / "desktop/dist/index.html"
        index.parent.mkdir(parents=True)
        index.write_text("transferred frontend")
        self.build()
        builds = [command for command, _ in self.calls if "--no-bundle" in command]
        self.assertEqual(1, len(builds))
        self.assertIn('{"build":{"beforeBuildCommand":"npm run rzm"}}', builds[0])
        self.assertEqual("transferred frontend", index.read_text())


if __name__ == "__main__":
    unittest.main()
