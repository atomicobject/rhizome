"""Live AO oracle tests. Run only with the pinned read-only toolkit on PYTHONPATH."""
import copy
import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
BINDINGS = HERE.parents[1] / "pkg/app/cli/init/templates/starters/ao-kb/module/bindings"
sys.path.insert(0, str(BINDINGS))
from adapter import export, load_engine  # noqa: E402


class AdapterTests(unittest.TestCase):
    def setUp(self):
        self.scratch = tempfile.TemporaryDirectory(prefix="aokb-laneM2-adapter-", dir="/private/tmp")
        self.addCleanup(self.scratch.cleanup)
        self.root = Path(self.scratch.name)
        shutil.copytree(HERE / "account", self.root, dirs_exist_ok=True)
        self.engine, _, _ = load_engine()

    def product_doc(self):
        path = self.root / "products/alpha/artifacts/_assets/_model/lane.yaml"
        return path, self.engine._yaml(str(path))

    def write(self, path, doc):
        import yaml
        path.write_text(yaml.safe_dump(doc, sort_keys=False))

    def test_live_export_matches_offline_intermediate(self):
        got = export(self.root, "alpha")
        want = json.loads((HERE / "intermediate.json").read_text())
        # JSON serialisation normalises AO's tuples without changing occurrence order.
        self.assertEqual(want, json.loads(json.dumps(got, default=str)))
        self.assertEqual(got, export(self.root, "alpha"))
        service = next(k for k in got["kinds"] if k["name"] == "service")
        self.assertIn("project_detail", service["fields"])
        self.assertIn("status", service["fields"])
        self.assertEqual(got["oracle"]["rows"]["position"]["1"]["name"], "Account first")
        self.assertEqual(got["oracle"]["rows"]["position"]["2"]["name"], "Product second")
        self.assertTrue(any(d["code"] == "ao_scalar_override" for d in got["diagnostics"]))
        self.assertFalse(any(d["severity"] == "error" for d in got["diagnostics"]))
        self.assertEqual(got["oracle"]["rows"]["service"]["S1"]["uses"], ["SHARED", "SHARED", "MISSING"])
        self.assertTrue(got["oracle"]["unresolvedEdges"])
        self.assertEqual([s["path"] for s in got["knowledgeSources"]], ["assertions/shared.md"])

    def test_multi_product_requires_explicit_selection(self):
        with self.assertRaisesRegex(ValueError, "ao_product_required.*persists selection"):
            export(self.root, None)
        with self.assertRaisesRegex(ValueError, "ao_product_invalid"):
            export(self.root, "unknown")
        orphan = self.root / "products/orphan/.kb/config"
        orphan.mkdir(parents=True)
        (orphan / "product.yaml").write_text("slug: orphan\naccount: ../..\n")
        with self.assertRaisesRegex(ValueError, "ao_product_unregistered"):
            export(self.root, "alpha")
        (orphan / "product.yaml").unlink()
        marker = self.root / "products/alpha/.kb/config/product.yaml"
        marker.write_text("slug: alpha\naccount: ../../somewhere\n")
        with self.assertRaisesRegex(ValueError, "slug/account marker"):
            export(self.root, "alpha")

    def test_mapping_overlap_names_kind_key_and_both_physical_paths(self):
        path, doc = self.product_doc()
        doc["mapped"]["account"] = {"name": "Overlaid product name"}
        self.write(path, doc)
        got = export(self.root, "alpha")
        error = next(d for d in got["diagnostics"] if d["code"] == "ao_mixed_origin_record")
        self.assertEqual(error["kind"], "map")
        self.assertEqual(error["key"], "account")
        self.assertEqual(error["paths"], ["artifacts/_assets/_model/lane.yaml", "products/alpha/artifacts/_assets/_model/lane.yaml"])
        self.assertEqual(error["address"], "/mapped/account")

    def test_filter_preserves_original_address_and_excludes_beta_collision(self):
        path = self.root / "artifacts/_assets/_model/lane.yaml"
        doc = self.engine._yaml(str(path))
        doc["users"].insert(0, {"id": "S1", "products": ["beta"]})
        self.write(path, doc)
        got = export(self.root, "alpha")
        user = next(r for r in got["records"] if r["kind"] == "user")
        self.assertEqual(user["address"], "/users/1")
        self.assertNotIn("S1", got["oracle"]["rows"]["user"])
        self.assertTrue(all("beta/" not in s["path"] for s in got["sources"]))

    def test_duplicate_ids_report_every_occurrence(self):
        path, doc = self.product_doc()
        doc["services"].append({"id": "SHARED", "name": "Duplicate shared ID"})
        self.write(path, doc)
        got = export(self.root, "alpha")
        error = next(d for d in got["diagnostics"] if d["code"] == "ao_duplicate_record")
        self.assertEqual(error["key"], "SHARED")
        self.assertEqual(len(error["occurrences"]), 2)
        self.assertNotEqual(error["occurrences"][0]["path"], error["occurrences"][1]["path"])
        self.assertIn("SHARED", error["message"])
        for occurrence in error["occurrences"]:
            self.assertIn(f'{occurrence["path"]}#{occurrence["address"]}', error["message"])

    def account_metamodel_override(self, field):
        import yaml
        path = self.root / ".kb/config/metamodel.yaml"
        doc = yaml.safe_load(path.read_text())
        doc["override"]["service"]["fields"][field] = {"type": "text"}
        path.write_text(yaml.safe_dump(doc, sort_keys=False))

    def service_fields(self):
        return next(k for k in export(self.root, "alpha")["kinds"] if k["name"] == "service")["fields"]

    def test_account_metamodel_wins_unless_the_product_owns_its_copy(self):
        # AO's site-scoped commands mirror the account's metamodel.yaml over the product copy first.
        self.account_metamodel_override("account_detail")
        self.assertIn("account_detail", self.service_fields())
        got = export(self.root, "alpha")
        self.assertTrue(any(d["code"] == "ao_config_mirror_differs" and d["path"] == "products/alpha/.kb/config/metamodel.yaml"
                            for d in got["diagnostics"]))
        # A product that owns metamodel.yaml keeps its own copy, as AO's mirror_config does.
        marker = self.root / "products/alpha/.kb/config/product.yaml"
        marker.write_text(marker.read_text() + "owns: metamodel.yaml\n")
        self.assertNotIn("account_detail", self.service_fields())
        self.assertIn("project_detail", self.service_fields())
        got = export(self.root, "alpha")
        self.assertFalse(any(d["code"] == "ao_config_mirror_differs" and d["path"].endswith("metamodel.yaml") for d in got["diagnostics"]))

    def test_overlaid_fields_are_notes_unless_required(self):
        # Alpha does not own its metamodel copy, so AO (and the adapter) resolve the account file.
        path = self.root / ".kb/config/metamodel.yaml"
        doc = self.engine._yaml(str(path))
        doc["override"]["service"]["fields"]["reads_fields"] = {"type": "text[]"}
        self.write(path, doc)
        got = export(self.root, "alpha")
        self.assertTrue(any(d["code"] == "ao_generated_field_overlay" for d in got["diagnostics"]))
        self.assertFalse(any(d["severity"] == "error" for d in got["diagnostics"]))
        doc["override"]["service"]["fields"]["reads_fields"]["required"] = True
        self.write(path, doc)
        got = export(self.root, "alpha")
        self.assertTrue(any(d["code"] == "ao_required_generated_field" for d in got["diagnostics"]))

    def test_wildcard_filter_admits_shared_account_rows(self):
        path = self.root / "artifacts/_assets/_model/lane.yaml"
        doc = self.engine._yaml(str(path))
        doc["services"][0]["products"] = ["*"]
        self.write(path, doc)
        got = export(self.root, "alpha")
        self.assertIn("SHARED", got["oracle"]["rows"]["service"])
        self.assertFalse(any(d["severity"] == "error" for d in got["diagnostics"]))

    def test_boolean_key_fails_with_a_specific_scalar_diagnostic(self):
        path, doc = self.product_doc()
        doc["services"][0]["id"] = True
        self.write(path, doc)
        got = export(self.root, "alpha")
        self.assertTrue(any(d["code"] == "ao_key_scalar_unsupported" for d in got["diagnostics"]))

    def test_repeated_scalar_children_keep_the_selected_parents_physical_address(self):
        path = self.root / "artifacts/_assets/_model/lane.yaml"
        account = self.engine._yaml(str(path))
        account["services"][0]["jobs"] = [1, 1]
        account["services"].insert(0, {"id": "HIDDEN", "products": ["beta"], "jobs": [1]})
        self.write(path, account)
        path, product = self.product_doc()
        product["services"][0]["jobs"] = [1]
        self.write(path, product)
        got = export(self.root, "alpha")
        jobs = {r["key"]: r for r in got["records"] if r["kind"] == "job"}
        self.assertEqual(jobs["SHARED.1"]["address"], "/services/1/jobs/0")
        self.assertEqual(jobs["SHARED.2"]["address"], "/services/1/jobs/1")
        self.assertEqual(jobs["S1.1"]["path"], "products/alpha/artifacts/_assets/_model/lane.yaml")
        self.assertEqual(jobs["S1.1"]["address"], "/services/0/jobs/0")
        self.assertFalse(any(d["severity"] == "error" for d in got["diagnostics"]))

    def test_child_membership_comes_from_its_selected_container(self):
        path = self.root / "artifacts/_assets/_model/lane.yaml"
        doc = self.engine._yaml(str(path))
        doc["services"][0]["jobs"][0]["products"] = ["*"]
        self.write(path, doc)
        got = export(self.root, "alpha")
        self.assertFalse(any(d["severity"] == "error" for d in got["diagnostics"]))
        self.assertEqual(got["oracle"]["rows"]["job"]["SHARED.1"]["products"], ["*"])

    def test_positional_grouped_mapping_order_is_not_faked_by_source_order(self):
        # Alpha does not own its metamodel copy, so AO (and the adapter) resolve the account file.
        path = self.root / ".kb/config/metamodel.yaml"
        project = self.engine._yaml(str(path))
        project["add"]["kinds"]["group"]["key"] = None
        self.write(path, project)
        path = self.root / "artifacts/_assets/_model/lane.yaml"
        account = self.engine._yaml(str(path))
        account["groups"] = {"account-only": [{"id": "GA", "products": ["alpha"]}]}
        self.write(path, account)
        got = export(self.root, "alpha")
        self.assertTrue(any(d["code"] == "ao_source_order_unsupported" for d in got["diagnostics"]))

    def test_unknown_toolkit_pin_fails_and_new_input_bytes_change_hash(self):
        before = export(self.root, "alpha")
        path = self.root / "products/alpha/.kb/config/metamodel.yaml"
        path.write_text(path.read_text() + "\n# metamodel edited\n")
        after = export(self.root, "alpha")
        self.assertNotEqual(before["inputs"], after["inputs"])
        manifest = self.root / "products/alpha/.kb/config/manifest.json"
        manifest.write_text('{"siteEngine":{"version":"9.0.0"}}')
        with self.assertRaisesRegex(ValueError, "ao_toolkit_pin"):
            export(self.root, "alpha")

    def test_knowledge_only_single_kb_without_site_engine_reads_no_model(self):
        shutil.rmtree(self.root / "products")
        shutil.rmtree(self.root / "artifacts")
        for name in ("manifest.json", "metamodel.yaml"):
            (self.root / ".kb/config" / name).unlink()
        (self.root / ".kb/config/vault.md").write_text('---\nschemaVersion: 8\nproductMode: single\nproducts: ["solo"]\n---\n')
        got = export(self.root, None)
        self.assertEqual(got["records"], [])
        self.assertIn({"path": ".kb/config/manifest.json", "hash": None}, got["inputs"])
        self.assertTrue(any(d["code"] == "ao_site_engine_absent" for d in got["diagnostics"]))
        (self.root / ".kb/config/manifest.json").write_text('{"siteEngine":{"version":"9.0.0"}}')
        with self.assertRaisesRegex(ValueError, "ao_toolkit_pin"):
            export(self.root, None)


if __name__ == "__main__":
    unittest.main()
