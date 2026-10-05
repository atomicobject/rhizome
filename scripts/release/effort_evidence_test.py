#!/usr/bin/env python3

import json
import re
import unittest
from pathlib import Path

import effort_evidence


ROOT = Path(__file__).resolve().parents[2]


def effort(
    status,
    actual,
    *,
    deviations="None.",
    effort_id="EFF-0042",
    name='"Release evidence"',
    extra="",
):
    return f"""\
---
type: EffortNote
id: {effort_id}
name: {name}
status: {status}
summary: Test fixture
{extra}---

# Release evidence

## Actual Delivered

{actual}

## Deviations

{deviations}

## Status

{status.title()}.
"""


class ParseEffortSnapshotTest(unittest.TestCase):
    def test_shipped_markdown_template_is_release_evidence(self):
        path = ROOT / "pkg/app/cli/init/templates/starters/agentic-engineering/repo/docs/efforts/templates/effort.md.template"
        title = 'Effort "quoted" with \\path\nand a second line'
        values = {
            "effort_id_yaml": json.dumps("EFF-2026-09-13-10-00"),
            "effort_title_yaml": json.dumps(title),
            "summary_yaml": json.dumps('Summary "quoted" with \\path\nand another line'),
            "created_at_iso_yaml": json.dumps("2026-09-13T14:00:00Z"),
            "effort_title": "Release effort",
        }
        text = re.sub(r"\{\{(\w+)\}\}", lambda match: values[match[1]], path.read_text())
        snapshot = effort_evidence.parse_effort_snapshot(text, "docs/efforts/new.md")
        self.assertEqual(snapshot.effort_id, "EFF-2026-09-13-10-00")
        self.assertEqual(snapshot.status, "planned")
        self.assertEqual(snapshot.name, title)

    def test_dogfood_pilot_is_release_evidence(self):
        path = "docs/efforts/2026-09-13-09-55-html-effort-pilot/2026-09-13-09-55-effort.html"
        reads = []

        def read_link(link):
            reads.append(link)
            return (ROOT / link).read_text()

        snapshot = effort_evidence.parse_effort_snapshot(
            (ROOT / path).read_text(), path, read_link=read_link,
        )
        self.assertEqual(snapshot.effort_id, "EFF-2026-09-13-09-55")
        self.assertEqual(reads[-2:], [str(Path(path).parent / name) for name in ("plan.html", "work-log.md")])
        self.assertEqual(snapshot.name, "HTML efforts and connected materials")
        self.assertTrue(snapshot.actual_delivered)

    def test_parses_frontmatter_and_delivery_sections(self):
        snapshot = effort_evidence.parse_effort_snapshot(
            effort(
                "complete",
                "- Added bounded evidence.\n- Preserved attribution.",
                deviations="- GitHub data is optional.\n- Full diffs are excluded.",
            ),
            "docs/efforts/release-evidence.md",
        )

        self.assertEqual(snapshot.effort_id, "EFF-0042")
        self.assertEqual(snapshot.name, "Release evidence")
        self.assertEqual(snapshot.status, "complete")
        self.assertEqual(
            snapshot.actual_delivered,
            "- Added bounded evidence.\n- Preserved attribution.",
        )
        self.assertEqual(
            snapshot.deviations,
            ("GitHub data is optional.", "Full diffs are excluded."),
        )

    def test_ignores_lifecycle_headings_inside_code_fences(self):
        text = """---
type: EffortNote
id: EFF-0042
name: Release evidence
status: complete
---
# Release evidence
```markdown
## Actual Delivered

Example delivery text.

## Deviations

- Example deviation.
```
"""
        snapshot = effort_evidence.parse_effort_snapshot(
            text, "docs/efforts/release-evidence.md"
        )

        self.assertEqual(snapshot.actual_delivered, "")
        self.assertEqual(snapshot.deviations, ())

    def test_rejects_non_effort_notes(self):
        text = effort("complete", "Shipped.").replace("type: EffortNote", "type: TechnicalSpec")

        with self.assertRaisesRegex(ValueError, "not an EffortNote"):
            effort_evidence.parse_effort_snapshot(text, "docs/efforts/not-effort.md")


