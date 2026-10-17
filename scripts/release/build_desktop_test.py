#!/usr/bin/env python3
"""Tests for the desktop app release hook."""

from __future__ import annotations

from datetime import datetime, timezone
import json
from pathlib import Path
import os
import tempfile
import unittest
from unittest.mock import patch

import build_desktop
from build_desktop import (
    NO_UPDATER_ARTIFACTS,
    DesktopBuildError,
    build,
    bundle_outputs,
    clear_outputs,
    skip_reason,
    update_feed,
)


PUBLISHED = datetime(2026, 10, 17, 18, 30, 5, tzinfo=timezone.utc)


class SkipReasonTest(unittest.TestCase):
    def test_only_public_arm64_snapshot_or_desktop_release_builds(self) -> None:
        self.assertIsNone(skip_reason("arm64", True, {}))
        self.assertIsNone(skip_reason("arm64", False, {"RZM_RELEASE_DESKTOP": "1"}))
        self.assertIn("amd64", skip_reason("amd64", True, {"RZM_RELEASE_DESKTOP": "1"}))
        internal = {"RZM_INTERNAL_S3_BUCKET": "example-bucket", "RZM_RELEASE_DESKTOP": "1"}
        self.assertIn("internal", skip_reason("arm64", True, internal))
        # The GitHub Actions fallback publishes without RZM_RELEASE_DESKTOP.
        self.assertIn("CLI only", skip_reason("arm64", False, {}))


class FakeTauri:
    """Records commands and produces the files a Tauri build would."""

    def __init__(self, root: Path) -> None:
        self.root = root
        self.calls: list[tuple[list[str], dict]] = []

    def __call__(self, command, **kwargs):
        self.calls.append((list(command), kwargs))
        if command[:3] == ["npm", "run", "tauri"]:
            bundle = self.root / build_desktop.BUNDLE_DIR
            (bundle / "dmg").mkdir(parents=True)
            (bundle / "dmg/Rhizome_0.51.0_aarch64.dmg").write_text("dmg")
            (bundle / "macos/Rhizome.app").mkdir(parents=True)
            if NO_UPDATER_ARTIFACTS not in command:
                (bundle / "macos/Rhizome.app.tar.gz").write_text("archive")
                (bundle / "macos/Rhizome.app.tar.gz.sig").write_text("c2lnbmF0dXJl\n")

    def commands(self) -> list[list[str]]:
        return [command for command, _ in self.calls]


class BuildTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        (self.root / "desktop/node_modules").mkdir(parents=True)
        self.rzm = self.root / "dist/rzm-darwin_darwin_arm64/rzm"
        self.rzm.parent.mkdir(parents=True)
        self.rzm.write_text("binary")
        self.home = self.root / "home"
        (self.home / ".cargo/bin").mkdir(parents=True)
        (self.home / ".cargo/bin/cargo").write_text("")
        self.runner = FakeTauri(self.root)
        self.output = self.root / build_desktop.OUTPUT_DIR

    def tearDown(self) -> None:
        self.temp.cleanup()

    def build(self, environ: dict[str, str]) -> list[Path]:
        return build(
            self.rzm, "v0.51.0", environ={"PATH": "/usr/bin", **environ}, root=self.root,
            runner=self.runner, machine="arm64", home=self.home, now=lambda: PUBLISHED,
        )

    def test_desktop_release_without_signing_key_fails_before_building(self) -> None:
        with self.assertRaisesRegex(DesktopBuildError, "TAURI_SIGNING_PRIVATE_KEY"):
            self.build({"RZM_RELEASE_DESKTOP": "1"})
        self.assertEqual([], self.runner.calls)

    def test_snapshot_without_key_builds_without_updater_artifacts(self) -> None:
        staged = self.build({})

        tauri = self.runner.commands()[0]
        self.assertEqual(NO_UPDATER_ARTIFACTS, tauri[-1])
        self.assertIn('{"version": "0.51.0"}', tauri)
        self.assertEqual([self.output / "Rhizome-macos-arm64.dmg"], staged)
        self.assertFalse((self.output / "latest.json").exists())

    def test_signed_build_stages_stable_names_and_update_feed(self) -> None:
        stale = self.root / build_desktop.BUNDLE_DIR / "dmg/Rhizome_0.50.0_aarch64.dmg"
        stale.parent.mkdir(parents=True)
        stale.write_text("old")

        staged = self.build({
            "RZM_RELEASE_DESKTOP": "1",
            "TAURI_SIGNING_PRIVATE_KEY": "synthetic-key",
            "TAURI_SIGNING_PRIVATE_KEY_PASSWORD": "synthetic-password",
            "GITHUB_TOKEN": "synthetic-github",
        })

        self.assertEqual(
            ["Rhizome-macos-arm64.dmg", "Rhizome-macos-arm64.app.tar.gz",
             "Rhizome-macos-arm64.app.tar.gz.sig", "latest.json"],
            [path.name for path in staged],
        )
        self.assertEqual("dmg", (self.output / "Rhizome-macos-arm64.dmg").read_text())
        feed = json.loads((self.output / "latest.json").read_text())
        self.assertEqual("c2lnbmF0dXJl", feed["platforms"]["darwin-aarch64"]["signature"])
        tauri, kwargs = self.runner.calls[0]
        self.assertNotIn(NO_UPDATER_ARTIFACTS, tauri)
        self.assertEqual(self.root / "desktop", kwargs["cwd"])
        env = kwargs["env"]
        self.assertEqual(str(self.rzm.resolve()), env["RZM_DESKTOP_PREBUILT_RZM"])
        self.assertEqual("synthetic-key", env["TAURI_SIGNING_PRIVATE_KEY"])
        self.assertEqual("synthetic-password", env["TAURI_SIGNING_PRIVATE_KEY_PASSWORD"])
        self.assertNotIn("GITHUB_TOKEN", env)
        self.assertTrue(env["PATH"].startswith(str(self.home / ".cargo/bin")))
        codesign = self.runner.commands()[1]
        self.assertEqual(["codesign", "--verify", "--deep", "--strict"], codesign[:4])
        self.assertTrue(codesign[4].endswith("bundle/macos/Rhizome.app"))

    def test_missing_dependencies_are_installed_first(self) -> None:
        (self.root / "desktop/node_modules").rmdir()
        self.build({})
        self.assertEqual(["make", "desktop-deps"], self.runner.commands()[0])

    def test_missing_rust_toolchain_is_reported(self) -> None:
        (self.home / ".cargo/bin/cargo").unlink()
        with self.assertRaisesRegex(DesktopBuildError, "cargo not found"):
            build(self.rzm, "0.51.0", environ={"PATH": str(self.root)}, root=self.root,
                  runner=self.runner, machine="arm64", home=self.home)

    def test_intel_host_is_refused(self) -> None:
        with self.assertRaisesRegex(DesktopBuildError, "Apple Silicon"):
            build(self.rzm, "0.51.0", environ={}, root=self.root, runner=self.runner,
                  machine="x86_64", home=self.home)


