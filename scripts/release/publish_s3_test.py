#!/usr/bin/env python3
"""Behavior tests for the internal S3 release publisher."""

from __future__ import annotations

import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile
import shutil
import textwrap
import unittest


SCRIPT = Path(__file__).with_name("publish_s3.sh")


class PublishS3PreflightTest(unittest.TestCase):
    def run_preflight(self, aws_script: str) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as temp_dir:
            fake_bin = Path(temp_dir)
            aws = fake_bin / "aws"
            aws.write_text(
                "#!/usr/bin/env bash\n" + textwrap.dedent(aws_script),
                encoding="utf-8",
            )
            aws.chmod(aws.stat().st_mode | stat.S_IXUSR)
            env = os.environ.copy()
            env["PATH"] = f"{fake_bin}:{env['PATH']}"
            env["PREFLIGHT_ONLY"] = "1"
            env["RZM_INTERNAL_S3_BUCKET"] = "example-bucket"
            return subprocess.run(
                ("bash", str(SCRIPT)),
                cwd=SCRIPT.parents[2],
                env=env,
                text=True,
                capture_output=True,
            )

    def test_reports_aws_execution_error_instead_of_expired_credentials(self) -> None:
        result = self.run_preflight(
            """
            echo "bad CPU type in executable" >&2
            exit 126
            """
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn(
            "AWS identity check failed: bad CPU type in executable", result.stderr
        )
        self.assertNotIn("credentials unavailable or expired", result.stderr)

    def test_preflight_accepts_accessible_release_bucket(self) -> None:
        result = self.run_preflight(
            """
            if [[ "$1 $2" == "sts get-caller-identity" ]]; then
              printf '%s\n' \
                '{"Account":"123456789012","Arn":"arn:aws:iam::123456789012:user/release"}'
              exit 0
            fi
            if [[ "$1 $2" == "s3api head-bucket" ]]; then
              exit 0
            fi
            echo "unexpected aws command: $*" >&2
            exit 2
            """
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("aws: account=123456789012", result.stdout)
        self.assertIn("preflight: ok", result.stdout)

    def test_requires_bucket_from_environment(self) -> None:
        env = {k: v for k, v in os.environ.items() if not k.startswith("RZM_INTERNAL_")}
        result = subprocess.run(("bash", str(SCRIPT)), env=env, text=True, capture_output=True)

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RZM_INTERNAL_S3_BUCKET is not set", result.stderr)

    def test_source_names_no_bucket_or_suffix(self) -> None:
        script = SCRIPT.read_text(encoding="utf-8")

        self.assertNotIn("s3.amazonaws.com/", script.replace("${RELEASE_BUCKET}.s3.amazonaws.com", ""))
        self.assertRegex(script, r'RELEASE_BUCKET="\$\{RZM_INTERNAL_S3_BUCKET:-\}"')

    def publish(self, temp_dir: str, version: str, built_version: str) -> tuple[subprocess.CompletedProcess[str], Path]:
        repo = SCRIPT.parents[2]
        targets = [("darwin", "amd64"), ("darwin", "arm64"), ("linux", "amd64"), ("linux", "arm64"), ("windows", "amd64")]
        root = Path(temp_dir) / "repo"
        for relative in ("scripts/release/publish_s3.sh", "scripts/release/build_installer.py", "scripts/install/install-rzm.sh", "scripts/teamkeys/release.py"):
            (root / relative).parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(repo / relative, root / relative)
        (root / "pkg/vault/version").mkdir(parents=True)
        (root / "pkg/vault/version/version.go").write_text(f'package version\n\nvar Version = "{version}"\n')
        (root / "dist").mkdir()
        (root / "dist/metadata.json").write_text(json.dumps({"tag": "v" + version, "version": built_version}))
        for goos, goarch in targets:
            build = root / "dist" / f"rzm-{goos}_{goos}_{goarch}_v1"
            build.mkdir(parents=True)
            (build / ("rzm.exe" if goos == "windows" else "rzm")).write_text("binary")
            (root / "dist" / f"rhizome-{goos}-{goarch}.tar.gz").write_text("archive")
        uploads = Path(temp_dir) / "uploads"
        uploads.mkdir()
        fake_bin = Path(temp_dir) / "bin"
        fake_bin.mkdir()
        gh = fake_bin / "gh"
        gh.write_text('#!/usr/bin/env bash\nprintf "PRIVATE\\n"\n')
        gh.chmod(gh.stat().st_mode | stat.S_IXUSR)
        aws = fake_bin / "aws"
        aws.write_text(textwrap.dedent(f"""\
            #!/usr/bin/env bash
            case "$1 $2" in
              "sts get-caller-identity") echo '{{"Account":"123456789012","Arn":"arn:aws:iam::123456789012:user/release"}}' ;;
              "s3api head-bucket") ;;
              "s3 cp") key="${{4#s3://example-bucket/}}"; mkdir -p "{uploads}/$(dirname "$key")"; cp "$3" "{uploads}/$key" ;;
              *) echo "unexpected aws command: $*" >&2; exit 2 ;;
            esac
            """))
        aws.chmod(aws.stat().st_mode | stat.S_IXUSR)
        env = {k: v for k, v in os.environ.items() if not k.startswith("RZM_INTERNAL_")}
        env.update(PATH=f"{fake_bin}:{env['PATH']}", RZM_INTERNAL_S3_BUCKET="example-bucket",
                   RZM_INTERNAL_S3_LEGACY_SUFFIX="abcde12345", GIT_CEILING_DIRECTORIES=temp_dir)

        result = subprocess.run(("bash", str(root / "scripts/release/publish_s3.sh")), env=env, text=True, capture_output=True)
        return result, uploads

    def test_publishes_legacy_and_github_shaped_layouts(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            result, uploads = self.publish(temp_dir, "0.51.0", "0.51.0")
            self.assertEqual(result.returncode, 0, result.stderr)
            targets = [("darwin", "amd64"), ("darwin", "arm64"), ("linux", "amd64"), ("linux", "arm64"), ("windows", "amd64")]

            base = "https://example-bucket.s3.amazonaws.com"
            legacy = json.loads((uploads / "latest.json").read_text())
            self.assertEqual("v0.51.0", legacy["version"])
            self.assertEqual(f"{base}/v0.51.0/rhizome-linux-arm64.tbz", legacy["artifacts"]["linux/arm64"]["url"])
            self.assertTrue((uploads / "rhizome-arm64-darwin-abcde12345.tbz").is_file())
            release = json.loads((uploads / "releases/latest").read_text())
            self.assertEqual(release, json.loads((uploads / "releases/tags/v0.51.0").read_text()))
            self.assertEqual("v0.51.0", release["tag_name"])
            for asset in release["assets"]:
                key = asset["browser_download_url"].removeprefix(base + "/")
                self.assertTrue((uploads / key).is_file(), key)
            checksums = (uploads / "v0.51.0/checksums.txt").read_text()
            for goos, goarch in targets:
                self.assertIn(f"  rhizome-{goos}-{goarch}.tar.gz\n", checksums)
            self.assertIn("install-rzm.sh.sha256", [asset["name"] for asset in release["assets"]])
            installer = (uploads / "install-rzm.sh").read_text()
            self.assertIn(f"{base}/releases", installer)
            self.assertNotIn("api.github.com/repos/atomicobject/rhizome/releases", installer)

    def test_refuses_snapshot_or_mismatched_dist(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            result, uploads = self.publish(temp_dir, "0.51.0", "0.51.0-SNAPSHOT-abc1234")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("not the v0.51.0 release", result.stderr)
            self.assertEqual([], list(uploads.rglob("*")))

    def test_prerelease_never_moves_root_aliases(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            result, uploads = self.publish(temp_dir, "0.51.0-rc.1", "0.51.0-rc.1")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue((uploads / "releases/tags/v0.51.0-rc.1").is_file())
            self.assertTrue(json.loads((uploads / "releases/tags/v0.51.0-rc.1").read_text())["prerelease"])
            top_level = sorted(path.name for path in uploads.iterdir() if path.is_file())
            self.assertEqual([], top_level)
            self.assertFalse((uploads / "releases/latest").exists())


if __name__ == "__main__":
    unittest.main()
