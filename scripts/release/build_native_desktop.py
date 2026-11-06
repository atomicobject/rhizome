#!/usr/bin/env python3
"""Build a non-publishing Windows/Linux candidate around a canonical rzm."""

from __future__ import annotations

import argparse
from contextlib import nullcontext
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tempfile

from desktop_candidates import (CandidateError, MANIFEST, ROOT, TARGETS, candidate_manifest, identity,
                                package_file, profile, sha256, sidecar_inventory, tauri_cli, validate_profile)
from run_clean import scoped_environment, sensitive_name
from windows_sign import configuration, signing_source
from desktop_dependencies import inspect_dependencies
from linux_sidecar import bundle_environment, mismatch_evidence


def build_failure(error, command, diagnostic_path):
    def decoded(value):
        return value.decode("utf-8", errors="replace") if isinstance(value, bytes) else (value or "")
    output = "\n".join(filter(None, (decoded(getattr(error, "stdout", None)), decoded(getattr(error, "stderr", None)))))
    output = re.sub(r"(?is)-----BEGIN [^-]*PRIVATE KEY-----.*?(?:-----END [^-]*PRIVATE KEY-----|\Z)", "[redacted private key]", output)
    output = re.sub(r"(?i)(https?://)[^\s/@:]+:[^\s/@]+@", r"\1[redacted]@", output)
    output = re.sub(r"(?i)(authorization\s*[:=]\s*(?:bearer|basic)\s+)\S+", r"\1[redacted]", output)
    output = re.sub(r"(?i)(\b(?:token|secret|password|api[_-]?key)\s*[:=]\s*)[^\s]+", r"\1[redacted]", output)
    output = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", output)
    output = "".join(character for character in output if character in "\n\t" or ord(character) >= 32)[-12000:]
    status = getattr(error, "returncode", None)
    detail = f"{Path(command[0]).name} failed ({'exit ' + str(status) if status is not None else type(error).__name__})"
    diagnostic_path.parent.mkdir(parents=True, exist_ok=True)
    diagnostic_path.write_text(detail + "\n" + output + "\n", encoding="utf-8")
    return detail + f"; build evidence: {diagnostic_path}" + ("\n" + output[-4000:] if output else "")


def run(command, *, root: Path, env: dict, runner=subprocess.run, timeout=1200, diagnostic_path: Path | None = None) -> str:
    if diagnostic_path is not None and (any(word in {"bundle", "signer", "sign"} for word in command)
            or any(value and sensitive_name(name) for name, value in env.items())):
        raise CandidateError("build diagnostics are unavailable for signing commands or credentialed environments")
    try:
        result = runner(command, cwd=root, env=env, check=True, capture_output=True, text=True,
                        encoding="utf-8", errors="replace", timeout=timeout)
    except (subprocess.SubprocessError, OSError) as error:
        if diagnostic_path is not None:
            raise CandidateError(build_failure(error, command, diagnostic_path)) from error
        # Build/signing tools may echo credentials on failure.
        raise CandidateError(f"desktop command failed: {Path(command[0]).name}; inspect the isolated native job") from error
    return (getattr(result, "stdout", "") or "").strip()


def windows_signature(path: Path, *, root: Path, env: dict, runner, thumbprint: str = "") -> dict:
    command = ["powershell.exe", "-NoProfile", "-NonInteractive", "-File",
               str(root / "scripts/release/verify_windows_signature.ps1"), "-Path", str(path)]
    if thumbprint:
        command += ["-ExpectedThumbprint", thumbprint]
    return json.loads(run(command, root=root, env=env, runner=runner, timeout=30))


def extract(package: Path, target: str, directory: Path, *, root: Path, env: dict, runner) -> Path:
    if target == "windows-x86_64":
        run(["7z", "x", "-y", f"-o{directory}", str(package)], root=root, env=env, runner=runner, timeout=120)
        return directory
    # Executed only by the unprivileged native producer on its own fresh output.
    run([str(package), "--appimage-extract"], root=directory, env=env, runner=runner, timeout=120)
    return directory / "squashfs-root"