class SelectEffortEvidenceTest(unittest.TestCase):
    def select(self, parent, final, changed_paths=("pkg/release/release.go",)):
        return effort_evidence.select_effort_evidence(
            parent,
            final,
            path="docs/efforts/release-evidence.md",
            changed_paths=changed_paths,
        )

    def test_includes_new_complete_effort_with_delivery_proof(self):
        result = self.select(None, effort("complete", "- Shipped bounded release evidence."))

        self.assertEqual(result.candidate.effort_id, "EFF-0042")
        self.assertEqual(result.candidate.inclusion_reason, "new_complete")
        self.assertEqual(result.diagnostics, ())

    def test_includes_completion_transition_with_changed_delivery(self):
        parent = effort("active", "None yet.")
        final = effort(
            "complete",
            "- Shipped bounded release evidence.",
            deviations="- GitHub lookup degrades to local evidence.",
        )

        result = self.select(parent, final)

        self.assertEqual(result.candidate.inclusion_reason, "completed_in_range")
        self.assertEqual(
            result.candidate.deviations,
            ("GitHub lookup degrades to local evidence.",),
        )

    def test_excludes_active_effort(self):
        result = self.select(None, effort("active", "- Work in progress."))

        self.assertIsNone(result.candidate)
        self.assertEqual([item.code for item in result.diagnostics], ["active_effort"])

    def test_excludes_completion_when_actual_delivered_was_unchanged(self):
        delivered = "- Delivery text was written before closure."
        result = self.select(effort("active", delivered), effort("complete", delivered))

        self.assertIsNone(result.candidate)
        self.assertEqual(
            [item.code for item in result.diagnostics],
            ["unchanged_actual_delivered"],
        )

    def test_link_normalization_does_not_count_as_delivery_change(self):
        parent = effort("active", "- Delivered [SPEC-0001](../old.md).")
        final = effort("complete", "* Delivered [[new-location|SPEC-0001]].")

        result = self.select(parent, final)

        self.assertIsNone(result.candidate)
        self.assertEqual(
            [item.code for item in result.diagnostics],
            ["unchanged_actual_delivered"],
        )

    def test_excludes_placeholder_actual_delivered(self):
        result = self.select(
            effort("active", "None yet."),
            effort("complete", "TBD"),
        )

        self.assertIsNone(result.candidate)
        self.assertEqual(
            [item.code for item in result.diagnostics],
            ["missing_actual_delivered"],
        )

    def test_excludes_post_closure_delivery_mutation(self):
        parent = effort("complete", "- Shipped the original behavior.")
        final = effort("complete", "- Shipped different behavior after closure.")

        result = self.select(parent, final)

        self.assertIsNone(result.candidate)
        self.assertEqual(
            [item.code for item in result.diagnostics],
            ["post_closure_mutation"],
        )

    def test_deviation_change_does_not_trigger_post_closure_inclusion(self):
        actual = "- Shipped the original behavior."
        parent = effort("complete", actual)
        final = effort("complete", actual, deviations="- Added a late caveat.")

        result = self.select(parent, final)

        self.assertIsNone(result.candidate)
        self.assertEqual(
            [item.code for item in result.diagnostics],
            ["post_closure_mutation"],
        )

    def test_excludes_administrative_only_change(self):
        parent = effort("complete", "- Shipped bounded release evidence.")
        final = effort(
            "complete",
            "- Shipped bounded release evidence.",
            extra="audit-status: complete\n",
        )

        result = self.select(parent, final)

        self.assertIsNone(result.candidate)
        self.assertEqual(
            [item.code for item in result.diagnostics],
            ["administrative_only"],
        )

    def test_excludes_effort_only_change_without_delivery_proof(self):
        result = self.select(
            effort("active", "None yet."),
            effort("complete", "- Claims a delivery."),
            changed_paths=("docs/efforts/release-evidence.md",),
        )

        self.assertIsNone(result.candidate)
        self.assertEqual(
            [item.code for item in result.diagnostics],
            ["missing_delivery_proof"],
        )

    def test_non_effort_docs_count_as_range_local_delivery_proof(self):
        result = self.select(
            effort("active", "None yet."),
            effort("complete", "- Claims a delivery."),
            changed_paths=(
                "docs/efforts/release-evidence.md",
                "docs/specs/release-evidence.md",
            ),
        )

        self.assertEqual(result.candidate.inclusion_reason, "completed_in_range")
        self.assertEqual(result.diagnostics, ())

    def test_invalid_final_snapshot_becomes_a_diagnostic(self):
        result = self.select(None, "# no frontmatter")

        self.assertIsNone(result.candidate)
        self.assertEqual([item.code for item in result.diagnostics], ["invalid_effort"])


def material_effort(*args, **kwargs):
    return effort(*args, **kwargs).replace("type: EffortNote", "type: EffortMaterial")