class MainTest(unittest.TestCase):
    def test_cli_only_release_clears_stale_desktop_assets(self) -> None:
        with tempfile.TemporaryDirectory() as name:
            output = Path(name) / build_desktop.OUTPUT_DIR
            output.mkdir(parents=True)
            (output / "Rhizome-macos-arm64.dmg").write_text("stale")
            with patch.object(build_desktop, "ROOT", Path(name)), patch.dict(os.environ, {}, clear=True):
                self.assertEqual(0, build_desktop.main(["rzm", "0.51.0", "arm64", "false"]))
            self.assertEqual([], list(output.iterdir()))


class OutputTest(unittest.TestCase):
    def test_clear_outputs_removes_only_desktop_assets(self) -> None:
        with tempfile.TemporaryDirectory() as name:
            output = Path(name)
            for asset in (*build_desktop.OUTPUTS, "install-rzm.sh"):
                (output / asset).write_text("x")
            clear_outputs(output)
            self.assertEqual(["install-rzm.sh"], [path.name for path in output.iterdir()])

    def test_bundle_outputs_requires_exactly_one_dmg(self) -> None:
        with tempfile.TemporaryDirectory() as name:
            bundle = Path(name)
            (bundle / "dmg").mkdir()
            with self.assertRaisesRegex(DesktopBuildError, "found 0"):
                bundle_outputs(bundle, updater=False)
            (bundle / "dmg/a.dmg").write_text("")
            (bundle / "dmg/b.dmg").write_text("")
            with self.assertRaisesRegex(DesktopBuildError, "found 2"):
                bundle_outputs(bundle, updater=False)
            (bundle / "dmg/b.dmg").unlink()
            with self.assertRaisesRegex(DesktopBuildError, "Rhizome.app.tar.gz"):
                bundle_outputs(bundle, updater=True)

    def test_update_feed_follows_tauri_static_format(self) -> None:
        self.assertEqual(
            {
                "version": "0.51.0",
                "notes": "",
                "pub_date": "2026-10-17T18:30:05Z",
                "platforms": {
                    "darwin-aarch64": {
                        "signature": "c2lnbmF0dXJl",
                        "url": "https://github.com/atomicobject/rhizome/releases/download/v0.51.0/Rhizome-macos-arm64.app.tar.gz",
                    }
                },
            },
            update_feed("v0.51.0", "c2lnbmF0dXJl\n", PUBLISHED),
        )


if __name__ == "__main__":
    unittest.main()
