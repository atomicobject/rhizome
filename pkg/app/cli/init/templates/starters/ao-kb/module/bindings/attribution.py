"""Physical occurrence attribution around AO's effective documents, never a merge."""
import collections


def located(node, parts, address="", mapkey=None):
    """AO's bounded walker plus JSON-pointer addresses; values remain original objects."""
    if not parts:
        if isinstance(node, list):
            for i, value in enumerate(node):
                yield mapkey, value, f"{address}/{i}"
        elif node is not None:
            yield mapkey, node, address
        return
    part, rest = parts[0], parts[1:]
    suffix = next((s for s in ("{}[]", "{}", "[]") if part.endswith(s)), "")
    name = part[:-len(suffix)] if suffix else part
    if not isinstance(node, dict) or name not in node:
        return
    value = node[name]
    at = address + "/" + name.replace("~", "~0").replace("/", "~1")
    if suffix in ("{}", "{}[]"):
        if not isinstance(value, dict):
            return
        for key, child in value.items():
            child_at = at + "/" + str(key).replace("~", "~0").replace("/", "~1")
            carried = key if mapkey is None else mapkey
            if suffix == "{}[]":
                if isinstance(child, list):
                    for i, item in enumerate(child):
                        yield from located(item, rest, f"{child_at}/{i}", carried)
            else:
                yield from located(child, rest, child_at, carried)
    elif suffix == "[]":
        if isinstance(value, list):
            for i, child in enumerate(value):
                yield from located(child, rest, f"{at}/{i}", mapkey)
    else:
        yield from located(value, rest, at, mapkey)


def inventory(engine, mm, docs, diagnostics):
    kinds = mm["kinds"]
    containers = {k: engine._find_container_for(mm, k) for k in kinds}
    orig, output, records = {}, {}, []

    def depth(kind, visiting=()):
        if kind in visiting:
            raise ValueError(f"ao_container_cycle: {kind}")
        parent = containers[kind]
        return 0 if not parent else depth(parent, (*visiting, kind)) + 1

    for kind in sorted(kinds, key=lambda k: (depth(k), k)):
        spec = kinds[kind]
        fname, selector, parts = engine._parse_file(spec)
        if fname is None:
            continue
        doc = docs[fname]
        parent = containers[kind]
        candidates = []
        for origin, data, path in (("account", doc.get("accountRaw"), doc["accountPath"]),
                                   ("product", doc["own"], doc["ownPath"])):
            if data is not None:
                candidates.extend((origin, mk, row, path, at) for mk, row, at in located(data, parts))
        effective = list(located(doc["effective"], parts))
        if [(mk, row) for mk, row, _ in effective] != list(engine._walk(doc["effective"], parts)):
            raise ValueError(f"ao_adapter_contract: address walker differs for {kind}")
        parent_ids = {}
        if parent:
            _, _, pp = engine._parse_file(kinds[parent])
            for _, value in engine._walk(doc["effective"], pp):
                pid = orig.get(parent, {}).get(id(value), "")
                for _, child in engine._walk(value, parts[len(pp):]):
                    parent_ids[id(child)] = pid
        start = len(records)
        counters = {}
        consumed = collections.Counter()
        got, row_orig = {}, {}
        for mk, raw, at in effective:
            if not engine._is_row(raw):
                continue
            row = dict(raw) if isinstance(raw, dict) else {
                spec["key"] if spec.get("key") and "{" not in spec["key"] else "id": raw}
            pid = parent_ids.get(id(raw), "")
            key, key_error = engine._leaf_id(kind, mk, parts[-1] if parts else "", row, spec.get("key"), pid, counters)
            row_orig[id(raw)] = key
            if key_error or engine.keyless(key):
                diagnostics.append({"code": "ao_record_key_missing", "severity": "error", "kind": kind, "key": key,
                                    "message": key_error or "missing authored key", "address": at})
            matches = [c for c in candidates if c[2] is raw] if isinstance(raw, dict) else [c for c in candidates if c[2] == raw]
            if not matches:
                paths = [p for p in (doc["accountPath"], doc["ownPath"]) if p]
                diagnostics.append({"code": "ao_mixed_origin_record", "severity": "error", "kind": kind, "key": key,
                                    "paths": paths, "address": at,
                                    "message": f"{kind}:{key} combines records from {' and '.join(paths)}"})
                continue
            slot = consumed[str(raw)] if not isinstance(raw, dict) else 0
            match = matches[min(slot, len(matches) - 1)]
            consumed[str(raw)] += 1
            origin, _, _, path, physical_at = match
            if origin == "account" and "*" in (row.get("products") or []):
                diagnostics.append({"code": "ao_membership_wildcard_unsupported", "severity": "error", "kind": kind,
                                    "key": key, "path": path,
                                    "message": "RECORD membership needs wildcard support before AO '*' account rows can be admitted"})
            if mk is not None and parts[-1].endswith("{}[]") and "group" not in row:
                row["group"] = mk
            got[key] = engine.account.unscoped(row)
            records.append({"kind": kind, "key": key, "path": path, "address": physical_at, "origin": origin,
                            "container": pid or None})
        if spec.get("key") is None:
            physical_order = {(c[3], c[4]): i for i, c in enumerate(candidates)}
            groups = collections.defaultdict(list)
            for record in records[start:]:
                groups[record["container"]].append(record)
            for group in groups.values():
                ordered = sorted(group, key=lambda r: (r["origin"] != "account", physical_order[(r["path"], r["address"])]))
                if group != ordered:
                    diagnostics.append({"code": "ao_source_order_unsupported", "severity": "error", "kind": kind,
                                        "paths": sorted({r["path"] for r in group}),
                                        "message": "positional composition interleaves mapping order across physical files"})
        output[kind], orig[kind] = got, row_orig
    return output, records
