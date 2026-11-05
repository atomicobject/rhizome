#!/usr/bin/env python3
"""Contract tests for the manual GitHub release fallback."""

from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github" / "workflows" / "release.yml"
GORELEASER_CONFIG = ROOT / ".goreleaser.yml"
DEPLOY_WORKFLOW = ROOT / ".github" / "workflows" / "team-api-deploy.yml"


class ReleaseWorkflowContractTest(unittest.TestCase):
    def test_release_fallback_is_manual_and_validates_tag_via_environment(self) -> None:
        workflow = WORKFLOW.read_text(encoding="utf-8")

        self.assertIn("workflow_dispatch:", workflow)
        self.assertNotIn("\n  push:", workflow)
        self.assertIn("tag:", workflow)
        self.assertIn("required: true", workflow)
        self.assertIn("RELEASE_TAG: ${{ inputs.tag }}", workflow)
        self.assertIn('git check-ref-format "refs/tags/$RELEASE_TAG"', workflow)
        self.assertIn('release_commit="$(git rev-parse "refs/tags/$RELEASE_TAG^{commit}")"', workflow)
        self.assertIn('if [[ "$release_commit" != "$release_tip" ]]', workflow)
        self.assertIn(
            'git merge-base --is-ancestor "$release_commit" refs/remotes/origin/main',
            workflow,
        )
        self.assertIn(
            "awk -v release=\"$release_commit\" 'NF >= 3 && $3 == release",
            workflow,
        )
        self.assertNotIn('grep -Fxq "$release_commit"', workflow)
        self.assertNotIn('grep -Fxq "$RELEASE_COMMIT"', workflow)
        self.assertNotIn("run: ${{ inputs.tag }}", workflow)

    def test_read_only_probe_gates_the_write_capable_publisher(self) -> None:
        workflow = WORKFLOW.read_text(encoding="utf-8")
        probe = workflow.split("\n  probe:\n", 1)[1].split("\n  test:\n", 1)[0]
        publisher = workflow.split("\n  goreleaser:\n", 1)[1].split("\n  notarization_candidate:\n", 1)[0]

        self.assertIn("permissions:\n  contents: read", workflow)
        self.assertIn("permissions:\n      contents: read", probe)
        self.assertIn("release_exists: ${{ steps.release.outputs.exists }}", probe)
        self.assertIn("release_commit: ${{ steps.topology.outputs.commit }}", probe)
        self.assertIn('echo "commit=$release_commit" >> "$GITHUB_OUTPUT"', probe)
        self.assertIn("/releases/tags/$RELEASE_TAG", probe)
        self.assertIn("needs: [probe, test, gateway]", publisher)
        self.assertIn("if: needs.probe.outputs.release_exists == 'false'", publisher)
        self.assertIn("permissions:\n      contents: write", publisher)
        self.assertIn("ref: ${{ needs.probe.outputs.release_commit }}", publisher)
        self.assertIn("RELEASE_COMMIT: ${{ needs.probe.outputs.release_commit }}", publisher)
        self.assertIn("GORELEASER_CURRENT_TAG: ${{ inputs.tag }}", publisher)
        self.assertIn("GITHUB_TOKEN: ${{ github.token }}", publisher)
        self.assertIn("BREW_GITHUB_TOKEN: ${{ secrets.BREW_GITHUB_TOKEN }}", publisher)
        self.assertIn("run_clean.py publish -- goreleaser release --clean", publisher)
        self.assertNotIn("bundle_team_keys", workflow)
        self.assertNotIn("ATOMIC_RHIZOME_KEY", workflow)
        self.assertIn("gitleaks/v8@v8.30.1", publisher)
        goreleaser_step = publisher.split("      - name: Run GoReleaser\n", 1)[1]
        self.assertEqual(goreleaser_step.count("\n        env:\n"), 1)
        self.assertIn(
            'test "$(git rev-parse "refs/tags/$RELEASE_TAG^{commit}")" = "$RELEASE_COMMIT"',
            publisher,
        )
        self.assertIn(
            "awk -v release=\"$RELEASE_COMMIT\" 'NF >= 3 && $3 == release",
            publisher,
        )
        self.assertIn(
            'echo "error: release commit is not on the canonical main topology" >&2',
            publisher,
        )

    def test_fallback_waits_for_the_trusted_gate_before_write_capable_publication(self) -> None:
        workflow = WORKFLOW.read_text(encoding="utf-8")
        gate = workflow.split("\n  gateway:\n", 1)[1].split("\n  goreleaser:\n", 1)[0]
        self.assertIn("needs: [probe, test]", gate)
        self.assertIn("contents: read\n      actions: write", gate)
        self.assertNotIn("contents: write", gate)
        self.assertNotIn("id-token: write", gate)
        self.assertIn("ref: ${{ github.sha }}\n          path: trusted", gate)
        self.assertIn("ref: ${{ needs.probe.outputs.release_commit }}\n          path: source", gate)
        self.assertEqual(2, gate.count("persist-credentials: false"))
        self.assertIn("python3 trusted/scripts/release/release_gateway.py", gate)
        self.assertIn('--repo-root "$GITHUB_WORKSPACE/source"', gate)
        self.assertIn('--release-commit "$RELEASE_COMMIT"', gate)
        self.assertIn("!inputs.candidate_only && github.ref == 'refs/heads/main' && github.repository == 'atomicobject/rhizome'", workflow)

    def test_desktop_signing_is_protected_and_always_cleaned_up(self) -> None:
        workflow = WORKFLOW.read_text(encoding="utf-8")
        for name in ("goreleaser", "notarization_candidate"):
            job = workflow.split(f"\n  {name}:\n", 1)[1].split("\n  notarization_candidate:\n", 1)[0]
            with self.subTest(job=name):
                self.assertIn("runs-on: macos-15", job)
                self.assertIn("environment: release", job)
                self.assertIn('test "$(uname -m)" = arm64', job)
                self.assertIn("ci_desktop_signing.py prepare", job)
                self.assertIn('RZM_RELEASE_DESKTOP: "1"', job)
                self.assertIn("APPLE_API_KEY: ${{ secrets.APPLE_API_KEY }}", job)
                self.assertIn("TAURI_SIGNING_PRIVATE_KEY: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY }}", job)
                self.assertIn("if: ${{ always() }}\n        run: python3 scripts/release/ci_desktop_signing.py cleanup", job)
                signing = job.split("      - name: Prepare desktop signing\n", 1)[1].split("      - name:", 1)[0]
                self.assertIn("APPLE_API_PRIVATE_KEY: ${{ secrets.APPLE_API_PRIVATE_KEY }}", signing)
                self.assertNotIn("GITHUB_TOKEN", signing)

    def test_candidate_cannot_publish_or_deploy(self) -> None:
        workflow = WORKFLOW.read_text(encoding="utf-8")
        candidate = workflow.split("\n  notarization_candidate:\n", 1)[1]
        self.assertIn("if: inputs.candidate_only && github.repository == 'atomicobject/rhizome'", candidate)
        self.assertIn("contents: read", candidate)
        self.assertIn("persist-credentials: false", candidate)
        self.assertIn("goreleaser build --snapshot --clean --single-target", candidate)
        for forbidden in ("contents: write", "actions: write", "id-token: write", "goreleaser release",
                          "release_gateway.py", "GITHUB_TOKEN:", "BREW_GITHUB_TOKEN:", "upload-artifact"):
            self.assertNotIn(forbidden, candidate)

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

        self.assertIn('release:\n  target_commitish: "{{ .Commit }}"', config)

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
