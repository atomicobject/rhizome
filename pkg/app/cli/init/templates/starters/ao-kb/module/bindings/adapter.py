"""Read-only adapter for the pinned AO reader. JSON on stdout, no AO workflows."""
import argparse
import hashlib
import importlib.util
import json
import sys
from pathlib import Path

sys.dont_write_bytecode = True

from attribution import inventory

HERE = Path(__file__).resolve().parent
PIN = json.loads((HERE / "toolkit-pin.json").read_text())


def digest(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


def load_engine():
    import kb_site
    import yaml
    from kb_site import product
    if sys.version_info < (3, 11) or yaml.__version__ != "6.0.3":
        raise ValueError("ao_python_dependency: requires Python >=3.11 and PyYAML 6.0.3")
    package = Path(kb_site.__file__).resolve().parent
    for rel, expected in PIN["files"].items():
        if digest((package / rel).read_bytes()) != expected:
            raise ValueError(f"ao_toolkit_incompatible: {rel} differs from {PIN['revision']}")
    build = package / "kits/core/_build"
    # AO uses bare co-located imports; bind them explicitly so a vault/account directory cannot shadow them.
    def module(name, filename):
        spec = importlib.util.spec_from_file_location(name, build / filename)
        loaded = importlib.util.module_from_spec(spec)
        sys.modules[name] = loaded
        spec.loader.exec_module(loaded)
        return loaded
    module("_kbcore", "_kbcore.py")
    module("account", "account.py")
    engine = module("ao_binding_metamodel", "metamodel.py")
    for name in ("resolve_vault", "_fields_of", "_find_container_for", "_leaf_id", "model_doc"):
        if not callable(getattr(engine, name, None)):
            raise ValueError(f"ao_adapter_contract: missing {name}")
    return engine, product, package


def selected_context(root, slug, product):
    if not (root / ".kb/config/vault.md").is_file():
        raise ValueError("ao_root_missing: --root must name an AO account with .kb/config/vault.md")
    fm = product._frontmatter(str(root), strict=True)
    registered = product.registered(str(root))
    if any(not isinstance(s, str) or not s or '/' in s or '\\' in s or s in ('.', '..') for s in registered):
        raise ValueError("ao_product_invalid: registry slugs must be simple directory names")
    marked = {p.parents[2].name for p in root.glob("products/*/.kb/config/product.yaml")}
    if marked - set(registered):
        raise ValueError("ao_product_unregistered: product marker exists outside the vault.md registry")
    roots = product.product_roots(str(root))
    if roots and not slug:
        raise ValueError("ao_product_required: product roots found; refresh-bindings --product <slug> persists selection")
    if not roots and fm.get("productMode", "single") != "single":
        raise ValueError("ao_product_setup_incomplete: registry has no marked product roots")
    if slug:
        if slug not in roots:
            raise ValueError(f"ao_product_invalid: {slug!r} is not a registered, marked product")
        site = root / "products" / slug
        marker = product._yaml(str(site / product.PRODUCT_YAML))
        if marker.get("slug") != slug or Path(product.account_of(str(site))).resolve() != root:
            raise ValueError("ao_product_invalid: product slug/account marker disagrees with selection")
    else:
        site = root
    return site, registered


def export(root, slug):
    engine, product, package = load_engine()
    site, registered = selected_context(root, slug, product)
    engine.require_current_schema(str(site))
    mm = engine.resolve_vault(str(site))
    schema = engine.load_schema()
    manifest = site / ".kb/config/manifest.json"
    pin = json.loads(manifest.read_text()).get("siteEngine", {})
    if str(pin.get("version")) not in (PIN["version"], PIN["revision"]):
        raise ValueError("ao_toolkit_pin: manifest siteEngine.version is not the supported baseline")
    sources = (PIN["source"], "git+ssh://git@github.com/atomicobject/atomic-ai-prompt-toolkit")
    if pin.get("source", PIN["source"]).removesuffix(".git") not in sources:
        raise ValueError("ao_toolkit_pin: manifest siteEngine.source is not the supported toolkit")
    diagnostics = []
    inputs = {}

    def relative(path):
        return path.resolve().relative_to(root).as_posix()

    def capture(path):
        rel = relative(path)
        inputs[rel] = digest(path.read_bytes()) if path.is_file() else None
        return rel

    for base in {root, site}:
        for name in ("metamodel.yaml", "manifest.json", "vault.md", "product.yaml"):
            capture(base / ".kb/config" / name)
        for path in sorted((base / ".kb/config").rglob("*")):
            if path.suffix in (".md", ".yaml", ".yml", ".json") and path.is_file():
                capture(path)
    for name in registered:
        capture(root / "products" / name / product.PRODUCT_YAML)
    capture(root / ".rhizome/source-ownership.yml")
    if slug:
        for name in product.MIRRORED_CONFIG:
            acct, own = root / ".kb/config" / name, site / ".kb/config" / name
            capture(acct)
            capture(own)
            if acct.is_file() and (not own.is_file() or acct.read_bytes() != own.read_bytes()):
                diagnostics.append({"code": "ao_config_mirror_differs", "severity": "note", "path": relative(own),
                                    "message": "AO resolves the selected product file; account config was not overlaid"})
    knowledge = []
    for name in ("assertions", "sources", "concepts", "artifacts"):
        selected = engine.account.product_notes(str(site), name, recursive=name not in ("assertions", "concepts"))
        paths = [Path(p) for p, _ in selected] if selected is not None else sorted((site / name).rglob("*.md"))
        for path in paths:
            if "_raw" in path.relative_to(root).parts or path.name.startswith("_"):
                continue
            knowledge.append({"path": relative(path), "kind": name, "origin": "account" if slug else "single"})
    sources, kinds, docs = [], [], {}
    model = site / engine.MODEL_REL
    for name, declaration in sorted(mm["kinds"].items()):
        fname, selector, parts = engine._parse_file(declaration)
        fields = engine._fields_of(mm, declaration)
        kind = {"name": name, "declaration": declaration, "fields": fields,
                "idPatterns": engine._id_patterns(mm, declaration),
                "selector": selector, "container": engine._find_container_for(mm, name), "sources": []}
        kinds.append(kind)
        if fname is None:
            continue
        if Path(fname).suffix not in (".yaml", ".yml", ".json"):
            raise ValueError(f"ao_source_unsupported: {name}: {fname}")
        if fname not in docs:
            own_path = model / fname
            own_rel = relative(own_path)
            raw = None
            own = engine._yaml(str(own_path)) if own_path.is_file() else None
            acct, acct_path = None, root / engine.MODEL_REL / fname
            paths = engine.account.row_paths(mm, fname)
            if slug and Path(fname).name == fname:
                # Keep the original objects/addresses; reuse AO's exact filtering and merge.
                if acct_path.is_file():
                    raw = engine._yaml(str(acct_path))
                    if isinstance(raw, dict) and (paths or fname in engine.account._kbcore.NON_ROW_FILES):
                        acct = engine.account._kbcore.keep_account_rows(raw, slug, paths)
                        if acct != engine.account.unscoped(engine.account.account_doc(str(model), fname, paths)):
                            raise ValueError("ao_adapter_contract: filtered account document disagrees with AO")
            effective, warnings = engine.account.merge_doc(own, acct, fname)
            diagnostics.extend({"code": "ao_scalar_override", "severity": "note", "message": w} for w in warnings)
            docs[fname] = {"own": own, "account": acct, "accountRaw": raw, "effective": effective, "ownPath": own_rel,
                           "accountPath": relative(acct_path) if slug and Path(fname).name == fname and paths else None}
        doc = docs[fname]
        # Declare absent files too, so later authored rows are admitted by ordinary indexing.
        for origin, path in (("product" if slug else "single", doc["ownPath"]), ("account", doc["accountPath"])):
            if path is None:
                continue
            sid = f"{name}:{origin}"
            membership = None
            if origin == "account" and not kind["container"]:
                if parts not in engine.account.row_paths(mm, fname):
                    diagnostics.append({"code": "ao_membership_unrepresentable", "severity": "error", "kind": name,
                                        "message": "account selection is not a root row position"})
                membership = {"path": "products", "includes": slug, "includeMissing": False}
            sources.append({"id": sid, "kind": name, "path": path, "format": Path(path).suffix[1:],
                            "origin": "generated" if Path(fname).suffix == ".json" else origin,
                            "compositionOrigin": origin, "readOnly": True, "membership": membership,
                            "sourceOrder": 0 if origin == "account" else 1,
                            "revision": digest((root / path).read_bytes()) if (root / path).is_file() else None})
            kind["sources"].append(sid)
    rows, evidence = inventory(engine, mm, docs, diagnostics)
    overlaid = engine._generated_field_kind(mm)
    if overlaid:
        path = model.joinpath(*engine.GENERATED_FIELDS_REL)
        diagnostics.append({"code": "ao_generated_field_overlay", "severity": "note", "kind": overlaid,
                            "field": "reads_fields", "path": relative(path),
                            "message": "reads_fields is omitted from bindings and parity; open saved output separately"})
        for kind in kinds:
            if kind["name"] == overlaid:
                if kind["fields"]["reads_fields"].get("required"):
                    diagnostics.append({"code": "ao_required_generated_field", "severity": "error", "kind": overlaid,
                                        "message": "required reads_fields needs multiple-source provenance"})
                kind["fields"] = {k: v for k, v in kind["fields"].items() if k != "reads_fields"}
    # AO's reader oracle, not the renderer, supplies the parity expectations.
    oracle_rows, duplicates = engine._rows_and_duplicates(mm, str(model))
    if not any(d["severity"] == "error" for d in diagnostics):
        for kind, values in rows.items():
            if set(values) != set(oracle_rows.get(kind, {})):
                diagnostics.append({"code": "ao_identity_parity_unsupported", "severity": "error", "kind": kind,
                                    "message": "physical attribution keys disagree with the pinned AO reader"})
    for kind, key, count in duplicates:
        diagnostics.append({"code": "ao_duplicate_record", "severity": "error", "kind": kind, "key": key,
                            "count": count, "occurrences": [r for r in evidence if r["kind"] == kind and r["key"] == key]})
    live = {f"{k}:{rid}" for k, values in oracle_rows.items() for rid in values}
    edges = engine.edges(mm, oracle_rows, schema)
    resolved_edges = [e for e in edges if e["from"] in live and e["to"] in live]
    unresolved = [e for e in edges if e not in resolved_edges]
    oracle = {k: {rid: {f: v for f, v in row.items() if not f.startswith("__") and f != "_scope"
                        and not (k == overlaid and f == "reads_fields")} for rid, row in values.items()}
              for k, values in oracle_rows.items() if values}
    return {"formatVersion": 1, "compilerVersion": "1", "providerContract": "RECORD-1",
            "toolkit": {"version": PIN["version"], "revision": PIN["revision"], "schemaVersion": engine.SCHEMA_VERSION, "pyYamlVersion": "6.0.3"},
            "context": {"accountRoot": ".", "product": slug, "productRoot": relative(site)},
            "inputs": [{"path": p, "hash": h} for p, h in sorted(inputs.items())],
            "sources": sources, "knowledgeSources": knowledge, "knowledgeAdapterVersion": "1",
            "kinds": kinds, "effectiveMetamodel": mm,
            "referenceVocabulary": {"reverse": schema.get("reverse", {}), "typed": schema.get("typed_ref", {})},
            "records": evidence, "diagnostics": diagnostics,
            "oracle": {"rows": oracle, "edges": resolved_edges, "unresolvedEdges": unresolved}}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--product")
    args = parser.parse_args()
    try:
        result = export(Path(args.root).resolve(), args.product)
        print(json.dumps(result, sort_keys=True, ensure_ascii=False, default=str))
    except Exception as error:
        print(json.dumps({"formatVersion": 1, "diagnostics": [{"code": str(error).split(":")[0],
                         "severity": "error", "message": str(error)}]}))
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