class WorkspaceEvidenceTest(unittest.TestCase):
    path = "docs/efforts/example/2026-effort.html"
    log_path = "docs/efforts/example/work-log.md"

    def entry(self, status="complete", **fields):
        metadata = dict(type="EffortWorkspace", id="EFF-2026-09-13-10-00", aliases=["EFF-2026-09-13-10-00"], status=status,
                        name='Quotes " & literal </script>', summary="Deliver the feature.",
                        **{"governing-specs": [], "created-at": "2026-09-13T10:00:00Z", "implementation-plan": "docs/efforts/example/plan.html", "work-log": self.log_path})
        metadata.update(fields)
        payload = json.dumps(metadata).replace("<", "\\u003c")
        return "<html><head><script type='application/json' id='rhizome-metadata'>" + payload + "</script></head><body></body></html>"

    def test_components_require_discovered_correctly_projected_snapshot_targets(self):
        targets = (("implementation-plan", "docs/efforts/example/plan.html"),
                   ("work-log", self.log_path),
                   ("materials", "docs/efforts/example/materials/detail.md"),
                   ("materials", "docs/efforts/example/materials/detail.html"))
        for key, target in targets:
            for problem in ("excluded", "wrong-type", "malformed"):
                for parent in (False, True):
                    with self.subTest(key=key, target=target, problem=problem, parent=parent):
                        fields = {key: [target] if key == "materials" else target}
                        if target.endswith(".html"):
                            bad = '<head><script id="rhizome-metadata" type="application/json">' + (
                                '{"type":"TechnicalSpec"}' if problem == "wrong-type" else '{broken') + '</script></head>'
                        else:
                            bad = "---\ntype: TechnicalSpec\n---\n" if problem == "wrong-type" else "---\ntype: [broken\n---\n"
                        def valid_read(path):
                            return "## Actual Delivered\n\nDelivered feature."
                        def invalid_read(path):
                            return bad if path == target and problem != "excluded" else valid_read(path)
                        result = effort_evidence.select_effort_evidence(
                            self.entry("active", **fields) if parent else None, self.entry(**fields),
                            path=self.path, changed_paths=["src/feature.go"],
                            parent_read_link=invalid_read if parent else valid_read,
                            final_read_link=valid_read if parent else invalid_read,
                            parent_is_discovered=lambda path: not (parent and problem == "excluded" and path == target),
                            final_is_discovered=lambda path: not (not parent and problem == "excluded" and path == target),
                        )
                        self.assertIsNone(result.candidate)
                        self.assertEqual(result.diagnostics[0].code, "invalid_effort")

    def test_required_workspace_scalars_reject_blank_snapshots(self):
        for key in ("id", "name", "created-at", "status", "summary", "implementation-plan", "work-log"):
            for value in ("", " \t\n ", None, 42):
                for parent in (False, True):
                    with self.subTest(key=key, value=value, parent=parent):
                        invalid = self.entry(**{key: value})
                        result = effort_evidence.select_effort_evidence(
                            invalid if parent else None, self.entry() if parent else invalid,
                            path=self.path, changed_paths=["src/feature.go"],
                            parent_read_link=lambda _: "Plan",
                            final_read_link=lambda _: "## Actual Delivered\n\nShipped feature.",
                        )
                        self.assertFalse(result.candidate)
                        self.assertEqual(result.diagnostics[0].code, "invalid_effort")

    def test_ignored_end_tags_preserve_head_metadata(self):
        for tag in ("div", "span", "script", "title", "template", "unknown"):
            for prefix in ("", "<html>", "<html><head>"):
                with self.subTest(tag=tag, prefix=prefix):
                    html = self.entry().replace("<html><head>", prefix + f"</{tag}>")
                    self.assertEqual(effort_evidence._workspace_metadata(html)["status"], "complete")

    def test_stray_head_end_tag_before_head_is_ignored(self):
        for prefix in ("", "<html>"):
            with self.subTest(prefix=prefix):
                html = self.entry().replace("<html><head>", prefix + "</head>")
                self.assertEqual(effort_evidence._workspace_metadata(html)["status"], "complete")

    def test_rename_uses_original_parent_selector(self):
        for original, reason in (("docs/efforts/example/draft.html", "new_complete"),
                                 ("docs/efforts/example/old-effort.html", None)):
            with self.subTest(original=original):
                result = effort_evidence.select_effort_evidence(
                    self.entry(), self.entry(), path=self.path, parent_path=original,
                    changed_paths=("src/feature.go",),
                    parent_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                    final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                )
                if reason:
                    self.assertEqual(result.candidate.inclusion_reason, reason)
                else:
                    self.assertIsNone(result.candidate)
                    self.assertEqual(result.diagnostics[0].code, "administrative_only")

    def test_required_metadata_fields_cannot_be_omitted(self):
        for key in ("id", "name", "created-at", "status", "summary", "implementation-plan", "work-log", "governing-specs"):
            for parent_invalid in (False, True):
                with self.subTest(key=key, parent_invalid=parent_invalid):
                    html = self.entry("active" if parent_invalid else "complete")
                    payload = html.split(">", 3)[3].split("</script>")[0]
                    metadata = json.loads(payload)
                    del metadata[key]
                    invalid = html.replace(payload, json.dumps(metadata).replace("<", "\\u003c"))
                    with self.assertRaisesRegex(ValueError, f"requires {key}"):
                        effort_evidence._workspace_metadata(invalid)
                    result = effort_evidence.select_effort_evidence(
                        invalid if parent_invalid else self.entry("active"),
                        self.entry() if parent_invalid else invalid,
                        path=self.path, changed_paths=("src/feature.go",),
                        parent_read_link=lambda path: "## Actual Delivered\n\nNone yet.",
                        final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                    )
                    self.assertIsNone(result.candidate)
                    self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                    self.assertIn(f"requires {key}", result.diagnostics[0].message)

    def test_outside_workspace_selector_cannot_supply_release_evidence(self):
        for path in (
            "docs/efforts/example/plan.html",
            "docs/efforts/example/materials/2026-effort.html",
            "docs/efforts/2026-effort.html",
            "docs/efforts/example/nested/2026-effort.html",
            "other/example/2026-effort.html",
        ):
            with self.subTest(path=path):
                reads = []
                result = effort_evidence.select_effort_evidence(
                    None, self.entry(), path=path, changed_paths=("src/feature.go",),
                    final_read_link=lambda link: reads.append(link),
                )
                self.assertIsNone(result.candidate)
                self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                self.assertIn("entry selector", result.diagnostics[0].message)
                self.assertEqual(reads, [])

    def test_governing_specs_require_canonical_markdown_path_array(self):
        for specs in (
            None, "docs/specs/example.md", {}, 42, [None], [42], [{}],
            [""], ["../spec.md"], ["/docs/spec.md"], ["docs//spec.md"],
            ["docs/./spec.md"], ["docs/../spec.md"], ["docs/spec.md#scope"],
            ["https://example/spec.md"], ["docs/spec.md?raw"],
            ["docs/%20spec.md"], [" docs/spec.md"], ["docs/spec.md "],
            ["docs/spec.html"], ["docs/spec\\name.md"], ["docs/spec\n.md"],
        ):
            for parent_invalid in (False, True):
                with self.subTest(specs=specs, parent_invalid=parent_invalid):
                    invalid = self.entry(**{"governing-specs": specs})
                    result = effort_evidence.select_effort_evidence(
                        invalid if parent_invalid else self.entry("active"),
                        self.entry() if parent_invalid else invalid,
                        path=self.path, changed_paths=("src/feature.go",),
                        parent_read_link=lambda path: "## Actual Delivered\n\nNone yet.",
                        final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                    )
                    self.assertIsNone(result.candidate)
                    self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                    self.assertIn("governing-specs", result.diagnostics[0].message)

    def test_canonical_workspace_selectors_and_explicit_spec_selections(self):
        for prefix in ("", "pkg/app/cli/init/templates/starters/agentic-engineering/repo/"):
            for specs in ([], ["docs/specs/technical/example.md", "custom/spec.md"]):
                with self.subTest(prefix=prefix, specs=specs):
                    result = effort_evidence.select_effort_evidence(
                        None, self.entry(**{"governing-specs": specs}),
                        path=prefix + self.path, changed_paths=("src/feature.go",),
                        final_read_link=lambda path: "---\ntype: TechnicalSpec\n---\n" if path in specs else "## Actual Delivered\n\nDelivered.",
                    )
                    self.assertIsNotNone(result.candidate)
                    self.assertEqual(result.diagnostics, ())

    def test_governing_spec_markdown_extensions_preserve_authored_snapshot_paths(self):
        for extension in (".MD", ".Md", ".mD"):
            target = "docs/specs/Example" + extension
            for parent in (False, True):
                with self.subTest(extension=extension, parent=parent):
                    reads = []

                    def read_link(path, actual="Delivered."):
                        reads.append(path)
                        if path == target:
                            return "---\ntype: TechnicalSpec\n---\n"
                        if path == self.log_path:
                            return "## Actual Delivered\n\n" + actual
                        if path == "docs/efforts/example/plan.html":
                            return "<html></html>"
                        return None

                    result = effort_evidence.select_effort_evidence(
                        self.entry("active", **{"governing-specs": [target]}) if parent else None,
                        self.entry(**{"governing-specs": [] if parent else [target]}),
                        path=self.path, changed_paths=("src/feature.go",),
                        parent_read_link=lambda path: read_link(path, "None yet."),
                        final_read_link=read_link,
                    )
                    self.assertIsNotNone(result.candidate, result.diagnostics)
                    self.assertEqual(result.diagnostics, ())
                    self.assertIn(target, reads)
                    self.assertNotIn(target.lower(), reads)

    def test_workspace_aliases_are_not_a_release_requirement(self):
        html = self.entry()
        payload = html.split(">", 3)[3].split("</script>")[0]
        metadata = json.loads(payload)
        del metadata["aliases"]
        snapshot = effort_evidence.parse_effort_snapshot(
            html.replace(payload, json.dumps(metadata).replace("<", "\\u003c")), self.path,
            read_link=lambda path: "## Actual Delivered\n\nDelivered.",
        )
        self.assertEqual(snapshot.effort_id, metadata["id"])

    def test_workspace_identifier_uses_shipped_datetime_contract(self):
        for identifier in ("EFF-2026-09-13-10-00", "EFF-2026-09-13-10-00-2", "EFF-2026-09-13-10-00-12"):
            with self.subTest(identifier=identifier):
                effort_evidence._workspace_metadata(self.entry(id=identifier))
        for identifier in ("garbage", "EFF-0042", "EFF-2026-02-30-10-00", "EFF-2026-09-13-25-00", "EFF-2026-09-13-10-00-1", "EFF-2026-09-13-10-00-02", "EFF-2026-09-13-10-00-x"):
            with self.subTest(identifier=identifier):
                with self.assertRaisesRegex(ValueError, "DATETIME"):
                    effort_evidence._workspace_metadata(self.entry(id=identifier))

    def test_governing_spec_uses_canonical_yaml_projection(self):
        target = "docs/specs/example.md"
        for content, valid in (
            ('---\n"type": TechnicalSpec\n---\n', True),
            ('---\ntype: >-\n  TechnicalSpec\n---\n', True),
            ('---\ntype: [TechnicalSpec]\n---\n', False),
            ('---\ntype: ProductSpec\ntype: TechnicalSpec\n---\n', False),
            ('---\ntype: TechnicalSpec\nname: [broken\n---\n', False),
        ):
            for parent_invalid in (False, True):
                with self.subTest(content=content, parent=parent_invalid):
                    result = effort_evidence.select_effort_evidence(
                        self.entry("active", **{"governing-specs": [target]}) if parent_invalid else None,
                        self.entry(**{"governing-specs": [] if parent_invalid else [target]}),
                        path=self.path, changed_paths=("src/feature.go",),
                        parent_read_link=lambda path: content if path == target else "## Actual Delivered\n\nNone yet.",
                        final_read_link=lambda path: content if path == target else "## Actual Delivered\n\nDelivered.",
                    )
                    self.assertEqual(result.candidate is not None, valid, result.diagnostics)

    def test_governing_specs_resolve_in_each_snapshot(self):
        for target, content in (
            ("docs/specs/missing.md", None),
            ("docs/readme.md", "# Unrelated Markdown"),
            ("docs/readme.md", "---\ntype: EffortNote\n---\n"),
            (self.log_path, "---\ntype: TechnicalSpec\n---\n"),
        ):
            for parent_invalid in (False, True):
                with self.subTest(target=target, content=content, parent_invalid=parent_invalid):
                    def read_invalid(path):
                        return content if path == target else "## Actual Delivered\n\nDelivered."

                    def read_valid(path):
                        return "---\ntype: TechnicalSpec\n---\n" if path == target else "## Actual Delivered\n\nDelivered."

                    result = effort_evidence.select_effort_evidence(
                        self.entry("active", **{"governing-specs": [target]}) if parent_invalid else None,
                        self.entry(**{"governing-specs": [] if parent_invalid else [target]}),
                        path=self.path, changed_paths=("src/feature.go",),
                        parent_read_link=read_invalid,
                        final_read_link=read_valid if parent_invalid else read_invalid,
                    )
                    self.assertIsNone(result.candidate)
                    self.assertEqual(result.diagnostics[0].code, "invalid_effort")

    def test_status_requires_exact_ontology_enum(self):
        for status in ("COMPLETE", "Complete", "ACTIVE", "finished", " complete ", ""):
            with self.subTest(status=status):
                with self.assertRaisesRegex(ValueError, "status"):
                    effort_evidence._workspace_metadata(self.entry(status))
                result = effort_evidence.select_effort_evidence(
                    None, self.entry(status), path=self.path,
                    changed_paths=("src/feature.go",),
                    final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                )
                self.assertIsNone(result.candidate)
                self.assertEqual(result.diagnostics[0].code, "invalid_effort")

    def test_created_at_rejects_invalid_timestamps(self):
        for created_at in (
            "", "yesterday", "2026-09-13", "2026-09-13T10:00:00",
            "2026-09-13 10:00:00Z", "2026-09-13T10:00Z",
            "2026-02-29T10:00:00Z", "2026-09-31T10:00:00Z",
            "2026-09-13T24:00:00Z", "2026-09-13T10:60:00Z",
            "2026-09-13T10:00:60Z", "2026-09-13T10:00:00+24:00",
            "2026-09-13T10:00:00+00:60", "2026-09-13T10:00:00+0400",
            "2026-09-13T10:00:00Z ",
        ):
            for parent_invalid in (False, True):
                with self.subTest(created_at=created_at, parent_invalid=parent_invalid):
                    invalid = self.entry(
                        "active" if parent_invalid else "complete",
                        **{"created-at": created_at},
                    )
                    result = effort_evidence.select_effort_evidence(
                        invalid if parent_invalid else self.entry("active"),
                        self.entry() if parent_invalid else invalid,
                        path=self.path, changed_paths=("src/feature.go",),
                        parent_read_link=lambda path: "## Actual Delivered\n\nNone yet.",
                        final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                    )
                    self.assertIsNone(result.candidate)
                    self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                    self.assertIn("created-at", result.diagnostics[0].message)

    def test_created_at_accepts_utc_offsets_and_fractional_seconds(self):
        for created_at in (
            "2026-09-13T10:00:00Z", "2024-02-29T10:00:00Z",
            "2026-09-13T10:00:00.123456789Z", "2026-09-13T10:00:00+00:00",
            "2026-09-13T10:00:00-04:00", "2026-09-13T10:00:00.123456789+05:30",
        ):
            with self.subTest(created_at=created_at):
                entry = self.entry(**{"created-at": created_at})
                self.assertEqual(effort_evidence._workspace_metadata(entry)["created-at"], created_at)
                result = effort_evidence.select_effort_evidence(
                    None, entry, path=self.path, changed_paths=("src/feature.go",),
                    final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                )
                self.assertIsNotNone(result.candidate)
                self.assertEqual(result.diagnostics, ())

    def test_historical_markdown_does_not_require_workspace_timestamp(self):
        result = effort_evidence.select_effort_evidence(
            None, effort("complete", "Delivered."),
            path="docs/efforts/legacy.md", changed_paths=("src/feature.go",),
        )
        self.assertIsNotNone(result.candidate)

    def test_valid_required_metadata_preserves_all_statuses(self):
        for status in ("planned", "active", "complete", "archived"):
            with self.subTest(status=status):
                metadata = effort_evidence._workspace_metadata(self.entry(status))
                self.assertEqual(metadata["status"], status)
                self.assertEqual(metadata["summary"], "Deliver the feature.")
                self.assertEqual(metadata["created-at"], "2026-09-13T10:00:00Z")

    def test_metadata_escaping_and_linked_sections(self):
        snapshot = effort_evidence.parse_effort_snapshot(
            self.entry(), self.path,
            read_link=lambda path: material_effort("complete", "Delivered.", deviations="- Caveat.")
                if path == self.log_path else "Plan",
        )
        self.assertEqual(snapshot.name, 'Quotes " & literal </script>')
        self.assertEqual(snapshot.actual_delivered, "Delivered.")
        self.assertEqual(snapshot.deviations, ("Caveat.",))

    def test_metadata_accepts_leading_bom(self):
        snapshot = effort_evidence.parse_effort_snapshot(
            "\ufeff<!DOCTYPE html><!-- comment -->\n" + self.entry(), self.path,
            read_link=lambda path: "## Actual Delivered\n\nDelivered.",
        )
        self.assertEqual(snapshot.effort_id, "EFF-2026-09-13-10-00")
        self.assertEqual(snapshot.actual_delivered, "Delivered.")

    def test_bom_does_not_hide_invalid_head_content(self):
        for html in (
            "\ufeff" + self.entry().replace("<head>", "<head><div>content</div>"),
            "\ufeff" + self.entry().replace("<head>", "<head>text"),
            "\ufeff\ufeff" + self.entry(),
            " \ufeff" + self.entry(),
            "<!-- comment -->\ufeff" + self.entry(),
            self.entry().replace("<head>", "<head>\ufeff"),
        ):
            with self.subTest(html=html):
                with self.assertRaisesRegex(ValueError, "direct child of head"):
                    effort_evidence.parse_effort_snapshot(html, self.path)

    def test_duplicate_metadata_keys_are_invalid_evidence(self):
        for extra, key in (
            ('"type": "EffortWorkspace"', "type"),
            ('"id": "EFF-OTHER"', "id"),
            ('"status": "complete"', "status"),
            ('"name": "Another name"', "name"),
            ('"implementation-plan": "docs/efforts/example/plan.html"', "implementation-plan"),
            ('"work-log": "docs/efforts/example/work-log.md"', "work-log"),
            ('"materials": [], "materials": []', "materials"),
            ('"sta\\u0074us": "complete"', "status"),
            ('"extra": {"nested": 1, "nested": 2}', "nested"),
        ):
            for parent_invalid in (False, True):
                with self.subTest(key=key, parent_invalid=parent_invalid):
                    invalid = self.entry("active" if parent_invalid else "complete").replace(
                        "}</script>", ", " + extra + "}</script>",
                    )
                    result = effort_evidence.select_effort_evidence(
                        invalid if parent_invalid else self.entry("active"),
                        self.entry() if parent_invalid else invalid,
                        path=self.path, changed_paths=("src/feature.go",),
                        parent_read_link=lambda path: "## Actual Delivered\n\nNone yet.",
                        final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                    )
                    self.assertIsNone(result.candidate)
                    self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                    self.assertIn(f"duplicate key {key!r}", result.diagnostics[0].message)

    def test_non_json_constants_are_invalid_evidence(self):
        for constant in ("NaN", "Infinity", "-Infinity"):
            for value in (constant, '{"nested": [' + constant + ']}'):
                for parent_invalid in (False, True):
                    with self.subTest(value=value, parent_invalid=parent_invalid):
                        invalid = self.entry("active" if parent_invalid else "complete").replace(
                            "}</script>", ', "extra": ' + value + "}</script>",
                        )
                        result = effort_evidence.select_effort_evidence(
                            invalid if parent_invalid else self.entry("active"),
                            self.entry() if parent_invalid else invalid,
                            path=self.path, changed_paths=("src/feature.go",),
                            parent_read_link=lambda path: "## Actual Delivered\n\nNone yet.",
                            final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                        )
                        self.assertIsNone(result.candidate)
                        self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                        self.assertIn(f"invalid JSON constant {constant!r}", result.diagnostics[0].message)

    def test_valid_json_values_remain_selectable(self):
        extra = {"values": [None, True, False, 0, -1, 1.5, "NaN", "Infinity", "-Infinity"]}
        result = effort_evidence.select_effort_evidence(
            self.entry("active", extra=extra), self.entry(extra=extra),
            path=self.path, changed_paths=("src/feature.go",),
            parent_read_link=lambda path: "## Actual Delivered\n\nNone yet.",
            final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
        )
        self.assertEqual(result.candidate.inclusion_reason, "completed_in_range")
        self.assertEqual(result.diagnostics, ())

    def test_duplicate_marker_attributes_are_invalid_evidence(self):
        for marker, canonical, other in (
            ("id", "rhizome-metadata", "other"),
            ("type", "application/json", "text/javascript"),
        ):
            for values in ((canonical, other), (other, canonical), (canonical, canonical)):
                for parent_invalid in (False, True):
                    with self.subTest(marker=marker, values=values, parent_invalid=parent_invalid):
                        invalid = self.entry("active" if parent_invalid else "complete").replace(
                            f"{marker}='{canonical}'",
                            f"{marker}='{values[0]}' {marker.upper()}='{values[1]}'",
                        )
                        expected = (f"duplicate {marker!r} attribute" if values[0] == canonical
                                    else "exactly one complete rhizome-metadata script")
                        with self.assertRaisesRegex(ValueError, expected):
                            effort_evidence.parse_effort_snapshot(invalid, self.path)
                        result = effort_evidence.select_effort_evidence(
                            invalid if parent_invalid else self.entry("active"),
                            self.entry() if parent_invalid else invalid,
                            path=self.path, changed_paths=("src/feature.go",),
                            parent_read_link=lambda path: "## Actual Delivered\n\nNone yet.",
                            final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                        )
                        self.assertIsNone(result.candidate)
                        self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                        self.assertIn(expected, result.diagnostics[0].message)

    def test_metadata_mime_accepts_case_and_surrounding_whitespace(self):
        for mime in ("Application/JSON", " application/json ", "\tApplication/JSON\n"):
            with self.subTest(mime=mime):
                snapshot = effort_evidence.parse_effort_snapshot(
                    self.entry().replace("application/json", mime), self.path,
                    read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                )
                self.assertEqual(snapshot.actual_delivered, "Delivered.")

    def test_metadata_rejects_unrelated_or_missing_mime(self):
        for attribute in ("type='text/javascript'", "type='text/json'",
                          "type='application/json; charset=utf-8'", "type=''", "type", ""):
            with self.subTest(attribute=attribute):
                with self.assertRaisesRegex(ValueError, "exactly one complete rhizome-metadata script"):
                    effort_evidence.parse_effort_snapshot(
                        self.entry().replace("type='application/json'", attribute), self.path,
                    )

    def test_noncanonical_scripts_do_not_displace_canonical_metadata(self):
        for attributes in ("type='text/javascript'", "type='text/json'", "",
                           "type='text/javascript' type='application/json'",
                           "id='other' id='rhizome-metadata' type='application/json'"):
            for location in ("head", "body"):
                with self.subTest(attributes=attributes, location=location):
                    ignored = "<script " + attributes + " id='rhizome-metadata'>not JSON</script>"
                    entry = self.entry().replace("<" + location + ">", "<" + location + ">" + ignored)
                    result = effort_evidence.select_effort_evidence(
                        self.entry("active"), entry, path=self.path,
                        changed_paths=("src/feature.go",),
                        parent_read_link=lambda path: "## Actual Delivered\n\nNone yet.",
                        final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                    )
                    self.assertEqual(result.candidate.inclusion_reason, "completed_in_range")
                    self.assertEqual(result.diagnostics, ())

    def test_metadata_must_be_directly_in_head(self):
        canonical = self.entry()
        script = canonical.split("<head>")[1].split("</head>")[0]
        for html in (
            "<html><head></head><body>" + script + "</body></html>",
            "<html><head><template>" + script + "</template></head></html>",
            "<html><head><div>" + script + "</div></head></html>",
            "<html><body><head>" + script + "</head></body></html>",
            "<html><div><head>" + script + "</head></div></html>",
        ):
            with self.subTest(html=html):
                with self.assertRaisesRegex(ValueError, "direct child of head"):
                    effort_evidence.parse_effort_snapshot(html, self.path)

    def test_metadata_accepts_omitted_head(self):
        script = self.entry().split("<head>")[1].split("</head>")[0]
        for prefix in ("", "<html>", "\ufeff<!DOCTYPE html><html>\n<!-- comment -->",
                       "<html><title>Workspace</title><meta charset='utf-8'>"):
            with self.subTest(prefix=prefix):
                snapshot = effort_evidence.parse_effort_snapshot(
                    prefix + script + "<body></body></html>", self.path,
                    read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                )
                self.assertEqual(snapshot.effort_id, "EFF-2026-09-13-10-00")
                self.assertEqual(snapshot.actual_delivered, "Delivered.")

    def test_omitted_head_rejects_metadata_after_body_content(self):
        script = self.entry().split("<head>")[1].split("</head>")[0]
        for prefix in ("<body>", "<body></body>", "<body></body></html>",
                       "<div>", "<div>content</div>", "text", "<title>Title</title>text",
                       "<head></head>"):
            with self.subTest(prefix=prefix):
                with self.assertRaisesRegex(ValueError, "direct child of head"):
                    effort_evidence.parse_effort_snapshot("<html>" + prefix + script, self.path)

    def test_metadata_requires_one_complete_script(self):
        canonical = self.entry()
        script = canonical.split("<head>")[1].split("</head>")[0]
        for html in (
            "<html><head></head></html>",
            canonical.replace(script, script + script),
            canonical.split("</script>")[0],
            canonical.replace(script, "<script id='rhizome-metadata' type='application/json'/>"),
        ):
            with self.subTest(html=html):
                with self.assertRaises(ValueError):
                    effort_evidence.parse_effort_snapshot(html, self.path)

    def test_metadata_rejects_content_that_ends_html5_head(self):
        for content in (
            "<div>content</div>", "<div><title>title</title></div>",
            "<body></body>", "<p>content</p>", "<br>", "text", "&nbsp;",
            "</head><head>", "</body>", "</html>",
        ):
            with self.subTest(content=content):
                with self.assertRaisesRegex(ValueError, "direct child of head"):
                    effort_evidence.parse_effort_snapshot(
                        self.entry().replace("<head>", "<head>" + content), self.path,
                    )

    def test_metadata_accepts_head_text_elements_whitespace_and_comments(self):
        for content in (
            " \t\n\r\f<!-- comment -->", "<title>Workspace &amp; plan</title>",
            "<style>body { color: red; }</style>", "<script>const x = 1;</script>",
        ):
            with self.subTest(content=content):
                snapshot = effort_evidence.parse_effort_snapshot(
                    self.entry().replace("<head>", "<head>" + content), self.path,
                    read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                )
                self.assertEqual(snapshot.actual_delivered, "Delivered.")

    def test_closed_template_and_noscript_preserve_head(self):
        for content in (
            "<template></template>",
            "<template><div><span>Content</span><br></div></template>",
            "<template><template><p>Nested</p></template></template>",
            "<noscript><link rel='stylesheet' href='fallback.css'></noscript>",
            "<noscript><div>Raw text with scripting enabled</div></noscript>",
        ):
            for explicit_head in (True, False):
                with self.subTest(content=content, explicit_head=explicit_head):
                    entry = self.entry().replace("<head>", "<head>" + content)
                    if not explicit_head:
                        entry = entry.replace("<head>", "", 1)
                    snapshot = effort_evidence.parse_effort_snapshot(
                        entry, self.path,
                        read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                    )
                    self.assertEqual(snapshot.actual_delivered, "Delivered.")

    def test_template_and_noscript_do_not_expose_nested_metadata(self):
        script = self.entry().split("<head>")[1].split("</head>")[0]
        for content in (
            "<template>" + script + "</template>",
            "<noscript>" + script + "</noscript>",
            "<template><div></template>" + script,
            "<template></template><div></div>" + script,
            "<noscript></noscript><body>" + script,
            "<template/>" + script,
            "<noscript/>" + script,
        ):
            with self.subTest(content=content):
                with self.assertRaises(ValueError):
                    effort_evidence.parse_effort_snapshot(
                        "<html><head>" + content + "</head></html>", self.path,
                    )

    def test_head_void_elements_do_not_nest_metadata(self):
        html = self.entry().replace("<head>", "<head><meta charset='utf-8'><link rel='stylesheet' href='a.css'/>")
        snapshot = effort_evidence.parse_effort_snapshot(
            html, self.path, read_link=lambda path: "## Actual Delivered\n\nDelivered.",
        )
        self.assertEqual(snapshot.actual_delivered, "Delivered.")

    def test_invalid_and_missing_links_are_diagnostics(self):
        for link in ("../work-log.md", "/work-log.md", "docs/../work-log.md",
                     "docs//work-log.md", "https://example/log.md", "docs/log.md#delivery",
                     "", self.log_path):
            with self.subTest(link=link):
                result = effort_evidence.select_effort_evidence(
                    None, self.entry(**{"work-log": link}), path=self.path,
                    changed_paths=("src/feature.go",), final_read_link=lambda path: "Plan" if path.endswith("plan.html") else None,
                )
                self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                self.assertIn("work-log", result.diagnostics[0].message)

    def test_required_plan_must_resolve_as_effort_material(self):
        for plan in ("", "docs/specs/plan.md", "docs/efforts/example/other.html",
                     "docs/efforts/example/2026-effort.html", "../plan.html",
                     "docs/efforts/example/plan.html"):
            with self.subTest(plan=plan):
                result = effort_evidence.select_effort_evidence(
                    None, self.entry(**{"implementation-plan": plan}), path=self.path,
                    changed_paths=("src/feature.go",),
                    final_read_link=lambda path: "## Actual Delivered\n\nDelivered."
                        if path == self.log_path else None,
                )
                self.assertIsNone(result.candidate)
                self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                self.assertIn("implementation-plan", result.diagnostics[0].message)
        missing = self.entry().replace(
            '"implementation-plan": "docs/efforts/example/plan.html", ', "",
        )
        with self.assertRaisesRegex(ValueError, "implementation-plan"):
            effort_evidence.parse_effort_snapshot(missing, self.path, read_link=lambda path: "Plan")

    def test_existing_arbitrary_markdown_cannot_supply_work_log(self):
        for log in ("docs/release.md", "docs/efforts/example/log.md",
                    "docs/efforts/example/nested/work-log.md", "docs/efforts/example/plan.html"):
            with self.subTest(log=log):
                result = effort_evidence.select_effort_evidence(
                    None, self.entry(**{"work-log": log}), path=self.path,
                    changed_paths=(log,),
                    final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                )
                self.assertIsNone(result.candidate)
                self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                self.assertIn("work-log", result.diagnostics[0].message)

    def test_linked_components_from_either_snapshot_are_not_delivery(self):
        external = "pkg/app/cli/init/templates/starters/agentic-engineering/repo/docs/efforts/example/materials/report.md"
        template = "pkg/app/cli/init/templates/starters/agentic-engineering/repo/docs/efforts/example/plan.html"
        for old_materials, new_materials, changed in (
            ([], [external], external), ([external], [], external),
            ([], [], template),
        ):
            with self.subTest(changed=changed, old_materials=old_materials):
                result = effort_evidence.select_effort_evidence(
                    self.entry("active", materials=old_materials, **{"implementation-plan": template}),
                    self.entry(materials=new_materials, **{"implementation-plan": template}),
                    path=self.path, changed_paths=(self.path, changed),
                    parent_read_link=lambda path: "## Actual Delivered\n\nNone yet.",
                    final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                )
                self.assertIsNone(result.candidate)
                self.assertEqual(result.diagnostics[0].code, "missing_delivery_proof")

    def test_selected_materials_must_resolve_in_each_snapshot(self):
        material = "docs/efforts/example/materials/report.md"
        for missing_snapshot in ("parent", "final", None):
            with self.subTest(missing_snapshot=missing_snapshot):
                reads = {"parent": [], "final": []}

                def reader(snapshot):
                    def read(link):
                        reads[snapshot].append(link)
                        if link == material:
                            return None if snapshot == missing_snapshot else ""
                        delivered = "None yet." if snapshot == "parent" else "Delivered."
                        return "## Actual Delivered\n\n" + delivered
                    return read

                result = effort_evidence.select_effort_evidence(
                    self.entry("active", materials=[material]), self.entry(materials=[material]),
                    path=self.path, changed_paths=("src/feature.go",),
                    parent_read_link=reader("parent"), final_read_link=reader("final"),
                )
                if missing_snapshot is None:
                    self.assertIsNotNone(result.candidate)
                    self.assertEqual(result.diagnostics, ())
                    self.assertIn(material, reads["parent"])
                    self.assertIn(material, reads["final"])
                else:
                    self.assertIsNone(result.candidate)
                    self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                    self.assertIn("linked materials is missing at this snapshot", result.diagnostics[0].message)
                    self.assertIn(material, result.diagnostics[0].message)

    def test_invalid_materials_reject_workspace_before_delivery_filtering(self):
        for materials in (["src/feature.go"], ["docs/release-report.md"],
                          ["docs/efforts/example/materials/../plan.html"],
                          [42], {"path": "src/feature.go"}):
            for parent_invalid in (False, True):
                with self.subTest(materials=materials, parent_invalid=parent_invalid):
                    invalid = self.entry("active" if parent_invalid else "complete", materials=materials)
                    result = effort_evidence.select_effort_evidence(
                        invalid if parent_invalid else self.entry("active"),
                        self.entry() if parent_invalid else invalid,
                        path=self.path, changed_paths=("src/feature.go", "docs/release-report.md"),
                        parent_read_link=lambda path: "## Actual Delivered\n\nNone yet.",
                        final_read_link=lambda path: "## Actual Delivered\n\nDelivered.",
                    )
                    self.assertIsNone(result.candidate)
                    self.assertEqual(result.diagnostics[0].code, "invalid_effort")
                    self.assertIn("materials", result.diagnostics[0].message)

    def test_workspace_preserves_lifecycle_rules(self):
        for parent_status, before, after, changed, expected in (
            ("active", "None yet.", "Delivered.", ("src/x",), None),
            ("complete", "Old delivery.", "Delivered.", ("src/x",), "post_closure_mutation"),
            ("complete", "Delivered.", "Delivered.", ("src/x",), "administrative_only"),
            ("active", "Delivered.", "Delivered.", ("src/x",), "unchanged_actual_delivered"),
            ("active", "None yet.", "TBD", ("src/x",), "missing_actual_delivered"),
            ("active", "None yet.", "Delivered.", (self.log_path,), "missing_delivery_proof"),
        ):
            with self.subTest(expected=expected):
                result = effort_evidence.select_effort_evidence(
                    self.entry(parent_status), self.entry(), path=self.path, changed_paths=changed,
                    parent_read_link=lambda path: material_effort("active", before),
                    final_read_link=lambda path: material_effort("complete", after),
                )
                self.assertEqual(result.diagnostics[0].code if result.diagnostics else None, expected)


if __name__ == "__main__":
    unittest.main()
