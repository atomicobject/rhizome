#!/usr/bin/env python3

import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import effort_evidence

from collect_evidence import collect_release_evidence, rank_efforts
from release_types import EffortEvidence


def git(repo: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", *args], cwd=repo, capture_output=True, text=True, check=True
    )
    return result.stdout.strip()


def write(repo: Path, path: str, content: str) -> None:
    target = repo / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(content, encoding="utf-8")


def effort(effort_id: str, status: str, delivered: str) -> str:
    return f"""---
type: EffortNote
id: {effort_id}
name: Test {effort_id}
status: {status}
---

# Test {effort_id}

## Actual Delivered

{delivered}

## Deviations

None.
"""


class CollectReleaseEvidenceTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp_dir = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp_dir.cleanup)
        self.repo = Path(self.temp_dir.name)
        git(self.repo, "init", "-b", "main")
        git(self.repo, "config", "user.email", "release@example.com")
        git(self.repo, "config", "user.name", "Release Test")
        for schema in (Path(__file__).resolve().parents[2] / ".rhizome/ontology").glob("*.graphql"):
            write(self.repo, f".rhizome/ontology/{schema.name}", schema.read_text())
        write(self.repo, ".rhizome/config.yml", 'notes:\n  includes: ["**/*.md", "**/*.html"]\n')
        write(
            self.repo,
            "CHANGELOG.md",
            "# Changelog\n\n## [Unreleased]\n\n- Existing curated entry.\n",
        )
        write(
            self.repo,
            "docs/efforts/test.md",
            effort("EFF-0001", "active", "None yet."),
        )
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "base")
        self.base = git(self.repo, "rev-parse", "HEAD")

    def test_collects_bounded_local_commit_changelog_and_completed_effort(self):
        write(self.repo, "src/feature.txt", "shipped\n")
        write(
            self.repo,
            "docs/efforts/test.md",
            effort("EFF-0001", "complete", "Delivered the feature."),
        )
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "feat: ship feature (#42)")

        result = collect_release_evidence(self.repo, self.base)

        self.assertEqual(result.base_commit, self.base)
        self.assertEqual(result.release_branch, "main")
        self.assertEqual(result.github_status, "unavailable")
        self.assertEqual(result.unreleased_entries, ("Existing curated entry.",))
        self.assertEqual(len(result.delivery_units), 1)
        unit = result.delivery_units[0]
        self.assertEqual(unit.pull_request.number, 42)
        self.assertEqual(unit.pull_request.role, "stub")
        self.assertEqual(unit.efforts[0].effort_id, "EFF-0001")
        self.assertEqual(unit.efforts[0].actual_delivered, "Delivered the feature.")
        self.assertIn("src/feature.txt", unit.changed_paths)

    def test_active_effort_is_excluded_with_an_inspectable_reason(self):
        write(self.repo, "src/feature.txt", "partial\n")
        write(
            self.repo,
            "docs/efforts/test.md",
            effort("EFF-0001", "active", "Implemented part of the feature."),
        )
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "wip: partial feature")

        result = collect_release_evidence(self.repo, self.base)

        self.assertEqual(result.delivery_units[0].efforts, ())
        self.assertIn("active_effort", {item.code for item in result.diagnostics})

    def test_rename_into_workspace_selector_is_new_evidence(self):
        folder = "docs/efforts/workspace"
        old = f"{folder}/draft.html"
        entry = f"{folder}/2026-effort.html"
        log = f"{folder}/work-log.md"
        html = '<html><head></div><script id="rhizome-metadata" type="application/json">' + json.dumps({
            "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00", "status": "complete",
            "name": "Workspace", "summary": "Deliver the feature.", "created-at": "2026-09-13T10:00:00Z",
            "governing-specs": [], "implementation-plan": f"{folder}/plan.html", "work-log": log,
        }) + '</script></head><body></body></html>'
        write(self.repo, old, html)
        write(self.repo, f"{folder}/plan.html", "Plan")
        write(self.repo, log, "## Actual Delivered\n\nDelivered feature.\n")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "prepare draft")
        before = git(self.repo, "rev-parse", "HEAD")
        git(self.repo, "mv", old, entry)
        write(self.repo, "src/feature.txt", "shipped")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "ship workspace")
        result = collect_release_evidence(self.repo, before)
        self.assertEqual(len(result.delivery_units[0].efforts), 1)
        self.assertEqual(result.delivery_units[0].efforts[0].inclusion_reason, "new_complete")

    def test_workspace_groups_materials_and_reads_git_snapshots(self):
        entry = "docs/efforts/workspace/2026-effort.html"
        log = "docs/efforts/workspace/work-log.md"
        def html(status):
            return '<html><head><script id="rhizome-metadata" type="application/json">' + json.dumps({
                "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00", "aliases": ["EFF-2026-09-13-10-00"], "status": status,
                "name": "Workspace", "summary": "Deliver the feature.", "created-at": "2026-09-13T10:00:00Z",
                "governing-specs": ["docs/specs/example.md"], "implementation-plan": "docs/efforts/workspace/plan.html", "work-log": log,
            }) + '</script></head><body></body></html>'
        write(self.repo, "docs/efforts/workspace/plan.html", "Plan")
        write(self.repo, "docs/specs/example.md", "---\ntype: TechnicalSpec\n---\n")
        write(self.repo, entry, html("active"))
        write(self.repo, log, "## Actual Delivered\n\nNone yet.\n")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "start workspace")
        before = git(self.repo, "rev-parse", "HEAD")
        write(self.repo, entry, html("complete"))
        write(self.repo, log, "## Actual Delivered\n\nDelivered feature.\n\n## Deviations\n\n- Caveat.\n")
        write(self.repo, "docs/efforts/workspace/plan.html", "Plan")
        write(self.repo, "src/feature.txt", "shipped")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "ship workspace")
        write(self.repo, log, "Uncommitted false evidence")
        write(self.repo, "docs/specs/example.md", "Uncommitted non-spec")
        result = collect_release_evidence(self.repo, before)
        self.assertEqual(len(result.delivery_units[0].efforts), 1)
        candidate = result.delivery_units[0].efforts[0]
        self.assertEqual(candidate.path, entry)
        self.assertEqual(candidate.actual_delivered, "Delivered feature.")
        self.assertEqual(candidate.deviations, ("Caveat.",))
        self.assertFalse(any(item.code == "invalid_effort" for item in result.diagnostics))
        closure = git(self.repo, "rev-parse", "HEAD")
        write(self.repo, "docs/specs/example.md", "---\ntype: TechnicalSpec\n---\n")
        write(self.repo, log, "## Actual Delivered\n\nLate delivery claim.\n")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "edit log after closure")
        late = collect_release_evidence(self.repo, closure)
        self.assertEqual(late.delivery_units[0].efforts, ())
        self.assertIn("post_closure_mutation", {item.code for item in late.diagnostics})

    def test_workspace_types_components_using_each_git_revision_schema(self):
        folder = "docs/efforts/workspace"
        entry = f"{folder}/2026-effort.html"
        schema_path = ".rhizome/ontology/spec-driven.graphql"
        schema = (self.repo / schema_path).read_text()

        def snapshot(status, plan_name):
            metadata = {
                "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00",
                "status": status, "name": "Workspace", "summary": "Deliver feature",
                "created-at": "2026-09-13T10:00:00Z", "governing-specs": [],
                "implementation-plan": f"{folder}/materials/{plan_name}.html",
                "work-log": f"{folder}/work-log.md",
            }
            write(self.repo, entry, '<html><script id="rhizome-metadata" type="application/json">'
                  + json.dumps(metadata) + '</script></html>')
            write(self.repo, metadata["implementation-plan"], "Plan")
            delivered = "None yet." if status == "active" else "Delivered feature."
            write(self.repo, metadata["work-log"], f"## Actual Delivered\n\n{delivered}\n")
            write(self.repo, schema_path, schema.replace(
                '"docs/efforts/*/materials/*.html"', f'"docs/efforts/*/materials/{plan_name}.html"'))
            git(self.repo, "add", ".")
            git(self.repo, "commit", "-m", status)

        snapshot("active", "old-plan")
        before = git(self.repo, "rev-parse", "HEAD")
        snapshot("complete", "new-plan")
        # Neither historical selector exists in the uncommitted schema.
        write(self.repo, schema_path, "invalid working tree schema")
        result = collect_release_evidence(self.repo, before)
        self.assertEqual(len(result.delivery_units[0].efforts), 1, result.diagnostics)
        self.assertEqual(result.delivery_units[0].efforts[0].inclusion_reason, "completed_in_range")
        self.assertFalse(any(item.code == "invalid_effort" for item in result.diagnostics))
        # A later snapshot whose schema excludes the linked plan must fail,
        # even though the parser checkout's broader selector accepts that path.
        write(self.repo, schema_path, schema.replace(
            '"docs/efforts/*/materials/*.html"', '"docs/efforts/*/materials/other.html"'))
        write(self.repo, f"{folder}/work-log.md", "## Actual Delivered\n\nChanged delivery.\n")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "exclude linked plan in schema")
        invalid = collect_release_evidence(self.repo, before)
        self.assertIn("invalid_effort", {item.code for item in invalid.diagnostics})

    def test_workspace_specs_use_each_git_revision_schema(self):
        folder = "docs/efforts/workspace"
        entry = f"{folder}/2026-effort.html"
        spec = "docs/specs/governing.md"
        schema_path = ".rhizome/ontology/spec-driven.graphql"
        schema = (self.repo / schema_path).read_text()

        def snapshot(status, spec_type, schema_text):
            metadata = {
                "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00",
                "status": status, "name": "Workspace", "summary": "Deliver feature",
                "created-at": "2026-09-13T10:00:00Z", "governing-specs": [spec],
                "implementation-plan": f"{folder}/plan.html",
                "work-log": f"{folder}/work-log.md",
            }
            write(self.repo, entry, '<html><script id="rhizome-metadata" type="application/json">'
                  + json.dumps(metadata) + '</script></html>')
            write(self.repo, metadata["implementation-plan"], "Plan")
            delivered = "None yet." if status == "active" else f"Delivered feature governed by {spec_type}."
            write(self.repo, metadata["work-log"], f"## Actual Delivered\n\n{delivered}\n")
            write(self.repo, spec, f"---\ntype: {spec_type}\n---\n")
            write(self.repo, schema_path, schema_text)
            git(self.repo, "add", ".")
            git(self.repo, "commit", "-m", status)

        snapshot("active", "TechnicalSpec", schema)
        before = git(self.repo, "rev-parse", "HEAD")
        snapshot("complete", "RevisionSpec", schema.replace("TechnicalSpec", "RevisionSpec"))
        write(self.repo, schema_path, "invalid working tree schema")
        result = collect_release_evidence(self.repo, before)
        self.assertEqual(len(result.delivery_units[0].efforts), 1, result.diagnostics)
        self.assertEqual(result.delivery_units[0].efforts[0].inclusion_reason, "completed_in_range")

        # A familiar authored type is invalid once the snapshot removes its interface.
        snapshot("complete", "TechnicalSpec", schema.replace(
            "type TechnicalSpec\n  implements SpecLike", "type TechnicalSpec"))
        invalid = collect_release_evidence(self.repo, before)
        self.assertTrue(any("target is not SpecLike" in item.message for item in invalid.diagnostics),
                        invalid.diagnostics)

    def test_missing_material_uses_parent_and_final_git_snapshots(self):
        folder = "docs/efforts/workspace"
        entry = f"{folder}/2026-effort.html"
        for missing_snapshot in ("parent", "final"):
            with self.subTest(missing_snapshot=missing_snapshot):
                def html(status, materials):
                    return '<html><head><script id="rhizome-metadata" type="application/json">' + json.dumps({
                        "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00", "status": status,
                        "name": "Workspace", "summary": "Deliver the feature.",
                        "created-at": "2026-09-13T10:00:00Z", "governing-specs": [],
                        "implementation-plan": f"{folder}/plan.html", "work-log": f"{folder}/work-log.md",
                        "materials": materials,
                    }) + '</script></head><body></body></html>'

                # Separate paths keep both cases independent in this repository.
                material = f"{folder}/materials/{missing_snapshot}.md"
                write(self.repo, entry, html("active", [material] if missing_snapshot == "parent" else []))
                write(self.repo, f"{folder}/plan.html", "Plan")
                write(self.repo, f"{folder}/work-log.md", "## Actual Delivered\n\nNone yet.\n")
                git(self.repo, "add", ".")
                git(self.repo, "commit", "-m", "start material snapshot case")
                before = git(self.repo, "rev-parse", "HEAD")
                write(self.repo, entry, html("complete", [material]))
                write(self.repo, f"{folder}/work-log.md", "## Actual Delivered\n\nDelivered feature.\n")
                write(self.repo, "src/feature.txt", missing_snapshot)
                if missing_snapshot == "parent":
                    write(self.repo, material, "Report")
                git(self.repo, "add", ".")
                git(self.repo, "commit", "-m", "close material snapshot case")
                # A working-tree file cannot repair a missing committed blob.
                write(self.repo, material, "Uncommitted report")

                result = collect_release_evidence(self.repo, before)

                self.assertEqual(result.delivery_units[0].efforts, ())
                diagnostics = [item for item in result.diagnostics if item.source == entry]
                self.assertEqual([item.code for item in diagnostics], ["invalid_effort"])
                self.assertIn("linked materials is missing at this snapshot", diagnostics[0].message)
                self.assertIn(material, diagnostics[0].message)

    def test_malformed_historical_schema_stops_collection(self):
        entry = "docs/efforts/workspace/2026-effort.html"
        metadata = {
            "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00",
            "status": "complete", "name": "Workspace", "summary": "Deliver feature",
            "created-at": "2026-09-13T10:00:00Z",
            "implementation-plan": "docs/efforts/workspace/plan.html",
            "work-log": "docs/efforts/workspace/work-log.md", "governing-specs": [],
        }
        write(self.repo, entry, '<html><head><script id="rhizome-metadata" type="application/json">'
              + json.dumps(metadata) + '</script></head><body></body></html>')
        write(self.repo, metadata["implementation-plan"], "Plan")
        write(self.repo, metadata["work-log"], "## Actual Delivered\n\nDelivered feature.\n")
        write(self.repo, "src/feature.txt", "shipped")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "close valid workspace")
        valid_head = git(self.repo, "rev-parse", "HEAD")
        result = collect_release_evidence(self.repo, self.base)
        self.assertEqual(len(result.delivery_units[0].efforts), 1)

        for schema in ("type Broken {", "type Broken @node { field: UndefinedType }"):
            with self.subTest(schema=schema):
                write(self.repo, ".rhizome/ontology/broken.graphql", schema)
                git(self.repo, "add", ".")
                git(self.repo, "commit", "--amend", "--no-edit")
                # A healthy checkout must not hide a broken committed snapshot.
                write(self.repo, ".rhizome/ontology/broken.graphql", "# repaired locally\n")
                with self.assertRaisesRegex(effort_evidence.CanonicalContractError, "invalid snapshot ontology"):
                    collect_release_evidence(self.repo, self.base)
        result = collect_release_evidence(self.repo, self.base, head=valid_head)
        self.assertEqual(len(result.delivery_units[0].efforts), 1)

    def test_workspace_parser_tool_failures_stop_collection(self):
        entry = "docs/efforts/workspace/2026-effort.html"
        metadata = {
            "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00",
            "status": "complete", "name": "Workspace", "summary": "Deliver feature",
            "created-at": "2026-09-13T10:00:00Z",
            "implementation-plan": "docs/efforts/workspace/plan.html",
            "work-log": "docs/efforts/workspace/work-log.md", "governing-specs": [],
        }
        write(self.repo, entry, '<html><head><script id="rhizome-metadata" type="application/json">'
              + json.dumps(metadata) + '</script></head><body></body></html>')
        write(self.repo, metadata["implementation-plan"], "Plan")
        write(self.repo, metadata["work-log"], "## Actual Delivered\n\nDelivered feature.\n")
        write(self.repo, "src/feature.txt", "shipped")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "close workspace")
        cases = [
            (FileNotFoundError("go unavailable"), "go unavailable"),
            (effort_evidence.CanonicalContractError("toolchain download failed"), "toolchain download failed"),
            ("not JSON", "invalid canonical"),
            ('{"result": null}', "invalid canonical"),
        ]
        for failure, message in cases:
            with self.subTest(message=message, failure=failure):
                effort_evidence._canonical_contract.cache_clear()
                replacement = ({"side_effect": failure} if isinstance(failure, Exception)
                               else {"return_value": failure})
                with patch("effort_evidence._canonical_contract_response", **replacement):
                    with self.assertRaisesRegex(effort_evidence.CanonicalContractError, message):
                        collect_release_evidence(self.repo, self.base)
        effort_evidence._canonical_contract.cache_clear()
        result = collect_release_evidence(self.repo, self.base)
        self.assertEqual(len(result.delivery_units[0].efforts), 1)
        metadata["id"] = "EFF-0042"
        write(self.repo, entry, '<html><head><script id="rhizome-metadata" type="application/json">'
              + json.dumps(metadata) + '</script></head><body></body></html>')
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "invalid workspace id")
        result = collect_release_evidence(self.repo, "HEAD^")
        self.assertEqual(result.delivery_units[0].efforts, ())
        self.assertTrue(any(item.code == "invalid_effort" and "DATETIME" in item.message
                            for item in result.diagnostics))

    def test_governing_spec_uses_discovery_policy_from_each_snapshot(self):
        folder = "docs/efforts/workspace"
        entry = f"{folder}/2026-effort.html"
        target = "docs/specs/Feature.MD"
        config = ".rhizome/config.yml"
        policies = (
            ({}, True),
            ({config: 'notes:\n  includes: ["docs/**/*.md", "docs/efforts/**/*.html"]\n'}, True),
            ({config: 'notes:\n  includes: ["docs/efforts/**/*"]\n'}, False),
            ({config: 'notes:\n  excludes: ["docs/specs/**"]\n'}, False),
            ({".rhizome/ignore": "docs/specs/\n"}, False),
            ({"docs/.gitignore": "specs/\n"}, False),
            ({".gitignore": "docs/specs/\n", ".rhizome/ignore": "!docs/specs/\n"}, True),
            ({".obsidianignore": "docs/specs/\n"}, False),
        )
        for policy, accepted in policies:
            for excluded_parent in (False, True):
                with self.subTest(policy=policy, excluded_parent=excluded_parent):
                    self.setUp()
                    # Include empty policy files to exercise default/fallback semantics.
                    for name in (config, ".rhizome/ignore", ".obsidianignore", ".gitignore", "docs/.gitignore"):
                        write(self.repo, name, policy.get(name, 'notes:\n  includes: ["**/*.md", "**/*.html"]\n' if name == config else ""))
                    metadata = {
                        "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00",
                        "status": "active" if excluded_parent else "complete", "name": "Workspace",
                        "summary": "Deliver feature", "created-at": "2026-09-13T10:00:00Z",
                        "governing-specs": [target], "implementation-plan": f"{folder}/plan.html",
                        "work-log": f"{folder}/work-log.md",
                    }
                    def write_entry():
                        write(self.repo, entry, '<head><script id="rhizome-metadata" type="application/json">'
                              + json.dumps(metadata) + '</script></head>')
                    write_entry()
                    write(self.repo, target, "---\ntype: TechnicalSpec\n---\n")
                    write(self.repo, f"{folder}/plan.html", "Plan")
                    write(self.repo, f"{folder}/work-log.md", "## Actual Delivered\n\nNone yet.\n" if excluded_parent
                          else "## Actual Delivered\n\nDelivered feature.\n")
                    write(self.repo, "src/feature.txt", json.dumps(policy) + str(excluded_parent))
                    git(self.repo, "add", "--force", ".")
                    git(self.repo, "commit", "--allow-empty", "-m", "snapshot policy")
                    before = self.base
                    if excluded_parent:
                        before = git(self.repo, "rev-parse", "HEAD")
                        for name in (config, ".rhizome/ignore", ".obsidianignore", ".gitignore", "docs/.gitignore"):
                            write(self.repo, name, 'notes:\n  includes: ["**/*.md", "**/*.html"]\n' if name == config else "")
                        metadata["status"] = "complete"
                        write_entry()
                        write(self.repo, f"{folder}/work-log.md", "## Actual Delivered\n\nDelivered feature.\n")
                        write(self.repo, "src/feature.txt", "closed")
                        git(self.repo, "add", "--force", ".")
                        git(self.repo, "commit", "-m", "close with visible spec")
                    # Live config intentionally contradicts the committed policy.
                    write(self.repo, config, 'notes:\n  includes: ["other/**/*.md"]\n' if accepted else "")
                    result = collect_release_evidence(self.repo, before)
                    unit = result.delivery_units[-1]
                    self.assertEqual(bool(unit.efforts), accepted)
                    if not accepted:
                        self.assertTrue(any(item.code == "invalid_effort" and "not a discovered note" in item.message
                                            for item in result.diagnostics))

    def test_components_use_discovery_policy_from_each_snapshot(self):
        folder = "docs/efforts/workspace"
        entry = f"{folder}/2026-effort.html"
        config = ".rhizome/config.yml"
        policies = []
        for target in (f"{folder}/plan.html", f"{folder}/work-log.md", f"{folder}/materials/detail.md"):
            for policy in (
                {config: "notes:\n  includes: " + json.dumps([path for path in (entry, f"{folder}/plan.html", f"{folder}/work-log.md", f"{folder}/materials/detail.md") if path != target]) + "\n"},
                {config: f'notes:\n  includes: ["**/*.md", "**/*.html"]\n  excludes: ["{target}"]\n'},
                {".rhizome/ignore": target + "\n"},
            ):
                policies.append((target, policy))
        for target, policy in policies:
            accepted = False
            for excluded_parent in (False, True):
                with self.subTest(policy=policy, excluded_parent=excluded_parent):
                    self.setUp()
                    # Include empty policy files to exercise default/fallback semantics.
                    for name in (config, ".rhizome/ignore", ".obsidianignore", ".gitignore", "docs/.gitignore"):
                        write(self.repo, name, policy.get(name, 'notes:\n  includes: ["**/*.md", "**/*.html"]\n' if name == config else ""))
                    metadata = {
                        "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00",
                        "status": "active" if excluded_parent else "complete", "name": "Workspace",
                        "summary": "Deliver feature", "created-at": "2026-09-13T10:00:00Z",
                        "governing-specs": [], "materials": [f"{folder}/materials/detail.md"], "implementation-plan": f"{folder}/plan.html",
                        "work-log": f"{folder}/work-log.md",
                    }
                    def write_entry():
                        write(self.repo, entry, '<head><script id="rhizome-metadata" type="application/json">'
                              + json.dumps(metadata) + '</script></head>')
                    write_entry()
                    write(self.repo, f"{folder}/materials/detail.md", "Material")
                    write(self.repo, f"{folder}/plan.html", "Plan")
                    write(self.repo, f"{folder}/work-log.md", "## Actual Delivered\n\nNone yet.\n" if excluded_parent
                          else "## Actual Delivered\n\nDelivered feature.\n")
                    write(self.repo, "src/feature.txt", json.dumps(policy) + str(excluded_parent))
                    git(self.repo, "add", "--force", ".")
                    git(self.repo, "commit", "--allow-empty", "-m", "snapshot policy")
                    before = self.base
                    if excluded_parent:
                        before = git(self.repo, "rev-parse", "HEAD")
                        for name in (config, ".rhizome/ignore", ".obsidianignore", ".gitignore", "docs/.gitignore"):
                            write(self.repo, name, 'notes:\n  includes: ["**/*.md", "**/*.html"]\n' if name == config else "")
                        metadata["status"] = "complete"
                        write_entry()
                        write(self.repo, f"{folder}/work-log.md", "## Actual Delivered\n\nDelivered feature.\n")
                        write(self.repo, "src/feature.txt", "closed")
                        git(self.repo, "add", "--force", ".")
                        git(self.repo, "commit", "-m", "close with visible spec")
                    # Live config intentionally contradicts the committed policy.
                    write(self.repo, config, 'notes:\n  includes: ["other/**/*.md"]\n' if accepted else "")
                    result = collect_release_evidence(self.repo, before)
                    unit = result.delivery_units[-1]
                    self.assertEqual(bool(unit.efforts), accepted)
                    if not accepted:
                        self.assertTrue(any(item.code == "invalid_effort" and "not a discovered note" in item.message
                                            for item in result.diagnostics))

    def test_uncommitted_plan_cannot_rescue_missing_snapshot_target(self):
        folder = "docs/efforts/workspace"
        entry = f"{folder}/2026-effort.html"
        plan = f"{folder}/plan.html"
        log = f"{folder}/work-log.md"
        write(self.repo, entry, '<html><head><script id="rhizome-metadata" type="application/json">' + json.dumps({
            "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00", "aliases": ["EFF-2026-09-13-10-00"], "status": "complete",
            "name": "Workspace", "summary": "Deliver the feature.", "created-at": "2026-09-13T10:00:00Z",
            "governing-specs": [], "implementation-plan": plan, "work-log": log,
        }) + '</script></head><body></body></html>')
        write(self.repo, log, "## Actual Delivered\n\nDelivered workspace.\n")
        write(self.repo, "src/feature.txt", "shipped")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "close without plan")
        write(self.repo, plan, "Uncommitted implementation plan")
        result = collect_release_evidence(self.repo, self.base)
        self.assertEqual(result.delivery_units[0].efforts, ())
        self.assertTrue(any(item.code == "invalid_effort" and "implementation-plan" in item.message
                            for item in result.diagnostics))

    def test_invalid_material_does_not_hide_independent_effort(self):
        folder = "docs/efforts/workspace"
        entry = f"{folder}/2026-effort.html"
        historical = "docs/efforts/independent.md"
        write(self.repo, entry, '<html><head><script id="rhizome-metadata" type="application/json">' + json.dumps({
            "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00", "aliases": ["EFF-2026-09-13-10-00"], "status": "complete",
            "name": "Workspace", "summary": "Deliver the feature.", "created-at": "2026-09-13T10:00:00Z",
            "governing-specs": [], "implementation-plan": f"{folder}/plan.html", "work-log": f"{folder}/work-log.md",
            "materials": [historical, "src/feature.txt"],
        }) + '</script></head><body></body></html>')
        write(self.repo, f"{folder}/plan.html", "Implementation plan")
        write(self.repo, f"{folder}/work-log.md", "## Actual Delivered\n\nDelivered workspace.\n")
        write(self.repo, historical, effort("EFF-0043", "complete", "Delivered independent work."))
        write(self.repo, "src/feature.txt", "shipped")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "close with invalid material")
        result = collect_release_evidence(self.repo, self.base)
        selected = [item.effort_id for unit in result.delivery_units for item in unit.efforts]
        self.assertEqual(selected, ["EFF-0043"])
        self.assertTrue(any(item.code == "invalid_effort" and "materials" in item.message
                            for item in result.diagnostics))
        self.assertFalse(any(item.code == "missing_delivery_proof" for item in result.diagnostics))

    def test_invalid_materials_retain_owner_when_only_work_log_changes(self):
        folder = "docs/efforts/workspace"
        entry = f"{folder}/2026-effort.html"
        log = f"{folder}/work-log.md"
        for materials, diagnostic in (
            (42, "invalid_effort"),
            (["src/feature.txt"], "invalid_effort"),
            ([None], "invalid_effort"),
            ([f"{folder}/materials/missing.md"], "invalid_effort"),
        ):
            with self.subTest(materials=materials):
                write(self.repo, entry, '<html><head><script id="rhizome-metadata" type="application/json">' + json.dumps({
                    "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00", "aliases": ["EFF-2026-09-13-10-00"], "status": "complete",
                    "name": "Workspace", "summary": "Deliver the feature.", "created-at": "2026-09-13T10:00:00Z",
                    "governing-specs": [], "implementation-plan": f"{folder}/plan.html", "work-log": log,
                    "materials": materials,
                }) + '</script></head><body></body></html>')
                write(self.repo, f"{folder}/plan.html", "Implementation plan")
                write(self.repo, log, effort("EFF-0044", "complete", "Initial delivery.").replace("type: EffortNote", "type: EffortMaterial"))
                git(self.repo, "add", ".")
                git(self.repo, "commit", "-m", "record invalid workspace")
                before = git(self.repo, "rev-parse", "HEAD")
                write(self.repo, log, effort("EFF-0044", "complete", "Updated delivery.").replace("type: EffortNote", "type: EffortMaterial"))
                git(self.repo, "add", ".")
                git(self.repo, "commit", "-m", "update linked work log")

                result = collect_release_evidence(self.repo, before)

                self.assertEqual(result.delivery_units[0].efforts, ())
                diagnostics = [item for item in result.diagnostics if item.source == entry]
                self.assertEqual([item.code for item in diagnostics], [diagnostic])
                if diagnostic == "invalid_effort":
                    self.assertIn("materials", diagnostics[0].message)
                self.assertFalse(any(item.source == log for item in result.diagnostics))

    def test_invalid_metadata_retains_owner_when_only_work_log_changes(self):
        folder = "docs/efforts/workspace"
        entry = f"{folder}/2026-effort.html"
        log = f"{folder}/work-log.md"
        for invalid_field in ('"name": "First", "name": "Second"', '"name": 42'):
            with self.subTest(invalid_field=invalid_field):
                metadata = json.dumps({
                    "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00", "aliases": ["EFF-2026-09-13-10-00"], "status": "complete",
                    "summary": "Deliver the feature.", "created-at": "2026-09-13T10:00:00Z",
                    "governing-specs": [], "implementation-plan": f"{folder}/plan.html", "work-log": log,
                })[:-1] + ", " + invalid_field + "}"
                write(self.repo, entry, '<html><head><script id="rhizome-metadata" type="application/json">'
                      + metadata + '</script></head><body></body></html>')
                write(self.repo, f"{folder}/plan.html", "Implementation plan")
                write(self.repo, log, effort("EFF-0044", "complete", "Initial delivery.").replace("type: EffortNote", "type: EffortMaterial"))
                git(self.repo, "add", ".")
                git(self.repo, "commit", "-m", "record invalid metadata")
                before = git(self.repo, "rev-parse", "HEAD")
                write(self.repo, log, effort("EFF-0044", "complete", "Updated delivery.").replace("type: EffortNote", "type: EffortMaterial"))
                git(self.repo, "add", ".")
                git(self.repo, "commit", "-m", "update linked work log")

                result = collect_release_evidence(self.repo, before)

                self.assertEqual(result.delivery_units[0].efforts, ())
                diagnostics = [item for item in result.diagnostics if item.source == entry]
                self.assertEqual([item.code for item in diagnostics], ["invalid_effort"])
                self.assertIn("name", diagnostics[0].message)
                self.assertFalse(any(item.source == log for item in result.diagnostics))

    def test_workspace_groups_typed_components_and_retains_independent_effort(self):
        folder = "docs/efforts/workspace"
        entry = f"{folder}/2026-effort.html"
        log = f"{folder}/work-log.md"
        material = f"{folder}/materials/selected.md"
        historical = f"{folder}/historical.md"
        write(self.repo, entry, '<html><head><script id="rhizome-metadata" type="application/json">' + json.dumps({
            "type": "EffortWorkspace", "id": "EFF-2026-09-13-10-00", "aliases": ["EFF-2026-09-13-10-00"], "status": "complete",
            "name": "Workspace", "summary": "Deliver the feature.", "created-at": "2026-09-13T10:00:00Z",
            "governing-specs": [], "implementation-plan": f"{folder}/plan.html", "work-log": log,
            "materials": [material],
        }) + '</script></head><body></body></html>')
        write(self.repo, log, effort("EFF-0044", "complete", "Delivered workspace.").replace("type: EffortNote", "type: EffortMaterial"))
        write(self.repo, f"{folder}/plan.html", "Implementation plan")
        write(self.repo, material, effort("EFF-0045", "complete", "Delivered supporting material.").replace("type: EffortNote", "type: EffortMaterial"))
        write(self.repo, historical, effort("EFF-0043", "complete", "Delivered historical feature."))
        write(self.repo, f"{folder}/collateral.md", "Unlinked collateral")
        write(self.repo, "src/feature.txt", "shipped")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "record workspace and historical effort")
        result = collect_release_evidence(self.repo, self.base)
        self.assertEqual(
            {item.effort_id for item in result.delivery_units[0].efforts},
            {"EFF-2026-09-13-10-00", "EFF-0043"},
        )
        self.assertFalse(any(item.code == "invalid_effort" for item in result.diagnostics))
        before = git(self.repo, "rev-parse", "HEAD")
        write(self.repo, f"{folder}/collateral.md", "Updated unlinked collateral")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "update collateral")
        collateral = collect_release_evidence(self.repo, before)
        self.assertEqual(collateral.delivery_units[0].efforts, ())
        self.assertFalse(any(item.source == entry for item in collateral.diagnostics))
        before = git(self.repo, "rev-parse", "HEAD")
        write(self.repo, material, "Updated selected evidence")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "update selected material")
        selected = collect_release_evidence(self.repo, before)
        self.assertTrue(any(item.source == entry for item in selected.diagnostics))

    def test_release_evidence_round_trips_with_the_same_fingerprint(self):
        write(self.repo, "src/fix.txt", "fixed\n")
        git(self.repo, "add", ".")
        git(self.repo, "commit", "-m", "fix: reliable release")

        result = collect_release_evidence(self.repo, self.base)

        self.assertEqual(type(result).from_json(result.to_json()), result)
        self.assertTrue(result.fingerprint.startswith("sha256:"))