def build(rzm: Path, version: str, commit: str, target: str, output: Path, *, canonical_hash: str,
          production: bool = False, sign_config: Path | None = None, test_overlay: Path | None = None,
          source=None, root: Path = ROOT, runner=subprocess.run, host=None, machine=None, phase="all",
          evidence_dir: Path | None = None) -> Path:
    version, commit = identity(version, commit)
    settings = TARGETS.get(target)
    if not settings or target.startswith("darwin-"):
        raise CandidateError("use the existing notarized builder for macOS")
    if (host or platform.system()) != settings["host"] or (machine or platform.machine()) not in settings["machine"]:
        raise CandidateError("desktop candidates must be built on the native target host")
    config_path = profile(root, target)
    validate_profile(config_path, target)
    rzm, output = rzm.resolve(), output.resolve()
    if rzm.is_symlink() or not rzm.is_file() or sha256(rzm) != canonical_hash:
        raise CandidateError("native builder requires the exact canonical CLI artifact")
    if output.exists():
        raise CandidateError("candidate output already exists; use a fresh candidate directory")
    if production and test_overlay is not None:
        raise CandidateError("production candidates cannot use a test updater overlay")
    source = dict(os.environ) if source is None else dict(source)
    env = scoped_environment(source, "build")
    env.update({"CI": "true", "RZM_DESKTOP_PREBUILT_RZM": str(rzm), "RZM_SKIP_REPO_DELEGATE": "1"})
    if run(["git", "rev-parse", "HEAD"], root=root, env=env, runner=runner) != commit:
        raise CandidateError("native build checkout does not match the canonical commit")
    with tempfile.TemporaryDirectory(prefix="rhizome-canonical-version-") as neutral:
        reported_version = run([str(rzm), "--version"], root=Path(neutral), env=env, runner=runner,
                               diagnostic_path=evidence_dir / "canonical-version.log" if evidence_dir else None).split()
    if not reported_version or reported_version[-1].removeprefix("v") != version:
        raise CandidateError("canonical CLI version does not match the desktop candidate")
    signing = {"status": "not-applicable"} if target.startswith("linux-") else {"status": "unsigned"}
    signing_overlay = None
    if production and target.startswith("windows-"):
        if sign_config is None:
            raise CandidateError("Windows production signing is not configured; unsigned candidates cannot be published")
        signer = configuration(sign_config)
        signing = windows_signature(rzm, root=root, env=env, runner=runner)
        signing_overlay = {"bundle": {"windows": {"signCommand": {
            "cmd": sys.executable, "args": [str(root / "scripts/release/windows_sign.py"), "--config",
              str(sign_config.resolve()), "--canonical-sha256", canonical_hash, "%1"]}}}}
    elif sign_config is not None:
        raise CandidateError("signer configuration is only used by production Windows candidates")
    command = tauri_cli(root)
    configs = ["--config", str(config_path), "--config", json.dumps({"version": version})]
    if source.get("RZM_DESKTOP_PREBUILT_FRONTEND") == "1":
        if not (root / "desktop/dist/index.html").is_file():
            raise CandidateError("prepared desktop frontend is missing; restore the verified CI frontend artifact")
        configs += ["--config", '{"build":{"beforeBuildCommand":"npm run rzm"}}']
    if test_overlay is not None:
        from updater_fixture import read_overlay
        read_overlay(test_overlay)
        configs += ["--config", str(test_overlay.resolve())]
    if phase != "package":
        run([*command, "build", "--no-bundle", *configs], root=root / "desktop", env=env, runner=runner,
            diagnostic_path=evidence_dir / "tauri-build.log" if evidence_dir else None)
    if phase == "compile":
        return root / "desktop/src-tauri/target/release"
    bundle_env = dict(env)
    if signing_overlay:
        configs += ["--config", json.dumps(signing_overlay)]
        native_source = signing_source(signer, source)
        bundle_env.update({name: native_source[name] for name in signer.get("environment", []) if name in native_source})
    bundle_dir = root / "desktop/src-tauri/target/release/bundle"
    shutil.rmtree(bundle_dir / settings["bundle"], ignore_errors=True)
    linux_environment = bundle_environment(root, bundle_dir, canonical_hash, bundle_env) if target.startswith("linux-") else nullcontext(bundle_env)
    with linux_environment as package_env:
        try:
            run([*command, "bundle", "--bundles", settings["bundle"], *configs],
                root=root / "desktop", env=package_env, runner=runner)
        except CandidateError:
            if target.startswith("linux-") and evidence_dir is not None:
                mismatch_evidence(rzm, bundle_dir / "appimage", evidence_dir / "sidecar-packaging.log", env=env, runner=runner)
            raise
    package = package_file(bundle_dir, target)
    if production and target.startswith("windows-"):
        windows_signature(package, root=root, env=env, runner=runner, thumbprint=signing["thumbprint"])
        windows_signature(root / "desktop/src-tauri/target/release/rhizome-desktop.exe",
                          root=root, env=env, runner=runner, thumbprint=signing["thumbprint"])
    if production:
        gui = root / ("desktop/src-tauri/target/release/rhizome-desktop.exe" if target.startswith("windows-")
                      else "desktop/src-tauri/target/release/rhizome-desktop")
        run([sys.executable, str(root / "scripts/teamkeys/release.py"), "--check-artifact", str(gui)],
            root=root, env=env, runner=runner)
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".desktop-candidate-", dir=output.parent) as temporary:
        staging = Path(temporary)
        extracted = staging / "extracted"
        extracted.mkdir()
        contents = extract(package, target, extracted, root=root, env=env, runner=runner)
        try:
            inventory = sidecar_inventory(contents, target, canonical_hash)
        except CandidateError:
            if target.startswith("linux-") and evidence_dir is not None:
                mismatch_evidence(rzm, contents, evidence_dir / "sidecar-mismatch.log", env=env, runner=runner)
            raise
        sidecar = next(path for path in contents.rglob(settings["cli"]) if path.is_file())
        dependencies = inspect_dependencies(sidecar, target, version, root=root, env=env, runner=runner)
        toolchains = {name: run(args, root=root, env=env, runner=runner, timeout=30)
                      for name, args in (("go", ["go", "version"]), ("rust", ["rustc", "--version"]),
                                         ("tauri", [*command, "--version"]))}
        manifest = candidate_manifest(target=target, version=version, commit=commit, package=package,
                                      cli_hash=canonical_hash, inventory=inventory, toolchains=toolchains,
                                      native_signature=signing, production=production, dependencies=dependencies)
        final = staging / "candidate"
        final.mkdir()
        shutil.copyfile(package, final / settings["asset"])
        shutil.copyfile(rzm, final / settings["cli"])
        (final / MANIFEST).write_text(json.dumps(manifest, indent=2) + "\n")
        final.rename(output)
    return output / MANIFEST


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", type=Path, default=ROOT)
    parser.add_argument("--target", choices=("windows-x86_64", "linux-x86_64"), required=True)
    parser.add_argument("--rzm", type=Path, required=True)
    parser.add_argument("--canonical-sha256", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--production", action="store_true")
    parser.add_argument("--windows-sign-config", type=Path)
    parser.add_argument("--test-overlay", type=Path)
    parser.add_argument("--phase", choices=("all", "compile", "package"), default="all")
    parser.add_argument("--evidence-dir", type=Path)
    args = parser.parse_args()
    try:
        print(build(args.rzm, args.version, args.commit, args.target, args.output,
                    canonical_hash=args.canonical_sha256, production=args.production,
                    sign_config=args.windows_sign_config, test_overlay=args.test_overlay, phase=args.phase,
                    root=args.repo_root.resolve(), evidence_dir=args.evidence_dir))
    except (CandidateError, OSError, ValueError) as error:
        print(f"desktop candidate: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
