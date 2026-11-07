import assert from "node:assert/strict";
import test from "node:test";
import { isPositional, ownerKey } from "../../templates/starters/ao-kb/module/views/knowledge/lib/knowledge.ts";
import {
  BINDING_MANIFESTS,
  codePath,
  declaredEvidence,
  declarer,
  fieldRows,
  modelObservations,
  readBindingFreshness,
  syncCompare,
  typeSpec,
} from "../../templates/starters/ao-kb/module/views/knowledge/lib/model.ts";

const str = (scalar) => ({ kind: "string", scalar });
const list = (...items) => ({ kind: "array", items: items.map(str) });
const obj = (entries) => ({
  kind: "object",
  entries: Object.entries(entries).map(([key, value]) => ({
    key,
    value: typeof value === "string" ? str(value) : value,
  })),
});
const FEATURES = "artifacts/_assets/_model/features.yaml";

test("endpoints open by their own fragment; positional records open as records", () => {
  const keyed = { notePath: FEATURES, kind: "EMBEDDED", fragment: "record:v1:def", nodeId: "record-v1-def" };
  assert.equal(ownerKey(keyed), `${FEATURES}#record:v1:def`);
  assert.equal(isPositional(keyed), false);
  const positional = { notePath: FEATURES, kind: "EMBEDDED", fragment: "record-pos:v1:h:123", nodeId: "record-pos-v1-123" };
  assert.equal(ownerKey(positional), `${FEATURES}#record-pos:v1:h:123`);
  assert.equal(isPositional(positional), true);
  const section = { notePath: "assertions/a.md", kind: "EMBEDDED", fragment: "connections-805", nodeId: "assertions/a.md#connections-805" };
  assert.equal(ownerKey(section), "assertions/a.md", "a Markdown section belongs to its note");
  assert.equal(ownerKey({ notePath: "assertions/a.md", kind: "NOTE" }), "assertions/a.md");
});

const spec = typeSpec(
  {
    type: {
      label: "Use case",
      annotations: { source: { key: { field: "id" }, title: ["name", "t"] } },
      fields: [
        { name: "v_name", sourcePath: "name", annotations: { field: { path: "name" } } },
        { name: "v_screens", annotations: { field: { path: "screens" }, references: { to: ["AOKBModel_screen"], values: "ELEMENTS", edge: { name: "serves", direction: "INBOUND" } } } },
        { name: "v_user", annotations: { field: { path: "user" }, references: { to: ["AOKBModel_user"], values: "VALUE", edge: { name: "serves", direction: "OUTBOUND" } } } },
        { name: "v_next", annotations: { field: { path: "next" }, references: { to: ["AOKBModel_flow"], values: "ELEMENTS" } } },
        { name: "v_supported_5f_by", annotations: { field: { path: "supported_by" }, references: { to: ["Assertion"], targetKey: ["$fileStem", "aoId"], values: "ELEMENTS", edge: { name: "evidenced-by", direction: "OUTBOUND" } } } },
      ],
    },
  },
  "AOKBModel_flow_single",
);