class EffortRankingTest(unittest.TestCase):
    def evidence(self, effort_id: str, delivered: str) -> EffortEvidence:
        return EffortEvidence(
            effort_id=effort_id,
            path=f"docs/efforts/{effort_id}.md",
            name=effort_id,
            actual_delivered=delivered,
        )

    def test_integration_effort_becomes_primary_without_dropping_children(self):
        efforts = (
            self.evidence("EFF-0046", "Delivered projection refresh."),
            self.evidence("EFF-0047", "Delivered transactions."),
            self.evidence(
                "EFF-0051",
                "Integrated EFF-0046 through EFF-0050 into the parent delivery.",
            ),
        )

        ranked = rank_efforts(efforts)

        self.assertEqual(ranked[0].effort_id, "EFF-0051")
        self.assertEqual(ranked[0].role, "primary")
        self.assertEqual({item.role for item in ranked[1:]}, {"supporting"})

    def test_peer_efforts_remain_peers_without_an_integration_account(self):
        efforts = (
            self.evidence("EFF-0041", "Delivered one change."),
            self.evidence("EFF-0042", "Delivered another change."),
        )

        ranked = rank_efforts(efforts)

        self.assertEqual([item.role for item in ranked], ["peer", "peer"])

    def test_numeric_ranges_and_datetime_ids_rank_together(self):
        workspace_id = "EFF-2026-09-13-10-00"
        efforts = (
            self.evidence("EFF-0046", "Delivered projection refresh."),
            self.evidence(workspace_id, "Delivered workspace support."),
            self.evidence(
                "EFF-0051",
                f"Integrated EFF-0046 through EFF-0050 and {workspace_id}.",
            ),
        )

        ranked = rank_efforts(efforts)

        self.assertEqual(ranked[0].effort_id, "EFF-0051")
        self.assertEqual(ranked[0].role, "primary")


if __name__ == "__main__":
    unittest.main()
