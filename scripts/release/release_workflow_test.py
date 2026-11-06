#!/usr/bin/env python3
"""Safety contracts for approved release CI and nonpublishing candidates."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "release.yml"
GORELEASER_CONFIG = ROOT / ".goreleaser.yml"
DEPLOY_WORKFLOW = ROOT / ".github" / "workflows" / "team-api-deploy.yml"


class ReleaseWorkflowContractTest(unittest.TestCase):
    @staticmethod
    def jobs() -> dict[str, str]:
        import re

        workflow = WORKFLOW.read_text(encoding="utf-8")
        sections = re.split(r"\n  ([a-z_]+):\n", "\n" + workflow.split("\njobs:\n", 1)[1])
        return dict(zip(sections[1::2], sections[2::2]))

    def test_only_approved_release_prs_or_explicit_dispatch_start_builds(self) -> None:
        workflow = WORKFLOW.read_text(encoding="utf-8")
        select = self.jobs()["select"]
        self.assertNotIn("\n  push:", workflow)
        self.assertIn("branches: [main]\n    types: [closed]", workflow)
        self.assertIn("workflow_dispatch:", workflow)
        self.assertIn("github.repository == 'atomicobject/rhizome'", select)
        self.assertIn("github.event.pull_request.merged", select)
        self.assertIn("github.event.pull_request.head.repo.full_name == github.repository", select)
        self.assertIn("startsWith(github.event.pull_request.head.ref, 'release-prep/')", select)
        self.assertIn("inputs.candidate_only || github.ref == 'refs/heads/main'", select)
        self.assertIn("ci_release_selection.py", select)
        self.assertIn('--event-file "$GITHUB_EVENT_PATH"', select)
        self.assertIn('--candidate-only "$CANDIDATE_ONLY"', select)
        self.assertNotIn('--release-pr', select)
        self.assertIn("group: public-release\n  cancel-in-progress: false", workflow)

    def test_every_worker_uses_the_validated_source_without_persisted_credentials(self) -> None:
        jobs = self.jobs()
        self.assertEqual({"select", "common", "linux", "windows", "mac", "assemble", "publish"}, set(jobs))
        for name, job in jobs.items():
            with self.subTest(job=name):
                self.assertIn("persist-credentials: false", job)
                if name != "select":
                    self.assertIn("ref: ${{ needs.select.outputs.source }}", job)
                    self.assertIn("SOURCE: ${{ needs.select.outputs.source }}", job)
        self.assertIn("permissions:\n  contents: read", WORKFLOW.read_text())
        self.assertIn("contents: read\n      pull-requests: read", jobs["select"])

    def test_candidates_complete_read_only_assembly_and_skip_the_only_publisher(self) -> None:
        jobs = self.jobs()
        publish = jobs["publish"]
        self.assertIn("needs: [select, assemble]", publish)
        self.assertIn("if: needs.select.outputs.candidate_only != 'true'", publish)
        self.assertIn("runs-on: ubuntu-latest", publish)
        self.assertIn("environment: release", publish)
        self.assertIn("contents: write\n      actions: write", publish)
        self.assertIn("ci_release_publish.py", publish)
        self.assertIn("BREW_GITHUB_TOKEN: ${{ secrets.BREW_GITHUB_TOKEN }}", publish)
        self.assertIn("name: release-verified", publish)
        self.assertIn("name: release-gateway-receipt", publish)
        self.assertIn("retention-days: 30", jobs["assemble"])
        self.assertIn("retention-days: 30", publish)
        self.assertIn("needs: [select, linux, windows, mac]", jobs["assemble"])
        self.assertIn("ci_release_artifacts.py assemble", jobs["assemble"])
        self.assertIn("ci_release_artifacts.py verify", jobs["assemble"])
        for name, job in jobs.items():
            if name == "publish":
                continue
            with self.subTest(job=name):
                for forbidden in ("contents: write", "actions: write", "id-token: write",
                                  "ci_release_publish.py", "release_gateway.py", "BREW_GITHUB_TOKEN",
                                  "goreleaser release", "git push"):
                    self.assertNotIn(forbidden, job)

    def test_frontends_and_cross_platform_builds_run_on_linux_once(self) -> None:
        jobs = self.jobs()
        workflow = WORKFLOW.read_text(encoding="utf-8")
        common, linux, mac = jobs["common"], jobs["linux"], jobs["mac"]
        self.assertEqual(1, workflow.count("runs-on: macos-15"))
        self.assertEqual(1, workflow.count("npm --prefix web run build"))
        self.assertEqual(1, workflow.count("npm --prefix desktop run build"))
        self.assertIn("runs-on: ubuntu-latest", common)
        self.assertIn("ci_release_frontend.py create", common)
        self.assertIn("GOOS: darwin\n          GOARCH: arm64", common)
        self.assertIn("gitleaks/v8@v8.30.1", common)
        self.assertIn("--id rzm-linux", linux)
        self.assertNotIn("--id rzm-windows-amd64", linux)
        self.assertIn("runs-on: ubuntu-24.04", linux)
        self.assertIn("build_native_desktop.py --target linux-x86_64", linux)
        self.assertIn("--id rzm-windows-amd64", jobs["windows"])
        self.assertIn("needs: [select, common, linux, windows]", mac)
        self.assertIn("ci_release_artifacts.py prepare-native", mac)
        self.assertIn("--id rzm-darwin", mac)
        for job in (linux, mac):
            self.assertIn("ci_release_frontend.py extract", job)
            self.assertIn("goreleaser build --snapshot --clean --skip=before", job)
        for forbidden in ("npm --prefix web", "npm --prefix desktop run build", "go install", "--id rzm-linux"):
            self.assertNotIn(forbidden, mac)
        self.assertIn("RZM_DESKTOP_PREBUILT_FRONTEND: '1'", mac)

    def test_signing_credentials_are_scoped_and_cleaned_before_artifact_collection(self) -> None:
        jobs = self.jobs()
        mac = jobs["mac"]
        self.assertIn("environment: release", mac)
        self.assertIn('test "$(uname -m)" = arm64', mac)
        prepare = mac.split("      - name: Prepare desktop signing\n", 1)[1].split("      - name:", 1)[0]
        build = mac.split("      - name: Build, notarize and verify native outputs\n", 1)[1].split("      - name:", 1)[0]
        collect = mac.split("      - name: Collect verified assets without signing credentials\n", 1)[1]
        for secret in ("APPLE_CERTIFICATE", "APPLE_CERTIFICATE_PASSWORD", "APPLE_API_PRIVATE_KEY"):
            self.assertIn(f"{secret}: ${{{{ secrets.{secret} }}}}", prepare)
            self.assertNotIn(f"secrets.{secret} }}}}", build)
        for secret in ("APPLE_SIGNING_IDENTITY", "APPLE_API_KEY", "APPLE_API_ISSUER",
                       "TAURI_SIGNING_PRIVATE_KEY", "TAURI_SIGNING_PRIVATE_KEY_PASSWORD"):
            self.assertIn(f"{secret}: ${{{{ secrets.{secret} }}}}", build)
        self.assertIn("RZM_RELEASE_DESKTOP: '1'", build)
        self.assertIn("RZM_NOTARIZE_DESKTOP: '1'", build)
        self.assertIn("if: ${{ always() }}\n        run: python3 scripts/release/ci_desktop_signing.py cleanup", mac)
        self.assertLess(mac.index("ci_desktop_signing.py cleanup"), mac.index("ci_release_artifacts.py collect"))
        self.assertNotIn("secrets.", collect)
        for name, job in jobs.items():
            with self.subTest(job=name):
                if name != "mac":
                    self.assertNotIn("secrets.APPLE_", job)
                    self.assertNotIn("secrets.TAURI_", job)
                for forbidden in ("bundle_team_keys", "ATOMIC_RHIZOME_KEY", "OPENAI_API_KEY"):
                    self.assertNotIn(forbidden, job)

    def test_native_credentials_and_same_run_candidates_have_one_coordinator(self):
        jobs = self.jobs()
        windows, mac = jobs['windows'], jobs['mac']
        self.assertIn('runs-on: windows-2025', windows)
        self.assertIn('Windows production signing is not configured', windows)
        compile = windows.split('- name: Compile Windows GUI without signing credentials', 1)[1].split('- name:', 1)[0]
        self.assertNotIn('secrets.', compile)
        self.assertIn('--phase compile', compile)
        self.assertIn('--phase package', windows)
        self.assertIn('--initial-cli', windows)
        self.assertNotIn('secrets.TAURI_', windows)
        self.assertIn('name: release-linux', mac)
        self.assertIn('name: release-windows', mac)
        self.assertLess(mac.index('ci_release_artifacts.py prepare-native'), mac.index('ci_desktop_signing.py prepare'))
        self.assertIn('RZM_DESKTOP_CANDIDATES_DIR:', mac)
        workflow = WORKFLOW.read_text()
        for obsolete in ('prepare_native_desktop.py', 'desktop-candidates.yml', 'goreleaser release', 'scripts/release/release_publish.py'):
            self.assertNotIn(obsolete, workflow)
        self.assertIn('path: ${{ runner.temp }}/release-state/', jobs['publish'])
        for job in (jobs['linux'], windows, mac, jobs['assemble']):
            self.assertIn('CANDIDATE_ONLY: ${{ needs.select.outputs.candidate_only }}', job)
            self.assertIn('--candidate-only', job)
        signing = windows.split('- name: Sign the canonical Windows CLI once', 1)[1].split('- name:', 1)[0]
        self.assertIn("if: needs.select.outputs.candidate_only != 'true'", signing)
        self.assertIn('RZM_CI_RELEASE_CANDIDATE_ONLY:', mac)

    def test_deployment_uses_trusted_main_controls_and_the_selected_source_image(self) -> None:
        workflow = DEPLOY_WORKFLOW.read_text(encoding="utf-8")
        self.assertNotIn("\n  push:", workflow)
        self.assertIn("workflow_dispatch:", workflow)
        self.assertIn("format('Deploy team API {0} {1}', inputs.request_id, inputs.source_commit)", workflow)
        self.assertIn("if: github.ref == 'refs/heads/main' && github.repository == 'atomicobject/rhizome'", workflow)
        self.assertIn("environment: production", workflow)
        self.assertIn("group: team-api-production\n  cancel-in-progress: false", workflow)
        self.assertIn("ref: ${{ github.sha }}\n          path: trusted", workflow)
        self.assertIn("ref: ${{ steps.selection.outputs.source_commit }}\n          path: source", workflow)
        self.assertEqual(2, workflow.count("persist-credentials: false"))
        self.assertLess(workflow.index("deployment_inputs.py"), workflow.index("Checkout selected source"))
        self.assertLess(workflow.index("go test -race"), workflow.index("aws-actions/configure-aws-credentials"))
        self.assertIn("python3 trusted/services/team-api/deploy/deploy.py", workflow)
        self.assertNotIn("python3 source/services/team-api/deploy/deploy.py", workflow)
        self.assertIn('--build-arg "VERSION=$IMAGE_TAG" -t "$repository:$IMAGE_TAG" source', workflow)
        self.assertIn('--image-tag "$IMAGE_TAG"', workflow)
        self.assertNotIn('imageTag=$GITHUB_SHA', workflow)

    def test_goreleaser_asks_github_to_create_tag_at_release_commit(self) -> None:
        config = GORELEASER_CONFIG.read_text(encoding="utf-8")

        release = config.split("\nrelease:\n", 1)[1].split("\narchives:\n", 1)[0]
        self.assertIn('target_commitish: "{{ .Commit }}"', release)

    def test_release_assets_match_installer_and_license_contract(self) -> None:
        config = GORELEASER_CONFIG.read_text(encoding="utf-8")

        self.assertIn('name_template: "rhizome-{{ .Os }}-{{ .Arch }}"', config)
        self.assertIn("formats: [tar.gz]", config)
        for path in (
            "LICENSE",
            "THIRD_PARTY_LICENSES.md",
            "licenses/**",
            ".tmp/goreleaser-extra/install-rzm.sh",
            ".tmp/goreleaser-extra/install-rzm.sh.sha256",
        ):
            self.assertIn(path, config)

    def test_every_release_build_checks_its_binary_before_publication(self) -> None:
        config = GORELEASER_CONFIG.read_text(encoding="utf-8")
        builds = config.split("\nbuilds:\n", 1)[1].split("\nrelease:\n", 1)[0]
        self.assertEqual(builds.count("    binary: rzm\n"), 3)
        self.assertEqual(builds.count('post:\n        - python3 scripts/teamkeys/release.py --check-artifact "{{ .Path }}"'), 3)


if __name__ == "__main__":
    unittest.main()