test("fields stay in authored order and each reference value shows its workspace link", () => {
  assert.equal(spec.label, "Use case");
  assert.equal(spec.references[0].name, "v_screens", "links are keyed by GraphQL field name");
  const to = (notePath, fragment) => ({ notePath, kind: fragment ? "EMBEDDED" : "NOTE", fragment });
  const link = (value, title, ref) => ({ value, resolved: !!ref, title: title ?? null, ref: ref ?? null });
  const rows = fieldRows(
    obj({ id: "FLOW-1", name: "Book", trigger: "Patient taps", screens: list("SCR-1", "SCR-gone", "*"), user: "PAT", next: list("FLOW-2"), supported_by: list("a-1", "a-missing"), implements: list("src/x.go") }),
    spec,
    {
      v_screens: [link("SCR-1", "Book screen", to("s.yaml", "record:v1:1")), link("SCR-gone"), link("*")],
      v_user: [link("PAT")],
      v_next: [link("FLOW-2", "Next flow", to("f.yaml", "record:v1:2"))],
      v_supported_5f_by: [link("a-1", "a-1 note title", to("assertions/a.md")), link("a-missing")],
    },
    new Map([["assertions/a.md", "A claim"]]),
  );
  assert.deepEqual(rows.map((r) => r.key), ["trigger", "screens", "user", "next", "supported_by"], "identity, title and code paths are shown elsewhere");
  assert.equal(rows[0].atoms, undefined, "a plain field is authored text");
  assert.deepEqual(rows[1].atoms.map((a) => a.target?.title ?? a.state), ["Book screen", "unresolved", "wildcard"]);
  assert.equal(rows[1].atoms[0].target.key, "s.yaml#record:v1:1");
  assert.deepEqual(rows[2].atoms.map((a) => a.state), ["unresolved"], "an ambiguous or missing key links nowhere");
  assert.deepEqual(rows[3].atoms.map((a) => [a.target?.title, a.state]), [["Next flow", "no edge"]], "a checked reference links but stays distinct from a relationship");
  assert.deepEqual(rows[4].atoms.map((a) => a.target?.title ?? a.state), ["A claim", "unresolved"], "a claim reads as it does elsewhere");
});

test("the declaring end of a stored edge is the one whose authored value names the other", () => {
  const flow = { key: "flow", aoValue: "FLOW-1", value: obj({ id: "FLOW-1", screens: list("SCR-1") }) };
  const screen = { key: "screen", aoValue: "SCR-1", value: obj({ id: "SCR-1" }) };
  assert.equal(declarer(screen, flow), "flow", "an INBOUND field stores screen → flow, authored by the flow");
  const feature = { key: "feature", aoValue: "F-1", value: obj({ flows: list("FLOW-1") }) };
  assert.equal(declarer(feature, flow), "feature");
  assert.equal(declarer({ key: "a" }, { key: "b" }), "a", "unknown values keep the stored source");
});

test("code paths are exact and never escape the configured roots", () => {
  assert.equal(codePath(" src/booking.go "), "src/booking.go");
  for (const raw of ["/etc/passwd", "../other/x.go", "src/../../x.go", "https://example.com/x.go", "C:\\x.go", ""])
    assert.equal(codePath(raw), undefined, raw);
});

test("model sync groups by file, names each row and changed claim, and missing dates are unknown", () => {
  const claim = (path, aoId, updatedAt) => ({ path, aoId, updatedAt, title: path, ref: { notePath: path, kind: "NOTE" } });
  const changed = claim("assertions/changed.md", "a-1", "2026-11-04");
  const older = claim("assertions/older.md", "a-2", "2026-10-01");
  const undated = claim("assertions/undated.md", "a-3", null);
  const claims = [changed, older, undated];
  const row = (key, notePath, evidence, supported) => ({
    key, ref: { notePath, kind: "EMBEDDED", fragment: key }, title: key, typeName: "AOKBModel_flow_single", aoValue: key,
    value: obj({ id: key, supported_by: list(...supported) }), evidence,
    links: {
      v_supported_5f_by: supported.map((value) => {
        const to = claims.find((a) => a.aoId === value);
        return { value, resolved: !!to, title: to?.title ?? null, ref: to?.ref ?? null };
      }),
    },
  });
  const rows = [
    row("F-1", FEATURES, [changed.path], ["a-1"]),
    row("F-2", FEATURES, [older.path], ["a-2"]),
    row("F-3", FEATURES, [undated.path, changed.path], ["a-3", "a-1", "a-gone"]),
    row("W-1", "flows.yaml", [changed.path], ["a-1"]),
  ];
  const result = modelObservations(
    [{ notePath: FEATURES, synced: "2026-10-20T09:00:00Z" }, { notePath: "flows.yaml", synced: null }],
    rows,
    { AOKBModel_flow_single: spec },
    [changed, older, undated],
  );
  assert.equal(result.sync.length, 1, "one entry per file, not per row");
  assert.deepEqual(result.sync[0].rows.map((r) => [r.record.key, r.changed.map((a) => a.aoId)]), [["F-1", ["a-1"]], ["F-3", ["a-1"]]]);
  assert.deepEqual(result.unknown.map((g) => [g.file.notePath, g.rows.map((r) => r.record.key)]), [[FEATURES, ["F-3"]], ["flows.yaml", ["W-1"]]]);
  assert.deepEqual(result.dangling.map((d) => [d.record.key, d.field, d.id]), [["F-3", "supported_by", "a-gone"]]);
  const capped = { ...row("F-4", FEATURES, [changed.path], ["a-1", "a-2"]), evidenceTruncated: true };
  const partial = modelObservations([], [capped], { AOKBModel_flow_single: spec }, [changed]);
  assert.deepEqual(partial.dangling, [], "a capped edge read does not change each value's native resolution");
  assert.deepEqual(partial.incomplete.map((r) => r.key), ["F-4"], "the capped read is disclosed");
  assert.equal(syncCompare("2026-11-04", "2026-11-04T00:00:00Z"), "not later");
  assert.equal(syncCompare("not a date", "2026-11-04"), "unknown");
});

