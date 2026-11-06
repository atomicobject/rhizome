from __future__ import annotations

import copy
import json
import io
from pathlib import Path
import tempfile
import tarfile
import unittest

from desktop_candidates import (CandidateError, MANIFEST, ROOT, TARGETS, candidate_manifest, identity,
                                profile, read_candidate, sha256, sidecar_inventory, validate_profile)
from desktop_candidates import tauri_cli


COMMIT = "a" * 40
VERSION = "1.2.3"


def dependency_fixture(target):
    windows = target.startswith("windows-")
    return {"status": "verified", "baseline": "windows-11-x86_64" if windows else "ubuntu-24.04-x86_64",
            "host": {"os": "synthetic-Windows" if windows else "Ubuntu", "version": "10.0.26100" if windows else "24.04",
                     **({"systemDirectory": "C:\\Windows\\System32"} if windows else {"libc": "glibc 2.39"})},
            "imports": [{"name": "kernel32.dll" if windows else "libc.so.6", "origin": "system",
                         "path": "C:\\Windows\\System32\\kernel32.dll" if windows else "/usr/lib/x86_64-linux-gnu/libc.so.6",
                         **({"signature": "microsoft-valid"} if windows else {})}],
            "probeVersion": "rzm version v" + VERSION, "methods": ["synthetic-dependency-fixture"]}


def make_candidate(parent: Path, target: str, *, production=True) -> tuple[Path, str]:
    directory = parent / target
    directory.mkdir()
    cli = directory / TARGETS[target]["cli"]
    cli.write_bytes(f"canonical {target}".encode())
    cli_hash = sha256(cli)
    package = directory / TARGETS[target]["asset"]
    if target == "darwin-aarch64":
        payload = cli.read_bytes() + b" signed by Developer ID"
        with tarfile.open(package, "w:gz") as archive:
            member = tarfile.TarInfo("Rhizome.app/Contents/MacOS/rzm")
            member.size = len(payload)
            archive.addfile(member, io.BytesIO(payload))
        import hashlib
        packaged_hash = hashlib.sha256(payload).hexdigest()
    else:
        package.write_bytes(f"package {target}".encode())
        packaged_hash = cli_hash
    state = "authenticode" if target.startswith("windows-") else "notarized" if target.startswith("darwin-") else "not-applicable"
    signature = {"status": state, **({"thumbprint": "synthetic-thumbprint"} if state == "authenticode" else {})}
    if not production:
        signature = {"status": "unsigned"}
    manifest = candidate_manifest(target=target, version=VERSION, commit=COMMIT, package=package,
                                  cli_hash=cli_hash, packaged_cli_hash=packaged_hash,
                                  inventory=[{"name": cli.name, "sha256": packaged_hash}],
                                  toolchains={"rust": "synthetic-rust"}, native_signature=signature,
                                  production=production, dependencies=dependency_fixture(target) if target != "darwin-aarch64" else None)
    (directory / MANIFEST).write_text(json.dumps(manifest))
    return directory, cli_hash


class CandidateTests(unittest.TestCase):
    def test_windows_tauri_argv_uses_node_without_batch_shell_parsing(self):
        self.assertEqual(["node", str(ROOT / "desktop/node_modules/@tauri-apps/cli/tauri.js")],
                         tauri_cli(ROOT, windows=True))

    def test_native_profiles_have_one_package_and_no_updater_key_requirement(self):
        for target in ("windows-x86_64", "linux-x86_64"):
            validate_profile(profile(ROOT, target), target)

    def test_identity_refuses_ambiguous_versions_and_commits(self):
        self.assertEqual((VERSION, COMMIT), identity("v" + VERSION, COMMIT))
        for version, commit in (("latest", COMMIT), ("1.2.3-..", COMMIT), (VERSION, "abc123")):
            with self.subTest(version=version, commit=commit), self.assertRaises(CandidateError):
                identity(version, commit)

    def test_inventory_checks_the_extracted_bytes_and_rejects_gateway(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            cli = root / "rzm.exe"
            cli.write_bytes(b"canonical")
            digest = sha256(cli)
            self.assertEqual([{"name": "rzm.exe", "sha256": digest}], sidecar_inventory(root, "windows-x86_64", digest))
            with self.assertRaisesRegex(CandidateError, "differs"):
                sidecar_inventory(root, "windows-x86_64", "0" * 64)
            (root / "team-api.exe").write_bytes(b"gateway")
            with self.assertRaisesRegex(CandidateError, "exactly"):
                sidecar_inventory(root, "windows-x86_64", digest)

    def test_candidate_revalidates_source_inventory_package_and_canonical_copy(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory, digest = make_candidate(Path(temporary), "windows-x86_64")
            original = json.loads((directory / MANIFEST).read_text())
            read_candidate(directory, version=VERSION, commit=COMMIT, canonical_hash=digest, production=True)
            mutations = [
                {"sourceCommit": "b" * 40}, {"version": "9.9.9"}, {"sidecars": []},
                {"production": False}, {"nativeSignature": {"status": "unsigned"}},
                {"nativeDependencies": None},
                {"nativeDependencies": {**dependency_fixture("windows-x86_64"), "imports": []}},
                {"artifact": {**original["artifact"], "file": "../outside.exe"}},
                {"artifact": {**original["artifact"], "sha256": "0" * 64}},
            ]
            for mutation in mutations:
                (directory / MANIFEST).write_text(json.dumps({**copy.deepcopy(original), **mutation}))
                with self.subTest(mutation=mutation), self.assertRaises(CandidateError):
                    read_candidate(directory, version=VERSION, commit=COMMIT, canonical_hash=digest, production=True)
            (directory / MANIFEST).write_text(json.dumps(original))
            (directory / "rzm.exe").write_bytes(b"tampered")
            with self.assertRaisesRegex(CandidateError, "CLI copy"):
                read_candidate(directory, version=VERSION, commit=COMMIT, canonical_hash=digest, production=True)


if __name__ == "__main__":
    unittest.main()
