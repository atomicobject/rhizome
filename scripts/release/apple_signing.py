"""Apple release credentials and verification, shared by local and CI builds."""

from contextlib import contextmanager
import os
import json
from pathlib import Path
import plistlib
import re
import subprocess
import sys
import tarfile
import tempfile


APPLE_SIGNING = (
    "APPLE_SIGNING_IDENTITY", "APPLE_API_KEY", "APPLE_API_ISSUER",
    "APPLE_API_KEY_PATH", "APPLE_API_PRIVATE_KEY",
)
SIGNING_FILE = "RZM_DESKTOP_SIGNING_FILE"
UPDATER_SIGNING = ("TAURI_SIGNING_PRIVATE_KEY", "TAURI_SIGNING_PRIVATE_KEY_PASSWORD")


@contextmanager
def release_credentials(source, environment):
    """GoReleaser builds inherit a file reference, never signing key values."""
    selected = {name: source[name] for name in (*APPLE_SIGNING, *UPDATER_SIGNING) if name in source}
    with tempfile.TemporaryDirectory(prefix="rzm-release-signing-") as directory:
        path = Path(directory) / "credentials.json"
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(descriptor, "w") as file:
            json.dump(selected, file)
        yield {**environment, SIGNING_FILE: str(path)}


def desktop_credentials(environment):
    result = dict(environment)
    reference = result.pop(SIGNING_FILE, None)
    if reference:
        path = Path(reference)
        if path.stat().st_mode & 0o077:
            raise AppleSigningError("desktop signing file must be readable only by its owner")
        values = json.loads(path.read_text())
        result.update({name: values[name] for name in (*APPLE_SIGNING, *UPDATER_SIGNING) if name in values})
    return result


class AppleSigningError(ValueError):
    pass


def notarizes(environ):
    return any(environ.get(name) == "1" for name in ("RZM_RELEASE_DESKTOP", "RZM_NOTARIZE_DESKTOP"))


def require_credentials(environ):
    if sys.version_info < (3, 12):
        raise AppleSigningError("notarized desktop builds require Python 3.12 or newer for safe archive verification")
    missing = [name for name in APPLE_SIGNING[:3] if not environ.get(name, "").strip()]
    if not (environ.get("APPLE_API_PRIVATE_KEY", "").strip() or environ.get("APPLE_API_KEY_PATH", "").strip()):
        missing.append("APPLE_API_PRIVATE_KEY or APPLE_API_KEY_PATH")
    if missing:
        raise AppleSigningError("notarized desktop build requires " + ", ".join(missing))
    identity = environ["APPLE_SIGNING_IDENTITY"]
    if not re.fullmatch(r"Developer ID Application: .+ \([A-Z0-9]{10}\)", identity):
        raise AppleSigningError("APPLE_SIGNING_IDENTITY must be a Developer ID Application identity with its Team ID")


@contextmanager
def credentials(environ):
    """Materialize the 1Password key only while Tauri signs and notarizes."""
    env = dict(environ)
    content = env.pop("APPLE_API_PRIVATE_KEY", None)
    with tempfile.TemporaryDirectory(prefix="rzm-notary-") as directory:
        if content:
            key = Path(directory) / "AuthKey.p8"
            descriptor = os.open(key, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(descriptor, "w") as file:
                file.write(content)
            env["APPLE_API_KEY_PATH"] = str(key)
        elif not Path(env["APPLE_API_KEY_PATH"]).is_file():
            raise AppleSigningError("APPLE_API_KEY_PATH does not name an existing private key file")
        yield env


def run_check(command, *, runner, env):
    try:
        return runner(command, env=env, check=True, capture_output=True, text=True, timeout=120)
    except (subprocess.SubprocessError, OSError) as error:
        # These checks run without credentials and describe only public artifacts.
        detail = ""
        if isinstance(error, subprocess.CalledProcessError):
            detail = (error.stderr or error.stdout or "").strip()[-2000:]
        step = " ".join(command[:3])
        target = str(command[-1])
        raise AppleSigningError(f"Apple distribution verification failed: {step} {target}; {detail}") from error


def notarize_dmg(dmg, *, runner, env):
    """Tauri notarizes the app; the outer DMG needs its own ticket."""
    command = ["xcrun", "notarytool", "submit", str(dmg), "--wait", "--timeout", "30m",
               "--key", env["APPLE_API_KEY_PATH"], "--key-id", env["APPLE_API_KEY"],
               "--issuer", env["APPLE_API_ISSUER"], "--output-format", "json"]
    try:
        result = runner(command, env=env, check=True, capture_output=True, text=True, timeout=1900)
        if json.loads(result.stdout).get("status") != "Accepted":
            raise AppleSigningError("Apple did not accept the desktop DMG; no assets staged")
    except (subprocess.SubprocessError, OSError, ValueError) as error:
        raise AppleSigningError("desktop DMG notarization failed or timed out; no assets staged") from error
    clean = {name: value for name, value in env.items() if not name.startswith(("APPLE_", "TAURI_SIGNING_"))}
    run_check(["xcrun", "stapler", "staple", str(dmg)], runner=runner, env=clean)


def verify_app(app, identity, *, runner, env):
    run_check(["codesign", "--verify", "--deep", "--strict", str(app)], runner=runner, env=env)
    team = identity.rsplit("(", 1)[1].removesuffix(")")
    for executable in (app, app / "Contents/MacOS/rzm"):
        result = run_check(["codesign", "-dv", "--verbose=4", str(executable)], runner=runner, env=env)
        details = result.stderr
        if f"TeamIdentifier={team}\n" not in details or not re.search(r"flags=.*\bruntime\b", details):
            raise AppleSigningError("desktop executable has the wrong signing team or lacks hardened runtime")
    run_check(["xcrun", "stapler", "validate", str(app)], runner=runner, env=env)
    run_check(["spctl", "--assess", "--type", "execute", "--verbose=2", str(app)], runner=runner, env=env)


def verify_distribution(app, dmg, archive, identity, *, runner, env):
    """Inspect final distributed copies, without changing any signed artifact."""
    verify_app(app, identity, runner=runner, env=env)
    run_check(["xcrun", "stapler", "validate", str(dmg)], runner=runner, env=env)
    result = run_check(["hdiutil", "attach", "-readonly", "-nobrowse", "-plist", str(dmg)], runner=runner, env=env)
    mounts = [entry["mount-point"] for entry in plistlib.loads(result.stdout.encode())["system-entities"] if "mount-point" in entry]
    try:
        if len(mounts) != 1:
            raise AppleSigningError("expected one mounted desktop volume")
        verify_app(Path(mounts[0]) / "Rhizome.app", identity, runner=runner, env=env)
    finally:
        for mount in mounts:
            run_check(["hdiutil", "detach", mount], runner=runner, env=env)
    if archive:
        with tempfile.TemporaryDirectory(prefix="rzm-notarized-archive-") as directory:
            with tarfile.open(archive, "r:gz") as bundle:
                # data_filter checks traversal and symlink targets on Python 3.12+.
                bundle.extractall(directory, filter="data")
            verify_app(Path(directory) / "Rhizome.app", identity, runner=runner, env=env)