test("declared evidence follows each value's native resolution, including file-stem citations", () => {
  const ref = { notePath: "assertions/claim-slug.md", kind: "NOTE" };
  const claim = { path: ref.notePath, aoId: "CLAIM-001", updatedAt: null, title: "Claim", ref };
  const record = {
    key: "F-1", ref: { notePath: FEATURES, kind: "EMBEDDED", fragment: "F-1" }, title: "F-1", typeName: "AOKBModel_flow_single", aoValue: "F-1",
    value: obj({ id: "F-1", supported_by: list("claim-slug", "CLAIM-001", "a-gone") }),
    evidence: [claim.path],
    links: {
      v_supported_5f_by: [
        { value: "claim-slug", resolved: true, title: "Claim", ref },
        { value: "CLAIM-001", resolved: true, title: "Claim", ref },
        { value: "a-gone", resolved: false, title: null, ref: null },
      ],
    },
  };
  const declared = declaredEvidence(record, spec, [claim]);
  assert.deepEqual(declared.map((d) => [d.field, d.id, d.assertion?.path]), [
    ["supported_by", "claim-slug", claim.path],
    ["supported_by", "CLAIM-001", claim.path],
    ["supported_by", "a-gone", undefined],
  ]);
  const result = modelObservations([], [record], { AOKBModel_flow_single: spec }, [claim]);
  assert.deepEqual(result.dangling.map((d) => d.id), ["a-gone"], "a citation by file stem is not dangling");
});

test("binding freshness is the compiler's published status, and only ready reads as current", async () => {
  const read = (model, knowledge = { status: "ready" }) => async (path) => {
    const doc = { [BINDING_MANIFESTS.model]: model, [BINDING_MANIFESTS.knowledge]: knowledge }[path];
    return doc === undefined || typeof doc === "string" ? doc : JSON.stringify(doc);
  };
  assert.deepEqual(await readBindingFreshness(read({ status: "ready" })), { state: "ready" });
  const failed = { status: "stale", sdlHash: "previous", diagnostics: [{ severity: "error", message: "metamodel.yaml: bad indentation" }] };
  const stale = await readBindingFreshness(read(failed));
  assert.equal(stale.state, "stale", "a failed refresh retains the previous generation, marked stale");
  assert.match(stale.notice, /^Product model bindings are stale/);
  assert.equal((await readBindingFreshness(read({ status: "ready" }, failed))).state, "stale", "stale knowledge admission is not current either");
  const error = await readBindingFreshness(read({ status: "error" }));
  assert.equal(error.state, "error");
  assert.match(error.notice, /^Product model bindings are incomplete/);
  for (const unreadable of [undefined, "{not json", { status: "pending" }]) {
    const unknown = await readBindingFreshness(read(unreadable));
    assert.equal(unknown.state, "unknown", JSON.stringify(unreadable));
    assert.match(unknown.notice, /could not be read/);
  }
  assert.equal((await readBindingFreshness(async () => { throw new Error("refused"); })).state, "unknown");
});
